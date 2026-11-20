package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dre4success/reopen/internal/agent"
	"github.com/dre4success/reopen/internal/store"
)

// codexRollout writes a Codex rollout with its session_meta line.
func (w *world) codexRollout(id, originator, source, first string) string {
	w.t.Helper()
	p := filepath.Join(w.home, ".codex", "sessions", "2026", "11", "19", "rollout-2026-11-19T10-00-00-"+id+".jsonl")
	meta := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"originator":%q,"source":%s,"thread_source":"user"}}`, id, originator, source)
	ctx := `{"type":"turn_context","payload":{"model":"gpt-6.2-sol","approval_policy":"never","sandbox_policy":{"type":"danger-full-access"}}}`
	msg := fmt.Sprintf(`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":%q}]}}`, first)
	writeFile(w.t, p, meta+"\n"+ctx+"\n"+msg+"\n", time.Time{})
	return p
}

// Codex's hooks have its shared daemon for parent. Its rollout's first line
// says who started the session.
func TestHookCodexThroughItsDaemon(t *testing.T) {
	w := newWorld(t)
	daemon := w.standIn("codex", "app-server", "--listen", "unix://", "--analytics-default-enabled", "--managed-daemon")
	desktop := w.standIn("codex", "-c", "features.x=true", "app-server", "--analytics-default-enabled")
	tui := w.codexRollout(idA, "codex-tui", `"vscode"`, "review R1")
	app := w.codexRollout(idB, "Codex Desktop", `"vscode"`, "x")
	sub := w.codexRollout(idC, "Codex Desktop", `{"subagent":{"other":"guardian"}}`, "x")
	in := func(id, rollout string) string {
		return fmt.Sprintf(`{"session_id":%q,"transcript_path":%q,"cwd":%q,"hook_event_name":"SessionStart","source":"startup"}`, id, rollout, w.root)
	}
	// The hook runs in the run's folder; its input's cwd says so too.
	w.parentHook("codex", w.home, in(idA, tui), daemon)
	w.parentHook("codex", w.root, in(idB, app), daemon)
	w.parentHook("codex", w.root, in(idC, sub), daemon)
	// No rollout to read: the desktop app's own app-server isn't the TUI's.
	w.parentHook("codex", w.root, in(idD, "/nowhere/rollout-x-"+idD+".jsonl"), desktop)
	l := w.list()
	if len(l.Entries) != 1 || l.Entries[0].ID != idA || l.Entries[0].Folder != w.root {
		t.Fatalf("entries %+v, want only %s in %s", l.Entries, idA, w.root)
	}
	// The daemon runs on: its report says nothing of whether A is open.
	got := w.printLines(t)
	if len(got) != 1 || got[0] != cd(w.root)+"codex resume "+idA {
		t.Fatalf("--print %q", got)
	}
	if !strings.Contains(w.stderr.String(), "⚠ codex aaaaaaaa comes back skipping permission checks") {
		t.Fatalf("no warning for never and danger-full-access: %q", w.stderr.String())
	}
	// While the daemon holds A's thread lock, A may be open: it isn't
	// offered, and the word says why and when it will be.
	w.held = []string{filepath.Join(w.home, ".codex", "thread-writer-locks", idA+".lock"), "/dev/null"}
	if got := w.printLines(t); len(got) != 0 {
		t.Fatalf("a session the daemon holds is ticked: %q", got)
	}
	if !strings.Contains(w.stderr.String(), "left out codex aaaaaaaa: Codex still has it loaded, so it's open somewhere or closed minutes ago; reopen offers it once Codex lets it go") {
		t.Fatalf("no word on the held session: %q", w.stderr.String())
	}
	// Ticking all and opening leaves it alone: it isn't offered at all.
	w.opened = nil
	w.run(w.root, "a\n\n")
	if len(w.opened) != 0 || !strings.Contains(w.stdout.String(), "left out codex aaaaaaaa") {
		t.Fatalf("opened %q\n%s", w.opened, w.stdout.String())
	}
	w.run(w.root, "", "list")
	if !strings.Contains(w.stdout.String(), "loaded      codex") || !strings.Contains(w.stdout.String(), "gpt-6.2-sol, approvals never, danger-full-access") ||
		!strings.Contains(w.stdout.String(), "Codex still has it loaded") {
		t.Fatalf("list:\n%s", w.stdout.String())
	}
}

