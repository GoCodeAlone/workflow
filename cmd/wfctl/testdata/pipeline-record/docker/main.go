//go:build linux || darwin

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--record-fixture-late-create" {
		runRecordDockerLateCreate()
		return
	}
	args := os.Args[1:]
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		flag, _, inline := strings.Cut(args[0], "=")
		args = args[1:]
		if !inline && flag != "--tls" && flag != "--tlsverify" {
			if len(args) == 0 {
				os.Exit(64)
			}
			args = args[1:]
		}
	}
	if len(args) == 0 {
		os.Exit(64)
	}
	if runRecordDockerTransport(args) {
		return
	}
	// Only daemon identity and empty lifecycle queries are substituted. A
	// container operation is never simulated by this fixture.
	switch args[0] {
	case "context":
		if len(args) < 2 {
			os.Exit(64)
		}
		switch args[1] {
		case "show":
			fmt.Println("record-fixture-context")
		case "inspect":
			fmt.Println(`[{"Name":"record-fixture-context","Endpoints":{"docker":{"Host":"unix:///record-fixture-daemon.sock","SkipTLSVerify":false}},"TLSMaterial":{},"Storage":{}}]`)
		default:
			os.Exit(64)
		}
	case "info":
		for _, arg := range args[1:] {
			if arg == "{{json .ID}}" {
				_ = json.NewEncoder(os.Stdout).Encode("record-fixture-daemon")
				return
			}
		}
		fmt.Println("record-fixture-daemon")
	case "ps":
		// This test dependency has no containers, including cleanup-label matches.
	default:
		fmt.Fprintln(os.Stderr, "fixture Docker supports only context, info, and empty ps queries")
		os.Exit(64)
	}
}
