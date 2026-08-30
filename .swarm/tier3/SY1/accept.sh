#!/usr/bin/env bash
# Oracle for SY1 — plan-modeled major-expense groups excluded from the
# what-if sync's living-expense average (SY-RUN-SPEC.md). Run from the ROOT
# of the tree under test:
#   bash .swarm/tier3/SY1/accept.sh | tee .swarm/tier3/SY1/oracle.<attempt>.log
# Emits ORACLE PASS as the final line only when every check passes.
#
# Stage 1 plants a contract test in internal/services/majorexpenses pinning
# the exported classifier signature + first-def-wins semantics (removed on
# exit). Stage 2 generates a fixture data dir with RUN-TIME dates (the sync
# window is time.Now()-relative), runs the server on 127.0.0.1:18094 (never
# :8080), and asserts on the rendered sync preview AND the value the apply
# path actually saves.
#
# Fixture arithmetic (dates today-270d .. today-30d, so months ~= 9.0 by the
# engine's days/30 rule; tolerance 5.0 absorbs DST-hour skew):
#   groceries 9 x -1000            stay in living
#   CAR LOAN  4 x -500.00          flagged def (amount-only) -> excluded
#   CAR LOAN REVERSAL +500.00      outflow-typed refund in the flagged
#                                  group -> excluded from living, and NETS
#                                  the displayed group total (SY-2026-08-30a)
#   GYM       1 x -500.00          matches EARLIER unflagged keyword def ->
#                                  MUST stay in living (first-def-wins trap)
#   HEALTH INS PREMIUM -900        HI category -> excluded (existing rule)
#   HEALTH INS SPECIAL -500.00     HI category AND flagged amount ->
#                                  excluded ONCE as HI; NOT in group total
#   expected NewMonthlyExpenses  = 9500/months  (~1055.56)
#   expected Car loan group      = NET 1500/months (~166.67), count 5
#   master (flag dropped by json) = 11000/months (~1222.22) -> fail end
#   flagged-def-list filtering    =  9000/months (~1000.00) -> caught
#   gross-abs group total defect  =  2500/months (~277.78)  -> caught
#   overlap counted into group    =  inflates monthly       -> caught
# Calibrated 2026-08-30 (v1): fail end on clean `git archive master`; pass
# end on throwaway prototype (discarded). Extended 2026-08-30 (v2, after
# rulings SY-2026-08-30a/b): refund row + net-total check added; both ends
# re-validated — see SY-RUN-SPEC.md calibration record.
set -u

PORT=18094
BASE="http://127.0.0.1:$PORT"
PASSN=0; FAILN=0
ok()  { echo "CHECK $1: PASS"; PASSN=$((PASSN+1)); }
bad() { echo "CHECK $1: FAIL${2:+ ($2)}"; FAILN=$((FAILN+1)); }

WORK=$(mktemp -d)
SRV_PID=""
PLANTED=internal/services/majorexpenses/zz_oracle_sy1_test.go
cleanup() {
  [[ -n "$SRV_PID" ]] && kill "$SRV_PID" 2>/dev/null && wait "$SRV_PID" 2>/dev/null
  rm -f "$PLANTED"
  rm -rf "$WORK"
}
trap cleanup EXIT

# --- Stage 1: planted contract test ------------------------------------------
cat >"$PLANTED" <<'GO'
package majorexpenses

import (
	"testing"

	"budget2/internal/models"
)

// Pin the exported classifier signature (SY-RUN-SPEC.md SY1 criterion 2).
var _ func(*models.TransactionSet, []models.MajorExpense, map[string]string) map[string]models.MajorExpense = ComputePlanSyncExclusions

func sy1fixture() (*models.TransactionSet, []models.MajorExpense) {
	defs := []models.MajorExpense{
		{ID: "gym", Name: "Gym", Keywords: []string{"GYM"}},
		{ID: "car", Name: "Car loan", ExpectedMin: 500, ExpectedMax: 500, ExcludeFromPlanSync: true},
	}
	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-gym", Description: "GYM MEMBERSHIP", Amount: -500, TransactionType: models.Outflow},
		{Hash: "h-car1", Description: "CAR LOAN PAYMENT", Amount: -500, TransactionType: models.Outflow},
		{Hash: "h-groc", Description: "WALMART GROCERY", Amount: -1000, TransactionType: models.Outflow},
	})
	return ts, defs
}

