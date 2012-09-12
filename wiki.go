package main

import (
	"log"
	"net/http"
	"html/template"
)

var views *template.Template

func main() {
	views = template.Must(template.ParseGlob("views/[a-z]*.html"))

	http.HandleFunc("/", rootHandler)
	http.Handle("/pub/", http.StripPrefix("/pub/", http.FileServer(http.Dir("pub"))))
	log.Fatal(http.ListenAndServe("0.0.0.0:8081", nil))
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	render(w, "index.html", nil)
}

func render(w http.ResponseWriter, templateName string, data interface{}) {
	views = template.Must(template.ParseGlob("views/[a-z]*.html"))
	err := views.ExecuteTemplate(w, templateName, data)
	if err != nil {
		serverError(w, err)
	}
}

func serverError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
