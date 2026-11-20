package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func cli(t *testing.T, name string) *CLI {
	t.Helper()
	c, ok := Lookup(name)
	if !ok {
		t.Fatalf("no CLI %s", name)
	}
	return c
}

func TestInteractive(t *testing.T) {
	for _, c := range []struct {
		cli  string
		args []string
		want bool
	}{
		{Claude, []string{"claude"}, true},
		{Claude, []string{"claude", "--dangerously-skip-permissions"}, true},
		{Claude, []string{"/x/.local/bin/claude", "--resume", "id", "--model", "opus"}, true},
		{Claude, []string{"claude", "-p", "hello"}, false},
		{Claude, []string{"claude", "--model", "haiku", "--print", "hello"}, false},
		{Claude, []string{"claude", "--output-format", "stream-json", "--verbose"}, false},
		{Claude, []string{"claude", "--", "-p"}, true}, // a prompt, after --
		{Codex, []string{"codex"}, true},
		{Codex, []string{"codex", "fix the tests"}, true},
		{Codex, []string{"codex", "resume", "019a-id"}, true},
		{Codex, []string{"codex", "-m", "o3", "fork"}, true},
		{Codex, []string{"codex", "exec", "hi"}, false},
		{Codex, []string{"codex", "e", "hi"}, false},
		{Codex, []string{"codex", "-c", "model=o3", "exec", "hi"}, false},
		{Codex, []string{"codex", "-s", "read-only", "review"}, false},
		// The daemon runs the TUI's sessions: the hook tells them apart by
		// Daemon and Meta.
		{Codex, []string{"codex", "app-server", "--listen", "unix://", "--managed-daemon"}, true},
		{Codex, []string{"codex", "-p", "work", "resume", "--last"}, true}, // -p is --profile
		{Antigravity, []string{"agy"}, true},
		{Antigravity, []string{"agy", "--dangerously-skip-permissions"}, true},
		{Antigravity, []string{"agy", "--conversation", "id", "--model", "m"}, true},
		{Antigravity, []string{"agy", "-p", "hi"}, false},
		{Antigravity, []string{"agy", "--print=hi"}, false},
		{Antigravity, []string{"agy", "--model", "m", "acp"}, false},
		// -p takes the next word: its prompt, even one that looks like a flag.
		{Antigravity, []string{"agy", "-p", "--model"}, false},
		{Antigravity, []string{"agy", "--output-format", "json"}, false},
		// No value is attached to a Go flag: -pfoo is a flag of its own.
		{Antigravity, []string{"agy", "-pfoo"}, true},
	} {
		if got := cli(t, c.cli).Interactive(c.args); got != c.want {
			t.Errorf("%s Interactive(%q) = %v, want %v", c.cli, c.args, got, c.want)
		}
	}
}

func words(fs []Flag) string { return strings.Join(Words(fs), " ") }

func TestFlagsKeepOnlyStartFlagsThatMatter(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "other")
	os.Mkdir(other, 0o700)
	os.Mkdir(filepath.Join(dir, "rel"), 0o700)
	for _, c := range []struct {
		cli  string
		args []string
		want string
	}{
		// A first prompt never comes back, nor a flag reopen doesn't pass on.
		// Claude Code's resume keeps the model and permission mode itself.
		{Claude, []string{"claude", "fix the bug", "--model", "opus", "--dangerously-skip-permissions"}, "--dangerously-skip-permissions"},
		{Claude, []string{"claude", "use --dangerously-skip-permissions"}, ""},
		{Claude, []string{"claude", "--resume", "old", "--model=sonnet", "--name", "x", "--effort", "high", "--allow-dangerously-skip-permissions"}, "--allow-dangerously-skip-permissions"},
		{Claude, []string{"claude", "--resume", "--permission-mode", "plan", "--add-dir", "rel"}, "--add-dir rel"},
		{Claude, []string{"claude", "--dangerously-skip-permissions", "--allowed-tools", "Bash", "Edit", "--allow-dangerously-skip-permissions"}, "--dangerously-skip-permissions --allow-dangerously-skip-permissions"},
		// --add-dir takes every word up to the next flag, a prompt among
		// them: only an existing folder comes back.
		{Claude, []string{"claude", "--add-dir", other, "rel", "/no/such/dir", "fix it"}, "--add-dir " + other + " --add-dir rel"},
		{Claude, []string{"claude", "--add-dir", "rel", "--", "--dangerously-skip-permissions"}, "--add-dir rel"},
		// codex resume keeps them all itself.
		{Codex, []string{"codex", "-m", "gpt-5", "-s", "danger-full-access", "-a", "never", "--add-dir", other, "write tests"}, ""},
		{Codex, []string{"codex", "--dangerously-bypass-approvals-and-sandbox", "a prompt"}, ""},
		{Antigravity, []string{"agy", "--model", "gemini", "--dangerously-skip-permissions", "--mode", "plan", "--sandbox"}, "--model gemini --dangerously-skip-permissions --mode plan --sandbox"},
		{Antigravity, []string{"agy", "-model=gemini", "--sandbox=false", "--add-dir", other, "--effort", "high"}, "--model gemini --sandbox=false --add-dir " + other + " --effort high"},
		{Antigravity, []string{"agy", "--effort", "ultra", "--dangerously-skip-permissions=0"}, "--dangerously-skip-permissions=0"},
		// Go's flag package stops at the first other word.
		{Antigravity, []string{"agy", "--model", "m", "hello", "--dangerously-skip-permissions"}, "--model m"},
	} {
		if got := words(cli(t, c.cli).Flags(c.args, dir)); got != c.want {
			t.Errorf("%s Flags(%q) = %q, want %q", c.cli, c.args, got, c.want)
		}
	}
}

