package whatifmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// State is what GET /whatif/state reports.
type State struct {
	App         string `json:"app"`
	SettingsDir string `json:"settings_dir"`
	Active      string `json:"active"`
	Revision    int    `json:"revision"`
}

// ApplyResult is what POST /whatif/apply returns.
type ApplyResult struct {
	Scenario string    `json:"scenario"`
	Applied  Overrides `json:"applied"`
	Revision int       `json:"revision"`
}

// Client talks to a running cmd/server. Every call verifies the server is
// budget2 and is serving the same settings directory this process reads, so a
// stray verify instance on another port cannot absorb a write meant for the
// real plan.
type Client struct {
	baseURL     string
	settingsDir string
	http        *http.Client
	// newCmd builds the command EnsureServer spawns. It defaults to
	// serverCommand; tests override it so EnsureServer's spawn-gating logic
	// can be exercised without launching a real cmd/server.
	newCmd func() *exec.Cmd
}

func NewClient(baseURL, settingsDir string) *Client {
	return &Client{
		baseURL:     baseURL,
		settingsDir: settingsDir,
		http:        &http.Client{Timeout: 10 * time.Second},
		newCmd:      serverCommand,
	}
}

// ResolveBaseURL returns the server URL: BUDGET_SERVER_URL, else the default
// cmd/server listen address (config.DefaultConfig uses :8080).
func ResolveBaseURL(env func(string) string) string {
	if u := env("BUDGET_SERVER_URL"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

// State fetches and verifies the server's identity.
func (c *Client) State(ctx context.Context) (State, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/whatif/state", nil)
	if err != nil {
		return State{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return State{}, fmt.Errorf("no budget2 server reachable at %s: %w", c.baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return State{}, fmt.Errorf("%s/whatif/state returned %s; this may not be a budget2 server", c.baseURL, resp.Status)
	}

	var s State
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return State{}, fmt.Errorf("%s answered but is not a budget2 server (unparseable /whatif/state): %w", c.baseURL, err)
	}
	if s.App != "budget2" {
		return State{}, fmt.Errorf("%s is running %q, not budget2; refusing to write", c.baseURL, s.App)
	}

	mine, err := filepath.Abs(c.settingsDir)
	if err != nil {
		return State{}, err
	}
	theirs, err := filepath.Abs(s.SettingsDir)
	if err != nil {
		return State{}, err
	}
	if mine != theirs {
		return State{}, fmt.Errorf(
			"refusing to write: this server is serving %s but these tools read %s. "+
				"Point BUDGET_SERVER_URL at the right instance, or start the MCP server with -data %s",
			theirs, mine, theirs)
	}
	return s, nil
}

// applyRequest is the POST /whatif/apply body. It mirrors applyRequest in
// internal/handlers/whatif/handlers_live.go: the override fields inline, plus
// the optional scenario expectation.
type applyRequest struct {
	Overrides
	ExpectedScenario string `json:"expected_scenario,omitempty"`
}

// Apply posts a sparse override set.
//
// expectedScenario is the scenario filename the caller snapshotted. The server
// compares it against the active scenario inside the lock that performs the
// write and refuses with 409 if they differ, so a scenario switch racing this
// call cannot land a write on a plan with no backup. Pass "" for no
// expectation.
func (c *Client) Apply(ctx context.Context, o Overrides, expectedScenario string) (ApplyResult, error) {
	body, err := json.Marshal(applyRequest{Overrides: o, ExpectedScenario: expectedScenario})
	if err != nil {
		return ApplyResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/whatif/apply", bytes.NewReader(body))
	if err != nil {
		return ApplyResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("apply: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		msg := new(bytes.Buffer)
		_, _ = msg.ReadFrom(resp.Body)
		return ApplyResult{}, fmt.Errorf("apply rejected (%s): %s", resp.Status, bytes.TrimSpace(msg.Bytes()))
	}

	var out ApplyResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ApplyResult{}, fmt.Errorf("apply: decoding response: %w", err)
	}
	return out, nil
}

// BaseURL returns the server URL this client talks to, for building a link a
// user can open in a browser.
func (c *Client) BaseURL() string { return c.baseURL }

// SwitchScenario changes the server's active scenario. The form field name
// (filename) matches handleSwitchScenario in
// internal/handlers/whatif/handlers_scenarios.go exactly -- that handler
// reads r.FormValue("filename"), not "scenario" or "name".
func (c *Client) SwitchScenario(ctx context.Context, name string) error {
	form := url.Values{"filename": {name}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/whatif/scenarios/switch",
		strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("switch scenario: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		msg := new(bytes.Buffer)
		_, _ = msg.ReadFrom(resp.Body)
		return fmt.Errorf("switch scenario %q rejected (%s): %s", name, resp.Status, bytes.TrimSpace(msg.Bytes()))
	}
	return nil
}

// spawnArgs derives the BUDGET_DATA_DIR value for a settings directory.
//
// config.Load does SettingsDirectory = filepath.Join(dataDir, "settings"), so
// the env var must carry the PARENT of the settings dir. Passing the settings
// dir itself would produce a server serving <S>/settings, which then fails the
// identity check this same client performs -- a refusal caused entirely by the
// spawn.
func spawnArgs(settingsDir string) (string, error) {
	abs, err := filepath.Abs(settingsDir)
	if err != nil {
		return "", err
	}
	if filepath.Base(abs) != "settings" {
		return "", fmt.Errorf(
			"cannot start a server for %s: BUDGET_DATA_DIR always resolves to <dir>/settings, "+
				"so no value produces this path. Start cmd/server yourself and set BUDGET_SERVER_URL", abs)
	}
	return filepath.Dir(abs), nil
}

// listenAddrFromBaseURL derives the BUDGET_LISTEN_ADDR value for a spawned
// server from the URL this client talks to.
//
// Without it the child's listen address comes from an inherited
// BUDGET_LISTEN_ADDR or config's :8080 default, which has nothing to do with
// c.baseURL. cmd/server's startup calls killPreviousInstance(cfg.ListenAddr)
// (cmd/server/main.go), so a spawn for a server on :8099 would shut down the
// budget2 the user is actually using on :8080 and then bind :8080 itself --
// serving whatever data directory this spawn chose. Deriving the port from the
// base URL means the process we start is the process we were about to talk to.
//
// Only a loopback host may be started: a non-loopback BUDGET_SERVER_URL names a
// machine this process cannot start anything on, so binding the port locally
// would produce a local server that answers nothing the caller asked for while
// still killing whatever holds that port here.
func listenAddrFromBaseURL(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("cannot start a server for %q: unparseable URL: %w", baseURL, err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("cannot start a server for %q: no host in the URL", baseURL)
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return "", fmt.Errorf(
				"refusing to start a server for %s: only a loopback address (localhost or 127.0.0.1) can be "+
					"started from here. Start cmd/server on %s yourself, or point BUDGET_SERVER_URL at a local instance",
				baseURL, host)
		}
	}
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	return ":" + port, nil
}

// serverCommand builds the command that starts cmd/server.
//
// The built binary is preferred over `go run`: go run compiles on every cold
// start and produces a two-level process tree (go run -> server), which makes
// the deliberately-detached lifetime murky -- killing the parent would leave
// the server orphaned rather than stopping it. `make build` produces ./budget2
// from ./cmd/server, so on any working checkout the binary is there. The go run
// fallback keeps a fresh clone working before the first make build.
func serverCommand() *exec.Cmd {
	const built = "./budget2"
	if info, err := os.Stat(built); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		return exec.Command(built)
	}
	return exec.Command("go", "run", "./cmd/server")
}

// isConnectionRefused reports whether err is (or wraps) ECONNREFUSED.
func isConnectionRefused(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return errors.Is(opErr.Err, syscall.ECONNREFUSED)
	}
	return false
}

// envWithout copies os.Environ() dropping any entry for key.
func envWithout(key string) []string {
	return envWithoutIn(os.Environ(), key)
}

// envWithoutIn copies env dropping any entry for key. The prefix includes the
// "=" so a variable whose name merely contains key survives.
func envWithoutIn(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// crashTailBytes bounds how much of a spawned server's stderr log EnsureServer
// reads back for a crash diagnostic, so a chatty process cannot balloon the
// error message. The tail is what matters -- a crash message is almost always
// the last thing written.
const crashTailBytes = 4096

// tailOfFile returns the last max bytes of the file at path, or "" if it cannot
// be read. Only the tail is read, so the file's size does not bound memory.
func tailOfFile(path string, max int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return ""
	}
	size := info.Size()
	if size > int64(max) {
		if _, err := f.Seek(size-int64(max), io.SeekStart); err != nil {
			return ""
		}
		size = int64(max)
	}
	buf := make([]byte, size)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return ""
	}
	return string(buf[:n])
}

// stderrLogFile creates the file a spawned server's stderr is redirected to.
//
// It must be a real *os.File: os/exec only passes an fd straight through when
// Stderr is one. Any other io.Writer gets a pipe whose read end this process
// holds, so the "detached, outlives this session" property open_page advertises
// would be false -- when the MCP exits, that read end closes and the server
// dies of SIGPIPE at its next stderr write.
//
// The file is deliberately left on disk: the child keeps writing to it for its
// whole life, and it is the only record of why a spawn that failed later did
// so. Spawns happen only when nothing is already listening, so these do not
// accumulate per tool call.
func stderrLogFile() (*os.File, error) {
	return os.CreateTemp("", "budget2-server-*.log")
}

// EnsureServer returns the state of a usable server, starting one if nothing is
// listening. The bool reports whether this call started it.
//
// A spawned server is deliberately detached so it outlives this process and the
// user's browser tab keeps working after the session ends. /killme stops it.
func (c *Client) EnsureServer(ctx context.Context) (State, bool, error) {
	if s, err := c.State(ctx); err == nil {
		return s, false, nil
	} else if !isConnectionRefused(err) {
		// A reachable server that failed verification must not be replaced by
		// a second one -- report the mismatch instead.
		return State{}, false, err
	}

	dataDir, err := spawnArgs(c.settingsDir)
	if err != nil {
		return State{}, false, err
	}
	listenAddr, err := listenAddrFromBaseURL(c.baseURL)
	if err != nil {
		return State{}, false, err
	}

	cmd := c.newCmd()
	// Both variables are stripped from the inherited environment first: an
	// inherited value would otherwise silently win over the one derived here
	// and put the child on a different port or a different plan than the one
	// this client verified.
	env := envWithout("BUDGET_DATA_DIR")
	env = envWithoutIn(env, "BUDGET_LISTEN_ADDR")
	cmd.Env = append(env, "BUDGET_DATA_DIR="+dataDir, "BUDGET_LISTEN_ADDR="+listenAddr)

	logFile, err := stderrLogFile()
	if err != nil {
		return State{}, false, fmt.Errorf("creating a log file for cmd/server: %w", err)
	}
	logPath := logFile.Name()
	// This process's handle is closed as soon as the child has its own copy;
	// the child's fd keeps the file open for as long as it runs.
	defer func() { _ = logFile.Close() }()
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return State{}, false, fmt.Errorf("starting cmd/server: %w", err)
	}

	// Buffered so the goroutine never blocks on the send, even if we return
	// from the success path below without ever reading from it -- it still
	// reaps the process (never leaves a zombie) without blocking this call.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := c.State(ctx); err == nil {
			return s, true, nil
		}
		select {
		case <-ctx.Done():
			return State{}, false, ctx.Err()
		case waitErr := <-exited:
			// The process is gone before it ever answered -- report why
			// instead of waiting out the rest of the 15s and describing a
			// crash as a slow start.
			tail := strings.TrimSpace(tailOfFile(logPath, crashTailBytes))
			if tail == "" {
				tail = "(no output on stderr)"
			}
			return State{}, false, fmt.Errorf(
				"cmd/server exited before it became healthy (%v); stderr (%s):\n%s",
				waitErr, logPath, tail)
		case <-time.After(250 * time.Millisecond):
		}
	}
	return State{}, false, fmt.Errorf(
		"started cmd/server but it did not become healthy at %s within 15s; "+
			"start it yourself with BUDGET_DATA_DIR=%s and retry", c.baseURL, dataDir)
}
