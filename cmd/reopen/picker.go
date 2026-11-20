package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/dre4success/reopen/internal/agent"
	"github.com/dre4success/reopen/internal/term"
)

// picker is the operator's pick: the rows offered, which are ticked, and
// the row the cursor is on. It knows nothing of the terminal, so its keys
// are tested on their own.
type picker struct {
	rows   []row
	tick   []bool
	cur    int
	detail bool // the row under the cursor is shown in full, on a screen of its own
	scroll int  // how far down that screen is
}

func newPicker(rows []row) *picker {
	p := &picker{rows: rows, tick: make([]bool, len(rows))}
	for i, r := range rows {
		p.tick[i] = r.ticked()
	}
	return p
}

// ticked counts the ticked rows.
func (p *picker) ticked() int {
	n := 0
	for _, t := range p.tick {
		if t {
			n++
		}
	}
	return n
}

// picked are the ticked rows, in order.
func (p *picker) picked() []row {
	var out []row
	for i, r := range p.rows {
		if p.tick[i] {
			out = append(out, r)
		}
	}
	return out
}

type action int

const (
	stay action = iota
	confirm
	cancel
)

// key acts on one key: arrows or j and k move, space or x ticks, a ticks
// all, n none, a digit ticks that row, i (or →) shows the row in full,
// Enter opens, and q, Esc, Ctrl-C or Ctrl-D leave with nothing opened.
func (p *picker) key(k string) action {
	if p.detail {
		// The row in full: arrows scroll it; ←, i or Esc go back to the
		// list; space, Enter and q do as on the list.
		switch k {
		case "up", "k", "\x10":
			p.scroll = max(p.scroll-1, 0)
		case "down", "j", "\x0e", "\t":
			p.scroll++ // render keeps it in range
		case "home", "g":
			p.scroll = 0
		case "left", "h", "i", "?", "esc":
			p.detail, p.scroll = false, 0
		case " ", "x":
			p.tick[p.cur] = !p.tick[p.cur]
		case "\r", "\n":
			return confirm
		case "q", "\x03", "\x04":
			return cancel
		}
		return stay
	}
	n := len(p.rows)
	switch k {
	case "up", "k", "\x10":
		p.cur = (p.cur - 1 + n) % n
	case "down", "j", "\x0e", "\t":
		p.cur = (p.cur + 1) % n
	case "home", "g":
		p.cur = 0
	case "end", "G":
		p.cur = n - 1
	case " ", "x":
		p.tick[p.cur] = !p.tick[p.cur]
	case "a":
		for i := range p.tick {
			p.tick[i] = true
		}
	case "n":
		for i := range p.tick {
			p.tick[i] = false
		}
	case "i", "?", "right", "l":
		p.detail, p.scroll = true, 0
	case "\r", "\n":
		return confirm
	case "q", "esc", "\x03", "\x04":
		return cancel
	default:
		if d, err := strconv.Atoi(k); err == nil && d >= 1 && d <= n && len(k) == 1 {
			p.cur = d - 1
			p.tick[d-1] = !p.tick[d-1]
		}
	}
	return stay
}

// keys splits what the terminal sent into keys. A read can hold several
// (typed fast, or pasted), and an arrow's sequence can be split between
// reads, so a lone Esc, or the start of a sequence, at the end is left in
// rest for the next read; partial says so. After a moment with nothing more,
// the caller takes a lone Esc as the key.
func keys(buf string) (out []string, rest string) {
	for len(buf) > 0 {
		if buf[0] != 0x1b {
			r, size := utf8.DecodeRuneInString(buf)
			if r == utf8.RuneError && size <= 1 && !utf8.FullRuneInString(buf) {
				return out, buf // half a character
			}
			out = append(out, buf[:size])
			buf = buf[size:]
			continue
		}
		if len(buf) == 1 {
			return out, buf // Esc, or the start of a sequence
		}
		switch buf[1] {
		case '[': // CSI: parameters, then a final byte from @ to ~
			end := strings.IndexFunc(buf[2:], func(r rune) bool { return r >= '@' && r <= '~' })
			if end < 0 {
				return out, buf
			}
			out = append(out, csiKey(buf[:2+end+1]))
			buf = buf[2+end+1:]
		case 'O': // SS3: one more byte
			if len(buf) < 3 {
				return out, buf
			}
			out = append(out, csiKey("\x1b["+buf[2:3]))
			buf = buf[3:]
		default: // Esc, then another key
			out = append(out, "esc")
			buf = buf[1:]
		}
	}
	return out, ""
}

