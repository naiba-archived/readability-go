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
	whitespacePattern  = regexp.MustCompile(`^\s*$`)
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
	bylinePattern               = regexp.MustCompile(`byline|author|dateline|writtenby|p-author`)
	okMaybeItsACandidatePattern = regexp.MustCompile(`and|article|body|column|main|shadow`)
	unlikelyCandidatesPattern   = regexp.MustCompile(`banner|breadcrumbs|combx|comment|community|cover-wrap|disqus|extra|foot|header|legends|menu|related|remark|replies|rss|shoutbox|sidebar|skyscraper|social|sponsor|supplemental|ad-break|agegate|pagination|pager|popup|yom-remote`)
	negativePattern             = regexp.MustCompile(`hidden|^hid$| hid$| hid |^hid |banner|combx|comment|com-|contact|foot|footer|footnote|masthead|media|meta|outbrain|promo|related|scroll|share|shoutbox|sidebar|skyscraper|sponsor|shopping|tags|tool|widget`)
	positivePattern             = regexp.MustCompile(`article|body|content|entry|hentry|h-entry|main|page|pagination|post|text|blog|story`)
	option                      = new(Option)
	flags                       = flagCleanConditionally | flagStripUnlikely | flagWeightClasses
)

const (
	flagStripUnlikely      = 0x1
	flagWeightClasses      = 0x2
	flagCleanConditionally = 0x4
)

//Option 解析配置
type Option struct {
	MaxNodeNum    int
	Debug         bool
	ArticleByline string
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

//Readability 节点评分
type Readability struct {
	ContentScore int
}

//ScoreSelection 可评分节点
type ScoreSelection struct {
	*goquery.Selection
	Readability *Readability
}

//Parse 进行解析
func Parse(s string, opt Option) (*Article, error) {
	d, err := goquery.NewDocumentFromReader(strings.NewReader(s))
	if err != nil {
		return nil, err
	}
	option = &opt
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
	page := d.Find("body").First().Children()
	if page.Length() == 0 {
		l("getArticle", "没有 body，哪里来的正文？")
		return nil
	}
	stripUnlikelyCandidates := flagIsActive(flagStripUnlikely)
	selectionsToScore := make([]*goquery.Selection, 0)
	sel := page.First()
	for sel != nil {
		node := sel.Get(0)
		// 首先，节点预处理。 清理看起来很糟糕的垃圾节点（比如类名为“comment”的垃圾节点），
		// 并将div转换为P标签，清理空节点。
		matchString, _ := sel.Attr("id")
		class, _ := sel.Attr("class")
		matchString += " " + class
		// 作者信息行
		if checkByline(sel, matchString) {
			l("getArticle", "作者信息", "清除")
			sel = removeAndGetNext(sel)
			continue
		}
		// 清理垃圾标签
		if stripUnlikelyCandidates {
			if unlikelyCandidatesPattern.MatchString(matchString) &&
				!okMaybeItsACandidatePattern.MatchString(matchString) &&
				node.Data != "body" &&
				node.Data != "a" {
				l("getArticle", "垃圾标签", "清除", matchString)
				sel = removeAndGetNext(sel)
				continue
			}
		}
		// 清理不含任何内容的 DIV, SECTION, 和 HEADER
		tags := map[string]int{"div": 0, "section": 0, "header": 0, "h1": 0, "h2": 0, "h3": 0, "h4": 0, "h5": 0, "h6": 0}
		if _, has := tags[node.Data]; has && len(ts(sel.Text())) == 0 {
			l("getArticle", "清理空块级元素", "清除")
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
				l(" -------------- hasSinglePInsideElement START  --------------")
				l(sel.Html())
				l(" -------------- hasSinglePInsideElement  END  --------------")
				sel.ReplaceWithSelection(sel.Children())
				selectionsToScore = append(selectionsToScore, sel)
			}
		} else if !hasChildBlockElement(sel) {
			// 节点是否含有块级元素
			sel.Get(0).Data = "p"
			sel.Get(0).Namespace = "p"
			selectionsToScore = append(selectionsToScore, sel)
		} else {
			// 含有块级元素
			sel.Children().Each(func(i int, s *goquery.Selection) {
				if len(ts(s.Text())) > 0 {
					p := s.Get(0)
					p.Data = "p"
					p.Namespace = "p"
					p.Attr = make([]html.Attribute, 0)
					s.SetAttr("class", "readability-styled")
					s.SetAttr("style", "display:inline;")
				}
			})
		}

		sel = getNextSelection(sel, false)
	}

	/*
	* 循环浏览所有段落，并根据它们的外观如何分配给他们一个分数。
	* 然后将他们的分数添加到他们的父节点。
	* 分数由 commas，class 名称 等的 数目决定。也许最终链接密度。
	**/
	l("selectionsToScore 长度", len(selectionsToScore), selectionsToScore)
	candidates := make([]*ScoreSelection, 0)
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

		contentScore := 0

		// 为段落本身添加一个基础分
		contentScore++

		innerText := sel.Text()
		// 在此段落内为所有逗号添加分数。
		contentScore += strings.Count(innerText, ",")
		contentScore += strings.Count(innerText, "，")

		// 本段中每100个字符添加一分。 最多3分。
		contentScore += int(math.Min(float64(utf8.RuneCountInString(innerText)/100), 3))

		// 给祖先初始化并评分。
		for level, ancestor := range ancestors {
			if ancestor.Length() == 0 {
				continue
			}
			if ancestor.Readability.ContentScore == 0 {
				// 初始化节点分数
				initializeScoreSelection(ancestor)
				candidates = append(candidates, ancestor)
			}
			// 节点加分规则：
			// - 父母：1（不划分）
			// - 祖父母：2
			// - 祖父母：祖先等级* 3
			divider := 1
			switch level {
			case 0:
				divider = 1
				break
			case 1:
				divider = 2
				break
			case 2:
				divider = level * 3
				break
			}
			ancestor.Readability.ContentScore += contentScore / divider
		}
	}

	//todo 获取评分最高节点

	return nil
}

