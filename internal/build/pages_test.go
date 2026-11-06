package build

import (
	"slices"
	"testing"
)

func TestMainTagContent(t *testing.T) {
	tests := []struct {
		html string
		want string
	}{
		{"<main>hi</main>", "hi"},
		{`<body><MAIN class="x">a <b>b</b></Main ></body>`, "a <b>b</b>"},
		{"<main>a\n<p>b</p>\n</main\n>", "a\n<p>b</p>\n"},
		{"no main", ""},
		{"<main>unclosed", ""},
		// Nested: the outer one has a <main inside, so the inner one wins.
		{"<main>a<main>b</main>c</main>", "b"},
		{"<main>a</mainx>", ""},
		{"<main>a<mainly>b</main>", "b"},
		{"<main>first</main><main>second</main>", "first"},
	}
	for _, tt := range tests {
		if got := mainTagContent(tt.html); got != tt.want {
			t.Errorf("mainTagContent(%q) = %q, want %q", tt.html, got, tt.want)
		}
	}
}

func TestCollapseSpaces(t *testing.T) {
	// Non-breaking space, em space and BOM are whitespace in JavaScript.
	got := collapseSpaces("\xc2\xa0 a \t\n b\xc2\xa0\xe2\x80\x83c \xef\xbb\xbf")
	if want := "a b c"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCollectPages(t *testing.T) {
	files := []outputFile{
		{path: "404.html", content: []byte("<title>Not found</title>")},
		{path: "a.css", copyFrom: "x"},
		{path: "blog/404.html", content: []byte("<title>x</title>")},
		{path: "blog/post.html", content: []byte(`<title>Post</title><meta name="description" content="About &amp; more" /><main><h1>Post</h1> <aside>skip</aside><p>Body</p></main>`)},
		{path: "logo.svg", content: []byte("<svg/>")},
	}
	got := collectPages(files)
	want := []Page{{Title: "Post", Description: "About &amp; more", URL: "/blog/post.html", Content: "Post Body"}}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestEncodePages(t *testing.T) {
	got, err := encodePages(collectPages(nil))
	if err != nil || string(got) != "[]" {
		t.Errorf("empty: got %q, %v; want []", got, err)
	}

	got, err = encodePages([]Page{{Title: `<a> & "b"`, URL: "/"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "[\n    {\n        \"title\": \"<a> & \\\"b\\\"\",\n        \"description\": \"\",\n        \"url\": \"/\",\n        \"content\": \"\"\n    }\n]"
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestCompareWalkOrder(t *testing.T) {
	paths := []string{"b.i/x.html", "b.html", "a/z.html", "a.html", "a", "a-b.html"}
	slices.SortFunc(paths, compareWalkOrder)
	want := []string{"a", "a/z.html", "a-b.html", "a.html", "b.html", "b.i/x.html"}
	if !slices.Equal(paths, want) {
		t.Errorf("got %v, want %v", paths, want)
	}
}