func csiKey(seq string) string {
	switch seq {
	case "\x1b[200~":
		return "paste-start" // bracketed paste: what follows was pasted, not typed
	case "\x1b[201~":
		return "paste-end"
	}
	switch seq[len(seq)-1] {
	case 'A':
		return "up"
	case 'B':
		return "down"
	case 'C':
		return "right"
	case 'D':
		return "left"
	case 'H':
		return "home"
	case 'F':
		return "end"
	}
	return "" // a key the picker doesn't use
}

// render draws the picker in at most cols columns and lines lines (0: no
// limit). Rows that don't fit are scrolled, the cursor's always shown.
func (a *app) render(p *picker, header string, s style, cols, lines int, noMessage bool) []string {
	c := a.columnsFor(p.rows)
	blocks := make([][]string, len(p.rows))
	for i, r := range p.rows {
		mark := s.dim("○")
		if p.tick[i] {
			mark = s.green("●")
		}
		cursor := "  "
		if i == p.cur {
			cursor = s.bold("› ")
		}
		blocks[i] = a.rowLines(r, s, c, cursor+mark+" ", 4, cols, noMessage, i == p.cur)
	}
	if p.detail {
		return a.renderDetail(p, blocks[p.cur][0], s, cols, lines, noMessage)
	}
	n := p.ticked()
	open := "nothing ticked"
	if n > 0 {
		open = fmt.Sprintf("opens %d in a new iTerm2 window", n)
	}
	help := s.dim("↑↓ move · space tick · a all · n none · i details · ") + s.bold("enter") + s.dim(" "+open+" · q quit")
	if cols > 0 && width(agent.Clean(help)) > cols-1 {
		help = s.dim("↑↓ · space · a · n · i · ") + s.bold("enter") + s.dim(fmt.Sprintf(" opens %d · q", n))
	}
	// Before Enter, what will come back skipping permission checks.
	var foot []string
	skipping := 0
	for i, r := range p.rows {
		if p.tick[i] && r.skips() {
			skipping++
		}
	}
	if skipping > 0 {
		foot = append(foot, s.red(fmt.Sprintf("%s: %d of the ticked %s", warning, skipping, plural(n, "one", "ones"))))
	}
	foot = append(foot, help)
	out := []string{s.bold(header), ""}
	room := 1 << 30
	if lines > 0 {
		// The header, the ↑ and ↓ lines, a blank line, the foot, and one
		// spare line, so the terminal never scrolls under the picker.
		room = max(lines-len(out)-2-1-len(foot)-1, 1)
	}
	// The window of rows: from first, as many as fit, with the cursor's in it.
	first := 0
	fits := func(from, to int) bool {
		h := 0
		for i := from; i <= to; i++ {
			h += len(blocks[i])
		}
		return h <= room
	}
	for first < p.cur && !fits(first, p.cur) {
		first++
	}
	last := first
	for last+1 < len(blocks) && fits(first, last+1) {
		last++
	}
	if first > 0 {
		out = append(out, s.dim(fmt.Sprintf("    ↑ %d more", first)))
	}
	for i := first; i <= last; i++ {
		b := blocks[i]
		if len(b) > room { // a row taller than the terminal
			b = b[:room]
		}
		out = append(out, b...)
	}
	if last < len(blocks)-1 {
		out = append(out, s.dim(fmt.Sprintf("    ↓ %d more", len(blocks)-1-last)))
	}
	out = append(append(out, ""), foot...)
	for i := range out {
		out[i] = clip(out[i], cols)
	}
	return out
}

