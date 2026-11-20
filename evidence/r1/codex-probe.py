#!/usr/bin/env python3
"""reopen R1 probe, Codex: which start flags `codex resume <id>` keeps (check 1), whether
`codex resume <id> "<message>"` opens the TUI and sends the message (check 3), the hook's parent
process and sandbox, the rollout's first user message, and `codex exec`'s argv.
Run: python3 codex-probe.py <step>, steps a1 a2 a3 a4 b d, in order. A scratch CODEX_HOME and repo."""
import glob, json, os, subprocess, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import drv

CH = os.path.join(drv.SP, "codex-home")
ENV = dict(drv.BASE_ENV, CODEX_HOME=CH)
STATE = os.path.join(drv.SP, "codex-state.json")
drv.LOG = os.path.join(drv.SP, "codex-probe.log")
MODEL = "gpt-6-luna"
drv.ENTER = "\x1b[13u"

def state():
    try:
        return json.load(open(STATE))
    except FileNotFoundError:
        return {}

def save(s):
    json.dump(s, open(STATE, "w"))

def rollout(sid):
    found = glob.glob(os.path.join(CH, "sessions", "*", "*", "*", "rollout-*-%s.jsonl" % sid))
    return found[0] if found else None

def lines(path):
    out = []
    for l in open(path):
        try:
            out.append(json.loads(l))
        except Exception:
            pass
    return out

def count(path, kind):
    if not path:
        return 0
    return sum(1 for d in lines(path) if d.get("type") == "event_msg" and (d.get("payload") or {}).get("type") == kind)

def last_turn(path):
    tc = [d for d in lines(path) if d.get("type") == "turn_context"]
    if not tc:
        return None
    p = tc[-1]["payload"]
    return {k: p.get(k) for k in ("cwd", "model", "approval_policy", "sandbox_policy", "effort", "permission_profile") if k in p} | {"turn_contexts": len(tc)}

def settings_events(path):
    return [d["payload"] for d in lines(path) if d.get("type") == "event_msg" and (d.get("payload") or {}).get("type") == "thread_settings_applied"]

def trust(t):
    if t.wait_for(r"Trust all and continue", 15):
        drv.say("   (Codex asks to review the new hook: answered 2, Trust all and continue)")
        t.send("2")
        t.pump(1)
        t.buf = ""

def new_hooks(seen):
    hs = drv.hooklog()
    for h in hs[seen:]:
        drv.say("   hook ran: stdin=%s cwd=%s" % (json.dumps(h["stdin"])[:400], h["cwd"]))
        drv.say("     parent: %s" % h["parent"][:400])
        drv.say("     grandparent: %s" % h["grandparent"][:300])
        drv.say("     write outside the repo: %s" % json.dumps(h["outside_write"]))
        drv.say("     env: %s" % json.dumps(h["env"]))
    return len(hs)

def turn(t, text, path_fn, secs=120):
    before = count(path_fn(), "task_complete")
    t.type(text)
    end = time.time() + secs
    while time.time() < end:
        t.pump(2)
        if count(path_fn(), "task_complete") > before:
            return True
        if "Allow" in t.text()[-600:] or "approve" in t.text()[-600:].lower():
            drv.say("   (an approval prompt is on screen)")
            return False
    return False

def run_tui(args, label, prompt=None, typed=None, trust_hooks=False, wait_ready=True):
    s = state()
    seen = len(drv.hooklog())
    drv.say("## %s: %s" % (label, " ".join(args)))
    t = drv.Term(args, ENV)
    if trust_hooks:
        trust(t)
    if wait_ready:
        ok = t.wait_for(r"Ask Codex|›", 30)
        drv.say("   TUI ready: %s" % ok)
    return t, seen

