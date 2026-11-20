package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dre4success/reopen/internal/shell"
)

// codexHook is the command reopen's Codex start hook runs.
func codexHook() string { return "exec " + shell.Quote(selfCommand()) + " hook codex" }

func (w *world) settingsFile(rel, data string, mode os.FileMode) string {
	w.t.Helper()
	p := filepath.Join(w.home, rel)
	os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(data), mode); err != nil {
		w.t.Fatal(err)
	}
	os.Chmod(p, mode)
	return p
}

func TestInitAddsTheHooks(t *testing.T) {
	w := newWorld(t)
	w.installed = map[string]bool{"claude": true, "codex": true, "agy": true}
	claudeOld := "{\n  \"model\": \"opus\",\n  \"hooks\": {\n    \"SessionStart\": [\n      {\n        \"hooks\": [{\"type\": \"command\", \"command\": \"handoff hook brief\"}]\n      }\n    ]\n  }\n}\n"
	claude := w.settingsFile(".claude/settings.json", claudeOld, 0o644)
	agyOld := `{"reopen": {"SessionStart": [{"type": "command", "command": "exec reopen hook antigravity"}]}}`
	agy := w.settingsFile(".gemini/config/hooks.json", agyOld, 0o600)
	codex := filepath.Join(w.home, ".codex", "hooks.json") // not there yet

	if code := w.run(w.root, "\n", "init"); code != 0 {
		t.Fatalf("init exit %d: %s %s", code, w.stdout.String(), w.stderr.String())
	}
	out := stripANSI(w.stdout.String())
	for _, s := range []string{"+ Claude Code  to add", "+ Codex        to add", "✓ Antigravity  set up already", "Add it to 2 files?", "Codex runs reopen's hook only once you trust it"} {
		if !strings.Contains(out, s) {
			t.Fatalf("init said:\n%s\nlacking %q", out, s)
		}
	}
	// Claude Code's settings: everything as it was, and the hook after
	// handoff's; the old file beside it, with its mode.
	data, _ := os.ReadFile(claude)
	h, _ := w.app(w.root, "").hookSpec("claude")
	keep := claudeOld[:strings.Index(claudeOld, "      }\n    ]")] + "      }"
	if !h.has(data) || !strings.Contains(string(data), "handoff hook brief") || !strings.HasPrefix(string(data), keep) {
		t.Fatalf("settings.json:\n%s", data)
	}
	if st, _ := os.Stat(claude); st.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	backups, _ := filepath.Glob(claude + ".reopen-backup-*")
	if len(backups) != 1 {
		t.Fatalf("backups %q", backups)
	}
	if b, _ := os.ReadFile(backups[0]); string(b) != claudeOld {
		t.Fatalf("the backup holds %q", b)
	}
	// Codex's: a new file, the operator's alone.
	data, _ = os.ReadFile(codex)
	hc, _ := w.app(w.root, "").hookSpec("codex")
	if st, err := os.Stat(codex); err != nil || st.Mode().Perm() != 0o600 || !hc.has(data) || !json.Valid(data) {
		t.Fatalf("hooks.json: %v\n%s", err, data)
	}
	if b, _ := os.ReadFile(agy); string(b) != agyOld {
		t.Fatal("Antigravity's settings changed though they had the hook")
	}

	// Again: nothing to add, nothing asked.
	if code := w.run(w.root, "", "init"); code != 0 || strings.Contains(w.stdout.String(), "Add it") || strings.Count(stripANSI(w.stdout.String()), "set up already") != 3 {
		t.Fatalf("init again, exit %d:\n%s", code, w.stdout.String())
	}
	if backups, _ := filepath.Glob(claude + ".reopen-backup-*"); len(backups) != 1 {
		t.Fatalf("backups %q", backups)
	}
}

func TestInitAsksFirst(t *testing.T) {
	w := newWorld(t)
	w.installed = map[string]bool{"claude": true}
	claude := w.settingsFile(".claude/settings.json", "{}\n", 0o600)
	for _, c := range []struct {
		stdin    string
		terminal bool
		vars     map[string]string
		code     int
	}{
		{"n\n", true, nil, 0},
		{"", true, nil, 0},  // Ctrl-D: no line, so no yes
		{"y", true, nil, 0}, // nor half of one
		{"\n", false, nil, 1},
		{"\n", true, map[string]string{"CODEX_THREAD_ID": "x"}, 1},
	} {
		w.terminal = c.terminal
		w.vars = map[string]string{}
		for k, v := range c.vars {
			w.vars[k] = v
		}
		if code := w.run(w.root, c.stdin, "init"); code != c.code {
			t.Fatalf("%+v: exit %d %s", c, code, w.stderr.String())
		}
		if b, _ := os.ReadFile(claude); string(b) != "{}\n" {
			t.Fatalf("%+v: changed to %s", c, b)
		}
	}
}

