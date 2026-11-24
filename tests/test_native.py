"""v0.3: 原生二进制一致性测试（以 Python 参考实现生成的 golden.json 为规范）。找不到二进制则跳过。"""
import os, re, shutil, subprocess, unittest
from pathlib import Path
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
def find_bin():
    for p in [os.environ.get("AETHER_NATIVE", ""), os.path.join(ROOT, "native", "build", "aether-linux-amd64"), os.path.join(ROOT, "native", "aether"), "/tmp/ae", shutil.which("aether-native") or ""]:
        if p and os.path.isfile(p) and os.access(p, os.X_OK):
            return p
BIN = find_bin()
# These are historical feature regressions, not assertions that the binary must
# remain at the old release that introduced them.
NATIVE_VERSION = re.search(r'^const Version = "([^"]+)"',
    Path(ROOT, "native", "cli.go").read_text(encoding="utf-8"), re.MULTILINE).group(1)

@unittest.skipUnless(BIN, "native binary not built")
class NativeConformance(unittest.TestCase):
    def test_golden(self):
        r = subprocess.run([BIN, "conform", os.path.join(ROOT, "conformance", "golden.json")], capture_output=True, text=True, timeout=120)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("0 failed", r.stdout)
    def test_examples(self):
        ex = os.path.join(ROOT, "examples")
        for f in sorted(os.listdir(ex)):
            if f.endswith(".ae"):
                for cmd in (["check"], ["test"]):
                    r = subprocess.run([BIN, *cmd, os.path.join(ex, f)], capture_output=True, text=True, timeout=60)
                    self.assertEqual(r.returncode, 0, f"{cmd} {f}: {r.stdout}{r.stderr}")
    def test_static(self):
        r = subprocess.run([BIN, "version"], capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout.strip(), f"aether {NATIVE_VERSION} (native)")

if __name__ == "__main__":
    unittest.main()
