# What reopen rests on

These facts were verified on the operator's Mac while building handoff, with throwaway sessions, from 2026-11-13 to 2026-11-19 (handoff items H30 to H33). The versions then were Claude Code 2.1.325 to 2.1.328, Codex 0.169.0 and Antigravity (`agy`) 1.5.9. Recheck a fact before building on it if the CLI has moved on, and add what you verify here, with its evidence. The evidence behind these is in handoff's repository, under `evidence/h30/` to `evidence/h33/` and `evidence/reviewer/H30-*` to `H33-*`.

## Claude Code

**Hooks and folders**

- A hook runs in the folder the session is in. A start, a resume, `/clear` and `/background` each put the session back in the folder its process was started or resumed in. `/compact` doesn't: its SessionStart runs where the session is.
- `CLAUDE_PROJECT_DIR` is set for hooks only, not for the agent's commands. It is the folder the process was started or resumed in, and it stays that after a `cd`.
- A hook also has `CLAUDE_PID`. That process's own folder is the run's folder, also for a `/background` worker. `/usr/sbin/lsof -a -p <pid> -d cwd -Fn` gives it in 10 to 30 ms.
- SessionStart's stdin has `session_id`, `transcript_path`, `source` and `hook_event_name`. `source` is one of `startup`, `resume`, `clear`, `compact` and `fork`.
- Claude Code cuts SessionEnd hooks short: one that slept 8 s held the process for 1.95 s.

**Ids**

- **`claude --resume <id>`** keeps the id and works from another folder. It keeps writing the first transcript, filed under the first folder. At that resume's SessionStart, `transcript_path` names a file under the new folder that doesn't exist. The transcript's first `cwd` says where the conversation began, not where this run started.
- **`/clear`** gives a new id in the same process, with source `clear`.
- **`/background`:**
  - The old session gets Stop and SessionEnd, and its process exits. The conversation goes on under a new id in a background worker, with a new `CLAUDE_PID` and source `fork`.
  - The old transcript ends with `{"type":"continued-in","continuedInSessionId":"<new id>"}`, with a `timestamp`. On 2.1.329 a few bookkeeping lines follow it (`cost-state`, `file-history-snapshot`, `last-prompt`), but no message; see "Rechecked for reopen" below.
  - The new session's SessionStart usually runs 0.1 to 1 s before the old process exits.
  - A session resumed after `/background` keeps its id, and its transcript goes on after that line, so the line can be stale.
- **`/fork`** starts a new session with source `fork`. The original keeps running, and no continued-in line is written.
- **`--fork-session`** gives a new id and writes no line.

**What gets recorded and what can be resumed**

- A session started and closed with nothing typed ran its start hooks but left no transcript. `claude --resume <id>` then says "No conversation found".
- `claude -p` runs the start hooks too.
- Transcripts are at `~/.claude/projects/<first folder>/<id>.jsonl`. In the folder's name, `/` and `.` (at least) become `-`, so find one by the glob `~/.claude/projects/*/<id>.jsonl` rather than by rebuilding the name.

**Background sessions**

- `claude agents --json` lists background jobs.
- `claude stop`, `claude rm` and `claude attach` take the 8-character job id, not the session id.
- `claude attach <job>` gives an idle background session a turn.

**With handoff**

- handoff's Claude Code SessionStart runs `handoff hook listen` with `asyncRewake`. So a resumed session listens for its work at once, with nothing typed: a request sent 15 s after the resume woke it.
- handoff keeps a resumed session's role under a new process id.

## Codex

- **Hooks:** `~/.codex/hooks.json`. SessionStart fires when the first turn starts, and handoff's listener is on Stop, so a resumed Codex session listens only after a turn. Hooks run in the run's folder, and stdin has `session_id` and `transcript_path`, which is the rollout. On 0.170.0 stdin also has `cwd`, `model`, `permission_mode` and `source`, and a hook's parent is Codex's shared daemon, not the session's process; see "Rechecked for reopen" below.
- **Rollouts** are at `~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<id>.jsonl`.
  - A `session_meta` line comes first and names the first folder.
  - A `turn_context` line at the start of each turn names the run's folder; the last one is current.
  - Rollouts reach tens of MB, so read the tail: the last `turn_context` was 2.4 MB from the end of a 34 MB rollout.
