// Command ao is AgentOrchestra: a Multi-Agent context-handoff tool. It runs
// either as an MCP server (`ao mcp`, stdio transport, for agents to connect
// to) or as a plain CLI (`ao init/status/handoff/log`, for humans).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ponthepmk/AgentOrchestra/internal/cli"
	"github.com/ponthepmk/AgentOrchestra/internal/mcpserver"
)

var version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) > 0 && args[0] == "mcp" {
		server := mcpserver.New(version)
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			fmt.Fprintf(os.Stderr, "ao mcp: %v\n", err)
			return 1
		}
		return 0
	}
	if len(args) > 0 && (args[0] == "version" || args[0] == "--version") {
		fmt.Println("ao version " + version)
		return 0
	}
	return cli.Run(args, os.Stdout, os.Stderr)
}
