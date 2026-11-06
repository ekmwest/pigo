package build

import (
	"bytes"
	"encoding/json"
	"path"
	"regexp"
)

// Page is one entry in docs/site_content.json, a list of the site's
// pages that sites use for things like search.
type Page struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Content     string `json:"content"`
}

// The patterns are piko's, with jsDot and jsSpace for JavaScript's "."
// and "\s".
var (
	titlePattern       = regexp.MustCompile(`<title>(` + jsDot + `*?)</title>`)
	descriptionPattern = regexp.MustCompile(`<meta name="description" content="(` + jsDot + `*?)" />`)
	asidePattern       = regexp.MustCompile(`<aside([\s\S]*?)</aside>`)
	tagPattern         = regexp.MustCompile(`</?[^>]+>`)

	mainOpenPattern  = regexp.MustCompile(`(?i)<main[^>]*>`)
	mainTagPattern   = regexp.MustCompile(`(?i)</?main`)
	mainClosePattern = regexp.MustCompile(`(?i)^</main` + jsSpace + `*>`)
)

// collectPages returns a Page for every .html file except 404.html
// files. files must be in walk order.
func collectPages(files []outputFile) []Page {
	pages := []Page{} // not nil, so that no pages encodes as [], not null
	for _, f := range files {
		if path.Ext(f.path) != ".html" || path.Base(f.path) == "404.html" {
			continue
		}
		html := string(f.content)
		pages = append(pages, Page{
			Title:       firstGroup(titlePattern, html),
			Description: firstGroup(descriptionPattern, html),
			URL:         "/" + f.path,
			Content:     collapseSpaces(stripTags(stripAsides(mainTagContent(html)))),
		})
	}
	return pages
}

// encodePages encodes pages like JSON.stringify(pages, null, 4).
func encodePages(pages []Page) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "    ")
	if err := enc.Encode(pages); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func firstGroup(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// mainTagContent ports piko's
//
//	/<main[^>]*>([^<]*(?:(?!<\/?main)<[^<]*)*)<\/main\s*>/i
//
// which Go's regexp can't express because of the lookahead. The content
// after an opening tag runs up to the next <main or </main. It is a
// match only if that is a closing tag; otherwise the next opening tag
// is tried.
func mainTagContent(s string) string {
	for _, open := range mainOpenPattern.FindAllStringIndex(s, -1) {
		content := s[open[1]:]
		next := mainTagPattern.FindStringIndex(content)
		if next == nil {
			// Later opening tags only see a part of this content.
			return ""
		}
		if mainClosePattern.MatchString(content[next[0]:]) {
			return content[:next[0]]
		}
	}
	return ""
}

func stripAsides(s string) string {
	return asidePattern.ReplaceAllLiteralString(s, "")
}

func stripTags(s string) string {
	return tagPattern.ReplaceAllLiteralString(s, "")
}

func collapseSpaces(s string) string {
	return jsTrim(jsSpaces.ReplaceAllLiteralString(s, " "))
}
