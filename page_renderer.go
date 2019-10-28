package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/parser"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"time"
)

const mdExt parser.Extensions = parser.Tables | parser.FencedCode |
	parser.Autolink | parser.Strikethrough | parser.SpaceHeadings |
	parser.NoEmptyLineBeforeBlock | parser.HeadingIDs | parser.AutoHeadingIDs |
	parser.BackslashLineBreak | parser.DefinitionLists | parser.MathJax |
	parser.SuperSubscript

func renderMarkdown(content []byte) []byte {
	// carriage returns (ASCII 13) are messing things up
	content = bytes.Replace(content, []byte{13}, []byte{}, -1)
	content = linkWikiWords(content)
	mdParser := parser.NewWithExtensions(mdExt)
	content = markdown.ToHTML(content, mdParser, nil)
	return content
}

func renderPage(w http.ResponseWriter, page string, version int, title string) {
	if bytes, err := readPage(page, version, w); err == nil {
		pi := &pageInfo{WikiName: cfg.wikiName, Page: page, Title: title, IsHome: (page == "home")}
		if version > 0 {
			pi.Ver = fmt.Sprintf("?ver=%d", version)
		}
		pi.Content = string(renderMarkdown(bytes))
		if fi, err := os.Stat(pageFile(page, version)); err == nil {
			pi.Mtime = fi.ModTime().Format(time.RFC1123)
		}
		render(w, "page.html", pi)
	} else {
		pageNotFound(w)
	}
}

var pageLinkPat *regexp.Regexp = regexp.MustCompile(
	"(?:\\[\\[ *(\\? *)?(?:([^|\\]]+) *\\| *)?([a-zA-Z][\\w -]*) *\\]\\])|" +
		"(?:\\b((?:[A-Z](?:[0-9a-z]*[a-z][0-9a-z]*)?){2,})\\b)")

func linkWikiWords(content []byte) []byte {
	// XXX whoa, need to use a byte buffer!
	buf := make([]byte, 0, len(content)*5/4)
	for {
		m := pageLinkPat.FindSubmatchIndex(content)
		if m == nil {
			buf = append(buf, content[0:]...)
			break // no more matches
		}
		buf = append(buf, content[0:m[0]]...)
		// m[0] - start of [[, m[1] - after ]]
		// m[2],m[3] - ! (turning off linking)
		// m[4],m[5] - link text
		// m[6],m[7] - page
		// m[8],m[9] - WikiWord
		if m[2] > 0 { // don't link
			buf = append(buf, content[m[6]:m[7]]...)
		} else if m[6] > 0 { // double-bracketed page link
			page := content[m[6]:m[7]]
			var linktext []byte
			if m[4] > 0 { // replacement link text
				linktext = content[m[4]:m[5]]
			} else {
				linktext = page
			}
			page = bytes.Replace(page, []byte(" "), []byte("-"), -1)
			buf = linkWikiPage(buf, page, linktext)
		} else { // WikiWord
			page := content[m[8]:m[9]]
			buf = linkWikiPage(buf, page, nil)
		}

		content = content[m[1]:]
	}
	return buf
}

// Normally wiki pages are singular, so use the depluralized version unless
// the page exists as is.
func linkWikiPage(buf []byte, page []byte, linktext []byte) []byte {
	var deplu bool
	if linktext == nil {
		linktext = page
		deplu = true
	}
	var pageExists bool = fileExists(pageFile(string(page), -1))
	if !pageExists && deplu {
		page = depluralize(page)
		pageExists = fileExists(pageFile(string(page), -1))
	}
	title := page
	buf = append(buf, []byte("<a href=\"/")...)
	if !pageExists {
		title = make([]byte, 0, len(page)+22)
		title = append(title, page...)
		title = append(title, []byte(" (page does not exist)")...)

		buf = append(buf, []byte("edit/")...)
	}
	buf = append(buf, page...)
	buf = append(buf, []byte("\" title=\"")...)
	buf = append(buf, title...)
	buf = append(buf, '"')
	if !pageExists {
		buf = append(buf, []byte(" class=\"new\"")...)
	}
	buf = append(buf, '>')
	buf = append(buf, linktext...)
	return append(buf, []byte("</a>")...)
}

var endES *regexp.Regexp = regexp.MustCompile("(?i)\\w(?:s|z|ch|sh|x)es$")
var endOS *regexp.Regexp = regexp.MustCompile("(?i)\\wos$")
var endOES *regexp.Regexp = regexp.MustCompile("(?i)\\woes$")
var endIES *regexp.Regexp = regexp.MustCompile("(?i)\\wies$")
var endS *regexp.Regexp = regexp.MustCompile("(?i)\\ws$")

func depluralize(word []byte) []byte {
	n := len(word)
	var i int
	if endES.Match(word) {
		i = -2 // end in (s, z, ch, sh, x)es -> remove es
	} else if endOS.Match(word) {
		i = -1 // end in os -> o
	} else if endOES.Match(word) {
		i = -2 // end in oes -> o
	} else if endIES.Match(word) {
		buf := make([]byte, 0, n-2)
		buf = append(buf, word[0:n-3]...)
		return append(buf, 'y') // end in ies -> y
	} else if endS.Match(word) {
		i = -1 // simple plural -> remove s
	} else {
		return word // not a plural
	}
	return word[0 : i+n]
}

func pageNotFound(w http.ResponseWriter) {
	http.Error(w, "Wiki page not found", http.StatusNotFound)
}

func readPage(page string, version int, w http.ResponseWriter) ([]byte, error) {
	if isPageName(page) {
		file := pageFile(page, version)
		return ioutil.ReadFile(file)
	}
	invalidPageName(w)
	return nil, errors.New("invalid page")
}

func invalidPageName(w http.ResponseWriter) {
	http.Error(w, "Invalid page name", http.StatusForbidden)
}

var pageFilePat *regexp.Regexp = regexp.MustCompile("^[a-zA-Z][\\w-]*$")

func isPageName(page string) bool {
	return pageFilePat.MatchString(page)
}

func pageFile(page string, version int) string {
	if version < 1 {
		return "pages/" + page
	}
	return fmt.Sprintf("old/%s.%d", page, version)
}
