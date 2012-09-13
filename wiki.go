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
	"regexp"
	"strconv"
	"text/template"
	"time"
)

type pageInfo struct {
	Page    string
	Title   string
	Content string
	Mtime   string
}

var views *template.Template

func main() {
	views = template.Must(template.ParseGlob("views/[a-z]*.html"))

	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/edit/", editHandler)
	http.HandleFunc("/preview", previewHandler)
	http.HandleFunc("/save/", saveHandler)
	http.HandleFunc("/versions/", versionsHandler)
	http.Handle("/pub/", http.StripPrefix("/pub/", http.FileServer(http.Dir("pub"))))
	http.HandleFunc("/favicon.ico", faviconHandler)
	log.Fatal(http.ListenAndServe("0.0.0.0:8081", nil))
}

func faviconHandler(w http.ResponseWriter, r *http.Request) {
	img, _ := ioutil.ReadFile("pub/favicon.ico")
	w.Write(img)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	v := verParam(r)
	renderPage(w, "index", v, "BWiki")
}

func editHandler(w http.ResponseWriter, r *http.Request) {
	v := verParam(r)
	page := r.URL.Path[6:]
	if bytes, _ := readPage(page, v, w); bytes != nil {
		pi := &pageInfo{Page: page}
		pi.Content = string(bytes)
		render(w, "edit.html", pi)
	}
}

func previewHandler(w http.ResponseWriter, r *http.Request) {
	content := r.FormValue("content")
	md := blackfriday.MarkdownCommon([]byte(content))
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
				http.Error(w, "Error writing wiki page: " + err.Error(),
					http.StatusInternalServerError)
				return
			}
		}
		http.Redirect(w, r, "/" + page, 303)
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
		fout, err := os.OpenFile(oldPath, os.O_WRONLY | os.O_CREATE | os.O_EXCL, 0644)
		if err == nil {
			return fout
		}
	}
	panic("Ran out of old version numbers!")
}

type pageVersion struct {
	Num int
	Mtime string
}

type versionInfo struct {
	Page string
	Mtime string
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
	for i, j := 0, len(ary) - 1; i < j; i, j = i + 1, j - 1 {
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

func renderPage(w http.ResponseWriter, page string, version int, title string) {
	if bytes, err := readPage(page, version, w); err == nil {
		pi := &pageInfo{Page: page, Title: title}
		// XXX link WikiPages
		pi.Content = string(blackfriday.MarkdownCommon(bytes))
		if fi, err := os.Stat(pageFile(page, version)); err == nil {
			pi.Mtime = fi.ModTime().Format(time.RFC1123)
		}
		render(w, "page.html", pi)
	} else {
		pageNotFound(w)
	}
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
