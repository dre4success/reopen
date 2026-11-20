import os, pty, select, sys, time, re, fcntl, termios, struct
D = sys.argv[1]; proj = sys.argv[2]; keys = sys.argv[3:]
env = {"HOME": D + "/home", "PATH": D + "/bin:/opt/homebrew/bin:/usr/bin:/bin", "TERM": "xterm-256color",
       "LANG": "en_US.UTF-8", "TERM_PROGRAM": "iTerm.app", "REOPEN_TEST_OSASCRIPT": D + "/bin/osascript"}
pid, fd = pty.fork()
if pid == 0:
    os.chdir(proj)
    os.execve(D + "/bin/reopen", ["reopen"], env)
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 110, 0, 0))
def pump(t):
    out = b""; end = time.time() + t
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.1)
        if r:
            try: out += os.read(fd, 65536)
            except OSError: break
    return out.decode("utf-8", "replace")
screens = [pump(1.0)]
for k in keys:
    os.write(fd, k.encode().decode("unicode_escape").encode())
    screens.append(pump(0.6))
screens.append(pump(1.0))
os.waitpid(pid, 0)
for i, s in enumerate(screens):
    print(f"=== screen after {'start' if i == 0 else repr(keys[i-1]) if i <= len(keys) else 'end'} ===")
    print(s)
