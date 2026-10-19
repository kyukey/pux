// internal/store/store.go

package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Errors returned by Store. Each one is wrapped with the key it is about.
var (
	ErrExists    = errors.New("key already exists")
	ErrNotFound  = errors.New("key not found")
	ErrAmbiguous = errors.New("key matches more than one entry")
)

// Mode says whether Save adds a new key or replaces one that is already saved.
type Mode int

const (
	Create Mode = iota
	Replace
)

// Store keeps one plain text file per key inside a single folder.
type Store struct {
	dir string
}

// DefaultDir picks the store folder. PUX_DIR wins when set. On macOS and Linux
// it is then $XDG_CONFIG_HOME/pux or ~/.config/pux, and on Windows %AppData%\pux.
func DefaultDir() (string, error) {
	if dir := os.Getenv("PUX_DIR"); dir != "" {
		return filepath.Abs(dir)
	}
	if runtime.GOOS == "windows" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, "pux"), nil
	}
	if base := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(base) {
		return filepath.Join(base, "pux"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "pux"), nil
}

// Open uses dir as the store and creates it, owner only, when it does not exist.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir returns the folder the store keeps its files in.
func (s *Store) Dir() string { return s.dir }

func (s *Store) path(key string) string { return filepath.Join(s.dir, key) }

// Save writes value under key byte for byte. Create fails with ErrExists when
// the key is taken, and Replace fails with ErrNotFound when it is missing.
func (s *Store) Save(key string, value []byte, mode Mode) error {
	if err := Validate(key); err != nil {
		return err
	}
	if mode == Replace {
		return s.replace(key, value)
	}
	return s.create(key, value)
}

// create lets the filesystem decide atomically whether the key is free.
func (s *Store) create(key string, value []byte) error {
	f, err := os.OpenFile(s.path(key), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: %s", ErrExists, key)
	}
	if err != nil {
		return err
	}
	if err := writeFile(f, value); err != nil {
		os.Remove(f.Name())
		return err
	}
	return nil
}

// replace writes a temp file and renames it over the old one, so a crash never
// leaves half a value. A symlinked key is written through to its target, and
// the file keeps its permissions.
func (s *Store) replace(key string, value []byte) error {
	info, err := s.stat(key)
	if err != nil {
		return err
	}
	target, err := filepath.EvalSymlinks(s.path(key))
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".tmp-*")
	if err != nil {
		return err
	}
	err = tmp.Chmod(info.Mode().Perm())
	if err == nil {
		err = writeFile(tmp, value)
	} else {
		tmp.Close()
	}
	if err == nil {
		err = rename(tmp.Name(), target)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// Windows error codes for a file another program has open, like an antivirus scan.
const (
	winAccessDenied     = syscall.Errno(5)
	winSharingViolation = syscall.Errno(32)
)

// rename moves tmp over target. On Windows it retries for about a third of a
// second while another program briefly holds the file, and fails at once otherwise.
func rename(tmp, target string) error {
	err := os.Rename(tmp, target)
	if runtime.GOOS != "windows" {
		return err
	}
	for wait := 10 * time.Millisecond; wait <= 160*time.Millisecond; wait *= 2 {
		if !errors.Is(err, winAccessDenied) && !errors.Is(err, winSharingViolation) {
			return err
		}
		time.Sleep(wait)
		err = os.Rename(tmp, target)
	}
	return err
}

// writeFile writes value, flushes it to disk and closes f, so the data is on
// disk before any name points to it.
func writeFile(f *os.File, value []byte) error {
	_, err := f.Write(value)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Get returns the value saved under key.
func (s *Store) Get(key string) ([]byte, error) {
	if err := Validate(key); err != nil {
		return nil, err
	}
	if _, err := s.stat(key); err != nil {
		return nil, err
	}
	return os.ReadFile(s.path(key))
}

// Exists reports whether key is saved. An invalid key never exists.
func (s *Store) Exists(key string) bool {
	if Validate(key) != nil {
		return false
	}
	_, err := s.stat(key)
	return err == nil
}

// Remove deletes key. A symlinked key loses only the link, never its target,
// and a broken link can be removed too, even though it does not count as a key.
func (s *Store) Remove(key string) error {
	if err := Validate(key); err != nil {
		return err
	}
	info, err := os.Lstat(s.path(key))
	if errors.Is(err, fs.ErrNotExist) || (err == nil && info.IsDir()) {
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if err != nil {
		return err
	}
	return os.Remove(s.path(key))
}

// stat follows symlinks and reports anything that is not a regular file as
// missing, so folders, broken links and pipes never count as keys.
func (s *Store) stat(key string) (fs.FileInfo, error) {
	info, err := os.Stat(s.path(key))
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.Mode().IsRegular()) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return info, err
}

// List returns every key in sorted order. Files that are not valid keys are skipped.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		if Validate(e.Name()) == nil && s.isFile(e) {
			keys = append(keys, e.Name())
		}
	}
	return keys, nil
}

// isFile reports whether e is a regular file, or a symlink to one, so keys
// linked in from a dotfiles repo work like any other.
func (s *Store) isFile(e fs.DirEntry) bool {
	switch {
	case e.Type().IsRegular():
		return true
	case e.Type()&fs.ModeSymlink == 0:
		return false
	}
	_, err := s.stat(e.Name())
	return err == nil
}

// Match returns every key that starts with prefix, in sorted order.
func (s *Store) Match(prefix string) ([]string, error) {
	keys, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []string
	for i := sort.SearchStrings(keys, prefix); i < len(keys) && strings.HasPrefix(keys[i], prefix); i++ {
		out = append(out, keys[i])
	}
	return out, nil
}

// Resolve turns input into one key. An exact key wins, then a single prefix match.
// With several matches it returns ErrAmbiguous along with the candidates.
func (s *Store) Resolve(input string) (string, []string, error) {
	matches, err := s.Match(input)
	if err != nil {
		return "", nil, err
	}
	switch {
	case len(matches) == 0:
		return "", nil, fmt.Errorf("%w: %s", ErrNotFound, input)
	case matches[0] == input, len(matches) == 1:
		return matches[0], nil, nil
	default:
		return "", matches, fmt.Errorf("%w: %s", ErrAmbiguous, input)
	}
}
