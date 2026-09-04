#!/usr/bin/env bash
# Tier-3 oracle for U14 — File Manager: Clear All becomes a button with a
# count-naming confirm and SERVER-SIDE count confirmation; encryption card
# collapsed by default. Run with cwd = the budget2 tree under test.
# Boots the server on :8199 against a SYNTHETIC data dir (never data/,
# never :8080), exercises the real HTTP surface with curl, tears down via
# /killme. Prints ORACLE PASS as the last line only when every check passed.
set -u
PORT=8199; BASE="http://localhost:$PORT"
WORK="$(mktemp -d)"; LOG="$WORK/oracle.log"
PASSN=0; FAILN=0
ck() { if [[ "$2" == "0" ]]; then echo "CHECK $1: PASS"; PASSN=$((PASSN+1)); else echo "CHECK $1: FAIL"; [[ -n "${3:-}" ]] && echo "    $3"; FAILN=$((FAILN+1)); fi; }
teardown() { curl -fsS --max-time 2 "$BASE/killme" >/dev/null 2>&1 || true; sleep 0.5; [[ -f "$WORK/pid" ]] && kill "$(cat "$WORK/pid")" 2>/dev/null; rm -rf "$WORK"; }
trap teardown EXIT
csvcount() { find "$WORK/data" -maxdepth 1 -name '*.csv' | wc -l; }
healthy() { curl -fsS --max-time 2 "$BASE/api/health" >/dev/null 2>&1; }
if healthy; then echo "CHECK 0 port $PORT free: FAIL (something already listening)"; echo "ORACLE FAIL"; exit 1; fi

# --- synthetic data: 3 CSVs + one non-CSV + a backups dir OUTSIDE data ---
mkdir -p "$WORK/data" "$WORK/backups" "$WORK/import"
for n in a b c; do cp testdata/transactions.csv "$WORK/data/usaa-checking_$n.csv"; done
echo '{}' > "$WORK/data/keep.json"
go build -o "$WORK/srv" ./cmd/server >"$LOG" 2>&1; ck "1 go build ./cmd/server" $? "$(tail -n 5 "$LOG")"
BUDGET_DATA_DIR="$WORK/data" BUDGET2_BACKUP_DIR="$WORK/backups" BUDGET2_IMPORT_DIR="$WORK/import" BUDGET_LISTEN_ADDR=":$PORT" \
  "$WORK/srv" >"$WORK/server.log" 2>&1 & echo $! > "$WORK/pid"
for _ in $(seq 1 80); do healthy && break; sleep 0.25; done
healthy; ck "2 server up on :$PORT with synthetic data (3 CSVs)" $? "$(tail -n 5 "$WORK/server.log")"
[[ "$(csvcount)" == 3 ]]; rc=$?; ck "3 fixture: 3 CSVs present before any request" $rc

PAGE="$WORK/page.html"; curl -fsS "$BASE/filemanager" -o "$PAGE" 2>>"$LOG"; ck "4 GET /filemanager renders" $?
# The Clear All control is a <button> (not a link) targeting DELETE /data/all …
python3 - "$PAGE" <<'PY' >"$WORK/btn.txt" 2>&1; ck "5 Clear All is a <button> with hx-delete=/data/all, carries expected_count, confirm names the count (3)" $? "$(cat "$WORK/btn.txt")"
import re,sys,html
s=open(sys.argv[1],encoding='utf-8').read()
btns=[m.group(0) for m in re.finditer(r'(?s)<button\b[^>]*>.*?</button>', s)]
cands=[b for b in btns if 'hx-delete="/data/all' in b]
assert cands, "no <button> with hx-delete=\"/data/all…\" found"
links=[m.group(0) for m in re.finditer(r'(?s)<a\b[^>]*>.*?</a>', s)]
assert not any('/data/all' in a for a in links), "an <a> still targets /data/all"
b=cands[0]
tag=re.match(r'(?s)<button\b[^>]*>', b).group(0)
assert 'expected_count' in b, "button does not carry expected_count (hx-vals / hx-delete query)"
assert re.search(r'expected_count["\'=:\s]*3\b', b) or 'expected_count=3' in b, "expected_count is not 3: "+tag
conf=re.search(r'hx-confirm="([^"]*)"', b) or re.search(r'data-confirm="([^"]*)"', b)
assert conf, "no hx-confirm on the button"
c=html.unescape(conf.group(1))
assert re.search(r'\b3\b', c) and re.search(r'(?i)delete', c), "confirm does not name the count and the action: "+c
assert 'Clear All' in re.sub(r'<[^>]+>','',b) or 'aria-label' in tag, "button lacks a visible or accessible name"
print("ok:", re.sub(r'\s+',' ',tag)[:200])
PY
# … Load Test Data must not share the button's immediate flex container (visual separation is the checker's; structural separation is here)
python3 - "$PAGE" <<'PY' >"$WORK/sep.txt" 2>&1; ck "6 Clear All is not in the same immediate container as Load Test Data" $? "$(cat "$WORK/sep.txt")"
import re,sys
s=open(sys.argv[1],encoding='utf-8').read()
i=s.find('hx-delete="/data/all'); j=s.find('hx-post="/restore/test-data"')
assert i>0 and j>0
lo,hi=sorted((i,j))
between=s[lo:hi]
# if the two buttons sit in one parent with no intervening closing container tag, they share the container
assert re.search(r'</div>|</section>|</details>|</form>', between), "no container boundary between Load Test Data and Clear All"
print("ok: container boundary between the two controls")
PY

