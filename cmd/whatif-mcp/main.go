// Command whatif-mcp serves the what-if retirement planner over MCP on stdio,
// so a plan can be discussed in Claude Code. It reads saved scenarios and runs
// the projection engine; it never writes to the data directory and makes no
// network calls.
package main

import (
	"context"
	"flag"
	"fmt"
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

// checkSettingsDir verifies the directory exists AND is readable. os.Stat is
// insufficient: stat(2) only needs search permission on the parents, so a
// directory with no read permission of its own still stats successfully —
// the failure would then surface per-call instead of at startup.
func checkSettingsDir(dir string) error {
	if _, err := os.ReadDir(dir); err != nil {
		return fmt.Errorf("settings directory %q is not readable: %w", dir, err)
	}
	return nil
}

func main() {
	dir := flag.String("data", "", "settings directory (default ./data/settings)")
	flag.Parse()

	settingsDir := resolveDataDir(*dir)
	if err := checkSettingsDir(settingsDir); err != nil {
		// stdout is the MCP transport — diagnostics must go to stderr.
		log.Fatal(err)
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
