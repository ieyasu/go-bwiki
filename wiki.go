package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/russross/blackfriday"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
)

type pageInfo struct {
	Page    string
	Ver     string
	Title   string
	Content string
	Mtime   string
	IsHome  bool
}

var views *template.Template

func main() {
	views = template.Must(template.ParseGlob("views/[a-z]*.html"))

	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/delete/", deleteHandler)
	http.HandleFunc("/edit/", editHandler)
	http.HandleFunc("/preview", previewHandler)
	http.HandleFunc("/save/", saveHandler)
	http.HandleFunc("/pages", pagesHandler)
	http.HandleFunc("/versions/", versionsHandler)
	http.HandleFunc("/search", searchHandler)
	http.Handle("/pub/", http.StripPrefix("/pub/", http.FileServer(http.Dir("pub"))))
	http.HandleFunc("/favicon.ico", faviconHandler)
	log.Fatal(http.ListenAndServe("0.0.0.0:8081", nil))
}

func faviconHandler(w http.ResponseWriter, r *http.Request) {
	img, _ := ioutil.ReadFile("pub/favicon.ico")
	w.Write(img)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Path[1:]
	v := verParam(r)
	if len(page) == 0 {
		renderPage(w, "home", v, "BWiki")
	} else if isPageName(page) {
		if page == "home" {
			http.Redirect(w, r, "/", 303)
		} else {
			renderPage(w, page, v, page)
		}
	} else {
		invalidPageName(w)
	}
}

func deleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only!", http.StatusMethodNotAllowed)
		return
	}
	page := r.URL.Path[8:]
	if isPageName(page) && fileExists(pageFile(page, -1)) {
		if page == "home" {
			http.Error(w, "You mustn't delete the home page!", http.StatusBadRequest)
			return
		}
		deletePage(page)
		w.Write([]byte("/"))
	} else {
		invalidPageName(w)
	}
}

func deletePage(page string) {
	os.Rename("pages/"+page, "deleted/"+page)
	if list, _ := filepath.Glob("old/" + page + ".*"); list != nil {
		for _, old := range list {
			newPg := filepath.Join("deleted", filepath.Base(old))
			os.Rename(old, newPg)
		}
	}
}

func editHandler(w http.ResponseWriter, r *http.Request) {
	v := verParam(r)
	page := r.URL.Path[6:]
	bytes, _ := readPage(page, v, w)
	pi := &pageInfo{Page: page, IsHome: (page == "home")}
	pi.Content = string(bytes)
	render(w, "edit.html", pi)
}

func previewHandler(w http.ResponseWriter, r *http.Request) {
	content := r.FormValue("content")
	md := blackfriday.MarkdownCommon(linkWikiWords([]byte(content)))
	w.Write(md)
}

func saveHandler(w http.ResponseWriter, r *http.Request) {
	v := verParam(r)
	page := r.URL.Path[6:]
	orig, _ := readPage(page, v, w)
	if isPageName(page) {
		content := []byte(r.FormValue("content"))
		if !bytes.Equal(orig, content) { // changed, save new page
			// save old version
			fout := openNextOldFile(page)
			fout.Write(orig)
			fout.Close()

			// write new version
			err := ioutil.WriteFile(pageFile(page, -1), content, 0644)
			if err == nil {
			} else {
				http.Error(w, "Error writing wiki page: "+err.Error(),
					http.StatusInternalServerError)
				return
			}
		}
		http.Redirect(w, r, "/"+page, 303)
	}
}

func verParam(r *http.Request) int {
	var v int64 = -1
	if ver := r.FormValue("ver"); len(ver) > 0 {
		if n, err := strconv.ParseInt(ver, 10, 32); err == nil {
			v = n
		}
	}
	return int(v)
}

