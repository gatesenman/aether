// Aether VS Code / Aether IDE extension: LSP client for the native `aether lsp` server,
// app preview webview, agent panel, semantic history, contracts and Testing API integration.
import * as vscode from "vscode";
import * as path from "path";
import * as fs from "fs";
import * as cp from "child_process";
import * as os from "os";
import { registerMVC } from "./mvc";
import { LanguageClient, LanguageClientOptions, ServerOptions, TransportKind } from "vscode-languageclient/node";

let client: LanguageClient | undefined;
let out: vscode.OutputChannel;

function serverPath(ctx: vscode.ExtensionContext): string {
  const cfg = vscode.workspace.getConfiguration("aether").get<string>("server.path");
  if (cfg) return cfg;
  const exe = process.platform === "win32" ? "aether.exe" : "aether";
  const bundled = ctx.asAbsolutePath(path.join("bin", `${process.platform}-${process.arch}`, exe));
  if (fs.existsSync(bundled)) {
    try { fs.chmodSync(bundled, 0o755); } catch { /* read-only install */ }
    return bundled;
  }
  return exe; // from PATH
}

function activeAether(): vscode.TextDocument | undefined {
  const ed = vscode.window.activeTextEditor;
  if (ed && ed.document.languageId === "aether") return ed.document;
  return vscode.workspace.textDocuments.find((d) => d.languageId === "aether");
}
async function req<T = any>(method: string, extra: any = {}, doc = activeAether()): Promise<T | undefined> {
  if (!client || !doc) { vscode.window.showWarningMessage("请先打开一个 .ae 文件"); return; }
  return client.sendRequest<T>(method, { textDocument: { uri: doc.uri.toString() }, ...extra });
}
async function applyLspEdit(edit: any) {
  if (!client || !edit) return false;
  const we = await client.protocol2CodeConverter.asWorkspaceEdit(edit);
  return vscode.workspace.applyEdit(we);
}

