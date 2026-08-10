package whatifmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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

func TestEnsureServer_AdoptsHealthyInstance(t *testing.T) {
	dir := t.TempDir()
	srv := stateServer(t, State{App: "budget2", SettingsDir: dir, Active: "whatif.json", Revision: 3})
	defer srv.Close()

	c := NewClient(srv.URL, dir)
	spawnAttempts := 0
	c.newCmd = func() *exec.Cmd {
		spawnAttempts++
		return exec.Command("true")
	}

	got, started, err := c.EnsureServer(context.Background())
	if err != nil {
		t.Fatalf("EnsureServer: %v", err)
	}
	if started {
		t.Fatal("started should be false when a healthy instance already answers")
	}
	if got.Revision != 3 {
		t.Fatalf("unexpected state: %+v", got)
	}
	if spawnAttempts != 0 {
		t.Fatalf("expected no spawn attempt, got %d", spawnAttempts)
	}
}

func TestEnsureServer_MismatchedReachableServerReturnsErrorWithoutSpawning(t *testing.T) {
	mine := t.TempDir()
	theirs := t.TempDir()
	srv := stateServer(t, State{App: "budget2", SettingsDir: theirs})
	defer srv.Close()

	c := NewClient(srv.URL, mine)
	spawnAttempts := 0
	c.newCmd = func() *exec.Cmd {
		spawnAttempts++
		return exec.Command("true")
	}

	_, started, err := c.EnsureServer(context.Background())
	if started {
		t.Fatal("started should be false on a verification failure")
	}
	if err == nil {
		t.Fatal("expected the verification error to be returned")
	}
	if !strings.Contains(err.Error(), mine) || !strings.Contains(err.Error(), theirs) {
		t.Fatalf("expected the settings-dir mismatch error naming both paths, got: %v", err)
	}
	if spawnAttempts != 0 {
		t.Fatalf("a reachable-but-mismatched server must not trigger a spawn, got %d attempts", spawnAttempts)
	}
}

func TestEnsureServer_ForeignAppReachableServerReturnsErrorWithoutSpawning(t *testing.T) {
	dir := t.TempDir()
	srv := stateServer(t, State{App: "something-else", SettingsDir: dir})
	defer srv.Close()

	c := NewClient(srv.URL, dir)
	spawnAttempts := 0
	c.newCmd = func() *exec.Cmd {
		spawnAttempts++
		return exec.Command("true")
	}

	_, started, err := c.EnsureServer(context.Background())
	if started {
		t.Fatal("started should be false on a verification failure")
	}
	if err == nil {
		t.Fatal("expected the verification error to be returned")
	}
	if spawnAttempts != 0 {
		t.Fatalf("a reachable-but-foreign server must not trigger a spawn, got %d attempts", spawnAttempts)
	}
}

// TestEnsureServer_SpawnEnvCarriesPortFromBaseURL pins the one thing that
// stops a spawn from taking down the user's real server: the child's listen
// address must come from the URL this client was pointed at, not from an
// inherited BUDGET_LISTEN_ADDR or config's :8080 default. cmd/server's startup
// calls killPreviousInstance(cfg.ListenAddr), so a child that inherits :8080
// while this client talks to :8099 shuts down the real instance and then binds
// :8080 to a throwaway data directory.
func TestEnsureServer_SpawnEnvCarriesPortFromBaseURL(t *testing.T) {
	// An inherited value that must NOT reach the child.
	t.Setenv("BUDGET_LISTEN_ADDR", ":8080")

	// Nothing is listening here, so EnsureServer proceeds to the spawn.
	srv := stateServer(t, State{App: "budget2"})
	baseURL := srv.URL
	srv.Close()

	settingsDir := filepath.Join(t.TempDir(), "settings")
	c := NewClient(baseURL, settingsDir)

	var spawned *exec.Cmd
	c.newCmd = func() *exec.Cmd {
		spawned = exec.Command("true")
		return spawned
	}

	// The spawned "server" exits immediately and never answers, so this
	// returns the crash error; the environment it was given is what matters.
	if _, _, err := c.EnsureServer(context.Background()); err == nil {
		t.Fatal("expected an error: the fake spawned process never becomes healthy")
	}

	if spawned == nil {
		t.Fatal("no command was constructed")
	}
	wantPort := baseURL[strings.LastIndex(baseURL, ":"):] // ":PORT"
	wantAddr := "BUDGET_LISTEN_ADDR=" + wantPort

	found := false
	for _, kv := range spawned.Env {
		if kv == wantAddr {
			found = true
		} else if strings.HasPrefix(kv, "BUDGET_LISTEN_ADDR=") {
			t.Errorf("child environment carries a second BUDGET_LISTEN_ADDR %q; the inherited value must be stripped", kv)
		}
	}
	if !found {
		t.Fatalf("child environment does not carry %q (base URL was %s)", wantAddr, baseURL)
	}
}

func TestListenAddrFromBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{"localhost with port", "http://localhost:8099", ":8099", false},
		{"loopback ip with port", "http://127.0.0.1:8080", ":8080", false},
		{"ipv6 loopback with port", "http://[::1]:8099", ":8099", false},
		{"no port defaults to the scheme's", "http://localhost", ":80", false},
		{"https no port", "https://localhost", ":443", false},
		{"non-loopback host refuses", "http://192.168.1.50:8080", "", true},
		{"named remote host refuses", "http://budget.example.com:8080", "", true},
		{"no host refuses", "not-a-url", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := listenAddrFromBaseURL(tc.url)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("listenAddrFromBaseURL(%q) = %q, want an error", tc.url, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("listenAddrFromBaseURL(%q): %v", tc.url, err)
			}
			if got != tc.want {
				t.Errorf("listenAddrFromBaseURL(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

// TestEnsureServer_RefusesToSpawnForNonLoopbackURL proves the refusal reaches
// EnsureServer: a remote BUDGET_SERVER_URL must not cause a local spawn, which
// would kill whatever local process holds that port and then serve a plan
// nobody asked about.
func TestEnsureServer_RefusesToSpawnForNonLoopbackURL(t *testing.T) {
	settingsDir := filepath.Join(t.TempDir(), "settings")
	// 192.0.2.1 is TEST-NET-1 (RFC 5737): guaranteed not to be a real host,
	// so the State() probe fails without reaching anything.
	c := NewClient("http://192.0.2.1:8099", settingsDir)
	c.http = &http.Client{Timeout: 500 * time.Millisecond}

	spawnAttempts := 0
	c.newCmd = func() *exec.Cmd {
		spawnAttempts++
		return exec.Command("true")
	}

	_, started, err := c.EnsureServer(context.Background())
	if started {
		t.Fatal("started should be false when the URL is not loopback")
	}
	if err == nil {
		t.Fatal("expected a refusal for a non-loopback base URL")
	}
	if spawnAttempts != 0 {
		t.Fatalf("a non-loopback URL must not spawn anything, got %d attempts", spawnAttempts)
	}
}

// TestEnsureServer_ChildStderrIsARealFile pins the detachment property: os/exec
// hands the child an fd directly only when Stderr is an *os.File. Any other
// io.Writer becomes a pipe held by this process, and the "server outlives this
// session" promise in open_page's description becomes false -- the child dies
// of SIGPIPE at its next stderr write once the MCP exits.
func TestEnsureServer_ChildStderrIsARealFile(t *testing.T) {
	srv := stateServer(t, State{App: "budget2"})
	baseURL := srv.URL
	srv.Close()

	settingsDir := filepath.Join(t.TempDir(), "settings")
	c := NewClient(baseURL, settingsDir)

	var spawned *exec.Cmd
	c.newCmd = func() *exec.Cmd {
		spawned = exec.Command("true")
		return spawned
	}

	if _, _, err := c.EnsureServer(context.Background()); err == nil {
		t.Fatal("expected an error: the fake spawned process never becomes healthy")
	}
	if spawned == nil {
		t.Fatal("no command was constructed")
	}
	if _, ok := spawned.Stderr.(*os.File); !ok {
		t.Fatalf("cmd.Stderr = %T, want *os.File so the child gets a real fd and no pipe to this process", spawned.Stderr)
	}
}

func TestTailOfFile_ReturnsOnlyTheTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("writing log: %v", err)
	}
	if got := tailOfFile(path, 4); got != "6789" {
		t.Errorf("tailOfFile(max 4) = %q, want %q", got, "6789")
	}
	if got := tailOfFile(path, 100); got != "0123456789" {
		t.Errorf("tailOfFile(max 100) = %q, want the whole file", got)
	}
	if got := tailOfFile(filepath.Join(t.TempDir(), "missing"), 100); got != "" {
		t.Errorf("tailOfFile on a missing file = %q, want \"\"", got)
	}
}

func TestEnsureServer_ReportsCrashInsteadOfTimeout(t *testing.T) {
	// A spawned server that dies immediately (bad data dir, port held by
	// something that isn't HTTP, permissions) must not be reported as "did
	// not become healthy within 15s" -- that reads as a slow start and
	// wastes 15s, and discards the real reason.
	srv := stateServer(t, State{App: "budget2"})
	url := srv.URL
	srv.Close() // nothing listening: State() sees connection-refused

	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "crash.sh")
	script := "#!/bin/sh\necho boom-crash-message >&2\nexit 7\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake crashing server: %v", err)
	}

	settingsDir := filepath.Join(t.TempDir(), "settings")
	c := NewClient(url, settingsDir)
	c.newCmd = func() *exec.Cmd {
		return exec.Command(scriptPath)
	}

	start := time.Now()
	_, started, err := c.EnsureServer(context.Background())
	elapsed := time.Since(start)

	if started {
		t.Fatal("started should be false when the spawned process crashed")
	}
	if err == nil {
		t.Fatal("expected an error when the spawned process exits immediately")
	}
	if !strings.Contains(err.Error(), "boom-crash-message") {
		t.Fatalf("error should include the captured stderr tail, got: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("a crash should be detected quickly, not after the 15s timeout; took %v", elapsed)
	}
}

