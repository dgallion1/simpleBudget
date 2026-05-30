package models

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ProjectionTiming string

const (
	ProjectionTimingStartOfMonth ProjectionTiming = "start_of_month"
	ProjectionTimingMidMonth     ProjectionTiming = "mid_month"
	ProjectionTimingEndOfMonth   ProjectionTiming = "end_of_month"
)

type PersonRole string

const (
	PersonRolePrimary PersonRole = "primary"
	PersonRoleSpouse  PersonRole = "spouse"
	PersonRoleOther   PersonRole = "other"
)

type Person struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	BirthMonth string     `json:"birth_month"`
	Role       PersonRole `json:"role"`
}

const yearMonthLayout = "2006-01"

func NormalizeProjectionTiming(timing ProjectionTiming) ProjectionTiming {
	switch timing {
	case ProjectionTimingStartOfMonth, ProjectionTimingMidMonth, ProjectionTimingEndOfMonth:
		return timing
	default:
		return ProjectionTimingEndOfMonth
	}
}

// RMDTiming controls when during each projection year the RMD withdrawal occurs.
type RMDTiming string

const (
	RMDTimingStartOfYear RMDTiming = "start_of_year"
	RMDTimingMidYear     RMDTiming = "mid_year"
	RMDTimingEndOfYear   RMDTiming = "end_of_year"
)

// NormalizeRMDTiming clamps to known values. The empty string (zero value) maps
// to mid_year for new scenarios. Settings loading migrates legacy saved scenarios
// to start_of_year so their existing projections are preserved (see
// initializeLoadedSettings in settings.go).
func NormalizeRMDTiming(t RMDTiming) RMDTiming {
	switch t {
	case RMDTimingStartOfYear, RMDTimingMidYear, RMDTimingEndOfYear:
		return t
	default:
		return RMDTimingMidYear
	}
}

// ScenarioChainLink references a scenario to transition to at a given age
type ScenarioChainLink struct {
	ScenarioFilename string `json:"scenario_filename"`
	TransitionAge    int    `json:"transition_age"`
}

// WhatIfSettings contains all user parameters for retirement planning
type WhatIfSettings struct {
	// Scenario metadata
	ScenarioName string `json:"scenario_name,omitempty"` // Display name for this scenario
	// Scenario chaining: ordered list of scenarios to run after this one
	ScenarioChain []ScenarioChainLink `json:"scenario_chain,omitempty"`

	// Portfolio
	PortfolioValue float64 `json:"portfolio_value"` // Current portfolio value

	// Expenses
	MonthlyLivingExpenses float64 `json:"monthly_living_expenses"` // Base monthly expenses
	MonthlyHealthcare     float64 `json:"monthly_healthcare"`      // Monthly healthcare costs (legacy)
	HealthcareStartYears  int     `json:"healthcare_start_years"`  // Years until healthcare starts (legacy)
	MonthlyPropertyTax    float64 `json:"monthly_property_tax"`    // Monthly property tax on primary residence

	// Multi-person healthcare model
	HealthcarePersons []HealthcarePerson `json:"healthcare_persons,omitempty"`
	StartDate         string             `json:"start_date"`
	Persons           []Person           `json:"persons"`

	// RMD Settings
	CurrentAge         int     `json:"-"`                             // User's current age (derived working state)
	SpouseAge          int     `json:"-"`                             // Spouse's current age (derived working state)
	PhaseAgeReference  string  `json:"phase_age_reference,omitempty"` // "younger", "older", "primary", "spouse" - which age triggers phases
	TaxDeferredPercent float64 `json:"tax_deferred_percent"`          // % of portfolio in tax-deferred accounts (401k, IRA)
	RothPercent        float64 `json:"roth_percent"`                  // % of portfolio in Roth accounts (Roth IRA, Roth 401k)
	// Taxable is computed as: 100 - TaxDeferredPercent - RothPercent

	// Per-Account Asset Allocation (stocks %, cash %; bonds = 100 - stocks - cash)
	TaxDeferredStockPercent float64 `json:"tax_deferred_stock_percent"` // % of tax-deferred in stocks
	TaxDeferredCashPercent  float64 `json:"tax_deferred_cash_percent"`  // % of tax-deferred in cash
	RothStockPercent        float64 `json:"roth_stock_percent"`         // % of Roth in stocks
	RothCashPercent         float64 `json:"roth_cash_percent"`          // % of Roth in cash
	TaxableStockPercent     float64 `json:"taxable_stock_percent"`      // % of taxable in stocks
	TaxableCashPercent      float64 `json:"taxable_cash_percent"`       // % of taxable in cash

	// Legacy global Asset Allocation (deprecated, use per-account allocation above)
	StockPercent float64 `json:"stock_percent"` // % in stocks (default: 60)
	CashPercent  float64 `json:"cash_percent"`  // % in cash/money market (default: 0)
	// Bond % computed as: 100 - StockPercent - CashPercent

	// Rates (as percentages, e.g., 4.0 for 4%)
	InflationRate                       float64 `json:"inflation_rate"`                                // Annual inflation
	HealthcareInflation                 float64 `json:"healthcare_inflation"`                          // Healthcare inflation (legacy, for single-person model)
	PropertyTaxInflation                float64 `json:"property_tax_inflation"`                        // Property tax inflation (default 4%; reflects assessment growth above CPI)
	SpendingDeclineRate                 float64 `json:"spending_decline_rate"`                         // Annual spending reduction (used when phases disabled)
	InvestmentReturn                    float64 `json:"investment_return"`                             // Expected portfolio return
	DiscountRate                        float64 `json:"discount_rate"`                                 // For PV calculations
	TaxableDividendYield                float64 `json:"taxable_dividend_yield,omitempty"`              // Annual dividend yield on taxable account
	TaxableQualifiedDividendPercent     float64 `json:"taxable_qualified_dividend_percent,omitempty"`  // Share of taxable dividends that are qualified
	TaxableCapitalGainsDistributionRate float64 `json:"taxable_cap_gains_distribution_rate,omitempty"` // Annual realized cap-gains distribution rate

	// Phase-based spending (go-go/slow-go/no-go retirement phases)
	SpendingPhaseConfig *SpendingPhaseConfig `json:"spending_phase_config,omitempty"`

	// Projection
	ProjectionYears         int              `json:"projection_years"`            // Number of years to project
	ProjectionTiming        ProjectionTiming `json:"projection_timing,omitempty"` // When monthly cash flow occurs relative to growth
	SteadyStateOverrideYear float64          `json:"steady_state_override_year"`  // Projection year displayed by steady-state views (0 = year 0; use a negative value to defer to the auto-pick fallback)
	TaxDeferredDelayYears   int              `json:"tax_deferred_delay_years"`    // Years before tax-deferred withdrawals begin (0 = immediate)

	// RMD timing: when during each projection year the RMD withdrawal is taken.
	// F-035: empty string → NormalizeRMDTiming returns mid_year (new default).
	// Settings loading migrates legacy saved scenarios to start_of_year.
	RMDTiming RMDTiming `json:"rmd_timing,omitempty"`

	// Income and Expense Sources
	IncomeSources  []IncomeSource  `json:"income_sources"`
	ExpenseSources []ExpenseSource `json:"expense_sources"`

	// Recently Removed (for restore functionality)
	RemovedIncomeSources  []IncomeSource  `json:"removed_income_sources,omitempty"`
	RemovedExpenseSources []ExpenseSource `json:"removed_expense_sources,omitempty"`

	// Tax Configuration
	TaxConfig *TaxConfig `json:"tax_config,omitempty"`

	// Roth Conversion Strategy
	RothConversion *RothConversionConfig `json:"roth_conversion,omitempty"`

	// RothFirstFundedYear is the calendar tax year of the user's first
	// Roth IRA regular contribution or conversion contribution. It drives
	// the IRS qualified-distribution 5-tax-year rule for earnings.
	// Zero means unknown/unset, not necessarily "no Roth exists."
	RothFirstFundedYear int `json:"roth_first_funded_year,omitempty"`

	// Glide Path (time-based allocation shift)
	GlidePath *GlidePathConfig `json:"glide_path,omitempty"`

	// Spending Guardrails (portfolio-performance-based spending rules)
	Guardrails *GuardrailConfig `json:"guardrails,omitempty"`

	// Social Security Optimization
	SocialSecurity *SocialSecurityConfig `json:"social_security,omitempty"`

	// Big Ticket Items (one-time financial events)
	BigTicketItems        []BigTicketItem `json:"big_ticket_items,omitempty"`
	RemovedBigTicketItems []BigTicketItem `json:"removed_big_ticket_items,omitempty"`
}

