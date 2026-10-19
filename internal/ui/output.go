// internal/ui/output.go

package ui

import (
	"fmt"
	"io"
	"strings"
)

// Every escape code pux writes. Brand and badge colours use the 256 colour
// palette, so they look the same in every terminal theme.
const (
	codeReset   = "\x1b[0m"
	codeBold    = "\x1b[1m"
	codeDim     = "\x1b[2m"
	codeRed     = "\x1b[31m"
	codeGreen   = "\x1b[32m"
	codeYellow  = "\x1b[33m"
	codeBrand   = "\x1b[38;5;141m"
	codeOnLabel = "\x1b[30;48;5;38m"
	codeOnGood  = "\x1b[30;48;5;114m"
	codeOnBad   = "\x1b[30;48;5;203m"
	codeOnBrand = "\x1b[30;48;5;141m"

	hideCursor  = "\x1b[?25l"
	showCursor  = "\x1b[?25h"
	clearToEnd  = "\r\x1b[J"
	cursorUp    = "\x1b[%dA"
	cursorRight = "\x1b[%dC"
	pasteOn     = "\x1b[?2004h"
	pasteOff    = "\x1b[?2004l"
)

// The gutter puts a badge in a right-aligned column and all text in one column:
//
//	saved   notes  3 lines
//	        run it with pux notes
const (
	gutterWidth = 10
	TextColumn  = gutterWidth + 2
)

// Symbols are the marks used in every layout. The ASCII set is for consoles
// that cannot draw the unicode one.
type Symbols struct {
	Focus, Idle       string
	On, Off           string
	Ok, Fail, Warn    string
	Ellipsis, Dot     string
	UpDown, LeftRight string
}

var unicodeSymbols = Symbols{
	Focus: "◆", Idle: "◇",
	On: "●", Off: "○",
	Ok: "✓", Fail: "✗", Warn: "▲",
	Ellipsis: "…", Dot: "·",
	UpDown: "↑↓", LeftRight: "←→",
}

var asciiSymbols = Symbols{
	Focus: "*", Idle: "o",
	On: ">", Off: "-",
	Ok: "+", Fail: "x", Warn: "!",
	Ellipsis: "...", Dot: "-",
	UpDown: "up/down", LeftRight: "left/right",
}

// Tone picks a badge colour: Label for sections and neutral messages, Good and
// Bad for results, and Brand for the tool's own name.
type Tone int

const (
	Label Tone = iota
	Good
	Bad
	Brand
)

var toneCodes = [...]string{
	Label: codeOnLabel,
	Good:  codeOnGood,
	Bad:   codeOnBad,
	Brand: codeOnBrand,
}

// Renderer writes output in one consistent style. Messages print directly,
// and layout helpers return strings for the caller to place.
type Renderer struct {
	w    io.Writer
	opts Options
	sym  Symbols
}

// NewRenderer returns a Renderer that writes to w as opts describes.
func NewRenderer(w io.Writer, opts Options) *Renderer {
	if opts.Width <= 0 {
		opts.Width = defaultWidth
	}
	sym := unicodeSymbols
	if opts.ASCII {
		sym = asciiSymbols
	}
	return &Renderer{w: w, opts: opts, sym: sym}
}

func (r *Renderer) Options() Options { return r.opts }

func (r *Renderer) Symbols() Symbols { return r.sym }

func (r *Renderer) paint(code, s string) string {
	if !r.opts.Color || s == "" {
		return s
	}
	return code + s + codeReset
}

func (r *Renderer) Bold(s string) string   { return r.paint(codeBold, s) }
func (r *Renderer) Dim(s string) string    { return r.paint(codeDim, s) }
func (r *Renderer) Red(s string) string    { return r.paint(codeRed, s) }
func (r *Renderer) Green(s string) string  { return r.paint(codeGreen, s) }
func (r *Renderer) Yellow(s string) string { return r.paint(codeYellow, s) }
func (r *Renderer) Brand(s string) string  { return r.paint(codeBrand, s) }

// Mark is the focus diamond: filled on the field being typed in, hollow on the others.
func (r *Renderer) Mark(focused bool) string {
	if focused {
		return r.Brand(r.sym.Focus)
	}
	return r.Dim(r.sym.Idle)
}