- **Resuming:** `codex exec resume <id>` from another folder keeps the one rollout and its one `session_meta`, and its hooks run in the new folder.
- **Commands:** `codex resume [SESSION_ID] [PROMPT]`. The picker without an id filters by folder, and `--all` shows every folder.
- **`codex exec`** runs its hooks. It waits on stdin, so give it `< /dev/null`.

## Antigravity (`agy`)

- **Hooks** are in `<project>/.agents/hooks.json` or `~/.gemini/config/hooks.json`.
  - A hook runs in the folder of its hooks.json, and its parent is the `agy` process.
  - Stdin has `conversationId`, `hookEventName`, `cwd` (the agent's folder) and `executionNum`. On 1.5.13 it has `conversationId`, `hookEventName`, `cwd`, `transcriptPath`, `modelName`, `lastUserInput`, `workspacePaths` and `artifactDirectoryPath`, and no `executionNum`.
- **When hooks fire:**
  - SessionStart fires at the first message only.
  - A reopened conversation (`agy --conversation <id>`) runs no hook until someone types to it, so until then it neither registers nor listens.
  - A Stop hook can continue the turn at most 10 times in a row until a person types.
  - While a Stop hook holds, a typed message waits; Esc ends the hook.
- **Its agent's commands** have a terminal and ten `ANTIGRAVITY_` variables, among them `ANTIGRAVITY_CONVERSATION_ID`. A hook gets only that one.
- **`agy -p`** runs its hooks. `agy -p --conversation <id>`, on a conversation with a handoff role, never ends, because its Stop hook listens.
- **Its data** is under `~/.gemini/antigravity-cli/`: `conversations/<id>.db` (a database), `brain/<id>/`, `history.jsonl`, `cache/`. Its global rules are `~/.gemini/GEMINI.md`. On 1.5.13 `brain/<id>/.system_generated/logs/transcript.jsonl` is a transcript; see below.
- **Options:** `--conversation <id>`, `--continue`, `--model`, `--prompt-interactive`. `--conversation` and `--prompt-interactive` work together (rechecked below).

## handoff's session files (read only)

- Each is `.handoff/sessions/<session id>.json`: `{"v":1,"role":…,"session":…,"pid":…,"host":…,"since":…,"was":[…]}`.
- `pid` is Claude Code's process, and `host` is the hook's parent for the other CLIs. `was` lists the ids the session went on from by `/background`.
- The format is handoff's own and can change. Show no role for a file whose `v` isn't 1.

## An agent's command or the operator's

handoff refuses its operator commands when `CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID` or `ANTIGRAVITY_CONVERSATION_ID` is set, and it needs a terminal on stdin. The known gap: a CLI that sets none of these but gives its commands a terminal passes, Gemini CLI among them. The operator hasn't decided how to close it.

## The operator's setup

- The terminal is iTerm2.
- handoff's hooks are installed for every project, in Claude Code, Codex and Antigravity.

## Probing safely

- **Inherited variables.** A CLI started from an agent session's shell inherits `CLAUDE_CODE_SESSION_ID` and `CLAUDE_PID`, so handoff would take it for that session. Start probe sessions with `env -i`, or unset them.
- **Alerts.** In any probe, set `HANDOFF_TEST_NOTIFY` to a scratch file, or real alerts reach the operator. An `@version` install uses a scratch `GOMODCACHE` and `GOBIN`.
- **Permission.** Throwaway Claude Code and Codex sessions are allowed. Antigravity ones need the operator's say-so first.
- **Claude Code probes:**
  - They need a pseudo-terminal, and the first prompt has to be typed.
  - The folder-trust dialog defaults to "No, exit".
  - A driver must reap its child at once, or `kill -0` sees the zombie as alive.
  - Clean up with `claude stop`, `claude rm` and `claude purge -y <scratch project>`.
- **Antigravity probes:**
  - Drive `agy --model gemini-3.8-flash-low --dangerously-skip-permissions` in a pseudo-terminal.
  - Afterwards, delete the probe's `brain/<id>/`, `conversations/<id>.db`, `annotations/<id>.pbtxt`, its lines in `history.jsonl`, its entries in `cache/conversation_metadata.json` and `cache/last_conversations.json`, its `log/cli-*.log`, and its rows in `conversation_summaries.db`.
  - Restore `settings.json` (`trustedWorkspaces`) from a copy made first.

## Rechecked for reopen (item R1)

Verified while building reopen, on 2026-11-19, with Claude Code 2.1.329, Codex 0.170.0, Antigravity 1.5.13 and iTerm2 3.7.8. The evidence is in `evidence/r1/`.

**Claude Code** (`claude-probe.log`, throwaway sessions on Haiku in a scratch repository, the probe hook added through `--settings`):

- **What a bare `claude --resume <id>` keeps** (design check 1):
  - It keeps the model and the permission mode, as the session last had them. They come from the transcript, which notes the mode in `permission-mode` lines.
  - It doesn't keep added folders. `--add-dir` passed again brings them back, after a question at the start whose default is "No, exit".
- **`/resume` typed inside a session** (check 2):
  - The process stays: the same `CLAUDE_PID`, with the same start time. SessionStart runs again, with source `resume` and the picked session's id, and `CLAUDE_PROJECT_DIR` stays.
  - The process's arguments still name the first id.
- **`/background`:**
  - The worker's arguments are `claude bg-spare --bg-spare <socket>`, with none of the session's flags.
  - The `continued-in` line is written before the new session's SessionStart runs. Bookkeeping lines follow it.
- **`claude agents --json`** (check 5):
  - It lists a background session as `{"id":"<8-character job>","sessionId":…,"kind":"background","pid":…,"state":…,"cwd":…}`, beside the interactive ones (`"kind":"interactive"`). It took 0.12 s.
  - The job id was the session id's first 8 characters, but reopen takes it from the list.
- **`claude agents --json` and the network** (`agents-network.log`):
  - As it is, it opens a connection off the Mac on every run, which its answer doesn't need. A sandbox that kills a process at such an attempt killed it every time.
  - With `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` it opens none, and gives the same list.
  - The list can hold a background session with no process (no `pid`): two of the operator's were like that.
- **Bypass mode doesn't survive a bare resume** (`e2e-claude.log`, try 2). A session started with `--permission-mode bypassPermissions` alone ran in that mode. After a bare `claude --resume <id>`, its next turn ran in `auto` mode, and a `permission-mode` line said so. `acceptEdits` does come back (A above). So bypass mode needs `--dangerously-skip-permissions` or `--allow-dangerously-skip-permissions` to come back.
- **`/fork`**, from handoff's own run, which reviewer-2 read for R1 (#25): handoff's `evidence/h30/probe-hooks.log`, "fork probe".
  - The fork's SessionStart has source `fork` and a `CLAUDE_PID` of its own, a worker's.
  - The original keeps its process.
  - No `continued-in` line is written.
- **Titles:**
  - A transcript holds `{"type":"custom-title","customTitle":…}` lines, the name the operator gave (`/rename`), and `{"type":"ai-title","aiTitle":…}` lines, Claude Code's own, which its `/resume` picker shows.
  - Both are written again and again, so the end of the file has them. 48 of the operator's 54 latest transcripts had `ai-title` lines.
  - A compaction writes a `user` line with `"isCompactSummary":true`, which isn't the operator's.
- **`CLAUDE_CODE_ENTRYPOINT`** reaches hooks. It is `cli` for an interactive run and its `/background` worker, and `sdk-cli` for `claude -p`. A `-p` run's process arguments hold the prompt.

**Codex** (`codex-probe.log`, throwaway sessions under a scratch `CODEX_HOME`, the probe hook started with `exec`):

- **What a bare `codex resume <id>` keeps** (check 1):
  - It keeps the model, the sandbox, the approval policy and added folders, as the session last had them, `--dangerously-bypass-approvals-and-sandbox` among them. A new session with no flags got the defaults, so these came from the session.
  - `codex resume` takes `-m`, `-s` and `-a` after `resume <id>` or before `resume`. A flag given at a resume becomes the session's setting.
- **`codex resume <id> "<message>"`** (check 3) opens the interactive screen and runs the message as the first turn at once.
- **A hook's parent is Codex's daemon, even with `exec`:**
  - In a terminal session the parent is `codex app-server --listen unix:// --analytics-default-enabled --managed-daemon`. It has parent process 1, lives long, and runs every TUI session of that Codex home.
  - The session's own TUI process names no id, and holds no file that does.
  - The daemon holds `<codex home>/thread-writer-locks/<id>.lock` and the rollout open while it has the session loaded. It still held them about 45 s after the TUI closed, and had let go by about 9 minutes. On the operator's Mac the daemon held one VS Code session's lock for over a day.
- **`codex exec`** runs without the daemon. The hook's parent is the `codex exec …` process itself.
- **Hooks aren't sandboxed.** A hook wrote outside the repository under `read-only` and `workspace-write`. Codex's trust question says so.
- **A rollout's first line, `session_meta`, says who started it** (seen on the operator's own rollouts):
  - `"originator":"codex-tui"` for the terminal (with `"source":"vscode"`);
  - `"Codex Desktop"` for the desktop app;
  - a subagent's thread, such as a guardian review, has `"source":{"subagent":…}` and its own `thread_source`.
