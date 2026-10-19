// internal/ui/prompt_test.go

package ui

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/kyukey/pux/internal/store"
)

// keys decodes input the way a terminal sends it. A trailing Esc byte becomes
// a lone Esc, as it does after the wait.
func keys(input string) []Key {
	var d Decoder
	return append(d.Feed([]byte(input)), d.Flush()...)
}

// press feeds input to m and returns the first outcome that is not pending.
func press(m model, input string) outcome {
	for _, k := range keys(input) {
		if o := step(m, k); o != pending {
			return o
		}
	}
	return pending
}

func typeInto(h interface{ handle(Key) bool }, input string) {
	for _, k := range keys(input) {
		h.handle(k)
	}
}

func plain() *Renderer { return NewRenderer(nil, Options{}) }

func exists(key string) string {
	if key == "deploy" {
		return "deploy already exists"
	}
	return ""
}

func TestSelect(t *testing.T) {
	tests := []struct {
		input string
		want  outcome
		sel   int
	}{
		{"\r", accepted, 0},
		{"\x1b[B\x1b[B\r", accepted, 2},
		{"\x1b[A\r", accepted, 0},
		{strings.Repeat("\x1b[B", 9) + "\r", accepted, 4},
		{"\x1b[F\x1b[A\r", accepted, 3},
		{"\x1b[6~\x1b[H\r", accepted, 0},
		{"\x1b[B\x1b", cancelled, 1},
		{"\x03", cancelled, 0},
	}
	for _, tt := range tests {
		s := &chooser{items: []string{"a", "b", "c", "d", "e"}}
		if got := press(s, tt.input); got != tt.want || s.sel != tt.sel {
			t.Errorf("press(%q) = %v, sel %d, want %v, sel %d", tt.input, got, s.sel, tt.want, tt.sel)
		}
	}
}

func TestSelectScrollsToSelection(t *testing.T) {
	s := &chooser{items: []string{"a", "b", "c", "d", "e"}}
	press(s, "\x1b[F")
	f := s.view(plain(), 11)
	if !strings.Contains(f.lines[f.row], "e") || strings.Contains(strings.Join(f.lines, "\n"), "○ a") {
		t.Errorf("view after End does not show e under the cursor: %q row %d", f.lines, f.row)
	}
}

func TestSearch(t *testing.T) {
	keys := []string{"build", "deploy", "deploy-prod", "prod-db"}
	search := func(text string) []string { return store.Search(keys, text) }
	tests := []struct {
		query, input string
		want         outcome
		pick         string
	}{
		{"", "\r", accepted, "build"},
		{"", "prod\r", accepted, "prod-db"},
		{"", "prod\x1b[B\r", accepted, "deploy-prod"},
		{"Prod", "\r", accepted, "prod-db"},
		{"zzz", "\r\x7f\x7f\x7fbu\r", accepted, "build"},
		{"de", "\x1b[B\x1b[Bp\r", accepted, "deploy"},
		{"", "prod\x1b", cancelled, ""},
	}
	for _, tt := range tests {
		s := newSearch(tt.query, search, "run")
		got := press(s, tt.input)
		pick := ""
		if got == accepted {
			pick = s.items[s.sel]
		}
		if got != tt.want || pick != tt.pick {
			t.Errorf("search %q then %q = %v, %q, want %v, %q", tt.query, tt.input, got, pick, tt.want, tt.pick)
		}
	}
}

func TestSearchView(t *testing.T) {
	s := newSearch("zz", func(string) []string { return nil }, "run")
	f := s.view(plain(), 24)
	all := strings.Join(f.lines, "\n")
	if !strings.Contains(all, "◆ zz") || !strings.Contains(all, "no keys match") {
		t.Errorf("view = %q, want the query and a no match note", f.lines)
	}
	if !f.cursor || f.row != 1 || f.col != TextColumn+4 {
		t.Errorf("cursor at row %d col %d shown %v, want it after the query", f.row, f.col, f.cursor)
	}
	press(s, "/")
	if all = strings.Join(s.view(plain(), 24).lines, "\n"); !strings.Contains(all, "keys only use") {
		t.Errorf("view after a bad character = %q, want a note", all)
	}
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		input string
		want  outcome
		yes   bool
	}{
		{"\r", accepted, false},
		{"y", accepted, true},
		{"N", accepted, false},
		{"\x1b[C\r", accepted, true},
		{"\t\t\r", accepted, false},
		{"\x1b[C\x1b", cancelled, true},
	}
	for _, tt := range tests {
		c := &confirm{}
		if got := press(c, tt.input); got != tt.want || c.yes != tt.yes {
			t.Errorf("press(%q) = %v, yes %v, want %v, yes %v", tt.input, got, c.yes, tt.want, tt.yes)
		}
	}
}

