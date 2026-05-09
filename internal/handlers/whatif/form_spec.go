package whatif

import (
	"fmt"
	"net/http"
	"strconv"

	"budget2/internal/models"
)

// fieldKind identifies how a settings form field is parsed and validated.
type fieldKind int

const (
	fieldFloat fieldKind = iota
	fieldInt
	fieldEnum
	fieldOptionalFloat // empty-but-present raw → nil; numeric raw (incl. "0") → &v
)

// fieldSpec declares the parse rules for one /whatif/settings form field.
//
// Inclusion rule: a field is added to the updates map when its parsed value
// is non-zero OR the form key is present with any non-empty string. This
// preserves the legacy semantics of the long handler so explicit zeros
// (e.g. spouse_age=0) are persisted while absent fields are left alone.
type fieldSpec struct {
	Name      string
	Kind      fieldKind
	HasBounds bool
	Min, Max  float64
	EnumVals  []string

	// ParseLabel is the noun used in "Invalid <label>: <err>" parse errors.
	// Required for Float and Int kinds.
	ParseLabel string

	// BoundsMsg is the full error string returned when a bounded value falls
	// outside [Min, Max]. Required when HasBounds is true.
	BoundsMsg string

	// EnumInvalidMsg is the full error string returned when an enum value is
	// not in EnumVals. Required for Enum kind.
	EnumInvalidMsg string
}

// settingsFormSpec lists every primitive field handled by handleWhatIfSettings,
// in the original parse order. Order matters because cross-field invariants
// (e.g. tax_deferred + roth ≤ 100) read earlier entries from the updates map.
var settingsFormSpec = []fieldSpec{
	{Name: "portfolio_value", Kind: fieldFloat, ParseLabel: "portfolio value"},
	{Name: "monthly_living_expenses", Kind: fieldFloat, ParseLabel: "monthly expenses"},
	{Name: "monthly_healthcare", Kind: fieldFloat, ParseLabel: "healthcare cost"},
	{Name: "healthcare_start_years", Kind: fieldInt, ParseLabel: "healthcare start years"},
	{Name: "current_age", Kind: fieldInt, ParseLabel: "age",
		HasBounds: true, Min: 18, Max: 120,
		BoundsMsg: "Age must be between 18 and 120"},
	{Name: "spouse_age", Kind: fieldInt, ParseLabel: "spouse age",
		HasBounds: true, Min: 0, Max: 120,
		BoundsMsg: "Spouse age must be between 0 and 120"},
	{Name: "phase_age_reference", Kind: fieldEnum,
		EnumVals:       []string{"younger", "older", "primary", "spouse"},
		EnumInvalidMsg: "Invalid phase age reference"},
	{Name: "tax_deferred_percent", Kind: fieldFloat, ParseLabel: "tax-deferred percent",
		HasBounds: true, Min: 0, Max: 100,
		BoundsMsg: "Tax-deferred percent must be between 0 and 100"},
	{Name: "roth_percent", Kind: fieldFloat, ParseLabel: "Roth percent",
		HasBounds: true, Min: 0, Max: 100,
		BoundsMsg: "Roth percent must be between 0 and 100"},
	{Name: "stock_percent", Kind: fieldFloat, ParseLabel: "stock percent",
		HasBounds: true, Min: 0, Max: 100,
		BoundsMsg: "Stock percent must be between 0 and 100"},
	{Name: "cash_percent", Kind: fieldFloat, ParseLabel: "cash percent",
		HasBounds: true, Min: 0, Max: 100,
		BoundsMsg: "Cash percent must be between 0 and 100"},
	{Name: "tax_deferred_stock_percent", Kind: fieldFloat, ParseLabel: "tax-deferred stock percent",
		HasBounds: true, Min: 0, Max: 100,
		BoundsMsg: "Tax-deferred stock percent must be between 0 and 100"},
	{Name: "tax_deferred_cash_percent", Kind: fieldFloat, ParseLabel: "tax-deferred cash percent",
		HasBounds: true, Min: 0, Max: 100,
		BoundsMsg: "Tax-deferred cash percent must be between 0 and 100"},
	{Name: "roth_stock_percent", Kind: fieldFloat, ParseLabel: "Roth stock percent",
		HasBounds: true, Min: 0, Max: 100,
		BoundsMsg: "Roth stock percent must be between 0 and 100"},
	{Name: "roth_cash_percent", Kind: fieldFloat, ParseLabel: "Roth cash percent",
		HasBounds: true, Min: 0, Max: 100,
		BoundsMsg: "Roth cash percent must be between 0 and 100"},
	{Name: "taxable_stock_percent", Kind: fieldFloat, ParseLabel: "taxable stock percent",
		HasBounds: true, Min: 0, Max: 100,
		BoundsMsg: "Taxable stock percent must be between 0 and 100"},
	{Name: "taxable_cash_percent", Kind: fieldFloat, ParseLabel: "taxable cash percent",
		HasBounds: true, Min: 0, Max: 100,
		BoundsMsg: "Taxable cash percent must be between 0 and 100"},
	{Name: "inflation_rate", Kind: fieldFloat, ParseLabel: "inflation rate"},
	{Name: "healthcare_inflation", Kind: fieldFloat, ParseLabel: "healthcare inflation"},
	{Name: "spending_decline_rate", Kind: fieldFloat, ParseLabel: "spending decline rate"},
	{Name: "investment_return", Kind: fieldFloat, ParseLabel: "investment return"},
	{Name: "discount_rate", Kind: fieldFloat, ParseLabel: "discount rate"},
	{Name: "taxable_dividend_yield", Kind: fieldFloat, ParseLabel: "taxable dividend yield",
		HasBounds: true, Min: 0, Max: 20,
		BoundsMsg: "Taxable dividend yield must be between 0 and 20%"},
	{Name: "taxable_qualified_dividend_percent", Kind: fieldFloat, ParseLabel: "qualified dividend share",
		HasBounds: true, Min: 0, Max: 100,
		BoundsMsg: "Qualified dividend share must be between 0 and 100%"},
	{Name: "taxable_cap_gains_distribution_rate", Kind: fieldFloat, ParseLabel: "capital gains distribution rate",
		HasBounds: true, Min: 0, Max: 20,
		BoundsMsg: "Capital gains distribution rate must be between 0 and 20%"},
	{Name: "projection_years", Kind: fieldInt, ParseLabel: "projection years",
		HasBounds: true, Min: 1, Max: 100,
		BoundsMsg: "Projection years must be between 1 and 100"},
	{Name: "tax_deferred_delay_years", Kind: fieldInt, ParseLabel: "tax-deferred delay",
		HasBounds: true, Min: 0, Max: 30,
		BoundsMsg: "Tax-deferred delay must be between 0 and 30 years"},
	{Name: "steady_state_override_year", Kind: fieldFloat, ParseLabel: "steady state year"},
	{Name: "state_income_tax_rate", Kind: fieldOptionalFloat, ParseLabel: "state income tax rate",
		HasBounds: true, Min: 0, Max: 20,
		BoundsMsg: "State income tax rate must be between 0 and 20%"},
	{Name: "filing_status", Kind: fieldEnum,
		EnumVals:       []string{"single", "married_joint", "married_separate", "head_of_household"},
		EnumInvalidMsg: "Invalid filing status"},
	{Name: "monthly_property_tax", Kind: fieldFloat, ParseLabel: "monthly property tax",
		HasBounds: true, Min: 0, Max: 50000,
		BoundsMsg: "Monthly property tax must be between 0 and 50000"},
	{Name: "property_tax_inflation", Kind: fieldFloat, ParseLabel: "property tax inflation",
		HasBounds: true, Min: 0, Max: 15,
		BoundsMsg: "Property tax inflation must be between 0 and 15%"},
}