func TestSkipsChecks(t *testing.T) {
	for _, c := range []struct {
		cli  string
		args []string
		want bool
	}{
		{Claude, []string{"claude", "--dangerously-skip-permissions"}, true},
		{Claude, []string{"claude", "--permission-mode", "plan", "--allow-dangerously-skip-permissions"}, false},
		{Antigravity, []string{"agy", "--dangerously-skip-permissions"}, true},
		{Antigravity, []string{"agy", "--dangerously-skip-permissions=false"}, false},
		{Antigravity, []string{"agy", "--dangerously-skip-permissions=F"}, false},
		{Antigravity, []string{"agy", "--dangerously-skip-permissions=1"}, true},
	} {
		if got := SkipsChecks(cli(t, c.cli).Flags(c.args, "")); got != c.want {
			t.Errorf("%s SkipsChecks(%q) = %v, want %v", c.cli, c.args, got, c.want)
		}
	}
}

func TestResume(t *testing.T) {
	flags := func(c string, args ...string) []Flag { return cli(t, c).Flags(append([]string{"x"}, args...), "") }
	for _, c := range []struct {
		cli     string
		flags   []Flag
		message string
		want    []string
	}{
		{Claude, flags(Claude, "--model", "opus", "--dangerously-skip-permissions"), "", []string{"claude", "--resume", "ID", "--dangerously-skip-permissions"}},
		{Codex, flags(Codex, "-m", "o3"), "", []string{"codex", "resume", "ID"}},
		{Codex, nil, Message, []string{"codex", "resume", "ID", Message}},
		{Antigravity, flags(Antigravity, "--model", "m"), "", []string{"agy", "--conversation", "ID", "--model", "m"}},
		{Antigravity, nil, Message, []string{"agy", "--conversation", "ID", "--prompt-interactive", Message}},
	} {
		if got := cli(t, c.cli).Resume("ID", c.flags, c.message); !slices.Equal(got, c.want) {
			t.Errorf("%s Resume = %q, want %q", c.cli, got, c.want)
		}
	}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTranscriptAndLabel(t *testing.T) {
	home := t.TempDir()
	h := HomesFor(home, func(string) string { return "" })
	id := "5e551011-7a2b-4c3d-8e9f-0a1b2c3d4e5f"
	claude := filepath.Join(home, ".claude", "projects", "-x-y", id+".jsonl")
	write(t, claude, strings.Join([]string{
		`{"type":"permission-mode","permissionMode":"plan","sessionId":"` + id + `"}`,
		`{"type":"user","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>Caveat</local-command-caveat>"}}`,
		`{"type":"user","message":{"role":"user","content":"<command-name>/model</command-name>"}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"x"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"  build reopen\n from   the design  "}]}}`,
		`{"type":"user","message":{"role":"user","content":"later"}}`,
	}, "\n")+"\n")
	c := cli(t, Claude)
	if got := c.Transcript(h, id); got != claude {
		t.Fatalf("Transcript = %q, want %q", got, claude)
	}
	if got := c.Label(claude); got != "build reopen from the design" {
		t.Fatalf("Label = %q", got)
	}
	if got := c.Mode(claude); got != "plan" {
		t.Fatalf("Mode = %q", got)
	}
	f, _ := os.OpenFile(claude, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(strings.Repeat(`{"type":"assistant"}`+"\n", 100000) + `{"type":"permission-mode","permissionMode":"bypassPermissions"}` + "\n")
	f.Close()
	if got := c.Mode(claude); got != "bypassPermissions" || !ModeSkips(got) {
		t.Fatalf("Mode after a change far down = %q", got)
	}
	if got := c.Transcript(h, "../etc"); got != "" {
		t.Fatalf("Transcript of an id that isn't plain = %q", got)
	}

	cid := "019a6a3f-0000-7000-8000-000000000001"
	rollout := filepath.Join(home, ".codex", "sessions", "2026", "11", "19", "rollout-2026-11-19T10-00-00-"+cid+".jsonl")
	write(t, rollout, strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"` + cid + `","cwd":"/x"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"rules"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions\n\nx"},{"type":"input_text","text":"<environment_context>\n</environment_context>"}]}}`,
		`{"type":"turn_context","payload":{"cwd":"/x"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"you're reviewer-2"}]}}`,
	}, "\n")+"\n")
	cc := cli(t, Codex)
	if got := cc.Transcript(h, cid); got != rollout {
		t.Fatalf("codex Transcript = %q", got)
	}
	if got := cc.Label(rollout); got != "you're reviewer-2" {
		t.Fatalf("codex Label = %q", got)
	}

	aid := "7c0e-conv"
	db := filepath.Join(home, ".gemini", "antigravity-cli", "conversations", aid+".db")
	write(t, db, "sqlite")
	old := time.Now().Add(-time.Hour)
	os.Chtimes(db, old, old)
	write(t, db+"-wal", "wal")
	ca := cli(t, Antigravity)
	if got := ca.Transcript(h, aid); got != db {
		t.Fatalf("agy Transcript = %q", got)
	}
	if got := LastUse(db); got.Before(old.Add(time.Minute)) {
		t.Fatalf("LastUse = %v, not the write-ahead log's newer time", got)
	}
	if got := ca.Label(db); got != "" {
		t.Fatalf("agy Label = %q", got)
	}
}

