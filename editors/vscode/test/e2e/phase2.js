// Beginner scenario, part 2 (inside the new project): read tutorial → MVC explorers → ask agent → accept → run → debug → deploy
const { vscode, sleep, waitFor, shot, runner, fs, path, cp } = require("./common");
exports.run = async function () {
  const { step } = runner(process.env.AETHER_E2E_OUT);
  const proj = vscode.workspace.workspaceFolders[0].uri.fsPath;
  const main = path.join(proj, "main.ae");
  let doc, fallback = false;
  await step("project opens with main.ae + Chinese README tutorial", async () => {
    await vscode.extensions.getExtension("aether-lang.aether-vscode").activate();
    try { doc = await waitFor(() => vscode.workspace.textDocuments.find((d) => d.fileName === main), 12000); }
    catch { // the wizard's "open after create" flag may not be flushed when the test host exits; open like a user would
      fallback = true; doc = await vscode.workspace.openTextDocument(main);
      vscode.commands.executeCommand("markdown.showPreviewToSide", vscode.Uri.file(path.join(proj, "README.md")));
    }
    await vscode.window.showTextDocument(doc, vscode.ViewColumn.One);
    await sleep(3000); shot("v05_e2e_03_project_opened");
  });
  if (fallback) await step("note: main.ae opened by the test (auto-open flag not persisted across test-host restart)", async () => {});
  await step("Model / Controller / View explorers + architecture (ER)", async () => {
    await vscode.commands.executeCommand("workbench.view.extension.aether"); await sleep(800);
    for (const [tab, name] of [[1, "v05_mvc_er_diagram"], [0, "v05_mvc_mapping"], [2, "v05_mvc_controller"]]) {
      await vscode.window.showTextDocument(doc, vscode.ViewColumn.One);
      await vscode.commands.executeCommand("aether.showArchitecture", tab); await sleep(2500); shot(name);
      await vscode.commands.executeCommand("workbench.action.closeActiveEditor"); await sleep(300);
    }
  });
  await step("agent: add a feature (proposal is type-checked)", async () => {
    await vscode.window.showTextDocument(doc, vscode.ViewColumn.One);
    const r = await vscode.commands.executeCommand("aether.askAgent", "给 Customer 添加字段 email: Text");
    if (!r || !r.ok) throw new Error("proposal not ok: " + JSON.stringify(r && r.diagnostics));
    await sleep(1500); shot("v05_e2e_04_agent_proposal");
  });
  await step("accept the proposal (applied + saved + history)", async () => {
    if (!(await vscode.commands.executeCommand("aether.agent.acceptLatest"))) throw new Error("accept failed");
    await waitFor(() => fs.readFileSync(main, "utf8").includes("email: Text"));
    await sleep(1200); shot("v05_e2e_05_agent_accepted");
  });
  await step("run: live preview + data browser + API tester", async () => {
    await vscode.window.showTextDocument(doc, vscode.ViewColumn.One);
    await vscode.commands.executeCommand("aether.openPreview"); await sleep(3000); shot("v05_e2e_06_run_preview");
    await vscode.window.showTextDocument(doc, vscode.ViewColumn.One);
    for (const [n, ph] of [["甲公司", "13800000001"], ["乙工作室", "13900000002"], ["丙商贸", ""]])
      await vscode.commands.executeCommand("aether._previewRequest", "POST", "/api/Customer", { name: n, phone: ph, email: n + "@example.com" });
    await vscode.commands.executeCommand("aether._previewRequest", "POST", "/api/Deal", { title: "年度合同", customer_id: 1, amount: 120000 });
    await vscode.commands.executeCommand("aether._previewRequest", "POST", "/api/Deal", { title: "试用订单", customer_id: 2, amount: 8000 });
    await vscode.commands.executeCommand("aether.dataBrowser"); await sleep(2500); shot("v05_data_browser");
    await vscode.commands.executeCommand("workbench.action.closeActiveEditor");
    await vscode.window.showTextDocument(doc, vscode.ViewColumn.One);
    await vscode.commands.executeCommand("aether.apiTester"); await sleep(2500); shot("v05_api_tester");
    await vscode.commands.executeCommand("workbench.action.closeActiveEditor");
  });
  await step("debug: breakpoint in test, stopped, step", async () => {
    await vscode.commands.executeCommand("workbench.action.closeEditorsInOtherGroups");
    await vscode.window.showTextDocument(doc, vscode.ViewColumn.One);
    const line = doc.getText().split("\n").findIndex((l) => l.trim() === "advance(d)");
    vscode.debug.addBreakpoints([new vscode.SourceBreakpoint(new vscode.Location(doc.uri, new vscode.Position(line, 0)))]);
    let stopped = false;
    const reg = vscode.debug.registerDebugAdapterTrackerFactory("aether", { createDebugAdapterTracker: () => ({ onDidSendMessage: (m) => { if (m.event === "stopped") stopped = true; } }) });
    await vscode.commands.executeCommand("aether.debugTests");
    await waitFor(() => stopped, 20000); await sleep(1500); shot("v05_e2e_07_debug_breakpoint");
    stopped = false; await vscode.commands.executeCommand("workbench.action.debug.stepOver");
    await waitFor(() => stopped, 10000); await sleep(800); shot("v05_e2e_08_debug_step");
    await vscode.commands.executeCommand("workbench.action.debug.continue");
    await waitFor(() => !vscode.debug.activeDebugSession, 15000); reg.dispose();
    vscode.debug.removeBreakpoints(vscode.debug.breakpoints);
  });
  await step("one-click deploy (local) → app responds", async () => {
    vscode.commands.executeCommand("aether.deploy");
    const dist = path.join(proj, "dist");
    await waitFor(() => fs.existsSync(path.join(dist, "deploy", "server.pid")), 30000); await sleep(1500);
    shot("v05_e2e_09_deploy");
    const port = vscode.workspace.getConfiguration("aether").get("deploy.port") || 8090;
    const ok = await waitFor(async () => { try { const r = await fetch(`http://127.0.0.1:${port}/api/_meta`); return r.status === 200 || r.status === 401; } catch { return false; } }, 15000);
    if (!ok) throw new Error("deployed app not reachable");
  });
  await step("agent context slice for a symbol", async () => {
    await vscode.window.showTextDocument(doc, vscode.ViewColumn.One); await vscode.commands.executeCommand("aether.compactView"); await sleep(1500); shot("v05_compact_skeleton");
  });
};
