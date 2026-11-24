package main

import (
	"strings"
	"testing"
)

func TestIterationParserDepth(t *testing.T) {
	for _, e := range []string{strings.Repeat("(", 700) + "1" + strings.Repeat(")", 700), strings.Repeat("not ", 700) + "true", strings.Repeat("[", 700) + "1" + strings.Repeat("]", 700)} {
		if _, err := Parse("let x = " + e); err == nil {
			t.Error("excessive nesting accepted")
		}
	}
	if _, err := Parse("let x = " + strings.Repeat("(", 20) + "1" + strings.Repeat(")", 20)); err != nil {
		t.Fatal(err)
	}
}
