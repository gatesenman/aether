"""Unified app compiler: one .ae program -> storage schema + REST API + auto-rendered web UI."""
import json, re, os
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
from .interp import Interpreter, show, AeThrow
from .errors import AeError

class App:
    def __init__(s, src, db_path=":memory:", base_dir="."):
        s.src = src; s.it = Interpreter(db_path=db_path, out=[]); s.it.load(src, base_dir)
    # ---------- compile app model -> UI spec
    def entity_spec(s, e):
        return {"name": e["name"], "fields": [{"name": f["name"], "type": f["type"]["name"]} for f in e["fields"]],
                "row_actions": [a["name"] for a in s.it.actions.values()
                                if len(a["params"]) == 1 and a["params"][0]["type"] and a["params"][0]["type"]["name"] == e["name"]]}
    def spec(s):
        it = s.it; views = []
        src_views = list(it.views.values()) or [{"name": e, "items": [{"kind":"list","args":[{"k":"Name","name":e}]},{"kind":"form","args":[{"k":"Name","name":e}]}]} for e in it.entities]
        for v in src_views:
            items = []
            for i in v["items"]:
                kind = i["kind"]; args = i["args"]; el = {"kind": kind}
                try:
                    if kind in ("list","table","form","chart"):
                        el["entity"] = args[0]["name"]
                        if kind == "chart": el["field"] = args[1]["name"] if len(args) > 1 else None
                        if kind == "list" and len(args) > 1: el["label_field"] = args[1]["name"]
                    elif kind == "button":
                        el["label"] = it.eval(args[0], it.globals); el["action"] = next(a["target"] for a in args if a["k"] == "Arrow")
                    elif kind == "stat":
                        el["label"] = it.eval(args[0], it.globals); el["value"] = show(it.eval(args[1], it.globals))
                    else:
                        val = it.eval(args[0], it.globals) if args else ""
                        el["text"] = val if isinstance(val, str) else show(val)
                except (AeError, AeThrow, IndexError, StopIteration, KeyError) as ex:
                    el = {"kind": "error", "text": f"{kind}: {getattr(ex, 'msg', ex)}"}
                items.append(el)
            views.append({"name": v["name"], "items": items})
        return {"module": it.module, "entities": [s.entity_spec(e) for e in it.entities.values()],
                "flows": list(it.flows), "actions": [{"name": a["name"], "allow": a.get("allow", []), "params": [{"name": p["name"], "type": p["type"]["name"] if p["type"] else "Any"} for p in a["params"]]} for a in it.actions.values()],
                "views": views}
    def openapi(s):
        paths = {}
        for e in s.it.entities:
            paths[f"/api/{e}"] = {"get": {"summary": f"list {e}"}, "post": {"summary": f"create {e}"}}
            paths[f"/api/{e}/{{id}}"] = {"get": {}, "put": {}, "delete": {}}
        for a in s.it.actions: paths[f"/api/action/{a}"] = {"post": {"summary": f"run action {a}"}}
        return {"openapi": "3.0.0", "info": {"title": s.it.module, "version": "1"}, "paths": paths}
    # ---------- request handling (framework-free so it can be embedded in the IDE)
    def handle(s, method, path, body=b"", role="user"):
        path = path.split("?")[0].rstrip("/") or "/"
        try:
            data = json.loads(body or b"{}") if body else {}
            if path.endswith("/_aether/codicon.ttf"): return 200, "font/ttf", icon_font()
            if path == "/" : return 200, "text/html; charset=utf-8", UI_HTML.encode()
            if path == "/api/_meta": return s.ok(s.spec())
            if path == "/api/_openapi": return s.ok(s.openapi())
            m = re.fullmatch(r"/api/action/(\w+)", path)
            if m and method == "POST":
                if m.group(1) not in s.it.actions: return s.err(404, f"no action {m.group(1)}")
                before = len(s.it.out); r = s.it.run_action(m.group(1), data, role)
                return s.ok({"result": r if isinstance(r, (int, float, str, list, dict, bool, type(None))) else show(r), "output": s.it.out[before:]})
            m = re.fullmatch(r"/api/flow/(\w+)", path)
            if m and method == "POST": return s.ok(s.it.run_flow(m.group(1), data))
            m = re.fullmatch(r"/api/(\w+)(?:/(\d+))?", path)
            if m and m.group(1) in s.it.entities:
                e, id = m.group(1), m.group(2); st = s.it.store
                if not id and method == "GET": return s.ok(st.all(e))
                if not id and method == "POST":
                    rec = s.it.new_record(s.it.entities[e], data); s.it.check_inv(e, rec); return s.ok(st.insert(e, rec), 201)
                if id and method == "GET":
                    r = st.get(e, int(id)); return s.ok(r) if r else s.err(404, "not found")
                if id and method in ("PUT", "PATCH"):
                    cur = st.get(e, int(id)) or {}; cur.update(data); s.it.check_inv(e, cur); return s.ok(st.update(e, int(id), data))
                if id and method == "DELETE": st.delete(e, int(id)); return s.ok({"deleted": int(id)})
            return s.err(404, f"no route {method} {path}")
        except PermissionError as ex: return s.err(403, str(ex))
        except AeThrow as ex: return s.err(400, show(ex.v))
        except AeError as ex: return s.err(400, ex.msg)
        except Exception as ex: return s.err(500, repr(ex))
    def ok(s, obj, code=200): return code, "application/json; charset=utf-8", json.dumps(obj, ensure_ascii=False, default=show).encode()
    def err(s, code, msg): return s.ok({"error": msg}, code)

