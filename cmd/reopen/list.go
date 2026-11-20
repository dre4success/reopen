package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/dre4success/reopen/internal/agent"
	"github.com/dre4success/reopen/internal/store"
	"github.com/dre4success/reopen/internal/term"
)

// cmdList shows every session on the project's list, open or not. It
// starts nothing.
func (a *app) cmdList(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("list takes no arguments\n\n%s", usage)
	}
	repo, err := a.repo()
	if err != nil {
		return err
	}
	rows, dropped, err := a.view(repo, false)
	if err != nil {
		return err
	}
	a.sayDropped(a.stdout, dropped)
	a.sayHookLog()
	if len(rows) == 0 {
		if len(dropped) == 0 {
			fmt.Fprintf(a.stdout, "reopen: no session of %s is on the list. A session gets on it when it starts, once its CLI has reopen's hook (reopen hooks).\n", project(repo))
		}
		return nil
	}
	s := style{on: a.color}
	fmt.Fprintf(a.stdout, "%s\n\n", s.bold(fmt.Sprintf("reopen · %s · %d %s, newest first", project(repo), len(rows), plural(len(rows), "session", "sessions"))))
	c := a.columnsFor(rows)
	for _, r := range rows {
		st := pad(stateText(r), 10)
		lead := "  " + st + "  "
		switch r.state {
		case open:
			lead = "  " + s.green(st) + "  "
		case background:
			lead = "  " + s.cyan(st) + "  "
		}
		for _, l := range a.rowLines(r, s, c, lead, 14, a.cols(), false, false) {
			fmt.Fprintln(a.stdout, l)
		}
	}
	return nil
}

// cols is the terminal's width when stdout is one, else 0: no limit, so
// what is piped isn't cut.
func (a *app) cols() int {
	if f := a.tty(); f != nil {
		c, _ := term.Size(f)
		return c
	}
	return 0
}

func stateText(r row) string {
	switch {
	case r.held:
		return "loaded"
	case r.state == open:
		return "open"
	case r.state == background:
		return "background"
	case !r.resumable():
		return "never used"
	}
	return "closed"
}

// cmdForget drops the entry of one session, by its id or the start of it,
// or with no id every entry of this project, and prints each one's command,
// so nothing is lost. A transcript is never touched. With no id it also
// starts over a list that can't be read.
func (a *app) cmdForget(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("forget takes one id at most\n\n%s", usage)
	}
	// The list is the operator's: what it drops, reopen won't bring back.
	if v := a.agentSession(); v != "" {
		return fmt.Errorf("forget is refused in an agent's session (%s is set): the list is the operator's to change", v)
	}
	repo, err := a.repo()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		l, err := store.Load(a.home, repo)
		if err != nil {
			if err := store.Reset(a.home, repo); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "reopen: %s couldn't be read, so the list of %s starts over, empty.\n", a.tilde(store.Path(a.home, repo)), project(repo))
			return nil
		}
		if len(l.Entries) == 0 {
			fmt.Fprintf(a.stdout, "reopen: the list of %s is empty already.\n", project(repo))
			return nil
		}
	}
	var gone []store.Entry
	err = store.Update(a.home, repo, func(l *store.List) (bool, error) {
		if len(args) == 0 {
			gone, l.Entries = l.Entries, nil
			return len(gone) > 0, nil
		}
		id := args[0]
		var match []int
		for i, e := range l.Entries {
			if e.ID == id {
				match = []int{i}
				break
			}
			if len(id) >= 4 && strings.HasPrefix(e.ID, id) {
				match = append(match, i)
			}
		}
		switch len(match) {
		case 0:
			return false, fmt.Errorf("no session %s on the list of %s", id, project(repo))
		case 1:
		default:
			return false, fmt.Errorf("%d sessions start with %s; give more of the id", len(match), id)
		}
		gone = append(gone, l.Entries[match[0]])
		l.Entries = slices.Delete(l.Entries, match[0], match[0]+1)
		return true, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "reopen: forgot %d %s of %s. No transcript was touched; each comes back by hand with:\n",
		len(gone), plural(len(gone), "session", "sessions"), project(repo))
	for _, e := range gone {
		c, ok := agent.Lookup(e.CLI)
		if !ok {
			continue
		}
		r := row{Entry: e, cli: c, flags: c.Flags(e.Args, e.Folder)}
		if unsafe(e, r.flags) {
			// Cleaned, the command could name another folder: none, then.
			fmt.Fprintf(a.stdout, "  (%s %s: no command, as its folder or a flag holds a control character)\n", c.Short, short(e.ID))
			continue
		}
		fmt.Fprintf(a.stdout, "  %s\n", a.command(r, true))
	}
	return nil
}

// sayHookLog points at the hook's log when it noted something in the last
// week: a session the hook couldn't record isn't on the list.
func (a *app) sayHookLog() {
	p := store.LogPath(a.home)
	st, err := os.Stat(p)
	if err != nil || a.now().Sub(st.ModTime()) > 7*24*time.Hour {
		return
	}
	data, _ := os.ReadFile(p)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	fmt.Fprintf(a.stdout, "reopen: the hook couldn't record a session %s; the latest: %s\n  (%s has the rest)\n",
		ago(a.now(), st.ModTime()), agent.Clean(lines[len(lines)-1]), a.tilde(p))
}
