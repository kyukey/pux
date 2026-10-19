// internal/cli/cli.go

package cli

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/kyukey/pux/internal/runner"
	"github.com/kyukey/pux/internal/store"
	"github.com/kyukey/pux/internal/ui"
)

// Exit codes for pux itself. A command that runs passes its own code through.
const (
	exitOK     = 0
	exitFail   = 1
	exitUsage  = 2
	exitCancel = 130
)

// App holds everything a pux command needs, so tests can swap in buffers, a
// fake runner and a fake prompter. Store is opened through Open the first time
// a command needs it, so help and version never create the folder. Interactive
// is true when prompts can run, on a terminal that understands escape codes.
type App struct {
	Store       *store.Store
	Open        func() (*store.Store, error)
	Stdin       io.Reader
	StdinTTY    bool
	Stdout      io.Writer
	Out         *ui.Renderer
	Err         *ui.Renderer
	Exec        func(value []byte, args []string) (int, error)
	Prompt      Prompter
	Interactive bool
	Version     string
}

// Prompter asks the user things on the terminal. Terminal draws real prompts,
// and tests use a fake.
type Prompter interface {
	Edit(c ui.Command) (key, value string, err error)
	Select(title string, items []string, action string) (int, error)
	Search(query string, search func(text string) []string, action string) (string, error)
	Confirm(label, question string) (bool, error)
}

// Terminal returns the Prompter that draws on the terminal described by opts.
func Terminal(opts ui.Options) Prompter { return terminal{opts} }

type terminal struct{ opts ui.Options }

func (t terminal) Edit(c ui.Command) (string, string, error) { return ui.Edit(t.opts, c) }

func (t terminal) Select(title string, items []string, action string) (int, error) {
	return ui.Select(t.opts, title, items, action)
}

func (t terminal) Search(query string, search func(text string) []string, action string) (string, error) {
	return ui.Search(t.opts, query, search, action)
}

func (t terminal) Confirm(label, question string) (bool, error) {
	return ui.Confirm(t.opts, label, question)
}

// command is one entry in the command table. The first name is the one shown in
// help, and names starting with a dash are listed as flags.
type command struct {
	names []string
	usage string
	about string
	store bool
	run   func(a *App, args []string) int
}

// commands is the one place pux commands are named. Dispatch, help and the
// reserved key names all come from it.
func commands() []command {
	return []command{
		{[]string{"new"}, `pux new [key] ["<cmd>"]`, "save a new command", true, (*App).newCommand},
		{[]string{"edit"}, `pux edit <key> ["<cmd>"]`, "change a command", true, (*App).edit},
		{[]string{"rm"}, "pux rm [-y] <key>", "delete a command", true, (*App).rm},
		{[]string{"ls"}, "pux ls [prefix]", "list saved commands", true, (*App).ls},
		{[]string{"find"}, "pux find [text]", "search your commands and run one", true, (*App).find},
		{[]string{"cat"}, "pux cat <key>", "print a command without running it", true, (*App).cat},
		{[]string{"where"}, "pux where", "show where pux and your commands live", false, (*App).where},
		{[]string{"help", "-h", "--help"}, "pux help", "show this help", false, (*App).help},
		{[]string{"version", "-v", "--version"}, "pux version", "show the version", false, (*App).version},
		{[]string{"--examples"}, "--examples", "show examples", false, (*App).examples},
		{[]string{"--ack"}, "--ack", "show who and what pux is built with", false, (*App).ack},
	}
}

// Main runs one pux invocation and returns the process exit code. Anything that
// is not a command or flag is a key to run, and the args after it are its own.
func (a *App) Main(args []string) int {
	if len(args) == 0 {
		if !a.openStore() {
			return exitFail
		}
		return a.home()
	}
	name, rest := args[0], args[1:]
	for _, c := range commands() {
		if slices.Contains(c.names, name) {
			if c.store && !a.openStore() {
				return exitFail
			}
			return c.run(a, rest)
		}
	}
	if strings.HasPrefix(name, "-") {
		return a.usage("Unknown flag "+name, "run pux help to see what pux can do")
	}
	if !a.openStore() {
		return exitFail
	}
	return a.run(name, rest)
}

