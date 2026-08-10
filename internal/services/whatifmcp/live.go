package whatifmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
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
}

func NewClient(baseURL, settingsDir string) *Client {
	return &Client{
		baseURL:     baseURL,
		settingsDir: settingsDir,
		http:        &http.Client{Timeout: 10 * time.Second},
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

// Apply posts a sparse override set.
func (c *Client) Apply(ctx context.Context, o Overrides) (ApplyResult, error) {
	body, err := json.Marshal(o)
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
	prefix := key + "="
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		out = append(out, kv)
	}
	return out
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

	cmd := serverCommand()
	cmd.Env = append(envWithout("BUDGET_DATA_DIR"), "BUDGET_DATA_DIR="+dataDir)
	if err := cmd.Start(); err != nil {
		return State{}, false, fmt.Errorf("starting cmd/server: %w", err)
	}
	go func() { _ = cmd.Wait() }() // reap; never block the tool call

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := c.State(ctx); err == nil {
			return s, true, nil
		}
		select {
		case <-ctx.Done():
			return State{}, false, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return State{}, false, fmt.Errorf(
		"started cmd/server but it did not become healthy at %s within 15s; "+
			"start it yourself with BUDGET_DATA_DIR=%s and retry", c.baseURL, dataDir)
}
