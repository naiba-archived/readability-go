/*
 * Copyright (c) 2018, 奶爸<1@5.nu>
 * All rights reserved.
 */

package readability

import (
	"io"
	"github.com/PuerkitoBio/goquery"
	"errors"
	"fmt"
	"log"
)

type Option struct {
	MaxNodeNum int
}
type Readability struct {
}

func Parse(r io.Reader, opt Option) (*Readability, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	// 超出最大解析限制
	if opt.MaxNodeNum > 0 && len(doc.Nodes) > opt.MaxNodeNum {
		return nil, errors.New(fmt.Sprintf("Node 数量超出最大限制：%d。", opt.MaxNodeNum))
	}
	log.Println(doc)
	return nil, nil
}