export async function activate(ctx: vscode.ExtensionContext) {
  out = vscode.window.createOutputChannel("Aether");
  const cmd = serverPath(ctx);
  const llm = vscode.workspace.getConfiguration("aether.llm");
  const env: any = { ...process.env };
  if (llm.get<string>("baseUrl")) env.AETHER_LLM_BASE_URL = llm.get<string>("baseUrl");
  if (llm.get<string>("model")) env.AETHER_LLM_MODEL = llm.get<string>("model");
  if (llm.get<string>("apiKey")) env.AETHER_LLM_API_KEY = llm.get<string>("apiKey");
  const serverOptions: ServerOptions = { command: cmd, args: ["lsp"], transport: TransportKind.stdio, options: { env } };
  ctx.subscriptions.push(vscode.workspace.onDidChangeConfiguration(async (e) => {
    if (e.affectsConfiguration("aether.llm") && (await vscode.window.showInformationMessage("大模型设置已更改，重启语言服务器后生效。", "立即重启")) === "立即重启") vscode.commands.executeCommand("aether.restartServer");
  }));
  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ language: "aether" }],
    outputChannel: out,
    synchronize: { fileEvents: vscode.workspace.createFileSystemWatcher("**/*.ae") },
  };
  client = new LanguageClient("aether", "Aether 语言服务器", serverOptions, clientOptions);
  // 0.6: MCP server for agents (VS Code >= 1.101 API; skipped silently on older hosts)
  const lmApi: any = (vscode as any).lm;
  if (lmApi?.registerMcpServerDefinitionProvider && (vscode as any).McpStdioServerDefinition) {
    ctx.subscriptions.push(lmApi.registerMcpServerDefinitionProvider("aether.mcp", {
      provideMcpServerDefinitions: () => [new (vscode as any).McpStdioServerDefinition("Aether", cmd, ["mcp"], {}, "0.6.0")],
    }));
  }
  const runAether = (args: string[]) => new Promise<string>((resolve) => {
    require("child_process").execFile(cmd, args, { env, maxBuffer: 16 << 20 }, (_e: any, so: string, se: string) => resolve((so || "") + (se || "")));
  });
  const activeAe = () => vscode.window.activeTextEditor?.document.languageId === "aether" ? vscode.window.activeTextEditor!.document : undefined;
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.explainCode", async (code?: string) => {
    code = code || await vscode.window.showInputBox({ prompt: "错误码，例如 E200 / W300 / M401" });
    if (!code) return;
    out.appendLine(await runAether(["explain", code.trim()])); out.show(true);
  }));
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.generateAgentsMd", async () => {
    const d = activeAe(); if (!d) { vscode.window.showWarningMessage("请先打开一个 .ae 文件"); return; }
    await d.save(); out.appendLine(await runAether(["agents", d.fileName, "-w"])); out.show(true);
  }));
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.migratePlan", async () => {
    const d = activeAe(); if (!d) { vscode.window.showWarningMessage("请先打开一个 .ae 文件"); return; }
    await d.save(); out.appendLine(await runAether(["migrate", d.fileName])); out.show(true);
  }));
  const status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
  status.text = "$(loading~spin) Aether";
  status.command = "aether.askAgent";
  status.show();
  ctx.subscriptions.push(status);
  try {
    await client.start();
    status.text = "$(check-all) Aether LSP";
    status.tooltip = `aether lsp（${cmd}）已连接；点击询问智能体`;
  } catch (e: any) {
    status.text = "$(error) Aether";
    vscode.window.showErrorMessage(`无法启动 Aether 语言服务器（${cmd}）：${e?.message ?? e}。可在设置 aether.server.path 中指定。`);
  }

  // ---------- preview ----------
  let panel: vscode.WebviewPanel | undefined;
  let previewDoc: vscode.TextDocument | undefined;
  const refreshPreview = async () => {
    if (!panel || !previewDoc) return;
    const r = await req<{ url: string }>("aether/preview", {}, previewDoc);
    if (!r) return;
    const ext = await vscode.env.asExternalUri(vscode.Uri.parse(r.url));
    panel.webview.html = previewHtml(ext.toString(true), path.basename(previewDoc.fileName));
  };
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.openPreview", async () => {
    previewDoc = activeAether();
    if (!previewDoc) return;
    if (!panel) {
      panel = vscode.window.createWebviewPanel("aether.preview", "预览 " + path.basename(previewDoc.fileName), vscode.ViewColumn.Beside, { enableScripts: true, retainContextWhenHidden: true });
      panel.iconPath = vscode.Uri.joinPath(ctx.extensionUri, "media", "file-icon.svg");
      panel.onDidDispose(() => (panel = undefined));
    }
    await refreshPreview();
  }));
  let timer: NodeJS.Timeout | undefined;
  ctx.subscriptions.push(vscode.workspace.onDidChangeTextDocument((e) => {
    if (panel && previewDoc && e.document === previewDoc) {
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => panel?.webview.postMessage({ type: "reload" }), 700);
    }
    if (e.document.languageId === "aether") history.refresh(), contracts.refresh();
  }));

  // ---------- agent panel ----------
  const agent = new AgentView(ctx);
  ctx.subscriptions.push(vscode.window.registerWebviewViewProvider("aether.agent", agent, { webviewOptions: { retainContextWhenHidden: true } }));
  // automation hook (used by the e2e test and by tasks): call the live preview app as the IDE developer identity
  ctx.subscriptions.push(vscode.commands.registerCommand("aether._previewRequest", (method: string, p: string, body?: any) => req("aether/previewRequest", { method, path: p, body, dev: true })));
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.agent.acceptLatest", () => agent.acceptLatest()));
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.askAgent", async (prompt?: string) => {
    if (prompt) { await vscode.commands.executeCommand("aether.agent.focus"); return agent.ask(prompt); }
    const p = await vscode.window.showInputBox({ prompt: "描述你想要的修改（智能体返回结构化补丁，应用前做类型检查）", placeHolder: "例如：给 Todo 添加字段 due: Text" });
    if (p) { await vscode.commands.executeCommand("aether.agent.focus"); agent.ask(p); }
  }));

  // ---------- history / contracts trees ----------
  const history = new HistoryTree();
  const contracts = new ContractsTree();
  ctx.subscriptions.push(vscode.window.registerTreeDataProvider("aether.history", history));
  ctx.subscriptions.push(vscode.window.registerTreeDataProvider("aether.contractsView", contracts));
  ctx.subscriptions.push(vscode.window.onDidChangeActiveTextEditor(() => { history.refresh(); contracts.refresh(); }));
  ctx.subscriptions.push(vscode.workspace.onDidSaveTextDocument((d) => d.languageId === "aether" && history.refresh()));
  for (const which of ["undo", "redo"]) {
    ctx.subscriptions.push(vscode.commands.registerCommand("aether." + which, async () => {
      const r = await req<any>("aether/" + which);
      if (r?.ok) { await applyLspEdit(r.edit); vscode.window.setStatusBarMessage(`已${which === "undo" ? "撤销" : "重做"}到 #${r.hash}`, 3000); }
      else if (r) vscode.window.showInformationMessage(r.status);
      history.refresh();
    }));
  }
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.refreshHistory", () => history.refresh()));

  // ---------- run / flows / explain / contracts ----------
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.run", async () => {
    const r = await req<any>("aether/run");
    if (!r) return;
    out.appendLine(`$ aether run ${activeAether()?.fileName}`);
    r.output.forEach((l: string) => out.appendLine(l));
    if (r.error) out.appendLine("错误: " + r.error);
    out.show(true);
  }));
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.runFlow", async () => {
    const f = await req<any>("aether/flows");
    if (!f) return;
    const name = f.flows.length === 1 ? f.flows[0] : await vscode.window.showQuickPick(f.flows, { placeHolder: "选择要运行的流程" });
    if (!name) return;
    const r = await req<any>("aether/runFlow", { name });
    out.appendLine(`$ aether flow ${name}  →  ${r.ok ? "成功" : "失败"}`);
    for (const s of r.steps) out.appendLine(`  步骤 ${s.step}  ${s.ok ? "成功" : "失败"}  ${s.value ?? ""}`);
    out.show(true);
  }));
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.explain", async () => {
    const r = await req<any>("aether/explain");
    if (!r) return;
    const d = await vscode.workspace.openTextDocument({ language: "markdown", content: r.text });
    await vscode.window.showTextDocument(d, vscode.ViewColumn.Beside);
  }));
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.contracts", () => vscode.commands.executeCommand("aether.contractsView.focus")));
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.restartServer", async () => { await client?.restart(); }));

  // ---------- Testing API ----------
  const tc = vscode.tests.createTestController("aether", "Aether 内嵌测试");
  ctx.subscriptions.push(tc);
  const discover = async (doc: vscode.TextDocument) => {
    if (doc.languageId !== "aether") return;
    const file = tc.createTestItem(doc.uri.toString(), path.basename(doc.fileName), doc.uri);
    tc.items.add(file);
    const r = await req<any>("aether/tests", {}, doc);
    file.children.replace((r?.results ?? []).map((t: any) => {
      const it = tc.createTestItem(doc.uri + "#" + t.name, t.name, doc.uri);
      it.range = new vscode.Range(Math.max(0, t.line - 1), 0, Math.max(0, t.line - 1), 0);
      return it;
    }));
  };
  vscode.workspace.textDocuments.forEach(discover);
  ctx.subscriptions.push(vscode.workspace.onDidOpenTextDocument(discover), vscode.workspace.onDidSaveTextDocument(discover));
  tc.createRunProfile("运行", vscode.TestRunProfileKind.Run, async (request, token) => {
    const run = tc.createTestRun(request);
    const files: vscode.TestItem[] = [];
    (request.include ?? [...iter(tc.items)]).forEach((t) => files.push(t.parent ?? t));
    for (const f of new Set(files)) {
      const doc = await vscode.workspace.openTextDocument(f.uri!);
      const r = await req<any>("aether/tests", {}, doc);
      for (const t of r?.results ?? []) {
        const item = f.children.get(doc.uri + "#" + t.name);
        if (!item) continue;
        if (t.ok) run.passed(item); else run.failed(item, new vscode.TestMessage(t.message || "失败"));
      }
    }
    run.end();
  }, true);

  // ---------- v0.5: MVC explorers, data browser, API tester ----------
  const mvc = registerMVC(ctx, req as any, activeAether);

  // ---------- v0.5: debugger (DAP) ----------
  ctx.subscriptions.push(vscode.debug.registerDebugAdapterDescriptorFactory("aether", {
    createDebugAdapterDescriptor: () => new vscode.DebugAdapterExecutable(cmd, ["dap"]),
  }));
  ctx.subscriptions.push(vscode.debug.registerDebugConfigurationProvider("aether", {
    resolveDebugConfiguration(_f, c) {
      if (!c.type && !c.request) { const d = activeAether(); if (!d) return undefined; c.type = "aether"; c.request = "launch"; c.name = "Aether: 调试"; c.mode = "run"; }
      if (!c.program) c.program = activeAether()?.fileName;
      if (!c.program) { vscode.window.showWarningMessage("请先打开一个 .ae 文件"); return undefined; }
      return c;
    },
  }));
  const debugWith = (mode: string, extra: any = {}) => async () => {
    const d = activeAether(); if (!d) return;
    if (d.isDirty) await d.save();
    await vscode.debug.startDebugging(vscode.workspace.getWorkspaceFolder(d.uri), { type: "aether", request: "launch", name: "Aether: " + (mode === "test" ? "调试测试" : "调试"), program: d.fileName, mode, ...extra });
  };
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.debug", debugWith("run")), vscode.commands.registerCommand("aether.debugTests", debugWith("test")));

  // ---------- v0.5: new project wizard, one-click run & deploy ----------
  const templates = [
    { id: "blank", label: "$(file) 空白项目", detail: "一个实体、一个动作、一个页面，适合从零开始" },
    { id: "todo", label: "$(checklist) 待办清单", detail: "最经典的入门示例：增删改查、统计、流程" },
    { id: "shop", label: "$(lock) 小店（登录与权限）", detail: "账号登录、角色权限、主题与布局、可重试流程" },
    { id: "blog", label: "$(book) 博客", detail: "文章与评论，多个实体与页面" },
    { id: "crm", label: "$(layers) 客户管理（MVC 示例）", detail: "清楚展示 Model / Controller / View 三层" },
  ];
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.newProject", async (tplId?: string, target?: string) => {
    let tpl = templates.find((t) => t.id === tplId);
    if (!tpl) tpl = await vscode.window.showQuickPick(templates, { title: "新建 Aether 项目（1/2）：选择模板", placeHolder: "不知道选哪个？选「待办清单」" });
    if (!tpl) return;
    if (!target) {
      const name = await vscode.window.showInputBox({ title: "新建 Aether 项目（2/2）：项目名称", value: tpl.id === "blank" ? "我的应用" : tpl.id, prompt: "项目会创建在 ~/AetherProjects/ 下（可在设置 aether.projectsDir 修改）" });
      if (!name) return;
      const base = vscode.workspace.getConfiguration("aether").get<string>("projectsDir") || path.join(os.homedir(), "AetherProjects");
      fs.mkdirSync(base, { recursive: true });
      target = path.join(base, name);
    }
    const r = cp.spawnSync(cmd, ["new", target, "--template", tpl.id], { encoding: "utf8" });
    out.appendLine(r.stdout + r.stderr);
    if (r.status !== 0) { vscode.window.showErrorMessage("创建失败：" + (r.stderr || r.stdout)); return; }
    await ctx.globalState.update("aether.openAfterCreate", path.join(target, "main.ae"));
    await vscode.commands.executeCommand("vscode.openFolder", vscode.Uri.file(target), { forceNewWindow: false });
  }));
  const pending = ctx.globalState.get<string>("aether.openAfterCreate");
  if (pending && vscode.workspace.workspaceFolders?.some((f) => pending.startsWith(f.uri.fsPath)) && fs.existsSync(pending)) {
    await ctx.globalState.update("aether.openAfterCreate", undefined);
    const d = await vscode.workspace.openTextDocument(pending);
    await vscode.window.showTextDocument(d);
    const readme = path.join(path.dirname(pending), "README.md");
    if (fs.existsSync(readme)) vscode.commands.executeCommand("markdown.showPreviewToSide", vscode.Uri.file(readme));
  }
  let serveTerm: vscode.Terminal | undefined;
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.serveApp", async () => {
    if (!vscode.workspace.isTrusted) { vscode.window.showWarningMessage("此操作会启动进程：请先信任此文件夹（左下角「受限模式」→ 信任）。", "管理信任").then((x) => x && vscode.commands.executeCommand("workbench.trust.manage")); return; }
    const d = activeAether(); if (!d) return;
    if (d.isDirty) await d.save();
    const port = vscode.workspace.getConfiguration("aether").get<number>("serve.port") || 8080;
    serveTerm?.dispose();
    serveTerm = vscode.window.createTerminal({ name: "Aether 应用", iconPath: new vscode.ThemeIcon("globe") });
    serveTerm.sendText(`"${cmd}" serve "${d.fileName}" --port ${port}`);
    serveTerm.show(true);
    setTimeout(() => vscode.env.openExternal(vscode.Uri.parse(`http://127.0.0.1:${port}/`)), 1200);
  }));
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.agentContext", async () => {
    const ed = vscode.window.activeTextEditor;
    const word = ed ? ed.document.getText(ed.document.getWordRangeAtPosition(ed.selection.active)) : "";
    const sym = await vscode.window.showInputBox({ prompt: "要给 Agent 的符号（实体/动作/函数/页面名）", value: word });
    if (!sym) return;
    try {
      const r: any = await req("aether/context", { symbol: sym });
      if (!r) return;
      await vscode.env.clipboard.writeText(r.text);
      const doc = await vscode.workspace.openTextDocument({ language: "aether", content: r.text });
      vscode.window.showTextDocument(doc, { preview: true, viewColumn: vscode.ViewColumn.Beside });
    } catch (e: any) { vscode.window.showErrorMessage(String(e?.message ?? e)); }
  }));
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.compactView", async () => {
    const r: any = await req("aether/compact");
    if (!r) return;
    const doc = await vscode.workspace.openTextDocument({ language: "aether", content: "// compact（给 Agent 的最少 token 形式）\n" + r.compact + "\n// skeleton（概要）\n" + r.skeleton });
    vscode.window.showTextDocument(doc, { preview: true, viewColumn: vscode.ViewColumn.Beside });
  }));
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.deploy", async () => {
    if (!vscode.workspace.isTrusted) { vscode.window.showWarningMessage("此操作会启动进程：请先信任此文件夹（左下角「受限模式」→ 信任）。", "管理信任").then((x) => x && vscode.commands.executeCommand("workbench.trust.manage")); return; }
    const d = activeAether(); if (!d) return;
    if (d.isDirty) await d.save();
    const port = vscode.workspace.getConfiguration("aether").get<number>("deploy.port") || 8090;
    await vscode.window.withProgress({ location: vscode.ProgressLocation.Notification, title: "Aether：正在打包并部署…" }, async () => {
      const r = cp.spawnSync(cmd, ["deploy", d.fileName, "--target", "local", "--port", String(port)], { encoding: "utf8", cwd: path.dirname(d.fileName) });
      out.appendLine(`$ aether deploy ${d.fileName} --target local\n${r.stdout}${r.stderr}`);
      if (r.status !== 0) { out.show(true); vscode.window.showErrorMessage("部署失败（详见输出面板）：" + (r.stderr || r.stdout).split("\n")[0]); return; }
      const url = /http:\/\/127\.0\.0\.1:\d+\//.exec(r.stdout)?.[0] ?? "";
      const dir = /部署包已生成: (.*)/.exec(r.stdout)?.[1] ?? "";
      const pick = await vscode.window.showInformationMessage(`已部署：${url}（部署包：${dir}，含 Dockerfile、run.sh、systemd 服务文件）`, "在浏览器打开", "打开部署包");
      if (pick === "在浏览器打开") vscode.env.openExternal(vscode.Uri.parse(url));
      if (pick === "打开部署包") vscode.commands.executeCommand("revealFileInOS", vscode.Uri.file(path.join(dir, "部署说明.md")));
    });
  }));

  // ---------- Aether IDE first-run defaults (only inside the Aether IDE distribution) ----------
  if (/aether/i.test(vscode.env.appName) && !ctx.globalState.get("aether.ide.defaultsApplied")) {
    const ws = vscode.workspace.getConfiguration();
    if (!ws.inspect("workbench.colorTheme")?.globalValue) await ws.update("workbench.colorTheme", "Aether Dark", vscode.ConfigurationTarget.Global);
    await ctx.globalState.update("aether.ide.defaultsApplied", true);
    vscode.commands.executeCommand("workbench.action.openWalkthrough", "aether-lang.aether-vscode#aether.gettingStarted", false);
  }
  // Fix (v0.5 / v0.5.1): a webview view restored in an already-visible sidebar before the provider was
  // registered stays blank until focused. Resolve it on activate and again on window focus / startup.
  const revealAgent = async () => {
    try {
      await vscode.commands.executeCommand("aether.agent.focus");
      // Ensure the webview document is actually created even if the view was "visible" but empty.
      if (!agent.view) {
        await vscode.commands.executeCommand("workbench.view.extension.aether");
        await vscode.commands.executeCommand("aether.agent.focus");
      }
    } catch { /* view container may not exist outside Aether IDE */ }
  };
  const wantAgent = /aether/i.test(vscode.env.appName) || vscode.workspace.getConfiguration("aether").get<boolean>("agent.revealOnStartup");
  if (wantAgent) {
    setTimeout(async () => {
      await revealAgent();
      await vscode.commands.executeCommand("workbench.action.focusActiveEditorGroup");
    }, 200);
    setTimeout(async () => { if (!agent.view) await revealAgent(); }, 1200);
  }
  ctx.subscriptions.push(vscode.window.onDidChangeWindowState(async (s) => {
    if (s.focused && wantAgent && !agent.view) await revealAgent();
  }));
}

