class AeError(Exception):
    def __init__(self, code, msg, line=0, col=0):
        super().__init__(msg); self.code, self.msg, self.line, self.col = code, msg, line, col
    def to_json(self):
        return {"severity":"error","code":self.code,"message":self.msg,"line":self.line,"col":self.col}