// Option returns a choice like "● yes" or "○ no".
func (r *Renderer) Option(selected bool, text string) string {
	if selected {
		return r.Brand(r.sym.On) + " " + text
	}
	return r.Dim(r.sym.Off) + " " + r.Dim(text)
}

// Status returns text after the mark for its tone, like "✓ saved".
func (r *Renderer) Status(t Tone, text string) string {
	mark := r.Yellow(r.sym.Warn)
	switch t {
	case Good:
		mark = r.Green(r.sym.Ok)
	case Bad:
		mark = r.Red(r.sym.Fail)
	}
	return mark + " " + text
}

// Hints lays out controls like "ctrl+d save · esc cancel". The first word of each
// item is the key and stays bright, and the rest says what it does, dimmed.
func (r *Renderer) Hints(items ...string) string {
	parts := make([]string, len(items))
	for i, item := range items {
		key, action, _ := strings.Cut(item, " ")
		parts[i] = key
		if action != "" {
			parts[i] += " " + r.Dim(action)
		}
	}
	return strings.Join(parts, r.Dim(" "+r.sym.Dot+" "))
}

// Badge returns label as a coloured block, right-aligned in the gutter.
// Without colour the label keeps its place, so the layout holds.
func (r *Renderer) Badge(t Tone, label string) string {
	block := " " + label + " "
	pad := strings.Repeat(" ", max(gutterWidth-Width(block), 0))
	return pad + r.paint(toneCodes[t], block)
}

// Block lays lines out beside a badge: the first line next to it, the rest under that.
func (r *Renderer) Block(t Tone, label string, lines ...string) []string {
	if len(lines) == 0 {
		return []string{r.Badge(t, label)}
	}
	out := make([]string, len(lines))
	out[0] = r.Badge(t, label) + "  " + lines[0]
	for i, l := range lines[1:] {
		out[i+1] = r.Indent(l)
	}
	return out
}

// Indent returns text placed in the text column with no badge.
func (r *Renderer) Indent(text string) string {
	if text == "" {
		return ""
	}
	return strings.Repeat(" ", TextColumn) + text
}

// TextWidth is the room left in the text column.
func (r *Renderer) TextWidth() int { return max(r.opts.Width-TextColumn, 10) }

// Header returns the title line: the brand badge, the version and a bold tagline.
func (r *Renderer) Header(name, version, tagline string) string {
	return r.Badge(Brand, name) + "  " + r.Brand(version) + "  " + r.Bold(tagline)
}

// Mascot puts art in the gutter beside text. The first text line is the speaker.
func (r *Renderer) Mascot(art, text []string) []string {
	artWidth := 0
	for _, a := range art {
		artWidth = max(artWidth, Width(a))
	}
	pad := strings.Repeat(" ", max(gutterWidth-artWidth, 0))
	out := make([]string, max(len(art), len(text)))
	for i := range out {
		left := strings.Repeat(" ", gutterWidth)
		if i < len(art) {
			left = pad + r.Brand(Pad(art[i], artWidth))
		}
		right := ""
		switch {
		case i == 0 && len(text) > 0:
			right = r.Brand(r.Bold(text[0]))
		case i < len(text):
			right = text[i]
		}
		out[i] = strings.TrimRight(left+"  "+right, " ")
	}
	return out
}

// Print writes lines. On a terminal the block gets a blank line above and below.
func (r *Renderer) Print(lines []string) {
	if r.opts.TTY {
		fmt.Fprintln(r.w)
	}
	for _, l := range lines {
		fmt.Fprintln(r.w, l)
	}
	if r.opts.TTY {
		fmt.Fprintln(r.w)
	}
}

// Done reports something that worked, like "saved notes".
func (r *Renderer) Done(label, msg, hint string) { r.message(Good, label, msg, hint) }

// Error reports what went wrong, with an optional hint on what to do next.
func (r *Renderer) Error(msg, hint string) { r.message(Bad, "error", msg, hint) }

// Info reports something neutral, like an empty list.
func (r *Renderer) Info(label, msg, hint string) { r.message(Label, label, msg, hint) }

func (r *Renderer) message(t Tone, label, msg, hint string) {
	lines := []string{msg}
	if hint != "" {
		lines = append(lines, r.Dim(hint))
	}
	r.Print(r.Block(t, label, lines...))
}
