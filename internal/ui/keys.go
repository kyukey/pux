// internal/ui/keys.go

package ui

import (
	"bytes"
	"unicode/utf8"
)

// KeyType says what kind of key was pressed. KeyRune carries a printable
// character in Key.Rune, KeyCtrl carries ctrl plus a letter as 'a' to 'z' in
// Key.Rune, and KeyPaste carries a pasted block in Key.Text.
type KeyType int

const (
	KeyRune KeyType = iota
	KeyCtrl
	KeyPaste
	KeyEnter
	KeyTab
	KeyBacktab
	KeyBackspace
	KeyDelete
	KeyEsc
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
)

// Key is one decoded key press.
type Key struct {
	Type KeyType
	Rune rune
	Text string
}

// IsCtrl reports whether k is ctrl plus the letter c.
func (k Key) IsCtrl(c rune) bool { return k.Type == KeyCtrl && k.Rune == c }

const esc = 0x1b

// pasteEnd closes a bracketed paste. The opening marker is ESC[200~, read in nextCSI.
var pasteEnd = []byte("\x1b[201~")

var ss3Keys = map[byte]KeyType{
	'A': KeyUp, 'B': KeyDown, 'C': KeyRight, 'D': KeyLeft, 'H': KeyHome, 'F': KeyEnd,
}

var csiFinal = map[byte]KeyType{
	'A': KeyUp, 'B': KeyDown, 'C': KeyRight, 'D': KeyLeft, 'H': KeyHome, 'F': KeyEnd, 'Z': KeyBacktab,
}

var csiTilde = map[string]KeyType{
	"1": KeyHome, "7": KeyHome, "4": KeyEnd, "8": KeyEnd,
	"3": KeyDelete, "5": KeyPageUp, "6": KeyPageDown,
}

// Decoder turns terminal input bytes into keys. Input can arrive split at any
// byte, so an unfinished sequence waits in the buffer for the next Feed.
type Decoder struct {
	buf     []byte
	pasting bool
	paste   bytes.Buffer
}

// Feed adds input and returns every key that is now complete.
func (d *Decoder) Feed(p []byte) []Key {
	d.buf = append(d.buf, p...)
	var keys []Key
	for len(d.buf) > 0 {
		k, n := d.next()
		if n == 0 {
			break
		}
		d.buf = d.buf[n:]
		if k != nil {
			keys = append(keys, *k)
		}
	}
	return keys
}

// Pending reports bytes waiting for the rest of a sequence.
func (d *Decoder) Pending() bool { return len(d.buf) > 0 && !d.pasting }

// Flush is called when no more input came in time. A waiting escape byte was
// a real Esc press, and anything else left is a broken UTF-8 sequence to drop.
func (d *Decoder) Flush() []Key {
	if len(d.buf) == 0 || d.pasting {
		return nil
	}
	rest := d.buf
	d.buf = nil
	if rest[0] == esc {
		return append([]Key{{Type: KeyEsc}}, d.Feed(rest[1:])...)
	}
	return nil
}

// next decodes one key from the start of the buffer. It returns n = 0 when it
// needs more bytes, and a nil key when bytes were used up without a key.
func (d *Decoder) next() (*Key, int) {
	if d.pasting {
		return d.nextPaste()
	}
	b := d.buf[0]
	switch {
	case b == esc:
		return d.nextEscape()
	case b == '\r' || b == '\n':
		return &Key{Type: KeyEnter}, 1
	case b == '\t':
		return &Key{Type: KeyTab}, 1
	case b == 0x7f || b == 0x08:
		return &Key{Type: KeyBackspace}, 1
	case b >= 1 && b <= 26:
		return &Key{Type: KeyCtrl, Rune: rune('a' + b - 1)}, 1
	case b < 0x20:
		return nil, 1
	}
	if !utf8.FullRune(d.buf) {
		return nil, 0
	}
	r, n := utf8.DecodeRune(d.buf)
	if r == utf8.RuneError {
		return nil, n
	}
	return &Key{Type: KeyRune, Rune: r}, n
}

// nextPaste collects everything up to the paste end marker as one key. A tail
// that could be the start of the marker waits for the next Feed.
func (d *Decoder) nextPaste() (*Key, int) {
	i := bytes.Index(d.buf, pasteEnd)
	if i < 0 {
		keep := min(len(d.buf), len(pasteEnd)-1)
		d.paste.Write(d.buf[:len(d.buf)-keep])
		if keep == len(d.buf) {
			return nil, 0
		}
		return nil, len(d.buf) - keep
	}
	d.paste.Write(d.buf[:i])
	text := d.paste.String()
	d.paste.Reset()
	d.pasting = false
	return &Key{Type: KeyPaste, Text: text}, i + len(pasteEnd)
}

// nextEscape decodes ESC sequences: CSI (ESC [), SS3 (ESC O) and alt plus a key.
// pux has no alt shortcuts, so alt plus a key arrives as the key alone.
func (d *Decoder) nextEscape() (*Key, int) {
	if len(d.buf) < 2 {
		return nil, 0
	}
	switch d.buf[1] {
	case '[':
		return d.nextCSI()
	case 'O':
		if len(d.buf) < 3 {
			return nil, 0
		}
		if t, ok := ss3Keys[d.buf[2]]; ok {
			return &Key{Type: t}, 3
		}
		return nil, 3
	case esc:
		return &Key{Type: KeyEsc}, 1
	}
	return nil, 1
}

// nextCSI reads ESC [ params final, such as ESC[A or ESC[3~ or ESC[1;5C.
func (d *Decoder) nextCSI() (*Key, int) {
	for i := 2; i < len(d.buf); i++ {
		c := d.buf[i]
		if c < 0x40 || c > 0x7e {
			continue
		}
		n := i + 1
		params := d.buf[2:i]
		if c == '~' {
			if string(params) == "200" {
				d.pasting = true
				return nil, n
			}
			first, _, _ := bytes.Cut(params, []byte(";"))
			if t, ok := csiTilde[string(first)]; ok {
				return &Key{Type: t}, n
			}
			return nil, n
		}
		if t, ok := csiFinal[c]; ok {
			return &Key{Type: t}, n
		}
		return nil, n
	}
	return nil, 0
}
