"""Natural-language projection: deterministic, rule-based Chinese explanation of a program's IR.
Same IR, different projection: text (source), structure (AST), language (this)."""
from .parser import parse
from .unparse import ex, ty

def explain(src):
    prog = parse(src); D = prog["decls"]; out = []
    by = lambda k: [d for d in D if d["k"] == k]
    mod = next((d["name"] for d in by("Module")), "未命名")
    ents, acts, fns, views, flows = by("Entity"), by("Action"), by("Fn"), by("View"), by("Flow")
    tests = by("Test") + by("Example")
    out.append(f"# 程序「{mod}」说明\n")
    out.append(f"概要：{len(ents)} 种数据、{len(acts)} 个动作、{len(fns)} 个函数、{len(views)} 个页面、{len(flows)} 个流程、{len(tests)} 个内嵌测试。\n")
    if ents:
        out.append("## 数据（会自动建表并提供 API）")
        for e in ents:
            fs = "、".join(f"{f['name']}（{ty(f['type'])}" + (f"，默认 {ex(f['default'])}" if f["default"] else "") + "）" for f in e["fields"])
            out.append(f"- **{e['name']}**：包含 {fs}。")
            for c in e.get("invariants", []): out.append(f"  - 不变式：任何时候都必须满足 `{ex(c)}`，否则写入会被拒绝。")
    if acts:
        out.append("\n## 动作（用户可触发的操作）")
        for a in acts:
            ps = "、".join(f"{p['name']}（{ty(p['type'])}）" for p in a["params"]) or "无参数"
            who = f"仅限角色 {' / '.join(a['allow'])}" if a.get("allow") else "所有用户"
            out.append(f"- **{a['name']}**：参数 {ps}；{who}可执行。" + _body(a["body"]))
            for c in a.get("requires", []): out.append(f"  - 前置条件：`{ex(c)}`")
            for c in a.get("ensures", []): out.append(f"  - 后置保证：`{ex(c)}`")
    if fns:
        out.append("\n## 函数")
        for f in fns:
            sig = ", ".join(f"{p['name']}: {ty(p['type'])}" for p in f["params"])
            eff = f"，副作用：{', '.join(f['effects'])}" if f["effects"] else "，无声明副作用（纯计算）"
            out.append(f"- **{f['name']}**({sig}) → {ty(f['ret'])}{eff}。")
            for c in f.get("requires", []): out.append(f"  - 调用前必须满足 `{ex(c)}`")
            for c in f.get("ensures", []): out.append(f"  - 返回后保证 `{ex(c)}`（result 表示返回值）")
    if views:
        out.append("\n## 页面")
        names = {"title": "标题", "text": "文字", "stat": "统计", "form": "新增表单", "list": "列表", "table": "表格", "chart": "图表", "button": "按钮", "section": "小节"}
        for v in views:
            parts = []
            for i in v["items"]:
                arg = " ".join(("→" + a["target"]) if a["k"] == "Arrow" else ex(a) for a in i["args"])
                parts.append(f"{names.get(i['kind'], i['kind'])} {arg}")
            out.append(f"- **{v['name']}**：" + "；".join(parts) + "。")
    if flows:
        out.append("\n## 流程（按顺序执行，任一步失败即停止并记录）")
        for f in flows: out.append(f"- **{f['name']}**：" + " → ".join(str(s["name"]) for s in f["steps"]))
    if tests:
        out.append("\n## 内嵌测试与示例（`aether test` 运行）")
        for t in tests: out.append(f"- " + (f"测试「{t['name']}」" if t["k"] == "Test" else f"示例：`{ex(t['expr'])}`"))
    return "\n".join(out) + "\n"

def _body(b):
    acts = []
    for st in b["body"]:
        if st["k"] == "Assign": acts.append(f"把 {ex(st['target'])} 设为 {ex(st['value'])}")
        elif st["k"] == "For": acts.append(f"对 {ex(st['iter'])} 中的每一项执行操作")
        elif st["k"] == "ExprStmt" and st["expr"]["k"] == "Call": acts.append(f"调用 {ex(st['expr'])}")
    return ("效果：" + "；".join(acts) + "。") if acts else ""
