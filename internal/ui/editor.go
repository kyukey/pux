// internal/ui/editor.go

package ui

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/kyukey/pux/internal/store"
)

// maxAreaRows is how tall the text area grows before it scrolls.
const maxAreaRows = 12

// Command is what the editor opens with. Title is the badge on the key row, like
// "new" or "edit", and Key may be empty for a new command. KeyFixed locks the key
// when editing, and Check says why a key cannot be used, or "" when it can.
type Command struct {
	Title    string
	Key      string
	Value    string
	KeyFixed bool
	Check    func(key string) string
}

// Edit opens the editor under the cursor and returns the key and text once the
// user saves with Ctrl+D. It returns ErrCancelled when they leave instead.
//
//	    new   ◇ deploy
//
//	command   ◆  1  cd app
//	              2  git pull
func Edit(opts Options, c Command) (key, value string, err error) {
	e := newEditor(c)
	if err = run(e, opts); err != nil {
		return "", "", err
	}
	return e.key.Value(), e.area.Value(), nil
}

// editor is the key field above the text area. armed is set when Esc was
// pressed once with unsaved changes, so a second Esc leaves.
type editor struct {
	cmd      Command
	key      keyField
	area     *textArea
	keyFocus bool
	dirty    bool
	armed    bool
	msg      message
}

// newEditor puts focus on the key when it still needs one, otherwise on the text.
func newEditor(c Command) *editor {
	e := &editor{cmd: c, area: newTextArea(c.Value)}
	e.key.set(c.Key)
	e.keyFocus = !c.KeyFixed && (store.Validate(c.Key) != nil || e.check(c.Key) != "")
	return e
}

func (e *editor) update(k Key) outcome {
	if k.Type != KeyEsc {
		e.armed = false
	}
	e.msg = message{}
	switch {
	case k.IsCtrl('d'):
		return e.submit()
	case k.Type == KeyEsc:
		return e.leave()
	case e.keyFocus && (k.Type == KeyTab || k.Type == KeyEnter || k.Type == KeyDown):
		e.keyFocus = false
	case !e.keyFocus && !e.cmd.KeyFixed && k.Type == KeyBacktab:
		e.keyFocus = true
	case e.keyFocus:
		e.dirty = e.key.handle(k) || e.dirty
	default:
		e.dirty = e.area.handle(k) || e.dirty
	}
	return pending
}

// leave cancels, asking for a second Esc first when there are unsaved changes.
func (e *editor) leave() outcome {
	if e.dirty && !e.armed {
		e.armed = true
		e.msg = message{"unsaved changes, press esc again to discard them", Label}
		return pending
	}
	return cancelled
}

// submit accepts only a valid, usable key with some text, and otherwise moves
// focus to whatever needs fixing.
func (e *editor) submit() outcome {
	if note := e.keyProblem(); note != "" {
		e.keyFocus = true
		e.msg = message{note, Bad}
		return pending
	}
	if strings.TrimSpace(e.area.Value()) == "" {
		e.keyFocus = false
		e.msg = message{"nothing to save yet, type a command first", Bad}
		return pending
	}
	return accepted
}

// keyProblem says why the key cannot be saved, or "" when it can.
func (e *editor) keyProblem() string {
	var ke *store.KeyError
	if errors.As(store.Validate(e.key.Value()), &ke) {
		return "key " + ke.Reason
	}
	return e.check(e.key.Value())
}

func (e *editor) check(key string) string {
	if e.cmd.KeyFixed || e.cmd.Check == nil {
		return ""
	}
	return e.cmd.Check(key)
}

// keyNote explains a key problem while it is typed, before the user tries to save.
func (e *editor) keyNote() string {
	switch {
	case e.cmd.KeyFixed:
		return ""
	case e.key.rejected:
		return "keys only use a-z 0-9 . _ -"
	case e.key.Value() == "":
		return ""
	}
	return e.keyProblem()
}

