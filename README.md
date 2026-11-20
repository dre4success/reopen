# reopen

`reopen` brings back a project's agent sessions in one step. It's a small CLI for macOS.

Once set up, it records the Claude Code, Codex and Antigravity sessions you start in a terminal in a git repository. Later, `reopen` in that repository resumes the closed ones in a new iTerm2 window: one tab each, each in its own folder.

## Install

You need macOS, iTerm2 and Go 1.25.3 or later.

```sh
go install github.com/dre4success/reopen/cmd/reopen@latest
reopen init
```

`go install` puts `reopen` in `~/go/bin` (or `$GOBIN`). If your shell says `command not found`, add that folder to your `PATH`.

`reopen init` adds reopen's start hook to each agent CLI on your `PATH`. Run it once, from any folder, and it covers every project. It asks before changing anything, backs up any file it changes, and leaves the rest of the file alone. Run it again any time to see what's set up.

From then on, each session is recorded when it starts. For Codex and Antigravity, that's at its first turn.

Codex only runs a new hook once you trust it. It asks the next time it starts, so say yes.

To add a hook by hand instead, `reopen hooks <cli>` prints it and the file it goes in.

## Use

Anywhere in the project:

```sh
reopen               # pick the sessions to bring back, then Enter
reopen list          # every recorded session, open or closed
reopen --print       # print each tab's command; starts nothing
reopen forget [id]   # drop one session, or all of them
reopen version
```

```
reopen · reopen · 3 sessions to bring back

› ● claude  implementer    3 h ago     build reopen from docs/DESIGN.md
    ⚠ skips permission checks  reopen · bypassPermissions mode --dangerously-skip-permissions · 4.2 MB · a1b2c3d4
  ● codex   reviewer       yesterday   you're the reviewer: review R1 hard
    reopen · gpt-6.2-sol, approvals on-request, workspace-write · 11 MB · 019a6a3f
    first message: "You were reopened. Read your handoff inbox."
  ○ claude  (implementer)  6 days ago  an old idea
    reopen · 116 B · f0e1d2c3
    not ticked: its role implementer is now session a1b2c3d4's

⚠ skips permission checks: 1 of the ticked ones
↑↓ move · space tick · a all · n none · i details · enter opens 2 in a new iTerm2 window · q quit
```

The sessions worth bringing back are already ticked, so Enter is usually all it takes. `i` shows a row in full, with the exact command its tab will run.

### What comes back

Each tab runs `cd <folder> && <command>`, so the shell stays open when the agent exits.

| CLI | Command | Flags passed back |
|-----|---------|-------------------|
| Claude Code | `claude --resume <id>` | Added folders and permission-skipping flags. Claude Code keeps the model and permission mode itself, except bypass mode, which needs its flag. |
| Codex | `codex resume <id>` | None. Codex keeps the model, sandbox, approvals and added folders itself. |
| Antigravity | `agy --conversation <id>` | Model, effort, mode, sandbox, permission skipping and added folders. |

A session that comes back skipping permission checks is marked ⚠.

When a Claude Code session comes back with added folders, Claude Code asks about them again. Its default answer exits, so pick yes or the tab's agent quits.

Codex and Antigravity sessions with a handoff role get one message when they come back: "You were reopened. Read your handoff inbox." `--no-message` turns that off. Your first prompt is never sent again.

### What's left out

- Sessions that are still open. Codex counts a session as open for a few minutes after you close it.
- Sessions unused for 30 days, or whose folder is gone.
- A session whose handoff role another session now holds is shown, but not ticked.
- A Claude Code session running in the background is shown as `claude attach <job>`, not ticked.

## With handoff

`reopen` also finds the sessions handoff has given a role in the project, even ones started before you ran `reopen init`, as long as their CLI's records place them in this project.

To keep their flags, run `reopen` once while the team is still open. It saves the flags of each session whose process it can confirm. Otherwise they come back with what their CLI keeps, and the row says what's lost:

- Codex: its model, sandbox, approvals and added folders.
- Claude Code: its model and permission mode, but not bypass mode or added folders.
- Antigravity: agy's defaults.

## Boundaries

- reopen itself makes no network or model calls.
- It writes only its own files in `~/.reopen/`, and the CLIs' settings when you say yes to `reopen init`.
- It reads `.handoff/` and the CLIs' records but never writes them. It never writes to git.
- It runs `ps`, `lsof`, `osascript`, `git`, and `claude agents --json` with Claude Code's non-essential traffic turned off.
- Opening sessions, `init` and `forget` are refused inside an agent session. It spots one by the variable each CLI sets, so a CLI that sets none, such as Gemini CLI, isn't caught.
- The hook never fails a session. If it can't record one, the reason goes to `~/.reopen/hook.log`.

A long session resumed after days re-reads its whole context, at full price, on its first turn. Compact it before closing to make that cheaper.

## Development

From a clone, `go install ./cmd/reopen`. To test, run `go test ./...`. The tests use a temporary home and stand-in CLIs, and never open a window or touch your real settings.

The spec is [docs/DESIGN.md](docs/DESIGN.md), and what it rests on is [docs/FACTS.md](docs/FACTS.md).

## License

MIT. See [LICENSE](LICENSE).
