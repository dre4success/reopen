package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dre4success/reopen/internal/agent"
	"github.com/dre4success/reopen/internal/gitx"
	"github.com/dre4success/reopen/internal/proc"
	"github.com/dre4success/reopen/internal/roles"
	"github.com/dre4success/reopen/internal/shell"
	"github.com/dre4success/reopen/internal/store"
)

// unusedFor is how long a closed session goes unused before reopen drops
// it, and says so.
const unusedFor = 30 * 24 * time.Hour

type state int

const (
	closed     state = iota
	open             // a process runs it, in some terminal
	background       // Claude Code runs it in the background, with no tab
)

// row is one entry as reopen shows it.
type row struct {
	store.Entry
	cli        *agent.CLI
	transcript string
	// lastUse is the transcript's time; LastSeen, in the entry, is the
	// hook's. They are different times and are kept apart.
	lastUse time.Time
	label   string
	role    string // the role handoff gives it now
	movedTo string // the session that holds the role it had, if another does
	state   state
	job     string // the background job's id, for claude attach
	flags   []agent.Flag
	mode    string // Claude Code's permission mode, which its resume keeps
	// modeLost is the mode the session was in that its resume won't keep.
	modeLost string
	codex    agent.CodexSettings // how a Codex session comes back
	// held: open only in that Codex's daemon still has it loaded, which it
	// does for some minutes after its terminal closes. It isn't offered:
	// the lock can't tell a live session from one closed minutes ago, and
	// opening a live one twice is the worse mistake.
	held bool
	gone bool // its folder no longer exists
	// unsafe: its folder, id or a flag holds a control character, which
	// the tab's shell would take as typed: a newline ends the line.
	unsafe bool
	size   int64 // the transcript's size, what a resume re-reads
}

// pickable says whether the row is offered to open: closed and resumable,
// or in the background, for claude attach.
func (r row) pickable() bool {
	if r.gone || r.unsafe {
		return false
	}
	if r.state == background {
		return true
	}
	return r.state == closed && r.resumable()
}

// leftOut says why a session that isn't open isn't offered, or "".
func (r row) leftOut() string {
	switch {
	case r.held:
		return heldWord
	case r.state == open:
		return ""
	case r.unsafe:
		return "its folder or a flag holds a control character, which its tab would run as typed"
	case r.gone:
		return "its folder is gone"
	case !r.resumable():
		return "" // nobody typed to it: not worth a word
	}
	return ""
}

// resumable: a Claude Code session nobody typed to has no transcript, and
// can't be resumed.
func (r row) resumable() bool { return r.cli.Name != agent.Claude || r.transcript != "" }

// ticked says whether the row starts ticked: every closed session, unless
// its role went to another session. One in the background, or one Codex
// still has loaded, is only offered: the operator knows if it is open.
func (r row) ticked() bool { return r.state == closed && r.movedTo == "" }

// repo is the common git folder of the folder reopen runs in.
func (a *app) repo() (string, error) {
	if !filepath.IsAbs(a.home) {
		return "", fmt.Errorf("no home folder (HOME is %q), so reopen has nowhere to keep its list", a.home)
	}
	common, err := gitx.CommonDir(a.cwd)
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository (or any") {
			return "", errors.New("not in a git repository; run it in a project's folder")
		}
		return "", err // git's own words, as for a repository another user owns
	}
	return common, nil
}

// project is the project's name, for what reopen says.
func project(repo string) string {
	if root := gitx.MainRoot(repo); root != "" {
		return filepath.Base(root)
	}
	return filepath.Base(repo)
}

