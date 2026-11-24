package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIterationWALRecovery(t *testing.T) {
	for _, tail := range []string{`{"op":`, ""} {
		t.Run(tail, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "数据.aedb")
			line := `{"op":"put","t":"Item","id":1,"r":{"title":"旧"}}`
			if tail != "" {
				line += "\n" + tail
			}
			if err := os.WriteFile(p, []byte(line), 0600); err != nil {
				t.Fatal(err)
			}
			s, err := OpenStore(p)
			if err != nil {
				t.Fatal(err)
			}
			r := NewRec()
			r.Set("title", "新")
			s.rawPut("Item", 2, r)
			s.Close()
			s, err = OpenStore(p)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if s.tables["Item"].rows[2] == nil {
				t.Fatal("append after recovery was lost")
			}
		})
	}
}
func TestIterationWALCorruption(t *testing.T) {
	for _, data := range []string{"broken\n{}\n", "{}\n", `{"op":"put","t":"X","id":1,"r":null}` + "\n"} {
		t.Run(data, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("malformed WAL panicked: %v", r)
				}
			}()
			p := filepath.Join(t.TempDir(), "bad.aedb")
			os.WriteFile(p, []byte(data), 0600)
			s, err := OpenStore(p)
			if s != nil {
				s.Close()
			}
			if err == nil {
				t.Error("corrupt WAL accepted")
			}
			after, _ := os.ReadFile(p)
			if string(after) != data {
				t.Error("corrupt WAL modified")
			}
		})
	}
}

func TestIterationWALAutoCompact(t *testing.T) {
	p := filepath.Join(t.TempDir(), "compact.aedb")
	s, err := OpenStore(p)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRec()
	r.Set("title", "ok")
	for id := int64(1); id <= 1002; id++ {
		s.rawPut("Item", id, r)
	}
	s.Close()
	s, err = OpenStore(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.tables["Item"].rows) != 1002 {
		t.Fatalf("compaction lost rows: %d", len(s.tables["Item"].rows))
	}
}
func TestIterationWALWriteFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fail.aedb")
	s, err := OpenStore(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Ensure(userDecl)
	s.f.Close() // deterministic injected disk-handle failure
	r := NewRec()
	r.Set("username", "test")
	err = catch(func() { s.Insert("_User", r) })
	if err == nil {
		t.Fatal("failed disk write reported success")
	}
	if len(s.All("_User")) != 0 {
		t.Fatal("failed write changed memory")
	}
}
