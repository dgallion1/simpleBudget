package mcpsvc

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect wires an assembled server to a client over the SDK's in-memory
// transport. *mcp.Server exposes no public lister, so enumerating what was
// registered requires a connected *mcp.ClientSession.
func connect(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}

func TestNewServerExposesTheAssumptionsResource(t *testing.T) {
	cs := connect(t, NewServer(Deps{}))

	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "whatif://assumptions"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("got %d contents, want 1", len(res.Contents))
	}
	if !strings.Contains(res.Contents[0].Text, "No mortality modeling") {
		t.Errorf("assumptions resource does not look like assumptions.md: %.80q", res.Contents[0].Text)
	}
}
