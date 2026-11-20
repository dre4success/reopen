import json, os, subprocess, sys, time
tag = sys.argv[1]
log = sys.argv[2]
outsides = sys.argv[3:]
raw = sys.stdin.read()
try:
    stdin = json.loads(raw)
except Exception:
    stdin = raw
pp = os.getppid()
def psline(pid):
    return subprocess.run(["ps", "-ww", "-o", "pid=,ppid=,lstart=,args=", "-p", str(pid)], capture_output=True, text=True).stdout.strip()
ppline = psline(pp)
gp = ppline.split()[1] if ppline else ""
write = {}
for outside in outsides:
    probe = os.path.join(outside, "w-%d-%d" % (os.getpid(), int(time.time()*1000)))
    try:
        with open(probe, "w") as f:
            f.write("x")
        os.remove(probe)
        write[outside] = "ok"
    except Exception as e:
        write[outside] = "failed: %r" % e
env = {k: ("<set>" if any(w in k for w in ("TOKEN", "KEY", "SECRET", "AUTH")) else v) for k, v in os.environ.items() if k.startswith(("CODEX", "ANTIGRAVITY", "CLAUDE"))}
rec = {"tag": tag, "t": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "stdin": stdin, "cwd": os.getcwd(), "ppid": pp,
       "parent": ppline, "grandparent": psline(gp) if gp else "", "outside_write": write, "env": env}
with open(log, "a") as f:
    f.write(json.dumps(rec) + "\n")
