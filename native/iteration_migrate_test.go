package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func migrationFixture(t *testing.T, src string) *Interp {
	t.Helper()
	it := NewInterp(":memory:", false)
	if err := it.Load(src, ""); err != nil {
		t.Fatal(err)
	}
	return it
}
func TestIterationMigrationValidation(t *testing.T) {
	for _, def := range []string{`"bad"`, `nil`} {
		it := migrationFixture(t, "entity T { a: Text p: Int = "+def+" }")
		r := NewRec()
		r.Set("a", "ok")
		it.Store.rawPut("T", 1, r)
		if st := PlanMigration(it); !anyBlock(st) {
			t.Errorf("invalid candidate accepted: %+v", st)
		}
	}
	it := migrationFixture(t, "entity T { a: Text invariant len(a) > 1 }")
	r := NewRec()
	r.Set("a", "valid")
	it.Store.rawPut("T", 1, r)
	old := PlanMigration(it)
	r = NewRec()
	r.Set("a", "x")
	it.Store.rawPut("T", 1, r)
	if _, err := ApplyMigration(it, old, false); err == nil {
		t.Error("stale plan bypassed new invariant violation")
	}
}

func TestIterationMigrationPlanNoSideEffects(t *testing.T) {
	it := migrationFixture(t, `entity T { a: Text p: Int = seed() }
 fn seed() -> Int ! db { let x = insert(T, {a: "new", p: 1}) 1 }`)
	r := NewRec()
	r.Set("a", "old")
	it.Store.rawPut("T", 1, r)
	if st := PlanMigration(it); !anyBlock(st) {
		t.Errorf("mutating default not blocked: %+v", st)
	}
	if len(it.Store.tables["T"].rows) != 1 {
		t.Fatal("planning executed a mutating default")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "main.ae")
	db := filepath.Join(dir, "data.aedb")
	os.WriteFile(file, []byte(`entity T { a: Text }
 let x = insert(T, {a: "side effect"})`), 0600)
	os.WriteFile(db, []byte{}, 0600)
	before, _ := os.ReadFile(db)
	if code := runCLI([]string{"migrate", file, "--db", db, "--json"}); code == 0 {
		t.Error("CLI plan executed a top-level write")
	}
	after, _ := os.ReadFile(db)
	if !bytes.Equal(before, after) {
		t.Error("CLI plan changed source database")
	}
	missing := filepath.Join(dir, "missing.aedb")
	os.WriteFile(file, []byte(`entity T { a: Text }`), 0600)
	if code := runCLI([]string{"migrate", file, "--db", missing, "--json"}); code != 0 {
		t.Fatal(code)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("plan created a database")
	}
}