// What is typed is data: every word is quoted, and a folder with a control
// character, which would end the line early, isn't typed at all.
func TestHostileFolders(t *testing.T) {
	w := newWorld(t)
	ids := []string{idA, idB, idC, idD}
	names := []string{"a b", "x'; touch pwned; '", "=sub", "nl\ntouch pwned"}
	for i, n := range names {
		dir := filepath.Join(w.root, n)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		w.add(store.Entry{CLI: "claude", ID: ids[i], Folder: dir, Args: []string{"claude"}})
		w.transcript(ids[i], "x", time.Time{})
	}
	got := w.printLines(t)
	if len(got) != 3 {
		t.Fatalf("--print %q, want 3 lines", got)
	}
	if !strings.Contains(w.stderr.String(), "left out claude dddddddd: its folder or a flag holds a control character") {
		t.Fatalf("stderr %q", w.stderr.String())
	}
	// Each line, run by sh, cds into the folder and runs claude with the id,
	// and nothing else.
	bin := filepath.Join(real(t, t.TempDir()), "bin")
	os.Mkdir(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\npwd > \"$OUT\"\n"), 0o755)
	// sh, and zsh, which a tab runs, with = expansion on as it is by default.
	for _, sh := range []string{"/bin/sh", "/bin/zsh"} {
		for i, line := range got {
			out := filepath.Join(bin, fmt.Sprint(i))
			args := []string{"-c", line}
			if sh == "/bin/zsh" {
				args = []string{"-f", "-c", line}
			}
			cmd := exec.Command(sh, args...)
			cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "OUT=" + out}
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s %q: %v %s", sh, line, err, b)
			}
			pwd, _ := os.ReadFile(out)
			j := slices.IndexFunc(ids, func(id string) bool { return strings.Contains(line, id) })
			if j < 0 || strings.TrimSpace(string(pwd)) != filepath.Join(w.root, names[j]) {
				t.Fatalf("%s: %q ran in %q", sh, line, pwd)
			}
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(w.root, "*", "pwned")); len(matches) > 0 {
		t.Fatalf("a folder's name ran: %v", matches)
	}
	if _, err := os.Stat(filepath.Join(w.root, "pwned")); err == nil {
		t.Fatal("a folder's name ran")
	}
}

func TestForgetRefusedInAnAgentsSession(t *testing.T) {
	w := newWorld(t)
	w.add(store.Entry{CLI: "claude", ID: idA, Args: []string{"claude"}})
	w.vars["CLAUDE_CODE_SESSION_ID"] = "x"
	if code := w.run(w.root, "", "forget"); code != 1 || !strings.Contains(w.stderr.String(), "forget is refused in an agent's session") {
		t.Fatalf("exit %d %q", code, w.stderr.String())
	}
	if len(w.list().Entries) != 1 {
		t.Fatal("forget dropped an entry from an agent's session")
	}
}

func TestListPointsAtTheHookLog(t *testing.T) {
	w := newWorld(t)
	w.add(store.Entry{CLI: "claude", ID: idA, Args: []string{"claude"}})
	w.vars["CLAUDE_PROJECT_DIR"] = w.root
	w.vars["CLAUDE_PID"] = "1" // launchd: not claude
	w.run(w.root, `{"session_id":"`+idB+`","source":"startup"}`, "hook", "claude")
	w.vars = map[string]string{}
	w.run(w.root, "", "list")
	if out := w.stdout.String(); !strings.Contains(out, "the hook couldn't record a session just now") || !strings.Contains(out, "CLAUDE_PID names no claude process") {
		t.Fatalf("list:\n%s", out)
	}
	if st, _ := os.Stat(store.LogPath(w.home)); st.Mode().Perm() != 0o600 {
		t.Fatalf("hook.log mode %v", st.Mode().Perm())
	}
}

func TestOpeningFailsWithTheCommandsShown(t *testing.T) {
	w := newWorld(t)
	w.add(store.Entry{CLI: "claude", ID: idA, Args: []string{"claude"}})
	w.transcript(idA, "x", time.Time{})
	w.openErr = errors.New("macOS doesn't let this terminal control iTerm2")
	if code := w.run(w.root, "\n"); code != 1 || !strings.Contains(w.stdout.String(), "claude --resume "+idA) || !strings.Contains(w.stderr.String(), "Nothing opened") {
		t.Fatalf("exit %d\n%s\n%s", code, w.stdout.String(), w.stderr.String())
	}
	w.openErr, w.noIterm, w.opened = nil, true, nil
	if code := w.run(w.root, "\n"); code != 1 || !strings.Contains(w.stdout.String(), "claude --resume "+idA) || len(w.opened) != 0 {
		t.Fatalf("no iTerm2: exit %d\n%s", code, w.stdout.String())
	}
	w.noIterm = false
	w.vars["TERM_PROGRAM"] = "Apple_Terminal"
	w.run(w.root, "\n")
	if !strings.Contains(w.stdout.String(), "macOS may ask whether Apple_Terminal may control iTerm2") {
		t.Fatalf("no word on macOS's question:\n%s", w.stdout.String())
	}
	w.vars["TERM_PROGRAM"] = "iTerm.app"
	w.run(w.root, "\n")
	if strings.Contains(w.stdout.String(), "macOS may ask") {
		t.Fatal("a word on macOS's question from inside iTerm2, which drives itself")
	}
}

