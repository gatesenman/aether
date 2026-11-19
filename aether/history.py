"""Content-addressed, attributed, reversible change history.
Every program version is identified by the hash of its canonical AST, so pure reformatting or comment edits
keep the same semantic identity. Every change records who (human/agent), when, why and which ops."""
import hashlib, json, os, time
from .parser import parse
from .errors import AeError

def _strip(n):
    if isinstance(n, dict): return {k: _strip(v) for k, v in n.items() if k not in ("line", "span")}
    if isinstance(n, list): return [_strip(x) for x in n]
    return n
def ast_hash(src):
    try: canon = json.dumps(_strip(parse(src)), sort_keys=True, ensure_ascii=False)
    except AeError: canon = "text:" + src
    return hashlib.sha256(canon.encode()).hexdigest()[:12]

class History:
    def __init__(s, path=None):
        s.path = path; s.entries = []; s.objects = {}; s.pos = -1
        if path and os.path.exists(path):
            d = json.load(open(path, encoding="utf-8")); s.entries, s.objects, s.pos = d["entries"], d["objects"], d["pos"]
    def _save(s):
        if s.path: json.dump({"entries": s.entries, "objects": s.objects, "pos": s.pos}, open(s.path, "w", encoding="utf-8"), ensure_ascii=False, indent=1)
    @property
    def head(s): return s.entries[s.pos] if s.pos >= 0 else None
    def commit(s, src, author="human", reason="", ops=None, kind="edit"):
        h = ast_hash(src); text_same = s.head and s.objects.get(s.head["hash"]) == src
        if s.head and s.head["hash"] == h and text_same: return s.head
        e = {"id": len(s.entries) + 1, "hash": h, "parent": s.head["hash"] if s.head else None, "author": author,
             "kind": kind, "reason": reason, "ops": ops or [], "time": time.strftime("%Y-%m-%d %H:%M:%S"),
             "semantic_change": not (s.head and s.head["hash"] == h)}
        s.objects[h] = src
        s.entries = s.entries[:s.pos + 1] + [e]; s.pos = len(s.entries) - 1; s._save(); return e
    def undo(s):
        if s.pos <= 0: raise AeError("H001", "nothing to undo")
        s.pos -= 1; s._save(); return s.objects[s.head["hash"]]
    def redo(s):
        if s.pos >= len(s.entries) - 1: raise AeError("H002", "nothing to redo")
        s.pos += 1; s._save(); return s.objects[s.head["hash"]]
    def checkout(s, h): return s.objects[h]
    def log(s): return [dict(e, current=(i == s.pos), undone=(i > s.pos)) for i, e in enumerate(s.entries)]
