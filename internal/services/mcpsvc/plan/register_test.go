package plan

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestManager writes one scenario file and returns a settings manager
// rooted at it, mirroring how cmd/server constructs the real one.
func newTestManager(t *testing.T) *retirement.SettingsManager {
	t.Helper()
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := map[string]any{
		"name":             "Base",
		"portfolio_value":  1000000.0,
		"projection_years": 30,
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "whatif.json"), data, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return retirement.NewSettingsManager(settingsDir, store)
}

func connect(t *testing.T, deps Deps) *mcp.ClientSession {
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

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}

func TestListScenariosReadsTheManagersScenarios(t *testing.T) {
	cs := connect(t, Deps{Settings: newTestManager(t)})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_scenarios",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_scenarios returned an error: %+v", res.Content)
	}

	var out listOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if len(out.Scenarios) != 1 {
		t.Fatalf("got %d scenarios, want 1: %+v", len(out.Scenarios), out.Scenarios)
	}
	if out.Scenarios[0].Filename != "whatif.json" {
		t.Errorf("filename = %q, want whatif.json", out.Scenarios[0].Filename)
	}
	if out.Scenarios[0].PortfolioValue != 1000000 {
		t.Errorf("portfolio_value = %v, want 1000000", out.Scenarios[0].PortfolioValue)
	}
}

func TestOpenPageReturnsTheWhatIfURLAndActiveScenario(t *testing.T) {
	cs := connect(t, Deps{Settings: newTestManager(t), BaseURL: "http://localhost:8080"})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "open_page",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("open_page returned an error: %+v", res.Content)
	}

	var out openPageOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.URL != "http://localhost:8080/whatif" {
		t.Errorf("url = %q, want http://localhost:8080/whatif", out.URL)
	}
	if out.Active != "whatif.json" {
		t.Errorf("active = %q, want whatif.json", out.Active)
	}
}

func TestOpenPageSwitchesToTheRequestedScenario(t *testing.T) {
	sm := newTestManager(t)
	if _, err := sm.CreateScenario("Later Retirement"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	// CreateScenario switches to the new file; switch back so the test's
	// request is a real change rather than a no-op.
	if err := sm.SwitchScenario("whatif.json"); err != nil {
		t.Fatalf("SwitchScenario: %v", err)
	}

	scenarios, err := sm.ListScenarios()
	if err != nil {
		t.Fatalf("ListScenarios: %v", err)
	}
	var target string
	for _, sc := range scenarios {
		if sc.Filename != "whatif.json" {
			target = sc.Filename
		}
	}
	if target == "" {
		t.Fatal("no second scenario was created")
	}

	cs := connect(t, Deps{Settings: sm, BaseURL: "http://localhost:8080"})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "open_page",
		Arguments: map[string]any{"scenario": target},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("open_page returned an error: %+v", res.Content)
	}

	var out openPageOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Active != target {
		t.Errorf("active = %q, want %q", out.Active, target)
	}
	if got := sm.ActiveFilename(); got != target {
		t.Errorf("manager active = %q, want %q", got, target)
	}
}

func TestApplyChangesWritesSnapshotsAndReportsBothRevisions(t *testing.T) {
	sm := newTestManager(t)
	snapDir := t.TempDir()
	deps := Deps{
		Settings:  sm,
		Snapshots: snapshot.New(sm.SettingsDir(), snapDir),
		BaseURL:   "http://localhost:8080",
	}
	before := sm.Revision()

	cs := connect(t, deps)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "apply_changes",
		Arguments: map[string]any{
			"overrides": map[string]any{"projection_years": 35},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("apply_changes returned an error: %+v", res.Content)
	}

	var out applyChangesOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Scenario != "whatif.json" {
		t.Errorf("scenario = %q, want whatif.json", out.Scenario)
	}
	if out.RevisionBefore != before {
		t.Errorf("revision_before = %d, want %d", out.RevisionBefore, before)
	}
	if out.RevisionAfter <= out.RevisionBefore {
		t.Errorf("revision_after = %d, want greater than %d", out.RevisionAfter, out.RevisionBefore)
	}
	if _, err := os.Stat(out.SnapshotPath); err != nil {
		t.Errorf("snapshot %q not written: %v", out.SnapshotPath, err)
	}

	saved, err := sm.LoadScenarioSettings("whatif.json")
	if err != nil {
		t.Fatalf("LoadScenarioSettings: %v", err)
	}
	if saved.ProjectionYears != 35 {
		t.Errorf("projection_years = %d, want 35 — the override was not persisted", saved.ProjectionYears)
	}
}

