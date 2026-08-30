#!/usr/bin/env bash
# Oracle for HC1 — healthcare budget prorated from the earliest Health
# Insurance bill (HC-RUN-SPEC.md). Run from the ROOT of the tree under test:
#   bash .swarm/tier3/HC1/accept.sh | tee .swarm/tier3/HC1/oracle.<attempt>.log
# Emits ORACLE PASS as the final line only when every check passes.
#
# Stage 1 plants a contract test against the pinned exported API in
# internal/services/metrics (removed on exit). Stage 2 builds the server,
# runs it on 127.0.0.1:18093 (NEVER :8080 — the binary kills whatever owns
# its port) against the fixture data dir, and asserts on the rendered
# output of every consumer: KPI partial (Budget card, Healthcare card,
# verdict bar), budget-vs-actual chart JSON, and MCP summarize_spending.
#
# Fixture arithmetic (window 2026-01-01..2026-06-30, targets 1000/1000):
#   monthsInRange   = 181/30.4375 = 5.94682  -> living target  5,946.82
#   coverageMonths  =  57/30.4375 = 1.87269  -> health target  1,872.69
#   living spend 7200 (+1,253 over). Health spend: the CSV loader types the
#   +50 refund row as Income, so the HTTP surfaces see hcTotal 1,800
#   (-72.69 under vs clipped target; combined +1,180.70 OVER rendered);
#   the planted metrics test constructs the refund as outflow-typed, so at
#   that level hcTotal nets to 1,750. Master (unclipped): ~-2,900 UNDER.
#   chart flat target line fixed: 7,819.51/5.94682 = 1,314.90 (master 2,000).
# Calibrated 2026-08-30: fail-end on master (all consumer checks fail on
# unclipped figures); pass-end via throwaway metrics prototype (contract
# tests green) — the prototype also proved budget-card-clipped-total and
# healthcare-card-since reject a "suppress healthcare entirely" shortcut.
set -u
ORACLE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PORT=18093
BASE="http://127.0.0.1:$PORT"
PASSN=0; FAILN=0
ok()  { echo "CHECK $1: PASS"; PASSN=$((PASSN+1)); }
bad() { echo "CHECK $1: FAIL${2:+ ($2)}"; FAILN=$((FAILN+1)); }
ckgrep() { # name pattern file
  if grep -Eq "$2" "$3"; then ok "$1"; else bad "$1" "pattern '$2' not found"; fi
}

WORK=$(mktemp -d)
SRV_PID=""
PLANTED=internal/services/metrics/zz_oracle_hc1_test.go
cleanup() {
  [[ -n "$SRV_PID" ]] && kill "$SRV_PID" 2>/dev/null
  rm -f "$PLANTED"
  rm -rf "$WORK"
}
trap cleanup EXIT

# --- Stage 0: build ---------------------------------------------------------
if go build ./... >"$WORK/build.log" 2>&1; then
  ok build
else
  bad build "go build failed"; tail -20 "$WORK/build.log"
  echo "ORACLE FAIL"; exit 1
fi

# --- Stage 1: planted contract test ------------------------------------------
cat >"$PLANTED" <<'GO'
package metrics

import (
	"math"
	"testing"
	"time"

	"budget2/internal/models"
)

