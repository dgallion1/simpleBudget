package retirement

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"budget2/internal/models"
)

// SettingsManager handles persistence of what-if settings
type SettingsManager struct {
	settingsDir string
	filename    string
	mu          sync.RWMutex
}

// NewSettingsManager creates a new settings manager
func NewSettingsManager(settingsDir string) *SettingsManager {
	return &SettingsManager{
		settingsDir: settingsDir,
		filename:    "whatif.json",
	}
}

// filepath returns the full path to the settings file
func (sm *SettingsManager) filepath() string {
	return filepath.Join(sm.settingsDir, sm.filename)
}

// Load reads settings from disk, returning defaults if file doesn't exist
func (sm *SettingsManager) Load() (*models.WhatIfSettings, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return sm.loadInternal()
}

// loadInternal reads settings without acquiring lock (caller must hold lock)
func (sm *SettingsManager) loadInternal() (*models.WhatIfSettings, error) {
	// Ensure settings directory exists
	if err := os.MkdirAll(sm.settingsDir, 0755); err != nil {
		return nil, err
	}

	path := sm.filepath()

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Return defaults (caller should save if needed)
		return models.DefaultWhatIfSettings(), nil
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return models.DefaultWhatIfSettings(), err
	}

	// Parse JSON
	var settings models.WhatIfSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return models.DefaultWhatIfSettings(), err
	}

	// Ensure slices are initialized
	if settings.IncomeSources == nil {
		settings.IncomeSources = []models.IncomeSource{}
	}
	if settings.ExpenseSources == nil {
		settings.ExpenseSources = []models.ExpenseSource{}
	}
	if settings.RemovedIncomeSources == nil {
		settings.RemovedIncomeSources = []models.IncomeSource{}
	}
	if settings.RemovedExpenseSources == nil {
		settings.RemovedExpenseSources = []models.ExpenseSource{}
	}
	if settings.HealthcarePersons == nil {
		settings.HealthcarePersons = []models.HealthcarePerson{}
	}

	// Migration: if no healthcare persons but legacy healthcare value exists,
	// create a single person from legacy values
	if len(settings.HealthcarePersons) == 0 && settings.MonthlyHealthcare > 0 {
		coverage := models.CoverageMedicare
		if settings.CurrentAge < 65 {
			coverage = models.CoverageACA
		}
		settings.HealthcarePersons = []models.HealthcarePerson{
			{
				ID:                    "migrated-user",
				Name:                  "User",
				CurrentAge:            settings.CurrentAge,
				CurrentCoverage:       coverage,
				CurrentMonthlyCost:    settings.MonthlyHealthcare,
				PreMedicareInflation:  settings.HealthcareInflation,
				MedicareMonthlyCost:   settings.MonthlyHealthcare,
				PostMedicareInflation: settings.HealthcareInflation,
				MedicareEligibleAge:   65,
			},
		}
	}

	return &settings, nil
}

// Save writes settings to disk
func (sm *SettingsManager) Save(settings *models.WhatIfSettings) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	return sm.saveInternal(settings)
}

// saveInternal writes settings without acquiring lock (caller must hold lock)
func (sm *SettingsManager) saveInternal(settings *models.WhatIfSettings) error {
	// Ensure settings directory exists
	if err := os.MkdirAll(sm.settingsDir, 0755); err != nil {
		return err
	}

	// Marshal to JSON with indentation for readability
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	// Write file
	return os.WriteFile(sm.filepath(), data, 0644)
}

// AddIncomeSource adds a new income source and saves atomically
func (sm *SettingsManager) AddIncomeSource(source models.IncomeSource) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	settings.IncomeSources = append(settings.IncomeSources, source)

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// RemoveIncomeSource moves an income source to the removed list by ID and saves atomically
func (sm *SettingsManager) RemoveIncomeSource(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.IncomeSource, 0, len(settings.IncomeSources))
	for _, source := range settings.IncomeSources {
		if source.ID != id {
			filtered = append(filtered, source)
		} else {
			// Move to removed list
			settings.RemovedIncomeSources = append(settings.RemovedIncomeSources, source)
		}
	}
	settings.IncomeSources = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// RestoreIncomeSource moves an income source back from the removed list atomically
func (sm *SettingsManager) RestoreIncomeSource(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.IncomeSource, 0, len(settings.RemovedIncomeSources))
	for _, source := range settings.RemovedIncomeSources {
		if source.ID != id {
			filtered = append(filtered, source)
		} else {
			// Restore to active list
			settings.IncomeSources = append(settings.IncomeSources, source)
		}
	}
	settings.RemovedIncomeSources = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// UpdateIncomeSource updates an existing income source by ID atomically