// SocialSecurityConfig holds user's SS benefit info for claiming age analysis
type SocialSecurityConfig struct {
	FRABenefit       float64 `json:"fra_benefit"`                  // Monthly PIA (benefit at FRA)
	FRA              int     `json:"fra"`                          // Full retirement age (default 67)
	COLARate         float64 `json:"cola_rate"`                    // Annual COLA as decimal (default 0.02)
	COLARateSet      bool    `json:"cola_rate_set,omitempty"`      // F-026: distinguishes explicit 0 from unset
	SpouseFRABenefit float64 `json:"spouse_fra_benefit,omitempty"` // Spouse PIA if applicable
	SpouseFRA        int     `json:"spouse_fra,omitempty"`         // Spouse FRA
	ClaimAge         int     `json:"claim_age,omitempty"`          // Primary claiming age, 62-70; 0 means unset
	SpouseClaimAge   int     `json:"spouse_claim_age,omitempty"`   // Spouse claiming age, 62-70; 0 means unset
}

// CurrentLocalMonth returns the current local month as "YYYY-MM".
func CurrentLocalMonth() string {
	return time.Now().In(time.Local).Format(yearMonthLayout)
}

// ParseYearMonth parses a "YYYY-MM" string. Exported so packages outside
// models (notably the retirement engine's prepare package) can perform
// equivalent date validation.
func ParseYearMonth(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, fmt.Errorf("month is required")
	}
	t, err := time.Parse(yearMonthLayout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid month %q", value)
	}
	return t, nil
}

// BirthMonthForAge returns the "YYYY-MM" birth month that would produce
// the given integer age at the specified start date. Returns "" for
// negative ages or unparseable start dates.
func BirthMonthForAge(startDate string, age int) string {
	if age < 0 {
		return ""
	}
	start, err := ParseYearMonth(startDate)
	if err != nil {
		return ""
	}
	return start.AddDate(-age, 0, 0).Format(yearMonthLayout)
}

func DeriveAgeAtStartDate(startDate, birthMonth string) (int, error) {
	start, err := ParseYearMonth(startDate)
	if err != nil {
		return 0, err
	}
	birth, err := ParseYearMonth(birthMonth)
	if err != nil {
		return 0, err
	}

	months := (start.Year()-birth.Year())*12 + int(start.Month()) - int(birth.Month())
	if months < 0 {
		return 0, fmt.Errorf("birth month %q is after start date %q", birthMonth, startDate)
	}
	return months / 12, nil
}

// SpendingPhase represents a retirement spending phase with age-based multiplier
type SpendingPhase struct {
	Name        string  `json:"name"`        // "Go-Go", "Slow-Go", "No-Go"
	StartAge    int     `json:"start_age"`   // Age when phase begins
	Multiplier  float64 `json:"multiplier"`  // Spending multiplier (e.g., 1.0, 0.85, 0.70)
	Description string  `json:"description"` // User-friendly description
}

// SpendingPhaseConfig holds phase-based spending configuration
type SpendingPhaseConfig struct {
	Enabled bool            `json:"enabled"` // Toggle between simple decline and phase-based
	Phases  []SpendingPhase `json:"phases"`
}

// DefaultSpendingPhases returns research-based spending phase defaults
// Uses 5 phases for smoother transitions through retirement
func DefaultSpendingPhases() []SpendingPhase {
	return []SpendingPhase{
		{
			Name:        "Go-Go",
			StartAge:    0, // Starts at retirement (relative to CurrentAge)
			Multiplier:  1.00,
			Description: "Active retirement: travel, hobbies, dining out",
		},
		{
			Name:        "Active",
			StartAge:    65,
			Multiplier:  0.95,
			Description: "Still active but pacing slows slightly",
		},
		{
			Name:        "Slow-Go",
			StartAge:    75,
			Multiplier:  0.85,
			Description: "Reduced activity: less travel, more home-based",
		},
		{
			Name:        "Late Slow-Go",
			StartAge:    80,
			Multiplier:  0.75,
			Description: "Further reduced activity and travel",
		},
		{
			Name:        "No-Go",
			StartAge:    85,
			Multiplier:  0.65,
			Description: "Limited mobility: basic needs focus",
		},
	}
}

// HasSpouse returns true if spouse age is configured
func (s *WhatIfSettings) HasSpouse() bool {
	if s.GetSpousePerson() != nil {
		return true
	}
	return s.SpouseAge > 0
}

func (s *WhatIfSettings) GetPrimaryPerson() *Person {
	for i := range s.Persons {
		if s.Persons[i].Role == PersonRolePrimary {
			return &s.Persons[i]
		}
	}
	return nil
}

func (s *WhatIfSettings) GetSpousePerson() *Person {
	for i := range s.Persons {
		if s.Persons[i].Role == PersonRoleSpouse {
			return &s.Persons[i]
		}
	}
	return nil
}

func (s *WhatIfSettings) FindPerson(id string) *Person {
	for i := range s.Persons {
		if s.Persons[i].ID == id {
			return &s.Persons[i]
		}
	}
	return nil
}

func (s *WhatIfSettings) PersonAge(personID string) int {
	person := s.FindPerson(personID)
	if person == nil {
		return 0
	}
	age, err := DeriveAgeAtStartDate(s.StartDate, person.BirthMonth)
	if err != nil {
		return 0
	}
	return age
}

// Note: ComputeAges, NormalizePhaseAgeReference, and ValidatePersons used to
// live here as methods on *WhatIfSettings. They were extracted to the
// retirement engine's prepare package as package-level functions:
// budget2/internal/services/retirement/prepare. Call them as
// prepare.ComputeAges(s), prepare.NormalizePhaseAgeReference(s), and
// prepare.ValidatePersons(s).

// GetYoungerAge returns the younger of primary and spouse ages
func (s *WhatIfSettings) GetYoungerAge() int {
	if !s.HasSpouse() || s.SpouseAge >= s.CurrentAge {
		return s.CurrentAge
	}
	return s.SpouseAge
}

// GetOlderAge returns the older of primary and spouse ages
func (s *WhatIfSettings) GetOlderAge() int {
	if !s.HasSpouse() || s.SpouseAge <= s.CurrentAge {
		return s.CurrentAge
	}
	return s.SpouseAge
}

// GetPhaseReferenceAge returns the age to use for spending phase calculations
// based on PhaseAgeReference setting ("younger", "older", "primary", "spouse")
func (s *WhatIfSettings) GetPhaseReferenceAge(yearsElapsed int) int {
	var baseAge int
	switch s.PhaseAgeReference {
	case "spouse":
		if s.HasSpouse() {
			baseAge = s.SpouseAge
		} else {
			baseAge = s.CurrentAge
		}
	case "older":
		baseAge = s.GetOlderAge()
	case "younger":
		baseAge = s.GetYoungerAge()
	case "primary":
		baseAge = s.CurrentAge
	default:
		baseAge = s.GetOlderAge()
	}
	return baseAge + yearsElapsed
}

// PrimaryAgeAt returns primary person's age at a given year in the projection
func (s *WhatIfSettings) PrimaryAgeAt(year int) int {
	return s.CurrentAge + year
}

// SpouseAgeAt returns spouse's age at a given year in the projection (0 if no spouse)
func (s *WhatIfSettings) SpouseAgeAt(year int) int {
	if !s.HasSpouse() {
		return 0
	}
	return s.SpouseAge + year
}

// GetSpendingMultiplier returns the spending multiplier for a given age
// based on the phase configuration. Returns 1.0 if phases are disabled.
func (s *WhatIfSettings) GetSpendingMultiplier(age int) float64 {
	config := s.SpendingPhaseConfig
	if config == nil || !config.Enabled || len(config.Phases) == 0 {
		return 1.0 // No phase-based adjustment
	}

	// Find the applicable phase (phases sorted by StartAge ascending)
	multiplier := 1.0
	for _, phase := range config.Phases {
		if age >= phase.StartAge {
			multiplier = phase.Multiplier
		}
	}
	return multiplier
}

// SpendingMultiplierAt returns the phase multiplier for a calendar
// instant t, using the same phase-age reference (older/younger/primary/
// spouse) the projection uses. Returns 1.0 when phases are disabled or
// when StartDate is unparseable. yearsElapsed is computed from the
// projection StartDate using floor division so months before StartDate
// produce a negative offset (Go-Go's StartAge=0 still applies for
// reasonable historical ages).
func (s *WhatIfSettings) SpendingMultiplierAt(t time.Time) float64 {
	config := s.SpendingPhaseConfig
	if config == nil || !config.Enabled || len(config.Phases) == 0 {
		return 1.0
	}
	sd, err := ParseYearMonth(s.StartDate)
	if err != nil {
		return 1.0
	}
	monthsFromStart := (t.Year()-sd.Year())*12 + int(t.Month()) - int(sd.Month())
	yearsElapsed := monthsFromStart / 12
	if monthsFromStart < 0 && monthsFromStart%12 != 0 {
		yearsElapsed--
	}
	age := s.GetPhaseReferenceAge(yearsElapsed)
	return s.GetSpendingMultiplier(age)
}

