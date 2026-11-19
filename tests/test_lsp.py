"""v0.4: protocol-level tests for `aether lsp` (JSON-RPC over stdio, Content-Length framing)."""
import os, sys, time, unittest
sys.path.insert(0, os.path.dirname(__file__))
from lsp_client import LSPClient
from test_native import BIN, ROOT

SRC = open(os.path.join(ROOT, "examples", "todo.ae"), encoding="utf-8").read()
URI = "file:///tmp/aether-lsp-test/todo.ae"

def pos_of(text, needle, nth=0, delta=0):
    i = -1
    for _ in range(nth + 1):
        i = text.index(needle, i + 1)
    i += delta
    line = text.count("\n", 0, i)
    col = len(text[text.rfind("\n", 0, i) + 1:i].encode("utf-16-le")) // 2
    return {"line": line, "character": col}

@unittest.skipUnless(BIN, "native binary not built")
class LSPProtocol(unittest.TestCase):
    def setUp(self):
        self.c = LSPClient([BIN, "lsp"])
        self.init = self.c.request("initialize", {"processId": None, "rootUri": None, "capabilities": {}})
        self.c.notify("initialized", {})
    def tearDown(self): self.c.close()
    def open(self, text, uri=URI):
        self.c.notify("textDocument/didOpen", {"textDocument": {"uri": uri, "languageId": "aether", "version": 1, "text": text}})
        return self.diags(uri)
    def diags(self, uri=URI, timeout=5):
        t = time.time()
        while time.time() - t < timeout:
            for n in reversed(self.c.notes):
                if n.get("method") == "textDocument/publishDiagnostics" and n["params"]["uri"] == uri:
                    self.c.notes.remove(n); return n["params"]["diagnostics"]
            time.sleep(0.02)
        self.fail("no diagnostics published")
    def req(self, m, **p):
        p.setdefault("textDocument", {"uri": URI})
        r = self.c.request(m, p)
        self.assertNotIn("error", r, r); return r["result"]

    def test_initialize_capabilities(self):
        caps = self.init["result"]["capabilities"]
        for k in ["hoverProvider", "completionProvider", "definitionProvider", "referencesProvider", "renameProvider",
                  "documentSymbolProvider", "documentFormattingProvider", "semanticTokensProvider", "codeActionProvider"]:
            self.assertIn(k, caps)
        self.assertEqual(self.init["result"]["serverInfo"]["name"], "aether-lsp")

    def test_diagnostics_clean_and_error(self):
        self.assertEqual([d for d in self.open(SRC) if d["severity"] == 1], [])
        bad = SRC.replace("count(Todo)", "cuont(Todo)", 1)
        self.c.notify("textDocument/didChange", {"textDocument": {"uri": URI, "version": 2}, "contentChanges": [{"text": bad}]})
        d = self.diags()
        self.assertEqual(d[0]["code"], "E200"); self.assertEqual(d[0]["severity"], 1)
        self.assertEqual(d[0]["range"]["end"]["character"] - d[0]["range"]["start"]["character"], 5)
        acts = self.req("textDocument/codeAction", range=d[0]["range"], context={"diagnostics": d})
        fix = [a for a in acts if a["kind"] == "quickfix" and "count" in a["title"]][0]
        self.assertEqual(fix["edit"]["changes"][URI][0]["newText"], "count")

    def test_incremental_change(self):
        self.open(SRC)
        p = pos_of(SRC, "count(Todo)")
        self.c.notify("textDocument/didChange", {"textDocument": {"uri": URI, "version": 2}, "contentChanges": [
            {"range": {"start": p, "end": {"line": p["line"], "character": p["character"] + 5}}, "text": "nope"}]})
        self.assertTrue(any(d["code"] == "E200" for d in self.diags()))

    def test_hover_definition_references_rename(self):
        self.open(SRC)
        p = pos_of(SRC, "t: Todo", delta=4)
        h = self.req("textDocument/hover", position=p)
        self.assertIn("entity Todo", h["contents"]["value"])
        d = self.req("textDocument/definition", position=p)
        self.assertEqual(d["range"]["start"], pos_of(SRC, "entity Todo", delta=7))
        refs = self.req("textDocument/references", position=p, context={"includeDeclaration": True})
        self.assertEqual(len(refs), SRC.count("Todo") - SRC.count("Todos"))
        ed = self.req("textDocument/rename", position=p, newName="Task")["changes"][URI]
        self.assertEqual(len(ed), len(refs))
        self.assertTrue(all(e["newText"] == "Task" for e in ed))
        r = self.c.request("textDocument/rename", {"textDocument": {"uri": URI}, "position": p, "newName": "if"})
        self.assertIn("error", r)

    def test_completion(self):
        self.open(SRC)
        labels = {i["label"] for i in self.req("textDocument/completion", position={"line": 0, "character": 0})["items"]}
        for x in ["Todo", "toggle", "entity", "count", "Int"]:
            self.assertIn(x, labels)
        txt = SRC + "\nfn q(t: Todo) -> Int { t. }\n"
        self.c.notify("textDocument/didChange", {"textDocument": {"uri": URI, "version": 3}, "contentChanges": [{"text": txt}]})
        self.diags()
        labels = {i["label"] for i in self.req("textDocument/completion", position=pos_of(txt, "t. }", delta=2))["items"]}
        self.assertTrue({"title", "done", "priority"} <= labels, labels)

    def test_symbols_semantic_tokens_formatting(self):
        self.open(SRC)
        syms = self.req("textDocument/documentSymbol")
        names = {s["name"]: s for s in syms}
        self.assertIn("Todo", names); self.assertEqual(names["Todo"]["kind"], 23)
        self.assertEqual({c["name"] for c in names["Todo"]["children"]}, {"title", "done", "priority"})
        data = self.req("textDocument/semanticTokens/full")["data"]
        self.assertEqual(len(data) % 5, 0); self.assertGreater(len(data), 100)
        legend = self.init["result"]["capabilities"]["semanticTokensProvider"]["legend"]["tokenTypes"]
        line = col = 0; kinds = {}
        for i in range(0, len(data), 5):
            dl, dc, ln, ty = data[i:i + 4]
            line += dl; col = dc if dl else col + dc
            kinds.setdefault(legend[ty], 0); kinds[legend[ty]] += 1
        for k in ["keyword", "class", "string", "comment", "method", "property"]:
            self.assertIn(k, kinds)
        ugly = SRC.split("\n", 1)[1].replace("  title: Text", "        title: Text", 1)
        self.c.notify("textDocument/didChange", {"textDocument": {"uri": URI, "version": 4}, "contentChanges": [{"text": ugly}]})
        self.diags()
        e = self.req("textDocument/formatting", options={"tabSize": 2, "insertSpaces": True})
        self.assertEqual(len(e), 1); self.assertIn("\n  title: Text", e[0]["newText"])

    def test_agent_code_action_and_custom_requests(self):
        self.open(SRC)
        acts = self.req("textDocument/codeAction", range={"start": pos_of(SRC, "title: Text"), "end": pos_of(SRC, "title: Text")}, context={"diagnostics": []})
        ag = [a for a in acts if a["title"].startswith("智能体补丁")]
        self.assertTrue(ag); self.assertIn("note: Text", ag[0]["edit"]["changes"][URI][0]["newText"])
        self.assertEqual(ag[0]["command"]["command"], "aether.acceptProposal")
        self.assertIn("待办", self.req("aether/explain")["text"])
        self.assertTrue(any(i["kind"] == "invariant" for i in self.req("aether/contracts")["items"]))
        tests = self.req("aether/tests")["results"]; self.assertTrue(tests and all(t["ok"] for t in tests))
        self.assertEqual(self.req("aether/flows")["flows"], ["每日整理"])
        self.assertTrue(self.req("aether/runFlow", name="每日整理")["ok"])
        r = self.req("aether/previewRequest", method="POST", path="/api/Todo", body={"title": "x", "priority": 2})
        self.assertIn(r["status"], (200, 201))
        self.assertEqual(self.req("aether/previewRequest", method="POST", path="/api/action/clear_done", body={})["status"], 403)
        pr = self.req("aether/propose", prompt="给 Todo 添加字段 due: Text")
        self.assertTrue(pr["ok"]); self.assertIn("due: Text", pr["source"])
        url = self.req("aether/preview")["url"]
        import urllib.request
        self.assertIn(b"<html", urllib.request.urlopen(url, timeout=5).read())
        self.assertEqual(urllib.request.urlopen(url + "_aether/codicon.ttf", timeout=5).read()[:4], b"\x00\x01\x00\x00")

    def test_history_undo_on_save(self):
        import tempfile, shutil, json
        d = tempfile.mkdtemp(); p = os.path.join(d, "a.ae"); open(p, "w").write(SRC)
        uri = "file://" + p
        self.open(SRC, uri)
        edited = SRC.replace('title "我的待办"', 'title "我的任务"')
        self.c.notify("textDocument/didChange", {"textDocument": {"uri": uri, "version": 2}, "contentChanges": [{"text": edited}]})
        self.c.notify("textDocument/didSave", {"textDocument": {"uri": uri}})
        h = self.c.request("aether/history", {"textDocument": {"uri": uri}})["result"]["entries"]
        self.assertEqual([e["kind"] for e in h], ["baseline", "save"])
        u = self.c.request("aether/undo", {"textDocument": {"uri": uri}})["result"]
        self.assertTrue(u["ok"]); self.assertIn("我的待办", u["source"])
        self.assertTrue(os.path.exists(p + ".history.json")); shutil.rmtree(d)

    def test_unknown_method_and_framing(self):
        r = self.c.request("aether/doesNotExist", {})
        self.assertEqual(r["error"]["code"], -32601)

if __name__ == "__main__":
    unittest.main()
