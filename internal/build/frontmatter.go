package build

import (
	"fmt"
	"regexp"
	"strings"
)

var frontmatterKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// parseFrontmatter splits text into its frontmatter data and content.
// Frontmatter exists only if the first line is "---" and a later line
// is "---". Without it, data is empty and content is text unchanged.
func parseFrontmatter(text string) (map[string]string, string, error) {
	data := map[string]string{}

	first, body, found := strings.Cut(text, "\n")
	if !found || strings.TrimSuffix(first, "\r") != "---" {
		return data, text, nil
	}

	var header []string
	for {
		line, next, found := strings.Cut(body, "\n")
		line = strings.TrimSuffix(line, "\r")
		if line == "---" {
			body = next
			break
		}
		if !found {
			return data, text, nil
		}
		header = append(header, line)
		body = next
	}

	for i, line := range header {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, _ := strings.Cut(line, ":")
		if !frontmatterKey.MatchString(key) || (value != "" && value[0] != ' ' && value[0] != '\t') {
			// Line 1 is the opening "---".
			return nil, "", fmt.Errorf("frontmatter line %d: expected \"key: value\", got %q", i+2, line)
		}
		data[key] = unquote(strings.TrimSpace(value))
	}

	// piko trims the content after frontmatter, but not a page without it.
	return data, jsTrim(body), nil
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
