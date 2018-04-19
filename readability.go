/*
 * Copyright (c) 2018, 奶爸<1@5.nu>
 * All rights reserved.
 */

package readability

import (
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
	"log"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	option             *Option
	scoreList          map[*goquery.Selection]float64
	whitespacePattern  = regexp.MustCompile(`\s*`)
	defaultTagsToScore = map[string]int{
		"section": 0,
		"h2":      0,
		"h3":      0,
		"h4":      0,
		"h5":      0,
		"h6":      0,
		"p":       0,
		"td":      0,
		"pre":     0,
	}
	divToPElement = map[string]int{
		"a":          0,
		"blockquote": 0,
		"dl":         0,
		"div":        0,
		"img":        0,
		"ol":         0,
		"p":          0,
		"pre":        0,
		"table":      0,
		"ul":         0,
		"select":     0,
	}
	bylinePattern               = regexp.MustCompile(`(?i)byline|author|dateline|writtenby|p-author`)
	okMaybeItsACandidatePattern = regexp.MustCompile(`(?i)and|article|body|column|main|shadow|app|container`)
	unlikelyCandidatesPattern   = regexp.MustCompile(`(?i)banner|breadcrumbs|combx|comment|community|cover-wrap|disqus|extra|foot|header|legends|menu|related|remark|replies|rss|shoutbox|sidebar|skyscraper|social|sponsor|supplemental|ad-break|agegate|pagination|pager|popup|yom-remote`)
	negativePattern             = regexp.MustCompile(`(?i)hidden|^hid$| hid$| hid |^hid |banner|combx|comment|com-|contact|foot|footer|footnote|masthead|media|meta|outbrain|promo|related|scroll|share|shoutbox|sidebar|skyscraper|sponsor|shopping|tags|tool|widget`)
	positivePattern             = regexp.MustCompile(`(?i)article|body|content|entry|hentry|h-entry|main|page|pagination|post|text|blog|story`)
	flags                       = flagCleanConditionally | flagStripUnlikely | flagWeightClasses
)

const (
	flagStripUnlikely      = 0x1
	flagWeightClasses      = 0x2
	flagCleanConditionally = 0x4
)

//Option 解析配置
type Option struct {
	MaxNodeNum      int
	Debug           bool
	ArticleByline   string
	NbTopCandidates int
}

//Metadata 文章摘要信息
type Metadata struct {
	Title   string
	Excerpt string
	Byline  string
}

//Article 解析结果
type Article struct {
	Title  string
	Byline string
}

//Parse 进行解析
func Parse(s string, opt Option) (*Article, error) {
	d, err := goquery.NewDocumentFromReader(strings.NewReader(s))
	if err != nil {
		return nil, err
	}
	defaultOption(&opt)
	scoreList = make(map[*goquery.Selection]float64)
	// 超出最大解析限制
	if opt.MaxNodeNum > 0 && len(d.Nodes) > opt.MaxNodeNum {
		return nil, fmt.Errorf("Node 数量超出最大限制：%d 。 ", opt.MaxNodeNum)
	}
	article := new(Article)
	// 预处理HTML文档以提高可读性。 这包括剥离JavaScript，CSS和处理没用的标记等内容。
	prepDocument(d)

	// 获取文章的摘要和作者信息
	md := getArticleMetadata(d)
	article.Title = md.Title

	//todo 提取文章正文
	grabArticle(d)

	return article, nil
}

