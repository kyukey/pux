// internal/store/store_test.go

package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func newStore(t *testing.T, keys ...string) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "pux"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if err := s.Save(k, []byte("echo "+k), Create); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func mkdir(t *testing.T, s *Store, name string) string {
	t.Helper()
	dir := filepath.Join(s.Dir(), name)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// symlink skips the test where links need extra rights, as on Windows by default.
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not available: %v", err)
	}
}

func TestValidate(t *testing.T) {
	for _, key := range []string{"a", "deploy-prod", "db_reset", "v1.2", "9lives", "ls", "con-fix", strings.Repeat("a", MaxKeyLen)} {
		if err := Validate(key); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", key, err)
		}
	}
	tests := []struct {
		key, reason string
	}{
		{"", "it is empty"},
		{strings.Repeat("a", MaxKeyLen+1), "it is over 64 characters"},
		{"Deploy", "it can only use a-z 0-9 . _ -"},
		{"../x", "it can only use a-z 0-9 . _ -"},
		{`a\b`, "it can only use a-z 0-9 . _ -"},
		{"café", "it can only use a-z 0-9 . _ -"},
		{"-x", "it must start with a letter or digit"},
		{".env", "it must start with a letter or digit"},
		{"a.", "it cannot end with a dot"},
		{"con.txt", "it is a reserved name on Windows"},
		{"lpt9", "it is a reserved name on Windows"},
	}
	for _, tt := range tests {
		var ke *KeyError
		err := Validate(tt.key)
		if !errors.As(err, &ke) || ke.Reason != tt.reason || !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Validate(%q) = %v, want reason %q", tt.key, err, tt.reason)
		}
	}
}

func TestSuggest(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"deploy", "deploy"},
		{"My Deploy!", "my-deploy"},
		{"  hello   world  ", "hello-world"},
		{"--start", "start"},
		{"v1.2.", "v1.2"},
		{"Build/Prod", "build-prod"},
		{strings.Repeat("a", 100), strings.Repeat("a", MaxKeyLen)},
		{"", ""},
		{"---", ""},
		{"CON", ""},
	}
	for _, tt := range tests {
		if got := Suggest(tt.input); got != tt.want {
			t.Errorf("Suggest(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestClosest(t *testing.T) {
	tests := []struct {
		input string
		keys  []string
		want  string
	}{
		{"depoly", []string{"build", "deploy"}, "deploy"},
		{"DEPLOY", []string{"build", "deploy"}, "deploy"},
		{"hj", []string{"hi"}, "hi"},
		{"production", []string{"build", "deploy"}, ""},
		{"abcd", []string{"abxy", "abcx"}, "abcx"},
		{"ab", []string{"ax", "ay"}, "ax"},
		{"deploy", nil, ""},
	}
	for _, tt := range tests {
		if got, ok := Closest(tt.input, tt.keys); got != tt.want || ok != (tt.want != "") {
			t.Errorf("Closest(%q, %v) = %q, want %q", tt.input, tt.keys, got, tt.want)
		}
	}
}

func TestSearch(t *testing.T) {
	keys := []string{"build", "deploy", "deploy-prod", "prod-db", "reprod-bug"}
	tests := []struct {
		text string
		want []string
	}{
		{"prod", []string{"prod-db", "deploy-prod", "reprod-bug"}},
		{"Prod", []string{"prod-db", "deploy-prod", "reprod-bug"}},
		{"dep", []string{"deploy", "deploy-prod"}},
		{"prod db", []string{"prod-db"}},
		{"", keys},
		{"zzz", nil},
	}
	for _, tt := range tests {
		if got := Search(keys, tt.text); !slices.Equal(got, tt.want) {
			t.Errorf("Search(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func FuzzSuggest(f *testing.F) {
	for _, s := range []string{"", "My Deploy!", "--a--", "CON", "v1.2.", "café"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		key := Suggest(input)
		if key != "" && (Validate(key) != nil || Suggest(key) != key) {
			t.Fatalf("Suggest(%q) = %q, which is invalid or not stable", input, key)
		}
	})
}

func FuzzValidate(f *testing.F) {
	for _, s := range []string{"deploy", "../x", "a/b", "con", ".."} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, key string) {
		if Validate(key) == nil && (!filepath.IsLocal(key) || filepath.Base(key) != key) {
			t.Fatalf("Validate accepted %q, which could leave the store folder", key)
		}
	})
}

func TestDefaultDir(t *testing.T) {
	dir := t.TempDir()
	check := func(want string) {
		t.Helper()
		if got, err := DefaultDir(); err != nil || got != want {
			t.Errorf("DefaultDir() = %q, %v, want %q", got, err, want)
		}
	}

	t.Setenv("PUX_DIR", dir)
	check(dir)
	t.Setenv("PUX_DIR", "commands")
	abs, _ := filepath.Abs("commands")
	check(abs)
	t.Setenv("PUX_DIR", "")

	if runtime.GOOS == "windows" {
		t.Setenv("AppData", dir)
		check(filepath.Join(dir, "pux"))
		return
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	check(filepath.Join(dir, "pux"))
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", "relative")
	check(filepath.Join(dir, ".config", "pux"))
}

func TestOpen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "pux")
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("Open did not create %s", dir)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("store folder mode = %v, want owner only", info.Mode().Perm())
	}
}

