//go:build windows

package main

import (
	"os"
	"syscall"
)

// openStoreFile opens the WAL with FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE so
// concurrent readers (doctor, another process peeking) and rename-after-close compaction work.
func openStoreFile(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	const (
		fileAppendData       = 0x0004
		fileWriteAttributes  = 0x0100
		fileShareRead        = 0x00000001
		fileShareWrite       = 0x00000002
		fileShareDelete      = 0x00000004
		openAlways           = 4
		fileAttributeNormal  = 0x80
		fileFlagSequential   = 0x08000000
	)
	access := uint32(fileAppendData | fileWriteAttributes | syscall.SYNCHRONIZE)
	share := uint32(fileShareRead | fileShareWrite | fileShareDelete)
	h, err := syscall.CreateFile(p, access, share, nil, openAlways, fileAttributeNormal|fileFlagSequential, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}