// A failed snapshot must abort the write. Pointing the snapshot directory at
// a path that cannot be created is the cheapest way to force that failure.
func TestApplyChangesDoesNotWriteWhenTheSnapshotFails(t *testing.T) {
	sm := newTestManager(t)
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	deps := Deps{
		Settings:  sm,
		Snapshots: snapshot.New(sm.SettingsDir(), filepath.Join(blocker, "snapshots")),
	}

	cs := connect(t, deps)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "apply_changes",
		Arguments: map[string]any{
			"overrides": map[string]any{"projection_years": 35},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("apply_changes should have failed when the snapshot could not be written")
	}

	saved, err := sm.LoadScenarioSettings("whatif.json")
	if err != nil {
		t.Fatalf("LoadScenarioSettings: %v", err)
	}
	if saved.ProjectionYears == 35 {
		t.Error("the scenario was written despite the snapshot failing")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	return b
}

// newManagerWithSocialSecurity builds a settings manager over a synthesized
// whatif.json with a Social Security config added, mirroring the deleted
// whatifmcp/server_test.go's newTestSourceWithSocialSecurity. DefaultHooks'
// SS-income hook is a no-op when no Social Security is configured, so a plain
// newTestManager fixture would reconcile identically whether or not
// get_months is wired to the hooks -- it would not actually catch the
// Critical-1 regression (get_months silently dropping
// retirement.DefaultHooks()). Adding a Social Security config makes the two
// tools' outputs provably diverge without the fix.
func newManagerWithSocialSecurity(t *testing.T) *retirement.SettingsManager {
	t.Helper()
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 1_500_000
	settings.ProjectionYears = 3
	settings.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 2_500,
		FRA:        67,
		// ClaimAge == CurrentAge (65): SS income starts at month 0, so the
		// hooked and unhooked runs diverge from the very first month rather
		// than only after some future claim age -- any yearN this test picks
		// is guaranteed to exercise the hook.
		ClaimAge:       65,
		SpouseClaimAge: 65,
	}
	b, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "whatif.json"), b, 0o644); err != nil {
		t.Fatalf("write whatif.json: %v", err)
	}

	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return retirement.NewSettingsManager(settingsDir, store)
}

// TestGetMonthsReconcilesWithGetAnalysis guards the Critical-1 seam: both
// get_analysis and get_months must run the projection through the same
// engine.Hooks (retirement.DefaultHooks()), or they silently disagree about
// the same plan -- on the real active plan this made get_analysis report
// survival with a $13.96M final balance while get_months showed depletion at
// month 417. The invariant checked here: get_months' row for the last month
// of projection year N must equal get_analysis's years[N].ending_balance
// (both whole-dollar rounded). Ported from the deleted
// whatifmcp/server_test.go.
func TestGetMonthsReconcilesWithGetAnalysis(t *testing.T) {
	cs := connect(t, Deps{Settings: newManagerWithSocialSecurity(t)})
	ctx := context.Background()

	analysisRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_analysis",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool(get_analysis): %v", err)
	}
	if analysisRes.IsError {
		t.Fatalf("get_analysis returned an error: %+v", analysisRes.Content)
	}
	var aOut analysisOutput
	if err := json.Unmarshal(mustJSON(t, analysisRes.StructuredContent), &aOut); err != nil {
		t.Fatalf("decode get_analysis structured content: %v", err)
	}
	const yearN = 1
	if len(aOut.Analysis.Years) <= yearN {
		t.Fatalf("get_analysis returned %d years, need at least %d", len(aOut.Analysis.Years), yearN+1)
	}
	wantEnding := aOut.Analysis.Years[yearN].EndingBalance

	lastMonthOfYearN := yearN*12 + 11
	monthsRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_months",
		Arguments: map[string]any{
			"from_month": lastMonthOfYearN,
			"to_month":   lastMonthOfYearN,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(get_months): %v", err)
	}
	if monthsRes.IsError {
		t.Fatalf("get_months returned an error: %+v", monthsRes.Content)
	}
	var mOut monthsOutput
	if err := json.Unmarshal(mustJSON(t, monthsRes.StructuredContent), &mOut); err != nil {
		t.Fatalf("decode get_months structured content: %v", err)
	}
	if len(mOut.Months) != 1 {
		t.Fatalf("get_months returned %d rows, want 1", len(mOut.Months))
	}
	gotBalance := mOut.Months[0].PortfolioBalance

	if gotBalance != wantEnding {
		t.Errorf("get_months month %d portfolio_balance = %v, want get_analysis years[%d].ending_balance = %v "+
			"(both tools must run the projection with the same hooks)", lastMonthOfYearN, gotBalance, yearN, wantEnding)
	}
}

