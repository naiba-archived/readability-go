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
	"regexp"
	"strings"
)

var (
	whitespaceRegex = regexp.MustCompile(`^\s*$`)
)

//Option 解析配置
type Option struct {
	MaxNodeNum int
}

//Readability 解析结果
type Readability struct {
}

//Parse 进行解析
func Parse(s string, opt Option) (*Readability, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(s))
	if err != nil {
		return nil, err
	}
	// 超出最大解析限制
	if opt.MaxNodeNum > 0 && len(doc.Nodes) > opt.MaxNodeNum {
		return nil, fmt.Errorf("Node 数量超出最大限制：%d 。 ", opt.MaxNodeNum)
	}
	// 预处理HTML文档以提高可读性。 这包括剥离JavaScript，CSS和处理没用的标记等内容。
	prepDocument(doc)
	return nil, nil
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

	//todo 从 metadata 尝试获取文章的摘要和作者信息
	log.Println(d.Html())
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
		whitespaceRegex.MatchString(n.Data) {
		n = n.NextSibling
	}
	return n
}
