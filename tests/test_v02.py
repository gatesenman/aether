import os, json, unittest, tempfile, subprocess, sys
from aether.interp import Interpreter
from aether.errors import AeError
from aether.app import App
from aether.checker import check_source
from aether.history import History, ast_hash
from aether.explain import explain
from aether.ide import Studio
EX = os.path.join(os.path.dirname(__file__), "..", "examples")
def ex(n): return open(os.path.join(EX, n), encoding="utf-8").read()
def it(src): i = Interpreter(out=[]); i.load(src); return i

class Contracts(unittest.TestCase):
    SRC = "fn f(x: Int) -> Int\n requires x > 0\n ensures result > x\n{ if x == 5 { x } else { x + 1 } }"
    def test_requires(self):
        with self.assertRaises(AeError) as c: it(self.SRC + "\nf(0)")
        self.assertEqual(c.exception.code, "R041")
    def test_ensures(self):
        with self.assertRaises(AeError) as c: it(self.SRC + "\nf(5)")
        self.assertEqual(c.exception.code, "R042")
    def test_ok(self): self.assertEqual(it(self.SRC + "\nprint(f(1))").out, ["2"])
    def test_invariant_api_and_action(self):
        a = App(ex("bank.ae"))
        self.assertEqual(a.handle("POST", "/api/Account", b'{"owner":""}')[0], 400)
        a.handle("POST", "/api/Account", b'{"owner":"Ada"}')
        c, _, b = a.handle("POST", "/api/action/withdraw", b'{"a":1}'); self.assertEqual(c, 400); self.assertIn("invariant", b.decode())
        self.assertEqual(json.loads(a.handle("GET", "/api/Account/1")[2])["balance"], 0)  # rolled back (not saved)
        a.handle("POST", "/api/action/deposit", b'{"a":1}'); self.assertEqual(json.loads(a.handle("GET", "/api/Account/1")[2])["balance"], 100)
    def test_checker_contract_scope(self):
        self.assertIn("E200", [d["code"] for d in check_source("fn f(x: Int) -> Int requires y > 0 { x }")[1]])
        self.assertEqual(check_source(self.SRC)[1], [])

class Permissions(unittest.TestCase):
    def test_roles(self):
        a = App(ex("bank.ae"))
        self.assertEqual(a.handle("POST", "/api/action/audit_reset", b"{}")[0], 403)
        self.assertEqual(a.handle("POST", "/api/action/audit_reset", b"{}", "auditor")[0], 200)
        self.assertEqual(json.loads(a.handle("GET", "/api/_meta")[2])["actions"][2]["allow"], ["admin", "auditor"])

class InlineTests(unittest.TestCase):
    def test_examples_pass(self):
        for f in ("todo.ae", "bank.ae"):
            r = it(ex(f)).run_tests(); self.assertTrue(r and all(x["ok"] for x in r), r)
    def test_failure_reported(self):
        r = it("fn sq(x: Int) -> Int { x * x }\nexample sq(3) == 10\ntest \"t\" { assert(1 == 2, \"nope\") }").run_tests()
        self.assertEqual([x["ok"] for x in r], [False, False]); self.assertIn("9", r[0]["message"]); self.assertEqual(r[1]["message"], "nope")
    def test_cli(self):
        r = subprocess.run([sys.executable, "-m", "aether", "test", os.path.join(EX, "bank.ae")], capture_output=True, text=True, cwd=os.path.join(EX, ".."))
        self.assertEqual(r.returncode, 0); self.assertIn("4 passed", r.stdout)

class Flows(unittest.TestCase):
    def test_flow_runs_and_stops(self):
        i = it('entity N { v: Int }\nflow f {\n step "a" { ctx.n = 1 }\n step "b" { throw "boom" }\n step "c" { ctx.n = 3 } }')
        r = i.run_flow("f"); self.assertFalse(r["ok"]); self.assertEqual([s["step"] for s in r["steps"]], ["a", "b"]); self.assertEqual(r["ctx"]["n"], 1)
    def test_flow_api(self):
        a = App(ex("todo.ae")); a.handle("POST", "/api/Todo", b'{"title":"x","done":true}')
        r = json.loads(a.handle("POST", "/api/flow/每日整理", b"{}")[2]); self.assertTrue(r["ok"]); self.assertIn("剩余 0", r["steps"][-1]["value"])

class HistoryProvenance(unittest.TestCase):
    def test_semantic_hash(self):
        self.assertEqual(ast_hash("let x = 1"), ast_hash("// c\nlet   x =\n 1"))
        self.assertNotEqual(ast_hash("let x = 1"), ast_hash("let x = 2"))
    def test_undo_redo_persist(self):
        p = os.path.join(tempfile.mkdtemp(), "h.json"); h = History(p)
        h.commit("let a = 1", "human", "base"); h.commit("let a = 2", "agent:mock", "change", [{"op": "x"}])
        self.assertEqual(h.undo(), "let a = 1"); self.assertEqual(History(p).redo(), "let a = 2")
        log = History(p).log(); self.assertEqual(log[1]["author"], "agent:mock"); self.assertTrue(log[1]["current"])
    def test_studio_history_flow(self):
        s = Studio(); J = lambda path, d: json.loads(s.route("POST", path, json.dumps(d).encode())[2])
        J("/history/commit", {"source": ex("todo.ae"), "author": "human", "reason": "base"})
        J("/history/commit", {"source": ex("todo.ae").replace("priority: Int = 1", "priority: Int = 2"), "author": "agent:mock", "reason": "p"})
        self.assertTrue(J("/history/commit", {"source": ex("todo.ae").replace("priority: Int = 1", "priority: Int = 2") + "\n// note", "semantic_only": True})["skipped"])
        self.assertIn("priority: Int = 1", J("/history/undo", {})["source"])
        self.assertEqual(len(J("/ide/test", {"source": ex("bank.ae")})["results"]), 4)
        kinds = {c["kind"] for c in J("/ide/contracts", {"source": ex("bank.ae")})["contracts"]}
        self.assertEqual(kinds, {"invariant", "requires", "ensures", "allow"})

class Explain(unittest.TestCase):
    def test_explain(self):
        t = explain(ex("bank.ae"))
        for w in ("Account", "不变式", "仅限角色 admin / auditor", "前置条件", "月结", "计息 → 汇总 → 报告"): self.assertIn(w, t)
