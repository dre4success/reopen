# reopen

`reopen` brings back a project's agent sessions in one step. It is a small local CLI for macOS. Each Claude Code, Codex and Antigravity session started in a git repository is recorded by its CLI's start hook, and a session handoff gives a role is found from handoff's own records. Later, `reopen` resumes the closed ones in one new iTerm2 window, a tab each, every session in its own folder. The operator picks which.

The spec is [docs/DESIGN.md](docs/DESIGN.md). What it rests on, verified on the operator's Mac, is [docs/FACTS.md](docs/FACTS.md).

## Install

It needs macOS and Go 1.25.3 or later to build, and nothing outside Go's standard library. To bring sessions back it uses iTerm2 and the agent CLIs themselves.

```sh
go install github.com/dre4success/reopen/cmd/reopen@latest
reopen init
```

That's all the setup, once, for every project. From then on, `reopen` in a project's folder is the only command.

- `go install` puts `reopen` in `~/go/bin` (or `$GOBIN`). From a clone, `go install ./cmd/reopen` does the same. `reopen version` says which version you have.
- `reopen init` finds the agent CLIs you have and says, for each, whether its settings have reopen's start hook. It asks once, then adds the hook where it's missing:
  - Claude Code: `~/.claude/settings.json`;
  - Codex: `~/.codex/hooks.json`. Codex runs a new hook only once you trust it: it asks the next time it starts;
  - Antigravity: `~/.gemini/config/hooks.json`.

  Each file it changes is copied beside itself first (`<file>.reopen-backup-<time>`). The hook goes in at one spot, and every other byte stays as it was, other tools' hooks among them. Run it again any time: it says what's set up.

A session gets on the list when it starts (Codex and Antigravity: at its first turn), from then on. A session handoff gives a role gets on it without its hook, so a team started before the setup comes back too.

To add a hook by hand instead, `reopen hooks <cli>` prints it and the file it goes in.

## Use

In any folder of the project:

```sh
reopen                 # pick the closed sessions to bring back; Enter opens them
reopen init            # set up the start hooks, once (above)
reopen --print         # each tab's command instead; starts nothing
reopen list            # every session on the list, open or not
reopen forget [<id>]   # drop one entry (an id or its first 4+ characters), or all, printing each one's command
reopen version         # the version installed
```

```
reopen · reopen · 4 sessions to bring back

› ● claude  implementer    3 h ago     build reopen from docs/DESIGN.md
    reopen · bypassPermissions mode --dangerously-skip-permissions · 4.2 MB · a1b2c3d4  ⚠ skips permission checks
  ● codex   reviewer       yesterday   you're the reviewer: review R1 hard
    reopen · gpt-6.2-sol, on-request, workspace-write · 11 MB · 019a6a3f
    first message: "You were reopened. Read your handoff inbox."
  ○ claude  (reviewer-2)   40 min ago  long migration of the store
    reopen/cmd · in the background · claude attach e5f6a7b8 · 88 B · e5f6a7b8
  ○ claude  (implementer)  6 days ago  an old idea
    reopen · 116 B · f0e1d2c3
    not ticked: its role implementer is now session a1b2c3d4's

⚠ skips permission checks: 1 of the ticked ones
↑↓ move · space tick · a all · n none · i details · enter opens 2 in a new iTerm2 window · q quit
```

Everything is ticked except what shouldn't be, so Enter alone is usually all it takes. `i` shows the row under the cursor in full, with the exact line its tab will type, on a screen of its own: ↑↓ scroll it, ← goes back. Every line fits the terminal's width. Without a terminal at both ends it asks line by line instead (numbers tick and untick).

**Each row** shows:
- the CLI, the session's handoff role, and when it was last used;
- its name: for Claude Code, the one you gave it (`/rename`) or Claude Code's own title, else the first thing typed to it;
- the folder it comes back in and how it comes back;
- the size of its transcript, a hint of what the resume re-reads;
- its id;
- ⚠ when it comes back skipping permission checks.

**Not ticked:**
- a session whose handoff role another session holds now;
- a Claude Code session in the background, which is open with no tab. It is offered as `claude attach <job>`.