// 提取文章正文
func grabArticle(d *goquery.Document) *goquery.Selection {
	l("**** grabArticle ****")
	isPaging := d != nil
	page := d.Find("body").First()
	if page.Children().Length() == 0 {
		return nil
	}

	selectionsToScore := make([]*goquery.Selection, 0)
	stripUnlikelyCandidates := flagIsActive(flagStripUnlikely)
	sel := d.First()
	for sel != nil {
		node := sel.Get(0)

		// 首先，节点预处理。 清理看起来很糟糕的垃圾节点（比如类名为“comment”的垃圾节点），
		// 并将div转换为P标签，清理空节点。
		matchString, _ := sel.Attr("id")
		class, _ := sel.Attr("class")
		matchString += " " + class
		// 作者信息行
		if checkByline(sel, matchString) {
			l("checkByline", node.Data, node.Attr)
			sel = removeAndGetNext(sel)
			continue
		}
		// 清理垃圾标签
		if stripUnlikelyCandidates && len(ts(matchString)) > 0 {
			if unlikelyCandidatesPattern.MatchString(matchString) &&
				!okMaybeItsACandidatePattern.MatchString(matchString) &&
				node.Data != "body" &&
				node.Data != "a" {
				l("Removing unlikely candidate - " + matchString)
				sel = removeAndGetNext(sel)
				continue
			}
		}
		// 清理不含任何内容的 DIV, SECTION, 和 HEADER
		tags := map[string]int{"div": 0, "section": 0, "header": 0, "h1": 0, "h2": 0, "h3": 0, "h4": 0, "h5": 0, "h6": 0}
		if _, has := tags[node.Data]; has && len(ts(sel.Text())) == 0 {
			sel = removeAndGetNext(sel)
			continue
		}
		// 内容标签，加分项
		if _, has := defaultTagsToScore[node.Data]; has {
			selectionsToScore = append(selectionsToScore, sel)
		}
		if node.Data == "div" {
			// 将只包含一个 p 标签的 div 标签去掉，将 p 提出来
			if hasSinglePInsideElement(sel) {
				next := getNextSelection(sel, true)
				sel.ReplaceWithSelection(sel.Children().First())
				selectionsToScore = append(selectionsToScore, sel)
				sel = next
				continue
			} else if !hasChildBlockElement(sel) {
				// 节点不含有块级元素
				replaceSelectionTags(sel, "p")
				selectionsToScore = append(selectionsToScore, sel)
			} else {
				// 含有块级元素
				for node := sel.Get(0).FirstChild; node != nil; node = node.NextSibling {
					if node.Type == html.TextNode && len(ts(node.Data)) > 0 {
						ts := d.FindNodes(node)
						tt := node.Data
						replaceSelectionTags(ts, "p")
						node.Attr = []html.Attribute{
							{
								Key: "class",
								Val: "readability-styled",
							},
							{
								Key: "style",
								Val: "display:inline;",
							},
						}
						ts.SetText(tt)
					}
				}
			}
		}
		sel = getNextSelection(sel, false)
	}

	/*
	* 循环浏览所有段落，并根据它们的外观如何分配给他们一个分数。
	* 然后将他们的分数添加到他们的父节点。
	* 分数由 commas，class 名称 等的 数目决定。也许最终链接密度。
	**/
	candidates := make([]*goquery.Selection, 0)
	for _, sel = range selectionsToScore {
		// 节点或节点的父节点为空，跳过
		if sel.Parent().Length() == 0 || sel.Length() == 0 {
			continue
		}
		// 如果该段落少于25个字符，跳过
		if utf8.RuneCountInString(sel.Text()) < 25 {
			continue
		}
		// 排除没有祖先的节点。
		ancestors := getSelectionAncestors(sel, 3)
		if len(ancestors) == 0 {
			continue
		}

		contentScore := 0.0

		// 为段落本身添加一个基础分
		contentScore++

		innerText := sel.Text()
		// 在此段落内为所有逗号添加分数。
		contentScore += float64(strings.Count(innerText, ","))
		contentScore += float64(strings.Count(innerText, "，"))

		// 本段中每100个字符添加一分。 最多3分。
		contentScore += math.Min(float64(utf8.RuneCountInString(innerText)/100), 3)

		// 给祖先初始化并评分。
		for level, ancestor := range ancestors {
			if ancestor.Length() == 0 {
				continue
			}
			if scoreList[ancestor] == 0 {
				// 初始化节点分数
				initializeScoreSelection(ancestor)
				candidates = append(candidates, ancestor)
			}
			// 节点加分规则：
			// - 父母：1（不划分）
			// - 祖父母：2
			// - 祖父母：祖先等级* 3
			divider := 1.0
			switch level {
			case 0:
				divider = 1
				break
			case 1:
				divider = 2
				break
			case 2:
				divider = float64(level) * 3
				break
			}
			scoreList[ancestor] += contentScore / divider
		}
	}

	// 在我们计算出分数后，循环遍历我们找到的所有可能的候选节点，并找到分数最高的候选节点。
	topCandidates := make([]*goquery.Selection, 0)
	for _, candidate := range candidates {
		candidateScore := 0.00
		// 根据链接密度缩放最终候选人分数。 良好的内容应该有一个相对较小的链接密度（5％或更少），并且大多不受此操作的影响。
		candidateScore = scoreList[candidate] * (1 - getLinkDensity(candidate))
		scoreList[candidate] = candidateScore

		l("Candidate:", candidate.Get(0).Data, "[", candidate.Get(0).Attr, "]", "with score", candidateScore)

		for i := 0; i < option.NbTopCandidates; i++ {
			var candi *goquery.Selection
			if i < len(topCandidates) {
				candi = topCandidates[i]
			}
			if candi == nil || candidateScore > scoreList[topCandidates[i]] {
				// 分数越高排名越靠前
				topCandidates = append(topCandidates, candidate)
				copy(topCandidates[i+1:], topCandidates[i:])
				topCandidates[i] = candidate
				// 限制数量
				if len(topCandidates) > option.NbTopCandidates {
					topCandidates = topCandidates[:len(topCandidates)-1]
				}
				break
			}
		}
	}

	var topCandidate, parentOfTopCandidate *goquery.Selection
	needToCreateTopCandidate := len(topCandidates) == 0
	if !needToCreateTopCandidate {
		topCandidate = topCandidates[0]
	}

	// 如果我们还没有topCandidate，那就把 body 作为 topCandidate。
	// 我们还必须复制body节点，以便我们可以修改它。
	if needToCreateTopCandidate || topCandidate.Get(0).Data == "body" {
		needToCreateTopCandidate = true
		// 将所有页面的子项移到topCandidate中
		topCandidate = new(goquery.Selection)
		topCandidate.Nodes = []*html.Node{{
			Type:      html.ElementNode,
			Namespace: "div",
			Data:      "div",
		}}
		page.Children().Each(func(i int, s *goquery.Selection) {
			l("Moving child out:", s.Get(0))
			topCandidate.AppendSelection(s)
		})
		page.AppendSelection(topCandidate)

		initializeScoreSelection(topCandidate)
	} else if !needToCreateTopCandidate {
		// 如果它包含（至少三个）属于`topCandidates`数组并且其分数与
		// 当前`topCandidate`节点非常接近的节点，则找到一个更好的顶级候选节点。
		alternativeCandidateAncestors := make([]map[*goquery.Selection]int, 0)
		for _, c := range topCandidates {
			if scoreList[c]/scoreList[topCandidate] >= 0.75 {
				t := make(map[*goquery.Selection]int)
				for _, a := range getSelectionAncestors(c, 0) {
					t[a] = 0
				}
				alternativeCandidateAncestors = append(alternativeCandidateAncestors, t)
			}
		}
		const MinimumTopCandidates = 3
		if len(alternativeCandidateAncestors) >= MinimumTopCandidates {
			parentOfTopCandidate = topCandidate.Parent()
			for parentOfTopCandidate.Get(0).Data != "body" {
				listsContainingThisAncestor := 0
				for i := 0; i < len(alternativeCandidateAncestors) && listsContainingThisAncestor < MinimumTopCandidates; i++ {
					if _, has := alternativeCandidateAncestors[i][topCandidates[i]]; has {
						listsContainingThisAncestor += 1
					}
				}
				if listsContainingThisAncestor > MinimumTopCandidates {
					topCandidate = parentOfTopCandidate
					break
				}
				parentOfTopCandidate = parentOfTopCandidate.Parent()
			}
		}
		if scoreList[topCandidate] == 0 {
			initializeScoreSelection(topCandidate)
		}

		/*
		 * 由于我们的奖金制度，节点的父节点可能会有自己的分数。 他们得到节点的一半。 不会有比我们的
		 * topCandidate分数更高的节点，但是如果我们在树的前几个步骤中看到分数增加，这是一个体面
		 * 的信号，可能会有更多的内容潜伏在我们想要的其他地方 统一英寸下面的兄弟姐妹的东西做了一些
		 * - 但只有当我们已经足够高的DOM树。
		**/
		parentOfTopCandidate = topCandidate
		lastScore := scoreList[topCandidate]
		// 分数不能太低。
		scoreThreshold := lastScore / 3
		for parentOfTopCandidate.Get(0).Data != "body" {
			if scoreList[parentOfTopCandidate] == 0 {
				initializeScoreSelection(parentOfTopCandidate)
				continue
			}
			if scoreList[parentOfTopCandidate] < scoreThreshold {
				break
			}
			if scoreList[parentOfTopCandidate] > lastScore {
				// 找到了一个更好的节点
				topCandidate = parentOfTopCandidate
				break
			}
			lastScore = scoreList[parentOfTopCandidate]
			parentOfTopCandidate = parentOfTopCandidate.Parent()
		}

		// 如果最上面的候选人是唯一的孩子，那就用父母代替。 当相邻内容实际位于父节点的兄弟节点中时，这将有助于兄弟连接逻辑。
		parentOfTopCandidate = topCandidate.Parent()
		for parentOfTopCandidate.Get(0).Data != "body" && parentOfTopCandidate.Children().Length() == 1 {
			topCandidate = parentOfTopCandidate
			parentOfTopCandidate = topCandidate.Parent()
		}
		if scoreList[parentOfTopCandidate] == 0 {
			initializeScoreSelection(parentOfTopCandidate)
		}
	}

	// 现在我们有了最好的候选人，通过它的兄弟姐妹查看可能也有关联的内容。 诸如前导，内容被我们删除的广告分割等
	articleContent := &goquery.Selection{Nodes: []*html.Node{{Type: html.ElementNode, Namespace: "div", Data: "div"}}}
	if isPaging {
		articleContent.SetAttr("id", "readability-content")
	}

	siblingScoreThreshold := math.Max(10, scoreList[topCandidate]*0.2)
	// 让潜在的顶级候选人的父节点稍后尝试获取文本方向。
	parentOfTopCandidate = topCandidate.Parent()
	sibling := parentOfTopCandidate.Children().First()
	for sibling.Length() > 0 {
		willAppend := false
		var next *goquery.Selection
		l("Looking at sibling node:", sibling.Get(0), scoreList[sibling])
		if sibling == topCandidate {
			willAppend = true
		} else {
			contentBonus := 0.0

			// 如果兄弟节点和顶级候选人具有相同的类名示例，则给予奖励
			if sibling.AttrOr("class", "") ==
				topCandidate.AttrOr("class", "") &&
				topCandidate.AttrOr("class", "") != "" {
				contentBonus += scoreList[topCandidate] * 0.2
			}

			if scoreList[sibling]+contentBonus >= siblingScoreThreshold {
				willAppend = true
			} else if sibling.Get(0).Data == "p" {
				linkDensity := getLinkDensity(sibling)
				innerText := sibling.Text()
				textLen := len(innerText)

				if textLen > 80 && linkDensity < 0.25 {
					willAppend = true
				} else if textLen < 80 && textLen > 0 && linkDensity == 0 &&
					regexp.MustCompile(`\.( |$)`).MatchString(innerText) {
					willAppend = true
				}
			}
		}

		if willAppend {
			alter := map[string]int{
				"div": 0, "article": 0, "section": 0, "p": 0,
			}
			sn := sibling.Get(0)
			if _, has := alter[sn.Data]; has {
				sn.Data = "div"
				sn.Namespace = "div"
			}
			next = sibling.Next()
			articleContent.AppendSelection(sibling)
			sibling = next
		} else {
			sibling = sibling.Next()
		}
	}

	logText, _ := articleContent.Html()
	l("Article content pre-prep:", logText)

	//todo prepArticle
	prepArticle(articleContent)

	return articleContent
}