func TestOracleSY1FirstDefWins(t *testing.T) {
	ts, defs := sy1fixture()
	ex := ComputePlanSyncExclusions(ts, defs, nil)
	if _, wrong := ex["h-gym"]; wrong {
		t.Error("GYM row matches the EARLIER unflagged def; it must not be excluded (D-SY-b)")
	}
	if d, okx := ex["h-car1"]; !okx || d.ID != "car" {
		t.Errorf("car-loan row: want excluded by def car, got ok=%v def=%+v", okx, d)
	}
	if _, wrong := ex["h-groc"]; wrong {
		t.Error("grocery row must not be excluded")
	}
}

func TestOracleSY1NilSafe(t *testing.T) {
	if got := ComputePlanSyncExclusions(nil, nil, nil); len(got) != 0 {
		t.Errorf("nil inputs: want empty map, got %d entries", len(got))
	}
}
GO

if go test ./internal/services/majorexpenses/ -run 'TestOracleSY1' -count=1 >"$WORK/unit.log" 2>&1; then
  ok contract-tests
else
  bad contract-tests "see tail below"
  tail -20 "$WORK/unit.log"
fi

# --- Stage 2: fixture server, sync preview + apply ----------------------------
mkdir -p "$WORK/data/settings"
D() { date -d "today -$1 days" +%F; }
{
  echo "Date,Description,Amount,Category"
  for k in 1 2 3 4 5 6 7 8 9; do
    echo "$(D $((k*30))),WALMART GROCERY,-1000.00,Groceries"
  done
  for k in 5 6 7 8; do
    echo "$(D $((k*30+15))),CAR LOAN PAYMENT,-500.00,Auto Payment"
  done
  echo "$(D 45),CAR LOAN PAYMENT REVERSAL,500.00,Auto Payment"
  echo "$(D 100),GYM MEMBERSHIP,-500.00,Health & Fitness"
  echo "$(D 90),HEALTH INS PREMIUM,-900.00,Health Insurance"
  echo "$(D 60),HEALTH INS SPECIAL,-500.00,Health Insurance"
} >"$WORK/data/transactions.csv"

cat >"$WORK/data/major_expenses.json" <<'JSON'
{
  "expenses": [
    { "id": "sy1-gym", "name": "Gym", "keywords": ["GYM"], "expected_min": 0, "expected_max": 0 },
    { "id": "sy1-carloan", "name": "Car loan", "keywords": [], "expected_min": 500, "expected_max": 500, "exclude_from_plan_sync": true }
  ]
}
JSON

cat >"$WORK/data/settings/whatif.json" <<'JSON'
{
  "portfolio_value": 500000,
  "monthly_living_expenses": 1000,
  "monthly_healthcare": 0,
  "start_date": "2026-01",
  "persons": [
    { "id": "86bb7d8f-8546-4024-8259-4fcb27840a1d", "name": "You", "birth_month": "1961-04", "role": "primary" }
  ],
  "phase_age_reference": "older",
  "tax_deferred_percent": 70,
  "roth_percent": 0,
  "stock_percent": 0,
  "cash_percent": 0,
  "inflation_rate": 3,
  "healthcare_inflation": 0,
  "spending_decline_rate": 0,
  "investment_return": 7,
  "discount_rate": 0,
  "spending_phase_config": { "enabled": false, "phases": [] },
  "projection_years": 30,
  "projection_timing": "end_of_month",
  "income_sources": [],
  "expense_sources": []
}
JSON

if ! go build -o "$WORK/sy1-server" ./cmd/server >"$WORK/sbuild.log" 2>&1; then
  bad server-build "go build ./cmd/server failed (coordinate with HC lead if foreign)"
  tail -10 "$WORK/sbuild.log"
  echo "ORACLE FAIL"; exit 1
fi
BUDGET_LISTEN_ADDR="127.0.0.1:$PORT" BUDGET_DATA_DIR="$WORK/data" \
  "$WORK/sy1-server" >"$WORK/server.log" 2>&1 &
