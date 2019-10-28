package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
)

type pageInfo struct {
	WikiName string
	Page     string
	Ver      string
	Title    string
	Content  string
	Mtime    string
	IsHome   bool
}

var views *template.Template

func main() {
	readConfig()

	views = template.Must(template.ParseGlob("views/[a-z]*.html"))

	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/delete/", deleteHandler)
	http.HandleFunc("/restore/", restoreHandler)
	http.HandleFunc("/edit/", editHandler)
	http.HandleFunc("/preview", previewHandler)
	http.HandleFunc("/save/", saveHandler)
	http.HandleFunc("/pages", pagesHandler)
	http.HandleFunc("/deleted", deletedHandler)
	http.HandleFunc("/versions/", versionsHandler)
	http.HandleFunc("/search", searchHandler)
	http.Handle("/pub/", http.StripPrefix("/pub/", http.FileServer(http.Dir("pub"))))
	http.HandleFunc("/favicon.ico", faviconHandler)

	log.Printf("Serving wiki pages on %v\n", cfg.servAddr)
	log.Fatal(http.ListenAndServe(cfg.servAddr, nil))
}

func faviconHandler(w http.ResponseWriter, r *http.Request) {
	img, _ := ioutil.ReadFile("pub/favicon.ico")
	w.Write(img)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Path[1:]
	v := verParam(r)
	if len(page) == 0 {
		renderPage(w, "home", v, cfg.wikiName)
	} else if isPageName(page) {
		if page == "home" {
			var u string
			if v > 0 {
				u = fmt.Sprintf("/?ver=%d", v)
			} else {
				u = "/"
			}
			http.Redirect(w, r, u, 302)
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
		// XXX log page deletion
		w.Write([]byte("/"))
	} else {
		invalidPageName(w)
	}
}

func deletePage(page string) {
	dp := "deleted/" + page
	if fileExists(dp) { // rename deleted/<page> and renumber old pages
		i := nextFileNum(dp)
		os.Rename(dp, fmt.Sprintf("%s.%d", dp, i))
		if list, _ := filepath.Glob("old/" + page + ".*"); list != nil {
			sort.Strings(list)
			for _, old := range list {
				i++
				os.Rename(old, fmt.Sprintf("deleted/%s.%d", page, i))
			}
		}
	} else {
		mvGlob(page+".*", "old/", "deleted/")
	}
	os.Rename("pages/"+page, dp)
}

func restoreHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only!", http.StatusMethodNotAllowed)
		return
	}
	page := r.URL.Path[9:]
	if isPageName(page) && fileExists("deleted/"+page) {
		redirTo := restorePage(page)
		w.Write([]byte(redirTo))
	} else {
		invalidPageName(w)
	}
}

func restorePage(page string) string {
	// 1. count # deleted versions
	deletedVers := sortedVersions("deleted/" + page)
	n := len(deletedVers)
	pageAlreadyThere := fileExists(pageFile(page, -1))
	if pageAlreadyThere {
		n++
	}
	// 2. rename pre-existing old versions ahead of deleted version count
	if n > 0 {
		preExistVers := sortedVersions("old/" + page)
		for _, ver := range preExistVers {
			old := fmt.Sprintf("old/%s.%d", page, ver)
			newname := fmt.Sprintf("old/%s.%d", page, ver+n)
			os.Rename(old, newname)
		}
	}
	// 3. rename deleteds to old.(1:n-1)
	i := 1
	for _, ver := range deletedVers {
		old := fmt.Sprintf("deleted/%s.%d", page, ver)
		newname := fmt.Sprintf("old/%s.%d", page, i)
		os.Rename(old, newname)
		i++
	}
	// 4. rename deleted page as appropriate and redirect
	if pageAlreadyThere {
		os.Rename("deleted/"+page, fmt.Sprintf("old/%s.%d", page, n))
		return fmt.Sprintf("/edit/%s?ver=%d", page, n)
	}
	os.Rename("deleted/"+page, "pages/"+page)
	return "/" + page
}

