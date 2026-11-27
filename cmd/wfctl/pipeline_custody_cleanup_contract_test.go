//go:build linux || darwin

package main

import (
	"errors"
	"os"
	"slices"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

type inertPipelineCustodyHandle struct {
	calls            *[]string
	dead             bool
	pollErr, sendErr error
	closeErr         error
}

func (h *inertPipelineCustodyHandle) exited() (bool, error) {
	*h.calls = append(*h.calls, "poll")
	return h.dead, h.pollErr
}
func (h *inertPipelineCustodyHandle) kill() error {
	*h.calls = append(*h.calls, "send-handle")
	return h.sendErr
}
func (h *inertPipelineCustodyHandle) close() error {
	*h.calls = append(*h.calls, "close")
	return h.closeErr
}

func TestPipelineCustodyCleanupHandles(t *testing.T) {
	owned := pipelineProcessIdentity{PID: 101, PGID: 100, Start: "private-fixture-start"}
	changed := pipelineProcessIdentity{PID: 101, PGID: 100, Start: "different-fixture-start"}
	permission := &os.PathError{Op: "read", Path: "inert-stat", Err: os.ErrPermission}
	for _, tc := range []struct {
		name                         string
		invalid                      *pipelineProcessIdentity
		mismatchValue                *pipelineProcessIdentity
		readErrAt, mismatchAt        int
		readErr, openErr             error
		openNil, dead                bool
		openHandleOnError            bool
		missingIdentity, missingOpen bool
		pollErr, sendErr, closeErr   error
		wantErr, wantErrAlso         error
		wantRefusal                  bool
		wantSends                    int
		wantCalls                    []string
	}{
		{name: "validated_live_handle", wantSends: 1, wantCalls: []string{"identity", "open", "identity", "poll", "send-handle", "close"}},
		{name: "invalid_PID", invalid: &pipelineProcessIdentity{PGID: 100, Start: owned.Start}, wantRefusal: true},
		{name: "invalid_PGID", invalid: &pipelineProcessIdentity{PID: 101, Start: owned.Start}, wantRefusal: true},
		{name: "missing_start", invalid: &pipelineProcessIdentity{PID: 101, PGID: 100}, wantRefusal: true},
		{name: "missing_identity_reader", missingIdentity: true, wantRefusal: true},
		{name: "missing_handle_opener", missingOpen: true, wantRefusal: true},
		{name: "already_absent", readErrAt: 1, readErr: syscall.ESRCH, wantCalls: []string{"identity"}},
		{name: "initial_permission_refusal", readErrAt: 1, readErr: permission, wantErr: os.ErrPermission, wantCalls: []string{"identity"}},
		{name: "initial_identity_refusal", mismatchAt: 1, wantRefusal: true, wantCalls: []string{"identity"}},
		{name: "handle_unavailable_refusal", openErr: syscall.ENOSYS, wantErr: syscall.ENOSYS, wantCalls: []string{"identity", "open"}},
		{name: "handle_permission_refusal", openErr: syscall.EPERM, wantErr: syscall.EPERM, wantCalls: []string{"identity", "open"}},
		{name: "unexpected_open_ENOENT_refusal", openErr: syscall.ENOENT, wantErr: syscall.ENOENT, wantCalls: []string{"identity", "open"}},
		{name: "acquired_handle_closes_on_open_error", openErr: syscall.EIO, openHandleOnError: true, wantErr: syscall.EIO, wantCalls: []string{"identity", "open", "close"}},
		{name: "exit_before_open", openErr: syscall.ESRCH, wantCalls: []string{"identity", "open"}},
		{name: "nil_handle_refusal", openNil: true, wantRefusal: true, wantCalls: []string{"identity", "open"}},
		{name: "identity_changes_after_open", mismatchAt: 2, wantRefusal: true, wantCalls: []string{"identity", "open", "identity", "close"}},
		{name: "PID_changes_after_open", mismatchAt: 2, mismatchValue: &pipelineProcessIdentity{PID: 202, PGID: 100, Start: owned.Start}, wantRefusal: true, wantCalls: []string{"identity", "open", "identity", "close"}},
		{name: "PGID_changes_after_open", mismatchAt: 2, mismatchValue: &pipelineProcessIdentity{PID: 101, PGID: 200, Start: owned.Start}, wantRefusal: true, wantCalls: []string{"identity", "open", "identity", "close"}},
		{name: "exit_during_revalidation", readErrAt: 2, readErr: syscall.ESRCH, wantCalls: []string{"identity", "open", "identity", "close"}},
		{name: "revalidation_permission_refusal", readErrAt: 2, readErr: permission, wantErr: os.ErrPermission, wantCalls: []string{"identity", "open", "identity", "close"}},
		{name: "handle_exited", dead: true, wantCalls: []string{"identity", "open", "identity", "poll", "close"}},
		{name: "poll_failure_refusal", pollErr: syscall.EINTR, wantErr: syscall.EINTR, wantCalls: []string{"identity", "open", "identity", "poll", "close"}},
		{name: "exit_at_send", sendErr: syscall.ESRCH, wantSends: 1, wantCalls: []string{"identity", "open", "identity", "poll", "send-handle", "close"}},
		{name: "send_permission_failure", sendErr: syscall.EPERM, wantErr: syscall.EPERM, wantSends: 1, wantCalls: []string{"identity", "open", "identity", "poll", "send-handle", "close"}},
		{name: "unexpected_send_ENOENT_failure", sendErr: syscall.ENOENT, wantErr: syscall.ENOENT, wantSends: 1, wantCalls: []string{"identity", "open", "identity", "poll", "send-handle", "close"}},
		{name: "close_failure_no_retry", closeErr: syscall.EIO, wantErr: syscall.EIO, wantSends: 1, wantCalls: []string{"identity", "open", "identity", "poll", "send-handle", "close"}},
		{name: "close_failure_preserves_refusal", readErrAt: 2, readErr: permission, closeErr: syscall.EIO, wantErr: os.ErrPermission, wantErrAlso: syscall.EIO, wantCalls: []string{"identity", "open", "identity", "close"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			reads := 0
			handle := &inertPipelineCustodyHandle{calls: &calls, dead: tc.dead, pollErr: tc.pollErr, sendErr: tc.sendErr, closeErr: tc.closeErr}
			ops := pipelineCustodyCleanupOps{
				identity: func(pid int) (pipelineProcessIdentity, error) {
					calls = append(calls, "identity")
					reads++
					if pid != owned.PID {
						t.Fatalf("unexpected inert identity %d", pid)
					}
					if reads == tc.readErrAt {
						return pipelineProcessIdentity{}, tc.readErr
					}
					if reads == tc.mismatchAt {
						if tc.mismatchValue != nil {
							return *tc.mismatchValue, nil
						}
						return changed, nil
					}
					return owned, nil
				},
				open: func(pid int) (pipelineCustodyCleanupHandle, error) {
					calls = append(calls, "open")
					if pid != owned.PID {
						t.Fatalf("unexpected inert handle %d", pid)
					}
					if tc.openErr != nil || tc.openNil {
						if tc.openHandleOnError {
							return handle, tc.openErr
						}
						return nil, tc.openErr
					}
					return handle, nil
				},
			}
			if tc.missingIdentity {
				ops.identity = nil
			}
			if tc.missingOpen {
				ops.open = nil
			}
			identity := owned
			if tc.invalid != nil {
				identity = *tc.invalid
			}
			err := cleanupPipelineCustodyProcessWith(identity, ops)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("error=%v, want %v", err, tc.wantErr)
				}
			} else if tc.wantRefusal {
				if err == nil {
					t.Error("unproven ownership was accepted")
				}
			} else if err != nil {
				t.Errorf("unexpected cleanup error: %v", err)
			}
			if tc.wantErrAlso != nil && !errors.Is(err, tc.wantErrAlso) {
				t.Errorf("error=%v lost %v", err, tc.wantErrAlso)
			}
			if !slices.Equal(calls, tc.wantCalls) {
				t.Errorf("operations=%v, want %v", calls, tc.wantCalls)
			}
			if sends := countPipelineCustodyCalls(calls, "send-handle"); sends != tc.wantSends {
				t.Errorf("handle sends=%d, want %d", sends, tc.wantSends)
			}
		})
	}
}

