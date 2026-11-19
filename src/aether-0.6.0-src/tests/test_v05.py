"""v0.5: field typing, modules & packages, auth, layout, durable flows, LLM self-repair (mock OpenAI server),
DAP debugger, MVC model, templates & deploy."""
import urllib.parse, json, os, socket, subprocess, sys, tempfile, threading, time, unittest, urllib.request
from http.server import BaseHTTPRequestHandler, HTTPServer
sys.path.insert(0, os.path.dirname(__file__))
from test_native import BIN, ROOT
from lsp_client import LSPClient

def run(*a, cwd=None, env=None, timeout=60):
    return subprocess.run([BIN, *a], capture_output=True, text=True, cwd=cwd, env=env, timeout=timeout)

def free_port():
    s = socket.socket(); s.bind(("127.0.0.1", 0)); p = s.getsockname()[1]; s.close(); return p

def http(url, data=None, token=None, method=None):
    h = {"Content-Type": "application/json"}
    if token: h["Authorization"] = "Bearer " + token
    req = urllib.request.Request(url, data=None if data is None else json.dumps(data).encode(), headers=h, method=method)
    try:
        with urllib.request.urlopen(req, timeout=10) as r: return r.status, json.loads(r.read())
    except urllib.error.HTTPError as e: return e.code, json.loads(e.read())

SHOP = open(os.path.join(ROOT, "native", "templates", "shop", "main.ae"), encoding="utf-8").read()

