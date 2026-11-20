package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dre4success/reopen/internal/iterm"
	"github.com/dre4success/reopen/internal/proc"
	"github.com/dre4success/reopen/internal/term"
)

// newApp is reopen in the real world.
func newApp() *app {
	if testing.Testing() {
		panic("newApp is the real world; a test builds its app with a temporary home and stand-ins")
	}
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	return &app{
		stdin:    os.Stdin,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		getenv:   os.Getenv,
		home:     home,
		cwd:      cwd,
		now:      time.Now,
		terminal: func() bool { return term.IsTerminal(os.Stdin) },
		ppid:     os.Getppid,
		process:  proc.Get,
		scan:     proc.Scan,
		claudeAgents: func() ([]byte, error) {
			// Claude Code's own list of its sessions, "for scripts". It starts
			// no session. Unless told not to, it also opens a connection off
			// the Mac, which its answer doesn't need
			// (evidence/r1/agents-network.log).
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "claude", "agents", "--json")
			cmd.Env = agentsEnv(os.Environ())
			// A wrapper's child may hold the pipe after the wrapper is killed:
			// stop waiting for it a second later.
			cmd.WaitDelay = time.Second
			return cmd.Output() // stdin is /dev/null
		},
		openFiles: proc.OpenFiles,
		itermInstalled: func() bool {
			for _, d := range []string{"/Applications", filepath.Join(home, "Applications")} {
				if _, err := os.Stat(filepath.Join(d, "iTerm.app")); err == nil {
					return true
				}
			}
			return false
		},
		tty: func() *os.File {
			// Key by key only where both ends are a terminal that can take it.
			if term.IsTerminal(os.Stdin) && term.IsTerminal(os.Stdout) && os.Getenv("TERM") != "dumb" {
				return os.Stdin
			}
			return nil
		},
		color: term.IsTerminal(os.Stdout) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb",
		openTabs: func(commands []string) error {
			return iterm.Open(osascript(), commands)
		},
	}
}

// osascript is macOS's own, unless a test names a stand-in.
func osascript() string {
	if p := os.Getenv("REOPEN_TEST_OSASCRIPT"); p != "" && filepath.IsAbs(p) {
		return p
	}
	return "/usr/bin/osascript"
}

// agentsEnv is the environment claude agents --json runs in: the
// operator's, without the variables of the agent session reopen may run in,
// so it can't be taken for that session, and with Claude Code's
// nonessential traffic off, so it opens no connection off the Mac.
func agentsEnv(environ []string) []string {
	out := []string{"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1"}
	for _, kv := range environ {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "CLAUDE_CODE_SESSION_ID", "CLAUDE_PID", "CLAUDE_PROJECT_DIR", "CLAUDE_CODE_ENTRYPOINT",
			"CODEX_THREAD_ID", "ANTIGRAVITY_CONVERSATION_ID", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC":
			continue
		}
		out = append(out, kv)
	}
	return out
}
