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

## R4: set up once, and handoff's sessions

Asked by the operator on 2026-11-20, in the implementer's session (`docs/LOG.md` rows 73 to 75). It changes two lines above: what reopen writes (Limits), and where a session on the list comes from (Recording). `CLAUDE.md`'s boundaries say the same.

**`reopen init`** adds reopen's start hook to each agent CLI's user settings: one step, once, for every project, in place of `reopen hooks <cli>` three times and a paste.
- It looks for `claude`, `codex` and `agy` on PATH and reads each one's settings: set up already, to add, or can't add (a file that isn't JSON, or isn't laid out as its CLI writes it).
- It shows that and asks once, `[Y/n]`, then adds what's missing. Only a whole line is an answer: Ctrl-D changes nothing. Each file it changes is first copied beside itself, as `<file>.reopen-backup-<time>`.
- A hook counts as reopen's, set up, only where the CLI runs it at a start: `hooks.SessionStart[].hooks[]` for Claude Code and Codex, a group's `SessionStart` for Antigravity. Its words must be reopen (by its name, a path to it, or this reopen's own path), `hook` and the CLI, after an `exec` if any, and nothing more. A command that only mentions them, as `echo` would, isn't it.
- The hook goes in at one spot, and every other byte stays as it was, other tools' hooks among them. A link is followed and the file it names is written. A file changed since it was read is left alone.
- It needs a terminal and an absolute home, and is refused in an agent's session: a CLI's settings are the operator's to change. A settings folder that isn't an absolute path (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`) is left alone.
- Codex runs a new hook only once the operator trusts it, at its next start. `init`, `reopen list` and an empty `reopen` say so while Codex's `config.toml` has no record of trusting it, or there is no `config.toml`. `list` and an empty `reopen` also name a CLI on PATH that has no hook from reopen.
- `reopen hooks <cli>` stays, to add a hook by hand.

**handoff's sessions.** `reopen` reads `.handoff/sessions/` for the sessions it names, not only for roles. A session handoff gives a role in this project, and that reopen's hook never reported, is put on the list:
- found by its id in its CLI's records: Claude Code's transcripts, Codex's rollouts, Antigravity's brain or database;
- in the folder those records give, which must be in this project. For Claude Code, its transcript's first `cwd`; for Codex, its rollout's latest settings' `cwd`; for Antigravity, the first workspace in `cache/conversation_metadata.json`;
- if used in the last 30 days, and not dropped with `forget`.

Its flags: reopen takes the arguments of the process that runs it now, as the hook would have, only when something says that process runs this session. A process's name and start time can't.
- Claude Code: `claude agents --json` lists the session as interactive with that process. It is the process handoff notes as `pid`.
- Antigravity: the process handoff notes as `host` holds the session's `presence/<id>.lock`.
- Arguments that resume another session aren't taken. Codex's never are: none of its flags come back, and its hooks' parent is mostly its daemon.

Otherwise the entry has none, and no flag is made up:
- Claude Code: it comes back with what its resume keeps. Bypass mode isn't kept without the flag, and is reached with `--permission-mode` too (`evidence/r1/e2e-claude.log`), so a row last in it says the mode won't carry over. Its added folders aren't known either, and the row says so.
- Codex: nothing is lost, since its resume keeps its settings.
- Antigravity: it comes back on agy's defaults, and the row says so.

Its entry says `"found": "handoff"`, and the hook's report, when the session next starts, replaces it. A later run seen with the same proof replaces the process and flags taken from an earlier one, since agy runs no hook on a resume. Later means the process started later: the entry keeps its process's start time (`started`), and the list's lock compares them, since two reopens at once may write in either order. A hook's report is never replaced so. `list` and `--print` show it and write nothing; a bare `reopen` writes it.

`forget` reaches every row reopen shows, one found through handoff and not on the list yet among them. A session handoff names that `forget` dropped is noted in the list, under `forgotten`, and isn't found through handoff again. Its hook takes it off when it next starts, and the note goes once handoff names it no more.

**Open or not:** `claude agents --json` lists Claude Code's sessions in terminals as `"kind":"interactive"`. Such a session is open too.