function* iter(c: vscode.TestItemCollection) { const a: vscode.TestItem[] = []; c.forEach((t) => a.push(t)); yield* a; }

function previewHtml(url: string, name: string) {
  return `<!doctype html><html><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; frame-src http://127.0.0.1:* http://localhost:* https:; style-src 'unsafe-inline'; script-src 'unsafe-inline';">
<style>html,body{margin:0;padding:0;height:100%;background:#f5f6fa}iframe{border:0;width:100%;height:100vh}</style></head>
<body><iframe id="f" src="${url}" title="${name}"></iframe>
<script>const f=document.getElementById('f');window.addEventListener('message',e=>{if(e.data&&e.data.type==='reload'){f.src='${url}?t='+Date.now()}});</script></body></html>`;
}

class AgentView implements vscode.WebviewViewProvider {
  view?: vscode.WebviewView;
  constructor(private ctx: vscode.ExtensionContext) {}
  resolveWebviewView(v: vscode.WebviewView) {
    this.view = v;
    const codicons = vscode.Uri.joinPath(this.ctx.extensionUri, "media", "codicons", "codicon.css");
    v.webview.options = { enableScripts: true, localResourceRoots: [vscode.Uri.joinPath(this.ctx.extensionUri, "media")] };
    const paint = () => { v.webview.html = agentHtml(v.webview.asWebviewUri(codicons).toString(), v.webview.cspSource); };
    paint();
    // v0.5.1: if VS Code restores a blank webview shell, repaint when it becomes visible again.
    v.onDidChangeVisibility(() => { if (v.visible) paint(); });
    v.webview.onDidReceiveMessage(async (m) => {
      if (m.type === "ask") this.ask(m.prompt);
      if (m.type === "accept") await this.accept(m.uri, m.id, m.edit);
    });
  }
  last?: { uri: string; r: any };
  async accept(uri: string, id: number, edit: any) {
    const doc = vscode.workspace.textDocuments.find((d) => d.uri.toString() === uri);
    if (!(await applyLspEdit(edit))) return false;
    await client?.sendRequest("workspace/executeCommand", { command: "aether.acceptProposal", arguments: [uri, id] });
    this.view?.webview.postMessage({ type: "accepted", id });
    if (doc) await doc.save();
    vscode.commands.executeCommand("aether.refreshHistory");
    return true;
  }
  async acceptLatest() {
    if (!this.last || !this.last.r?.ok) { vscode.window.showInformationMessage("没有可接受的智能体建议"); return false; }
    const { uri, r } = this.last; this.last = undefined;
    return this.accept(uri, r.id, r.edit);
  }
  async ask(prompt: string) {
    const doc = activeAether();
    if (!doc) return;
    this.view?.webview.postMessage({ type: "me", prompt });
    const r = await req<any>("aether/propose", { prompt }, doc);
    this.view?.webview.postMessage({ type: "proposal", uri: doc.uri.toString(), r });
    this.last = { uri: doc.uri.toString(), r };
    return r;
  }
}

