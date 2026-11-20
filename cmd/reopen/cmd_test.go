package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dre4success/reopen/internal/agent"
	"github.com/dre4success/reopen/internal/store"
)

func TestHookRecordsClaudeCode(t *testing.T) {
	w := newWorld(t)
	sub := filepath.Join(w.root, "sub")
	os.Mkdir(sub, 0o755)
	pid := w.standIn("claude", "--model", "opus", "--dangerously-skip-permissions", "fix the bug")
	w.claudeHook(idA, "startup", sub, pid)
	l := w.list()
	if len(l.Entries) != 1 {
		t.Fatalf("entries %+v", l.Entries)
	}
	e := l.Entries[0]
	if e.CLI != "claude" || e.ID != idA || e.Folder != sub || e.PID != pid || !slices.Equal(e.Args, []string{"claude", "--model", "opus", "--dangerously-skip-permissions", "fix the bug"}) {
		t.Fatalf("entry %+v", e)
	}
	if l.Repo != w.repo {
		t.Fatalf("listed under %s, want %s", l.Repo, w.repo)
	}
	st, _ := os.Stat(store.Path(w.home, w.repo))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("list mode %v", st.Mode().Perm())
	}
	// It wrote nothing in the project.
	if _, err := os.Stat(filepath.Join(w.root, ".handoff")); !os.IsNotExist(err) {
		t.Fatal("the hook wrote in the project")
	}
}

