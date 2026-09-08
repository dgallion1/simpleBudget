// Promoted from DI5 attempt2 adversarial lane:
// /tmp/DI5-second-prep.vQcE3l/cmd/server/di5_second_cross_money_test.go
package main

import (
	"budget2/internal/config"
	"budget2/internal/services/storage"
	"budget2/internal/testutil"
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Independent assembled-router + assembled-MCP probe: combines fractional
// component arithmetic, split months/merchants, top-N and date sentinels.
func TestDI5SecondCrossMoney(t *testing.T) {
	for _, tc := range []struct {
		name, income, charge  string
		incomeC, spendC, netC int
	}{
		{"split charges", "10.006", "-0.004", 1001, 1, 1000},
		{"split credits", "10.006", "0.004", 1001, -1, 1002},
		{"tie", "2.675", "-0.004", 267, 1, 266},
		{"displayed neutral", "0.008", "-0.004", 1, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := t.TempDir()
			root := testutil.ProjectRoot()
			rows := fmt.Sprintf("Date,Description,Amount,Category\n2019-12-31,Outside Shop,-999,Home\n2020-01-01,PAYCHECK,%s,Income\n2020-01-31,Alder Store Credit,%s,Home\n2020-02-01,Birch Store Credit,%s,Shopping\n2020-03-01,Outside Shop,-999,Home\n", tc.income, tc.charge, tc.charge)
			if err := os.WriteFile(filepath.Join(data, "synthetic.csv"), []byte(rows), 0600); err != nil {
				t.Fatal(err)
			}
			c := &config.Config{ListenAddr: ":0", Debug: true, DataDirectory: data, UploadsDirectory: filepath.Join(data, "uploads"), SettingsDirectory: filepath.Join(data, "settings"), TemplatesDirectory: filepath.Join(root, "web/templates"), StaticDirectory: filepath.Join(root, "web/static"), BackupDir: t.TempDir(), ImportDirectory: t.TempDir()}
			var err error
			store, err = storage.New(data)
			if err != nil {
				t.Fatal(err)
			}
			if err = SetupDependencies(c); err != nil {
				t.Fatal(err)
			}
			router := SetupRouter()
			money := func(n int) string {
				sign := ""
				if n < 0 {
					sign = "-"
					n = -n
				}
				return fmt.Sprintf("%s$%d.%02d", sign, n/100, n%100)
			}
			q := "?start=2020-01-01&end=2020-02-29"
			for _, surface := range []string{"/dashboard", "/dashboard/kpis", "/insights", "HX"} {
				path := surface
				if path == "HX" {
					path = "/insights"
				}
				r := httptest.NewRequest("GET", path+q, nil)
				if surface == "HX" {
					r.Header.Set("HX-Request", "true")
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				if w.Code != 200 {
					t.Fatal(w.Code)
				}
				body := w.Body.String()
				wants := map[string]int{"Recorded income": tc.incomeC, "Net spending": tc.spendC, "Cash-flow balance": tc.netC}
				if path == "/insights" {
					wants = map[string]int{"Selected period:": tc.spendC}
				}
				for label, want := range wants {
					// RF2 (2026-09-07, ruling RF-2026-09-07b) added a
					// #dashboard-lead sentence that also starts with the
					// literal text "Recorded income" and renders BEFORE the
					// Income tile. Anchor on the tile's own heading
					// ("Recorded income</h2>") so this always captures the
					// Income TILE's figure regardless of what the lead says,
					// instead of whichever "Recorded income..." text happens
					// to come first in the document.
					anchor := label
					if label == "Recorded income" {
						anchor = "Recorded income</h2>"
					}
					m := regexp.MustCompile("(?s)" + regexp.QuoteMeta(anchor) + `.*?(-?\$[0-9,]+\.[0-9]{2})`).FindStringSubmatch(body)
					if len(m) != 2 || m[1] != money(want) {
						t.Fatalf("%s %s %v want %s", surface, label, m, money(want))
					}
				}
				if path != "/insights" && tc.netC == 0 && (!strings.Contains(body, "Recorded income matches spending") || strings.Contains(body, "Recorded income above spending") || strings.Contains(body, "Spending not covered by recorded income")) {
					t.Fatal("non-neutral displayed zero")
				}
				// RF2 cross-surface money oracle (ruling RF-2026-09-07b):
				// #dashboard-lead's own money figure(s) must agree with the
				// Cash-flow balance tile ON THE SAME PAGE -- both surfaces
				// render from the SAME $flow.Balance, so they can never be
				// allowed to drift apart. /insights (and the HX request,
				// which also targets /insights) carries no #dashboard-lead.
				if path != "/insights" {
					leadMatch := regexp.MustCompile(`(?s)id="dashboard-lead"[^>]*>(.*?)</p>`).FindStringSubmatch(body)
					if leadMatch == nil {
						t.Fatalf("%s: expected #dashboard-lead in body", surface)
					}
					leadText := leadMatch[1]
					cfMatch := regexp.MustCompile(`(?s)Cash-flow balance</h2>.*?(-?\$[0-9,]+\.[0-9]{2})`).FindStringSubmatch(body)
					if cfMatch == nil {
						t.Fatalf("%s: could not locate Cash-flow balance tile figure", surface)
					}
					cfFigure := strings.TrimPrefix(cfMatch[1], "-")
					switch {
					case strings.Contains(leadText, "did not cover spending by") || strings.Contains(leadText, "exceeded spending by"):
						moneyMatch := regexp.MustCompile(`<span class="num">(\$[0-9,]+\.[0-9]{2})</span>\.`).FindStringSubmatch(leadText)
						if moneyMatch == nil {
							t.Fatalf("%s: could not find the balance money span in lead %q", surface, leadText)
						}
						if moneyMatch[1] != cfFigure {
							t.Fatalf("%s: lead balance figure %s does not equal Cash-flow tile figure %s (sign-normalized); lead=%q", surface, moneyMatch[1], cfFigure, leadText)
						}
					case strings.Contains(leadText, "matched spending"):
						if cfFigure != "$0.00" {
							t.Fatalf("%s: lead says \"matched spending\" but Cash-flow tile shows %s, want $0.00", surface, cfFigure)
						}
					default:
						t.Fatalf("%s: #dashboard-lead did not match any expected cash-flow phrase: %q", surface, leadText)
					}
				}
			}
			st, ct := mcp.NewInMemoryTransports()
			ss, err := mcpServer.Connect(context.Background(), st, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "second", Version: "1"}, nil).Connect(context.Background(), ct, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			for _, top := range []int{1, 10} {
				res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "summarize_spending", Arguments: map[string]any{"start_date": "2020-01-01", "end_date": "2020-02-29", "top_n": top}})
				if err != nil || res.IsError {
					t.Fatalf("tool %v %v", res, err)
				}
				b, err := json.Marshal(res.StructuredContent)
				if err != nil {
					t.Fatal(err)
				}
				var out struct {
					Income     float64 `json:"total_income"`
					Spend      float64 `json:"total_expenses"`
					Net        float64 `json:"net_savings"`
					Adjustment float64 `json:"monthly_rounding_adjustment"`
					Months     []struct {
						Month  string
						Amount float64
					} `json:"by_month"`
				}
				if err = json.Unmarshal(b, &out); err != nil {
					t.Fatal(err)
				}
				if out.Income != float64(tc.incomeC)/100 || out.Spend != float64(tc.spendC)/100 || out.Net != float64(tc.netC)/100 {
					t.Fatalf("MCP/UI disagree top=%d %s", top, b)
				}
				if len(out.Months) != 2 || out.Months[0].Month != "2020-01" || out.Months[1].Month != "2020-02" {
					t.Fatalf("missing/borrowed month %s", b)
				}
				sum := int(math.Round(out.Adjustment * 100))
				for _, m := range out.Months {
					sum += int(math.Round(m.Amount * 100))
					if m.Amount == 0 && math.Signbit(m.Amount) {
						t.Fatal("negative zero")
					}
				}
				if sum != tc.spendC {
					t.Fatalf("residual sum %d want %d", sum, tc.spendC)
				}
			}
			t.Logf("four HTTP surfaces plus assembled MCP top1/top10: income=%s spending=%s balance=%s; two exact months reconcile", money(tc.incomeC), money(tc.spendC), money(tc.netC))
		})
	}
}
