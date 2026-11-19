// v0.5: Model / Controller / View made first-class in the IDE:
//  - three explorer trees (模型 / 控制 / 界面) fed by the LSP request `aether/model`
//  - Architecture panel: ER diagram, controller map (actions, permissions, endpoints, flow graphs), MVC mapping
//  - Data browser (records in the live preview store + flow runs with resume)
//  - API tester (any endpoint, identity: developer / anonymous / logged-in token)
import * as vscode from "vscode";

export type Req = <T = any>(method: string, extra?: any, doc?: vscode.TextDocument) => Promise<T | undefined>;
export type DocFn = () => vscode.TextDocument | undefined;

const goto = (doc: vscode.TextDocument | undefined, line?: number) =>
  doc && line ? { command: "vscode.open", title: "跳转", arguments: [doc.uri, { selection: new vscode.Range(line - 1, 0, line - 1, 0) }] } : undefined;

function item(label: string, icon: string, opts: { desc?: string; tip?: string; kids?: any[]; cmd?: any; ctx?: string; color?: string } = {}) {
  const it: any = new vscode.TreeItem(label, opts.kids && opts.kids.length ? vscode.TreeItemCollapsibleState.Expanded : vscode.TreeItemCollapsibleState.None);
  it.iconPath = new vscode.ThemeIcon(icon, opts.color ? new vscode.ThemeColor(opts.color) : undefined);
  it.description = opts.desc; it.tooltip = opts.tip; it.command = opts.cmd; it.kids = opts.kids ?? []; it.contextValue = opts.ctx;
  return it;
}

export class LayerTree implements vscode.TreeDataProvider<any> {
  private ev = new vscode.EventEmitter<void>();
  onDidChangeTreeData = this.ev.event;
  constructor(private layer: "model" | "controller" | "view", private req: Req, private doc: DocFn) {}
  refresh() { this.ev.fire(); }
  getTreeItem(e: any) { return e; }
  async getChildren(e?: any) {
    if (e) return e.kids;
    const doc = this.doc();
    if (!doc) return [item("打开一个 .ae 文件", "info")];
    const m = await this.req<any>("aether/model", {}, doc).catch(() => undefined);
    if (!m || !m.model) return [item("程序有语法错误，修复后显示", "warning")];
    if (this.layer === "model") {
      const rel = m.model.relations as any[];
      const ents = m.model.entities.map((e: any) => item(e.name, "symbol-class", {
        desc: `${e.fields.length} 个字段` + (e.invariants.length ? ` · ${e.invariants.length} 条规则` : ""), cmd: goto(doc, e.line), tip: "实体 = 一张数据表 + 自动生成的增删改查接口",
        kids: [
          ...e.fields.map((f: any) => item(f.name, f.ref ? "references" : "symbol-field", { desc: f.type + (f.default ? ` = ${f.default}` : "") + (f.ref ? `  → ${f.ref}` : ""), cmd: goto(doc, f.line) })),
          ...e.invariants.map((iv: string) => item(iv, "shield", { desc: "不变式", color: "charts.green" })),
        ],
      }));
      if (rel.length) ents.push(item("关系", "type-hierarchy", { kids: rel.map((r: any) => item(`${r.from}.${r.field} → ${r.to}`, "arrow-right", { desc: r.cardinality })) }));
      return ents;
    }
    if (this.layer === "controller") {
      const c = m.controller; const out: any[] = [];
      out.push(item("动作 action", "zap", { desc: String(c.actions.length), kids: c.actions.map((a: any) => item(a.name, a.allow.length ? "lock" : "symbol-event", {
        desc: `${a.endpoint}` + (a.allow.length ? ` · 需要 ${a.allow.join("/")}` : ""), cmd: goto(doc, a.line),
        kids: [...a.params.map((p: any) => item(`${p.name}: ${p.type}`, "symbol-parameter")), ...a.requires.map((r: string) => item(r, "arrow-right", { desc: "前置条件" })),
          ...a.ensures.map((r: string) => item(r, "arrow-left", { desc: "后置保证" })), ...(a.touches.length ? [item("读写 " + a.touches.join(", "), "database")] : [])],
      })) }));
      out.push(item("流程 flow", "type-hierarchy-sub", { desc: String(c.flows.length), kids: c.flows.map((f: any) => item(f.name, "run-all", {
        desc: f.endpoint, cmd: goto(doc, f.line),
        kids: f.steps.map((s: any, i: number) => item(`${i + 1}. ${s.name}`, "debug-step-over", { desc: s.retry ? `失败重试 ${s.retry} 次（退避 ${s.backoff}ms）` : "", cmd: goto(doc, s.line) })),
      })) }));
      if (c.functions.length) out.push(item("函数 fn", "symbol-method", { desc: String(c.functions.length), kids: c.functions.map((f: any) => item(f.name, "symbol-method", { desc: f.effects?.length ? "! " + f.effects.join(", ") : "纯函数", cmd: goto(doc, f.line) })) }));
      if (c.auth) out.push(item("权限 auth", "shield", { desc: `角色 ${c.auth.roles.join(", ")}`, cmd: goto(doc, c.auth.line), kids: [
        item(`登录：${c.auth.login === "required" ? "必须登录" : "可选"}`, "account"), item(`注册：${c.auth.signup === "open" ? "开放" : "关闭"}`, "person-add"),
        item(`首个注册用户成为 ${c.auth.roles[0]}`, "star-full"),
        ...c.auth.rules.map((r: any) => item(`${r.roles.join(", ")} 可以${{ read: "查看", write: "修改", manage: "管理" }[r.op as string] ?? r.op} ${r.entities.join(", ")}`, "key", { cmd: goto(doc, r.line) }))] }));
      else out.push(item("权限 auth（未声明：开发模式）", "unlock", { tip: "添加 auth { roles admin, user ... } 启用登录与权限" }));
      out.push(item("接口 API", "plug", { desc: String(c.endpoints.length), kids: c.endpoints.map((e: any) => item(`${e.method} ${e.path}`, "globe", { desc: e.desc, cmd: { command: "aether.apiTester", title: "测试", arguments: [e] } })) }));
      return out;
    }
    const v = m.view;
    const out = v.views.map((x: any) => item(x.name, "window", { desc: `${x.items} 个元素`, cmd: goto(doc, x.line), kids: [
      ...x.entities.map((e: string) => item("显示 " + e, "symbol-class", { desc: "Model" })), ...x.actions.map((a: string) => item("按钮 → " + a, "zap", { desc: "Controller" }))] }));
    if (v.theme) out.push(item("主题 theme", "symbol-color", { desc: v.theme }));
    return out;
  }
}

