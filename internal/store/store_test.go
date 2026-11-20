package store

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUpdateWritesPrivately(t *testing.T) {
	home := t.TempDir()
	repo := "/work/my repo/.git"
	err := Update(home, repo, func(l *List) (bool, error) {
		l.Entries = append(l.Entries, Entry{CLI: "claude", ID: "a", Folder: "/work/my repo", LastSeen: time.Now()})
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	p := Path(home, repo)
	if !strings.HasPrefix(filepath.Base(p), "my_repo-") || filepath.Dir(p) != filepath.Join(home, ".reopen") {
		t.Fatalf("Path = %s", p)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("list mode %v, want 0600", st.Mode().Perm())
	}
	if d, _ := os.Stat(Dir(home)); d.Mode().Perm() != 0o700 {
		t.Fatalf("folder mode %v, want 0700", d.Mode().Perm())
	}
	if l, _ := os.Stat(p + ".lock"); l.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode %v, want 0600", l.Mode().Perm())
	}
	l, err := Load(home, repo)
	if err != nil || len(l.Entries) != 1 || l.Find("claude", "a") != 0 || l.Find("codex", "a") != -1 {
		t.Fatalf("Load = %+v, %v", l, err)
	}
	// Another repository with the same folder name has its own list.
	if Path(home, "/other/my repo/.git") == p {
		t.Fatal("two repositories share a list")
	}
}

func TestUpdateUnchangedWritesNothing(t *testing.T) {
	home := t.TempDir()
	if err := Update(home, "/r/.git", func(*List) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(home, "/r/.git")); !os.IsNotExist(err) {
		t.Fatal("an unchanged list was written")
	}
}

func TestLoadRefusesAnotherFormat(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(Dir(home), 0o700)
	os.WriteFile(Path(home, "/r/.git"), []byte(`{"v":2,"repo":"/r/.git"}`), 0o600)
	if _, err := Load(home, "/r/.git"); err == nil {
		t.Fatal("Load read format 2")
	}
}

// Hooks of sessions started together run at once; none loses another's entry.
func TestUpdateConcurrent(t *testing.T) {
	home := t.TempDir()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := Update(home, "/r/.git", func(l *List) (bool, error) {
				l.Entries = append(l.Entries, Entry{CLI: "codex", ID: string(rune('a' + i))})
				return true, nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	l, _ := Load(home, "/r/.git")
	if len(l.Entries) != 20 {
		t.Fatalf("%d entries, want 20", len(l.Entries))
	}
}

// With HOME unset or relative, reopen writes nowhere, not in the project.
func TestNoHomeNoWrite(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(wd)
	for _, home := range []string{"", "rel"} {
		if err := Update(home, "/r/.git", func(*List) (bool, error) { return true, nil }); err == nil {
			t.Fatalf("Update with home %q succeeded", home)
		}
		if err := Log(home, time.Now(), "x"); err == nil {
			t.Fatalf("Log with home %q succeeded", home)
		}
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("something was written in the working folder: %v", ents)
	}
}

func TestResetAnUnreadableList(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(Dir(home), 0o700)
	os.WriteFile(Path(home, "/r/.git"), []byte(`{"v":1,"repo":"/r/.git","entries":[`), 0o600)
	if _, err := Load(home, "/r/.git"); err == nil || !strings.Contains(err.Error(), "reopen forget starts it over") {
		t.Fatalf("Load = %v", err)
	}
	if err := Reset(home, "/r/.git"); err != nil {
		t.Fatal(err)
	}
	if l, err := Load(home, "/r/.git"); err != nil || len(l.Entries) != 0 {
		t.Fatalf("after Reset: %+v %v", l, err)
	}
}
