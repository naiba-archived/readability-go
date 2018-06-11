/*
 * Copyright (c) 2018, 奶爸<1@5.nu>
 * All rights reserved.
 */

package readability

import (
	"errors"
	"fmt"
	"log"
	"math"

	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

var (
	whitespacePattern  = regexp.MustCompile(`\s{2,}`)
	defaultTagsToScore = map[string]struct{}{
		"section": {},
		"h2":      {},
		"h3":      {},
		"h4":      {},
		"h5":      {},
		"h6":      {},
		"p":       {},
		"td":      {},
		"pre":     {},
	}
	divToPElement = map[string]struct{}{
		"a":          {},
		"blockquote": {},
		"dl":         {},
		"div":        {},
		"img":        {},
		"ol":         {},
		"p":          {},
		"pre":        {},
		"table":      {},
		"ul":         {},
		"select":     {},
	}
	bylinePattern               = regexp.MustCompile(`(?i)byline|author|dateline|writtenby|p-author`)
	okMaybeItsACandidatePattern = regexp.MustCompile(`(?i)and|article|body|column|main|shadow|app|container`)
	unlikelyCandidatesPattern   = regexp.MustCompile(`(?i)banner|breadcrumbs|combx|comment|community|cover-wrap|disqus|extra|foot|header|legends|menu|related|remark|replies|rss|shoutbox|sidebar|skyscraper|social|sponsor|supplemental|ad-break|agegate|pagination|pager|popup|yom-remote`)
	negativePattern             = regexp.MustCompile(`(?i)hidden|^hid$| hid$| hid |^hid |banner|combx|comment|com-|contact|foot|footer|footnote|masthead|media|meta|outbrain|promo|related|scroll|share|shoutbox|sidebar|skyscraper|sponsor|shopping|tags|tool|widget`)
	positivePattern             = regexp.MustCompile(`(?i)article|body|content|entry|hentry|h-entry|main|page|pagination|post|text|blog|story`)
	videoLinkPattern            = regexp.MustCompile(`(?i)\/\/(www\.)?(dailymotion|youtube|youtube-nocookie|player\.vimeo|v\.youku|v\.qq)\.com`)
	sharePattern                = regexp.MustCompile(`(?i)share`)
	flags                       = map[int]bool{flagStripUnlikely: true, flagCleanConditionally: true, flagWeightClasses: true}
	presentationalAttributes    = []string{"align", "background", "bgcolor", "border", "cellpadding", "cellspacing", "frame", "hspace", "rules", "style", "valign", "vspace"}
	deprecatedSizeAttributeElem = []string{"table", "th", "td", "hr", "pre"}
	// 注释掉的元素符合短语内容，但在放入段落时往往会因可读性而被删除，所以我们在此忽略它们。
	phrasingElements = []string{
		// "CANVAS", "IFRAME", "SVG", "VIDEO",
		"abbr", "audio", "b", "bdo", "br",
		"button", "cite", "code", "data",
		"datalist", "dfn", "em", "embed", "i", "img", "input", "kbd", "label",
		"mark", "math", "meter", "noscript", "object", "output", "progress", "q",
		"ruby", "samp", "script", "select", "small", "span", "strong", "sub",
		"sup", "textarea", "time", "var", "wbr",
	}
	classesToPreserve = []string{"page"}
)

const (
	flagStripUnlikely = iota
	flagWeightClasses
	flagCleanConditionally
	defaultCharThreshold
)

//Option 解析配置
type Option struct {
	MaxNodeNum        int
	Debug             bool
	NbTopCandidates   int
	CharThreshold     int
	PageURL           string
	ClassesToPreserve []string
}

type metadata struct {
	Title   string
	Excerpt string
	Byline  string
}

//Readability 网页正文提取
type Readability struct {
	article              *Article
	option               *Option
	scoreList            map[*html.Node]float64
	readabilityDataTable map[*html.Node]bool
	attempts             []*goquery.Selection

	dom *goquery.Document
}

//Article 解析结果
type Article struct {
	URL         string
	Title       string
	Byline      string
	Dir         string
	Content     string
	TextContent string
	Length      int
	Excerpt     string
}

//New 新建一个对象
func New(o Option) *Readability {
	if o.NbTopCandidates == 0 {
		o.NbTopCandidates = 5
	}
	if o.CharThreshold == 0 {
		o.CharThreshold = defaultCharThreshold
	}
	o.ClassesToPreserve = append(o.ClassesToPreserve, classesToPreserve...)
	return &Readability{article: new(Article),
		scoreList:            make(map[*html.Node]float64),
		readabilityDataTable: make(map[*html.Node]bool),
		attempts:             make([]*goquery.Selection, 0),
		option:               &o,
	}
}

//Parse 进行解析
func (read *Readability) Parse(s string) (*Article, error) {
	var err error
	read.dom, err = goquery.NewDocumentFromReader(strings.NewReader(s))
	if err != nil {
		return nil, err
	}

	// 超出最大解析限制
	if read.option.MaxNodeNum > 0 && len(read.dom.Nodes) > read.option.MaxNodeNum {
		return nil, fmt.Errorf("Node 数量超出最大限制：%d 。 ", read.option.MaxNodeNum)
	}
	// 预处理HTML文档以提高可读性。 这包括剥离JavaScript，CSS和处理没用的标记等内容。
	read.prepDocument()

	// 获取文章的摘要和作者信息
	md := read.getArticleMetadata()
	read.article.Title = md.Title

	// 提取文章正文
	articleContent := read.grabArticle()

	if articleContent == nil {
		return nil, errors.New("没能获取到主体")
	}
	oh, _ := goquery.OuterHtml(articleContent)
	read.l("Grabbed: ", oh)

	// 后期处理
	read.postProcessContent(articleContent)

	// 如果我们没有在文章的元数据中找到摘录，请使用文章的第一段作为摘录。 这用于显示文章内容的预览。
	if len(md.Excerpt) == 0 {
		paragraphs := articleContent.Find("p").First()
		if len(ts(paragraphs.Text())) > 0 {
			md.Excerpt = ts(paragraphs.Text())
		}
	}

	read.article.Title = normalizeSpace(md.Title)
	if len(read.article.Byline) > 0 {
		read.article.Byline = normalizeSpace(read.article.Byline)
	} else {
		read.article.Byline = normalizeSpace(md.Byline)
	}
	read.article.URL = read.option.PageURL
	read.article.TextContent = normalizeSpace(ts(articleContent.Text()))
	read.article.Content, err = goquery.OuterHtml(articleContent)
	read.article.Content = normalizeSpace(read.article.Content)
	read.article.Length = len(read.article.TextContent)
	read.article.Excerpt = md.Excerpt

	return read.article, err
}

// 根据需要运行对文章内容的任何后期处理修改。
func (read *Readability) postProcessContent(articleContent *goquery.Selection) {
	// Readability 无法打开相关uris，因此我们将它们转换为绝对uris。
	read.fixRelativeUris(articleContent)
	// 删除 class
	cleanClasses(articleContent)
}

// 从给定子树中的每个元素中除去class =“”属性，除了匹配CLASSES_TO_PRESERVE的
// 元素和来自options对象的classesToPreserve数组。
func cleanClasses(articleContent *goquery.Selection) {
	articleContent.Children().Each(func(i int, sel *goquery.Selection) {
		cleanClasses(sel)
		class, has := sel.Attr("class")
		if has {
			for _, cls := range classesToPreserve {
				if strings.Contains(cls, class) {
					return
				}
			}
			sel.RemoveAttr("class")
		}
	})
}

// 将给定元素中的每个<a>和<img> uri转换为绝对URI，忽略#ref URI。
func (read *Readability) fixRelativeUris(articleContent *goquery.Selection) {
	if !strings.HasPrefix(read.option.PageURL, "http://") && !strings.HasPrefix(read.option.PageURL, "https://") {
		read.option.PageURL = "http://" + read.option.PageURL
	}
	documentURL := read.option.PageURL[:strings.Index(read.option.PageURL[8:], "/")+8]
	baseURL, has := read.dom.Find("base").First().Attr("href")
	if !has || len(ts(baseURL)) == 0 {
		baseURL = read.option.PageURL[:strings.LastIndex(read.option.PageURL, "/")+1]
	}
	toAbsoluteURI := func(url string) string {
		if len(url) == 0 {
			return ""
		}
		if strings.HasPrefix(url, "#") || strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "//") {
			return url
		} else if strings.HasPrefix(url, "/") {
			return documentURL + url
		} else {
			return baseURL + "/" + url
		}
	}
	articleContent.Find("a").Each(func(i int, a *goquery.Selection) {
		href, has := a.Attr("href")
		if has {
			if strings.HasPrefix(href, "javascript:") {
				a.Get(0).Type = html.TextNode
				a.Get(0).Data = a.Text()
			} else {
				a.SetAttr("href", toAbsoluteURI(href))
			}
		}
	})
	articleContent.Find("img").Each(func(i int, img *goquery.Selection) {
		src, has := img.Attr("src")
		if has {
			img.SetAttr("src", toAbsoluteURI(src))
		}
	})
}