// TestRunScenarioAppliesOverridesWithoutWritingToTheSavedPlan covers
// run_scenario, which (like get_analysis and get_months before this fix wave)
// was previously invoked by no test at all: it must run the override through
// the engine and return the resulting analysis without touching the saved
// scenario file, and must always report monte_carlo_omitted so a caller does
// not compare a stochastic figure across two calls.
func TestRunScenarioAppliesOverridesWithoutWritingToTheSavedPlan(t *testing.T) {
	sm := newTestManager(t)
	cs := connect(t, Deps{Settings: sm})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "run_scenario",
		Arguments: map[string]any{
			"overrides": map[string]any{"projection_years": 40},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("run_scenario returned an error: %+v", res.Content)
	}

	var out runOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Scenario != "whatif.json" {
		t.Errorf("scenario = %q, want whatif.json", out.Scenario)
	}
	if !out.MonteCarloOmitted {
		t.Error("monte_carlo_omitted = false, want true -- run_scenario's result is deterministic-only")
	}
	if len(out.Analysis.Years) == 0 {
		t.Fatal("run_scenario returned no projection years")
	}

	saved, err := sm.LoadScenarioSettings("whatif.json")
	if err != nil {
		t.Fatalf("LoadScenarioSettings: %v", err)
	}
	if saved.ProjectionYears == 40 {
		t.Error("run_scenario must not persist the override to the saved plan")
	}
}

// TestRecoverToErrorConvertsPanic is a unit test of the helper in isolation:
// it proves recoverToError turns a panic into an error, but not that it is
// wired up correctly at the transport boundary (see
// TestPanicInToolHandlerSurvivesRealTransport). Ported from the deleted
// whatifmcp/server_test.go.
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
// the helper: the go-sdk dispatches every tool call on its own goroutine and
// contains no recover() of its own, so an unrecovered panic in a handler
// takes down the whole process -- and now that MCP is served in-process by
// cmd/server rather than by a standalone whatif-mcp binary, "the whole
// process" means the user's web server, not a side process. This registers a
// deliberately panicking tool on a real *mcp.Server, wraps it exactly the way
// every tool in this package is wrapped (named error return + `defer
// recoverToError`), connects a client over the in-memory transport, and calls
// it. If recoverToError is missing or misplaced, this test process itself
// crashes instead of reporting a failure -- that is the point: surviving to
// make an assertion at all is the proof. Ported from the deleted
// whatifmcp/server_test.go.
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
		t.Fatal("CallTool(panics) result.IsError = false, want true -- the panic should surface as a tool error")
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

