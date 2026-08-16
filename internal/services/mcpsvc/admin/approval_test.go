package admin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"budget2/internal/services/mcpsvc/confirm"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// approvalTTL is short so the nobody-answered test does not sit for minutes.
const approvalTTL = 400 * time.Millisecond

// connectViaBrowser connects a client that declares URL elicitation, i.e. one
// that can hand a person a link. The handler stands in for the whole
// out-of-band flow: it pulls the approval id out of the URL and answers it the
// way the /mcp/approve page does, which is the same call that page makes.
//
// answer == nil means nobody ever answers, which is what a person closing the
// tab looks like.
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
				// A form prompt reaching a URL-only client is the bug CanAsk
				// exists to prevent, so say so rather than quietly answering.
				t.Errorf("mode = %q, want url on a client that advertises only URL elicitation", req.Params.Mode)
				return &mcp.ElicitResult{Action: "decline"}, nil
			}
			if answer != nil && deps.Approvals != nil {
				id := url[strings.LastIndex(url, "/")+1:]
				if err := deps.Approvals.Decide(id, *answer); err != nil {
					t.Errorf("the page could not answer %s: %v", id, err)
				}
			}
			// The client only ever acknowledges that it showed the link. It
			// does NOT carry the person's answer -- that is the whole
			// difference between this and a form.
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

// browserDeps is restoreDeps plus the browser-approval wiring.
func browserDeps(t *testing.T) (Deps, string, string) {
	t.Helper()
	deps, dir, svc := restoreDeps(t)
	deps.Approvals = confirm.NewApprovals(approvalTTL)
	deps.BaseURL = "http://localhost:8080"
	return deps, dir, takeRealBackup(t, svc)
}

func decisionPtr(d confirm.Decision) *confirm.Decision { return &d }

func TestRestoreRunsWhenApprovedInTheBrowser(t *testing.T) {
	deps, dir, name := browserDeps(t)
	added := filepath.Join(dir, "added-after-the-backup.csv")
	if err := os.WriteFile(added, []byte("Date,Amount\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cs, seenURL := connectViaBrowser(t, deps, decisionPtr(confirm.Approved))

	first := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{"name": name}))
	out := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{
		"name":          name,
		"confirm_token": first.ConfirmToken,
	}))

	if !out.Confirmed {
		t.Fatalf("confirmed = false after the user approved in the browser (note: %q)", out.Note)
	}
	if out.HumanApproval != "approved" {
		t.Errorf("human_approval = %q, want approved", out.HumanApproval)
	}
	if _, err := os.Stat(added); !os.IsNotExist(err) {
		t.Errorf("the approved restore did not prune (stat err = %v)", err)
	}
	// The person has to be sent to this server's own page, not anywhere else.
	if u := seenURL.Load(); u == nil || !strings.HasPrefix(*u, "http://localhost:8080/mcp/approve/") {
		t.Errorf("approval URL = %v, want this server's approve page", u)
	}
}

// The point of the whole feature: someone reads the page, says no, and the
// data survives.
func TestRestoreDoesNotRunWhenDeclinedInTheBrowser(t *testing.T) {
	deps, dir, name := browserDeps(t)
	sentinel := filepath.Join(dir, "added-after-the-backup.csv")
	if err := os.WriteFile(sentinel, []byte("Date,Amount\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cs, _ := connectViaBrowser(t, deps, decisionPtr(confirm.Refused))

	first := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{"name": name}))
	out := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{
		"name":          name,
		"confirm_token": first.ConfirmToken,
	}))

	if out.Confirmed {
		t.Error("confirmed = true after the user declined in the browser")
	}
	if out.HumanApproval != "refused" {
		t.Errorf("human_approval = %q, want refused", out.HumanApproval)
	}
	if !strings.Contains(out.Note, "said NO") {
		t.Errorf("note = %q, want it to say the user declined", out.Note)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("the declined restore deleted data anyway: %v", err)
	}
}

// Nobody clicks. Silence is not consent, and the note must say that nobody
// answered rather than that they refused -- the model should offer to ask
// again, not report a decision the user never made.
func TestRestoreDoesNotRunWhenNobodyAnswers(t *testing.T) {
	deps, dir, name := browserDeps(t)
	sentinel := filepath.Join(dir, "added-after-the-backup.csv")
	if err := os.WriteFile(sentinel, []byte("Date,Amount\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cs, _ := connectViaBrowser(t, deps, nil)

	first := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{"name": name}))
	out := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{
		"name":          name,
		"confirm_token": first.ConfirmToken,
	}))

	if out.Confirmed {
		t.Error("confirmed = true when nobody answered")
	}
	if !strings.Contains(out.Note, "nobody answered") {
		t.Errorf("note = %q, want it to say nobody answered rather than that they said no", out.Note)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("an unanswered restore deleted data: %v", err)
	}
}

func TestShutdownDoesNotRunWhenDeclinedInTheBrowser(t *testing.T) {
	deps, calls := shutdownDeps(t)
	deps.Approvals = confirm.NewApprovals(approvalTTL)
	deps.BaseURL = "http://localhost:8080"
	cs, _ := connectViaBrowser(t, deps, decisionPtr(confirm.Refused))

	first := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{}))
	out := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{
		"confirm_token": first.ConfirmToken,
	}))

	if out.Confirmed {
		t.Error("confirmed = true after the user declined in the browser")
	}
	assertNoShutdown(t, calls, "after a browser refusal")
}

func TestShutdownRunsWhenApprovedInTheBrowser(t *testing.T) {
	deps, calls := shutdownDeps(t)
	deps.Approvals = confirm.NewApprovals(approvalTTL)
	deps.BaseURL = "http://localhost:8080"
	cs, _ := connectViaBrowser(t, deps, decisionPtr(confirm.Approved))

	first := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{}))
	out := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{
		"confirm_token": first.ConfirmToken,
	}))

	if !out.Confirmed {
		t.Fatalf("confirmed = false after a browser approval (note: %q)", out.Note)
	}
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("shutdown func called %d times after approval, want 1", got)
	}
}

// Without the registry or a base URL there is nowhere to send anyone, so the
// tools must fall to the next rung rather than failing.
func TestWithoutBrowserWiringTheToolsFallBack(t *testing.T) {
	deps, dir, name := browserDeps(t)
	deps.Approvals = nil
	deps.BaseURL = ""
	sentinel := filepath.Join(dir, "added-after-the-backup.csv")
	if err := os.WriteFile(sentinel, []byte("Date,Amount\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A client that could have shown a URL, but the server cannot build one.
	cs, seenURL := connectViaBrowser(t, deps, decisionPtr(confirm.Approved))

	first := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{"name": name}))
	out := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{
		"name":          name,
		"confirm_token": first.ConfirmToken,
	}))

	if seenURL.Load() != nil {
		t.Errorf("a URL was sent with no approvals registry configured: %v", *seenURL.Load())
	}
	// It still worked, by a lesser route.
	if !out.Confirmed {
		t.Fatalf("the restore did not run at all (note: %q)", out.Note)
	}
	if out.HumanApproval == "approved" {
		t.Error("human_approval = approved, but nobody was asked in a browser")
	}
}