//
func prepArticle(a *goquery.Selection) {

}

// 获取连接密度
func getLinkDensity(s *goquery.Selection) float64 {
	textLength := len(s.Text())
	if textLength == 0 {
		return 0
	}
	linkLength := 0.0
	s.Find("a").Each(func(i int, is *goquery.Selection) {
		linkLength += float64(len(is.Text()))
	})
	return linkLength / float64(textLength)
}

// 默认配置
func defaultOption(o *Option) {
	if o.NbTopCandidates == 0 {
		o.NbTopCandidates = 5
	}
	option = o
}

// 初始化节点分数
func initializeScoreSelection(s *goquery.Selection) {
	switch s.Get(0).Data {
	case "div":
		scoreList[s] += 5
		break
	case "pre":
	case "td":
	case "blockquote":
		scoreList[s] += 3
		break
	case "address":
	case "ol":
	case "ul":
	case "dl":
	case "dd":
	case "dt":
	case "li":
	case "form":
		scoreList[s] -= 3
		break
	case "h1":
	case "h2":
	case "h3":
	case "h4":
	case "h5":
	case "h6":
	case "th":
		scoreList[s] -= 5
		break
	}
	// 获取元素类/标识权重。 使用正则表达式来判断这个元素是好还是坏。
	getClassWeight(s)

	// 如果为 0 置负0.00001
	if scoreList[s] == 0 {
		scoreList[s] = -0.00001
	}
}

