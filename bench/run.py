#!/usr/bin/env python3
"""Aether v0.5 agent-friendliness benchmark.

1. Token counts (tiktoken o200k_base and cl100k_base) for the same app in
   Aether text / Aether compact / Aether skeleton / TS+React+Express+SQL / Python FastAPI+SQLAlchemy+Jinja.
2. Generation eval with the built-in (offline) agent: first-try `aether check` pass rate over a prompt set.
3. Self-repair loop against a local mock OpenAI-compatible server that injects one error in round 1.
Writes bench/results.json and bench/results.md.  No numbers are hand-edited.
"""
import json, os, subprocess, sys, tempfile, shutil, threading, http.server, re
import tiktoken

HERE = os.path.dirname(os.path.abspath(__file__))
AE = os.environ.get("AETHER", os.path.join(HERE, "..", "native", "aether"))
ENCS = {n: tiktoken.get_encoding(n) for n in ("o200k_base", "cl100k_base")}


def tok(text):
    return {n: len(e.encode(text)) for n, e in ENCS.items()}


def strip_comments(text, style):
    if style == "c":
        text = re.sub(r"(?m)^\s*//.*\n", "", text)
    elif style == "py":
        text = re.sub(r'(?m)^\s*#.*\n', "", text)
    return text


def ae(*args, inp=None, env=None):
    return subprocess.run([AE, *args], capture_output=True, text=True, input=inp, env=env)


def read(*p):
    return open(os.path.join(HERE, *p), encoding="utf-8").read()


STACKS = {
    "ts_react_express_sql": ["schema.sql", "server.ts", "App.tsx"],
    "python_fastapi_sqlalchemy_jinja": ["main.py", "templates/home.html", "templates/table.html"],
}


def token_bench():
    rows = []
    apps = {"todo": os.path.join(HERE, "apps/todo/app.ae"), "notes": os.path.join(HERE, "apps/notes/app.ae"),
            "crm (no other-stack port)": os.path.join(HERE, "..", "examples/v05/crm_mvc.ae")}
    for name, path in apps.items():
        src = open(path, encoding="utf-8").read()
        r = {"app": name, "aether_text": tok(src), "aether_text_nocomments": tok(strip_comments(src, "c")),
             "aether_compact": tok(ae("compact", path).stdout), "aether_skeleton": tok(ae("skeleton", path).stdout)}
        d = os.path.dirname(path)
        if os.path.exists(os.path.join(d, "server.ts")):
            for stack, files in STACKS.items():
                txt = "".join(read(os.path.relpath(os.path.join(d, f), HERE)) for f in files if os.path.exists(os.path.join(d, f)))
                r[stack] = tok(txt)
        # round-trip check
        c = ae("compact", path).stdout
        with tempfile.NamedTemporaryFile("w", suffix=".ae", delete=False) as f:
            f.write(c)
        e = ae("expand", f.name).stdout
        with tempfile.NamedTemporaryFile("w", suffix=".ae", delete=False) as g:
            g.write(e)
        h = [ae("hash", p).stdout.strip() for p in (path, f.name, g.name)]
        r["roundtrip_ast_equal"] = len(set(h)) == 1
        rows.append(r)
    return rows


PROMPTS = [  # (template, prompt)
    ("todo", "加一个字段 due: Text"), ("todo", "给 Todo 加字段 tag"), ("todo", "add field note: Text to Todo"),
    ("todo", "加一个实体 Tag，字段 name"), ("todo", "add an entity Project with field name"),
    ("todo", "加一个统计 完成数"), ("todo", "加一个表格页面"), ("todo", "add a test for toggle"),
    ("shop", "加一个字段 stock: Int"), ("shop", "给 Product 加字段 sku"), ("shop", "加一个实体 Coupon，字段 code, percent: Int"),
    ("shop", "add a view 报表 with a table of Product"), ("blog", "加一个字段 tags: Text"),
    ("blog", "加一个实体 Comment，字段 body, post_id: Int"), ("blog", "加一个列表页面"),
    ("crm", "给 Customer 加字段 phone"), ("crm", "加一个实体 Note，字段 text, customer_id: Int"),
    ("crm", "加一个动作 归档客户"), ("blank", "做一个记账应用，实体 Expense，字段 item, amount: Int"),
    ("blank", "make a notes app with entity Note title body"),
]


