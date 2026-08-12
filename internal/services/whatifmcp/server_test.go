package whatifmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// TestNewServerRegistersTheEightTools drives an in-memory client/server
// round trip (the SDK's mcp.NewInMemoryTransports) to actually enumerate
// what NewServer registered, rather than merely asserting non-nil. This is
// the SDK's supported way to inspect registrations: *mcp.Server exposes no
// public lister, but a connected *mcp.ClientSession does via ListTools and
// ListResources.
//
// live and snaps are passed as nil: none of the tools this test calls
// (list_scenarios, get_analysis) reach into them, and registration itself
// never invokes a tool handler.
func TestNewServerRegistersTheEightTools(t *testing.T) {
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
		"get_anomalies", "get_price_creep",
	} {
		if !gotTools[want] {
			t.Errorf("tool %q not registered; got %v", want, toolNames(toolsRes.Tools))
		}
	}
	if len(toolsRes.Tools) != 8 {
		t.Errorf("expected exactly 8 tools, got %d: %v", len(toolsRes.Tools), toolNames(toolsRes.Tools))
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
		"get_anomalies", "get_price_creep",
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

	var applyCount atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/whatif/state":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(State{App: "budget2", SettingsDir: dir, Active: "whatif.json", Revision: 1})
		case "/whatif/apply":
			applyCount.Add(1)
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

	if got := applyCount.Load(); got != 1 {
		t.Errorf("expected exactly one POST to /whatif/apply, got %d", got)
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

	var applyCount atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/whatif/state":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(State{App: "budget2", SettingsDir: dir, Active: "whatif.json", Revision: 1})
		case "/whatif/apply":
			applyCount.Add(1)
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
	if got := applyCount.Load(); got != 0 {
		t.Errorf("snapshot failure must prevent the POST to /whatif/apply, got %d requests", got)
	}
}

// TestApplyChangesTool_SwitchesScenarioBeforeSnapshotting exercises the
// switch branch (server.go's "in.Scenario != state.Active" path), which the
// two tests above never reach because both pass an empty Scenario. The fake
// /whatif/state answers "whatif.json" until the switch POST lands, then
// answers "whatif-b.json" -- the same shape the real handler has (in-process
// state that changes only once SwitchScenario's POST is processed). The
// property under test: the snapshot taken is of the POST-switch active
// scenario, not the one active when the call started, which is exactly what
// the re-fetch-state-after-switch step exists to guarantee. The fake
// /whatif/apply must answer with the post-switch scenario name too, or the
// result.Scenario != state.Active guard added for finding 1 would (correctly)
// reject this call.
func TestApplyChangesTool_SwitchesScenarioBeforeSnapshotting(t *testing.T) {
	ctx := context.Background()

	dir := t.TempDir()
	for _, name := range []string{"whatif.json", "whatif-b.json"} {
		b, err := json.Marshal(models.DefaultWhatIfSettings())
		if err != nil {
			t.Fatalf("marshal default settings: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	src := NewSource(dir, store)

	var switched atomic.Bool
	var applyCount atomic.Int32
	var switchCount atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/whatif/state":
			active := "whatif.json"
			if switched.Load() {
				active = "whatif-b.json"
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(State{App: "budget2", SettingsDir: dir, Active: active, Revision: 1})
		case r.URL.Path == "/whatif/scenarios/switch" && r.Method == http.MethodPost:
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if got := r.FormValue("filename"); got != "whatif-b.json" {
				http.Error(w, "unexpected filename "+got, http.StatusBadRequest)
				return
			}
			switchCount.Add(1)
			switched.Store(true)
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/whatif/apply":
			applyCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ApplyResult{Scenario: "whatif-b.json", Applied: Overrides{}, Revision: 2})
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
		Arguments: applyChangesInput{Scenario: "whatif-b.json"},
	})
	if err != nil {
		t.Fatalf("CallTool(apply_changes): %v", err)
	}
	out := decodeToolResult[applyChangesOutput](t, res)

	if got := switchCount.Load(); got != 1 {
		t.Errorf("expected exactly one POST to /whatif/scenarios/switch, got %d", got)
	}
	if got := applyCount.Load(); got != 1 {
		t.Errorf("expected exactly one POST to /whatif/apply, got %d", got)
	}
	base := filepath.Base(out.SnapshotPath)
	if !strings.HasPrefix(base, "whatif-b.json.") {
		t.Errorf("snapshot_path basename = %q, want it to start with %q (the snapshot must follow the switch, "+
			"not cover the plan that was active before it)", base, "whatif-b.json.")
	}
	if out.Scenario != "whatif-b.json" {
		t.Errorf("reported scenario = %q, want %q", out.Scenario, "whatif-b.json")
	}
}

// toolErrorText returns the text of an errored tool result, failing the test
// if the call did not error.
func toolErrorText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if !res.IsError {
		t.Fatalf("expected an error result, got: %+v", res.StructuredContent)
	}
	if len(res.Content) == 0 {
		t.Fatal("error result has no content describing the failure")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("error content = %#v, want text", res.Content[0])
	}
	return text.Text
}

// TestApplyChangesTool_ReportsWriteToADifferentScenarioThanSnapshotted covers
// the post-hoc collision guard — the branch every other apply_changes test
// avoids by making the fake answer with the scenario it reported active.
//
// It is the last line of defence for this tool's one unrecoverable failure: a
// write that lands on a plan the snapshot does not cover cannot be undone, so
// the tool must report it loudly instead of returning success. The fake here
// plays a server that answers /whatif/state with whatif.json and then writes
// whatif-b.json anyway — i.e. one that ignores the expected_scenario field the
// call sends.
func TestApplyChangesTool_ReportsWriteToADifferentScenarioThanSnapshotted(t *testing.T) {
	ctx := context.Background()
	src, dir := newLiveFixture(t)

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/whatif/state":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(State{App: "budget2", SettingsDir: dir, Active: "whatif.json", Revision: 1})
		case "/whatif/apply":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ApplyResult{Scenario: "whatif-b.json", Applied: Overrides{}, Revision: 2})
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
	msg := toolErrorText(t, res)

	// Both filenames, or the user cannot tell which plan took the write and
	// which one the recoverable copy covers.
	if !strings.Contains(msg, "whatif-b.json") {
		t.Errorf("error must name the scenario that was written (whatif-b.json), got: %s", msg)
	}
	if !strings.Contains(msg, "whatif.json") {
		t.Errorf("error must name the snapshotted scenario (whatif.json), got: %s", msg)
	}
	// And the snapshot path, which is the only thing a hand recovery can use.
	if !strings.Contains(msg, "whatif.json.") {
		t.Errorf("error must name the snapshot path taken for this call, got: %s", msg)
	}
}