// GetTotalHealthcareCost returns total healthcare cost for a given month
// Uses multi-person model if HealthcarePersons is populated, otherwise falls back to legacy single value
func (s *WhatIfSettings) GetTotalHealthcareCost(month int) float64 {
	// Use multi-person model if available
	if len(s.HealthcarePersons) > 0 {
		total := 0.0
		for _, person := range s.HealthcarePersons {
			// F-067: pass StartDate for month-precise ACA→Medicare transition when
			// BirthMonth is set on the HealthcarePerson.
			total += person.GetMonthlyCostAt(month, s.StartDate)
		}
		return total
	}

	// Legacy single-value model
	healthcareStartMonth := s.HealthcareStartYears * 12
	if month < healthcareStartMonth {
		return 0
	}

	// month >= healthcareStartMonth guaranteed by check above
	monthsActive := month - healthcareStartMonth

	// Apply healthcare inflation to legacy model
	return s.MonthlyHealthcare * math.Pow(1+s.HealthcareInflation/100, float64(monthsActive)/12.0)
}

// HasMultiPersonHealthcare returns true if multi-person healthcare model is being used
func (s *WhatIfSettings) HasMultiPersonHealthcare() bool {
	return len(s.HealthcarePersons) > 0
}

// AssetAllocationIsSet returns true if the user has explicitly configured asset allocation.
// This checks both legacy global fields and per-account allocation fields.
func (s *WhatIfSettings) AssetAllocationIsSet() bool {
	return s.StockPercent != 0 || s.CashPercent != 0 || s.PerAccountAllocationIsSet()
}

// GetEffectiveAssetAllocation returns normalized asset allocation percentages
// with defaults applied. Returns (stocks, bonds, cash) percentages.
// If no allocation is set, defaults to 60% stocks, 40% bonds, 0% cash.
func (s *WhatIfSettings) GetEffectiveAssetAllocation() (stockPercent, bondPercent, cashPercent float64) {
	stockPercent = s.StockPercent
	cashPercent = s.CashPercent

	if !s.AssetAllocationIsSet() {
		stockPercent = 60.0 // Default 60% stocks
		cashPercent = 0.0
	}

	bondPercent = 100.0 - stockPercent - cashPercent
	return stockPercent, bondPercent, cashPercent
}

// EffectiveStockPercent returns the stock percentage with default applied.
// This is useful for templates where a single value is needed.
func (s *WhatIfSettings) EffectiveStockPercent() float64 {
	stock, _, _ := s.GetEffectiveAssetAllocation()
	return stock
}

// EffectiveBondPercent returns the bond percentage calculated from stocks and cash.
func (s *WhatIfSettings) EffectiveBondPercent() float64 {
	_, bond, _ := s.GetEffectiveAssetAllocation()
	return bond
}

// PerAccountAllocationIsSet returns true if per-account allocation has been configured.
func (s *WhatIfSettings) PerAccountAllocationIsSet() bool {
	return s.TaxDeferredStockPercent != 0 || s.TaxDeferredCashPercent != 0 ||
		s.RothStockPercent != 0 || s.RothCashPercent != 0 ||
		s.TaxableStockPercent != 0 || s.TaxableCashPercent != 0
}

// GetTaxDeferredAllocation returns the asset allocation for tax-deferred accounts.
// If per-account allocation is enabled, uses the explicit values (even if 0).
// Otherwise falls back to global allocation or defaults (60/40/0).
func (s *WhatIfSettings) GetTaxDeferredAllocation() (stock, bond, cash float64) {
	// Check if user is in per-account allocation mode (any account has values set)
	// This allows explicit 0% stocks (100% bonds) to be honored
	if s.PerAccountAllocationIsSet() {
		stock = s.TaxDeferredStockPercent
		cash = s.TaxDeferredCashPercent
	} else {
		// Fall back to global allocation
		stock, _, cash = s.GetEffectiveAssetAllocation()
	}
	bond = 100.0 - stock - cash
	return stock, bond, cash
}

// GetRothAllocation returns the asset allocation for Roth accounts.
// If per-account allocation is enabled, uses the explicit values (even if 0).
// Otherwise falls back to global allocation or defaults (60/40/0).
func (s *WhatIfSettings) GetRothAllocation() (stock, bond, cash float64) {
	// Check if user is in per-account allocation mode (any account has values set)
	// This allows explicit 0% stocks (100% bonds) to be honored
	if s.PerAccountAllocationIsSet() {
		stock = s.RothStockPercent
		cash = s.RothCashPercent
	} else {
		// Fall back to global allocation
		stock, _, cash = s.GetEffectiveAssetAllocation()
	}
	bond = 100.0 - stock - cash
	return stock, bond, cash
}

// GetTaxableAllocation returns the asset allocation for taxable accounts.
// If per-account allocation is enabled, uses the explicit values (even if 0).
// Otherwise falls back to global allocation or defaults (60/40/0).
func (s *WhatIfSettings) GetTaxableAllocation() (stock, bond, cash float64) {
	// Check if user is in per-account allocation mode (any account has values set)
	// This allows explicit 0% stocks (100% bonds) to be honored
	if s.PerAccountAllocationIsSet() {
		stock = s.TaxableStockPercent
		cash = s.TaxableCashPercent
	} else {
		// Fall back to global allocation
		stock, _, cash = s.GetEffectiveAssetAllocation()
	}
	bond = 100.0 - stock - cash
	return stock, bond, cash
}

// GuardrailConfig defines portfolio-performance-based spending adjustment rules
type GuardrailConfig struct {
	Enabled         bool    `json:"enabled"`
	FloorDropPct    float64 `json:"floor_drop_pct"`    // Portfolio drop from peak to trigger cut (e.g., 20)
	FloorCutPct     float64 `json:"floor_cut_pct"`     // Spending reduction when floor hit (e.g., 10)
	CeilingRisePct  float64 `json:"ceiling_rise_pct"`  // Portfolio rise above initial to trigger raise (e.g., 20)
	CeilingRaisePct float64 `json:"ceiling_raise_pct"` // Spending increase when ceiling hit (e.g., 10)
	MinSpendingPct  float64 `json:"min_spending_pct"`  // Floor: never below X% of original (e.g., 75)
	MaxSpendingPct  float64 `json:"max_spending_pct"`  // Cap: never above X% of original (e.g., 120)
}

// GuardrailEvent records when a guardrail triggered during projection
type GuardrailEvent struct {
	Year                  int     `json:"year"`
	Type                  string  `json:"type"`                          // "cut" or "raise"
	Multiplier            float64 `json:"multiplier"`                    // New (cumulative) spending multiplier
	PreviousMultiplier    float64 `json:"previous_multiplier,omitempty"` // Multiplier just before this event fired
	Portfolio             float64 `json:"portfolio"`                     // Portfolio value at time
	MonthlySpendingBefore float64 `json:"monthly_spending_before,omitempty"`
	MonthlySpendingAfter  float64 `json:"monthly_spending_after,omitempty"`
	CumulativeInflation   float64 `json:"cumulative_inflation,omitempty"` // Compounded inflation factor from start to event year (1.0 = no inflation)
}

// GlidePathConfig defines a linear shift in stock allocation over time
type GlidePathConfig struct {
	Enabled         bool    `json:"enabled"`
	StartStockPct   float64 `json:"start_stock_pct"`  // Stock % at year 0
	EndStockPct     float64 `json:"end_stock_pct"`    // Stock % at end of transition
	TransitionYears int     `json:"transition_years"` // Years over which to shift
}

// GlidePathStockPct returns the target stock % at a given projection year.
// Returns -1 if glide path is not enabled.
func (s *WhatIfSettings) GlidePathStockPct(year int) float64 {
	if s.GlidePath == nil || !s.GlidePath.Enabled || s.GlidePath.TransitionYears <= 0 {
		return -1
	}
	if year >= s.GlidePath.TransitionYears {
		return s.GlidePath.EndStockPct
	}
	if year <= 0 {
		return s.GlidePath.StartStockPct
	}
	progress := float64(year) / float64(s.GlidePath.TransitionYears)
	return s.GlidePath.StartStockPct + progress*(s.GlidePath.EndStockPct-s.GlidePath.StartStockPct)
}

// GetAllocationAtYear returns per-account allocation adjusted for glide path.
// When glide path is disabled, returns the same as the static getters.
func (s *WhatIfSettings) GetAllocationAtYear(year int) (tdStock, tdBond, tdCash, rothStock, rothBond, rothCash, taxStock, taxBond, taxCash float64) {
	tdStock, tdBond, tdCash = s.GetTaxDeferredAllocation()
	rothStock, rothBond, rothCash = s.GetRothAllocation()
	taxStock, taxBond, taxCash = s.GetTaxableAllocation()

	targetStock := s.GlidePathStockPct(year)
	if targetStock < 0 {
		return
	}

	applyGlide := func(cash float64) (float64, float64, float64) {
		b := 100.0 - targetStock - cash
		if b < 0 {
			b = 0
			cash = 100.0 - targetStock
		}
		return targetStock, b, cash
	}

	tdStock, tdBond, tdCash = applyGlide(tdCash)
	rothStock, rothBond, rothCash = applyGlide(rothCash)
	taxStock, taxBond, taxCash = applyGlide(taxCash)
	return
}