func TestEmptyStates(t *testing.T) {
	w := newWorld(t)
	w.run(w.root, "")
	if !strings.Contains(w.stdout.String(), "no session of proj is on the list yet") || !strings.Contains(w.stdout.String(), "reopen init adds") {
		t.Fatalf("%q", w.stdout.String())
	}
	w.claudeHook(idA, "startup", w.root, w.standIn("claude"))
	w.transcript(idA, "x", time.Time{})
	w.run(w.root, "")
	if !strings.Contains(w.stdout.String(), "every session of proj is open (1)") {
		t.Fatalf("%q", w.stdout.String())
	}
}

// Sessions opened together start together: their hooks hit one list at once.
func TestConcurrentHooks(t *testing.T) {
	w := newWorld(t)
	const n = 12
	pids := make([]int, n)
	for i := range pids {
		pids[i] = w.standIn("claude")
	}
	// Each hook's app is made first: the test's helper shares its buffers.
	apps := make([]*app, n)
	for i := range n {
		id := fmt.Sprintf("%08d-0000-4000-8000-000000000000", i)
		vars := map[string]string{"CLAUDE_PROJECT_DIR": w.root, "CLAUDE_PID": fmt.Sprint(pids[i])}
		apps[i] = w.app(w.root, `{"session_id":"`+id+`","source":"startup"}`)
		apps[i].getenv = func(k string) string { return vars[k] }
		apps[i].stdout, apps[i].stderr = &strings.Builder{}, &strings.Builder{}
	}
	var wg sync.WaitGroup
	for _, a := range apps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.run([]string{"hook", "claude"})
		}()
	}
	wg.Wait()
	if got := len(w.list().Entries); got != n {
		t.Fatalf("%d entries, want %d", got, n)
	}
}

func TestPickerKeys(t *testing.T) {
	c, _ := agent.Lookup(agent.Claude)
	rows := []row{{cli: c}, {cli: c, movedTo: "x"}, {cli: c}}
	p := newPicker(rows)
	if p.ticked() != 2 || p.tick[1] {
		t.Fatalf("ticks %v: a session whose role moved starts unticked", p.tick)
	}
	steps := []struct {
		key  string
		cur  int
		tick []bool
		act  action
	}{
		{"down", 1, []bool{true, false, true}, stay},
		{" ", 1, []bool{true, true, true}, stay},
		{"j", 2, []bool{true, true, true}, stay},
		{"j", 0, []bool{true, true, true}, stay}, // wraps
		{"up", 2, []bool{true, true, true}, stay},
		{"n", 2, []bool{false, false, false}, stay},
		{"2", 1, []bool{false, true, false}, stay},
		{"a", 1, []bool{true, true, true}, stay},
		{"x", 1, []bool{true, false, true}, stay},
		{"9", 1, []bool{true, false, true}, stay}, // no such row
		{"\r", 1, []bool{true, false, true}, confirm},
	}
	for i, st := range steps {
		if act := p.key(st.key); act != st.act || p.cur != st.cur || fmt.Sprint(p.tick) != fmt.Sprint(st.tick) {
			t.Fatalf("step %d (%q): cur %d tick %v act %v", i, st.key, p.cur, p.tick, act)
		}
	}
	for _, k := range []string{"q", "esc", "\x03", "\x04"} {
		if newPicker(rows).key(k) != cancel {
			t.Fatalf("%q doesn't leave", k)
		}
	}
}

