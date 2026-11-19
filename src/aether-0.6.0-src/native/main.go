package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(runCLI(os.Args[1:]))
}

func fatal(e error) int {
	fmt.Fprintln(os.Stderr, e)
	return 1
}