func sortedVersions(prefix string) []int {
	var vers []int
	if list, _ := filepath.Glob(prefix + ".*"); list != nil {
		vers = make([]int, len(list))
		for i, p := range list {
			j := strings.IndexRune(p, '.') + 1
			vers[i], _ = strconv.Atoi(p[j:])
		}
		sort.Ints(vers)
	}
	return vers
}

func mvGlob(pat, fromDir, toDir string) {
	if list, _ := filepath.Glob(fromDir + pat); list != nil {
		for _, old := range list {
			os.Rename(old, toDir+filepath.Base(old))
		}
	}
}

func editHandler(w http.ResponseWriter, r *http.Request) {
	v := verParam(r)
	page := r.URL.Path[6:]
	bytes, _ := readPage(page, v, w)
	pi := &pageInfo{WikiName: cfg.wikiName, Page: page, IsHome: (page == "home")}
	pi.Content = string(bytes)
	render(w, "edit.html", pi)
}

func previewHandler(w http.ResponseWriter, r *http.Request) {
	content := r.FormValue("content")
	w.Write(renderMarkdown([]byte(content)))
}

func saveHandler(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Path[6:]
	orig, err := readPage(page, -1, w)
	if isPageName(page) {
		content := []byte(r.FormValue("content"))
		if err != nil || !bytes.Equal(orig, content) { // changed, save new page
			if err == nil { // save old version
				fout := openNextOldFile(page)
				fout.Write(orig)
				fout.Close()
			}

			// write new version
			err := ioutil.WriteFile(pageFile(page, -1), content, 0644)
			if err == nil {
				// XXX log the fact a page was edited, ip addy, etc
			} else {
				http.Error(w, "Error writing wiki page: "+err.Error(),
					http.StatusInternalServerError)
				return
			}
		}
		http.Redirect(w, r, "/"+page, 302)
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
	i := nextFileNum("old/" + page)
	fout, err := os.OpenFile(pageFile(page, i), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		panic(err)
	}
	return fout
}

func nextFileNum(prefix string) int {
	for i := 1; i < 10000; i++ {
		_, err := os.Stat(fmt.Sprintf("%s.%d", prefix, i))
		if err != nil {
			return i
		}
	}
	panic("Ran out of file version numbers!")
}

type pageList struct {
	WikiName string
	List     []string
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
	pl := &pageList{WikiName: cfg.wikiName, List: list}
	render(w, "pages.html", pl)
}

type deletedPage struct {
	Page  string
	Mtime string
}

type deletedPages struct {
	WikiName string
	List     []*deletedPage
}

func deletedHandler(w http.ResponseWriter, r *http.Request) {
	list, err := filepath.Glob("deleted/[a-zA-Z]*")
	if err != nil {
		panic(err)
	}
	sort.Strings(list)
	var list2 []*deletedPage
	for _, path := range list {
		if !strings.ContainsRune(path, '.') {
			m := fileMtime(path)
			list2 = append(list2, &deletedPage{Page: path[8:], Mtime: m})
		}
	}
	dp := &deletedPages{WikiName: cfg.wikiName, List: list2}
	render(w, "deleted.html", dp)
}

type pageVersion struct {
	Num   int
	Mtime string
}

type versionInfo struct {
	WikiName string
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
		vi := versionInfo{WikiName: cfg.wikiName, Page: page, Mtime: m}
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
	WikiName string
	Q        string
	Hits     hitSlice
}

func searchHandler(w http.ResponseWriter, r *http.Request) {
	q := r.FormValue("q")
	res := &search{WikiName: cfg.wikiName, Q: q}
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func render(w http.ResponseWriter, templateName string, data interface{}) {
	if cfg.devMode {
		views = template.Must(template.ParseGlob("views/[a-z]*.html"))
	}
	err := views.ExecuteTemplate(w, templateName, data)
	if err != nil {
		serverError(w, err)
	}
}

func serverError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