func TestShorten(t *testing.T) {
	if got := shorten(strings.Repeat("é", 100), 10); got != strings.Repeat("é", 9)+"…" {
		t.Fatalf("shorten = %q", got)
	}
}

func TestContinuedIn(t *testing.T) {
	dir := t.TempDir()
	seen := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	line := `{"type":"continued-in","continuedInSessionId":"new-id","timestamp":"2026-11-19T11:00:00.000Z"}`
	for _, c := range []struct {
		name  string
		lines []string
		since time.Time
		want  string
	}{
		{"at the end", []string{`{"type":"user","message":{}}`, `{"type":"assistant"}`, line}, seen, "new-id"},
		{"short lines after it", []string{`{"type":"assistant"}`, line, `{"type":"last-prompt"}`, `{"type":"file-history-snapshot"}`}, seen, "new-id"},
		{"resumed after it", []string{line, `{"type":"user","message":{}}`}, seen, ""},
		{"older than the entry's report", []string{line}, seen.Add(2 * time.Hour), ""},
		{"none", []string{`{"type":"assistant"}`}, seen, ""},
	} {
		p := filepath.Join(dir, strings.ReplaceAll(c.name, " ", "-")+".jsonl")
		write(t, p, strings.Join(c.lines, "\n")+"\n")
		if got := ContinuedIn(p, c.since); got != c.want {
			t.Errorf("%s: ContinuedIn = %q, want %q", c.name, got, c.want)
		}
	}
	if got := ContinuedIn(filepath.Join(dir, "missing"), seen); got != "" {
		t.Errorf("missing file: %q", got)
	}
}

func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"plain words":                              "plain words",
		"\x1b[31mred\x1b[0m":                       "red",
		"\x1b]0;new title\x07after":                "after",
		"\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\": "link",
		"bell\x07 and nul\x00":                     "bell and nul",
		"tab\there\nnewline":                       "tab here newline",
		"bidi ‮esrever‬":                           "bidi esrever",
		"c1 \u009b31m":                             "c1 31m",
	} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
	if !HasControl("a\nb") || !HasControl("a\tb") || HasControl("a b/é") {
		t.Error("HasControl")
	}
}

func TestLabelIsCleaned(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.jsonl")
	write(t, p, `{"type":"user","message":{"role":"user","content":"\u001b[2J\u001b[Hfix \u001b]0;pwned\u0007it"}}`+"\n")
	if got := cli(t, Claude).Label(p); got != "fix it" {
		t.Fatalf("Label = %q", got)
	}
}