// renderDetail draws the row under the cursor in full, on a screen of its
// own: its first line, the permission warning if it skips checks, then its
// details, scrolled to p.scroll and kept within cols and lines.
func (a *app) renderDetail(p *picker, first string, s style, cols, lines int, noMessage bool) []string {
	r := p.rows[p.cur]
	body := a.detailLines(r, s, 4, cols, noMessage)
	head := []string{s.bold(fmt.Sprintf("reopen · %d of %d in full", p.cur+1, len(p.rows))), "", first}
	if r.skips() {
		head = append(head, "    "+s.red(warning)) // always in view
	}
	tick := "space tick"
	if p.tick[p.cur] {
		tick = "space untick"
	}
	help := s.dim("↑↓ scroll · "+tick+" · ← back · ") + s.bold("enter") + s.dim(fmt.Sprintf(" opens %d · q quit", p.ticked()))
	if cols > 0 && width(agent.Clean(help)) > cols-1 {
		help = s.dim("↑↓ · space · ← · ") + s.bold("enter") + s.dim(" · q")
	}
	room := len(body)
	if lines > 0 {
		// The head, the ↑ and ↓ lines, a blank line, the help and a spare.
		room = max(lines-len(head)-2-1-1-1, 1)
	}
	p.scroll = min(p.scroll, max(len(body)-room, 0))
	out := head
	if p.scroll > 0 {
		out = append(out, s.dim(fmt.Sprintf("    ↑ %d more", p.scroll)))
	}
	end := min(p.scroll+room, len(body))
	out = append(out, body[p.scroll:end]...)
	if end < len(body) {
		out = append(out, s.dim(fmt.Sprintf("    ↓ %d more", len(body)-end)))
	}
	out = append(out, "", help)
	for i := range out {
		out[i] = clip(out[i], cols)
	}
	return out
}

// The smallest terminal the picker draws in: below, it asks line by line.
// From 10 lines a frame always fits with a line to spare.
const minCols, minLines = 30, 10

// leaves says whether k leaves the picker: q, Esc, Ctrl-C or Ctrl-D.
func leaves(k string) bool { return k == "q" || k == "esc" || k == "\x03" || k == "\x04" }

