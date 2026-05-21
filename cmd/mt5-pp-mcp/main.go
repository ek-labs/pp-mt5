// mt5-pp-mcp exposes the mt5-pp-cli command tree over the MCP protocol so
// Claude Desktop and other MCP clients can drive MetaTrader 5 directly.
//
// Phase 10 stub. Implementation pending — see ../../STATUS.md.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "mt5-pp-mcp: not yet implemented — see library/trading/mt5/STATUS.md (Phase 10)")
	os.Exit(1)
}
