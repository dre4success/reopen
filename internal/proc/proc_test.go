//go:build darwin

package proc

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A stand-in: the test binary, started again under another name, sleeps.
func TestMain(m *testing.M) {
	if os.Getenv("PROC_TEST_STANDIN") == "1" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func standIn(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Args = args
	cmd.Env = append(os.Environ(), "PROC_TEST_STANDIN=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd
}

func TestGetReadsExactArguments(t *testing.T) {
	before := time.Now()
	args := []string{"claude", "--model", "opus", "use --dangerously-skip-permissions", "a folder/with space"}
	cmd := standIn(t, args...)
	var info Info
	var ok bool
	for range 50 { // the kernel may not have the arguments until exec is done
		if info, ok = Get(cmd.Process.Pid); ok && info.Args[0] == "claude" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ok {
		t.Fatal("Get found no process")
	}
	if !slices.Equal(info.Args, args) {
		t.Fatalf("args = %q, want %q", info.Args, args)
	}
	if info.Start.Before(before.Add(-time.Second)) || info.Start.After(time.Now()) {
		t.Fatalf("start %v is not between %v and now", info.Start, before)
	}
	// ps agrees to the second.
	out, err := exec.Command("/bin/ps", "-o", "lstart=", "-p", strconv.Itoa(cmd.Process.Pid)).Output()
	if err != nil {
		t.Fatal(err)
	}
	ps, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", strings.TrimSpace(string(out)), time.Local)
	if err != nil {
		cmd2 := exec.Command("/bin/ps", "-o", "lstart=", "-p", strconv.Itoa(cmd.Process.Pid))
		cmd2.Env = []string{"LC_ALL=C"}
		out, _ = cmd2.Output()
		ps, err = time.ParseInLocation("Mon Jan _2 15:04:05 2006", strings.TrimSpace(string(out)), time.Local)
		if err != nil {
			t.Fatalf("ps lstart %q: %v", out, err)
		}
	}
	if !info.Start.Truncate(time.Second).Equal(ps) {
		t.Fatalf("start %v, ps says %v", info.Start, ps)
	}
}

func TestGetGone(t *testing.T) {
	cmd := standIn(t, "codex")
	pid := cmd.Process.Pid
	cmd.Process.Kill()
	cmd.Wait() // reaped, so no zombie is left
	if _, ok := Get(pid); ok {
		t.Fatal("Get found a process that has ended")
	}
	if _, ok := Get(0); ok {
		t.Fatal("Get(0) found a process")
	}
}

func TestGetZombie(t *testing.T) {
	cmd := standIn(t, "agy")
	cmd.Process.Kill()
	time.Sleep(200 * time.Millisecond) // dead, not reaped yet
	if _, ok := Get(cmd.Process.Pid); ok {
		t.Fatal("Get took a zombie for a live process")
	}
}

func TestScanFindsAnID(t *testing.T) {
	// An id of this run's own: another run's stand-ins may be about.
	id := fmt.Sprintf("0b7e8f7e-%04x-4222-8333-%012x", os.Getpid()&0xffff, time.Now().UnixNano()&0xffffffffffff)
	cmd := standIn(t, "claude", "--resume", id)
	var found bool
	for range 50 {
		lines, err := Scan()
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range lines {
			if l.PID == cmd.Process.Pid && slices.Contains(l.Fields, id) && Named(l.Fields, "claude") {
				found = true
			}
		}
		if found {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Fatal("Scan did not list the stand-in with its id")
	}
}

func TestNamed(t *testing.T) {
	for _, c := range []struct {
		args []string
		name string
		want bool
	}{
		{[]string{"claude"}, "claude", true},
		{[]string{"/a/b/.local/bin/claude", "--resume", "x"}, "claude", true},
		{[]string{"node", "/n/bin/codex", "resume"}, "codex", true},
		{[]string{"node", "/n/bin/other"}, "codex", false},
		{[]string{"grep", "claude"}, "claude", false},
		{nil, "agy", false},
	} {
		if got := Named(c.args, c.name); got != c.want {
			t.Errorf("Named(%q, %q) = %v", c.args, c.name, got)
		}
	}
}