func TestSave(t *testing.T) {
	s := newStore(t, "deploy")
	for _, v := range []string{"cd app\r\ngit pull\r\n", "echo héllo 🚀", "\x00\xff", ""} {
		if err := s.Save("deploy", []byte(v), Replace); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Get("deploy"); string(got) != v {
			t.Errorf("Get = %q, want %q byte for byte", got, v)
		}
	}
	if err := s.Save("deploy", []byte("x"), Create); !errors.Is(err, ErrExists) {
		t.Errorf("Create on a taken key = %v, want ErrExists", err)
	}
	if err := s.Save("missing", []byte("x"), Replace); !errors.Is(err, ErrNotFound) {
		t.Errorf("Replace on a missing key = %v, want ErrNotFound", err)
	}
	entries, _ := os.ReadDir(s.Dir())
	if len(entries) != 1 {
		t.Errorf("store holds %d files, want only deploy and no temp files", len(entries))
	}
}

func TestSaveFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes work differently on Windows")
	}
	s := newStore(t, "deploy")
	path := filepath.Join(s.Dir(), "deploy")
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("new file mode = %v, want owner only", info.Mode().Perm())
	}
	os.Chmod(path, 0o640)
	if err := s.Save("deploy", []byte("new"), Replace); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Errorf("mode after Replace = %v, want 0640 kept", info.Mode().Perm())
	}
}

func TestInvalidKeys(t *testing.T) {
	s := newStore(t)
	for _, key := range []string{"../x", "a/b", ""} {
		if err := s.Save(key, []byte("x"), Create); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Save(%q) = %v, want ErrInvalidKey", key, err)
		}
		if _, err := s.Get(key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Get(%q) = %v, want ErrInvalidKey", key, err)
		}
		if err := s.Remove(key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Remove(%q) = %v, want ErrInvalidKey", key, err)
		}
		if s.Exists(key) {
			t.Errorf("Exists(%q) = true", key)
		}
	}
}

