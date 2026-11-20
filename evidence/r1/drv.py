"""A small pty driver for the probes: start a CLI in a pseudo-terminal, read its screen, type to it."""
import json, os, pty, re, select, signal, subprocess, sys, time, glob

HOME = os.environ["HOME"]
SP = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.join(SP, "repo")
LOG = None
ENTER = "\r"  # Codex turns on the kitty keyboard protocol, and takes Enter only as CSI 13 u
ANSI = re.compile(r"\x1b\[[0-9;?<>=]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][A-Za-z0-9]|\x1b[=>78]|\x1bP[^\x1b]*\x1b\\")
BASE_ENV = {
    "HOME": HOME, "USER": os.environ.get("USER", ""), "TERM": "xterm-256color", "LANG": "en_US.UTF-8", "SHELL": "/bin/zsh",
    "PATH": ":".join([HOME + "/.nvm/versions/node/v24.11.1/bin", HOME + "/.local/bin", HOME + "/go/bin", "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"]),
    "HANDOFF_TEST_NOTIFY": os.path.join(SP, "alerts.txt"),
}

def clean(s):
    return s.replace(HOME, "~").replace(os.environ.get("USER", "\0"), "<user>")

def say(msg):
    line = time.strftime("%H:%M:%S", time.gmtime()) + " " + clean(str(msg))
    print(line, flush=True)
    if LOG:
        with open(LOG, "a") as f:
            f.write(line + "\n")

def hooklog():
    try:
        with open(os.path.join(SP, "hooklog.jsonl")) as f:
            return [json.loads(l) for l in f if l.strip()]
    except FileNotFoundError:
        return []

class Term:
    def __init__(self, args, env, cwd=REPO, rows=50, cols=160):
        self.args = args
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(cwd)
            os.execvpe(args[0], args, env)
        import fcntl, termios, struct
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        self.buf = ""
        self.status = None

    def pump(self, secs):
        end = time.time() + secs
        while time.time() < end:
            r, _, _ = select.select([self.fd], [], [], 0.2)
            if r:
                try:
                    data = os.read(self.fd, 65536)
                except OSError:
                    return False
                if not data:
                    return False
                self.buf += data.decode("utf-8", "replace")
        return True

    def text(self):
        return re.sub(r"[ \t]+", " ", ANSI.sub(" ", self.buf))

    def wait_for(self, pat, secs):
        end = time.time() + secs
        while time.time() < end:
            if re.search(pat, self.text(), re.I):
                return True
            if not self.pump(0.5):
                return bool(re.search(pat, self.text(), re.I))
        return False

    def send(self, s):
        os.write(self.fd, s.encode())

    def type(self, s, enter=True):
        for ch in s:
            self.send(ch)
            self.pump(0.03)
        if enter:
            self.pump(3)
            self.send(ENTER)

    def alive(self):
        if self.status is not None:
            return False
        try:
            pid, st = os.waitpid(self.pid, os.WNOHANG)
        except ChildProcessError:
            self.status = -1
            return False
        if pid == 0:
            return True
        self.status = st
        return False

    def tail(self, n=12):
        lines = [l.strip() for l in self.text().splitlines() if l.strip()]
        return " / ".join(lines[-n:])[:1500]

    def close(self, keys=("\x03", "\x03", "\x04")):
        for s in keys:
            if not self.alive():
                break
            try:
                self.send(s)
            except OSError:
                break
            self.pump(1.5)
        for sig, wait in ((signal.SIGTERM, 3), (signal.SIGKILL, 1)):
            if self.alive():
                os.kill(self.pid, sig)
                time.sleep(wait)
        self.alive()
        try:
            os.close(self.fd)
        except OSError:
            pass