func TestRenderFitsTheTerminal(t *testing.T) {
	w := newWorld(t)
	c, _ := agent.Lookup(agent.Claude)
	var rows []row
	for i := range 20 {
		rows = append(rows, row{cli: c, label: strings.Repeat("long label 長い ", 10), Entry: store.Entry{ID: fmt.Sprintf("id-%02d", i), Folder: w.root, LastSeen: time.Now(), Role: "reviewer-2"}, transcript: "x", size: 5 << 20,
			flags: []agent.Flag{{Words: []string{"--dangerously-skip-permissions"}, Skips: true}}})
	}
	a := w.app(w.root, "")
	p := newPicker(rows)
	p.cur = 15
	for _, cols := range []int{40, 60} {
		for _, cur := range []int{0, 10, 19} {
			p.cur = cur
			lines := a.render(p, "reopen · proj · 20 sessions to bring back", style{on: true}, cols, 24, false)
			// At most a line short of the terminal: the cursor ends a line
			// below the frame, and nothing may scroll.
			if len(lines) > 23 {
				t.Fatalf("%d lines for a terminal of 24", len(lines))
			}
			for _, l := range lines {
				if width(stripANSI(l)) > cols-1 {
					t.Fatalf("%d columns: a line of %d: %q", cols, width(stripANSI(l)), stripANSI(l))
				}
			}
		}
	}
	p.cur = 15
	lines := a.render(p, "reopen · proj · 20 sessions to bring back", style{on: true}, 60, 20, false)
	if len(lines) > 19 {
		t.Fatalf("%d lines for a terminal of 20", len(lines))
	}
	plain := strings.Join(lines, "\n")
	for _, l := range lines {
		if width(stripANSI(l)) > 59 {
			t.Fatalf("a line wider than 59 columns: %q", stripANSI(l))
		}
	}
	a.root = filepath.Dir(w.root)
	lines = a.render(p, "reopen · proj · 20 sessions to bring back", style{on: true}, 60, 20, false)
	plain = strings.Join(lines, "\n")
	for _, want := range []string{"› ● claude", "↑ ", "↓ ", " more", "⚠ skips permission checks", "enter"} {
		if !strings.Contains(stripANSI(plain), want) {
			t.Fatalf("render lacks %q:\n%s", want, stripANSI(plain))
		}
	}
	// Without colour, no escape at all.
	for _, l := range a.render(p, "h", style{}, 0, 0, false) {
		if strings.Contains(l, "\x1b") {
			t.Fatalf("an escape without colour: %q", l)
		}
	}
}

func stripANSI(s string) string { return agent.Clean(s) }

