import sys, os, time, json
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import drv
env = dict(drv.BASE_ENV, CODEX_HOME=os.path.join(drv.SP, "codex-home"))
t = drv.Term(["codex", "-m", "gpt-6-luna", "-s", "read-only", "-a", "on-request"], env)
t.wait_for("Ask Codex", 30); t.pump(5)
for i, (gap, wait) in enumerate([(0.03, 3.0), (0.12, 2.0)]):
    txt = "Reply with the single word ready%d. Do not run any command." % i
    for ch in txt:
        t.send(ch); time.sleep(gap)
    t.pump(wait)
    t.send("\x1b[13u")
    t.pump(8)
    hist = open(os.path.join(drv.SP, "codex-home", "history.jsonl")).read()
    print(gap, wait, "submitted:", txt in hist)
    if txt in hist:
        break
t.close()
