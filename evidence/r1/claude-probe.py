#!/usr/bin/env python3
"""reopen R1 probe: throwaway Claude Code sessions (Haiku) in a scratch git repo.

A  which of --model, --permission-mode and --add-dir come back on `claude --resume <id>` with no flags
B  /resume typed inside a running session: same process? new id and source? CLAUDE_PROJECT_DIR?
C  /background: the worker's SessionStart and argv, the old transcript's last line at that moment,
   and `claude agents --json`
D  CLAUDE_CODE_ENTRYPOINT for hooks, interactive vs -p, and a -p run's CLAUDE_PID argv

The probe hook (hook.py) is added only through --settings on the command line.
Usage: probe.py <step> [ids...]
"""
import glob, json, os, pty, re, select, signal, subprocess, sys, time

S = os.path.dirname(os.path.abspath(__file__))
PROJ = os.path.join(S, "proj")
DIR2 = os.path.join(S, "dir2")
HOOKLOG = os.path.join(S, "log", "hook.jsonl")
HOME = os.environ["HOME"]
ENV = {
    "HOME": HOME, "USER": os.environ.get("USER", ""), "TERM": "xterm-256color",
    "LANG": "en_US.UTF-8", "SHELL": "/bin/zsh",
    "PATH": HOME + "/.local/bin:" + HOME + "/go/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
    "HANDOFF_TEST_NOTIFY": os.path.join(S, "alerts.txt"),
}
SETTINGS = json.dumps({"hooks": {"SessionStart": [{"hooks": [
    {"type": "command", "command": "/usr/bin/python3", "args": [os.path.join(S, "hook.py")]}]}]}})
ANSI = re.compile(r"\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][A-Za-z0-9]|\x1b[=>]")


def say(msg):
    print(time.strftime("%H:%M:%S", time.gmtime()), msg, flush=True)


def hooks():
    try:
        return [json.loads(l) for l in open(HOOKLOG) if l.strip()]
    except FileNotFoundError:
        return []


def show_hooks(seen):
    hs = hooks()
    for h in hs[seen:]:
        i = h.get("in") or {}
        say("   hook %s SessionStart source=%s session=%s CLAUDE_PID=%s ppid=%s entry=%s cwd=%s PROJECT_DIR=%s" % (
            h["t"], i.get("source"), i.get("session_id"), h["env"]["CLAUDE_PID"], h["ppid"],
            h["env"]["CLAUDE_CODE_ENTRYPOINT"], h["cwd"], h["env"]["CLAUDE_PROJECT_DIR"]))
        say("        transcript_path=%s" % i.get("transcript_path"))
        say("        ps of CLAUDE_PID: %s" % h["ps_claude_pid"])
        say("        transcripts' last lines then: %s" % json.dumps(h["tails"]))
    return len(hs)


def transcript(sid):
    found = glob.glob(os.path.join(HOME, ".claude", "projects", "*", sid + ".jsonl"))
    return found[0] if found else None


def tlines(sid):
    p = transcript(sid)
    if not p:
        return []
    out = []
    for l in open(p):
        try:
            out.append(json.loads(l))
        except Exception:
            pass
    return out


def wait_assistant(sid, before, secs=90):
    end = time.time() + secs
    while time.time() < end:
        ls = tlines(sid)
        for l in ls[before:]:
            if l.get("type") != "assistant":
                continue
            m = l.get("message", {})
            texts = [c.get("text") for c in m.get("content", []) if isinstance(c, dict) and c.get("type") == "text"]
            if texts:
                return m.get("model"), texts[-1]
        time.sleep(1)
    return None, None


class Term:
    def __init__(self, args):
        self.args = args
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(PROJ)
            os.execvpe(args[0], args, ENV)
        self.buf = ""

    def pump(self, secs):
        end = time.time() + secs
        while time.time() < end:
            r, _, _ = select.select([self.fd], [], [], 0.3)
            if r:
                try:
                    self.buf += os.read(self.fd, 65536).decode("utf-8", "replace")
                except OSError:
                    return False
        return True

    def text(self):
        return re.sub(r"[ \t]+", " ", ANSI.sub("", self.buf))

    def send(self, s):
        os.write(self.fd, s.encode())

    def type(self, s):
        for ch in s:
            self.send(ch)
            time.sleep(0.02)
        self.pump(0.5)
        self.send("\r")

    def alive(self):
        try:
            return os.waitpid(self.pid, os.WNOHANG) == (0, 0)
        except ChildProcessError:
            return False

    def screen_tail(self, n=12):
        tail = [l.strip() for l in self.text().splitlines() if l.strip()]
        return " / ".join(tail[-n:])[:1500]

    def gone_within(self, secs):
        end = time.time() + secs
        while time.time() < end:
            if not self.alive():
                return True
            self.pump(0.5)  # keep draining, or an exiting process blocks on the terminal
        return not self.alive()

    def close(self):
        for s in ("\x1b", "\x03", "\x03"):
            try:
                self.send(s)
            except OSError:
                break
            self.pump(1)
        if not self.gone_within(5):
            os.kill(self.pid, signal.SIGTERM)
            if not self.gone_within(5):
                os.kill(self.pid, signal.SIGKILL)
                self.gone_within(3)
        try:
            os.close(self.fd)
        except OSError:
            pass
        try:
            os.waitpid(self.pid, 0)
        except ChildProcessError:
            pass