// GetBlendedReturn calculates the expected annual return for an account based on its allocation.
// Uses historical means for each asset class.
func GetBlendedReturn(stockPct, bondPct, cashPct float64, stockMean, bondMean, cashMean float64) float64 {
	return (stockPct/100)*stockMean + (bondPct/100)*bondMean + (cashPct/100)*cashMean
}

// GetExpectedReturnFromAllocation calculates the overall expected return based on
// portfolio weights and per-account asset allocations using conservative estimates.
// This is displayed in the UI when InvestmentReturn is 0 (allocation-based mode).
// Uses conservative forward-looking estimates rather than historical averages.
func (s *WhatIfSettings) GetExpectedReturnFromAllocation() float64 {
	// Conservative forward-looking estimates (more prudent for retirement planning)
	// Historical averages (~10.5% stocks, ~5.2% bonds) are arguably too optimistic
	stockMean := 7.0
	bondMean := 4.0
	cashMean := 3.0

	// Get per-account allocations
	tdStock, tdBond, tdCash := s.GetTaxDeferredAllocation()
	rothStock, rothBond, rothCash := s.GetRothAllocation()
	taxStock, taxBond, taxCash := s.GetTaxableAllocation()

	// Get account weights
	tdWeight := s.TaxDeferredPercent / 100
	rothWeight := s.RothPercent / 100
	taxWeight := 1.0 - tdWeight - rothWeight
	if taxWeight < 0 {
		taxWeight = 0
	}

	// Calculate weighted-average return
	tdReturn := GetBlendedReturn(tdStock, tdBond, tdCash, stockMean, bondMean, cashMean)
	rothReturn := GetBlendedReturn(rothStock, rothBond, rothCash, stockMean, bondMean, cashMean)
	taxReturn := GetBlendedReturn(taxStock, taxBond, taxCash, stockMean, bondMean, cashMean)

	return tdWeight*tdReturn + rothWeight*rothReturn + taxWeight*taxReturn
}

// Template helper methods for per-account allocation display
func (s *WhatIfSettings) TaxDeferredStockPct() float64 {
	stock, _, _ := s.GetTaxDeferredAllocation()
	return stock
}
func (s *WhatIfSettings) TaxDeferredBondPct() float64 {
	_, bond, _ := s.GetTaxDeferredAllocation()
	return bond
}
func (s *WhatIfSettings) TaxDeferredCashPct() float64 {
	_, _, cash := s.GetTaxDeferredAllocation()
	return cash
}
func (s *WhatIfSettings) RothStockPct() float64 {
	stock, _, _ := s.GetRothAllocation()
	return stock
}
func (s *WhatIfSettings) RothBondPct() float64 {
	_, bond, _ := s.GetRothAllocation()
	return bond
}
func (s *WhatIfSettings) RothCashPct() float64 {
	_, _, cash := s.GetRothAllocation()
	return cash
}
func (s *WhatIfSettings) TaxableStockPct() float64 {
	stock, _, _ := s.GetTaxableAllocation()
	return stock
}
func (s *WhatIfSettings) TaxableBondPct() float64 {
	_, bond, _ := s.GetTaxableAllocation()
	return bond
}
func (s *WhatIfSettings) TaxableCashPct() float64 {
	_, _, cash := s.GetTaxableAllocation()
	return cash
}

// DefaultWhatIfSettings returns sensible defaults for retirement planning
func DefaultWhatIfSettings() *WhatIfSettings {
	startDate := CurrentLocalMonth()
	settings := &WhatIfSettings{
		PortfolioValue:        0,
		MonthlyLivingExpenses: 4000,
		MonthlyHealthcare:     500,
		HealthcareStartYears:  0,
		StartDate:             startDate,
		Persons: []Person{
			{
				ID:         uuid.New().String(),
				Name:       "You",
				BirthMonth: BirthMonthForAge(startDate, 65),
				Role:       PersonRolePrimary,
			},
		},
		CurrentAge:         65,
		SpouseAge:          0,
		PhaseAgeReference:  "older",
		TaxDeferredPercent: 60.0, // Reduced from 70 to make room for Roth
		RothPercent:        10.0, // Default 10% Roth
		// Taxable is computed as: 100 - 60 - 10 = 30%
		StockPercent:                    60.0, // Default 60% stocks
		CashPercent:                     0.0,  // Default 0% cash (bonds = 40%)
		InflationRate:                   3.0,
		HealthcareInflation:             6.0,
		PropertyTaxInflation:            4.0,
		SpendingDeclineRate:             1.0,
		InvestmentReturn:                0.0, // 0 = use asset allocation to calculate returns
		DiscountRate:                    5.0,
		TaxableQualifiedDividendPercent: 100.0,
		ProjectionYears:                 30,
		ProjectionTiming:                ProjectionTimingEndOfMonth,
		TaxDeferredDelayYears:           0,
		IncomeSources:                   []IncomeSource{},
		ExpenseSources:                  []ExpenseSource{},
		RemovedIncomeSources:            []IncomeSource{},
		RemovedExpenseSources:           []ExpenseSource{},
		// Phase-based spending (disabled by default to preserve existing behavior)
		SpendingPhaseConfig: &SpendingPhaseConfig{
			Enabled: false,
			Phases:  DefaultSpendingPhases(),
		},
		// Tax configuration
		TaxConfig: DefaultTaxConfig(),
		// Roth conversions (disabled by default)
		RothConversion: &RothConversionConfig{
			Enabled: false,
		},
		// Big ticket items (empty by default)
		BigTicketItems:        []BigTicketItem{},
		RemovedBigTicketItems: []BigTicketItem{},
	}
	// Derived state (CurrentAge/SpouseAge) is populated by prepare.From at the
	// engine boundary. The CurrentAge field above (65) matches the primary
	// Person's BirthMonth so the value is consistent until preparation runs.
	return settings
}

func (s *WhatIfSettings) GetProjectionTiming() ProjectionTiming {
	return NormalizeProjectionTiming(s.ProjectionTiming)
}

func (s *WhatIfSettings) GetTaxableQualifiedDividendPercent() float64 {
	switch {
	case s.TaxableQualifiedDividendPercent < 0:
		return 0
	case s.TaxableQualifiedDividendPercent > 100:
		return 100
	default:
		return s.TaxableQualifiedDividendPercent
	}
}

// ProjectionMonth represents a single month in the projection
type ProjectionMonth struct {
	Month                int     `json:"month"`
	Year                 float64 `json:"year"`
	CumulativeInflation  float64 `json:"cumulative_inflation,omitempty"`
	PortfolioBalance     float64 `json:"portfolio_balance"`
	PortfolioBalanceReal float64 `json:"portfolio_balance_real,omitempty"`
	TaxDeferredBalance   float64 `json:"tax_deferred_balance"` // Tax-deferred portion (401k, IRA)
	TaxableBalance       float64 `json:"taxable_balance"`      // Taxable portion (brokerage)
	RothBalance          float64 `json:"roth_balance"`         // Roth portion (Roth IRA, Roth 401k)
	GeneralExpenses      float64 `json:"general_expenses"`
	HealthcareExpense    float64 `json:"healthcare_expense"`
	TotalExpenses        float64 `json:"total_expenses"`
	TotalExpensesReal    float64 `json:"total_expenses_real,omitempty"`
	TotalIncome          float64 `json:"total_income"`
	TotalIncomeReal      float64 `json:"total_income_real,omitempty"`
	SocialSecurityIncome float64 `json:"social_security_income,omitempty"` // SS portion of TotalIncome (manual sources, or SS-optimizer output when active)
	GrossIncome          float64 `json:"gross_income,omitempty"`
	NetIncome            float64 `json:"net_income,omitempty"`
	TaxesPaid            float64 `json:"taxes_paid,omitempty"`
	NetWithdrawal        float64 `json:"net_withdrawal"`
	RMDWithdrawal        float64 `json:"rmd_withdrawal"` // Forced RMD withdrawal (age 73+)
	TaxableWithdrawals   float64 `json:"taxable_withdrawals,omitempty"`
	RothConversions      float64 `json:"roth_conversions,omitempty"`
	PortfolioGrowth      float64 `json:"portfolio_growth"`
	Depleted             bool    `json:"depleted"`

	// Withdrawal source tracking
	WithdrawalFromTaxDeferred float64 `json:"withdrawal_tax_deferred,omitempty"`
	WithdrawalFromTaxable     float64 `json:"withdrawal_taxable,omitempty"`
	WithdrawalFromRoth        float64 `json:"withdrawal_roth,omitempty"`

	// Guardrail visibility (F-079)
	PlannedLivingExpenses float64 `json:"planned_living_expenses,omitempty"` // Pre-guardrail-multiplier living expense for the month
	GuardrailMultiplier   float64 `json:"guardrail_multiplier"`              // Active guardrail spending multiplier (1.0 if disabled); not omitempty so 0 vs 1 stays unambiguous
}

// ProjectionResult contains the complete projection with summary metrics
type ProjectionResult struct {
	Months          []ProjectionMonth       `json:"months"`
	YearlySummaries []ProjectionYearSummary `json:"yearly_summaries,omitempty"`
	LongevityYears  *float64                `json:"longevity_years"` // nil if portfolio survives
	FinalBalance    float64                 `json:"final_balance"`
	DepletionMonth  *int                    `json:"depletion_month"` // nil if no depletion
	Survives        bool                    `json:"survives"`
	GuardrailEvents []GuardrailEvent        `json:"guardrail_events,omitempty"`
}

