package whatifmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/storage"

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

// TestNewServerRegistersTheSixTools drives an in-memory client/server
// round trip (the SDK's mcp.NewInMemoryTransports) to actually enumerate
// what NewServer registered, rather than merely asserting non-nil. This is
// the SDK's supported way to inspect registrations: *mcp.Server exposes no
// public lister, but a connected *mcp.ClientSession does via ListTools and
// ListResources.
//
// live and snaps are passed as nil: none of the tools this test calls
// (list_scenarios, get_analysis) reach into them, and registration itself
// never invokes a tool handler.
func TestNewServerRegistersTheSixTools(t *testing.T) {
	ctx := context.Background()

	srv := NewServer(newTestSource(t), nil, nil)
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
	for _, want := range []string{
		"list_scenarios", "get_analysis", "get_months", "run_scenario", "open_page", "apply_changes",
	} {
		if !gotTools[want] {
			t.Errorf("tool %q not registered; got %v", want, toolNames(toolsRes.Tools))
		}
	}
	if len(toolsRes.Tools) != 6 {
		t.Errorf("expected exactly 6 tools, got %d: %v", len(toolsRes.Tools), toolNames(toolsRes.Tools))
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

// TestToolDescriptionsAreMeaningful drives the same in-memory round trip to
// check every registered tool has a non-empty description, and that
// apply_changes' description carries the write warning a model consuming
// these tools relies on to decide when it is safe to write to a real
// retirement plan.
func TestToolDescriptionsAreMeaningful(t *testing.T) {
	ctx := context.Background()

	srv := NewServer(newTestSource(t), nil, nil)
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
	descByName := map[string]string{}
	for _, tool := range toolsRes.Tools {
		descByName[tool.Name] = tool.Description
	}

	for _, name := range []string{
		"list_scenarios", "get_analysis", "get_months", "run_scenario", "open_page", "apply_changes",
	} {
		t.Run(name, func(t *testing.T) {
			desc, ok := descByName[name]
			if !ok {
				t.Fatalf("tool %q not registered", name)
			}
			if desc == "" {
				t.Fatalf("tool %q has an empty description", name)
			}
		})
	}

	if !strings.Contains(descByName["apply_changes"], "MODIFIES THE SAVED PLAN") {
		t.Errorf("apply_changes description must warn it writes the saved plan; got: %q", descByName["apply_changes"])
	}
}

// newLiveFixture writes a synthesized whatif.json into a fresh temp directory
// and opens a Source over it, for tests that also need the settings
// directory path to build a matching Client and Snapshotter.
func newLiveFixture(t *testing.T) (*Source, string) {
	t.Helper()
	dir := t.TempDir()
	b, err := json.Marshal(models.DefaultWhatIfSettings())
	if err != nil {
		t.Fatalf("marshal default settings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "whatif.json"), b, 0o644); err != nil {
		t.Fatalf("write whatif.json: %v", err)
	}
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return NewSource(dir, store), dir
}

// connectInMemory connects srv and a fresh client over
// mcp.NewInMemoryTransports and registers cleanup to close both sessions.
func connectInMemory(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession
}

// TestApplyChangesTool_SnapshotsBeforePOSTAndSucceeds drives apply_changes
// against a fake HTTP server that answers /whatif/state and /whatif/apply,
// and asserts a snapshot file lands on disk -- proving the snapshot actually
// happens, not just that Ensure would be reachable code.
func TestApplyChangesTool_SnapshotsBeforePOSTAndSucceeds(t *testing.T) {
	ctx := context.Background()
	src, dir := newLiveFixture(t)

	applyCount := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/whatif/state":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(State{App: "budget2", SettingsDir: dir, Active: "whatif.json", Revision: 1})
		case "/whatif/apply":
			applyCount++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ApplyResult{Scenario: "whatif.json", Applied: Overrides{}, Revision: 2})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	live := NewClient(fake.URL, dir)
	snapDir := t.TempDir()
	snaps := NewSnapshotter(dir, snapDir)

	clientSession := connectInMemory(t, NewServer(src, live, snaps))

	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "apply_changes",
		Arguments: applyChangesInput{},
	})
	if err != nil {
		t.Fatalf("CallTool(apply_changes): %v", err)
	}
	out := decodeToolResult[applyChangesOutput](t, res)

	if applyCount != 1 {
		t.Errorf("expected exactly one POST to /whatif/apply, got %d", applyCount)
	}
	if out.SnapshotPath == "" {
		t.Fatal("apply_changes did not report a snapshot path")
	}
	if _, err := os.Stat(out.SnapshotPath); err != nil {
		t.Errorf("reported snapshot path does not exist on disk: %v", err)
	}
	entries, err := os.ReadDir(snapDir)
	if err != nil {
		t.Fatalf("reading snapshot dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected exactly one snapshot file, got %d", len(entries))
	}
	if out.RevisionBefore != 1 || out.RevisionAfter != 2 {
		t.Errorf("revisions = before %d after %d, want 1 and 2", out.RevisionBefore, out.RevisionAfter)
	}
}

