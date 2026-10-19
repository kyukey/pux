// internal/ui/term_windows.go

//go:build windows

package ui

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// enableVT turns on escape code handling in the Windows console. It returns
// false on old consoles that cannot do it, so colour and prompts stay off there.
func enableVT(f *os.File) bool {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}

// waitInput reports whether the console has input to read within timeout.
func waitInput(f *os.File, timeout time.Duration) (bool, error) {
	ev, err := windows.WaitForSingleObject(windows.Handle(f.Fd()), uint32(timeout.Milliseconds()))
	return err == nil && ev == windows.WAIT_OBJECT_0, err
}
