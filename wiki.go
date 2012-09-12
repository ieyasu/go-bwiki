package main

import (
	"github.com/russross/blackfriday"
	"io/ioutil"
	"log"
	"net/http"
	"text/template"
)

type pageInfo struct {
	Title string
	Content string
}

var views *template.Template

func main() {
	views = template.Must(template.ParseGlob("views/[a-z]*.html"))

	http.HandleFunc("/", rootHandler)
	http.Handle("/pub/", http.StripPrefix("/pub/", http.FileServer(http.Dir("pub"))))
	log.Fatal(http.ListenAndServe("0.0.0.0:8081", nil))
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	renderPage(w, "index", "BWiki")
}

func renderPage(w http.ResponseWriter, page string, title string) {
	pi := &pageInfo{Title: title}
	bytes, err := ioutil.ReadFile("pages/" + page)
	if err == nil {
		pi.Content = string(blackfriday.MarkdownCommon(bytes))
		render(w, "index.html", pi)
	} else {
		http.Error(w, "Page not found", http.StatusNotFound)
	}
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