func countPipelineCustodyCalls(calls []string, name string) int {
	count := 0
	for _, call := range calls {
		if call == name {
			count++
		}
	}
	return count
}

func TestPipelineCustodyCleanupPoll(t *testing.T) {
	for _, tc := range []struct {
		name         string
		count        int
		events       int16
		err          error
		dead, refuse bool
	}{
		{name: "unambiguously_live"},
		{name: "terminated", count: 1, events: unix.POLLIN, dead: true},
		{name: "reaped", count: 1, events: unix.POLLHUP, dead: true},
		{name: "terminated_and_reaped", count: 1, events: unix.POLLIN | unix.POLLHUP, dead: true},
		{name: "invalid_fd", count: 1, events: unix.POLLNVAL, refuse: true},
		{name: "error", count: 1, events: unix.POLLERR, refuse: true},
		{name: "exit_with_error", count: 1, events: unix.POLLIN | unix.POLLERR, refuse: true},
		{name: "interrupted", err: syscall.EINTR, refuse: true},
		{name: "unexpected_flags", count: 1, events: unix.POLLPRI, refuse: true},
		{name: "ready_without_flags", count: 1, refuse: true},
		{name: "flags_without_ready", events: unix.POLLIN, refuse: true},
		{name: "negative_count", count: -1, refuse: true},
		{name: "excess_ready_count", count: 2, events: unix.POLLIN, refuse: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dead, err := classifyPipelineCustodyPoll(tc.count, tc.events, tc.err)
			if dead != tc.dead || (err != nil) != tc.refuse {
				t.Fatalf("dead=%v error=%v; want dead=%v refusal=%v", dead, err, tc.dead, tc.refuse)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("poll error lost: %v", err)
			}
		})
	}
}
