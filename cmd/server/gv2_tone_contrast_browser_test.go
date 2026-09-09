package main

// GV2 attempt 2 (ruling GV-2026-09-09d): the projection chart's guardrail-
// trigger and living-budget traces are drawn as Plotly SVG, which axe cannot
// see — that gap is exactly how checker-a11y's manual contrast walk caught
// attempt 1's failure. This file promotes that manual walk to a permanent,
// automated regression: a real Chromium browser drives an isolated test
// server (never main or demo), enables guardrails, then reads the ACTUAL
// rendered SVG legend-swatch colors of the tone traces and the ACTUAL
// computed card background via getComputedStyle, asserting >=3:1 WCAG
// contrast in both themes and that the two-panel chart height survives a
// theme toggle and a tab switch.
//
// The Go-internal test suite has no way to execute a real browser. This
// test skips cleanly (t.Skip, matching internal/templates's
// render_spending_preview_test.go node-unavailable precedent) whenever
// `node` is missing or PLAYWRIGHT_MODULE cannot be resolved, rather than
// failing the build in environments without a browser toolchain — a
// genuine PASS here requires an actual Chromium render, so a fabricated
// pass would be worse than a visible, named skip.
//
// PLAYWRIGHT_MODULE and CHROMIUM_EXECUTABLE follow the existing convention
// in internal/handlers/whatif/testdata/roth_apply_browser.cjs: point them at
// an installed playwright package and a chromium executable.
import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/config"
	"budget2/internal/services/storage"
	"budget2/internal/testutil"
)

// setupIsolatedTestServer is setupTestServer's isolation-hardened sibling:
// it copies the testdata/ fixture tree into a throwaway t.TempDir() and
// points every directory in config.Config at that copy, so a test that
// actually SAVES something (this file's seedGuardrailFixture writes
// guardrails settings via the real endpoint, and the Plotly CDN mirror
// handler caches a file under DataDirectory/cache) can never touch the
// repo-tracked testdata/ fixture every other cmd/server test shares.
// setupTestServer's plain testutil.TestDataDir() is fine for read-only
// tests; a 2026-09-09 incident (an earlier draft of this test, running
// against the shared fixture) persisted guardrails.enabled=true straight
// into the committed testdata/settings/whatif.json and left a stray
// testdata/cache/ directory — both had to be reverted by hand. Never
// reuse setupTestServer for a test that writes.
func setupIsolatedTestServer(t *testing.T) *testutil.TestServer {
	t.Helper()

	root := testutil.ProjectRoot()
	isolatedData := filepath.Join(t.TempDir(), "testdata")
	if out, err := exec.Command("cp", "-r", filepath.Join(root, "testdata"), isolatedData).CombinedOutput(); err != nil {
		t.Fatalf("copy testdata fixture to isolated dir: %v\n%s", err, out)
	}

	cfg := &config.Config{
		ListenAddr:         ":0",
		Debug:              true,
		DataDirectory:      isolatedData,
		UploadsDirectory:   isolatedData + "/uploads",
		SettingsDirectory:  isolatedData + "/settings",
		TemplatesDirectory: root + "/web/templates",
		StaticDirectory:    root + "/web/static",
		BackupDir:          t.TempDir(),
	}

	var err error
	store, err = storage.New(cfg.DataDirectory)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	if err := SetupDependencies(cfg); err != nil {
		t.Fatalf("SetupDependencies: %v", err)
	}

	router := SetupRouter()
	return testutil.NewTestServer(t, router)
}

// seedGuardrailFixture enables guardrails on the isolated test server's
// active plan via the real /whatif/guardrails endpoint (the same one
// internal/handlers/whatif/handlers_test.go's TestHandleWhatIfGuardrails
// drives), so the browser sees the identical two-panel chart a real save
// would produce — no direct settings-file surgery.
func seedGuardrailFixture(t *testing.T, baseURL string) {
	t.Helper()
	form := url.Values{
		"enabled":           {"on"},
		"floor_drop_pct":    {"20"},
		"floor_cut_pct":     {"10"},
		"ceiling_rise_pct":  {"25"},
		"ceiling_raise_pct": {"10"},
		"min_spending_pct":  {"75"},
		"max_spending_pct":  {"125"},
	}
	resp, err := http.PostForm(baseURL+"/whatif/guardrails", form)
	if err != nil {
		t.Fatalf("POST /whatif/guardrails: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /whatif/guardrails status = %d, want 200", resp.StatusCode)
	}
}

func TestGuardrailChartToneContrastBrowser(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available; skipping browser contrast regression test")
	}

	playwrightModule := os.Getenv("PLAYWRIGHT_MODULE")
	if playwrightModule == "" {
		t.Skip("PLAYWRIGHT_MODULE not set; skipping browser contrast regression test")
	}
	check := exec.Command("node", "-e", fmt.Sprintf("require(%q)", playwrightModule))
	if out, err := check.CombinedOutput(); err != nil {
		t.Skipf("playwright module at %q not usable; skipping browser contrast regression test: %v\n%s", playwrightModule, err, out)
	}

	ts := setupIsolatedTestServer(t)
	defer ts.Close()

	seedGuardrailFixture(t, ts.BaseURL)

	cjs := filepath.Join(testutil.ProjectRoot(), "cmd", "server", "testdata", "guardrail_tone_contrast_browser.cjs")
	cmd := exec.Command("node", cjs)
	cmd.Env = append(os.Environ(),
		"PLAYWRIGHT_MODULE="+playwrightModule,
		"CHROMIUM_EXECUTABLE="+os.Getenv("CHROMIUM_EXECUTABLE"),
		"BUDGET2_TEST_URL="+ts.BaseURL,
	)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "PASS") {
		t.Fatalf("browser contrast probe failed: %v\n%s", err, out)
	}
}
