"""Formatter: normalises indentation (2 spaces per brace level), trims whitespace, collapses blank lines.
Refuses to format code that does not parse, so formatting never changes meaning."""
from .parser import parse
from .lexer import lex
def fmt(src):
    parse(src)
    out = []; depth = 0; blank = 0
    toks = lex(src); bylines = {}
    for t in toks:
        if t.kind == "op" and t.val in "{}()[]": bylines.setdefault(t.line, []).append(t.val)
    for i, raw in enumerate(src.split("\n"), 1):
        line = raw.strip()
        if not line:
            blank += 1
            if blank <= 1 and out: out.append("")
            continue
        blank = 0
        br = bylines.get(i, [])
        lead = 0
        for c in br:
            if c in "}])" and line.startswith(c, lead): lead += 1
            else: break
        ind = max(depth - lead, 0)
        out.append("  " * ind + line)
        for c in br: depth += 1 if c in "{[(" else -1
    return "\n".join(out).rstrip() + "\n"