// ProjectionYearSummary reconciles one projection year for explainability.
type ProjectionYearSummary struct {
	Year                     int     `json:"year"`
	StartingBalance          float64 `json:"starting_balance"`
	Growth                   float64 `json:"growth"`
	GrossIncome              float64 `json:"gross_income"`
	MAGI                     float64 `json:"magi,omitempty"`
	Taxes                    float64 `json:"taxes"`
	NIIT                     float64 `json:"niit,omitempty"`
	IRMAA                    float64 `json:"irmaa,omitempty"`
	TaxableSocialSecurityPct float64 `json:"taxable_social_security_pct,omitempty"`
	Expenses                 float64 `json:"expenses"`
	Withdrawals              float64 `json:"withdrawals"`
	EndingBalance            float64 `json:"ending_balance"`
	EndingBalanceReal        float64 `json:"ending_balance_real"`
	CumulativeInflation      float64 `json:"cumulative_inflation"`

	// Guardrail visibility (F-079)
	PlannedExpenses     float64 `json:"planned_expenses,omitempty"` // Total expenses for the year as if no guardrail multiplier were applied; accumulates alongside Expenses in the projection loop
	GuardrailMultiplier float64 `json:"guardrail_multiplier"`       // Multiplier in effect at year-end (1.0 if disabled); not omitempty so 0 vs 1 stays unambiguous

	// Roth 5-year rule: taxable Roth earnings withdrawn before the clock matures.
	TaxableRothEarnings float64 `json:"taxable_roth_earnings,omitempty"`
}

// ProjectionExplainability contains reconciliation data for the projection UI.
type ProjectionExplainability struct {
	YearlySummaries         []ProjectionYearSummary `json:"yearly_summaries"`
	TotalTaxes              float64                 `json:"total_taxes"`
	TotalGrossIncome        float64                 `json:"total_gross_income"`
	TaxShareOfGrossCashFlow float64                 `json:"tax_share_of_gross_cash_flow"`
	FinalBalanceReal        float64                 `json:"final_balance_real"`
	CumulativeInflation     float64                 `json:"cumulative_inflation"`
	InflationLossPercent    float64                 `json:"inflation_loss_percent"`
}

// ExpenseBreakdownItem shows a named expense component
type ExpenseBreakdownItem struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Note   string  `json:"note,omitempty"` // e.g., "employer covered", "ends year 3"
}

// BudgetFitAnalysis shows monthly gap and required rates
type BudgetFitAnalysis struct {
	MonthlyExpenses          float64 `json:"monthly_expenses"`
	MonthlyIncome            float64 `json:"monthly_income"`
	GrossIncome              float64 `json:"gross_income,omitempty"`
	NetIncome                float64 `json:"net_income,omitempty"`
	MonthlyTaxes             float64 `json:"monthly_taxes,omitempty"`
	MonthlyStateTax          float64 `json:"monthly_state_tax,omitempty"`
	MonthlyNIIT              float64 `json:"monthly_niit,omitempty"`
	MonthlyIRMAA             float64 `json:"monthly_irmaa,omitempty"`
	TaxableSocialSecurityPct float64 `json:"taxable_social_security_pct,omitempty"`
	MonthlyRMD               float64 `json:"monthly_rmd"` // Required Minimum Distribution (age 73+)
	MonthlyGap               float64 `json:"monthly_gap"` // Expenses - Income - RMD
	AnnualGap                float64 `json:"annual_gap"`
	RequiredRate             float64 `json:"required_rate"` // Rate needed to cover gap

	// Suggested withdrawal mix to close MonthlyGap, split proportionally
	// across the user's portfolio allocation (TaxDeferred / Roth /
	// Taxable). NetWithdrawal* sum to MonthlyGap. GrossWithdrawal* show
	// the actual amount to pull from each bucket (>= net for Tax-Deferred
	// because part is lost to income tax). All zero when MonthlyGap <= 0.
	GrossWithdrawalTaxDeferred float64 `json:"gross_withdrawal_tax_deferred,omitempty"`
	NetWithdrawalTaxDeferred   float64 `json:"net_withdrawal_tax_deferred,omitempty"`
	MarginalRateTaxDeferred    float64 `json:"marginal_rate_tax_deferred,omitempty"` // % 0-100
	GrossWithdrawalTaxable     float64 `json:"gross_withdrawal_taxable,omitempty"`
	NetWithdrawalTaxable       float64 `json:"net_withdrawal_taxable,omitempty"`
	EffectiveRateTaxable       float64 `json:"effective_rate_taxable,omitempty"` // % 0-100
	GrossWithdrawalRoth        float64 `json:"gross_withdrawal_roth,omitempty"`
	NetWithdrawalRoth          float64 `json:"net_withdrawal_roth,omitempty"`

	// Breakdowns for transparency
	ExpenseBreakdown []ExpenseBreakdownItem `json:"expense_breakdown,omitempty"`
	IncomeBreakdown  []ExpenseBreakdownItem `json:"income_breakdown,omitempty"` // reuses same struct

	// RMD/Gap relationship fields
	GapBeforeRMD float64 `json:"gap_before_rmd"` // Expenses - Income (before RMD applied)
	RMDCoverage  float64 `json:"rmd_coverage"`   // How much of the gap RMD covers
	ExcessRMD    float64 `json:"excess_rmd"`     // RMD beyond what's needed (forced taxable withdrawal)

	// Steady-state analysis (when all income sources are active)
	SteadyStateMonth                    int     `json:"steady_state_month"`    // Month when all income is active
	SteadyStateYear                     float64 `json:"steady_state_year"`     // Year when all income is active (or override)
	MinSteadyStateYear                  float64 `json:"min_steady_state_year"` // Auto-calculated minimum (when all income starts)
	SteadyStateExpenses                 float64 `json:"steady_state_expenses"` // Expenses at steady state (inflated)
	SteadyStateIncome                   float64 `json:"steady_state_income"`   // Income at steady state (with COLA)
	SteadyStateGrossIncome              float64 `json:"steady_state_gross_income,omitempty"`
	SteadyStateNetIncome                float64 `json:"steady_state_net_income,omitempty"`
	SteadyStateTaxes                    float64 `json:"steady_state_taxes,omitempty"`
	SteadyStateStateTax                 float64 `json:"steady_state_state_tax,omitempty"`
	SteadyStateNIIT                     float64 `json:"steady_state_niit,omitempty"`
	SteadyStateIRMAA                    float64 `json:"steady_state_irmaa,omitempty"`
	SteadyStateTaxableSocialSecurityPct float64 `json:"steady_state_taxable_social_security_pct,omitempty"`
	SteadyStateRMD                      float64 `json:"steady_state_rmd"`  // RMD at steady state (if applicable)
	SteadyStateGap                      float64 `json:"steady_state_gap"`  // Gap at steady state
	SteadyStateRate                     float64 `json:"steady_state_rate"` // Required withdrawal rate at steady state

	// Suggested withdrawal mix to close SteadyStateGap, split
	// proportionally across the user's portfolio allocation. Net* sum to
	// SteadyStateGap. All zero when SteadyStateGap <= 0.
	SteadyStateGrossWithdrawalTaxDeferred float64 `json:"steady_state_gross_withdrawal_tax_deferred,omitempty"`
	SteadyStateNetWithdrawalTaxDeferred   float64 `json:"steady_state_net_withdrawal_tax_deferred,omitempty"`
	SteadyStateMarginalRateTaxDeferred    float64 `json:"steady_state_marginal_rate_tax_deferred,omitempty"`
	SteadyStateGrossWithdrawalTaxable     float64 `json:"steady_state_gross_withdrawal_taxable,omitempty"`
	SteadyStateNetWithdrawalTaxable       float64 `json:"steady_state_net_withdrawal_taxable,omitempty"`
	SteadyStateEffectiveRateTaxable       float64 `json:"steady_state_effective_rate_taxable,omitempty"`
	SteadyStateGrossWithdrawalRoth        float64 `json:"steady_state_gross_withdrawal_roth,omitempty"`
	SteadyStateNetWithdrawalRoth          float64 `json:"steady_state_net_withdrawal_roth,omitempty"`

	HasSteadyState bool `json:"has_steady_state"` // True if steady state differs from current
}

// RMDProjection represents RMD estimates for a specific year
type RMDProjection struct {
	Age            int     `json:"age"`
	Year           int     `json:"year"`             // Years from now
	TaxDeferredBal float64 `json:"tax_deferred_bal"` // Estimated balance at start of year
	LifeExpFactor  float64 `json:"life_exp_factor"`  // IRS Uniform Lifetime factor
	RMDAmount      float64 `json:"rmd_amount"`       // Required distribution
	RMDPercent     float64 `json:"rmd_percent"`      // RMD as % of tax-deferred balance
}

