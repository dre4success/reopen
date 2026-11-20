import os, pty, select, sys, time, re, fcntl, termios, struct
D, proj, cols, rows = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
steps = sys.argv[5:]   # each: raw keys (escapes decoded); "+" between parts of one step means a 20 ms pause
env = {"HOME": D + "/home", "PATH": D + "/bin:/opt/homebrew/bin:/usr/bin:/bin", "TERM": "xterm-256color",
       "LANG": "en_US.UTF-8", "TERM_PROGRAM": "iTerm.app", "REOPEN_TEST_OSASCRIPT": D + "/bin/osascript"}
pid, fd = pty.fork()
if pid == 0:
    os.chdir(proj)
    os.execve(D + "/bin/reopen", ["reopen"], env)
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
ANSI = re.compile(r"\x1b\[[0-9;?]*[ -/]*[@-~]")
def pump(t):
    out = b""; end = time.time() + t
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.05)
        if r:
            try: out += os.read(fd, 65536)
            except OSError: break
    return out.decode("utf-8", "replace")
def widest(s):
    return max((len(l) for l in ANSI.sub("", s).replace("\r", "").split("\n")), default=0)
def frame(s):
    # the last frame drawn: after the last "erase" (cursor up, clear to end)
    i = s.rfind("\x1b[J")
    return s[i+3:] if i >= 0 else s
s = pump(1.0)
print("== start: widest line %d columns (terminal %d)" % (widest(s), cols))
for st in steps:
    if st.startswith("RESIZE:"):
        _, c, r = st.split(":")
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", int(r), int(c), 0, 0))
        try: os.kill(pid, 28)  # SIGWINCH, as a terminal sends on a resize
        except ProcessLookupError: pass
        s = pump(0.6)
        f = ANSI.sub("", frame(s)).replace("\r", "").rstrip("\n").split("\n")
        print("== after resizing to %sx%s: last frame %d lines; still running: %s" % (c, r, len(f), os.waitpid(pid, os.WNOHANG) == (0, 0)))
        print("\n".join(f))
        continue
    for part in st.split("+"):
        os.write(fd, part.encode().decode("unicode_escape").encode("latin-1"))
        time.sleep(0.02)
    s = pump(0.6)
    alive = os.waitpid(pid, os.WNOHANG) == (0, 0)
    f = ANSI.sub("", frame(s)).replace("\r", "").rstrip("\n").split("\n")
    print("== after %r: widest line %d columns, last frame %d lines (terminal %dx%d); still running: %s" % (st, widest(s), len(f), cols, rows, alive))
    print("\n".join(f))
    if not alive:
        break
