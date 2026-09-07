package main

import (
	"budget2/internal/config"
	"budget2/internal/services/storage"
	"budget2/internal/testutil"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestDI5SameDateEdgeMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, start, end, rows, want string
		history                      bool
	}{
		{"historical", "2020-07-01", "2020-07-31", "2020-06-01,PAYCHECK,10,Income\n2020-06-15,Grocer,-20,Food\n2020-07-10,Grocer,-30,Food\n2020-07-31,PAYCHECK,40,Income\n", "$30.00", true},
		{"custom refund", "2020-07-10", "2020-07-20", "2020-06-01,PAYCHECK,10,Income\n2020-07-10,Grocer,-100,Food\n2020-07-20,Boutique Store Credit,150,Food\n", "-$50.00", true},
		{"incomplete month", "2020-08-01", "2020-08-28", "2020-07-01,PAYCHECK,10,Income\n2020-08-28,Grocer,-12.34,Food\n", "$12.34", true},
		{"no history", "2020-08-01", "2020-08-28", "2020-08-28,Grocer,-12.34,Food\n", "$12.34", false},
		{"empty selection", "2020-08-01", "2020-08-28", "2020-06-01,Grocer,-999,Food\n", "$0.00", false},
		{"empty data", "2020-08-01", "2020-08-28", "", "$0.00", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := t.TempDir()
			root := testutil.ProjectRoot()
			if err := os.WriteFile(filepath.Join(data, "synthetic.csv"), []byte("Date,Description,Amount,Category\n"+tc.rows), 0600); err != nil {
				t.Fatal(err)
			}
			c := &config.Config{ListenAddr: ":0", Debug: true, DataDirectory: data, UploadsDirectory: filepath.Join(data, "uploads"), SettingsDirectory: filepath.Join(data, "settings"), TemplatesDirectory: filepath.Join(root, "web/templates"), StaticDirectory: filepath.Join(root, "web/static"), BackupDir: t.TempDir(), ImportDirectory: t.TempDir()}
			var err error
			store, err = storage.New(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := SetupDependencies(c); err != nil {
				t.Fatal(err)
			}
			router := SetupRouter()
			q := "?start=" + tc.start + "&end=" + tc.end
			for _, surface := range []string{"dashboard", "kpis", "insights", "HX"} {
				url := "/dashboard" + q
				if surface == "kpis" {
					url = "/dashboard/kpis" + q
				}
				if surface == "insights" || surface == "HX" {
					url = "/insights" + q
				}
				req := httptest.NewRequest("GET", url, nil)
				if surface == "HX" {
					req.Header.Set("HX-Request", "true")
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if w.Code != 200 {
					t.Fatalf("%s status %d", surface, w.Code)
				}
				label := "Net spending"
				if surface == "insights" || surface == "HX" {
					label = "Selected period:"
				}
				matches := regexp.MustCompile("(?s)" + regexp.QuoteMeta(label) + `.*?(-?\$[0-9,]+\.[0-9]{2})`).FindStringSubmatch(w.Body.String())
				if len(matches) != 2 || matches[1] != tc.want {
					t.Fatalf("%s displayed net %v want %s", surface, matches, tc.want)
				}
				if !strings.Contains(w.Body.String(), tc.start) || !strings.Contains(w.Body.String(), tc.end) {
					t.Fatalf("%s lost selected bounds", surface)
				}
				if surface == "insights" || surface == "HX" {
					missing := strings.Contains(w.Body.String(), "Not enough history to compare")
					if missing == tc.history {
						t.Fatalf("%s history=%v missing=%v", surface, tc.history, missing)
					}
				}
			}
			st, ct := mcp.NewInMemoryTransports()
			ss, err := mcpServer.Connect(context.Background(), st, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			client := mcp.NewClient(&mcp.Implementation{Name: "DI5-edges", Version: "1"}, nil)
			cs, err := client.Connect(context.Background(), ct, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			summary, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "summarize_spending", Arguments: map[string]any{"start_date": tc.start, "end_date": tc.end}})
			if err != nil || summary.IsError {
				t.Fatalf("summary %v %v", summary, err)
			}
			encoded, err := json.Marshal(summary.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			var totals struct {
				Expenses float64 `json:"total_expenses"`
			}
			if err := json.Unmarshal(encoded, &totals); err != nil {
				t.Fatal(err)
			}
			want, err := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(tc.want, "$", ""), ",", ""), 64)
			if err != nil {
				t.Fatal(err)
			}
			if totals.Expenses != want {
				t.Fatalf("MCP net=%v UI net=%s", totals.Expenses, tc.want)
			}
			result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_trends", Arguments: map[string]any{"start_date": tc.start, "end_date": tc.end}})
			if err != nil || result.IsError {
				t.Fatalf("tool %v %v", result, err)
			}
			b, err := json.Marshal(result.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			var out struct {
				Start, End string
				Period     struct {
					History  bool `json:"history_available"`
					Forecast bool `json:"forecast_available"`
				}
			}
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatal(err)
			}
			if out.Start != tc.start || out.End != tc.end || out.Period.History != tc.history || out.Period.Forecast {
				t.Fatalf("MCP bounds/history/forecast: %s", b)
			}
			t.Logf("%s full/partial/Insights/HX net=%s; tool same-date history=%v; historical forecast unavailable", tc.name, tc.want, tc.history)
		})
	}
}