// view draws the key row and the text area. Only the first text line carries
// the focus mark, so the line numbers stay lined up.
func (e *editor) view(r *Renderer, height int) frame {
	lines := []string{""}

	keyRow := len(lines)
	keyText := e.key.Value()
	if keyText == "" && e.keyFocus {
		keyText = r.Dim("name it, like deploy")
	}
	if !e.cmd.KeyFixed {
		keyText = r.Mark(e.keyFocus) + " " + keyText
	}
	lines = append(lines, r.Block(Label, e.cmd.Title, keyText)...)
	if note := e.keyNote(); note != "" {
		lines = append(lines, r.Indent(r.Status(Label, r.Dim(note))))
	}
	lines = append(lines, "")

	areaRow := len(lines)
	rows := min(max(len(e.area.lines), 1), maxAreaRows, max(height-len(lines)-6, 1))
	text, curRow, curCol := e.area.view(r, r.TextWidth()-2, rows)
	for i := range text {
		prefix := "  "
		if i == 0 {
			prefix = r.Mark(!e.keyFocus) + " "
		}
		text[i] = prefix + text[i]
	}
	lines = append(lines, r.Block(Label, "command", text...)...)
	lines = append(lines, footer(r, e.msg, e.hints()...)...)

	f := frame{lines: lines, cursor: true}
	if e.keyFocus {
		f.row, f.col = keyRow, TextColumn+2+e.key.col
	} else {
		f.row, f.col = areaRow+curRow, TextColumn+2+curCol
	}
	return f
}

func (e *editor) hints() []string {
	switch {
	case e.keyFocus:
		return []string{"tab to command", "ctrl+d save", "esc cancel"}
	case e.cmd.KeyFixed:
		return []string{"ctrl+d save", "esc cancel", argsHint()}
	}
	return []string{"ctrl+d save", "shift+tab to key", "esc cancel"}
}

func argsHint() string {
	if runtime.GOOS == "windows" {
		return "%~1 for args"
	}
	return "$1 for args"
}

// keyField is a one-line input for a key. Each typed character goes through
// store.KeyByte, so "D" becomes "d" and a space becomes "-" as you type. Keys are
// ASCII, so one byte is one column. rejected is set when the last character
// could not be part of a key.
type keyField struct {
	text     []byte
	col      int
	rejected bool
}

func (f *keyField) Value() string { return string(f.text) }

func (f *keyField) set(s string) {
	f.text = []byte(s)
	f.col = len(f.text)
}

// handle applies a key and reports whether the text changed. A paste stops at
// its first line break, since a key is one line.
func (f *keyField) handle(k Key) (changed bool) {
	f.rejected = false
	switch {
	case k.Type == KeyRune:
		return f.add(k.Rune)
	case k.Type == KeyPaste:
		line, _, _ := strings.Cut(normalizeNewlines(k.Text), "\n")
		for _, r := range line {
			changed = f.add(r) || changed
		}
		return changed
	case k.Type == KeyBackspace && f.col > 0:
		f.text = append(f.text[:f.col-1], f.text[f.col:]...)
		f.col--
		return true
	case k.Type == KeyDelete && f.col < len(f.text):
		f.text = append(f.text[:f.col], f.text[f.col+1:]...)
		return true
	case k.Type == KeyLeft && f.col > 0:
		f.col--
	case k.Type == KeyRight && f.col < len(f.text):
		f.col++
	case k.Type == KeyHome, k.IsCtrl('a'):
		f.col = 0
	case k.Type == KeyEnd, k.IsCtrl('e'):
		f.col = len(f.text)
	}
	return false
}

func (f *keyField) add(r rune) bool {
	c, ok := store.KeyByte(r)
	if !ok || len(f.text) >= store.MaxKeyLen {
		f.rejected = true
		return false
	}
	f.text = append(f.text[:f.col], append([]byte{c}, f.text[f.col:]...)...)
	f.col++
	return true
}

// textArea is a multi-line editor. Lines hold runes, so the cursor moves by
// character and never lands inside a multi-byte one. want is the column to aim
// for when moving up or down past shorter lines, and top is the first visible line.
type textArea struct {
	lines [][]rune
	row   int
	col   int
	want  int
	top   int
}

func newTextArea(text string) *textArea {
	t := &textArea{}
	for l := range strings.SplitSeq(normalizeNewlines(text), "\n") {
		t.lines = append(t.lines, []rune(l))
	}
	t.row = len(t.lines) - 1
	t.col = len(t.lines[t.row])
	t.want = t.col
	return t
}

