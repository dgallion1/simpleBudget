package majorexpenses

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestValidateAcceptsTheThreeValidShapes(t *testing.T) {
	cases := map[string]models.MajorExpense{
		"keyword only":             {Name: "Mortgage", Keywords: []string{"mortgage"}},
		"keyword with a range":     {Name: "Mortgage", Keywords: []string{"mortgage"}, ExpectedMin: 1900, ExpectedMax: 2100},
		"amount only, both bounds": {Name: "Quarterly Check", ExpectedMin: 450, ExpectedMax: 450},
		"pin-only target":          {Name: "Amazon — Books"},
		"transfer with a keyword":  {Name: "Transfer", Keywords: []string{"xfer"}, IsInternalTransfer: true},
	}
	for name, me := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Validate(me); err != nil {
				t.Errorf("Validate(%+v) = %v, want nil", me, err)
			}
		})
	}
}

func TestValidateRejectsTheInvalidShapes(t *testing.T) {
	cases := []struct {
		name string
		me   models.MajorExpense
		want string
	}{
		{"no name", models.MajorExpense{Keywords: []string{"x"}}, "name is required"},
		{"blank name", models.MajorExpense{Name: "   ", Keywords: []string{"x"}}, "name is required"},
		{"name too long", models.MajorExpense{Name: strings.Repeat("a", 201), Keywords: []string{"x"}}, "too long"},
		{"negative min", models.MajorExpense{Name: "A", ExpectedMin: -1}, "expected_min cannot be negative"},
		{"negative max", models.MajorExpense{Name: "A", ExpectedMax: -1}, "expected_max cannot be negative"},
		{"min above max", models.MajorExpense{Name: "A", ExpectedMin: 100, ExpectedMax: 10}, "cannot exceed"},
		{"only min, no keyword", models.MajorExpense{Name: "A", ExpectedMin: 100}, "set BOTH Min and Max"},
		{"only max, no keyword", models.MajorExpense{Name: "A", ExpectedMax: 100}, "set BOTH Min and Max"},
		{"transfer with nothing to match", models.MajorExpense{Name: "A", IsInternalTransfer: true}, "internal-transfer filter needs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.me)
			if err == nil {
				t.Fatalf("Validate(%+v) = nil, want an error mentioning %q", tc.me, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestValidateAllowsOnlyAMaxWhenAKeywordIsPresent pins the asymmetry: the
// both-or-neither rule applies only when there is no keyword, because a
// keyword-matched group uses a one-sided bound purely for anomaly detection.
func TestValidateAllowsOnlyAMaxWhenAKeywordIsPresent(t *testing.T) {
	if err := Validate(models.MajorExpense{Name: "A", Keywords: []string{"x"}, ExpectedMax: 100}); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}
