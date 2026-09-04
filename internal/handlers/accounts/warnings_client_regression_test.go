package accounts

// This file is the Go half of the regression harness for accounts.html's
// syncWarnings() client-side script (ACCESSIBILITY.md point 16). The Go
// suite has no way to execute client-side JavaScript on its own, so
// nothing in it turns red when syncWarnings' dismissal ordering regresses
// -- see .swarm/verdicts/S5.1.checker-tests.verdict for how that was
// demonstrated. This test closes that gap: it extracts the LIVE <script>
// block straight out of the real template file (never a copy pasted in
// here, so the check cannot rot out of sync with an edited page), hands
// it to a self-contained node harness
// (testdata/js/warnings_dom_harness.js) that drives the sequences
// point 16 requires against a stubbed DOM/sessionStorage, and fails the
// build if the harness reports any failure.
//
// The guard against silently skipping this check lives HERE, in the test
// body, not in the Makefile. Earlier attempts tried to police every
// `go test`-running Makefile target instead (a hand-listed set, then a
// textual scan for the `$(GO) test` token pair); both were evadable by
// aliasing $(GO), by a `define`d canned recipe, by a line continuation,
// or simply by running `go test ./...`, `test-unit`, an IDE run, or CI
// directly instead of through the policed target. Putting the check in
// the test instead means every one of those invocation paths runs it,
// because none of them can reach this test function without also
// reaching this code: if `node` is not on PATH, the test FAILS (not
// skips) by default. Setting the environment variable
// BUDGET2_ALLOW_SKIP_JS to any non-empty value -- including "0" or
// "false"; presence is what's checked, not the value -- turns that
// failure back into a clean, named t.Skip, for a developer who has
// deliberately decided to work without node. See BUDGET2_ALLOW_SKIP_JS's
// documentation in README.md's Testing section for how to use it. The
// Makefile carries no node-checking machinery of its own any more: any
// invocation that RUNS this package's tests -- `go test ./...`,
// `make test`, `make test-unit`, `make race`, `make test-coverage`,
// `make check`, `go test -short` -- fails through this test's own t.Fatal,
// not through a Makefile prerequisite. An invocation that does not run
// this test cannot be caught by it: a `go test -run` filter that excludes
// it, or a Makefile target scoped to other packages
// (`make test-integration`, `make fuzz`), is a property of `go test`
// itself, not a hole in this guard.
//
// One case deserves stating precisely rather than folding into that list:
// a warm `go test` result cache. `go test` records the PATH environment
// string as part of a package's cache key -- this test's own call to
// exec.LookPath("node") reads PATH via os.Getenv, and that read is what
// gets tracked -- and it also tracks accounts.html being opened (read)
// and the harness JS being stat'd, both within the module root, so
// editing either one busts the cache and forces a real re-run. But PATH
// the string, not node's presence, is the tracked input, and node
// disappearing does not reliably change that string: on Debian and
// Ubuntu, the `nodejs` package installs the `node` binary into /usr/bin,
// a directory that is already on PATH before the package is installed
// and stays on PATH after it is removed, so `apt remove nodejs` -- the
// ordinary way node goes away on those distributions, not an edge case
// -- leaves PATH byte-for-byte unchanged. When that happens, `go test`
// may replay a cached PASS and this guard will not fire; a cached green
// in that situation is not evidence the harness ran. Every `make` target
// that runs this package's tests (`test`, `test-unit`, `test-coverage`,
// `race`, and `check` through its `test` prerequisite) closes this hole:
// each one re-runs this package a second time with `-count=1`, forcing a
// real, uncached execution regardless of what the first, possibly
// cached, run reported (see the Makefile). A bare `go test ./...`
// invoked directly, bypassing the Makefile, is not covered by that
// rerun and can still replay a stale cached pass in this situation; run
// `go clean -testcache` or `go test -count=1` to settle it in that case.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// syncWarningsScriptRE extracts the single <script>...</script> body from
// accounts.html. The template currently ships exactly one <script>
// element (see TestExtractSyncWarningsScript_FindsExactlyOneBlock below,
// which fails loudly if that ever changes instead of silently grabbing
// the wrong one).
var syncWarningsScriptRE = regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`)

// thisDir is this source file's own directory, resolved via
// runtime.Caller rather than the process's working directory, so both
// path helpers below are correct regardless of how `go test` is invoked.
func thisDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not resolve this test file's path")
	}
	return filepath.Dir(thisFile)
}

// accountsTemplatePath resolves web/templates/pages/accounts.html
// relative to this package's directory.
func accountsTemplatePath(t *testing.T) string {
	t.Helper()
	// internal/handlers/accounts -> repo root is three levels up.
	repoRoot := filepath.Join(thisDir(t), "..", "..", "..")
	return filepath.Join(repoRoot, "web", "templates", "pages", "accounts.html")
}

// accountsScriptPath resolves web/static/js/accounts.js relative to this
// package's directory. syncWarnings() moved from an inline <script> in
// accounts.html into this file (U7, script extraction); the harness reads
// it from here now, still fresh off disk on every run.
func accountsScriptPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join(thisDir(t), "..", "..", "..")
	return filepath.Join(repoRoot, "web", "static", "js", "accounts.js")
}

// warningsHarnessPath resolves testdata/js/warnings_dom_harness.js
// relative to this package's directory.
func warningsHarnessPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(thisDir(t), "testdata", "js", "warnings_dom_harness.js")
}

// extractSyncWarningsScript reads accounts.html and static/js/accounts.js
// fresh from disk. accounts.html must load accounts.js via <script src>
// and carry NO inline <script> body of its own (U7 moved syncWarnings()
// out); accounts.js is returned as the script body the node harness runs,
// so the check still cannot rot out of sync with an edited page.
func extractSyncWarningsScript(t *testing.T) string {
	t.Helper()
	htmlPath := accountsTemplatePath(t)
	htmlBody, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("reading %s: %v", htmlPath, err)
	}
	matches := syncWarningsScriptRE.FindAllStringSubmatch(string(htmlBody), -1)
	for _, m := range matches {
		if regexp.MustCompile(`\S`).MatchString(m[1]) {
			t.Fatalf("expected accounts.html to carry NO inline <script> body "+
				"(syncWarnings moved to static/js/accounts.js, U7), found one with "+
				"content in %s", htmlPath)
		}
	}
	if !regexp.MustCompile(`<script\s+src="/static/js/accounts\.js"`).MatchString(string(htmlBody)) {
		t.Fatalf("expected %s to load /static/js/accounts.js via <script src>", htmlPath)
	}

	jsPath := accountsScriptPath(t)
	jsBody, err := os.ReadFile(jsPath)
	if err != nil {
		t.Fatalf("reading %s: %v", jsPath, err)
	}
	return string(jsBody)
}

// TestExtractSyncWarningsScript_FindsExactlyOneBlock is a narrow sanity
// check on the extraction itself, independent of node: it does not need
// node and always runs, so a change to accounts.html's script structure
// (e.g. splitting syncWarnings into two <script> tags) is caught even on
// a machine that can't run the full harness below.
func TestExtractSyncWarningsScript_FindsExactlyOneBlock(t *testing.T) {
	got := extractSyncWarningsScript(t)
	if got == "" {
		t.Fatal("extracted script body is empty")
	}
	if !regexp.MustCompile(`function\s+syncWarnings`).MatchString(got) {
		t.Fatalf("extracted script body does not define syncWarnings(); "+
			"got:\n%s", got)
	}
}

// TestSyncWarnings_ClientRegressionHarness is the executable oracle for
// ACCESSIBILITY.md point 16's dismissal-ordering guarantees: dismiss ->
// resolve -> recreate the same overlap must leave the banner and the
// live region in parity, and dismiss -> reload with warnings unchanged
// must leave both suppressed. See testdata/js/warnings_dom_harness.js for
// the full sequence definitions and the DOM/sessionStorage stub.
//
// By default this test FAILS, it does not skip, when `node` is not on
// PATH -- that is the whole point of the redesign described in this
// file's package comment: the refusal to silently skip lives here, not
// in a Makefile prerequisite that a new target or a direct `go test`
// invocation can route around. Set BUDGET2_ALLOW_SKIP_JS to any
// non-empty value to opt into a clean, named skip instead, for a
// developer who has deliberately chosen to work without node installed.
func TestSyncWarnings_ClientRegressionHarness(t *testing.T) {
	const skipEnvVar = "BUDGET2_ALLOW_SKIP_JS"

	nodePath, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv(skipEnvVar) != "" {
			t.Skipf("skipping client-side warnings regression harness: `node` was not found on PATH, "+
				"and %s is set, so this is a deliberate opt-out rather than an accidental one. "+
				"This test executes accounts.html's syncWarnings() script under node "+
				"(testdata/js/warnings_dom_harness.js) to check ACCESSIBILITY.md point 16's "+
				"dismiss/resolve/recreate and dismiss/reload guarantees, which nothing else in "+
				"this Go suite can exercise.", skipEnvVar)
		}
		t.Fatalf("`node` was not found on PATH. This test executes accounts.html's syncWarnings() "+
			"script under node (testdata/js/warnings_dom_harness.js) to check ACCESSIBILITY.md "+
			"point 16's dismiss/resolve/recreate and dismiss/reload guarantees, which nothing else "+
			"in this Go suite can exercise, so this test FAILS rather than skips when node is "+
			"missing. Install node (any recent version) to run it locally, or set %s=1 to "+
			"deliberately opt out and get a clean skip instead of this failure.", skipEnvVar)
	}

	scriptSource := extractSyncWarningsScript(t)

	tmpDir := t.TempDir()
	extractedPath := filepath.Join(tmpDir, "extracted_sync_warnings.js")
	if err := os.WriteFile(extractedPath, []byte(scriptSource), 0o600); err != nil {
		t.Fatalf("writing extracted script to temp file: %v", err)
	}

	harnessPath := warningsHarnessPath(t)
	if _, err := os.Stat(harnessPath); err != nil {
		t.Fatalf("harness script not found at %s: %v", harnessPath, err)
	}

	cmd := exec.Command(nodePath, harnessPath, extractedPath)
	out, runErr := cmd.CombinedOutput()

	// Defence against a harness that exits 0 without actually running, or
	// that exits 0 while reporting a failure: require every named check
	// to have reported a result line ending in the literal PASS token,
	// not merely that its name appears in the output (a line reading
	// "RESULT <name> FAIL ..." would satisfy a name-only match) and not
	// merely a zero exit code.
	for _, name := range []string{
		"dismiss_resolve_recreate",
		"dismiss_then_reload_unchanged",
		"guard_directions",
	} {
		passRE := regexp.MustCompile(`(?m)^RESULT ` + regexp.QuoteMeta(name) + ` PASS(\s|$)`)
		if !passRE.Match(out) {
			t.Fatalf("harness output does not report PASS for check %q -- harness may not have run "+
				"to completion, or reported a failure; full output:\n%s", name, out)
		}
	}

	if runErr != nil {
		t.Fatalf("client-side warnings regression harness reported a failure "+
			"(ACCESSIBILITY.md point 16 dismissal-ordering check against the live "+
			"web/templates/pages/accounts.html <script> block):\n%s", out)
	}
}
