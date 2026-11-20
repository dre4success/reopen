import sys, os, time, json
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import drv
env = dict(drv.BASE_ENV, CODEX_HOME=os.path.join(drv.SP, "codex-home"))
t = drv.Term(["codex", "-m", "gpt-6-luna", "-s", "read-only", "-a", "on-request"], env)
t.wait_for("Ask Codex", 30); t.pump(5)
raw = t.buf
print("modes enabled:", sorted(set(__import__('re').findall(r"\x1b\[[>?=<][0-9;]*[a-zA-Z]", raw)))[:30])
for i, key in enumerate(["\r", "\n", "\x1b[13u", "\x1bOM"]):
    t.buf = ""
    t.type("say hi %d" % i, enter=False)
    time.sleep(1.5)
    t.send(key)
    t.pump(6)
    h = open(os.path.join(drv.SP, "codex-home", "history.jsonl")).read().count("say hi %d" % i)
    print(repr(key), "submitted:", h, "| tail:", drv.clean(t.tail(3))[-300:])
    if h:
        break
t.close()
