// Headless integration test: runs inside VS Code/VSCodium (extension host) against the real native LSP server.
const vscode = require("vscode");
const assert = require("assert");
const fs = require("fs");
const path = require("path");
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function waitFor(fn, ms = 20000) {
  const t = Date.now();
  while (Date.now() - t < ms) { const v = await fn(); if (v) return v; await sleep(200); }
  throw new Error("timeout");
}
exports.run = async function () {
  const results = [];
  const check = async (name, f) => {
    try { await f(); results.push({ name, ok: true }); } catch (e) { results.push({ name, ok: false, error: String(e && e.stack || e) }); }
  };
  const ws = process.env.AETHER_TEST_WS;
  const uri = vscode.Uri.file(path.join(ws, "todo.ae"));
  const doc = await vscode.workspace.openTextDocument(uri);
  const ed = await vscode.window.showTextDocument(doc);
  const text = doc.getText();
  const at = (needle, d = 0) => doc.positionAt(text.indexOf(needle) + d);
  await check("extension activates and language is aether", async () => {
    const ext = vscode.extensions.getExtension("aether-lang.aether-vscode");
    assert.ok(ext, "extension installed"); await ext.activate(); assert.strictEqual(doc.languageId, "aether");
  });
  await check("diagnostics: clean file has no errors", async () => {
    await sleep(1500);
    assert.strictEqual(vscode.languages.getDiagnostics(uri).filter((d) => d.severity === 0).length, 0);
  });
  await check("hover on entity reference", async () => {
    const h = await waitFor(async () => { const r = await vscode.commands.executeCommand("vscode.executeHoverProvider", uri, at("t: Todo", 4)); return r && r.length && r; });
    assert.ok(h[0].contents.map((c) => c.value || c).join("").includes("entity Todo"));
  });
  await check("go to definition", async () => {
    const d = await vscode.commands.executeCommand("vscode.executeDefinitionProvider", uri, at("t: Todo", 4));
    assert.strictEqual((d[0].range || d[0].targetRange).start.line, at("entity Todo").line);
  });
  await check("references", async () => {
    const r = await vscode.commands.executeCommand("vscode.executeReferenceProvider", uri, at("t: Todo", 4));
    assert.ok(r.length >= 10, "refs=" + r.length);
  });
  await check("rename", async () => {
    const we = await vscode.commands.executeCommand("vscode.executeDocumentRenameProvider", uri, at("t: Todo", 4), "Task");
    assert.ok(we.get(uri).length >= 10);
  });
  await check("completion", async () => {
    const c = await vscode.commands.executeCommand("vscode.executeCompletionItemProvider", uri, new vscode.Position(0, 0));
    const labels = c.items.map((i) => (typeof i.label === "string" ? i.label : i.label.label));
    for (const x of ["Todo", "toggle", "entity", "count"]) assert.ok(labels.includes(x), x);
  });
  await check("document symbols (outline)", async () => {
    const s = await vscode.commands.executeCommand("vscode.executeDocumentSymbolProvider", uri);
    assert.ok(s.some((x) => x.name === "Todo" && x.children.length === 3));
  });
  await check("semantic tokens", async () => {
    const t = await vscode.commands.executeCommand("vscode.provideDocumentSemanticTokens", uri);
    assert.ok(t && t.data.length > 500);
  });
  await check("formatting", async () => {
    const e = await vscode.commands.executeCommand("vscode.executeFormatDocumentProvider", uri, { tabSize: 2, insertSpaces: true });
    assert.ok(Array.isArray(e));
  });
  await check("diagnostic + quick fix code action after edit", async () => {
    const i = text.indexOf("count(Todo)");
    await ed.edit((b) => b.replace(new vscode.Range(doc.positionAt(i), doc.positionAt(i + 5)), "cuont"));
    const dg = await waitFor(() => vscode.languages.getDiagnostics(uri).find((d) => String(d.code) === "E200"));
    const acts = await vscode.commands.executeCommand("vscode.executeCodeActionProvider", uri, dg.range);
    assert.ok(acts.some((a) => a.title.includes("count")), acts.map((a) => a.title).join("|"));
    await vscode.commands.executeCommand("undo");
  });
  await check("agent patch offered as code action", async () => {
    const acts = await vscode.commands.executeCommand("vscode.executeCodeActionProvider", uri, new vscode.Range(at("title: Text"), at("title: Text")));
    assert.ok(acts.some((a) => a.title.startsWith("智能体补丁")));
  });
  await check("custom commands registered", async () => {
    const all = await vscode.commands.getCommands(true);
    for (const c of ["aether.openPreview", "aether.askAgent", "aether.explain", "aether.undo", "aether.runFlow", "aether.acceptProposal"]) assert.ok(all.includes(c), c);
  });
  await check("open preview webview", async () => {
    await vscode.window.showTextDocument(doc);
    await vscode.commands.executeCommand("aether.openPreview");
    await sleep(1500);
  });
  await check("v0.5 commands registered", async () => {
    const all = await vscode.commands.getCommands(true);
    for (const c of ["aether.debug", "aether.showArchitecture", "aether.dataBrowser", "aether.apiTester", "aether.newProject", "aether.deploy", "aether.serveApp"]) assert.ok(all.includes(c), c);
  });
  await check("field type error E234 (a.balanse)", async () => {
    const i = text.indexOf("t.done = not t.done");
    await ed.edit((b) => b.replace(new vscode.Range(doc.positionAt(i), doc.positionAt(i + 6)), "t.dnoe"));
    const dg = await waitFor(() => vscode.languages.getDiagnostics(uri).find((d) => String(d.code) === "E234"));
    assert.ok(dg.message.includes("dnoe"));
    await vscode.commands.executeCommand("undo");
  });
  await check("debugger: breakpoint hit, stack + variables via DAP", async () => {
    const dbgFile = path.join(path.dirname(uri.fsPath), "dbg.ae");
    fs.writeFileSync(dbgFile, "fn add(a: Int, b: Int) -> Int {\n  let s = a + b\n  s\n}\nprint(add(2, 3))\n");
    const bp = new vscode.SourceBreakpoint(new vscode.Location(vscode.Uri.file(dbgFile), new vscode.Position(1, 0)));
    vscode.debug.addBreakpoints([bp]);
    let stoppedFrame;
    const tracker = vscode.debug.registerDebugAdapterTrackerFactory("aether", { createDebugAdapterTracker: () => ({ onDidSendMessage: (m) => { if (m.type === "event" && m.event === "stopped") stoppedFrame = true; } }) });
    const ok = await vscode.debug.startDebugging(undefined, { type: "aether", request: "launch", name: "t", program: dbgFile, mode: "run" });
    assert.ok(ok);
    await waitFor(() => stoppedFrame);
    const s = vscode.debug.activeDebugSession;
    const st = await s.customRequest("stackTrace", { threadId: 1 });
    assert.strictEqual(st.stackFrames[0].name, "add");
    const sc = await s.customRequest("scopes", { frameId: st.stackFrames[0].id });
    const vs = await s.customRequest("variables", { variablesReference: sc.scopes[0].variablesReference });
    assert.deepStrictEqual(vs.variables.map((v) => v.name + "=" + v.value).sort(), ["a=2", "b=3"]);
    await s.customRequest("continue", { threadId: 1 });
    await waitFor(() => !vscode.debug.activeDebugSession);
    tracker.dispose(); vscode.debug.removeBreakpoints([bp]);
  });
  await check("MVC model request + architecture / data / API panels open", async () => {
    await vscode.window.showTextDocument(doc);
    await vscode.commands.executeCommand("aether.showArchitecture");
    await vscode.commands.executeCommand("aether.dataBrowser");
    await vscode.commands.executeCommand("aether.apiTester");
    await sleep(800);
    const tabs = vscode.window.tabGroups.all.flatMap((g) => g.tabs.map((t) => t.label));
    for (const t of ["架构", "数据浏览", "接口测试"]) assert.ok(tabs.some((x) => x.startsWith(t)), tabs.join("|"));
  });
  fs.writeFileSync(process.env.AETHER_TEST_OUT, JSON.stringify(results, null, 2));
  if (process.env.AETHER_TEST_HOLD) await sleep(+process.env.AETHER_TEST_HOLD);
  const failed = results.filter((r) => !r.ok);
  if (failed.length) throw new Error(failed.length + " failed");
};
