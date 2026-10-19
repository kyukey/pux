// internal/cli/commands.go

package cli

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/kyukey/pux/internal/store"
	"github.com/kyukey/pux/internal/ui"
)

// maxKeyColumn stops one long key from pushing every preview out of view in ls.
const maxKeyColumn = 24

// run executes a saved command with args. A partial key works, and when it
// matches several keys a terminal gets a picker.
func (a *App) run(input string, args []string) int {
	key, code := a.resolve(input, "run")
	if key == "" {
		return code
	}
	return a.runKey(key, args)
}

// runKey executes the command saved under an exact key.
func (a *App) runKey(key string, args []string) int {
	value, err := a.Store.Get(key)
	if err != nil {
		return a.fail(err, key)
	}
	start := time.Now()
	code, err := a.Exec(value, args)
	switch {
	case err != nil:
		a.fail(err, key)
	case code != exitOK:
		a.runSummary(code, time.Since(start))
	}
	return code
}

// runSummary marks a failed run on a terminal. A successful run stays silent,
// so the only output is the command's own.
func (a *App) runSummary(code int, took time.Duration) {
	if !a.Err.Options().TTY {
		return
	}
	dot := " " + a.Err.Symbols().Dot + " "
	a.Err.Print(a.Err.Block(ui.Bad, "exit", strconv.Itoa(code)+a.Err.Dim(dot+formatDuration(took))))
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
	}
	return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + "s"
}

// newCommand saves a new command from a quoted argument, from piped input, or
// from the editor when neither is given. The key is checked first, so nothing
// is read or typed for a key that cannot be saved.
func (a *App) newCommand(args []string) int {
	if code := a.checkArgs("new", args, 2); code != exitOK {
		return code
	}
	if len(args) == 0 {
		if !a.StdinTTY {
			return a.usage("Missing key", "name the command: cat script.sh | pux new <key>")
		}
		return a.save(store.Create, "", nil, ui.Command{Title: "new", Check: a.taken})
	}
	key := args[0]
	if err := validKey(key); err != nil {
		return a.fail(err, key)
	}
	if a.Store.Exists(key) {
		return a.fail(store.ErrExists, key)
	}
	return a.save(store.Create, key, args[1:], ui.Command{Title: "new", Key: key, Check: a.taken})
}

// edit changes a saved command. The editor accepts a partial key, since it shows
// which key it opened. A quoted command or a pipe replaces without showing
// anything, so those need the exact key.
func (a *App) edit(args []string) int {
	if code := a.checkArgs("edit", args, 2); code != exitOK {
		return code
	}
	if len(args) == 0 {
		return a.usage("Missing key", `usage: pux edit <key> ["<cmd>"]`)
	}
	if len(args) == 2 || !a.StdinTTY || !a.Interactive {
		return a.save(store.Replace, args[0], args[1:], ui.Command{Title: "edit"})
	}
	key, code := a.resolve(args[0], "edit")
	if key == "" {
		return code
	}
	old, err := a.Store.Get(key)
	if err != nil {
		return a.fail(err, key)
	}
	return a.save(store.Replace, key, nil, ui.Command{Title: "edit", Key: key, Value: string(old), KeyFixed: true})
}

// save is the flow new and edit share. The command comes from a quoted argument,
// then a pipe, then the editor opened with c, and an empty one is never saved.
func (a *App) save(mode store.Mode, key string, quoted []string, c ui.Command) int {
	var value []byte
	switch {
	case len(quoted) == 1:
		value = []byte(quoted[0])
	case !a.StdinTTY:
		v, err := io.ReadAll(a.Stdin)
		if err != nil {
			return a.fail(err, key)
		}
		value = v
	case !a.Interactive:
		return a.needsTerminal(c.Title, key)
	default:
		k, v, err := a.Prompt.Edit(c)
		if err != nil {
			return a.fail(err, key)
		}
		key, value = k, []byte(v)
	}

	if len(bytes.TrimSpace(value)) == 0 {
		return a.usage("Nothing to save", fmt.Sprintf(`pass the command in quotes, or pipe it: cat script.sh | pux %s %s`, c.Title, key))
	}
	if err := a.Store.Save(key, value, mode); err != nil {
		return a.fail(err, key)
	}
	label := "saved"
	if mode == store.Replace {
		label = "updated"
	}
	a.Out.Done(label, key+"  "+a.Out.Dim(ui.Plural(len(ui.Lines(value)), "line")), "run it with pux "+key)
	return exitOK
}

