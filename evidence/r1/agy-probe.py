#!/usr/bin/env python3
"""reopen R1 probe, Antigravity: whether `agy --conversation <id>` keeps the model and the
permission skipping (check 1), whether `--conversation <id> --prompt-interactive "<message>"` opens
interactively and sends the message (check 3), the hook's parent and its argv, `agy -p`'s argv, and
the conversation database's mtime as its last use. Run: python3 agy-probe.py <step>: e1 e2 e4 e3 f g,
with PROBE_OUTSIDE set for e3 and e4 (see outside() below)."""
import json, os, re, shlex, subprocess, sys, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import drv
# E3 and E4 touch a file in a folder outside the probe's workspace: the folder the scratch repo's
# .agents/hooks.json passes probe-hook.py last (<repository>/evidence/r1/.probe-outside when it ran).
# It was a literal path then; for publication it is an input: PROBE_OUTSIDE, an absolute path, the
# same the hooks.json names. The command that touches it quotes it as one shell word.
def outside():
    d = os.environ.get("PROBE_OUTSIDE", "")
    if not os.path.isabs(d):
        sys.exit("agy-probe.py: set PROBE_OUTSIDE to the absolute folder outside the workspace that E3 and E4 touch, the one the hooks.json passes probe-hook.py last")
    return d

G = os.path.join(drv.HOME, ".gemini", "antigravity-cli")
STATE = os.path.join(drv.SP, "agy-state.json")
drv.LOG = os.path.join(drv.SP, "agy-probe.log")
MODEL = "gemini-3.8-flash-low"
ENV = drv.BASE_ENV

def state():
    try:
        return json.load(open(STATE))
    except FileNotFoundError:
        return {}

def save(s):
    json.dump(s, open(STATE, "w"))

def db(cid):
    return os.path.join(G, "conversations", cid + ".db")

def mtime(cid):
    try:
        return time.strftime("%H:%M:%S", time.gmtime(os.stat(db(cid)).st_mtime))
    except FileNotFoundError:
        return "no file"

def new_hooks(seen):
    hs = drv.hooklog()
    for h in hs[seen:]:
        drv.say("   hook ran: stdin=%s cwd=%s" % (json.dumps(h["stdin"])[:400], h["cwd"]))
        drv.say("     parent: %s" % h["parent"][:400])
        drv.say("     grandparent: %s" % h["grandparent"][:300])
        drv.say("     write outside the repo: %s" % json.dumps(h["outside_write"]))
        drv.say("     env: %s" % json.dumps(h["env"]))
    return len(hs)

def trust(t):
    if t.wait_for(r"trust this folder", 20):
        drv.say("   (the folder's trust question: answered Yes, the default, with Enter)")
        t.pump(0.5)
        t.send("\r")
        t.pump(1)

def ready(t, secs=40):
    return t.wait_for(r"auto ·|Type your message|> ", secs)

def reply(t, word, secs=90):
    """wait for the word to appear on the screen after the prompt was typed"""
    mark = len(t.buf)
    end = time.time() + secs
    while time.time() < end:
        t.pump(1)
        new = drv.ANSI.sub(" ", t.buf[mark:])
        if re.search(r"\b%s\b" % word, new, re.I) and not re.search(r"(Thinking|Working|esc to interrupt)", new[-300:], re.I):
            t.pump(3)
            return True
    return False

def status_line(t):
    m = re.findall(r"Gemini[^/\n]{0,60}·[^/\n]{0,40}", t.text())
    return m[-1].strip() if m else "?"

def ps_agy(pid):
    out = subprocess.run(["ps", "-ww", "-o", "pid=,ppid=,args=", "-g", str(pid)], capture_output=True, text=True).stdout
    return " | ".join(l.strip() for l in out.splitlines() if l.strip())