func hc1d(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

func hc1near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.IsNaN(got) || math.Abs(got-want) > 0.005 {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func hc1living() []models.Transaction {
	var tx []models.Transaction
	for m := 1; m <= 6; m++ {
		tx = append(tx, models.Transaction{
			Date: time.Date(2026, time.Month(m), 15, 0, 0, 0, 0, time.UTC),
			Amount: -1200, Description: "GROC", Category: "Groceries",
			TransactionType: models.Outflow,
		})
	}
	return tx
}

func hc1full() *models.TransactionSet {
	tx := hc1living()
	tx = append(tx,
		models.Transaction{Date: hc1d("2026-01-02"), Amount: 100, Description: "DEP",
			Category: "Paycheck", TransactionType: models.Income},
		// refund: positive amount, outflow-typed, EARLIER than any premium —
		// must never define coverage start
		models.Transaction{Date: hc1d("2026-03-01"), Amount: 50, Description: "HEALTH INS REFUND",
			Category: HealthInsuranceCategory, TransactionType: models.Outflow},
		models.Transaction{Date: hc1d("2026-05-05"), Amount: -900, Description: "HEALTH INS",
			Category: HealthInsuranceCategory, TransactionType: models.Outflow},
		models.Transaction{Date: hc1d("2026-06-05"), Amount: -900, Description: "HEALTH INS",
			Category: HealthInsuranceCategory, TransactionType: models.Outflow},
	)
	return models.NewTransactionSet(tx)
}

func TestOracleHC1CoverageStartEarliestBill(t *testing.T) {
	start, okc := HealthcareCoverageStart(hc1full())
	if !okc {
		t.Fatal("expected coverage start, got ok=false")
	}
	if !start.Equal(hc1d("2026-05-05")) {
		t.Errorf("coverage start = %v, want 2026-05-05 (earliest negative bill; refund must not count)", start)
	}
}

func TestOracleHC1CoverageStartNoneWithoutBills(t *testing.T) {
	tx := hc1living()
	tx = append(tx, models.Transaction{Date: hc1d("2026-03-01"), Amount: 50,
		Description: "HEALTH INS REFUND", Category: HealthInsuranceCategory,
		TransactionType: models.Outflow})
	if _, okc := HealthcareCoverageStart(models.NewTransactionSet(tx)); okc {
		t.Error("refund-only set must yield ok=false")
	}
}

func TestOracleHC1CalculateClipsHealthcareAccrual(t *testing.T) {
	rs, re := hc1d("2026-01-01"), hc1d("2026-06-30")
	cov := hc1d("2026-05-05")
	m := Calculate(hc1full(), rs, re, 1000, 1000, cov, true)
	mm := MonthsBetween(rs, re)
	cm := MonthsBetween(cov, re)
	if !m.HasHealthcareTarget {
		t.Fatal("HasHealthcareTarget = false, want true")
	}
	// healthcareTotal nets the +50 outflow-typed refund: 1800 - 50 = 1750
	// (existing SumAmount behavior, out of HC1 scope to change).
	hc1near(t, "HealthcareTargetTotal", m.HealthcareTargetTotal, 1000*cm)
	hc1near(t, "HealthcareActual", m.HealthcareActual, 1750/cm)
	hc1near(t, "HealthcareCumulativeDelta", m.HealthcareCumulativeDelta, 1750-1000*cm)
	hc1near(t, "CombinedCumulativeDelta", m.CombinedCumulativeDelta,
		(7200-1000*mm)+(1750-1000*cm))
	if m.CombinedCumulativeDelta <= 0 {
		t.Errorf("CombinedCumulativeDelta = %v, want > 0 (over budget)", m.CombinedCumulativeDelta)
	}
}

func TestOracleHC1CoverageAfterWindowSuppresses(t *testing.T) {
	rs, re := hc1d("2026-01-01"), hc1d("2026-06-30")
	ts := models.NewTransactionSet(hc1living())
	m := Calculate(ts, rs, re, 1000, 1000, hc1d("2027-01-01"), true)
	if m.HasHealthcareTarget {
		t.Error("coverage after window: HasHealthcareTarget must be false")
	}
	for name, v := range map[string]float64{
		"HealthcareActual":        m.HealthcareActual,
		"HealthcareTargetTotal":   m.HealthcareTargetTotal,
		"CombinedCumulativeDelta": m.CombinedCumulativeDelta,
	} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("%s is NaN/Inf", name)
		}
	}
	mm := MonthsBetween(rs, re)
	hc1near(t, "CombinedCumulativeDelta(living-only)", m.CombinedCumulativeDelta, 7200-1000*mm)
}

