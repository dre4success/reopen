// Package iterm opens a new iTerm2 window with a tab for each command, and
// types the command into the tab's shell, so the shell stays when the
// command ends. It runs osascript, macOS's own.
package iterm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Script is the AppleScript. The commands come as its arguments, never
// inside its text, so no command needs quoting for AppleScript. Each tab
// starts in whatever folder its profile gives, so each command cds first.
const Script = `on run argv
	tell application "iTerm2"
		set w to (create window with default profile)
		tell current session of w to write text (item 1 of argv)
		repeat with i from 2 to (count of argv)
			tell w to set t to (create tab with default profile)
			tell current session of t to write text (item i of argv)
		end repeat
		activate
	end tell
end run
`

// Open opens one window with a tab per command, using the osascript at
// path. It waits 30 seconds at most, as macOS may ask the operator first
// whether the terminal reopen runs in may control iTerm2.
func Open(osascript string, commands []string) error {
	if len(commands) == 0 {
		return errors.New("no command to open")
	}
	if testing.Testing() && osascript == "/usr/bin/osascript" {
		return errors.New("a test may not run the real osascript")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, osascript, append([]string{"-"}, commands...)...)
	cmd.Stdin = strings.NewReader(Script)
	cmd.WaitDelay = time.Second
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if strings.Contains(msg, "-1743") || strings.Contains(msg, "Not authorized") {
			return fmt.Errorf("macOS doesn't let this terminal control iTerm2 (%s). Allow it in System Settings, Privacy & Security, Automation, then run reopen again", msg)
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("osascript: %s", msg)
	}
	return nil
}
