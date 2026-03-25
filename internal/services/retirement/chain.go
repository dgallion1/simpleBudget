package retirement

import (
	"budget2/internal/models"
	"sort"
)

func prepareChainedSettings(linked *models.WhatIfSettings, primary *models.WhatIfSettings, transitionYear int) *models.WhatIfSettings {
	prepared := *linked

	prepared.CurrentAge = primary.CurrentAge
	prepared.SpouseAge = primary.SpouseAge
	prepared.PhaseAgeReference = primary.PhaseAgeReference
	prepared.ProjectionYears = primary.ProjectionYears
	prepared.TaxDeferredDelayYears = primary.TaxDeferredDelayYears

	transitionMonth := transitionYear * 12

	prepared.IncomeSources = rebaseIncomeSources(linked.IncomeSources, transitionMonth)
	prepared.ExpenseSources = rebaseExpenseSources(linked.ExpenseSources, transitionYear)
	prepared.BigTicketItems = rebaseBigTicketItems(linked.BigTicketItems, transitionYear)
	prepared.RothConversion = rebaseRothConversion(linked.RothConversion, transitionYear)

	if len(linked.HealthcarePersons) > 0 {
		persons := make([]models.HealthcarePerson, len(linked.HealthcarePersons))
		copy(persons, linked.HealthcarePersons)
		for i := range persons {
			persons[i].CurrentAge = persons[i].CurrentAge - transitionYear
		}
		prepared.HealthcarePersons = persons
	}

	return &prepared
}

func rebaseIncomeSources(sources []models.IncomeSource, transitionMonth int) []models.IncomeSource {
	result := make([]models.IncomeSource, 0, len(sources))
	for _, s := range sources {
		s := s
		if s.EndMonth != nil && *s.EndMonth <= transitionMonth {
			continue
		}
		s.StartMonth = max(0, s.StartMonth-transitionMonth)
		if s.EndMonth != nil {
			rebased := *s.EndMonth - transitionMonth
			s.EndMonth = &rebased
		}
		result = append(result, s)
	}
	return result
}

func rebaseExpenseSources(sources []models.ExpenseSource, transitionYear int) []models.ExpenseSource {
	result := make([]models.ExpenseSource, 0, len(sources))
	for _, s := range sources {
		s := s
		if s.EndYear > 0 && s.EndYear <= transitionYear {
			continue
		}
		s.StartYear = max(0, s.StartYear-transitionYear)
		if s.EndYear > 0 {
			s.EndYear = s.EndYear - transitionYear
		}
		result = append(result, s)
	}
	return result
}

func rebaseBigTicketItems(items []models.BigTicketItem, transitionYear int) []models.BigTicketItem {
	result := make([]models.BigTicketItem, 0, len(items))
	for _, item := range items {
		item := item
		rebased := item.Year - transitionYear
		if rebased < 0 {
			continue
		}
		item.Year = rebased
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Year < result[j].Year
	})
	return result
}

func rebaseRothConversion(config *models.RothConversionConfig, transitionYear int) *models.RothConversionConfig {
	if config == nil || !config.Enabled {
		return config
	}
	result := *config
	if result.EndYear > 0 && result.EndYear <= transitionYear {
		result.Enabled = false
		return &result
	}
	result.StartYear = max(0, result.StartYear-transitionYear)
	if result.EndYear > 0 {
		result.EndYear = result.EndYear - transitionYear
	}
	return &result
}

func (c *Calculator) nextChainTransition(currentYear int, nextChainIndex int, primarySettings *models.WhatIfSettings) (int, *models.WhatIfSettings) {
	if nextChainIndex >= len(c.ResolvedChain) {
		return nextChainIndex, nil
	}
	link := c.ResolvedChain[nextChainIndex]
	currentAge := primarySettings.CurrentAge + currentYear
	if currentAge >= link.TransitionAge {
		transitionYear := link.TransitionAge - primarySettings.CurrentAge
		prepared := prepareChainedSettings(link.Settings, primarySettings, transitionYear)
		return nextChainIndex + 1, prepared
	}
	return nextChainIndex, nil
}