// 获取元素类/标识权重。 使用正则表达式来判断这个元素是好还是坏。
func getClassWeight(s *goquery.Selection) {
	if !flagIsActive(flagWeightClasses) {
		return
	}
	// 寻找一个特殊的类名
	className, has := s.Attr("class")
	if has && len(className) > 0 {
		if negativePattern.MatchString(className) {
			scoreList[s] -= 25
		}
		if positivePattern.MatchString(className) {
			scoreList[s] += 25
		}
	}
	// 寻找一个特殊的ID
	id, has := s.Attr("id")
	if has && len(className) > 0 {
		if negativePattern.MatchString(id) {
			scoreList[s] -= 25
		}
		if positivePattern.MatchString(id) {
			scoreList[s] += 25
		}
	}
}

// 向上获取祖先节点
func getSelectionAncestors(s *goquery.Selection, i int) []*goquery.Selection {
	ancestors := make([]*goquery.Selection, 0)
	count := 0
	for s.Parent().Length() > 0 {
		count++
		s = s.Parent()
		ancestors = append(ancestors, s)
		if i > 0 && i == count {
			return ancestors
		}
	}
	return ancestors
}

// 节点是否含有块级元素
func hasChildBlockElement(s *goquery.Selection) bool {
	flag := false
	s.Children().EachWithBreak(func(i int, is *goquery.Selection) bool {
		if _, has := divToPElement[is.Get(0).Data]; has {
			flag = true
			return false
		}
		if hasChildBlockElement(is) {
			flag = true
			return false
		}
		return true
	})
	return flag
}

