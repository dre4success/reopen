package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// Homes are the folders where the CLIs keep their data.
type Homes struct {
	Claude      string // CLAUDE_CONFIG_DIR, else ~/.claude
	Codex       string // CODEX_HOME, else ~/.codex
	Antigravity string // ~/.gemini/antigravity-cli
}

// HomesFor finds the CLIs' folders for home, with getenv for the variables
// that move them.
func HomesFor(home string, getenv func(string) string) Homes {
	h := Homes{
		Claude:      filepath.Join(home, ".claude"),
		Codex:       filepath.Join(home, ".codex"),
		Antigravity: filepath.Join(home, ".gemini", "antigravity-cli"),
	}
	if d := getenv("CLAUDE_CONFIG_DIR"); d != "" {
		h.Claude = d
	}
	if d := getenv("CODEX_HOME"); d != "" {
		h.Codex = d
	}
	return h
}

// plainID is a session id that is safe in a file name and a glob: the CLIs'
// ids are UUIDs.
var plainID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// PlainID says whether id is safe to look a transcript up by.
func PlainID(id string) bool { return plainID.MatchString(id) }

// Transcript is the file the CLI keeps session id's conversation in, or ""
// if it has none.
//   - Claude Code: <claude>/projects/<first folder>/<id>.jsonl, found by
//     the id, since the folder's name is the first folder's path mangled.
//   - Codex: <codex>/sessions/YYYY/MM/DD/rollout-<time>-<id>.jsonl.
//   - Antigravity: <antigravity>/brain/<id>/.system_generated/logs/transcript.jsonl,
//     else its database, <antigravity>/conversations/<id>.db.
//
// Of more than one, the latest written.
func (c *CLI) Transcript(h Homes, id string) string {
	if !PlainID(id) {
		return ""
	}
	var pattern string
	switch c.Name {
	case Claude:
		pattern = filepath.Join(h.Claude, "projects", "*", id+".jsonl")
	case Codex:
		pattern = filepath.Join(h.Codex, "sessions", "*", "*", "*", "rollout-*-"+id+".jsonl")
	default:
		// Since 1.5.13 a conversation has a transcript beside its database,
		// written at every step; the database only when the session closes.
		pattern = filepath.Join(h.Antigravity, "brain", id, ".system_generated", "logs", "transcript.jsonl")
		if found, _ := filepath.Glob(pattern); len(found) == 0 {
			pattern = filepath.Join(h.Antigravity, "conversations", id+".db")
		}
	}
	found, _ := filepath.Glob(pattern)
	latest, at := "", time.Time{}
	for _, p := range found {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.ModTime().After(at) {
			latest, at = p, st.ModTime()
		}
	}
	return latest
}

// LastUse is when the session was last used: its transcript's time. A
// database's latest writes can be in its write-ahead log, beside it.
func LastUse(path string) time.Time {
	var t time.Time
	for _, p := range []string{path, path + "-wal"} {
		if st, err := os.Stat(p); err == nil && st.ModTime().After(t) {
			t = st.ModTime()
		}
	}
	return t
}

// Size is a transcript's size: what a resume re-reads, at full price after
// days away. A database's write-ahead log counts too.
func Size(path string) int64 {
	var n int64
	for _, p := range []string{path, path + "-wal"} {
		if st, err := os.Stat(p); err == nil {
			n += st.Size()
		}
	}
	return n
}

// head is the start of a transcript, which holds the first message. A line
// cut off at its end doesn't parse and is skipped.
func head(path string, n int64) []byte {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return nil
	}
	buf, _ := io.ReadAll(io.LimitReader(f, n))
	return buf
}

// Label names a session in a line, or "": for Claude Code, the name the
// operator gave it (/rename), else the title Claude Code gave it, which its
// own /resume picker shows; else, for every CLI, the first thing the
// operator typed to it. A first message is often "resume" or "what's next?",
// and a title tells sessions apart better. Antigravity's database isn't
// read: only its transcript.
func (c *CLI) Label(path string) string {
	if t := c.Title(path); t != "" {
		return t
	}
	return c.First(path)
}

