package build

import (
	"maps"
	"strings"
	"testing"
)

func TestRenderVariables(t *testing.T) {
	data := map[string]string{"title": "Home", "empty": ""}
	tests := []struct {
		text string
		want string
	}{
		{"{{ title }}", "Home"},
		{"{{title}}", "Home"},
		{"{{  title  }}", "Home"},
		{"{{ title || Default }}", "Home"},
		{"{{ missing || Default }}", "Default"},
		{"{{ empty || Default }}", "Default"},
		{"{{ missing || }}", ""},
		{"{{missing||}}", ""},
		{"{{ missing }}", "{{ missing }}"},
		{"{{ empty }}", "{{ empty }}"},
		{"{{ missing || a || b }}", "a"},
		{"a {{title}} b {{ missing || x }} c", "a Home b x c"},
		{"{{ title\n}}", "{{ title\n}}"},
	}
	for _, tt := range tests {
		if got := renderVariables(tt.text, data); got != tt.want {
			t.Errorf("renderVariables(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

func TestParseIncludeComment(t *testing.T) {
	tests := []struct {
		comment   string
		wantName  string
		wantProps map[string]string
		wantErr   string // substring
	}{
		{comment: "<!-- nav.html -->", wantName: "nav.html"},
		{comment: "<!--nav.html-->", wantName: "nav.html"},
		{comment: "<!-- svg/search.svg -->", wantName: "svg/search.svg"},
		{
			comment:   `<!-- shot.html, { src: "a.png", alt: "Shot " } -->`,
			wantName:  "shot.html",
			wantProps: map[string]string{"src": "a.png", "alt": "Shot "},
		},
		{
			comment:   `<!-- shot.html,{src:"a, b: c",$x_1:"",} -->`,
			wantName:  "shot.html",
			wantProps: map[string]string{"src": "a, b: c", "$x_1": ""},
		},
		{comment: `<!-- shot.html, {} -->`, wantName: "shot.html", wantProps: map[string]string{}},
		{comment: `<!-- shot.html, { a: "1", a: "2" } -->`, wantName: "shot.html", wantProps: map[string]string{"a": "2"}},
		{comment: `<!-- shot.html, -->`, wantErr: `expected "{"`},
		{comment: `<!-- shot.html, { src: 'a' } -->`, wantErr: "double-quoted"},
		{comment: `<!-- shot.html, { src: "a" alt: "b" } -->`, wantErr: `expected "," or "}"`},
		{comment: `<!-- shot.html, { src "a" } -->`, wantErr: `expected ":" after src`},
		{comment: `<!-- shot.html, { "src": "a" } -->`, wantErr: "expected a key"},
		{comment: `<!-- shot.html, { 1a: "a" } -->`, wantErr: "expected a key"},
		{comment: `<!-- shot.html, { n: 3 } -->`, wantErr: "double-quoted"},
		{comment: `<!-- shot.html, { a: "x\"y" } -->`, wantErr: "backslash"},
		{comment: `<!-- shot.html, { a: "x } -->`, wantErr: "missing closing quote"},
		{comment: `<!-- shot.html, { a: "x" } extra -->`, wantErr: "unexpected text"},
		{comment: `<!-- shot.html, { a: "x" -->`, wantErr: `expected "," or "}"`},
		{comment: `<!-- , { a: "x.html" } -->`, wantErr: "no file name"},
	}
	for _, tt := range tests {
		name, props, err := parseIncludeComment(tt.comment)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s: error = %v, want it to contain %q", tt.comment, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tt.comment, err)
			continue
		}
		if name != tt.wantName || !maps.Equal(props, tt.wantProps) {
			t.Errorf("%s: got %q %v, want %q %v", tt.comment, name, props, tt.wantName, tt.wantProps)
		}
	}
}

func TestReadTextOutsideFolder(t *testing.T) {
	r := newRenderer(t.TempDir())
	defer r.close()
	for _, name := range []string{"../x.html", "/x.html", "a/../../x.html", ""} {
		if _, err := readText(r.includes, name); err != errOutside {
			t.Errorf("readText(%q) error = %v, want errOutside", name, err)
		}
	}
}

func TestApplyLayoutInsertsLiterally(t *testing.T) {
	src := t.TempDir()
	writeFiles(t, src, map[string]string{"_layouts/base.html": "<b>{{content}}</b> {{ content }}"})
	r := newRenderer(src)
	defer r.close()

	got := r.applyLayout("$& $1", "base.html")
	if want := "<b>$& $1</b> {{ content }}"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripBOM(t *testing.T) {
	data, content, err := parseFrontmatter(stripBOM("\xef\xbb\xbf---\ntitle: T\n---\nc"))
	if err != nil || data["title"] != "T" || content != "c" {
		t.Errorf("got %v %q %v", data, content, err)
	}
}
