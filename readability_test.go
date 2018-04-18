/*
 * Copyright (c) 2018, 奶爸<1@5.nu>
 * All rights reserved.
 */

package readability

import (
	"io/ioutil"
	"testing"
)

func TestParse(t *testing.T) {
	htmlStr, _ := ioutil.ReadFile("./README/readability.html")
	Parse(string(htmlStr), Option{Debug: true})
}