const css = `body{font-family:var(--vscode-font-family);color:var(--vscode-foreground);background:var(--vscode-editor-background);margin:0;padding:12px 16px;font-size:13px}
h2{font-size:15px;margin:14px 0 8px}.tabs{display:flex;gap:4px;border-bottom:1px solid var(--vscode-panel-border);margin-bottom:10px}.tab{padding:6px 12px;cursor:pointer;border-bottom:2px solid transparent;display:flex;gap:6px;align-items:center}
.tab.on{border-color:var(--vscode-focusBorder);color:var(--vscode-textLink-foreground)}table{border-collapse:collapse;width:100%}td,th{border-bottom:1px solid var(--vscode-panel-border);padding:5px 8px;text-align:left;vertical-align:top}
th{color:var(--vscode-descriptionForeground);font-weight:600}button{background:var(--vscode-button-background);color:var(--vscode-button-foreground);border:0;border-radius:3px;padding:4px 10px;cursor:pointer;display:inline-flex;gap:4px;align-items:center}
button.sec{background:var(--vscode-button-secondaryBackground);color:var(--vscode-button-secondaryForeground)}input,select,textarea{background:var(--vscode-input-background);color:var(--vscode-input-foreground);border:1px solid var(--vscode-input-border,#3c3c3c);border-radius:3px;padding:4px 6px;font:inherit}
.muted{color:var(--vscode-descriptionForeground)}.pill{display:inline-block;border-radius:9px;padding:0 7px;font-size:11px;margin-right:4px}.GET{background:#1f6f43}.POST{background:#8a5a00}.PUT{background:#1c4e8a}.DELETE{background:#8a1c1c}
.m{color:#4fc1ff}.c{color:#dcb67a}.v{color:#c586c0}.box{border:1px solid var(--vscode-panel-border);border-radius:6px;padding:8px 10px;margin:6px 0;background:var(--vscode-sideBar-background)}
pre{background:var(--vscode-textCodeBlock-background);padding:8px;overflow:auto;max-height:420px}.cols{display:grid;grid-template-columns:1fr 1fr 1fr;gap:12px}.ok{color:#73c991}.bad{color:#f48771}`;

