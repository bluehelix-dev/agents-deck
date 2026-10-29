package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// printable makes text a board reported (its type, firmware, ID, Wi-Fi
// network, address) safe to print in a terminal. Control characters (ESC,
// CR, LF, tab, DEL, the C1 range) and the marks that reverse text direction
// are shown as visible escapes such as \x1b, so a board cannot move the
// cursor, rewrite earlier rows, set the window title or write the
// clipboard. Bytes that are not UTF-8 are shown the same way.
func printable(s string) string {
	ok := true
	for _, r := range s {
		if r == utf8.RuneError || unsafeRune(r) {
			ok = false
			break
		}
	}
	if ok {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && n <= 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case r < 0x100 && unsafeRune(r):
			fmt.Fprintf(&b, `\x%02x`, r)
		case unsafeRune(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}

func unsafeRune(r rune) bool {
	if unicode.IsControl(r) { // C0, DEL and C1
		return true
	}
	switch {
	case r == 0x061c, r == 0x200e, r == 0x200f, // direction marks
		r >= 0x202a && r <= 0x202e, // embeddings and overrides
		r >= 0x2066 && r <= 0x2069, // isolates
		r == 0x2028, r == 0x2029:   // line and paragraph separators
		return true
	}
	return false
}
