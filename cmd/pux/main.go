// cmd/pux/main.go

package main

import (
	"os"
	"runtime/debug"
	"strings"

	"github.com/kyukey/pux/internal/cli"
	"github.com/kyukey/pux/internal/runner"
	"github.com/kyukey/pux/internal/store"
	"github.com/kyukey/pux/internal/ui"
)

// version can be set at build time with -ldflags "-X main.version=1.0.0".
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	out := ui.Detect(os.Stdout)
	stdinTTY := ui.IsTerminal(os.Stdin)
	app := &cli.App{
		Open:        openStore,
		Stdin:       os.Stdin,
		StdinTTY:    stdinTTY,
		Stdout:      os.Stdout,
		Out:         ui.NewRenderer(os.Stdout, out),
		Err:         ui.NewRenderer(os.Stderr, ui.Detect(os.Stderr)),
		Exec:        runner.Run,
		Prompt:      cli.Terminal(out),
		Interactive: stdinTTY && out.TTY && out.VT,
		Version:     buildVersion(),
	}
	return app.Main(os.Args[1:])
}

func openStore() (*store.Store, error) {
	dir, err := store.DefaultDir()
	if err != nil {
		return nil, err
	}
	return store.Open(dir)
}

// buildVersion falls back to the module version, so go install builds report a real one.
// The leading v is dropped to match release builds.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return version
}
