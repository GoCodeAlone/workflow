//go:build linux || darwin

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// pipelineLinuxProcessGroupActive classifies a Linux census snapshot. The
// caller supplies the stat reader so transient rows can be tested without
// inspecting processes. Darwin keeps its native census implementation.
func pipelineLinuxProcessGroupActive(entries []os.DirEntry, pgid, excludedPID int, readStat func(string) ([]byte, error)) (bool, error) {
	for _, entry := range entries {
		if pid, err := strconv.Atoi(entry.Name()); err != nil || pid == excludedPID {
			continue
		}
		data, err := readStat(entry.Name())
		// Tasks can disappear after /proc enumeration. Only absence is safe to
		// skip; permission, I/O, and malformed-record errors remain fail-closed.
		if pipelineProcessAbsent(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			return false, fmt.Errorf("invalid process census")
		}
		fields := strings.Fields(string(data[end+1:]))
		if len(fields) < 3 {
			return false, fmt.Errorf("invalid process census")
		}
		group, err := strconv.Atoi(fields[2])
		if err != nil {
			return false, err
		}
		if group == pgid && fields[0] != "Z" && fields[0] != "X" {
			return true, nil
		}
	}
	return false, nil
}