func TestIsConnectionRefused(t *testing.T) {
	// A genuine ECONNREFUSED, produced the same way State()'s http.Client
	// produces it: *url.Error -> *net.OpError -> *os.SyscallError wrapping
	// syscall.ECONNREFUSED.
	closed := stateServer(t, State{App: "budget2"})
	url := closed.URL
	closed.Close()
	_, refusedErr := http.Get(url)
	if refusedErr == nil {
		t.Fatal("expected an error connecting to a closed listener")
	}

	resetErr := &net.OpError{
		Op:  "read",
		Net: "tcp",
		Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET},
	}

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"genuine connection refused", refusedErr, true},
		{"context canceled", context.Canceled, false},
		{"context deadline exceeded", context.DeadlineExceeded, false},
		{"plain error", errors.New("boom"), false},
		{"connection reset is not connection refused", resetErr, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isConnectionRefused(tc.err); got != tc.want {
				t.Errorf("isConnectionRefused(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestClientSwitchScenario_PostsExpectedFilenameField asserts the exact form
// field name SwitchScenario sends matches what handleSwitchScenario
// (internal/handlers/whatif/handlers_scenarios.go) reads via
// r.FormValue("filename"). Nothing else in this codebase pins that name down
// mechanically; a rename on either side would previously go unnoticed until
// it either 400'd or, worse, silently no-op'd and let a write land on the
// wrong scenario.
func TestClientSwitchScenario_PostsExpectedFilenameField(t *testing.T) {
	dir := t.TempDir()

	gotFilename := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/whatif/scenarios/switch" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "want POST", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotFilename <- r.FormValue("filename")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, dir)
	if err := c.SwitchScenario(context.Background(), "whatif-b.json"); err != nil {
		t.Fatalf("SwitchScenario: %v", err)
	}

	select {
	case got := <-gotFilename:
		if got != "whatif-b.json" {
			t.Errorf(`server received form field "filename" = %q, want %q`, got, "whatif-b.json")
		}
	default:
		t.Fatal("the fake server's handler was never invoked")
	}
}

// TestClientSwitchScenario_NonOKBecomesError proves a rejected switch (bad
// filename, server-side error) surfaces as a Go error naming the scenario
// rather than being swallowed.
func TestClientSwitchScenario_NonOKBecomesError(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "scenario filename is required", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, dir)
	err := c.SwitchScenario(context.Background(), "does-not-exist.json")
	if err == nil {
		t.Fatal("expected an error for a non-200 response")
	}
	if !strings.Contains(err.Error(), "does-not-exist.json") {
		t.Errorf("error should name the scenario, got: %v", err)
	}
}

func TestEnvWithout_MatchesKeyPrefixOnly(t *testing.T) {
	t.Setenv("BUDGET_DATA_DIR", "/should/be/removed")
	t.Setenv("MY_BUDGET_DATA_DIR_EXTRA", "/should/survive")

	got := envWithout("BUDGET_DATA_DIR")

	for _, kv := range got {
		if strings.HasPrefix(kv, "BUDGET_DATA_DIR=") {
			t.Fatalf("expected BUDGET_DATA_DIR to be removed, found %q", kv)
		}
	}
	found := false
	for _, kv := range got {
		if kv == "MY_BUDGET_DATA_DIR_EXTRA=/should/survive" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected MY_BUDGET_DATA_DIR_EXTRA to survive: its name merely contains the key")
	}
}
