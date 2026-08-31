# DP2.1 Manifest: Near-Duplicates Oracle Guard

## Test Function Added

**TestDetect_PendingPosted_NegativeBothPosted** in `internal/services/dataloader/near_duplicates_test.go`

Tests the case where two Posted transactions share the same account, amount, and date, but have different OriginalDescription values. This prevents both the same-day-reimport shape (shape 2) and the pending->posted shape (shape 3) from firing, so the expected result is 0 pairs.

Test code:
```go
func TestDetect_PendingPosted_NegativeBothPosted(t *testing.T) {
	// Two Posted transactions with same account, amount, and date, but
	// different OriginalDescription values. The same-day-reimport shape
	// cannot fire (different Original Description), and the pending->posted
	// shape cannot fire (both are Posted, not Pending->Posted).
	a := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools", "Posted", "usaa-credit")
	a.OriginalDescription = "HARBOR FREIGHT TOOLS3185 PENFIELD     NY"
	b := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools USA", "Posted", "usaa-credit")
	b.OriginalDescription = "Harbor Freight Tools USA"
	txns := []models.Transaction{a, b}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (both Posted), got %d", len(got))
	}
}
```

## Test Execution Result

All 24 TestDetect_* tests PASS:
```
=== RUN   TestDetect_PendingPosted_NegativeBothPosted
--- PASS: TestDetect_PendingPosted_NegativeBothPosted (0.00s)
PASS
ok  	budget2/internal/services/dataloader	0.003s
```

Command: `go test -count=1 -run TestDetect ./internal/services/dataloader/`
