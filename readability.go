/*
 * Copyright (c) 2018, 奶爸<1@5.nu>
 * All rights reserved.
 */

package readability

import (
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"io"
	"log"
)

//Option 提取配置
type Option struct {
	MaxNodeNum int
}

//Readability 解析结果
type Readability struct {
}

//Parse 进行解析
func Parse(r io.Reader, opt Option) (*Readability, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	// 超出最大解析限制
	if opt.MaxNodeNum > 0 && len(doc.Nodes) > opt.MaxNodeNum {
		return nil, fmt.Errorf("Node 数量超出最大限制：%d 。 ", opt.MaxNodeNum)
	}
	doc.Find("script,noscript").Each(func(i int, selection *goquery.Selection) {
		selection.Nodes[0].Data = "span"
	})
	doc.Find("span").Each(func(i int, selection *goquery.Selection) {
		log.Println(selection.Html())
	})
	return nil, nil
}