# --- server-side count confirmation (HTMX puts DELETE params in the query string) ---
code=$(curl -s -o "$WORK/r1.txt" -w '%{http_code}' -X DELETE "$BASE/data/all"); n=$(csvcount); [[ "$code" == 409 && "$n" == 3 ]]; rc=$?; ck "7 DELETE without expected_count → 409 and nothing deleted (got $code, csvs=$n)" $rc
code=$(curl -s -o "$WORK/r2.txt" -w '%{http_code}' -X DELETE "$BASE/data/all?expected_count=2"); n=$(csvcount); [[ "$code" == 409 && "$n" == 3 ]]; rc=$?; ck "8 DELETE with stale expected_count=2 → 409 and nothing deleted (got $code, csvs=$n)" $rc
code=$(curl -s -o "$WORK/r3.txt" -w '%{http_code}' -X DELETE "$BASE/data/all?expected_count=abc"); n=$(csvcount); [[ "$code" == 409 || "$code" == 400 ]] && [[ "$n" == 3 ]]; rc=$?; ck "9 DELETE with non-numeric expected_count → 4xx and nothing deleted (got $code, csvs=$n)" $rc
code=$(curl -s -o "$WORK/r4.txt" -w '%{http_code}' -X DELETE "$BASE/data/all?expected_count=3"); n=$(csvcount); [[ "$code" == 200 ]] && grep -q '3' "$WORK/r4.txt" && [[ "$n" == 0 ]] && [[ -f "$WORK/data/keep.json" ]] && [[ -d "$WORK/backups" ]]; rc=$?; ck "10 DELETE with matching expected_count=3 → 200, all 3 CSVs gone, non-CSV and backups dir survive (got $code, csvs=$n)" $rc
code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$BASE/data/all?expected_count=3"); [[ "$code" == 409 ]]; rc=$?; ck "11 repeating the same request after deletion → 409 (count is now 0, not 3; got $code)" $rc

# --- encryption card collapsed by default behind a disclosure ---
python3 - "$PAGE" <<'PY' >"$WORK/enc.txt" 2>&1; ck "12 encryption card collapsed by default: disclosure button aria-expanded=false controls a hidden panel" $? "$(cat "$WORK/enc.txt")"
import re,sys
s=open(sys.argv[1],encoding='utf-8').read()
i=s.find('Data Encryption'); assert i>0, "no 'Data Encryption' heading"
region=s[max(0,i-3000):i+6000]
btn=re.search(r'(?s)<button\b[^>]*aria-expanded="false"[^>]*aria-controls="([^"]+)"[^>]*>|<button\b[^>]*aria-controls="([^"]+)"[^>]*aria-expanded="false"[^>]*>', region)
assert btn, "no disclosure <button aria-expanded=\"false\" aria-controls=…> near the encryption card"
cid=btn.group(1) or btn.group(2)
panel=re.search(r'<[a-z]+\b[^>]*\bid="'+re.escape(cid)+r'"[^>]*>', s)
assert panel, "aria-controls target id not found: "+cid
p=panel.group(0)
assert re.search(r'\bhidden\b', p), "panel is not hidden by default: "+p[:200]
print("ok: disclosure controls #%s, hidden" % cid)
PY

go test -count=1 ./internal/handlers/backup/ ./internal/services/storage/ >"$LOG" 2>&1; ck "13 backup handlers + storage suites green" $? "$(grep -E '^(FAIL|---|ok)' "$LOG" | head -n 8)"
echo "SUMMARY: $PASSN passed, $FAILN failed"
if [[ "$FAILN" == 0 ]]; then echo "ORACLE PASS"; else echo "ORACLE FAIL"; exit 1; fi
