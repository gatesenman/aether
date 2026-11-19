import re
from .errors import AeError

KEYWORDS = {"module","import","entity","view","action","fn","let","var","return","if","else","while","for","in",
            "match","true","false","nil","and","or","not","try","catch","throw","type","spawn","await","flow","step","requires","ensures","invariant","test","example","allow"}
TOKEN_RE = re.compile(r'''
 (?P<ws>[ \t\r\n]+)|(?P<comment>//[^\n]*)|
 (?P<num>\d+\.\d+|\d+)|(?P<str>"(?:\\.|[^"\\])*")|
 (?P<id>[A-Za-z_\u4e00-\u9fff][A-Za-z0-9_\u4e00-\u9fff]*)|
 (?P<op>\|>|=>|->|==|!=|<=|>=|\+=|-=|[-+*/%<>=!(){}\[\],.:;?|])''', re.X)

_ESC={"n":"\n","t":"\t",'"':'"',"\\":"\\","{":"\x00LB","}":"\x00RB"}
def _unesc(s): return re.sub(r'\\(.)', lambda m: _ESC.get(m.group(1), m.group(1)), s)

class Tok:
    __slots__=("kind","val","line","col","pos")
    def __init__(s,k,v,l,c,p): s.kind,s.val,s.line,s.col,s.pos=k,v,l,c,p
    def __repr__(s): return f"{s.kind}:{s.val!r}@{s.line}"

def lex(src):
    toks=[]; i=0; line=1; ls=0
    while i < len(src):
        m = TOKEN_RE.match(src, i)
        if not m: raise AeError("E001", f"unexpected character {src[i]!r}", line, i-ls+1)
        k = m.lastgroup; v = m.group()
        if k == "num": toks.append(Tok("num", float(v) if "." in v else int(v), line, i-ls+1, i))
        elif k == "str": toks.append(Tok("str", _unesc(v[1:-1]), line, i-ls+1, i))
        elif k == "id": toks.append(Tok("kw" if v in KEYWORDS else "id", v, line, i-ls+1, i))
        elif k == "op": toks.append(Tok("op", v, line, i-ls+1, i))
        nl = v.count("\n")
        if nl: line += nl; ls = i + v.rfind("\n") + 1
        i = m.end()
    toks.append(Tok("eof", None, line, i-ls+1, i))
    return toks