if __name__ == "__main__":
    step = sys.argv[1]
    s = state()
    if step == "a1":
        t, seen = run_tui(["codex", "-m", MODEL, "-s", "read-only", "-a", "on-request"], "A1 a new session, read-only sandbox, on-request approvals")
        t.pump(3)
        # the session id is in the rollout's name once the first turn starts
        t.type("Reply with the single word ready. Do not run any command.")
        sid = None
        for _ in range(60):
            t.pump(2)
            hs = drv.hooklog()[seen:]
            if hs and isinstance(hs[-1]["stdin"], dict):
                sid = hs[-1]["stdin"].get("session_id")
            if sid and count(rollout(sid), "task_complete") > 0:
                break
        seen = new_hooks(seen)
        drv.say("   session id: %s; first turn complete: %s" % (sid, bool(sid and count(rollout(sid), "task_complete"))))
        drv.say("   screen: %s" % t.tail(8))
        t.close()
        drv.say("   closed; process alive: %s" % t.alive())
        s["sid"] = sid
        save(s)
        r = rollout(sid)
        drv.say("   rollout: %s" % (r and r.replace(CH, "<CODEX_HOME>")))
        drv.say("   last turn_context: %s" % json.dumps(last_turn(r)))
    elif step == "a2":
        sid = s["sid"]
        t, seen = run_tui(["codex", "resume", sid], "A2 resume with no flags", trust_hooks=False)
        t.pump(3)
        drv.say("   screen at resume: %s" % t.tail(6)[-700:])
        seen = new_hooks(seen)
        done = turn(t, "Reply with the single word again. Do not run any command.", lambda: rollout(sid))
        seen = new_hooks(seen)
        drv.say("   turn complete: %s" % done)
        drv.say("   screen: %s" % t.tail(6)[-700:])
        t.close()
        drv.say("   last turn_context after the resumed turn: %s" % json.dumps(last_turn(rollout(sid))))
        drv.say("   thread_settings_applied events: %s" % json.dumps(settings_events(rollout(sid)))[:1500])
    elif step == "a2b":
        sid = s["sid"]
        drv.say("## A2b the scratch app-server daemon was stopped first, so the thread is read back from the rollout")
        t, seen = run_tui(["codex", "resume", sid], "A2b resume with no flags, a new daemon")
        t.pump(3)
        done = turn(t, "Reply with the single word afresh. Do not run any command.", lambda: rollout(sid))
        seen = new_hooks(seen)
        drv.say("   turn complete: %s" % done)
        drv.say("   screen: %s" % t.tail(4)[-500:])
        t.close()
        drv.say("   last turn_context after the resumed turn: %s" % json.dumps(last_turn(rollout(sid))))
    elif step == "a3":
        sid = s["sid"]
        t, seen = run_tui(["codex", "resume", sid, "-m", MODEL, "-s", "workspace-write", "-a", "never"], "A3 resume with -m -s -a after the id")
        t.pump(3)
        drv.say("   screen at resume: %s" % t.tail(6)[-700:])
        done = turn(t, "Reply with the single word third. Do not run any command.", lambda: rollout(sid))
        seen = new_hooks(seen)
        drv.say("   turn complete: %s" % done)
        t.close()
        drv.say("   last turn_context: %s" % json.dumps(last_turn(rollout(sid))))
    elif step == "a4":
        sid = s["sid"]
        t, seen = run_tui(["codex", "-s", "read-only", "-a", "on-request", "resume", sid], "A4 resume with -s -a before resume")
        t.pump(3)
        drv.say("   screen at resume: %s" % t.tail(6)[-700:])
        if t.alive():
            done = turn(t, "Reply with the single word fourth. Do not run any command.", lambda: rollout(sid))
            seen = new_hooks(seen)
            drv.say("   turn complete: %s" % done)
        t.close()
        drv.say("   last turn_context: %s" % json.dumps(last_turn(rollout(sid))))
    elif step == "a5":
        sid = s["sid"]
        t, seen = run_tui(["codex", "resume", sid], "A5 resume with no flags again, after A4")
        t.pump(3)
        done = turn(t, "Reply with the single word fifth. Do not run any command.", lambda: rollout(sid))
        seen = new_hooks(seen)
        drv.say("   turn complete: %s" % done)
        t.close()
        drv.say("   last turn_context: %s" % json.dumps(last_turn(rollout(sid)))[:400])
    elif step == "a0":
        t, seen = run_tui(["codex"], "A0 a new session with no flags, for the defaults of this CODEX_HOME")
        t.pump(3)
        t.type("Reply with the single word zero. Do not run any command.")
        sid0 = None
        for _ in range(60):
            t.pump(2)
            hs = drv.hooklog()[seen:]
            if hs and isinstance(hs[-1]["stdin"], dict):
                sid0 = hs[-1]["stdin"].get("session_id")
            if sid0 and count(rollout(sid0), "task_complete") > 0:
                break
        seen = new_hooks(seen)
        t.close()
        s["sid0"] = sid0
        save(s)
        drv.say("   last turn_context: %s" % json.dumps(last_turn(rollout(sid0)))[:400])
    elif step == "a6":
        out = os.path.join(drv.SP, "outside")
        t, seen = run_tui(["codex", "-m", MODEL, "--dangerously-bypass-approvals-and-sandbox", "--add-dir", out], "A6 a new session with the bypass flag and an added folder")
        t.pump(3)
        t.type("Reply with the single word six. Do not run any command.")
        sid6 = None
        for _ in range(60):
            t.pump(2)
            hs = drv.hooklog()[seen:]
            if hs and isinstance(hs[-1]["stdin"], dict):
                sid6 = hs[-1]["stdin"].get("session_id")
            if sid6 and count(rollout(sid6), "task_complete") > 0:
                break
        seen = new_hooks(seen)
        t.close()
        s["sid6"] = sid6
        save(s)
        drv.say("   last turn_context: %s" % json.dumps(last_turn(rollout(sid6))))
    elif step == "a6b":
        sid6 = s["sid6"]
        killed = []
        for line in subprocess.run(["ps", "-axww", "-o", "pid=,args="], capture_output=True, text=True).stdout.splitlines():
            if os.path.join(CH, "packages", "app-server-daemon") in line:
                pid = int(line.split()[0]); os.kill(pid, 15); killed.append(pid)
        time.sleep(2)
        drv.say("## A6b the scratch daemon stopped (%d processes), then a plain resume of A6's session" % len(killed))
        t, seen = run_tui(["codex", "resume", sid6], "A6b resume with no flags")
        t.pump(3)
        done = turn(t, "Reply with the single word seven. Do not run any command.", lambda: rollout(sid6))
        seen = new_hooks(seen)
        drv.say("   turn complete: %s" % done)
        t.close()
        drv.say("   last turn_context: %s" % json.dumps(last_turn(rollout(sid6))))
    elif step == "b":
        sid = s["sid"]
        msg = "You were reopened. Read your handoff inbox."
        before = count(rollout(sid), "task_started")
        t, seen = run_tui(["codex", "resume", sid, msg], "B resume with a first message", wait_ready=False)
        started = False
        for _ in range(40):
            t.pump(1)
            if count(rollout(sid), "task_started") > before:
                started = True
                break
        drv.say("   a turn started with nothing typed: %s" % started)
        users = [d for d in lines(rollout(sid)) if d.get("type") == "response_item" and (d.get("payload") or {}).get("role") == "user"]
        drv.say("   the rollout's last user item: %s" % (json.dumps(users[-1]["payload"]["content"])[:300] if users else "none"))
        t.pump(15)
        seen = new_hooks(seen)
        drv.say("   screen: %s" % t.tail(10))
        drv.say("   the TUI is still running: %s" % t.alive())
        t.close()
    elif step == "d":
        seen = len(drv.hooklog())
        drv.say("## D codex exec, stdin from /dev/null")
        r = subprocess.run(["codex", "exec", "-m", MODEL, "Reply with the single word ok. Do not run any command."], cwd=drv.REPO, env=ENV,
                           stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=180)
        drv.say("   exit %d, stdout: %r" % (r.returncode, r.stdout.strip()[-200:]))
        new_hooks(seen)
    elif step == "head":
        r = rollout(s["sid"])
        for i, d in enumerate(lines(r)[:40]):
            p = d.get("payload") if isinstance(d.get("payload"), dict) else {}
            desc = ""
            if d.get("type") == "response_item" and p.get("type") == "message":
                desc = " | ".join((x.get("text") or "")[:90].replace("\n", " ") for x in p.get("content") or [] if isinstance(x, dict))
            elif d.get("type") == "event_msg" and p.get("type") == "user_message":
                desc = (p.get("message") or "")[:120]
            elif d.get("type") == "session_meta":
                desc = json.dumps({k: p.get(k) for k in ("id", "cwd", "originator", "source", "thread_source", "cli_version")})
            drv.say("   line %d: %s %s %s %s" % (i, d.get("type"), p.get("type"), p.get("role") or "", desc[:300]))
