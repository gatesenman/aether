"""Aether Studio: local web IDE for human + agent co-development."""
import json, os, threading, glob
from http.server import ThreadingHTTPServer
from .app import App, make_handler
from .checker import check_source
from .interp import Interpreter, AeThrow, show
from .errors import AeError
from .fmt import fmt
from . import agent
from .history import History, ast_hash
from .explain import explain
from .unparse import ex as unp
from .parser import parse

HERE = os.path.dirname(__file__)
def examples_dir():
    for d in (os.path.join(HERE, "examples"), os.path.join(HERE, "..", "examples")):
        if os.path.isdir(d): return d
    return HERE

def run_source(src, timeout=5):
    out = []; res = {}
    def go():
        try: Interpreter(out=out).load(src)
        except AeThrow as e: res["error"] = f"uncaught: {show(e.v)}"
        except AeError as e: res["error"] = f"[{e.code}] line {e.line}: {e.msg}"
        except RecursionError: res["error"] = "stack overflow"
        except Exception as e: res["error"] = repr(e)
    t = threading.Thread(target=go, daemon=True); t.start(); t.join(timeout)
    if t.is_alive(): res["error"] = f"timeout after {timeout}s"
    return {"output": out, **res}

class Studio:
    def __init__(s, path=None):
        s.path = path; s.preview = None; s.lock = threading.Lock()
        s.hist = History(path + ".history.json" if path else None)
    def route(s, method, path, body, role="user"):
        J = lambda o, c=200: (c, "application/json; charset=utf-8", json.dumps(o, ensure_ascii=False, default=str).encode())
        try:
            data = json.loads(body or b"{}")
            if path.startswith("/preview"):
                if not s.preview: return 200, "text/html; charset=utf-8", "<p style='font-family:sans-serif;color:#888'>点击『预览』生成应用</p>".encode()
                return s.preview.handle(method, path[len("/preview"):] or "/", body, role)
            p = path.split("?")[0]
            if p.endswith("/_aether/codicon.ttf"):
                from .app import icon_font
                return 200, "font/ttf", icon_font()
            if p == "/": return 200, "text/html; charset=utf-8", IDE_HTML.encode()
            if p == "/ide/init":
                src = open(s.path, encoding="utf-8").read() if s.path and os.path.exists(s.path) else open(os.path.join(examples_dir(), "todo.ae"), encoding="utf-8").read()
                return J({"source": src, "path": s.path, "examples": sorted(os.path.basename(f) for f in glob.glob(os.path.join(examples_dir(), "*.ae"))),
                          "llm": bool(os.environ.get("AETHER_LLM_API_KEY"))})
            if p == "/ide/example":
                name = os.path.basename(data["name"]); return J({"source": open(os.path.join(examples_dir(), name), encoding="utf-8").read()})
            if p == "/ide/check":
                ast, d = check_source(data["source"]); return J({"diagnostics": d, "ast": ast})
            if p == "/ide/run": return J(run_source(data["source"]))
            if p == "/ide/fmt":
                try: return J({"source": fmt(data["source"])})
                except AeError as e: return J({"error": e.to_json()})
            if p == "/ide/preview":
                ast, d = check_source(data["source"])
                if any(x["severity"] == "error" for x in d): return J({"ok": False, "diagnostics": d})
                with s.lock:
                    old = s.preview; s.preview = App(data["source"])
                    if old:  # carry data across edits (live schema migration)
                        for e in s.preview.it.entities:
                            if e in old.it.entities:
                                for r in old.it.store.all(e): s.preview.it.store.insert(e, r)
                return J({"ok": True})
            if p == "/ide/save":
                target = data.get("path") or s.path
                if not target: return J({"error": "no file path (start with: aether studio file.ae)"}, 400)
                open(target, "w", encoding="utf-8").write(data["source"]); return J({"saved": target})
            if p == "/history/commit":
                src = data["source"]
                if data.get("semantic_only") and s.hist.head and s.hist.head["hash"] == ast_hash(src): return J({"skipped": True, "log": s.hist.log()})
                e = s.hist.commit(src, data.get("author", "human"), data.get("reason", ""), data.get("ops"), data.get("kind", "edit"))
                return J({"entry": e, "log": s.hist.log()})
            if p == "/history/log": return J({"log": s.hist.log()})
            if p in ("/history/undo", "/history/redo"):
                src = s.hist.undo() if p.endswith("undo") else s.hist.redo(); return J({"source": src, "log": s.hist.log()})
            if p == "/history/checkout": return J({"source": s.hist.checkout(data["hash"])})
            if p == "/ide/test":
                it = Interpreter(out=[]); it.load(data["source"]); return J({"results": it.run_tests()})
            if p == "/ide/explain": return J({"text": explain(data["source"]), "hash": ast_hash(data["source"])})
            if p == "/ide/contracts":
                out = []
                for d in parse(data["source"])["decls"]:
                    for c in d.get("invariants", []): out.append({"on": d["name"], "kind": "invariant", "expr": unp(c), "line": c["line"]})
                    for c in d.get("requires", []): out.append({"on": d["name"], "kind": "requires", "expr": unp(c), "line": c["line"]})
                    for c in d.get("ensures", []): out.append({"on": d["name"], "kind": "ensures", "expr": unp(c), "line": c["line"]})
                    if d.get("allow"): out.append({"on": d["name"], "kind": "allow", "expr": ", ".join(d["allow"]), "line": d["line"]})
                return J({"contracts": out})
            if p == "/agent/schema": return J({"ops": agent.OPS_SCHEMA, "language": agent.LANG_BRIEF})
            if p == "/agent/propose": return J(agent.propose(data["source"], data["prompt"]))
            if p == "/agent/apply": return J(agent.apply_ops(data["source"], data["ops"]))
            return J({"error": "not found"}, 404)
        except AeError as e: return J({"error": e.to_json()}, 400)
        except Exception as e: return J({"error": repr(e)}, 500)

