//go:build linux || darwin

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

type pipelineCustodyCleanupHandle interface {
	exited() (bool, error)
	kill() error
	close() error
}

type pipelineCustodyCleanupOps struct {
	identity func(int) (pipelineProcessIdentity, error)
	open     func(int) (pipelineCustodyCleanupHandle, error)
}

func cleanupPipelineCustodyProcessWith(identity pipelineProcessIdentity, ops pipelineCustodyCleanupOps) (err error) {
	if identity.PID <= 0 || identity.PGID <= 0 || identity.Start == "" || ops.identity == nil || ops.open == nil {
		return fmt.Errorf("invalid fixture cleanup identity or operations")
	}
	current, err := ops.identity(identity.PID)
	if pipelineProcessAbsent(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if current != identity {
		return fmt.Errorf("fixture cleanup identity no longer matches")
	}
	handle, err := ops.open(identity.PID)
	if handle != nil {
		defer func() {
			if closeErr := handle.close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close fixture process handle: %w", closeErr))
			}
		}()
	}
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return err
	}
	if handle == nil {
		return fmt.Errorf("fixture cleanup did not acquire a process handle")
	}
	// Only fixture-owned identities are accepted: private helper records or
	// direct exec.Cmd-owned children, never arbitrary external PID records.
	// Revalidate after acquisition, then prove the handle has not exited. This
	// binds the numeric lookup to the opened process; any later signal uses only
	// that stable handle, even if its former PID is subsequently recycled.
	current, err = ops.identity(identity.PID)
	if pipelineProcessAbsent(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if current != identity {
		return fmt.Errorf("fixture cleanup identity changed during handle acquisition")
	}
	exited, err := handle.exited()
	if err != nil {
		return err
	}
	if exited {
		return nil
	}
	if err := handle.kill(); err != nil && !errors.Is(err, unix.ESRCH) {
		return err
	}
	return nil
}

func pipelineCustodyCleanupIdentity(pid int) (pipelineProcessIdentity, error) {
	start, err := pipelineProcessStartToken(pid)
	if err != nil {
		return pipelineProcessIdentity{}, err
	}
	pgid, err := unix.Getpgid(pid)
	if err != nil {
		return pipelineProcessIdentity{}, err
	}
	return pipelineProcessIdentity{PID: pid, PGID: pgid, Start: start}, nil
}

func cleanupPipelineCustodyFixtureIdentity(identity pipelineProcessIdentity) error {
	return cleanupPipelineCustodyProcessWith(identity, pipelineCustodyCleanupOps{
		identity: pipelineCustodyCleanupIdentity,
		open:     openPipelineCustodyCleanupHandle,
	})
}

// Call only for records written by the helpers in this fixture's t.TempDir.
// These records establish fixture ownership; they are not external PID input.
func cleanupPipelineCustodyFixtureFiles(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(root, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Errorf("read fixture cleanup identity %s: %v", name, err)
			continue
		}
		var identity pipelineProcessIdentity
		if err := json.Unmarshal(data, &identity); err != nil {
			t.Errorf("decode fixture cleanup identity %s: %v", name, err)
			continue
		}
		if err := cleanupPipelineCustodyFixtureIdentity(identity); err != nil {
			t.Errorf("refusing unsafe fixture cleanup for %s: %v", name, err)
		}
	}
}

func classifyPipelineCustodyPoll(count int, events int16, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	if count == 0 && events == 0 {
		return false, nil
	}
	if count == 1 && events != 0 && events & ^int16(unix.POLLIN|unix.POLLHUP) == 0 {
		return true, nil
	}
	return false, fmt.Errorf("unverified process handle poll state: count=%d events=%d", count, events)
}
