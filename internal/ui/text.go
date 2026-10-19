// internal/ui/text.go

package ui

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

const tabWidth = 4

// Width returns how many terminal columns s takes. Colour codes take none.
// It covers the common wide ranges (CJK, emoji) without a lookup table.
func Width(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if end := escapeEnd(s, i); end > i {
			i = end
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		n += runeWidth(r)
		i += size
	}
	return n
}

func runeWidth(r rune) int {
	switch {
	case r == 0x200B, r == 0x200D, r == 0xFE0F, r >= 0x0300 && r <= 0x036F:
		return 0
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE4F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1FAFF,
		r >= 0x20000 && r <= 0x3FFFD:
		return 2
	}
	return 1
}

// Clean makes saved text safe to print. Tabs become spaces and control characters
// show as ^X, so a stored escape sequence can never drive the terminal.
func Clean(s string) string {
	var b strings.Builder
	col := 0
	for _, r := range s {
		text, w := cleanRune(r, col)
		b.WriteString(text)
		col += w
	}
	return b.String()
}

// cleanRune returns how r prints when it starts at column col, and its width.
func cleanRune(r rune, col int) (string, int) {
	switch {
	case r == '\t':
		pad := tabWidth - col%tabWidth
		return "    "[:pad], pad
	case r < 0x20 || r == 0x7F:
		return string([]byte{'^', byte(r) ^ 0x40}), 2
	case r >= 0x80 && r <= 0x9F, r == utf8.RuneError:
		return "?", 1
	}
	return string(r), runeWidth(r)
}

// Truncate cuts s to fit in width columns, ending with ellipsis when it was cut.
// Colour codes are kept but take no room, and a styled line that was cut gets a
// reset after the ellipsis, so colour never runs past it.
func Truncate(s string, width int, ellipsis string) string {
	if Width(s) <= width {
		return s
	}
	room := width - Width(ellipsis)
	if room < 0 {
		return ""
	}
	var b strings.Builder
	used, styled := 0, false
	for i := 0; i < len(s); {
		if end := escapeEnd(s, i); end > i {
			b.WriteString(s[i:end])
			styled = true
			i = end
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if used+runeWidth(r) > room {
			break
		}
		b.WriteRune(r)
		used += runeWidth(r)
		i += size
	}
	b.WriteString(ellipsis)
	if styled {
		b.WriteString(codeReset)
	}
	return b.String()
}

// Pad fills s with spaces on the right up to n columns.
func Pad(s string, n int) string {
	if w := Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// Lines splits value into lines, dropping the final newline and any \r endings.
func Lines(value []byte) []string {
	if len(value) == 0 {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(string(value), "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// FirstLine returns the first line with any text in it, trimmed.
func FirstLine(value []byte) string {
	for _, l := range Lines(value) {
		if s := strings.TrimSpace(l); s != "" {
			return s
		}
	}
	return ""
}

// Plural returns a count with its word, like "1 key" or "12 keys".
func Plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// escapeEnd returns where a colour code starting at i ends, or i when there is none.
func escapeEnd(s string, i int) int {
	if !strings.HasPrefix(s[i:], "\x1b[") {
		return i
	}
	for j := i + 2; j < len(s); j++ {
		if c := s[j]; c >= 0x40 && c <= 0x7e {
			return j + 1
		}
	}
	return len(s)
}
