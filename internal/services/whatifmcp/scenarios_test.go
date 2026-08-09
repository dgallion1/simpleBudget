package whatifmcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if name == "" {
		t.Error("Load(\"\") should report the resolved scenario filename")
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

// newTestSource builds a Source over a temp copy of the repo's shipped
// settings fixtures. Never point a test at the real data/ directory.
func newTestSource(t *testing.T) *Source {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join("..", "..", "..", "data", "settings")
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644); err != nil {
			t.Fatalf("write %s: %v", e.Name(), err)
		}
	}
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return NewSource(dir, store)
}
