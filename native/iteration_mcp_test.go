package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestIterationMCPValidation(t *testing.T) {
	cases := []struct {
		src  string
		code int
	}{
		{`{`, -32700}, {`null`, -32600}, {`[]`, -32600},
		{`{"id":1,"method":"ping"}`, -32600},
		{`{"jsonrpc":"2.0","id":{},"method":"ping"}`, -32600},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":[]}`, -32602},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"aether_explain_code","arguments":{"code":3}}}`, -32602},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"aether_context","arguments":{"source":"module X"}}}`, -32602},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			var out bytes.Buffer
			ServeMCP(bytes.NewBufferString(c.src+"\n"), &out)
			var r struct {
				Error struct {
					Code int `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &r); err != nil {
				t.Fatalf("missing JSON response: %q", out.String())
			}
			if r.Error.Code != c.code {
				t.Fatalf("want %d got %s", c.code, out.String())
			}
		})
	}
	var out bytes.Buffer
	ServeMCP(bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n"+`{"jsonrpc":"2.0","id":1234567890123456789,"method":"ping"}`+"\n"), &out)
	if !bytes.Contains(out.Bytes(), []byte(`1234567890123456789`)) {
		t.Fatalf("request id lost precision: %s", out.String())
	}
}

func TestIterationMCPReadOnly(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "data.aedb")
	file := filepath.Join(dir, "main.ae")
	s, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	src := `entity T { a: Text }
 let x = insert(T, {a: "must not persist"})`
	os.WriteFile(file, []byte(src), 0600)
	before, _ := os.ReadFile(db)
	for _, tool := range mcpTools() {
		if tool.Name == "aether_migrate_plan" {
			_, err = tool.Run(map[string]any{"file": file, "db": db})
			if err == nil {
				t.Error("read-only migration tool executed top-level mutation")
			}
		}
	}
	after, _ := os.ReadFile(db)
	if !bytes.Equal(before, after) {
		t.Error("MCP modified database")
	}
	// Even opening a large or torn WAL must not repair/compact the user's file.
	data := bytes.Repeat([]byte(`{"op":"seq","t":"T","id":1}`+"\n"), 501)
	data = append(data, []byte(`{"op":`)...)
	os.WriteFile(db, data, 0600)
	os.WriteFile(file, []byte(`entity T { a: Text }`), 0600)
	for _, tool := range mcpTools() {
		if tool.Name == "aether_migrate_plan" {
			_, err = tool.Run(map[string]any{"file": file, "db": db})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	after, _ = os.ReadFile(db)
	if !bytes.Equal(data, after) {
		t.Error("read-only plan repaired/compacted source WAL")
	}
}
func TestIterationMCPNoNetwork(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.Write([]byte("ok")) }))
	defer srv.Close()
	for _, tool := range mcpTools() {
		if tool.Name == "aether_test" {
			_, err := tool.Run(map[string]any{"source": `let x = http_get("` + srv.URL + `")`})
			if err == nil {
				t.Error("MCP test permitted external network effect")
			}
		}
	}
	if calls != 0 {
		t.Errorf("network called %d times", calls)
	}
}