// RMDAnalysis contains RMD projections and summary
type RMDAnalysis struct {
	StartsInYears     int             `json:"starts_in_years"` // Years until RMDs begin
	StartAge          int             `json:"start_age"`       // Age when RMDs begin (73)
	CurrentAge        int             `json:"current_age"`
	TaxDeferredValue  float64         `json:"tax_deferred_value"` // Current tax-deferred balance
	Projections       []RMDProjection `json:"projections"`        // Year-by-year projections
	TotalRMDsOver10Yr float64         `json:"total_rmds_10yr"`    // Sum of first 10 years of RMDs

	// F-072: depletion context driven by the actual projection.
	DepletionYear     *int `json:"depletion_year,omitempty"` // year index of portfolio depletion; nil if survives
	DepletionAge      *int `json:"depletion_age,omitempty"`  // older-person age at depletion year
	DepletedBeforeRMD bool `json:"depleted_before_rmd"`      // true when depletion precedes the first RMD year
}

// PresentValueAnalysis shows PV of expenses vs income
type PresentValueAnalysis struct {
	PVExpenses     float64 `json:"pv_expenses"`         // Living + healthcare + property tax + expense sources
	PVTaxes        float64 `json:"pv_taxes,omitempty"`  // Discounted income taxes + IRMAA from the projection (0 when no projection is supplied)
	PVIncome       float64 `json:"pv_income"`
	PVGap          float64 `json:"pv_gap"`          // (PV Expenses + PV Taxes) - PV Income
	CoverageRatio  float64 `json:"coverage_ratio"`  // (Portfolio + PV Income) / (PV Expenses + PV Taxes)
	SurplusDeficit float64 `json:"surplus_deficit"` // Portfolio + PV Income - PV Expenses - PV Taxes
}

// SustainabilityScore represents a 0-100 score with visual attributes
type SustainabilityScore struct {
	Score       int    `json:"score"` // 0-100
	Label       string `json:"label"` // "Excellent", "Good", "Fair", "Poor", "Critical"
	Color       string `json:"color"` // CSS color class
	Description string `json:"description"`
}

// CalculateSustainabilityScore computes score from withdrawal rate
func CalculateSustainabilityScore(requiredRate float64, survives bool) *SustainabilityScore {
	var score int
	var label, color, description string

	if !survives {
		score = 0
		label = "Critical"
		color = "red"
		description = "Portfolio depletes before projection end"
	} else if requiredRate <= 3 {
		score = 100
		label = "Excellent"
		color = "green"
		description = "Very sustainable withdrawal rate"
	} else if requiredRate <= 4 {
		score = 90
		label = "Good"
		color = "green"
		description = "Sustainable based on 4% rule"
	} else if requiredRate <= 5 {
		score = 75
		label = "Fair"
		color = "yellow"
		description = "Moderate risk, consider reducing expenses"
	} else if requiredRate <= 6 {
		score = 60
		label = "Caution"
		color = "orange"
		description = "Higher risk of depletion"
	} else if requiredRate <= 8 {
		score = 40
		label = "Poor"
		color = "orange"
		description = "High withdrawal rate, adjustments recommended"
	} else {
		score = int(max(0, 100-(requiredRate-3)*15))
		label = "Critical"
		color = "red"
		description = "Unsustainable withdrawal rate"
	}

	return &SustainabilityScore{
		Score:       score,
		Label:       label,
		Color:       color,
		Description: description,
	}
}

// SensitivityScenario defines a parameter variation for testing
type SensitivityScenario struct {
	Name       string  `json:"name"`
	ParamName  string  `json:"param_name"`
	ParamValue float64 `json:"param_value"`
	Change     string  `json:"change"` // e.g., "+2%", "-1%"
}

// SensitivityResult contains the outcome of a scenario test
type SensitivityResult struct {
	Scenario       SensitivityScenario `json:"scenario"`
	LongevityYears *float64            `json:"longevity_years"`
	FinalBalance   float64             `json:"final_balance"`
	Survives       bool                `json:"survives"`
	ScoreChange    int                 `json:"score_change"` // vs baseline
}

// FailurePoint represents the threshold where a parameter causes portfolio failure
type FailurePoint struct {
	ParamName    string  `json:"param_name"`    // e.g., "investment_return"
	ParamLabel   string  `json:"param_label"`   // e.g., "Investment Return"
	CurrentValue float64 `json:"current_value"` // Current setting value
	Threshold    float64 `json:"threshold"`     // Value at which failure occurs
	Direction    string  `json:"direction"`     // "below" or "above"
	Margin       float64 `json:"margin"`        // How much buffer before failure (as %)
	SafetyLevel  string  `json:"safety_level"`  // "safe", "marginal", "critical"
}

// FailurePointAnalysis contains all failure thresholds
type FailurePointAnalysis struct {
	FailurePoints    []FailurePoint `json:"failure_points"`
	BaselineSurvives bool           `json:"baseline_survives"` // Does current scenario survive?
}

// MonteCarloResult represents a single simulation run outcome
type MonteCarloResult struct {
	FinalBalance    float64 `json:"final_balance"`
	DepletionYear   float64 `json:"depletion_year"` // 0 if survives
	Survives        bool    `json:"survives"`
	MarketCrashes   int     `json:"market_crashes"`   // Number of crash years
	SpendingShocks  int     `json:"spending_shocks"`  // Number of spending shock events
	HealthShocks    int     `json:"health_shocks"`    // Number of health emergency events
	ProjectionYears int     `json:"projection_years"` // Actual years projected (varies with longevity)

	// Crash timing breakdown
	EarlyCrashes   int `json:"early_crashes"`    // Crashes in years 1-5
	MidCrashes     int `json:"mid_crashes"`      // Crashes in years 6-15
	LateCrashes    int `json:"late_crashes"`     // Crashes in years 16+
	FirstCrashYear int `json:"first_crash_year"` // Year of first crash (0 if none)
}

// SequenceRiskBreakdown provides detailed crash timing analysis
type SequenceRiskBreakdown struct {
	// Survival rates by crash timing
	NoCrashSurvival    float64 `json:"no_crash_survival"`    // Survival rate with no crashes
	EarlyCrashSurvival float64 `json:"early_crash_survival"` // Survival rate when crashes in years 1-5
	MidCrashSurvival   float64 `json:"mid_crash_survival"`   // Survival rate when crashes in years 6-15
	LateCrashSurvival  float64 `json:"late_crash_survival"`  // Survival rate when crashes in years 16+

	// Sample sizes for each category
	NoCrashCount    int `json:"no_crash_count"`
	EarlyCrashCount int `json:"early_crash_count"`
	MidCrashCount   int `json:"mid_crash_count"`
	LateCrashCount  int `json:"late_crash_count"`

	// Impact metrics
	EarlyVsLateImpact float64 `json:"early_vs_late_impact"` // Difference: late survival - early survival
	EarlyVsNoneImpact float64 `json:"early_vs_none_impact"` // Difference: no crash survival - early survival

	// Recovery analysis
	RecoveryRate     float64 `json:"recovery_rate"`      // % of early crash runs that still survived
	AvgRecoveryYears float64 `json:"avg_recovery_years"` // Avg years to recover after early crash

	// Buffer recommendation (years of expenses to hold safe)
	RecommendedBuffer int     `json:"recommended_buffer"`
	BufferRationale   string  `json:"buffer_rationale"`
	BufferAmount      float64 `json:"buffer_amount"`     // Dollar amount of recommended buffer
	AnnualExpenses    float64 `json:"annual_expenses"`   // Annual expenses used for buffer calculation
	AdjustedSpending  float64 `json:"adjusted_spending"` // Monthly spending if buffer is set aside from portfolio

	// Buffer calculation breakdown (accounts for partial portfolio value during crashes)
	CrashDrawdownPercent      float64 `json:"crash_drawdown_percent"`       // Expected portfolio drop during crash (e.g., 30%)
	CrashedPortfolioValue     float64 `json:"crashed_portfolio_value"`      // Portfolio value after crash
	SafeWithdrawalDuringCrash float64 `json:"safe_withdrawal_during_crash"` // Annual safe withdrawal from crashed portfolio
	AnnualShortfall           float64 `json:"annual_shortfall"`             // Gap between expenses and safe withdrawal
	NaiveBufferAmount         float64 `json:"naive_buffer_amount"`          // What buffer would be without accounting for portfolio

	// Adaptive spending analysis (discretionary expense flexibility)
	HasDiscretionary          bool    `json:"has_discretionary"`            // Whether user has discretionary expenses
	MonthlyDiscretionary      float64 `json:"monthly_discretionary"`        // Monthly discretionary expenses
	MonthlyEssential          float64 `json:"monthly_essential"`            // Monthly essential (non-discretionary) expenses
	DiscretionaryCutPercent   float64 `json:"discretionary_cut_percent"`    // % to cut during crashes (e.g., 40)
	EarlyCrashSurvivalAdapted float64 `json:"early_crash_survival_adapted"` // Survival with spending adaptation
	AdaptationBoost           float64 `json:"adaptation_boost"`             // Improvement from adaptation (percentage points)
	AdaptationRationale       string  `json:"adaptation_rationale"`         // Explanation of adaptation benefit
}