func TestAgentsEnv(t *testing.T) {
	env := agentsEnv([]string{"HOME=/h", "PATH=/p", "CLAUDE_CODE_SESSION_ID=x", "CLAUDE_PID=1", "CLAUDE_PROJECT_DIR=/d",
		"CODEX_THREAD_ID=y", "ANTIGRAVITY_CONVERSATION_ID=z", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=0"})
	want := []string{"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "HOME=/h", "PATH=/p"}
	if strings.Join(env, " ") != strings.Join(want, " ") {
		t.Fatalf("agentsEnv = %q", env)
	}
}

func TestKeysSplitAndCoalesced(t *testing.T) {
	for _, c := range []struct {
		in   string
		keys []string
		rest string
	}{
		{"qn", []string{"q", "n"}, ""},
		{"jj ", []string{"j", "j", " "}, ""},
		{"\x1b[A\x1b[B", []string{"up", "down"}, ""},
		{"\x1bOB", []string{"down"}, ""},
		{"\x1b", nil, "\x1b"},              // Esc, or an arrow's start: wait
		{"j\x1b[", []string{"j"}, "\x1b["}, // an arrow split between reads
		{"\x1bq", []string{"esc", "q"}, ""},
		{"\xc3", nil, "\xc3"}, // half of é
		{"\x1b[1;5C", []string{"right"}, ""},
		{"\x1b[200~a\r\x1b[201~", []string{"paste-start", "a", "\r", "paste-end"}, ""},
	} {
		got, rest := keys(c.in)
		if strings.Join(got, ",") != strings.Join(c.keys, ",") || rest != c.rest {
			t.Errorf("keys(%q) = %q, %q; want %q, %q", c.in, got, rest, c.keys, c.rest)
		}
	}
	// The split arrow, completed by the next read.
	_, rest := keys("\x1b")
	if got, _ := keys(rest + "[B"); len(got) != 1 || got[0] != "down" {
		t.Fatalf("a split arrow gives %q", got)
	}
}

// A 40-column terminal: no line wraps, the warning shows, and the details
// give the full command.
func TestRenderNarrow(t *testing.T) {
	w := newWorld(t)
	long := filepath.Join(w.root, "a-rather-long-folder-name", "and-another-one-below-it")
	os.MkdirAll(long, 0o755)
	w.role(idD, "reviewer", 1)
	w.add(store.Entry{CLI: "codex", ID: idA, Folder: long, Args: []string{"codex"}, Role: "reviewer"})
	w.codexRollout(idA, "codex-tui", `"vscode"`, "you're the reviewer: review R1 hard, especially open-or-not")
	w.add(store.Entry{CLI: "claude", ID: idB, Args: []string{"claude", "--dangerously-skip-permissions"}})
	w.transcript(idB, "build it", time.Time{})
	a := w.app(w.root, "")
	rows, _, err := a.view(w.repo, false)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows %d %v", len(rows), err)
	}
	p := newPicker(rows)
	for _, detail := range []bool{false, true} {
		for cur := range rows {
			p.cur, p.detail = cur, detail
			lines := a.render(p, "reopen · proj · 2 sessions to bring back", style{on: true}, 40, 30, true)
			plain := stripANSI(strings.Join(lines, "\n"))
			for _, l := range lines {
				if width(stripANSI(l)) > 39 {
					t.Fatalf("detail %v: a line of %d columns: %q", detail, width(stripANSI(l)), stripANSI(l))
				}
			}
			if !strings.Contains(plain, "⚠ skips permission checks") {
				t.Fatalf("no warning at 40 columns:\n%s", plain)
			}
			if strings.Contains(plain, "first message") {
				t.Fatal("--no-message, but the picker shows a first message")
			}
		}
	}
	p.cur = slices.IndexFunc(rows, func(r row) bool { return r.cli.Name == agent.Codex })
	p.detail = true
	plain := plainLines(a.render(p, "h", style{}, 40, 40, false))
	for _, want := range []string{"types    cd --", "approvals", "danger-full-access"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("the details lack %q:\n%s", want, plain)
		}
	}
	if got, want := field(plain, "types"), a.command(rows[p.cur], false); got != want {
		t.Fatalf("the details type %q, the tab gets %q", got, want)
	}
}

// agy --continue names no id, and a resumed conversation runs no hook: the
// presence lock it holds tells that it runs.
func TestAgyHoldsItsPresenceLock(t *testing.T) {
	w := newWorld(t)
	w.add(store.Entry{CLI: "antigravity", ID: idA, Args: []string{"agy"}})
	w.conversation(idA, time.Time{})
	w.add(store.Entry{CLI: "antigravity", ID: idB, Args: []string{"agy"}})
	w.conversation(idB, time.Time{})
	w.standIn("agy", "--continue")
	w.held = []string{
		filepath.Join(w.home, ".gemini", "antigravity-cli", "presence", idA+".lock"),
		filepath.Join(w.home, ".gemini", "antigravity-cli", "conversations", idA+".db"),
	}
	got := w.printLines(t)
	if len(got) != 1 || !strings.Contains(got[0], idB) {
		t.Fatalf("--print %q, want only %s", got, idB)
	}
}

// Only an id a process resumes counts, in either form; one a prompt
// mentions doesn't.
func TestOpenByWhatAProcessResumes(t *testing.T) {
	w := newWorld(t)
	for _, id := range []string{idA, idB, idC} {
		w.add(store.Entry{CLI: "claude", ID: id, Args: []string{"claude"}})
		w.transcript(id, "x", time.Time{})
	}
	w.add(store.Entry{CLI: "antigravity", ID: idD, Args: []string{"agy"}})
	w.conversation(idD, time.Time{})
	w.standIn("claude", "-p", "inspect "+idA)
	w.standIn("claude", "--resume="+idB)
	w.standIn("claude", "fix "+idC)
	w.standIn("agy", "--conversation="+idD, "--model", "m")
	got := strings.Join(w.printLines(t), "\n")
	if !strings.Contains(got, idA) || strings.Contains(got, idB) || !strings.Contains(got, idC) || strings.Contains(got, idD) {
		t.Fatalf("--print:\n%s\nwant %s and %s only", got, idA, idC)
	}
}

// A session resumed with its own flags after a /background keeps them.
func TestContinuedInKeepsANewerRunsFlags(t *testing.T) {
	w := newWorld(t)
	seen := time.Now().Add(-2 * time.Hour)
	w.add(store.Entry{CLI: "claude", ID: idA, Args: []string{"claude", "--allow-dangerously-skip-permissions"}, LastSeen: seen})
	cont := `{"type":"continued-in","continuedInSessionId":"` + idB + `","timestamp":"` + time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano) + `"}`
	w.transcript(idA, "a", time.Time{}, cont)
	w.add(store.Entry{CLI: "claude", ID: idB, Args: []string{"claude", "--resume", idB, "--dangerously-skip-permissions"}, LastSeen: seen.Add(90 * time.Minute)})
	w.transcript(idB, "a", time.Time{})
	got := w.printLines(t)
	if len(got) != 1 || !strings.HasSuffix(got[0], "claude --resume "+idB+" --dangerously-skip-permissions") {
		t.Fatalf("--print %q", got)
	}
	// Resumed by hand with no flag: no flag comes back, though the first
	// run skipped permission checks.
	store.Update(w.home, w.repo, func(l *store.List) (bool, error) {
		l.Entries[l.Find("claude", idB)].Args = []string{"claude", "--resume", idB}
		return true, nil
	})
	if got := w.printLines(t); len(got) != 1 || !strings.HasSuffix(got[0], "claude --resume "+idB) {
		t.Fatalf("--print %q", got)
	}
}