// Title is the name of a Claude Code session: the latest custom-title line
// (the operator's), else the latest ai-title line (Claude Code's), or "".
// They are written again and again, so the end of the transcript has them;
// failing that, its start.
func (c *CLI) Title(path string) string {
	if c.Name != Claude || path == "" {
		return ""
	}
	for _, buf := range [][]byte{tail(path, 256<<10), head(path, 1<<20)} {
		custom, ai := "", ""
		for line := range bytes.SplitSeq(buf, []byte("\n")) {
			if !bytes.Contains(line, []byte(`-title"`)) {
				continue
			}
			var e struct {
				Type   string `json:"type"`
				Custom string `json:"customTitle"`
				AI     string `json:"aiTitle"`
			}
			if json.Unmarshal(line, &e) != nil {
				continue
			}
			switch {
			case e.Type == "custom-title" && strings.TrimSpace(e.Custom) != "":
				custom = e.Custom
			case e.Type == "ai-title" && strings.TrimSpace(e.AI) != "":
				ai = e.AI
			}
		}
		if custom != "" {
			return shorten(custom, 72)
		}
		if ai != "" {
			return shorten(ai, 72)
		}
	}
	return ""
}

// First is the first thing the operator typed to the session, shortened
// to a line, or "".
func (c *CLI) First(path string) string {
	if path == "" || strings.HasSuffix(path, ".db") {
		return ""
	}
	for line := range bytes.SplitSeq(head(path, 1<<20), []byte("\n")) {
		var text string
		switch c.Name {
		case Claude:
			text = claudeUserText(line)
		case Codex:
			text = codexUserText(line)
		default:
			text = agyUserText(line)
		}
		if text != "" {
			return shorten(text, 72)
		}
	}
	return ""
}

