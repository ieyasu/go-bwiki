package main

import (
	"errors"
	"github.com/russross/blackfriday"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
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
	http.Handle("/pub/", http.StripPrefix("/pub/", http.FileServer(http.Dir("pub"))))
	log.Fatal(http.ListenAndServe("0.0.0.0:8081", nil))
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	renderPage(w, "index", "BWiki")
}

func editHandler(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Path[6:]
	if bytes, _ := readPage(page, w); bytes != nil {
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
	if page := r.URL.Path[6:]; isPageName(page) {
		content := r.FormValue("content")
		err := ioutil.WriteFile(pageFile(page), []byte(content), os.ModePerm)
		if err == nil {
			http.Redirect(w, r, "/" + page, 303)
		} else {
			http.Error(w, "Error writing wiki page: " + err.Error(),
				http.StatusInternalServerError)
		}
	}
}

func renderPage(w http.ResponseWriter, page string, title string) {
	if bytes, err := readPage(page, w); err == nil {
		pi := &pageInfo{Page: page, Title: title}
		pi.Content = string(blackfriday.MarkdownCommon(bytes))
		if fi, err := os.Stat(pageFile(page)); err == nil {
			pi.Mtime = fi.ModTime().Format(time.RFC1123)
		}
		render(w, "page.html", pi)
	} else {
		http.Error(w, "Wiki page not found", http.StatusNotFound)
	}
}

func readPage(page string, w http.ResponseWriter) ([]byte, error) {
	if isPageName(page) {
		return ioutil.ReadFile(pageFile(page))
	}
	http.Error(w, "Invalid page name", http.StatusForbidden)
	return nil, errors.New("invalid page")
}

var pageFilePat *regexp.Regexp = regexp.MustCompile("^[a-zA-Z]\\w*$")

func isPageName(page string) bool {
	return pageFilePat.MatchString(page)
}

func pageFile(page string) string {
	return "pages/" + page
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
