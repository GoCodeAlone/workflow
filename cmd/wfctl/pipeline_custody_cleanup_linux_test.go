package main

import (
	"fmt"
	"math"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

type pipelineCustodyPIDFD struct{ fd int }

func openPipelineCustodyCleanupHandle(pid int) (pipelineCustodyCleanupHandle, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, err
	}
	return &pipelineCustodyPIDFD{fd: fd}, nil
}

func (h *pipelineCustodyPIDFD) exited() (bool, error) {
	if h.fd < 0 || h.fd > math.MaxInt32 {
		return false, fmt.Errorf("invalid fixture process handle")
	}
	fds := []unix.PollFd{{Fd: int32(h.fd), Events: unix.POLLIN}}
	count, err := unix.Poll(fds, 0)
	return classifyPipelineCustodyPoll(count, fds[0].Revents, err)
}

func (h *pipelineCustodyPIDFD) kill() error {
	return unix.PidfdSendSignal(h.fd, unix.SIGKILL, nil, 0)
}

func (h *pipelineCustodyPIDFD) close() error { return unix.Close(h.fd) }

// This native control runs only in Linux CI. Local Darwin qualification only
// compiles it; it never starts this helper or invokes the kernel handle APIs.
func TestPipelineCustodyCleanupLinuxOwnedHandle(t *testing.T) {
	child := pipelineCustodyFixtureCommand(t, "actor", t.TempDir(), "control")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := false
	t.Cleanup(func() {
		if !reaped {
			// This is the original exec.Cmd-owned Process, never FindProcess.
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	identity, err := pipelineCustodyCleanupIdentity(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanupPipelineCustodyFixtureIdentity(identity); err != nil {
		t.Fatal(err)
	}
	waitErr := child.Wait()
	reaped = true
	if child.ProcessState == nil {
		t.Fatalf("owned fixture has no wait state: %v", waitErr)
	}
	status, ok := child.ProcessState.Sys().(syscall.WaitStatus)
	if waitErr == nil || !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("owned handle did not terminate its fixture: wait=%v status=%v", waitErr, status)
	}
}
