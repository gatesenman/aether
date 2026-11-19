"""Agent protocol: structured, verifiable edits on Aether programs.
An agent (LLM or human tool) never rewrites text blindly; it sends a list of operations which are
applied to the AST-anchored source, re-checked, and returned as a proposal with a diff."""
import json, os, re, difflib, urllib.request
from .parser import parse
from .lexer import lex
from .checker import check_source
from .errors import AeError

OPS_SCHEMA = {
  "add_decl": {"code": "Aether source of one or more declarations"},
  "replace_decl": {"name": "declaration name", "code": "new source"},
  "remove_decl": {"name": "declaration name"},
  "add_field": {"entity": "Entity", "field": "name", "type": "Text|Int|Float|Bool|...", "default": "optional Aether expression"},
  "remove_field": {"entity": "Entity", "field": "name"},
  "rename": {"old": "identifier", "new": "identifier"},
  "add_view_item": {"view": "View name", "item": "e.g. list Todo / stat \"总数\" count(Todo) / button \"X\" -> action"},
  "set_effects": {"fn": "function name", "effects": ["io", "db"]},
}

def _find(src, name):
    for d in parse(src)["decls"]:
        if d.get("name") == name: return d
    raise AeError("A001", f"no declaration named '{name}'")

def apply_op(src, op):
    t = op.get("op")
    if t == "add_decl":
        parse(op["code"]); return src.rstrip() + "\n\n" + op["code"].strip() + "\n"
    if t in ("replace_decl", "remove_decl"):
        d = _find(src, op["name"]); a, b = d["span"]
        return src[:a] + (op["code"].strip() if t == "replace_decl" else "") + src[b:]
    if t in ("add_field", "add_view_item"):
        d = _find(src, op["entity"] if t == "add_field" else op["view"]); a, b = d["span"]
        line = f'{op["field"]}: {op["type"]}' + (f' = {op["default"]}' if op.get("default") not in (None, "") else "") if t == "add_field" else op["item"]
        close = src.rindex("}", a, b)
        before = src[:close].rstrip()
        return before + "\n  " + line + "\n" + src[close:]
    if t == "remove_field":
        d = _find(src, op["entity"]); a, b = d["span"]
        body = re.sub(rf'(?m)^[ \t]*{re.escape(op["field"])}\s*:[^\n]*\n?', "", src[a:b], count=1)
        return src[:a] + body + src[b:]
    if t == "rename":
        out = []; last = 0
        for tok in lex(src):
            if tok.kind == "id" and tok.val == op["old"]:
                out.append(src[last:tok.pos]); out.append(op["new"]); last = tok.pos + len(op["old"])
            elif tok.kind == "str" and "{" in src[tok.pos:tok.pos + 2 + len(tok.val) * 2]:
                end = tok.pos + 1
                while src[end] != '"': end += 2 if src[end] == "\\" else 1
                raw = src[tok.pos:end + 1]
                fixed = re.sub(r"\{[^{}]*\}", lambda m: re.sub(rf"\b{re.escape(op['old'])}\b", op["new"], m.group()), raw)
                out.append(src[last:tok.pos]); out.append(fixed); last = end + 1
        out.append(src[last:]); return "".join(out)
    if t == "set_effects":
        d = _find(src, op["fn"]); a, b = d["span"]
        seg = src[a:b]; brace = seg.index("{")
        head = re.sub(r"\s*!\s*[\w\s,]+$", "", seg[:brace].rstrip())
        return src[:a] + head + " ! " + ", ".join(op["effects"]) + " " + seg[brace:] + src[b:]
    raise AeError("A002", f"unknown op '{t}'")

def apply_ops(src, ops):
    new = src
    for op in ops: new = apply_op(new, op)
    ast, diags = check_source(new)
    return {"source": new, "diagnostics": diags, "ok": not any(d["severity"] == "error" for d in diags),
            "diff": "".join(difflib.unified_diff(src.splitlines(True), new.splitlines(True), "before.ae", "after.ae"))}

# ---------------- proposers
LANG_BRIEF = """Aether is a declarative+functional language. Declarations: `entity Name { field: Type = default }` (persisted table + REST API),
`action name(x: Entity) { ... }` (server mutation; entity params are loaded & saved automatically),
`view Name { title "..." | text expr | stat "label" expr | form Entity | list Entity [labelField] | table Entity | chart Entity numField | button "label" -> action }`,
`fn name(a: T) -> T ! effects { expr }`, `type T = A(x: Int) | B`, `match`, `if`, `for x in xs {}`, lambdas `fn(x) => x+1`, pipe `|>`,
stdlib: all(E), where(E, f), count(E), insert(E, {..}), save(E, r), remove(E, r), map, filter, sum, len, print."""

