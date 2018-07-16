/*
 * Copyright (c) 2018, 奶爸<1@5.nu>
 * All rights reserved.
 */

package readability

import (
	"io/ioutil"
	"net/http"
	"testing"
)

func TestParse(t *testing.T) {
	pageUrls := []string{
		"https://www.jianshu.com/p/725c7dc55d58",
		"http://politics.people.com.cn/n1/2018/0612/c1001-30051069.html",
	}
	for page := 0; page < len(pageUrls); page++ {
		resp, err := http.Get(pageUrls[page])
		if err != nil {
			continue
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			continue
		}
		article, err := New(Option{Debug: false, PageURL: pageUrls[page]}).Parse(string(body))
		if err != nil {
			panic(err)
		} else {
			t.Log("标题", article.Title)
		}
		resp.Body.Close()
	}
}
