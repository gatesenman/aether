"""Dependency-free wheel builder (no setuptools needed): python3 mkwheel.py -> dist/aether_lang-0.6.0-py3-none-any.whl"""
import zipfile, hashlib, base64, os, glob
V = "0.6.0"; N = "aether_lang"; DI = f"{N}-{V}.dist-info"
os.makedirs("dist", exist_ok=True); out = f"dist/{N}-{V}-py3-none-any.whl"
files = {}
for p in glob.glob("aether/**/*", recursive=True):
    if os.path.isfile(p) and "__pycache__" not in p: files[p] = open(p, "rb").read()
files[f"{DI}/METADATA"] = f"Metadata-Version: 2.1\nName: aether-lang\nVersion: {V}\nSummary: Aether agent+human programming language\nRequires-Python: >=3.9\n\n{open('README.md', encoding='utf-8').read()}".encode()
files[f"{DI}/WHEEL"] = b"Wheel-Version: 1.0\nGenerator: mkwheel\nRoot-Is-Purelib: true\nTag: py3-none-any\n"
files[f"{DI}/entry_points.txt"] = b"[console_scripts]\naether = aether.cli:main\n"
rec = []
for k, v in files.items():
    h = base64.urlsafe_b64encode(hashlib.sha256(v).digest()).rstrip(b"=").decode(); rec.append(f"{k},sha256={h},{len(v)}")
rec.append(f"{DI}/RECORD,,")
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
    for k, v in files.items(): z.writestr(k, v)
    z.writestr(f"{DI}/RECORD", "\n".join(rec) + "\n")
print(out)
