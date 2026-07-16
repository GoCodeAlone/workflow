//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var syncCredentialOperationDirectory = func(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func durableReplaceCredentialOperationState(temporaryPath, path string) error {
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncCredentialOperationDirectory(filepath.Dir(path))
}

func privateRecoveryStateMode(info os.FileInfo) bool {
	return info.Mode().Perm()&0o077 == 0
}

func lockCredentialOperationFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
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
	unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
	return errors.Join(unlockErr, file.Close())
}
