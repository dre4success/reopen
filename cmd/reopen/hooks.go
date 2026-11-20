package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dre4success/reopen/internal/agent"
	"github.com/dre4success/reopen/internal/settings"
	"github.com/dre4success/reopen/internal/shell"
)

// hookSpec is reopen's start hook for one CLI: the settings file it goes
// in, where in that file (a key at each level, down to an array), the hook
// itself, and what the operator should know.
type hookSpec struct {
	name string   // the CLI's own name
	file string   // its user settings file
	path []string // where in the file
	elem any      // what goes in that array
	note string
	cli  string
	self string // how a hook runs this reopen: its name or its path
}

func (a *app) hookSpec(cli string) (hookSpec, error) {
	self := selfCommand()
	switch cli {
	case agent.Claude:
		// Exec form, with no shell: the hook's parent is Claude Code, and
		// CLAUDE_PID names its process anyway. No matcher, so it runs at
		// every start: startup, resume, /clear, /compact, /fork and
		// /background.
		return hookSpec{
			name: "Claude Code",
			file: filepath.Join(a.homes().Claude, "settings.json"),
			path: []string{"hooks", "SessionStart"},
			elem: settings.Object("hooks", []any{settings.Object("type", "command", "command", self, "args", []string{"hook", "claude"}, "timeout", 10)}),
			note: "Sessions already running get it when they start again.",
			cli:  cli, self: self,
		}, nil
	case agent.Codex:
		// Codex runs a hook's command through a shell; exec makes the
		// hook's parent Codex's own process: its daemon, which runs the
		// TUI's sessions, or the TUI itself with --no-daemon.
		return hookSpec{
			name: "Codex",
			file: filepath.Join(a.homes().Codex, "hooks.json"),
			path: []string{"hooks", "SessionStart"},
			elem: settings.Object("hooks", []any{settings.Object("type", "command", "command", "exec "+shell.Quote(self)+" hook codex", "timeout", 10)}),
			note: "The next time Codex starts, it asks you to review the new hook: trust it. Sessions already running get it when they start again.",
			cli:  cli, self: self,
		}, nil
	case agent.Antigravity:
		// Antigravity's hooks.json holds named groups of hooks; this is
		// reopen's. It too runs a command through a shell, hence exec.
		return hookSpec{
			name: "Antigravity",
			file: filepath.Join(a.home, ".gemini", "config", "hooks.json"),
			path: []string{"reopen", "SessionStart"},
			elem: settings.Object("type", "command", "command", "exec "+shell.Quote(self)+" hook antigravity", "timeout", 10),
			note: "Sessions already running get it when they start again. A new conversation gets on the list at its first message; agy runs no start hook for a resumed one, which stays on the list as it was.",
			cli:  cli, self: self,
		}, nil
	}
	return hookSpec{}, fmt.Errorf("unknown CLI %q: claude, codex or antigravity", cli)
}

// has says whether settings data holds reopen's start hook for the CLI,
// where the CLI runs it at a session's start.
func (h hookSpec) has(data []byte) bool {
	return slices.ContainsFunc(h.startCommands(data), h.reopens)
}

// reopens says whether a hook's words run reopen's start hook for this CLI:
// reopen, by its name, a path to it, or this reopen's own path, then hook
// and the CLI, and nothing else, after an exec if any. A command that only
// mentions those words, as echo would, isn't it.
func (h hookSpec) reopens(words []string) bool {
	if len(words) > 0 && words[0] == "exec" {
		words = words[1:]
	}
	return len(words) == 3 && words[1] == "hook" && words[2] == h.cli &&
		(words[0] == h.self || filepath.Base(words[0]) == "reopen")
}

// startCommands are the start hooks in a CLI's settings data, as the words
// each runs, where the CLI reads them: for Claude Code and Codex,
// hooks.SessionStart[].hooks[]; for Antigravity, the SessionStart of each
// named group. A hook for another event doesn't run at a start. A hook with
// args runs its command as a program, with those arguments; one without is
// a shell's command line, split as a shell would, and left out if it holds
// anything a shell would do more with than split.
func (h hookSpec) startCommands(data []byte) [][]string {
	var v any
	if json.Unmarshal(data, &v) != nil {
		return nil
	}
	top, _ := v.(map[string]any)
	var hooks []any
	if h.path[0] == "hooks" {
		ev, _ := top["hooks"].(map[string]any)
		groups, _ := ev["SessionStart"].([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hs, _ := gm["hooks"].([]any)
			hooks = append(hooks, hs...)
		}
	} else {
		for _, g := range top {
			gm, _ := g.(map[string]any)
			hs, _ := gm["SessionStart"].([]any)
			hooks = append(hooks, hs...)
		}
	}
	var out [][]string
	for _, x := range hooks {
		m, _ := x.(map[string]any)
		c, ok := m["command"].(string)
		if !ok {
			continue
		}
		if args, isExec := m["args"].([]any); isExec {
			words := []string{c}
			for _, a := range args {
				s, ok := a.(string)
				if !ok {
					words = nil
					break
				}
				words = append(words, s)
			}
			if words != nil {
				out = append(out, words)
			}
			continue
		}
		if words, ok := shellWords(c); ok {
			out = append(out, words)
		}
	}
	return out
}

// shellWords splits a command line into words as a POSIX shell would, for
// the plain lines reopen writes: words, single and double quotes, and
// backslashes. ok is false for a line with anything else a shell acts on,
// such as ; | & $ or a glob, whose words aren't simply what it runs.
func shellWords(line string) (words []string, ok bool) {
	var w strings.Builder
	in := false // within a word
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == ' ' || c == '\t':
			if in {
				words, in = append(words, w.String()), false
				w.Reset()
			}
		case c == '\'':
			j := strings.IndexByte(line[i+1:], '\'')
			if j < 0 {
				return nil, false
			}
			w.WriteString(line[i+1 : i+1+j])
			i, in = i+1+j, true
		case c == '"':
			i++
			for ; i < len(line) && line[i] != '"'; i++ {
				switch line[i] {
				case '$', '`':
					return nil, false
				case '\\':
					if i+1 < len(line) && strings.IndexByte("\"\\$`", line[i+1]) >= 0 {
						i++
					}
				}
				w.WriteByte(line[i])
			}
			if i >= len(line) {
				return nil, false
			}
			in = true
		case c == '\\':
			if i+1 >= len(line) {
				return nil, false
			}
			i++
			w.WriteByte(line[i])
			in = true
		case strings.IndexByte(";|&<>()$`*?[]{}~#!\n", c) >= 0:
			return nil, false
		default:
			w.WriteByte(c)
			in = true
		}
	}
	if in {
		words = append(words, w.String())
	}
	return words, true
}

// cmdHooks prints the start hook to add to a CLI's user settings, so its
// sessions in every project get on their project's list. It prints only;
// reopen init adds them.
func (a *app) cmdHooks(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("hooks needs a CLI: claude, codex or antigravity")
	}
	h, err := a.hookSpec(args[0])
	if err != nil {
		return err
	}
	b, err := settings.Append(nil, h.path, h.elem)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Add this to %s, merged with the hooks already there, or let reopen init add it.\n%s\n\n%s", a.tilde(h.file), h.note, b)
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
