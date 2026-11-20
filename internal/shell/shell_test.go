package shell

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestQuote(t *testing.T) {
	for in, want := range map[string]string{
		"":             "''",
		"claude":       "claude",
		"/a/b-c_d.e":   "/a/b-c_d.e",
		"--model=opus": "--model=opus",
		"a b":          "'a b'",
		"it's":         `'it'\''s'`,
		"$HOME":        "'$HOME'",
		"~/x":          "'~/x'",
		"!x":           "'!x'",
		"=ls":          "'=ls'", // zsh: =ls is /bin/ls
		"a=b":          "a=b",
	} {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %q, want %q", in, got, want)
		}
	}
}

// What Join writes, sh and zsh (a tab's shell) read back as the same words.
func TestJoinRoundTrip(t *testing.T) {
	words := []string{"codex", "resume", "it's", "a b", "$HOME", "`x`", "\"q\"", "back\\slash", "!bang", "tab\there", "new\nline", "*", "~", "~root", "=ls", "=sub", "#hash", "-dash", "semi;colon", "x=y", "%1", "^caret"}
	for _, sh := range [][]string{{"/bin/sh", "-c"}, {"/bin/zsh", "-f", "-c"}} {
		out, err := exec.Command(sh[0], append(sh[1:], `for w in `+Join(words)+`; do printf '%s\0' "$w"; done`)...).Output()
		if err != nil {
			t.Fatalf("%s: %v", sh[0], err)
		}
		got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
		if !slices.Equal(got, words) {
			t.Fatalf("%s read %q, want %q", sh[0], got, words)
		}
	}
}
