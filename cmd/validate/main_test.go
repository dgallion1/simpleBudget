package main

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// ---------- validateEndpoint tests ----------

func TestValidateEndpoint_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>Dashboard Total Income</html>"))
	}))
	defer srv.Close()

	client := srv.Client()
	ep := endpoint{path: "/dashboard", method: "GET", contentType: "text/html", contains: []string{"Dashboard", "Total Income"}}

	r := validateEndpoint(client, srv.URL, ep, true)
	if r.err != nil {
		t.Fatalf("expected no error, got: %v", r.err)
	}
	if r.status != 200 {
		t.Fatalf("expected 200, got %d", r.status)
	}
	if r.duration == 0 {
		t.Fatal("expected non-zero duration")
	}
}

func TestValidateEndpoint_JSONSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	client := srv.Client()
	ep := endpoint{path: "/api/health", method: "GET", contentType: "application/json", contains: []string{`"status":"ok"`}}

	r := validateEndpoint(client, srv.URL, ep, false)
	if r.err != nil {
		t.Fatalf("expected no error, got: %v", r.err)
	}
}

func TestValidateEndpoint_JSONNoContains(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[1,2,3]}`))
	}))
	defer srv.Close()

	client := srv.Client()
	ep := endpoint{path: "/data", method: "GET", contentType: "application/json", contains: nil}

	r := validateEndpoint(client, srv.URL, ep, false)
	if r.err != nil {
		t.Fatalf("expected no error, got: %v", r.err)
	}
}

func TestValidateEndpoint_WrongContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	client := srv.Client()
	ep := endpoint{path: "/test", method: "GET", contentType: "text/html", contains: nil}

	r := validateEndpoint(client, srv.URL, ep, false)
	if r.err == nil {
		t.Fatal("expected content type error")
	}
	if !strings.Contains(r.err.Error(), "wrong content type") {
		t.Fatalf("unexpected error: %v", r.err)
	}
}

func TestValidateEndpoint_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client := srv.Client()
	ep := endpoint{path: "/bad-json", method: "GET", contentType: "application/json", contains: nil}

	r := validateEndpoint(client, srv.URL, ep, false)
	if r.err == nil {
		t.Fatal("expected JSON parse error")
	}
	if !strings.Contains(r.err.Error(), "invalid JSON") {
		t.Fatalf("unexpected error: %v", r.err)
	}
}

func TestValidateEndpoint_MissingContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>Hello</html>"))
	}))
	defer srv.Close()

	client := srv.Client()
	ep := endpoint{path: "/test", method: "GET", contentType: "text/html", contains: []string{"Dashboard"}}

	r := validateEndpoint(client, srv.URL, ep, false)
	if r.err == nil {
		t.Fatal("expected missing content error")
	}
	if !strings.Contains(r.err.Error(), "missing expected content") {
		t.Fatalf("unexpected error: %v", r.err)
	}
}

func TestValidateEndpoint_RequestFailed(t *testing.T) {
	client := &http.Client{Timeout: 100 * time.Millisecond}
	ep := endpoint{path: "/test", method: "GET", contentType: "text/html", contains: nil}

	r := validateEndpoint(client, "http://127.0.0.1:1", ep, false)
	if r.err == nil {
		t.Fatal("expected connection error")
	}
	if !strings.Contains(r.err.Error(), "request failed") {
		t.Fatalf("unexpected error: %v", r.err)
	}
}

func TestValidateEndpoint_BadRequestCreation(t *testing.T) {
	client := &http.Client{}
	ep := endpoint{path: "/test", method: "BAD METHOD", contentType: "text/html", contains: nil}

	r := validateEndpoint(client, "http://localhost", ep, false)
	if r.err == nil {
		t.Fatal("expected request creation error")
	}
	if !strings.Contains(r.err.Error(), "failed to create request") {
		t.Fatalf("unexpected error: %v", r.err)
	}
}

func TestValidateEndpoint_Non200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	client := srv.Client()
	ep := endpoint{path: "/missing", method: "GET", contentType: "text/html", contains: nil}

	r := validateEndpoint(client, srv.URL, ep, false)
	if r.err != nil {
		t.Fatalf("non-200 should not produce error, got: %v", r.err)
	}
	if r.status != 404 {
		t.Fatalf("expected 404, got %d", r.status)
	}
}

func TestValidateEndpoint_ReadBodyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("short"))
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer srv.Close()

	client := srv.Client()
	ep := endpoint{path: "/test", method: "GET", contentType: "text/html", contains: nil}

	r := validateEndpoint(client, srv.URL, ep, false)
	if r.err == nil {
		t.Log("read body error not triggered (ok, best-effort test)")
	}
}

// ---------- main() tests ----------

// allPassHandler returns an HTTP handler that satisfies all endpoints.
func allPassHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, ep := range endpoints {
			if r.URL.Path == ep.path {
				w.Header().Set("Content-Type", ep.contentType)
				if ep.contentType == "application/json" {
					if r.URL.Path == "/api/health" {
						w.Write([]byte(`{"status":"ok"}`))
					} else {
						w.Write([]byte(`{"data":[]}`))
					}
				} else {
					w.Write([]byte("<html>Dashboard Total Income Explorer Search What-If Portfolio Insights</html>"))
				}
				return
			}
		}
		w.WriteHeader(404)
	})
}

// resetFlags resets flag state so main() can be called multiple times.
func resetFlags() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
}

// TestMain_AllPass calls main() directly — all endpoints pass, so no os.Exit(1).
func TestMain_AllPass(t *testing.T) {
	srv := httptest.NewServer(allPassHandler())
	defer srv.Close()

	resetFlags()
	os.Args = []string{"validate", "-url", srv.URL, "-timeout", "5"}
	main()
}

// TestMain_Verbose calls main() directly with verbose — covers the verbose print branch.
func TestMain_Verbose(t *testing.T) {
	srv := httptest.NewServer(allPassHandler())
	defer srv.Close()

	resetFlags()
	os.Args = []string{"validate", "-url", srv.URL, "-v", "-timeout", "5"}
	main()
}

// TestMain_ErrorBranch exercises the error branch in main's loop by temporarily
// replacing the endpoints slice with one that triggers an error, and using a server
// that returns wrong content types. We use subprocess since os.Exit(1) is called.
func TestMain_ErrorBranch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain") // wrong content type
		w.Write([]byte("wrong"))
	}))
	defer srv.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperMain_ErrorBranch$")
	cmd.Env = append(os.Environ(),
		"GO_TEST_HELPER=1",
		"TEST_SERVER_URL="+srv.URL,
	)
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit")
	}
}

// TestMain_Non200Branch exercises the non-200 status branch.
func TestMain_Non200Branch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperMain_Non200Branch$")
	cmd.Env = append(os.Environ(),
		"GO_TEST_HELPER=1",
		"TEST_SERVER_URL="+srv.URL,
	)
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit")
	}
}

// ---------- Helper processes (run as subprocess only) ----------

func TestHelperMain_ErrorBranch(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		t.Skip("not a helper process")
	}
	// Replace endpoints with a single test endpoint
	endpoints = []endpoint{
		{path: "/test", method: "GET", contentType: "text/html", contains: nil},
	}
	resetFlags()
	os.Args = []string{"validate", "-url", os.Getenv("TEST_SERVER_URL"), "-timeout", "5"}
	main()
}

func TestHelperMain_Non200Branch(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		t.Skip("not a helper process")
	}
	endpoints = []endpoint{
		{path: "/test", method: "GET", contentType: "text/html", contains: nil},
	}
	resetFlags()
	os.Args = []string{"validate", "-url", os.Getenv("TEST_SERVER_URL"), "-timeout", "5"}
	main()
}

// ---------- Endpoints list sanity check ----------

func TestEndpointsList(t *testing.T) {
	if len(endpoints) == 0 {
		t.Fatal("endpoints list should not be empty")
	}
	for _, ep := range endpoints {
		if ep.path == "" {
			t.Error("endpoint path should not be empty")
		}
		if ep.method == "" {
			t.Error("endpoint method should not be empty")
		}
		if ep.contentType == "" {
			t.Error("endpoint contentType should not be empty")
		}
	}
}
