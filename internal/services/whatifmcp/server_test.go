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

// TestRecoverToErrorConvertsPanic is a unit test of the helper in isolation:
// it proves recoverToError turns a panic into an error, but not that it is
// wired up correctly at the transport boundary.
func TestRecoverToErrorConvertsPanic(t *testing.T) {
	var err error
	func() {
		defer recoverToError("demo", &err)
		panic("boom")
	}()
	if err == nil || !strings.Contains(err.Error(), "demo panicked") {
		t.Errorf("err = %v, want a wrapped panic", err)
	}
}

// TestPanicInToolHandlerSurvivesRealTransport proves the placement, not just
// the helper: the go-sdk (v1.7.0, confirmed by reading
// internal/jsonrpc2/conn.go) dispatches every tool call on its own goroutine
// and contains no recover() of its own, so an unrecovered panic in a handler
// takes down the whole process — a deferred recover in main cannot catch it,
// because that recover only runs on the goroutine that panics, which here is
// one the SDK spawned, not main's.
//
// This registers a deliberately panicking tool on a real *mcp.Server, wraps
// it exactly the way run_scenario etc. are wrapped (named error return +
// `defer recoverToError`), connects a client over the same
// mcp.NewInMemoryTransports() pattern TestNewServerRegistersTheFourTools
// uses, and calls it. If recoverToError is missing or misplaced, this test
// process itself crashes instead of reporting a failure — that is the point:
// surviving to make an assertion at all is the proof.
func TestPanicInToolHandlerSurvivesRealTransport(t *testing.T) {
	ctx := context.Background()

	s := mcp.NewServer(&mcp.Implementation{Name: "panic-test", Version: "v0.0.0"}, nil)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "panics",
		Description: "always panics, for testing handler-level recovery",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (res *mcp.CallToolResult, out struct{}, err error) {
		defer recoverToError("panics", &err)
		panic("simulated engine panic")
	})

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := s.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	defer serverSession.Close()

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "panics"})
	if err != nil {
		t.Fatalf("CallTool(panics) returned a transport-level error, want a tool result with IsError set: %v", err)
	}
	if !result.IsError {
		t.Fatal("CallTool(panics) result.IsError = false, want true — the panic should surface as a tool error")
	}
	if len(result.Content) == 0 {
		t.Fatal("CallTool(panics) result has no content describing the error")
	}
	if text, ok := result.Content[0].(*mcp.TextContent); !ok || !strings.Contains(text.Text, "panics panicked") {
		t.Errorf("CallTool(panics) content = %#v, want text containing %q", result.Content, "panics panicked")
	}

	// The session and process are both still alive: prove the session can
	// still be used for an unrelated call after the panic.
	toolsRes, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools after panicking call: %v", err)
	}
	if len(toolsRes.Tools) != 1 {
		t.Errorf("session unusable after recovered panic: ListTools = %v", toolsRes.Tools)
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