// TestToolDescriptionsAreMeaningful drives an in-memory round trip to check
// every registered planner tool has a non-empty description, and that
// apply_changes' description carries the write warning a model consuming
// these tools relies on to decide when it is safe to write to a real
// retirement plan. Ported from the deleted whatifmcp/server_test.go; the
// tool list is narrowed to plan's own six -- get_anomalies and
// get_price_creep now live in mcpsvc/spend and are covered by
// mcpsvc.TestNewServerRegistersAllTwelveTools instead.
func TestToolDescriptionsAreMeaningful(t *testing.T) {
	cs := connect(t, Deps{Settings: newTestManager(t)})

	toolsRes, err := cs.ListTools(context.Background(), nil)
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

// secondScenarioFilename creates a second scenario ("Later Retirement") on
// sm, switches back to whatif.json (CreateScenario switches to the new file),
// and returns the new scenario's filename.
func secondScenarioFilename(t *testing.T, sm *retirement.SettingsManager) string {
	t.Helper()
	if _, err := sm.CreateScenario("Later Retirement"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	if err := sm.SwitchScenario("whatif.json"); err != nil {
		t.Fatalf("SwitchScenario: %v", err)
	}
	scenarios, err := sm.ListScenarios()
	if err != nil {
		t.Fatalf("ListScenarios: %v", err)
	}
	for _, sc := range scenarios {
		if sc.Filename != "whatif.json" {
			return sc.Filename
		}
	}
	t.Fatal("no second scenario was created")
	return ""
}

// TestApplyChangesSwitchesToNamedScenarioBeforeWriting covers apply_changes'
// in.Scenario != "" branch (previously exercised by no test): naming a
// scenario other than the active one must switch to it, snapshot it (not the
// scenario that was active before the call), and write the override there.
func TestApplyChangesSwitchesToNamedScenarioBeforeWriting(t *testing.T) {
	sm := newTestManager(t)
	target := secondScenarioFilename(t, sm)

	deps := Deps{Settings: sm, Snapshots: snapshot.New(sm.SettingsDir(), t.TempDir()), BaseURL: "http://localhost:8080"}
	cs := connect(t, deps)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "apply_changes",
		Arguments: map[string]any{
			"scenario":  target,
			"overrides": map[string]any{"projection_years": 40},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("apply_changes returned an error: %+v", res.Content)
	}

	var out applyChangesOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Scenario != target {
		t.Errorf("scenario = %q, want %q", out.Scenario, target)
	}
	if got := sm.ActiveFilename(); got != target {
		t.Errorf("manager active = %q, want %q", got, target)
	}
	if base := filepath.Base(out.SnapshotPath); !strings.HasPrefix(base, target+".") {
		t.Errorf("snapshot_path basename = %q, want it to start with %q (the snapshot must cover the "+
			"switched-to scenario, not the one active before the call)", base, target+".")
	}

	saved, err := sm.LoadScenarioSettings(target)
	if err != nil {
		t.Fatalf("LoadScenarioSettings(%s): %v", target, err)
	}
	if saved.ProjectionYears != 40 {
		t.Errorf("%s projection_years = %d, want 40 -- the override was not written to the switched-to scenario",
			target, saved.ProjectionYears)
	}

	base, err := sm.LoadScenarioSettings("whatif.json")
	if err != nil {
		t.Fatalf("LoadScenarioSettings(whatif.json): %v", err)
	}
	if base.ProjectionYears == 40 {
		t.Error("the override landed on whatif.json instead of the requested scenario")
	}
}

// TestApplyChangesReportsScenarioConflictAsToolError proves a
// *retirement.ScenarioConflictError from ApplyOverrides surfaces as a tool
// error rather than a transport-level failure or a silent success.
//
// It drives the manager's own self-heal path deterministically instead of a
// timing-dependent goroutine race: reconcileActiveScenarioLocked (inside
// ApplyOverrides' loadInternal) reverts the active scenario to whatif.json
// when the active scenario's file has vanished from disk, after a fixed
// ~100ms confirmation delay. The first apply_changes call primes the
// Snapshotter's per-scenario cache for target (a real, once-only disk read),
// so the second call's Snapshots.Ensure(target, ...) is satisfied from cache
// and never notices the file is gone; the write then proceeds into
// ApplyOverrides, whose loadInternal reconciles the missing file and reverts
// sm.filename to whatif.json, tripping the expectedScenario-vs-sm.filename
// mismatch this call's "scenario" (target) was pinned to.
func TestApplyChangesReportsScenarioConflictAsToolError(t *testing.T) {
	sm := newTestManager(t)
	target := secondScenarioFilename(t, sm)
	if err := sm.SwitchScenario(target); err != nil {
		t.Fatalf("SwitchScenario(%s): %v", target, err)
	}

	deps := Deps{Settings: sm, Snapshots: snapshot.New(sm.SettingsDir(), t.TempDir())}
	cs := connect(t, deps)
	ctx := context.Background()

	priming, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "apply_changes",
		Arguments: map[string]any{"overrides": map[string]any{"projection_years": 31}},
	})
	if err != nil {
		t.Fatalf("CallTool (priming): %v", err)
	}
	if priming.IsError {
		t.Fatalf("priming apply_changes call failed: %+v", priming.Content)
	}

	if err := os.Remove(filepath.Join(sm.SettingsDir(), target)); err != nil {
		t.Fatalf("removing %s to simulate it vanishing: %v", target, err)
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "apply_changes",
		Arguments: map[string]any{"overrides": map[string]any{"projection_years": 32}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("apply_changes should report an error when the active scenario vanishes and " +
			"ApplyOverrides self-heals to a different scenario than this call was pinned to")
	}
	if len(res.Content) == 0 {
		t.Fatal("error result has no content describing the failure")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("error content = %#v, want text", res.Content[0])
	}
	if !strings.Contains(text.Text, "Nothing was written") {
		t.Errorf("error should surface ApplyOverrides' ScenarioConflictError message, got: %s", text.Text)
	}
}

// TestOpenPageNoOpsWhenScenarioAlreadyActive covers the branch where the
// caller names a scenario that is already active: it must not call
// SwitchScenario (which would bump the revision even though nothing actually
// changed). Also asserts on Revision, which no existing open_page test
// checked.
func TestOpenPageNoOpsWhenScenarioAlreadyActive(t *testing.T) {
	sm := newTestManager(t)
	revBefore := sm.Revision()

	cs := connect(t, Deps{Settings: sm, BaseURL: "http://localhost:8080"})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "open_page",
		Arguments: map[string]any{"scenario": "whatif.json"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("open_page returned an error: %+v", res.Content)
	}

	var out openPageOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Active != "whatif.json" {
		t.Errorf("active = %q, want whatif.json", out.Active)
	}
	if out.Revision != revBefore {
		t.Errorf("revision = %d, want %d -- naming the already-active scenario must be a no-op "+
			"(no SwitchScenario call, so no revision bump)", out.Revision, revBefore)
	}
	if got := sm.Revision(); got != revBefore {
		t.Errorf("manager revision = %d, want unchanged %d", got, revBefore)
	}
}
