// Command reopen records each agent session started in a project (Claude
// Code, Codex, Antigravity) and brings them back in one step, each resumed
// in its own folder, in a tab of one new iTerm2 window. The operator
// decides: it starts sessions only for the operator at a terminal.
//
// It makes no model call and has no network code. It writes its own list,
// under ~/.reopen/, and, only when the operator runs reopen init and says
// yes, its start hook into the CLIs' settings. It reads .handoff/ and the
// CLIs' transcripts and never writes them, and asks git only read-only
// questions.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/dre4success/reopen/internal/agent"
	"github.com/dre4success/reopen/internal/proc"
)

// app is one run of reopen. Its fields are the world it runs in, so a test
// can give it a temporary home and stand-ins.
type app struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	getenv         func(string) string
	home           string
	cwd            string
	now            func() time.Time
	terminal       func() bool // stdin is a terminal
	ppid           func() int  // the hook's parent process
	process        func(int) (proc.Info, bool)
	scan           func() ([]proc.Line, error)
	claudeAgents   func() ([]byte, error)        // claude agents --json
	openFiles      func([]int) ([]string, error) // the files the processes hold open, by lsof
	openTabs       func([]string) error          // a new iTerm2 window, a tab per command
	itermInstalled func() bool
	lookPath       func(string) (string, error) // a command on PATH
	// tty is the terminal to pick in key by key, or nil to ask line by
	// line; color says whether stdout takes colour.
	tty   func() *os.File
	color bool
	root  string // the project's main folder, once known
	wrote bool   // the list was brought up to date: a bare reopen's
}

const usage = `usage: reopen [--print] [--no-message]
       reopen init
       reopen list
       reopen forget [<id>]
       reopen hooks claude|codex|antigravity
       reopen hook claude|codex|antigravity      (run by a CLI's start hook)
       reopen version

reopen          lists this project's sessions that aren't open, newest first,
                and opens the ones you pick (all, by default) in one new
                iTerm2 window, a tab each. It is refused in an agent's session.
                The sessions handoff gives roles are on it even without the hook.
  --print       prints each tab's command instead, and starts nothing.
  --no-message  sends no first message to a Codex or Antigravity session
                with a handoff role. By default one is sent, since those
                listen for their work only after a turn.
reopen init     adds reopen's start hook to each agent CLI's settings, after
                asking: once, for every project.
reopen list     shows every session on this project's list, open or not.
reopen forget   drops one entry, or every entry of this project. Never a
                transcript.
reopen hooks    prints the start hook to add to a CLI's settings by hand.
reopen version  prints the version this reopen was built as.
`

func main() {
	a := newApp()
	os.Exit(a.run(os.Args[1:]))
}

func (a *app) run(args []string) int {
	if len(args) == 0 {
		return a.cmdOpen(nil)
	}
	var err error
	code := 0
	switch args[0] {
	case "hook":
		return a.cmdHook(args[1:])
	case "init":
		return a.cmdInit(args[1:])
	case "hooks":
		err = a.cmdHooks(args[1:])
	case "list":
		err = a.cmdList(args[1:])
	case "forget":
		err = a.cmdForget(args[1:])
	case "help", "-h", "--help":
		fmt.Fprint(a.stdout, usage)
		return 0
	case "version", "--version":
		fmt.Fprintln(a.stdout, version())
		return 0
	default:
		if strings.HasPrefix(args[0], "-") {
			return a.cmdOpen(args)
		}
		err = fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
		code = 2
	}
	if err != nil {
		fmt.Fprintf(a.stderr, "reopen: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

// agentSession is the variable that says reopen runs in an agent's
// session, and that agent's CLI, or "". handoff refuses its operator's
// commands by the same variables. The known gap is the same too: a CLI that
// sets none of them but gives its commands a terminal, such as Gemini CLI,
// passes.
func (a *app) agentSession() string {
	for _, v := range []string{"CLAUDE_CODE_SESSION_ID", "CODEX_THREAD_ID", "ANTIGRAVITY_CONVERSATION_ID"} {
		if a.getenv(v) != "" {
			return v
		}
	}
	return ""
}

func (a *app) homes() agent.Homes { return agent.HomesFor(a.home, a.getenv) }

// version is the module's version this reopen was built from: v0.1.0 for
// go install …@v0.1.0, which is how an install is checked after a release;
// a pseudo-version or "(devel)" for a build from a clone.
func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "(devel)"
}
