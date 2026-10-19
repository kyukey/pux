// internal/ui/prompt.go

package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// ErrCancelled is returned when the user leaves a prompt with Esc or Ctrl+C.
var ErrCancelled = errors.New("cancelled")

// pageSize is how far Page Up and Page Down jump, in lists and in the editor.
const pageSize = 10

// newlineRaw ends a line in raw mode, where a plain \n does not return to column 0.
const newlineRaw = "\r\n"

type outcome int

const (
	pending outcome = iota
	accepted
	cancelled
)

// model is one prompt. update applies a key and view draws the prompt. Neither
// touches the terminal, so tests can drive them with plain keys.
type model interface {
	update(k Key) outcome
	view(r *Renderer, height int) frame
}

// message is one line of feedback under the hints, like an error on save.
type message struct {
	text string
	tone Tone
}

// frame is one full drawing of a prompt, with the row and column the cursor
// should sit at, and whether it shows at all.
type frame struct {
	lines    []string
	row, col int
	cursor   bool
}

// run shows m under the cursor until it is accepted or cancelled, then clears it.
// The terminal is always put back as it was, even when something fails.
func run(m model, opts Options) error {
	t, err := Open(os.Stdin, os.Stdout)
	if err != nil {
		return err
	}
	defer t.Close()
	scr := &screen{w: t}
	defer scr.clear()

	for {
		width, height := t.Size()
		o := opts
		o.Width = width
		r := NewRenderer(io.Discard, o)
		scr.draw(fit(m.view(r, height), r, width, height))

		k, err := t.ReadKey()
		if err != nil {
			return err
		}
		switch step(m, k) {
		case accepted:
			return nil
		case cancelled:
			return ErrCancelled
		}
	}
}

// step applies a key, with Ctrl+C leaving every prompt the same way.
func step(m model, k Key) outcome {
	if k.IsCtrl('c') {
		return cancelled
	}
	return m.update(k)
}

// fit keeps a frame inside the terminal. A line wider than the terminal would wrap
// and break the redraw, so lines are cut, and one column and one row stay free.
func fit(f frame, r *Renderer, width, height int) frame {
	for i, line := range f.lines {
		f.lines[i] = Truncate(line, width-1, r.Symbols().Ellipsis)
	}
	return fitHeight(f, height-1)
}

// fitHeight shows the part of a tall frame around the cursor.
func fitHeight(f frame, height int) frame {
	if height <= 0 || len(f.lines) <= height {
		return f
	}
	start := min(max(f.row-height+1, 0), len(f.lines)-height)
	f.lines = f.lines[start : start+height]
	f.row -= start
	return f
}

// footer is the hints line and any message, with a blank line above and below.
func footer(r *Renderer, msg message, hints ...string) []string {
	lines := []string{"", r.Indent(r.Hints(hints...))}
	if msg.text != "" {
		lines = append(lines, r.Indent(r.Status(msg.tone, msg.text)))
	}
	return append(lines, "")
}

// screen draws frames in place under the shell prompt instead of taking over the
// whole terminal. Each draw replaces the one before it, and cursor is the row the
// cursor was left on, counted from the top of the last frame.
type screen struct {
	w      io.Writer
	cursor int
}

func (s *screen) draw(f frame) {
	var b strings.Builder
	b.WriteString(hideCursor)
	s.toTop(&b)
	b.WriteString(clearToEnd)
	b.WriteString(strings.Join(f.lines, newlineRaw))
	if up := len(f.lines) - 1 - f.row; up > 0 {
		fmt.Fprintf(&b, cursorUp, up)
	}
	b.WriteString("\r")
	if f.col > 0 {
		fmt.Fprintf(&b, cursorRight, f.col)
	}
	if f.cursor {
		b.WriteString(showCursor)
	}
	s.cursor = f.row
	io.WriteString(s.w, b.String())
}

// clear erases the last frame and leaves the cursor where the frame started.
func (s *screen) clear() {
	var b strings.Builder
	s.toTop(&b)
	b.WriteString(clearToEnd + showCursor)
	s.cursor = 0
	io.WriteString(s.w, b.String())
}

func (s *screen) toTop(b *strings.Builder) {
	if s.cursor > 0 {
		fmt.Fprintf(b, cursorUp, s.cursor)
	}
}

// Select asks the user to pick one of items and returns its index. Action is
// what Enter does, like "run", and shows in the hints.
//
//	pick   de matches 3 keys
//	       ● deploy
//	       ○ deploy-prod
func Select(opts Options, title string, items []string, action string) (int, error) {
	s := &chooser{label: "pick", title: title, items: items, action: action}
	if err := run(s, opts); err != nil {
		return 0, err
	}
	return s.sel, nil
}