func TestOracleHC1NoCoverageFlagSuppresses(t *testing.T) {
	rs, re := hc1d("2026-01-01"), hc1d("2026-06-30")
	ts := models.NewTransactionSet(hc1living())
	m := Calculate(ts, rs, re, 1000, 1000, time.Time{}, false)
	if m.HasHealthcareTarget {
		t.Error("hasCoverage=false: HasHealthcareTarget must be false")
	}
	mm := MonthsBetween(rs, re)
	hc1near(t, "CombinedCumulativeDelta(living-only)", m.CombinedCumulativeDelta, 7200-1000*mm)
}

func TestOracleHC1CoverageBeforeWindowUnchanged(t *testing.T) {
	rs, re := hc1d("2026-01-01"), hc1d("2026-06-30")
	m := Calculate(hc1full(), rs, re, 1000, 1000, hc1d("2025-12-01"), true)
	mm := MonthsBetween(rs, re)
	hc1near(t, "HealthcareTargetTotal(full-window)", m.HealthcareTargetTotal, 1000*mm)
}
GO

if go test ./internal/services/metrics/ -run 'TestOracleHC1' -count=1 >"$WORK/unit.log" 2>&1; then
  ok contract-tests
else
  bad contract-tests "see tail below"
  tail -30 "$WORK/unit.log"
fi

# --- Stage 2: fixture server, every rendered consumer -------------------------
cp -a "$ORACLE_DIR/fixture/data" "$WORK/data"
if ! go build -o "$WORK/hc1-server" ./cmd/server >"$WORK/sbuild.log" 2>&1; then
  bad server-build "go build ./cmd/server failed"; tail -10 "$WORK/sbuild.log"
  echo "ORACLE FAIL"; exit 1
fi
BUDGET_LISTEN_ADDR="127.0.0.1:$PORT" BUDGET_DATA_DIR="$WORK/data" \
  "$WORK/hc1-server" >"$WORK/server.log" 2>&1 &
SRV_PID=$!
up=0
for _ in $(seq 1 50); do
  if curl -s -o /dev/null "$BASE/dashboard"; then up=1; break; fi
  sleep 0.2
done
if [[ "$up" != 1 ]]; then
  bad server-up "server did not answer on $PORT"; tail -10 "$WORK/server.log"
  echo "ORACLE FAIL"; exit 1
fi
ok server-up

RANGE="start=2026-01-01&end=2026-06-30"
curl -s "$BASE/dashboard/kpis?$RANGE" | tr '\n' ' ' >"$WORK/kpis.flat"

# Budget card: health target total must be the CLIPPED 1,872.xx, with the
# actual 1,800.00 beside it (master renders 5,946.xx and fails here).
ckgrep budget-card-clipped-total 'Health:.*1,(750|800)\.00.*of.*1,87[0-9]\.[0-9]{2}' "$WORK/kpis.flat"
# Verdict bar: combined must be OVER plan on this fixture.
ckgrep verdict-over-plan 'over plan for this period' "$WORK/kpis.flat"
# Healthcare card provenance: coverage start visible.
ckgrep healthcare-card-since 'since[^<]*May[^<]*5[^<]*2026' "$WORK/kpis.flat"

# Chart JSON: combined-target shape must reflect the clipped accrual
# (< 1500/mo average vs ~1982 unclipped), and the cumulative balance must
# END NEGATIVE (over budget) — master ends positive.
curl -s "$BASE/dashboard/charts/data/budget-vs-actual?$RANGE" >"$WORK/chart.json"
if python3 - "$WORK/chart.json" <<'PY' >"$WORK/chart.verdict" 2>&1
import json, sys
d = json.load(open(sys.argv[1]))
shapes = d.get("layout", {}).get("shapes", [])
assert shapes, "no target shape"
y = shapes[0]["y0"]
assert y < 1500, f"target line {y} not clipped"
cum = [s for s in d["data"] if s.get("name") == "Cumulative balance"]
assert cum, "no cumulative series"
last = cum[0]["y"][-1]
assert last < 0, f"cumulative balance ends {last}, want negative (over budget)"
print("chart ok")
PY
then ok chart-clipped-target; else bad chart-clipped-target "$(tail -1 "$WORK/chart.verdict")"; fi

