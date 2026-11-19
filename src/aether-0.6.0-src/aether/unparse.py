"""Expression/AST -> canonical Aether text (the 'text projection' of the IR)."""
def ex(e):
    if e is None: return ""
    k = e["k"]
    if k == "Num": return str(e["v"])
    if k == "Bool": return "true" if e["v"] else "false"
    if k == "Nil": return "nil"
    if k == "Str": return '"' + "".join(p if isinstance(p, str) else "{" + ex(p) + "}" for p in e["parts"]) + '"'
    if k == "Name": return e["name"]
    if k == "List": return "[" + ", ".join(ex(x) for x in e["items"]) + "]"
    if k == "Record": return "{" + ", ".join(f"{a}: {ex(b)}" for a, b in e["fields"]) + "}"
    if k == "Unary": return ("-" if e["op"] == "-" else "not ") + ex(e["e"])
    if k == "Bin": return f"{ex(e['l'])} {e['op']} {ex(e['r'])}"
    if k == "Pipe": return f"{ex(e['l'])} |> {ex(e['r'])}"
    if k == "Call": return ex(e["fn"]) + "(" + ", ".join(ex(a) for a in e["args"]) + ")"
    if k == "Field": return ex(e["obj"]) + "." + e["name"]
    if k == "Index": return ex(e["obj"]) + "[" + ex(e["idx"]) + "]"
    if k == "Lambda": return "fn(" + ", ".join(p["name"] for p in e["params"]) + ") => …"
    if k == "If": return f"if {ex(e['cond'])} {{…}}"
    if k == "Match": return f"match {ex(e['subj'])} {{…}}"
    if k in ("Spawn", "Await"): return k.lower() + " " + ex(e["e"])
    return "…"
def ty(t):
    if not t: return "Any"
    s = t["name"] + ("<" + ", ".join(ty(a) for a in t["args"]) + ">" if t["args"] else "")
    return s + ("?" if t.get("optional") else "")
