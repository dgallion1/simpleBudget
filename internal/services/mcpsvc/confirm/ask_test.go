package confirm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// session connects a real client and returns the server side of it. Whether
// the client declared the elicitation capability is what CanAsk reads, and
// only a real initialize handshake sets that.
func session(t *testing.T, elicits bool) *mcp.ServerSession {
	t.Helper()
	ctx := context.Background()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v0.0.0"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	var opts *mcp.ClientOptions
	if elicits {
		opts = &mcp.ClientOptions{
			ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
				return &mcp.ElicitResult{Action: "accept"}, nil
			},
		}
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, opts).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return ss
}

// CanAsk gates whether a guarded tool may return an approval request at all.
// Getting it wrong in the false-positive direction does not degrade the tool,
// it breaks it: the SDK fulfills input requests by calling the client, and
// that call fails outright on a client with no elicitation handler.
func TestCanAskFollowsTheClientCapability(t *testing.T) {
	if !CanAsk(session(t, true)) {
		t.Error("CanAsk = false for a client that declared elicitation")
	}
	if CanAsk(session(t, false)) {
		t.Error("CanAsk = true for a client that cannot prompt anyone")
	}
	if CanAsk(nil) {
		t.Error("CanAsk = true with no session at all")
	}
}

func TestApprovalRequestAsksForAnExplicitBoolean(t *testing.T) {
	req := ApprovalRequest("restore this backup", "everything would be overwritten")
	if req.Mode != "form" {
		t.Errorf("Mode = %q, want form", req.Mode)
	}
	if req.Message != "everything would be overwritten" {
		t.Errorf("Message = %q, want the consequences verbatim", req.Message)
	}
	// RequestedSchema is `any` on the wire type; assert the concrete schema so
	// a change of shape fails here rather than at a client.
	schema, ok := req.RequestedSchema.(*jsonschema.Schema)
	if !ok || schema == nil {
		t.Fatalf("RequestedSchema = %#v, want a *jsonschema.Schema -- a human has nothing to fill in otherwise",
			req.RequestedSchema)
	}
	if _, ok := schema.Properties[confirmField]; !ok {
		t.Fatalf("schema has no %q property: %+v", confirmField, schema.Properties)
	}
	if len(schema.Required) != 1 || schema.Required[0] != confirmField {
		t.Errorf("Required = %v, want [%s] -- an optional confirmation is not a confirmation",
			schema.Required, confirmField)
	}
}

func TestDecisionFromApprovesOnlyAnExplicitYes(t *testing.T) {
	if got := DecisionFrom(&mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}); got != Approved {
		t.Errorf("Decision = %v, want Approved", got)
	}
}

// Every one of these is a way of not saying yes, and a destructive operation
// needs a yes.
func TestDecisionFromRefusesEverythingElse(t *testing.T) {
	for _, tc := range []struct {
		name string
		resp mcp.InputResponse
	}{
		{"declined", &mcp.ElicitResult{Action: "decline"}},
		{"dismissed", &mcp.ElicitResult{Action: "cancel"}},
		// A user who read the prompt and left the box unticked.
		{"accepted with confirm=false", &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": false}}},
		// A client that auto-accepts empty forms has not asked anybody.
		{"accepted with no content", &mcp.ElicitResult{Action: "accept"}},
		// A response that is not an approval at all. CreateMessageResult is
		// deprecated but still functional for at least a year (SEP-2577), so
		// a confused client really can send one, and answering "approved" to
		// something that is not an approval would be the worst possible bug
		// here.
		//lint:ignore SA1019 deliberately exercising a wrong-but-still-live response type
		{"wrong response type", &mcp.CreateMessageResult{}},
		{"nothing at all", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecisionFrom(tc.resp); got != Refused {
				t.Errorf("Decision = %v, want Refused", got)
			}
		})
	}
}

// NotAsked must never render as something a reader could mistake for consent.
func TestDecisionStrings(t *testing.T) {
	for d, want := range map[Decision]string{
		Approved: "approved",
		Refused:  "refused",
		NotAsked: "not asked",
	} {
		if got := d.String(); got != want {
			t.Errorf("Decision(%d).String() = %q, want %q", d, got, want)
		}
	}
}

// Check must validate exactly what Redeem validates, and consume nothing --
// a guarded tool checks before asking a human and redeems after, so a Check
// that spent the token would make every approval round-trip fail on the way
// back.
func TestCheckValidatesWithoutConsuming(t *testing.T) {
	type args struct{ Name string }
	r := NewRegistry(time.Minute)
	tok, _, err := r.Mint("restore_backup", args{Name: "a.zip"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := r.Check(tok, "restore_backup", args{Name: "a.zip"}); err != nil {
			t.Fatalf("Check #%d: %v", i, err)
		}
	}
	if err := r.Check(tok, "shutdown_server", args{Name: "a.zip"}); !errors.Is(err, ErrBadToken) {
		t.Errorf("Check with the wrong tool = %v, want ErrBadToken", err)
	}
	if err := r.Check(tok, "restore_backup", args{Name: "b.zip"}); !errors.Is(err, ErrBadToken) {
		t.Errorf("Check with different args = %v, want ErrBadToken", err)
	}
	if err := r.Check("not-a-token", "restore_backup", args{Name: "a.zip"}); !errors.Is(err, ErrBadToken) {
		t.Errorf("Check with a bogus token = %v, want ErrBadToken", err)
	}

	// Still redeemable, exactly once.
	if err := r.Redeem(tok, "restore_backup", args{Name: "a.zip"}); err != nil {
		t.Fatalf("Redeem after Check: %v", err)
	}
	if err := r.Check(tok, "restore_backup", args{Name: "a.zip"}); !errors.Is(err, ErrBadToken) {
		t.Errorf("Check after Redeem = %v, want ErrBadToken", err)
	}
}

func TestCheckRefusesAnExpiredToken(t *testing.T) {
	type args struct{}
	r := NewRegistry(time.Minute)
	now := time.Now()
	r.now = func() time.Time { return now }
	tok, _, err := r.Mint("restore_backup", args{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	now = now.Add(2 * time.Minute)
	if err := r.Check(tok, "restore_backup", args{}); !errors.Is(err, ErrBadToken) {
		t.Errorf("Check on an expired token = %v, want ErrBadToken", err)
	}
}
