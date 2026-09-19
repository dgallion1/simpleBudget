package explorer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
)

// seedAccounts persists accts into the current test store through the real
// accounts package, matching production's own read/write path rather than a
// hand-rolled fixture.
func seedAccounts(t *testing.T, accts []models.Account) {
	t.Helper()
	if err := accounts.Save(store, accts); err != nil {
		t.Fatalf("accounts.Save: %v", err)
	}
}

// importScanLIBlocks splits a rendered import-scan partial into its
// per-entry <li>...</li> blocks. Every entry's markup is a single,
// non-nested <li>, so a non-greedy match stops at the right closing tag.
func importScanLIBlocks(html string) []string {
	return regexp.MustCompile(`(?s)<li\b.*?</li>`).FindAllString(html, -1)
}

func liFor(t *testing.T, blocks []string, name string) string {
	t.Helper()
	for _, b := range blocks {
		if strings.Contains(b, `value="`+name+`"`) {
			return b
		}
	}
	t.Fatalf("no <li> block found for %q in blocks: %v", name, blocks)
	return ""
}

// ---- AC2: naming, MatchFile refusal, suffixing ----

// A unique account chosen for an import lands under importer.Name's
// generated filename, and the outcome's reason names both the file and the
// account.
func TestHandleImport_AccountChosen_ImportsUnderGeneratedNameWithReason(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	seedAccounts(t, []models.Account{
		{ID: "usaa-credit-card", Name: "USAA Credit Card", FilePatterns: []string{"usaa-credit*.csv"}},
	})

	src := "Date,Description,Amount\n2026-07-01,A,10.00\n2026-09-18,B,20.00\n"
	seedImportFile(t, importDir, "bk_download (4).csv", src)

	rec := postImport(t, url.Values{
		"name":                        {"bk_download (4).csv"},
		"account:bk_download (4).csv": {"usaa-credit-card"},
		"delete_source":               {"true"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	out := importOutcomeFor(t, decodeImportResult(t, rec), "bk_download (4).csv")
	if out.Status != "imported" {
		t.Fatalf("Status=%q want imported (reason %q)", out.Status, out.Reason)
	}
	wantReason := "imported as usaa-credit-card_2026-07-01_to_2026-09-18.csv (USAA Credit Card), source file deleted"
	if out.Reason != wantReason {
		t.Errorf("Reason=%q want %q", out.Reason, wantReason)
	}
	if !out.SourceDeleted {
		t.Errorf("SourceDeleted=false, want true")
	}
	mustExist(t, filepath.Join(dataDir, "usaa-credit-card_2026-07-01_to_2026-09-18.csv"), "the generated name must exist")
}

// A generated name that already exists in the data dir under DIFFERENT
// content suffixes _2, and the suffixed name is re-verified against the
// account's own file pattern.
func TestHandleImport_AccountChosen_SuffixesOnNameCollisionWithDifferentContent(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	seedAccounts(t, []models.Account{
		{ID: "acct1", Name: "Acct One", FilePatterns: []string{"acct1*.csv"}},
	})

	collisionName := "acct1_2026-01-01_to_2026-01-05.csv"
	collisionContent := "Date,Description,Amount\n2026-06-01,Unrelated,-1.00\n"
	if err := os.WriteFile(filepath.Join(dataDir, collisionName), []byte(collisionContent), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	src := "Date,Description,Amount\n2026-01-01,Coffee,-4.00\n2026-01-05,Groceries,-40.00\n"
	seedImportFile(t, importDir, "browser-export.csv", src)

	rec := postImport(t, url.Values{
		"name":                       {"browser-export.csv"},
		"account:browser-export.csv": {"acct1"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	out := importOutcomeFor(t, decodeImportResult(t, rec), "browser-export.csv")
	if out.Status != "imported" {
		t.Fatalf("Status=%q want imported (reason %q)", out.Status, out.Reason)
	}
	wantName := "acct1_2026-01-01_to_2026-01-05_2.csv"
	if !strings.Contains(out.Reason, "imported as "+wantName) {
		t.Fatalf("Reason=%q want it to mention %q", out.Reason, wantName)
	}

	got, err := os.ReadFile(filepath.Join(dataDir, wantName))
	if err != nil {
		t.Fatalf("ReadFile suffixed destination: %v", err)
	}
	if string(got) != src {
		t.Errorf("suffixed destination content = %q, want %q", got, src)
	}

	untouched, err := os.ReadFile(filepath.Join(dataDir, collisionName))
	if err != nil {
		t.Fatalf("ReadFile collision file: %v", err)
	}
	if string(untouched) != collisionContent {
		t.Errorf("collision file was modified: %q", untouched)
	}
}

// A chosen account whose only file pattern would never claim the generated
// name is refused, not silently saved somewhere the account can't find it
// again on a future load.
func TestHandleImport_AccountChosen_PatternMismatchRejected(t *testing.T) {
	_, importDir := setupImportScanEnv(t)
	seedAccounts(t, []models.Account{
		{ID: "acct2", Name: "Acct Two", FilePatterns: []string{"other*.csv"}},
	})

	src := "Date,Description,Amount\n2026-02-01,A,-1.00\n2026-02-10,B,-2.00\n"
	seedImportFile(t, importDir, "browser-export2.csv", src)

	rec := postImport(t, url.Values{
		"name":                        {"browser-export2.csv"},
		"account:browser-export2.csv": {"acct2"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	out := importOutcomeFor(t, decodeImportResult(t, rec), "browser-export2.csv")
	if out.Status != "rejected" {
		t.Fatalf("Status=%q want rejected (reason %q)", out.Status, out.Reason)
	}
	wantName := "acct2_2026-02-01_to_2026-02-10.csv"
	wantReason := fmt.Sprintf("account Acct Two has no file pattern matching %s; add the pattern on the Accounts page", wantName)
	if out.Reason != wantReason {
		t.Errorf("Reason=%q want %q", out.Reason, wantReason)
	}
}

// A file that fails to parse under the chosen account is rejected with the
// parser's own message, and the source is kept.
func TestHandleImport_AccountChosen_ParseFailureRejectedSourceKept(t *testing.T) {
	_, importDir := setupImportScanEnv(t)
	seedAccounts(t, []models.Account{
		{ID: "acct3", Name: "Acct Three", FilePatterns: []string{"acct3*.csv"}},
	})

	src := seedImportFile(t, importDir, "garbage.csv", "not,a,valid,header\n1,2,3,4\n")

	rec := postImport(t, url.Values{
		"name":                {"garbage.csv"},
		"account:garbage.csv": {"acct3"},
		"delete_source":       {"true"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	out := importOutcomeFor(t, decodeImportResult(t, rec), "garbage.csv")
	if out.Status != "rejected" {
		t.Fatalf("Status=%q want rejected (reason %q)", out.Status, out.Reason)
	}
	if out.Reason == "" {
		t.Errorf("Reason is empty, want the parser's error message")
	}
	if out.SourceDeleted {
		t.Errorf("SourceDeleted=true for a parse failure")
	}
	mustExist(t, src, "a parse failure must keep the source")
}

// ---- AC3: byte-identical skip, both surfaces ----

func TestHandleImport_UnassignedIdenticalContentSkipsAcrossNames(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	content := "Date,Description,Amount\n2026-03-01,Coffee,-4.00\n"
	if err := os.WriteFile(filepath.Join(dataDir, "existing.csv"), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	src := seedImportFile(t, importDir, "existing (1).csv", content)

	rec := postImport(t, url.Values{"name": {"existing (1).csv"}, "delete_source": {"true"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	out := importOutcomeFor(t, decodeImportResult(t, rec), "existing (1).csv")
	if out.Status != "skipped" {
		t.Fatalf("Status=%q want skipped (reason %q)", out.Status, out.Reason)
	}
	if out.Reason != "identical to existing.csv" {
		t.Errorf("Reason=%q want %q", out.Reason, "identical to existing.csv")
	}
	if out.SourceDeleted {
		t.Errorf("SourceDeleted=true for an identical skip even with delete_source=true")
	}
	mustExist(t, src, "an identical file's source must survive even with delete_source=true")
	mustNotExist(t, filepath.Join(dataDir, "existing (1).csv"), "nothing may be written for an identical skip")
}

func TestHandleFileUpload_IdenticalContentSkips(t *testing.T) {
	dataDir := setupTestEnv(t)
	content := []byte("Date,Description,Amount\n2026-03-01,Coffee,-4.00\n")
	if err := store.WriteFile(filepath.Join(dataDir, "existing.csv"), content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	req := newUploadRequest(t, "existing (1).csv", content)
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	payload := decodeUploadResponse(t, rec)
	out := outcomeFor(t, payload, "existing (1).csv")
	if out.Status != "skipped" {
		t.Fatalf("Status=%q want skipped (reason %q)", out.Status, out.Reason)
	}
	if out.Reason != "identical to existing.csv" {
		t.Errorf("Reason=%q want %q", out.Reason, "identical to existing.csv")
	}
	mustNotExist(t, filepath.Join(dataDir, "existing (1).csv"), "nothing may be written for an identical skip")
}

// ---- AC4: scan partial rendering ----

func TestHandleImportScan_WithRenderer_AccountSelectAndDetectionText(t *testing.T) {
	dataDir := setupTestEnvWithRenderer(t)
	importDir := t.TempDir()
	cfg.ImportDirectory = importDir

	seedAccounts(t, []models.Account{
		{ID: "acctA", Name: "Account A", FilePatterns: []string{"acctA*.csv"}},
	})

	ledgerCSV := "Date,Description,Amount\n2026-04-01,Coffee Shop,-4.00\n2026-04-02,Groceries Store,-40.00\n2026-04-03,Gas Station,-30.00\n"
	ledgerName := "acctA_2026-04-01_to_2026-04-03.csv"
	if err := os.WriteFile(filepath.Join(dataDir, ledgerName), []byte(ledgerCSV), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Same keys (date/desc/amount) as the ledger file, different case so the
	// raw bytes differ -- a genuine detection, not a byte-identical skip.
	detectedCSV := "Date,Description,Amount\n2026-04-01,COFFEE SHOP,-4.00\n2026-04-02,GROCERIES STORE,-40.00\n2026-04-03,GAS STATION,-30.00\n"
	seedImportFile(t, importDir, "detected.csv", detectedCSV)
	// Byte-identical to the ledger file already in the data dir.
	seedImportFile(t, importDir, "dup.csv", ledgerCSV)
	// Shares nothing with anything loaded.
	seedImportFile(t, importDir, "unknown.csv", "Date,Description,Amount\n2026-05-01,Something Else,-9.00\n")
	// Same NAME as an existing data-dir file, but different content: the
	// name-only collision text must still render (unaffected by IM2).
	existsDiffName := "exists-diff.csv"
	if err := os.WriteFile(filepath.Join(dataDir, existsDiffName), []byte("Date,Description,Amount\n2026-01-01,Old,-1.00\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	seedImportFile(t, importDir, existsDiffName, "Date,Description,Amount\n2026-01-01,New,-2.00\n")

	req := httptest.NewRequest(http.MethodGet, "/explorer/import/scan", nil)
	rec := httptest.NewRecorder()
	handleImportScan(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	blocks := importScanLIBlocks(body)

	detected := liFor(t, blocks, "detected.csv")
	dup := liFor(t, blocks, "dup.csv")
	unknown := liFor(t, blocks, "unknown.csv")
	existsDiff := liFor(t, blocks, existsDiffName)

	// detected.csv: unique match, select pre-selected to acctA, label bound
	// to the select's id, no "could not detect" text, checkbox still
	// enabled/checked.
	labelMatch := regexp.MustCompile(`<label for="(import-account-\d+)"[^>]*>Account for detected\.csv</label>`).FindStringSubmatch(detected)
	if labelMatch == nil {
		t.Fatalf("detected.csv: label not found or not reading 'Account for detected.csv': %s", detected)
	}
	if !strings.Contains(detected, `<select id="`+labelMatch[1]+`"`) {
		t.Errorf("detected.csv: select id does not match label's for=%q: %s", labelMatch[1], detected)
	}
	if !strings.Contains(detected, `name="account:detected.csv"`) {
		t.Errorf("detected.csv: select name attribute missing: %s", detected)
	}
	if !regexp.MustCompile(`<option value="acctA"[^>]*selected`).MatchString(detected) {
		t.Errorf("detected.csv: acctA option not pre-selected: %s", detected)
	}
	if strings.Contains(detected, "could not detect") {
		t.Errorf("detected.csv: unexpected 'could not detect' text: %s", detected)
	}
	if !strings.Contains(detected, "checked") {
		t.Errorf("detected.csv: checkbox should remain enabled/checked: %s", detected)
	}

	// dup.csv: byte-identical to the ledger file -- disabled checkbox, exact
	// "(identical to <name>, will be skipped)" text, name-only text absent.
	if !strings.Contains(dup, "disabled") {
		t.Errorf("dup.csv: checkbox should be disabled: %s", dup)
	}
	wantIdentical := fmt.Sprintf("(identical to %s, will be skipped)", ledgerName)
	if !strings.Contains(dup, wantIdentical) {
		t.Errorf("dup.csv: missing %q: %s", wantIdentical, dup)
	}
	if strings.Contains(dup, "already present, will be skipped") {
		t.Errorf("dup.csv: the name-only text must not also render: %s", dup)
	}

	// unknown.csv: no match at all -- "could not detect" text, pre-selected
	// to "unassigned".
	if !strings.Contains(unknown, "(could not detect the account — choose one)") {
		t.Errorf("unknown.csv: missing could-not-detect text: %s", unknown)
	}
	if !regexp.MustCompile(`<option value="unassigned"[^>]*selected`).MatchString(unknown) {
		t.Errorf("unknown.csv: unassigned option not pre-selected: %s", unknown)
	}

	// exists-diff.csv: name-only collision (unchanged pre-existing text),
	// not the new identical wording.
	if !strings.Contains(existsDiff, "(already present, will be skipped)") {
		t.Errorf("exists-diff.csv: missing the pre-existing name-only text: %s", existsDiff)
	}
	if strings.Contains(existsDiff, "identical to") {
		t.Errorf("exists-diff.csv: must not claim identical content: %s", existsDiff)
	}
	if !strings.Contains(existsDiff, "disabled") {
		t.Errorf("exists-diff.csv: checkbox should be disabled: %s", existsDiff)
	}
}

// The JSON fallback (renderer == nil) must carry the new detection fields,
// not just the pre-existing ones.
func TestHandleImportScan_JSONFallback_CarriesDetectionFields(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	seedAccounts(t, []models.Account{
		{ID: "acctJ", Name: "Account J", FilePatterns: []string{"acctJ*.csv"}},
	})
	if err := os.WriteFile(filepath.Join(dataDir, "acctJ_2026-06-01_to_2026-06-03.csv"),
		[]byte("Date,Description,Amount\n2026-06-01,X1,-1.00\n2026-06-02,X2,-2.00\n2026-06-03,X3,-3.00\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	seedImportFile(t, importDir, "detect-me.csv",
		"Date,Description,Amount\n2026-06-01,X1,-1.00\n2026-06-02,X2,-2.00\n2026-06-03,X3,-3.00\n2026-06-04,X4,-4.00\n")

	req := httptest.NewRequest(http.MethodGet, "/explorer/import/scan", nil)
	rec := httptest.NewRecorder()
	handleImportScan(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		ImportEntries []importScanEntry `json:"ImportEntries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v (body: %s)", err, rec.Body.String())
	}
	if len(payload.ImportEntries) != 1 {
		t.Fatalf("expected 1 entry, got %d: %+v", len(payload.ImportEntries), payload.ImportEntries)
	}
	e := payload.ImportEntries[0]
	if e.Detected != "acctJ" {
		t.Errorf("Detected = %q, want acctJ", e.Detected)
	}
	if e.DetectedName != "acctJ_2026-06-01_to_2026-06-04.csv" {
		t.Errorf("DetectedName = %q", e.DetectedName)
	}
	if e.IdenticalTo != "" {
		t.Errorf("IdenticalTo = %q, want empty", e.IdenticalTo)
	}
}

// ---- AC6: upload outcomes ----

func TestHandleFileUpload_UniqueDetectionRenamesWithReason(t *testing.T) {
	dataDir := setupTestEnv(t)
	seedAccounts(t, []models.Account{
		{ID: "acctU", Name: "Upload Acct", FilePatterns: []string{"acctU*.csv"}},
	})

	// A wider ledger file so the detected/generated name for the uploaded
	// subset differs from this file's own name -- a clean detection with no
	// suffix collision.
	ledgerCSV := "Date,Description,Amount\n2026-04-01,Groceries,-100.00\n2026-04-15,Internet,-60.00\n" +
		"2026-05-01,Rent,-1200.00\n2026-05-05,Electric,-90.00\n2026-05-10,Water,-40.00\n"
	if err := os.WriteFile(filepath.Join(dataDir, "acctU_2026-04-01_to_2026-05-10.csv"), []byte(ledgerCSV), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	uploaded := []byte("Date,Description,Amount\n2026-05-01,RENT,-1200.00\n2026-05-05,ELECTRIC,-90.00\n2026-05-10,WATER,-40.00\n")
	req := newUploadRequest(t, "browser-export.csv", uploaded)
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	payload := decodeUploadResponse(t, rec)
	out := outcomeFor(t, payload, "browser-export.csv")
	if out.Status != "saved" {
		t.Fatalf("Status=%q want saved (reason %q)", out.Status, out.Reason)
	}
	wantReason := "saved as acctU_2026-05-01_to_2026-05-10.csv (Upload Acct)"
	if out.Reason != wantReason {
		t.Errorf("Reason=%q want %q", out.Reason, wantReason)
	}
	mustExist(t, filepath.Join(dataDir, "acctU_2026-05-01_to_2026-05-10.csv"), "the detected name must exist")
	mustNotExist(t, filepath.Join(dataDir, "browser-export.csv"), "the original browser name must not be used")
}

func TestHandleFileUpload_NoMatchKeepsOriginalNameWithReason(t *testing.T) {
	dataDir := setupTestEnv(t)
	uploaded := []byte("Date,Description,Amount\n2026-08-01,Something New,-5.00\n")
	req := newUploadRequest(t, "mystery.csv", uploaded)
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	payload := decodeUploadResponse(t, rec)
	out := outcomeFor(t, payload, "mystery.csv")
	if out.Status != "saved" {
		t.Fatalf("Status=%q want saved (reason %q)", out.Status, out.Reason)
	}
	want := "saved unassigned: could not detect the account (use the import folder to choose one, or rename to an account pattern)"
	if out.Reason != want {
		t.Errorf("Reason=%q want %q", out.Reason, want)
	}
	mustExist(t, filepath.Join(dataDir, "mystery.csv"), "the original name must be used")
}

// ---- AC7: unassigned surfaces go to zero after a detected import ----

func TestHandleImport_DetectedImportResultsInZeroUnassigned(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	seedAccounts(t, []models.Account{
		{ID: "acctX", Name: "Account X", FilePatterns: []string{"acctX*.csv"}},
		{ID: "acctY", Name: "Account Y", FilePatterns: []string{"acctY*.csv"}},
	})
	if err := os.WriteFile(filepath.Join(dataDir, "acctX_2026-01-01_to_2026-01-03.csv"),
		[]byte("Date,Description,Amount\n2026-01-01,A1,-1.00\n2026-01-02,A2,-2.00\n2026-01-03,A3,-3.00\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "acctY_2026-02-01_to_2026-02-03.csv"),
		[]byte("Date,Description,Amount\n2026-02-01,B1,-10.00\n2026-02-02,B2,-20.00\n2026-02-03,B3,-30.00\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// A browser-named export sharing acctX's three rows by key, plus a
	// fourth row -- enough overlap to detect, not byte-identical (the
	// content differs by that fourth row).
	seedImportFile(t, importDir, "bk_download (4).csv",
		"Date,Description,Amount\n2026-01-01,A1,-1.00\n2026-01-02,A2,-2.00\n2026-01-03,A3,-3.00\n2026-01-04,Extra,-4.00\n")

	entries, msg := scanImportDirectory(importDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 scan entry, got %d (message %q): %+v", len(entries), msg, entries)
	}
	if entries[0].Detected != "acctX" {
		t.Fatalf("Detected = %q, want acctX (entry=%+v)", entries[0].Detected, entries[0])
	}

	rec := postImport(t, url.Values{
		"name":                        {"bk_download (4).csv"},
		"account:bk_download (4).csv": {entries[0].Detected},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	out := importOutcomeFor(t, decodeImportResult(t, rec), "bk_download (4).csv")
	if out.Status != "imported" {
		t.Fatalf("Status=%q want imported (reason %q)", out.Status, out.Reason)
	}

	if _, err := loader.LoadData(); err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if got := loader.UnassignedCount(); got != 0 {
		t.Errorf("UnassignedCount() = %d, want 0", got)
	}
}

// ---- IM2F: promoted probes (rulings 2026-09-18c/d) ----

// The suffixed name is re-verified against the account's patterns: an
// account whose only pattern is the exact base name claims the base but not
// the _2 variant, so a collision must be REFUSED, not saved under a name the
// account can never load again. (Promoted from primary-checker F2.)
func TestHandleImport_AccountChosen_SuffixedNameIsReverifiedAgainstPatterns(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	baseName := "acct3_2026-01-01_to_2026-01-05.csv"
	seedAccounts(t, []models.Account{
		{ID: "acct3", Name: "Acct Three", FilePatterns: []string{baseName}},
	})

	collisionContent := "Date,Description,Amount\n2026-06-01,Unrelated,-1.00\n"
	if err := os.WriteFile(filepath.Join(dataDir, baseName), []byte(collisionContent), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	src := "Date,Description,Amount\n2026-01-01,Coffee,-4.00\n2026-01-05,Groceries,-40.00\n"
	seedImportFile(t, importDir, "browser-export3.csv", src)

	rec := postImport(t, url.Values{
		"name":                        {"browser-export3.csv"},
		"account:browser-export3.csv": {"acct3"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	out := importOutcomeFor(t, decodeImportResult(t, rec), "browser-export3.csv")
	if out.Status != "rejected" {
		t.Fatalf("Status=%q want rejected (reason %q)", out.Status, out.Reason)
	}
	suffixed := "acct3_2026-01-01_to_2026-01-05_2.csv"
	wantReason := fmt.Sprintf("account Acct Three has no file pattern matching %s; add the pattern on the Accounts page", suffixed)
	if out.Reason != wantReason {
		t.Errorf("Reason=%q want %q", out.Reason, wantReason)
	}
	mustNotExist(t, filepath.Join(dataDir, suffixed), "a refused import must write nothing")
	untouched, err := os.ReadFile(filepath.Join(dataDir, baseName))
	if err != nil {
		t.Fatalf("ReadFile collision file: %v", err)
	}
	if string(untouched) != collisionContent {
		t.Errorf("collision file was modified: %q", untouched)
	}
	mustExist(t, filepath.Join(importDir, "browser-export3.csv"), "a refused import keeps the source")
}

// With two accounts configured and a loaded ledger, an upload that shares
// fewer than three keys with each stays unassigned under its original name:
// detection must never fall back to "the first account". (Promoted from
// primary-checker F4.)
func TestHandleFileUpload_NoMatchWithAccountsConfigured_NeverPicksFirstAccount(t *testing.T) {
	dataDir := setupTestEnv(t)
	seedAccounts(t, []models.Account{
		{ID: "acctA", Name: "Account A", FilePatterns: []string{"acctA*.csv"}},
		{ID: "acctB", Name: "Account B", FilePatterns: []string{"acctB*.csv"}},
	})
	ledgerA := "Date,Description,Amount\n2026-04-01,Groceries,-100.00\n2026-04-15,Internet,-60.00\n2026-04-20,Rent,-1200.00\n"
	ledgerB := "Date,Description,Amount\n2026-04-02,Coffee,-4.00\n2026-04-16,Books,-30.00\n2026-04-21,Fuel,-55.00\n"
	for name, content := range map[string]string{"acctA_2026-04-01_to_2026-04-20.csv": ledgerA, "acctB_2026-04-02_to_2026-04-21.csv": ledgerB} {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	// One row in common with each account: below the threshold for both.
	uploaded := []byte("Date,Description,Amount\n2026-04-01,Groceries,-100.00\n2026-04-02,Coffee,-4.00\n2026-04-30,Something New,-5.00\n")
	req := newUploadRequest(t, "mystery2.csv", uploaded)
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	out := outcomeFor(t, decodeUploadResponse(t, rec), "mystery2.csv")
	if out.Status != "saved" {
		t.Fatalf("Status=%q want saved (reason %q)", out.Status, out.Reason)
	}
	want := "saved unassigned: could not detect the account (use the import folder to choose one, or rename to an account pattern)"
	if out.Reason != want {
		t.Errorf("Reason=%q want %q", out.Reason, want)
	}
	mustExist(t, filepath.Join(dataDir, "mystery2.csv"), "the original name must be used")
	mustNotExist(t, filepath.Join(dataDir, "acctA_2026-04-01_to_2026-04-30.csv"), "must not be attributed to the first account")
	mustNotExist(t, filepath.Join(dataDir, "acctB_2026-04-01_to_2026-04-30.csv"), "must not be attributed to the second account")
}