// 是不是只包含一个 p 标签的节点
func hasSinglePInsideElement(s *goquery.Selection) bool {
	if s.Children().Length() != 1 || s.Children().Get(0).Data != "p" {
		return false
	}
	return ts(s.Children().Text()) == ts(s.Text())
}

// 删除并获取下一个
func removeAndGetNext(s *goquery.Selection) *goquery.Selection {
	t := getNextSelection(s, true)
	s.Remove()
	return t
}

/*
 * 从 node 开始遍历DOM，
 * 如果 ignoreSelfAndKids 为 true 则不遍历子 element
 * 改为遍历 兄弟 和 父级兄弟 element
 */
func getNextSelection(s *goquery.Selection, ignoreSelfAndChildren bool) *goquery.Selection {
	if s.Length() == 0 {
		return nil
	}
	// 如果 ignoreSelfAndKids 不为 true 且 node 有子 element 返回第一个子 element
	if !ignoreSelfAndChildren && s.Children().Length() > 0 {
		t := s.Children().First()
		if t.Length() > 0 {
			return t
		}
	}
	// 然后是兄弟 element
	if s.Next().Length() > 0 {
		return s.Next()
	}
	// 最后，父节点的兄弟 element
	//（因为这是深度优先遍历，我们已经遍历了父节点本身）。
	for {
		s = s.Parent()
		t := s.Next()
		if t.Length() == 0 {
			if s.Parent().Length() > 0 {
				continue
			}
			break
		} else {
			return t
		}
	}
	return nil
}

