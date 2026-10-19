// internal/cli/cli_test.go

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kyukey/pux/internal/runner"
	"github.com/kyukey/pux/internal/store"
	"github.com/kyukey/pux/internal/ui"
)

// fakePrompt answers prompts with fixed values and records what it was asked.
type fakePrompt struct {
	key, value string
	pick       int
	yes        bool
	err        error

	edit     *ui.Command
	found    []string
	title    string
	action   string
	question string
}

func (f *fakePrompt) Edit(c ui.Command) (string, string, error) {
	f.edit = &c
	key := f.key
	if key == "" {
		key = c.Key
	}
	return key, f.value, f.err
}

func (f *fakePrompt) Select(title string, items []string, action string) (int, error) {
	f.title, f.action = title, action
	return f.pick, f.err
}

// Search records the query and what it finds, and picks the item at pick.
func (f *fakePrompt) Search(query string, search func(string) []string, action string) (string, error) {
	f.title, f.action, f.found = query, action, search(query)
	if f.err != nil {
		return "", f.err
	}
	return f.found[f.pick], nil
}

func (f *fakePrompt) Confirm(label, question string) (bool, error) {
	f.question = question
	return f.yes, f.err
}

// fakeExec records the command it was asked to run.
type fakeExec struct {
	value string
	args  []string
	calls int
	code  int
	err   error
}

func (f *fakeExec) run(value []byte, args []string) (int, error) {
	f.value, f.args = string(value), args
	f.calls++
	return f.code, f.err
}

type harness struct {
	app    *App
	store  *store.Store
	out    *bytes.Buffer
	err    *bytes.Buffer
	prompt *fakePrompt
	exec   *fakeExec
}

// newHarness gives an App on a terminal with a temp store holding keys, each
// saved as "echo <key>". The store is opened lazily, like the real one.
func newHarness(t *testing.T, keys ...string) *harness {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if err := s.Save(k, []byte("echo "+k), store.Create); err != nil {
			t.Fatal(err)
		}
	}
	h := &harness{store: s, out: &bytes.Buffer{}, err: &bytes.Buffer{}, prompt: &fakePrompt{}, exec: &fakeExec{}}
	h.app = &App{
		Open:        func() (*store.Store, error) { return s, nil },
		Stdin:       strings.NewReader(""),
		StdinTTY:    true,
		Stdout:      h.out,
		Out:         ui.NewRenderer(h.out, ui.Options{}),
		Err:         ui.NewRenderer(h.err, ui.Options{}),
		Exec:        h.exec.run,
		Prompt:      h.prompt,
		Interactive: true,
		Version:     "v1.2.3",
	}
	return h
}

// pipe makes stdin a pipe holding s, as in a script.
func (h *harness) pipe(s string) {
	h.app.Stdin = strings.NewReader(s)
	h.app.StdinTTY = false
	h.app.Interactive = false
}

func (h *harness) onTerminal() {
	h.app.Out = ui.NewRenderer(h.out, ui.Options{TTY: true})
	h.app.Err = ui.NewRenderer(h.err, ui.Options{TTY: true})
}

func (h *harness) value(t *testing.T, key string) string {
	t.Helper()
	v, err := h.store.Get(key)
	if err != nil {
		t.Fatalf("Get(%q) = %v", key, err)
	}
	return string(v)
}

func (h *harness) want(t *testing.T, args []string, code int, parts ...string) {
	t.Helper()
	if got := h.app.Main(args); got != code {
		t.Errorf("Main(%q) = %d, want %d (stderr %q)", args, got, code, h.err.String())
	}
	all := h.out.String() + h.err.String()
	for _, p := range parts {
		if !strings.Contains(all, p) {
			t.Errorf("Main(%q) output does not mention %q:\n%s", args, p, all)
		}
	}
}

func TestDispatch(t *testing.T) {
	h := newHarness(t)
	h.app.Open = func() (*store.Store, error) { return nil, errors.New("disk on fire") }
	for _, args := range [][]string{{"version"}, {"-v"}, {"help"}, {"--examples"}, {"--ack"}, {"where"}} {
		h.want(t, args, exitOK)
	}
	if !strings.Contains(h.out.String(), "pux v1.2.3\n") {
		t.Errorf("version printed %q", h.out.String())
	}
	h.want(t, []string{"-x"}, exitUsage, "Unknown flag -x")
	h.want(t, []string{"ls"}, exitFail, "Cannot open the pux folder", "disk on fire")
}

