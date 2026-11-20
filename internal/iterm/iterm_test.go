package iterm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A stand-in osascript notes its arguments and the script it reads; no
// window opens.
func TestOpenPassesCommandsAsArguments(t *testing.T) {
	dir := t.TempDir()
	stand := filepath.Join(dir, "osascript")
	os.WriteFile(stand, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \""+dir+"/args\"\ncat > \""+dir+"/script\"\n"), 0o700)
	cmds := []string{`cd -- '/a b' && claude --resume x`, `cd -- /c && codex resume y "q"`}
	if err := Open(stand, cmds); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if got := strings.Split(strings.TrimSpace(string(args)), "\n"); len(got) != 3 || got[0] != "-" || got[1] != cmds[0] || got[2] != cmds[1] {
		t.Fatalf("osascript args %q", got)
	}
	script, _ := os.ReadFile(filepath.Join(dir, "script"))
	if string(script) != Script || strings.Contains(Script, "claude") {
		t.Fatal("the script isn't Script, or has a command in its text")
	}
	if err := Open(stand, nil); err == nil {
		t.Fatal("Open with no command succeeded")
	}
}

func TestOpenSaysWhatMacOSRefused(t *testing.T) {
	dir := t.TempDir()
	stand := filepath.Join(dir, "osascript")
	os.WriteFile(stand, []byte("#!/bin/sh\ncat >/dev/null\necho 'execution error: Not authorized to send Apple events to iTerm2. (-1743)' >&2\nexit 1\n"), 0o700)
	err := Open(stand, []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "Privacy & Security, Automation") {
		t.Fatalf("err = %v", err)
	}
}