// A settings file that is a link stays one: the file it names gets the
// hook. One that isn't JSON is left as it is, with a word.
func TestInitLinksAndBrokenFiles(t *testing.T) {
	w := newWorld(t)
	w.installed = map[string]bool{"claude": true, "codex": true}
	dots := filepath.Join(w.home, "dotfiles", "claude.json")
	w.settingsFile("dotfiles/claude.json", `{"theme": "dark"}`, 0o644)
	os.MkdirAll(filepath.Join(w.home, ".claude"), 0o700)
	link := filepath.Join(w.home, ".claude", "settings.json")
	os.Symlink(dots, link)
	broken := w.settingsFile(".codex/hooks.json", "{\"hooks\": // a comment\n}", 0o600)

	if code := w.run(w.root, "y\n", "init"); code != 0 {
		t.Fatalf("exit %d: %s", code, w.stdout.String())
	}
	out := stripANSI(w.stdout.String())
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced by a file")
	}
	data, _ := os.ReadFile(dots)
	h, _ := w.app(w.root, "").hookSpec("claude")
	if !h.has(data) || !strings.HasPrefix(string(data), `{"theme": "dark", "hooks": {`) {
		t.Fatalf("the linked file:\n%s", data)
	}
	if b, _ := os.ReadFile(broken); string(b) != "{\"hooks\": // a comment\n}" {
		t.Fatal("a file that isn't JSON was changed")
	}
	if !strings.Contains(out, "✗ Codex") || !strings.Contains(out, "isn't valid JSON") || !strings.Contains(out, "reopen hooks codex prints it") {
		t.Fatalf("init said:\n%s", out)
	}
}

// Codex runs a hook only once the operator trusts it, and notes that in
// its config.toml; reopen says when it hasn't.
func TestCodexTrust(t *testing.T) {
	w := newWorld(t)
	w.installed = map[string]bool{"codex": true}
	hooks := fmt.Sprintf(`{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "exec handoff hook brief"}]}, {"hooks": [{"type": "command", "command": %q}]}]}}`, codexHook())
	w.settingsFile(".codex/hooks.json", hooks, 0o600)
	file := filepath.Join(w.home, ".codex", "hooks.json")
	conf := fmt.Sprintf("model = \"x\"\n\n[hooks.state.%q]\ntrusted_hash = \"sha256:a\"\n", file+":session_start:0:0")
	w.settingsFile(".codex/config.toml", conf, 0o600)
	w.run(w.root, "", "list")
	if !strings.Contains(w.stdout.String(), "Codex has no record of trusting reopen's hook") {
		t.Fatalf("list said:\n%s", w.stdout.String())
	}
	conf += fmt.Sprintf("\n[hooks.state.%q]\ntrusted_hash = \"sha256:b\"\n", file+":session_start:1:0")
	w.settingsFile(".codex/config.toml", conf, 0o600)
	w.run(w.root, "", "list")
	if strings.Contains(w.stdout.String(), "trusting") {
		t.Fatalf("list said:\n%s", w.stdout.String())
	}
	// No config.toml: no record of trusting it either.
	os.Remove(filepath.Join(w.home, ".codex", "config.toml"))
	w.run(w.root, "", "init")
	if !strings.Contains(w.stdout.String(), "has no record of that yet") {
		t.Fatalf("init said:\n%s", w.stdout.String())
	}
}

// reopen list says which CLIs here have no hook from reopen yet.
func TestListSaysWhatIsntSetUp(t *testing.T) {
	w := newWorld(t)
	w.installed = map[string]bool{"claude": true, "agy": true}
	w.run(w.root, "", "list")
	if !strings.Contains(w.stdout.String(), "Claude Code and Antigravity have no start hook from reopen") || !strings.Contains(w.stdout.String(), "reopen init adds it") {
		t.Fatalf("list said:\n%s", w.stdout.String())
	}
	if _, err := os.Stat(filepath.Join(w.home, ".claude")); !os.IsNotExist(err) {
		t.Fatal("list wrote a CLI's settings")
	}
}

// With no home, or a relative one, init writes nowhere: the CLIs' settings
// would land in the folder it runs in. A settings folder named relative to
// it is left alone too.
func TestInitNeedsAHome(t *testing.T) {
	w := newWorld(t)
	w.installed = map[string]bool{"claude": true, "codex": true, "agy": true}
	for _, home := range []string{"", "rel"} {
		a := w.app(w.root, "\n")
		a.home = home
		if code := a.run([]string{"init"}); code != 1 || !strings.Contains(w.stderr.String(), "no home folder") {
			t.Fatalf("HOME %q: exit %d %q", home, code, w.stderr.String())
		}
	}
	w.vars["CODEX_HOME"] = "rel-codex"
	if code := w.run(w.root, "\n", "init"); code != 0 {
		t.Fatalf("exit %d: %s", code, w.stdout.String())
	}
	if !strings.Contains(stripANSI(w.stdout.String()), "✗ Codex") {
		t.Fatalf("init said:\n%s", w.stdout.String())
	}
	for _, p := range []string{".claude", ".codex", ".gemini", "rel", "rel-codex"} {
		if _, err := os.Stat(filepath.Join(w.root, p)); !os.IsNotExist(err) {
			t.Fatalf("init wrote %s in the folder it ran in", p)
		}
	}
}

