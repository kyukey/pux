// internal/cli/help.go

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/kyukey/pux/internal/store"
	"github.com/kyukey/pux/internal/ui"
)

// Ori is the pux mascot, shown only on the home screen. Change the art or the hello here.
var (
	oriArt   = []string{" ,_,", "(o,o)", "/)_)"}
	oriHello = "Hi, I'm Ori. I remember your commands."
)

const editorNote = `leave out "<cmd>" on new or edit to type or paste it below`

// depNotes describe the Go modules pux is built with. The list itself comes from
// the binary, so a dependency that is added or dropped shows up on its own.
var depNotes = map[string]string{
	"golang.org/x/term": "raw mode and terminal size",
	"golang.org/x/sys":  "terminal input and colour on every OS",
}

// helpRow is one line of help: what to type, and a short note on what it does.
type helpRow struct{ text, note string }

// helpSection is a badge label with its rows, and an optional note under them.
type helpSection struct {
	label string
	rows  []helpRow
	note  string
}

// usageRows lists running a key and every command that is not a flag, from the command table.
func usageRows() []helpRow {
	rows := []helpRow{{"pux", "a short overview"}, {"pux <key> [args...]", "run a saved command"}}
	for _, c := range commands() {
		if !strings.HasPrefix(c.names[0], "-") {
			rows = append(rows, helpRow{c.usage, c.about})
		}
	}
	return rows
}

func flagRows() []helpRow {
	var rows []helpRow
	for _, c := range commands() {
		if strings.HasPrefix(c.names[0], "-") {
			rows = append(rows, helpRow{c.usage, c.about})
		}
	}
	return append(rows, helpRow{"-y, --yes", "delete without asking (rm)"})
}

func (a *App) help([]string) int {
	sections := []helpSection{
		{label: "usage", rows: usageRows(), note: editorNote},
		{label: "flags", rows: flagRows()},
		{label: "env", rows: []helpRow{
			{"PUX_DIR", "keep commands in another folder"},
			{"PUX_ASCII=1", "plain symbols for old consoles"},
			{"NO_COLOR=1", "turn colour off"},
		}},
		{label: "exit", rows: []helpRow{
			{"0", "done"},
			{"1", "key not found, ambiguous or taken"},
			{"2", "wrong usage or a bad key name"},
			{"127", "interpreter not found"},
			{"130", "cancelled"},
			{"other", "your command's own exit code"},
		}},
		{label: "store", rows: []helpRow{{a.storePath(), ""}}},
	}
	a.print(append(a.title(), a.layout(noteColumn(sections), sections...)...))
	return exitOK
}

// home is bare pux: Ori with the key count, the usage, where commands live and
// where to read more. It never lists the keys themselves, pux ls does that.
func (a *App) home() int {
	keys, err := a.Store.List()
	if err != nil {
		return a.fail(err, "")
	}
	count := "Nothing saved yet. Try pux new."
	if len(keys) > 0 {
		count = ui.Plural(len(keys), "key") + " saved."
	}
	lines := append(a.title(), a.Out.Mascot(oriArt, []string{"Ori:", oriHello, a.Out.Dim(count)})...)
	lines = append(lines, "")
	sections := []helpSection{
		{label: "usage", rows: usageRows(), note: editorNote},
		{label: "store", rows: []helpRow{{a.storePath(), ""}}},
		{label: "more", rows: []helpRow{{"pux --examples   pux --ack   pux help", ""}}},
	}
	a.print(append(lines, a.layout(noteColumn(sections), sections...)...))
	return exitOK
}

// examples shows pux in use, with a small picture of the editor.
func (a *App) examples([]string) int {
	before := []helpSection{
		{label: "one line", rows: oneLiner()},
		{label: "scripts", rows: []helpRow{
			{"cat setup.sh | pux new setup", "save a whole script"},
			{"pux new deploy", "type or paste it in the editor below"},
			{"pux edit deploy", "change it the same way"},
		}},
	}
	after := []helpSection{
		{label: "shebang", rows: []helpRow{
			{"#!/usr/bin/env python3", "a first line like this picks the interpreter"},
			{"no shebang", "runs with sh, or cmd on Windows"},
		}},
		{label: "keys", rows: []helpRow{
			{"pux de", "a unique start of a key is enough"},
			{"pux ls dep", "list the keys that start with dep"},
			{"pux find prod", "search by any part of a name, then run it"},
		}},
	}
	r := a.Out
	col := noteColumn(append(append([]helpSection{}, before...), after...))
	num := func(n string) string { return r.Dim(n) + "  " }

	lines := append(a.title(), a.layout(col, before...)...)
	lines = append(lines, r.Block(ui.Label, "editor",
		"pux new deploy",
		r.Mark(true)+" "+num(" 1")+"cd app",
		"  "+num(" 2")+"git pull",
		"",
		r.Hints("ctrl+d save", "esc cancel"),
	)...)
	lines = append(lines, "")
	a.print(append(lines, a.layout(col, after...)...))
	return exitOK
}

