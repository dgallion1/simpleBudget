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
)

func stateServer(t *testing.T, s State) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/whatif/state" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s)
	}))
}

func TestClientState_AdoptsMatchingInstance(t *testing.T) {
	dir := t.TempDir()
	srv := stateServer(t, State{App: "budget2", SettingsDir: dir, Active: "whatif.json", Revision: 7})
	defer srv.Close()

	got, err := NewClient(srv.URL, dir).State(context.Background())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if got.Revision != 7 || got.Active != "whatif.json" {
		t.Fatalf("unexpected state: %+v", got)
	}
}

func TestClientState_RefusesDifferentPlan(t *testing.T) {
	mine := t.TempDir()
	theirs := t.TempDir()
	srv := stateServer(t, State{App: "budget2", SettingsDir: theirs, Active: "whatif.json"})
	defer srv.Close()

	_, err := NewClient(srv.URL, mine).State(context.Background())
	if err == nil {
		t.Fatal("expected a refusal when the server serves a different settings dir")
	}
	// Both paths must appear, or the user cannot tell which one they meant.
	if !strings.Contains(err.Error(), mine) || !strings.Contains(err.Error(), theirs) {
		t.Fatalf("error must name both paths, got: %v", err)
	}
}

func TestClientState_RefusesForeignApp(t *testing.T) {
	dir := t.TempDir()
	srv := stateServer(t, State{App: "something-else", SettingsDir: dir})
	defer srv.Close()

	if _, err := NewClient(srv.URL, dir).State(context.Background()); err == nil {
		t.Fatal("expected a refusal when the app identity does not match")
	}
}

func TestClientState_RefusesUnparseableBody(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>not budget2</html>"))
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL, dir).State(context.Background()); err == nil {
		t.Fatal("expected a refusal for a non-JSON body")
	}
}

func TestClientState_ErrorsCleanlyWhenRefused(t *testing.T) {
	dir := t.TempDir()
	srv := stateServer(t, State{App: "budget2", SettingsDir: dir})
	url := srv.URL
	srv.Close() // nothing is listening now

	_, err := NewClient(url, dir).State(context.Background())
	if err == nil {
		t.Fatal("expected an error when the connection is refused")
	}
}

func TestSpawnArgs_DerivesDataDirFromSettingsDir(t *testing.T) {
	// config.Load appends "settings" to BUDGET_DATA_DIR, so the env var must
	// carry the PARENT. Passing the settings dir itself yields <S>/settings and
	// the identity check refuses a server we just launched.
	got, err := spawnArgs(filepath.Join("/home/u/budget2", "data", "settings"))
	if err != nil {
		t.Fatalf("spawnArgs: %v", err)
	}
	if want := filepath.Join("/home/u/budget2", "data"); got != want {
		t.Fatalf("BUDGET_DATA_DIR = %q, want %q", got, want)
	}
}

func TestSpawnArgs_RefusesUnreachableSettingsDir(t *testing.T) {
	// No BUDGET_DATA_DIR value can produce this settings path, so guessing
	// would silently serve the wrong plan.
	if _, err := spawnArgs("/somewhere/custom-plan-dir"); err == nil {
		t.Fatal("expected a refusal for a settings dir not named \"settings\"")
	}
}

func TestResolveBaseURL(t *testing.T) {
	withEnv := func(k string) string {
		if k == "BUDGET_SERVER_URL" {
			return "http://localhost:9999"
		}
		return ""
	}
	if got := ResolveBaseURL(withEnv); got != "http://localhost:9999" {
		t.Errorf("env override ignored: %q", got)
	}
	if got := ResolveBaseURL(func(string) string { return "" }); got != "http://localhost:8080" {
		t.Errorf("default = %q, want http://localhost:8080", got)
	}
}

func TestServerCommand_PrefersBuiltBinary(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	binPath := filepath.Join(dir, "budget2")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing fake binary: %v", err)
	}

	cmd := serverCommand()
	if cmd.Path != "./budget2" && filepath.Base(cmd.Path) != "budget2" {
		t.Fatalf("expected the built binary to be preferred, got Path=%q Args=%v", cmd.Path, cmd.Args)
	}
	if len(cmd.Args) == 0 || cmd.Args[0] != "./budget2" {
		t.Fatalf("expected Args[0] = ./budget2, got %v", cmd.Args)
	}
}

func TestServerCommand_FallsBackToGoRun(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	cmd := serverCommand()
	if len(cmd.Args) < 3 || cmd.Args[0] != "go" || cmd.Args[1] != "run" || cmd.Args[2] != "./cmd/server" {
		t.Fatalf("expected fallback to `go run ./cmd/server`, got Args=%v", cmd.Args)
	}
}
