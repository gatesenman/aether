package main

// aether mcp（0.6 第 3 轮）：Model Context Protocol 服务器（stdio，换行分隔 JSON-RPC 2.0）。
// 借鉴 Unison UCM 内置 `ucm mcp`（typecheck/docs/依赖工具）与 Darklang 面向智能体的 CLI。
// 只提供只读工具：检查、测试、骨架、上下文切片、错误码解释、迁移计划、OpenAPI。写操作仍走 `aether ops`（带历史与撤销）。

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type mcpTool struct {
	Name, Desc string
	Props      map[string]any
	Req        []string
	Run        func(a map[string]any) (any, error)
}

func mcpSrc(a map[string]any) (string, string, error) {
	if s, ok := a["source"].(string); ok && s != "" {
		return s, ".", nil
	}
	f, _ := a["file"].(string)
	if f == "" {
		return "", "", fmt.Errorf("需要 file 或 source")
	}
	return readSrc(f)
}

var strP = map[string]any{"type": "string"}
var srcProps = map[string]any{"file": map[string]any{"type": "string", "description": ".ae 文件路径"}, "source": map[string]any{"type": "string", "description": "直接给出 Aether 源码"}}

func mcpTools() []mcpTool {
	return []mcpTool{
		{"aether_check", "静态检查（类型、副作用、契约、视图），返回带错误码与修复提示的诊断", srcProps, nil, func(a map[string]any) (any, error) {
			s, d, err := mcpSrc(a)
			if err != nil {
				return nil, err
			}
			_, ds := CheckSourceIn(s, d)
			for _, x := range ds {
				enrichDiag(x)
			}
			return map[string]any{"diagnostics": diagList(ds)}, nil
		}},
		{"aether_test", "运行 inline test / example", srcProps, nil, func(a map[string]any) (any, error) {
			s, d, err := mcpSrc(a)
			if err != nil {
				return nil, err
			}
			it := NewInterp(":memory:", false)
			if err := it.Load(s, d); err != nil {
				return nil, err
			}
			return testsAny(it.RunTests()), nil
		}},
		{"aether_skeleton", "签名级摘要（省 token 的全局视图）", srcProps, nil, func(a map[string]any) (any, error) {
			s, _, err := mcpSrc(a)
			if err != nil {
				return nil, err
			}
			return Skeleton(s)
		}},
		{"aether_context", "某个符号的上下文切片：声明 + 依赖 + 契约 + 使用者", map[string]any{"file": strP, "source": strP, "symbol": strP}, []string{"symbol"}, func(a map[string]any) (any, error) {
			s, d, err := mcpSrc(a)
			if err != nil {
				return nil, err
			}
			sym, _ := a["symbol"].(string)
			return AgentContext(s, sym, d)
		}},
		{"aether_explain_code", "错误码中文解释（E200、W300、R040、M401…）", map[string]any{"code": strP}, []string{"code"}, func(a map[string]any) (any, error) {
			c, _ := a["code"].(string)
			if t, ok := ExplainCode(c); ok {
				return t, nil
			}
			return nil, fmt.Errorf("未知错误码 %s", c)
		}},
		{"aether_migrate_plan", "schema 与已有数据对账（只读计划）", map[string]any{"file": strP, "db": strP}, []string{"file"}, func(a map[string]any) (any, error) {
			s, d, err := mcpSrc(a)
			if err != nil {
				return nil, err
			}
			db, _ := a["db"].(string)
			if db == "" {
				db = defaultDB(a["file"].(string))
			}
			if _, err := os.Stat(db); err != nil {
				return map[string]any{"steps": []any{}, "note": "数据库不存在：" + filepath.Base(db)}, nil
			}
			it := NewInterp(db, false)
			defer it.Store.Close()
			if err := it.Load(s, d); err != nil {
				return nil, err
			}
			st := PlanMigration(it)
			return map[string]any{"steps": st, "ok": !anyBlock(st)}, nil
		}},
		{"aether_openapi", "生成的 HTTP API 的 OpenAPI 3.1 描述", srcProps, nil, func(a map[string]any) (any, error) {
			s, d, err := mcpSrc(a)
			if err != nil {
				return nil, err
			}
			it := NewInterp(":memory:", false)
			if err := it.Load(s, d); err != nil {
				return nil, err
			}
			return BuildOpenAPI(it), nil
		}},
	}
}

func mcpHandle(m map[string]any) map[string]any {
	method, _ := m["method"].(string)
	id, hasID := m["id"]
	if !hasID {
		return nil // notification
	}
	resp := map[string]any{"jsonrpc": "2.0", "id": id}
	params, _ := m["params"].(map[string]any)
	switch method {
	case "initialize":
		pv := "2025-06-18"
		if p, ok := params["protocolVersion"].(string); ok && p != "" {
			pv = p
		}
		resp["result"] = map[string]any{"protocolVersion": pv, "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{"name": "aether", "version": Version},
			"instructions": "Aether 语言工具。先 aether_skeleton 看全局，再 aether_context 取切片，改完用 aether_check / aether_test 验证。"}
	case "ping":
		resp["result"] = map[string]any{}
	case "tools/list":
		ts := []any{}
		for _, t := range mcpTools() {
			sc := map[string]any{"type": "object", "properties": t.Props}
			if len(t.Req) > 0 {
				sc["required"] = t.Req
			}
			ts = append(ts, map[string]any{"name": t.Name, "description": t.Desc, "inputSchema": sc})
		}
		resp["result"] = map[string]any{"tools": ts}
	case "tools/call":
		name, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]any)
		if args == nil {
			args = map[string]any{}
		}
		for _, t := range mcpTools() {
			if t.Name != name {
				continue
			}
			var out any
			var err error
			func() {
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("%v", r)
					}
				}()
				out, err = t.Run(args)
			}()
			if err != nil {
				resp["result"] = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": err.Error()}}}
			} else {
				txt, ok := out.(string)
				if !ok {
					b, _ := json.MarshalIndent(out, "", " ")
					txt = string(b)
				}
				resp["result"] = map[string]any{"content": []any{map[string]any{"type": "text", "text": txt}}}
			}
			return resp
		}
		resp["error"] = map[string]any{"code": -32602, "message": "unknown tool " + name}
	default:
		resp["error"] = map[string]any{"code": -32601, "message": "method not found: " + method}
	}
	return resp
}

func ServeMCP(in io.Reader, out io.Writer) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if r := mcpHandle(m); r != nil {
			enc.Encode(r)
		}
	}
}
