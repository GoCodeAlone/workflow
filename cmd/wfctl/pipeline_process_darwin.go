package main

import (
	"fmt"
	"math"

	"golang.org/x/sys/unix"
)

func pipelineProcessStartToken(pid int) (string, error) {
	if pid <= 0 || pid > math.MaxInt32 {
		return "", fmt.Errorf("invalid pipeline process ID")
	}
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	if len(processes) == 0 {
		return "", unix.ESRCH
	}
	if len(processes) != 1 || processes[0].Proc.P_pid != int32(pid) {
		return "", fmt.Errorf("invalid pipeline process identity")
	}
	proc := &processes[0]
	return fmt.Sprintf("darwin:%d:%d", proc.Proc.P_starttime.Sec, proc.Proc.P_starttime.Usec), nil
}

func pipelineProcessGroupActive(pgid int) (bool, error) {
	return pipelineProcessGroupActiveExcept(pgid, 0)
}

func pipelineProcessGroupActiveExcept(pgid, excludedPID int) (bool, error) {
	if pgid <= 0 || pgid > math.MaxInt32 {
		return false, fmt.Errorf("invalid pipeline process group ID")
	}
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return false, err
	}
	for i := range processes {
		process := &processes[i]
		if process.Eproc.Pgid == int32(pgid) && int(process.Proc.P_pid) != excludedPID && process.Proc.P_stat != 5 {
			return true, nil
		}
	}
	return false, nil
}
