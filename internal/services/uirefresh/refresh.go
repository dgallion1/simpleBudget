// Package uirefresh coordinates explicit browser refresh requests in memory.
package uirefresh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type State struct {
	Epoch    string `json:"epoch"`
	Revision uint64 `json:"revision"`
}

type Coordinator struct {
	mu    sync.Mutex
	state State
}

func New() *Coordinator { return &Coordinator{state: State{Epoch: uuid.NewString()}} }

func (c *Coordinator) Request() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Revision++
	return c.state
}

func (c *Coordinator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c.mu.Lock()
	state := c.state
	c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(state)
}

type Result struct {
	Requested bool   `json:"requested"`
	Revision  uint64 `json:"revision"`
	Message   string `json:"message"`
}

func Register(s *mcp.Server, c *Coordinator) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "refresh_pages",
		Description: "Request a refresh of open pages connected to THIS server only. Real and demo instances are separate; check the connected instance before calling. No saved data is changed. Hidden pages and submissions may defer; unsaved edits require browser confirmation. The result confirms a request, not that every browser reloaded.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, Result, error) {
		if c == nil {
			return nil, Result{}, fmt.Errorf("page refresh coordinator is not configured")
		}
		state := c.Request()
		return nil, Result{Requested: true, Revision: state.Revision, Message: "Refresh requested for this server's open pages; browser delivery and reload are not confirmed."}, nil
	})
}
