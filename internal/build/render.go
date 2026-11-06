package build

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

var (
	// The patterns are piko's, with jsDot for JavaScript's ".".
	contentPattern  = regexp.MustCompile(`\{\{ ?content ?\}\}`)
	variablePattern = regexp.MustCompile(`\{\{(` + jsDot + `*?)\}\}`)
	commentPattern  = regexp.MustCompile(`<!--` + jsDot + `*?-->`)
)

// Raw HTML is allowed because markdown pages contain HTML and include
// comments. The GFM extensions are listed one by one, instead of
// extension.GFM, to render table alignment as an align attribute, like
// piko's markdown renderer (marked) does.
var markdown = goldmark.New(
	goldmark.WithExtensions(
		extension.Linkify,
		extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)),
		extension.Strikethrough,
		extension.TaskList,
	),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

type renderer struct {
	// includes and layouts are nil when the folder doesn't exist. Reading
	// through an os.Root keeps names from escaping the folder.
	includes *os.Root
	layouts  *os.Root
	problems []string
}

func newRenderer(src string) *renderer {
	r := &renderer{}
	// A missing folder is fine; a page that needs it reports the problem.
	r.includes, _ = os.OpenRoot(filepath.Join(src, "_includes"))
	r.layouts, _ = os.OpenRoot(filepath.Join(src, "_layouts"))
	return r
}

func (r *renderer) close() {
	if r.includes != nil {
		r.includes.Close()
	}
	if r.layouts != nil {
		r.layouts.Close()
	}
}

func (r *renderer) problemf(format string, args ...any) {
	r.problems = append(r.problems, fmt.Sprintf(format, args...))
}

// renderPage renders a page's source text. It returns every problem
// found, so that all of them can be reported at once.
func (r *renderer) renderPage(text string, isMarkdown bool) (string, []string) {
	r.problems = nil

	data, content, err := parseFrontmatter(stripBOM(text))
	if err != nil {
		return "", []string{err.Error()}
	}

	if isMarkdown {
		var buf bytes.Buffer
		if err := markdown.Convert([]byte(content), &buf); err != nil {
			return "", []string{fmt.Sprintf("render markdown: %v", err)}
		}
		content = buf.String()
	}

	if layout := data["layout"]; layout != "" {
		content = r.applyLayout(content, layout)
	}

	content = r.renderIncludesAndVariables(content, data, nil)
	return content, r.problems
}

func (r *renderer) applyLayout(content, layout string) string {
	text, err := readText(r.layouts, layout)
	if err != nil {
		r.problemf("%s", describeReadError("layout", layout, "src/_layouts", err))
		return content
	}
	loc := contentPattern.FindStringIndex(text)
	if loc == nil {
		r.problemf("layout %q has no {{ content }}", layout)
		return content
	}
	// Inserted as is: piko's string replace would expand "$&" and similar
	// patterns in the page, which is never what the page meant.
	return text[:loc[0]] + content + text[loc[1]:]
}

// renderIncludesAndVariables renders variables in text, then replaces
// include comments with the rendered include. stack holds the includes
// being rendered, outermost first, to detect cycles.
func (r *renderer) renderIncludesAndVariables(text string, data map[string]string, stack []string) string {
	text = renderVariables(text, data)

	return commentPattern.ReplaceAllStringFunc(text, func(comment string) string {
		if !strings.Contains(comment, ".html") && !strings.Contains(comment, ".svg") {
			return comment // a regular comment
		}

		where := ""
		if len(stack) > 0 {
			where = " (included from " + stack[len(stack)-1] + ")"
		}

		name, props, err := parseIncludeComment(comment)
		if err != nil {
			r.problemf("%v%s", err, where)
			return comment
		}
		name = path.Clean(name)
		if slices.Contains(stack, name) {
			r.problemf("include cycle: %s -> %s", strings.Join(stack, " -> "), name)
			return comment
		}

		includeText, err := readText(r.includes, name)
		if err != nil {
			r.problemf("%s%s", describeReadError("include", name, "src/_includes", err), where)
			return comment
		}

		merged := make(map[string]string, len(data)+len(props))
		maps.Copy(merged, data)
		maps.Copy(merged, props)
		return r.renderIncludesAndVariables(includeText, merged, append(slices.Clone(stack), name))
	})
}