// view returns repo's rows, newest first, and the entries unused for 30
// days. With write, which only reopen at the operator's terminal asks, it
// brings the list up to date under its lock: it follows Claude Code's
// /background, notes the roles it sees, and drops what has gone unused.
// Without, it shows the same and changes nothing.
func (a *app) view(repo string, write bool) ([]row, []row, error) {
	var rows, dropped []row
	// The processes and Claude Code's background jobs are asked for before
	// the list's lock is taken, so a hook never waits on them.
	a.root = gitx.MainRoot(repo)
	pre, err := store.Load(a.home, repo)
	if err != nil || len(pre.Entries) == 0 {
		return nil, nil, err // an empty list needs no lock, nor ~/.reopen/ made for it
	}
	lines, _ := a.scan() // with no process list, only the recorded processes are checked
	jobs := a.backgroundJobs(pre)
	loaded, presence := a.held(pre, lines)
	update := store.Update
	a.wrote = write
	if !write {
		// list and --print change nothing: they show what reopen would do,
		// and the operator's next reopen does it, and says so.
		update = func(home, repo string, fn func(*store.List) (bool, error)) error {
			l, err := store.Load(home, repo)
			if err != nil {
				return err
			}
			_, err = fn(l)
			return err
		}
	}
	err = update(a.home, repo, func(l *store.List) (bool, error) {
		changed := followContinued(l, a.homes())
		now := a.now()
		held := roles.Read(gitx.MainRoot(repo))
		run := a.running(l, lines)
		keep := l.Entries[:0:0]
		for _, e := range l.Entries {
			c, ok := agent.Lookup(e.CLI)
			if !ok {
				keep = append(keep, e) // a CLI a later reopen knows
				continue
			}
			r := row{Entry: e, cli: c}
			r.transcript = c.Transcript(a.homes(), e.ID)
			if r.transcript != "" {
				r.lastUse = agent.LastUse(r.transcript)
				r.label = c.Label(r.transcript)
			}
			r.role = held[e.ID]
			if r.role != "" && r.role != e.Role {
				r.Role, changed = r.role, true
			}
			if r.role == "" && e.Role != "" {
				if h := roles.Holder(held, e.Role); h != "" && h != e.ID {
					r.movedTo = h
				}
			}
			// A Claude Code session in the background is open, with no tab.
			switch job, bg := jobs[e.ID]; {
			case bg && c.Name == agent.Claude:
				r.state, r.job = background, job
			case run[key{e.CLI, e.ID}], c.Name == agent.Antigravity && presence[e.ID]:
				r.state = open
			case c.Name == agent.Codex && loaded[e.ID]:
				r.state, r.held = open, true
			}
			r.mode = c.Mode(r.transcript)
			if c.Name == agent.Codex && r.transcript != "" {
				r.codex = agent.Settings(r.transcript)
			}
			if r.state == closed && now.Sub(used(r)) > unusedFor {
				dropped = append(dropped, r)
				changed = true
				continue
			}
			r.flags = c.Flags(e.Args, e.Folder)
			if c.Name == agent.Claude && r.mode == "bypassPermissions" && !slices.ContainsFunc(agent.Words(r.flags), func(w string) bool {
				return w == "--dangerously-skip-permissions" || w == "--allow-dangerously-skip-permissions"
			}) {
				// A bare claude --resume keeps the session's mode, but not
				// this one without the flag that allows it: the session came
				// back in auto mode (evidence/r1/e2e-claude.log).
				r.mode, r.modeLost = "", "bypassPermissions"
			}
			if st, err := os.Stat(e.Folder); err != nil || !st.IsDir() {
				r.gone = true
			}
			r.unsafe = unsafe(e, r.flags)
			if r.transcript != "" {
				r.size = agent.Size(r.transcript)
			}
			keep = append(keep, r.Entry)
			rows = append(rows, r)
		}
		l.Entries = keep
		return changed, nil
	})
	slices.SortStableFunc(rows, func(x, y row) int { return used(y).Compare(used(x)) })
	return rows, dropped, err
}

func used(r row) time.Time {
	if r.lastUse.IsZero() {
		return r.LastSeen
	}
	return r.lastUse
}

