package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"
)

func TestSource_LoadUnknownScenarioNamesValidOnes(t *testing.T) {
	s := newTestSource(t)
	_, _, err := s.Load("no-such-plan.json")
	if err == nil {
		t.Fatal("expected an error for an unknown scenario")
	}
	if !strings.Contains(err.Error(), "no-such-plan.json") {
		t.Errorf("error should name the requested scenario, got: %v", err)
	}
	if !strings.Contains(err.Error(), "whatif.json") {
		t.Errorf("error should list valid scenario names, got: %v", err)
	}
}

func TestSource_LoadEmptyNameResolvesActive(t *testing.T) {
	s := newTestSource(t)
	settings, name, err := s.Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error: %v", err)
	}
	if settings == nil {
		t.Fatal("Load(\"\") returned nil settings")
	}
	if name != "whatif.json" {
		t.Errorf("Load(\"\") resolved filename = %q, want %q", name, "whatif.json")
	}
}

func TestSource_ListReportsActiveFlag(t *testing.T) {
	s := newTestSource(t)
	list, err := s.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("List() returned no scenarios")
	}
	active := 0
	for _, sc := range list {
		if sc.Active {
			active++
		}
	}
	if active != 1 {
		t.Errorf("expected exactly one active scenario, got %d", active)
	}
}

func TestSource_ListFlagsUnreadableScenario(t *testing.T) {
	s := newTestSourceWithBrokenScenario(t)
	list, err := s.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}

	var broken, active *ScenarioInfo
	for i := range list {
		switch list[i].Filename {
		case "whatif_broken.json":
			broken = &list[i]
		case "whatif.json":
			active = &list[i]
		}
	}

	if broken == nil {
		t.Fatal("List() did not include the broken scenario file")
	}
	if !broken.Unreadable {
		t.Error("broken scenario should have Unreadable true")
	}
	if broken.LoadError == "" {
		t.Error("broken scenario should have a non-empty LoadError")
	}

	if active == nil {
		t.Fatal("List() did not include whatif.json")
	}
	if active.Unreadable {
		t.Error("whatif.json should have Unreadable false")
	}
}

// newTestSource builds a Source over a synthesized settings fixture in a
// temp directory: one valid whatif.json derived from
// models.DefaultWhatIfSettings(), which List() reports as the sole active
// scenario. Never point a test at the real data/ directory — it is
// gitignored and holds the owner's private financial data, so a test that
// reads it fails on any machine that doesn't have that directory.
func newTestSource(t *testing.T) *Source {
	t.Helper()
	return newTestSourceFixture(t, false)
}

// newTestSourceWithBrokenScenario builds on newTestSource's fixture but also
// drops in a deliberately corrupt whatif_broken.json, so List() has an
// unreadable entry to report alongside the valid one.
func newTestSourceWithBrokenScenario(t *testing.T) *Source {
	t.Helper()
	return newTestSourceFixture(t, true)
}

// newTestSourceFixture writes a synthesized whatif.json (and, if
// withBroken, a corrupt whatif_broken.json) into a temp directory and opens
// a Source over it.
func newTestSourceFixture(t *testing.T, withBroken bool) *Source {
	t.Helper()
	dir := t.TempDir()

	b, err := json.Marshal(models.DefaultWhatIfSettings())
	if err != nil {
		t.Fatalf("marshal default settings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "whatif.json"), b, 0o644); err != nil {
		t.Fatalf("write whatif.json: %v", err)
	}

	if withBroken {
		if err := os.WriteFile(filepath.Join(dir, "whatif_broken.json"), []byte("{not valid json"), 0o644); err != nil {
			t.Fatalf("write whatif_broken.json: %v", err)
		}
	}

	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return NewSource(retirement.NewSettingsManager(dir, store))
}