// In an agent's session, list and --print show what reopen would do, and
// change nothing.
func TestAnAgentsSessionChangesNothing(t *testing.T) {
	w := newWorld(t)
	old := time.Now().Add(-40 * 24 * time.Hour)
	w.add(store.Entry{CLI: "codex", ID: idA, Args: []string{"codex"}, LastSeen: old})
	w.rollout(idA, "old", old)
	before, _ := os.ReadFile(store.Path(w.home, w.repo))
	w.vars["CODEX_THREAD_ID"] = "x"
	for _, args := range [][]string{{"list"}, {"--print"}} {
		if code := w.run(w.root, "", args...); code != 0 {
			t.Fatalf("%v: exit %d", args, code)
		}
		if out := w.stdout.String() + w.stderr.String(); !strings.Contains(out, "which reopen drops when it next runs") {
			t.Fatalf("%v: %q", args, out)
		}
	}
	after, _ := os.ReadFile(store.Path(w.home, w.repo))
	if string(before) != string(after) {
		t.Fatal("an agent's session changed the list")
	}
	w.vars = map[string]string{}
	w.run(w.root, "", "--print") // the operator's --print changes nothing either
	if len(w.list().Entries) != 1 {
		t.Fatal("--print dropped an entry")
	}
	w.run(w.root, "q\n")
	if !strings.Contains(w.stdout.String(), "dropped 1 session unused for 30 days") || len(w.list().Entries) != 0 {
		t.Fatalf("the operator's reopen didn't drop it: %q", w.stdout.String())
	}
}

// The row in full, on a short and narrow terminal: it scrolls, the warning
// stays in view, and every line fits.
func TestDetailScreenScrolls(t *testing.T) {
	w := newWorld(t)
	extra := filepath.Join(w.root, "extra")
	os.Mkdir(extra, 0o755)
	p := w.codexRollout(idA, "codex-tui", `"vscode"`, "review it")
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":"gpt-6.2-sol","approval_policy":"never","permission_profile":{"type":"disabled"},"cwd":"` + w.root + `","runtime_workspace_roots":["` + w.root + `","` + extra + `"]}}}` + "\n")
	f.Close()
	w.add(store.Entry{CLI: "codex", ID: idA, Args: []string{"codex"}})
	a := w.app(w.root, "")
	rows, _, _ := a.view(w.repo, false)
	pk := newPicker(rows)
	pk.key("i")
	body := a.detailLines(rows[0], style{}, 4, 40, false)
	seen := ""
	for range 30 {
		lines := a.render(pk, "h", style{on: true}, 40, 12, false)
		if len(lines) > 12 {
			t.Fatalf("%d lines for a terminal of 12", len(lines))
		}
		plain := stripANSI(strings.Join(lines, "\n"))
		for _, l := range lines {
			if width(stripANSI(l)) > 39 {
				t.Fatalf("a line of %d columns: %q", width(stripANSI(l)), stripANSI(l))
			}
		}
		if !strings.Contains(plain, warning) {
			t.Fatalf("the warning scrolled away:\n%s", plain)
		}
		seen += plain + "\n"
		pk.key("down")
	}
	// Every line of the row in full was in view at some point: the added
	// folder and the command among them.
	for _, want := range body {
		if !strings.Contains(seen, stripANSI(want)) {
			t.Fatalf("scrolling never showed %q:\n%s", stripANSI(want), seen)
		}
	}
	if b := stripANSI(strings.Join(body, "\n")); !strings.Contains(b, "+") || !strings.Contains(b, "types") || !strings.Contains(b, "codex") {
		t.Fatalf("the row in full lacks its added folder or command:\n%s", b)
	}
	// Back to the list, not out of the picker.
	if pk.key("esc") != stay || pk.detail {
		t.Fatal("Esc on the details left the picker")
	}
}

func TestWrap(t *testing.T) {
	for _, n := range []int{5, 10, 26} {
		long := strings.Repeat("abcdefghij", 7) + "-end"
		got := wrap("with "+long+" and 長い言葉", n)
		joined := ""
		for _, l := range got {
			if width(l) > n {
				t.Fatalf("n=%d: a line of %d columns: %q", n, width(l), l)
			}
			joined += strings.ReplaceAll(l, " ", "")
		}
		if want := "with" + long + "and長い言葉"; joined != want {
			t.Fatalf("n=%d: wrap lost text: %q", n, joined)
		}
	}
}

// With HOME unset or relative, reopen writes nowhere: not the list, not the
// hook's log, and nothing in the project.
func TestNoHomeFolder(t *testing.T) {
	w := newWorld(t)
	pid := w.standIn("claude")
	for _, home := range []string{"", "rel"} {
		w.vars = map[string]string{"CLAUDE_PROJECT_DIR": w.root, "CLAUDE_PID": fmt.Sprint(pid)}
		a := w.app(w.root, `{"session_id":"`+idA+`","source":"startup"}`)
		a.home = home
		wd, _ := os.Getwd()
		os.Chdir(w.root)
		code := a.run([]string{"hook", "claude"})
		a = w.app(w.root, "")
		a.home = home
		listCode := a.run([]string{"list"})
		os.Chdir(wd)
		if code != 0 || w.stdout.Len() != 0 {
			t.Fatalf("home %q: hook exit %d", home, code)
		}
		if listCode != 1 || !strings.Contains(w.stderr.String(), "no home folder") {
			t.Fatalf("home %q: list exit %d %q", home, listCode, w.stderr.String())
		}
	}
	for _, p := range []string{".reopen", "rel"} {
		if _, err := os.Stat(filepath.Join(w.root, p)); err == nil {
			t.Fatalf("%s was written in the project", p)
		}
	}
}

