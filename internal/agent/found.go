package agent

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
)

// Find is the CLI that has session id, and its transcript: the first of
// the CLIs whose records hold it. The CLIs' ids are UUIDs, so only one does.
func Find(h Homes, id string) (*CLI, string) {
	for _, name := range Names() {
		c, _ := Lookup(name)
		if t := c.Transcript(h, id); t != "" {
			return c, t
		}
	}
	return nil, ""
}

// Folder is the folder a session reopen found by its id, not by its start
// hook, comes back in, from its CLI's own records, or "":
//   - Claude Code: the first folder its transcript notes, where it began.
//     Later lines note where a command ran, after a cd too.
//   - Codex: the folder its rollout's latest settings name, else its first
//     line's, session_meta.
//   - Antigravity: the first workspace its conversation index lists,
//     <antigravity>/cache/conversation_metadata.json.
func (c *CLI) Folder(h Homes, id, transcript string) string {
	var f string
	switch c.Name {
	case Claude:
		f = claudeFolder(transcript)
	case Codex:
		if f = Settings(transcript).Folder; f == "" {
			f = codexMetaFolder(transcript)
		}
	default:
		f = agyFolder(h, id)
	}
	if !filepath.IsAbs(f) {
		return ""
	}
	return filepath.Clean(f)
}

func claudeFolder(transcript string) string {
	for line := range bytes.SplitSeq(head(transcript, 1<<20), []byte("\n")) {
		if !bytes.Contains(line, []byte(`"cwd"`)) {
			continue
		}
		var e struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(line, &e) == nil && e.Cwd != "" {
			return e.Cwd
		}
	}
	return ""
}

func codexMetaFolder(rollout string) string {
	first, _, _ := bytes.Cut(head(rollout, 1<<20), []byte("\n"))
	var e struct {
		Type    string `json:"type"`
		Payload struct {
			Cwd string `json:"cwd"`
		} `json:"payload"`
	}
	if json.Unmarshal(first, &e) != nil || e.Type != "session_meta" {
		return ""
	}
	return e.Payload.Cwd
}

// agyFolder reads Antigravity's conversation index: under one key per
// account, each conversation by its id, with a summary that lists its
// workspaces as file URLs.
func agyFolder(h Homes, id string) string {
	p := filepath.Join(h.Antigravity, "cache", "conversation_metadata.json")
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 64<<20 {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var index map[string]map[string]struct {
		Summary struct {
			WorkspaceURIs []string `json:"WorkspaceURIs"`
		} `json:"summary"`
	}
	if json.Unmarshal(data, &index) != nil {
		return ""
	}
	for _, convs := range index {
		conv, ok := convs[id]
		if !ok {
			continue
		}
		for _, w := range conv.Summary.WorkspaceURIs {
			if u, err := url.Parse(w); err == nil && u.Scheme == "file" && (u.Host == "" || u.Host == "localhost") {
				return u.Path
			}
		}
	}
	return ""
}