func TestCodexMetaAndSettings(t *testing.T) {
	dir := t.TempDir()
	tui := filepath.Join(dir, "tui.jsonl")
	ctx := func(model, approval, sandbox string) string {
		return `{"type":"turn_context","payload":{"model":"` + model + `","approval_policy":"` + approval + `","sandbox_policy":{"type":"` + sandbox + `"}}}`
	}
	write(t, tui, strings.Join([]string{
		`{"type":"session_meta","payload":{"originator":"codex-tui","source":"vscode","thread_source":"user","base_instructions":"` + strings.Repeat("x", 30000) + `"}}`,
		ctx("gpt-6-luna", "on-request", "read-only"),
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`,
		ctx("gpt-6.2-sol", "never", "danger-full-access"),
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"\"turn_context\" in text"}]}}`,
	}, "\n")+"\n")
	if o, sub, ok := Meta(tui); o != "codex-tui" || sub || !ok {
		t.Fatalf("Meta = %q %v %v", o, sub, ok)
	}
	if c := Settings(tui); !c.Known || c.Model != "gpt-6.2-sol" || c.Approval != "never" || c.Sandbox != "danger-full-access" || !SettingsSkip(c.Approval, c.Sandbox) {
		t.Fatalf("Settings = %+v", c)
	}
	// A resume with other flags applies new settings, with no turn after:
	// they are what a bare resume comes back with. Added folders are only in
	// thread_settings_applied.
	applied := func(model, approval, profile string, roots ...string) string {
		r, _ := json.Marshal(roots)
		return `{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":"` + model + `","approval_policy":"` + approval + `","permission_profile":` + profile + `,"cwd":"/w/repo","runtime_workspace_roots":` + string(r) + `}}}`
	}
	readOnly := `{"type":"managed","file_system":{"type":"restricted","entries":[{"access":"read"}]}}`
	writable := `{"type":"managed","file_system":{"type":"restricted","entries":[{"access":"read"},{"access":"write"}]}}`
	later := filepath.Join(dir, "later.jsonl")
	write(t, later, strings.Join([]string{
		applied("gpt-6-luna", "on-request", readOnly, "/w/repo"),
		ctx("gpt-6-luna", "on-request", "read-only"),
		applied("gpt-6.2-sol", "never", writable, "/w/repo", "/w/outside"),
	}, "\n")+"\n")
	if c := Settings(later); c.Model != "gpt-6.2-sol" || c.Approval != "never" || c.Sandbox != "workspace-write" || strings.Join(c.Roots, " ") != "/w/outside" {
		t.Fatalf("Settings after a resume with other flags = %+v", c)
	}
	// A turn after: still the folders of the latest applied settings.
	write(t, later, strings.Join([]string{
		applied("gpt-6.2-sol", "never", `{"type":"disabled"}`, "/w/repo", "/w/outside"),
		ctx("gpt-6.2-sol", "never", "danger-full-access"),
	}, "\n")+"\n")
	if c := Settings(later); c.Sandbox != "danger-full-access" || strings.Join(c.Roots, " ") != "/w/outside" {
		t.Fatalf("Settings after a turn = %+v", c)
	}
	if c := Settings(filepath.Join(dir, "none")); c.Known {
		t.Fatalf("Settings of no file = %+v", c)
	}
	guard := filepath.Join(dir, "guard.jsonl")
	write(t, guard, `{"type":"session_meta","payload":{"originator":"Codex Desktop","source":{"subagent":{"other":"guardian"}},"thread_source":"guardian_review"}}`+"\n")
	if o, sub, ok := Meta(guard); o != "Codex Desktop" || !sub || !ok {
		t.Fatalf("Meta of a subagent = %q %v %v", o, sub, ok)
	}
	if _, _, ok := Meta(filepath.Join(dir, "none")); ok {
		t.Fatal("Meta of no file")
	}
	if d, m := Daemon([]string{"/x/codex", "app-server", "--listen", "unix://", "--managed-daemon"}); !d || !m {
		t.Fatal("Daemon of the managed daemon")
	}
	if d, m := Daemon([]string{"/A/codex", "-c", "x=1", "app-server", "--analytics-default-enabled"}); !d || m {
		t.Fatal("Daemon of the desktop app's app-server")
	}
}