// pickRaw runs the picker in the terminal f, key by key, and says whether
// the operator confirmed. The terminal is put back on every way out: a
// return, a panic, or a signal. err means raw mode couldn't be had, and
// the caller asks line by line instead.
func (a *app) pickRaw(f *os.File, p *picker, header string, s style, noMessage bool) (ok bool, err error) {
	if cols, lines := term.Size(f); cols < minCols || lines < minLines {
		return false, errors.New("the terminal is too small to pick in")
	}
	restore, err := term.MakeRaw(f)
	if err != nil {
		return false, err
	}
	out := a.stdout
	drawn := 0
	erase := func() {
		if drawn > 0 {
			fmt.Fprintf(out, "\r\x1b[%dA\x1b[J", drawn)
			drawn = 0
		}
	}
	done := func() {
		erase()
		fmt.Fprint(out, "\x1b[?2004l\x1b[?25h") // bracketed paste off, the cursor shown again
		restore()
	}
	defer func() {
		if r := recover(); r != nil {
			done()
			panic(r)
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGWINCH)
	defer signal.Stop(sig)
	reads := make(chan string)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := f.Read(buf)
			if err != nil || n == 0 {
				close(reads)
				return
			}
			reads <- string(buf[:n])
		}
	}()
	small := false // the terminal is too small to show the picker
	draw := func() {
		cols, lines := term.Size(f)
		erase()
		ls := a.render(p, header, s, cols, lines, noMessage)
		if small = cols < minCols || lines < minLines; small {
			// Made too small while picking: one line, until it is larger.
			ls = []string{clip("reopen: too small: enlarge, or q quits", cols)}
		}
		fmt.Fprint(out, strings.Join(ls, "\r\n")+"\r\n")
		drawn = len(ls)
	}
	fmt.Fprint(out, "\x1b[?25l\x1b[?2004h") // the cursor hidden while picking; pastes marked
	draw()
	pending := ""
	var wait <-chan time.Time // set while a lone Esc may yet start a sequence
	pasting := false
	act := func(ks []string) (action, bool) {
		for _, k := range ks {
			// A paste is ignored whole: a newline in it isn't Enter. While
			// the terminal is too small to show the picker, only leaving
			// works: nothing is ticked or opened unseen.
			switch {
			case k == "paste-start":
				pasting = true
				continue
			case k == "paste-end":
				pasting = false
				continue
			case pasting:
				continue
			case small && !leaves(k):
				continue
			}
			if r := p.key(k); r != stay {
				return r, true
			}
		}
		return stay, false
	}
	for {
		select {
		case chunk, open := <-reads:
			if !open {
				done()
				return false, nil
			}
			var ks []string
			ks, pending = keys(pending + chunk)
			wait = nil
			if pending != "" {
				wait = time.After(50 * time.Millisecond)
			}
			if r, end := act(ks); end {
				done()
				return r == confirm, nil
			}
			draw()
		case <-wait:
			// Nothing followed: a lone Esc is Esc; half a sequence is dropped.
			ks := []string{}
			if pending == "\x1b" {
				ks = append(ks, "esc")
			}
			pending, wait = "", nil
			if r, end := act(ks); end {
				done()
				return r == confirm, nil
			}
			draw()
		case got := <-sig:
			if got == syscall.SIGWINCH {
				draw()
				continue
			}
			done()
			os.Exit(130)
		}
	}
}

// pickLines asks line by line, where raw mode can't be had: Enter opens
// the ticked rows, numbers tick and untick, a ticks all, n none, q quits.
func (a *app) pickLines(p *picker, header string, s style, noMessage bool) bool {
	in := bufio.NewScanner(a.stdin)
	c := a.columnsFor(p.rows)
	show := true
	for {
		if show {
			fmt.Fprintln(a.stdout, s.bold(header))
			fmt.Fprintln(a.stdout)
			for i, r := range p.rows {
				box := "[ ]"
				if p.tick[i] {
					box = "[x]"
				}
				lead := fmt.Sprintf("%2d %s ", i+1, box)
				for _, l := range a.rowLines(r, s, c, lead, len(lead), 0, noMessage, false) {
					fmt.Fprintln(a.stdout, l)
				}
			}
			fmt.Fprintf(a.stdout, "\nEnter opens the ticked ones (%d) in a new iTerm2 window, a tab each.\nNumbers tick or untick (1 3), a ticks all, n none, q quits.\n", p.ticked())
		}
		fmt.Fprint(a.stdout, "> ")
		if !in.Scan() {
			fmt.Fprintln(a.stdout)
			return false
		}
		line := strings.TrimSpace(in.Text())
		show = true
		switch strings.ToLower(line) {
		case "":
			return true
		case "q":
			return false
		case "a", "n":
			p.key(strings.ToLower(line))
			continue
		}
		// Every number is checked before any is ticked, so a typo changes
		// nothing.
		var nums []int
		for _, w := range strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == ',' }) {
			n, err := strconv.Atoi(w)
			if err != nil || n < 1 || n > len(p.rows) {
				fmt.Fprintf(a.stdout, "reopen: %q isn't one of 1 to %d.\n", w, len(p.rows))
				nums, show = nil, false
				break
			}
			nums = append(nums, n)
		}
		for _, n := range nums {
			p.tick[n-1] = !p.tick[n-1]
		}
	}
}