// TestApplyChangesTool_SnapshotFailureAbortsPOST points the snapshotter at a
// settings directory with no scenario file, so Ensure fails before any HTTP
// write happens, and asserts the fake server's /whatif/apply handler was
// never called -- a failed snapshot must abort the write, not merely be
// reported alongside it.
func TestApplyChangesTool_SnapshotFailureAbortsPOST(t *testing.T) {
	ctx := context.Background()

	// No whatif.json written here: Snapshotter.Ensure will fail to read it.
	dir := t.TempDir()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	src := NewSource(dir, store)

	applyCount := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/whatif/state":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(State{App: "budget2", SettingsDir: dir, Active: "whatif.json", Revision: 1})
		case "/whatif/apply":
			applyCount++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ApplyResult{Scenario: "whatif.json", Applied: Overrides{}, Revision: 2})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	live := NewClient(fake.URL, dir)
	snaps := NewSnapshotter(dir, t.TempDir())

	clientSession := connectInMemory(t, NewServer(src, live, snaps))

	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "apply_changes",
		Arguments: applyChangesInput{},
	})
	if err != nil {
		t.Fatalf("CallTool(apply_changes) returned a transport-level error, want a tool result with IsError set: %v", err)
	}
	if !res.IsError {
		t.Fatal("apply_changes should fail when the snapshot cannot be taken")
	}
	if applyCount != 0 {
		t.Errorf("snapshot failure must prevent the POST to /whatif/apply, got %d requests", applyCount)
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
// mcp.NewInMemoryTransports() pattern TestNewServerRegistersTheSixTools
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

// newTestSourceWithSocialSecurity builds a synthesized Source the same way
// newTestSource (scenarios_test.go) does, but with a Social Security config
// added. DefaultHooks' SS-income hook is a no-op when no Social Security is
// configured, so newTestSource's plain fixture would reconcile identically
// whether or not get_months is wired to the hooks — it would not actually
// catch the Critical-1 regression (get_months silently dropping
// retirement.DefaultHooks()). Adding a Social Security config makes the two
// tools' outputs provably diverge without the fix.
func newTestSourceWithSocialSecurity(t *testing.T) *Source {
	t.Helper()
	dir := t.TempDir()

	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 1_500_000
	settings.ProjectionYears = 3
	settings.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 2_500,
		FRA:        67,
		// ClaimAge == CurrentAge (65): SS income starts at month 0, so the
		// hooked and unhooked runs diverge from the very first month rather
		// than only after some future claim age — any yearN this test picks
		// is guaranteed to exercise the hook.
		ClaimAge:       65,
		SpouseClaimAge: 65,
	}
	b, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "whatif.json"), b, 0o644); err != nil {
		t.Fatalf("write whatif.json: %v", err)
	}

	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return NewSource(dir, store)
}

// decodeToolResult marshals a CallToolResult's StructuredContent back to
// JSON and unmarshals it into T. StructuredContent arrives client-side as a
// generically-decoded `any` (map[string]any for an object), not the
// server's original typed value or a json.RawMessage, so a direct type
// assertion to T is not available — round-tripping through json is the
// portable way to recover the typed value.
func decodeToolResult[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool call returned an error result: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal StructuredContent into %T: %v", out, err)
	}
	return out
}

// TestGetMonthsReconcilesWithGetAnalysis guards the Critical-1 seam: both
// get_analysis and get_months must run the projection through the same
// engine.Hooks (retirement.DefaultHooks()), or they silently disagree about
// the same plan — on the real active plan this made get_analysis report
// survival with a $13.96M final balance while get_months showed depletion
// at month 417. The invariant checked here: get_months' row for the last
// month of projection year N must equal get_analysis's years[N].ending_balance
// (both whole-dollar rounded), driven over the same in-memory MCP transport
// server_test.go's other tests use, against the synthesized (never real
// data/) fixture from newTestSourceWithSocialSecurity.
func TestGetMonthsReconcilesWithGetAnalysis(t *testing.T) {
	ctx := context.Background()

	srv := NewServer(newTestSourceWithSocialSecurity(t), nil, nil)
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

	analysisRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_analysis",
		Arguments: analysisInput{},
	})
	if err != nil {
		t.Fatalf("CallTool(get_analysis): %v", err)
	}
	aOut := decodeToolResult[analysisOutput](t, analysisRes)
	const yearN = 1
	if len(aOut.Analysis.Years) <= yearN {
		t.Fatalf("get_analysis returned %d years, need at least %d", len(aOut.Analysis.Years), yearN+1)
	}
	wantEnding := aOut.Analysis.Years[yearN].EndingBalance

	lastMonthOfYearN := yearN*12 + 11
	monthsRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_months",
		Arguments: monthsInput{FromMonth: lastMonthOfYearN, ToMonth: lastMonthOfYearN},
	})
	if err != nil {
		t.Fatalf("CallTool(get_months): %v", err)
	}
	mOut := decodeToolResult[monthsOutput](t, monthsRes)
	if len(mOut.Months) != 1 {
		t.Fatalf("get_months returned %d rows, want 1", len(mOut.Months))
	}
	gotBalance := mOut.Months[0].PortfolioBalance

	if gotBalance != wantEnding {
		t.Errorf("get_months month %d portfolio_balance = %v, want get_analysis years[%d].ending_balance = %v "+
			"(both tools must run the projection with the same hooks)", lastMonthOfYearN, gotBalance, yearN, wantEnding)
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
