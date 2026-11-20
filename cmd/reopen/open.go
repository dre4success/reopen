package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/dre4success/reopen/internal/agent"
)

// cmdOpen is reopen on its own: it lists the project's sessions that aren't
// open, and opens the ones the operator picks in one new iTerm2 window, a
// tab each. With --print it prints each tab's command and starts nothing.
func (a *app) cmdOpen(args []string) int {
	fs := flag.NewFlagSet("reopen", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	print := fs.Bool("print", false, "")
	noMessage := fs.Bool("no-message", false, "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		what := strings.Join(fs.Args(), " ")
		if err != nil {
			what = err.Error()
		}
		fmt.Fprintf(a.stderr, "reopen: %s\n\n%s", what, usage)
		return 2
	}
	if !*print {
		// Starting sessions, and typing to them, is the operator's to do.
		if v := a.agentSession(); v != "" {
			fmt.Fprintf(a.stderr, "reopen: refused in an agent's session (%s is set): it starts sessions and types to them, which only the operator does, at a terminal of their own. reopen --print and reopen list work here and start nothing.\n", v)
			return 1
		}
		if !a.terminal() {
			fmt.Fprintf(a.stderr, "reopen: refused: stdin isn't a terminal, and the operator picks the sessions there. reopen --print prints the commands and starts nothing.\n")
			return 1
		}
	}
	repo, err := a.repo()
	if err != nil {
		fmt.Fprintf(a.stderr, "reopen: %v\n", err)
		return 1
	}
	rows, dropped, err := a.view(repo, !*print)
	if err != nil {
		fmt.Fprintf(a.stderr, "reopen: %v\n", err)
		return 1
	}
	var offer, left []row
	var nOpen, nNever int
	for _, r := range rows {
		switch {
		case r.pickable():
			offer = append(offer, r)
		case r.leftOut() != "":
			left = append(left, r)
		case r.state == open:
			nOpen++
		case !r.resumable():
			nNever++
		}
	}
	s := style{on: a.color && !*print}
	// With --print, stdout holds only the commands; the rest goes to stderr.
	notes := a.stdout
	if *print {
		notes = a.stderr
	}
	a.sayDropped(notes, dropped)
	for _, r := range left {
		fmt.Fprintf(notes, "reopen: left out %s %s: %s.\n", r.cli.Short, short(r.ID), r.leftOut())
	}

	if *print {
		for _, r := range offer {
			cmd := a.command(r, *noMessage)
			switch {
			case r.ticked():
				fmt.Fprintln(a.stdout, cmd)
				if r.skips() {
					fmt.Fprintf(notes, "reopen: ⚠ %s %s comes back skipping permission checks.\n", r.cli.Short, short(r.ID))
				}
			case r.state == background:
				fmt.Fprintf(notes, "reopen: %s %s runs in the background, with no tab: %s\n", r.cli.Short, short(r.ID), cmd)
			case r.movedTo != "":
				fmt.Fprintf(notes, "reopen: not ticked, %s %s: its role %s is now session %s's: %s\n", r.cli.Short, short(r.ID), r.Role, short(r.movedTo), cmd)
			}
		}
		return 0
	}

	name := project(repo)
	if len(offer) == 0 {
		switch {
		case len(rows) == 0:
			fmt.Fprintf(a.stdout, "reopen: no session of %s is on the list yet. A session gets on it when it starts, once its CLI has reopen's hook, which reopen init adds; and a session handoff gives a role, without it.\n", name)
		case nOpen == len(rows):
			fmt.Fprintf(a.stdout, "reopen: every session of %s is open (%d). reopen list shows them.\n", name, nOpen)
		default:
			fmt.Fprintf(a.stdout, "reopen: no session of %s to bring back: %s. reopen list shows them all.\n", name, a.counts(nOpen, nNever, len(left)))
		}
		for _, n := range a.setupNotes() {
			fmt.Fprintln(a.stdout, n)
		}
		return 0
	}
	header := fmt.Sprintf("reopen · %s · %d %s to bring back", name, len(offer), plural(len(offer), "session", "sessions"))
	if c := a.counts(nOpen, nNever, len(left)); c != "" {
		header += s.dim(" (" + c + ")")
	}
	p := newPicker(offer)
	var ok bool
	if f := a.tty(); f != nil {
		if ok, err = a.pickRaw(f, p, header, s, *noMessage); err != nil {
			ok = a.pickLines(p, header, s, *noMessage)
		}
	} else {
		ok = a.pickLines(p, header, s, *noMessage)
	}
	picked := p.picked()
	if !ok || len(picked) == 0 {
		fmt.Fprintln(a.stdout, "reopen: nothing opened.")
		return 0
	}
	var commands []string
	for _, r := range picked {
		commands = append(commands, a.command(r, *noMessage))
	}
	if !a.itermInstalled() {
		fmt.Fprintln(a.stderr, "reopen: iTerm2 isn't in /Applications, and reopen opens its tabs there. Each line below brings one back, in a terminal of your own:")
		fmt.Fprintln(a.stdout, strings.Join(commands, "\n"))
		return 1
	}
	if tp := a.getenv("TERM_PROGRAM"); tp != "iTerm.app" {
		app := "this terminal"
		if tp != "" {
			app = tp
		}
		fmt.Fprintf(a.stdout, "reopen: macOS may ask whether %s may control iTerm2. Allow it, or reopen can't open the tabs.\n", app)
	}
	if err := a.openTabs(commands); err != nil {
		fmt.Fprintf(a.stderr, "reopen: %v\nNothing opened. Each line below brings one back, in a terminal of your own:\n", err)
		fmt.Fprintln(a.stdout, strings.Join(commands, "\n"))
		return 1
	}
	a.summary(p, s, *noMessage, nOpen)
	return 0
}

// counts says how many sessions aren't offered, and why.
func (a *app) counts(nOpen, nNever, nLeft int) string {
	var parts []string
	if nOpen > 0 {
		parts = append(parts, fmt.Sprintf("%d open", nOpen))
	}
	if nNever > 0 {
		parts = append(parts, fmt.Sprintf("%d never typed to", nNever))
	}
	if nLeft > 0 {
		parts = append(parts, fmt.Sprintf("%d left out", nLeft))
	}
	return strings.Join(parts, ", ")
}

// summary says what was opened, then what wasn't and why, a line each.
func (a *app) summary(p *picker, s style, noMessage bool, nOpen int) {
	picked := p.picked()
	fmt.Fprintln(a.stdout, s.bold(fmt.Sprintf("reopen · opened %d of %d in a new iTerm2 window", len(picked), len(p.rows))))
	c := a.columnsFor(p.rows)
	cols := a.cols()
	line := func(mark string, r row, what string) {
		head := "  " + mark + " " + s.cli(r.cli.Name, pad(r.cli.Short, c.cli)) + "  " + s.cyan(pad(fit(roleText(r), c.role), c.role)) + "  "
		room := 1 << 30
		if cols > 0 {
			room = cols - 1 - (4 + c.cli + 2 + c.role + 2)
		}
		fmt.Fprintln(a.stdout, strings.TrimRight(head+fit(what, room), " "))
	}
	join := func(parts ...string) string {
		var out []string
		for _, p := range parts {
			if p != "" {
				out = append(out, p)
			}
		}
		return strings.Join(out, " · ")
	}
	for i, r := range p.rows {
		if !p.tick[i] {
			continue
		}
		what := r.label
		switch {
		case r.state == background:
			what = join(what, "attached: claude attach "+r.job)
		case a.sendsMessage(r, noMessage):
			what = join(what, `told "`+agent.Message+`"`)
		}
		line(s.green("●"), r, what)
	}
	if len(picked) < len(p.rows) {
		fmt.Fprintln(a.stdout, s.dim("  not opened:"))
	}
	for i, r := range p.rows {
		if p.tick[i] {
			continue
		}
		why := ""
		switch {
		case r.state == background:
			why = "in the background: claude attach " + r.job
		case r.movedTo != "":
			why = "its role is session " + short(r.movedTo) + "'s now"
		}
		line(s.dim("○"), r, join(r.label, why))
	}
	if nOpen > 0 {
		fmt.Fprintln(a.stdout, s.dim(fmt.Sprintf("  %d already open · reopen list shows every session", nOpen)))
	}
}
