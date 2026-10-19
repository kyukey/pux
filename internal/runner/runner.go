// internal/runner/runner.go

package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ExitNotFound follows the shell convention for a missing command or interpreter.
const ExitNotFound = 127

// ErrNotFound is returned when a shebang names an interpreter that is not installed.
var ErrNotFound = errors.New("interpreter not found")

// killDelay is how long a cancelled command gets to exit before it is killed.
const killDelay = 5 * time.Second

// scriptExt names temp files for interpreters that refuse a file without the right extension.
var scriptExt = map[string]string{"pwsh": ".ps1", "powershell": ".ps1"}

// Run executes value with args on the current terminal and returns its exit code.
func Run(value []byte, args []string) (int, error) {
	return Exec(context.Background(), value, args, os.Stdin, os.Stdout, os.Stderr)
}

// Exec executes value with args and returns its exit code. A value starting with
// a shebang runs with that interpreter, anything else with the system shell.
// Cancelling ctx asks the command to stop, and kills it after killDelay.
func Exec(ctx context.Context, value []byte, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cmd, cleanup, err := command(ctx, value, args)
	if errors.Is(err, ErrNotFound) {
		return ExitNotFound, err
	}
	if err != nil {
		return 1, err
	}
	defer cleanup()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	return wait(cmd)
}

// command builds the process for value without starting it.
// The returned cleanup removes any temp file and must always be called.
func command(ctx context.Context, value []byte, args []string) (*exec.Cmd, func(), error) {
	var (
		cmd     *exec.Cmd
		cleanup func()
		err     error
	)
	if interp, ok := parseShebang(value); ok {
		cmd, cleanup, err = shebangCommand(ctx, interp, value, args)
	} else {
		cmd, cleanup, err = plainCommand(ctx, value, args)
	}
	if err != nil {
		return nil, nil, err
	}
	cmd.Cancel = func() error { return terminate(cmd.Process) }
	cmd.WaitDelay = killDelay
	return cmd, cleanup, nil
}

// wait starts cmd and waits for it to exit. Catching signals keeps pux alive until
// then, so the temp file is always removed and the exit code always reported.
// Notify is used instead of Ignore because an ignored signal is inherited by the child.
func wait(cmd *exec.Cmd) (int, error) {
	sig := make(chan os.Signal, len(caughtSignals))
	signal.Notify(sig, caughtSignals...)
	defer signal.Stop(sig)

	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return ExitNotFound, err
		}
		return 1, err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case s := <-sig:
				forward(cmd.Process, s)
			case <-done:
				return
			}
		}
	}()

	err := cmd.Wait()
	if cmd.ProcessState == nil {
		return 1, err
	}
	if code := cmd.ProcessState.ExitCode(); code >= 0 {
		return code, nil
	}
	return signalStatus(cmd.ProcessState), nil
}

// parseShebang reads a first line like "#!/usr/bin/env python3 -u" and returns
// the interpreter followed by its flags. A leading env is dropped, along with env -S.
func parseShebang(value []byte) ([]string, bool) {
	rest, ok := bytes.CutPrefix(value, []byte("#!"))
	if !ok {
		return nil, false
	}
	line, _, _ := bytes.Cut(rest, []byte("\n"))
	fields := strings.Fields(string(bytes.TrimSuffix(line, []byte("\r"))))
	if len(fields) > 0 && filepath.Base(fields[0]) == "env" {
		fields = fields[1:]
		if len(fields) > 0 && fields[0] == "-S" {
			fields = fields[1:]
		}
	}
	return fields, len(fields) > 0
}

// shebangCommand runs value as a temp script file with the interpreter it names.
// The script gets LF line endings, which every interpreter reads on every OS.
func shebangCommand(ctx context.Context, interp []string, value []byte, args []string) (*exec.Cmd, func(), error) {
	path, err := lookInterpreter(interp[0])
	if err != nil {
		return nil, nil, err
	}
	script, cleanup, err := writeTemp(toLF(value), tempPattern(interp[0]))
	if err != nil {
		return nil, nil, err
	}
	return exec.CommandContext(ctx, path, slices.Concat(interp[1:], []string{script}, args)...), cleanup, nil
}

// tempPattern gives the temp file the extension its interpreter needs, if any.
func tempPattern(interp string) string {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(interp)), ".exe")
	return "pux-*" + scriptExt[name]
}

// lookInterpreter falls back to the bare name, so /usr/bin/python3 still works
// where that exact path does not exist.
func lookInterpreter(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	if base := filepath.Base(name); base != name {
		if path, err := exec.LookPath(base); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrNotFound, name)
}

// toLF turns CRLF line endings into LF, so a script saved on Windows runs in sh
// and bash. A lone \r is left alone, since a script may mean it.
func toLF(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}

// writeTemp saves data to a private temp file and returns its path.
func writeTemp(data []byte, pattern string) (string, func(), error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.Remove(f.Name()) }
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return f.Name(), cleanup, nil
}
