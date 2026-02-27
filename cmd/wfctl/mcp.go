package main

import (
	"flag"
	"fmt"

	workflowmcp "github.com/GoCodeAlone/workflow/mcp"
)

// runMCP starts the workflow MCP (Model Context Protocol) server over stdio.
// This exposes workflow engine tools and resources to AI assistants.
func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	pluginDir := fs.String("plugin-dir", "data/plugins", "Plugin data directory")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl mcp [options]

Start the workflow MCP (Model Context Protocol) server over stdio.
This exposes workflow engine tools and resources to AI assistants such as
Claude Desktop, VS Code with GitHub Copilot, and Cursor.

The server provides tools for listing module types, validating configs,
generating schemas, and inspecting workflow YAML configurations.

Options:
`)
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), `
Example Claude Desktop configuration (~/.config/claude/claude_desktop_config.json):

  {
    "mcpServers": {
      "workflow": {
        "command": "wfctl",
        "args": ["mcp", "-plugin-dir", "/path/to/data/plugins"]
      }
    }
  }

See docs/mcp.md for full setup instructions.
`)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	srv := workflowmcp.NewServer(*pluginDir)
	return srv.ServeStdio()
}