// MonteCarloStats contains aggregated simulation statistics
type MonteCarloStats struct {
	Runs           int     `json:"runs"`             // Number of simulations
	SuccessRate    float64 `json:"success_rate"`     // % of scenarios that survive
	MedianBalance  float64 `json:"median_balance"`   // Median final balance
	MeanBalance    float64 `json:"mean_balance"`     // Average final balance
	Percentile10   float64 `json:"percentile_10"`    // 10th percentile (worst 10%)
	Percentile25   float64 `json:"percentile_25"`    // 25th percentile
	Percentile75   float64 `json:"percentile_75"`    // 75th percentile
	Percentile90   float64 `json:"percentile_90"`    // 90th percentile (best 10%)
	WorstCase      float64 `json:"worst_case"`       // Minimum final balance
	BestCase       float64 `json:"best_case"`        // Maximum final balance
	AvgDepletionYr float64 `json:"avg_depletion_yr"` // Avg years to depletion (failed runs only)

	// Enhanced simulation stats
	MarketCrashCount   int     `json:"market_crash_count"`   // Runs that experienced crashes
	SpendingShockCount int     `json:"spending_shock_count"` // Runs with spending shocks
	HealthShockCount   int     `json:"health_shock_count"`   // Runs with health emergencies
	AvgCrashesPerRun   float64 `json:"avg_crashes_per_run"`  // Average market crashes per simulation
	AvgShocksPerRun    float64 `json:"avg_shocks_per_run"`   // Average spending shocks per simulation
	SequenceRiskImpact float64 `json:"sequence_risk_impact"` // How much sequence of returns affected outcomes

	// Detailed sequence risk analysis
	SequenceRisk *SequenceRiskBreakdown `json:"sequence_risk"`
}

// MonteCarloDistribution contains bucketed results for visualization
type MonteCarloDistribution struct {
	Buckets []MonteCarloDistBucket `json:"buckets"`
}

// MonteCarloDistBucket represents a histogram bucket
type MonteCarloDistBucket struct {
	Label      string  `json:"label"`      // e.g., "$0-100K"
	Count      int     `json:"count"`      // Number of simulations in this bucket
	Percentage float64 `json:"percentage"` // % of total
}

// MonteCarloAnalysis contains complete simulation analysis
type MonteCarloAnalysis struct {
	Stats        *MonteCarloStats        `json:"stats"`
	Distribution *MonteCarloDistribution `json:"distribution"`
}

// FilingStatus represents IRS tax filing status
type FilingStatus string

const (
	FilingSingle          FilingStatus = "single"
	FilingMarriedJoint    FilingStatus = "married_joint"
	FilingMarriedSeparate FilingStatus = "married_separate"
	FilingHeadOfHousehold FilingStatus = "head_of_household"
)

// TaxConfig holds tax modeling settings
type TaxConfig struct {
	FilingStatus       FilingStatus `json:"filing_status"`
	StateIncomeTaxRate *float64     `json:"state_income_tax_rate,omitempty"` // nil = unset; *0 = explicit no-tax state; *x = configured rate
	Age65Count         int          `json:"age_65_count"`                    // F-001: number of filers 65 or older (0, 1, or 2 for MFJ).
	MFSLivedWithSpouse bool         `json:"mfs_lived_with_spouse"`           // F-018: 26 USC § 86(c)(2) sub-case; true = lived with spouse → $0/$0 thresholds.
}

// DefaultTaxConfig returns sensible tax defaults
func DefaultTaxConfig() *TaxConfig {
	return &TaxConfig{
		FilingStatus:       FilingSingle,
		StateIncomeTaxRate: nil, // unset; user must explicitly set (incl. 0 for no-tax states)
	}
}

// StateIncomeTaxRateOrZero returns the configured state rate, or 0 if
// the TaxConfig pointer or rate is nil/zero. Use this at engine and
// math boundaries; use direct nil checks at completeness/validation
// boundaries where "unset" semantics matter.
func (t *TaxConfig) StateIncomeTaxRateOrZero() float64 {
	if t == nil || t.StateIncomeTaxRate == nil {
		return 0
	}
	return *t.StateIncomeTaxRate
}

// RothConversionConfig models annual Roth conversions
type RothConversionConfig struct {
	Enabled      bool    `json:"enabled"`
	AnnualAmount float64 `json:"annual_amount"` // Fixed amount to convert per year
	StartYear    int     `json:"start_year"`    // Year to begin conversions (0 = now)
	EndYear      int     `json:"end_year"`      // Year to stop conversions (0 = indefinite)

	// PerYearOverrides is keyed by projection-year offset (same key semantics
	// as StartYear/EndYear). When non-nil and a key is present for the
	// current year, the engine uses that override amount instead of
	// AnnualAmount. A zero value in the map suppresses the conversion for
	// that year (used by bracket-fill when other income already fills the
	// target bracket). Excluded from JSON serialization (`json:"-"`) — the
	// Tax Optimizer constructs this in-memory on each run.
	PerYearOverrides map[int]float64 `json:"-"`
}

// BigTicketType represents whether an item is income or expense
type BigTicketType string

const (
	BigTicketIncome  BigTicketType = "income"
	BigTicketExpense BigTicketType = "expense"
)

// TaxTreatment represents how a big ticket item is taxed
type TaxTreatment string

const (
	TaxNone     TaxTreatment = "none"      // Not taxable (gifts, certain home sale gains)
	TaxOrdinary TaxTreatment = "ordinary"  // Ordinary income tax
	TaxCapGains TaxTreatment = "cap_gains" // Capital gains rate
)

// BigTicketItem represents a one-time financial event
type BigTicketItem struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Amount       float64       `json:"amount"`        // Always positive; Type determines direction
	Year         int           `json:"year"`          // Years from now (0 = this year)
	Type         BigTicketType `json:"type"`          // income or expense
	TaxTreatment TaxTreatment  `json:"tax_treatment"` // How it's taxed
	Notes        string        `json:"notes"`         // Optional description
}

// GetNetAmount returns the signed amount (positive for income, negative for expense)
func (b *BigTicketItem) GetNetAmount() float64 {
	if b.Type == BigTicketExpense {
		return -b.Amount
	}
	return b.Amount
}

// YearlyTaxSummary provides annual tax breakdown
type YearlyTaxSummary struct {
	Year            int     `json:"year"`
	Age             int     `json:"age"`
	TaxableIncome   float64 `json:"taxable_income"`
	FederalTax      float64 `json:"federal_tax"`
	StateTax        float64 `json:"state_tax"`
	TotalTax        float64 `json:"total_tax"`
	EffectiveRate   float64 `json:"effective_rate"`
	MarginalBracket float64 `json:"marginal_bracket"`
	RothConversion  float64 `json:"roth_conversion"`
	RMDAmount       float64 `json:"rmd_amount"`
}

// TaxAnalysis contains tax projections summary
type TaxAnalysis struct {
	TotalFederalTaxPaid  float64            `json:"total_federal_tax_paid"`
	TotalStateTaxPaid    float64            `json:"total_state_tax_paid"`
	TotalTaxPaid         float64            `json:"total_tax_paid"`
	AverageEffectiveRate float64            `json:"average_effective_rate"`
	ConversionTaxPaid    float64            `json:"conversion_tax_paid"` // Tax paid on Roth conversions
	YearlyTaxSummary     []YearlyTaxSummary `json:"yearly_tax_summary"`
}

// HistoricalYear represents one year of market data
type HistoricalYear struct {
	Year          int     `json:"year"`
	SP500Return   float64 `json:"sp500_return"`   // S&P 500 total return %
	BondReturn    float64 `json:"bond_return"`    // 10-year Treasury return %
	CashReturn    float64 `json:"cash_return"`    // 3-month T-bill return %
	InflationRate float64 `json:"inflation_rate"` // CPI inflation %
}

// HistoricalBacktestResult represents testing retirement from one starting year
type HistoricalBacktestResult struct {
	StartYear           int     `json:"start_year"`
	EndYear             int     `json:"end_year"`
	Survives            bool    `json:"survives"`
	FinalBalance        float64 `json:"final_balance"`        // Nominal final balance
	FinalBalanceReal    float64 `json:"final_balance_real"`   // Inflation-adjusted (start-year dollars)
	CumulativeInflation float64 `json:"cumulative_inflation"` // Total inflation factor over period
	DepletionYear       int     `json:"depletion_year"`       // Year of depletion (0 if survives)
	WorstDrawdown       float64 `json:"worst_drawdown"`       // Worst portfolio decline %
	SequenceQuality     string  `json:"sequence_quality"`     // "favorable", "neutral", "adverse"
}

