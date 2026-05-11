package testutil

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectRootAndTestConfig(t *testing.T) {
	root := ProjectRoot()
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("project root does not contain go.mod: %v", err)
	}

	dataDir := TestDataDir()
	if filepath.Base(dataDir) != "testdata" {
		t.Fatalf("TestDataDir got %q", dataDir)
	}

	cfg := TestConfig()
	if cfg["BUDGET_DATA_DIR"] != dataDir {
		t.Fatalf("BUDGET_DATA_DIR got %q, want %q", cfg["BUDGET_DATA_DIR"], dataDir)
	}
	if !strings.HasSuffix(cfg["BUDGET_TEMPLATES_DIR"], filepath.Join("web", "templates")) {
		t.Fatalf("templates dir got %q", cfg["BUDGET_TEMPLATES_DIR"])
	}
	if cfg["BUDGET_DEBUG"] != "true" {
		t.Fatalf("BUDGET_DEBUG got %q", cfg["BUDGET_DEBUG"])
	}
	if cfg["BUDGET_LISTEN_ADDR"] != ":0" {
		t.Fatalf("BUDGET_LISTEN_ADDR got %q", cfg["BUDGET_LISTEN_ADDR"])
	}
}

func TestSetTestEnvRestoresPreviousValues(t *testing.T) {
	t.Setenv("BUDGET_DATA_DIR", "/before")
	os.Unsetenv("BUDGET_STATIC_DIR")

	cleanup := SetTestEnv(t)
	if got := os.Getenv("BUDGET_DATA_DIR"); got == "/before" || got == "" {
		t.Fatalf("BUDGET_DATA_DIR was not set to test value: %q", got)
	}
	if got := os.Getenv("BUDGET_STATIC_DIR"); got == "" {
		t.Fatal("BUDGET_STATIC_DIR was not set")
	}

	cleanup()

	if got := os.Getenv("BUDGET_DATA_DIR"); got != "/before" {
		t.Fatalf("BUDGET_DATA_DIR after cleanup got %q", got)
	}
	if got := os.Getenv("BUDGET_STATIC_DIR"); got != "" {
		t.Fatalf("BUDGET_STATIC_DIR after cleanup got %q", got)
	}
}

func TestTestServerGetHelpersAndReadBody(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s %s", r.URL.Path, r.URL.RawQuery)
	})
	server := NewTestServer(t, handler)
	defer server.Close()

	resp := server.GET("/plain")
	if got := ReadBody(t, resp); got != "/plain " {
		t.Fatalf("GET body got %q", got)
	}

	resp = server.GETWithQuery("/search", map[string]string{"q": "budget"})
	if got := ReadBody(t, resp); got != "/search q=budget" {
		t.Fatalf("GETWithQuery body got %q", got)
	}
}