// claudeUserText is the operator's text in a Claude Code transcript line,
// or "" for any other line: an assistant's, a tool's result, a note the CLI
// adds (isMeta), the summary a compaction writes (isCompactSummary), or a
// slash command's record, which starts with a tag such as <command-name>
// or <local-command-stdout>.
func claudeUserText(line []byte) string {
	if !bytes.Contains(line, []byte(`"user"`)) {
		return ""
	}
	var e struct {
		Type    string `json:"type"`
		IsMeta  bool   `json:"isMeta"`
		Compact bool   `json:"isCompactSummary"`
		Hidden  bool   `json:"isVisibleInTranscriptOnly"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &e) != nil || e.Type != "user" || e.IsMeta || e.Compact || e.Hidden || e.Message.Role != "user" {
		return ""
	}
	var s string
	if json.Unmarshal(e.Message.Content, &s) != nil {
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(e.Message.Content, &parts) != nil {
			return ""
		}
		for _, p := range parts {
			if p.Type == "tool_result" {
				return ""
			}
			if p.Type == "text" && s == "" {
				s = p.Text
			}
		}
	}
	if s = strings.TrimSpace(s); strings.HasPrefix(s, "<") {
		return ""
	}
	return s
}

// codexUserText is the operator's text in a Codex rollout line, or "". The
// user's messages are response_item lines with role user; Codex puts the
// project's AGENTS.md and an <environment_context> in one of those before
// the first turn.
func codexUserText(line []byte) string {
	if !bytes.Contains(line, []byte(`"response_item"`)) {
		return ""
	}
	var e struct {
		Type    string `json:"type"`
		Payload struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &e) != nil || e.Type != "response_item" || e.Payload.Type != "message" || e.Payload.Role != "user" {
		return ""
	}
	for _, p := range e.Payload.Content {
		t := strings.TrimSpace(p.Text)
		if p.Type != "input_text" || t == "" || strings.HasPrefix(t, "<") || strings.HasPrefix(t, "# AGENTS.md instructions") {
			continue
		}
		return t
	}
	return ""
}

// agyUserText is the operator's text in an Antigravity transcript line: a
// USER_INPUT step the operator typed (source USER_EXPLICIT), whose content
// wraps it in <USER_REQUEST>, with other context after.
func agyUserText(line []byte) string {
	if !bytes.Contains(line, []byte(`"USER_INPUT"`)) {
		return ""
	}
	var e struct {
		Type    string `json:"type"`
		Source  string `json:"source"`
		Content string `json:"content"`
	}
	if json.Unmarshal(line, &e) != nil || e.Type != "USER_INPUT" || e.Source != "USER_EXPLICIT" {
		return ""
	}
	t := e.Content
	if i := strings.Index(t, "<USER_REQUEST>"); i >= 0 {
		t = t[i+len("<USER_REQUEST>"):]
		if j := strings.Index(t, "</USER_REQUEST>"); j >= 0 {
			t = t[:j]
		}
	}
	if t = strings.TrimSpace(t); strings.HasPrefix(t, "<") {
		return ""
	}
	return t
}

// shorten makes s one clean line of at most n characters.
func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(Clean(s)), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// ContinuedIn is the session a Claude Code transcript says its conversation
// went on in, after /background: its last "continued-in" line names it, no
// message follows that line (a few bookkeeping lines do), and the line is
// newer than since, the entry's last report. Otherwise "". A session resumed after /background keeps its
// id and its transcript goes on after the line, so a line followed by a
// message, or older than the resume's report, is stale.
func ContinuedIn(path string, since time.Time) string {
	next := ""
	for line := range bytes.SplitSeq(tail(path, 256<<10), []byte("\n")) {
		var e struct {
			Type string    `json:"type"`
			Next string    `json:"continuedInSessionId"`
			At   time.Time `json:"timestamp"`
		}
		if len(bytes.TrimSpace(line)) == 0 || json.Unmarshal(line, &e) != nil {
			continue
		}
		switch e.Type {
		case "continued-in":
			next = ""
			if e.At.After(since) && PlainID(e.Next) {
				next = e.Next
			}
		case "user", "assistant":
			next = ""
		}
	}
	return next
}

// Mode is the permission mode a Claude Code session comes back in, as its
// transcript last notes it in a permission-mode line, or "". claude
// --resume keeps it, so reopen shows it rather than passing it back. The
// lines are at the transcript's start and, after a change, further on.
func (c *CLI) Mode(path string) string {
	if c.Name != Claude || path == "" {
		return ""
	}
	mode := ""
	scan := func(buf []byte) {
		for line := range bytes.SplitSeq(buf, []byte("\n")) {
			if !bytes.Contains(line, []byte(`"permission-mode"`)) {
				continue
			}
			var e struct {
				Type string `json:"type"`
				Mode string `json:"permissionMode"`
			}
			if json.Unmarshal(line, &e) == nil && e.Type == "permission-mode" && e.Mode != "" {
				mode = e.Mode
			}
		}
	}
	// The latest line is the mode now: in the last 256 KiB, else the last
	// 16 MiB (lines reach megabytes), else the start, where the first is.
	for _, n := range []int64{256 << 10, 16 << 20} {
		buf := tail(path, n)
		if scan(buf); mode != "" || int64(len(buf)) < n {
			return Clean(mode)
		}
	}
	scan(head(path, 256<<10))
	return Clean(mode)
}

// tail is the end of a file, n bytes at most.
func tail(path string, n int64) []byte {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil
	}
	off := max(st.Size()-n, 0)
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
		return nil
	}
	return buf
}

// ModeSkips says whether a Claude Code permission mode skips permission
// checks.
func ModeSkips(mode string) bool { return mode == "bypassPermissions" }

// escape matches a terminal's escape sequences: CSI (colours, cursor
// moves), OSC (titles, links) and the two-byte ones.
var escape = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(\x07|\x1b\\\\)?|\x1b[@-Z\\\\-_]")

// Clean makes text from a transcript safe to print: a transcript is written
// by whatever the session read, so its text could move the cursor, retitle
// the terminal or hide words. Escape sequences go, then every other control
// character but a space, and the characters that reorder text (bidi).
func Clean(s string) string {
	s = escape.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n':
			return ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x200e, r == 0x200f:
			return -1
		}
		return r
	}, s)
}

// HasControl says whether s holds a control character, or one that is
// invisible or reorders text. A tab's line is typed into a shell, where a
// newline would end the command early and a tab would ask for completion,
// so a folder or flag with one isn't typed; and the operator reads the
// exact command before it is, so one with an invisible or reordering
// character isn't either.
func HasControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		switch {
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return true
		case r >= 0x200b && r <= 0x200f, r >= 0x2028 && r <= 0x202e, r >= 0x2060 && r <= 0x2069, r == 0xfeff:
			return true // zero-width, line and paragraph separators, bidi
		}
		return false
	}) >= 0
}

// Meta is what a Codex rollout's first line, session_meta, says of who
// started the session: originator is "codex-tui" for the terminal's TUI
// and "Codex Desktop" for the desktop app; a subagent's thread, such as a
// guardian review, has a source of its own. ok is false if it can't be read.
func Meta(path string) (originator string, subagent, ok bool) {
	first, _, _ := bytes.Cut(head(path, 1<<20), []byte("\n"))
	var e struct {
		Type    string `json:"type"`
		Payload struct {
			Originator   string          `json:"originator"`
			Source       json.RawMessage `json:"source"`
			ThreadSource string          `json:"thread_source"`
		} `json:"payload"`
	}
	if json.Unmarshal(first, &e) != nil || e.Type != "session_meta" {
		return "", false, false
	}
	sub := len(e.Payload.Source) > 0 && e.Payload.Source[0] == '{' ||
		e.Payload.ThreadSource != "" && e.Payload.ThreadSource != "user"
	return e.Payload.Originator, sub, true
}

// CodexSettings are how a Codex session comes back: codex resume keeps
// them itself, as the session last had them.
type CodexSettings struct {
	Known    bool     // false: the rollout says nothing reopen can read
	Model    string   // gpt-6.2-sol
	Approval string   // on-request, never, …
	Sandbox  string   // read-only, workspace-write, danger-full-access
	Roots    []string // folders added to the session's own, writable
}

// Settings reads a Codex rollout's latest settings. Codex notes them in two
// kinds of line: thread_settings_applied, when a start, a resume or a change
// in the session applies them, and turn_context, at each turn. A resume with
// other flags applies new settings with no turn after, and a bare resume
// then comes back with those (evidence/r1/codex-settings.log), so the latest
// line of either kind counts. Added folders are only in
// thread_settings_applied, as runtime_workspace_roots beside the session's
// own folder. Rollouts reach tens of MB, so only the tail is read: 1 MiB,
// then 16 MiB.
func Settings(path string) CodexSettings {
	var out CodexSettings
	haveRoots := false
	for _, n := range []int64{1 << 20, 16 << 20} {
		buf := tail(path, n)
		for end := len(buf); end > 0 && !(out.Known && haveRoots); {
			i := lastOf(buf[:end], `"turn_context"`, `"thread_settings_applied"`)
			if i < 0 {
				break
			}
			start := bytes.LastIndexByte(buf[:i], '\n') + 1
			line := buf[start:]
			if j := bytes.IndexByte(line, '\n'); j >= 0 {
				line = line[:j]
			}
			end = start
			var e struct {
				Type    string `json:"type"`
				Payload struct {
					Type     string `json:"type"`
					Model    string `json:"model"`
					Approval string `json:"approval_policy"`
					Sandbox  struct {
						Type string `json:"type"`
					} `json:"sandbox_policy"`
					Thread *struct {
						Model    string          `json:"model"`
						Approval string          `json:"approval_policy"`
						Profile  json.RawMessage `json:"permission_profile"`
						Cwd      string          `json:"cwd"`
						Roots    []string        `json:"runtime_workspace_roots"`
					} `json:"thread_settings"`
				} `json:"payload"`
			}
			if json.Unmarshal(line, &e) != nil {
				continue
			}
			switch {
			case e.Type == "turn_context" && !out.Known:
				out = CodexSettings{Known: true, Model: Clean(e.Payload.Model), Approval: Clean(e.Payload.Approval), Sandbox: Clean(e.Payload.Sandbox.Type), Roots: out.Roots}
			case e.Type == "event_msg" && e.Payload.Type == "thread_settings_applied" && e.Payload.Thread != nil:
				t := e.Payload.Thread
				if !out.Known {
					out = CodexSettings{Known: true, Model: Clean(t.Model), Approval: Clean(t.Approval), Sandbox: profileSandbox(t.Profile)}
				}
				if !haveRoots {
					haveRoots = true
					for _, r := range t.Roots {
						if filepath.Clean(r) != filepath.Clean(t.Cwd) {
							out.Roots = append(out.Roots, Clean(r))
						}
					}
				}
			}
		}
		if out.Known && haveRoots || int64(len(buf)) < n {
			break // found, or the whole file was read
		}
	}
	return out
}

// lastOf is the last index in b of any of the markers, or -1.
func lastOf(b []byte, markers ...string) int {
	last := -1
	for _, m := range markers {
		last = max(last, bytes.LastIndex(b, []byte(m)))
	}
	return last
}

// profileSandbox names the sandbox a thread_settings_applied permission
// profile gives, as turn_context names it: "disabled" is no sandbox; a
// managed profile that may write somewhere is workspace-write, one that only
// reads is read-only.
func profileSandbox(raw json.RawMessage) string {
	var p struct {
		Type       string `json:"type"`
		FileSystem struct {
			Entries []struct {
				Access string `json:"access"`
			} `json:"entries"`
		} `json:"file_system"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return ""
	}
	switch p.Type {
	case "disabled":
		return "danger-full-access"
	case "managed":
		for _, e := range p.FileSystem.Entries {
			if e.Access == "write" {
				return "workspace-write"
			}
		}
		return "read-only"
	}
	return Clean(p.Type)
}

// SettingsSkip says whether Codex settings skip its checks: no approval
// asked and no sandbox.
func SettingsSkip(approval, sandbox string) bool {
	return approval == "never" && sandbox == "danger-full-access"
}