def make_handler(route):
    class H(BaseHTTPRequestHandler):
        def _go(self):
            n = int(self.headers.get("Content-Length") or 0); body = self.rfile.read(n) if n else b""
            code, ctype, out = route(self.command, self.path, body, self.headers.get("X-Aether-Role") or "user")
            self.send_response(code); self.send_header("Content-Type", ctype); self.send_header("Content-Length", str(len(out)))
            self.end_headers(); self.wfile.write(out)
        do_GET = do_POST = do_PUT = do_DELETE = do_PATCH = _go
        def log_message(self, *a): pass
    return H

def serve(path, port=8080, db=None, host="127.0.0.1"):
    src = open(path, encoding="utf-8").read()
    db = db or os.path.splitext(path)[0] + ".db"
    app = App(src, db, os.path.dirname(os.path.abspath(path)))
    print(f"Aether app '{app.it.module}' -> http://{host}:{port}  (API: /api/_meta, /api/_openapi, storage: {db})")
    ThreadingHTTPServer((host, port), make_handler(app.handle)).serve_forever()

UI_HTML = r"""<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Aether App</title><style>
*{box-sizing:border-box}body{font-family:system-ui,-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;margin:0;background:#f5f6fa;color:#1d2433}
header{background:linear-gradient(90deg,#4f46e5,#7c3aed);color:#fff;padding:14px 22px;display:flex;gap:18px;align-items:center}
header b{font-size:18px}nav a{color:#e0e7ff;margin-right:12px;cursor:pointer;text-decoration:none}nav a.on{color:#fff;font-weight:600;border-bottom:2px solid #fff}
main{max-width:860px;margin:20px auto;padding:0 16px}.card{background:#fff;border-radius:12px;padding:16px 18px;margin-bottom:14px;box-shadow:0 1px 3px #0001}
h1{font-size:24px;margin:6px 0 14px}.row{display:flex;align-items:center;gap:10px;padding:8px 0;border-bottom:1px solid #eef}
.row:last-child{border:0}.grow{flex:1}.muted{color:#778}button{background:#4f46e5;color:#fff;border:0;border-radius:8px;padding:6px 12px;cursor:pointer}
button.ghost{background:#eef;color:#334}button.del{background:#fee;color:#b91c1c}input,textarea{border:1px solid #d5d8e6;border-radius:8px;padding:7px 9px;font:inherit}
form{display:flex;flex-wrap:wrap;gap:8px;align-items:center}.stat{display:inline-block;margin-right:28px}.stat .v{font-size:28px;font-weight:700;color:#4f46e5}
.done{text-decoration:line-through;color:#99a}.bar{background:#818cf8;height:18px;border-radius:4px}.err{color:#b91c1c}table{width:100%;border-collapse:collapse}td,th{text-align:left;padding:6px;border-bottom:1px solid #eef}
@font-face{font-family:codicon;src:url("_aether/codicon.ttf") format("truetype")}.ci{font-family:codicon;font-style:normal;vertical-align:-2px;margin-right:4px;font-size:15px}</style></head><body><header><b id="mod">Aether</b><nav id="nav"></nav><span style="flex:1"></span><span id="flows"></span><select id="role" title="当前角色（能力权限）" onchange="localStorage.aeRole=this.value;load()" style="border-radius:6px"><option>user</option><option>admin</option><option>guest</option></select></header><main id="app"></main><script>
const B=location.pathname.replace(/\/$/,'');let meta,cur=0;
const role=()=>localStorage.aeRole||'user';const api=(p,o={})=>fetch(B+p,{headers:{'Content-Type':'application/json','X-Aether-Role':role()},...o}).then(r=>r.json());
async function flow(n){const r=await api('/api/flow/'+n,{method:'POST',body:'{}'});alert('流程 '+n+(r.ok?' 完成':' 失败')+'\n'+r.steps.map(s=>(s.ok?'[成功] ':'[失败] ')+s.step+(s.error?' — '+s.error:'')).join('\n'));load()}
const esc=s=>String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
async function load(){meta=await api('/api/_meta');document.getElementById('mod').textContent=meta.module;document.getElementById('role').value=role();
 document.getElementById('flows').innerHTML=(meta.flows||[]).map(f=>`<button class="ghost" onclick="flow('${f}')"><i class="ci">&#xeb2c;</i>${esc(f)}</button>`).join(' ');render()}
function ent(n){return meta.entities.find(e=>e.name==n)}
async function render(){const v=meta.views[cur]||{items:[]};
 document.getElementById('nav').innerHTML=meta.views.map((x,i)=>`<a class="${i==cur?'on':''}" onclick="cur=${i};render()">${esc(x.name)}</a>`).join('');
 let h='';for(const it of v.items){h+=await el(it)}document.getElementById('app').innerHTML=h}
async function el(it){switch(it.kind){
 case 'title':return `<h1>${esc(it.text)}</h1>`;case 'text':return `<p class="card">${esc(it.text)}</p>`;case 'section':return `<h3>${esc(it.text)}</h3>`;
 case 'error':return `<div class="card err"><i class="ci">&#xea6c;</i>${esc(it.text)}</div>`;
 case 'stat':return `<div class="card stat"><div class="muted">${esc(it.label)}</div><div class="v">${esc(it.value)}</div></div>`;
 case 'button':{const a=meta.actions.find(x=>x.name==it.action)||{};return `<button onclick="act('${it.action}',{})" title="${a.allow&&a.allow.length?'需要角色: '+a.allow.join('/'):''}">${a.allow&&a.allow.length?'<i class="ci">&#xea75;</i>':''}${esc(it.label)}</button> `}
 case 'form':{const e=ent(it.entity);return `<div class="card"><form onsubmit="return add(event,'${e.name}')">`+e.fields.map(f=>f.type=='Bool'?`<label><input type="checkbox" name="${f.name}"> ${f.name}</label>`:`<input name="${f.name}" placeholder="${f.name} (${f.type})" ${f.type=='Int'||f.type=='Float'?'type=number step=any':''}>`).join('')+`<button>＋ 新增 ${e.name}</button></form></div>`}
 case 'list':case 'table':{const e=ent(it.entity);const rows=await api('/api/'+e.name);
  if(it.kind=='table')return `<div class="card"><table><tr><th>id</th>${e.fields.map(f=>`<th>${f.name}</th>`).join('')}<th></th></tr>${rows.map(r=>`<tr><td>${r.id}</td>${e.fields.map(f=>`<td>${esc(r[f.name])}</td>`).join('')}<td>${btns(e,r)}</td></tr>`).join('')}</table></div>`;
  const lf=it.label_field||(e.fields.find(f=>f.type=='Text')||e.fields[0]||{}).name;const bf=e.fields.find(f=>f.type=='Bool');
  return `<div class="card">${rows.length?'':'<span class="muted">暂无数据</span>'}${rows.map(r=>`<div class="row"><span class="grow ${bf&&r[bf.name]?'done':''}">${esc(r[lf])} <span class="muted">${e.fields.filter(f=>f.name!=lf).map(f=>f.name+': '+esc(r[f.name])).join(' · ')}</span></span>${btns(e,r)}</div>`).join('')}</div>`}
 case 'chart':{const e=ent(it.entity);const rows=await api('/api/'+e.name);const f=it.field;const mx=Math.max(1,...rows.map(r=>+r[f]||0));const lf=(e.fields.find(x=>x.type=='Text')||{}).name;
  return `<div class="card">${rows.map(r=>`<div class="row"><span style="width:140px">${esc(r[lf])}</span><div class="bar" style="width:${(+r[f]||0)/mx*60}%"></div>${esc(r[f])}</div>`).join('')}</div>`}
 default:return ''}}
function btns(e,r){return e.row_actions.map(a=>`<button class="ghost" onclick="act('${a}',{${'x'}:${r.id}},'${e.name}')">${a}</button>`).join(' ')+` <button class="del" onclick="del('${e.name}',${r.id})">删除</button>`}
async function add(ev,n){ev.preventDefault();const fd=new FormData(ev.target),o={};for(const f of ent(n).fields){o[f.name]=f.type=='Bool'?fd.has(f.name):(f.type=='Int'||f.type=='Float')?+fd.get(f.name):fd.get(f.name)}
 await api('/api/'+n,{method:'POST',body:JSON.stringify(o)});load();return false}
async function del(n,id){await api('/api/'+n+'/'+id,{method:'DELETE'});load()}
async function act(a,args,en){const spec=meta.actions.find(x=>x.name==a);const body={};if(en&&spec.params[0])body[spec.params[0].name]=args.x;
 const r=await api('/api/action/'+a,{method:'POST',body:JSON.stringify(body)});if(r.error)alert(r.error);load()}
load()</script></body></html>"""


def icon_font():
    """Codicons icon font (CC-BY-4.0, see assets/CODICONS-LICENSE.txt); the UI uses font icons, never emoji."""
    import os
    with open(os.path.join(os.path.dirname(__file__), "assets", "codicon.ttf"), "rb") as f:
        return f.read()
