#!/usr/bin/env python3
"""reopen R1 round 2, Codex: when a resume's settings differ from the last turn's, which does a bare
`codex resume <id>` come back with? A scratch CODEX_HOME (auth.json and a minimal config.toml), no hooks."""
import glob, json, os, subprocess, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import drv
CH = os.path.join(drv.SP, "codex-home")
ENV = dict(drv.BASE_ENV, CODEX_HOME=CH)
drv.LOG = os.path.join(drv.SP, "probe7.log")
drv.ENTER = "\x1b[13u"
OUT = os.path.join(drv.SP, "outside")

def rollout(sid=None):
    found = sorted(glob.glob(os.path.join(CH, "sessions", "*", "*", "*", "rollout-*.jsonl")), key=os.path.getmtime)
    if sid:
        found = [f for f in found if f.endswith(sid + ".jsonl")]
    return found[-1] if found else None

def lines(p):
    out = []
    for l in open(p):
        try: out.append(json.loads(l))
        except Exception: pass
    return out

def events(p):
    """turn_context and thread_settings_applied, in order, with what reopen would show."""
    out = []
    for d in lines(p):
        pl = d.get("payload") or {}
        if d.get("type") == "turn_context":
            out.append(("turn_context", pl.get("model"), pl.get("approval_policy"), (pl.get("sandbox_policy") or {}).get("type"), pl.get("permission_profile")))
        elif d.get("type") == "event_msg" and pl.get("type") == "thread_settings_applied":
            ts = pl.get("thread_settings") or {}
            out.append(("thread_settings_applied", ts.get("model"), ts.get("approval_policy"), ts.get("permission_profile"), [r.replace(drv.SP, "<scratch>") for r in ts.get("runtime_workspace_roots") or []]))
    return out

def done(p):
    return sum(1 for d in lines(p) if d.get("type") == "event_msg" and (d.get("payload") or {}).get("type") == "task_complete")

def turn(t, sid, word):
    before = done(rollout(sid))
    t.type("Reply with the single word %s. Do not run any command." % word)
    for _ in range(60):
        t.pump(2)
        if done(rollout(sid)) > before:
            return True
    return False

def stop_daemon():
    n = 0
    for line in subprocess.run(["ps", "-axww", "-o", "pid=,args="], capture_output=True, text=True).stdout.splitlines():
        if CH in line and "app-server" in line:
            os.kill(int(line.split()[0]), 15); n += 1
    time.sleep(2)
    return n

drv.say("## 1. a new session: codex -m gpt-6-luna -s read-only -a on-request, one turn, closed")
t = drv.Term(["codex", "-m", "gpt-6-luna", "-s", "read-only", "-a", "on-request"], ENV)
t.wait_for(r"Ask Codex|›", 40); t.pump(2)
t.type("Reply with the single word one. Do not run any command.")
sid = None
for _ in range(60):
    t.pump(2)
    r = rollout()
    if r and done(r) > 0:
        sid = os.path.basename(r)[-len("00000000-0000-0000-0000-000000000000.jsonl"):-6]
        break
t.close()
drv.say("   session %s; events: %s" % (sid, json.dumps(events(rollout(sid)))))
drv.say("   daemons stopped: %d" % stop_daemon())

drv.say("## 2. resumed with codex resume <id> -m gpt-6.2-sol -s workspace-write -a never --add-dir <scratch>/outside, NO turn, closed")
t = drv.Term(["codex", "resume", sid, "-m", "gpt-6.2-sol", "-s", "workspace-write", "-a", "never", "--add-dir", OUT], ENV)
t.wait_for(r"Ask Codex|›", 40); t.pump(4)
drv.say("   screen: %s" % t.tail(5)[-500:])
t.close()
drv.say("   events now: %s" % json.dumps(events(rollout(sid))))
drv.say("   daemons stopped: %d" % stop_daemon())

drv.say("## 3. a bare codex resume <id>, one turn, closed")
t = drv.Term(["codex", "resume", sid], ENV)
t.wait_for(r"Ask Codex|›", 40); t.pump(3)
drv.say("   screen at the resume: %s" % t.tail(5)[-500:])
drv.say("   turn complete: %s" % turn(t, sid, "three"))
t.close()
ev = events(rollout(sid))
drv.say("   events now: %s" % json.dumps(ev))
drv.say("   the last turn_context (what the resumed turn ran with): %s" % json.dumps([e for e in ev if e[0] == "turn_context"][-1]))
drv.say("   daemons stopped: %d" % stop_daemon())
print("SID", sid)
