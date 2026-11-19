"""Tree-walking interpreter with closures, ADTs, pattern matching, exceptions, async (threads) and entity storage."""
import threading, time, json, math, os
from concurrent.futures import ThreadPoolExecutor
from .errors import AeError
from .parser import parse
from . import storage, unparse

POOL = ThreadPoolExecutor(max_workers=16)

class ReturnSig(Exception):
    def __init__(s, v): s.v = v
class AeThrow(Exception):
    def __init__(s, v): super().__init__(str(v)); s.v = v

class Env:
    def __init__(s, parent=None): s.vars = {}; s.parent = parent; s.mut = set()
    def get(s, n, line=0):
        e = s
        while e:
            if n in e.vars: return e.vars[n]
            e = e.parent
        raise AeError("R001", f"undefined name '{n}'", line)
    def define(s, n, v, mutable=True):
        s.vars[n] = v
        if mutable: s.mut.add(n)
    def set(s, n, v, line=0):
        e = s
        while e:
            if n in e.vars:
                if n not in e.mut: raise AeError("R002", f"cannot reassign immutable '{n}' (use var)", line)
                e.vars[n] = v; return
            e = e.parent
        raise AeError("R001", f"undefined name '{n}'", line)

class Closure:
    def __init__(s, name, params, body, env, decl=None): s.name, s.params, s.body, s.env, s.decl = name, params, body, env, decl
    def __repr__(s): return f"<fn {s.name}>"
class Builtin:
    def __init__(s, name, f, effect=None): s.name, s.f, s.effect = name, f, effect
    def __repr__(s): return f"<builtin {s.name}>"
class Ctor:
    def __init__(s, tname, name, fields): s.tname, s.name, s.fields = tname, name, fields
class Variant:
    def __init__(s, ctor, vals): s.ctor, s.vals = ctor, vals
    def __repr__(s): return s.ctor.name + (f"({', '.join(show(v) for v in s.vals)})" if s.vals else "")
    def __eq__(s, o): return isinstance(o, Variant) and o.ctor.name == s.ctor.name and o.vals == s.vals
class EntityRef:
    def __init__(s, decl): s.decl = decl; s.name = decl["name"]
    def __repr__(s): return f"<entity {s.name}>"
class Future:
    def __init__(s, fut): s.fut = fut
    def __repr__(s): return "<future>"

def show(v):
    if v is None: return "nil"
    if v is True: return "true"
    if v is False: return "false"
    if isinstance(v, float) and v.is_integer(): return str(v)
    if isinstance(v, list): return "[" + ", ".join(show(x) if not isinstance(x, str) else json.dumps(x, ensure_ascii=False) for x in v) + "]"
    if isinstance(v, dict): return "{" + ", ".join(f"{k}: {show(x) if not isinstance(x,str) else json.dumps(x, ensure_ascii=False)}" for k, x in v.items()) + "}"
    return str(v)