func oneLiner() []helpRow {
	if runtime.GOOS == "windows" {
		return []helpRow{
			{`pux new hi "echo hi %1"`, "cmd reads the first argument as %1"},
			{"pux hi there", "there becomes %1"},
		}
	}
	return []helpRow{
		{`pux new hi 'echo hi $1'`, "single quotes keep $1 for later"},
		{"pux hi there", "there becomes $1"},
	}
}

// ack thanks the language, the modules and the people pux is built with.
func (a *App) ack([]string) int {
	rows := []helpRow{{"Go", "the language and standard library"}}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			rows = append(rows, helpRow{d.Path, depNotes[d.Path]})
		}
	}
	rows = append(rows,
		helpRow{"Astro and clack", "inspiration for the CLI design"},
		helpRow{"Claude and Gemini", "development partners"},
	)
	sections := []helpSection{{label: "thanks", rows: rows}}
	a.print(append(a.title(), a.layout(noteColumn(sections), sections...)...))
	return exitOK
}

func (a *App) version([]string) int {
	fmt.Fprintln(a.Stdout, "pux", a.Version)
	return exitOK
}

// where shows the pux binary and the store folder, so people know what to
// delete when they uninstall. It never creates the folder.
func (a *App) where([]string) int {
	bin, err := os.Executable()
	if err == nil {
		bin, err = filepath.EvalSymlinks(bin)
	}
	if err != nil {
		bin = "unknown"
	}
	r := a.Out
	lines := append(r.Block(ui.Label, "binary", bin), "")
	r.Print(append(lines, r.Block(ui.Label, "store", a.storePath())...))
	return exitOK
}

// title is the first line of every overview: the pux badge, the version and the tagline.
func (a *App) title() []string {
	return []string{"", a.Out.Header("pux", a.Version, "Save commands, run them by name"), ""}
}

// layout draws sections as badge blocks, with every note starting at col.
func (a *App) layout(col int, sections ...helpSection) []string {
	r := a.Out
	var lines []string
	for _, s := range sections {
		text := make([]string, 0, len(s.rows)+2)
		for _, row := range s.rows {
			if row.note == "" {
				text = append(text, row.text)
				continue
			}
			text = append(text, ui.Pad(row.text, col)+r.Dim(row.note))
		}
		if s.note != "" {
			text = append(text, "", r.Dim(s.note))
		}
		lines = append(lines, r.Block(ui.Label, s.label, text...)...)
		lines = append(lines, "")
	}
	return lines
}

// noteColumn lines up every note in one column, three spaces past the widest row.
// Rows without a note, like the store path, do not push the column out.
func noteColumn(sections []helpSection) int {
	widest := 0
	for _, s := range sections {
		for _, row := range s.rows {
			if row.note != "" {
				widest = max(widest, ui.Width(row.text))
			}
		}
	}
	return widest + 3
}

func (a *App) print(lines []string) {
	fmt.Fprintln(a.Stdout, strings.Join(lines, "\n"))
}

// storePath is the store folder as people would type it: with ~ for the home
// folder on macOS and Linux, and in full on Windows, where ~ is not a habit.
func (a *App) storePath() string {
	dir, err := store.DefaultDir()
	if a.Store != nil {
		dir, err = a.Store.Dir(), nil
	}
	if err != nil {
		return "unknown"
	}
	if runtime.GOOS == "windows" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return tildePath(dir, home)
}

func tildePath(dir, home string) string {
	if home == "" {
		return dir
	}
	rest, ok := strings.CutPrefix(dir, home)
	if !ok || (rest != "" && rest[0] != filepath.Separator) {
		return dir
	}
	return "~" + rest
}
