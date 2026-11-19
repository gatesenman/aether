package main

import (
	"path/filepath"
	"testing"
)

func TestMigratePlanApply(t *testing.T) {
	db := filepath.Join(t.TempDir(), "m.aedb")
	it := NewInterp(db, false)
	if err := it.Load("entity T {\n a: Text\n old: Int\n}\nlet x = insert(T, {a: \"hi\", old: 1})\n", ""); err != nil {
		t.Fatal(err)
	}
	it.Store.Close()
	it = NewInterp(db, false)
	if err := it.Load("entity T {\n a: Text\n p: Int = 3\n}\n", ""); err != nil {
		t.Fatal(err)
	}
	steps := PlanMigration(it)
	got := map[string]bool{}
	for _, s := range steps {
		got[s.Code+":"+s.Field] = true
	}
	if !got["M400:p"] || !got["M402:old"] || len(steps) != 2 {
		t.Fatalf("plan: %+v", steps)
	}
	if n, err := ApplyMigration(it, steps, false); err != nil || n != 1 {
		t.Fatalf("apply n=%d err=%v", n, err)
	}
	if s := PlanMigration(it); len(s) != 0 {
		t.Fatalf("after apply: %+v", s)
	}
	r := it.Store.All("T")[0]
	if r.M["p"] != int64(3) {
		t.Fatalf("backfill: %v", r.M["p"])
	}
	it.Store.Close()
}
