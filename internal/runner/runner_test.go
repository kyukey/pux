// internal/runner/runner_test.go

package runner

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func run(t *testing.T, ctx context.Context, value, in string, args ...string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code, err := Exec(ctx, []byte(value), args, strings.NewReader(in), &out, &out)
	if err != nil {
		t.Fatalf("Exec(%q) = %d, %v", value, code, err)
	}
	return code, out.String()
}

func unixOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a Unix shell")
	}
}

func TestParseShebang(t *testing.T) {
	tests := []struct {
		value string
		want  []string
	}{
		{"#!/bin/bash\necho hi\n", []string{"/bin/bash"}},
		{"#!/bin/sh\r\necho hi\r\n", []string{"/bin/sh"}},
		{"#! /bin/sh -e\n", []string{"/bin/sh", "-e"}},
		{"#!/usr/bin/env python3 -u\n", []string{"python3", "-u"}},
		{"#!/usr/bin/env -S deno run -A\n", []string{"deno", "run", "-A"}},
		{"#!\necho hi\n", nil},
		{"#!/usr/bin/env\n", nil},
		{" #!/bin/sh\n", nil},
		{"echo hi\n", nil},
	}
	for _, tt := range tests {
		got, ok := parseShebang([]byte(tt.value))
		if !slices.Equal(got, tt.want) || ok != (tt.want != nil) {
			t.Errorf("parseShebang(%q) = %q, %v, want %q", tt.value, got, ok, tt.want)
		}
	}
}

func TestTempPattern(t *testing.T) {
	for interp, want := range map[string]string{"/usr/bin/pwsh": "pux-*.ps1", "PowerShell.exe": "pux-*.ps1", "python3": "pux-*"} {
		if got := tempPattern(interp); got != want {
			t.Errorf("tempPattern(%q) = %q, want %q", interp, got, want)
		}
	}
}

func TestExecUnix(t *testing.T) {
	unixOnly(t)
	tests := []struct {
		name, value, in string
		args            []string
		code            int
		out             string
	}{
		{"output", "echo hi", "", nil, 0, "hi\n"},
		{"exit code", "exit 3", "", nil, 3, ""},
		{"multi line", "x=1\ny=2\nexit $((x + y))\n", "", nil, 3, ""},
		{"crlf", "x=1\r\ny=2\r\necho \"$x$y\"\r\nexit $((x + y))\r\n", "", nil, 3, "12\n"},
		{"shebang crlf", "#!/bin/sh\r\nx=7\r\necho \"$x\"\r\nexit $x\r\n", "", nil, 7, "7\n"},
		{"lone cr kept", "printf 'a\rb'", "", nil, 0, "a\rb"},
		{"zero is pux", `echo "$0"`, "", nil, 0, "pux\n"},
		{"args", `echo "$#:$1"`, "", []string{"a b", "c"}, 0, "2:a b\n"},
		{"stdin", `read x; echo "got $x"`, "abc\n", nil, 0, "got abc\n"},
		{"signal", "kill -TERM $$", "", nil, 143, ""},
		{"shebang", "#!/bin/sh\necho \"$1\"\nexit 4\n", "", []string{"hi"}, 4, "hi\n"},
		{"shebang flags", "#!/bin/sh -e\nfalse\nexit 0\n", "", nil, 1, ""},
		{"shebang env", "#!/usr/bin/env sh\nexit 5\n", "", nil, 5, ""},
		{"shebang fallback", "#!/no/such/dir/sh\nexit 6\n", "", nil, 6, ""},
	}
	for _, tt := range tests {
		code, out := run(t, context.Background(), tt.value, tt.in, tt.args...)
		if code != tt.code || out != tt.out {
			t.Errorf("%s: Exec = %d, %q, want %d, %q", tt.name, code, out, tt.code, tt.out)
		}
	}
}

func TestExecWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("needs cmd.exe")
	}
	tests := []struct {
		name, value string
		args        []string
		code        int
	}{
		{"exit code", "exit /b 3", nil, 3},
		{"multi line", "set x=1\nexit /b 4\n", nil, 4},
		{"first arg", "if \"%~1\"==\"hi\" exit /b 0\nexit /b 1\n", []string{"hi"}, 0},
		{"label and goto", "goto done\nexit /b 1\n:done\nexit /b 0\n", nil, 0},
		{"ampersand", "if \"%~1\"==\"a&b\" exit /b 0\nexit /b 1\n", []string{"a&b"}, 0},
		{"pipe", "if \"%~1\"==\"x|y\" exit /b 0\nexit /b 1\n", []string{"x|y"}, 0},
		{"caret and angles", "if \"%~1\"==\"a^b<c>d\" exit /b 0\nexit /b 1\n", []string{"a^b<c>d"}, 0},
		{"percent", "if \"%~1\"==\"100%%\" exit /b 0\nexit /b 1\n", []string{"100%"}, 0},
		{"no expansion", "if \"%~1\"==\"%%PATH%%\" exit /b 0\nexit /b 1\n", []string{"%PATH%"}, 0},
		{"empty arg", "if \"%~1\"==\"\" if \"%~2\"==\"b\" exit /b 0\nexit /b 1\n", []string{"", "b"}, 0},
		{"quote keeps count", "if \"%~2\"==\"next\" exit /b 0\nexit /b 1\n", []string{`say "hi"`, "next"}, 0},
	}
	for _, tt := range tests {
		if code, _ := run(t, context.Background(), tt.value, "", tt.args...); code != tt.code {
			t.Errorf("%s: Exec = %d, want %d", tt.name, code, tt.code)
		}
	}
	if _, err := Exec(context.Background(), []byte("exit /b 0"), []string{"a\nb"}, nil, nil, nil); err == nil {
		t.Error("Exec with a line break in an argument succeeded, want an error")
	}
}

func TestExecMissingInterpreter(t *testing.T) {
	code, err := Exec(context.Background(), []byte("#!/no/such/pux-interpreter\necho hi\n"), nil, nil, nil, nil)
	if code != ExitNotFound || !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "pux-interpreter") {
		t.Errorf("Exec = %d, %v, want %d and ErrNotFound naming it", code, err, ExitNotFound)
	}
}

func TestTempFileRemoved(t *testing.T) {
	value := "#!/bin/sh\necho \"$0\"\n"
	if runtime.GOOS == "windows" {
		value = "echo %~f0"
	}
	_, out := run(t, context.Background(), value, "")
	path := strings.TrimSpace(out)
	if path == "" {
		t.Fatal("script did not print its own path")
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("temp file %s still exists after Exec: %v", path, err)
	}
}

func TestExecCancel(t *testing.T) {
	unixOnly(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	code, _ := run(t, ctx, "exec sleep 5", "")
	if code != 143 || time.Since(start) > 3*time.Second {
		t.Errorf("cancelled Exec = %d after %v, want 143 well before the command ends", code, time.Since(start))
	}
}

// TestForwardsTerm signals the whole test process, so it must never run in parallel.
func TestForwardsTerm(t *testing.T) {
	unixOnly(t)
	ready := filepath.Join(t.TempDir(), "ready")
	done := make(chan int, 1)
	go func() {
		code, _ := Exec(context.Background(), []byte("touch \"$1\"\nexec sleep 5"), []string{ready}, nil, nil, nil)
		done <- code
	}()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command did not start")
		}
	}
	p, _ := os.FindProcess(os.Getpid())
	if err := p.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 143 {
			t.Errorf("Exec = %d, want 143 from the forwarded SIGTERM", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SIGTERM was not forwarded to the command")
	}
}