@unittest.skipUnless(BIN, "native binary not built")
class V05(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp()
    def w(self, name, text):
        p = os.path.join(self.d, name); os.makedirs(os.path.dirname(p), exist_ok=True)
        open(p, "w", encoding="utf-8").write(text); return p

    def test_field_typing(self):
        p = self.w("a.ae", "entity Account { owner: Text\n balance: Int = 0 }\naction dep(a: Account) { a.balanse += 1 }\n"
                   "fn f() -> Int ! db { let x = insert(Account, {owner: \"a\", balanc: 1})\n x.balance }\n"
                   "fn g() -> Int ! db { let n = 0\n for a in all(Account) { n += a.blance }\n n }\n"
                   "fn h() ! db { where(Account, fn(x) => x.ownr == \"a\") }\n")
        r = run("check", p, "--json"); ds = json.loads(r.stdout)
        msgs = [d["message"] for d in ds if d["code"] == "E234"]
        for bad in ("balanse", "balanc", "blance", "ownr"):
            self.assertTrue(any(bad in m for m in msgs), (bad, ds))
        self.assertTrue(any("balance" in d.get("hint", "") for d in ds))
        for f in os.listdir(os.path.join(ROOT, "examples")):
            if f.endswith(".ae"):
                self.assertEqual(run("check", os.path.join(ROOT, "examples", f)).returncode, 0, f)

    def test_modules_and_packages(self):
        self.w("lib/money/main.ae", "fn fee(x: Int) -> Int { x / 10 }\nfn _hidden() { 1 }\nlet RATE = 7\n")
        self.w("lib/util.ae", "fn twice(x: Int) -> Int { x * 2 }\n")
        proj = os.path.join(self.d, "proj"); os.makedirs(proj)
        self.assertEqual(run("pkg", "init", "proj", cwd=proj).returncode, 0)
        r = run("pkg", "add", "money", "../lib/money", cwd=proj); self.assertEqual(r.returncode, 0, r.stderr)
        lock = json.load(open(os.path.join(proj, "aether.lock")))
        self.assertEqual(len(lock["money"]["sha256"]), 64)
        main = os.path.join(proj, "main.ae")
        open(main, "w").write('import "money" as m\nimport "../lib/util"\nprint(m.fee(100) + m.RATE + twice(1))\n')
        r = run("run", main); self.assertEqual(r.stdout.strip(), "19", r.stderr)
        open(main, "a").write("print(m._hidden())\n")
        r = run("check", main); self.assertIn("E261", r.stdout + r.stderr)
        # frozen install detects tampering
        open(os.path.join(self.d, "lib/money/main.ae"), "a").write("let X = 1\n")
        r = run("pkg", "install", "--frozen", cwd=proj); self.assertNotEqual(r.returncode, 0)
        self.assertIn("aether.lock", r.stderr)

    def test_auth_permissions_layout(self):
        p = self.w("shop.ae", SHOP); port = free_port()
        srv = subprocess.Popen([BIN, "serve", p, "--port", str(port), "--db", os.path.join(self.d, "s.aedb")], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            U = f"http://127.0.0.1:{port}"
            for _ in range(50):
                try: http(U + "/api/_meta"); break
                except Exception: time.sleep(0.1)
            code, meta = http(U + "/api/_meta")
            self.assertEqual(meta["auth"]["roles"], ["admin", "staff", "customer"])
            self.assertEqual(meta["theme"]["primary"], "#0f766e")
            v = meta["views"][0]["items"]
            self.assertEqual(v[0]["style"]["color"], "#0f766e")
            self.assertEqual(v[1]["kind"], "row"); self.assertEqual(v[1]["children"][0]["kind"], "card")
            self.assertEqual(http(U + "/api/Product")[0], 401)
            c, a = http(U + "/api/_auth/signup", {"username": "boss", "password": "secret1"}); self.assertEqual(a["user"]["role"], "admin")
            c, b = http(U + "/api/_auth/signup", {"username": "bob", "password": "secret2"}); self.assertEqual(b["user"]["role"], "customer")
            self.assertEqual(http(U + "/api/_auth/signup", {"username": "bob", "password": "secret2"})[0], 409)
            self.assertEqual(http(U + "/api/_auth/login", {"username": "bob", "password": "nope"})[0], 401)
            A, B = a["token"], b["token"]
            self.assertEqual(http(U + "/api/Product", {"name": "x", "price": 1}, B)[0], 403)
            self.assertEqual(http(U + "/api/Product", {"name": "x", "price": 1}, A)[0], 201)
            self.assertEqual(http(U + "/api/Product", token=B)[0], 200)
            self.assertEqual(http(U + "/api/action/restock", {"p": 1}, B)[0], 403)
            self.assertEqual(http(U + "/api/Product", token=A[:-2] + "xx")[0], 401)   # forged token
            self.assertEqual(http(U + "/api/Product", {"name": "y"}, token=None, method="POST")[0], 401)
            # header role spoofing is ignored when auth is declared
            req = urllib.request.Request(U + "/api/Product", data=b'{"name":"z"}', headers={"X-Aether-Role": "admin", "Content-Type": "application/json"})
            with self.assertRaises(urllib.error.HTTPError): urllib.request.urlopen(req)
            # admin promotes bob to staff -> now allowed
            self.assertEqual(http(U + "/api/_auth/users/2", {"role": "staff"}, A, "PUT")[0], 200)
            self.assertEqual(http(U + "/api/action/restock", {"p": 1}, B)[0], 200)
            self.assertEqual(http(U + "/api/Product/1", token=A)[1]["stock"], 10)
            # durable flow with retry
            c, f = http(U + "/api/flow/" + urllib.parse.quote("补货"), {}, A); self.assertTrue(f["ok"]); self.assertEqual(f["steps"][0]["attempts"], 2)
            self.assertEqual(http(U + "/api/_flows/runs", token=A)[1][0]["status"], "done")
        finally:
            srv.kill()
        bad = self.w("bad.ae", SHOP.replace("allow staff", "allow staf"))
        self.assertIn("E271", run("check", bad).stdout)

    def test_flow_resume(self):
        p = self.w("f.ae", 'entity Job { n: Int = 0 }\nflow 导入 {\n step "a" { ctx.a = 1 }\n step "b" retry 1 backoff 1ms {\n  if count(Job) == 0 { throw "down" }\n  ctx.b = 2\n }\n step "c" { ctx.c = ctx.a + ctx.b }\n}\n')
        db = os.path.join(self.d, "f.aedb")
        r = run("flow", p, "导入", "--db", db); self.assertEqual(r.returncode, 1)
        res = json.loads(r.stdout); self.assertEqual(res["status"], "failed"); self.assertEqual(res["steps"][1]["attempts"], 2)
        open(db, "a").write('{"op": "put", "t": "Job", "id": 1, "r": {"n": 1}}\n')
        runs = json.loads(run("flow", "runs", p, "--db", db).stdout); self.assertEqual(runs[0]["status"], "failed")
        r = run("flow", "resume", p, "1", "--db", db); self.assertEqual(r.returncode, 0, r.stderr)
        res = json.loads(r.stdout); self.assertEqual(res["ctx"]["c"], 3); self.assertEqual(res["status"], "done")

    def test_llm_self_repair_mock_openai(self):
        calls = []
        class H(BaseHTTPRequestHandler):
            def log_message(self, *a): pass
            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                calls.append(body)
                if len(calls) == 1:   # first answer is wrong: refers to an unknown field
                    ops = {"summary": "加字段", "ops": [{"op": "add_decl", "code": "fn bad(t: Todo) -> Int { t.priorty }"}]}
                else:
                    ops = {"summary": "给 Todo 添加 due 字段", "ops": [{"op": "add_field", "entity": "Todo", "field": "due", "type": "Text", "default": "\"\""}]}
                out = json.dumps({"choices": [{"message": {"role": "assistant", "content": "```json\n" + json.dumps(ops, ensure_ascii=False) + "\n```"}}]}).encode()
                self.send_response(200); self.send_header("Content-Type", "application/json"); self.send_header("Content-Length", str(len(out))); self.end_headers(); self.wfile.write(out)
        srv = HTTPServer(("127.0.0.1", 0), H); threading.Thread(target=srv.serve_forever, daemon=True).start()
        env = dict(os.environ, AETHER_LLM_BASE_URL=f"http://127.0.0.1:{srv.server_port}/v1", AETHER_LLM_MODEL="mock-model")
        env.pop("AETHER_LLM_API_KEY", None)
        p = self.w("todo.ae", open(os.path.join(ROOT, "examples", "todo.ae"), encoding="utf-8").read())
        r = run("propose", p, "加一个截止日期", env=env); srv.shutdown()
        self.assertEqual(len(calls), 2, r.stdout)
        self.assertEqual(calls[0]["model"], "mock-model")
        self.assertIn("did not type-check", calls[1]["messages"][-1]["content"])
        self.assertIn("E234", calls[1]["messages"][-1]["content"])
        self.assertIn("due: Text", r.stdout); self.assertIn("自我修复", r.stdout)

    def test_dap_debugger(self):
        p = self.w("d.ae", "fn add(a: Int, b: Int) -> Int {\n  let s = a + b\n  s\n}\nlet r = {name: \"x\", n: 1}\nlet x = add(1, 2)\nprint(x)\n")
        c = LSPClient([BIN, "dap"])
        seq = [0]
        def req(cmd, **args):
            seq[0] += 1; c._send({"seq": seq[0], "type": "request", "command": cmd, "arguments": args})
            t = time.time()
            while time.time() - t < 10:
                for m in list(c.notes):
                    if m.get("type") == "response" and m.get("request_seq") == seq[0]:
                        c.notes.remove(m); return m
                time.sleep(0.01)
            self.fail("no response to " + cmd)
        def event(name, timeout=10):
            t = time.time()
            while time.time() - t < timeout:
                for m in list(c.notes):
                    if m.get("type") == "event" and m.get("event") == name:
                        c.notes.remove(m); return m
                time.sleep(0.01)
            self.fail("no event " + name + " " + str(c.notes))
        try:
            self.assertTrue(req("initialize", adapterID="aether")["body"]["supportsConfigurationDoneRequest"])
            event("initialized")
            req("launch", program=p, mode="run")
            self.assertEqual(req("setBreakpoints", source={"path": p}, breakpoints=[{"line": 2}])["body"]["breakpoints"][0]["verified"], True)
            req("configurationDone")
            st = event("stopped"); self.assertEqual(st["body"]["reason"], "breakpoint")
            frames = req("stackTrace", threadId=1)["body"]["stackFrames"]
            self.assertEqual([f["name"] for f in frames], ["add", "main"]); self.assertEqual(frames[0]["line"], 2)
            sc = req("scopes", frameId=frames[0]["id"])["body"]["scopes"]
            vs = {v["name"]: v["value"] for v in req("variables", variablesReference=sc[0]["variablesReference"])["body"]["variables"]}
            self.assertEqual(vs, {"a": "1", "b": "2"})
            self.assertEqual(req("evaluate", expression="a * 10", frameId=frames[0]["id"])["body"]["result"], "10")
            g = req("variables", variablesReference=sc[1]["variablesReference"])["body"]["variables"]
            rv = [v for v in g if v["name"] == "r"][0]; self.assertGreater(rv["variablesReference"], 0)
            kids = req("variables", variablesReference=rv["variablesReference"])["body"]["variables"]
            self.assertEqual({k["name"] for k in kids}, {"name", "n"})
            req("next", threadId=1); event("stopped")
            self.assertEqual(req("stackTrace", threadId=1)["body"]["stackFrames"][0]["line"], 3)
            req("stepOut", threadId=1); event("stopped")
            self.assertEqual(req("stackTrace", threadId=1)["body"]["stackFrames"][0]["name"], "main")
            req("continue", threadId=1)
            out = event("output"); self.assertEqual(out["body"]["output"], "3\n")
            event("terminated")
            req("disconnect")
        finally:
            c.close()

    def test_mvc_model_templates_deploy(self):
        m = json.loads(run("mvc", os.path.join(ROOT, "examples", "v05", "crm_mvc.ae"), "--json").stdout)
        self.assertEqual([e["name"] for e in m["model"]["entities"]], ["Customer", "Deal"])
        self.assertEqual(m["model"]["relations"][0], {"from": "Deal", "field": "customer_id", "to": "Customer", "cardinality": "N:1"})
        self.assertEqual({a["name"] for a in m["controller"]["actions"]}, {"advance", "upgrade"})
        self.assertIn({"method": "POST", "path": "/api/action/advance", "desc": "执行动作 advance", "layer": "controller", "source": "advance", "allow": ["sales"]}, m["controller"]["endpoints"])
        self.assertEqual(m["controller"]["flows"][0]["steps"][0]["retry"], 2)
        self.assertEqual(m["view"]["views"][1]["entities"], ["Deal"])
        for t in ("blank", "todo", "shop", "blog", "crm"):
            d = os.path.join(self.d, "p_" + t)
            self.assertEqual(run("new", d, "--template", t).returncode, 0)
            for f in ("main.ae", "README.md", "aether.json", ".vscode/launch.json"):
                self.assertTrue(os.path.exists(os.path.join(d, f)), f)
            self.assertEqual(run("check", os.path.join(d, "main.ae")).returncode, 0, t)
            self.assertEqual(run("test", os.path.join(d, "main.ae")).returncode, 0, t)
        d = os.path.join(self.d, "p_crm")
        r = run("deploy", os.path.join(d, "main.ae"), "--out", os.path.join(self.d, "dep")); self.assertEqual(r.returncode, 0, r.stderr)
        for f in ("aether", "run.sh", "run.bat", "Dockerfile", "aether-app.service", "app/main.ae", "部署说明.md"):
            self.assertTrue(os.path.exists(os.path.join(self.d, "dep", f)), f)
        port = free_port()
        srv = subprocess.Popen([os.path.join(self.d, "dep", "run.sh")], env=dict(os.environ, PORT=str(port)), stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            for _ in range(50):
                try: code, meta = http(f"http://127.0.0.1:{port}/api/_meta"); break
                except Exception: time.sleep(0.1)
            self.assertEqual(meta["module"], "客户管理")
        finally: srv.kill()
        self.assertTrue(os.path.exists(os.path.join(self.d, "dep", "data", "app.aedb")))

    def test_lsp_model_request(self):
        c = LSPClient([BIN, "lsp"]); c.request("initialize", {"processId": None, "rootUri": None, "capabilities": {}})
        uri = "file://" + self.w("crm.ae", open(os.path.join(ROOT, "examples", "v05", "crm_mvc.ae"), encoding="utf-8").read())
        c.notify("textDocument/didOpen", {"textDocument": {"uri": uri, "languageId": "aether", "version": 1, "text": open(uri[7:], encoding="utf-8").read()}})
        r = c.request("aether/model", {"textDocument": {"uri": uri}})["result"]
        self.assertEqual(len(r["model"]["entities"]), 2); self.assertTrue(r["controller"]["auth"]["rules"])
        pr = c.request("aether/previewRequest", {"textDocument": {"uri": uri}, "method": "POST", "path": "/api/_auth/signup", "body": {"username": "admin", "password": "123456"}})["result"]
        self.assertEqual(pr["status"], 201)
        anon = c.request("aether/previewRequest", {"textDocument": {"uri": uri}, "method": "GET", "path": "/api/Customer"})["result"]
        dev = c.request("aether/previewRequest", {"textDocument": {"uri": uri}, "method": "GET", "path": "/api/Customer", "dev": True})["result"]
        self.assertEqual((anon["status"], dev["status"]), (401, 200))  # IDE data browser bypasses login; HTTP cannot
        c.close()

    def test_inference_and_projections(self):
        p = self.w("n.ae", "entity Note { title, body, pinned = false, rank = 1, owner_id, is_public }\n")
        r = run("mvc", p); self.assertEqual(r.returncode, 0, r.stderr)
        for t in ("title: Text", "pinned: Bool", "rank: Int", "owner_id: Int", "is_public: Bool"):
            self.assertIn(t, r.stdout)
        files = [os.path.join(ROOT, "examples", f) for f in os.listdir(os.path.join(ROOT, "examples")) if f.endswith(".ae")]
        files += [os.path.join(ROOT, "examples", "v05", f) for f in os.listdir(os.path.join(ROOT, "examples", "v05"))]
        for f in files:
            c = run("compact", f); self.assertEqual(c.returncode, 0, f)
            self.assertLess(len(c.stdout), len(open(f, encoding="utf-8").read()), f)
            cp = self.w("c.ae", c.stdout); e = run("expand", cp); ep = self.w("e.ae", e.stdout)
            h = {run("hash", x).stdout.strip() for x in (f, cp, ep)}
            self.assertEqual(len(h), 1, f)
            self.assertEqual(run("check", ep).returncode, run("check", f).returncode, f)
        crm = os.path.join(ROOT, "examples", "v05", "crm_mvc.ae")
        sk = run("skeleton", crm).stdout
        self.assertIn("entity Customer", sk); self.assertNotIn("for ", sk)
        cx = json.loads(run("context", crm, "Customer", "--json").stdout)
        self.assertEqual(cx["kind"], "Entity"); self.assertTrue(cx["usedBy"])
        c = LSPClient([BIN, "lsp"]); c.request("initialize", {"processId": None, "rootUri": None, "capabilities": {}})
        uri = "file://" + self.w("crm.ae", open(crm, encoding="utf-8").read())
        c.notify("textDocument/didOpen", {"textDocument": {"uri": uri, "languageId": "aether", "version": 1, "text": open(uri[7:], encoding="utf-8").read()}})
        r = c.request("aether/context", {"textDocument": {"uri": uri}, "symbol": cx["usedBy"][0]})["result"]
        self.assertIn("Customer", " ".join(r["dependencies"]))
        r = c.request("aether/compact", {"textDocument": {"uri": uri}})["result"]
        self.assertIn("skeleton", r); c.close()

if __name__ == "__main__":
    unittest.main()
