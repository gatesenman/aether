"""Standard library. Each builtin is tagged with its effect (io / db / time / net / async) for the effect checker."""
import math, time, json, random, urllib.request
from .interp import Builtin, EntityRef, Future, Variant, show, typename, AeThrow, POOL
from .errors import AeError

EFFECTS = {}  # name -> effect

def install(it):
    g = it.globals
    def reg(name, f, effect=None):
        g.define(name, Builtin(name, f, effect), False)
        if effect: EFFECTS[name] = effect
    def ent(e):
        if not isinstance(e, EntityRef): raise AeError("R030", f"expected an entity, got {show(e)}")
        return e.name
    call = it.call
    # io
    reg("print", lambda *a: it.emit(" ".join(x if isinstance(x, str) else show(x) for x in a)), "io")
    reg("show", show); reg("type_of", typename)
    # core
    reg("len", lambda x: len(x)); reg("str", lambda x: x if isinstance(x, str) else show(x))
    reg("int", lambda x: int(float(x))); reg("float", lambda x: float(x))
    reg("range", lambda a, b=None: list(range(a)) if b is None else list(range(a, b)))
    reg("map", lambda xs, f: [call(f, [x]) for x in xs])
    reg("filter", lambda xs, f: [x for x in xs if call(f, [x])])
    def reduce_(xs, f, init):
        acc = init
        for x in xs: acc = call(f, [acc, x])
        return acc
    reg("reduce", reduce_)
    reg("sum", lambda xs: sum(xs)); reg("max", lambda *a: max(a[0]) if len(a) == 1 else max(a)); reg("min", lambda *a: min(a[0]) if len(a) == 1 else min(a))
    reg("push", lambda xs, x: (xs.append(x), xs)[1])
    reg("sort", lambda xs, f=None: sorted(xs, key=(lambda x: call(f, [x])) if f else None))
    reg("reverse", lambda xs: xs[::-1]); reg("join", lambda xs, sep="": sep.join(x if isinstance(x, str) else show(x) for x in xs))
    reg("split", lambda s, sep=" ": s.split(sep)); reg("upper", lambda s: s.upper()); reg("lower", lambda s: s.lower())
    reg("contains", lambda xs, x: x in xs); reg("keys", lambda r: list(r.keys())); reg("values", lambda r: list(r.values()))
    reg("abs", abs); reg("sqrt", math.sqrt); reg("floor", lambda x: math.floor(x)); reg("round", lambda x, n=0: round(x, n))
    reg("random", random.random, "random")
    reg("json", lambda x: json.dumps(x, ensure_ascii=False)); reg("parse_json", json.loads)
    reg("error", lambda m: (_ for _ in ()).throw(AeThrow(m)))
    reg("assert", lambda c, m="assertion failed": None if c else (_ for _ in ()).throw(AeThrow(m)))
    # time / async
    reg("now", lambda: time.strftime("%Y-%m-%d %H:%M:%S"), "time")
    reg("sleep", lambda ms: time.sleep(ms / 1000), "time")
    reg("all_of", lambda fs: [f.fut.result() for f in fs], "async")
    # net
    reg("http_get", lambda url: urllib.request.urlopen(url, timeout=10).read().decode("utf-8", "replace"), "net")
    # data (entities)
    st = it.store
    reg("all", lambda e: st.all(ent(e)), "db")
    reg("get", lambda e, id: st.get(ent(e), int(id)), "db")
    reg("count", lambda e: len(st.all(ent(e))), "db")
    def insert(e, rec={}):
        r = it.new_record(e.decl, rec); it.check_inv(ent(e), r); return st.insert(e.name, r)
    def save(e, rec):
        it.check_inv(ent(e), rec); return st.update(e.name, rec["id"], rec) if rec.get("id") else st.insert(e.name, rec)
    reg("insert", insert, "db"); reg("save", save, "db")
    reg("run_flow", lambda name, ctx=None: it.run_flow(name, ctx), "flow")
    reg("remove", lambda e, rec: st.delete(ent(e), rec["id"] if isinstance(rec, dict) else int(rec)), "db")
    reg("where", lambda e, f: [r for r in st.all(ent(e)) if call(f, [r])], "db")
