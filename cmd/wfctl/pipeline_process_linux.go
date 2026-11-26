package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func pipelineProcessStartToken(pid int) (string, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	// comm may contain whitespace and parentheses; fields after its final ')'
	// start with field 3 (state), not field 1 (pid).
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", fmt.Errorf("invalid process identity")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) <= 19 {
		return "", fmt.Errorf("incomplete process identity")
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", err
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return "linux:" + strings.TrimSpace(string(boot)) + ":" + fields[19], nil
}

func pipelineProcessGroupActive(pgid int) (bool, error) {
	return pipelineProcessGroupActiveExcept(pgid, 0)
}

func pipelineProcessGroupActiveExcept(pgid, excludedPID int) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	return pipelineLinuxProcessGroupActive(entries, pgid, excludedPID, func(name string) ([]byte, error) {
		return os.ReadFile("/proc/" + name + "/stat")
	})
}
