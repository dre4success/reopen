package roles

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRead(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".handoff", "sessions")
	os.MkdirAll(dir, 0o700)
	files := map[string]string{
		"a.json": `{"v":1,"role":"implementer","session":"a","pid":1}`,
		"b.json": `{"v":2,"role":"reviewer","session":"b"}`,     // a format reopen doesn't know
		"c.json": `{"v":1,"role":"reviewer","session":"other"}`, // named for another session
		"d.json": `not json`,
		"e.txt":  `{"v":1,"role":"x","session":"e"}`,
		"f.json": `{"v":1,"role":"","session":"f"}`,
	}
	for name, data := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600)
	}
	got := Read(root)
	if len(got) != 1 || got["a"] != "implementer" {
		t.Fatalf("Read = %v, want only a: implementer", got)
	}
	if Holder(got, "implementer") != "a" || Holder(got, "reviewer") != "" {
		t.Fatal("Holder")
	}
	if len(Read(filepath.Join(root, "none"))) != 0 || len(Read("")) != 0 {
		t.Fatal("a missing folder has roles")
	}
}

// Each session's process and time, as handoff notes them: pid for Claude
// Code's, host for the others'. A file whose time or process reopen can't
// read still gives its role.
func TestSessions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".handoff", "sessions")
	os.MkdirAll(dir, 0o700)
	files := map[string]string{
		"a.json": `{"v":1,"role":"implementer","session":"a","pid":41,"since":"2026-11-19T22:33:44.603658Z"}`,
		"b.json": `{"v":1,"role":"reviewer","session":"b","host":42,"since":"2026-11-19T22:34:15Z"}`,
		"c.json": `{"v":1,"role":"reviewer-2","session":"c","pid":"x","since":"yesterday"}`,
		"d.json": `{"v":1,"role":"reviewer-3","session":"d","host":-3}`,
	}
	for name, data := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600)
	}
	got := map[string]Session{}
	for _, s := range Sessions(root) {
		got[s.ID] = s
	}
	want := map[string]Session{
		"a": {ID: "a", Role: "implementer", PID: 41, Since: time.Date(2026, 11, 19, 22, 33, 44, 603658000, time.UTC)},
		"b": {ID: "b", Role: "reviewer", PID: 42, Since: time.Date(2026, 11, 19, 22, 34, 15, 0, time.UTC)},
		"c": {ID: "c", Role: "reviewer-2"},
		"d": {ID: "d", Role: "reviewer-3"},
	}
	if len(got) != len(want) {
		t.Fatalf("Sessions = %v", got)
	}
	for id, w := range want {
		g := got[id]
		if g.ID != w.ID || g.Role != w.Role || g.PID != w.PID || !g.Since.Equal(w.Since) {
			t.Errorf("%s: got %+v, want %+v", id, g, w)
		}
	}
}
