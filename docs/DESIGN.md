# reopen: bring back a project's sessions in one step

The spec. Approved by the operator on 2026-11-19 after two review rounds, as handoff's item H33 (request #192, the reviewer's APPROVE #194, the operator's approve #195), from the implementer's and the reviewer's drafts. Copied unchanged below this paragraph from handoff's `docs/proposals/REOPEN_DESIGN.md` at `11a63c2`, with the reviewer's notes for the build added at the end. `reopen` is a working name. It starts agents and types to them, which handoff never does; handoff doesn't change.

## The problem

The operator closes agent sessions to keep the Mac fast. Resuming two or three of them days later, in different CLIs and folders, is tedious enough to put off.

## What it does

- `reopen` lists the project's sessions that aren't open, newest first, with CLI, role and last use. It opens the ones picked (all, by default) in one new iTerm2 window, a tab each.
- `reopen --print` prints the commands instead. The tests check it.
- `reopen list` shows the list. `reopen forget [id]` drops one entry or all, never a transcript.
- `reopen hooks claude|codex|antigravity` prints the hooks to add.
- It is refused in an agent's session, as `decide` is, with the same known gap.

## Recording

Each CLI's start hook runs `reopen hook`. It records the CLI, the id, the run's folder, the process id and that process's arguments.

- **The folder:** `CLAUDE_PROJECT_DIR` for Claude Code, the hook's own folder for Codex, stdin `cwd` for Antigravity. Never a transcript's. It follows the latest run.
- **Skipped:** runs that aren't interactive (`claude -p`, `codex exec`, `agy -p`), and runs outside a git repository.
- **Claude Code's ids:**
  - `source: "clear"` replaces the entry from the same process.
  - A `/background` id replaces the entry whose transcript ends with a `continued-in` line naming it (H30).
  - `/fork` adds an entry.
- **The list:** one file per project under `~/.reopen/`, keyed by the full path of the common git folder, so worktree sessions are on it too.

## Open or not

A session is open in either case:
- a live process's arguments name its id (`reopen` typed them);
- its recorded process is alive, is that CLI, started before the entry was last seen, and last reported this id.

A backgrounded Claude Code session is open but has no tab. `reopen` offers `claude attach <job>` for it.

Also at `reopen`:
- A Claude Code session with no transcript is left out. Nobody typed to it, and it can't be resumed.
- Last use is the transcript file's time. Entries unused for 30 days are dropped, and `reopen` says so.
- Labels come from the transcript's first message for Claude Code and Codex. Antigravity keeps its conversations in a database, so its sessions show role and time.
- A missing folder is skipped, with a word. A session whose role went to another session starts unticked.

## Each tab

`cd <folder>`, then `claude --resume <id>`, `codex resume <id>` or `agy --conversation <id>`, with the start flags that matter: permission mode, sandbox, added folders, model. It is typed into the tab's shell, so the shell stays when the agent exits.

A resumed Claude Code session listens at once. Codex and Antigravity listen only after a turn, so one with a role starts with "You were reopened. Read your handoff inbox." This can be turned off.

## With handoff

`reopen` reads `.handoff/sessions/` only for roles, and shows none for a file that isn't `"v":1`. handoff keeps a resumed session's role under a new process id (checked in the H33 review).

## Limits

- **Boundaries:** no model call and no network. It writes only its list.
- **Not yet:** closing sessions, other terminal apps, desktop-app or editor sessions.
- **Cost:** a long session resumed after days re-reads its context at full price. Compacting before closing helps.

## To check while building

1. Which start flags each resume keeps, the model among them.
2. Whether `/resume` inside a session keeps the process.
3. That `codex resume <id> "<message>"` and `agy --conversation <id> --prompt-interactive` work.
4. iTerm2's `write text` from another terminal, and what macOS asks.
5. How to find a backgrounded session's job in `claude agents --json`.

## Notes for the build

From the reviewer's approval (handoff #194). No change to the design was asked; these are part of the spec.

- **Show the flags a session will come back with** in the list, before the operator picks. A flag that skips permission checks comes back too, days later.
- **Never replay a first prompt.** The recorded arguments can hold one (`claude "<prompt>"`). Pass back flags only.
- **Keep the list file private** (mode 0600), since the recorded arguments can hold what the operator typed.
- **Two times, two names.** "Last seen" is the hook's time and "last use" is the transcript's. Keep them apart in the code.
