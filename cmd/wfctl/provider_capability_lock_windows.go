//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func durableReplaceCredentialOperationState(temporaryPath, path string) error {
	from, err := windows.UTF16PtrFromString(temporaryPath)
	if err != nil {
		return fmt.Errorf("encode temporary credential state path: %w", err)
	}
	to, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode credential state path: %w", err)
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// Windows FileMode permission bits report only the read-only attribute, not
// ACL ownership/group/world access. Structural checks still reject symlinks
// and non-regular paths; POSIX privacy bits are intentionally not interpreted.
func privateRecoveryStateMode(os.FileInfo) bool {
	return true
}

func lockCredentialOperationFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var overlapped windows.Overlapped
	err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &overlapped,
	)
	if err != nil {
		_ = file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, errCredentialOperationLocked
		}
		return nil, err
	}
	return file, nil
}

func unlockCredentialOperationFile(file *os.File) error {
	if file == nil {
		return nil
	}
	var overlapped windows.Overlapped
	unlockErr := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
	return errors.Join(unlockErr, file.Close())
}