// running finds the sessions on l that a live process runs now. A process
// runs the id it last reported, if it reported one since it started: /clear
// and a /resume typed in a session move a process to another id, and its
// arguments still name the first. One that has reported nothing yet runs
// the id its arguments resume (claude --resume <id>, codex resume <id>, agy
// --conversation <id>): reopen typed them, and Codex's and Antigravity's
// hooks run only at the first turn, Antigravity's not at all on a resume.
// An id only mentioned, as in a prompt, doesn't count. A process counts only if
// it is that CLI; one that started after an entry was last seen has reused
// that entry's process id, and its report is someone else's.
func (a *app) running(l *store.List, lines []proc.Line) map[key]bool {
	latest := map[key]store.Entry{} // by CLI and process id
	for _, e := range l.Entries {
		c, ok := agent.Lookup(e.CLI)
		if !ok || e.PID <= 0 {
			continue
		}
		info, ok := a.process(e.PID)
		if !ok || !proc.Named(info.Args, c.Command) || !info.Start.Before(e.LastSeen) {
			continue
		}
		if daemon, _ := agent.Daemon(info.Args); daemon {
			continue // Codex's daemon runs many sessions: its reports say nothing of one
		}
		k := key{e.CLI, fmt.Sprint(e.PID)}
		if r, ok := latest[k]; !ok || e.LastSeen.After(r.LastSeen) {
			latest[k] = e
		}
	}
	run := map[key]bool{}
	for _, e := range latest {
		run[key{e.CLI, e.ID}] = true
	}
	for _, ln := range lines {
		for _, name := range agent.Names() {
			c, _ := agent.Lookup(name)
			if !proc.Named(ln.Fields, c.Command) {
				continue
			}
			id := c.ResumedID(ln.Fields)
			if id == "" {
				continue
			}
			if _, reported := latest[key{name, fmt.Sprint(ln.PID)}]; reported {
				continue // it said since which session it runs
			}
			run[key{name, id}] = true
		}
	}
	return run
}

// key is a CLI and an id: a session's, or a process's.
type key struct{ cli, id string }

// held finds the sessions whose files a live process of their CLI holds
// open, which the CLIs do for a session they have loaded:
//   - Codex's daemon, <codex>/thread-writer-locks/<id>.lock. A hook's parent
//     is the daemon, which runs every TUI session, and the TUI's own process
//     names no id. The daemon keeps a session loaded for some minutes after
//     its terminal closes, so this says only that it may be open.
//   - agy, <antigravity>/presence/<id>.lock, for the conversation it runs.
//     agy --continue names no id, and a resumed conversation runs no hook,
//     so this is how one is known to be open.
//
// It asks lsof once, and only when the list has a session of either CLI.
func (a *app) held(l *store.List, lines []proc.Line) (codex, agy map[string]bool) {
	codex, agy = map[string]bool{}, map[string]bool{}
	want := func(cli string) bool {
		return slices.ContainsFunc(l.Entries, func(e store.Entry) bool { return e.CLI == cli })
	}
	var pids []int
	for _, ln := range lines {
		daemon, _ := agent.Daemon(ln.Fields)
		switch {
		case want(agent.Codex) && daemon && proc.Named(ln.Fields, "codex"),
			want(agent.Antigravity) && proc.Named(ln.Fields, "agy"):
			pids = append(pids, ln.PID)
		}
	}
	if len(pids) == 0 {
		return
	}
	files, err := a.openFiles(pids)
	if err != nil {
		return
	}
	for _, f := range files {
		id, ok := strings.CutSuffix(filepath.Base(f), ".lock")
		if !ok || !agent.PlainID(id) {
			continue
		}
		switch filepath.Base(filepath.Dir(f)) {
		case "thread-writer-locks":
			codex[id] = true
		case "presence":
			agy[id] = true
		}
	}
	return
}

// backgroundJobs maps the id of each Claude Code session running in the
// background to its job's id, from claude agents --json. It asks only when
// the list has a Claude Code session.
func (a *app) backgroundJobs(l *store.List) map[string]string {
	jobs := map[string]string{}
	if !slices.ContainsFunc(l.Entries, func(e store.Entry) bool { return e.CLI == agent.Claude }) {
		return jobs
	}
	out, err := a.claudeAgents()
	if err != nil {
		return jobs
	}
	var list []struct {
		ID        string `json:"id"`
		SessionID string `json:"sessionId"`
		Kind      string `json:"kind"`
	}
	if json.Unmarshal(out, &list) != nil {
		return jobs
	}
	for _, j := range list {
		if j.Kind == "background" && agent.PlainID(j.ID) && j.SessionID != "" {
			jobs[j.SessionID] = j.ID
		}
	}
	return jobs
}

