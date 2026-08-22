package explorer

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"budget2/internal/services/dataloader"
)

// TestExplorerListsBothSidesOfAResolvedDuplicate is acceptance criterion 2
// for R3: the explorer deliberately keeps the raw transaction slice (not
// Active()) so a user can see -- and undo -- a resolved duplicate. This
// test guards that behavior: it must NOT change when R3's dashboard/mcpsvc
// fixes land, since the explorer's whole point is showing suppressed rows.
func TestExplorerListsBothSidesOfAResolvedDuplicate(t *testing.T) {
	csv := "Date,Description,Amount,Status\n" +
		"2026-08-01,Lucid,-900.00,Scheduled Bill Pay\n" +
		"2026-08-02,Check #55501,-900.00,Posted\n"
	setupTestEnvWithRenderer(t, csv)

	// First load discovers the candidate pair and its hashes.
	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData (discovery): %v", err)
	}
	var pairKey, billHash, checkHash string
	for _, tr := range ts.Transactions {
		if strings.HasPrefix(strings.ToLower(tr.Description), "check") {
			checkHash = tr.Hash
		} else {
			billHash = tr.Hash
		}
		if tr.DuplicatePairKey != "" {
			pairKey = tr.DuplicatePairKey
		}
	}
	if pairKey == "" || billHash == "" || checkHash == "" {
		t.Fatalf("setup: did not detect the expected candidate pair (pairKey=%q bill=%q check=%q)", pairKey, billHash, checkHash)
	}

	// Resolve it: keep the check, suppress the bill-pay side.
	if err := loader.SaveDuplicateDecision(pairKey, dataloader.DuplicateDecision{
		KeptHash:       checkHash,
		SuppressedHash: billHash,
		Outcome:        dataloader.DuplicateOutcomeKeptWinner,
	}); err != nil {
		t.Fatalf("SaveDuplicateDecision: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/explorer", nil)
	rec := httptest.NewRecorder()
	handleExplorer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Lucid") {
		t.Errorf("explorer must still list the suppressed side (Lucid) of a resolved duplicate; got:\n%s", body)
	}
	if !strings.Contains(body, "Check #55501") {
		t.Errorf("explorer must still list the kept side (Check #55501) of a resolved duplicate; got:\n%s", body)
	}
}
