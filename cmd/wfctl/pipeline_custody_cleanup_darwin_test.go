package main

import "fmt"

func openPipelineCustodyCleanupHandle(_ int) (pipelineCustodyCleanupHandle, error) {
	return nil, fmt.Errorf("ownership-safe orphan process handles unavailable on darwin; refusing numeric-PID cleanup")
}