def trust(t):
    for _ in range(20):
        t.pump(1)
        if "trust" in t.text().lower():
            say("   (the folder's trust question: answered Yes, with Down and Enter)")
            t.send("\x1b[B")
            t.pump(1)
            t.send("\r")
            t.buf = ""
            t.pump(3)
            return
    say("   (no trust question within 20 s)")


def ask(t, sid, prompt):
    before = len(tlines(sid))
    t.type(prompt)
    model, text = wait_assistant(sid, before)
    say("   asked %r: reply %r, message.model=%s" % (prompt, (text or "")[:120], model))
    return model


def perm_lines(sid):
    return [l.get("permissionMode") for l in tlines(sid) if l.get("type") == "permission-mode"]


def step_x(sid):
    say("## X a plain session for the /resume picker: claude --session-id %s --model haiku" % sid)
    seen = len(hooks())
    t = Term(["claude", "--session-id", sid, "--model", "haiku", "--settings", SETTINGS])
    trust(t)
    t.pump(5)
    ask(t, sid, "Reply with the one word alpha.")
    seen = show_hooks(seen)
    t.close()
    say("## X closed; process %s" % ("alive" if t.alive() else "ended"))


def step_a1(sid):
    mode = os.environ.get("MODE", "acceptEdits")
    say("## A1 claude --session-id %s --model haiku --permission-mode %s --add-dir <dir2>" % (sid, mode))
    seen = len(hooks())
    t = Term(["claude", "--session-id", sid, "--model", "haiku", "--permission-mode", mode, "--add-dir", DIR2, "--settings", SETTINGS])
    trust(t)
    t.pump(5)
    ask(t, sid, "Reply with the one word pong.")
    seen = show_hooks(seen)
    say("   transcript permission-mode lines: %s" % perm_lines(sid))
    say("   screen: %s" % t.screen_tail(6))
    t.close()
    say("## A1 closed; process %s" % ("alive" if t.alive() else "ended"))


def step_a2(sid, other):
    say("## A2 claude --resume %s, no flags but --settings (the probe hook)" % sid)
    seen = len(hooks())
    t = Term(["claude", "--resume", sid, "--settings", SETTINGS])
    trust(t)
    t.pump(6)
    seen = show_hooks(seen)
    say("   screen at start: %s" % t.screen_tail(8))
    t.buf = ""
    t.type("/status")
    t.pump(4)
    say("   /status screen: %s" % t.screen_tail(30))
    t.send("\x1b")
    t.pump(2)
    t.buf = ""
    model = ask(t, sid, "Reply with the one word ping.")
    say("   transcript permission-mode lines now: %s" % perm_lines(sid))
    t.buf = ""
    before = len(tlines(sid))
    t.type("List the directories you are allowed to access besides the current one, as bare paths, or say none.")
    m, text = wait_assistant(sid, before)
    say("   asked about added folders: reply %r" % (text or "")[:300])
    say("   screen footer: %s" % t.screen_tail(4))
    # B: /resume inside this running session, picking the X session
    say("## B /resume typed in the running session (process %d), picking session %s" % (t.pid, other))
    t.buf = ""
    t.type("/resume")
    t.pump(4)
    say("   picker screen: %s" % t.screen_tail(20))
    for ch in "alpha":
        t.send(ch)
        time.sleep(0.05)
    t.pump(2)
    say("   picker after typing alpha: %s" % t.screen_tail(12))
    t.send("\r")
    t.pump(8)
    seen = show_hooks(seen)
    say("   screen after the pick: %s" % t.screen_tail(8))
    say("   the driver's child (pid %d) is %s" % (t.pid, "alive" if t.alive() else "gone"))
    ps = subprocess.run(["ps", "-ww", "-o", "pid=,lstart=,args=", "-p", str(t.pid)], capture_output=True, text=True).stdout.strip()
    say("   ps of it: %s" % ps)
    t.close()
    say("## A2/B closed; process %s" % ("alive" if t.alive() else "ended"))


