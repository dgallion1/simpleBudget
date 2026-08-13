// Package plan serves the what-if retirement planner over MCP. It reads and
// runs saved scenarios through the server's own settings manager, so its
// answers and the what-if page's answers come from one place.
package plan

import (
	_ "embed"
	"context"
	"fmt"

	"budget2/internal/services/retirement"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed assumptions.md
var assumptionsMD string

// Deps is what the planner tools need. The settings manager is the server's
// own instance, not a second one opened on the same directory: it owns the
// active-scenario selection, the settings cache, and the write lock.
type Deps struct {
	Settings *retirement.SettingsManager
	BaseURL  string
}

// recoverToError converts a panic into an error so a bad scenario fails one
// tool call instead of terminating the session. The go-sdk dispatches every
// tool call on its own goroutine with no recover of its own, so this must run
// via a defer inside each handler closure.
//
//lint:ignore U1000 unused until Task 2 wires the first tool handler that defers it
func recoverToError(tool string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panicked: %v", tool, r)
	}
}

// Register adds the planner tools and the assumptions resource to s.
func Register(s *mcp.Server, deps Deps) {
	s.AddResource(&mcp.Resource{
		URI:         "whatif://assumptions",
		Name:        "Engine assumptions and limitations",
		Description: "What the projection engine does and does not model. Read before drawing conclusions from any analysis.",
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      "whatif://assumptions",
				MIMEType: "text/markdown",
				Text:     assumptionsMD,
			}},
		}, nil
	})
}
