package whatifmcp

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAssumptionsResourceIsEmbeddedAndNonEmpty(t *testing.T) {
	if len(assumptionsMD) == 0 {
		t.Fatal("assumptions.md was not embedded")
	}
	for _, want := range []string{"No mortality modeling", "one household pool", "Monte Carlo is stochastic"} {
		if !strings.Contains(assumptionsMD, want) {
			t.Errorf("assumptions resource missing %q", want)
		}
	}
}

// TestNewServerRegistersTheFourTools drives an in-memory client/server
// round trip (the SDK's mcp.NewInMemoryTransports) to actually enumerate
// what NewServer registered, rather than merely asserting non-nil. This is
// the SDK's supported way to inspect registrations: *mcp.Server exposes no
// public lister, but a connected *mcp.ClientSession does via ListTools and
// ListResources.
func TestNewServerRegistersTheFourTools(t *testing.T) {
	ctx := context.Background()

	srv := NewServer(newTestSource(t))
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	defer serverSession.Close()

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer clientSession.Close()

	toolsRes, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	gotTools := map[string]bool{}
	for _, tool := range toolsRes.Tools {
		gotTools[tool.Name] = true
	}
	for _, want := range []string{"list_scenarios", "get_analysis", "get_months", "run_scenario"} {
		if !gotTools[want] {
			t.Errorf("tool %q not registered; got %v", want, toolNames(toolsRes.Tools))
		}
	}
	if len(toolsRes.Tools) != 4 {
		t.Errorf("expected exactly 4 tools, got %d: %v", len(toolsRes.Tools), toolNames(toolsRes.Tools))
	}

	resourcesRes, err := clientSession.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	foundAssumptions := false
	for _, r := range resourcesRes.Resources {
		if r.URI == "whatif://assumptions" {
			foundAssumptions = true
		}
	}
	if !foundAssumptions {
		t.Errorf("resource whatif://assumptions not registered; got %v", resourceURIs(resourcesRes.Resources))
	}

	// Listing only proves the resource is advertised. Actually read it, the
	// way a real client would, to prove the handler serves the real document
	// rather than something empty or stale.
	readRes, err := clientSession.ReadResource(ctx, &mcp.ReadResourceParams{URI: "whatif://assumptions"})
	if err != nil {
		t.Fatalf("ReadResource(whatif://assumptions): %v", err)
	}
	if len(readRes.Contents) == 0 {
		t.Fatal("ReadResource(whatif://assumptions) returned no contents")
	}
	served := readRes.Contents[0].Text
	if served == "" {
		t.Fatal("ReadResource(whatif://assumptions) returned empty text")
	}
	const distinctive = "Joint and Last Survivor Table"
	if !strings.Contains(served, distinctive) {
		t.Errorf("served assumptions resource missing %q; got:\n%s", distinctive, served)
	}
}

func toolNames(tools []*mcp.Tool) []string {
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}
	return names
}

func resourceURIs(resources []*mcp.Resource) []string {
	uris := make([]string, len(resources))
	for i, r := range resources {
		uris[i] = r.URI
	}
	return uris
}
