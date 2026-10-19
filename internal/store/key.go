// internal/store/key.go

package store

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// MaxKeyLen is the longest key allowed, in bytes.
const MaxKeyLen = 64

// ErrInvalidKey matches every KeyError with errors.Is.
var ErrInvalidKey = errors.New("invalid key")

// windowsNames are file names Windows refuses, even with an extension like con.txt.
var windowsNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// KeyError explains why a key was rejected.
type KeyError struct {
	Key    string
	Reason string
}

func (e *KeyError) Error() string {
	return fmt.Sprintf("invalid key %q: %s", e.Key, e.Reason)
}

func (e *KeyError) Unwrap() error { return ErrInvalidKey }

// Validate checks that key is a safe file name on Linux, macOS and Windows.
func Validate(key string) error {
	if reason := keyProblem(key); reason != "" {
		return &KeyError{Key: key, Reason: reason}
	}
	return nil
}

func keyProblem(key string) string {
	switch {
	case key == "":
		return "it is empty"
	case len(key) > MaxKeyLen:
		return fmt.Sprintf("it is over %d characters", MaxKeyLen)
	}
	for i := range len(key) {
		if !isKeyChar(key[i]) {
			return "it can only use a-z 0-9 . _ -"
		}
	}
	base, _, _ := strings.Cut(key, ".")
	switch {
	case !isAlnum(key[0]):
		return "it must start with a letter or digit"
	case key[len(key)-1] == '.':
		return "it cannot end with a dot"
	case windowsNames[base]:
		return "it is a reserved name on Windows"
	}
	return ""
}

// KeyByte maps a typed character to a key character. Letters are lowercased
// and a space becomes a dash. It is shared by Suggest and the live key field.
func KeyByte(r rune) (byte, bool) {
	if r == ' ' {
		return '-', true
	}
	r = unicode.ToLower(r)
	if r < 0x80 && isKeyChar(byte(r)) {
		return byte(r), true
	}
	return 0, false
}

// Suggest reshapes input into a valid key, or returns "" when that is not possible.
// Characters that cannot be mapped become dashes, and repeated dashes collapse.
func Suggest(input string) string {
	var buf [MaxKeyLen]byte
	n := 0
	for _, r := range input {
		c, ok := KeyByte(r)
		if !ok {
			c = '-'
		}
		leading := n == 0 && !isAlnum(c)
		repeated := c == '-' && n > 0 && buf[n-1] == '-'
		if leading || repeated {
			continue
		}
		buf[n] = c
		n++
		if n == MaxKeyLen {
			break
		}
	}
	for n > 0 && (buf[n-1] == '-' || buf[n-1] == '.') {
		n--
	}
	key := string(buf[:n])
	if Validate(key) != nil {
		return ""
	}
	return key
}

// Closest returns the key nearest to input, for "did you mean" hints.
// Short inputs allow fewer edits so hints stay relevant. On a tie the earlier key wins.
func Closest(input string, keys []string) (string, bool) {
	target := Suggest(input)
	if target == "" {
		target = strings.ToLower(input)
	}
	limit := 2
	if len(target) <= 3 {
		limit = 1
	}
	best := ""
	for _, k := range keys {
		if d, ok := within(target, k, limit); ok {
			best, limit = k, d-1
			if limit < 0 {
				break
			}
		}
	}
	return best, best != ""
}

// Search returns the keys that contain text, those starting with it first, and
// keeps the given order within each group. Text is reshaped like a typed key,
// so "Prod" finds prod, and empty text matches every key.
func Search(keys []string, text string) []string {
	var b strings.Builder
	for _, r := range text {
		if c, ok := KeyByte(r); ok {
			b.WriteByte(c)
		}
	}
	text = b.String()
	var starts, contains []string
	for _, k := range keys {
		switch {
		case strings.HasPrefix(k, text):
			starts = append(starts, k)
		case strings.Contains(k, text):
			contains = append(contains, k)
		}
	}
	return append(starts, contains...)
}

// within returns the edit distance between a and b when it is at most limit.
// It gives up early on a length gap or once a whole row is over the limit.
func within(a, b string, limit int) (int, bool) {
	diff := len(a) - len(b)
	if diff > limit || -diff > limit || len(b) > MaxKeyLen {
		return 0, false
	}
	var rows [2][MaxKeyLen + 1]int
	prev, cur := rows[0][:len(b)+1], rows[1][:len(b)+1]
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		rowMin := i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			rowMin = min(rowMin, cur[j])
		}
		if rowMin > limit {
			return 0, false
		}
		prev, cur = cur, prev
	}
	if d := prev[len(b)]; d <= limit {
		return d, true
	}
	return 0, false
}

func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

func isKeyChar(c byte) bool {
	return isAlnum(c) || c == '.' || c == '_' || c == '-'
}
