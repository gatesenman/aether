package main

import "testing"

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
