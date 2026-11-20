package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dre4success/reopen/internal/agent"
	"github.com/dre4success/reopen/internal/shell"
)

// style colours what reopen prints, when it prints to a terminal and
// NO_COLOR isn't set. Every colour goes with words that say the same, so
// nothing is lost without it.
type style struct{ on bool }

func (s style) wrap(code, text string) string {
	if !s.on || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (s style) bold(t string) string   { return s.wrap("1", t) }
func (s style) dim(t string) string    { return s.wrap("2", t) }
func (s style) red(t string) string    { return s.wrap("1;31", t) }
func (s style) yellow(t string) string { return s.wrap("33", t) }
func (s style) green(t string) string  { return s.wrap("32", t) }
func (s style) cyan(t string) string   { return s.wrap("36", t) }

// cli colours a CLI's name, each its own.
func (s style) cli(name, text string) string {
	switch name {
	case agent.Claude:
		return s.wrap("38;5;173", text)
	case agent.Codex:
		return s.wrap("38;5;79", text)
	}
	return s.wrap("38;5;75", text)
}

// width is how many columns a string takes in a terminal, roughly: wide
// characters (CJK, emoji) take two, combining marks none.
func width(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

func runeWidth(r rune) int {
	switch {
	case unicode.Is(unicode.Mn, r) || r == 0x200d || (r >= 0xfe00 && r <= 0xfe0f):
		return 0
	case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0xa4cf, r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f, r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6, r >= 0x1f300 && r <= 0x1faff, r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}

// fit cuts s to n columns, ending with … when cut.
func fit(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if width(s) <= n {
		return s
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		if w+runeWidth(r) > n-1 {
			break
		}
		b.WriteRune(r)
		w += runeWidth(r)
	}
	return strings.TrimRight(b.String(), " ") + "…"
}

// pad fills s with spaces to n columns.
func pad(s string, n int) string {
	if w := width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// humanSize writes a size as the operator reads it.
func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 10<<20:
		return fmt.Sprintf("%d MB", n>>20)
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

// roleText is the role column: the role it holds; in brackets, the role it
// had, which another session holds now or no one does (its note says
// which); or "–".
func roleText(r row) string {
	switch {
	case r.role != "":
		return agent.Clean(r.role)
	case r.Role != "":
		return "(" + agent.Clean(r.Role) + ")"
	}
	return "–"
}

// skips says whether the row comes back skipping permission checks.
func (r row) skips() bool {
	return r.state != background && (agent.SkipsChecks(r.flags) || agent.ModeSkips(r.mode) ||
		agent.SettingsSkip(r.codex.Approval, r.codex.Sandbox))
}

// sendsMessage says whether its tab sends the first message.
func (a *app) sendsMessage(r row, noMessage bool) bool {
	return r.state != background && !r.cli.Listens() && r.role != "" && !noMessage
}

// columns are the widths of a list's first columns, from its rows.
type columns struct{ cli, role, ago int }

func (a *app) columnsFor(rows []row) columns {
	c := columns{cli: 6, role: 1, ago: 1}
	for _, r := range rows {
		c.role = max(c.role, min(width(roleText(r)), 16))
		c.ago = max(c.ago, width(ago(a.now(), used(r))))
	}
	return c
}

// how says how a row comes back: what its CLI keeps by itself and what
// reopen passes on.
func (a *app) how(r row) string {
	if r.state == background {
		return "in the background · claude attach " + r.job
	}
	var how []string
	if r.mode != "" && r.mode != "default" {
		how = append(how, r.mode+" mode")
	}
	if r.cli.Name == agent.Codex {
		c := r.codex
		if !c.Known {
			how = append(how, "settings unknown")
		} else {
			var cs []string
			for _, v := range []string{c.Model, labelled("approvals", c.Approval), c.Sandbox} {
				if v != "" {
					cs = append(cs, v)
				}
			}
			for _, root := range c.Roots {
				cs = append(cs, "+ "+a.tilde(root))
			}
			how = append(how, strings.Join(cs, ", "))
		}
	}
	if len(r.flags) > 0 {
		how = append(how, shell.Join(agent.Words(r.flags)))
	}
	// Shown, never typed: what a folder or flag holds can't reach the
	// terminal as an escape (the row is left out of the commands then).
	return agent.Clean(strings.Join(how, " "))
}

func labelled(name, v string) string {
	if v == "" {
		return ""
	}
	return name + " " + v
}

// note says what is unusual about a row, or "".
func (a *app) note(r row, s style, noMessage bool) []string {
	var notes []string
	switch {
	case r.unsafe:
		notes = append(notes, s.red("left out: "+r.leftOut()))
	case r.gone:
		notes = append(notes, s.yellow("left out: its folder is gone"))
	case r.held:
		notes = append(notes, s.yellow(heldWord))
	case r.movedTo != "":
		notes = append(notes, s.yellow("not ticked: its role "+agent.Clean(r.Role)+" is now session "+short(r.movedTo)+"'s"))
	case r.Role != "" && r.role == "":
		notes = append(notes, s.dim("handoff no longer lists its role "+agent.Clean(r.Role)+"; give it again if it should have it"))
	}
	if r.modeLost != "" && r.state != background {
		notes = append(notes, s.dim("it was in "+r.modeLost+" mode, which a resume doesn't keep without the flag that allows it"))
	}
	if r.cli.Name == agent.Claude && r.state != background && !r.unsafe && !r.gone && slices.Contains(agent.Words(r.flags), "--add-dir") {
		// claude --resume asks first whether to add each, and its default
		// is No, exit (evidence/r1/claude-probe.log A).
		notes = append(notes, s.dim("Claude Code asks again whether to add its folders; the default answer exits"))
	}
	if a.sendsMessage(r, noMessage) && (r.state != open || r.held) {
		notes = append(notes, s.dim(`first message: "`+agent.Message+`"`))
	}
	return notes
}

const warning = "⚠ skips permission checks"

// rowLines writes a row as lines of at most cols columns (0: no limit):
// its CLI, role, last use and label; then the warning, if it skips
// permission checks, where it comes back and how, its size and id; then,
// if any, what is unusual about it. lead starts its first line (a tick box,
// a state), and indent, its width, the others. Every line is cut to the
// width, so a terminal never wraps one and a redraw erases what it drew.
func (a *app) rowLines(r row, s style, c columns, lead string, indent, cols int, noMessage, active bool) []string {
	role := fit(roleText(r), c.role)
	label := r.label
	if label == "" && r.cli.Name != agent.Antigravity {
		label = "(no message yet)"
		if r.resumable() {
			label = "(no label)"
		}
	}
	if active {
		label = s.bold(label) // the row under the cursor
	}
	first := lead + s.cli(r.cli.Name, pad(r.cli.Short, c.cli)) + "  " + s.cyan(pad(role, c.role)) + "  " + s.dim(pad(ago(a.now(), used(r)), c.ago)) + "  " + label

	in := strings.Repeat(" ", indent)
	parts := []string{a.where(r.Folder)}
	if h := a.how(r); h != "" {
		parts = append(parts, h)
	}
	if r.size > 0 {
		parts = append(parts, humanSize(r.size))
	}
	second := in
	if r.skips() {
		second += s.red(warning) + "  "
	}
	second += s.dim(strings.Join(append(parts, short(r.ID)), " · "))

	lines := []string{first, second}
	for _, n := range a.note(r, s, noMessage) {
		lines = append(lines, in+n)
	}
	for i := range lines {
		lines[i] = clip(strings.TrimRight(lines[i], " "), cols)
	}
	return lines
}

// detailLines write a row in full, wrapped to cols: its folder, how it
// comes back, the added folders, the exact line its tab will type, its
// transcript and its id. The picker shows them for the row under the
// cursor on i.
func (a *app) detailLines(r row, s style, indent, cols int, noMessage bool) []string {
	in := strings.Repeat(" ", indent)
	// An exact value (the command, a folder, a path, an id) is shown as it
	// is, broken only at the edge; prose (the first message) is wrapped at
	// spaces.
	exact := map[string]bool{"folder": true, "with": true, "types": true, "history": true, "id": true}
	field := func(name, value string) []string {
		head := in + pad(name, 9)
		n := 1 << 30 // no limit
		if cols > 0 {
			// The width left after the name: never more, or the cut that
			// fits every line to the screen would drop part of an exact
			// value. From 30 columns, the picker's floor, that is 16.
			n = max(cols-1-width(head), 4)
		}
		var out []string
		if exact[name] {
			out = verbatim(value, n, s)
		} else {
			out = wrap(agent.Clean(value), n)
		}
		for i := range out {
			if i == 0 {
				out[i] = s.dim(head) + out[i]
			} else {
				out[i] = strings.Repeat(" ", width(head)) + out[i]
			}
		}
		return out
	}
	var lines []string
	if first := r.cli.First(r.transcript); first != "" && first != r.label {
		lines = append(lines, field("began", first)...)
	}
	lines = append(lines, field("folder", a.tilde(r.Folder))...)
	if h := a.how(r); h != "" {
		lines = append(lines, field("with", h)...)
	}
	if r.unsafe {
		lines = append(lines, in+s.red("types    nothing: "+r.leftOut()))
	} else {
		lines = append(lines, field("types", a.command(r, noMessage))...)
	}
	if r.transcript != "" {
		lines = append(lines, field("history", a.tilde(r.transcript)+" · "+humanSize(r.size))...)
	}
	lines = append(lines, field("id", r.ID)...)
	for _, n := range a.note(r, s, noMessage) {
		lines = append(lines, in+n)
	}
	for i := range lines {
		lines[i] = clip(lines[i], cols)
	}
	return lines
}

// verbatim breaks an exact value into lines of at most n columns, every
// byte kept, spaces too: a break comes only at the edge, and a dim ↵ ends
// each line that goes on. With no limit (n huge) it is one line. Its value
// is shown, never typed, and only for a row whose words are all printable
// (unsafe() leaves the others out), but it is cleaned all the same, which
// then changes nothing.
func verbatim(value string, n int, s style) []string {
	value = agent.Clean(value)
	if cols := n; width(value) <= cols {
		return []string{value}
	}
	var out []string
	line, w := "", 0
	for _, r := range value {
		if w+runeWidth(r) > n-1 && line != "" { // a column for the ↵
			out = append(out, line+s.dim("↵"))
			line, w = "", 0
		}
		line += string(r)
		w += runeWidth(r)
	}
	return append(out, line)
}

// wrap breaks text into lines of at most n columns, at spaces where it can
// and anywhere in a word longer than a line, as a path can be.
func wrap(text string, n int) []string {
	var out []string
	line := ""
	for _, w := range strings.Fields(text) {
		for width(w) > n {
			if line != "" {
				out, line = append(out, line), ""
			}
			// As much of the word as takes n columns at most, at least one
			// character.
			cut, cw := 0, 0
			for i, r := range w {
				if cw+runeWidth(r) > n && i > 0 {
					break
				}
				cw += runeWidth(r)
				cut = i + utf8.RuneLen(r)
			}
			out = append(out, w[:cut])
			w = w[cut:]
		}
		switch {
		case line == "":
			line = w
		case width(line)+1+width(w) <= n:
			line += " " + w
		default:
			out, line = append(out, line), w
		}
	}
	if line != "" || len(out) == 0 {
		out = append(out, line)
	}
	return out
}

// clip cuts a line that may hold colour to cols-1 columns (0: no limit),
// ending it with … and a reset when cut.
func clip(line string, cols int) string {
	if cols <= 0 || width(agent.Clean(line)) <= cols-1 {
		return line
	}
	limit := cols - 2 // room for …
	var b strings.Builder
	w, colour := 0, false
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			if m := escape.FindStringIndex(line[i:]); m != nil && m[0] == 0 {
				b.WriteString(line[i : i+m[1]])
				i += m[1]
				colour = true
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if w+runeWidth(r) > limit {
			break
		}
		b.WriteRune(r)
		w += runeWidth(r)
		i += size
	}
	b.WriteString("…")
	if colour {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// escape is a colour or cursor sequence, as style writes them.
var escape = regexp.MustCompile("^\x1b\\[[0-?]*[ -/]*[@-~]")