func TestAntigravityTranscript(t *testing.T) {
	home := t.TempDir()
	h := HomesFor(home, func(string) string { return "" })
	id := "5b857594-8ff2-4489-ac0f-54ea60d2ef37"
	tr := filepath.Join(home, ".gemini", "antigravity-cli", "brain", id, ".system_generated", "logs", "transcript.jsonl")
	write(t, tr, strings.Join([]string{
		`{"step_index":"0","source":"SYSTEM","type":"USER_INPUT","content":"<USER_REQUEST>\nnot this\n</USER_REQUEST>"}`,
		`{"step_index":"1","source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","content":"<USER_REQUEST>\nyou're reviewer 3\n</USER_REQUEST>\n<ADDITIONAL_METADATA>x</ADDITIONAL_METADATA>"}`,
	}, "\n")+"\n")
	write(t, filepath.Join(home, ".gemini", "antigravity-cli", "conversations", id+".db"), "db")
	c := cli(t, Antigravity)
	if got := c.Transcript(h, id); got != tr {
		t.Fatalf("Transcript = %q", got)
	}
	if got := c.Label(tr); got != "you're reviewer 3" {
		t.Fatalf("Label = %q", got)
	}
}

func TestResumedID(t *testing.T) {
	for _, c := range []struct {
		cli  string
		args []string
		want string
	}{
		{Claude, []string{"claude", "--resume", "ID", "--model", "x"}, "ID"},
		{Claude, []string{"claude", "--resume=ID"}, "ID"},
		{Claude, []string{"claude", "-r", "ID"}, "ID"},
		{Claude, []string{"claude", "--session-id", "ID"}, "ID"},
		{Claude, []string{"claude", "--resume", "--model", "x"}, ""}, // the picker
		{Claude, []string{"claude", "-p", "--resume", "ID"}, ""},     // not interactive
		{Claude, []string{"claude", "inspect ID"}, ""},               // a mention
		{Claude, []string{"claude", "--", "--resume", "ID"}, ""},
		{Codex, []string{"codex", "resume", "ID", "carry on"}, "ID"},
		{Codex, []string{"codex", "-m", "o3", "resume", "-s", "read-only", "ID"}, "ID"},
		{Codex, []string{"codex", "exec", "resume", "ID"}, ""},
		{Codex, []string{"codex", "check ID"}, ""},
		{Antigravity, []string{"agy", "--conversation", "ID", "--model", "m"}, "ID"},
		{Antigravity, []string{"agy", "--conversation=ID"}, "ID"},
		{Antigravity, []string{"agy", "-conversation", "ID"}, "ID"},
		{Antigravity, []string{"agy", "-p", "look at ID"}, ""},
		{Antigravity, []string{"agy", "--continue"}, ""},
	} {
		if got := cli(t, c.cli).ResumedID(c.args); got != c.want {
			t.Errorf("%s ResumedID(%q) = %q, want %q", c.cli, c.args, got, c.want)
		}
	}
}

func TestTitleAndFirst(t *testing.T) {
	dir := t.TempDir()
	c := cli(t, Claude)
	p := filepath.Join(dir, "t.jsonl")
	write(t, p, strings.Join([]string{
		`{"type":"user","isCompactSummary":true,"message":{"role":"user","content":"This session is being continued from a previous conversation"}}`,
		`{"type":"user","message":{"role":"user","content":"resume"}}`,
		`{"type":"ai-title","aiTitle":"First title","sessionId":"x"}`,
		`{"type":"ai-title","aiTitle":"Build the reopen picker","sessionId":"x"}`,
	}, "\n")+"\n")
	if got := c.First(p); got != "resume" {
		t.Fatalf("First = %q", got)
	}
	if got := c.Label(p); got != "Build the reopen picker" {
		t.Fatalf("Label = %q, want the latest ai-title", got)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"custom-title","customTitle":"reviewer-2's \u001b[31mreview","sessionId":"x"}` + "\n" + `{"type":"ai-title","aiTitle":"Later title","sessionId":"x"}` + "\n")
	f.Close()
	if got := c.Label(p); got != "reviewer-2's review" {
		t.Fatalf("Label = %q, want the operator's own name, cleaned", got)
	}
	// Codex has no titles: its first message.
	if got := cli(t, Codex).Title(p); got != "" {
		t.Fatalf("codex Title = %q", got)
	}
}

func TestModeFarBack(t *testing.T) {
	p := filepath.Join(t.TempDir(), "m.jsonl")
	big := strings.Repeat(`{"type":"assistant","x":"`+strings.Repeat("y", 1000)+`"}`+"\n", 600) // 600 KB
	write(t, p, `{"type":"permission-mode","permissionMode":"default"}`+"\n"+big+`{"type":"permission-mode","permissionMode":"bypassPermissions"}`+"\n"+big)
	if got := cli(t, Claude).Mode(p); got != "bypassPermissions" {
		t.Fatalf("Mode = %q: the start's mode was taken for the latest", got)
	}
}
