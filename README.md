# Readability 网页正文提取

[![Go Report Card](https://goreportcard.com/badge/git.cm/naiba/go-readability)](https://goreportcard.com/report/git.cm/naiba/go-readability)  [![Build status](https://ci.appveyor.com/api/projects/status/28d8a25yts51nor5?svg=true)](https://ci.appveyor.com/project/naiba/go-readability)

Arc90 **Readability** 网页正文提取算法的 Golang 实现。奶爸做了一些中文网页提取优化。

### 算法简介

通过清理网页中的非可读标签、杂乱样式，保留可读内容并根据有意义的 `css class` 和 `element id` 以及 `元素内包含的内容`（如 文本中的逗号、表格、图像 等）来给元素 `加分` 或者 `减分`，最后取分数最高的元素进行后期处理（清理 `style` 标签、空元素）等操作。

此算法最大的可取之处就是根据 `css class name` 或 `element id` 及 `元素内包含内容` 的特征打分的想法，可以结合机器学习优化打分规则。

### 使用说明

1. 获取包
    ```shell
go get git.cm/naiba/go-readability
go test -v git.cm/naiba/go-readability
    ```

2. 使用包

    ```go
    import (
    	"log"
    	"git.cm/naiba/go-readability"
    )
    func main(){
        article,err := readability.Parse(htmlString, Option{Debug: false, PageUrl: pageUrlString})
        if err != nil {
            log.Fatal(err)
        } else {
            log.Println("标题", article.Title)
            log.Println("链接", article.URL)
            log.Println("作者", article.Byline)
            log.Println("长度", article.Length)
            log.Println("目录", article.Dir)
            log.Println("摘要", article.Excerpt)
            log.Println(article.Content)
        }
    }
    ```

### 提取效果

```shell
=== RUN   TestParse
--- PASS: TestParse (0.05s)
	readability_test.go:22: 标题 野生（初级）程序员面试经
	readability_test.go:23: 链接 https://www.lifelonglearning.cc/p82_interview.html
	readability_test.go:24: 作者 博主： 奶爸
	readability_test.go:25: 长度 6689
	readability_test.go:26: 目录
	readability_test.go:27: 摘要 #0 野生程序员野生程序员是指仅凭对计算机开发的兴趣进入这个行业，从前端到后台一手包揽，但各方面能力都不精通的人。野生程序员有很强大的单兵作战能力，但是在编入“正规军”之后，可能会不适应新的做事...
	readability_test.go:28: <div id="readability-page-1" class="page"><div> <h2>#0 野生程序员</h2><blockquote><p>野生程序员是指仅凭对计算机开发的兴趣进入这个行业，从前端到后台一手包揽，但各方面能力都不精通的人。野生程序员有很强大的单兵作战能力，但是在编入“正规军”之后，可能会不适应新的做事方法。—— <a href="http://geek.csdn.net/news/detail/38743" target="_blank">CSDN，野生程序员的故事</a></p></blockquote><p>他们大多数人有一个共同点：基础不扎实。比如 <code>算法</code>、<code>数据结构</code>、<code>系统原理</code>、<code>计算机网络</code> 等等，做项目的方式是入门水平 + Google，拼凑起来一个能用的
    --------   省略部分文字 ---------
    祝您马到成功</h2><p>如果您没有进入到心仪的企业也不要气馁，不论什么时候不要放弃<strong>学习</strong>，博主认为只要您的能力在那，价值终究也会在那。</p><h3>撰写过程中的改进</h3><ul><li><p><code>2018-03-20</code></p><ul><li>添加《牛逼程序员面试总结》文章分享</li></ul></li><li><p><code>2018-03-14</code></p><ul><li>全部的 ”你“ 改为 ”您“</li><li>全部的 ”最好“ 改为 ”建议“</li></ul></li></ul> </div></div>
```

### 其他实现

- [Node.js 实现 - Mozilla@GitHub](https://github.com/mozilla/readability)
- [PHP 实现 - andreskrey@GitHub](https://github.com/andreskrey/readability.php)

### 版权声明

```
Copyright (c) 2010 Arc90 Inc

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```