func TestHookSkips(t *testing.T) {
	w := newWorld(t)
	outside := real(t, t.TempDir())
	// claude -p, outside a repository, a session from the desktop app.
	w.claudeHook(idA, "startup", w.root, w.standIn("claude", "-p", "hello"))
	w.claudeHook(idB, "startup", outside, w.standIn("claude"))
	w.vars["CLAUDE_CODE_ENTRYPOINT"] = "sdk-cli"
	w.claudeHook(idC, "startup", w.root, w.standIn("claude"))
	w.vars["CLAUDE_CODE_ENTRYPOINT"] = "cli"
	w.claudeHook(idD, "startup", w.root, w.standIn("claude"))
	// codex exec, agy -p.
	w.parentHook("codex", w.root, `{"session_id":"`+idB+`"}`, w.standIn("codex", "exec", "hi"))
	w.parentHook("antigravity", w.home, `{"conversationId":"`+idC+`","cwd":"`+w.root+`"}`, w.standIn("agy", "-p", "hi"))
	// A hook whose parent is a shell, not the CLI: its command lacks exec.
	w.parentHook("codex", w.root, `{"session_id":"`+idA+`"}`, w.standIn("sh", "-c", "reopen hook codex"))
	if w.stderr.Len() != 0 {
		t.Fatalf("the hook wrote on stderr: %q", w.stderr.String())
	}
	if log, _ := os.ReadFile(store.LogPath(w.home)); !strings.Contains(string(log), "starts with exec") {
		t.Fatalf("hook.log %q", log)
	}
	// Input that isn't JSON: exit 0 all the same.
	if code := w.run(w.root, "nonsense", "hook", "claude"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	l := w.list()
	if len(l.Entries) != 1 || l.Entries[0].ID != idD {
		t.Fatalf("entries %+v, want only %s", l.Entries, idD)
	}
}

func TestHookRecordsCodexAndAntigravity(t *testing.T) {
	w := newWorld(t)
	sub := filepath.Join(w.root, "sub")
	os.Mkdir(sub, 0o755)
	cpid := w.standIn("codex", "-m", "gpt-5", "-s", "workspace-write", "write tests")
	w.parentHook("codex", sub, `{"session_id":"`+idA+`","transcript_path":"/x"}`, cpid)
	// Antigravity's hook runs in its hooks.json's folder; stdin's cwd is the agent's.
	apid := w.standIn("agy", "--model", "gemini", "--dangerously-skip-permissions")
	w.parentHook("antigravity", w.home, `{"conversationId":"`+idB+`","cwd":"`+w.root+`","hookEventName":"SessionStart"}`, apid)
	l := w.list()
	if len(l.Entries) != 2 {
		t.Fatalf("entries %+v", l.Entries)
	}
	if e := l.Entries[0]; e.CLI != "codex" || e.Folder != sub || e.PID != cpid {
		t.Fatalf("codex entry %+v", e)
	}
	if e := l.Entries[1]; e.CLI != "antigravity" || e.Folder != w.root || e.PID != apid {
		t.Fatalf("agy entry %+v", e)
	}
}

func TestHookWorktreeSharesTheList(t *testing.T) {
	w := newWorld(t)
	wt := filepath.Join(filepath.Dir(w.root), "proj-wt")
	git(t, w.root, "worktree", "add", "-q", wt)
	w.claudeHook(idA, "startup", w.root, w.standIn("claude"))
	w.claudeHook(idB, "startup", wt, w.standIn("claude"))
	l := w.list()
	if len(l.Entries) != 2 || l.Entries[1].Folder != wt {
		t.Fatalf("entries %+v", l.Entries)
	}
}

func TestHookFollowsTheLatestRun(t *testing.T) {
	w := newWorld(t)
	other := filepath.Join(w.root, "other")
	os.Mkdir(other, 0o755)
	w.claudeHook(idA, "startup", w.root, w.standIn("claude", "--model", "opus"))
	p2 := w.standIn("claude", "--resume", idA)
	w.claudeHook(idA, "resume", other, p2)
	l := w.list()
	if len(l.Entries) != 1 || l.Entries[0].Folder != other || l.Entries[0].PID != p2 {
		t.Fatalf("entries %+v", l.Entries)
	}
}

func TestHookClearReplacesTheEntryFromItsProcess(t *testing.T) {
	w := newWorld(t)
	pid := w.standIn("claude")
	w.claudeHook(idA, "startup", w.root, pid)
	w.claudeHook(idB, "clear", w.root, pid)
	l := w.list()
	if len(l.Entries) != 1 || l.Entries[0].ID != idB {
		t.Fatalf("entries %+v", l.Entries)
	}
	// An entry an earlier process reported under the same process id stays:
	// the id was reused.
	store.Update(w.home, w.repo, func(l *store.List) (bool, error) {
		l.Entries = append(l.Entries, store.Entry{CLI: "claude", ID: idC, Folder: w.root, PID: pid, LastSeen: time.Now().Add(-time.Hour)})
		return true, nil
	})
	w.claudeHook(idD, "clear", w.root, pid)
	var ids []string
	for _, e := range w.list().Entries {
		ids = append(ids, e.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{idC, idD}) {
		t.Fatalf("ids %v, want %s and %s", ids, idC, idD)
	}
}

func TestHookKeepsFlagsFromABackgroundWorker(t *testing.T) {
	w := newWorld(t)
	w.claudeHook(idA, "startup", w.root, w.standIn("claude", "--dangerously-skip-permissions"))
	w.claudeHook(idA, "resume", w.root, w.standIn("claude", "bg-spare", "--bg-spare", "/tmp/x.sock"))
	if e := w.list().Entries[0]; !slices.Equal(e.Args, []string{"claude", "--dangerously-skip-permissions"}) {
		t.Fatalf("args %q", e.Args)
	}
}

func TestHookNotesTheRole(t *testing.T) {
	w := newWorld(t)
	w.role(idA, "implementer", 1)
	w.role(idB, "reviewer", 2) // a format reopen doesn't know
	w.claudeHook(idA, "startup", w.root, w.standIn("claude"))
	w.claudeHook(idB, "startup", w.root, w.standIn("claude"))
	l := w.list()
	if l.Entries[0].Role != "implementer" || l.Entries[1].Role != "" {
		t.Fatalf("roles %q %q", l.Entries[0].Role, l.Entries[1].Role)
	}
}

// add puts an entry on the list as if its hook had run then.
func (w *world) add(e store.Entry) {
	w.t.Helper()
	if e.Folder == "" {
		e.Folder = w.root
	}
	if e.LastSeen.IsZero() {
		e.LastSeen = time.Now().Add(-time.Hour)
	}
	err := store.Update(w.home, w.repo, func(l *store.List) (bool, error) {
		l.Entries = append(l.Entries, e)
		return true, nil
	})
	if err != nil {
		w.t.Fatal(err)
	}
}

func TestPrintResumesClosedSessionsNewestFirst(t *testing.T) {
	w := newWorld(t)
	extra := real(t, t.TempDir())
	now := time.Now()
	w.add(store.Entry{CLI: "claude", ID: idA, Args: []string{"claude", "--model", "opus", "--dangerously-skip-permissions", "--add-dir", extra, "fix the bug"}})
	w.transcript(idA, "fix the bug", now.Add(-3*time.Hour))
	w.add(store.Entry{CLI: "codex", ID: idB, Args: []string{"codex", "-m", "gpt-5", "-s", "read-only", "write tests"}})
	w.rollout(idB, "write tests", now.Add(-time.Hour))
	w.add(store.Entry{CLI: "antigravity", ID: idC, Args: []string{"agy", "--model", "gemini", "--dangerously-skip-permissions"}})
	w.conversation(idC, now.Add(-2*time.Hour))
	got := w.printLines(t)
	want := []string{
		cd(w.root) + "codex resume " + idB, // codex resume keeps its settings itself
		cd(w.root) + "agy --conversation " + idC + " --model gemini --dangerously-skip-permissions",
		cd(w.root) + "claude --resume " + idA + " --dangerously-skip-permissions --add-dir " + extra,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("--print:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if strings.Contains(w.stdout.String(), "fix the bug") || strings.Contains(w.stdout.String(), "write tests") {
		t.Fatal("a first prompt is passed back")
	}
	if len(w.opened) != 0 {
		t.Fatal("--print opened a window")
	}
}

func TestPrintQuotesAFolder(t *testing.T) {
	w := newWorld(t)
	odd := filepath.Join(w.root, "it's a dir")
	os.Mkdir(odd, 0o755)
	w.add(store.Entry{CLI: "claude", ID: idA, Folder: odd, Args: []string{"claude"}})
	w.transcript(idA, "x", time.Time{})
	got := w.printLines(t)
	if len(got) != 1 || got[0] != "cd -- '"+strings.ReplaceAll(odd, "'", `'\''`)+"' && claude --resume "+idA {
		t.Fatalf("--print %q", got)
	}
}

func TestOpenSessionsAreLeftOut(t *testing.T) {
	w := newWorld(t)
	// A: its recorded process runs, started before the entry was last seen.
	pa := w.standIn("claude")
	w.claudeHook(idA, "startup", w.root, pa)
	w.transcript(idA, "a", time.Time{})
	// B: a live process's arguments name it, as reopen typed them, and its
	// hook hasn't run: Codex's runs at the first turn.
	w.add(store.Entry{CLI: "codex", ID: idB, PID: 1, Args: []string{"codex"}})
	w.rollout(idB, "b", time.Time{})
	w.standIn("codex", "resume", idB, agent.Message)
	// C: its process id was reused by a process that started later.
	pc := w.standIn("agy")
	w.add(store.Entry{CLI: "antigravity", ID: idC, PID: pc, Args: []string{"agy"}, LastSeen: time.Now().Add(-time.Hour)})
	w.conversation(idC, time.Time{})
	// D: its process is alive, but isn't that CLI.
	pd := w.standIn("vim")
	w.add(store.Entry{CLI: "claude", ID: idD, PID: pd, Args: []string{"claude"}, LastSeen: time.Now()})
	w.transcript(idD, "d", time.Time{})
	got := strings.Join(w.printLines(t), "\n")
	if strings.Contains(got, idA) || strings.Contains(got, idB) {
		t.Fatalf("an open session is offered:\n%s", got)
	}
	if !strings.Contains(got, idC) || !strings.Contains(got, idD) {
		t.Fatalf("a closed session is left out:\n%s", got)
	}
}

func TestResumeInsideASessionMovesItsProcess(t *testing.T) {
	w := newWorld(t)
	// The process started as claude --resume A; /resume typed in it went to
	// B. Its arguments still name A.
	pid := w.standIn("claude", "--resume", idA)
	w.claudeHook(idA, "resume", w.root, pid)
	time.Sleep(10 * time.Millisecond)
	w.claudeHook(idB, "resume", w.root, pid)
	w.transcript(idA, "a", time.Time{})
	w.transcript(idB, "b", time.Time{})
	got := w.printLines(t)
	if len(got) != 1 || !strings.Contains(got[0], idA) {
		t.Fatalf("--print %q, want only %s", got, idA)
	}
	// /clear in the same process: the cleared entry is dropped.
	w.claudeHook(idC, "clear", w.root, pid)
	w.transcript(idC, "c", time.Time{})
	got = w.printLines(t)
	if len(got) != 1 || !strings.Contains(got[0], idA) {
		t.Fatalf("after /clear, --print %q", got)
	}
}

func TestLeftOutAndDropped(t *testing.T) {
	w := newWorld(t)
	old := time.Now().Add(-31 * 24 * time.Hour)
	// A: Claude Code, no transcript: nobody typed to it.
	w.add(store.Entry{CLI: "claude", ID: idA, Args: []string{"claude"}})
	// B: unused for 31 days.
	w.add(store.Entry{CLI: "codex", ID: idB, Args: []string{"codex"}, LastSeen: old})
	w.rollout(idB, "b", old)
	// C: its folder is gone.
	w.add(store.Entry{CLI: "claude", ID: idC, Folder: filepath.Join(w.root, "gone"), Args: []string{"claude"}})
	w.transcript(idC, "c", time.Time{})
	w.add(store.Entry{CLI: "claude", ID: idD, Args: []string{"claude"}})
	w.transcript(idD, "d", time.Time{})
	got := w.printLines(t)
	if len(got) != 1 || !strings.Contains(got[0], idD) {
		t.Fatalf("--print %q, want only %s", got, idD)
	}
	errs := w.stderr.String()
	if !strings.Contains(errs, "1 session unused for 30 days, which reopen drops when it next runs: codex bbbbbbbb") {
		t.Fatalf("stderr doesn't say what will be dropped: %q", errs)
	}
	if !strings.Contains(errs, "left out claude cccccccc: its folder is gone") {
		t.Fatalf("stderr doesn't say the folder is gone: %q", errs)
	}
	// --print changes nothing; reopen itself drops it, and says so.
	if w.list().Find("codex", idB) == -1 {
		t.Fatal("--print dropped an entry")
	}
	w.run(w.root, "q\n")
	if !strings.Contains(w.stdout.String(), "dropped 1 session unused for 30 days: codex bbbbbbbb") {
		t.Fatalf("reopen doesn't say what it dropped: %q", w.stdout.String())
	}
	if w.list().Find("codex", idB) != -1 {
		t.Fatal("the unused entry is still on the list")
	}
	if w.list().Find("claude", idA) == -1 {
		t.Fatal("the entry with no transcript was dropped; it is only left out")
	}
}

func TestRolesAndTheFirstMessage(t *testing.T) {
	w := newWorld(t)
	w.role(idA, "implementer", 1)
	w.role(idB, "reviewer", 1)
	w.role(idD, "reviewer-2", 1)
	w.add(store.Entry{CLI: "claude", ID: idA, Args: []string{"claude"}, Role: "implementer"})
	w.transcript(idA, "a", time.Time{})
	w.add(store.Entry{CLI: "codex", ID: idB, Args: []string{"codex"}})
	w.rollout(idB, "b", time.Time{})
	// C had reviewer-2, which D holds now: C starts unticked.
	w.add(store.Entry{CLI: "antigravity", ID: idC, Args: []string{"agy"}, Role: "reviewer-2"})
	w.conversation(idC, time.Time{})
	w.add(store.Entry{CLI: "antigravity", ID: idD, Args: []string{"agy"}})
	w.conversation(idD, time.Time{})
	got := w.printLines(t)
	slices.Sort(got)
	want := []string{
		cd(w.root) + "agy --conversation " + idD + " --prompt-interactive '" + agent.Message + "'",
		cd(w.root) + "claude --resume " + idA,
		cd(w.root) + "codex resume " + idB + " '" + agent.Message + "'",
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("--print:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !strings.Contains(w.stderr.String(), "not ticked, agy cccccccc: its role reviewer-2 is now session dddddddd's") {
		t.Fatalf("stderr %q", w.stderr.String())
	}
	// reopen notes the role for later, once handoff drops its file;
	// --print doesn't write.
	if l := w.list(); l.Entries[l.Find("codex", idB)].Role != "" {
		t.Fatal("--print wrote the list")
	}
	w.run(w.root, "q\n")
	if l := w.list(); l.Entries[l.Find("codex", idB)].Role != "reviewer" {
		t.Fatal("the role wasn't noted")
	}
	// The picker shows the first message, and with --no-message doesn't.
	if !strings.Contains(w.stdout.String(), `first message: "`+agent.Message+`"`) {
		t.Fatalf("the picker doesn't show the first message:\n%s", w.stdout.String())
	}
	w.run(w.root, "q\n", "--no-message")
	if strings.Contains(w.stdout.String(), "first message") {
		t.Fatalf("--no-message, but the picker shows a first message:\n%s", w.stdout.String())
	}
	w.run(w.root, "", "--print", "--no-message")
	if strings.Contains(w.stdout.String(), agent.Message) {
		t.Fatal("--no-message sent a message")
	}
}

func TestBackgroundFollowsContinuedIn(t *testing.T) {
	w := newWorld(t)
	extra := real(t, t.TempDir())
	seen := time.Now().Add(-2 * time.Hour)
	// A went on as B by /background: its transcript's continued-in line
	// names B, followed by bookkeeping lines only. B's entry is the
	// worker's, whose arguments carry none of the session's flags.
	w.add(store.Entry{CLI: "claude", ID: idA, Args: []string{"claude", "--dangerously-skip-permissions", "--add-dir", extra}, LastSeen: seen, Role: "implementer"})
	cont := `{"type":"continued-in","continuedInSessionId":"` + idB + `","timestamp":"` + time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano) + `"}`
	w.transcript(idA, "a", time.Time{}, cont, `{"type":"cost-state"}`, `{"type":"last-prompt"}`)
	w.add(store.Entry{CLI: "claude", ID: idB, Args: []string{"claude", "bg-spare", "--bg-spare", "/tmp/x.sock"}, LastSeen: seen.Add(time.Hour)})
	w.transcript(idB, "a", time.Time{})
	// C went on as D, whose hook never ran.
	w.add(store.Entry{CLI: "claude", ID: idC, Args: []string{"claude"}, LastSeen: seen})
	w.transcript(idC, "c", time.Time{}, `{"type":"continued-in","continuedInSessionId":"`+idD+`","timestamp":"`+time.Now().UTC().Format(time.RFC3339Nano)+`"}`)
	w.transcript(idD, "c", time.Time{})
	// D runs in the background; B doesn't any more.
	w.agents = `[{"id":"dddddddd","sessionId":"` + idD + `","kind":"background","state":"idle"},{"pid":1,"sessionId":"x","kind":"interactive"}]`
	got := w.printLines(t)
	if want := cd(w.root) + "claude --resume " + idB + " --dangerously-skip-permissions --add-dir " + extra; len(got) != 1 || got[0] != want {
		t.Fatalf("--print %q\nwant %q", got, want)
	}
	if !strings.Contains(w.stderr.String(), "runs in the background, with no tab: "+cd(w.root)+"claude attach dddddddd") {
		t.Fatalf("stderr %q", w.stderr.String())
	}
	w.run(w.root, "q\n") // reopen itself writes what it followed
	l := w.list()
	if l.Find("claude", idA) != -1 || l.Find("claude", idC) != -1 || l.Entries[l.Find("claude", idB)].Role != "implementer" {
		t.Fatalf("entries %+v", l.Entries)
	}
}

func TestStaleContinuedInIsIgnored(t *testing.T) {
	w := newWorld(t)
	// A was backgrounded, then resumed: its transcript goes on after the line.
	w.add(store.Entry{CLI: "claude", ID: idA, Args: []string{"claude"}})
	cont := `{"type":"continued-in","continuedInSessionId":"` + idB + `","timestamp":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}`
	w.transcript(idA, "a", time.Time{}, cont, `{"type":"user","message":{"role":"user","content":"more"}}`)
	got := w.printLines(t)
	if len(got) != 1 || !strings.Contains(got[0], idA) {
		t.Fatalf("--print %q", got)
	}
}

func TestRefusedInAnAgentsSession(t *testing.T) {
	w := newWorld(t)
	w.add(store.Entry{CLI: "claude", ID: idA, Args: []string{"claude"}})
	w.transcript(idA, "a", time.Time{})
	for _, v := range []string{"CLAUDE_CODE_SESSION_ID", "CODEX_THREAD_ID", "ANTIGRAVITY_CONVERSATION_ID"} {
		w.vars = map[string]string{v: "x"}
		if code := w.run(w.root, "\n"); code != 1 || !strings.Contains(w.stderr.String(), "refused in an agent's session ("+v) {
			t.Fatalf("%s: exit %d, %q", v, code, w.stderr.String())
		}
		// --print and list start nothing, and work there.
		if code := w.run(w.root, "", "--print"); code != 0 || !strings.Contains(w.stdout.String(), idA) {
			t.Fatalf("%s: --print exit %d", v, code)
		}
		if code := w.run(w.root, "", "list"); code != 0 {
			t.Fatalf("%s: list exit %d", v, code)
		}
	}
	w.vars = map[string]string{}
	w.terminal = false
	if code := w.run(w.root, "\n"); code != 1 || !strings.Contains(w.stderr.String(), "stdin isn't a terminal") {
		t.Fatalf("no terminal: exit %d, %q", code, w.stderr.String())
	}
	if len(w.opened) != 0 {
		t.Fatal("a refused reopen opened a window")
	}
}

func TestPickAndOpen(t *testing.T) {
	w := newWorld(t)
	now := time.Now()
	w.role(idD, "reviewer", 1)
	w.add(store.Entry{CLI: "claude", ID: idA, Args: []string{"claude", "--dangerously-skip-permissions"}})
	w.transcript(idA, "build reopen", now.Add(-time.Minute), `{"type":"permission-mode","permissionMode":"bypassPermissions"}`)
	w.add(store.Entry{CLI: "codex", ID: idB, Args: []string{"codex", "-m", "o3"}})
	w.rollout(idB, "write tests", now.Add(-2*time.Minute))
	w.add(store.Entry{CLI: "antigravity", ID: idC, Args: []string{"agy"}, Role: "reviewer"})
	w.conversation(idC, now.Add(-3*time.Minute))

	// Enter opens the ticked ones: C's role is D's now, so C is unticked.
	if code := w.run(w.root, "\n"); code != 0 {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	out := w.stdout.String()
	for _, s := range []string{
		"1 [x] claude", "build reopen", "⚠ skips permission checks  proj · bypassPermissions mode --dangerously-skip-permissions · 183 B · aaaaaaaa",
		"2 [x] codex", "write tests",
		"3 [ ] agy", "(reviewer)", "not ticked: its role reviewer is now session dddddddd's",
		"opened 2 of 3 in a new iTerm2 window", "not opened:", "○ agy     (reviewer)  its role is session dddddddd's now",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("the picker doesn't show %q:\n%s", s, out)
		}
	}
	if len(w.opened) != 1 || len(w.opened[0]) != 2 || !strings.Contains(w.opened[0][0], idA) || !strings.Contains(w.opened[0][1], idB) {
		t.Fatalf("opened %q", w.opened)
	}

	// Numbers tick and untick; a bad number changes nothing.
	w.opened = nil
	w.run(w.root, "1 3\n9\n\n")
	if len(w.opened) != 1 || len(w.opened[0]) != 2 || !strings.Contains(w.opened[0][0], idB) || !strings.Contains(w.opened[0][1], idC) {
		t.Fatalf("opened %q", w.opened)
	}
	if !strings.Contains(w.stdout.String(), `"9" isn't one of 1 to 3`) {
		t.Fatalf("no word for a bad number:\n%s", w.stdout.String())
	}

	// q, end of input, and none ticked open nothing.
	w.opened = nil
	for _, in := range []string{"q\n", "", "n\n\n"} {
		if code := w.run(w.root, in); code != 0 {
			t.Fatalf("%q: exit %d", in, code)
		}
	}
	if len(w.opened) != 0 {
		t.Fatalf("opened %q", w.opened)
	}
}

func TestListAndForget(t *testing.T) {
	w := newWorld(t)
	pid := w.standIn("claude")
	w.claudeHook(idA, "startup", w.root, pid)
	w.transcript(idA, "open one", time.Time{})
	w.add(store.Entry{CLI: "claude", ID: idB, Args: []string{"claude"}})
	w.add(store.Entry{CLI: "codex", ID: idC, Args: []string{"codex"}})
	w.rollout(idC, "closed one", time.Time{})
	if code := w.run(w.root, "", "list"); code != 0 {
		t.Fatalf("list exit %d: %s", code, w.stderr.String())
	}
	out := w.stdout.String()
	for _, s := range []string{"reopen · proj · 3 sessions, newest first", "open        claude", "never used  claude", "closed      codex", "closed one"} {
		if !strings.Contains(out, s) {
			t.Errorf("list doesn't show %q:\n%s", s, out)
		}
	}
	if code := w.run(w.root, "", "forget", "ccc"); code != 1 {
		t.Fatal("forget took a 3-character prefix")
	}
	if code := w.run(w.root, "", "forget", "cccc"); code != 0 || !strings.Contains(w.stdout.String(), "forgot 1 session of proj") || !strings.Contains(w.stdout.String(), "codex resume "+idC) {
		t.Fatalf("forget: %q %q", w.stdout.String(), w.stderr.String())
	}
	if code := w.run(w.root, "", "forget", idC); code != 1 {
		t.Fatal("forget of a session not on the list succeeded")
	}
	if code := w.run(w.root, ""); code != 0 {
		t.Fatal()
	}
	if code := w.run(w.root, "", "forget"); code != 0 || len(w.list().Entries) != 0 {
		t.Fatalf("forget all: %q", w.stderr.String())
	}
	// A transcript is never touched.
	if _, err := os.Stat(filepath.Join(w.home, ".claude", "projects", "-proj", idA+".jsonl")); err != nil {
		t.Fatal("forget removed a transcript")
	}
}

func TestHooksPrintsValidSettings(t *testing.T) {
	w := newWorld(t)
	for _, c := range []struct{ cli, file, want string }{
		{"claude", "~/.claude/settings.json", `"hook",`},
		{"codex", "~/.codex/hooks.json", `hook codex"`},
		{"antigravity", "~/.gemini/config/hooks.json", `hook antigravity"`},
	} {
		if code := w.run(w.root, "", "hooks", c.cli); code != 0 {
			t.Fatalf("hooks %s exit %d", c.cli, code)
		}
		out := w.stdout.String()
		if !strings.Contains(out, "Add this to "+c.file) || !strings.Contains(out, c.want) {
			t.Fatalf("hooks %s:\n%s", c.cli, out)
		}
		var v map[string]any
		if err := json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &v); err != nil {
			t.Fatalf("hooks %s prints JSON that doesn't parse: %v", c.cli, err)
		}
		if c.cli != "claude" && !strings.Contains(out, `"exec `) {
			t.Fatalf("hooks %s: the command doesn't start with exec", c.cli)
		}
	}
	if code := w.run(w.root, "", "hooks", "gemini"); code != 1 {
		t.Fatal("hooks took an unknown CLI")
	}
}

func TestAnEmptyListMakesNoFolder(t *testing.T) {
	w := newWorld(t)
	for _, args := range [][]string{{"--print"}, {"list"}, {"forget"}} {
		if code := w.run(w.root, "", args...); code != 0 {
			t.Fatalf("%v: exit %d %s", args, code, w.stderr.String())
		}
	}
	if _, err := os.Stat(filepath.Join(w.home, ".reopen")); !os.IsNotExist(err) {
		t.Fatal("~/.reopen/ was made for an empty list")
	}
}

func TestOutsideARepository(t *testing.T) {
	w := newWorld(t)
	if code := w.run(real(t, t.TempDir()), "", "--print"); code != 1 || !strings.Contains(w.stderr.String(), "not in a git repository") {
		t.Fatalf("exit %d %q", code, w.stderr.String())
	}
}

// A bare resume doesn't keep bypassPermissions mode unless a flag allows
// it, so a row warns only when one comes back.
func TestBypassModeComesBackOnlyWithItsFlag(t *testing.T) {
	w := newWorld(t)
	mode := `{"type":"permission-mode","permissionMode":"bypassPermissions"}`
	w.add(store.Entry{CLI: "claude", ID: idA, Args: []string{"claude", "--permission-mode", "bypassPermissions"}})
	w.transcript(idA, "a", time.Now().Add(-time.Minute), mode)
	w.add(store.Entry{CLI: "claude", ID: idB, Args: []string{"claude", "--allow-dangerously-skip-permissions"}})
	w.transcript(idB, "b", time.Now().Add(-2*time.Minute), mode)
	w.run(w.root, "", "list")
	out := w.stdout.String()
	lines := strings.Split(out, "\n")
	var rowA, rowB string
	for i, l := range lines {
		if strings.Contains(l, short(idA)) {
			rowA = strings.Join(lines[i-1:min(i+3, len(lines))], "\n")
		}
		if strings.Contains(l, short(idB)) {
			rowB = strings.Join(lines[i-1:min(i+3, len(lines))], "\n")
		}
	}
	if strings.Contains(rowA, "⚠") || !strings.Contains(rowA, "it was in bypassPermissions mode") {
		t.Fatalf("A, started with --permission-mode only:\n%s", rowA)
	}
	if !strings.Contains(rowB, "⚠ skips permission checks") || !strings.Contains(rowB, "bypassPermissions mode") {
		t.Fatalf("B, with the flag that allows it:\n%s", rowB)
	}
}