# MCP summarize_spending: combined_cumulative_delta must be POSITIVE (over).
SID=$(curl -s -D- -o /dev/null -X POST "$BASE/mcp" \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"oracle","version":"0"}}}' \
  | grep -i mcp-session-id | tr -d '\r' | awk '{print $2}')
curl -s -X POST "$BASE/mcp" \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"summarize_spending","arguments":{"start_date":"2026-01-01","end_date":"2026-06-30"}}}' \
  >"$WORK/mcp.out"
if python3 - "$WORK/mcp.out" <<'PY' >"$WORK/mcp.verdict" 2>&1
import json, re, sys
raw = open(sys.argv[1]).read()
m = re.search(r'combined_cumulative_delta\\?":\s*(-?[0-9.]+)', raw)
assert m, "combined_cumulative_delta not found"
v = float(m.group(1))
assert v > 0, f"combined_cumulative_delta {v}, want > 0 (over budget)"
print("mcp ok")
PY
then ok mcp-combined-over; else bad mcp-combined-over "$(tail -1 "$WORK/mcp.verdict")"; fi

kill "$SRV_PID" 2>/dev/null; wait "$SRV_PID" 2>/dev/null; SRV_PID=""

# --- Stage 3: no-coverage variant (added attempt 2, checker-second finding) ---
# Same plan (healthcare target 1000 configured) but ZERO Health Insurance
# transactions: every surface must suppress the healthcare budget — no
# phantom "under". The MCP budget block previously copied
# HealthcareTarget/Delta without gating on HasHealthcareTarget.
mkdir -p "$WORK/data2/settings"
grep -v 'Health Insurance' "$ORACLE_DIR/fixture/data/transactions.csv" >"$WORK/data2/transactions.csv"
cp "$ORACLE_DIR/fixture/data/settings/whatif.json" "$WORK/data2/settings/whatif.json"
BUDGET_LISTEN_ADDR="127.0.0.1:$PORT" BUDGET_DATA_DIR="$WORK/data2" \
  "$WORK/hc1-server" >"$WORK/server2.log" 2>&1 &
SRV_PID=$!
up=0
for _ in $(seq 1 50); do
  if curl -s -o /dev/null "$BASE/dashboard"; then up=1; break; fi
  sleep 0.2
done
if [[ "$up" != 1 ]]; then
  bad server2-up "no-coverage server did not answer on $PORT"; tail -10 "$WORK/server2.log"
  echo "ORACLE FAIL"; exit 1
fi
ok server2-up

SID=$(curl -s -D- -o /dev/null -X POST "$BASE/mcp" \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"oracle","version":"0"}}}' \
  | grep -i mcp-session-id | tr -d '\r' | awk '{print $2}')
curl -s -X POST "$BASE/mcp" \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"summarize_spending","arguments":{"start_date":"2026-01-01","end_date":"2026-06-30"}}}' \
  >"$WORK/mcp2.out"
if python3 - "$WORK/mcp2.out" <<'PY' >"$WORK/mcp2.verdict" 2>&1
import re, sys
raw = open(sys.argv[1]).read()
def num(field):
    m = re.search(field + r'\\?":\s*(-?[0-9.]+)', raw)
    assert m, field + " not found"
    return float(m.group(1))
t = num("healthcare_monthly_target")
d = num("healthcare_monthly_delta")
assert t == 0, f"healthcare_monthly_target {t}, want 0 (no coverage => no phantom budget)"
assert d == 0, f"healthcare_monthly_delta {d}, want 0 (no coverage => no phantom credit)"
print("mcp no-coverage ok")
PY
then ok mcp-no-coverage-suppressed; else bad mcp-no-coverage-suppressed "$(tail -1 "$WORK/mcp2.verdict")"; fi

