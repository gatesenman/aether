import os, json, unittest, threading, urllib.request
from http.server import ThreadingHTTPServer
from aether.parser import parse
from aether.interp import Interpreter, AeThrow
from aether.checker import check_source
from aether.app import App, make_handler
from aether import agent
from aether.fmt import fmt
from aether.ide import Studio, run_source
EX = os.path.join(os.path.dirname(__file__), "..", "examples")
def run(src): out = []; Interpreter(out=out).load(src); return out
def ex(n): return open(os.path.join(EX, n), encoding="utf-8").read()

class Lang(unittest.TestCase):
    def test_arith_and_strings(self): self.assertEqual(run('let x = 2 + 3 * 4\nprint("x={x}")'), ["x=14"])
    def test_closure(self): self.assertEqual(run('fn mk() -> fn { var n = 0\n fn() { n += 1; n } }\nlet c = mk()\nc()\nprint(c())'), ["2"])
    def test_adt_match(self): self.assertEqual(run('type S = A(x: Int) | B\nfn f(s: S) -> Int { match s { A(x) => x * 2, B => 0 } }\nprint(f(A(21)), f(B))'), ["42 0"])
    def test_guard(self): self.assertEqual(run('print(match 7 { x if x > 5 => "big", _ => "small" })'), ["big"])
    def test_pipe(self): self.assertEqual(run('print(range(5) |> map(fn(x) => x * x) |> sum)'), ["30"])
    def test_try(self): self.assertEqual(run('try { throw "e1" } catch e { print(e) }'), ["e1"])
    def test_uncaught(self):
        with self.assertRaises(AeThrow): run('throw "x"')
    def test_async(self): self.assertEqual(run('fn sq(x: Int) -> Int { x * x }\nprint(await [spawn sq(3), spawn sq(4)])'), ["[9, 16]"])
    def test_immutable(self):
        r = run_source('let a = 1\na = 2'); self.assertIn("immutable", r["error"])
    def test_entities(self): self.assertEqual(run('entity T { n: Int = 5 }\ninsert(T, {})\ninsert(T, {n: 1})\nprint(count(T), sum(map(all(T), fn(t) => t.n)))'), ["2 6"])
    def test_tour(self): self.assertTrue(any("并发结果" in l for l in run(ex("language_tour.ae"))))

class Check(unittest.TestCase):
    def codes(self, src): return [d["code"] for d in check_source(src)[1]]
    def test_examples_clean(self):
        for f in os.listdir(EX):
            if f.endswith(".ae"): self.assertEqual(check_source(ex(f))[1], [], f)
    def test_undefined(self): self.assertIn("E200", self.codes("print(zz)"))
    def test_type_mismatch(self): self.assertIn("E220", self.codes('let a: Int = "s"'))
    def test_arity(self): self.assertIn("E240", self.codes("fn f(a: Int) -> Int { a }\nf(1, 2)"))
    def test_effects(self): self.assertIn("W300", self.codes("fn f() { print(1) }"))
    def test_view_refs(self): self.assertTrue({"E231", "E232"} <= set(self.codes("view V { list Nope\n button \"x\" -> nope }")))
    def test_syntax(self): self.assertEqual(self.codes("let = 1"), ["E101"])

class AppModel(unittest.TestCase):
    def test_todo_api(self):
        a = App(ex("todo.ae"))
        c, _, b = a.handle("POST", "/api/Todo", b'{"title":"t"}'); self.assertEqual(c, 201)
        a.handle("POST", "/api/action/toggle", b'{"t":1}')
        self.assertTrue(json.loads(a.handle("GET", "/api/Todo/1")[2])["done"])
        meta = json.loads(a.handle("GET", "/api/_meta")[2]); self.assertEqual(meta["views"][0]["items"][1]["value"], "1 / 1 已完成")
        self.assertEqual(a.handle("POST", "/api/action/clear_done", b"{}")[0], 403)
        a.handle("POST", "/api/action/clear_done", b"{}", "admin"); self.assertEqual(json.loads(a.handle("GET", "/api/Todo")[2]), [])
    def test_counter_and_blog(self):
        a = App(ex("counter.ae")); a.handle("POST", "/api/Counter", b'{"name":"a"}')
        for _ in range(3): a.handle("POST", "/api/action/inc", b'{"c":1}')
        self.assertEqual(json.loads(a.handle("GET", "/api/Counter/1")[2])["value"], 3)
        b = App(ex("blog.ae")); self.assertEqual(len(json.loads(b.handle("GET", "/api/_meta")[2])["views"]), 3)
        self.assertIn("/api/Post", json.loads(b.handle("GET", "/api/_openapi")[2])["paths"])
    def test_http_server(self):
        a = App(ex("todo.ae")); srv = ThreadingHTTPServer(("127.0.0.1", 0), make_handler(a.handle))
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        html = urllib.request.urlopen(f"http://127.0.0.1:{srv.server_port}/").read().decode()
        self.assertIn("Aether", html); srv.shutdown()
    def test_sqlite_persistence(self):
        import tempfile; p = os.path.join(tempfile.mkdtemp(), "t.db")
        App(ex("todo.ae"), p).handle("POST", "/api/Todo", b'{"title":"keep"}')
        self.assertEqual(json.loads(App(ex("todo.ae"), p).handle("GET", "/api/Todo")[2])[0]["title"], "keep")

class Agent(unittest.TestCase):
    def test_ops(self):
        src = ex("todo.ae")
        r = agent.apply_ops(src, [{"op": "add_field", "entity": "Todo", "field": "due", "type": "Text"}, {"op": "rename", "old": "Todo", "new": "Task"}])
        self.assertTrue(r["ok"]); self.assertIn("entity Task", r["source"]); self.assertIn("due: Text", r["source"])
        r = agent.apply_ops(src, [{"op": "remove_decl", "name": "表格"}]); self.assertTrue(r["ok"]); self.assertNotIn("view 表格", r["source"])
    def test_mock_propose(self):
        os.environ.pop("AETHER_LLM_API_KEY", None)
        self.assertTrue(agent.propose(ex("todo.ae"), "新增实体 Note title: Text")["ok"])
        r = agent.propose("fn f() { print(1) }", "修复错误"); self.assertEqual(r["diagnostics"], [])
    def test_fmt(self): self.assertEqual(fmt("fn f() {\nprint(1)\n}"), "fn f() {\n  print(1)\n}\n")
    def test_studio_routes(self):
        s = Studio(); src = json.dumps({"source": ex("todo.ae")}).encode()
        self.assertTrue(json.loads(s.route("POST", "/ide/preview", src)[2])["ok"])
        self.assertEqual(s.route("GET", "/preview/api/Todo", b"")[0], 200)
        self.assertIn("ast", json.loads(s.route("POST", "/ide/check", src)[2]))

if __name__ == "__main__": unittest.main()