func TestHelpListsEveryCommand(t *testing.T) {
	h := newHarness(t)
	h.app.Main([]string{"help"})
	for _, c := range commands() {
		if !strings.Contains(h.out.String(), c.usage) {
			t.Errorf("help does not list %q", c.usage)
		}
	}
}

func TestHome(t *testing.T) {
	h := newHarness(t, "a", "b")
	h.want(t, nil, exitOK, "Ori:", oriHello, "2 keys saved.", "pux --examples")
	if strings.Contains(h.out.String(), "echo a") {
		t.Error("home lists commands, want only the count")
	}
	h = newHarness(t)
	h.want(t, nil, exitOK, "Nothing saved yet. Try pux new.")
}

func TestRun(t *testing.T) {
	tests := []struct {
		args  []string
		value string
		rest  []string
	}{
		{[]string{"deploy"}, "echo deploy", nil},
		{[]string{"deploy", "a", "b c"}, "echo deploy", []string{"a", "b c"}},
		{[]string{"dev"}, "echo dev-db", nil},
		{[]string{"deploy-"}, "echo deploy-prod", nil},
	}
	for _, tt := range tests {
		h := newHarness(t, "deploy", "deploy-prod", "dev-db")
		h.want(t, tt.args, exitOK)
		if h.exec.value != tt.value || !slices.Equal(h.exec.args, tt.rest) {
			t.Errorf("Main(%q) ran %q %q, want %q %q", tt.args, h.exec.value, h.exec.args, tt.value, tt.rest)
		}
	}
}

func TestRunExitCode(t *testing.T) {
	h := newHarness(t, "deploy")
	h.exec.code = 3
	h.want(t, []string{"deploy"}, 3)
	if h.err.Len() > 0 {
		t.Errorf("printed %q off a terminal, want nothing", h.err.String())
	}
	h = newHarness(t, "deploy")
	h.onTerminal()
	h.exec.code = 3
	h.want(t, []string{"deploy"}, 3, "exit   3")
}

func TestRunPicks(t *testing.T) {
	h := newHarness(t, "deploy", "deploy-prod", "dev-db")
	h.prompt.pick = 2
	h.want(t, []string{"de", "x"}, exitOK)
	if h.prompt.title != "de matches 3 keys" || h.exec.value != "echo dev-db" || !slices.Equal(h.exec.args, []string{"x"}) {
		t.Errorf("picker %q ran %q %q, want dev-db with its args", h.prompt.title, h.exec.value, h.exec.args)
	}

	h = newHarness(t, "deploy", "deploy-prod")
	h.prompt.err = ui.ErrCancelled
	h.want(t, []string{"dep"}, exitCancel)

	h = newHarness(t, "deploy", "deploy-prod")
	h.app.Interactive = false
	h.want(t, []string{"dep"}, exitFail, `"dep" matches 2 keys`, "deploy-prod", "type more of the key")
	if h.exec.calls != 0 {
		t.Error("ran a command for an ambiguous key")
	}
}

func TestRunNotFound(t *testing.T) {
	h := newHarness(t, "deploy")
	h.want(t, []string{"depoly"}, exitFail, `No key named "depoly"`, "did you mean deploy?")
	h = newHarness(t, "deploy")
	h.want(t, []string{"zzz"}, exitFail, "run pux ls to see your keys")
	h = newHarness(t, "deploy")
	h.exec.code, h.exec.err = runner.ExitNotFound, fmt.Errorf("%w: python9", runner.ErrNotFound)
	h.want(t, []string{"deploy"}, runner.ExitNotFound, "python9", "install it")
}

func TestNew(t *testing.T) {
	h := newHarness(t)
	h.want(t, []string{"new", "hi", "echo hi $1"}, exitOK, "saved", "run it with pux hi")
	if h.value(t, "hi") != "echo hi $1" || h.prompt.edit != nil {
		t.Error("one-line new did not save as given, or opened the editor")
	}

	h = newHarness(t)
	h.pipe("#!/bin/sh\necho piped\n")
	h.want(t, []string{"new", "p"}, exitOK)
	if h.value(t, "p") != "#!/bin/sh\necho piped\n" {
		t.Error("piped new did not save the exact bytes")
	}

	h = newHarness(t, "hi")
	h.prompt.key, h.prompt.value = "made", "cd app\ngit pull"
	h.want(t, []string{"new"}, exitOK, "2 lines")
	if e := h.prompt.edit; e == nil || e.Title != "new" || e.Key != "" || e.KeyFixed {
		t.Fatalf("editor opened with %+v", e)
	}
	if h.value(t, "made") != "cd app\ngit pull" {
		t.Error("editor new did not save")
	}
	for key, note := range map[string]string{"hi": "hi already exists", "ls": "key it is a pux command", "free": ""} {
		if got := h.prompt.edit.Check(key); got != note {
			t.Errorf("editor Check(%q) = %q, want %q", key, got, note)
		}
	}
}