if __name__ == "__main__":
    step = sys.argv[1]
    s = state()
    if step == "e1":
        seen = len(drv.hooklog())
        args = ["agy", "--model", MODEL, "--dangerously-skip-permissions"]
        drv.say("## E1 a new conversation: %s" % " ".join(args))
        t = drv.Term(args, ENV)
        trust(t)
        drv.say("   ready: %s" % ready(t))
        t.pump(2)
        drv.say("   status line before typing: %s" % status_line(t))
        drv.say("   processes: %s" % ps_agy(t.pid))
        t.type("Reply with the single word ready. Do not run any command.")
        drv.say("   reply seen: %s" % reply(t, "ready"))
        seen2 = new_hooks(seen)
        hs = drv.hooklog()[seen:]
        cid = next((h["stdin"].get("conversationId") for h in hs if isinstance(h["stdin"], dict) and h["stdin"].get("conversationId")), None)
        drv.say("   conversation id: %s; its database's mtime: %s" % (cid, mtime(cid) if cid else "-"))
        drv.say("   screen: %s" % t.tail(8))
        t.close(keys=("\x03", "\x03", "\x04"))
        drv.say("   closed; process alive: %s; database mtime after close: %s" % (t.alive(), mtime(cid) if cid else "-"))
        s["cid"] = cid
        save(s)
    elif step == "e2":
        cid = s["cid"]
        seen = len(drv.hooklog())
        args = ["agy", "--conversation", cid]
        drv.say("## E2 the conversation again with no flags: %s" % " ".join(args))
        drv.say("   database mtime before: %s" % mtime(cid))
        t = drv.Term(args, ENV)
        trust(t)
        drv.say("   ready: %s" % ready(t))
        t.pump(4)
        drv.say("   status line at resume, nothing typed: %s" % status_line(t))
        seen = new_hooks(seen)
        drv.say("   hooks run with nothing typed: %d" % (len(drv.hooklog()) - seen))
        drv.say("   processes: %s" % ps_agy(t.pid))
        t.type("Run exactly this shell command and nothing else: echo probe-ok")
        end = time.time() + 90
        asked = False
        mark = len(t.buf)
        while time.time() < end:
            t.pump(1)
            new = drv.ANSI.sub(" ", t.buf[mark:])
            if re.search(r"(allow|approve|permission|Yes, |Deny)", new, re.I):
                asked = True
                break
            if re.search(r"probe-ok[\s\S]*probe-ok", new):
                break
        t.pump(3)
        drv.say("   a permission question appeared: %s" % asked)
        drv.say("   screen: %s" % t.tail(14))
        seen = new_hooks(seen)
        drv.say("   status line after the turn: %s" % status_line(t))
        drv.say("   database mtime after the turn: %s" % mtime(cid))
        t.close(keys=("\x1b", "\x03", "\x03", "\x04"))
        drv.say("   closed; process alive: %s" % t.alive())
    elif step in ("e3", "e4"):
        target = os.path.join(outside(), "agy-touch-" + step)
        if step == "e4":
            cid = s["cid"]
            args = ["agy", "--conversation", cid]
            label = "E4 the E1 conversation (started with --dangerously-skip-permissions) again with no flags"
        else:
            args = ["agy", "--model", MODEL]
            label = "E3 the control: a new conversation without --dangerously-skip-permissions"
        drv.say("## %s: %s" % (label, " ".join(args)))
        t = drv.Term(args, ENV)
        trust(t)
        drv.say("   ready: %s" % ready(t))
        t.pump(3)
        t.type("Run exactly this shell command and nothing else: touch " + shlex.quote(target))
        end = time.time() + 90
        asked = False
        mark = len(t.buf)
        while time.time() < end:
            t.pump(1)
            new = drv.ANSI.sub(" ", t.buf[mark:])
            if re.search(r"(allow|approve|permission|Deny|Reject|\(y/n\))", new, re.I):
                asked = True
                break
            if os.path.exists(target):
                break
        t.pump(3)
        drv.say("   a permission question appeared: %s; the file was made: %s" % (asked, os.path.exists(target)))
        drv.say("   screen: %s" % drv.ANSI.sub(" ", t.buf[mark:])[-900:].replace("\n", " / "))
        t.close(keys=("\x1b", "\x1b", "\x03", "\x03", "\x04"))
        drv.say("   closed; process alive: %s" % t.alive())
        hs = drv.hooklog()
        if step == "e3" and hs and isinstance(hs[-1]["stdin"], dict):
            s["cid3"] = hs[-1]["stdin"].get("conversationId")
            save(s)
            drv.say("   E3's conversation: %s" % s["cid3"])
        if os.path.exists(target):
            os.remove(target)
    elif step == "f":
        cid = s["cid"]
        seen = len(drv.hooklog())
        msg = "You were reopened. Read your handoff inbox."
        args = ["agy", "--conversation", cid, "--prompt-interactive", msg]
        drv.say("## F the conversation with a first message: %s" % " ".join(args))
        t = drv.Term(args, ENV)
        trust(t)
        t.pump(25)
        seen = new_hooks(seen)
        drv.say("   hooks run with nothing typed: see above")
        drv.say("   screen: %s" % t.tail(16))
        drv.say("   still running (interactive): %s" % t.alive())
        drv.say("   database mtime: %s" % mtime(cid))
        t.close(keys=("\x1b", "\x03", "\x03", "\x04"))
        drv.say("   closed; process alive: %s" % t.alive())
    elif step == "g":
        seen = len(drv.hooklog())
        args = ["agy", "--model", MODEL, "-p", "Reply with the single word ok. Do not run any command."]
        drv.say("## G print mode: %s" % " ".join(args))
        r = subprocess.run(args, cwd=drv.REPO, env=ENV, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=180)
        drv.say("   exit %d, stdout: %r" % (r.returncode, r.stdout.strip()[-200:]))
        new_hooks(seen)
        hs = drv.hooklog()[seen:]
        cid = next((h["stdin"].get("conversationId") for h in hs if isinstance(h["stdin"], dict) and h["stdin"].get("conversationId")), None)
        s["cid_p"] = cid
        save(s)
