// mt5-pp-mcp exposes the mt5-pp-cli command tree over the MCP protocol so
// Claude Desktop and other MCP clients can drive MetaTrader 5 directly.
//
// Transport: stdio (the default MCP transport). Logs go to stderr; stdout is
// the JSON-RPC stream and must not be written to from anywhere else.
//
// Install + register with Claude Desktop:
//
//	go install github.com/mvanhorn/printing-press-library/library/trading/mt5/cmd/mt5-pp-mcp@latest
//	claude mcp add mt5-pp-mcp -- mt5-pp-mcp
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
		fmt.Println("mt5-pp-mcp", ppmcp.ServerVersion)
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
		fmt.Fprintf(os.Stderr, "mt5-pp-mcp: %v\n", err)
		os.Exit(1)
	}
}
