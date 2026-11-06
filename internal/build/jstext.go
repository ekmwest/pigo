package build

import (
	"regexp"
	"strings"
)

// piko is JavaScript, and its regexes and trim() have JavaScript
// semantics. Go's differ in ways that change output, so the ports use
// these instead of Go's ".", "\s" and strings.TrimSpace.

// jsDot matches what "." matches in a JavaScript regex: anything but a
// line terminator. Go's "." also matches "\r", U+2028 and U+2029.
const jsDot = `[^\n\r\x{2028}\x{2029}]`

// jsSpace matches what "\s" matches in a JavaScript regex. Go's "\s" is
// ASCII only, so it would leave non-breaking spaces in place.
const jsSpace = `[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var jsSpaces = regexp.MustCompile(jsSpace + `+`)

func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// jsTrim is JavaScript's String.prototype.trim.
func jsTrim(s string) string {
	return strings.TrimFunc(s, isJSSpace)
}

func jsTrimLeft(s string) string {
	return strings.TrimLeftFunc(s, isJSSpace)
}
