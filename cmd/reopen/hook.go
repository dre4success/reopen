package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/dre4success/reopen/internal/agent"
	"github.com/dre4success/reopen/internal/gitx"
	"github.com/dre4success/reopen/internal/proc"
	"github.com/dre4success/reopen/internal/roles"
	"github.com/dre4success/reopen/internal/store"
)

// hookInput is what a CLI's start hook gets on stdin: Claude Code and Codex
// send session_id (and Claude Code a source), Antigravity conversationId
// and the agent's folder, cwd.
type hookInput struct {
	SessionID      string `json:"session_id"`
	Source         string `json:"source"`
	TranscriptPath string `json:"transcript_path"`
	ConversationID string `json:"conversationId"`
	Cwd            string `json:"cwd"`
}

// cmdHook records the session whose start hook runs it. It must never get
// in the session's way: it prints nothing, since Claude Code adds a
// start hook's output to the session's context, and it exits 0 even when it
// records nothing, noting why in ~/.reopen/hook.log.
func (a *app) cmdHook(args []string) int {
	if err := a.hook(args); err != nil {
		// Not on stderr either: the log is where the operator looks, and
		// reopen list points at it.
		store.Log(a.home, a.now(), fmt.Sprintf("%s: %v", strings.Join(args, " "), err))
	}
	return 0
}

func (a *app) hook(args []string) error {
	if len(args) != 1 {
		return errors.New("hook needs a CLI: claude, codex or antigravity")
	}
	c, ok := agent.Lookup(args[0])
	if !ok {
		return fmt.Errorf("unknown CLI %q: claude, codex or antigravity", args[0])
	}
	data, err := io.ReadAll(io.LimitReader(a.stdin, 1<<20))
	if err != nil {
		return err
	}
	var in hookInput
	if err := json.Unmarshal(data, &in); err != nil {
		return fmt.Errorf("the hook's input isn't JSON: %w", err)
	}
	id := in.SessionID
	if c.Name == agent.Antigravity {
		id = in.ConversationID
	}
	if !agent.PlainID(id) {
		return fmt.Errorf("the hook's input has no session id")
	}

	// The run's folder, never a transcript's: a transcript says where the
	// conversation began, not where this run started.
	//   - Claude Code: CLAUDE_PROJECT_DIR, the folder its process started or
	//     resumed in, which stays so after a cd. Its process is CLAUDE_PID.
	//   - Codex: stdin's cwd, the run's folder, where the hook also runs. Its
	//     process is the hook's parent: Codex's shared daemon, which runs
	//     the TUI's sessions, or with --no-daemon the TUI itself.
	//   - Antigravity: stdin's cwd, the agent's folder; the hook runs in the
	//     folder of its hooks.json. Its process is the hook's parent.
	var folder string
	var pid int
	switch c.Name {
	case agent.Claude:
		folder = a.getenv("CLAUDE_PROJECT_DIR")
		pid, _ = strconv.Atoi(a.getenv("CLAUDE_PID"))
	case agent.Codex:
		folder, pid = in.Cwd, a.ppid()
		if !filepath.IsAbs(folder) {
			folder = a.cwd
		}
	default:
		folder, pid = in.Cwd, a.ppid()
	}
	if !filepath.IsAbs(folder) {
		return fmt.Errorf("no folder for session %s", id)
	}
	info, ok := a.process(pid)
	if !ok || !proc.Named(info.Args, c.Command) {
		if c.Name == agent.Claude {
			return fmt.Errorf("session %s: CLAUDE_PID names no claude process", id)
		}
		return fmt.Errorf("session %s: the hook's parent, process %d, isn't %s; its command starts with exec, as reopen hooks prints", id, pid, c.Command)
	}
	if !c.Interactive(info.Args) {
		return nil // claude -p, codex exec, agy -p: not a session to bring back
	}
	if c.Name == agent.Codex && !a.codexTerminal(id, in.TranscriptPath, info.Args) {
		return nil
	}
	if ep := a.getenv("CLAUDE_CODE_ENTRYPOINT"); c.Name == agent.Claude && ep != "" && ep != "cli" {
		// Claude Code says what started it: cli in a terminal, also for a
		// /background worker; sdk-cli for claude -p; others for the desktop
		// app, an editor or the SDK, whose sessions aren't brought back yet.
		return nil
	}
	common, err := gitx.CommonDir(folder)
	if err != nil {
		return nil // outside a git repository: no project to list it under
	}
	role := roles.Read(gitx.MainRoot(common))[id]
	now := a.now()
	return store.Update(a.home, common, func(l *store.List) (bool, error) {
		if c.Name == agent.Claude && in.Source == "clear" {
			// /clear gives a new id in the same process, and the cleared
			// conversation's entry goes: the one this process reported last.
			// Not one it left earlier by a /resume, nor one of a process that
			// had its id before.
			last := -1
			for i, e := range l.Entries {
				if e.CLI == c.Name && e.ID != id && e.PID == pid && !e.LastSeen.Before(info.Start) &&
					(last < 0 || e.LastSeen.After(l.Entries[last].LastSeen)) {
					last = i
				}
			}
			if last >= 0 {
				l.Entries = slices.Delete(l.Entries, last, last+1)
			}
		}
		// A session forget dropped is the operator's again once it starts.
		l.Forgotten = slices.DeleteFunc(l.Forgotten, func(f string) bool { return f == id })
		e := store.Entry{CLI: c.Name, ID: id, Folder: folder, PID: pid, Args: info.Args, LastSeen: now, Role: role}
		if i := l.Find(c.Name, id); i >= 0 {
			if e.Role == "" {
				e.Role = l.Entries[i].Role
			}
			if c.Name == agent.Claude && slices.Contains(e.Args, "bg-spare") {
				// A /background worker's arguments carry none of the
				// session's flags (claude bg-spare --bg-spare <socket>).
				e.Args = l.Entries[i].Args
			}
			l.Entries[i] = e
		} else {
			l.Entries = append(l.Entries, e)
		}
		return true, nil
	})
}

// codexTerminal says whether a Codex session runs in a terminal's TUI. Its
// rollout's first line says who started it: the desktop app ("Codex
// Desktop") and a subagent's thread, such as a guardian review, aren't
// brought back. Without that line, the hook's parent tells: Codex's
// daemon runs the TUI's sessions when the CLI started it (--managed-daemon);
// the desktop app runs one of its own.
func (a *app) codexTerminal(id, rollout string, parent []string) bool {
	if filepath.Base(rollout) == "" || !strings.HasSuffix(rollout, "-"+id+".jsonl") {
		rollout = ""
	}
	if rollout == "" {
		c, _ := agent.Lookup(agent.Codex)
		rollout = c.Transcript(a.homes(), id)
	}
	if originator, sub, ok := agent.Meta(rollout); ok {
		o := strings.ToLower(originator)
		return !sub && !strings.Contains(o, "desktop") && !strings.Contains(o, "exec")
	}
	daemon, managed := agent.Daemon(parent)
	return !daemon || managed
}
