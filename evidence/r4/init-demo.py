"""Drives reopen init in a pseudo-terminal, as the operator would: it waits
for the question, then types the answer. Usage:

    python3 -I init-demo.py <reopen> <home> <path> <answer>

<answer> is "enter", "ctrl-d" or "none" (init asks nothing). init runs with
only HOME, PATH, TERM and NO_COLOR set, so no agent session's variables
reach it. It prints what the terminal showed.
"""

import os
import pty
import select
import sys
import time

reopen, home, path, answer = sys.argv[1:5]
keys = {"enter": b"\r", "ctrl-d": b"\x04", "none": b""}[answer]
env = {"HOME": home, "PATH": path, "TERM": "xterm-256color", "NO_COLOR": "1"}

pid, fd = pty.fork()
if pid == 0:
    os.execve(reopen, [reopen, "init"], env)

out = b""
typed = False
deadline = time.time() + 20
while time.time() < deadline:
    r, _, _ = select.select([fd], [], [], 0.2)
    if r:
        try:
            chunk = os.read(fd, 4096)
        except OSError:
            break
        if not chunk:
            break
        out += chunk
    if not typed and b"[Y/n] " in out and keys:
        time.sleep(0.2)
        os.write(fd, keys)
        typed = True
_, status = os.waitpid(pid, 0)
sys.stdout.write(out.decode().replace("\r\n", "\n"))
print(f"(exit {os.waitstatus_to_exitcode(status)})")
