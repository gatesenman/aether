package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// 每个在源码中出现的错误码都必须有中文解释。
func TestEveryCodeDocumented(t *testing.T) {
	re := regexp.MustCompile(`"([EWMR][0-9]{3})"`)
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if f == "errcodes.go" || f == "errcodes_test.go" {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			if _, ok := ErrDocs[m[1]]; !ok {
				t.Errorf("%s: code %s has no ErrDocs entry", f, m[1])
			}
		}
	}
	if s, ok := ExplainCode("e200"); !ok || s == "" {
		t.Fatal("explain e200")
	}
}
