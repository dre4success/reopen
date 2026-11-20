# CLAUDE.md

`reopen` is a small local CLI for macOS. It records each agent session started in a project (Claude Code, Codex, Antigravity) and brings them all back in one step, each resumed in its own folder. The operator decides.

## Source of truth

- `docs/DESIGN.md` is the spec, approved by the operator. Read it before any work.
- `docs/FACTS.md` is what the design rests on, verified on the operator's Mac. Recheck a fact if its CLI has moved on, and add what you verify, with its evidence.
- `docs/LOG.md` is the resolution log. Respond to review points and record decisions by appending a row. Never edit an earlier one.

## Boundaries that never move

- No model call and no network.
- It writes only its own list under `~/.reopen/`, with mode 0600; and, when the operator runs `reopen init` at a terminal and says yes, its start hook into the agent CLIs' settings, each file copied beside itself first.
- It reads `.handoff/` and the CLIs' transcripts and records and never writes them.
- It starts agent CLIs only from `reopen` itself, for the operator at a terminal. It is refused in an agent's session. `--print` and `list` start nothing.
- It passes back a session's flags, never its first prompt.
- Git is read-only.

## Conventions

- Go, with no third-party dependencies. Of the Mac's own tools, it may run `ps`, `/usr/sbin/lsof`, `osascript` and `git`.
- Tests never open a terminal window, start an agent CLI, or touch the real `~/.reopen/` or a CLI's real settings. Use `--print`, a temporary HOME and stand-in CLIs.
- A check passes only with Run evidence under `evidence/`: the command, the date and who ran it.
- In repository files, write "the operator" and `~`, never a name or a home path.
- Conventional Commit subjects (`feat:`, `fix:`, `docs:`, `test:`), one concern per commit.
- One branch per item. No merge or push without the operator.
