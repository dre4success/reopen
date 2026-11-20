#!/usr/bin/env python3
"""reopen R1 end-to-end check with a real Claude Code session (Haiku), in a scratch git repo.

The session's only extra hook is reopen's own, added through --settings, run with HOME set to a
scratch home, so the real ~/.reopen/ is never touched. Nothing opens a window: reopen --print gives
the command, and the driver runs that command itself in a pseudo-terminal.
"""
import glob, json, os, pty, re, select, signal, subprocess, sys, time, uuid

S = os.path.dirname(os.path.abspath(__file__))
PROJ = os.path.join(S, "proj")
RHOME = os.path.join(S, "rhome")  # reopen's home
REOPEN = os.path.join(S, "reopen")
HOME = os.environ["HOME"]
ENV = {
    "HOME": HOME, "USER": os.environ.get("USER", ""), "TERM": "xterm-256color",
    "LANG": "en_US.UTF-8", "SHELL": "/bin/zsh",
    "PATH": HOME + "/.local/bin:" + HOME + "/go/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
    "HANDOFF_TEST_NOTIFY": os.path.join(S, "alerts.txt"),
}
HOOK = {"type": "command", "command": "/usr/bin/env", "args": ["HOME=" + RHOME, REOPEN, "hook", "claude"]}
SETTINGS = json.dumps({"hooks": {"SessionStart": [{"hooks": [HOOK]}]}})
ANSI = re.compile(r"\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][A-Za-z0-9]|\x1b[=>]")


def say(msg):
    print(time.strftime("%H:%M:%S", time.gmtime()), msg, flush=True)


def reopen(*args):
    # reopen's own home is the scratch one; the CLI's data stays where Claude Code keeps it.
    env = dict(ENV, HOME=RHOME, CLAUDE_CONFIG_DIR=os.path.join(HOME, ".claude"))
    r = subprocess.run([REOPEN] + list(args), cwd=PROJ, env=env, capture_output=True, text=True, timeout=60)
    say("$ reopen %s  -> exit %d" % (" ".join(args), r.returncode))
    for l in r.stdout.splitlines():
        say("   out: " + l)
    for l in r.stderr.splitlines():
        say("   err: " + l)
    return r


def entries():
    out = []
    for p in glob.glob(os.path.join(RHOME, ".reopen", "*.json")):
        out += json.load(open(p))["entries"]
        say("   list file %s mode %o" % (os.path.basename(p), os.stat(p).st_mode & 0o777))
    return out


class Term:
    def __init__(self, args):
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

    def close(self):
        try:
            self.type("/exit")
        except OSError:
            pass
        end = time.time() + 10
        while time.time() < end and self.alive():
            self.pump(0.5)
        for s in ("\x1b", "\x03", "\x03"):
            try:
                self.send(s)
            except OSError:
                break
            self.pump(1)
        end = time.time() + 8
        while time.time() < end and self.alive():
            self.pump(0.5)
        if self.alive():
            os.kill(self.pid, signal.SIGTERM)
            time.sleep(2)
        try:
            os.waitpid(self.pid, os.WNOHANG)
        except ChildProcessError:
            pass


def trust(t):
    for _ in range(20):
        t.pump(1)
        if "trust" in t.text().lower():
            say("   (the folder's trust question: answered Yes, with Down and Enter)")
            t.send("\x1b[B"); t.pump(1); t.send("\r"); t.buf = ""; t.pump(3)
            return


def transcript(sid):
    f = glob.glob(os.path.join(HOME, ".claude", "projects", "*", sid + ".jsonl"))
    return f[0] if f else None


def wait_reply(sid, n_before, secs=90):
    end = time.time() + secs
    while time.time() < end:
        p = transcript(sid)
        if p:
            ls = [json.loads(l) for l in open(p) if l.strip()]
            for l in ls[n_before:]:
                if l.get("type") == "assistant":
                    return True
        time.sleep(1)
    return False


def nlines(sid):
    p = transcript(sid)
    return sum(1 for _ in open(p)) if p else 0


def modes(sid):
    p = transcript(sid)
    return [json.loads(l).get("permissionMode") for l in open(p) if '"permission-mode"' in l] if p else []

def footer(t):
    tail = [l.strip() for l in t.text().splitlines() if l.strip()]
    return [l for l in tail[-8:] if "bypass" in l.lower() or "permission" in l.lower() or "accept" in l.lower()][-1:] or tail[-1:]

sid = str(uuid.uuid4())
say("## 1. a new session: claude --session-id %s --model haiku --permission-mode bypassPermissions, with reopen's hook (reopen at %s)" % (sid[:8], os.environ.get("REOPEN_COMMIT", "?")))
t = Term(["claude", "--session-id", sid, "--model", "haiku", "--permission-mode", "bypassPermissions", "--settings", SETTINGS])
trust(t)
t.pump(4)
say("   footer: %s" % footer(t))
n = nlines(sid)
t.type("Reply with the one word alpha.")
say("   reply written: %s" % wait_reply(sid, n))
say("   the transcript's permission-mode lines: %s" % modes(sid))
e = entries()
say("   entries: %s" % json.dumps([{k: x[k] for k in ("cli", "id", "pid", "args")} for x in e]))
say("## 2. while it runs")
reopen("--print")
reopen("list")
t.close()
say("## 3. closed (process %s)" % ("alive" if t.alive() else "ended"))
time.sleep(2)
r = reopen("--print")
reopen("list")
line = r.stdout.strip()
if not line:
    say("## stop: nothing printed"); sys.exit(1)
say("## 4. the printed line, as a tab types it: /bin/zsh -f -c <line>, plus --settings with reopen's hook (the operator's user settings would have it)")
t2 = Term(["/bin/zsh", "-f", "-c", line + " --settings " + "'" + SETTINGS + "'"])
trust(t2)
t2.pump(8)
say("   footer at the resume: %s" % footer(t2))
reopen("--print")
n = nlines(sid)
t2.type("Reply with the one word beta.")
say("   reply written after the resume: %s" % wait_reply(sid, n))
say("   the transcript's permission-mode lines now: %s" % modes(sid))
say("   footer after a turn: %s" % footer(t2))
t2.close()
say("## 5. closed again")
time.sleep(2)
reopen("--print")
reopen("list")
say("## done: session %s" % sid)
