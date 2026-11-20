#!/usr/bin/env python3
# Probe hook: one JSON line per run with stdin, folder, parent, env, the CLAUDE_PID process,
# and the last line's type of each transcript of the scratch project.
import json, os, sys, subprocess, glob, time
LOG = os.path.join(os.path.dirname(os.path.abspath(__file__)), "log", "hook.jsonl")
try:
    raw = sys.stdin.read()
    inp = json.loads(raw) if raw.strip() else None
except Exception:
    inp = {"unparsed": raw[:500]}
pid = os.environ.get("CLAUDE_PID", "")
ps = ""
if pid:
    ps = subprocess.run(["ps", "-ww", "-o", "pid=,lstart=,args=", "-p", pid], capture_output=True, text=True).stdout.strip()
tails = {}
for p in glob.glob(os.path.expanduser("~/.claude/projects/*cprobe-proj*/*.jsonl")):
    try:
        with open(p, "rb") as f:
            f.seek(max(os.path.getsize(p) - 8192, 0))
            lines = [l for l in f.read().split(b"\n") if l.strip()]
        last = json.loads(lines[-1]) if lines else {}
        tails[os.path.basename(p)[:8]] = {"last_type": last.get("type"), "continuedIn": last.get("continuedInSessionId")}
    except Exception as e:
        tails[os.path.basename(p)[:8]] = {"err": str(e)[:80]}
rec = {
    "t": time.strftime("%H:%M:%S", time.gmtime()) + ".%03d" % int((time.time() % 1) * 1000),
    "in": inp, "cwd": os.getcwd(), "ppid": os.getppid(),
    "env": {k: os.environ.get(k) for k in ("CLAUDE_PID", "CLAUDE_PROJECT_DIR", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SESSION_ID")},
    "ps_claude_pid": ps, "tails": tails,
}
with open(LOG, "a") as f:
    f.write(json.dumps(rec) + "\n")
