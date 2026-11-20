// Package agent is what reopen knows of each agent CLI: which runs are
// sessions to bring back, which start flags come back, the command that
// resumes a session, and where its transcript is. It reads transcripts and
// never writes them.
package agent

import (
	"slices"
	"strings"
)

// The CLIs, as reopen names them.
const (
	Claude      = "claude"
	Codex       = "codex"
	Antigravity = "antigravity"
)

// CLI is one agent CLI.
type CLI struct {
	Name    string // as reopen names it
	Command string // the program a shell runs
	Short   string // the name in a list
	parser  parser
	// batch says whether parsed arguments start a run that isn't
	// interactive, which isn't brought back.
	batch func(parsed) bool
}

// Lookup returns the CLI reopen names name.
func Lookup(name string) (*CLI, bool) {
	for _, c := range clis {
		if c.Name == name {
			return c, true
		}
	}
	return nil, false
}

// Names are the CLIs, as reopen names them.
func Names() []string { return []string{Claude, Codex, Antigravity} }

// Interactive says whether a process started with args is a session the
// operator typed to in a terminal, not a print-mode run (claude -p, codex
// exec, agy -p) or a subcommand's server.
func (c *CLI) Interactive(args []string) bool {
	return !c.batch(c.parser.parse(args, ""))
}

// Flags are the start flags in args that come back when the session is
// resumed: permission mode, sandbox, added folders, model. Never a prompt.
// folder is the folder the process started in.
func (c *CLI) Flags(args []string, folder string) []Flag {
	return c.parser.parse(args, folder).kept
}

// Resume is the command that resumes session id with flags. message, if
// any, is typed as the first turn; Claude Code takes none (see Message).
func (c *CLI) Resume(id string, flags []Flag, message string) []string {
	var fw []string
	for _, f := range flags {
		fw = append(fw, f.Words...)
	}
	switch c.Name {
	case Claude:
		return append([]string{c.Command, "--resume", id}, fw...)
	case Codex:
		// codex resume [OPTIONS] [SESSION_ID] [PROMPT]
		out := append(append([]string{c.Command, "resume"}, fw...), id)
		if message != "" {
			out = append(out, message)
		}
		return out
	default:
		// agy reads its flags with Go's flag package: every flag before any
		// other word.
		out := append([]string{c.Command, "--conversation", id}, fw...)
		if message != "" {
			out = append(out, "--prompt-interactive", message)
		}
		return out
	}
}

// ResumedID is the session a process's arguments resume, or "": what
// reopen types, and what the operator types to resume one by hand.
//   - claude --resume <id>, -r <id>, --resume=<id>, --session-id <id>;
//   - codex resume <id>;
//   - agy --conversation <id>, --conversation=<id>.
//
// A run that isn't interactive resumes nothing, and an id that is only
// mentioned, as in a prompt, isn't the one resumed.
func (c *CLI) ResumedID(args []string) string {
	if len(args) == 0 || !c.Interactive(args) {
		return ""
	}
	if c.Name == Codex {
		if w := c.parser.parse(args, "").words; len(w) >= 2 && w[0] == "resume" {
			return w[1]
		}
		return ""
	}
	names := []string{"--resume", "-r", "--session-id"}
	if c.Name == Antigravity {
		names = goNames("conversation")
	}
	for i := 1; i < len(args); i++ {
		w := args[i]
		if w == "--" {
			break
		}
		for _, n := range names {
			if v, ok := strings.CutPrefix(w, n+"="); ok {
				return v
			}
			if w == n && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				return args[i+1]
			}
		}
	}
	return ""
}

// Daemon says whether args start Codex's app-server, which runs sessions
// for its clients, and managed whether it is the daemon the Codex CLI
// starts for its TUI (rather than the desktop app's own).
func Daemon(args []string) (daemon, managed bool) {
	daemon = slices.Contains(args, "app-server")
	return daemon, daemon && slices.Contains(args, "--managed-daemon")
}

// Listens says whether a resumed session of this CLI listens for its
// handoff work with nothing typed to it. Claude Code's does: handoff's
// SessionStart hook starts its listener. Codex's and Antigravity's run
// their hooks only once a turn starts, so one with a role is sent a first
// message.
func (c *CLI) Listens() bool { return c.Name == Claude }

// Message is the first message sent to a resumed session that has a role
// and doesn't listen until a turn.
const Message = "You were reopened. Read your handoff inbox."

// SkipsChecks says whether any of flags skips permission checks.
func SkipsChecks(flags []Flag) bool {
	return slices.ContainsFunc(flags, func(f Flag) bool { return f.Skips })
}

// Words are flags as the words of a command line.
func Words(flags []Flag) []string {
	var out []string
	for _, f := range flags {
		out = append(out, f.Words...)
	}
	return out
}

func long(names ...string) []string { return names }

// goNames are a flag's names for Go's flag package, which takes -name and
// --name alike.
func goNames(name string) []string { return []string{"--" + name, "-" + name} }