// applyFieldSpec parses a single field according to spec and (on success)
// adds it to updates. Returns a non-empty user-facing message to render as
// a 400 if the field is malformed or out of range. Returns (false, "") when
// the field is absent or empty and should not be added to updates.
//
// The signature returns a string rather than an error because every message
// is a user-visible HTTP response body (with intentional sentence-case
// capitalization), not an internal error to be wrapped or logged.
func applyFieldSpec(r *http.Request, spec fieldSpec, updates map[string]interface{}) (included bool, errMsg string) {
	raw := r.FormValue(spec.Name)

	switch spec.Kind {
	case fieldFloat:
		v, parseErr := parseFormFloat(r, spec.Name)
		if parseErr != nil {
			return false, fmt.Sprintf("Invalid %s: %s", spec.ParseLabel, parseErr.Error())
		}
		if v == 0 && raw == "" {
			return false, ""
		}
		if spec.HasBounds && (v < spec.Min || v > spec.Max) {
			return false, spec.BoundsMsg
		}
		updates[spec.Name] = v
	case fieldInt:
		v, parseErr := parseFormInt(r, spec.Name)
		if parseErr != nil {
			return false, fmt.Sprintf("Invalid %s: %s", spec.ParseLabel, parseErr.Error())
		}
		if v == 0 && raw == "" {
			return false, ""
		}
		if spec.HasBounds && (float64(v) < spec.Min || float64(v) > spec.Max) {
			return false, spec.BoundsMsg
		}
		updates[spec.Name] = v
	case fieldEnum:
		if raw == "" {
			return false, ""
		}
		valid := false
		for _, ev := range spec.EnumVals {
			if raw == ev {
				valid = true
				break
			}
		}
		if !valid {
			return false, spec.EnumInvalidMsg
		}
		updates[spec.Name] = raw
	case fieldOptionalFloat:
		// Distinguish three input states:
		// - key absent (FormValue returns "" and the form has no key) → don't include
		// - key present and empty                                       → include as nil
		// - key present with parseable numeric (incl. "0")              → include as &v
		if _, present := r.Form[spec.Name]; !present {
			return false, ""
		}
		if raw == "" {
			updates[spec.Name] = (*float64)(nil)
			return true, ""
		}
		v, parseErr := parseFormFloat(r, spec.Name)
		if parseErr != nil {
			return false, fmt.Sprintf("Invalid %s: %s", spec.ParseLabel, parseErr.Error())
		}
		if spec.HasBounds && (v < spec.Min || v > spec.Max) {
			return false, spec.BoundsMsg
		}
		ptr := v
		updates[spec.Name] = &ptr
	}
	return true, ""
}