// A hook of reopen's for another event doesn't run at a session's start,
// so it doesn't count as set up.
func TestInitHookUnderAnotherEvent(t *testing.T) {
	w := newWorld(t)
	w.installed = map[string]bool{"codex": true}
	h, _ := w.app(w.root, "").hookSpec("codex")
	codex := w.settingsFile(".codex/hooks.json", fmt.Sprintf(`{"hooks": {"PreToolUse": [{"hooks": [{"type": "command", "command": %q}]}]}}`, codexHook()), 0o600)
	if code := w.run(w.root, "\n", "init"); code != 0 || !strings.Contains(stripANSI(w.stdout.String()), "+ Codex        to add") {
		t.Fatalf("exit %d:\n%s", code, w.stdout.String())
	}
	data, _ := os.ReadFile(codex)
	if !h.has(data) || !strings.Contains(string(data), "PreToolUse") {
		t.Fatalf("hooks.json:\n%s", data)
	}
}

// reopen's hook is one whose words run reopen, hook and the CLI, and
// nothing else: written by init, by hand, or as an older reopen wrote it.
// A hook that only mentions those words isn't it.
func TestHookForms(t *testing.T) {
	w := newWorld(t)
	a := w.app(w.root, "")
	codex, _ := a.hookSpec("codex")
	claude, _ := a.hookSpec("claude")
	start := func(hooks ...string) []byte {
		return []byte(`{"hooks": {"SessionStart": [{"hooks": [` + strings.Join(hooks, ",") + `]}]}}`)
	}
	cmd := func(c string) string {
		b, _ := json.Marshal(map[string]string{"type": "command", "command": c})
		return string(b)
	}
	for _, c := range []struct {
		spec hookSpec
		hook string
		want bool
	}{
		{codex, cmd(codexHook()), true},
		{codex, cmd("exec reopen hook codex"), true},
		{codex, cmd("reopen hook codex"), true},
		{codex, cmd("/usr/local/bin/reopen hook codex"), true},
		{codex, cmd("exec '/a b/reopen' hook codex"), true},
		{codex, cmd(`exec "/a b/reopen" hook codex`), true},
		{codex, cmd("echo reopen hook codex"), false},
		{codex, cmd("reopen hook codex; true"), false},
		{codex, cmd("reopen hook codex && true"), false},
		{codex, cmd("reopen hook codex extra"), false},
		{codex, cmd("exec reopen hook claude"), false},
		{codex, cmd("reopenx hook codex"), false},
		{codex, cmd("exec $REOPEN hook codex"), false},
		{codex, cmd("exec 'reopen hook codex"), false},
		{claude, `{"type": "command", "command": "reopen", "args": ["hook", "claude"]}`, true},
		{claude, `{"type": "command", "command": "/a b/reopen", "args": ["hook", "claude"]}`, true},
		{claude, `{"type": "command", "command": "reopen hook claude"}`, true},
		{claude, `{"type": "command", "command": "echo", "args": ["reopen", "hook", "claude"]}`, false},
		{claude, `{"type": "command", "command": "reopen", "args": ["hook", "claude", "x"]}`, false},
	} {
		if got := c.spec.has(start(c.hook)); got != c.want {
			t.Errorf("%s %s: has = %v", c.spec.cli, c.hook, got)
		}
	}
	agy, _ := a.hookSpec("antigravity")
	if !agy.has([]byte(`{"x": {"SessionStart": [{"command": "exec reopen hook antigravity"}]}}`)) ||
		agy.has([]byte(`{"x": {"SessionStart": [{"command": "echo exec reopen hook antigravity"}]}}`)) {
		t.Error("Antigravity's groups")
	}
}

// A start hook that only mentions reopen's words doesn't stop init adding
// reopen's.
func TestInitAddsBesideAHookThatOnlyMentionsReopen(t *testing.T) {
	w := newWorld(t)
	w.installed = map[string]bool{"codex": true}
	p := w.settingsFile(".codex/hooks.json", `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "echo reopen hook codex"}]}]}}`, 0o600)
	if code := w.run(w.root, "\n", "init"); code != 0 || !strings.Contains(stripANSI(w.stdout.String()), "+ Codex        to add") {
		t.Fatalf("exit %d:\n%s", code, w.stdout.String())
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "echo reopen hook codex") || !strings.Contains(string(data), codexHook()) {
		t.Fatalf("hooks.json:\n%s", data)
	}
}
