package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestOpenAPIAgentsMCP(t *testing.T) {
	b, _ := os.ReadFile("templates/todo/main.ae")
	src := string(b)
	it := NewInterp(":memory:", false)
	if err := it.Load(src, "."); err != nil {
		t.Fatal(err)
	}
	oa := BuildOpenAPI(it)
	if _, err := json.Marshal(oa); err != nil {
		t.Fatal(err)
	}
	sc := oa["components"].(map[string]any)["schemas"].(map[string]any)["Todo"].(map[string]any)
	if r := sc["required"].([]any); len(r) != 1 || r[0] != "title" {
		t.Fatalf("required %v", r)
	}
	md, err := AgentsMD(src, "main.ae")
	if err != nil || !strings.Contains(md, "`Todo`") || !strings.Contains(md, "clear_done") {
		t.Fatalf("agents.md: %v\n%s", err, md)
	}
	r := mcpHandle(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if n := len(r["result"].(map[string]any)["tools"].([]any)); n < 7 {
		t.Fatalf("tools %d", n)
	}
	r = mcpHandle(map[string]any{"id": 2, "method": "tools/call", "params": map[string]any{"name": "aether_explain_code", "arguments": map[string]any{"code": "M401"}}})
	txt := r["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(txt, "默认值") {
		t.Fatal(txt)
	}
	if mcpHandle(map[string]any{"method": "notifications/initialized"}) != nil {
		t.Fatal("notification must not get a reply")
	}
}
