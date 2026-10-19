// internal/runner/shell_windows.go

//go:build windows

package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// caughtSignals keep pux alive while a command runs. Windows sends Ctrl+C and a
// closing console to every process attached to it, so nothing needs forwarding.
var caughtSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// errLineBreak is returned for an argument cmd would cut off at its line break.
var errLineBreak = errors.New("cmd cannot pass an argument with a line break")

func forward(*os.Process, os.Signal) {}

// terminate stops a cancelled command. Windows has no polite stop signal for it.
func terminate(p *os.Process) error {
	return p.Kill()
}

// plainCommand runs value as a temp .cmd file, since cmd /c cannot take
// multi-line text or %1 inline. Echo is turned off so lines are not printed.
// The command line is built here because Go quotes arguments for normal
// programs, and cmd reads & | < > ^ % differently.
func plainCommand(ctx context.Context, value []byte, args []string) (*exec.Cmd, func(), error) {
	for _, a := range args {
		if strings.ContainsAny(a, "\r\n") {
			return nil, nil, errLineBreak
		}
	}
	script := append([]byte("@echo off\r\n"), toCRLF(value)...)
	path, cleanup, err := writeTemp(script, "pux-*.cmd")
	if err != nil {
		return nil, nil, err
	}
	shell := os.Getenv("ComSpec")
	if shell == "" {
		shell = "cmd.exe"
	}
	cmd := exec.CommandContext(ctx, shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdLine(shell, path, args)}
	return cmd, cleanup, nil
}

// cmdLine builds "shell" /d /s /c ""script" "arg" ...". With /s, cmd drops only
// the outer pair of quotes, so every inner quoted part stays as it is.
func cmdLine(shell, script string, args []string) string {
	var b strings.Builder
	b.WriteString(`"` + shell + `" /d /s /c "`)
	b.WriteString(cmdQuote(script))
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(cmdQuote(a))
	}
	b.WriteByte('"')
	return b.String()
}

// cmdQuote wraps s in quotes, so & | < > ^ are taken literally, and doubles
// any quote inside it. A % is split as %%cd:~,% because cmd expands %NAME%
// even inside quotes, and that expands back to a single %.
func cmdQuote(s string) string {
	s = strings.ReplaceAll(s, `"`, `""`)
	s = strings.ReplaceAll(s, "%", "%%cd:~,%")
	return `"` + s + `"`
}

// toCRLF normalizes line endings, because labels and goto in batch files break with plain LF.
func toCRLF(b []byte) []byte {
	return bytes.ReplaceAll(toLF(b), []byte("\n"), []byte("\r\n"))
}

func signalStatus(*os.ProcessState) int { return 1 }
