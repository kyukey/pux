// internal/runner/shell_unix.go

//go:build !windows

package runner

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// caughtSignals keep pux alive while a command runs, so it can clean up and report.
var caughtSignals = []os.Signal{os.Interrupt, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP}

// forward passes on signals that were sent to pux alone. Ctrl+C and Ctrl+\
// already reach the child from the terminal, so they are not sent twice.
func forward(p *os.Process, s os.Signal) {
	if s == syscall.SIGTERM || s == syscall.SIGHUP {
		p.Signal(s)
	}
}

// terminate asks a cancelled command to stop, giving it the chance to clean up.
func terminate(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}

// plainCommand runs value with sh, with LF line endings since sh reads a \r as
// part of the command. The extra "pux" becomes $0, so args start at $1.
func plainCommand(ctx context.Context, value []byte, args []string) (*exec.Cmd, func(), error) {
	argv := append([]string{"-c", string(toLF(value)), "pux"}, args...)
	return exec.CommandContext(ctx, "/bin/sh", argv...), func() {}, nil
}

// signalStatus reports a process killed by a signal as 128 plus the signal number, like a shell.
func signalStatus(ps *os.ProcessState) int {
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return 1
}