// 初始化节点分数
func initializeScoreSelection(s *ScoreSelection) {
	switch s.Get(0).Data {
	case "div":
		s.Readability.ContentScore += 5
		break
	case "pre":
	case "td":
	case "blockquote":
		s.Readability.ContentScore += 3
		break
	case "address":
	case "ol":
	case "ul":
	case "dl":
	case "dd":
	case "dt":
	case "li":
	case "form":
		s.Readability.ContentScore -= 3
		break
	case "h1":
	case "h2":
	case "h3":
	case "h4":
	case "h5":
	case "h6":
	case "th":
		s.Readability.ContentScore -= 5
		break
	}
	// 获取元素类/标识权重。 使用正则表达式来判断这个元素是好还是坏。
	getClassWeight(s)
}

// 获取元素类/标识权重。 使用正则表达式来判断这个元素是好还是坏。
func getClassWeight(s *ScoreSelection) {
	if !flagIsActive(flagWeightClasses) {
		return
	}
	// 寻找一个特殊的类名
	className, has := s.Attr("class")
	if has && len(className) > 0 {
		if negativePattern.MatchString(className) {
			s.Readability.ContentScore -= 25
		}
		if positivePattern.MatchString(className) {
			s.Readability.ContentScore += 25
		}
	}
	// 寻找一个特殊的ID
	id, has := s.Attr("id")
	if has && len(className) > 0 {
		if negativePattern.MatchString(id) {
			s.Readability.ContentScore -= 25
		}
		if positivePattern.MatchString(id) {
			s.Readability.ContentScore += 25
		}
	}
}

// 向上获取祖先节点
func getSelectionAncestors(s *goquery.Selection, i int) []*ScoreSelection {
	ancestors := make([]*ScoreSelection, 0)
	count := 0
	for s.Parent().Length() > 0 {
		count++
		s = s.Parent()
		ancestors = append(ancestors, &ScoreSelection{s, &Readability{}})
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
		innerFlag := false
		if _, has := divToPElement[is.Get(0).Data]; has {
			innerFlag = true
		}
		if hasChildBlockElement(is) || innerFlag {
			flag = true
			return true
		}
		return false
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
	l("removeAndGetNext", s.Get(0))
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
		l("getNextSelection", "空空如也😂")
		return nil
	}
	// 如果 ignoreSelfAndKids 不为 true 且 node 有子 element 返回第一个子 element
	if !ignoreSelfAndChildren && s.Children().Length() > 0 {
		t := s.Children().First()
		if t.Length() > 0 {
			l("getNextSelection", "儿子", t.Get(0))
			return t
		}
	}
	// 然后是兄弟 element
	if s.Next().Length() > 0 {
		l("getNextSelection", "兄弟", s.Next().Get(0))
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
			l("getNextSelection", "父兄", t.Get(0))
			return t
		}
	}
	l("getNextSelection", "遍历完毕😂")
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
		l("nextElement", n)
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
		is.Get(0).Data = tag
		is.Get(0).Namespace = tag
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