func TestNewRefuses(t *testing.T) {
	tests := []struct {
		args  []string
		code  int
		parts []string
	}{
		{[]string{"new", "hi", "echo"}, exitFail, []string{"hi already exists", "pux edit hi"}},
		{[]string{"new", "Hi", "echo"}, exitUsage, []string{`Invalid key "Hi"`, "did you mean hi?"}},
		{[]string{"new", "ls", "echo"}, exitUsage, []string{"it is a pux command", "pick a different name"}},
		{[]string{"new", "a", "b", "c"}, exitUsage, []string{"Too many arguments", `pux new a "<cmd>"`}},
		{[]string{"new", "-f", "a"}, exitUsage, []string{"Unknown flag -f"}},
		{[]string{"new", "a", "  "}, exitUsage, []string{"Nothing to save"}},
	}
	for _, tt := range tests {
		h := newHarness(t, "hi")
		h.want(t, tt.args, tt.code, tt.parts...)
	}

	h := newHarness(t)
	h.pipe("echo")
	h.want(t, []string{"new"}, exitUsage, "Missing key")

	h = newHarness(t)
	h.app.Interactive = false
	h.want(t, []string{"new", "deploy"}, exitUsage, "needs a terminal", `pux new deploy "<cmd>"`)

	h = newHarness(t)
	h.prompt.err = ui.ErrCancelled
	h.want(t, []string{"new", "deploy"}, exitCancel)
	if h.store.Exists("deploy") || h.out.Len()+h.err.Len() > 0 {
		t.Error("a cancelled new saved or printed something")
	}
}

func TestEdit(t *testing.T) {
	h := newHarness(t, "deploy")
	h.want(t, []string{"edit", "deploy", "echo new"}, exitOK, "updated")
	if h.value(t, "deploy") != "echo new" {
		t.Error("quoted edit did not replace")
	}

	h = newHarness(t, "deploy")
	h.pipe("echo piped")
	h.want(t, []string{"edit", "deploy"}, exitOK)
	if h.value(t, "deploy") != "echo piped" {
		t.Error("piped edit did not replace")
	}

	h = newHarness(t, "deploy")
	h.prompt.value = "echo edited"
	h.want(t, []string{"edit", "dep"}, exitOK)
	if e := h.prompt.edit; e == nil || e.Key != "deploy" || e.Value != "echo deploy" || !e.KeyFixed {
		t.Fatalf("editor opened with %+v, want the resolved key locked", e)
	}
	if h.value(t, "deploy") != "echo edited" {
		t.Error("editor edit did not save")
	}

	h = newHarness(t, "deploy")
	h.want(t, []string{"edit", "dep", "x"}, exitFail, `No key named "dep"`)
	h.want(t, []string{"edit"}, exitUsage, "Missing key")
	if h.store.Exists("dep") {
		t.Error("edit created a key")
	}
}

func TestRm(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		interactive bool
		yes         bool
		removed     bool
		output      string
	}{
		{"confirmed", []string{"rm", "deploy"}, true, true, true, "removed"},
		{"declined", []string{"rm", "deploy"}, true, false, false, "kept"},
		{"-y skips the question", []string{"rm", "-y", "deploy"}, true, false, true, "removed"},
		{"scripts are not asked", []string{"rm", "deploy"}, false, false, true, "removed"},
	}
	for _, tt := range tests {
		h := newHarness(t, "deploy")
		h.app.Interactive = tt.interactive
		h.prompt.yes = tt.yes
		h.want(t, tt.args, exitOK, tt.output)
		if h.store.Exists("deploy") == tt.removed {
			t.Errorf("%s: removed = %v, want %v", tt.name, !tt.removed, tt.removed)
		}
	}

	h := newHarness(t, "deploy")
	h.want(t, []string{"rm"}, exitUsage, "Expected one key")
	h.want(t, []string{"rm", "dep"}, exitFail, `No key named "dep"`)
	h.want(t, []string{"rm", "Deploy"}, exitUsage, "did you mean deploy?")
	if h.prompt.question != "" || !h.store.Exists("deploy") {
		t.Error("rm asked about or removed a key it should not have")
	}
}

