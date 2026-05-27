// pp-mt5-mcp exposes the pp-mt5 command tree over the MCP protocol so
// Claude Desktop and other MCP clients can drive MetaTrader 5 directly.
//
// Transport: stdio (the default MCP transport). Logs go to stderr; stdout is
// the JSON-RPC stream and must not be written to from anywhere else.
//
// Install + register with Claude Desktop:
//
//	go install github.com/ek-labs/pp-mt5/cmd/pp-mt5-mcp@latest
//	claude mcp add pp-mt5-mcp -- pp-mt5-mcp
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"

	ppmcp "github.com/ek-labs/pp-mt5/internal/mcp"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	listTools := flag.Bool("list-tools", false, "print registered tool names and exit (does not start the server)")
	logFile := flag.String("log-file", "", "append a line per tool call to this file (timestamp, tool, exit code, error summary). Useful when running under Claude Desktop where stderr is hard to see.")
	flag.Parse()

	if *showVersion {
		fmt.Println("pp-mt5-mcp", ppmcp.ServerVersion())
		return
	}

	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "pp-mt5-mcp: --log-file %q: %v\n", *logFile, err)
			os.Exit(1)
		}
		defer f.Close()
		ppmcp.SetLogWriter(f)
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