class Interpreter:
    def __init__(s, db_path=":memory:", out=None):
        s.out = out if out is not None else []
        s.echo = out is None
        s.globals = Env(); s.tests = []; s.flow_log = []; s.entities = {}; s.views = {}; s.actions = {}; s.flows = {}; s.module = "main"
        s.store = storage.Store(db_path)
        from .stdlib import install
        install(s)
    def emit(s, text):
        s.out.append(text)
        if s.echo: print(text)

    def load(s, src, base_dir="."):
        prog = parse(src)
        s.base_dir = base_dir
        # hoist declarations first, then run top-level statements in order
        for d in prog["decls"]: s.declare(d)
        for e in s.entities.values(): s.store.ensure(e)
        result = None
        for d in prog["decls"]:
            if d["k"] not in ("Module","Import","Entity","TypeDecl","Fn","Action","View","Flow","Test","Example"):
                result = s.exec(d, s.globals)
        return result

    def declare(s, d):
        k = d["k"]; g = s.globals
        if k == "Module": s.module = d["name"]
        elif k == "Import":
            path = os.path.join(s.base_dir, d["path"] if d["path"].endswith(".ae") else d["path"] + ".ae")
            with open(path, encoding="utf-8") as f:
                for sd in parse(f.read())["decls"]: s.declare(sd)
        elif k == "Entity": s.entities[d["name"]] = d; g.define(d["name"], EntityRef(d), False)
        elif k == "TypeDecl":
            for v in d["variants"]:
                c = Ctor(d["name"], v["name"], [p["name"] for p in v["params"]])
                g.define(v["name"], Variant(c, []) if not c.fields else c, False)
        elif k == "Fn": g.define(d["name"], Closure(d["name"], d["params"], d["body"], g, d), False)
        elif k in ("Test", "Example"): s.tests.append(d)
        elif k == "Action": s.actions[d["name"]] = d; g.define(d["name"], Closure(d["name"], d["params"], d["body"], g, d), False)
        elif k == "View": s.views[d["name"]] = d
        elif k == "Flow": s.flows[d["name"]] = d

    # ---------- statements
    def exec_block(s, block, env):
        r = None
        for st in block["body"]: r = s.exec(st, env)
        return r
    def exec(s, st, env):
        k = st["k"]
        if k == "ExprStmt": return s.eval(st["expr"], env)
        if k == "Let": env.define(st["name"], s.eval(st["value"], env), st["mutable"]); return None
        if k == "Assign":
            v = s.eval(st["value"], env); t = st["target"]
            if t["k"] == "Name": env.set(t["name"], v, st["line"])
            elif t["k"] == "Field":
                o = s.eval(t["obj"], env)
                if not isinstance(o, dict): raise AeError("R003", "field assignment on non-record", st["line"])
                o[t["name"]] = v
            else:
                o = s.eval(t["obj"], env); o[s.eval(t["idx"], env)] = v
            return None
        if k == "Return": raise ReturnSig(s.eval(st["value"], env) if st["value"] else None)
        if k == "While":
            while truthy(s.eval(st["cond"], env)): s.exec_block(st["body"], Env(env))
            return None
        if k == "For":
            for x in iterate(s.eval(st["iter"], env), st["line"]):
                e = Env(env); e.define(st["var"], x); s.exec_block(st["body"], e)
            return None
        if k == "Try":
            try: return s.exec_block(st["body"], Env(env))
            except (AeThrow, AeError) as ex:
                e = Env(env); e.define(st["var"], ex.v if isinstance(ex, AeThrow) else ex.msg); return s.exec_block(st["handler"], e)
        if k == "Throw": raise AeThrow(s.eval(st["value"], env))
        raise AeError("R010", f"cannot execute {k}", st.get("line", 0))

    # ---------- expressions
    def eval(s, e, env):
        k = e["k"]
        if k in ("Num","Bool"): return e["v"]
        if k == "Nil": return None
        if k == "Str": return "".join(p if isinstance(p, str) else (show(s.eval(p, env)) if not isinstance(v := s.eval(p, env), str) else v) for p in e["parts"])
        if k == "Name": return env.get(e["name"], e["line"])
        if k == "List": return [s.eval(x, env) for x in e["items"]]
        if k == "Record": return {kk: s.eval(v, env) for kk, v in e["fields"]}
        if k == "Lambda": return Closure("lambda", e["params"], e["body"], env)
        if k == "Unary":
            v = s.eval(e["e"], env); return -v if e["op"] == "-" else not truthy(v)
        if k == "Bin": return s.binop(e, env)
        if k == "Pipe":
            l = s.eval(e["l"], env); r = e["r"]
            if r["k"] == "Call": return s.call(s.eval(r["fn"], env), [l] + [s.eval(a, env) for a in r["args"]], r["line"])
            return s.call(s.eval(r, env), [l], e["line"])
        if k == "Call":
            return s.call(s.eval(e["fn"], env), [s.eval(a, env) for a in e["args"]], e["line"])
        if k == "Field":
            o = s.eval(e["obj"], env); n = e["name"]
            if isinstance(o, dict):
                if n in o: return o[n]
                raise AeError("R004", f"record has no field '{n}'", e["line"])
            if isinstance(o, Variant) and n in o.ctor.fields: return o.vals[o.ctor.fields.index(n)]
            # method-call sugar: x.len() -> len(x)
            fn = s.globals.get(n, e["line"])
            return Builtin(n, lambda *a: s.call(fn, [o, *a], e["line"]))
        if k == "Index":
            o = s.eval(e["obj"], env); i = s.eval(e["idx"], env)
            try: return o[i]
            except (IndexError, KeyError, TypeError): raise AeError("R005", f"index {show(i)} out of range", e["line"])
        if k == "If":
            if truthy(s.eval(e["cond"], env)): return s.exec_block(e["then"], Env(env))
            return s.exec_block(e["els"], Env(env)) if e["els"] else None
        if k == "Match":
            v = s.eval(e["subj"], env)
            for arm in e["arms"]:
                b = {}
                if match(arm["pat"], v, b):
                    ae = Env(env)
                    for n, x in b.items(): ae.define(n, x)
                    if arm["guard"] and not truthy(s.eval(arm["guard"], ae)): continue
                    body = arm["body"]
                    return s.exec_block(body, ae) if body["k"] == "Block" else s.eval(body, ae)
            raise AeError("R006", f"no match arm for {show(v)}", e["line"])
        if k == "Spawn":
            inner = e["e"]
            if inner["k"] == "Call":
                fn = s.eval(inner["fn"], env); args = [s.eval(a, env) for a in inner["args"]]
                return Future(POOL.submit(s.call, fn, args, e["line"]))
            return Future(POOL.submit(s.eval, inner, env))
        if k == "Await":
            v = s.eval(e["e"], env)
            if isinstance(v, Future): return v.fut.result()
            if isinstance(v, list) and all(isinstance(x, Future) for x in v): return [x.fut.result() for x in v]
            return v
        raise AeError("R011", f"cannot evaluate {k}", e.get("line", 0))

    def binop(s, e, env):
        op = e["op"]
        if op == "and": return truthy(s.eval(e["l"], env)) and truthy(s.eval(e["r"], env))
        if op == "or":
            l = s.eval(e["l"], env); return l if truthy(l) else s.eval(e["r"], env)
        l = s.eval(e["l"], env); r = s.eval(e["r"], env)
        try:
            if op == "+":
                if isinstance(l, str) or isinstance(r, str): return (l if isinstance(l, str) else show(l)) + (r if isinstance(r, str) else show(r))
                return l + r
            if op == "-": return l - r
            if op == "*": return l * r
            if op == "/":
                if r == 0: raise AeError("R007", "division by zero", e["line"])
                return l // r if isinstance(l, int) and isinstance(r, int) and l % r == 0 else l / r
            if op == "%": return l % r
            if op == "==": return l == r
            if op == "!=": return l != r
            if op == "<": return l < r
            if op == ">": return l > r
            if op == "<=": return l <= r
            if op == ">=": return l >= r
        except TypeError:
            raise AeError("R008", f"type error: cannot apply '{op}' to {typename(l)} and {typename(r)}", e["line"])

    def call(s, fn, args, line=0):
        if isinstance(fn, Builtin): return fn.f(*args)
        if isinstance(fn, Ctor):
            if len(args) != len(fn.fields): raise AeError("R009", f"{fn.name} expects {len(fn.fields)} args", line)
            return Variant(fn, args)
        if isinstance(fn, EntityRef):  # Todo({title: "x"}) -> new record (unsaved) with defaults
            return s.new_record(fn.decl, args[0] if args else {})
        if not isinstance(fn, Closure): raise AeError("R012", f"{show(fn)} is not callable", line)
        if len(args) != len(fn.params): raise AeError("R009", f"{fn.name} expects {len(fn.params)} args, got {len(args)}", line)
        env = Env(fn.env)
        for p, a in zip(fn.params, args): env.define(p["name"], a)
        dc = fn.decl or {}
        for c in dc.get("requires", []):
            if not truthy(s.eval(c, env)): raise AeError("R041", f"precondition failed in {fn.name}: requires {unparse.ex(c)}", c["line"])
        try: res = s.exec_block(fn.body, env)
        except ReturnSig as r: res = r.v
        if dc.get("ensures"):
            env.define("result", res)
            for c in dc["ensures"]:
                if not truthy(s.eval(c, env)): raise AeError("R042", f"postcondition failed in {fn.name}: ensures {unparse.ex(c)} (result = {show(res)})", c["line"])
        return res

    # ---------- contracts on data
    def check_inv(s, ename, rec):
        decl = s.entities[ename]
        if not decl.get("invariants"): return
        env = Env(s.globals)
        for f in decl["fields"]: env.define(f["name"], rec.get(f["name"]))
        for c in decl["invariants"]:
            if not truthy(s.eval(c, env)): raise AeError("R040", f"invariant of {ename} violated: {unparse.ex(c)}", c["line"])

    # ---------- inline tests / examples
    def run_tests(s):
        res = []
        for t in s.tests:
            name = t["name"] if t["k"] == "Test" else "example: " + unparse.ex(t["expr"])
            try:
                if t["k"] == "Test": s.exec_block(t["body"], Env(s.globals)); ok, msg = True, ""
                else:
                    v = s.eval(t["expr"], s.globals); ok = truthy(v)
                    msg = "" if ok else (f"left = {show(s.eval(t['expr']['l'], s.globals))}" if t["expr"]["k"] == "Bin" else f"got {show(v)}")
            except AeThrow as e: ok, msg = False, show(e.v)
            except AeError as e: ok, msg = False, f"[{e.code}] {e.msg}"
            res.append({"name": name, "ok": ok, "message": msg, "line": t["line"]})
        return res

    # ---------- flows: ordered, logged, stop-on-failure workflows
    def run_flow(s, name, ctx=None):
        f = s.flows.get(name)
        if not f: raise AeError("R050", f"no flow '{name}'")
        env = Env(s.globals); env.define("ctx", ctx if ctx is not None else {})
        log = {"flow": name, "steps": [], "ok": True}
        for st in f["steps"]:
            t0 = time.time()
            try:
                v = s.exec_block(st["body"], env)
                log["steps"].append({"step": st["name"], "ok": True, "value": show(v), "ms": round((time.time() - t0) * 1000, 2)})
            except (AeThrow, AeError) as e:
                log["steps"].append({"step": st["name"], "ok": False, "error": show(e.v) if isinstance(e, AeThrow) else e.msg}); log["ok"] = False; break
        log["ctx"] = env.get("ctx"); s.flow_log.append(log)
        return log

    def new_record(s, decl, data):
        rec = {}
        for f in decl["fields"]:
            if f["name"] in data: rec[f["name"]] = data[f["name"]]
            elif f["default"] is not None: rec[f["name"]] = s.eval(f["default"], s.globals)
            else: rec[f["name"]] = {"Int":0,"Float":0.0,"Bool":False}.get(f["type"]["name"], "" if f["type"]["name"] in ("Text","String") else None)
        if "id" in data: rec["id"] = data["id"]
        return rec

    def run_action(s, name, payload, role="user"):
        """Invoke an action from the API. Entity-typed params are loaded by id and saved back afterwards."""
        a = s.actions[name]; args = []; to_save = []
        if a.get("allow") and role not in a["allow"]:
            raise PermissionError(f"action '{name}' requires role {' / '.join(a['allow'])}, you are '{role}'")
        for p in a["params"]:
            v = payload.get(p["name"])
            tn = p["type"]["name"] if p["type"] else None
            if tn in s.entities:
                rec = s.store.get(tn, int(v if not isinstance(v, dict) else v["id"]))
                if rec is None: raise AeError("R020", f"{tn} #{v} not found")
                to_save.append((tn, rec)); v = rec
            args.append(v)
        with s.store.lock:
            r = s.call(s.globals.get(name), args)
            for tn, rec in to_save: s.check_inv(tn, rec)
            for tn, rec in to_save: s.store.update(tn, rec["id"], rec)
        return r

def truthy(v): return v is not None and v is not False and v != 0 and v != "" and v != []
def typename(v):
    return {bool:"Bool", int:"Int", float:"Float", str:"Text", list:"List", dict:"Record", type(None):"Nil"}.get(type(v), type(v).__name__)
def iterate(v, line):
    if isinstance(v, (list, str)): return v
    if isinstance(v, dict): return [{"key": k, "value": x} for k, x in v.items()]
    raise AeError("R013", f"cannot iterate over {typename(v)}", line)
def match(p, v, b):
    k = p["k"]
    if k == "PWild": return True
    if k == "PBind": b[p["name"]] = v; return True
    if k == "PLit": return v == p["v"] and type(v) is not bool or v is p["v"]
    if k == "PList":
        return isinstance(v, list) and len(v) == len(p["items"]) and all(match(x, y, b) for x, y in zip(p["items"], v))
    if k == "PCtor":
        return isinstance(v, Variant) and v.ctor.name == p["name"] and len(p["args"]) in (0, len(v.vals)) and all(match(x, y, b) for x, y in zip(p["args"], v.vals))
    return False