def gen_eval():
    env = {k: v for k, v in os.environ.items() if not k.startswith("AETHER_LLM")}
    res = []
    tdir = os.path.join(HERE, "..", "native", "templates")
    for tpl, prompt in PROMPTS:
        p = ae("propose", os.path.join(tdir, tpl, "main.ae"), prompt, env=env)
        try:
            j = json.loads(p.stdout)
        except Exception:
            j = {"ok": False}
        changed = bool(j.get("ops")) or bool(j.get("diff"))
        res.append({"template": tpl, "prompt": prompt, "check_pass": bool(j.get("ok")), "made_change": changed,
                    "summary": (j.get("summary") or "")[:80]})
    n = len(res)
    return {"agent": "built-in offline rule agent (no LLM available on this box)", "n": n,
            "first_try_check_pass": sum(r["check_pass"] for r in res),
            "first_try_pass_and_changed": sum(r["check_pass"] and r["made_change"] for r in res), "cases": res}


class Mock(http.server.BaseHTTPRequestHandler):
    calls = []

    def log_message(self, *a):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        Mock.calls.append(body)
        # round 1: a plausible LLM mistake (misspelled field); round 2: the fix
        bad = len(Mock.calls) == 1
        ops = [{"op": "add_decl", "code": "action finish(t: Todo) {\n  t.%s = true\n}" % ("dnoe" if bad else "done")}]
        out = {"choices": [{"message": {"role": "assistant", "content": json.dumps({"summary": "add finish", "ops": ops})}}]}
        b = json.dumps(out).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)


def repair_eval():
    srv = http.server.HTTPServer(("127.0.0.1", 0), Mock)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    env = dict(os.environ, AETHER_LLM_BASE_URL="http://127.0.0.1:%d/v1" % srv.server_port, AETHER_LLM_MODEL="mock")
    tdir = os.path.join(HERE, "..", "native", "templates")
    p = ae("propose", os.path.join(tdir, "todo", "main.ae"), "add an action finish that marks a todo done", env=env)
    srv.shutdown()
    try:
        j = json.loads(p.stdout)
    except Exception:
        j = {"ok": False, "raw": p.stdout[-300:] + p.stderr[-300:]}
    sysmsg = Mock.calls[0]["messages"][0]["content"] if Mock.calls else ""
    return {"llm_rounds": len(Mock.calls), "ok_after_repair": bool(j.get("ok")), "summary": j.get("summary"),
            "round2_saw_diagnostic": bool(len(Mock.calls) > 1 and "dnoe" in json.dumps(Mock.calls[1]["messages"])),
            "system_prompt_tokens": tok(sysmsg)}


def main():
    out = {"tokens": token_bench(), "generation": gen_eval(), "self_repair": repair_eval()}
    json.dump(out, open(os.path.join(HERE, "results.json"), "w"), ensure_ascii=False, indent=1)
    L = ["| app | Aether 原文 | 去注释 | compact | skeleton | TS+React+Express+SQL | Python FastAPI+SQLAlchemy+Jinja |", "|---|---|---|---|---|---|---|"]
    for r in out["tokens"]:
        g = lambda k: str(r[k]["o200k_base"]) if k in r else "—"
        L.append(f"| {r['app']} | {g('aether_text')} | {g('aether_text_nocomments')} | {g('aether_compact')} | {g('aether_skeleton')} | {g('ts_react_express_sql')} | {g('python_fastapi_sqlalchemy_jinja')} |")
    L.append("")
    L.append("(o200k_base；cl100k_base 见 results.json)")
    gen = out["generation"]
    L.append(f"\n生成评测（{gen['agent']}）：首次 check 通过 {gen['first_try_check_pass']}/{gen['n']}，其中确实产生修改 {gen['first_try_pass_and_changed']}/{gen['n']}")
    s = out["self_repair"]
    L.append(f"\n自我修复（mock OpenAI 服务注入一次字段拼写错误）：LLM 轮数 {s['llm_rounds']}，修复后通过 {s['ok_after_repair']}，第二轮收到诊断 {s['round2_saw_diagnostic']}，系统提示 {s['system_prompt_tokens']['o200k_base']} tokens")
    open(os.path.join(HERE, "results.md"), "w").write("\n".join(L) + "\n")
    print("\n".join(L))
    print(json.dumps([{k: v for k, v in r.items()} for r in out["tokens"]], ensure_ascii=False))


if __name__ == "__main__":
    main()