// TestApplyChangesTool_SendsExpectedScenarioAndSurfacesA409 covers the guard
// that runs one layer earlier: apply_changes tells the server which scenario
// it snapshotted, and a server whose active scenario has changed refuses with
// 409 *without writing*. Detection after the fact cannot undo a write; this is
// the path where the collision is prevented instead.
func TestApplyChangesTool_SendsExpectedScenarioAndSurfacesA409(t *testing.T) {
	ctx := context.Background()
	src, dir := newLiveFixture(t)

	gotExpected := make(chan string, 1)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/whatif/state":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(State{App: "budget2", SettingsDir: dir, Active: "whatif.json", Revision: 1})
		case "/whatif/apply":
			var body struct {
				ExpectedScenario string `json:"expected_scenario"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			gotExpected <- body.ExpectedScenario
			// The browser switched to whatif-b.json between the state read
			// and this POST: the real handler answers 409 and writes nothing.
			http.Error(w, "refusing to write: the active scenario is whatif-b.json, but this change was "+
				"prepared for "+body.ExpectedScenario+" (the active scenario changed between the two). "+
				"Nothing was written", http.StatusConflict)
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
	msg := toolErrorText(t, res)

	select {
	case got := <-gotExpected:
		if got != "whatif.json" {
			t.Errorf(`POST /whatif/apply carried expected_scenario = %q, want %q — without it the server cannot refuse the write`, got, "whatif.json")
		}
	default:
		t.Fatal("the fake server's /whatif/apply handler was never invoked")
	}

	if !strings.Contains(msg, "409") && !strings.Contains(msg, "Conflict") {
		t.Errorf("error should surface the 409 refusal, got: %s", msg)
	}
	if !strings.Contains(msg, "Nothing was written") {
		t.Errorf("error should carry the server's explanation verbatim, got: %s", msg)
	}
}

// TestOpenPageTool_ReturnsURLAndPostSwitchState exercises open_page's actual
// handler, not just its registration: it drives a real tool call over the
// in-memory transport against a fake HTTP server, requesting a scenario
// switch, and asserts both the returned URL and that Active reflects the
// state *after* the switch (i.e. open_page re-fetched state rather than
// returning the pre-switch snapshot it got from EnsureServer).
func TestOpenPageTool_ReturnsURLAndPostSwitchState(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	var switched atomic.Bool
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/whatif/state":
			active := "whatif.json"
			if switched.Load() {
				active = "whatif-b.json"
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(State{App: "budget2", SettingsDir: dir, Active: active, Revision: 5})
		case r.URL.Path == "/whatif/scenarios/switch" && r.Method == http.MethodPost:
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if got := r.FormValue("filename"); got != "whatif-b.json" {
				http.Error(w, "unexpected filename "+got, http.StatusBadRequest)
				return
			}
			switched.Store(true)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	live := NewClient(fake.URL, dir)
	clientSession := connectInMemory(t, NewServer(newTestSource(t), live, nil))

	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "open_page",
		Arguments: openPageInput{Scenario: "whatif-b.json"},
	})
	if err != nil {
		t.Fatalf("CallTool(open_page): %v", err)
	}
	out := decodeToolResult[openPageOutput](t, res)

	if want := fake.URL + "/whatif"; out.URL != want {
		t.Errorf("URL = %q, want %q", out.URL, want)
	}
	if out.Active != "whatif-b.json" {
		t.Errorf("Active = %q, want %q (open_page must re-fetch state after switching, not return the pre-switch value)",
			out.Active, "whatif-b.json")
	}
	if out.Started {
		t.Error("Started = true, want false: the fake server was already reachable")
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