func openNextOldFile(page string) *os.File {
	for i := 1; i < 10000; i++ {
		oldPath := pageFile(page, i)
		fout, err := os.OpenFile(oldPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err == nil {
			return fout
		}
	}
	panic("Ran out of old version numbers!")
}

func pagesHandler(w http.ResponseWriter, r *http.Request) {
	list, err := filepath.Glob("pages/[a-zA-Z]*")
	if err != nil {
		panic(err)
	}
	for i, s := range list {
		list[i] = s[6:]
	}
	sort.Strings(list)
	render(w, "pages.html", list)
}

type pageVersion struct {
	Num   int
	Mtime string
}

type versionInfo struct {
	Page     string
	Mtime    string
	Versions []*pageVersion
}

func versionsHandler(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Path[10:]
	if isPageName(page) {
		m := fileMtime(pageFile(page, -1))
		if len(m) == 0 {
			pageNotFound(w)
			return
		}
		vi := versionInfo{Page: page, Mtime: m}
		vi.Versions = listPageVersions(page)
		render(w, "versions.html", vi)
	} else {
		invalidPageName(w)
	}
}

func listPageVersions(page string) []*pageVersion {
	var ary []*pageVersion
	for i := 1; i < 10000; i++ {
		m := fileMtime(pageFile(page, i))
		if len(m) == 0 {
			break
		}
		ary = append(ary, &pageVersion{Num: i, Mtime: m})
	}
	for i, j := 0, len(ary)-1; i < j; i, j = i+1, j-1 {
		ary[i], ary[j] = ary[j], ary[i]
	}
	return ary
}

func fileMtime(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	t := fi.ModTime().Local()
	return shortDate(t)
}

func shortDate(t time.Time) string {
	return fmt.Sprintf("%s %d, %d", t.Month().String()[0:3], t.Day(), t.Year())
}

type hit struct {
	Page  string
	Count int
	Hits  string
}

type hitSlice []*hit

func (h hitSlice) Len() int {
	return len(h)
}

func (h hitSlice) Less(i, j int) bool {
	return h[i].Count > h[j].Count
}

func (h hitSlice) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

type search struct {
	Q    string
	Hits hitSlice
}

func searchHandler(w http.ResponseWriter, r *http.Request) {
	q := r.FormValue("q")
	res := &search{Q: q}
	argv := []string{"-ci"}
	for _, aq := range strings.Fields(q) {
		argv = append(argv, "-e", aq)
	}
	pages, _ := filepath.Glob("pages/[a-zA-Z]*")
	argv = append(argv, pages...)
	out, err := exec.Command("grep", argv...).CombinedOutput()
	if err != nil {
		fmt.Printf("error greping: %s\n", err.Error())
	}

	// parse grep output--lines of "pages/<page>:<hit count>"
	hits := make(map[string]int)
	for _, line := range strings.Split(string(out), "\n") {
		if ary1 := strings.SplitN(line, "/", 2); len(ary1) == 2 {
			if ary := strings.SplitN(ary1[1], ":", 2); len(ary) == 2 {
				page := ary[0]
				if count, _ := strconv.Atoi(ary[1]); count > 0 {
					hits[page] = hits[page] + count
				}
			}
		}
	}
	for page, count := range hits {
		n := count
		if count > 50 {
			n = 50
		}
		res.Hits = append(res.Hits, &hit{Page: page, Count: count, Hits: strings.Repeat("▎", n)})
	}
	sort.Sort(res.Hits)
	render(w, "search.html", res)
}

func renderPage(w http.ResponseWriter, page string, version int, title string) {
	if bytes, err := readPage(page, version, w); err == nil {
		pi := &pageInfo{Page: page, Title: title}
		if version > 0 {
			pi.Ver = fmt.Sprintf("?ver=%d", version)
		}
		pi.Content = string(blackfriday.MarkdownCommon(linkWikiWords(bytes)))
		if fi, err := os.Stat(pageFile(page, version)); err == nil {
			pi.Mtime = fi.ModTime().Format(time.RFC1123)
		}
		render(w, "page.html", pi)
	} else {
		pageNotFound(w)
	}
}

var wikiWordPat *regexp.Regexp = regexp.MustCompile(
	"\\b(?:[A-Z](?:[0-9a-z]*[a-z][0-9a-z]*)?){2,}\\b")

func linkWikiWords(content []byte) []byte {
	return wikiWordPat.ReplaceAllFunc(content, func(word []byte) []byte {
		if fileExists(pageFile(string(word), -1)) {
			return []byte(linkWikiPage(string(word)))
		}
		sing, plu := splitPlural(string(word))
		if fileExists(pageFile(sing, -1)) {
			return []byte(linkWikiPage(string(sing)) + plu)
		}
		link := sing + "<a href=\"/edit/" + sing + "\">?</a>"
		if len(plu) > 0 {
			link += plu + "<a href=\"/edit/" + string(word) + "\">?</a>"
		}
		return []byte(link)
	})
}

func linkWikiPage(page string) string {
	return "<a href=\"/" + page + "\">" + page + "</a>"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var endES *regexp.Regexp = regexp.MustCompile("(?i)\\w(?:s|z|ch|sh|x)es$")
var endOS *regexp.Regexp = regexp.MustCompile("(?i)\\wos$")
var endOES *regexp.Regexp = regexp.MustCompile("(?i)\\woes$")
var endIES *regexp.Regexp = regexp.MustCompile("(?i)\\wies$")
var endS *regexp.Regexp = regexp.MustCompile("(?i)\\ws$")

func splitPlural(word string) (string, string) {
	var i int
	if endES.MatchString(word) {
		i = -2 // end in s, z, ch, sh, x -> remove es
	} else if endOS.MatchString(word) {
		i = -1 // end in os -> o
	} else if endOES.MatchString(word) {
		i = -2 // end in oes -> o
	} else if endIES.MatchString(word) {
		i = -3 // end in ies -> y
	} else if endS.MatchString(word) {
		i = -1 // simple plural -> remove s
	} else {
		return word, "" // not a plural
	}
	n := len(word)
	i += n
	return word[0:i], word[i:]
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

var pageFilePat *regexp.Regexp = regexp.MustCompile("^[a-zA-Z]\\w*$")

func isPageName(page string) bool {
	return pageFilePat.MatchString(page)
}

func pageFile(page string, version int) string {
	if version < 1 {
		return "pages/" + page
	}
	return fmt.Sprintf("old/%s.%d", page, version)
}

func render(w http.ResponseWriter, templateName string, data interface{}) {
	// XXX remove re-parsing line after done with inital dev
	views = template.Must(template.ParseGlob("views/[a-z]*.html"))
	err := views.ExecuteTemplate(w, templateName, data)
	if err != nil {
		serverError(w, err)
	}
}

func serverError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
