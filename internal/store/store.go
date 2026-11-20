// Package store keeps reopen's list: one file per project under ~/.reopen/,
// named for the full path of the project's common git folder, so every
// worktree of a repository shares one list and two repositories never do.
// The files are the operator's alone (mode 0600, in a folder of mode 0700),
// since the recorded arguments can hold what the operator typed.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Version is the list file's format.
const Version = 1

// Entry is one session, as its CLI's start hook last reported it.
type Entry struct {
	CLI    string `json:"cli"`    // claude, codex or antigravity
	ID     string `json:"id"`     // the session's id
	Folder string `json:"folder"` // the folder its latest run started in
	// PID is the process its latest run reported from: Claude Code's own
	// (CLAUDE_PID), or the hook's parent for the other two.
	PID int `json:"pid,omitempty"`
	// Args are that process's arguments, Args[0] first. Only the start flags
	// that matter are ever passed back; the rest, a first prompt among them,
	// stays here.
	Args []string `json:"args,omitempty"`
	// LastSeen is when the hook last ran for this session. It is not the
	// session's last use, which is its transcript's time.
	LastSeen time.Time `json:"last_seen"`
	// Role is the handoff role reopen last saw the session hold, kept so it
	// can say so once handoff has moved the role or dropped its file.
	Role string `json:"role,omitempty"`
}

// List is a project's list.
type List struct {
	V       int     `json:"v"`
	Repo    string  `json:"repo"` // the common git folder's full path
	Entries []Entry `json:"entries"`
}

// Find returns the index of the entry for cli's session id, or -1.
func (l *List) Find(cli, id string) int {
	for i, e := range l.Entries {
		if e.CLI == cli && e.ID == id {
			return i
		}
	}
	return -1
}

// Dir is reopen's folder in home.
func Dir(home string) string { return filepath.Join(home, ".reopen") }

// Path is the list file for the repository whose common git folder is
// repo. Its name starts with the project's folder name, to be readable, and
// ends with a hash of the full path, to be its own.
func Path(home, repo string) string {
	sum := sha256.Sum256([]byte(repo))
	name := filepath.Base(repo)
	if name == ".git" {
		name = filepath.Base(filepath.Dir(repo))
	}
	return filepath.Join(Dir(home), fmt.Sprintf("%s-%s.json", safeName(name), hex.EncodeToString(sum[:])[:12]))
}

func safeName(s string) string {
	b := []byte(s)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			b[i] = '_'
		}
	}
	if len(b) > 40 {
		b = b[:40]
	}
	if len(b) == 0 || b[0] == '.' {
		b = append([]byte("repo"), b...)
	}
	return string(b)
}

// Load reads repo's list; a list never written is empty.
func Load(home, repo string) (*List, error) {
	data, err := os.ReadFile(Path(home, repo))
	if errors.Is(err, fs.ErrNotExist) {
		return &List{V: Version, Repo: repo}, nil
	}
	if err != nil {
		return nil, err
	}
	var l List
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("%s can't be read (%w); reopen forget starts it over", Path(home, repo), err)
	}
	if l.V != Version {
		return nil, fmt.Errorf("%s has format %d, which this reopen doesn't read", Path(home, repo), l.V)
	}
	if l.Repo != repo {
		return nil, fmt.Errorf("%s is the list of %s, not %s", Path(home, repo), l.Repo, repo)
	}
	return &l, nil
}

// Update changes repo's list under its lock: fn changes the list it is
// given and says whether it changed it, and a change is written whole, in
// a new file put in the old one's place, so a reader never sees half of
// one. Hooks of sessions started together run at once, hence the lock. It
// waits for the lock five seconds at most.
func Update(home, repo string, fn func(*List) (bool, error)) error {
	if err := ready(home); err != nil {
		return err
	}
	unlock, err := lock(Path(home, repo) + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	l, err := Load(home, repo)
	if err != nil {
		return err
	}
	changed, err := fn(l)
	if err != nil || !changed {
		return err
	}
	return write(Path(home, repo), l)
}

// ready makes reopen's folder, the operator's alone. A test that forgot its
// temporary home fails here rather than write the real ~/.reopen/.
func ready(home string) error {
	// With HOME unset or relative, Dir would be a folder in the project,
	// one git add from a commit: reopen writes nowhere then.
	if !filepath.IsAbs(home) {
		return fmt.Errorf("no home folder (HOME is %q), so reopen has nowhere to keep its list", home)
	}
	if testing.Testing() {
		if real, err := os.UserHomeDir(); err == nil && filepath.Clean(real) == filepath.Clean(home) {
			return errors.New("a test may not write the real ~/.reopen/")
		}
	}
	if err := os.MkdirAll(Dir(home), 0o700); err != nil {
		return err
	}
	return os.Chmod(Dir(home), 0o700)
}

// LogPath is where the hook notes what kept it from recording a session:
// it prints nothing, so as not to get in the session's way.
func LogPath(home string) string { return filepath.Join(Dir(home), "hook.log") }

// Log notes a line in the hook's log. The log is kept small: once past
// 64 KiB, it starts again.
func Log(home string, now time.Time, msg string) error {
	if err := ready(home); err != nil {
		return err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_APPEND
	if st, err := os.Stat(LogPath(home)); err == nil && st.Size() > 64<<10 {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(LogPath(home), flags, 0o600)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s %s\n", now.UTC().Format(time.RFC3339), msg)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func write(path string, l *List) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	// CreateTemp makes the file with mode 0600.
	f, err := os.CreateTemp(filepath.Dir(path), ".list-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // a no-op once renamed
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Reset starts repo's list over, empty, under its lock, whatever the old
// file held, an unreadable one too.
func Reset(home, repo string) error {
	if err := ready(home); err != nil {
		return err
	}
	unlock, err := lock(Path(home, repo) + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	return write(Path(home, repo), &List{V: Version, Repo: repo})
}