// normalizeNewlines turns CRLF and lone CR into LF. Terminals paste with CR.
func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func (t *textArea) Value() string {
	parts := make([]string, len(t.lines))
	for i, l := range t.lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

// handle applies a key and reports whether the text changed.
func (t *textArea) handle(k Key) (changed bool) {
	switch k.Type {
	case KeyRune:
		t.insert(string(k.Rune))
	case KeyPaste:
		t.insert(normalizeNewlines(k.Text))
	case KeyTab:
		t.insert("\t")
	case KeyEnter:
		t.insert("\n")
	case KeyBackspace:
		return t.backspace()
	case KeyDelete:
		return t.deleteForward()
	default:
		t.move(k)
		return false
	}
	return true
}

func (t *textArea) insert(s string) {
	line := t.lines[t.row]
	before, after := string(line[:t.col]), string(line[t.col:])
	parts := strings.Split(before+s+after, "\n")
	lastLen := len([]rune(parts[len(parts)-1])) - len([]rune(after))

	rows := make([][]rune, len(parts))
	for i, p := range parts {
		rows[i] = []rune(p)
	}
	t.lines = append(t.lines[:t.row], append(rows, t.lines[t.row+1:]...)...)
	t.row += len(parts) - 1
	t.col = lastLen
	t.want = t.col
}

func (t *textArea) backspace() bool {
	switch {
	case t.col > 0:
		line := t.lines[t.row]
		t.lines[t.row] = append(line[:t.col-1:t.col-1], line[t.col:]...)
		t.col--
	case t.row > 0:
		prev := t.lines[t.row-1]
		t.col = len(prev)
		t.lines[t.row-1] = append(prev[:len(prev):len(prev)], t.lines[t.row]...)
		t.lines = append(t.lines[:t.row], t.lines[t.row+1:]...)
		t.row--
	default:
		return false
	}
	t.want = t.col
	return true
}

func (t *textArea) deleteForward() bool {
	line := t.lines[t.row]
	switch {
	case t.col < len(line):
		t.lines[t.row] = append(line[:t.col:t.col], line[t.col+1:]...)
	case t.row < len(t.lines)-1:
		t.lines[t.row] = append(line[:len(line):len(line)], t.lines[t.row+1]...)
		t.lines = append(t.lines[:t.row+1], t.lines[t.row+2:]...)
	default:
		return false
	}
	return true
}

func (t *textArea) move(k Key) {
	switch {
	case k.Type == KeyLeft && t.col > 0:
		t.col--
	case k.Type == KeyLeft && t.row > 0:
		t.row--
		t.col = len(t.lines[t.row])
	case k.Type == KeyRight && t.col < len(t.lines[t.row]):
		t.col++
	case k.Type == KeyRight && t.row < len(t.lines)-1:
		t.row++
		t.col = 0
	case k.Type == KeyHome, k.IsCtrl('a'):
		t.col = 0
	case k.Type == KeyEnd, k.IsCtrl('e'):
		t.col = len(t.lines[t.row])
	case k.Type == KeyUp:
		t.moveRows(-1)
		return
	case k.Type == KeyDown:
		t.moveRows(1)
		return
	case k.Type == KeyPageUp:
		t.moveRows(-pageSize)
		return
	case k.Type == KeyPageDown:
		t.moveRows(pageSize)
		return
	default:
		return
	}
	t.want = t.col
}

// moveRows goes up or down, keeping the column the user was on where it can.
func (t *textArea) moveRows(n int) {
	t.row = min(max(t.row+n, 0), len(t.lines)-1)
	t.col = min(t.want, len(t.lines[t.row]))
}

// view returns the visible lines with line numbers, at most height of them, each
// fitting in width. It also returns the cursor position inside those lines.
func (t *textArea) view(r *Renderer, width, height int) (lines []string, curRow, curCol int) {
	height = max(height, 1)
	if t.row < t.top {
		t.top = t.row
	}
	if t.row >= t.top+height {
		t.top = t.row - height + 1
	}
	t.top = min(t.top, max(len(t.lines)-height, 0))

	numWidth := max(2, len(strconv.Itoa(len(t.lines))))
	room := max(width-numWidth-2, 4)
	end := min(t.top+height, len(t.lines))
	for i := t.top; i < end; i++ {
		num := r.Dim(fmt.Sprintf("%*d", numWidth, i+1)) + "  "
		if i != t.row {
			lines = append(lines, num+Truncate(Clean(string(t.lines[i])), room, r.Symbols().Ellipsis))
			continue
		}
		text, col := scrollToCursor(t.lines[i], t.col, room)
		lines = append(lines, num+text)
		curRow, curCol = i-t.top, numWidth+2+col
	}
	return lines, curRow, curCol
}

// scrollToCursor shows the part of a long line around the cursor, so the cursor
// stays visible while typing past the right edge. It cleans the line once, with
// tab stops counted from the line start like every other line, and returns the
// visible text and the cursor column within it.
func scrollToCursor(line []rune, col, room int) (string, int) {
	var b strings.Builder
	cols := make([]int, len(line)+1)
	offs := make([]int, len(line)+1)
	for i, r := range line {
		text, w := cleanRune(r, cols[i])
		b.WriteString(text)
		cols[i+1], offs[i+1] = cols[i]+w, b.Len()
	}
	start := 0
	for cols[col]-cols[start] > room-1 {
		start++
	}
	return Truncate(b.String()[offs[start]:], room, ""), cols[col] - cols[start]
}
