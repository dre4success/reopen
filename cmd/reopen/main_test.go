package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dre4success/reopen/internal/proc"
	"github.com/dre4success/reopen/internal/store"
)

// The tests never open a terminal window, start an agent CLI or touch the
// real ~/.reopen/. Each has a temporary home and a scratch git repository.
// A session's process is a stand-in: this test binary, started again under
// the CLI's name (claude, codex, agy), which only sleeps. Windows are never
// opened: openTabs is a stand-in that notes the commands.

func TestMain(m *testing.M) {
	if os.Getenv("REOPEN_TEST_STANDIN") == "1" {
		time.Sleep(2 * time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type world struct {
	t          *testing.T
	home, root string // root is the main worktree
	repo       string // its common git folder
	vars       map[string]string
	terminal   bool
	agents     string // what claude agents --json prints
	opened     [][]string
	openErr    error    // what opening the tabs fails with
	held       []string // the files Codex's daemon holds open
	noIterm    bool
	now        time.Time // zero: the clock's
	stdout     bytes.Buffer
	stderr     bytes.Buffer
}

func real(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, home: real(t, t.TempDir()), vars: map[string]string{}, terminal: true, agents: "[]"}
	w.root = filepath.Join(real(t, t.TempDir()), "proj")
	os.Mkdir(w.root, 0o755)
	git(t, w.root, "init", "-q")
	git(t, w.root, "commit", "-q", "--allow-empty", "-m", "first")
	w.repo = filepath.Join(w.root, ".git")
	return w
}

func (w *world) app(cwd, stdin string) *app {
	w.stdout.Reset()
	w.stderr.Reset()
	return &app{
		stdin:  strings.NewReader(stdin),
		stdout: &w.stdout,
		stderr: &w.stderr,
		getenv: func(k string) string { return w.vars[k] },
		home:   w.home,
		cwd:    cwd,
		now: func() time.Time {
			if !w.now.IsZero() {
				return w.now
			}
			return time.Now()
		},
		terminal:     func() bool { return w.terminal },
		ppid:         func() int { return 0 },
		process:      proc.Get,
		scan:         proc.Scan,
		claudeAgents: func() ([]byte, error) { return []byte(w.agents), nil },
		openTabs: func(c []string) error {
			w.opened = append(w.opened, c)
			return w.openErr
		},
		openFiles:      func(pids []int) ([]string, error) { return w.held, nil },
		itermInstalled: func() bool { return !w.noIterm },
		tty:            func() *os.File { return nil },
	}
}

// run runs reopen in folder cwd with stdin, and returns its exit code.
func (w *world) run(cwd, stdin string, args ...string) int {
	return w.app(cwd, stdin).run(args)
}

// standIn starts a process that only sleeps, with args as its arguments,
// args[0] its name. It is ended when the test ends.
func (w *world) standIn(args ...string) int {
	w.t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Args = args
	cmd.Env = append(os.Environ(), "REOPEN_TEST_STANDIN=1")
	if err := cmd.Start(); err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	for range 100 { // until the kernel has its arguments
		if info, ok := proc.Get(cmd.Process.Pid); ok && info.Args[0] == args[0] {
			return cmd.Process.Pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	w.t.Fatal("the stand-in didn't start")
	return 0
}

// stop ends the stand-in pid.
func stop(pid int) {
	p, _ := os.FindProcess(pid)
	p.Kill()
	time.Sleep(50 * time.Millisecond)
}

// claudeHook runs Claude Code's start hook for session id, from the process
// pid started in folder.
func (w *world) claudeHook(id, source, folder string, pid int) {
	w.t.Helper()
	w.vars["CLAUDE_PROJECT_DIR"] = folder
	w.vars["CLAUDE_PID"] = fmt.Sprint(pid)
	in := fmt.Sprintf(`{"session_id":%q,"source":%q,"hook_event_name":"SessionStart","transcript_path":"/nowhere/%s.jsonl"}`, id, source, id)
	a := w.app(w.home, in) // a hook's own folder doesn't matter for Claude Code
	if code := a.run([]string{"hook", "claude"}); code != 0 {
		w.t.Fatalf("hook exit %d", code)
	}
	delete(w.vars, "CLAUDE_PROJECT_DIR")
	delete(w.vars, "CLAUDE_PID")
	if w.stdout.Len() != 0 {
		w.t.Fatalf("the hook printed %q on stdout", w.stdout.String())
	}
}

// parentHook runs Codex's or Antigravity's start hook, whose parent is pid.
func (w *world) parentHook(cli, cwd, in string, pid int) {
	w.t.Helper()
	a := w.app(cwd, in)
	a.ppid = func() int { return pid }
	if code := a.run([]string{"hook", cli}); code != 0 {
		w.t.Fatalf("hook exit %d", code)
	}
	if w.stdout.Len() != 0 {
		w.t.Fatalf("the hook printed %q on stdout", w.stdout.String())
	}
}

func (w *world) list() *store.List {
	w.t.Helper()
	l, err := store.Load(w.home, w.repo)
	if err != nil {
		w.t.Fatal(err)
	}
	return l
}

// transcript writes Claude Code's transcript for id, last written at.
func (w *world) transcript(id, first string, at time.Time, more ...string) string {
	w.t.Helper()
	p := filepath.Join(w.home, ".claude", "projects", "-proj", id+".jsonl")
	lines := []string{fmt.Sprintf(`{"type":"user","message":{"role":"user","content":%q}}`, first), `{"type":"assistant","message":{"role":"assistant"}}`}
	writeFile(w.t, p, strings.Join(append(lines, more...), "\n")+"\n", at)
	return p
}

func (w *world) rollout(id, first string, at time.Time) {
	w.t.Helper()
	p := filepath.Join(w.home, ".codex", "sessions", "2026", "11", "19", "rollout-2026-11-19T10-00-00-"+id+".jsonl")
	writeFile(w.t, p, fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q}}`+"\n"+`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":%q}]}}`+"\n", id, first), at)
}

func (w *world) conversation(id string, at time.Time) {
	w.t.Helper()
	writeFile(w.t, filepath.Join(w.home, ".gemini", "antigravity-cli", "conversations", id+".db"), "db", at)
}

func writeFile(t *testing.T, p, data string, at time.Time) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if !at.IsZero() {
		os.Chtimes(p, at, at)
	}
}

// role gives session id the role in handoff's session files.
func (w *world) role(id, role string, v int) {
	w.t.Helper()
	b, _ := json.Marshal(map[string]any{"v": v, "role": role, "session": id, "pid": 1, "since": "2026-11-19T00:00:00Z"})
	writeFile(w.t, filepath.Join(w.root, ".handoff", "sessions", id+".json"), string(b), time.Time{})
}

func (w *world) printLines(t *testing.T) []string {
	t.Helper()
	if code := w.run(w.root, "", "--print"); code != 0 {
		t.Fatalf("--print exit %d: %s", code, w.stderr.String())
	}
	out := strings.TrimSpace(w.stdout.String())
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func cd(folder string) string { return "cd -- " + folder + " && " }

// The sessions' ids. Their first 8 characters, which reopen shows, are
// fixed; the rest is this run's own. reopen looks at every process on the
// Mac for one that resumes an id, and another run of these tests at the
// same time, in a reviewer's worktree say, has stand-ins of its own.
var (
	idA = testID("aaaaaaaa")
	idB = testID("bbbbbbbb")
	idC = testID("cccccccc")
	idD = testID("dddddddd")
)

func testID(first string) string {
	b := make([]byte, 11)
	rand.Read(b)
	h := hex.EncodeToString(b)
	return first + "-" + h[0:4] + "-4" + h[4:7] + "-8" + h[7:10] + "-" + h[10:22]
}

// execSh runs line with sh, as a tab's shell would, with env.
func execSh(line string, env ...string) *exec.Cmd {
	cmd := exec.Command("/bin/sh", "-c", line)
	cmd.Env = env
	return cmd
}