def mock_propose(src, prompt):
    """Deterministic rule-based 'agent' so the collaboration loop works without any API key."""
    ast, diags = check_source(src); ents = [d["name"] for d in (ast or {"decls": []})["decls"] if d["k"] == "Entity"]
    views = [d["name"] for d in (ast or {"decls": []})["decls"] if d["k"] == "View"]
    p = prompt.strip(); ops = []
    m = re.search(r"(?:rename|把)\s*(\w+)\s*(?:to|改名为|改成|重命名为)\s*(\w+)", p, re.I)
    if m: return {"summary": f"重命名 {m.group(1)} → {m.group(2)}", "ops": [{"op": "rename", "old": m.group(1), "new": m.group(2)}]}
    if re.search(r"fix|修复|修正|错误", p, re.I):
        for d in diags:
            if d["code"] == "W300":
                fn = re.search(r"function '(\w+)'", d["message"]).group(1)
                eff = re.search(r"`! ([^`]+)`", d["hint"]).group(1).split(", ")
                ops.append({"op": "set_effects", "fn": fn, "effects": eff})
            if d["code"] == "E200" and d.get("hint"):
                bad = re.search(r"'(\w+)'", d["message"]).group(1); good = re.search(r"'(\w+)'", d["hint"]).group(1)
                ops.append({"op": "rename", "old": bad, "new": good})
        return {"summary": f"根据诊断自动修复 {len(ops)} 处" if ops else "没有可自动修复的问题", "ops": ops}
    fields = re.findall(r"(\w+)\s*[:：]\s*(Text|Int|Float|Bool|Date)", p)
    target = next((e for e in ents if re.search(rf"\b{e}\b", p)), None)
    m = re.search(r"(?:entity|实体|模型|表)\s*(\w+)|(?:add|新增|添加|创建)\s+([A-Z]\w*)", p)
    newent = (m.group(1) or m.group(2)) if m else None
    if newent and newent not in ents:
        fs = fields or [("name", "Text")]
        code = f"entity {newent} {{\n" + "".join(f"  {n}: {t}\n" for n, t in fs) + "}\n\nview " + newent + "s {\n  title \"" + newent + "\"\n  form " + newent + "\n  list " + newent + "\n}"
        return {"summary": f"新增实体 {newent}（自动获得数据表、API 和页面）", "ops": [{"op": "add_decl", "code": code}]}
    if fields and (target or ents):
        e = target or ents[0]
        return {"summary": f"给 {e} 添加字段 " + ", ".join(n for n, _ in fields),
                "ops": [{"op": "add_field", "entity": e, "field": n, "type": t} for n, t in fields]}
    if re.search(r"统计|stat|count|数量", p, re.I) and ents and views:
        e = target or ents[0]
        return {"summary": f"在 {views[0]} 页面添加 {e} 数量统计", "ops": [{"op": "add_view_item", "view": views[0], "item": f'stat "{e} 总数" count({e})'}]}
    return {"summary": "Mock 模式未能理解该请求。可尝试：『给 Todo 添加字段 due: Text』『新增实体 Note title: Text』『修复错误』『把 Todo 改名为 Task』『添加统计』；或设置 AETHER_LLM_API_KEY 启用真实大模型。", "ops": []}

def llm_propose(src, prompt):
    key = os.environ["AETHER_LLM_API_KEY"]
    base = os.environ.get("AETHER_LLM_BASE_URL", "https://api.openai.com/v1").rstrip("/")
    model = os.environ.get("AETHER_LLM_MODEL", "gpt-4o-mini")
    _, diags = check_source(src)
    sys_p = (LANG_BRIEF + "\nYou edit programs ONLY via JSON operations. Schema: " + json.dumps(OPS_SCHEMA) +
             '\nReply with JSON only: {"summary": "中文说明", "ops": [{"op": "...", ...}]}')
    user = f"Program:\n```\n{src}\n```\nDiagnostics: {json.dumps(diags, ensure_ascii=False)}\nRequest: {prompt}"
    req = urllib.request.Request(base + "/chat/completions", method="POST",
        headers={"Authorization": f"Bearer {key}", "Content-Type": "application/json"},
        data=json.dumps({"model": model, "messages": [{"role": "system", "content": sys_p}, {"role": "user", "content": user}], "temperature": 0.2}).encode())
    txt = json.loads(urllib.request.urlopen(req, timeout=60).read())["choices"][0]["message"]["content"]
    txt = re.sub(r"^```(?:json)?|```$", "", txt.strip(), flags=re.M)
    return json.loads(txt[txt.index("{"): txt.rindex("}") + 1])

def propose(src, prompt):
    mode = "llm" if os.environ.get("AETHER_LLM_API_KEY") else "mock"
    try: p = llm_propose(src, prompt) if mode == "llm" else mock_propose(src, prompt)
    except Exception as ex:
        p = mock_propose(src, prompt); p["summary"] = f"[LLM 调用失败，已回退到 mock: {ex}] " + p["summary"]; mode = "mock"
    try: res = apply_ops(src, p.get("ops", []))
    except AeError as ex: res = {"source": src, "diagnostics": [ex.to_json()], "ok": False, "diff": ""}
    return {"mode": mode, "summary": p.get("summary", ""), "ops": p.get("ops", []), **res}