func (a *App) openStore() bool {
	if a.Store != nil {
		return true
	}
	s, err := a.Open()
	if err != nil {
		a.Err.Error("Cannot open the pux folder", err.Error())
		return false
	}
	a.Store = s
	return true
}

// validKey checks key like the store does, and also refuses the name of a pux
// command, since such a key could never be run.
func validKey(key string) error {
	if err := store.Validate(key); err != nil {
		return err
	}
	for _, c := range commands() {
		if c.names[0] == key {
			return &store.KeyError{Key: key, Reason: "it is a pux command"}
		}
	}
	return nil
}

// taken is the editor's live check for a key that cannot be used.
func (a *App) taken(key string) string {
	var ke *store.KeyError
	switch {
	case errors.As(validKey(key), &ke):
		return "key " + ke.Reason
	case a.Store.Exists(key):
		return key + " already exists"
	}
	return ""
}

// resolve turns a full or partial key into one key. When several keys match, it
// asks which one on a terminal and lists them otherwise. On failure it reports
// the problem and returns an empty key with the exit code to use.
func (a *App) resolve(input, action string) (string, int) {
	key, matches, err := a.Store.Resolve(input)
	switch {
	case err == nil:
		return key, exitOK
	case !errors.Is(err, store.ErrAmbiguous):
		return "", a.fail(err, input)
	case !a.Interactive:
		lines := append([]string{fmt.Sprintf("%q matches %d keys", input, len(matches))}, matches...)
		lines = append(lines, a.Err.Dim("type more of the key to pick one"))
		a.Err.Print(a.Err.Block(ui.Bad, "error", lines...))
		return "", exitFail
	}
	i, err := a.Prompt.Select(fmt.Sprintf("%s matches %d keys", input, len(matches)), matches, action)
	if err != nil {
		return "", a.fail(err, input)
	}
	return matches[i], exitOK
}

// usage reports a mistake in how pux was called.
func (a *App) usage(msg, hint string) int {
	a.Err.Error(msg, hint)
	return exitUsage
}

// fail turns an error into a message with a next step. Leaving a prompt is not
// a failure worth a message, so it only sets the exit code.
func (a *App) fail(err error, key string) int {
	var ke *store.KeyError
	switch {
	case errors.Is(err, ui.ErrCancelled):
		return exitCancel
	case errors.As(err, &ke):
		hint := "pick a different name"
		if s := store.Suggest(ke.Key); s != "" && s != ke.Key && validKey(s) == nil {
			hint = "did you mean " + s + "?"
		}
		return a.usage(fmt.Sprintf("Invalid key %q: %s", ke.Key, ke.Reason), hint)
	case errors.Is(err, store.ErrNotFound):
		a.Err.Error(fmt.Sprintf("No key named %q", key), a.closestHint(key))
	case errors.Is(err, store.ErrExists):
		a.Err.Error(key+" already exists", "change it with pux edit "+key)
	case errors.Is(err, runner.ErrNotFound):
		a.Err.Error(err.Error(), "install it, or change the first line of the saved command")
	default:
		a.Err.Error(err.Error(), "")
	}
	return exitFail
}

func (a *App) closestHint(input string) string {
	if keys, err := a.Store.List(); err == nil {
		if k, ok := store.Closest(input, keys); ok {
			return "did you mean " + k + "?"
		}
	}
	return "run pux ls to see your keys"
}

// needsTerminal explains that the editor cannot open here, and what works in a script.
func (a *App) needsTerminal(name, key string) int {
	if key == "" {
		key = "<key>"
	}
	return a.usage("pux "+name+" needs a terminal to open the editor", fmt.Sprintf(`in scripts use pux %s %s "<cmd>"`, name, key))
}

// checkArgs refuses a flag and more than limit arguments, with a hint built
// from how the command is used.
func (a *App) checkArgs(name string, args []string, limit int) int {
	switch {
	case len(args) > 0 && strings.HasPrefix(args[0], "-"):
		return a.usage("Unknown flag "+args[0], "run pux help to see what pux "+name+" takes")
	case len(args) > limit:
		return a.usage("Too many arguments", fmt.Sprintf(`wrap the command in quotes: pux %s %s "<cmd>"`, name, args[0]))
	}
	return exitOK
}
