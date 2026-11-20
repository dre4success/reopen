package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The built binary, in the real world's wiring, with a temporary home, a
// stand-in claude on PATH (for claude agents --json) and a stand-in
// osascript. It is never given a terminal, so it never opens a window.
func TestBinary(t *testing.T) {
	w := newWorld(t)
	bin := filepath.Join(real(t, t.TempDir()), "bin")
	os.Mkdir(bin, 0o755)
	reopen := filepath.Join(bin, "reopen")
	if out, err := exec.Command("go", "build", "-o", reopen, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n[ \"$1 $2\" = 'agents --json' ] && echo '[]' && exit 0\necho 'the stand-in claude was asked:' \"$@\" >&2; exit 1\n"), 0o755)
	marker := filepath.Join(bin, "osascript-ran")
	os.WriteFile(filepath.Join(bin, "osascript"), []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755)
	gitDir := filepath.Dir(mustLook(t, "git"))
	env := []string{"HOME=" + w.home, "PATH=" + bin + ":" + gitDir + ":/usr/bin:/bin:/usr/sbin:/sbin", "REOPEN_TEST_OSASCRIPT=" + filepath.Join(bin, "osascript")}
	run := func(dir, stdin string, extra []string, args ...string) (int, string, string) {
		cmd := exec.Command(reopen, args...)
		cmd.Dir, cmd.Env, cmd.Stdin = dir, append(env, extra...), strings.NewReader(stdin)
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return code, out.String(), errb.String()
	}

	pid := w.standIn("claude", "--dangerously-skip-permissions", "start here")
	code, out, errs := run(w.home, `{"session_id":"`+idA+`","source":"startup"}`, []string{"CLAUDE_PROJECT_DIR=" + w.root, fmt.Sprintf("CLAUDE_PID=%d", pid), "CLAUDE_CODE_ENTRYPOINT=cli"}, "hook", "claude")
	if code != 0 || out != "" || errs != "" {
		t.Fatalf("hook: exit %d, stdout %q, stderr %q", code, out, errs)
	}
	w.transcript(idA, "start here", time.Time{})
	if code, out, _ := run(w.root, "", nil, "--print"); code != 0 || out != "" {
		t.Fatalf("--print while it runs: exit %d, %q", code, out)
	}
	stop(pid)
	code, out, errs = run(w.root, "", nil, "--print")
	if want := cd(w.root) + "claude --resume " + idA + " --dangerously-skip-permissions\n"; code != 0 || out != want {
		t.Fatalf("--print: exit %d, %q, want %q (stderr %q)", code, out, want, errs)
	}
	if code, out, _ := run(w.root, "", nil, "list"); code != 0 || !strings.Contains(out, "closed      claude") {
		t.Fatalf("list: exit %d\n%s", code, out)
	}
	// Without a terminal, or in an agent's session, it opens nothing.
	if code, _, errs := run(w.root, "\n", nil); code != 1 || !strings.Contains(errs, "isn't a terminal") {
		t.Fatalf("no terminal: exit %d, %q", code, errs)
	}
	if code, _, errs := run(w.root, "\n", []string{"CODEX_THREAD_ID=x"}); code != 1 || !strings.Contains(errs, "refused in an agent's session") {
		t.Fatalf("agent: exit %d, %q", code, errs)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("osascript ran")
	}
	if code, out, _ := run(w.root, "", nil, "hooks", "codex"); code != 0 || !strings.Contains(out, `"exec reopen hook codex"`) {
		t.Fatalf("hooks: %q", out)
	}
	if code, _, _ := run(w.root, "", nil, "frobnicate"); code != 2 {
		t.Fatalf("an unknown command: exit %d", code)
	}
	// A build from a clone has no release version: a pseudo-version from
	// git, or (devel). go install …@v0.1.0 gives v0.1.0.
	if code, out, _ := run(w.root, "", nil, "version"); code != 0 || strings.Count(out, "\n") != 1 || !(strings.HasPrefix(out, "v") || out == "(devel)\n") {
		t.Fatalf("version: exit %d, %q", code, out)
	}
	// A claude that is a script whose child hangs, holding the pipe: reopen
	// gives up on it within its limit, and lists all the same.
	os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nsleep 30\n"), 0o755)
	start := time.Now()
	if code, out, _ := run(w.root, "", nil, "list"); code != 0 || !strings.Contains(out, "closed      claude") {
		t.Fatalf("list with a hanging claude: exit %d\n%s", code, out)
	}
	if took := time.Since(start); took > 8*time.Second {
		t.Fatalf("list waited %v on a hanging claude agents --json", took)
	}
}

func mustLook(t *testing.T, name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
