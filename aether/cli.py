"""aether CLI: run | check | serve | studio | fmt | ast | ops | new"""
import argparse, json, sys, os
from .errors import AeError

def main(argv=None):
    ap = argparse.ArgumentParser(prog="aether", description="Aether — agent+human programming language")
    sub = ap.add_subparsers(dest="cmd", required=True)
    for c, h in [("run", "execute a program"), ("check", "type/effect/app checks"), ("fmt", "format source"), ("ast", "print JSON IR")]:
        p = sub.add_parser(c, help=h); p.add_argument("file")
        if c in ("check",): p.add_argument("--json", action="store_true")
        if c == "fmt": p.add_argument("-w", "--write", action="store_true")
    p = sub.add_parser("serve", help="compile and serve as a full web app"); p.add_argument("file"); p.add_argument("--port", type=int, default=8080); p.add_argument("--db"); p.add_argument("--host", default="127.0.0.1")
    p = sub.add_parser("studio", help="open the Aether Studio IDE"); p.add_argument("file", nargs="?"); p.add_argument("--port", type=int, default=7777); p.add_argument("--host", default="127.0.0.1")
    p = sub.add_parser("ops", help="apply structured agent operations (JSON) to a file"); p.add_argument("file"); p.add_argument("ops", help="JSON list or @file.json"); p.add_argument("-w", "--write", action="store_true")
    p.add_argument("--author", default="agent"); p.add_argument("--reason", default="")
    p = sub.add_parser("propose", help="ask the agent (LLM or mock) for a patch"); p.add_argument("file"); p.add_argument("prompt")
    p = sub.add_parser("new", help="create a starter app"); p.add_argument("name")
    p = sub.add_parser("test", help="run inline test/example blocks"); p.add_argument("files", nargs="+"); p.add_argument("--json", action="store_true")
    p = sub.add_parser("explain", help="natural-language projection of a program"); p.add_argument("file")
    p = sub.add_parser("flow", help="execute a flow"); p.add_argument("file"); p.add_argument("name"); p.add_argument("--db")
    p = sub.add_parser("history", help="show content-addressed change history"); p.add_argument("file")
    p = sub.add_parser("undo", help="revert file to previous version in history"); p.add_argument("file")
    p = sub.add_parser("redo", help="re-apply an undone version"); p.add_argument("file")
    p = sub.add_parser("hash", help="semantic (AST) hash of a program"); p.add_argument("file")
    sub.add_parser("version")
    a = ap.parse_args(argv)
    try:
        if a.cmd == "version":
            from . import __version__; print("aether", __version__); return 0
        if a.cmd == "new":
            fn = a.name if a.name.endswith(".ae") else a.name + ".ae"
            open(fn, "w", encoding="utf-8").write(f'module {os.path.splitext(os.path.basename(fn))[0]}\n\nentity Item {{\n  name: Text\n  done: Bool = false\n}}\n\naction toggle(i: Item) {{ i.done = not i.done }}\n\nview Home {{\n  title "Hello Aether"\n  stat "总数" count(Item)\n  form Item\n  list Item\n}}\n')
            print("created", fn, "->  aether serve", fn); return 0
        if a.cmd == "studio":
            from .ide import serve; serve(a.file, a.port, a.host); return 0
        if a.cmd == "test":
            from .interp import Interpreter
            fails = 0; allres = {}
            for f in a.files:
                it = Interpreter(out=[]); it.load(open(f, encoding="utf-8").read(), os.path.dirname(os.path.abspath(f)))
                res = it.run_tests(); allres[f] = res
                if not a.json:
                    for r in res: print(f"{'PASS' if r['ok'] else 'FAIL'} {f}:{r['line']} {r['name']}" + (f"  — {r['message']}" if r["message"] else ""))
                fails += sum(not r["ok"] for r in res)
            total = sum(len(v) for v in allres.values())
            if a.json: print(json.dumps(allres, ensure_ascii=False, indent=1))
            else: print(f"\n{total - fails} passed, {fails} failed")
            return 1 if fails else 0
        src = open(a.file, encoding="utf-8").read()
        hist_path = a.file + ".history.json"
        if a.cmd == "explain":
            from .explain import explain; print(explain(src)); return 0
        if a.cmd == "hash":
            from .history import ast_hash; print(ast_hash(src)); return 0
        if a.cmd == "flow":
            from .interp import Interpreter
            it = Interpreter(db_path=a.db or os.path.splitext(a.file)[0] + ".db"); it.load(src, os.path.dirname(os.path.abspath(a.file)))
            r = it.run_flow(a.name); print(json.dumps(r, ensure_ascii=False, indent=1)); return 0 if r["ok"] else 1
        if a.cmd in ("history", "undo", "redo"):
            from .history import History
            h = History(hist_path)
            if a.cmd == "history":
                for e in h.log(): print(f"{'→' if e['current'] else ' '} #{e['id']} {e['hash']} {e['time']} [{e['author']}] {e['kind']}: {e['reason']}" + (" (undone)" if e["undone"] else ""))
                if not h.entries: print("(no history yet — changes via `aether ops` or Studio are recorded here)")
                return 0
            new = h.undo() if a.cmd == "undo" else h.redo()
            open(a.file, "w", encoding="utf-8").write(new); print(f"{a.cmd}: now at {h.head['hash']} ({h.head['reason']})"); return 0
        if a.cmd == "run":
            from .interp import Interpreter, AeThrow, show
            try: Interpreter().load(src, os.path.dirname(os.path.abspath(a.file)))
            except AeThrow as e: print(f"uncaught error: {show(e.v)}", file=sys.stderr); return 1
            return 0
        if a.cmd == "check":
            from .checker import check_source
            _, d = check_source(src)
            if a.json: print(json.dumps(d, ensure_ascii=False, indent=1))
            else:
                for x in d: print(f"{a.file}:{x['line']}: {x['severity']} {x['code']}: {x['message']}" + (f"  (hint: {x['hint']})" if x.get("hint") else ""))
                if not d: print(f"{a.file}: ok")
            return 1 if any(x["severity"] == "error" for x in d) else 0
        if a.cmd == "ast":
            from .parser import parse; print(json.dumps(parse(src), ensure_ascii=False, indent=1)); return 0
        if a.cmd == "fmt":
            from .fmt import fmt; out = fmt(src)
            if a.write: open(a.file, "w", encoding="utf-8").write(out)
            else: sys.stdout.write(out)
            return 0
        if a.cmd == "serve":
            from .app import serve; serve(a.file, a.port, a.db, a.host); return 0
        if a.cmd in ("ops", "propose"):
            from . import agent
            if a.cmd == "ops":
                ops = json.loads(open(a.ops[1:]).read() if a.ops.startswith("@") else a.ops)
                r = agent.apply_ops(src, ops)
            else: r = agent.propose(src, a.prompt)
            if a.cmd == "ops" and a.write and r["ok"]:
                from .history import History
                h = History(hist_path)
                if not h.entries: h.commit(src, "human", "baseline", kind="baseline")
                r["history"] = h.commit(r["source"], a.author, a.reason or "apply ops", ops, kind="ops")
                open(a.file, "w", encoding="utf-8").write(r["source"])
            print(json.dumps({k: v for k, v in r.items() if k != "source"}, ensure_ascii=False, indent=1)); return 0 if r["ok"] else 1
    except AeError as e:
        print(f"{getattr(a, 'file', '')}:{e.line}:{e.col}: error {e.code}: {e.msg}", file=sys.stderr); return 1

if __name__ == "__main__": sys.exit(main())
