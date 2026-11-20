# reopen

`reopen` brings back a project's agent sessions in one step. It is a small local CLI for macOS. Each Claude Code, Codex and Antigravity session started in a git repository is recorded by its CLI's start hook. Later, `reopen` resumes the closed ones in one new iTerm2 window, a tab each, every session in its own folder. The operator picks which.

The spec is [docs/DESIGN.md](docs/DESIGN.md). What it rests on, verified on the operator's Mac, is [docs/FACTS.md](docs/FACTS.md).

## Install

It needs macOS and Go 1.25.3 or later to build, and nothing outside Go's standard library. To bring sessions back it uses iTerm2 and the agent CLIs themselves.

```sh
go install github.com/dre4success/reopen/cmd/reopen@latest
```

That puts `reopen` in `~/go/bin` (or `$GOBIN`). From a clone, `go install ./cmd/reopen` does the same. `reopen version` says which version you have.

Then add its start hook to each CLI. `reopen hooks <cli>` prints the hook and the file it goes in. It prints only; it never edits a CLI's settings.

```sh
reopen hooks claude            # ~/.claude/settings.json
reopen hooks codex             # ~/.codex/hooks.json; Codex asks you to trust it once
reopen hooks antigravity       # ~/.gemini/config/hooks.json
```

A session gets on the list when it starts (Codex and Antigravity: at its first turn), from then on.

## Use

In any folder of the project:

```sh
reopen                 # pick the closed sessions to bring back; Enter opens them
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

**When it is open.** A session counts as open in any of these cases:
- a live process runs it;
- Claude Code runs it in the background;
- for Codex, its daemon still has it loaded. That lasts some minutes after its terminal closes, so such a session is left out until then;
- for Antigravity, a live `agy` holds its presence lock, as with `agy --continue`.

**The hook.** It prints nothing and never fails a session. What keeps it from recording one goes to `~/.reopen/hook.log`, and `reopen list` points at it.

## Boundaries

- No model call and no network.
- It writes only its own list: one file per repository under `~/.reopen/` (mode 0600, in a folder of mode 0700), and the hook's log beside it.
- It reads `.handoff/sessions/` (only for roles) and the CLIs' transcripts, and never writes them. Git is read-only.
- It runs `ps`, `/usr/sbin/lsof`, `osascript` and `git`. To find background sessions it runs `claude agents --json`, with `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`, without which that command opens a connection off the Mac.
- Opening sessions, and `forget`, are refused in an agent's session: when `CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID` or `ANTIGRAVITY_CONVERSATION_ID` is set. Opening also needs a terminal on stdin. `--print`, `list` and `hooks` work anywhere and change nothing: only `reopen` itself, and `forget`, change the list.
- **The known gap:** it is handoff's. A CLI that sets none of those variables but gives its commands a terminal, such as Gemini CLI, passes.

## A cost to know

A long session resumed after days re-reads its context at full price on its first turn. Compacting before closing makes that cheaper.

## Tests

```sh
go test ./...
```

The tests never open a terminal window, start an agent CLI or touch the real `~/.reopen/`. Each runs with:
- a temporary home and a scratch git repository;
- stand-in processes: the test binary, started again under the names `claude`, `codex` and `agy`;
- a stand-in `osascript`.

## License

MIT. See [LICENSE](LICENSE).
