//go:build ignore

// Pack: go run pack_setup.go <sfx.exe> <payload.zip> <out-Setup.exe>
package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: pack_setup sfx.exe payload.zip out-Setup.exe")
		os.Exit(2)
	}
	sfx, err := os.ReadFile(os.Args[1])
	must(err)
	zipb, err := os.ReadFile(os.Args[2])
	must(err)
	magic := []byte("AETHERSFX0.6.0!!")
	var size [8]byte
	binary.LittleEndian.PutUint64(size[:], uint64(len(zipb)))
	out := append(append(append(sfx, magic...), size[:]...), zipb...)
	must(os.WriteFile(os.Args[3], out, 0755))
	fmt.Printf("wrote %s (%d bytes)\n", os.Args[3], len(out))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
