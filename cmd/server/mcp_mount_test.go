package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"budget2/internal/config"
	"budget2/internal/handlers/backup"
	"budget2/internal/services/storage"
	"budget2/internal/testutil"

	"github.com/go-chi/chi/v5"
)

// initializeBody is a minimal MCP initialize request. A mounted streamable
// handler answers it; an unmounted path 404s.
const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{` +
	`"protocolVersion":"2025-06-18","capabilities":{},` +
	`"clientInfo":{"name":"mount-test","version":"v0.0.0"}}}`

// newMCPRouter mirrors the setup helper at the top of main_test.go: the
// package's dependencies are globals, so a router is only meaningful after
// SetupDependencies has run against a test config.
func newMCPRouter(t *testing.T) chi.Router {
	t.Helper()
	root := testutil.ProjectRoot()
	cfg := &config.Config{
		ListenAddr:         ":0",
		Debug:              true,
		DataDirectory:      testutil.TestDataDir(),
		UploadsDirectory:   testutil.TestDataDir() + "/uploads",
		SettingsDirectory:  testutil.TestDataDir() + "/settings",
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
	return SetupRouter()
}

// newLockedMCPRouter is like newMCPRouter, but its storage is encrypted and
// then locked before the router is returned, so lockCheckMiddleware is
// actually armed (backup.IsStorageLocked() requires store.IsEncrypted() &&
// !store.IsUnlocked() -- the plain testdata store newMCPRouter uses can never
// satisfy that, which is what let the redirect test pass without proving
// anything). It uses an isolated t.TempDir() data directory rather than the
// shared testdata fixtures, since EnableEncryption rewrites files in place.
func newLockedMCPRouter(t *testing.T) chi.Router {
	t.Helper()
	root := testutil.ProjectRoot()
	dataDir := t.TempDir()
	cfg := &config.Config{
		ListenAddr:         ":0",
		Debug:              true,
		DataDirectory:      dataDir,
		UploadsDirectory:   dataDir + "/uploads",
		SettingsDirectory:  dataDir + "/settings",
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

	if err := store.EnableEncryption("mcp-mount-test-password"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	store.Lock()
	if !backup.IsStorageLocked() {
		t.Fatal("setup bug: store should be locked after EnableEncryption + Lock")
	}

	return SetupRouter()
}

func newMCPRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	return req
}

func TestMCPEndpointIsMounted(t *testing.T) {
	r := newMCPRouter(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, newMCPRequest(t, initializeBody))

	if rec.Code == http.StatusNotFound {
		t.Fatal("/mcp is not mounted")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("initialize returned %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "budget2") {
		t.Errorf("initialize result does not name the budget2 server: %s", rec.Body.String())
	}
}

// The lock-check middleware answers 307 -> /unlock, which a JSON-RPC client
// cannot follow. /mcp must sit outside that group; its tools report a locked
// store as an error string instead.
//
// This test only means something while the middleware is actually armed, so
// it locks storage for real and checks a control route (/dashboard, which
// IS inside the lock group) redirects while /mcp does not. Without the
// control route, this test would stay green even if /mcp were moved inside
// r.Group -- an unencrypted store makes lockCheckMiddleware an unconditional
// pass-through, so a bare "no 307" assertion proves nothing.
func TestMCPEndpointIsNotBehindTheLockRedirect(t *testing.T) {
	r := newLockedMCPRouter(t)

	dashRec := httptest.NewRecorder()
	r.ServeHTTP(dashRec, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	if dashRec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("control check failed: /dashboard returned %d while storage is locked, want %d (redirect to /unlock) -- lockCheckMiddleware is not actually armed in this test", dashRec.Code, http.StatusTemporaryRedirect)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, newMCPRequest(t, initializeBody))

	if rec.Code == http.StatusTemporaryRedirect {
		t.Fatalf("/mcp redirected to %q while storage is locked; it must not sit behind lockCheckMiddleware", rec.Header().Get("Location"))
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("/mcp returned %d while storage is locked, want 200 (locked store is reported by tools, not routing); body: %s", rec.Code, rec.Body.String())
	}
}

// Cross-origin protection must reject a browser-issued cross-site POST: a
// page on any origin could otherwise drive these tools against local data.
func TestMCPEndpointRejectsCrossOriginRequests(t *testing.T) {
	r := newMCPRouter(t)

	req := newMCPRequest(t, initializeBody)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("a cross-site POST to /mcp got status %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// TestMCPEndpointRejectsSpoofedHostOnLoopbackConnection exercises the SDK's
// DNS-rebinding guard end to end. httptest.NewRecorder (used by the tests
// above) never populates http.LocalAddrContextKey, so the guard in
// mcp.StreamableHTTPHandler.ServeHTTP never even reaches its check -- those
// tests would stay green even if DisableLocalhostProtection were set, or if
// the SDK's default flipped (the option is slated for removal in v1.8.0).
// httptest.NewServer runs a real net/http server on 127.0.0.1, which DOES
// populate LocalAddrContextKey with the accepted connection's local address,
// so a request over it actually exercises the guard.
func TestMCPEndpointRejectsSpoofedHostOnLoopbackConnection(t *testing.T) {
	r := newMCPRouter(t)
	srv := httptest.NewServer(r)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(initializeBody))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	// The connection itself is loopback (httptest.NewServer listens on
	// 127.0.0.1); only the Host header is spoofed, the way a DNS-rebinding
	// attack would present it.
	req.Host = "evil.example"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("got status %d, want %d (a spoofed Host on a loopback connection must be rejected); body: %s",
			resp.StatusCode, http.StatusForbidden, body)
	}
}
