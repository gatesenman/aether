"""Minimal LSP stdio client used by protocol-level tests."""
import json, subprocess, threading, queue
class LSPClient:
    def __init__(self, cmd):
        self.p = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.q = queue.Queue(); self.notes = []; self.id = 0; self.pending = {}
        threading.Thread(target=self._read, daemon=True).start()
    def _read(self):
        f = self.p.stdout
        while True:
            hdr = {}
            while True:
                line = f.readline()
                if not line: return
                line = line.decode().strip()
                if not line: break
                k, v = line.split(":", 1); hdr[k.lower()] = v.strip()
            body = json.loads(f.read(int(hdr["content-length"])))
            if "id" in body and "method" not in body: self.q.put(body)
            else: self.notes.append(body)
    def _send(self, obj):
        b = json.dumps(obj).encode()
        self.p.stdin.write(b"Content-Length: %d\r\n\r\n" % len(b) + b); self.p.stdin.flush()
    def request(self, method, params=None, timeout=20):
        self.id += 1; self._send({"jsonrpc": "2.0", "id": self.id, "method": method, "params": params or {}})
        while True:
            r = self.q.get(timeout=timeout)
            if r["id"] == self.id: return r
    def notify(self, method, params=None): self._send({"jsonrpc": "2.0", "method": method, "params": params or {}})
    def close(self):
        try:
            self.request("shutdown"); self.notify("exit"); self.p.wait(5)
        except Exception: self.p.kill()
