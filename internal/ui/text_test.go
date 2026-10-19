// internal/ui/text_test.go

package ui

import (
	"slices"
	"testing"
)

func TestWidth(t *testing.T) {
	tests := map[string]int{
		"":                              0,
		"abc":                           3,
		"世界":                            4,
		"🚀":                             2,
		"é":                             1,
		"a‍b":                           2,
		"◆ ✓":                           3,
		"\x1b[30;48;5;38m keys \x1b[0m": 6,
		"\x1b[2m世\x1b[0m":               2,
	}
	for s, want := range tests {
		if got := Width(s); got != want {
			t.Errorf("Width(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestClean(t *testing.T) {
	tests := map[string]string{
		"plain":       "plain",
		"a\tb":        "a   b",
		"世\tx":        "世  x",
		"\x1b[31mred": "^[[31mred",
		"a\x7fb":      "a^?b",
		"line\r":      "line^M",
		"\u0085":      "?",
		"\xff":        "?",
	}
	for s, want := range tests {
		if got := Clean(s); got != want {
			t.Errorf("Clean(%q) = %q, want %q", s, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s        string
		width    int
		ellipsis string
		want     string
	}{
		{"hello", 5, "…", "hello"},
		{"hello world", 8, "…", "hello w…"},
		{"abcdef", 4, "...", "a..."},
		{"世界世界", 5, "…", "世界…"},
		{"abc", 2, "", "ab"},
		{"abc", 0, "…", ""},
		{"\x1b[1mhello\x1b[0m", 5, "…", "\x1b[1mhello\x1b[0m"},
		{"ab\x1b[2mcdefgh\x1b[0m", 5, "…", "ab\x1b[2mcd…\x1b[0m"},
	}
	for _, tt := range tests {
		if got := Truncate(tt.s, tt.width, tt.ellipsis); got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
		}
	}
}

func TestPad(t *testing.T) {
	if got := Pad("世", 3); got != "世 " {
		t.Errorf("Pad = %q, want one space after a wide rune", got)
	}
	if got := Pad("abcd", 2); got != "abcd" {
		t.Errorf("Pad = %q, want long text left as it is", got)
	}
}

func TestLines(t *testing.T) {
	tests := []struct {
		value string
		lines []string
		first string
	}{
		{"", nil, ""},
		{"a\r\nb\n", []string{"a", "b"}, "a"},
		{"\n  \n  cd app  \ngit pull", []string{"", "  ", "  cd app  ", "git pull"}, "cd app"},
	}
	for _, tt := range tests {
		if got := Lines([]byte(tt.value)); !slices.Equal(got, tt.lines) {
			t.Errorf("Lines(%q) = %q, want %q", tt.value, got, tt.lines)
		}
		if got := FirstLine([]byte(tt.value)); got != tt.first {
			t.Errorf("FirstLine(%q) = %q, want %q", tt.value, got, tt.first)
		}
	}
}
