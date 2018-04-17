/*
 * Copyright (c) 2018, 奶爸<1@5.nu>
 * All rights reserved.
 */

package readability

import (
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
	"regexp"
	"strings"
	"log"
	"unicode/utf8"
)

var (
	whitespacePattern = regexp.MustCompile(`^\s*$`)
)

//Option 解析配置
type Option struct {
	MaxNodeNum int
}

//Metadata 文章摘要信息
type Metadata struct {
	Title   string
	Excerpt string
	Byline  string
}

//Readability 解析结果
type Readability struct {
}

//Parse 进行解析
func Parse(s string, opt Option) (*Readability, error) {
	d, err := goquery.NewDocumentFromReader(strings.NewReader(s))
	if err != nil {
		return nil, err
	}
	// 超出最大解析限制
	if opt.MaxNodeNum > 0 && len(d.Nodes) > opt.MaxNodeNum {
		return nil, fmt.Errorf("Node 数量超出最大限制：%d 。 ", opt.MaxNodeNum)
	}
	// 预处理HTML文档以提高可读性。 这包括剥离JavaScript，CSS和处理没用的标记等内容。
	prepDocument(d)

	//todo 从 metadata 尝试获取文章的摘要和作者信息
	md := getArticleMetadata(d)
	log.Println(md)

	return nil, nil
}

// 从 metadata 尝试获取文章的摘要和作者信息
func getArticleMetadata(d *goquery.Document) Metadata {
	var md Metadata
	values := make(map[string]string)

	namePattern := regexp.MustCompile(`/^\s*((twitter)\s*:\s*)?(description|title)\s*$/gi`)
	propertyPattern := regexp.MustCompile(`/^\s*((twitter)\s*:\s*)?(description|title)\s*$/gi`)

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
				values["name"] = strings.TrimSpace(elementContent)
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
	originTitle = strings.TrimSpace(elementTitle.Text())
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
			if strings.TrimSpace(s.Text()) == title {
				flag = true
			}
			return !flag
		})
		if !flag {
			// 如果不存在取 ":" 前后的文字
			i := strings.LastIndex(originTitle, "：")
			if i == -1 {
				i = strings.LastIndex(originTitle, ":")
			}
			title = originTitle[i:]
			if utf8.RuneCountInString(title) < 3 {
				i = strings.Index(originTitle, "：")
				if i == -1 {
					i = strings.Index(originTitle, ":")
				}
				title = originTitle[i:]
			} else if utf8.RuneCountInString(originTitle[0:i]) > 5 {
				title = originTitle
			}
		}
	} else if utf8.RuneCountInString(title) > 150 || utf8.RuneCountInString(title) < 15 {
		// 如果标题字数很离谱切只有一个h1标签，取其文字
		h1s := d.Find("h1")
		if h1s.Length() == 1 {
			title = strings.TrimSpace(h1s.First().Text())
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
			temp := next.NextSibling
			next.Parent.RemoveChild(next)
			next = nextElement(temp)
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
		n.Type != html.ElementNode &&
		whitespacePattern.MatchString(n.Data) {
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