func TestFoldersAreNotKeys(t *testing.T) {
	s := newStore(t)
	dir := mkdir(t, s, "folder")
	if _, err := s.Get("folder"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(folder) = %v, want ErrNotFound", err)
	}
	if err := s.Save("folder", []byte("x"), Replace); !errors.Is(err, ErrNotFound) {
		t.Errorf("Replace(folder) = %v, want ErrNotFound", err)
	}
	if err := s.Remove("folder"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove(folder) = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(dir); err != nil || s.Exists("folder") {
		t.Error("folder was removed or counted as a key")
	}
}

func TestRemove(t *testing.T) {
	s := newStore(t, "deploy", "build")
	if err := s.Remove("deploy"); err != nil {
		t.Fatal(err)
	}
	if s.Exists("deploy") || !s.Exists("build") {
		t.Error("Remove deleted the wrong key")
	}
	if err := s.Remove("deploy"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Remove = %v, want ErrNotFound", err)
	}
}

func TestSymlinks(t *testing.T) {
	s := newStore(t, "deploy")
	repo := t.TempDir()
	target := filepath.Join(repo, "linked")
	os.WriteFile(target, []byte("old"), 0o600)
	link := filepath.Join(s.Dir(), "linked")
	symlink(t, target, link)
	symlink(t, filepath.Join(repo, "missing"), filepath.Join(s.Dir(), "broken"))
	symlink(t, repo, filepath.Join(s.Dir(), "dirlink"))

	if got, _ := s.List(); !slices.Equal(got, []string{"deploy", "linked"}) {
		t.Errorf("List() = %v, want deploy and linked only", got)
	}
	if err := s.Save("linked", []byte("new"), Replace); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Error("linked is no longer a symlink after Replace")
	}
	if got, _ := os.ReadFile(target); string(got) != "new" {
		t.Errorf("target holds %q, want new", got)
	}
	if err := s.Remove("linked"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Error("Remove deleted the symlink target")
	}
}

func TestListMatchResolve(t *testing.T) {
	s := newStore(t, "build", "deploy", "deploy-prod", "dev-db")
	for _, name := range []string{".tmp-123", "Bad", ".DS_Store", "my key"} {
		os.WriteFile(filepath.Join(s.Dir(), name), nil, 0o600)
	}
	mkdir(t, s, "folder")

	if got, _ := s.List(); !slices.Equal(got, []string{"build", "deploy", "deploy-prod", "dev-db"}) {
		t.Errorf("List() = %v, want only valid keys", got)
	}
	if got, _ := s.Match("de"); !slices.Equal(got, []string{"deploy", "deploy-prod", "dev-db"}) {
		t.Errorf("Match(de) = %v", got)
	}

	tests := []struct {
		input   string
		key     string
		matches []string
		err     error
	}{
		{"deploy", "deploy", nil, nil},
		{"b", "build", nil, nil},
		{"deploy-", "deploy-prod", nil, nil},
		{"dep", "", []string{"deploy", "deploy-prod"}, ErrAmbiguous},
		{"x", "", nil, ErrNotFound},
	}
	for _, tt := range tests {
		key, matches, err := s.Resolve(tt.input)
		if key != tt.key || !slices.Equal(matches, tt.matches) || !errors.Is(err, tt.err) || (tt.err == nil) != (err == nil) {
			t.Errorf("Resolve(%q) = %q, %v, %v, want %q, %v, %v", tt.input, key, matches, err, tt.key, tt.matches, tt.err)
		}
	}
}

func TestConcurrentCreate(t *testing.T) {
	s := newStore(t)
	const n = 20
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { errs <- s.Save("deploy", []byte{byte('a' + i)}, Create) })
	}
	wg.Wait()
	close(errs)
	won := 0
	for err := range errs {
		if err == nil {
			won++
		} else if !errors.Is(err, ErrExists) {
			t.Errorf("Create = %v, want nil or ErrExists", err)
		}
	}
	if won != 1 {
		t.Errorf("%d creates succeeded, want exactly 1", won)
	}
}

func TestReplaceWhileFileIsOpen(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows locks files that another program has open")
	}
	s := newStore(t, "deploy")
	f, err := os.Open(filepath.Join(s.Dir(), "deploy"))
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		f.Close()
	}()
	if err := s.Save("deploy", []byte("new"), Replace); err != nil {
		t.Fatalf("Replace while the file was briefly open = %v, want a retry to succeed", err)
	}
	if got, _ := s.Get("deploy"); string(got) != "new" {
		t.Errorf("Get = %q, want new", got)
	}
}
