package main

import (
	"errors"
	"math"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPipelineDarwinReapedProcessIsAbsent(t *testing.T) {
	if token, err := pipelineProcessStartToken(os.Getpid()); token == "" || err != nil {
		t.Fatalf("live process identity was rejected: %q, %v", token, err)
	}
	child := exec.Command("/usr/bin/true")
	if err := child.Run(); err != nil {
		t.Fatal(err)
	}
	pid := child.Process.Pid
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pid)
	if err != nil || len(processes) != 0 {
		t.Fatalf("reaped PID %d did not yield an empty process census: %v, %v", pid, processes, err)
	}
	if token, err := pipelineProcessStartToken(pid); token != "" || !errors.Is(err, unix.ESRCH) {
		t.Fatalf("reaped PID %d was not recognized as absent: %q, %v", pid, token, err)
	}
}

func TestPipelineDarwinProcessIdentityBounds(t *testing.T) {
	if active, err := pipelineProcessGroupActive(syscall.Getpgrp()); err != nil || !active {
		t.Fatalf("live process group was rejected: %v, %v", active, err)
	}
	for name, id := range map[string]int{
		"negative":        -1,
		"zero":            0,
		"above int32":     math.MaxInt32 + 1,
		"below int32":     math.MinInt32 - 1,
		"wrapped process": 1<<32 + os.Getpid(),
		"wrapped group":   1<<32 + syscall.Getpgrp(),
	} {
		t.Run(name, func(t *testing.T) {
			if token, err := pipelineProcessStartToken(id); err == nil || token != "" {
				t.Errorf("out-of-range PID %d returned a process identity: %q, %v", id, token, err)
			}
			if active, err := pipelineProcessGroupActive(id); err == nil || active {
				t.Errorf("out-of-range PGID %d did not fail closed: %v, %v", id, active, err)
			}
		})
	}
}
