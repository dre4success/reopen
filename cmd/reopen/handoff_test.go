package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dre4success/reopen/internal/agent"
	"github.com/dre4success/reopen/internal/proc"
	"github.com/dre4success/reopen/internal/store"
)

// handoff writes handoff's session file for id: its role, the process it
// saw it in (key is "pid" for Claude Code, "host" for the other two), and
// when.
func (w *world) handoff(id, role, key string, pid int, since time.Time) {
	w.t.Helper()
	b, _ := json.Marshal(map[string]any{"v": 1, "role": role, "session": id, key: pid, "since": since.UTC().Format(time.RFC3339Nano)})
	writeFile(w.t, filepath.Join(w.root, ".handoff", "sessions", id+".json"), string(b), time.Time{})
}

// The CLIs' own records of a session that began in folder, as each CLI
// writes them, with no start hook having run.
func (w *world) claudeRecord(id, folder, mode string) {
	w.t.Helper()
	lines := []string{
		fmt.Sprintf(`{"type":"permission-mode","permissionMode":%q,"sessionId":%q}`, mode, id),
		fmt.Sprintf(`{"type":"user","cwd":%q,"sessionId":%q,"message":{"role":"user","content":"build it"}}`, folder, id),
		fmt.Sprintf(`{"type":"assistant","cwd":%q,"message":{"role":"assistant"}}`, filepath.Join(folder, "sub")),
	}
	writeFile(w.t, filepath.Join(w.home, ".claude", "projects", "-x", id+".jsonl"), strings.Join(lines, "\n")+"\n", time.Now().Add(-time.Hour))
}

func (w *world) codexRecord(id, folder string) {
	w.t.Helper()
	p := filepath.Join(w.home, ".codex", "sessions", "2026", "11", "19", "rollout-2026-11-19T10-00-00-"+id+".jsonl")
	lines := []string{
		fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":%q,"originator":"codex-tui"}}`, id, folder),
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"review it"}]}}`,
		fmt.Sprintf(`{"type":"turn_context","payload":{"cwd":%q,"model":"gpt-6","approval_policy":"on-request","sandbox_policy":{"type":"workspace-write"}}}`, folder),
	}
	writeFile(w.t, p, strings.Join(lines, "\n")+"\n", time.Now().Add(-2*time.Hour))
}

func (w *world) agyRecord(id, folder string) {
	w.t.Helper()
	agy := filepath.Join(w.home, ".gemini", "antigravity-cli")
	writeFile(w.t, filepath.Join(agy, "brain", id, ".system_generated", "logs", "transcript.jsonl"),
		`{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","content":"<USER_REQUEST>check it</USER_REQUEST>"}`+"\n", time.Now().Add(-3*time.Hour))
	index := map[string]any{"account": map[string]any{id: map[string]any{"summary": map[string]any{
		"ID": id, "WorkspaceURIs": []string{"file://" + folder}, "PermissionPreset": 0,
	}}}}
	b, _ := json.Marshal(index)
	writeFile(w.t, filepath.Join(agy, "cache", "conversation_metadata.json"), string(b), time.Time{})
}