// 是否是作者信息
func checkByline(s *goquery.Selection, matchString string) bool {
	if len(option.ArticleByline) > 0 {
		return false
	}
	innerText := s.Text()
	if (s.AttrOr("rel", "") == "author" || bylinePattern.MatchString(matchString)) && isValidByline(innerText) {
		option.ArticleByline = ts(innerText)
		return true
	}
	return false
}

// 合理的作者信息行
func isValidByline(line string) bool {
	length := utf8.RuneCountInString(ts(line))
	return length > 0 && length < 100
}

// 是否启用
func flagIsActive(flag int) bool {
	return flags&flag > 0
}

// 从 metadata 获取文章的摘要和作者信息
func getArticleMetadata(d *goquery.Document) Metadata {
	var md Metadata
	values := make(map[string]string)

	namePattern := regexp.MustCompile(`^\s*((twitter)\s*:\s*)?(description|title)\s*$`)
	propertyPattern := regexp.MustCompile(`^\s*og\s*:\s*(description|title)\s*$`)

	// 提取元数据
	d.Find("meta").Each(func(i int, s *goquery.Selection) {
		elementName, _ := s.Attr("name")
		elementProperty, _ := s.Attr("property")

		if _, has := map[string]string{elementName: "", elementProperty: ""}["author"]; has {
			md.Byline, _ = s.Attr("content")
		}

		var name string
		if namePattern.MatchString(elementName) {
			name = elementName
		} else if propertyPattern.MatchString(elementProperty) {
			name = elementProperty
		}

		if len(name) > 0 {
			elementContent, _ := s.Attr("content")
			if len(elementContent) > 0 {
				name = whitespacePattern.ReplaceAllString(strings.ToLower(name), " ")
				values[name] = ts(elementContent)
			}
		}
	})

	// 取文章摘要
	if val, has := values["description"]; has {
		md.Excerpt = val
	} else if val, has := values["og:description"]; has {
		md.Excerpt = val
	} else if val, has := values["twitter:description"]; has {
		md.Excerpt = val
	}

	// 取网页标题
	md.Title = getArticleTitle(d)
	if len(md.Title) < 1 {
		if val, has := values["og:title"]; has {
			md.Title = val
		} else if val, has := values["twitter:title"]; has {
			md.Title = val
		}
	}

	return md
}

// 获取文章标题
func getArticleTitle(d *goquery.Document) string {
	titleSplitPattern := regexp.MustCompile(`(.*)[\|\-\\\/>»](.*)`)
	var title, originTitle string

	// 从 title 标签获取标题
	elementTitle := d.Find("title").First()
	originTitle = ts(elementTitle.Text())
	title = originTitle

	hasSplit := titleSplitPattern.MatchString(title)
	if hasSplit {
		// 是否有分隔符，判断主题在前还是在后
		title = titleSplitPattern.ReplaceAllString(originTitle, "$1")
		if utf8.RuneCountInString(title) < 3 {
			title = titleSplitPattern.ReplaceAllString(originTitle, "$2")
		}
	} else if strings.Index("：", originTitle) != -1 || strings.Index(":", originTitle) != -1 {
		// 判断是否有 "：" 符号
		flag := false
		d.Find("h1,h2").EachWithBreak(func(i int, s *goquery.Selection) bool {
			// 提取的标题是否在正文中存在
			if ts(s.Text()) == title {
				flag = true
			}
			return !flag
		})
		if !flag {
			// 如果不存在取 ":" 前后的文字
			i := strings.LastIndex(originTitle, "：")
			if i == -1 {
				i = strings.LastIndex(originTitle, ":")
			} else {
				title = originTitle[i:]
				if utf8.RuneCountInString(title) < 3 {
					i = strings.Index(originTitle, "：")
					if i == -1 {
						i = strings.Index(originTitle, ":")
					} else {
						title = originTitle[i:]
					}
				} else if utf8.RuneCountInString(originTitle[0:i]) > 5 {
					title = originTitle
				}
			}
		}
	} else if utf8.RuneCountInString(title) > 150 || utf8.RuneCountInString(title) < 15 {
		// 如果标题字数很离谱切只有一个h1标签，取其文字
		h1s := d.Find("h1")
		if h1s.Length() == 1 {
			title = ts(h1s.First().Text())
		}
	}

	titleCount := utf8.RuneCountInString(title)

	if titleCount < 4 && (!hasSplit || utf8.RuneCountInString(titleSplitPattern.ReplaceAllString(originTitle, "$1$2"))-1 != titleCount) {
		// 如果提取的标题很短 取网页标题
		title = originTitle
	}

	return title
}