// rm deletes a command by its exact key, since a partial match is never enough
// to delete something. On a terminal it asks first, unless -y says not to. A
// broken link is not a key, so it is removed without asking.
func (a *App) rm(args []string) int {
	yes := false
	for len(args) > 0 && (args[0] == "-y" || args[0] == "--yes") {
		yes = true
		args = args[1:]
	}
	if len(args) != 1 {
		return a.usage("Expected one key", "usage: pux rm [-y] <key>")
	}
	key := args[0]
	if err := store.Validate(key); err != nil {
		return a.fail(err, key)
	}
	if a.Interactive && !yes && a.Store.Exists(key) {
		ok, err := a.Prompt.Confirm("delete", "Delete "+key+"?")
		if err != nil {
			return a.fail(err, key)
		}
		if !ok {
			a.Out.Info("kept", key, "")
			return exitOK
		}
	}
	if err := a.Store.Remove(key); err != nil {
		return a.fail(err, key)
	}
	a.Out.Done("removed", key, "")
	return exitOK
}

// ls lists saved keys. On a terminal each key shows the first line of its command,
// and piped output is one plain key per line, so scripts can read it.
func (a *App) ls(args []string) int {
	if len(args) > 1 {
		return a.usage("Expected at most one prefix", "usage: pux ls [prefix]")
	}
	prefix := ""
	if len(args) == 1 {
		prefix = args[0]
	}
	keys, err := a.Store.Match(prefix)
	if err != nil {
		return a.fail(err, "")
	}

	r := a.Out
	tty := r.Options().TTY
	switch {
	case !tty:
		for _, k := range keys {
			fmt.Fprintln(a.Stdout, k)
		}
	case len(keys) == 0 && prefix != "":
		r.Info("keys", fmt.Sprintf("No keys start with %q", prefix), "run pux ls to see all keys")
	case len(keys) == 0:
		r.Info("keys", "No keys yet", `save one with pux new <key> "<cmd>"`)
	default:
		col := 0
		for _, k := range keys {
			col = max(col, len(k))
		}
		col = min(col, maxKeyColumn)
		rows := make([]string, len(keys))
		for i, k := range keys {
			rows[i] = a.keyRow(k, col)
		}
		r.Print(r.Block(ui.Label, "keys", rows...))
	}
	return exitOK
}

// find searches keys by any part of their name. On a terminal the list narrows
// as the user types and Enter runs the pick. Elsewhere it prints the matches
// one per line, like ls, so scripts can read them.
func (a *App) find(args []string) int {
	if len(args) > 1 {
		return a.usage("Expected at most one search", "usage: pux find [text]")
	}
	text := ""
	if len(args) == 1 {
		text = args[0]
	}
	keys, err := a.Store.List()
	if err != nil {
		return a.fail(err, "")
	}
	search := func(t string) []string { return store.Search(keys, t) }
	switch {
	case !a.Interactive:
		for _, k := range search(text) {
			fmt.Fprintln(a.Stdout, k)
		}
		return exitOK
	case len(keys) == 0:
		a.Out.Info("keys", "No keys yet", `save one with pux new <key> "<cmd>"`)
		return exitOK
	}
	key, err := a.Prompt.Search(text, search, "run")
	if err != nil {
		return a.fail(err, "")
	}
	return a.runKey(key, nil)
}

// keyRow shows a key, the first line of its command, and how many more lines it has.
func (a *App) keyRow(key string, col int) string {
	r := a.Out
	pad := strings.Repeat(" ", max(col-len(key), 0)) + "  "
	value, err := a.Store.Get(key)
	if err != nil {
		return key + pad + r.Dim("cannot be read")
	}
	line := ui.Clean(ui.FirstLine(value))
	more := ""
	if n := len(ui.Lines(value)) - 1; n > 0 {
		more = "  +" + ui.Plural(n, "line")
	}
	room := r.TextWidth() - len(key) - len(pad) - ui.Width(more)
	if room < 8 || line == "" {
		return key
	}
	return key + pad + r.Dim(ui.Truncate(line, room, r.Symbols().Ellipsis)+more)
}

// cat prints a command without running it. The bytes are exact, so the output
// can be piped, and a terminal also gets a final newline for the shell prompt.
func (a *App) cat(args []string) int {
	if len(args) != 1 {
		return a.usage("Expected one key", "usage: pux cat <key>")
	}
	key, code := a.resolve(args[0], "print")
	if key == "" {
		return code
	}
	value, err := a.Store.Get(key)
	if err != nil {
		return a.fail(err, key)
	}
	a.Stdout.Write(value)
	if a.Out.Options().TTY && len(value) > 0 && !bytes.HasSuffix(value, []byte("\n")) {
		io.WriteString(a.Stdout, "\n")
	}
	return exitOK
}