function agentHtml(codicons: string, csp: string) {
  return `<!doctype html><html><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; font-src ${csp}; style-src ${csp} 'unsafe-inline'; script-src 'unsafe-inline';">
<link rel="stylesheet" href="${codicons}">
<style>body{font-family:var(--vscode-font-family);font-size:13px;color:var(--vscode-foreground);padding:6px 10px}
.hint{color:var(--vscode-descriptionForeground);font-size:12px;margin-bottom:8px}.chip{display:inline-block;background:var(--vscode-input-background);border-radius:4px;padding:2px 8px;margin:0 4px 4px 0;cursor:pointer;font-size:12px}
.who{color:var(--vscode-descriptionForeground);font-size:12px;margin-top:12px;display:flex;gap:4px;align-items:center}
pre{background:var(--vscode-editor-background);border:1px solid var(--vscode-panel-border);padding:6px;font-size:12px;overflow:auto;white-space:pre}
.add{color:#9ece9e;background:#1e3a2480;display:block}.del{color:#f19999;background:#4b181880;display:block}.hunk{color:#3794ff}
.ok{color:#73c991}.warn{color:#cca700}button{background:var(--vscode-button-background);color:var(--vscode-button-foreground);border:0;border-radius:3px;padding:4px 10px;cursor:pointer;margin-right:6px;display:inline-flex;gap:4px;align-items:center}
button.sec{background:var(--vscode-button-secondaryBackground);color:var(--vscode-button-secondaryForeground)}
textarea{width:100%;box-sizing:border-box;background:var(--vscode-input-background);color:var(--vscode-input-foreground);border:1px solid var(--vscode-input-border,#3c3c3c);border-radius:4px;padding:6px;font:inherit;resize:vertical;min-height:54px}
#bar{position:sticky;bottom:0;background:var(--vscode-sideBar-background);padding-top:8px}</style></head><body>
<div class="hint">用自然语言描述修改；智能体返回<b>结构化补丁</b>（不是盲改文本），应用前由语言服务器做类型检查，你决定接受或拒绝。补丁也会作为代码操作（Ctrl+.）出现。</div>
<div id="chips"><span class="chip">给 Todo 添加字段 due: Text</span><span class="chip">添加统计</span><span class="chip">修复错误</span></div>
<div id="log"></div>
<div id="bar"><textarea id="p" placeholder="描述你想要的修改…（Ctrl+Enter 发送）"></textarea>
<button id="send"><i class="codicon codicon-send"></i>发送</button></div>
<script>const vs=acquireVsCodeApi();const $=s=>document.querySelector(s);const log=$('#log');
const esc=s=>String(s).replace(/[&<>]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;'}[c]));
function send(t){t=t||$('#p').value;if(!t.trim())return;vs.postMessage({type:'ask',prompt:t});$('#p').value=''}
$('#send').onclick=()=>send();$('#p').onkeydown=e=>{if(e.key==='Enter'&&e.ctrlKey)send()};
document.querySelectorAll('.chip').forEach(c=>c.onclick=()=>send(c.textContent));
window.addEventListener('message',e=>{const m=e.data;
 if(m.type==='me'){log.insertAdjacentHTML('beforeend','<div class="who"><i class="codicon codicon-account"></i>你</div><div>'+esc(m.prompt)+'</div>')}
 if(m.type==='proposal'){const r=m.r;const diff=(r.diff||'').split('\\n').map(l=>'<span class="'+(l.startsWith('+++')||l.startsWith('---')?'':l.startsWith('+')?'add':l.startsWith('-')?'del':l.startsWith('@@')?'hunk':'')+'">'+esc(l)+'</span>').join('\\n');
  let h='<div class="who"><i class="codicon codicon-robot"></i>Aether 智能体 · '+esc(r.mode)+'</div><div>'+esc(r.summary)+'</div>';
  if(r.ops&&r.ops.length){h+='<pre>'+diff+'</pre>'+(r.ok?'<div class="ok"><i class="codicon codicon-pass"></i> 补丁通过类型检查</div>':'<div class="warn"><i class="codicon codicon-warning"></i> 补丁后有 '+r.diagnostics.length+' 个问题</div>')+
   '<div id="a'+r.id+'" style="margin-top:6px"><button data-id="'+r.id+'"><i class="codicon codicon-check"></i>接受</button><button class="sec" onclick="this.parentNode.innerHTML=\\'已拒绝\\'"><i class="codicon codicon-close"></i>拒绝</button></div>'}
  log.insertAdjacentHTML('beforeend',h);const b=document.querySelector('#a'+r.id+' button[data-id]');if(b)b.onclick=()=>vs.postMessage({type:'accept',uri:m.uri,id:r.id,edit:r.edit});window.scrollTo(0,document.body.scrollHeight)}
 if(m.type==='accepted'){const d=$('#a'+m.id);if(d)d.innerHTML='<span class="ok"><i class="codicon codicon-check-all"></i> 已接受并应用（已记入历史）</span>'}
});</script></body></html>`;
}