// 预处理HTML文档以提高可读性。 这包括剥离JavaScript，CSS和处理没用的标记等内容。
func prepDocument(d *goquery.Document) {
	// 移除所有script标签
	removeTags("script,noscript", d)

	// 移除所有style标签
	removeTags("style", d)

	// 将多个连续的<br>替换成<p>
	replaceBrs(d)

	// 将所有的font替换成span
	replaceSelectionTags(d.Find("font"), "span")
}

// 将多个连续的<br>替换成<p>
func replaceBrs(d *goquery.Document) {
	d.Find("br").Each(func(i int, br *goquery.Selection) {
		// 当有 2 个或多个 <br> 时替换成 <p>
		replaced := false

		// 如果找到了一串相连的 <br>，忽略中间的空格，移除所有相连的 <br>
		next := nextElement(br.Get(0).NextSibling)
		for next != nil && next.Data == "br" {
			replaced = true
			t := nextElement(next.NextSibling)
			next.Parent.RemoveChild(next)
			next = t
		}

		// 如果移除了 <br> 链，将其余的 <br> 替换为 <p>，将其他相邻节点添加到 <p> 下。直到遇到第二个 <br>
		if replaced {
			pNode := br.Get(0)
			pNode.Data = "p"
			pNode.Namespace = "p"
			br.Text()

			next = pNode.NextSibling
			for next != nil {
				// 如果我们遇到了其他的 <br><br> 结束添加
				if pNode.Data == "br" {
					innerNext := nextElement(next)
					if innerNext.Data == "br" {
						break
					}
				}
				// 否则将节点添加为 <p> 的子节点
				temp := next.NextSibling
				next.Parent.RemoveChild(next)
				next.Parent = nil
				next.PrevSibling = nil
				next.NextSibling = nil
				pNode.AppendChild(next)
				next = temp
			}
		}
	})
}

// 获取下一个Element
func nextElement(n *html.Node) *html.Node {
	for n != nil &&
		n.Type != html.ElementNode && (whitespacePattern.MatchString(n.Data) ||
		n.Type == html.CommentNode) {
		n = n.NextSibling
	}
	return n
}

// 移除所有 tags 标签
// 例如 "script,noscript" 清理所有script
func removeTags(tags string, d *goquery.Document) {
	d.Find(tags).Each(func(i int, s *goquery.Selection) {
		s.Remove()
	})
}

// 将所有的s的标签替换成tag
func replaceSelectionTags(s *goquery.Selection, tag string) {
	s.Each(func(i int, is *goquery.Selection) {
		l("_setNodeTag", i, ts(is.Get(0).Data), tag)
		n := is.Get(0)
		n.Type = html.ElementNode
		n.Data = tag
		n.Namespace = tag
	})
}

// 调试日志
func l(ms ...interface{}) {
	if option.Debug {
		log.Println(ms...)
	}
}

// TrimSpace
func ts(s string) string {
	return strings.TrimSpace(s)
}