// 提取文章正文
func (read *Readability) grabArticle() *goquery.Selection {
	read.l("**** grabArticle ****")
	isPaging := read.dom != nil
	originDoc := goquery.CloneDocument(read.dom)

	page := read.dom.Find("body").First()
	if page.Children().Length() == 0 {
		return nil
	}

	for {
		selectionsToScore := make([]*goquery.Selection, 0)
		stripUnlikelyCandidates := flagIsActive(flagStripUnlikely)
		sel := page.First()
		for sel != nil {
			node := sel.Get(0)

			// 首先，节点预处理。 清理看起来很糟糕的垃圾节点（比如类名为“comment”的垃圾节点），
			// 并将div转换为P标签，清理空节点。
			matchString := sel.AttrOr("class", "") + " " + sel.AttrOr("id", "")

			if !read.isProbablyVisible(sel) {
				read.l("Removing hidden node - ", matchString)
				sel = removeAndGetNext(sel)
				continue
			}

			// 如果是作者信息 node，删除并将指针移到下一个 node
			if read.checkByline(sel, matchString) {
				read.l("checkByline", node.Data, node.Attr)
				sel = removeAndGetNext(sel)
				continue
			}

			// 清理垃圾标签
			if stripUnlikelyCandidates && len(ts(matchString)) > 0 {
				if unlikelyCandidatesPattern.MatchString(matchString) &&
					!okMaybeItsACandidatePattern.MatchString(matchString) &&
					node.Data != "body" &&
					node.Data != "a" {
					read.l("Removing unlikely candidate - " + matchString)
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
			// 将所有没有 children 的 div 转换为 p
			if node.Data == "div" {
				// 将短语内容分段。
				var p *html.Node
				childNode := node.FirstChild
				for childNode != nil {
					nextSibling := childNode.NextSibling
					if read.isPhrasingContent(childNode) {
						if p != nil {
							nodeAppendChild(childNode, p, false)
						} else if !read.isWhitespace(childNode) {
							p = read.createSelection("p").Get(0)
							nodeAppendChild(childNode, p, true)
						}
					} else if p != nil {
						for p.LastChild != nil && read.isWhitespace(p.LastChild) {
							p.RemoveChild(p.LastChild)
						}
						p = nil
					}
					childNode = nextSibling
				}

				// 将只包含一个 p 标签的 div 标签去掉，将 p 提出来
				if hasSingleTagInsideElement(sel, "p") && getLinkDensity(sel) < 0.25 {
					next := getNextSelection(sel, true)
					sel.ReplaceWithSelection(sel.Children().First())
					selectionsToScore = append(selectionsToScore, sel)
					sel = next
					continue
				} else if !hasChildBlockElement(sel) {
					// 节点不含有块级元素
					read.replaceSelectionTags(sel, "p")
					selectionsToScore = append(selectionsToScore, sel)
				}
			}
			sel = getNextSelection(sel, false)
		}

		/*
		  循环浏览所有段落，并根据它们的外观如何分配给他们一个分数。
		  然后将他们的分数添加到他们的父节点。
		  分数由 commas，class 名称 等的 数目决定。也许最终链接密度。
		*/
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
				if ancestor.Length() == 0 || ancestor.Get(0).Parent == nil || ancestor.Get(0).Parent.Data == "" {
					continue
				}
				if read.scoreList[ancestor.Get(0)] == 0 {
					// 初始化节点分数
					read.initializeScoreSelection(ancestor)
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
				read.scoreList[ancestor.Get(0)] += contentScore / divider
			}
		}

		// 在我们计算出分数后，循环遍历我们找到的所有可能的候选节点，并找到分数最高的候选节点。
		topCandidates := make([]*goquery.Selection, 0)
		for _, candidate := range candidates {
			candidateScore := 0.00
			// 根据链接密度缩放最终候选人分数。 良好的内容应该有一个相对较小的链接密度（5％或更少），并且大多不受此操作的影响。
			candidateScore = read.scoreList[candidate.Get(0)] * (1 - getLinkDensity(candidate))
			read.scoreList[candidate.Get(0)] = candidateScore

			read.l("Candidate:", candidate.Get(0).Data, "[", candidate.Get(0).Attr, "]", "with score", candidateScore)

			for i := 0; i < read.option.NbTopCandidates; i++ {
				var candi *goquery.Selection
				if i < len(topCandidates) {
					candi = topCandidates[i]
				}
				if candi == nil || candidateScore > read.scoreList[topCandidates[i].Get(0)] {
					// 分数越高排名越靠前
					topCandidates = append(topCandidates, candidate)
					copy(topCandidates[i+1:], topCandidates[i:])
					topCandidates[i] = candidate
					// 限制数量
					if len(topCandidates) > read.option.NbTopCandidates {
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
			page = read.dom.Find("body").First()
			page.Children().Each(func(i int, s *goquery.Selection) {
				read.l("Moving child out:", s.Get(0))
				topCandidate.AppendSelection(s)
			})
			page.AppendSelection(topCandidate)

			read.initializeScoreSelection(topCandidate)
		} else if !needToCreateTopCandidate {
			// 如果它包含（至少三个）属于`topCandidates`数组并且其分数与
			// 当前`topCandidate`节点非常接近的节点，则找到一个更好的顶级候选节点。
			alternativeCandidateAncestors := make([]map[*goquery.Selection]int, 0)
			for _, c := range topCandidates {
				if read.scoreList[c.Get(0)]/read.scoreList[topCandidate.Get(0)] >= 0.75 {
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
							listsContainingThisAncestor++
						}
					}
					if listsContainingThisAncestor > MinimumTopCandidates {
						topCandidate = parentOfTopCandidate
						break
					}
					parentOfTopCandidate = parentOfTopCandidate.Parent()
				}
			}
			if read.scoreList[topCandidate.Get(0)] == 0 {
				read.initializeScoreSelection(topCandidate)
			}

			/*
			  由于我们的奖金制度，节点的父节点可能会有自己的分数。 他们得到节点的一半。 不会有比我们的
			  topCandidate分数更高的节点，但是如果我们在树的前几个步骤中看到分数增加，这是一个体面
			  的信号，可能会有更多的内容潜伏在我们想要的其他地方 统一英寸下面的兄弟姐妹的东西做了一些
			  - 但只有当我们已经足够高的DOM树。
			*/
			parentOfTopCandidate = topCandidate
			lastScore := read.scoreList[topCandidate.Get(0)]
			// 分数不能太低。
			scoreThreshold := lastScore / 3
			for parentOfTopCandidate.Get(0).Data != "body" {
				if read.scoreList[parentOfTopCandidate.Get(0)] == 0 {
					read.initializeScoreSelection(parentOfTopCandidate)
					continue
				}
				if read.scoreList[parentOfTopCandidate.Get(0)] < scoreThreshold {
					break
				}
				if read.scoreList[parentOfTopCandidate.Get(0)] > lastScore {
					// 找到了一个更好的节点
					topCandidate = parentOfTopCandidate
					break
				}
				lastScore = read.scoreList[parentOfTopCandidate.Get(0)]
				parentOfTopCandidate = parentOfTopCandidate.Parent()
			}

			// 如果最上面的候选人是唯一的孩子，那就用父母代替。 当相邻内容实际位于父节点的兄弟节点中时，这将有助于兄弟连接逻辑。
			parentOfTopCandidate = topCandidate.Parent()
			for parentOfTopCandidate.Get(0).Data != "body" && parentOfTopCandidate.Get(0).FirstChild.NextSibling == nil {
				topCandidate = parentOfTopCandidate
				parentOfTopCandidate = topCandidate.Parent()
			}
			if read.scoreList[parentOfTopCandidate.Get(0)] == 0 {
				read.initializeScoreSelection(parentOfTopCandidate)
			}
		}

		// 现在我们有了最好的候选人，通过它的兄弟姐妹查看可能也有关联的内容。 诸如前导，内容被我们删除的广告分割等
		articleContent := read.createSelection("div")
		if isPaging {
			articleContent.SetAttr("id", "readability-content")
		}

		siblingScoreThreshold := math.Max(10, read.scoreList[topCandidate.Get(0)]*0.2)
		// 让潜在的顶级候选人的父节点稍后尝试获取文本方向。
		parentOfTopCandidate = topCandidate.Parent()
		sibling := parentOfTopCandidate.Children().First()
		for sibling.Length() > 0 {
			willAppend := false
			var next *goquery.Selection
			read.l("Looking at sibling node:", sibling.Get(0).Data, sibling.Get(0).Attr)
			read.l("Sibling has score", read.scoreList[sibling.Get(0)])
			if sibling.Get(0) == topCandidate.Get(0) {
				willAppend = true
			} else {
				contentBonus := 0.0

				// 如果兄弟节点和顶级候选人具有相同的类名示例，则给予奖励
				if sibling.AttrOr("class", "") ==
					topCandidate.AttrOr("class", "") &&
					topCandidate.AttrOr("class", "") != "" {
					contentBonus += read.scoreList[topCandidate.Get(0)] * 0.2
				}

				if read.scoreList[sibling.Get(0)]+contentBonus >= siblingScoreThreshold {
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
				read.l("Appending node:", sibling.Get(0))
				alter := map[string]int{
					"div": 0, "article": 0, "section": 0, "p": 0,
				}
				sn := sibling.Get(0)
				if _, has := alter[sn.Data]; has {
					sn.Data = "div"
					sn.Namespace = "div"
					read.l("Altering sibling:", sibling.Get(0), "to div.")
				}
				next = sibling.Next()
				articleContent.AppendSelection(sibling)
				sibling = next
			} else {
				sibling = sibling.Next()
			}
		}

		logText, _ := goquery.OuterHtml(articleContent)
		read.l("Article content pre-prep:", logText)

		// 准备要显示的文章节点。 清理任何内联样式，iframe，表单，去除无关的<p>标签等。
		read.prepArticle(articleContent)

		logText, _ = goquery.OuterHtml(articleContent)
		read.l("Article content post-prep:", logText)

		if needToCreateTopCandidate {
			// 我们已经创建了一个假的div事物，并且之前的循环没有任何兄弟姐妹，所以尝试创建一个新的div，然后将所有的
			// 孩子移动过去都没有意义。 只需在这里分配ID和类名。 无需追加，因为无论如何已经发生了。
			topCandidate.SetAttr("id", "readability-page-1")
			topCandidate.SetAttr("class", "page")
		} else {
			div := read.createSelection("div")
			div.SetAttr("id", "readability-page-1")
			div.SetAttr("class", "page")
			ch := articleContent.Get(0).FirstChild
			dn := div.Get(0)
			for ch != nil {
				t := ch.NextSibling
				nodeAppendChild(ch, dn, false)
				ch = t
			}
			articleContent.Get(0).FirstChild = div.Get(0)
		}

		logText, _ = goquery.OuterHtml(articleContent)
		read.l("Article content after paging: ", logText)

		parseSuccessful := true
		// 现在我们已经完成了完整的算法，请检查是否有任何有意义的内容。 如果我们没有，我们可能需要
		// 重新运行具有不同标志的grabArticle。 这使我们更有可能找到内容，而筛选方法使我们更有可
		// 能找到正确内容。
		textLength := len(ts(articleContent.Text()))
		if textLength < read.option.CharThreshold {
			parseSuccessful = false
			read.dom = originDoc
			if flagIsActive(flagStripUnlikely) {
				removeFlag(flagStripUnlikely)
				read.attempts = append(read.attempts, articleContent)
			} else if flagIsActive(flagWeightClasses) {
				removeFlag(flagWeightClasses)
				read.attempts = append(read.attempts, articleContent)
			} else if flagIsActive(flagCleanConditionally) {
				removeFlag(flagCleanConditionally)
				read.attempts = append(read.attempts, articleContent)
			} else {
				bestContent := articleContent
				for _, c := range read.attempts {
					if len(ts(bestContent.Text())) < len(ts(c.Text())) {
						bestContent = c
					}
				}
				if len(ts(bestContent.Text())) == 0 {
					return nil
				}
				parseSuccessful = true
			}
		}
		if parseSuccessful {
			// 找出来自最终候选人祖先的文字方向。
			as := getSelectionAncestors(parentOfTopCandidate, 0)
			as = append(as, parentOfTopCandidate, topCandidate)
			for _, ancestor := range as {
				if len(ts(ancestor.Get(0).Data)) == 0 {
					continue
				}
				dir := ancestor.AttrOr("dir", "")
				if len(ts(dir)) > 0 {
					read.article.Dir = ts(dir)
				}
			}
			return articleContent
		}
	}
}

func nodeAppendChild(child *html.Node, parent *html.Node, replace bool) {
	if replace {
		child.Parent.RemoveChild(child)
	}
	child.PrevSibling = nil
	child.NextSibling = nil
	child.Parent = nil
	parent.AppendChild(child)
}

// best way to create element in GoQuery
func (read *Readability) createSelection(tag string) *goquery.Selection {
	tempNode := &html.Node{Type: html.ElementNode, Namespace: tag, Data: tag}
	read.dom.Get(0).AppendChild(tempNode)
	return read.dom.FindNodes(tempNode)
}

// 准备要显示的文章节点。 清理任何内联样式，iframe，表单，去除无关的<p>标签等。
func (read *Readability) prepArticle(s *goquery.Selection) {
	cleanStyles(s)
	/*
	  在我们继续之前检查数据表，以避免移除这些表中的项目，即使它们与其他内容元素（文本，图像等）
	  可视化链接，这些表格也会被隔离。
	*/
	read.markDataTables(s)
	// 清除文章内容中的垃圾
	read.cleanConditionally(s, "form")
	read.cleanConditionally(s, "fieldset")
	clean(s, "object")
	clean(s, "embed")
	clean(s, "h1")
	clean(s, "footer")
	clean(s, "link")
	clean(s, "aside")

	// 清理出来的元素在最终候选名单中与他们的id / class组合“共享”，这意味着即使他们有“分享”
	// ，我们也不会删除顶级候选人。
	s.Children().Each(func(i int, ch *goquery.Selection) {
		cleanMatchedNodes(ch, sharePattern)
	})

	// 如果只有一个h2，并且其文本内容与文章标题大致相同，那么它们可能将其用作标题而不是子标题，因此，
	// 请将其删除，因为我们已经分别提取标题。
	h2 := s.Find("h2")
	if h2.Length() == 1 {
		h2 = h2.First()
		lengthSimilarRate := float64(len(h2.Text())-len(read.article.Title)) / float64(len(read.article.Title))
		if math.Abs(lengthSimilarRate) < 0.5 {
			var titlesMatch = false
			if lengthSimilarRate > 0 {
				titlesMatch = strings.Contains(h2.Text(), read.article.Title)
			} else {
				titlesMatch = strings.Contains(read.article.Title, h2.Text())
			}
			if titlesMatch {
				clean(s, "h2")
			}
		}
	}

	clean(s, "iframe")
	clean(s, "input")
	clean(s, "textarea")
	clean(s, "select")
	clean(s, "button")
	read.cleanHeaders(s)

	// 这些最后的东西可能会删除会影响这些东西的垃圾
	read.cleanConditionally(s, "table")
	read.cleanConditionally(s, "ul")
	read.cleanConditionally(s, "div")

	// 删除多余的段落
	s.Find("p").Each(func(i int, p *goquery.Selection) {
		imgCount := p.Find("img").Length()
		embedCount := p.Find("embed").Length()
		objectCount := p.Find("object").Length()
		// 此时，讨厌的iframe已被删除，只保留嵌入的视频。
		iframeCount := p.Find("iframe").Length()
		totalCount := imgCount + embedCount + objectCount + iframeCount

		if totalCount == 0 && len(ts(p.Text())) == 0 {
			p.Remove()
		}
	})

	s.Find("br").Each(func(i int, br *goquery.Selection) {
		next := br.Next()
		if next.Length() > 0 && next.Get(0).Data == "p" {
			br.Remove()
		}
	})

	// 移除单单元格表格
	s.Find("table").Each(func(i int, table *goquery.Selection) {
		var tbody *goquery.Selection
		tag := "p"
		if hasSingleTagInsideElement(table, "tbody") {
			tbody = table.Children().First()
		} else {
			tbody = table
		}
		if hasSingleTagInsideElement(tbody, "tr") {
			tbody = tbody.Children().First()
			if hasSingleTagInsideElement(tbody, "td") {
				tbody = tbody.Children().First()
				tbody.Each(func(i int, cell *goquery.Selection) {
					if !read.isPhrasingContent(cell.Get(0)) {
						tag = "div"
					}
				})
				tbody.Get(0).Data = tag
				table.ReplaceWithSelection(tbody)
			}
		}
	})
}

// 清除元素中的虚假标题。 检查类名和链接密度。
func (read *Readability) cleanHeaders(s *goquery.Selection) {
	for h := 1; h < 3; h++ {
		s.Find("h" + strconv.Itoa(h)).Each(func(i int, hs *goquery.Selection) {
			read.getClassWeight(hs)
			if read.scoreList[hs.Get(0)] < 0 {
				hs.Remove()
			}
		})
	}
}

// 清除id / class组合与特定字符串匹配的元素。
func cleanMatchedNodes(s *goquery.Selection, m *regexp.Regexp) {
	end := getNextSelection(s, true)
	next := getNextSelection(s, false)
	for next != nil && end != nil && next.Get(0) != end.Get(0) {
		if m.MatchString(next.AttrOr("class", "") + " " + next.AttrOr("id", "")) {
			next = removeAndGetNext(next)
		} else {
			next = getNextSelection(next, false)
		}
	}
}

// 清理“tag”类型的所有元素的节点。（除非它是一个YouTube等的视频，人们喜欢看视频）
func clean(s *goquery.Selection, tag string) {
	embedded := map[string]int{"object": 0, "embed": 0, "iframe": 0}
	s.Find(tag).Each(func(i int, junk *goquery.Selection) {
		// 允许youtube和vimeo视频通过人们通常希望看到的视频。
		_, isEmbedded := embedded[junk.Get(0).Data]
		if isEmbedded {
			var as []string
			for _, a := range junk.Get(0).Attr {
				as = append(as, a.Val)
			}
			// 首先，检查元素属性，看它们是否包含youtube或vimeo
			attributeValues := strings.Join(as, "|")
			if videoLinkPattern.MatchString(attributeValues) {
				return
			}
			// 然后检查这个元素中的元素
			h, err := junk.Html()
			if err == nil && videoLinkPattern.MatchString(h) {
				return
			}
		}
		junk.Remove()
	})

}

// 清洁“标签”类型的所有标签的元素，如果它们看起来很腥。
// “Fishy”是一种基于内容长度，类名，链接密度，图像和嵌入数量等的算法。
func (read *Readability) cleanConditionally(s *goquery.Selection, tag string) {
	if !flagIsActive(flagCleanConditionally) {
		return
	}
	isList := tag == "ul" || tag == "ol"
	// 聚集计算嵌入其他典型元素。向后返回，以便我们可以在不影响遍历的情况下同时移除节点。
	s.Find(tag).Each(func(i int, junk *goquery.Selection) {
		if hasAncestorTag(junk, "table", -1, func(s *goquery.Selection) bool {
			return read.readabilityDataTable[s.Get(0)]
		}) {
			return
		}
		read.l("Cleaning Conditionally", junk.Get(0).Data, junk.Get(0).Attr)
		read.getClassWeight(junk)
		if read.scoreList[junk.Get(0)] < 0 {
			junk.Remove()
		}
		t := junk.Text()
		if strings.Count(t, ",") < 10 || strings.Count(t, "，") < 10 {
			// 如果逗号不多，并且非段落元素的数量多于段落或其他不祥的标志，则删除该元素。
			p := junk.Find("p").Length()
			img := junk.Find("img").Length()
			li := junk.Find("li").Length() - 100
			input := junk.Find("input").Length()

			embedCount := 0
			junk.Find("embed").Each(func(i int, embed *goquery.Selection) {
				if videoLinkPattern.MatchString(embed.AttrOr("src", "")) {
					embedCount++
				}
			})

			linkDensity := getLinkDensity(junk)
			contentLength := len(junk.Text())
			if (img > 1 && float64(p/img) < 0.5 && !hasAncestorTag(junk, "figure", 0, nil)) ||
				(!isList && li > p) ||
				(input > int(math.Floor(float64(p)/3))) ||
				(!isList && contentLength < 25 && (img == 0 || img > 2) && !hasAncestorTag(junk, "figure", 0, nil)) ||
				(!isList && read.scoreList[junk.Get(0)] < 25 && linkDensity > 0.2) ||
				(read.scoreList[junk.Get(0)] >= 25 && linkDensity > 0.5) ||
				((embedCount == 1 && contentLength < 75) || embedCount > 1) {
				junk.Remove()
			}
		}
	})
}

// 检查给定节点是否具有与提供的节点相匹配的其祖先标签名称之一。
func hasAncestorTag(s *goquery.Selection, tag string, max int, call func(s *goquery.Selection) bool) bool {
	if max == 0 {
		max = 3
	}
	deep := 0
	p := s.Parent()
	for p.Length() > 0 && (max < 0 || max > 0 && deep < max) {
		if p.Get(0).Data == tag && (call == nil || call(p)) {
			return true
		}
		p = p.Parent()
		deep++
	}
	return false
}

// 查找'数据'（而不是'布局'）表格，我们使用类似的检查方式，
// 如 https://dxr.mozilla.org/mozilla-central/rev/71224049c0b52ab190564d3ea0eab089a159a4cf/accessible/html/HTMLTableAccessible.cpp#920
func (read *Readability) markDataTables(s *goquery.Selection) {
	s.Find("table").Each(func(i int, table *goquery.Selection) {
		if table.AttrOr("role", "") == "presentation" {
			read.readabilityDataTable[table.Get(0)] = false
			return
		}
		if table.AttrOr("datatable", "") == "0" {
			read.readabilityDataTable[table.Get(0)] = false
			return
		}
		if _, has := table.Attr("summary"); has {
			read.readabilityDataTable[table.Get(0)] = true
			return
		}
		if table.Find("caption").First().Children().Length() > 0 {
			read.readabilityDataTable[table.Get(0)] = true
			return
		}
		// 如果表中有任何这些标签的后代，请考虑数据表：
		var dataTableDescendants = []string{"col", "colgroup", "tfoot", "thead", "th"}
		for _, tag := range dataTableDescendants {
			if table.Find(tag).Length() > 0 {
				read.l("Data table because found data-y descendant")
				read.readabilityDataTable[table.Get(0)] = true
				return
			}
		}
		// 嵌套表格表示布局表格：
		if table.Find("table").Length() > 0 {
			read.readabilityDataTable[table.Get(0)] = true
			return
		}
		var rows, cols int = getRowAndColumnCount(table)
		if rows >= 10 || cols > 4 {
			read.readabilityDataTable[table.Get(0)] = true
			return
		}
		// 现在完全按照尺寸进行：
		read.readabilityDataTable[table.Get(0)] = rows*cols > 10
	})
}

// 获取table的行列数
func getRowAndColumnCount(table *goquery.Selection) (int, int) {
	rows := 0
	cols := 0
	table.Find("tr").Each(func(i int, tr *goquery.Selection) {
		rowSpan, _ := strconv.Atoi(tr.AttrOr("rowspan", "0"))
		if rowSpan == 0 {
			rowSpan = 1
		}
		rows += rowSpan
		// 现在查找与列相关的信息
		colsInRow := 0
		tr.Find("td").Each(func(i int, td *goquery.Selection) {
			colSpan, _ := strconv.Atoi(tr.AttrOr("colspan", "0"))
			if colSpan == 0 {
				colSpan = 1
			}
			colsInRow += colSpan
		})
		cols = int(math.Max(float64(colsInRow), 10.0))
	})
	return rows, cols
}

// 删除节点及子节点样式属性。
func cleanStyles(s *goquery.Selection) {
	if s.Length() == 0 || s.Get(0).Data == "svg" {
		return
	}
	// 删除`style`和不推荐的表示属性
	for _, style := range presentationalAttributes {
		s.RemoveAttr(style)
	}
	for _, tag := range deprecatedSizeAttributeElem {
		if strings.Contains(s.Get(0).Data, tag) {
			s.RemoveAttr("width")
			s.RemoveAttr("height")
		}
	}
	s.Children().Each(func(i int, is *goquery.Selection) {
		cleanStyles(is)
	})
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

// 初始化节点分数
func (read *Readability) initializeScoreSelection(s *goquery.Selection) {
	switch s.Get(0).Data {
	case "div":
		read.scoreList[s.Get(0)] += 5
		break
	case "pre":
	case "td":
	case "blockquote":
		read.scoreList[s.Get(0)] += 3
		break
	case "address":
	case "ol":
	case "ul":
	case "dl":
	case "dd":
	case "dt":
	case "li":
	case "form":
		read.scoreList[s.Get(0)] -= 3
		break
	case "h1":
	case "h2":
	case "h3":
	case "h4":
	case "h5":
	case "h6":
	case "th":
		read.scoreList[s.Get(0)] -= 5
		break
	}
	// 获取元素类/标识权重。 使用正则表达式来判断这个元素是好还是坏。
	read.getClassWeight(s)

	// 如果为 0 置负0.00001
	if read.scoreList[s.Get(0)] == 0 {
		read.scoreList[s.Get(0)] = -0.00001
	}
}

// 获取元素类/标识权重。 使用正则表达式来判断这个元素是好还是坏。
func (read *Readability) getClassWeight(s *goquery.Selection) {
	if !flagIsActive(flagWeightClasses) {
		return
	}
	// 寻找一个特殊的类名
	className, has := s.Attr("class")
	if has && len(className) > 0 {
		if negativePattern.MatchString(className) {
			read.scoreList[s.Get(0)] -= 25
		}
		if positivePattern.MatchString(className) {
			read.scoreList[s.Get(0)] += 25
		}
	}
	// 寻找一个特殊的ID
	id, has := s.Attr("id")
	if has && len(className) > 0 {
		if negativePattern.MatchString(id) {
			read.scoreList[s.Get(0)] -= 25
		}
		if positivePattern.MatchString(id) {
			read.scoreList[s.Get(0)] += 25
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

// 检查此节点是否只有空白，并且具有给定标记的单个元素如果DIV节点包含非空文本节点，
// 或者它不包含具有给定标记或多个元素的元素，则返回false。
func hasSingleTagInsideElement(s *goquery.Selection, tag string) bool {
	// 子节点个数大于一，或者第一个子节点不是本身
	if s.Children().Length() != 1 || s.Children().Get(0).Data != tag {
		return false
	}
	// 并且不应该有真实内容的文本节点
	return len(ts(s.Text())) == 0
}

// 确定节点是否符合短语内容。
func (read *Readability) isPhrasingContent(n *html.Node) bool {
	if n != nil && n.Type == html.TextNode || inSlice(phrasingElements, n.Data) {
		return true
	}
	innerN := n.FirstChild
	for innerN != nil {
		if !read.isPhrasingContent(innerN) {
			return false
		}
		innerN = innerN.NextSibling
	}
	return true
}

// 删除并获取下一个
func removeAndGetNext(s *goquery.Selection) *goquery.Selection {
	t := getNextSelection(s, true)
	s.Remove()
	return t
}

/*
  从 node 开始遍历DOM，
  如果 ignoreSelfAndKids 为 true 则不遍历子 element
  改为遍历 兄弟 和 父级兄弟 element
*/
func getNextSelection(s *goquery.Selection, ignoreSelfAndChildren bool) *goquery.Selection {
	if s.Length() == 0 {
		return nil
	}
	var t *goquery.Selection
	// 如果 ignoreSelfAndKids 不为 true 且 node 有子 element 返回第一个子 element
	t = s.Children()
	if !ignoreSelfAndChildren && t.Length() > 0 {
		t = t.First()
		if t.Length() > 0 {
			return t
		}
	}
	// 然后是兄弟 element
	t = s.Next()
	if t.Length() > 0 {
		return t
	}
	// 最后，父节点的兄弟 element
	//（因为这是深度优先遍历，我们已经遍历了父节点本身）。
	for {
		s = s.Parent()
		t = s.Next()
		if s.Length() > 0 {
			if t.Length() > 0 {
				return t
			}
		} else {
			break
		}
	}
	return nil
}

// 是否是作者信息
func (read *Readability) checkByline(s *goquery.Selection, matchString string) bool {
	if len(read.article.Byline) > 0 {
		return false
	}
	innerText := s.Text()
	if (s.AttrOr("rel", "") == "author" || bylinePattern.MatchString(matchString)) && isValidByline(innerText) {
		read.article.Byline = ts(innerText)
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
	return flags[flag]
}

// 禁用flag
func removeFlag(flag int) {
	flags[flag] = false
}

// 从 metadata 获取文章的摘要和作者信息
func (read *Readability) getArticleMetadata() metadata {
	var md metadata
	values := make(map[string]string)

	namePattern := regexp.MustCompile(`^\s*((twitter)\s*:\s*)?(description|title)\s*$`)
	propertyPattern := regexp.MustCompile(`^\s*og\s*:\s*(description|title)\s*$`)

	// 提取元数据
	read.dom.Find("meta").Each(func(i int, s *goquery.Selection) {
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
				name = normalizeSpace(strings.ToLower(name))
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
	md.Title = read.getArticleTitle()
	if len(md.Title) < 1 {
		if val, has := values["og:title"]; has {
			md.Title = val
		} else if val, has := values["twitter:title"]; has {
			md.Title = val
		}
	}

	return md
}

// 将多个空格替换成单个空格
func normalizeSpace(str string) string {
	return whitespacePattern.ReplaceAllString(str, " ")
}

// 获取文章标题
func (read *Readability) getArticleTitle() string {
	titleSplitPattern := regexp.MustCompile(`(.*)[\|\-\\\/>»](.*)`)
	var title, originTitle string

	// 从 title 标签获取标题
	elementTitle := read.dom.Find("title").First()
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
		trimTitle := ts(title)
		read.dom.Find("h1,h2").EachWithBreak(func(i int, s *goquery.Selection) bool {
			// 提取的标题是否在正文中存在
			if ts(s.Text()) == trimTitle {
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
		h1s := read.dom.Find("h1")
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
func (read *Readability) prepDocument() {
	// 移除所有script标签
	read.removeTags("script,noscript")

	// 移除所有style标签
	read.removeTags("style")

	// 将多个连续的<br>替换成<p>
	read.replaceBrs()

	// 将所有的font替换成span
	read.replaceSelectionTags(read.dom.Find("font"), "span")

	// 清除所有注释节点
	removeComments(read.dom.Get(0))
}

// 清除所有注释节点
func removeComments(pNode *html.Node) {
	for pNode != nil {
		tmp := pNode
		if pNode.Type == html.CommentNode {
			tmp = pNode.NextSibling
			if tmp == nil {
				tmp = pNode.Parent.NextSibling
			}
			pNode.Parent.RemoveChild(pNode)
			pNode = tmp
			continue
		}
		pNode = tmp.FirstChild
		if pNode != nil {
			continue
		}
		pNode = tmp.NextSibling
		if pNode != nil {
			continue
		}
		tmp = tmp.Parent
		for tmp != nil {
			pNode = tmp.NextSibling
			if pNode != nil {
				break
			}
			tmp = tmp.Parent
		}
	}
}

// 将多个连续的<br>替换成<p>
func (read *Readability) replaceBrs() {
	read.dom.Find("br").Each(func(i int, br *goquery.Selection) {
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

				if !read.isPhrasingContent(next) {
					break
				}

				// 否则将节点添加为 <p> 的子节点
				temp := next.NextSibling
				nodeAppendChild(next, pNode, true)
				next = temp
			}

			for pNode.LastChild != nil && read.isWhitespace(pNode.LastChild) {
				pNode.RemoveChild(pNode.LastChild)
			}

			if pNode.Parent.Data == "p" {
				pNode.Parent.Data = "div"
			}
		}
	})
}

// 获取下一个Element
func nextElement(n *html.Node) *html.Node {
	for n != nil &&
		n.Type != html.ElementNode &&
		(len(ts(n.Data)) == 0 ||
			n.Type == html.CommentNode) {
		n = n.NextSibling
	}
	return n
}

// 移除所有 tags 标签
// 例如 "script,noscript" 清理所有script
func (read *Readability) removeTags(tags string) {
	read.dom.Find(tags).Each(func(i int, s *goquery.Selection) {
		s.Remove()
	})
}

// 将所有的s的标签替换成tag
func (read *Readability) replaceSelectionTags(s *goquery.Selection, tag string) {
	s.Each(func(i int, is *goquery.Selection) {
		read.l("_setNodeTag", i, ts(is.Get(0).Data), tag)
		n := is.Get(0)
		n.Type = html.ElementNode
		n.Data = tag
		n.Namespace = tag
	})
}

func inSlice(s []string, i string) bool {
	for _, v := range s {
		if v == i {
			return true
		}
	}
	return false
}

// 调试日志
func (read *Readability) l(ms ...interface{}) {
	if read.option.Debug {
		log.Println(ms...)
	}
}

func (read *Readability) isWhitespace(node *html.Node) bool {
	return (node.Type == html.TextNode && len(strings.TrimSpace(node.Data)) == 0) ||
		(node.Type == html.ElementNode && node.Data == "br")
}

func (read *Readability) isProbablyVisible(sel *goquery.Selection) bool {
	m, _ := regexp.MatchString(`display:\s*none`, sel.AttrOr("style", ""))
	_, m1 := sel.Attr("hidden")
	return !m && !m1
}

// TrimSpace
func ts(s string) string {
	return strings.TrimSpace(s)
}