# Dashboard on the same no-coverage data: no "under" credit in the Budget
# card's Health line (the line must be absent) and page renders (no NaN).
curl -s "$BASE/dashboard/kpis?start=2026-01-01&end=2026-06-30" | tr '\n' ' ' >"$WORK/kpis2.flat"
if grep -Eq 'Health:' "$WORK/kpis2.flat"; then
  bad dashboard-no-coverage-suppressed "Health budget line rendered without coverage"
else
  ok dashboard-no-coverage-suppressed
fi
if grep -Eq 'NaN' "$WORK/kpis2.flat"; then
  bad dashboard-no-coverage-nan "NaN rendered"
else
  ok dashboard-no-coverage-nan
fi

kill "$SRV_PID" 2>/dev/null; wait "$SRV_PID" 2>/dev/null; SRV_PID=""

# --- Stage 4: derivation-basis discriminators (added attempt 3) --------------
# data3: an ACTIVE premium before the window (2025-12-20). Correct code
# derives coverage from the FULL ledger -> clips to rangeStart -> full-window
# accrual "of 5,94x.xx". The window-derived mutation sees May 5 first and
# renders "of 1,87x.xx".
# data4: same pre-window premium but duplicate-SUPPRESSED (kept twin is
# categorized outside Health Insurance). Correct code derives from the
# post-duplicate-resolution set -> May 5 -> "of 1,87x.xx". The
# duplicates-included mutation sees Dec 20 -> "of 5,94x.xx".
run_variant() { # name datadir grep_pattern
  local name="$1" datadir="$2" pat="$3"
  rm -rf "$WORK/$name"
  cp -a "$datadir" "$WORK/$name"
  mkdir -p "$WORK/$name/settings"
  cp "$ORACLE_DIR/fixture/data/settings/whatif.json" "$WORK/$name/settings/whatif.json"
  BUDGET_LISTEN_ADDR="127.0.0.1:$PORT" BUDGET_DATA_DIR="$WORK/$name" \
    "$WORK/hc1-server" >"$WORK/$name.server.log" 2>&1 &
  SRV_PID=$!
  local vup=0
  for _ in $(seq 1 50); do
    if curl -s -o /dev/null "$BASE/dashboard"; then vup=1; break; fi
    sleep 0.2
  done
  if [[ "$vup" != 1 ]]; then
    bad "$name-up" "server did not answer"; tail -5 "$WORK/$name.server.log"
  else
    curl -s "$BASE/dashboard/kpis?$RANGE" | tr '\n' ' ' >"$WORK/$name.kpis.flat"
    ckgrep "$name" "$pat" "$WORK/$name.kpis.flat"
  fi
  kill "$SRV_PID" 2>/dev/null; wait "$SRV_PID" 2>/dev/null; SRV_PID=""
}

if [[ ! -f "$ORACLE_DIR/fixture/data4/duplicate_decisions.json" ]]; then
  bad stage4-fixture "fixture/data4/duplicate_decisions.json missing (calibration incomplete)"
else
  run_variant full-ledger-prewindow-accrual "$ORACLE_DIR/fixture/data3" \
    'Health:.*1,800\.00.*of.*5,94[0-9]\.[0-9]{2}'
  run_variant duplicates-excluded-derivation "$ORACLE_DIR/fixture/data4" \
    'Health:.*1,800\.00.*of.*1,87[0-9]\.[0-9]{2}'
fi

# --- Verdict ------------------------------------------------------------------
echo "checks: $PASSN passed, $FAILN failed"
if [[ "$FAILN" -eq 0 ]]; then
  echo "ORACLE PASS"
else
  echo "ORACLE FAIL"
  exit 1
fi