// followContinued follows Claude Code's /background on the list. It ends a
// session and goes on with its conversation under a new id, in a
// background worker; the old transcript ends with a continued-in line
// naming the new id. So the old entry is replaced by the new one, which
// keeps the old one's role, and its start arguments while its own are the
// worker's, which carry none of the session's flags (a run resumed since
// keeps its own). If the new id's hook never ran, the old entry takes the new id.
// A /fork writes no such line, so its new entry stays beside the old.
func followContinued(l *store.List, h agent.Homes) bool {
	claude, _ := agent.Lookup(agent.Claude)
	changed := false
	for range 8 { // a session backgrounded again goes on once more
		moved := false
		for i := 0; i < len(l.Entries); i++ {
			e := l.Entries[i]
			if e.CLI != agent.Claude {
				continue
			}
			next := agent.ContinuedIn(claude.Transcript(h, e.ID), e.LastSeen)
			if next == "" || next == e.ID {
				continue
			}
			if j := l.Find(agent.Claude, next); j >= 0 {
				n := &l.Entries[j]
				if len(n.Args) == 0 || slices.Contains(n.Args, "bg-spare") {
					n.Args = e.Args // the worker's carry none of the session's flags
				}
				if n.Role == "" {
					n.Role = e.Role
				}
				l.Entries = slices.Delete(l.Entries, i, i+1)
			} else {
				l.Entries[i].ID = next
				l.Entries[i].PID = 0
			}
			moved, changed = true, true
			break
		}
		if !moved {
			break
		}
	}
	return changed
}

// command is the line typed into a row's tab: cd to its folder, then
// resume it, or attach to it in the background.
func (a *app) command(r row, noMessage bool) string {
	var argv []string
	if r.state == background {
		argv = []string{"claude", "attach", r.job}
	} else {
		msg := ""
		if !r.cli.Listens() && r.role != "" && !noMessage {
			msg = agent.Message
		}
		argv = r.cli.Resume(r.ID, r.flags, msg)
	}
	return "cd -- " + shell.Quote(r.Folder) + " && " + shell.Join(argv)
}

// where writes a session's folder short: from the project's own folder's
// name when it is in it ("reopen", "reopen/cmd"), else in full, as for a
// worktree elsewhere.
func (a *app) where(folder string) string {
	if a.root != "" {
		if rel, err := filepath.Rel(a.root, folder); err == nil && !strings.HasPrefix(rel, "..") {
			return agent.Clean(filepath.Join(filepath.Base(a.root), rel))
		}
	}
	return agent.Clean(a.tilde(folder))
}

// unsafe says whether an entry's command can't be typed as it is: its id
// isn't plain, or its folder or a flag holds a control character, which a
// shell would take as typed (a newline ends the line). Such a command is
// never typed, printed or shown cleaned, since cleaned it could name
// another folder.
func unsafe(e store.Entry, flags []agent.Flag) bool {
	return !agent.PlainID(e.ID) || agent.HasControl(e.Folder) || slices.ContainsFunc(agent.Words(flags), agent.HasControl)
}

// heldWord says why a session Codex still has loaded isn't offered.
const heldWord = "Codex still has it loaded, so it's open somewhere or closed minutes ago; reopen offers it once Codex lets it go"

// tilde writes a path in home with ~.
func (a *app) tilde(p string) string {
	if a.home != "" && (p == a.home || strings.HasPrefix(p, a.home+"/")) {
		return "~" + p[len(a.home):]
	}
	return p
}

// short is an id's first 8 characters, as reopen shows it.
func short(id string) string {
	if len(id) > 8 {
		id = id[:8]
	}
	return agent.Clean(id)
}

// ago says how long ago t was, roughly.
func ago(now, t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d/time.Hour))
	case d < 48*time.Hour:
		return "yesterday"
	}
	return fmt.Sprintf("%d days ago", int(d/(24*time.Hour)))
}

// sayDropped tells the operator which entries went unused for 30 days and
// were dropped.
func (a *app) sayDropped(w io.Writer, dropped []row) {
	if len(dropped) == 0 {
		return
	}
	var names []string
	for _, r := range dropped {
		n := r.cli.Short + " " + short(r.ID)
		if r.Role != "" {
			n += " (" + r.Role + ")"
		}
		names = append(names, n)
	}
	if !a.wrote {
		fmt.Fprintf(w, "reopen: %d %s unused for 30 days, which reopen drops when it next runs: %s.\n",
			len(dropped), plural(len(dropped), "session", "sessions"), strings.Join(names, ", "))
		return
	}
	fmt.Fprintf(w, "reopen: dropped %d %s unused for 30 days: %s. Their transcripts are untouched.\n",
		len(dropped), plural(len(dropped), "session", "sessions"), strings.Join(names, ", "))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