function shell(title: string, codicons: string, csp: string, body: string, script: string) {
  return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; font-src ${csp}; style-src ${csp} 'unsafe-inline'; script-src 'unsafe-inline'; img-src ${csp} data:;">
<link rel="stylesheet" href="${codicons}"><title>${title}</title><style>${css}</style></head><body>${body}<script>const vs=acquireVsCodeApi();const $=s=>document.querySelector(s);
const esc=s=>String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));const ci=n=>'<i class="codicon codicon-'+n+'"></i>';${script}</script></body></html>`;
}

// ER diagram as SVG, computed in the webview from the model JSON
const erScript = `function er(m){const E=m.model.entities;const W=230,G=60,cols=Math.max(1,Math.min(3,E.length));const pos={};let x=0,y=0,rowH=0,svg='';
E.forEach((e,i)=>{const h=34+e.fields.length*20+e.invariants.length*18+8;if(i&&i%cols==0){y+=rowH+G;rowH=0}x=(i%cols)*(W+G);pos[e.name]={x,y,h};rowH=Math.max(rowH,h)});
const H=y+rowH+20,Wt=cols*(W+G);
m.model.relations.forEach(r=>{const a=pos[r.from],b=pos[r.to];if(!a||!b)return;const ax=a.x+W/2,ay=a.y+a.h/2,bx=b.x+W/2,by=b.y+b.h/2;
 svg+='<line x1="'+(a.x+(bx>ax?W:0))+'" y1="'+ay+'" x2="'+(b.x+(bx>ax?0:W))+'" y2="'+by+'" stroke="#dcb67a" stroke-width="2" marker-end="url(#ar)"/><text x="'+((ax+bx)/2)+'" y="'+((ay+by)/2-6)+'" fill="#dcb67a" font-size="11" text-anchor="middle">'+esc(r.field)+' '+r.cardinality+'</text>'});
E.forEach(e=>{const p=pos[e.name];svg+='<g transform="translate('+p.x+','+p.y+')"><rect width="'+W+'" height="'+p.h+'" rx="6" fill="var(--vscode-sideBar-background)" stroke="#4fc1ff"/><rect width="'+W+'" height="26" rx="6" fill="#4fc1ff33"/>'+
 '<text x="10" y="18" fill="#4fc1ff" font-weight="700">'+esc(e.name)+'</text><text x="'+(W-8)+'" y="18" text-anchor="end" fill="#888" font-size="11">entity</text>';
 let yy=44;e.fields.forEach(f=>{svg+='<text x="10" y="'+yy+'" fill="var(--vscode-foreground)" font-size="12">'+esc(f.name)+'</text><text x="'+(W-8)+'" y="'+yy+'" text-anchor="end" fill="'+(f.ref?'#dcb67a':'#9cdcfe')+'" font-size="12">'+esc(f.type)+(f.ref?' → '+esc(f.ref):'')+'</text>';yy+=20});
 e.invariants.forEach(iv=>{svg+='<text x="10" y="'+yy+'" fill="#73c991" font-size="11">不变式 '+esc(iv)+'</text>';yy+=18});svg+='</g>'});
return '<svg width="'+Wt+'" height="'+H+'" font-family="var(--vscode-editor-font-family)"><defs><marker id="ar" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0,0L10,5L0,10z" fill="#dcb67a"/></marker></defs>'+svg+'</svg>'}
function flowSvg(f){let x=0,s='';const w=150;f.steps.forEach((st,i)=>{s+='<g transform="translate('+x+',10)"><rect width="'+w+'" height="46" rx="6" fill="var(--vscode-sideBar-background)" stroke="#dcb67a"/><text x="10" y="20" fill="var(--vscode-foreground)" font-size="12">'+(i+1)+'. '+esc(st.name)+'</text><text x="10" y="37" fill="#888" font-size="11">'+(st.retry?'重试 '+st.retry+' 次 · '+st.backoff+'ms':'不重试')+'</text></g>';
 if(i<f.steps.length-1)s+='<line x1="'+(x+w)+'" y1="33" x2="'+(x+w+34)+'" y2="33" stroke="#dcb67a" stroke-width="2" marker-end="url(#ar2)"/>';x+=w+36});
 return '<svg width="'+x+'" height="66"><defs><marker id="ar2" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0,0L10,5L0,10z" fill="#dcb67a"/></marker></defs>'+s+'</svg>'}`;

export function archHtml(codicons: string, csp: string) {
  return shell("架构", codicons, csp, `<div class="tabs" id="tabs"></div><div id="body" class="muted">加载中…</div>`, erScript + `
