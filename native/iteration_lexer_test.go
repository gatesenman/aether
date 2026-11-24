package main

import (
	"strings"
	"testing"
)

func TestIterationNumericOverflow(t *testing.T) {
	for _, src := range []string{"9223372036854775808", strings.Repeat("9", 400) + ".0"} {
		if _, err := Parse("let x = " + src); err == nil {
			t.Errorf("out-of-range literal accepted: %.30s", src)
		}
	}
	ts, err := Lex("9223372036854775807")
	if err != nil || ts[0].Val != int64(9223372036854775807) {
		t.Fatalf("valid boundary: %v %v", ts, err)
	}
}

func TestIterationMinimumInteger(t *testing.T) {
	it := NewInterp(":memory:", false)
	if err := it.Load("let low = -9223372036854775808", ""); err != nil {
		t.Fatal(err)
	}
	if got := it.Globals.Get("low", 1); got != int64(-9223372036854775808) {
		t.Fatalf("minimum integer changed: %v", got)
	}
	if _, err := Parse("let x = -9223372036854775809"); err == nil {
		t.Fatal("negative overflow accepted")
	}
	if _, err := Parse("fn f(x: Int) -> Int { match x { -9223372036854775808 => 1, _ => 0 } }"); err != nil {
		t.Fatal(err)
	}
}