func TestEditSaves(t *testing.T) {
	tests := []struct {
		name  string
		cmd   Command
		input string
		key   string
		value string
	}{
		{"key then command", Command{}, "deploy\techo hi\x04", "deploy", "echo hi"},
		{"enter leaves the key", Command{}, "a\recho\x04", "a", "echo"},
		{"key reshaped while typed", Command{}, "My Key\techo\x04", "my-key", "echo"},
		{"several lines", Command{}, "x\tcd app\rgit pull\x04", "x", "cd app\ngit pull"},
		{"paste", Command{}, "x\t\x1b[200~one\r\ntwo\x1b[201~\x04", "x", "one\ntwo"},
		{"back to the key", Command{Key: "dep"}, "echo\x1b[Zloy\x04", "deploy", "echo"},
		{"edit", Command{Key: "deploy", Value: "echo old", KeyFixed: true, Check: exists}, "\x7f\x7f\x7fnew\x04", "deploy", "echo new"},
		{"fixed key ignores shift+tab", Command{Key: "deploy", Value: "x", KeyFixed: true}, "\x1b[Z!\x04", "deploy", "x!"},
	}
	for _, tt := range tests {
		e := newEditor(tt.cmd)
		if got := press(e, tt.input); got != accepted {
			t.Errorf("%s: outcome = %v, want accepted (message %q)", tt.name, got, e.msg.text)
			continue
		}
		if e.key.Value() != tt.key || e.area.Value() != tt.value {
			t.Errorf("%s: saved %q, %q, want %q, %q", tt.name, e.key.Value(), e.area.Value(), tt.key, tt.value)
		}
	}
}

func TestEditRefuses(t *testing.T) {
	tests := []struct {
		cmd      Command
		input    string
		msg      string
		keyFocus bool
	}{
		{Command{}, "\techo\x04", "key it is empty", true},
		{Command{Check: exists}, "deploy\techo\x04", "deploy already exists", true},
		{Command{Key: "deploy"}, "   \x04", "nothing to save yet, type a command first", false},
	}
	for _, tt := range tests {
		e := newEditor(tt.cmd)
		if got := press(e, tt.input); got != pending || e.msg.text != tt.msg || e.keyFocus != tt.keyFocus {
			t.Errorf("press(%q) = %v, %q, key focus %v, want pending, %q, %v", tt.input, got, e.msg.text, e.keyFocus, tt.msg, tt.keyFocus)
		}
	}
}

func TestEditStartFocus(t *testing.T) {
	tests := []struct {
		cmd      Command
		keyFocus bool
	}{
		{Command{}, true},
		{Command{Key: "ok"}, false},
		{Command{Key: "Bad Key"}, true},
		{Command{Key: "deploy", Check: exists}, true},
		{Command{Key: "deploy", KeyFixed: true, Check: exists}, false},
	}
	for _, tt := range tests {
		if got := newEditor(tt.cmd).keyFocus; got != tt.keyFocus {
			t.Errorf("newEditor(%+v) key focus = %v, want %v", tt.cmd, got, tt.keyFocus)
		}
	}
}

func TestEditCancel(t *testing.T) {
	if got := press(newEditor(Command{Key: "x"}), "\x1b"); got != cancelled {
		t.Errorf("Esc with no changes = %v, want cancelled", got)
	}
	e := newEditor(Command{Key: "x"})
	if got := press(e, "a\x1b"); got != pending || !strings.Contains(e.msg.text, "press esc again") {
		t.Errorf("first Esc with changes = %v, %q, want pending and a warning", got, e.msg.text)
	}
	if got := press(e, "b\x1b"); got != pending {
		t.Errorf("Esc after typing again = %v, want a fresh warning", got)
	}
	if got := press(e, "\x1b"); got != cancelled {
		t.Errorf("second Esc = %v, want cancelled", got)
	}
}

func TestEditKeyNote(t *testing.T) {
	e := newEditor(Command{Check: exists})
	for input, note := range map[string]string{"deploy": "deploy already exists", "/": "keys only use a-z 0-9 . _ -", "\x7fa.": "key it cannot end with a dot"} {
		press(e, input)
		if got := e.keyNote(); got != note {
			t.Errorf("after %q key note = %q, want %q", input, got, note)
		}
		e.key.set("")
	}
}

