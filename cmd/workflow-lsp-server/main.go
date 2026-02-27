// Package main is the entrypoint for the workflow LSP server binary.
// It communicates with editors via the Language Server Protocol over stdio.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/lsp"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	lsp.Version = version
	s := lsp.NewServer()
	if err := s.RunStdio(); err != nil {
		fmt.Fprintf(os.Stderr, "workflow-lsp-server error: %v\n", err)
		os.Exit(1)
	}
}
