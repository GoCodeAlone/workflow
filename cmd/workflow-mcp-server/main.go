// Command workflow-mcp-server starts the workflow MCP (Model Context Protocol) server.
//
// The server runs over stdio by default, making it compatible with any
// MCP-capable AI client. It exposes workflow engine tools (validate,
// list types, generate schemas) and resources (documentation, examples).
//
// Usage:
//
//	workflow-mcp-server [options]
//
// Options:
//
//	-plugin-dir string   Plugin data directory (default "data/plugins")
package main

import (
	"flag"
	"fmt"
	"os"

	workflowmcp "github.com/GoCodeAlone/workflow/mcp"
)

func main() {
	pluginDir := flag.String("plugin-dir", "data/plugins", "Plugin data directory")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("workflow-mcp-server %s\n", workflowmcp.Version)
		os.Exit(0)
	}

	srv := workflowmcp.NewServer(*pluginDir)
	if err := srv.ServeStdio(); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}
