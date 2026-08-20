package ledger

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"budget2/internal/services/mcpsvc/confirm"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// R10 gap 1: internal/services/mcpsvc/admin has six end-to-end browser-
// approval tests driving a real in-memory MCP client/server through the full
// round trip; this package had none, even though set_balance_anchor and
// resolve_transfer are the two tools that write money data. These tests
// exercise that same round trip -- approval URL out, decision recorded out
// of band, re-invoked call consumes THAT decision -- for both.
//
// The harness below is deliberately the same shape as
// admin/approval_test.go's connectViaBrowser: a client that declares URL
// elicitation, whose handler pulls the approval id out of the URL and
// answers it the way the /mcp/approve page does.

// browserApprovalTTL is short so an unanswered request in a future test would
// not sit for minutes; unused today but kept alongside the harness so any
// added nobody-answered test does not need to invent its own.
const browserApprovalTTL = 400 * time.Millisecond

func decisionPtr(d confirm.Decision) *confirm.Decision { return &d }

// connectViaBrowser connects a client that can hand a person a link. The
// elicitation handler stands in for the whole out-of-band flow: it pulls the
// approval id out of the URL and answers it the way the /mcp/approve page
// does, which is the same call that page makes. answer == nil means nobody
// ever answers.
func connectViaBrowser(t *testing.T, deps Deps, answer *confirm.Decision) (*mcp.ClientSession, *atomic.Pointer[string]) {
	t.Helper()
	ctx := context.Background()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v0.0.0"}, nil)
	Register(srv, deps)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	var seenURL atomic.Pointer[string]
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, &mcp.ClientOptions{
		Capabilities: &mcp.ClientCapabilities{
			Elicitation: &mcp.ElicitationCapabilities{URL: &mcp.URLElicitationCapabilities{}},
		},
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			url := req.Params.URL
			seenURL.Store(&url)
			if req.Params.Mode != "url" {
				t.Errorf("mode = %q, want url on a client that advertises only URL elicitation", req.Params.Mode)
				return &mcp.ElicitResult{Action: "decline"}, nil
			}
			if answer != nil && deps.Approvals != nil {
				id := url[strings.LastIndex(url, "/")+1:]
				if err := deps.Approvals.Decide(id, *answer); err != nil {
					t.Errorf("the page could not answer %s: %v", id, err)
				}
			}
			// The client only ever acknowledges that it showed the link; it
			// does NOT carry the person's answer.
			return &mcp.ElicitResult{Action: "accept"}, nil
		},
	})
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession, &seenURL
}

// --- set_balance_anchor -----------------------------------------------------

// Criterion 1: the tool returns an approval URL, a human decision is
// recorded out of band (via connectViaBrowser's handler calling
// deps.Approvals.Decide, exactly as the /mcp/approve HTTP handler would),
// the re-invoked call consumes THAT decision, and the anchor written to disk
// is the amount the approval described.
func TestSetBalanceAnchorApprovedInBrowserWritesTheDescribedAmount(t *testing.T) {
	deps, _ := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
	}})
	deps.Approvals = confirm.NewApprovals(browserApprovalTTL)
	deps.BaseURL = "http://localhost:8080"
	cs, seenURL := connectViaBrowser(t, deps, decisionPtr(confirm.Approved))

	const wantAmount = 4210.55
	first := decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id": "checking",
		"date":       "2026-08-15",
		"amount":     wantAmount,
	}))
	out := decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id":    "checking",
		"date":          "2026-08-15",
		"amount":        wantAmount,
		"confirm_token": first.ConfirmToken,
	}))

	if !out.Confirmed {
		t.Fatalf("confirmed = false after a browser approval (note: %q)", out.Note)
	}
	if out.HumanApproval != "approved" {
		t.Errorf("human_approval = %q, want approved", out.HumanApproval)
	}
	if u := seenURL.Load(); u == nil || !strings.HasPrefix(*u, "http://localhost:8080/mcp/approve/") {
		t.Errorf("approval URL = %v, want this server's own approve page", u)
	}

	accts, err := accounts.Load(deps.Store)
	if err != nil {
		t.Fatalf("accounts.Load: %v", err)
	}
	var found bool
	for _, a := range accts {
		if a.ID != "checking" {
			continue
		}
		if len(a.Anchors) != 1 {
			t.Fatalf("anchors = %d, want 1", len(a.Anchors))
		}
		if a.Anchors[0].Amount != wantAmount {
			t.Errorf("written anchor amount = %.2f, want the amount the browser approval described (%.2f)", a.Anchors[0].Amount, wantAmount)
		}
		found = true
	}
	if !found {
		t.Fatal("checking account not found after the write")
	}
}