let M=null,tab=0;const T=[['mvc','MVC 映射','layers'],['model','模型 · ER 图','database'],['ctl','控制器 · 动作/流程/权限','zap'],['view','界面','window']];
function tabs(){$('#tabs').innerHTML=T.map((t,i)=>'<div class="tab '+(i==tab?'on':'')+'" onclick="tab='+i+';draw()">'+ci(t[2])+t[1]+'</div>').join('')}
function draw(){tabs();if(!M)return;const b=$('#body');const c=M.controller;
 if(T[tab][0]=='model'){b.innerHTML='<p class="muted">实体（entity）= 数据表 + 字段类型 + 不变式；箭头表示关系（字段类型是另一个实体，或字段名以 _id 结尾）。</p>'+er(M)}
 if(T[tab][0]=='ctl'){let h='<h2>'+ci('zap')+' 动作（业务操作）</h2><table><tr><th>动作</th><th>参数</th><th>权限</th><th>前置 / 后置</th><th>读写</th><th>接口</th></tr>'+c.actions.map(a=>'<tr><td class="c"><a href="#" onclick="go('+a.line+')">'+esc(a.name)+'</a></td><td>'+a.params.map(p=>esc(p.name+': '+p.type)).join(', ')+'</td><td>'+(a.allow.length?ci('lock')+esc(a.allow.join(' / ')):'<span class="muted">所有登录者</span>')+'</td><td>'+a.requires.map(esc).join('<br>')+(a.ensures.length?'<br><span class="muted">⇒</span> '+a.ensures.map(esc).join('<br>'):'')+'</td><td class="m">'+a.touches.join(', ')+'</td><td><span class="pill POST">POST</span>/api/action/'+esc(a.name)+'</td></tr>').join('')+'</table>';
  h+='<h2>'+ci('type-hierarchy-sub')+' 流程（持久化、可恢复、可重试）</h2>'+c.flows.map(f=>'<div class="box"><b class="c">'+esc(f.name)+'</b> <span class="muted">POST /api/flow/'+esc(f.name)+'</span><br>'+flowSvg(f)+'</div>').join('');
  if(c.auth){const ents=M.model.entities.map(e=>e.name);const can=(r,e,op)=>r==c.auth.roles[0]||!c.auth.rules.some(x=>x.entities.includes(e)&&(x.op==op||x.op=='manage'||(op=='read'&&x.op=='write')))||c.auth.rules.some(x=>x.entities.includes(e)&&x.roles.includes(r)&&(x.op==op||x.op=='manage'||(op=='read'&&x.op=='write')));
   h+='<h2>'+ci('shield')+' 权限矩阵</h2><p class="muted">登录：'+(c.auth.login=='required'?'必须':'可选')+' · 注册：'+(c.auth.signup=='open'?'开放':'关闭')+' · 第一个注册的用户成为 <b>'+esc(c.auth.roles[0])+'</b></p><table><tr><th>角色</th>'+ents.map(e=>'<th class="m">'+e+'</th>').join('')+'</tr>'+c.auth.roles.map(r=>'<tr><td>'+esc(r)+'</td>'+ents.map(e=>'<td>'+(can(r,e,'read')?'<span class="ok">查看</span>':'<span class="bad">—</span>')+' / '+(can(r,e,'write')?'<span class="ok">修改</span>':'<span class="bad">—</span>')+'</td>').join('')+'</tr>').join('')+'</table>'}
  else h+='<h2>'+ci('unlock')+' 权限</h2><p class="muted">未声明 auth：开发模式（预览里可切换角色）。添加 <code>auth { roles admin, user; login required }</code> 即启用账号登录与权限。</p>';
  h+='<h2>'+ci('plug')+' 全部接口</h2><table>'+c.endpoints.map(e=>'<tr><td><span class="pill '+e.method+'">'+e.method+'</span>'+esc(e.path)+'</td><td class="'+(e.layer=='model'?'m':e.layer=='view'?'v':'c')+'">'+e.layer+'</td><td>'+esc(e.desc)+'</td><td><button class="sec" onclick=\\'vs.postMessage({type:"try",ep:'+JSON.stringify(e).replace(/'/g,"&#39;")+'})\\'>'+ci('debug-start')+'测试</button></td></tr>').join('')+'</table>';b.innerHTML=h}
 if(T[tab][0]=='view'){b.innerHTML=M.view.views.map(v=>'<div class="box"><b class="v">'+ci('window')+' '+esc(v.name)+'</b> <span class="muted">'+v.items+' 个元素</span><br>显示数据：'+v.entities.map(e=>'<span class="m">'+e+'</span>').join(', ')+'<br>按钮调用：'+(v.actions.map(a=>'<span class="c">'+a+'</span>').join(', ')||'<span class="muted">无</span>')+'</div>').join('')+(M.view.theme?'<div class="box">'+ci('symbol-color')+' 主题 '+esc(M.view.theme)+'</div>':'')}
 if(T[tab][0]=='mvc'){const col=(cls,icon,t,sub,items)=>'<div><h2 class="'+cls+'">'+ci(icon)+' '+t+'</h2><p class="muted">'+sub+'</p>'+items.join('')+'</div>';
  b.innerHTML='<p>Aether 程序天然分成三层：<b class="m">Model</b> 决定保存什么数据，<b class="c">Controller</b> 决定能做什么、谁能做，<b class="v">View</b> 决定用户看到什么。运行时三层分别变成 数据表、接口与权限、页面。</p><div class="cols">'+
  col('m','database','Model 模型','entity → 数据表 + CRUD 接口',M.model.entities.map(e=>'<div class="box"><b class="m">'+esc(e.name)+'</b><br><span class="muted">'+e.fields.map(f=>esc(f.name)).join(', ')+'</span></div>'))+
  col('c','zap','Controller 控制','action / flow / auth → 业务接口与权限',[...c.actions.map(a=>'<div class="box"><b class="c">'+esc(a.name)+'</b> → <span class="m">'+a.touches.join(', ')+'</span>'+(a.allow.length?'<br>'+ci('lock')+esc(a.allow.join('/')):'')+'</div>'),...c.flows.map(f=>'<div class="box"><b class="c">流程 '+esc(f.name)+'</b><br><span class="muted">'+f.steps.map(s=>esc(s.name)).join(' → ')+'</span></div>')])+
  col('v','window','View 界面','view / theme → 页面',M.view.views.map(v=>'<div class="box"><b class="v">'+esc(v.name)+'</b><br>'+v.entities.map(e=>'<span class="m">'+e+'</span>').join(', ')+(v.actions.length?' · '+v.actions.map(a=>'<span class="c">'+a+'</span>').join(', '):'')+'</div>'))+'</div>'}}
function go(l){vs.postMessage({type:'goto',line:l})}
window.addEventListener('message',e=>{if(e.data.type=='model'){M=e.data.m;if(e.data.tab!=null)tab=e.data.tab;draw()}});vs.postMessage({type:'ready'});`);
}

export function dataHtml(codicons: string, csp: string) {
  return shell("数据浏览", codicons, csp, `<div class="tabs" id="tabs"></div><div id="tools" style="margin-bottom:8px"></div><div id="body"></div>`, `
let M=null,cur=0,rows=[];
function tabs(){const E=M.model.entities.map(e=>e.name).concat(['流程运行记录']);$('#tabs').innerHTML=E.map((n,i)=>'<div class="tab '+(i==cur?'on':'')+'" onclick="cur='+i+';load()">'+ci(i<M.model.entities.length?'database':'history')+esc(n)+'</div>').join('')}
function load(){tabs();const E=M.model.entities;if(cur<E.length)vs.postMessage({type:'list',entity:E[cur].name});else vs.postMessage({type:'runs'})}
function draw(){const E=M.model.entities;const b=$('#body');
 if(cur>=E.length){$('#tools').innerHTML='<button class="sec" onclick="load()">'+ci('refresh')+'刷新</button> <span class="muted">每次流程运行都会持久化；失败或中断的运行可以从失败的步骤继续。</span>';
  b.innerHTML='<table><tr><th>#</th><th>流程</th><th>状态</th><th>进度</th><th>上下文 ctx</th><th>错误</th><th></th></tr>'+rows.map(r=>'<tr><td>'+r.id+'</td><td>'+esc(r.flow)+'</td><td class="'+(r.status=='done'?'ok':'bad')+'">'+esc(r.status)+'</td><td>'+(r.steps||[]).map(s=>(s.ok?'<span class="ok">'+ci('pass')+'</span>':'<span class="bad">'+ci('error')+'</span>')+esc(s.step)+(s.attempts>1?'×'+s.attempts:'')).join(' ')+'</td><td><code>'+esc(JSON.stringify(r.ctx))+'</code></td><td class="bad">'+esc(r.error||'')+'</td><td>'+(r.status!='done'?'<button onclick="vs.postMessage({type:\\'resume\\',id:'+r.id+'})">'+ci('debug-continue')+'恢复</button>':'')+'</td></tr>').join('')+'</table>'+(rows.length?'':'<p class="muted">还没有流程运行。</p>');return}
 const e=E[cur];$('#tools').innerHTML='<button onclick="add()">'+ci('add')+'新增 '+esc(e.name)+'</button> <button class="sec" onclick="load()">'+ci('refresh')+'刷新</button> <span class="muted">'+rows.length+' 条记录（预览数据库，与预览窗口共享）。双击单元格可编辑。</span>';
 b.innerHTML='<table><tr><th>id</th>'+e.fields.map(f=>'<th>'+esc(f.name)+' <span class="muted">'+esc(f.type)+'</span></th>').join('')+'<th></th></tr>'+rows.map(r=>'<tr><td>'+r.id+'</td>'+e.fields.map(f=>'<td ondblclick="edit(this,'+r.id+',\\''+f.name+'\\',\\''+f.type+'\\')">'+esc(JSON.stringify(r[f.name]))+'</td>').join('')+'<td><button class="sec" onclick="vs.postMessage({type:\\'del\\',entity:\\''+e.name+'\\',id:'+r.id+'})">'+ci('trash')+'</button></td></tr>').join('')+'</table>'}
function conv(v,t){return t.startsWith('Int')||t.startsWith('Float')?Number(v):t.startsWith('Bool')?(v=='true'):v}
function edit(td,id,f,t){const v=prompt(f+' =',td.textContent.replace(/^"|"$/g,''));if(v==null)return;vs.postMessage({type:'put',entity:M.model.entities[cur].name,id,body:{[f]:conv(v,t)}})}
function add(){const e=M.model.entities[cur];const o={};for(const f of e.fields){if(f.default)continue;const v=prompt(e.name+'.'+f.name+' ('+f.type+')','');if(v==null)return;o[f.name]=conv(v,f.type)}vs.postMessage({type:'post',entity:e.name,body:o})}
window.addEventListener('message',e=>{const m=e.data;if(m.type=='model'){M=m.m;if(m.cur!=null)cur=m.cur;load()}if(m.type=='rows'){rows=Array.isArray(m.rows)?m.rows:[];draw()}if(m.type=='error'){$('#body').innerHTML='<p class="bad">'+esc(m.msg)+'</p>'}});vs.postMessage({type:'ready'});`);
}

export function apiHtml(codicons: string, csp: string) {
  return shell("接口测试", codicons, csp, `<div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap"><select id="ep" style="max-width:320px"></select><select id="method"><option>GET</option><option>POST</option><option>PUT</option><option>DELETE</option></select>
<input id="path" style="flex:1;min-width:240px" value="/api/_meta"><select id="who"><option value="dev">身份：开发者（管理员）</option><option value="anon">身份：未登录访客</option><option value="token">身份：已登录令牌</option></select>
<button id="send">${""}<i class="codicon codicon-send"></i>发送</button></div>
<div style="margin:8px 0"><textarea id="body" rows="5" style="width:100%;box-sizing:border-box" placeholder='请求体 JSON，例如 {"name": "甲公司"}'></textarea></div>
<div class="muted" id="tok"></div><h2>响应 <span id="st"></span></h2><pre id="out" class="muted">（尚未发送）</pre><h2>curl</h2><pre id="curl"></pre><h2>历史</h2><div id="hist"></div>`, `
let M=null,token='',hist=[];
function fill(){const E=M.controller.endpoints;$('#ep').innerHTML='<option value="">选择接口…</option>'+E.map((e,i)=>'<option value="'+i+'">'+e.method+' '+esc(e.path)+' — '+esc(e.desc)+'</option>').join('')}
$('#ep').onchange=()=>{const e=M.controller.endpoints[$('#ep').value];if(!e)return;pick(e)};
function pick(e){$('#method').value=e.method;$('#path').value=e.path.replace('{id}','1');const ent=M.model.entities.find(x=>e.path=='/api/'+x.name||e.path=='/api/'+x.name+'/{id}');
 if(e.method=='POST'||e.method=='PUT'){if(ent){const o={};ent.fields.forEach(f=>o[f.name]=f.type.startsWith('Int')||f.type.startsWith('Float')?0:f.type.startsWith('Bool')?false:'');$('#body').value=JSON.stringify(o,null,1)}
 else if(e.path.includes('_auth/')){$('#body').value=JSON.stringify({username:'admin',password:'123456'},null,1)}else{const a=M.controller.actions.find(a=>e.path.endsWith('/'+a.name));$('#body').value=a?JSON.stringify(Object.fromEntries(a.params.map(p=>[p.name,1])),null,1):'{}'}}else $('#body').value=''}
$('#send').onclick=()=>{let b=null;try{b=$('#body').value.trim()?JSON.parse($('#body').value):null}catch(e){$('#out').textContent='请求体不是合法 JSON: '+e.message;return}
 const r={method:$('#method').value,path:$('#path').value,body:b,who:$('#who').value,token};vs.postMessage({type:'send',r});
 $('#curl').textContent="curl -X "+r.method+" http://127.0.0.1:8080"+r.path+(r.who=='token'&&token?" -H 'Authorization: Bearer "+token+"'":"")+(b?" -H 'Content-Type: application/json' -d '"+JSON.stringify(b)+"'":"")};
window.addEventListener('message',e=>{const m=e.data;if(m.type=='model'){M=m.m;fill();if(m.ep)pick(m.ep)}
 if(m.type=='resp'){let j=m.body;try{j=JSON.stringify(JSON.parse(m.body),null,2)}catch(_){}$('#st').innerHTML='<span class="'+(m.status<300?'ok':'bad')+'">'+m.status+'</span> <span class="muted">'+m.ms+' ms</span>';$('#out').textContent=j;$('#out').className='';
  try{const o=JSON.parse(m.body);if(o.token){token=o.token;$('#who').value='token';$('#tok').innerHTML=ci('key')+' 已保存登录令牌（'+esc(o.user&&o.user.username)+' · '+esc(o.user&&o.user.role)+'），后续请求以该身份发送'}}catch(_){}
  hist.unshift(m.req.method+' '+m.req.path+' → '+m.status);$('#hist').innerHTML=hist.slice(0,12).map(h=>'<div class="muted">'+esc(h)+'</div>').join('')}});vs.postMessage({type:'ready'});`);
}

export function registerMVC(ctx: vscode.ExtensionContext, req: Req, active: DocFn) {
  const trees = { model: new LayerTree("model", req, active), controller: new LayerTree("controller", req, active), view: new LayerTree("view", req, active) };
  ctx.subscriptions.push(vscode.window.registerTreeDataProvider("aether.modelView", trees.model), vscode.window.registerTreeDataProvider("aether.controllerView", trees.controller), vscode.window.registerTreeDataProvider("aether.viewView", trees.view));
  let t: NodeJS.Timeout | undefined;
  const refresh = () => { if (t) clearTimeout(t); t = setTimeout(() => { Object.values(trees).forEach((x) => x.refresh()); panels.forEach((p) => p.refresh()); }, 500); };
  ctx.subscriptions.push(vscode.workspace.onDidChangeTextDocument((e) => e.document.languageId === "aether" && refresh()), vscode.window.onDidChangeActiveTextEditor(refresh));
  const codicons = (w: vscode.Webview) => w.asWebviewUri(vscode.Uri.joinPath(ctx.extensionUri, "media", "codicons", "codicon.css")).toString();
  const panels: { refresh: () => void }[] = [];
  const open = (id: string, title: string, icon: string, html: (c: string, csp: string) => string, onMsg: (m: any, p: vscode.WebviewPanel, doc: vscode.TextDocument) => Promise<void>, init: any = {}) => {
    const doc = active();
    if (!doc) { vscode.window.showWarningMessage("请先打开一个 .ae 文件"); return; }
    const p = vscode.window.createWebviewPanel(id, title + " · " + doc.fileName.split(/[\\/]/).pop(), vscode.ViewColumn.Active, { enableScripts: true, retainContextWhenHidden: true, localResourceRoots: [vscode.Uri.joinPath(ctx.extensionUri, "media")] });
    p.iconPath = new vscode.ThemeIcon(icon) as any;
    p.webview.html = html(codicons(p.webview), p.webview.cspSource);
    const send = async (extra: any = {}) => p.webview.postMessage({ type: "model", m: await req("aether/model", {}, doc), ...extra });
    const entry = { refresh: () => send() };
    panels.push(entry);
    p.onDidDispose(() => panels.splice(panels.indexOf(entry), 1));
    p.webview.onDidReceiveMessage(async (m) => { if (m.type === "ready") return send(init); await onMsg(m, p, doc); });
    return p;
  };
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.showArchitecture", (tab?: number) => open("aether.arch", "架构", "layers", archHtml, async (m, p, doc) => {
    if (m.type === "goto") { const ed = await vscode.window.showTextDocument(doc, vscode.ViewColumn.One); const pos = new vscode.Position(m.line - 1, 0); ed.selection = new vscode.Selection(pos, pos); ed.revealRange(new vscode.Range(pos, pos)); }
    if (m.type === "try") vscode.commands.executeCommand("aether.apiTester", m.ep);
  }, { tab: typeof tab === "number" ? tab : 0 })));
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.dataBrowser", (cur?: number) => open("aether.data", "数据浏览", "database", dataHtml, async (m, p, doc) => {
    const call = (method: string, path: string, body?: any) => req<any>("aether/previewRequest", { method, path, body, dev: true }, doc);
    if (m.type === "list" || m.type === "post" || m.type === "put" || m.type === "del") {
      if (m.type === "post") await call("POST", "/api/" + m.entity, m.body);
      if (m.type === "put") await call("PUT", `/api/${m.entity}/${m.id}`, m.body);
      if (m.type === "del") await call("DELETE", `/api/${m.entity}/${m.id}`);
      const r = await call("GET", "/api/" + m.entity);
      const body = JSON.parse(r?.body ?? "[]");
      if (r && r.status >= 300) p.webview.postMessage({ type: "error", msg: body.error }); else p.webview.postMessage({ type: "rows", rows: body });
    }
    if (m.type === "runs" || m.type === "resume") {
      if (m.type === "resume") { const r = await req<any>("aether/resumeFlow", { id: m.id }, doc).catch((e) => ({ error: String(e) })); vscode.window.showInformationMessage(r?.ok ? `流程运行 #${m.id} 已恢复并完成` : `流程运行 #${m.id} 仍失败：${r?.error ?? (r?.steps ?? []).map((s: any) => s.error).filter(Boolean).join("; ")}`); }
      const r = await req<any>("aether/flowRuns", {}, doc);
      p.webview.postMessage({ type: "rows", rows: r?.runs ?? [] });
    }
  }, { cur: typeof cur === "number" ? cur : 0 })));
  ctx.subscriptions.push(vscode.commands.registerCommand("aether.apiTester", (ep?: any) => open("aether.api", "接口测试", "plug", apiHtml, async (m, p, doc) => {
    if (m.type !== "send") return;
    const t0 = Date.now(); const r = m.r;
    const res = await req<any>("aether/previewRequest", { method: r.method, path: r.path, body: r.body, dev: r.who === "dev", token: r.who === "token" ? r.token : "", role: r.who === "anon" ? "guest" : undefined }, doc);
    p.webview.postMessage({ type: "resp", status: res?.status, body: res?.body ?? "", ms: Date.now() - t0, req: r });
  }, { ep: ep && ep.method ? ep : undefined })));
  return trees;
}