SRV_PID=$!
up=0
for _ in $(seq 1 50); do
  if curl -s -o /dev/null "$BASE/whatif"; then up=1; break; fi
  sleep 0.2
done
if [[ "$up" != 1 ]]; then
  bad server-up "server did not answer on $PORT"; tail -10 "$WORK/server.log"
  echo "ORACLE FAIL"; exit 1
fi
ok server-up

curl -s -X POST "$BASE/whatif/sync" >"$WORK/preview.html"

# Excluded section present, with the group name and its 4-transaction count.
if grep -q "Car loan" "$WORK/preview.html"; then ok preview-group-name; else bad preview-group-name; fi
if grep -Eq "5 transactions" "$WORK/preview.html"; then ok preview-group-count; else bad preview-group-count; fi

# Group monthly amount ~= NET 1500/months (ruling SY-2026-08-30a: the +500
# reversal REDUCES the displayed total; gross-abs would show 2500/months.
# The HI-category overlap row must not inflate it either, D-SY-e).
if python3 - "$WORK/preview.html" "$WORK/data/transactions.csv" <<'PY' >"$WORK/group.verdict" 2>&1
import csv, datetime, re, sys
html = open(sys.argv[1]).read()
rows = list(csv.DictReader(open(sys.argv[2])))
mind = min(datetime.date.fromisoformat(r["Date"]) for r in rows)
months = (datetime.date.today() - mind).days / 30
want = 1500 / months
m = re.search(r"Car loan[^$]*\$([0-9,]+(?:\.[0-9]+)?)", html)
assert m, "no dollar figure on the Car loan line"
got = float(m.group(1).replace(",", ""))
assert abs(got - want) <= 2.5, (
    f"group monthly {got}, want net ~{want:.2f} "
    f"(gross-abs defect shows ~{2500/months:.2f})")
print("group ok")
PY
then ok preview-group-monthly; else bad preview-group-monthly "$(tail -1 "$WORK/group.verdict")"; fi

# Apply through the real guard flow and assert on the SAVED value — the one
# rounding-path-free figure (9500/months; see header math).
HASH=$(grep -o 'name="plan_hash" value="[^"]*"' "$WORK/preview.html" | sed 's/.*value="//;s/"//')
REV=$(grep -o 'name="expected_revision" value="[^"]*"' "$WORK/preview.html" | sed 's/.*value="//;s/"//')
SCEN=$(grep -o 'name="expected_scenario" value="[^"]*"' "$WORK/preview.html" | sed 's/.*value="//;s/"//')
if [[ -n "$HASH" && -n "$REV" && -n "$SCEN" ]]; then ok preview-guard-fields; else bad preview-guard-fields "hidden fields missing"; fi
CODE=$(curl -s -o "$WORK/apply.html" -w "%{http_code}" -X POST "$BASE/whatif/sync/apply" \
  -d "expected_scenario=$SCEN" -d "plan_hash=$HASH" -d "expected_revision=$REV")
if [[ "$CODE" == "200" ]]; then ok apply-200; else bad apply-200 "http $CODE"; fi

if python3 - "$WORK/data/settings/whatif.json" "$WORK/data/transactions.csv" <<'PY' >"$WORK/saved.verdict" 2>&1
import csv, datetime, json, sys
saved = json.load(open(sys.argv[1]))["monthly_living_expenses"]
rows = list(csv.DictReader(open(sys.argv[2])))
mind = min(datetime.date.fromisoformat(r["Date"]) for r in rows)
months = (datetime.date.today() - mind).days / 30
want = 9500 / months
assert abs(saved - want) <= 5.0, (
    f"saved living {saved:.2f}, want ~{want:.2f} "
    f"(master ~{11000/months:.2f}; flagged-def filtering ~{9000/months:.2f})")
print("saved ok")
PY
then ok apply-saved-living; else bad apply-saved-living "$(tail -1 "$WORK/saved.verdict")"; fi

kill "$SRV_PID" 2>/dev/null; wait "$SRV_PID" 2>/dev/null; SRV_PID=""

echo "checks passed: $PASSN, failed: $FAILN"
if [[ "$FAILN" -eq 0 && "$PASSN" -ge 8 ]]; then
  echo "ORACLE PASS"
else
  echo "ORACLE FAIL"
  exit 1
fi
