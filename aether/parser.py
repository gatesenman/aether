"""Recursive-descent parser. AST nodes are plain JSON-serialisable dicts: {"k": kind, "line": n, ...}."""
import re
from .lexer import lex
from .errors import AeError

PREC = [("or",), ("and",), ("==","!=","<",">","<=",">="), ("|>",), ("+","-"), ("*","/","%")]

class Parser:
    def __init__(s, src):
        s.src = src; s.t = lex(src); s.i = 0
    # helpers
    def peek(s, o=0): return s.t[min(s.i+o, len(s.t)-1)]
    def at(s, *vals): return s.peek().val in vals and s.peek().kind in ("op","kw")
    def next(s): t = s.t[s.i]; s.i += 1; return t
    def accept(s, *vals):
        if s.at(*vals): return s.next()
    def expect(s, val):
        if not s.at(val):
            t = s.peek(); raise AeError("E100", f"expected '{val}' but found {t.val!r}", t.line, t.col)
        return s.next()
    def ident(s):
        t = s.peek()
        if t.kind != "id": raise AeError("E101", f"expected identifier but found {t.val!r}", t.line, t.col)
        return s.next().val
    def node(s, k, tok, **kw): d = {"k": k, "line": tok.line}; d.update(kw); return d

    # program
    def program(s):
        decls = []
        while s.peek().kind != "eof":
            start = s.peek().pos; d = s.decl()
            d["span"] = [start, s.t[s.i-1].pos + len(s._tokstr(s.t[s.i-1]))]
            decls.append(d)
            while s.accept(";"): pass
        return {"k": "Program", "decls": decls}
    def _tokstr(s, t):
        if t.kind == "str":  # find closing quote in source
            j = t.pos + 1
            while s.src[j] != '"': j += 2 if s.src[j] == "\\" else 1
            return s.src[t.pos:j+1]
        return str(t.val) if t.kind != "num" else re.match(r"\d+(\.\d+)?", s.src[t.pos:]).group()

    def decl(s):
        t = s.peek()
        if s.accept("module"): return s.node("Module", t, name=s.ident())
        if s.accept("import"):
            p = s.next(); return s.node("Import", t, path=p.val)
        if s.accept("entity"):
            name = s.ident(); s.expect("{"); fields = []
            invs = []
            while not s.accept("}"):
                if s.at("invariant"):
                    it = s.next(); invs.append(s.expr()); s.accept(","); continue
                ft = s.peek(); fname = s.ident(); s.expect(":"); ty = s.type_()
                default = s.expr() if s.accept("=") else None
                fields.append(s.node("Field", ft, name=fname, type=ty, default=default)); s.accept(",")
            return s.node("Entity", t, name=name, fields=fields, invariants=invs)
        if s.accept("type"):
            name = s.ident(); tparams = s.tparams(); s.expect("="); variants = []
            while True:
                vt = s.peek(); vn = s.ident(); params = s.params() if s.at("(") else []
                variants.append(s.node("Variant", vt, name=vn, params=params))
                if not s.accept("|"): break
            return s.node("TypeDecl", t, name=name, tparams=tparams, variants=variants)
        if s.at("fn") and s.peek(1).kind == "id":
            s.next(); name = s.ident(); tp = s.tparams(); params = s.params()
            ret = s.type_() if s.accept("->") else None
            effects = []
            if s.accept("!"):
                effects.append(s.ident())
                while s.accept(","): effects.append(s.ident())
            req, ens = s.contracts()
            return s.node("Fn", t, name=name, tparams=tp, params=params, ret=ret, effects=effects, requires=req, ensures=ens, body=s.block())
        if s.accept("action"):
            name = s.ident(); params = s.params() if s.at("(") else []
            allow = []
            if s.accept("allow"):
                allow.append(s.ident())
                while s.accept(","): allow.append(s.ident())
            req, ens = s.contracts()
            return s.node("Action", t, name=name, params=params, allow=allow, requires=req, ensures=ens, body=s.block())
        if s.accept("test"):
            n = s.next(); return s.node("Test", t, name=str(n.val), body=s.block())
        if s.accept("example"):
            return s.node("Example", t, expr=s.expr())
        if s.accept("view"):
            name = s.ident(); s.expect("{"); items = []
            while not s.accept("}"):
                it = s.peek(); kind = s.ident(); args = []
                while not (s.peek().kind == "id" and s.peek().line != it.line) and not s.at("}") :
                    if s.accept("->"): args.append({"k":"Arrow","target": s.ident()}); continue
                    args.append(s.expr())
                    if s.peek().line != it.line: break
                items.append(s.node("ViewItem", it, kind=kind, args=args))
            return s.node("View", t, name=name, items=items)
        if s.accept("flow"):
            name = s.ident(); s.expect("{"); steps = []
            while not s.accept("}"):
                st = s.expect("step"); sn = s.next().val; steps.append(s.node("Step", st, name=sn, body=s.block()))
            return s.node("Flow", t, name=name, steps=steps)
        return s.stmt()

    def contracts(s):
        req, ens = [], []
        while s.at("requires", "ensures"):
            lst = req if s.next().val == "requires" else ens
            lst.append(s.expr())
            while s.accept(","): lst.append(s.expr())
        return req, ens
    def tparams(s):
        tp = []
        if s.accept("<"):
            tp.append(s.ident())
            while s.accept(","): tp.append(s.ident())
            s.expect(">")
        return tp
    def type_(s):
        t = s.peek()
        if s.accept("fn"):
            args = []
            if s.accept("("):
                while not s.accept(")"): args.append(s.type_()); s.accept(",")
            ret = s.type_() if s.accept("->") else None
            return {"k": "Type", "name": "Fn", "args": args + ([ret] if ret else []), "optional": False}
        name = s.ident(); args = []
        if s.accept("<"):
            args.append(s.type_())
            while s.accept(","): args.append(s.type_())
            s.expect(">")
        opt = bool(s.accept("?"))
        return {"k": "Type", "name": name, "args": args, "optional": opt}
    def params(s):
        s.expect("("); ps = []
        while not s.accept(")"):
            pt = s.peek(); n = s.ident(); ty = s.type_() if s.accept(":") else None
            ps.append({"k":"Param","name":n,"type":ty,"line":pt.line}); s.accept(",")
        return ps
    def block(s):
        t = s.expect("{"); body = []
        while not s.accept("}"):
            body.append(s.stmt()); s.accept(";")
        return s.node("Block", t, body=body)

    def stmt(s):
        t = s.peek()
        if s.at("let","var"):
            mut = s.next().val == "var"; name = s.ident()
            ty = s.type_() if s.accept(":") else None; s.expect("=")
            return s.node("Let", t, name=name, mutable=mut, type=ty, value=s.expr())
        if s.accept("return"):
            v = None if s.at("}") or s.peek().line != t.line else s.expr()
            return s.node("Return", t, value=v)
        if s.accept("while"): return s.node("While", t, cond=s.expr(), body=s.block())
        if s.accept("for"):
            v = s.ident(); s.expect("in"); it = s.expr(); return s.node("For", t, var=v, iter=it, body=s.block())
        if s.accept("try"):
            body = s.block(); s.expect("catch"); v = s.ident(); return s.node("Try", t, body=body, var=v, handler=s.block())
        if s.accept("throw"): return s.node("Throw", t, value=s.expr())
        e = s.expr()
        if s.at("=","+=","-="):
            op = s.next().val; v = s.expr()
            if op != "=": v = s.node("Bin", t, op=op[0], l=e, r=v)
            if e["k"] not in ("Name","Field","Index"): raise AeError("E102","invalid assignment target",t.line,t.col)
            return s.node("Assign", t, target=e, value=v)
        return s.node("ExprStmt", t, expr=e)

    def expr(s, lvl=0):
        if lvl == len(PREC): return s.unary()
        l = s.expr(lvl+1)
        while s.at(*PREC[lvl]):
            t = s.next(); r = s.expr(lvl+1)
            l = s.node("Pipe", t, l=l, r=r) if t.val == "|>" else s.node("Bin", t, op=t.val, l=l, r=r)
        return l
    def unary(s):
        t = s.peek()
        if s.accept("-"): return s.node("Unary", t, op="-", e=s.unary())
        if s.accept("not", "!"): return s.node("Unary", t, op="not", e=s.unary())
        if s.accept("await"): return s.node("Await", t, e=s.unary())
        if s.accept("spawn"): return s.node("Spawn", t, e=s.unary())
        return s.postfix(s.primary())
    def postfix(s, e):
        while True:
            t = s.peek()
            if s.at("(") and t.line == s.t[s.i-1].line:
                s.next(); args = []
                while not s.accept(")"): args.append(s.expr()); s.accept(",")
                e = s.node("Call", t, fn=e, args=args)
            elif s.accept("."): e = s.node("Field", t, obj=e, name=s.ident())
            elif s.at("[") and t.line == s.t[s.i-1].line:
                s.next(); idx = s.expr(); s.expect("]"); e = s.node("Index", t, obj=e, idx=idx)
            else: return e
    def primary(s):
        t = s.next()
        if t.kind == "num": return s.node("Num", t, v=t.val)
        if t.kind == "str": return s.strlit(t)
        if t.kind == "id": return s.node("Name", t, name=t.val)
        v = t.val
        if v in ("true","false"): return s.node("Bool", t, v=v == "true")
        if v == "nil": return s.node("Nil", t)
        if v == "(":
            e = s.expr(); s.expect(")"); return e
        if v == "[":
            items = []
            while not s.accept("]"): items.append(s.expr()); s.accept(",")
            return s.node("List", t, items=items)
        if v == "{":
            fields = []
            while not s.accept("}"):
                kt = s.next(); s.expect(":"); fields.append([kt.val, s.expr()]); s.accept(",")
            return s.node("Record", t, fields=fields)
        if v == "fn":
            params = s.params()
            if s.accept("=>"): body = s.node("Block", t, body=[s.node("ExprStmt", t, expr=s.expr())])
            else: body = s.block()
            return s.node("Lambda", t, params=params, body=body)
        if v == "if":
            c = s.expr(); th = s.block(); el = None
            if s.accept("else"):
                el = s.block() if s.at("{") else s.node("Block", t, body=[s.node("ExprStmt", t, expr=s.primary_if())])
            return s.node("If", t, cond=c, then=th, els=el)
        if v == "match":
            subj = s.expr(); s.expect("{"); arms = []
            while not s.accept("}"):
                pat = s.pattern(); guard = s.expr() if s.accept("if") else None
                s.expect("=>"); body = s.block() if s.at("{") else s.expr()
                arms.append({"pat": pat, "guard": guard, "body": body}); s.accept(",")
            return s.node("Match", t, subj=subj, arms=arms)
        raise AeError("E103", f"unexpected token {t.val!r}", t.line, t.col)
    def primary_if(s):
        s.i -= 0; return s.primary()
    def pattern(s):
        t = s.peek()
        if t.kind == "num" or t.kind == "str" or s.at("true","false","nil"):
            e = s.primary(); return {"k":"PLit","v": e.get("v") if e["k"] != "Str" else e["parts"][0]}
        if s.accept("-"): n = s.next(); return {"k":"PLit","v":-n.val}
        if s.accept("["):
            items = []
            while not s.accept("]"): items.append(s.pattern()); s.accept(",")
            return {"k":"PList","items":items}
        name = s.ident()
        if name == "_": return {"k":"PWild"}
        if s.accept("("):
            args = []
            while not s.accept(")"): args.append(s.pattern()); s.accept(",")
            return {"k":"PCtor","name":name,"args":args}
        if name[0].isupper(): return {"k":"PCtor","name":name,"args":[]}
        return {"k":"PBind","name":name}
    def strlit(s, t):
        raw = t.val
        if "{" not in raw: return s.node("Str", t, parts=[raw.replace("\x00LB","{").replace("\x00RB","}")])
        parts = []
        for i, seg in enumerate(re.split(r"\{([^{}]*)\}", raw)):
            if i % 2 == 0: parts.append(seg.replace("\x00LB","{").replace("\x00RB","}"))
            else:
                sub = Parser(seg); e = sub.expr(); parts.append(e)
        return s.node("Str", t, parts=parts)

def parse(src): return Parser(src).program()