// What a folder holds reaches the terminal cleaned, in list and the picker.
func TestControlBytesAreShownClean(t *testing.T) {
	w := newWorld(t)
	for i, name := range []string{"esc\x1b[31mred", "two\nlines"} {
		dir := filepath.Join(w.root, name)
		os.Mkdir(dir, 0o755)
		id := []string{idA, idB}[i]
		w.add(store.Entry{CLI: "claude", ID: id, Folder: dir, Args: []string{"claude", "--add-dir", dir}})
		w.transcript(id, "x", time.Time{})
	}
	w.run(w.root, "", "list")
	out := w.stdout.String()
	if strings.ContainsAny(out, "\x1b") {
		t.Fatalf("an escape reached the terminal:\n%q", out)
	}
	if !strings.Contains(out, "proj/escred") || !strings.Contains(out, "proj/two lines") {
		t.Fatalf("list:\n%s", out)
	}
	w.run(w.root, "q\n")
	if strings.ContainsAny(w.stdout.String(), "\x1b") {
		t.Fatalf("an escape reached the picker:\n%q", w.stdout.String())
	}
}

func TestForgetStartsOverAnUnreadableList(t *testing.T) {
	w := newWorld(t)
	w.add(store.Entry{CLI: "claude", ID: idA, Args: []string{"claude"}})
	os.WriteFile(store.Path(w.home, w.repo), []byte(`{"v":1,"repo":`), 0o600)
	if code := w.run(w.root, "", "list"); code != 1 || !strings.Contains(w.stderr.String(), "reopen forget starts it over") {
		t.Fatalf("list: exit %d %q", code, w.stderr.String())
	}
	if code := w.run(w.root, "", "forget"); code != 0 || !strings.Contains(w.stdout.String(), "couldn't be read, so the list of proj starts over, empty") || strings.Contains(w.stdout.String(), "forget starts it over") {
		t.Fatalf("forget: exit %d %q %q", code, w.stdout.String(), w.stderr.String())
	}
	if code := w.run(w.root, "", "list"); code != 0 {
		t.Fatalf("list after: exit %d %q", code, w.stderr.String())
	}
}

// /fork starts a new session in another process and writes no
// continued-in line: both stay, each open by its own process, and neither
// is offered.
func TestForkKeepsBoth(t *testing.T) {
	w := newWorld(t)
	p1 := w.standIn("claude", "--dangerously-skip-permissions")
	w.claudeHook(idA, "startup", w.root, p1)
	w.transcript(idA, "a", time.Time{})
	p2 := w.standIn("claude", "bg-spare", "--bg-spare", "/tmp/x.sock")
	w.claudeHook(idB, "fork", w.root, p2)
	w.transcript(idB, "a", time.Time{})
	if got := w.printLines(t); len(got) != 0 {
		t.Fatalf("an open session is offered: %q", got)
	}
	w.run(w.root, "q\n")
	if l := w.list(); len(l.Entries) != 2 {
		t.Fatalf("entries %+v", l.Entries)
	}
	// Once both end, both come back: the fork with no flag (its worker's
	// arguments carry none), which errs the safe way.
	stop(p1)
	stop(p2)
	got := w.printLines(t)
	slices.Sort(got)
	want := []string{cd(w.root) + "claude --resume " + idA + " --dangerously-skip-permissions", cd(w.root) + "claude --resume " + idB}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("--print %q, want %q", got, want)
	}
}

