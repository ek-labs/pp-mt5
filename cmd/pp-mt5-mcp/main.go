// pp-mt5-mcp exposes the pp-mt5 command tree over the MCP protocol so
// Claude Desktop and other MCP clients can drive MetaTrader 5 directly.
//
// Transport: stdio (the default MCP transport). Logs go to stderr; stdout is
// the JSON-RPC stream and must not be written to from anywhere else.
//
// Install + register with Claude Desktop:
//
//	go install github.com/mvanhorn/printing-press-library/library/trading/mt5/cmd/pp-mt5-mcp@latest
//	claude mcp add pp-mt5-mcp -- pp-mt5-mcp
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"

	ppmcp "github.com/mvanhorn/printing-press-library/library/trading/mt5/internal/mcp"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	listTools := flag.Bool("list-tools", false, "print registered tool names and exit (does not start the server)")
	flag.Parse()

	if *showVersion {
		fmt.Println("pp-mt5-mcp", ppmcp.ServerVersion())
		return
	}

	s := ppmcp.NewServer()

	if *listTools {
		for _, name := range ppmcp.ListToolNames() {
			fmt.Println(name)
		}
		return
	}

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "pp-mt5-mcp: %v\n", err)
		os.Exit(1)
	}
}