// Search shows the items search returns for the typed text, starting from
// query, and returns the one picked. The list narrows as the user types, and
// typed text is reshaped like a key, so "Prod" becomes prod.
//
//	find   ◆ prod
//	       ● deploy-prod
//	       ○ prod-db
func Search(opts Options, query string, search func(text string) []string, action string) (string, error) {
	s := newSearch(query, search, action)
	if err := run(s, opts); err != nil {
		return "", err
	}
	return s.items[s.sel], nil
}

func newSearch(query string, search func(text string) []string, action string) *chooser {
	s := &chooser{label: "find", action: action, search: search}
	for _, r := range query {
		s.query.add(r)
	}
	s.query.rejected = false
	s.items = search(s.query.Value())
	return s
}

// chooser is a list to pick from. With search set it is a search, and query
// is the text that narrows the list.
type chooser struct {
	label  string
	title  string
	items  []string
	action string
	sel    int
	top    int
	search func(text string) []string
	query  keyField
}

func (s *chooser) update(k Key) outcome {
	if s.search != nil && typing(k) {
		if s.query.handle(k) {
			s.items, s.sel, s.top = s.search(s.query.Value()), 0, 0
		}
		return pending
	}
	last := max(len(s.items)-1, 0)
	switch k.Type {
	case KeyUp:
		s.sel = max(s.sel-1, 0)
	case KeyDown:
		s.sel = min(s.sel+1, last)
	case KeyPageUp:
		s.sel = max(s.sel-pageSize, 0)
	case KeyPageDown:
		s.sel = min(s.sel+pageSize, last)
	case KeyHome:
		s.sel = 0
	case KeyEnd:
		s.sel = last
	case KeyEnter:
		if len(s.items) == 0 {
			return pending
		}
		return accepted
	case KeyEsc:
		return cancelled
	}
	return pending
}

// typing reports whether k edits the search text rather than moving in the list.
func typing(k Key) bool {
	switch k.Type {
	case KeyRune, KeyPaste, KeyBackspace, KeyDelete, KeyLeft, KeyRight:
		return true
	}
	return k.IsCtrl('a') || k.IsCtrl('e')
}

func (s *chooser) view(r *Renderer, height int) frame {
	visible := max(min(len(s.items), height-8), 3)
	if s.sel < s.top {
		s.top = s.sel
	}
	if s.sel >= s.top+visible {
		s.top = s.sel - visible + 1
	}
	end := min(s.top+visible, len(s.items))

	rows := []string{s.head(r)}
	for i := s.top; i < end; i++ {
		rows = append(rows, r.Option(i == s.sel, s.items[i]))
	}
	if rest := len(s.items) - end; rest > 0 {
		rows = append(rows, r.Dim(r.Symbols().Ellipsis+" "+strconv.Itoa(rest)+" more"))
	}
	if len(s.items) == 0 {
		rows = append(rows, r.Dim("no keys match"))
	}
	var msg message
	if s.query.rejected {
		msg = message{"keys only use a-z 0-9 . _ -", Label}
	}
	lines := append([]string{""}, r.Block(Label, s.label, rows...)...)
	lines = append(lines, footer(r, msg, r.Symbols().UpDown+" choose", "enter "+s.action, "esc cancel")...)
	if s.search != nil {
		return frame{lines: lines, row: 1, col: TextColumn + 2 + s.query.col, cursor: true}
	}
	return frame{lines: lines, row: 2 + s.sel - s.top}
}

// head is the first row: the title, or for a search the text typed so far.
func (s *chooser) head(r *Renderer) string {
	if s.search == nil {
		return s.title
	}
	text := s.query.Value()
	if text == "" {
		text = r.Dim("type to search")
	}
	return r.Mark(true) + " " + text
}

// Confirm asks a yes or no question beside a red badge, for things that cannot be
// undone. No is the default, so Enter alone is always safe.
//
//	delete   Delete deploy?  ○ yes  ● no
func Confirm(opts Options, label, question string) (bool, error) {
	c := &confirm{label: label, question: question}
	if err := run(c, opts); err != nil {
		return false, err
	}
	return c.yes, nil
}

type confirm struct {
	label    string
	question string
	yes      bool
}

func (c *confirm) update(k Key) outcome {
	switch {
	case k.Type == KeyLeft, k.Type == KeyRight, k.Type == KeyTab:
		c.yes = !c.yes
	case k.Type == KeyRune && (k.Rune == 'y' || k.Rune == 'Y'):
		c.yes = true
		return accepted
	case k.Type == KeyRune && (k.Rune == 'n' || k.Rune == 'N'):
		c.yes = false
		return accepted
	case k.Type == KeyEnter:
		return accepted
	case k.Type == KeyEsc:
		return cancelled
	}
	return pending
}

func (c *confirm) view(r *Renderer, height int) frame {
	question := c.question + "  " + r.Option(c.yes, "yes") + "  " + r.Option(!c.yes, "no")
	lines := append([]string{""}, r.Block(Bad, c.label, question)...)
	lines = append(lines, footer(r, message{}, r.Symbols().LeftRight+" choose", "enter confirm", "esc cancel")...)
	return frame{lines: lines, row: 1}
}
