package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/dre4success/reopen/internal/shell"
)

// cmdHooks prints the start hook to add to a CLI's user settings, so its
// sessions in every project get on their project's list. It prints; it
// never edits a CLI's settings.
func (a *app) cmdHooks(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("hooks needs a CLI: claude, codex or antigravity")
	}
	self := selfCommand()
	var where, note string
	var hooks any
	switch args[0] {
	case "claude":
		// Exec form, with no shell: the hook's parent is Claude Code, and
		// CLAUDE_PID names its process anyway. No matcher, so it runs at
		// every start: startup, resume, /clear, /compact, /fork and
		// /background.
		where = filepath.Join(a.homes().Claude, "settings.json")
		note = "Sessions already running get it when they start again."
		hooks = map[string]any{"hooks": map[string]any{
			"SessionStart": []any{map[string]any{"hooks": []any{
				map[string]any{"type": "command", "command": self, "args": []string{"hook", "claude"}, "timeout": 10},
			}}},
		}}
	case "codex":
		// Codex runs a hook's command through a shell; exec makes the
		// hook's parent Codex's own process: its daemon, which runs the
		// TUI's sessions, or the TUI itself with --no-daemon.
		where = filepath.Join(a.homes().Codex, "hooks.json")
		note = "The next time Codex starts, it asks you to review the new hook: trust it. Sessions already running get it when they start again."
		hooks = map[string]any{"hooks": map[string]any{
			"SessionStart": []any{map[string]any{"hooks": []any{
				map[string]any{"type": "command", "command": "exec " + shell.Quote(self) + " hook codex", "timeout": 10},
			}}},
		}}
	case "antigravity":
		// Antigravity's hooks.json holds named groups of hooks; this is
		// reopen's. It too runs a command through a shell, hence exec.
		home, _ := os.UserHomeDir()
		if a.home != "" {
			home = a.home
		}
		where = filepath.Join(home, ".gemini", "config", "hooks.json")
		note = "Sessions already running get it when they start again. A new conversation gets on the list at its first message; agy runs no start hook for a resumed one, which stays on the list as it was."
		hooks = map[string]any{"reopen": map[string]any{
			"SessionStart": []any{
				map[string]any{"type": "command", "command": "exec " + shell.Quote(self) + " hook antigravity", "timeout": 10},
			},
		}}
	default:
		return fmt.Errorf("unknown CLI %q: claude, codex or antigravity", args[0])
	}
	b, err := json.MarshalIndent(hooks, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Add this to %s, merged with the hooks already there.\n%s\n\n%s\n", a.tilde(where), note, b)
	return nil
}

// selfCommand is how a hook should run this reopen: by name when that name
// finds this binary on PATH, else by its full path.
func selfCommand() string {
	self, err := os.Executable()
	if err != nil {
		return "reopen"
	}
	if real, err := filepath.EvalSymlinks(self); err == nil {
		self = real
	}
	if found, err := exec.LookPath("reopen"); err == nil {
		if real, err := filepath.EvalSymlinks(found); err == nil && real == self {
			return "reopen"
		}
	}
	return self
}
