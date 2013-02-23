package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/kless/goconfig/config"
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

// 'config' vars
var wikiName string
var inDevMode bool

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
	c, err := config.ReadDefault("wiki.ini")
	panicIni(err)
	wikiName, err = c.String("wiki", "name")
	panicIni(err)
	servAddr, err := c.String("wiki", "serv_addr")
	panicIni(err)
	inDevMode, err = c.Bool("wiki", "dev_mode")
	panicIni(err)
	log.Printf("Read wiki.ini")

	views = template.Must(template.ParseGlob("views/[a-z]*.html"))
	log.Printf("Parsed page templates\n")

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
	log.Printf("Serving wiki pages from %s...\n", servAddr)
	log.Fatal(http.ListenAndServe(servAddr, nil))
}

func panicIni(err error) {
	if err != nil {
		fmt.Printf("Error reading wiki.ini!")
		panic(err)
	}
}

func faviconHandler(w http.ResponseWriter, r *http.Request) {
	img, _ := ioutil.ReadFile("pub/favicon.ico")
	w.Write(img)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Path[1:]
	v := verParam(r)
	if len(page) == 0 {
		renderPage(w, "home", v, wikiName)
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
	pi := &pageInfo{WikiName: wikiName, Page: page, IsHome: (page == "home")}
	pi.Content = string(bytes)
	render(w, "edit.html", pi)
}

func previewHandler(w http.ResponseWriter, r *http.Request) {
	content := r.FormValue("content")
	w.Write(formatPage([]byte(content)))
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
	pl := &pageList{WikiName: wikiName, List: list}
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
	dp := &deletedPages{WikiName: wikiName, List: list2}
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
		vi := versionInfo{WikiName: wikiName, Page: page, Mtime: m}
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
	res := &search{WikiName: wikiName, Q: q}
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
	bytes, err := readPage(page, version, w)
	if err == nil {
		pi := &pageInfo{WikiName: wikiName, Page: page, Title: title, IsHome: (page == "home")}
		if version > 0 {
			pi.Ver = fmt.Sprintf("?ver=%d", version)
		}
		pi.Content = string(formatPage(bytes))
		if fi, err := os.Stat(pageFile(page, version)); err == nil {
			pi.Mtime = fi.ModTime().Format(time.RFC1123)
		}
		render(w, "page.html", pi)
	} else {
		log.Printf("Error reading page '%s': %s\n", page, err.Error())
		pageNotFound(w)
	}
}

func formatPage(content []byte) []byte {
	return parseTables(content)
}

func parseTables(content []byte) []byte {
	out := make([]byte, 0, 32)
	i := 0
	for i < len(content) {
		j := tableStart(content[i:])
		if j < 0 {
			break
		}
		j += i

		if i < j { // format chunk before table
			out = append(out, formatWikiChunk(content[i:j])...)
		}
		out = append(out, "<table class='wiki'>\n"...)
		var k int
		out, k = parseTableRows(out, content[j:])
		out = append(out, "</table>\n"...)

		i = j + k // XXX check for off-by-1
	}
	if i < len(content) {
		out = append(out, formatWikiChunk(content[i:])...)
	}

	return out
}

var tableStartPat *regexp.Regexp = regexp.MustCompile(
	"(?:^|[\r\n])\\|[^\r\n|]*\\|")

func tableStart(content []byte) int {
	loc := tableStartPat.FindIndex(content)
	if loc == nil {
		return -1
	} else if content[loc[0]] != '|' {
		return loc[0] + 1
	}
	return loc[0]
}

func nextCellEnd(content []byte) int {
	for i := 0; i < len(content); i++ {
		switch content[i] {
		case '|': return i
		case '\r', '\n': return -1
		}
	}
	return -1
}

func parseTableRows(out, content []byte) ([]byte, int) {
	i := 0
	for i < len(content) && content[i] == '|' { // each line/row
		out = append(out, "  <tr>\n"...)

		j := i
		for j < len(content) {
			j += 1
			//k := bytes.IndexByte(content[j:], '|')
			k := nextCellEnd(content[j:])
			if k < 0 {
				break // no more cells!
			}
			k += j
			out = parseTableCell(out, content[j:k])
			j = k
			if content[k] != '|' {
				fmt.Printf("not on a pipe at %d!\n", k)
			}
		}
		out = append(out, "  </tr>\n"...)

		// j is after last '|'; find newline
		for ; j < len(content); j++ {
			if !(content[j] == ' ' || content[j] == '\t') {
				break
			}
		}
		var newline bool = false
		if content[j] == '\r' {
			newline = true
			j++
		}
		if content[j] == '\n' {
			newline = true
			j++
		}
		i = j
		if !newline {
			break
		}
	}
	return out, i
}

func parseSpan(content []byte) ([]byte) {
	j := 0
	for ; j < len(content) && '0' <= content[j] && content[j] <= '9'; j++ {}
	return content[0:j]
}

func parseTableCell(out, content []byte) []byte {
	tag := "td"
	
	atts := make([]byte, 0)
	i := 0
	for ; i < len(content); i++ {
		switch content[i] {
		case '.': i++; goto endoptions
		case '_': tag = "th"
		case '<': atts = append(atts, " align='left'"...)
		case '>': atts = append(atts, " align='right'"...)
		case '=': atts = append(atts, " align='center'"...)
		case '#': atts = append(atts, " align='justify'"...)
		case '^': atts = append(atts, " valign='top'"...)
		case '~': atts = append(atts, " valign='bottom'"...)
		case '\\': // XXX parse colspan
			if span := parseSpan(content[i+1:]); len(span) > 0 {
				atts = append(atts, " colspan='"...)
				atts = append(atts, span...)
				atts = append(atts, '\'')
				i += len(span)
			}
		case '/':  // XXX parse rowspan
			if span := parseSpan(content[i+1:]); len(span) > 0 {
				atts = append(atts, " rowspan='"...)
				atts = append(atts, span...)
				atts = append(atts, '\'')
				i += len(span)
			}
		default: if i == 0 { goto endoptions }
		}
	}
endoptions:

	out = append(out, "    <"...)
	out = append(out, tag...)
	out = append(out, atts...)
	out = append(out, ">\n      "...)

	chunk := formatWikiChunk(bytes.TrimSpace(content[i:]))
	if len(chunk) > 7 && bytes.Compare(chunk[0:3], []byte("<p>")) == 0 {
		chunk = chunk[3:len(chunk)-5]
	}
	out = append(out, chunk...)

	out = append(out, "\n    </"...)
	out = append(out, tag...)
	out = append(out, ">\n"...)

	return out
}

func formatWikiChunk(content []byte) []byte {
	return blackfriday.MarkdownCommon(linkWikiWords(content))
}

var pageLinkPat *regexp.Regexp = regexp.MustCompile(
	"(?:\\[\\[ *(\\? *)?(?:([^|\\]]+) *\\| *)?([a-zA-Z][\\w -]*) *\\]\\])|" +
		"(?:\\b((?:[A-Z](?:[0-9a-z]*[a-z][0-9a-z]*)?){2,})\\b)")

func linkWikiWords(content []byte) []byte {
	buf := make([]byte, 0, len(content)*5/4)
	for {
		m := pageLinkPat.FindSubmatchIndex(content)
		if m == nil {
			buf = append(buf, content...)
			break // no more matches
		}
		i := bytes.LastIndexAny(content[:m[0]], "\r\n")
		if i < 0 {
			i = 0
		} else {
			i++
		}
		if bytes.Compare(content[i:i+4], []byte("    ")) == 0 {
			buf = append(buf, content...)
			break // don't link anything in a code block
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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

func render(w http.ResponseWriter, templateName string, data interface{}) {
	if inDevMode {
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
