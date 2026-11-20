package roles

import (
	"os"
	"path/filepath"
	"testing"
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
