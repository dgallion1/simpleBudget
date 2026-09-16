package retirement

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"budget2/internal/services/storage"
)

// A linked chain scenario is loaded through LoadScenarioSettings, and its
// schedule offsets are measured from ITS file's start_date. If that seam ever
// stopped re-anchoring the scenario to the current month, the linked plan's
// offsets would sit in a stale frame while the primary's were re-anchored,
// putting every linked cash flow a whole month (or more) out at the chain
// transition. The re-anchoring happens inside decodeSettings —
// normalizeLoadedWhatIfSettings calls resolveCurrentMonth for every decode,
// this path included — and these tests pin that at the LoadScenarioSettings
// seam itself, which is what callers actually use (handlers.go chain
// resolution and the MCP scenario tools).
//
// Loading stays read-only: nothing is persisted, so the scenario FILE keeps
// its own start_date and offsets.
func scenarioMonthManager(t *testing.T) (*SettingsManager, string) {
	t.Helper()
	root := t.TempDir()
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return NewSettingsManager(root, store), root
}

func TestLoadScenarioSettingsResolvesCurrentMonth(t *testing.T) {
	sm, root := scenarioMonthManager(t)

	now := time.Now()
	thisMonth := now.Format("2006-01")
	savedThreeMonthsAgo := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -3, 0).Format("2006-01")

	doc := `{"use_current_month":true,"start_date":"` + savedThreeMonthsAgo + `","projection_years":5,
	 "persons":[{"id":"you","name":"You","role":"primary","birth_month":"1961-09"}],
	 "income_sources":[{"id":"i","name":"Pension","amount":1000,"income_type":"fixed","start_month":12}],
	 "expense_sources":[{"id":"e","name":"Boat","amount":500,"start_month":12,"end_month":24}],
	 "one_time_expenses":[{"id":"o","description":"Roof","month":12,"amount":12000}],
	 "big_ticket_items":[{"id":"b","name":"Car","amount":5000,"month":12,"type":"expense"}]}`
	path := filepath.Join(root, "whatif_linked.json")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := sm.LoadScenarioSettings("whatif_linked.json")
	if err != nil {
		t.Fatalf("LoadScenarioSettings: %v", err)
	}

	if s.StartDate != thisMonth {
		t.Errorf("scenario StartDate = %s, want the current month %s", s.StartDate, thisMonth)
	}
	// Three months elapsed, so every 12-month offset is now 9 months out.
	if got := s.IncomeSources[0].StartMonth; got != 9 {
		t.Errorf("income StartMonth = %d, want 9", got)
	}
	if got := s.ExpenseSources[0].StartMonth; got != 9 {
		t.Errorf("expense StartMonth = %d, want 9", got)
	}
	if s.ExpenseSources[0].EndMonth == nil || *s.ExpenseSources[0].EndMonth != 21 {
		t.Errorf("expense EndMonth = %v, want 21", s.ExpenseSources[0].EndMonth)
	}
	if got := s.OneTimeExpenses[0].Month; got != 9 {
		t.Errorf("one-time Month = %d, want 9", got)
	}
	if got := s.BigTicketItems[0].Month; got != 9 {
		t.Errorf("big-ticket Month = %d, want 9", got)
	}

	// Read-only: the scenario file on disk is untouched by the load.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != doc {
		t.Errorf("LoadScenarioSettings rewrote the scenario file:\n%s", raw)
	}
}

func TestLoadScenarioSettingsLeavesFixedDateScenarioAlone(t *testing.T) {
	sm, root := scenarioMonthManager(t)

	doc := `{"use_current_month":false,"start_date":"2026-04","projection_years":5,
	 "persons":[{"id":"you","name":"You","role":"primary","birth_month":"1961-09"}],
	 "one_time_expenses":[{"id":"o","description":"Roof","month":12,"amount":12000}],
	 "big_ticket_items":[{"id":"b","name":"Car","amount":5000,"month":12,"type":"expense"}]}`
	path := filepath.Join(root, "whatif_fixed.json")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := sm.LoadScenarioSettings("whatif_fixed.json")
	if err != nil {
		t.Fatalf("LoadScenarioSettings: %v", err)
	}

	if s.StartDate != "2026-04" {
		t.Errorf("fixed-date scenario StartDate = %s, want 2026-04 (never advanced)", s.StartDate)
	}
	if got := s.OneTimeExpenses[0].Month; got != 12 {
		t.Errorf("fixed-date scenario one-time Month = %d, want 12 (never shifted)", got)
	}
	if got := s.BigTicketItems[0].Month; got != 12 {
		t.Errorf("fixed-date scenario big-ticket Month = %d, want 12 (never shifted)", got)
	}
}

// Legacy year keys in a scenario file convert on decode and then shift with
// everything else, so a stored chain scenario written before the month fields
// existed lands in the same frame as the primary.
func TestLoadScenarioSettingsConvertsAndShiftsLegacyKeys(t *testing.T) {
	sm, root := scenarioMonthManager(t)

	now := time.Now()
	savedThreeMonthsAgo := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -3, 0).Format("2006-01")
	doc := `{"use_current_month":true,"start_date":"` + savedThreeMonthsAgo + `","projection_years":5,
	 "persons":[{"id":"you","name":"You","role":"primary","birth_month":"1961-09"}],
	 "expense_sources":[{"id":"e","name":"Boat","amount":500,"start_year":1,"end_year":2}],
	 "one_time_expenses":[{"id":"o","description":"Roof","year":1,"amount":12000}]}`
	if err := os.WriteFile(filepath.Join(root, "whatif_legacy.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := sm.LoadScenarioSettings("whatif_legacy.json")
	if err != nil {
		t.Fatalf("LoadScenarioSettings: %v", err)
	}
	if got := s.ExpenseSources[0].StartMonth; got != 9 {
		t.Errorf("legacy expense StartMonth = %d, want 9 (12 converted, then shifted by 3)", got)
	}
	if s.ExpenseSources[0].EndMonth == nil || *s.ExpenseSources[0].EndMonth != 21 {
		t.Errorf("legacy expense EndMonth = %v, want 21", s.ExpenseSources[0].EndMonth)
	}
	if got := s.OneTimeExpenses[0].Month; got != 9 {
		t.Errorf("legacy one-time Month = %d, want 9", got)
	}
}
