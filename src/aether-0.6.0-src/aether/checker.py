"""Static checker: name resolution, basic type inference, arity, effect tracking, app-model validation.
Produces structured diagnostics (JSON) that agents can consume directly."""
from .parser import parse
from .errors import AeError
from .stdlib import EFFECTS
from .interp import Interpreter

BUILTIN_TYPES = {"Int","Float","Text","String","Bool","List","Map","Record","Any","Nil","Fn","Future","Date","Result","Option"}
VIEW_KINDS = {"title","text","list","table","form","button","stat","chart","section"}

def _builtin_names():
    it = Interpreter(out=[]); return set(it.globals.vars)
BUILTINS = None

class Checker:
    def __init__(s):
        global BUILTINS
        if BUILTINS is None: BUILTINS = _builtin_names()
        s.diags = []; s.fns = {}; s.types = set(BUILTIN_TYPES); s.ctors = {}; s.entities = {}; s.actions = {}; s.globals = set(BUILTINS)
    def d(s, sev, code, msg, line, hint=None):
        x = {"severity": sev, "code": code, "message": msg, "line": line, "col": 1}
        if hint: x["hint"] = hint
        s.diags.append(x)

    def check(s, prog):
        for d in prog["decls"]:
            k = d["k"]
            if k == "Entity": s.entities[d["name"]] = d; s.types.add(d["name"]); s.globals.add(d["name"])
            elif k == "TypeDecl":
                s.types.add(d["name"])
                for v in d["variants"]: s.ctors[v["name"]] = len(v["params"]); s.globals.add(v["name"])
            elif k == "Fn": s.fns[d["name"]] = d; s.globals.add(d["name"])
            elif k == "Action": s.actions[d["name"]] = d; s.globals.add(d["name"])
            elif k == "Let": s.globals.add(d["name"])
            elif k == "Import": s.d("info", "I001", f"import '{d['path']}' resolved at runtime", d["line"])
        for d in prog["decls"]:
            k = d["k"]
            if k == "Entity":
                seen = set()
                for c in d.get("invariants", []): s.infer(c, [{f["name"] for f in d["fields"]}], None)
                for f in d["fields"]:
                    if f["name"] in seen: s.d("error", "E210", f"duplicate field '{f['name']}' in entity {d['name']}", f["line"])
                    seen.add(f["name"]); s.type_ok(f["type"], f["line"], set())
                    if f["default"] is not None:
                        t = s.infer(f["default"], [set()], None)
                        if not s.compat(f["type"], t): s.d("error", "E220", f"default of '{f['name']}' is {t}, expected {f['type']['name']}", f["line"])
            elif k in ("Fn", "Action"):
                tps = set(d.get("tparams", []))
                for p in d["params"]:
                    if p["type"]: s.type_ok(p["type"], p["line"], tps)
                if d.get("ret"): s.type_ok(d["ret"], d["line"], tps)
                ctx = {"fn": d, "used": set()} if k == "Fn" else None
                pn = set(p["name"] for p in d["params"])
                for c in d.get("requires", []): s.infer(c, [pn], None)
                for c in d.get("ensures", []): s.infer(c, [pn | {"result"}], None)
                t = s.block(d["body"], [pn], ctx)
                if d.get("ret") and not s.compat(d["ret"], t):
                    s.d("error", "E221", f"function '{d['name']}' returns {t} but declares {d['ret']['name']}", d["line"])
                if ctx:
                    missing = ctx["used"] - set(d["effects"]) - {"pure"}
                    if missing:
                        s.d("warning", "W300", f"function '{d['name']}' performs effects {sorted(missing)} but declares {d['effects'] or 'none'}",
                            d["line"], hint=f"add `! {', '.join(sorted(missing | set(d['effects'])))}` after the signature")
            elif k == "Test": s.block(d["body"], [set()], None)
            elif k == "Example":
                if s.infer(d["expr"], [set()], None) not in ("Bool", None): s.d("warning", "W320", "example should be a boolean expression, e.g. f(1) == 2", d["line"])
            elif k == "View":
                for it in d["items"]:
                    if it["kind"] not in VIEW_KINDS: s.d("error", "E230", f"unknown view element '{it['kind']}' (known: {', '.join(sorted(VIEW_KINDS))})", it["line"])
                    if it["kind"] in ("list","table","form","chart") and it["args"]:
                        a0 = it["args"][0]
                        if a0["k"] != "Name" or a0["name"] not in s.entities:
                            s.d("error", "E232", f"view element '{it['kind']}' references unknown entity '{a0.get('name', '?')}'", it["line"])
                        else:
                            fnames = {f["name"] for f in s.entities[a0["name"]]["fields"]}
                            for a in it["args"][1:]:
                                if a["k"] != "Name" or a["name"] not in fnames:
                                    s.d("error", "E233", f"entity {a0['name']} has no field '{a.get('name', '?')}'", it["line"], hint=f"fields: {', '.join(sorted(fnames))}")
                        continue
                    for a in it["args"]:
                        if a["k"] == "Arrow" and a["target"] not in s.actions: s.d("error", "E231", f"button targets unknown action '{a['target']}'", it["line"])
                        elif a["k"] == "Name" and it["kind"] in ("list","table","form") and a["name"] not in s.entities:
                            s.d("error", "E232", f"view element '{it['kind']}' references unknown entity '{a['name']}'", it["line"])
                        elif a["k"] != "Arrow": s.infer(a, [set()], None)
            elif k == "Flow":
                for st in d["steps"]: s.block(st["body"], [{"ctx"}], None)
            elif k not in ("Module", "Import", "TypeDecl"):
                s.stmt(d, [s.globals_scope()], None)
        return s.diags
    def globals_scope(s): return set()

    def type_ok(s, t, line, tps):
        if t["name"] not in s.types and t["name"] not in tps:
            s.d("error", "E201", f"unknown type '{t['name']}'", line)
        for a in t["args"]: s.type_ok(a, line, tps)
    def compat(s, decl, t):
        if t in (None, "Any") or decl is None: return True
        n = decl["name"]
        if t == "Nil" and decl["optional"]: return True
        if n in ("Any",) or (len(n) == 1 and n.isupper()): return True
        if n == "Float" and t == "Int": return True
        if n == "String": n = "Text"
        if n in s.entities and t == "Record": return True
        return n == t or n not in BUILTIN_TYPES
    def lookup(s, name, scopes):
        return any(name in sc for sc in scopes) or name in s.globals
    def block(s, b, scopes, ctx):
        scopes = scopes + [set()]; t = None
        for st in b["body"]: t = s.stmt(st, scopes, ctx)
        return t
    def stmt(s, st, scopes, ctx):
        k = st["k"]
        if k == "ExprStmt": return s.infer(st["expr"], scopes, ctx)
        if k == "Let":
            t = s.infer(st["value"], scopes, ctx)
            if st["type"]:
                s.type_ok(st["type"], st["line"], set())
                if not s.compat(st["type"], t): s.d("error", "E220", f"'{st['name']}' declared {st['type']['name']} but value is {t}", st["line"])
            scopes[-1].add(st["name"]); return None
        if k == "Assign": s.infer(st["target"], scopes, ctx); s.infer(st["value"], scopes, ctx); return None
        if k == "Return": return s.infer(st["value"], scopes, ctx) if st["value"] else None
        if k == "While": s.infer(st["cond"], scopes, ctx); s.block(st["body"], scopes, ctx); return None
        if k == "For": s.infer(st["iter"], scopes, ctx); s.block(st["body"], scopes + [{st["var"]}], ctx); return None
        if k == "Try": s.block(st["body"], scopes, ctx); s.block(st["handler"], scopes + [{st["var"]}], ctx); return None
        if k == "Throw": s.infer(st["value"], scopes, ctx); return None
        return None
    def infer(s, e, scopes, ctx):
        if e is None: return None
        k = e["k"]
        if k == "Num": return "Float" if isinstance(e["v"], float) else "Int"
        if k == "Str":
            for p in e["parts"]:
                if not isinstance(p, str): s.infer(p, scopes, ctx)
            return "Text"
        if k == "Bool": return "Bool"
        if k == "Nil": return "Nil"
        if k == "Name":
            if not s.lookup(e["name"], scopes):
                cands = [n for n in list(s.globals) + [x for sc in scopes for x in sc] if n[:2] == e["name"][:2]]
                s.d("error", "E200", f"undefined name '{e['name']}'", e["line"], hint=f"did you mean '{cands[0]}'?" if cands else None)
            if e["name"] in EFFECTS and ctx: ctx["used"].add(EFFECTS[e["name"]])
            return None
        if k == "List":
            for x in e["items"]: s.infer(x, scopes, ctx)
            return "List"
        if k == "Record":
            for _, v in e["fields"]: s.infer(v, scopes, ctx)
            return "Record"
        if k == "Lambda": s.block(e["body"], scopes + [set(p["name"] for p in e["params"])], ctx); return "Fn"
        if k == "Unary":
            t = s.infer(e["e"], scopes, ctx); return "Bool" if e["op"] == "not" else t
        if k == "Bin":
            l = s.infer(e["l"], scopes, ctx); r = s.infer(e["r"], scopes, ctx); op = e["op"]
            if op in ("==","!=","<",">","<=",">=","and"): return "Bool"
            if op == "or": return l
            if op == "+" and "Text" in (l, r): return "Text"
            if l in ("Int","Float") and r in ("Int","Float"):
                return "Float" if "Float" in (l, r) or op == "/" else "Int"
            if l and r and l not in ("Int","Float") and op in "-*/%" and l != "Any":
                s.d("error", "E222", f"operator '{op}' cannot apply to {l} and {r}", e["line"])
            return None
        if k == "Pipe":
            s.infer(e["l"], scopes, ctx)
            r = e["r"]
            if r["k"] == "Call":
                s.infer(r["fn"], scopes, ctx)
                for a in r["args"]: s.infer(a, scopes, ctx)
            else: s.infer(r, scopes, ctx)
            return None
        if k == "Call":
            for a in e["args"]: s.infer(a, scopes, ctx)
            f = e["fn"]
            if f["k"] == "Name":
                n = f["name"]; local = any(n in sc for sc in scopes)
                s.infer(f, scopes, ctx)
                if not local and n in s.fns:
                    fd = s.fns[n]
                    if len(e["args"]) != len(fd["params"]): s.d("error", "E240", f"'{n}' expects {len(fd['params'])} arguments, got {len(e['args'])}", e["line"])
                    if ctx: ctx["used"] |= set(fd["effects"])
                    return fd["ret"]["name"] if fd["ret"] else None
                if not local and n in s.ctors and s.ctors[n] != len(e["args"]):
                    s.d("error", "E240", f"constructor '{n}' expects {s.ctors[n]} arguments, got {len(e['args'])}", e["line"])
                if n in ("len","count","int"): return "Int"
                if n in ("str","show","join","upper","lower","now","json"): return "Text"
                if n in ("all","where","map","filter","range","sort","split"): return "List"
                return None
            s.infer(f, scopes, ctx); return None
        if k == "Field": s.infer(e["obj"], scopes, ctx); return None
        if k == "Index": s.infer(e["obj"], scopes, ctx); s.infer(e["idx"], scopes, ctx); return None
        if k == "If":
            s.infer(e["cond"], scopes, ctx); a = s.block(e["then"], scopes, ctx)
            b = s.block(e["els"], scopes, ctx) if e["els"] else "Nil"
            return a if a == b else None
        if k == "Match":
            s.infer(e["subj"], scopes, ctx); ts = set()
            for arm in e["arms"]:
                bs = set(); s.pat(arm["pat"], bs, e["line"]); sc = scopes + [bs]
                if arm["guard"]: s.infer(arm["guard"], sc, ctx)
                b = arm["body"]; ts.add(s.block(b, sc, ctx) if b["k"] == "Block" else s.infer(b, sc, ctx))
            if not any(a["pat"]["k"] in ("PWild","PBind") for a in e["arms"]):
                ctor_pats = {a["pat"]["name"] for a in e["arms"] if a["pat"]["k"] == "PCtor"}
                if not ctor_pats: s.d("warning", "W310", "match may be non-exhaustive (add a `_ =>` arm)", e["line"])
            return ts.pop() if len(ts) == 1 else None
        if k in ("Spawn", "Await"):
            if ctx: ctx["used"].add("async")
            s.infer(e["e"], scopes, ctx); return "Future" if k == "Spawn" else None
        return None
    def pat(s, p, bs, line):
        if p["k"] == "PBind": bs.add(p["name"])
        elif p["k"] == "PCtor":
            if p["name"] not in s.ctors: s.d("error", "E250", f"unknown constructor '{p['name']}' in pattern", line)
            for a in p["args"]: s.pat(a, bs, line)
        elif p["k"] == "PList":
            for a in p["items"]: s.pat(a, bs, line)

def check_source(src):
    """Returns (ast_or_None, diagnostics)."""
    try: prog = parse(src)
    except AeError as e: return None, [e.to_json()]
    return prog, Checker().check(prog)