// A command that can't be typed as it is, isn't printed cleaned either:
// cleaned, a folder named "a\nb" would read "a b", which is another folder.
func TestForgetPrintsNoCleanedCommand(t *testing.T) {
	w := newWorld(t)
	bad, twin := filepath.Join(w.root, "a\nb"), filepath.Join(w.root, "a b")
	os.Mkdir(bad, 0o755)
	os.Mkdir(twin, 0o755)
	w.add(store.Entry{CLI: "claude", ID: idA, Folder: bad, Args: []string{"claude"}})
	tab := filepath.Join(w.root, "x\ty") // an added folder that exists, so its flag comes back
	os.Mkdir(tab, 0o755)
	w.add(store.Entry{CLI: "claude", ID: idB, Folder: twin, Args: []string{"claude", "--add-dir", tab}})
	w.add(store.Entry{CLI: "claude", ID: idC, Folder: twin, Args: []string{"claude"}})
	w.transcript(idA, "a", time.Time{})
	// The row in full says nothing is typed for it.
	a := w.app(w.root, "")
	rows, _, _ := a.view(w.repo, false)
	for _, r := range rows {
		d := stripANSI(strings.Join(a.detailLines(r, style{}, 4, 0, false), "\n"))
		if r.ID == idA && (strings.Contains(d, "cd --") || !strings.Contains(d, "types    nothing")) {
			t.Fatalf("the row in full of an unsafe row:\n%s", d)
		}
	}
	if code := w.run(w.root, "", "forget"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	out := w.stdout.String()
	if strings.Contains(out, idA) || strings.Contains(out, "'"+twin+"' && claude --resume "+idA) {
		t.Fatalf("a cleaned command was printed:\n%s", out)
	}
	if !strings.Contains(out, "(claude aaaaaaaa: no command") || !strings.Contains(out, "(claude bbbbbbbb: no command") {
		t.Fatalf("no word on the commands left out:\n%s", out)
	}
	if !strings.Contains(out, "cd -- '"+twin+"' && claude --resume "+idC) {
		t.Fatalf("the safe command is missing:\n%s", out)
	}
}

func TestOnlyLeavingWorksWhenTooSmall(t *testing.T) {
	for _, k := range []string{"q", "esc", "\x03", "\x04"} {
		if !leaves(k) {
			t.Fatalf("%q doesn't leave", k)
		}
	}
	for _, k := range []string{"\r", " ", "a", "j", "down", "1"} {
		if leaves(k) {
			t.Fatalf("%q leaves", k)
		}
	}
}

// field rebuilds a field of the row in full from its lines: the first after
// its name, the next ones after the indent, each ↵ the end of a line that
// goes on.
func field(plain, name string) string {
	lines := strings.Split(plain, "\n")
	for i, l := range lines {
		_, v, ok := strings.Cut(l, "    "+name+strings.Repeat(" ", 9-len(name)))
		if !ok {
			continue
		}
		out := v
		for j := i + 1; strings.HasSuffix(out, "↵") && j < len(lines); j++ {
			out = strings.TrimSuffix(out, "↵") + strings.TrimPrefix(lines[j], strings.Repeat(" ", 13))
		}
		return out
	}
	return ""
}

// The row in full shows the exact command the tab gets: two spaces stay
// two, wrapped or not, beside a folder whose name has one.
func TestDetailsShowTheExactCommand(t *testing.T) {
	w := newWorld(t)
	two, one := filepath.Join(w.root, "two  lines"), filepath.Join(w.root, "two lines")
	os.Mkdir(two, 0o755)
	os.Mkdir(one, 0o755)
	w.add(store.Entry{CLI: "claude", ID: idA, Folder: two, Args: []string{"claude", "--add-dir", two}})
	w.transcript(idA, "a", time.Time{})
	bidi := filepath.Join(w.root, "abc\u202edcb")
	os.Mkdir(bidi, 0o755)
	w.add(store.Entry{CLI: "claude", ID: idB, Folder: bidi, Args: []string{"claude"}})
	w.transcript(idB, "b", time.Now().Add(-time.Hour))
	a := w.app(w.root, "")
	rows, _, _ := a.view(w.repo, false)
	widths := []int{200}
	for c := minCols; c <= 40; c++ { // every width the picker draws in, up to 40
		widths = append(widths, c)
	}
	for _, r := range rows {
		for _, cols := range widths {
			plain := plainLines(a.detailLines(r, style{on: true}, 4, cols, false))
			for _, l := range strings.Split(plain, "\n") {
				if width(l) > cols-1 {
					t.Fatalf("%d columns: a line of %d: %q", cols, width(l), l)
				}
			}
			switch r.ID {
			case idA:
				if got, want := field(plain, "types"), a.command(r, false); got != want {
					t.Fatalf("%d columns: the details type %q, the tab gets %q", cols, got, want)
				}
				if got := field(plain, "folder"); got != a.tilde(two) {
					t.Fatalf("%d columns: the folder shows as %q", cols, got)
				}
			case idB:
				// A reordering character: no command at all, not a cleaned one.
				if !r.unsafe || !strings.Contains(plain, "types    nothing") {
					t.Fatalf("%d columns: a folder with a bidi character:\n%s", cols, plain)
				}
			}
		}
	}
}

// plainLines removes the colour from each line, then joins them: cleaning
// the joined text would make its newlines spaces.
func plainLines(lines []string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = stripANSI(l)
	}
	return strings.Join(out, "\n")
}

func TestVersion(t *testing.T) {
	w := newWorld(t)
	// A test binary has no module version.
	if code := w.run(w.root, "", "version"); code != 0 || w.stdout.String() != version()+"\n" || version() == "" {
		t.Fatalf("version: exit %d, %q", code, w.stdout.String())
	}
}
