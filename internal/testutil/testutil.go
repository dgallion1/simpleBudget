// Package testutil provides testing utilities for the budget application.
package testutil

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestServer wraps httptest.Server with convenience methods
type TestServer struct {
	Server  *httptest.Server
	BaseURL string
	t       *testing.T
}

// ProjectRoot returns the root directory of the project.
// It works by finding the go.mod file.
func ProjectRoot() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("could not get caller info")
	}

	// Start from this file's directory and walk up
	dir := filepath.Dir(filename)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find project root (go.mod)")
		}
		dir = parent
	}
}

// TestDataDir returns the path to the testdata directory
func TestDataDir() string {
	return filepath.Join(ProjectRoot(), "testdata")
}

// TestConfig returns a config suitable for testing
func TestConfig() map[string]string {
	root := ProjectRoot()
	return map[string]string{
		"BUDGET_DATA_DIR":      filepath.Join(root, "testdata"),
		"BUDGET_TEMPLATES_DIR": filepath.Join(root, "web", "templates"),
		"BUDGET_STATIC_DIR":    filepath.Join(root, "web", "static"),
		"BUDGET_DEBUG":         "true",
		"BUDGET_LISTEN_ADDR":   ":0", // Random port
	}
}

// SetTestEnv sets environment variables for testing and returns a cleanup function
func SetTestEnv(t *testing.T) func() {
	t.Helper()

	cfg := TestConfig()
	oldValues := make(map[string]string)

	for k, v := range cfg {
		oldValues[k] = os.Getenv(k)
		os.Setenv(k, v)
	}

	return func() {
		for k, v := range oldValues {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}
}

// NewTestServer creates a new test server using the application's router.
// It sets up the test environment with testdata directory.
func NewTestServer(t *testing.T, router http.Handler) *TestServer {
	t.Helper()

	server := httptest.NewServer(router)

	return &TestServer{
		Server:  server,
		BaseURL: server.URL,
		t:       t,
	}
}

// GET performs a GET request to the given path
func (ts *TestServer) GET(path string) *http.Response {
	ts.t.Helper()

	resp, err := http.Get(ts.BaseURL + path)
	if err != nil {
		ts.t.Fatalf("GET %s failed: %v", path, err)
	}
	return resp
}

// GETWithQuery performs a GET request with query parameters
func (ts *TestServer) GETWithQuery(path string, query map[string]string) *http.Response {
	ts.t.Helper()

	url := ts.BaseURL + path
	if len(query) > 0 {
		url += "?"
		first := true
		for k, v := range query {
			if !first {
				url += "&"
			}
			url += k + "=" + v
			first = false
		}
	}

	resp, err := http.Get(url)
	if err != nil {
		ts.t.Fatalf("GET %s failed: %v", path, err)
	}
	return resp
}

// POST performs a POST request to the given path
func (ts *TestServer) POST(path string, contentType string, body io.Reader) *http.Response {
	ts.t.Helper()

	resp, err := http.Post(ts.BaseURL+path, contentType, body)
	if err != nil {
		ts.t.Fatalf("POST %s failed: %v", path, err)
	}
	return resp
}

// Close shuts down the test server
func (ts *TestServer) Close() {
	ts.Server.Close()
}

// ReadBody reads and returns the response body as a string
func ReadBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	return string(body)
}