def serve(path=None, port=7777, host="127.0.0.1"):
    st = Studio(path)
    print(f"Aether Studio -> http://{host}:{port}   (agent mode: {'LLM' if os.environ.get('AETHER_LLM_API_KEY') else 'mock'})")
    ThreadingHTTPServer((host, port), make_handler(st.route)).serve_forever()

IDE_HTML = r"""<!doctype html><html><head><meta charset="utf-8"><title>Aether Studio</title><style>
*{box-sizing:border-box}html,body{height:100%;margin:0}body{font-family:system-ui,"PingFang SC","Microsoft YaHei",sans-serif;background:#0f1222;color:#dfe3f5;display:flex;flex-direction:column}
.top{display:flex;align-items:center;gap:8px;padding:8px 12px;background:#171a30;border-bottom:1px solid #262a48}
.logo{font-weight:800;background:linear-gradient(90deg,#818cf8,#c084fc);-webkit-background-clip:text;color:transparent;font-size:18px;margin-right:10px}
button,select{background:#2a2f55;color:#e6e8ff;border:1px solid #3a4075;border-radius:6px;padding:5px 11px;cursor:pointer;font:inherit;font-size:13px}button:hover{background:#373d6e}
button.pri{background:#6366f1;border-color:#6366f1}.grid{flex:1;display:grid;grid-template-columns:1.1fr 1fr 360px;grid-template-rows:1fr 230px;gap:1px;background:#262a48;min-height:0}
.pane{background:#12152a;display:flex;flex-direction:column;min-height:0;min-width:0}.ph{font-size:12px;text-transform:uppercase;letter-spacing:.06em;color:#8e94c4;padding:6px 10px;border-bottom:1px solid #22264a;display:flex;gap:8px;align-items:center}
.ph .tab{cursor:pointer}.ph .tab.on{color:#fff;border-bottom:2px solid #818cf8}
.ed{flex:1;display:flex;min-height:0;font:13px/1.55 "JetBrains Mono",Menlo,Consolas,monospace}#gut{padding:8px 6px;color:#555b8a;text-align:right;user-select:none;overflow:hidden;white-space:pre;background:#0e1124}
#src{flex:1;background:#12152a;color:#e6e8ff;border:0;outline:0;resize:none;padding:8px;font:inherit;white-space:pre;tab-size:2}
iframe{flex:1;border:0;background:#fff}.scroll{flex:1;overflow:auto;padding:8px 10px;font-size:13px}
.d{padding:5px 7px;border-radius:5px;margin-bottom:4px;cursor:pointer;background:#1a1e38}.d.error{border-left:3px solid #f87171}.d.warning{border-left:3px solid #fbbf24}.d.info{border-left:3px solid #60a5fa}
.code{font-family:Menlo,Consolas,monospace;font-size:12px;white-space:pre-wrap}.ok{color:#4ade80}.muted{color:#8088b8}
details{margin-left:12px}summary{cursor:pointer;color:#a5b4fc}.msg{background:#1a1e38;border-radius:8px;padding:8px;margin-bottom:8px}.msg.me{background:#25295a}
.diff .a{color:#4ade80}.diff .r{color:#f87171}.diff .h{color:#818cf8}#ask{display:flex;gap:6px;padding:8px;border-top:1px solid #22264a}#ask textarea{flex:1;background:#0e1124;color:#fff;border:1px solid #333a6e;border-radius:6px;padding:6px;font:inherit;height:52px;resize:none}
.badge{font-size:11px;padding:1px 6px;border-radius:9px;background:#334;color:#cbd}.chips span{display:inline-block;background:#232850;border-radius:10px;padding:2px 8px;margin:2px;font-size:12px;cursor:pointer}
@font-face{font-family:codicon;src:url("/_aether/codicon.ttf") format("truetype")}.ci{font-family:codicon;font-style:normal;vertical-align:-2px;margin-right:4px;font-size:15px}</style></head><body>
<div class="top"><span class="logo"><i class="ci">&#xeb63;</i>Aether Studio</span><select id="ex"></select>
<button onclick="check()">检查</button><button onclick="run()">运行</button><button class="pri" onclick="preview()"><i class="ci">&#xeb2c;</i>预览应用</button><button onclick="format()">格式化</button><button onclick="save()">保存</button><button onclick="runTests()"><i class="ci">&#xea79;</i>测试</button><button onclick="undo()" title="撤销（基于内容寻址历史）"><i class="ci">&#xeae2;</i>撤销</button><button onclick="redo()"><i class="ci">&#xebb0;</i>重做</button>
<span style="flex:1"></span><span id="status" class="muted"></span></div>
<div class="grid">
 <div class="pane" style="grid-row:1/3"><div class="ph">源码 · 一份描述 = 界面 + API + 数据<span id="fname" class="badge"></span></div>
  <div class="ed"><div id="gut"></div><textarea id="src" spellcheck="false"></textarea></div></div>
 <div class="pane" style="grid-column:2;grid-row:1"><div class="ph">实时预览 <a id="open" href="/preview/" target="_blank" style="color:#a5b4fc;margin-left:auto">新窗口打开</a></div><iframe id="pv" src="/preview/"></iframe></div>
 <div class="pane" style="grid-column:3;grid-row:1/3"><div class="ph"><i class="ci">&#xec20;</i>智能体协作 <span id="mode" class="badge"></span></div>
  <div class="scroll" id="chat"><div class="msg muted">用自然语言描述修改，智能体会给出<b>结构化补丁</b>（不是盲改文本），并在应用前自动类型检查。你来决定接受或拒绝。<div class="chips" id="chips"></div></div></div>
  <div id="ask"><textarea id="prompt" placeholder="例如：给 Todo 添加字段 due: Text"></textarea><button class="pri" onclick="ask()">发送</button></div></div>
 <div class="pane" style="grid-column:2;grid-row:2"><div class="ph"><span class="tab on" data-t="diag" onclick="tab(this)">诊断</span><span class="tab" data-t="out" onclick="tab(this)">输出</span><span class="tab" data-t="ast" onclick="tab(this)">AST / IR</span><span class="tab" data-t="tests" onclick="tab(this)">测试</span><span class="tab" data-t="ctr" onclick="tab(this)">契约</span><span class="tab" data-t="exp" onclick="tab(this)">解释</span><span class="tab" data-t="hist" onclick="tab(this)">历史</span><span id="hash" class="badge" style="margin-left:auto" title="语义哈希（AST 内容寻址）"></span></div>
  <div class="scroll" id="diag"></div><div class="scroll" id="out" style="display:none"></div><div class="scroll" id="ast" style="display:none"></div><div class="scroll" id="tests" style="display:none"></div><div class="scroll" id="ctr" style="display:none"></div><div class="scroll" id="exp" style="display:none"></div><div class="scroll" id="hist" style="display:none"></div></div>
</div><script>
const $=id=>document.getElementById(id),src=$('src');let timer,props={};
const post=(u,b)=>fetch(u,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(b||{})}).then(r=>r.json());
const esc=s=>String(s??'').replace(/[&<>]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;'}[c]));
function gut(){$('gut').textContent=src.value.split('\n').map((_,i)=>i+1).join('\n');$('gut').scrollTop=src.scrollTop}
src.addEventListener('scroll',gut);src.addEventListener('input',()=>{gut();clearTimeout(timer);timer=setTimeout(()=>check(true),500)});
src.addEventListener('keydown',e=>{if(e.key=='Tab'){e.preventDefault();document.execCommand('insertText',false,'  ')}if((e.ctrlKey||e.metaKey)&&e.key=='s'){e.preventDefault();save()}});
function tab(el){document.querySelectorAll('.tab').forEach(t=>t.classList.toggle('on',t==el));['diag','out','ast','tests','ctr','exp','hist'].forEach(t=>$(t).style.display=t==el.dataset.t?'':'none');if(el.dataset.t=='exp')explainP();if(el.dataset.t=='ctr')contracts();if(el.dataset.t=='hist')hist()}
const T=n=>tab(document.querySelector(`[data-t=${n}]`));
async function runTests(){const r=await post('/ide/test',{source:src.value});T('tests');const f=r.results.filter(x=>!x.ok).length;
 $('tests').innerHTML=`<div style="margin-bottom:6px"><b class="${f?'':'ok'}">${r.results.length-f} 通过 / ${f} 失败</b> <span class="muted">（test 块与 example 是语言的一部分）</span></div>`+r.results.map(x=>`<div class="d ${x.ok?'info':'error'}" onclick="goto(${x.line})">${x.ok?'<i class="ci">&#xeab2;</i>':'<i class="ci">&#xea87;</i>'} ${esc(x.name)} ${x.message?`<span class="muted">— ${esc(x.message)}</span>`:''}</div>`).join('')}
async function contracts(){const r=await post('/ide/contracts',{source:src.value});const ic={invariant:'<i class="ci">&#xeb53;</i>不变式',requires:'<i class="ci">&#xea9c;</i>前置',ensures:'<i class="ci">&#xea9b;</i>后置',allow:'<i class="ci">&#xea75;</i>权限'};
 $('ctr').innerHTML=r.contracts.length?r.contracts.map(c=>`<div class="d info" onclick="goto(${c.line})"><b>${ic[c.kind]}</b> <span class="muted">${esc(c.on)}</span> <span class="code">${esc(c.expr)}</span></div>`).join(''):'<span class="muted">没有契约。可在 fn/action 后写 requires / ensures，在 entity 中写 invariant，在 action 后写 allow 角色。</span>'}
async function explainP(){const r=await post('/ide/explain',{source:src.value});$('exp').innerHTML='<div style="white-space:pre-wrap;line-height:1.6">'+esc(r.text).replace(/\*\*(.+?)\*\*/g,'<b>$1</b>').replace(/`(.+?)`/g,'<code style="color:#c4b5fd">$1</code>').replace(/^# (.*)$/m,'<b style="font-size:15px">$1</b>').replace(/^## (.*)$/gm,'<b style="color:#a5b4fc">$1</b>')+'</div>'}
function histHtml(log){return log.slice().reverse().map(e=>`<div class="d ${e.author.startsWith('agent')?'warning':'info'}" style="${e.undone?'opacity:.45':''}" onclick="checkout('${e.hash}')">${e.current?'→ ':''}<b>#${e.id}</b> <span class="code">${e.hash}</span> <span class="badge">${e.author.startsWith('agent')?'<i class="ci">&#xec20;</i>':'<i class="ci">&#xeb99;</i>'}${esc(e.author)}</span> <span class="muted">${e.time}</span><div>${esc(e.kind)}：${esc(e.reason)}${e.ops&&e.ops.length?` <span class="muted">(${e.ops.length} 个操作)</span>`:''}${e.undone?' <i class="muted">已撤销</i>':''}</div></div>`).join('')||'<span class="muted">暂无历史</span>'}
async function hist(){const r=await post('/history/log');$('hist').innerHTML=histHtml(r.log)}
async function commit(author,reason,ops,kind,semantic){const r=await post('/history/commit',{source:src.value,author,reason,ops,kind,semantic_only:semantic});if($('hist').style.display=='')$('hist').innerHTML=histHtml(r.log)}
async function undo(){const r=await post('/history/undo');if(r.source){src.value=r.source;gut();check();preview();T('hist');$('hist').innerHTML=histHtml(r.log)}else $('status').textContent='没有可撤销的'}
async function redo(){const r=await post('/history/redo');if(r.source){src.value=r.source;gut();check();preview();T('hist');$('hist').innerHTML=histHtml(r.log)}else $('status').textContent='没有可重做的'}
async function checkout(h){if(!confirm('查看版本 '+h+'？（会作为一次新提交载入编辑器）'))return;const r=await post('/history/checkout',{hash:h});src.value=r.source;gut();await check();preview();commit('human','检出历史版本 '+h,[],'checkout')}
function tree(n,k){if(n===null||typeof n!='object')return `<div>${k?`<span class="muted">${esc(k)}:</span> `:''}${esc(JSON.stringify(n))}</div>`;
 const lbl=Array.isArray(n)?`[${n.length}]`:(n.k||'')+(n.name?' '+n.name:'');return `<details ${n.k=='Program'?'open':''}><summary>${k?esc(k)+': ':''}${esc(lbl)}${n.line?` <span class="muted">L${n.line}</span>`:''}</summary>${Object.entries(n).filter(([a])=>a!='k'&&a!='line'&&a!='span').map(([a,b])=>tree(b,Array.isArray(n)?'':a)).join('')}</details>`}
function goto(l){const lines=src.value.split('\n');let p=0;for(let i=0;i<l-1;i++)p+=lines[i].length+1;src.focus();src.setSelectionRange(p,p+(lines[l-1]||'').length)}
async function check(auto){const r=await post('/ide/check',{source:src.value});const d=r.diagnostics;
 $('diag').innerHTML=d.length?d.map(x=>`<div class="d ${x.severity}" onclick="goto(${x.line})"><b>${x.code}</b> 第${x.line}行 · ${esc(x.message)}${x.hint?`<div class="muted"><i class="ci">&#xea61;</i>${esc(x.hint)}</div>`:''}</div>`).join(''):'<span class="ok"><i class="ci">&#xeab2;</i>没有问题：类型、名称、效果、视图引用均已检查</span>';
 $('ast').innerHTML=r.ast?tree(r.ast):'<span class="muted">解析失败</span>';const errs=d.filter(x=>x.severity=='error').length;
 if(auto&&!errs)commit('human','人工编辑',[],'edit',true);post('/ide/explain',{source:src.value}).then(x=>$('hash').textContent='#'+(x.hash||''));
 $('status').innerHTML=errs?`<span style="color:#f87171">${errs} 个错误</span>`:'<span class="ok"><i class="ci">&#xeab2;</i>通过</span>';if(auto&&!errs)preview(true);return d}
async function run(){const r=await post('/ide/run',{source:src.value});tab(document.querySelector('[data-t=out]'));
 $('out').innerHTML=`<div class="code">${esc(r.output.join('\n'))}${r.error?`\n<span style="color:#f87171">${esc(r.error)}</span>`:''}</div>`||'<span class="muted">(无输出)</span>'}
async function preview(q){const r=await post('/ide/preview',{source:src.value});if(r.ok)$('pv').src='/preview/?t='+Date.now();else if(!q)check()}
async function format(){const r=await post('/ide/fmt',{source:src.value});if(r.source){src.value=r.source;gut()}else check()}
async function save(){commit('human','保存',[],'save',true);const r=await post('/ide/save',{source:src.value});$('status').textContent=r.saved?'已保存 '+r.saved:(r.error||'')}
function diffHtml(d){return '<div class="code diff">'+d.split('\n').map(l=>`<span class="${l.startsWith('+')&&!l.startsWith('+++')?'a':l.startsWith('-')&&!l.startsWith('---')?'r':l.startsWith('@@')?'h':''}">${esc(l)}</span>`).join('\n')+'</div>'}
async function ask(q){const p=q||$('prompt').value.trim();if(!p)return;$('prompt').value='';const chat=$('chat');chat.innerHTML+=`<div class="msg me">${esc(p)}</div>`;
 const r=await post('/agent/propose',{source:src.value,prompt:p});const id='p'+Date.now();r.prompt=p;props[id]=r;
 chat.innerHTML+=`<div class="msg" id="${id}"><div><i class="ci">&#xec20;</i><b>${esc(r.summary)}</b> <span class="badge">${r.mode}</span></div>`+(r.ops.length?`<details><summary>结构化操作 (${r.ops.length})</summary><div class="code">${esc(JSON.stringify(r.ops,null,1))}</div></details>${diffHtml(r.diff)}
 <div>${r.ok?'<span class="ok"><i class="ci">&#xeab2;</i>补丁通过类型检查</span>':`<span style="color:#fbbf24"><i class="ci">&#xea6c;</i>补丁后有 ${r.diagnostics.filter(x=>x.severity=='error').length} 个错误</span>`}</div>
 <div style="margin-top:6px" class="act"><button class="pri" onclick="accept('${id}')">接受</button> <button onclick="reject('${id}')">拒绝</button></div>`:'')+`</div>`;chat.scrollTop=1e9}
function accept(id){const p=props[id];src.value=p.source;gut();commit('agent:'+p.mode,(p.prompt||'')+' → '+p.summary,p.ops,'agent-patch');check();preview();$(id).querySelector('.act').innerHTML='<span class="ok">已接受并应用</span>'}
function reject(id){$(id).querySelector('.act').innerHTML='<span class="muted">已拒绝</span>'}
$('ex').onchange=async e=>{const r=await post('/ide/example',{name:e.target.value});src.value=r.source;gut();await check();preview()};
(async()=>{const r=await fetch('/ide/init').then(r=>r.json());src.value=r.source;$('fname').textContent=r.path||'(示例)';$('mode').textContent=r.llm?'LLM':'mock 模式';
 $('ex').innerHTML='<option>— 示例 —</option>'+r.examples.map(x=>`<option>${x}</option>`).join('');
 $('chips').innerHTML=['给 Todo 添加字段 due: Text','把 Todo 改名为 Task','新增实体 Note title: Text','添加统计','修复错误'].map(x=>`<span onclick="ask('${x}')">${x}</span>`).join('');gut();await check();commit('human','打开文件（基线）',[],'baseline',true);preview();const Q=new URLSearchParams(location.search),q=Q.get('ask');if(q){await ask(q);if(Q.get('accept')){accept(Object.keys(props).pop());await new Promise(r=>setTimeout(r,300))}}if(Q.get('tab')){if(Q.get('tab')=='tests')runTests();else T(Q.get('tab'))}})()
</script></body></html>"""
