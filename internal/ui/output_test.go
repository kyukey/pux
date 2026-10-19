// internal/ui/output_test.go

package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestBlockKeepsTextColumn(t *testing.T) {
	for _, opts := range []Options{{}, {Color: true}, {ASCII: true}} {
		r := NewRenderer(nil, opts)
		lines := r.Block(Good, "saved", "deploy", "run it")
		for i, l := range lines {
			text := []string{"deploy", "run it"}[i]
			if col := Width(l) - Width(text); col != TextColumn {
				t.Errorf("opts %+v: line %d starts text at column %d, want %d", opts, i, col, TextColumn)
			}
		}
	}
}

func TestColorOff(t *testing.T) {
	var buf bytes.Buffer
	r := NewRenderer(&buf, Options{})
	r.Error("No key named x", "run pux ls")
	r.Print([]string{r.Header("pux", "v1", "tagline"), r.Status(Bad, "failed"), r.Hints("esc cancel")})
	if strings.Contains(buf.String(), "\x1b") {
		t.Errorf("output without colour holds escape codes: %q", buf.String())
	}
}

func TestPrintSpacing(t *testing.T) {
	for _, tty := range []bool{false, true} {
		var buf bytes.Buffer
		NewRenderer(&buf, Options{TTY: tty}).Print([]string{"x"})
		if want := map[bool]string{false: "x\n", true: "\nx\n\n"}[tty]; buf.String() != want {
			t.Errorf("Print with TTY %v = %q, want %q", tty, buf.String(), want)
		}
	}
}

func TestSymbols(t *testing.T) {
	for _, opts := range []Options{{}, {ASCII: true}} {
		s := NewRenderer(nil, opts).Symbols()
		for _, mark := range []string{s.Focus, s.Idle, s.On, s.Off, s.Ok, s.Fail, s.Warn} {
			if Width(mark) != 1 {
				t.Errorf("symbol %q is %d columns wide, want 1 so layouts line up", mark, Width(mark))
			}
		}
	}
}