var clis = []*CLI{
	{
		Name: Claude, Command: "claude", Short: "claude",
		parser: parser{specs: []spec{
			// claude --resume keeps the session's model and permission mode
			// itself, as the session last had them, after a /model or a
			// change of mode too; passing the start ones back would undo
			// those. It doesn't keep added folders (evidence/r1/claude-probe.log).
			{names: long("--model"), kind: value},
			{names: long("--permission-mode"), kind: value},
			{names: long("--dangerously-skip-permissions"), kind: boolean, keep: true, skips: always},
			{names: long("--allow-dangerously-skip-permissions"), kind: boolean, keep: true},
			{names: long("--add-dir"), kind: variadic, keep: true, dir: true},
			{names: long("--print", "-p"), kind: boolean},
			{names: long("--resume", "-r"), kind: optional},
			{names: long("--continue", "-c"), kind: boolean},
			{names: long("--debug", "-d"), kind: optional},
			{names: long("--cloud"), kind: optional},
			{names: long("--session-id"), kind: value},
			{names: long("--name", "-n"), kind: value},
			{names: long("--settings"), kind: value},
			{names: long("--setting-sources"), kind: value},
			{names: long("--agent"), kind: value},
			{names: long("--agents"), kind: value},
			{names: long("--effort"), kind: value},
			{names: long("--fallback-model"), kind: value},
			{names: long("--system-prompt"), kind: value},
			{names: long("--system-prompt-file"), kind: value},
			{names: long("--append-system-prompt"), kind: value},
			{names: long("--append-system-prompt-file"), kind: value},
			{names: long("--output-format"), kind: value},
			{names: long("--input-format"), kind: value},
			{names: long("--json-schema"), kind: value},
			{names: long("--max-budget-usd"), kind: value},
			{names: long("--debug-file"), kind: value},
			{names: long("--mcp-config"), kind: variadic},
			{names: long("--allowed-tools", "--allowedTools"), kind: variadic},
			{names: long("--disallowed-tools", "--disallowedTools"), kind: variadic},
			{names: long("--tools"), kind: variadic},
			{names: long("--betas"), kind: variadic},
			{names: long("--plugin-dir"), kind: value},
		}},
		batch: func(p parsed) bool {
			// -p runs once and exits; the stream formats work only with it.
			return p.has["--print"] || p.has["--input-format"] || p.has["--output-format"]
		},
	},
	{
		Name: Codex, Command: "codex", Short: "codex",
		// codex resume keeps the session's model, sandbox, approval policy
		// and added folders as the session last had them, so none is passed
		// back: that would undo a change made in the session
		// (evidence/r1/codex-probe.log A). The flags are listed for their
		// values only. Its hooks' parent is mostly Codex's shared daemon,
		// whose arguments aren't the session's anyway.
		parser: parser{specs: []spec{
			{names: long("--model", "-m"), kind: value},
			{names: long("--sandbox", "-s"), kind: value},
			{names: long("--ask-for-approval", "-a"), kind: value},
			{names: long("--add-dir"), kind: value},
			{names: long("--profile", "-p"), kind: value},
			{names: long("--local-provider"), kind: value},
			{names: long("--config", "-c"), kind: value},
			{names: long("--enable"), kind: value},
			{names: long("--disable"), kind: value},
			{names: long("--remote"), kind: value},
			{names: long("--remote-auth-token-env"), kind: value},
			{names: long("--image", "-i"), kind: variadic},
			{names: long("--cd", "-C"), kind: value},
		}},
		batch: func(p parsed) bool {
			// codex [OPTIONS] [PROMPT] and codex resume/fork are the
			// interactive TUI; any other subcommand isn't, exec (e) and
			// review among them. app-server is Codex's daemon, which runs
			// the TUI's sessions: Daemon and Meta tell those apart.
			return len(p.words) > 0 && codexSubcommands[p.words[0]] && p.words[0] != "app-server"
		},
	},
	{
		Name: Antigravity, Command: "agy", Short: "agy",
		parser: parser{stopAtWord: true, specs: []spec{
			{names: goNames("model"), kind: value, keep: true},
			{names: goNames("mode"), kind: value, keep: true},
			{names: goNames("dangerously-skip-permissions"), kind: boolean, keep: true, skips: always},
			{names: goNames("sandbox"), kind: boolean, keep: true},
			{names: goNames("add-dir"), kind: value, keep: true, dir: true},
			// --effort, like --model, is the session's own; whether
			// --conversation keeps it wasn't tried, and passing it is harmless.
			{names: goNames("effort"), kind: value, keep: true, only: is("low", "medium", "high", "xhigh", "max")},
			// -p takes the next word as its prompt (evidence/r1/agy-probe.log G).
			{names: append(append(goNames("print"), goNames("p")...), goNames("prompt")...), kind: value},
			{names: append(goNames("continue"), goNames("c")...), kind: boolean},
			{names: goNames("agent"), kind: value},
			{names: goNames("conversation"), kind: value},
			{names: goNames("input-format"), kind: value},
			{names: goNames("json-schema"), kind: value},
			{names: goNames("log-file"), kind: value},
			{names: goNames("output-format"), kind: value},
			{names: goNames("print-timeout"), kind: value},
			{names: goNames("project"), kind: value},
			{names: append(goNames("prompt-interactive"), goNames("i")...), kind: value},
			{names: goNames("settings"), kind: value},
		}},
		batch: func(p parsed) bool {
			// The stream formats are print mode's only, as with Claude Code.
			return p.has["--print"] || p.has["--input-format"] || p.has["--output-format"] ||
				(len(p.words) > 0 && agySubcommands[p.words[0]])
		},
	},
}

// codexSubcommands are Codex's subcommands that don't start its
// interactive TUI, as codex --help lists them.
var codexSubcommands = set("exec", "e", "review", "login", "logout", "mcp", "mcp-server", "plugin",
	"app-server", "remote-control", "app", "completion", "update", "doctor", "sandbox", "debug",
	"apply", "a", "queue", "archive", "delete", "migrate-rollouts", "migrate-nfs-db", "unarchive",
	"cloud", "exec-server", "features", "help", "agents")

// agySubcommands are agy's subcommands, as agy --help lists them.
var agySubcommands = set("acp", "agent", "agents", "changelog", "help", "install", "mcp",
	"mic-serve", "models", "plugin", "plugins", "remote-control", "update")

func set(ws ...string) map[string]bool {
	m := map[string]bool{}
	for _, w := range ws {
		m[w] = true
	}
	return m
}