// renderVariables replaces {{ name }} with the value of name, and
// {{ name || default }} with the default when the value is empty.
// Without a default, an unknown name is left as it is.
func renderVariables(text string, data map[string]string) string {
	return variablePattern.ReplaceAllStringFunc(text, func(match string) string {
		// Like piko's split('||'): more than one || ignores the rest.
		parts := strings.Split(match[2:len(match)-2], "||")
		if value := data[jsTrim(parts[0])]; value != "" {
			return value
		}
		if len(parts) < 2 {
			return match
		}
		return jsTrim(parts[1])
	})
}

// parseIncludeComment parses "<!-- name -->" or "<!-- name, { props } -->".
func parseIncludeComment(comment string) (string, map[string]string, error) {
	s := strings.Replace(comment, "<!--", "", 1)
	s = strings.Replace(s, "-->", "", 1)
	s = jsTrim(s)

	name, propsText, hasProps := strings.Cut(s, ",")
	name = jsTrim(name)
	if name == "" {
		return "", nil, fmt.Errorf("include comment %s has no file name", comment)
	}
	if !hasProps {
		return name, nil, nil
	}

	propsText = jsTrim(propsText)
	props, err := parseProps(propsText)
	if err != nil {
		return "", nil, fmt.Errorf("include %q has invalid props %s: %v (props look like { key: \"value\" })", name, propsText, err)
	}
	return name, props, nil
}

// parseProps parses { key: "value", ... }: unquoted keys and single or
// double quoted values, kept exactly. piko evaluates props as JavaScript; this
// accepts the subset that sites use, without evaluating anything.
func parseProps(s string) (map[string]string, error) {
	rest, ok := strings.CutPrefix(s, "{")
	if !ok {
		return nil, errors.New(`expected "{"`)
	}
	props := map[string]string{}
	for {
		rest = jsTrimLeft(rest)
		if after, ok := strings.CutPrefix(rest, "}"); ok {
			if jsTrim(after) != "" {
				return nil, errors.New(`unexpected text after "}"`)
			}
			return props, nil
		}

		n := identifierLength(rest)
		if n == 0 {
			return nil, errors.New("expected a key")
		}
		key := rest[:n]

		rest, ok = strings.CutPrefix(jsTrimLeft(rest[n:]), ":")
		if !ok {
			return nil, fmt.Errorf("expected \":\" after %s", key)
		}
		rest = jsTrimLeft(rest)
		if rest == "" || (rest[0] != '"' && rest[0] != '\'') {
			return nil, fmt.Errorf("expected a quoted value for %s", key)
		}
		quote := rest[0]
		rest = rest[1:]
		end := strings.IndexAny(rest, string(quote)+`\`)
		if end == -1 {
			return nil, fmt.Errorf("missing closing quote for %s", key)
		}
		if rest[end] == '\\' {
			return nil, fmt.Errorf("value of %s has a backslash; escapes are not supported", key)
		}
		props[key] = rest[:end]

		rest = jsTrimLeft(rest[end+1:])
		if after, ok := strings.CutPrefix(rest, ","); ok {
			rest = after
		} else if !strings.HasPrefix(rest, "}") {
			return nil, fmt.Errorf("expected \",\" or \"}\" after the value of %s", key)
		}
	}
}

// identifierLength returns the length of the JavaScript identifier
// (ASCII only) at the start of s, or 0.
func identifierLength(s string) int {
	for i := 0; i < len(s); i++ {
		c := s[i]
		letter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '$'
		digit := c >= '0' && c <= '9'
		if !letter && !(digit && i > 0) {
			return i
		}
	}
	return len(s)
}

// readText reads name from root. root is nil when its folder is missing.
func readText(root *os.Root, name string) (string, error) {
	if !filepath.IsLocal(filepath.FromSlash(name)) {
		return "", errOutside
	}
	if root == nil {
		return "", fs.ErrNotExist
	}
	b, err := root.ReadFile(filepath.FromSlash(name))
	if err != nil {
		return "", err
	}
	return stripBOM(string(b)), nil
}

var errOutside = errors.New("outside its folder")

func describeReadError(kind, name, folder string, err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("missing %s %q in %s", kind, name, folder)
	case errors.Is(err, errOutside):
		return fmt.Sprintf("%s %q is outside %s", kind, name, folder)
	default:
		return fmt.Sprintf("can't read %s %q: %v", kind, name, err)
	}
}

// stripBOM removes a UTF-8 byte order mark, like Deno's readTextFile
// does in piko. Otherwise frontmatter after a BOM would not be found.
func stripBOM(s string) string {
	return strings.TrimPrefix(s, "\xef\xbb\xbf")
}
