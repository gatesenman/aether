package main

import (
	"bytes"
	"encoding/json"
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
