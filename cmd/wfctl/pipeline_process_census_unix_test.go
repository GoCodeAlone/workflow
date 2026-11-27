//go:build linux || darwin

package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func TestPipelineLinuxCensusSnapshot(t *testing.T) {
	type statResult struct {
		data string
		err  error
	}
	absent := func(err error) error {
		return &os.PathError{Op: "read", Path: "/proc/101/stat", Err: err}
	}
	const live = "202 (synthetic) R 1 700"
	for _, tc := range []struct {
		name          string
		entries       []string
		stats         map[string]statResult
		excluded      int
		wantActive    bool
		wantErr       error
		wantErrorText string
		wantReads     []string
	}{
		{name: "vanished_ESRCH_then_live", entries: []string{"101", "202"}, stats: map[string]statResult{"101": {err: absent(syscall.ESRCH)}, "202": {data: live}}, wantActive: true, wantReads: []string{"101", "202"}},
		{name: "vanished_ENOENT_then_live", entries: []string{"101", "202"}, stats: map[string]statResult{"101": {err: absent(syscall.ENOENT)}, "202": {data: live}}, wantActive: true, wantReads: []string{"101", "202"}},
		{name: "vanished_ESRCH_only", entries: []string{"101"}, stats: map[string]statResult{"101": {err: absent(syscall.ESRCH)}}, wantReads: []string{"101"}},
		{name: "permission_error_fails_closed", entries: []string{"101", "202"}, stats: map[string]statResult{"101": {err: absent(os.ErrPermission)}, "202": {data: live}}, wantErr: os.ErrPermission, wantReads: []string{"101"}},
		{name: "IO_error_fails_closed", entries: []string{"101", "202"}, stats: map[string]statResult{"101": {err: absent(syscall.EIO)}, "202": {data: live}}, wantErr: syscall.EIO, wantReads: []string{"101"}},
		{name: "malformed_stat_fails_closed", entries: []string{"101"}, stats: map[string]statResult{"101": {data: "invalid"}}, wantErrorText: "invalid process census", wantReads: []string{"101"}},
		{name: "incomplete_stat_fails_closed", entries: []string{"101"}, stats: map[string]statResult{"101": {data: "101 (synthetic) R"}}, wantErrorText: "invalid process census", wantReads: []string{"101"}},
		{name: "invalid_group_fails_closed", entries: []string{"101"}, stats: map[string]statResult{"101": {data: "101 (synthetic) R 1 invalid"}}, wantErrorText: "invalid syntax", wantReads: []string{"101"}},
		{name: "zombie_then_live", entries: []string{"101", "202"}, stats: map[string]statResult{"101": {data: "101 (synthetic) Z 1 700"}, "202": {data: live}}, wantActive: true, wantReads: []string{"101", "202"}},
		{name: "exited_then_live", entries: []string{"101", "202"}, stats: map[string]statResult{"101": {data: "101 (synthetic) X 1 700"}, "202": {data: live}}, wantActive: true, wantReads: []string{"101", "202"}},
		{name: "other_group_then_live", entries: []string{"101", "202"}, stats: map[string]statResult{"101": {data: "101 (synthetic) R 1 800"}, "202": {data: live}}, wantActive: true, wantReads: []string{"101", "202"}},
		{name: "excluded_PID_not_read", entries: []string{"101", "202"}, excluded: 101, stats: map[string]statResult{"202": {data: "202 (synthetic) R 1 800"}}, wantReads: []string{"202"}},
		{name: "non_PID_entries_not_read", entries: []string{"self", "cpuinfo"}},
		{name: "empty_snapshot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, name := range tc.entries {
				if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
					t.Fatal(err)
				}
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			var reads []string
			// Only owned directory entries and inert metadata are supplied. This
			// test never reads /proc, inspects a process, or sends a signal.
			active, err := pipelineLinuxProcessGroupActive(entries, 700, tc.excluded, func(name string) ([]byte, error) {
				reads = append(reads, name)
				result, ok := tc.stats[name]
				if !ok {
					t.Fatalf("unexpected stat request for %s", name)
				}
				return []byte(result.data), result.err
			})
			if active != tc.wantActive {
				t.Errorf("active=%v, want %v; error=%v", active, tc.wantActive, err)
			}
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("error=%v, want %v", err, tc.wantErr)
				}
			case tc.wantErrorText != "":
				if err == nil || !strings.Contains(err.Error(), tc.wantErrorText) {
					t.Errorf("error=%v, want %q", err, tc.wantErrorText)
				}
			default:
				if err != nil {
					t.Errorf("unexpected census error: %v", err)
				}
			}
			if !slices.Equal(reads, tc.wantReads) {
				t.Errorf("stat requests=%v, want %v", reads, tc.wantReads)
			}
		})
	}
}
