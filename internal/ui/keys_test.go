// internal/ui/keys_test.go

package ui

import (
	"errors"
	"io"
	"os"
	"runtime"
	"slices"
	"testing"
	"time"
)

// feed runs chunks through one Decoder, as if each arrived in its own read.
func feed(chunks ...string) []Key {
	var d Decoder
	var keys []Key
	for _, c := range chunks {
		keys = append(keys, d.Feed([]byte(c))...)
	}
	return keys
}

func char(r rune) Key    { return Key{Type: KeyRune, Rune: r} }
func ctrl(r rune) Key    { return Key{Type: KeyCtrl, Rune: r} }
func key(t KeyType) Key  { return Key{Type: t} }
func paste(s string) Key { return Key{Type: KeyPaste, Text: s} }

func TestFeed(t *testing.T) {
	tests := []struct {
		in   string
		want []Key
	}{
		{"ab", []Key{char('a'), char('b')}},
		{"世🚀", []Key{char('世'), char('🚀')}},
		{"\r", []Key{key(KeyEnter)}},
		{"\n", []Key{key(KeyEnter)}},
		{"\t", []Key{key(KeyTab)}},
		{"\x7f", []Key{key(KeyBackspace)}},
		{"\x03", []Key{ctrl('c')}},
		{"\x04", []Key{ctrl('d')}},
		{"\x00\x1c", nil},
		{"\xffa", []Key{char('a')}},
		{"\x1b[A\x1bOB\x1b[1;5C", []Key{key(KeyUp), key(KeyDown), key(KeyRight)}},
		{"\x1b[H\x1b[1~\x1b[F\x1b[4~", []Key{key(KeyHome), key(KeyHome), key(KeyEnd), key(KeyEnd)}},
		{"\x1b[3~\x1b[3;5~", []Key{key(KeyDelete), key(KeyDelete)}},
		{"\x1b[5~\x1b[6~\x1b[Z", []Key{key(KeyPageUp), key(KeyPageDown), key(KeyBacktab)}},
		{"\x1b[15~\x1bOP", nil},
		{"\x1ba", []Key{char('a')}},
		{"\x1b\x1b[A", []Key{key(KeyEsc), key(KeyUp)}},
		{"\x1b[200~hello\nworld\x1b[201~", []Key{paste("hello\nworld")}},
		{"\x1b[200~a\x1b[Ab\x1b[201~x", []Key{paste("a\x1b[Ab"), char('x')}},
		{"\x1b[200~\x1b[201~", []Key{paste("")}},
	}
	for _, tt := range tests {
		if got := feed(tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("Feed(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestFeedSplit(t *testing.T) {
	tests := []struct {
		chunks []string
		want   []Key
	}{
		{[]string{"\x1b", "[A"}, []Key{key(KeyUp)}},
		{[]string{"\x1b[3", "~"}, []Key{key(KeyDelete)}},
		{[]string{"\xf0", "\x9f\x9a", "\x80"}, []Key{char('🚀')}},
		{[]string{"\x1b[20", "0~hi\x1b[201~"}, []Key{paste("hi")}},
		{[]string{"\x1b[200~abc\x1b[2", "01~"}, []Key{paste("abc")}},
		{[]string{"\x1b[200~a\x1b[20", "9b\x1b[201~"}, []Key{paste("a\x1b[209b")}},
	}
	for _, tt := range tests {
		if got := feed(tt.chunks...); !slices.Equal(got, tt.want) {
			t.Errorf("Feed(%q) = %v, want %v", tt.chunks, got, tt.want)
		}
	}
}

func TestFlush(t *testing.T) {
	tests := []struct {
		in      string
		pending bool
		want    []Key
	}{
		{"\x1b", true, []Key{key(KeyEsc)}},
		{"\x1b[", true, []Key{key(KeyEsc), char('[')}},
		{"\xe4", true, nil},
		{"\x1b[200~abc", false, nil},
	}
	for _, tt := range tests {
		var d Decoder
		d.Feed([]byte(tt.in))
		if d.Pending() != tt.pending {
			t.Errorf("Pending() after %q = %v, want %v", tt.in, !tt.pending, tt.pending)
		}
		if got := d.Flush(); !slices.Equal(got, tt.want) {
			t.Errorf("Flush() after %q = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// pipeReader gives a Reader on the read end of a pipe, plus both ends.
func pipeReader(t *testing.T) (*Reader, *os.File, *os.File) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Esc wait needs a console handle on Windows, not a pipe")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		r.Close()
		w.Close()
	})
	return NewReader(r), r, w
}

func TestReaderEsc(t *testing.T) {
	rd, _, w := pipeReader(t)
	w.WriteString("\x1b")
	start := time.Now()
	if k, err := rd.ReadKey(); err != nil || k != key(KeyEsc) || time.Since(start) < escTimeout {
		t.Errorf("lone Esc = %v, %v after %v, want Esc after the wait", k, err, time.Since(start))
	}

	w.WriteString("\x1b")
	go func() {
		time.Sleep(escTimeout / 5)
		w.WriteString("[A")
	}()
	if k, err := rd.ReadKey(); err != nil || k != key(KeyUp) {
		t.Errorf("split arrow = %v, %v, want Up", k, err)
	}
}

func TestReaderLeavesRest(t *testing.T) {
	rd, r, w := pipeReader(t)
	w.WriteString("a")
	if k, _ := rd.ReadKey(); k != char('a') {
		t.Fatalf("ReadKey = %v, want a", k)
	}
	w.WriteString("left for the command")
	w.Close()
	if rest, _ := io.ReadAll(r); string(rest) != "left for the command" {
		t.Errorf("pipe holds %q, want the unread bytes untouched", rest)
	}
}

func TestReaderEOF(t *testing.T) {
	rd, _, w := pipeReader(t)
	w.WriteString("\x1b")
	w.Close()
	if k, _ := rd.ReadKey(); k != key(KeyEsc) {
		t.Errorf("ReadKey = %v, want the pending Esc first", k)
	}
	if _, err := rd.ReadKey(); !errors.Is(err, io.EOF) {
		t.Errorf("ReadKey after close = %v, want io.EOF", err)
	}
}
