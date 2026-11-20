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
	// unrecorded: found through handoff, with no start arguments of its own,
	// so it comes back with only what its CLI's records keep.
	unrecorded bool
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
// brings the list up to date under its lock: it adds the sessions handoff
// gives roles that its hook never reported, follows Claude Code's
// /background, notes the roles it sees, and drops what has gone unused.
// Without, it shows the same and changes nothing.
func (a *app) view(repo string, write bool) ([]row, []row, error) {
	var rows, dropped []row
	// handoff's sessions, the processes and Claude Code's own list are asked
	// for before the list's lock is taken, so a hook never waits on them.
	a.root = gitx.MainRoot(repo)
	pre, err := store.Load(a.home, repo)
	if err != nil {
		return nil, nil, err
	}
	found := a.handoffSessions(repo, pre)
	if len(pre.Entries) == 0 && len(found) == 0 && (!write || len(pre.Forgotten) == 0) {
		return nil, nil, nil // an empty list needs no lock, nor ~/.reopen/ made for it
	}
	known := &store.List{Entries: slices.Clone(pre.Entries)}
	for _, f := range found {
		known.Entries = append(known.Entries, f.entry)
	}
	lines, _ := a.scan() // with no process list, only the recorded processes are checked
	jobs, live := a.claudeSessions(known)
	loaded, presence := a.held(known, lines)
	for i := range found {
		found[i].seen = a.adopt(&found[i], live)
	}
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
		changed := false
		for _, f := range found {
			switch i := l.Find(f.entry.CLI, f.entry.ID); {
			case slices.Contains(l.Forgotten, f.entry.ID):
			case i < 0:
				l.Entries = append(l.Entries, f.entry)
				changed = true
			case f.seen && l.Entries[i].Found != "" && f.entry.Started.After(l.Entries[i].Started):
				// Its process, seen running it now: a later run's, whose flags
				// may differ, since agy runs no hook on a resume. Later by when
				// the process started, not by when a reopen saw it: one that
				// saw an earlier run may take the lock last. Not once its own
				// hook has reported it, which made the entry its own.
				l.Entries[i] = f.entry
				changed = true
			}
		}
		// What forget dropped stays dropped while handoff names it; then it
		// needn't be kept.
		named := roles.Read(gitx.MainRoot(repo))
		if kept := slices.DeleteFunc(slices.Clone(l.Forgotten), func(id string) bool { return named[id] == "" }); len(kept) != len(l.Forgotten) {
			l.Forgotten, changed = kept, true
		}
		changed = followContinued(l, a.homes()) || changed
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
			case run[key{e.CLI, e.ID}], c.Name == agent.Antigravity && presence[e.ID],
				c.Name == agent.Claude && live[e.ID] > 0:
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
			// Its start arguments weren't recorded: no flag is made up for
			// it. Bypass mode, say, is reached with --permission-mode too, which
			// a resume doesn't keep (evidence/r1/e2e-claude.log), so a row in it
			// comes back without, and says so, as any row without the flag.
			r.unrecorded = e.Found != "" && len(e.Args) == 0
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

// claudeSessions asks claude agents --json which Claude Code sessions run
// now: jobs maps the id of each in the background to its job's id, and live
// the id of each in a terminal (interactive) to its process. It asks only
// when the list has a Claude Code session.
func (a *app) claudeSessions(l *store.List) (jobs map[string]string, live map[string]int) {
	jobs, live = map[string]string{}, map[string]int{}
	if !slices.ContainsFunc(l.Entries, func(e store.Entry) bool { return e.CLI == agent.Claude }) {
		return
	}
	out, err := a.claudeAgents()
	if err != nil {
		return
	}
	var list []struct {
		ID        string `json:"id"`
		SessionID string `json:"sessionId"`
		Kind      string `json:"kind"`
		PID       int    `json:"pid"`
	}
	if json.Unmarshal(out, &list) != nil {
		return
	}
	for _, j := range list {
		switch {
		case j.SessionID == "":
		case j.Kind == "background" && agent.PlainID(j.ID):
			jobs[j.SessionID] = j.ID
		case j.Kind == "interactive":
			live[j.SessionID] = max(j.PID, 1) // open, whether or not its process is known
		}
	}
	return
}

// foundSession is a session handoff gives a role that its hook never
// reported, as reopen found it.
type foundSession struct {
	entry  store.Entry
	handed roles.Session // handoff's record of it
	onList bool          // on the list already, as found through handoff
	seen   bool          // its process was seen running it, and taken
}

// handoffSessions are the sessions handoff gives roles in the project that
// the list lacks, or has only as found through handoff, as entries: each
// found by its id in its CLI's records, in a folder of this project, and
// used in the last 30 days. One forget dropped isn't found again.
func (a *app) handoffSessions(repo string, l *store.List) []foundSession {
	var out []foundSession
	h := a.homes()
	for _, s := range roles.Sessions(gitx.MainRoot(repo)) {
		if !agent.PlainID(s.ID) || slices.Contains(l.Forgotten, s.ID) {
			continue
		}
		f := foundSession{entry: store.Entry{ID: s.ID, Role: s.Role, LastSeen: s.Since, Found: "handoff"}, handed: s}
		if i := slices.IndexFunc(l.Entries, func(e store.Entry) bool { return e.ID == s.ID }); i >= 0 {
			if l.Entries[i].Found == "" {
				continue // its hook reported it
			}
			f.entry, f.onList = l.Entries[i], true
			out = append(out, f)
			continue
		}
		c, t := agent.Find(h, s.ID)
		if c == nil || a.now().Sub(agent.LastUse(t)) > unusedFor {
			continue
		}
		f.entry.CLI, f.entry.Folder = c.Name, c.Folder(h, s.ID, t)
		if f.entry.Folder == "" {
			continue
		}
		if common, err := gitx.CommonDir(f.entry.Folder); err != nil || common != repo {
			continue // not this project's, or its folder is gone
		}
		out = append(out, f)
	}
	return out
}

// adopt takes the arguments of the process that runs a found session now,
// as its hook would have, when something says that process runs that
// session: for Claude Code, its own list of the sessions in terminals; for
// Antigravity, the session's presence lock, held by the process handoff
// noted. A process's start time and name can't tell which session it runs.
// Arguments that resume another session aren't taken. Codex's never are:
// none of its flags come back, and its hooks' parent is mostly its daemon.
func (a *app) adopt(f *foundSession, live map[string]int) bool {
	c, ok := agent.Lookup(f.entry.CLI)
	if !ok {
		return false
	}
	id, pid := f.entry.ID, 0
	switch c.Name {
	case agent.Claude:
		pid = live[id]
	case agent.Antigravity:
		if f.handed.PID > 0 {
			files, err := a.openFiles([]int{f.handed.PID})
			if err == nil && slices.ContainsFunc(files, func(p string) bool {
				return filepath.Base(p) == id+".lock" && filepath.Base(filepath.Dir(p)) == "presence"
			}) {
				pid = f.handed.PID
			}
		}
	}
	if pid <= 1 {
		return false
	}
	info, ok := a.process(pid)
	if !ok || !proc.Named(info.Args, c.Command) || !c.Interactive(info.Args) {
		return false
	}
	if r := c.ResumedID(info.Args); r != "" && r != id {
		return false
	}
	f.entry.PID, f.entry.Args, f.entry.LastSeen, f.entry.Started = pid, info.Args, a.now(), info.Start
	return true
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