- **Rollouts on 0.170.0** have no `event_msg` `user_message` lines. The operator's text is the first `response_item` message with role `user` that isn't the injected `# AGENTS.md instructions` or `<environment_context>`.
- **Each turn's `turn_context`** holds `model`, `approval_policy` and `sandbox_policy.type`.
- **`thread_settings_applied`** (an `event_msg`) is written when a start, a resume or a change applies the session's settings (`codex-settings.log`).
  - It holds `thread_settings`: `model`, `approval_policy`, `permission_profile` (`"disabled"` is no sandbox; `"managed"` lists what may be read and written), `cwd` and `runtime_workspace_roots`. That last is the session's folder plus its added ones, which `turn_context` doesn't hold.
  - A resume with other flags writes one with no turn after. A bare resume then comes back with it, not with the last `turn_context`. So the latest line of either kind is the session's settings.

**Antigravity** (`agy-probe.log`, throwaway sessions on `gemini-3.8-flash-low`, with the operator's leave):

- **What a bare `agy --conversation <id>` keeps** (check 1):
  - The model: no. The resumed turns ran on the default model, so reopen passes `--model` back.
  - Whether it keeps `--dangerously-skip-permissions` couldn't be told: in agy's default mode, safe commands ran without a question either way. reopen passes it back.
- **`agy --conversation <id> --prompt-interactive "<message>"`** (check 3) runs the message as a turn, and the session stays interactive.
- **A resumed conversation runs no SessionStart hook,** even after typed turns. So after a resume its entry's folder, process and last-seen time stay as they were.
- **A hook's parent is the `agy` process itself.** `-p` takes the next word as its prompt.
- **A live `agy` holds `presence/<id>.lock` open** for the conversation it runs, beside `conversations/<id>.db` and its `-wal`, from reviewer-3's `lsof` of a live session (#17). The `presence/` folder keeps lock files of closed conversations too, so what counts is a held one. That is how a conversation resumed with `agy --continue`, whose arguments name no id, is known to be open. A probe's cleanup also removes its `presence/<id>.lock`.
- **The transcript:** `brain/<id>/.system_generated/logs/transcript.jsonl` is written at every step. `conversations/<id>.db` changes only when the session closes.
  - Its `USER_INPUT` lines with `"source":"USER_EXPLICIT"` hold what the operator typed, wrapped in `<USER_REQUEST>…</USER_REQUEST>`.
  - Its `PLANNER_RESPONSE` lines carry `model`.

**Processes:** a process's start time and exact arguments are read from the kernel (`sysctl` `KERN_PROC_PID` and `KERN_PROCARGS2`). `ps` joins the arguments with spaces. `ps -o lstart=` prints the date in the locale's order, "Thu Nov 19" or "Thu 19 Nov". The two agree to the second (`go test ./internal/proc`, `go-test.log`).

**zsh**, the shell a tab runs, reads a word that starts with `=` as a command's path: `=ls` is `/bin/ls`. reviewer-2 ran this under `zsh -f` (#25), and `TestJoinRoundTrip` runs it under `/bin/zsh -f`. So reopen quotes such a word.

**iTerm2** (`iterm-probe.log`, check 4):
- `osascript` run from a shell inside iTerm2 opened a window and a second tab, and typed a command into each. macOS asked nothing.
- Text typed before the shell's prompt waited for it and ran. Quotes and a space in a folder's name held.
- A new tab starts in whatever folder its profile gives, so each command must `cd` first.
- Not tried: `osascript` from another terminal app, where macOS would ask for Automation permission.

**End to end** (`e2e-claude.log`): a real Claude Code session with reopen's hook, run from a build of this branch with reopen's home in a scratch folder.
- The hook recorded it, and `reopen list` showed it open while it ran.
- Once it ended, `reopen --print` printed `cd -- <folder> && claude --resume <id> --allow-dangerously-skip-permissions`. That line resumed the session, which showed open again, then closed again once it ended.

