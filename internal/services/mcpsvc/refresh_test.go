package mcpsvc

import (
	"budget2/internal/services/uirefresh"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http/httptest"
	"testing"
)

func TestRefreshToolSchemaAndSharedCoordinator(t *testing.T) {
	c := uirefresh.New()
	cs := connect(t, NewServer(Deps{Refresh: c}))
	listed, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range listed.Tools {
		if tool.Name != "refresh_pages" {
			continue
		}
		found = true
		raw, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Type       string
			Properties map[string]any
			Required   []string
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		if schema.Type != "object" || len(schema.Properties) != 0 || len(schema.Required) != 0 {
			t.Fatalf("not argument-free: %s", raw)
		}
	}
	if !found {
		t.Fatal("refresh_pages absent")
	}
	for i := 1; i <= 2; i++ {
		result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "refresh_pages", Arguments: map[string]any{}})
		if err != nil || result.IsError {
			t.Fatalf("call: %v %+v", err, result)
		}
		var got struct {
			Requested bool
			Revision  int
		}
		if err := json.Unmarshal([]byte(toolText(t, result)), &got); err != nil {
			t.Fatal(err)
		}
		if !got.Requested || got.Revision != i {
			t.Fatalf("result %+v", got)
		}
		w := httptest.NewRecorder()
		c.ServeHTTP(w, httptest.NewRequest("GET", "/api/ui-refresh", nil))
		var state uirefresh.State
		json.Unmarshal(w.Body.Bytes(), &state)
		if state.Revision != uint64(i) {
			t.Fatal("tool and HTTP not sharing coordinator")
		}
	}
}