**Left out, with a word:**
- a session whose folder is gone, or whose folder or flags hold a control character;
- a Codex session that Codex's daemon still has loaded: it's open somewhere, or closed minutes ago. It's offered once Codex lets it go;
- one unused for 30 days, which is dropped.

A Claude Code session nobody typed to is left out quietly, since it can't be resumed.

**Each tab** types `cd -- <folder> && <command>` into its shell, so the shell stays when the agent exits. The commands:

| CLI | Command | What comes back |
|-----|---------|-----------------|
| Claude Code | `claude --resume <id>` | Its added folders and permission-skipping flags. The resume keeps the model and permission mode itself, as the session last had them, except bypass mode without its flag. Claude Code asks again whether to add each folder; its default answer exits. |
| Codex | `codex resume <id>` | Nothing passed: the resume keeps the model, sandbox, approval policy and added folders itself. The row shows them from the rollout's latest settings. |
| Antigravity | `agy --conversation <id>` | Its model, which the resume would lose, and its mode, sandbox, permission skipping and added folders. |

**The first message.**
- A Codex or Antigravity session with a handoff role is sent "You were reopened. Read your handoff inbox." Those two listen for their work only after a turn. `--no-message` turns it off.
- A Claude Code session listens at once, and is sent nothing.
- A first prompt is never passed back.

**With handoff.** Every session handoff gives a role in the project is on the list, whether or not its hook ever ran. reopen finds it by its id in its CLI's own records, which give its folder (it must be in this project), and drops it after 30 days unused, as any other.
- While it runs, reopen takes its exact flags from its process, as the hook would have: the process Claude Code lists for it, or for Antigravity the one holding its lock. So run `reopen` once while your team is open, and each comes back as it was.
- If reopen never saw it run, no flag is made up, and it comes back with what its CLI keeps:
  - Codex: everything, since its resume keeps its settings;
  - Claude Code: its model and permission mode, but not bypass mode or folders it added. The row says so;
  - Antigravity: agy's defaults, and the row says so.
- `forget` drops one, and reopen doesn't find it through handoff again until it starts.

**When it is open.** A session counts as open in any of these cases:
- a live process runs it;
- Claude Code lists it as running, in a terminal or in the background;
- for Codex, its daemon still has it loaded. That lasts some minutes after its terminal closes, so such a session is left out until then;
- for Antigravity, a live `agy` holds its presence lock, as with `agy --continue`.

**The hook.** It prints nothing and never fails a session. What keeps it from recording one goes to `~/.reopen/hook.log`, and `reopen list` points at it.

## Boundaries

- No model call and no network.
- It writes its own list: one file per repository under `~/.reopen/` (mode 0600, in a folder of mode 0700), and the hook's log beside it.
- It writes a CLI's settings only when you run `reopen init` at a terminal and say yes, each file copied beside itself first. It is refused in an agent's session.
- It reads `.handoff/sessions/` (for roles and the sessions they name) and the CLIs' transcripts and records, and never writes them. Git is read-only.
- It runs `ps`, `/usr/sbin/lsof`, `osascript` and `git`. To find background sessions it runs `claude agents --json`, with `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`, without which that command opens a connection off the Mac.
- Opening sessions, `forget` and `init` are refused in an agent's session: when `CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID` or `ANTIGRAVITY_CONVERSATION_ID` is set. Opening and `init` also need a terminal on stdin. `--print`, `list` and `hooks` work anywhere and change nothing: only `reopen` itself, and `forget`, change the list.
- **The known gap:** it is handoff's. A CLI that sets none of those variables but gives its commands a terminal, such as Gemini CLI, passes.

## A cost to know

A long session resumed after days re-reads its context at full price on its first turn. Compacting before closing makes that cheaper.

## Tests

```sh
go test ./...
```

The tests never open a terminal window, start an agent CLI, or touch the real `~/.reopen/` or a CLI's real settings. Each runs with:
- a temporary home and a scratch git repository;
- stand-in processes: the test binary, started again under the names `claude`, `codex` and `agy`;
- a stand-in `osascript`.

## License

MIT. See [LICENSE](LICENSE).