// HistoricalBacktestAnalysis contains complete backtesting results
type HistoricalBacktestAnalysis struct {
	DataStartYear         int                        `json:"data_start_year"`
	DataEndYear           int                        `json:"data_end_year"`
	TotalSequences        int                        `json:"total_sequences"`
	SurvivedCount         int                        `json:"survived_count"` // Count of sequences that survived
	SuccessRate           float64                    `json:"success_rate"`   // % of sequences that survive
	Results               []HistoricalBacktestResult `json:"results"`
	WorstStartYears       []int                      `json:"worst_start_years"` // Top 5 worst starting years
	BestStartYears        []int                      `json:"best_start_years"`  // Top 5 best starting years
	MonteCarloSuccessRate float64                    `json:"monte_carlo_success_rate"`
	HistoricalVsMC        float64                    `json:"historical_vs_mc"` // Difference in success rates
}

// RothStrategyKind names a Roth conversion strategy family.
type RothStrategyKind string

const (
	RothStrategyNone        RothStrategyKind = "none"
	RothStrategyLadder      RothStrategyKind = "ladder"
	RothStrategyBracketFill RothStrategyKind = "bracket_fill"
)

// RothOptimizerStrategy describes a Roth conversion strategy in a form
// the Tax Optimizer can apply to the engine without mutating saved
// settings.
type RothOptimizerStrategy struct {
	Kind          RothStrategyKind `json:"kind"`
	AnnualAmount  float64          `json:"annual_amount,omitempty"`  // ladder only
	TargetBracket float64          `json:"target_bracket,omitempty"` // bracket_fill only; e.g. 0.22
	StartAge      int              `json:"start_age"`
	EndAge        int              `json:"end_age"`
	Label         string           `json:"label"` // human-readable, e.g. "$100k/yr to RMD age"
}

// YearlyConversion is one year's planned Roth conversion as part of an
// optimizer strategy. Age is the primary's age in that year; Amount is
// the dollar amount converted in that year, in nominal dollars.
type YearlyConversion struct {
	Age    int     `json:"age"`
	Amount float64 `json:"amount"`
}

// TaxOptimizerCandidate is one (SS pair, Roth strategy) configuration
// and its scored outcome.
type TaxOptimizerCandidate struct {
	PrimaryClaimAge int `json:"primary_claim_age"`
	// SpouseClaimAge is 0 (and omitted from JSON) for single-filer
	// scenarios. Template/handler authors should guard on the active
	// scenario's HasSpouse() rather than on this field's presence.
	SpouseClaimAge int                   `json:"spouse_claim_age,omitempty"`
	RothStrategy   RothOptimizerStrategy `json:"roth_strategy"`

	// Deterministic projection scores.
	EndingPortfolioReal float64 `json:"ending_portfolio_real"`
	LifetimeTaxReal     float64 `json:"lifetime_tax_real"`
	PeakMarginalBracket float64 `json:"peak_marginal_bracket"`
	TotalRothConverted  float64 `json:"total_roth_converted"`

	// Monte Carlo refinement; zero-valued for non-top-5 entries.
	MCSurvivalRate     float64 `json:"mc_survival_rate,omitempty"` // 0–100 percent (matches MonteCarloAnalysis.Stats.SuccessRate)
	MCMedianEndingReal float64 `json:"mc_median_ending_real,omitempty"`

	// PerYearConversions is the year-by-year conversion plan implied by
	// RothStrategy. Empty when the strategy is the no-conversion baseline.
	// Ladder strategies produce uniform Amount across the window;
	// bracket-fill strategies size each year to (bracket ceiling − other
	// estimated taxable income for that year).
	PerYearConversions []YearlyConversion `json:"per_year_conversions,omitempty"`
}

// TaxOptimizerAnalysis is the per-scenario recommendation produced by
// analysis.TaxOptimizer. Always non-nil when produced via RunFull;
// Eligible=false carries IneligibleReason for UI rendering.
type TaxOptimizerAnalysis struct {
	Eligible         bool   `json:"eligible"`
	IneligibleReason string `json:"ineligible_reason,omitempty"`

	Baseline TaxOptimizerCandidate   `json:"baseline"` // user's saved scenario, scored for delta comparisons
	Best     TaxOptimizerCandidate   `json:"best"`     // top-ranked finalist after MC tiebreak
	Top      []TaxOptimizerCandidate `json:"top"`      // up to taxOptimizerTopFinalists entries; Best at index 0

	MonteCarloRuns   int `json:"monte_carlo_runs"`  // MC budget per top-5 candidate; 0 before refinement
	CandidatesScored int `json:"candidates_scored"` // total deterministic projections run
}

// WhatIfAnalysis is the complete analysis container returned to templates
type WhatIfAnalysis struct {
	Settings                 *WhatIfSettings             `json:"settings"`
	Projection               *ProjectionResult           `json:"projection"`
	ProjectionExplainability *ProjectionExplainability   `json:"projection_explainability,omitempty"`
	BudgetFit                *BudgetFitAnalysis          `json:"budget_fit"`
	PresentValue             *PresentValueAnalysis       `json:"present_value"`
	Sustainability           *SustainabilityScore        `json:"sustainability"`
	Sensitivity              []SensitivityResult         `json:"sensitivity"`
	FailurePoints            *FailurePointAnalysis       `json:"failure_points"`
	MonteCarlo               *MonteCarloAnalysis         `json:"monte_carlo"`
	RMD                      *RMDAnalysis                `json:"rmd"`
	Tax                      *TaxAnalysis                `json:"tax"`
	HistoricalBacktest       *HistoricalBacktestAnalysis `json:"historical_backtest"`
	SocialSecurity           *SSComparisonAnalysis       `json:"social_security,omitempty"`
	// TaxOptimizer holds the Tax Optimizer recommendation. May be nil
	// when no analysis has been run; carries Eligible=false with a
	// reason when the scenario doesn't qualify.
	TaxOptimizer *TaxOptimizerAnalysis `json:"tax_optimizer,omitempty"`
}

// SSClaimingOption represents the benefit analysis for a specific claiming age
type SSClaimingOption struct {
	ClaimAge       int     `json:"claim_age"`
	MonthlyBenefit float64 `json:"monthly_benefit"`
	AnnualBenefit  float64 `json:"annual_benefit"`
	PctOfPIA       float64 `json:"pct_of_pia"`
	CumulativeAt80 float64 `json:"cumulative_at_80"`
	CumulativeAt85 float64 `json:"cumulative_at_85"`
	CumulativeAt90 float64 `json:"cumulative_at_90"`
}

// SSBreakevenResult represents the age at which delaying benefits surpasses claiming earlier
type SSBreakevenResult struct {
	EarlyAge     int `json:"early_age"`
	LateAge      int `json:"late_age"`
	BreakevenAge int `json:"breakeven_age"`
}

// SSComparisonAnalysis contains the full claiming age analysis
type SSComparisonAnalysis struct {
	Options                   []SSClaimingOption   `json:"options"`
	Breakevens                []SSBreakevenResult  `json:"breakevens"`
	BestAge                   int                  `json:"best_age"`
	SpouseOptions             []SSClaimingOption   `json:"spouse_options,omitempty"`
	SpouseBreakevens          []SSBreakevenResult  `json:"spouse_breakevens,omitempty"`
	SpouseBestAge             int                  `json:"spouse_best_age,omitempty"`
	SpouseUsingSpousalBenefit bool                 `json:"spouse_using_spousal_benefit,omitempty"`
	SpouseEarlyClaimGapPct    float64              `json:"spouse_early_claim_gap_pct,omitempty"` // % difference between earliest and best cumulative at 85
	Portfolio                 *SSPortfolioAnalysis `json:"portfolio,omitempty"`
}

type SSPortfolioOption struct {
	ClaimAge            int     `json:"claim_age"`
	MonthlyBenefit      float64 `json:"monthly_benefit"`
	SurvivalRate        float64 `json:"survival_rate"`
	MedianEndingBalance float64 `json:"median_ending_balance"`
	P10EndingBalance    float64 `json:"p10_ending_balance"`
	P90EndingBalance    float64 `json:"p90_ending_balance"`
	DeltaSurvivalRate   float64 `json:"delta_survival_rate"`
}

type SSPortfolioAnalysis struct {
	PrimaryOptions       []SSPortfolioOption `json:"primary_options"`
	SpouseOptions        []SSPortfolioOption `json:"spouse_options"`
	OptimalPrimaryAge    int                 `json:"optimal_primary_age"`
	OptimalSpouseAge     int                 `json:"optimal_spouse_age"`
	OptimalSurvivalRate  float64             `json:"optimal_survival_rate"`
	BaselineSurvivalRate float64             `json:"baseline_survival_rate"`
	MonteCarloRuns       int                 `json:"monte_carlo_runs"`
}

// WhatIfPageData is the data passed to the whatif template
type WhatIfPageData struct {
	Title     string          `json:"title"`
	ActiveTab string          `json:"active_tab"`
	Settings  *WhatIfSettings `json:"settings"`
	Analysis  *WhatIfAnalysis `json:"analysis"`
	// Findings holds []completeness.Finding but is typed interface{} to avoid
	// an import cycle (models → completeness → models). When the Finding type
	// moves to models/ or a shared types package (Phase-2), retype this field.
	Findings interface{} `json:"findings,omitempty"`
}
