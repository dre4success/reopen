import json, os, subprocess, sys, time, hashlib
D = sys.argv[1]
H = os.path.join(D, "home")
proj = os.path.join(D, "work", "reopen")
os.makedirs(proj, exist_ok=True)
subprocess.run(["git", "init", "-q"], cwd=proj, check=True)
subprocess.run(["git", "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "--allow-empty", "-m", "x"], cwd=proj, check=True)
os.makedirs(os.path.join(proj, "cmd"), exist_ok=True)
repo = os.path.realpath(os.path.join(proj, ".git"))
proj = os.path.realpath(proj)
now = time.time()
def w(p, data, age):
    os.makedirs(os.path.dirname(p), exist_ok=True)
    open(p, "w").write(data)
    os.utime(p, (now - age, now - age))
A, B, C, E, F = ("a1b2c3d4-1111-4111-8111-111111111111", "019a6a3f-2222-7222-8222-222222222222",
                 "c9d8e7f6-3333-4333-8333-333333333333", "e5f6a7b8-5555-4555-8555-555555555555", "f0e1d2c3-6666-4666-8666-666666666666")
u = lambda t: json.dumps({"type": "user", "message": {"role": "user", "content": t}})
w(f"{H}/.claude/projects/-x/{A}.jsonl", "\n".join([json.dumps({"type":"permission-mode","permissionMode":"bypassPermissions"}), u("build reopen from docs/DESIGN.md"), "x"*4_400_000]) + "\n", 3*3600)
w(f"{H}/.codex/sessions/2026/11/19/rollout-2026-11-19T10-00-00-{B}.jsonl", "\n".join([
    json.dumps({"type":"session_meta","payload":{"id":B,"originator":"codex-tui"}}),
    json.dumps({"type":"turn_context","payload":{"model":"gpt-6.2-sol","approval_policy":"on-request","sandbox_policy":{"type":"workspace-write"}}}),
    json.dumps({"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"you're the reviewer: review R1 hard, especially open-or-not"}]}})]) + "\n" + "y"*12_000_000 + "\n", 26*3600)
w(f"{H}/.gemini/antigravity-cli/brain/{C}/.system_generated/logs/transcript.jsonl", json.dumps({"type":"USER_INPUT","source":"USER_EXPLICIT","content":"<USER_REQUEST>\nyou're reviewer-3: try to break the picker\n</USER_REQUEST>"}) + "\n", 3*86400)
w(f"{H}/.claude/projects/-x/{E}.jsonl", u("long migration of the store") + "\n", 40*60)
w(f"{H}/.claude/projects/-x/{F}.jsonl", u("old idea, \u001b[31mred\u001b[0m text from a transcript") + "\n", 6*86400)
os.makedirs(os.path.join(proj, ".handoff", "sessions"), exist_ok=True)
for sid, role in ((A, "implementer"), (B, "reviewer"), (C, "reviewer-3")):
    json.dump({"v":1,"role":role,"session":sid,"pid":1}, open(os.path.join(proj, ".handoff", "sessions", sid + ".json"), "w"))
seen = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now - 7200))
E_ = lambda cli, sid, folder, args, role="": {"cli": cli, "id": sid, "folder": folder, "pid": 1, "args": args, "last_seen": seen, **({"role": role} if role else {})}
lst = {"v": 1, "repo": repo, "entries": [
    E_("claude", A, proj, ["claude", "--dangerously-skip-permissions"]),
    E_("codex", B, proj, ["codex", "app-server", "--managed-daemon"]),
    E_("antigravity", C, proj, ["agy", "--model", "gemini-4-neon-high"]),
    E_("claude", E, os.path.join(proj, "cmd"), ["claude"], "reviewer-2"),
    E_("claude", F, proj, ["claude", "--model", "opus"], "implementer"),
]}
name = "reopen-" + hashlib.sha256(repo.encode()).hexdigest()[:12] + ".json"
os.makedirs(os.path.join(H, ".reopen"), mode=0o700, exist_ok=True)
p = os.path.join(H, ".reopen", name)
json.dump(lst, open(p, "w"), indent=2); os.chmod(p, 0o600)
print(proj)
