// Package roles reads the roles handoff gives sessions, from its session
// files, .handoff/sessions/<session id>.json. It only reads, and only for
// roles. The format is handoff's own and can change, so a file whose "v"
// isn't 1 gives no role.
package roles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type file struct {
	V       int    `json:"v"`
	Role    string `json:"role"`
	Session string `json:"session"`
}

// Read returns the role of each session handoff has one for in the project
// whose main worktree is root, by session id. A missing folder gives none.
func Read(root string) map[string]string {
	out := map[string]string{}
	if root == "" {
		return out
	}
	dir := filepath.Join(root, ".handoff", "sessions")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !e.Type().IsRegular() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || len(data) > 64<<10 {
			continue
		}
		var f file
		if json.Unmarshal(data, &f) != nil || f.V != 1 || f.Session != id || !plain(f.Role) {
			continue
		}
		out[id] = f.Role
	}
	return out
}

// Holder is the session holding role in m, or "".
func Holder(m map[string]string, role string) string {
	for id, r := range m {
		if r == role {
			return id
		}
	}
	return ""
}

// plain says whether a role is one reopen prints as it is: handoff's are
// short words, and one with a control character would reach the terminal.
func plain(role string) bool {
	if role == "" || len(role) > 64 {
		return false
	}
	for _, r := range role {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return false
		}
	}
	return true
}