func TestLs(t *testing.T) {
	h := newHarness(t, "deploy", "deploy-prod", "build")
	h.pipe("")
	h.want(t, []string{"ls", "dep"}, exitOK)
	if h.out.String() != "deploy\ndeploy-prod\n" {
		t.Errorf("piped ls = %q, want one key per line", h.out.String())
	}

	h = newHarness(t, "build")
	h.store.Save("deploy", []byte("cd app\ngit pull\n"), store.Create)
	h.onTerminal()
	h.want(t, []string{"ls"}, exitOK, "build   echo build", "deploy  cd app  +1 line")

	h = newHarness(t)
	h.onTerminal()
	h.want(t, []string{"ls", "x"}, exitOK, `No keys start with "x"`)
	h.want(t, []string{"ls", "a", "b"}, exitUsage, "Expected at most one prefix")
}

func TestCat(t *testing.T) {
	h := newHarness(t, "deploy")
	h.want(t, []string{"cat", "dep"}, exitOK)
	if h.out.String() != "echo deploy" {
		t.Errorf("cat = %q, want the exact bytes", h.out.String())
	}
	h = newHarness(t, "deploy")
	h.onTerminal()
	h.want(t, []string{"cat", "deploy"}, exitOK)
	if h.out.String() != "echo deploy\n" {
		t.Errorf("cat on a terminal = %q, want a final newline", h.out.String())
	}
}

func TestTildePath(t *testing.T) {
	tests := []struct{ dir, home, want string }{
		{"/home/u/.config/pux", "/home/u", "~/.config/pux"},
		{"/home/user2/pux", "/home/u", "/home/user2/pux"},
		{"/srv/pux", "", "/srv/pux"},
	}
	for _, tt := range tests {
		tt.dir, tt.home, tt.want = filepath.FromSlash(tt.dir), filepath.FromSlash(tt.home), filepath.FromSlash(tt.want)
		if got := tildePath(tt.dir, tt.home); got != tt.want {
			t.Errorf("tildePath(%q, %q) = %q, want %q", tt.dir, tt.home, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{0: "0ms", 999 * time.Millisecond: "999ms", 12345 * time.Millisecond: "12.3s"} {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestWhere(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "never-made")
	t.Setenv("PUX_DIR", dir)
	h := newHarness(t)
	h.want(t, []string{"where"}, exitOK)
	lines := strings.Split(strings.TrimSpace(h.out.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "binary") || strings.TrimSpace(lines[1]) != "" || !strings.Contains(lines[2], "store") {
		t.Errorf("where = %q, want binary, a blank line, then store", lines)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("where created the store folder")
	}
}

func TestFind(t *testing.T) {
	h := newHarness(t, "build", "deploy", "deploy-prod", "prod-db")
	h.prompt.pick = 1
	h.want(t, []string{"find", "Prod"}, exitOK)
	if !slices.Equal(h.prompt.found, []string{"prod-db", "deploy-prod"}) || h.prompt.action != "run" {
		t.Errorf("search found %v with action %q, want prod-db first", h.prompt.found, h.prompt.action)
	}
	if h.exec.value != "echo deploy-prod" || len(h.exec.args) != 0 {
		t.Errorf("ran %q %q, want the pick with no args", h.exec.value, h.exec.args)
	}

	h = newHarness(t, "build", "deploy")
	h.prompt.err = ui.ErrCancelled
	h.want(t, []string{"find"}, exitCancel)
	if h.exec.calls != 0 {
		t.Error("a cancelled find ran a command")
	}

	h = newHarness(t, "build", "deploy", "deploy-prod", "prod-db")
	h.pipe("")
	h.want(t, []string{"find", "prod"}, exitOK)
	if h.out.String() != "prod-db\ndeploy-prod\n" || h.prompt.found != nil {
		t.Errorf("piped find = %q, want the matches one per line and no prompt", h.out.String())
	}

	h = newHarness(t)
	h.want(t, []string{"find"}, exitOK, "No keys yet")
	h.want(t, []string{"find", "a", "b"}, exitUsage, "Expected at most one search")
	h.want(t, []string{"new", "find", "echo"}, exitUsage, "it is a pux command")
}