func TestKeyField(t *testing.T) {
	tests := []struct {
		start, input, want string
		col                int
	}{
		{"", "My Key", "my-key", 6},
		{"", "a/b", "ab", 2},
		{"dploy", "\x1b[H\x1b[Ce", "deploy", 2},
		{"deploy", "\x7f", "deplo", 5},
		{"deploy", "\x1b[H\x1b[3~", "eploy", 0},
		{"deploy", "\x01x\x05y", "xdeployy", 8},
		{"", "\x1b[200~abc\r\ndef\x1b[201~", "abc", 3},
		{strings.Repeat("a", store.MaxKeyLen), "b", strings.Repeat("a", store.MaxKeyLen), store.MaxKeyLen},
	}
	for _, tt := range tests {
		var f keyField
		f.set(tt.start)
		typeInto(&f, tt.input)
		if f.Value() != tt.want || f.col != tt.col {
			t.Errorf("type %q into %q = %q col %d, want %q col %d", tt.input, tt.start, f.Value(), f.col, tt.want, tt.col)
		}
	}
}

func TestTextArea(t *testing.T) {
	tests := []struct {
		start, input, want string
		row, col           int
	}{
		{"", "abc", "abc", 0, 3},
		{"a\r\nb", "", "a\nb", 1, 1},
		{"ac", "\x1b[D\x1b[200~x\ny\x1b[201~", "ax\nyc", 1, 1},
		{"ab\ncd", "\x1b[H\x7f", "abcd", 0, 2},
		{"ab", "\x1b[H\x7f", "ab", 0, 0},
		{"ab\ncd", "\x1b[A\x1b[3~", "abcd", 0, 2},
		{"ab\ncd", "\x1b[A\x1b[C", "ab\ncd", 1, 0},
		{"abcdef\nx\nabcdef", "\x1b[A\x1b[A", "abcdef\nx\nabcdef", 0, 6},
		{"a", "\tb\rc", "a\tb\nc", 1, 1},
		{"世界", "\x7f!", "世!", 0, 2},
	}
	for _, tt := range tests {
		a := newTextArea(tt.start)
		typeInto(a, tt.input)
		if a.Value() != tt.want || a.row != tt.row || a.col != tt.col {
			t.Errorf("type %q into %q = %q at %d:%d, want %q at %d:%d", tt.input, tt.start, a.Value(), a.row, a.col, tt.want, tt.row, tt.col)
		}
	}
}

func TestTextAreaView(t *testing.T) {
	tests := []struct {
		text   string
		width  int
		line   string
		curCol int
	}{
		{"cd app", 40, " 1  cd app", 10},
		{strings.Repeat("x", 50), 20, " 1  " + strings.Repeat("x", 15), 19},
		{"a\tb" + strings.Repeat("x", 30), 20, " 1  " + strings.Repeat("x", 15), 19},
		{"世界", 40, " 1  世界", 8},
		{"\tx", 40, " 1      x", 9},
	}
	for _, tt := range tests {
		lines, curRow, curCol := newTextArea(tt.text).view(plain(), tt.width, 5)
		if !slices.Equal(lines, []string{tt.line}) || curRow != 0 || curCol != tt.curCol {
			t.Errorf("view(%q) = %q at %d:%d, want %q at 0:%d", tt.text, lines, curRow, curCol, tt.line, tt.curCol)
		}
	}

	a := newTextArea("1\n2\n3\n4\n5")
	typeInto(a, "\x1b[5~")
	if lines, curRow, _ := a.view(plain(), 40, 3); !slices.Equal(lines, []string{" 1  1", " 2  2", " 3  3"}) || curRow != 0 {
		t.Errorf("view after Page Up = %q row %d, want the first three lines", lines, curRow)
	}
}

func TestFit(t *testing.T) {
	f := fit(frame{lines: []string{"short", strings.Repeat("x", 20), "a", "b", "c"}, row: 4}, plain(), 10, 4)
	if want := []string{"a", "b", "c"}; !slices.Equal(f.lines, want) || f.row != 2 {
		t.Errorf("fit = %q row %d, want %q row 2", f.lines, f.row, want)
	}
	f = fit(frame{lines: []string{strings.Repeat("x", 20)}}, plain(), 10, 24)
	if f.lines[0] != "xxxxxxxx…" {
		t.Errorf("fit cut a wide line to %q, want one column kept free", f.lines[0])
	}
}

func TestScreen(t *testing.T) {
	var buf bytes.Buffer
	s := &screen{w: &buf}
	s.draw(frame{lines: []string{"a", "b", "c"}, row: 1, col: 2, cursor: true})
	if want := hideCursor + clearToEnd + "a\r\nb\r\nc\x1b[1A\r\x1b[2C" + showCursor; buf.String() != want {
		t.Errorf("first draw wrote %q, want %q", buf.String(), want)
	}
	buf.Reset()
	s.clear()
	if want := "\x1b[1A" + clearToEnd + showCursor; buf.String() != want {
		t.Errorf("clear wrote %q, want it to go back up to the frame top first", buf.String())
	}
}