class HistoryTree implements vscode.TreeDataProvider<any> {
  private ev = new vscode.EventEmitter<void>();
  onDidChangeTreeData = this.ev.event;
  refresh() { this.ev.fire(); }
  getTreeItem(e: any) { return e; }
  async getChildren() {
    const r = await (activeAether() && client ? req<any>("aether/history") : undefined);
    return (r?.entries ?? []).slice().reverse().map((e: any) => {
      const it = new vscode.TreeItem(e.reason);
      it.description = `#${e.id} ${e.hash.slice(0, 6)} · ${e.author} · ${e.time.slice(11)}`;
      it.iconPath = new vscode.ThemeIcon(e.head ? "circle-filled" : e.author.startsWith("agent") ? "robot" : "account", e.head ? new vscode.ThemeColor("testing.iconPassed") : undefined);
      if (e.undone) it.description += "（已撤销）";
      it.tooltip = `${e.kind}${e.ops ? `，${e.ops} 个结构化操作` : ""}`;
      return it;
    });
  }
}
class ContractsTree implements vscode.TreeDataProvider<any> {
  private ev = new vscode.EventEmitter<void>();
  onDidChangeTreeData = this.ev.event;
  refresh() { this.ev.fire(); }
  getTreeItem(e: any) { return e; }
  async getChildren() {
    const doc = activeAether();
    const r = await (doc && client ? req<any>("aether/contracts") : undefined);
    const icons: any = { invariant: "shield", requires: "arrow-right", ensures: "arrow-left", allow: "lock" };
    return (r?.items ?? []).map((c: any) => {
      const it = new vscode.TreeItem(`${c.kind}  ${c.owner}`);
      it.description = c.text;
      it.iconPath = new vscode.ThemeIcon(icons[c.kind] ?? "symbol-misc");
      it.command = { command: "vscode.open", title: "跳转", arguments: [doc!.uri, { selection: new vscode.Range(c.line - 1, 0, c.line - 1, 0) }] };
      return it;
    });
  }
}

export async function deactivate() { await client?.stop(); }