def step_c(sid):
    say("## C claude --session-id %s --model haiku, one exchange, then /background" % sid)
    seen = len(hooks())
    t = Term(["claude", "--session-id", sid, "--model", "haiku", "--settings", SETTINGS])
    trust(t)
    t.pump(5)
    ask(t, sid, "Reply with the one word gamma.")
    seen = show_hooks(seen)
    t.buf = ""
    t.type("/background")
    for i in range(30):
        t.pump(1)
        if len(hooks()) > seen or not t.alive():
            break
    t.pump(3)
    say("   screen after /background: %s" % t.screen_tail(12))
    seen = show_hooks(seen)
    for i in range(10):
        if not t.alive():
            break
        t.pump(1)
    say("   the old process (pid %d) is %s" % (t.pid, "alive" if t.alive() else "gone"))
    ls = tlines(sid)
    say("   old transcript's last line now: %s" % json.dumps(ls[-1])[:300] if ls else "   no old transcript")
    r = subprocess.run(["claude", "agents", "--json"], cwd=PROJ, env=ENV, capture_output=True, text=True, timeout=60)
    try:
        jobs = [j for j in json.loads(r.stdout) if "cprobe" in (j.get("cwd") or "")]
    except Exception:
        jobs = r.stdout[:500] + r.stderr[:300]
    say("   claude agents --json, this probe's rows: %s" % json.dumps(jobs))
    t.close()
    return jobs


def step_d(sid):
    say("## D claude -p --session-id %s --model haiku 'Reply with the one word delta.'" % sid)
    seen = len(hooks())
    r = subprocess.run(["claude", "-p", "--session-id", sid, "--model", "haiku", "--settings", SETTINGS, "Reply with the one word delta."],
                       cwd=PROJ, env=ENV, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=120)
    say("   exit %d, printed %r" % (r.returncode, (r.stdout + r.stderr).strip()[:200]))
    show_hooks(seen)


def step_dirs(sid, flags):
    say("## dirs claude --resume %s %s: what /permissions lists as working folders" % (sid, " ".join(flags) or "(no flags but --settings)"))
    seen = len(hooks())
    args = ["claude", "--resume", sid] + [DIR2 if f == "DIR2" else f for f in flags] + ["--settings", SETTINGS]
    t = Term(args)
    t.pump(8)
    if "Yes, add it" in t.text() or "Yes,addit" in t.text():
        say("   (the added folder's question: answered Yes, with Down and Enter)")
        t.send("\x1b[B")
        t.pump(1)
        t.send("\r")
        t.pump(5)
    seen = show_hooks(seen)
    t.buf = ""
    t.type("/permissions")
    t.pump(4)
    for i in range(5):
        say("   /permissions tab %d: %s" % (i, t.screen_tail(14)))
        if "dir2" in t.text() or "irector" in t.screen_tail(14):
            pass
        t.buf = ""
        t.send("\x1b[C")  # right arrow, the next tab
        t.pump(2)
    t.send("\x1b")
    t.pump(1)
    t.close()
    say("## dirs closed; process %s" % ("alive" if t.alive() else "ended"))


def step_b(sid, other_word):
    say("## B claude --resume %s, then /resume typed in it, picking the session whose first prompt names %r" % (sid, other_word))
    seen = len(hooks())
    t = Term(["claude", "--resume", sid, "--settings", SETTINGS])
    t.pump(8)
    seen = show_hooks(seen)
    say("   the driver's child, the session's process: %d" % t.pid)
    t.buf = ""
    t.type("/resume")
    t.pump(5)
    for ch in other_word:
        t.send(ch)
        time.sleep(0.1)
    t.pump(4)
    say("   picker after typing %r: %s" % (other_word, t.screen_tail(8)))
    t.send("\r")
    for i in range(3):
        t.pump(6)
        if len(hooks()) > seen:
            break
        say("   no SessionStart yet; screen: %s" % t.screen_tail(6))
        t.send("\r")
    t.pump(3)
    seen = show_hooks(seen)
    say("   screen after the pick: %s" % t.screen_tail(8))
    say("   the driver's child (pid %d) is %s" % (t.pid, "alive" if t.alive() else "gone"))
    ps = subprocess.run(["ps", "-ww", "-o", "pid=,lstart=,args=", "-p", str(t.pid)], capture_output=True, text=True).stdout.strip()
    say("   ps of it: %s" % ps[:200])
    t.close()
    say("## B closed; process %s" % ("alive" if t.alive() else "ended"))


if __name__ == "__main__":
    step = sys.argv[1]
    a = sys.argv[2:]
    {"x": lambda: step_x(a[0]), "a1": lambda: step_a1(a[0]), "a2": lambda: step_a2(a[0], a[1]),
     "c": lambda: step_c(a[0]), "b": lambda: step_b(a[0], a[1]), "dirs": lambda: step_dirs(a[0], a[1:]), "d": lambda: step_d(a[0])}[step]()