func (sm *SettingsManager) UpdateIncomeSource(id string, startYear, endYear int, colaRate float64) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	for i := range settings.IncomeSources {
		if settings.IncomeSources[i].ID == id {
			settings.IncomeSources[i].StartMonth = startYear * 12
			settings.IncomeSources[i].COLARate = colaRate
			if endYear > 0 {
				endMonth := endYear * 12
				settings.IncomeSources[i].EndMonth = &endMonth
			} else {
				settings.IncomeSources[i].EndMonth = nil
			}
			break
		}
	}

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// AddExpenseSource adds a new expense source and saves atomically
func (sm *SettingsManager) AddExpenseSource(source models.ExpenseSource) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	settings.ExpenseSources = append(settings.ExpenseSources, source)

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// UpdateExpenseSource updates an existing expense source by ID atomically
func (sm *SettingsManager) UpdateExpenseSource(id string, startYear, endYear int, inflation, discretionary bool) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	for i := range settings.ExpenseSources {
		if settings.ExpenseSources[i].ID == id {
			settings.ExpenseSources[i].StartYear = startYear
			settings.ExpenseSources[i].EndYear = endYear
			settings.ExpenseSources[i].Inflation = inflation
			settings.ExpenseSources[i].Discretionary = discretionary
			break
		}
	}

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// RemoveExpenseSource moves an expense source to the removed list by ID and saves atomically
func (sm *SettingsManager) RemoveExpenseSource(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.ExpenseSource, 0, len(settings.ExpenseSources))
	for _, source := range settings.ExpenseSources {
		if source.ID != id {
			filtered = append(filtered, source)
		} else {
			// Move to removed list
			settings.RemovedExpenseSources = append(settings.RemovedExpenseSources, source)
		}
	}
	settings.ExpenseSources = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// RestoreExpenseSource moves an expense source back from the removed list atomically
func (sm *SettingsManager) RestoreExpenseSource(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.ExpenseSource, 0, len(settings.RemovedExpenseSources))
	for _, source := range settings.RemovedExpenseSources {
		if source.ID != id {
			filtered = append(filtered, source)
		} else {
			// Restore to active list
			settings.ExpenseSources = append(settings.ExpenseSources, source)
		}
	}
	settings.RemovedExpenseSources = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// UpdateSettings updates all settings fields from form data and saves atomically
func (sm *SettingsManager) UpdateSettings(updates map[string]interface{}) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	// Apply updates
	if v, ok := updates["portfolio_value"].(float64); ok {
		settings.PortfolioValue = v
	}
	if v, ok := updates["monthly_living_expenses"].(float64); ok {
		settings.MonthlyLivingExpenses = v
	}
	if v, ok := updates["monthly_healthcare"].(float64); ok {
		settings.MonthlyHealthcare = v
	}
	if v, ok := updates["healthcare_start_years"].(int); ok {
		settings.HealthcareStartYears = v
	}
	if v, ok := updates["current_age"].(int); ok {
		settings.CurrentAge = v
	}
	if v, ok := updates["tax_deferred_percent"].(float64); ok {
		settings.TaxDeferredPercent = v
	}
	if v, ok := updates["inflation_rate"].(float64); ok {
		settings.InflationRate = v
	}
	if v, ok := updates["healthcare_inflation"].(float64); ok {
		settings.HealthcareInflation = v
	}
	if v, ok := updates["spending_decline_rate"].(float64); ok {
		settings.SpendingDeclineRate = v
	}
	if v, ok := updates["investment_return"].(float64); ok {
		settings.InvestmentReturn = v
	}
	if v, ok := updates["discount_rate"].(float64); ok {
		settings.DiscountRate = v
	}
	if v, ok := updates["projection_years"].(int); ok {
		settings.ProjectionYears = v
	}
	if v, ok := updates["steady_state_override_year"].(float64); ok {
		settings.SteadyStateOverrideYear = v
	}

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// AddHealthcarePerson adds a new healthcare person and saves atomically
func (sm *SettingsManager) AddHealthcarePerson(person models.HealthcarePerson) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	settings.HealthcarePersons = append(settings.HealthcarePersons, person)

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// UpdateHealthcarePerson updates an existing healthcare person by ID atomically
func (sm *SettingsManager) UpdateHealthcarePerson(id string, updates map[string]interface{}) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	for i := range settings.HealthcarePersons {
		if settings.HealthcarePersons[i].ID == id {
			if v, ok := updates["name"].(string); ok {
				settings.HealthcarePersons[i].Name = v
			}
			if v, ok := updates["current_age"].(int); ok {
				settings.HealthcarePersons[i].CurrentAge = v
			}
			if v, ok := updates["current_coverage"].(string); ok {
				settings.HealthcarePersons[i].CurrentCoverage = models.CoverageType(v)
			}
			if v, ok := updates["current_monthly_cost"].(float64); ok {
				settings.HealthcarePersons[i].CurrentMonthlyCost = v
			}
			if v, ok := updates["pre_medicare_inflation"].(float64); ok {
				settings.HealthcarePersons[i].PreMedicareInflation = v
			}
			if v, ok := updates["medicare_monthly_cost"].(float64); ok {
				settings.HealthcarePersons[i].MedicareMonthlyCost = v
			}
			if v, ok := updates["post_medicare_inflation"].(float64); ok {
				settings.HealthcarePersons[i].PostMedicareInflation = v
			}
			if v, ok := updates["employer_coverage_years"].(int); ok {
				settings.HealthcarePersons[i].EmployerCoverageYears = v
			}
			if v, ok := updates["aca_cost_after_employer"].(float64); ok {
				settings.HealthcarePersons[i].ACACostAfterEmployer = v
			}
			break
		}
	}

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// RemoveHealthcarePerson removes a healthcare person by ID atomically
func (sm *SettingsManager) RemoveHealthcarePerson(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.HealthcarePerson, 0, len(settings.HealthcarePersons))
	for _, person := range settings.HealthcarePersons {
		if person.ID != id {
			filtered = append(filtered, person)
		}
	}
	settings.HealthcarePersons = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}
