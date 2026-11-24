package main

import (
	"encoding/json"
	"testing"
)

func TestIterationReadOnlyPermissions(t *testing.T) {
	src := `auth {
 roles admin, reader
 default reader
 signup open
 login required
 can reader read Item
}
 entity Item { title: Text }`
	a, err := NewApp(src, ":memory:", "")
	if err != nil {
		t.Fatal(err)
	}
	signup := func(name string) string {
		code, _, body := a.Handle("POST", "/api/_auth/signup", []byte(`{"username":"`+name+`","password":"secret1"}`), "")
		if code != 201 {
			t.Fatalf("signup %d %s", code, body)
		}
		var v map[string]any
		json.Unmarshal(body, &v)
		return v["token"].(string)
	}
	admin, reader := signup("boss"), signup("reader")
	cases := []struct {
		method, path, token string
		want                int
	}{
		{"POST", "/api/Item", admin, 201},
		{"GET", "/api/Item", reader, 200},
		{"POST", "/api/Item", reader, 403},
		{"PATCH", "/api/Item/1", reader, 403},
		{"DELETE", "/api/Item/1", reader, 403},
		{"POST", "/api/Item", "", 401},
	}
	for _, c := range cases {
		code, _, body := a.Handle(c.method, c.path, []byte(`{"title":"test"}`), "admin\x00"+c.token)
		if code != c.want {
			t.Errorf("%s %s want=%d got=%d %s", c.method, c.path, c.want, code, body)
		}
	}
}
