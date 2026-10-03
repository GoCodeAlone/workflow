package module

import (
	"os"

	"golang.org/x/sys/windows"
)

func replaceIaCStateFile(from, to string) error {
	return replaceIaCStateFileWith(from, to, windows.MoveFileEx)
}

func replaceIaCStateFileWith(from, to string, move func(*uint16, *uint16, uint32) error) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return &os.LinkError{Op: "replace", Old: from, New: to, Err: err}
	}
	target, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return &os.LinkError{Op: "replace", Old: from, New: to, Err: err}
	}
	// The temp file shares the target directory: no copy/delete fallback or
	// delayed operation is allowed. WRITE_THROUGH is the durability boundary.
	if err := move(source, target, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return &os.LinkError{Op: "replace", Old: from, New: to, Err: err}
	}
	return nil
}

func syncIaCStateDirectory(string) error {
	// Replacement already used WRITE_THROUGH. FlushFileBuffers on the
	// read-only directory handle used by os.Open is not a Windows fsync.
	return nil
}
