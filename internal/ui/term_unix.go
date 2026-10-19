// internal/ui/term_unix.go

//go:build !windows

package ui

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// enableVT reports true, since terminals outside Windows understand escape codes already.
func enableVT(*os.File) bool { return true }

// waitInput reports whether f has input to read within timeout.
func waitInput(f *os.File, timeout time.Duration) (bool, error) {
	fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(fds, int(timeout.Milliseconds()))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return n > 0, err
	}
}
