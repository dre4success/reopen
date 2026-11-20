# The implementer's evidence for R1

Run by Opus 5.5 (implementer) and two probe forks of that session, on the operator's Mac, 2026-11-19, with Claude Code 2.1.329, Codex 0.170.0, Antigravity 1.5.13, iTerm2 3.7.8 and Go 1.25.3. Each log starts with its command, its date and who ran it.

## The design's five checks

| Check | Evidence |
|-------|----------|
| 1. Which start flags each resume keeps | `claude-probe.log` A, `codex-probe.log` A, `agy-probe.log` E |
| 2. Whether `/resume` inside a session keeps the process | `claude-probe.log` B |
| 3. `codex resume <id> "<message>"` and `agy --conversation <id> --prompt-interactive` | `codex-probe.log` B, `agy-probe.log` F |
| 4. iTerm2's `write text`, and what macOS asks | `iterm-probe.log` (from inside iTerm2; not tried from another terminal) |
| 5. A backgrounded session's job in `claude agents --json` | `claude-probe.log` C |

## The rest

- `claude-probe.log`: also `/background` (the worker's arguments, the `continued-in` line) and `CLAUDE_CODE_ENTRYPOINT` (D).
- `codex-probe.log`: also the hook's parent (Codex's daemon), whether hooks are sandboxed, the rollout's lines, and `codex exec` (C, D).
- `agy-probe.log`: also the hook's input on 1.5.13, the transcript under `brain/`, and `agy -p` (G).
- `e2e-claude.log`: a real Claude Code session with reopen's hook, started in bypass mode, from a build of this branch at the commit named, with reopen's home in a scratch folder.
  - It shows the session recorded, open, closed, `--print`, resumed by that line under `zsh -f`, open, then closed again.
  - It also shows that the resume didn't keep bypass mode.
  - `e2e-claude-round1.log` is the first round's run of the same check.
- `ui-demo.log`: the built reopen's picker in a pseudo-terminal, with sample sessions of all three CLIs and a stand-in `osascript`. It shows the screens after each key, and the commands the tabs would get.
- `go-test.log`: gofmt, vet, and every test with the race detector, then twice more.
- Round 2, for reviewer #16:
  - `codex-settings.log`: which settings a bare `codex resume` comes back with when the latest settings line differs from the last turn's.
  - `ui-narrow.log`: the picker at 40 columns, with the details view, an arrow split between two writes, and `qn` in one write.
- Round 3, for reviewer #23: `ui-short.log`, the row in full at 40 by 12, scrolled to its end and back.
- Round 4, for reviewer-2 #25: `e2e-claude.log` again, at the round's commit.
- Round 5, for reviewer #43: `ui-resize.log`, the picker shrunk below its floor while picking, Enter and space pressed, then grown back.

## The drivers

- `claude-probe.py` with `claude-probe-hook.py`;
- `codex-probe.py`, `agy-probe.py`, `drv.py` (their pseudo-terminal driver), `probe-hook.py`, `codex-enter.py` and `codex-enter2.py`;
- `e2e-claude.py`;
- `ui-demo-setup.py`, `ui-demo-drive.py` and `ui-narrow-drive.py`;
- `codex-settings.py`, with `drv.py`;
- `iterm-probe.applescript`.

Scratch data was purged after each probe: Claude Code's with `claude purge`, Codex's under a scratch `CODEX_HOME`, and Antigravity's as `docs/FACTS.md` lists. No probe process is left.

Home paths are written `~`, and scratch folders `<scratch>` or `<demo>`.

**Redacted for publication (R3, `evidence/r3/redact.log`).** None of the following is evidence for anything reopen rests on:
- `claude-probe.log`: `/status` keeps the version, the session's name, id and kind, cwd and model. A note says what was cut. Tabs 0 to 3 of `/permissions` are cut, and tab 4, Workspace, is kept.
- `agy-probe.log`: the plan's name is written `<plan>`.
- Scratch paths are written `<scratch>`, and the repository's own path `<repository>`. In `ui-narrow.log` and `ui-short.log`, a path wrapped across a screen's lines is masked with `x` at the same width, so the screens read as they wrapped.