// Symmetric to the admin package's decline coverage: a human who opens the
// page and says no must leave the account exactly as it was.
func TestSetBalanceAnchorRefusedInBrowserWritesNothing(t *testing.T) {
	deps, _ := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
	}})
	deps.Approvals = confirm.NewApprovals(browserApprovalTTL)
	deps.BaseURL = "http://localhost:8080"
	cs, _ := connectViaBrowser(t, deps, decisionPtr(confirm.Refused))

	first := decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id": "checking",
		"date":       "2026-08-15",
		"amount":     4210.55,
	}))
	out := decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id":    "checking",
		"date":          "2026-08-15",
		"amount":        4210.55,
		"confirm_token": first.ConfirmToken,
	}))

	if out.Confirmed {
		t.Error("confirmed = true after the user declined in the browser")
	}
	if out.HumanApproval != "refused" {
		t.Errorf("human_approval = %q, want refused", out.HumanApproval)
	}

	accts, err := accounts.Load(deps.Store)
	if err != nil {
		t.Fatalf("accounts.Load: %v", err)
	}
	for _, a := range accts {
		if a.ID == "checking" && len(a.Anchors) != 0 {
			t.Errorf("an anchor was written after a browser refusal; anchors = %v", a.Anchors)
		}
	}
}

// --- resolve_transfer --------------------------------------------------------

// Criterion 1 (for resolve_transfer): the round trip, asserted on the
// recorded verdict.
func TestResolveTransferApprovedInBrowserRecordsTheDescribedVerdict(t *testing.T) {
	deps, dir := newDeps(t)
	writeCSV(t, dir, transferDecisionsFile, `{"decisions": {}}`)
	key := seedSuspectedPair(t, deps, dir)
	deps.Approvals = confirm.NewApprovals(browserApprovalTTL)
	deps.BaseURL = "http://localhost:8080"
	cs, seenURL := connectViaBrowser(t, deps, decisionPtr(confirm.Approved))

	first := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key": key,
		"verdict":  "confirm",
	}))
	out := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key":      key,
		"verdict":       "confirm",
		"confirm_token": first.ConfirmToken,
	}))

	if !out.Confirmed {
		t.Fatalf("confirmed = false after a browser approval (note: %q)", out.Note)
	}
	if out.Verdict != "confirm" {
		t.Errorf("verdict = %q, want confirm (the verdict the approval described)", out.Verdict)
	}
	if out.HumanApproval != "approved" {
		t.Errorf("human_approval = %q, want approved", out.HumanApproval)
	}
	if len(out.SnapshotPaths) == 0 {
		t.Error("no snapshot_paths; a .bak should have been taken for the pre-existing transfer_decisions.json")
	}
	if u := seenURL.Load(); u == nil || !strings.HasPrefix(*u, "http://localhost:8080/mcp/approve/") {
		t.Errorf("approval URL = %v, want this server's own approve page", u)
	}
}

// Criterion 2: a human answering NO in the browser must leave
// transfer_decisions.json byte-unchanged.
func TestResolveTransferRefusedInBrowserLeavesDecisionsFileByteUnchanged(t *testing.T) {
	deps, dir := newDeps(t)
	decisionsPath := filepath.Join(dir, transferDecisionsFile)
	const before = `{"decisions": {}}`
	writeCSV(t, dir, transferDecisionsFile, before)
	key := seedSuspectedPair(t, deps, dir)
	deps.Approvals = confirm.NewApprovals(browserApprovalTTL)
	deps.BaseURL = "http://localhost:8080"
	cs, _ := connectViaBrowser(t, deps, decisionPtr(confirm.Refused))

	first := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key": key,
		"verdict":  "confirm",
	}))
	out := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key":      key,
		"verdict":       "confirm",
		"confirm_token": first.ConfirmToken,
	}))

	if out.Confirmed {
		t.Error("confirmed = true after the user declined in the browser")
	}
	if out.HumanApproval != "refused" {
		t.Errorf("human_approval = %q, want refused", out.HumanApproval)
	}

	after, err := os.ReadFile(decisionsPath)
	if err != nil {
		t.Fatalf("read %s after the refused approval: %v", decisionsPath, err)
	}
	if string(after) != before {
		t.Errorf("transfer_decisions.json changed after a browser refusal: got %q, want unchanged %q", after, before)
	}
}
