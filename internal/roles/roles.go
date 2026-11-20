// Package roles reads the sessions handoff gives roles, from its session
// files, .handoff/sessions/<session id>.json. It only reads. The format is
// handoff's own and can change, so a file whose "v" isn't 1 gives nothing.
package roles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type file struct {
	V       int    `json:"v"`
	Role    string `json:"role"`
	Session string `json:"session"`
	// Since, pid and host are read apart, so a file whose since or process
	// reopen can't read still gives its role.
	Since json.RawMessage `json:"since"`
	PID   json.RawMessage `json:"pid"`
	Host  json.RawMessage `json:"host"`
}

// Session is a session handoff gives a role.
type Session struct {
	ID   string
	Role string
	// Since is when handoff saw the session take the role, in its process.
	Since time.Time
	// PID is that process, as handoff noted it: Claude Code's own (pid), or
	// for Codex and Antigravity their hook's parent (host). 0 if neither.
	PID int
}

// Sessions are the sessions handoff gives roles in the project whose main
// worktree is root. A missing folder gives none.
func Sessions(root string) []Session {
	var out []Session
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
		s := Session{ID: id, Role: f.Role}
		json.Unmarshal(f.Since, &s.Since)
		if json.Unmarshal(f.PID, &s.PID) != nil || s.PID <= 0 {
			s.PID = 0
			if json.Unmarshal(f.Host, &s.PID) != nil || s.PID <= 0 {
				s.PID = 0
			}
		}
		out = append(out, s)
	}
	return out
}

// Read returns the role of each session handoff has one for in the project
// whose main worktree is root, by session id.
func Read(root string) map[string]string {
	out := map[string]string{}
	for _, s := range Sessions(root) {
		out[s.ID] = s.Role
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