// A handoff team whose sessions began before reopen's hook was added: none
// is on reopen's list, and each comes back from its CLI's own records.
func TestHandoffSessionsWithoutTheirHook(t *testing.T) {
	w := newWorld(t)
	since := time.Now().Add(-4 * time.Hour)
	w.claudeRecord(idA, w.root, "bypassPermissions")
	w.handoff(idA, "implementer", "pid", 1, since) // launchd: not claude, so not the session's process
	w.codexRecord(idB, w.root)
	w.handoff(idB, "reviewer", "host", 1, since)
	w.agyRecord(idC, w.root)
	w.handoff(idC, "reviewer-3", "host", 1, since)

	got := w.printLines(t)
	want := []string{
		// No flag is made up: bypass mode is reached without the skip flag
		// too, so the row says it won't carry over.
		cd(w.root) + "claude --resume " + idA,
		cd(w.root) + "codex resume " + idB + " 'You were reopened. Read your handoff inbox.'",
		cd(w.root) + "agy --conversation " + idC + " --prompt-interactive 'You were reopened. Read your handoff inbox.'",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if _, err := os.Stat(filepath.Join(w.home, ".reopen")); !os.IsNotExist(err) {
		t.Fatal("--print wrote the list")
	}

	if code := w.run(w.root, "", "list"); code != 0 {
		t.Fatalf("list exit %d", code)
	}
	out := stripANSI(w.stdout.String())
	for _, s := range []string{"3 sessions", "implementer", "reviewer-3", "found through handoff: its start flags weren't recorded, so it comes back with agy's defaults",
		"found through handoff: its start flags weren't recorded, so a flag that skips checks, or a folder it added, doesn't come back",
		"it was in bypassPermissions mode, which a resume doesn't keep without the flag that allows it"} {
		if !strings.Contains(out, s) {
			t.Fatalf("list lacks %q:\n%s", s, out)
		}
	}
	if strings.Contains(out, "⚠") {
		t.Fatalf("a row warns, though nothing comes back skipping checks:\n%s", out)
	}

	// A bare reopen puts them on the list, as found through handoff.
	if _, _, err := w.app(w.root, "").view(w.repo, true); err != nil {
		t.Fatal(err)
	}
	l := w.list()
	if len(l.Entries) != 3 {
		t.Fatalf("%d entries", len(l.Entries))
	}
	for _, e := range l.Entries {
		if e.Found != "handoff" || len(e.Args) != 0 || e.PID != 0 || e.Folder != w.root || e.Role == "" {
			t.Fatalf("entry %+v", e)
		}
	}
	// It's the same from the list as before it.
	if got2 := w.printLines(t); !slices.Equal(got2, want) {
		t.Fatalf("from the list:\n%s", strings.Join(got2, "\n"))
	}

	// Its hook's report, when it comes, replaces the entry.
	w.claudeHook(idA, "resume", w.root, w.standIn("claude", "--resume", idA, "--dangerously-skip-permissions"))
	l = w.list()
	e := l.Entries[l.Find(agent.Claude, idA)]
	if e.Found != "" || len(e.Args) == 0 || e.Role != "implementer" {
		t.Fatalf("after its hook: %+v", e)
	}
}

// A session handoff lists that isn't this project's, or was unused for 30
// days, or that no CLI has, isn't brought back.
func TestHandoffSessionsNotOfThisProject(t *testing.T) {
	w := newWorld(t)
	other := filepath.Join(real(t, t.TempDir()), "other")
	os.Mkdir(other, 0o755)
	git(t, other, "init", "-q")
	w.claudeRecord(idA, other, "default")
	w.handoff(idA, "implementer", "pid", 1, time.Now())
	w.handoff(idB, "reviewer", "host", 1, time.Now()) // no CLI has it
	w.codexRecord(idC, w.root)
	old := time.Now().Add(-40 * 24 * time.Hour)
	os.Chtimes(filepath.Join(w.home, ".codex", "sessions", "2026", "11", "19", "rollout-2026-11-19T10-00-00-"+idC+".jsonl"), old, old)
	w.handoff(idC, "reviewer-2", "host", 1, time.Now())
	if got := w.printLines(t); len(got) != 0 {
		t.Fatalf("brought back %q", got)
	}
}

// A handoff session seen running is taken with its process's arguments, as
// its hook would have recorded them, once something says that process runs
// it: Claude Code's list of its sessions, or for agy the session's presence
// lock in that process. So once it closes, it comes back with its flags.
func TestHandoffSessionRunningIsTakenWithItsFlags(t *testing.T) {
	w := newWorld(t)
	w.agyRecord(idC, w.root)
	agy := w.standIn("agy", "--model", "gemini-x", "--dangerously-skip-permissions")
	w.handoff(idC, "reviewer-3", "host", agy, time.Now())
	w.held = []string{filepath.Join(w.home, ".gemini", "antigravity-cli", "presence", idC+".lock")}
	w.claudeRecord(idA, w.root, "default")
	claude := w.standIn("claude", "--add-dir", w.home)
	w.handoff(idA, "implementer", "pid", claude, time.Now())
	w.agents = fmt.Sprintf(`[{"pid":%d,"kind":"interactive","sessionId":%q}]`, claude, idA)

	rows, _, err := w.app(w.root, "").view(w.repo, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.state != open {
			t.Fatalf("%s %s isn't open", r.CLI, r.ID)
		}
	}
	for _, e := range w.list().Entries {
		if e.Found != "handoff" || len(e.Args) == 0 || e.PID == 0 {
			t.Fatalf("not taken with its process: %+v", e)
		}
	}
	stop(agy)
	stop(claude)
	w.held, w.agents = nil, "[]"
	got := w.printLines(t)
	want := []string{
		cd(w.root) + "claude --resume " + idA + " --add-dir " + w.home,
		cd(w.root) + "agy --conversation " + idC + " --model gemini-x --dangerously-skip-permissions --prompt-interactive 'You were reopened. Read your handoff inbox.'",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A later run of a session, seen with the same proof, replaces the flags
// taken from an earlier one: agy runs no hook on a resume, so this is how
// the list learns the flags changed. A hook's report isn't replaced.
func TestHandoffLaterRunReplacesFlags(t *testing.T) {
	w := newWorld(t)
	w.agyRecord(idC, w.root)
	w.held = []string{filepath.Join(w.home, ".gemini", "antigravity-cli", "presence", idC+".lock")}
	first := w.standIn("agy", "--dangerously-skip-permissions")
	w.handoff(idC, "reviewer-3", "host", first, time.Now())
	if _, _, err := w.app(w.root, "").view(w.repo, true); err != nil {
		t.Fatal(err)
	}
	if e := w.list().Entries[0]; e.PID != first || !slices.Contains(e.Args, "--dangerously-skip-permissions") {
		t.Fatalf("first run: %+v", e)
	}
	stop(first)
	second := w.standIn("agy", "--conversation", idC)
	w.handoff(idC, "reviewer-3", "host", second, time.Now())
	if _, _, err := w.app(w.root, "").view(w.repo, true); err != nil {
		t.Fatal(err)
	}
	if e := w.list().Entries[0]; e.PID != second || slices.Contains(e.Args, "--dangerously-skip-permissions") {
		t.Fatalf("second run: %+v", e)
	}
	stop(second)
	w.held = nil
	if got := w.printLines(t); len(got) != 1 || strings.Contains(got[0], "--dangerously") {
		t.Fatalf("got %q", got)
	}

	// Once the session's hook reports it, a run seen through handoff
	// doesn't replace that report.
	w2 := newWorld(t)
	w2.claudeRecord(idA, w2.root, "default")
	hooked := w2.standIn("claude", "--resume", idA, "--dangerously-skip-permissions")
	w2.claudeHook(idA, "resume", w2.root, hooked)
	w2.handoff(idA, "implementer", "pid", hooked, time.Now())
	other := w2.standIn("claude")
	w2.agents = fmt.Sprintf(`[{"pid":%d,"kind":"interactive","sessionId":%q}]`, other, idA)
	if _, _, err := w2.app(w2.root, "").view(w2.repo, true); err != nil {
		t.Fatal(err)
	}
	if e := w2.list().Entries[0]; e.PID != hooked || e.Found != "" {
		t.Fatalf("a hook's report was replaced: %+v", e)
	}
}

// Two reopens at once: one sees an earlier run of a session, the other a
// later one and writes first. The one that takes the lock last doesn't put
// the earlier run's flags back: the later-started process is the newer.
func TestAnEarlierRunSeenLastDoesntWin(t *testing.T) {
	w := newWorld(t)
	w.agyRecord(idC, w.root)
	w.held = []string{filepath.Join(w.home, ".gemini", "antigravity-cli", "presence", idC+".lock")}
	first := w.standIn("agy", "--dangerously-skip-permissions")
	w.handoff(idC, "reviewer-3", "host", first, time.Now())
	var second int
	slow := w.app(w.root, "")
	seen := false
	slow.process = func(pid int) (proc.Info, bool) {
		info, ok := proc.Get(pid)
		if pid == first && !seen {
			// This reopen has seen the first run. Before it takes the lock,
			// the session closes, comes back without the flag, and another
			// reopen sees that and writes.
			seen = true
			stop(first)
			second = w.standIn("agy", "--conversation", idC)
			w.handoff(idC, "reviewer-3", "host", second, time.Now())
			if _, _, err := w.app(w.root, "").view(w.repo, true); err != nil {
				t.Fatal(err)
			}
		}
		return info, ok
	}
	if _, _, err := slow.view(w.repo, true); err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("the slow reopen never looked at the first run")
	}
	if e := w.list().Entries[0]; e.PID != second || slices.Contains(e.Args, "--dangerously-skip-permissions") {
		t.Fatalf("the earlier run's flags came back: %+v", e)
	}
	stop(second)
	w.held = nil
	if got := w.printLines(t); len(got) != 1 || strings.Contains(got[0], "--dangerously") {
		t.Fatalf("got %q", got)
	}
}

// A process's name and start time don't say which session it runs, so
// without more its arguments aren't taken: not a Claude Code process
// Claude Code doesn't list for the session, nor an agy whose arguments
// resume another conversation, nor one that holds no lock of the session's,
// nor Codex's.
func TestHandoffProcessMustBeTheSessions(t *testing.T) {
	w := newWorld(t)
	w.claudeRecord(idA, w.root, "default")
	w.handoff(idA, "implementer", "pid", w.standIn("claude", "--dangerously-skip-permissions"), time.Now())
	other := testID("eeeeeeee")
	w.agyRecord(idC, w.root)
	agy := w.standIn("agy", "--conversation", other, "--dangerously-skip-permissions")
	w.handoff(idC, "reviewer-3", "host", agy, time.Now())
	// The stand-in lsof answers alike for every process: it holds idC's lock.
	w.held = []string{filepath.Join(w.home, ".gemini", "antigravity-cli", "presence", idC+".lock")}
	w.codexRecord(idB, w.root)
	w.handoff(idB, "reviewer", "host", w.standIn("codex", "--dangerously-bypass-approvals-and-sandbox"), time.Now())

	if _, _, err := w.app(w.root, "").view(w.repo, true); err != nil {
		t.Fatal(err)
	}
	for _, e := range w.list().Entries {
		if len(e.Args) != 0 || e.PID != 0 {
			t.Fatalf("took a process that may not be the session's: %+v", e)
		}
	}
	// Without the lock, an agy that resumes nothing isn't taken either.
	w2 := newWorld(t)
	w2.agyRecord(idC, w2.root)
	w2.handoff(idC, "reviewer-3", "host", w2.standIn("agy", "--dangerously-skip-permissions"), time.Now())
	if _, _, err := w2.app(w2.root, "").view(w2.repo, true); err != nil {
		t.Fatal(err)
	}
	if e := w2.list().Entries[0]; len(e.Args) != 0 {
		t.Fatalf("took an agy that holds no lock of the session's: %+v", e)
	}
}

// forget reaches a row found through handoff, on the list or not yet, and
// reopen doesn't find it through handoff again; once the session starts,
// its hook puts it back.
func TestForgetAHandoffSession(t *testing.T) {
	w := newWorld(t)
	w.claudeRecord(idA, w.root, "default")
	w.handoff(idA, "implementer", "pid", 1, time.Now())
	w.codexRecord(idB, w.root)
	w.handoff(idB, "reviewer", "host", 1, time.Now())
	if got := w.printLines(t); len(got) != 2 {
		t.Fatalf("got %q", got)
	}
	// Not on the list yet: forget by the start of its id.
	if code := w.run(w.root, "", "forget", idA[:8]); code != 0 {
		t.Fatalf("forget exit %d: %s", code, w.stderr.String())
	}
	if !strings.Contains(w.stdout.String(), "claude --resume "+idA) {
		t.Fatalf("forget said %q", w.stdout.String())
	}
	if got := w.printLines(t); len(got) != 1 || !strings.Contains(got[0], idB) {
		t.Fatalf("after forget: %q", got)
	}
	// On the list: forget all.
	if _, _, err := w.app(w.root, "").view(w.repo, true); err != nil {
		t.Fatal(err)
	}
	if code := w.run(w.root, "", "forget"); code != 0 {
		t.Fatalf("forget exit %d: %s", code, w.stderr.String())
	}
	if got := w.printLines(t); len(got) != 0 {
		t.Fatalf("after forget all: %q", got)
	}
	if _, _, err := w.app(w.root, "").view(w.repo, true); err != nil {
		t.Fatal(err)
	}
	if l := w.list(); len(l.Entries) != 0 || len(l.Forgotten) != 2 {
		t.Fatalf("list %+v", l)
	}
	// Its hook, when it starts, puts it back.
	w.claudeHook(idA, "resume", w.root, w.standIn("claude", "--resume", idA))
	if l := w.list(); len(l.Entries) != 1 || slices.Contains(l.Forgotten, idA) || !slices.Contains(l.Forgotten, idB) {
		t.Fatalf("after its hook: %+v", l)
	}
	// Once handoff names it no more, it needn't be kept.
	os.Remove(filepath.Join(w.root, ".handoff", "sessions", idB+".json"))
	if _, _, err := w.app(w.root, "").view(w.repo, true); err != nil {
		t.Fatal(err)
	}
	if l := w.list(); len(l.Forgotten) != 0 {
		t.Fatalf("forgotten %q", l.Forgotten)
	}

	// Nor when it is all the list holds: given a role again later, it is
	// found again.
	w2 := newWorld(t)
	w2.codexRecord(idB, w2.root)
	w2.handoff(idB, "reviewer", "host", 1, time.Now())
	if code := w2.run(w2.root, "", "forget"); code != 0 {
		t.Fatalf("forget exit %d: %s", code, w2.stderr.String())
	}
	if l := w2.list(); len(l.Entries) != 0 || len(l.Forgotten) != 1 {
		t.Fatalf("list %+v", l)
	}
	os.Remove(filepath.Join(w2.root, ".handoff", "sessions", idB+".json"))
	if _, _, err := w2.app(w2.root, "").view(w2.repo, true); err != nil {
		t.Fatal(err)
	}
	if l := w2.list(); len(l.Forgotten) != 0 {
		t.Fatalf("forgotten %q", l.Forgotten)
	}
	w2.handoff(idB, "reviewer", "host", 1, time.Now())
	if got := w2.printLines(t); len(got) != 1 {
		t.Fatalf("given a role again, got %q", got)
	}
}

// Claude Code's own list of its sessions in terminals says one is open,
// whatever reopen recorded of its process.
func TestClaudeAgentsSaysOpen(t *testing.T) {
	w := newWorld(t)
	w.add(store.Entry{CLI: "claude", ID: idA, Args: []string{"claude"}})
	w.transcript(idA, "x", time.Now().Add(-time.Hour))
	if got := w.printLines(t); len(got) != 1 {
		t.Fatalf("got %q", got)
	}
	w.agents = fmt.Sprintf(`[{"pid":1,"kind":"interactive","sessionId":%q,"status":"idle"}]`, idA)
	if got := w.printLines(t); len(got) != 0 {
		t.Fatalf("an open session brought back: %q", got)
	}
}
