// internal/ui/term.go

package ui

import (
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

// Fallback size for a terminal that does not report one.
const (
	defaultWidth  = 80
	defaultHeight = 24
)

// escTimeout is how long a lone Esc waits for more bytes. Arrow keys also start
// with Esc, but their bytes arrive together, well inside this window.
const escTimeout = 50 * time.Millisecond

// Options describe the output a Renderer writes to. VT means the terminal
// understands escape codes, which prompts need to move the cursor.
type Options struct {
	TTY   bool
	VT    bool
	Color bool
	ASCII bool
	Width int
}

// Detect reads the options for f from the terminal and environment. NO_COLOR
// takes away the colour but never the cursor movement, and PUX_ASCII=1 swaps
// in plain symbols for consoles without unicode fonts.
func Detect(f *os.File) Options {
	opts := Options{
		TTY:   IsTerminal(f),
		ASCII: os.Getenv("PUX_ASCII") == "1",
		Width: defaultWidth,
	}
	if !opts.TTY {
		return opts
	}
	opts.Width, _ = size(f)
	opts.VT = os.Getenv("TERM") != "dumb" && enableVT(f)
	opts.Color = opts.VT && os.Getenv("NO_COLOR") == ""
	return opts
}

// IsTerminal reports whether f is a terminal, such as stdin when nothing is piped in.
func IsTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

// size returns the width and height of the terminal on f, or the fallback size.
func size(f *os.File) (int, int) {
	w, h, err := term.GetSize(int(f.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		return defaultWidth, defaultHeight
	}
	return w, h
}

// Terminal is the user's terminal in raw mode, so every key arrives as it is pressed.
type Terminal struct {
	*Reader
	in    *os.File
	out   *os.File
	state *term.State
}

// Open puts the terminal in raw mode and turns on bracketed paste, which makes
// a pasted block arrive as one KeyPaste. Always call Close to put it back.
func Open(in, out *os.File) (*Terminal, error) {
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return nil, err
	}
	io.WriteString(out, pasteOn)
	return &Terminal{Reader: NewReader(in), in: in, out: out, state: state}, nil
}

// Close restores the terminal exactly as it was before Open.
func (t *Terminal) Close() error {
	io.WriteString(t.out, pasteOff+showCursor)
	return term.Restore(int(t.in.Fd()), t.state)
}

// Size returns the width and height of the terminal.
func (t *Terminal) Size() (int, int) { return size(t.out) }

func (t *Terminal) Write(p []byte) (int, error) { return t.out.Write(p) }

// Reader reads keys from a file. It only reads when asked for a key, so once a
// prompt is done, every byte left goes to whatever runs next.
type Reader struct {
	f     *os.File
	buf   []byte
	dec   Decoder
	queue []Key
}

// NewReader returns a Reader for f.
func NewReader(f *os.File) *Reader {
	return &Reader{f: f, buf: make([]byte, 4096)}
}

// ReadKey waits for the next key.
func (rd *Reader) ReadKey() (Key, error) {
	for len(rd.queue) == 0 {
		if rd.dec.Pending() {
			ready, err := waitInput(rd.f, escTimeout)
			if err != nil {
				return Key{}, err
			}
			if !ready {
				rd.queue = append(rd.queue, rd.dec.Flush()...)
				continue
			}
		}
		n, err := rd.f.Read(rd.buf)
		if n > 0 {
			rd.queue = append(rd.queue, rd.dec.Feed(rd.buf[:n])...)
		}
		if err != nil {
			rd.queue = append(rd.queue, rd.dec.Flush()...)
			if len(rd.queue) == 0 {
				return Key{}, err
			}
		}
	}
	k := rd.queue[0]
	rd.queue = rd.queue[1:]
	return k, nil
}