// applySettingsFormSpec runs every entry in settingsFormSpec, producing the
// updates map handed to retirementMgr.UpdateSettings*. The first parse or
// bounds error returns immediately so the caller can render a 400.
func applySettingsFormSpec(r *http.Request, updates map[string]interface{}) string {
	for _, spec := range settingsFormSpec {
		if _, msg := applyFieldSpec(r, spec, updates); msg != "" {
			return msg
		}
	}
	return ""
}

// applyProjectionTiming parses the projection_timing form field, which uses
// model-level normalization rather than a static enum list. Returns an
// empty string on success or a user-facing message on invalid input.
func applyProjectionTiming(r *http.Request, updates map[string]interface{}) string {
	v := r.FormValue("projection_timing")
	if v == "" {
		return ""
	}
	timing := models.ProjectionTiming(v)
	if models.NormalizeProjectionTiming(timing) != timing {
		return "Invalid projection timing"
	}
	updates["projection_timing"] = timing
	return ""
}

// applyRMDTiming parses the rmd_timing form field. Returns an empty string on
// success or a user-facing message on invalid input.
func applyRMDTiming(r *http.Request, updates map[string]interface{}) string {
	v := r.FormValue("rmd_timing")
	if v == "" {
		return ""
	}
	timing := models.RMDTiming(v)
	if models.NormalizeRMDTiming(timing) != timing {
		return "Invalid RMD timing"
	}
	updates["rmd_timing"] = timing
	return ""
}

// validateSettingsCrossFieldInvariants enforces the two cross-field rules
// from the legacy handler: tax_deferred + roth ≤ 100 (when roth is being set)
// and stock + cash ≤ 100 (when cash is being set). For each rule, the partner
// field is read from updates if present, otherwise from the raw form value
// (matching the original "use whatever value is on its way to the model"
// behavior so a form that only sets roth still validates against the
// previously-submitted tax_deferred value). Returns an empty string on
// success or a user-facing message on violation.
func validateSettingsCrossFieldInvariants(r *http.Request, updates map[string]interface{}) string {
	if rothV, ok := updates["roth_percent"].(float64); ok {
		taxDeferred, _ := readFloatFromUpdatesOrForm(r, updates, "tax_deferred_percent", 0)
		if taxDeferred+rothV > 100 {
			return "Tax-deferred + Roth cannot exceed 100%"
		}
	}
	if cashV, ok := updates["cash_percent"].(float64); ok {
		stockPct, _ := readFloatFromUpdatesOrForm(r, updates, "stock_percent", 60.0)
		if stockPct+cashV > 100 {
			return "Stocks + Cash cannot exceed 100%"
		}
	}
	return ""
}

// readFloatFromUpdatesOrForm returns the float value for key, preferring the
// already-parsed value in updates and falling back to the raw form value.
// Returns (fallback, false) when neither source supplies a value.
func readFloatFromUpdatesOrForm(r *http.Request, updates map[string]interface{}, key string, fallback float64) (float64, bool) {
	if v, ok := updates[key].(float64); ok {
		return v, true
	}
	if raw := r.FormValue(key); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			return v, true
		}
	}
	return fallback, false
}

// clampPerAccountAllocations enforces the silent invariant that, for each
// per-account allocation pair (tax-deferred, Roth, taxable), stock + cash ≤ 100.
// When the sum exceeds 100, cash is reduced rather than the request rejected,
// preserving the "favor stocks, trim cash" UX of the legacy handler.
func clampPerAccountAllocations(updates map[string]interface{}) {
	pairs := [...][2]string{
		{"tax_deferred_stock_percent", "tax_deferred_cash_percent"},
		{"roth_stock_percent", "roth_cash_percent"},
		{"taxable_stock_percent", "taxable_cash_percent"},
	}
	for _, p := range pairs {
		stockVal, hasStock := updates[p[0]].(float64)
		cashVal, hasCash := updates[p[1]].(float64)
		if hasStock && hasCash && stockVal+cashVal > 100 {
			updates[p[1]] = max(0.0, 100-stockVal)
		}
	}
}
