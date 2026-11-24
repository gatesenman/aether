package main

import (
	"strings"
	"testing"
)

func TestIterationNumericOverflow(t *testing.T) {
	for _, src := range []string{"9223372036854775808", strings.Repeat("9", 400) + ".0"} {
		if _, err := Lex(src); err == nil {
			t.Errorf("out-of-range literal accepted: %.30s", src)
		}
	}
	ts, err := Lex("9223372036854775807")
	if err != nil || ts[0].Val != int64(9223372036854775807) {
		t.Fatalf("valid boundary: %v %v", ts, err)
	}
}
