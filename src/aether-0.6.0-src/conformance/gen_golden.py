"""Generate conformance golden data from the Python reference implementation (the executable spec).
The native runtime must reproduce every case:  aether conform conformance/golden.json"""
import json, os, sys, glob, subprocess
HERE = os.path.dirname(os.path.abspath(__file__)); ROOT = os.path.dirname(HERE)
sys.path.insert(0, ROOT)
from aether.checker import check_source
from aether.interp import Interpreter, AeThrow, show
from aether.errors import AeError
from aether.app import App
from aether.history import ast_hash
from aether.explain import explain
from aether.fmt import fmt
from aether import agent

def rel(p): return os.path.relpath(p, ROOT)
def strip_ms(o):
    if isinstance(o, dict): return {k: strip_ms(v) for k, v in o.items() if k != "ms"}
    if isinstance(o, list): return [strip_ms(x) for x in o]
    return o

import re
def norm_err(s):
    m = re.search(r"error (\w+): (.*)", s) or re.search(r"(uncaught) error: (.*)", s)
    return f"{m.group(1)}: {m.group(2)}" if m else ""
cases = []
progs = sorted(glob.glob(os.path.join(HERE, "programs", "*.ae")))
examples = sorted(glob.glob(os.path.join(ROOT, "examples", "*.ae")))
for p in progs + examples:
    src = open(p, encoding="utf-8").read()
    # run
    r = subprocess.run([sys.executable, "-m", "aether", "run", p], capture_output=True, text=True, cwd=ROOT)
    cases.append({"kind": "run", "file": rel(p), "expect": {"stdout": r.stdout, "exit": r.returncode, "error": norm_err(r.stderr)}})
    cases.append({"kind": "check", "file": rel(p), "expect": check_source(src)[1]})
    cases.append({"kind": "hash", "file": rel(p), "expect": ast_hash(src)})
    try:
        cases.append({"kind": "fmt", "file": rel(p), "expect": fmt(src)})
        cases.append({"kind": "explain", "file": rel(p), "expect": explain(src)})
    except AeError: pass
    if "c05" not in p and "c04" not in p:
        try:
            it = Interpreter(out=[]); it.load(src, os.path.dirname(p))
            cases.append({"kind": "test", "file": rel(p), "expect": [[t["name"], t["ok"], t["message"]] for t in it.run_tests()]})
        except (AeThrow, AeError): pass

def api(file, steps):
    a = App(open(os.path.join(ROOT, file), encoding="utf-8").read())
    out = []
    for m, path, body, role in steps:
        code, ctype, b = a.handle(m, path, json.dumps(body, ensure_ascii=False).encode() if body is not None else b"", role)
        out.append([code, strip_ms(json.loads(b)) if "json" in ctype else None])
    cases.append({"kind": "api", "file": file, "steps": steps, "expect": out})

api("examples/todo.ae", [["GET", "/api/_meta", None, "user"], ["POST", "/api/Todo", {"title": "买菜"}, "user"],
    ["POST", "/api/Todo", {"title": "写代码", "priority": 3, "done": True}, "user"], ["POST", "/api/Todo", {"title": ""}, "user"],
    ["POST", "/api/Todo", {"title": "x", "priority": 9}, "user"], ["POST", "/api/action/toggle", {"t": 1}, "user"],
    ["POST", "/api/action/bump", {"t": 2}, "user"], ["POST", "/api/action/bump", {"t": 2}, "user"], ["POST", "/api/action/bump", {"t": 2}, "user"],
    ["GET", "/api/Todo", None, "user"], ["POST", "/api/action/clear_done", {}, "user"], ["POST", "/api/action/clear_done", {}, "admin"],
    ["GET", "/api/_meta", None, "user"], ["PUT", "/api/Todo/2", {"title": "改名"}, "user"], ["GET", "/api/Todo/2", None, "user"],
    ["DELETE", "/api/Todo/2", None, "user"], ["GET", "/api/Todo/2", None, "user"], ["POST", "/api/flow/每日整理", {}, "user"],
    ["GET", "/api/_openapi", None, "user"], ["GET", "/api/nothing", None, "user"]])
api("examples/bank.ae", [["POST", "/api/Account", {"owner": "Ada"}, "user"], ["POST", "/api/action/deposit", {"a": 1}, "user"],
    ["POST", "/api/action/withdraw", {"a": 1}, "user"], ["POST", "/api/action/withdraw", {"a": 1}, "user"], ["POST", "/api/action/withdraw", {"a": 1}, "user"],
    ["GET", "/api/Account/1", None, "user"], ["POST", "/api/action/audit_reset", {}, "guest"], ["POST", "/api/action/audit_reset", {}, "auditor"],
    ["POST", "/api/flow/月结", {}, "user"], ["GET", "/api/_meta", None, "user"]])
api("examples/counter.ae", [["POST", "/api/Counter", {"name": "a"}, "user"], ["POST", "/api/Counter", {"name": "b", "value": "5"}, "user"],
    ["POST", "/api/action/inc", {"c": 1}, "user"], ["POST", "/api/action/dec", {"c": 2}, "user"], ["GET", "/api/_meta", None, "user"],
    ["POST", "/api/action/reset_all", {}, "user"], ["GET", "/api/Counter", None, "user"]])
api("examples/blog.ae", [["POST", "/api/Post", {"title": "A", "body": "b", "likes": 2}, "user"], ["POST", "/api/Post", {"title": "B", "body": "c"}, "user"],
    ["POST", "/api/action/like", {"p": 2}, "user"], ["POST", "/api/action/publish", {"p": 1}, "user"], ["GET", "/api/_meta", None, "user"]])

todo = open(os.path.join(ROOT, "examples/todo.ae"), encoding="utf-8").read()
for ops in ([{"op": "add_field", "entity": "Todo", "field": "due", "type": "Text"}],
            [{"op": "rename", "old": "Todo", "new": "Task"}],
            [{"op": "remove_decl", "name": "表格"}, {"op": "add_view_item", "view": "首页", "item": "stat \"总\" count(Todo)"}],
            [{"op": "remove_field", "entity": "Todo", "field": "priority"}],
            [{"op": "set_effects", "fn": "progress", "effects": ["db", "io"]}],
            [{"op": "replace_decl", "name": "toggle", "code": "action toggle(t: Todo) { t.done = true }"}],
            [{"op": "add_decl", "code": "entity Note { body: Text }"}]):
    r = agent.apply_ops(todo, ops)
    cases.append({"kind": "ops", "file": "examples/todo.ae", "ops": ops, "expect": {"source": r["source"], "ok": r["ok"]}})
for prompt in ["给 Todo 添加字段 due: Text", "新增实体 Note title: Text", "把 Todo 改名为 Task", "添加统计", "你好"]:
    os.environ.pop("AETHER_LLM_API_KEY", None)
    p = agent.mock_propose(todo, prompt)
    cases.append({"kind": "propose", "file": "examples/todo.ae", "prompt": prompt, "expect": {"summary": p["summary"], "ops": p["ops"]}})
p = agent.mock_propose("fn f(x: Int) { print(x) }", "修复错误")
cases.append({"kind": "propose", "source": "fn f(x: Int) { print(x) }", "prompt": "修复错误", "expect": {"summary": p["summary"], "ops": p["ops"]}})

json.dump(cases, open(os.path.join(HERE, "golden.json"), "w", encoding="utf-8"), ensure_ascii=False, indent=1)
print(f"{len(cases)} conformance cases written")
