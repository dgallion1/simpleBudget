// Command whatif-mcp serves the what-if retirement planner over MCP on stdio,
// so a plan can be discussed in Claude Code. It reads saved scenarios and runs
// the projection engine; it never writes to the data directory and makes no
// network calls.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"

	"budget2/internal/services/storage"
	"budget2/internal/services/whatifmcp"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// resolveDataDir returns the settings directory: the flag when set, otherwise
// ./data/settings relative to the working directory.
func resolveDataDir(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return filepath.Join("data", "settings")
}

func main() {
	dir := flag.String("data", "", "settings directory (default ./data/settings)")
	flag.Parse()

	settingsDir := resolveDataDir(*dir)
	if _, err := os.Stat(settingsDir); err != nil {
		// stdout is the MCP transport — diagnostics must go to stderr.
		log.Fatalf("settings directory %q is not readable: %v", settingsDir, err)
	}

	store, err := storage.New(settingsDir)
	if err != nil {
		log.Fatalf("open storage at %q: %v", settingsDir, err)
	}

	src := whatifmcp.NewSource(settingsDir, store)
	if err := whatifmcp.NewServer(src).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("whatif-mcp: %v", err)
	}
}
