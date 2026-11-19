"""Entity persistence on SQLite. Each `entity` becomes a table; records are dicts."""
import sqlite3, threading, json
SQLT = {"Int": "INTEGER", "Float": "REAL", "Bool": "INTEGER", "Text": "TEXT", "String": "TEXT", "Date": "TEXT"}
class Store:
    def __init__(s, path=":memory:"):
        s.db = sqlite3.connect(path, check_same_thread=False); s.db.row_factory = sqlite3.Row
        s.lock = threading.RLock(); s.schemas = {}
    def ensure(s, ent):
        name = ent["name"]; s.schemas[name] = {f["name"]: f["type"]["name"] for f in ent["fields"]}
        with s.lock:
            s.db.execute(f'CREATE TABLE IF NOT EXISTS "{name}" (id INTEGER PRIMARY KEY AUTOINCREMENT)')
            have = {r[1] for r in s.db.execute(f'PRAGMA table_info("{name}")')}
            for f, t in s.schemas[name].items():
                if f not in have: s.db.execute(f'ALTER TABLE "{name}" ADD COLUMN "{f}" {SQLT.get(t, "TEXT")}')  # auto-migration
            s.db.commit()
    def _out(s, name, row):
        d = dict(row)
        for f, t in s.schemas[name].items():
            if t == "Bool" and f in d: d[f] = bool(d[f])
            elif t not in SQLT and isinstance(d.get(f), str):
                try: d[f] = json.loads(d[f])
                except Exception: pass
        return d
    def _in(s, name, rec):
        out = {}
        for f, t in s.schemas[name].items():
            if f in rec:
                v = rec[f]
                if t == "Int" and isinstance(v, str): v = int(v or 0)
                if t == "Float" and isinstance(v, str): v = float(v or 0)
                if t == "Bool": v = 1 if v in (True, 1, "true", "on", "1") else 0
                if isinstance(v, (list, dict)): v = json.dumps(v, ensure_ascii=False)
                out[f] = v
        return out
    def all(s, name):
        with s.lock: return [s._out(name, r) for r in s.db.execute(f'SELECT * FROM "{name}" ORDER BY id')]
    def get(s, name, id):
        with s.lock:
            r = s.db.execute(f'SELECT * FROM "{name}" WHERE id=?', (id,)).fetchone()
        return s._out(name, r) if r else None
    def insert(s, name, rec):
        d = s._in(name, rec)
        with s.lock:
            if d:
                cur = s.db.execute(f'INSERT INTO "{name}" ({",".join(chr(34)+k+chr(34) for k in d)}) VALUES ({",".join("?"*len(d))})', list(d.values()))
            else: cur = s.db.execute(f'INSERT INTO "{name}" DEFAULT VALUES')
            s.db.commit(); return s.get(name, cur.lastrowid)
    def update(s, name, id, rec):
        d = s._in(name, rec)
        with s.lock:
            if d: s.db.execute(f'UPDATE "{name}" SET {",".join(chr(34)+k+chr(34)+"=?" for k in d)} WHERE id=?', [*d.values(), id]); s.db.commit()
        return s.get(name, id)
    def delete(s, name, id):
        with s.lock: s.db.execute(f'DELETE FROM "{name}" WHERE id=?', (id,)); s.db.commit()
