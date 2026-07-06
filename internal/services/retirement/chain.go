package retirement

import (
	"sort"
	"strings"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// prepareChainedSettings builds the settings that take effect at a chain
// transition: the linked scenario's plan fields with the primary scenario's
// people and timeline identity, and its income/expense/big-ticket/Roth
// schedules rebased to the transition year. The result goes through
// prepare.From — the same deep-copy + normalize + validate pipeline as
// every other engine input — so chained settings cannot share pointer
// state with the stored snapshot or skip validation. (CurrentAge/SpouseAge
// are not copied from the primary: From re-derives them from the copied
// Persons and StartDate.)
func prepareChainedSettings(linked *models.WhatIfSettings, primary *models.WhatIfSettings, transitionYear int) (prepare.PreparedSettings, error) {
	prepared := *linked

	prepared.StartDate = primary.StartDate
	prepared.Persons = append([]models.Person(nil), primary.Persons...)
	reconcilePreparedPersons(&prepared, primary)
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
			if persons[i].PersonID != "" {
				if prepared.FindPerson(persons[i].PersonID) == nil {
					// Linked person ID doesn't exist in the primary scenario's
					// persons — try to map by role or name.
					if linkedPerson := linked.FindPerson(persons[i].PersonID); linkedPerson != nil {
						if mapped := findPreparedScenarioPerson(primary, linkedPerson); mapped != nil {
							persons[i].PersonID = mapped.ID
						} else {
							// No match found; clear the link so the entry
							// becomes manual rather than silently orphaned.
							persons[i].PersonID = ""
						}
					} else {
						persons[i].PersonID = ""
					}
				}
				continue
			}
			persons[i].CurrentAge = persons[i].CurrentAge - transitionYear
		}
		prepared.HealthcarePersons = persons
	}

	return prepare.From(&prepared)
}

func findPreparedScenarioPerson(primary *models.WhatIfSettings, linkedPerson *models.Person) *models.Person {
	switch linkedPerson.Role {
	case models.PersonRolePrimary:
		return primary.GetPrimaryPerson()
	case models.PersonRoleSpouse:
		return primary.GetSpousePerson()
	}

	normalized := strings.ToLower(strings.TrimSpace(linkedPerson.Name))
	var match *models.Person
	for i := range primary.Persons {
		if strings.ToLower(strings.TrimSpace(primary.Persons[i].Name)) != normalized {
			continue
		}
		if match != nil {
			return nil
		}
		match = &primary.Persons[i]
	}
	return match
}

// reconcilePreparedPersons removes the spouse person from the prepared
// settings when the primary scenario has no canonical spouse person.
// Birth months are NOT recalculated here — the deep-copied Persons
// from the primary scenario already carry the canonical birth months.
func reconcilePreparedPersons(settings *models.WhatIfSettings, primary *models.WhatIfSettings) {
	if primary.GetSpousePerson() != nil {
		return
	}
	filtered := make([]models.Person, 0, len(settings.Persons))
	for _, person := range settings.Persons {
		if person.Role == models.PersonRoleSpouse {
			continue
		}
		filtered = append(filtered, person)
	}
	settings.Persons = filtered
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

// Calculator.nextChainTransition was the chain-transition resolver
// used by the deprecated retirement-side backtest.go. With backtest's
// move to analysis (which routes through engine.Input.Hooks), no caller
// remains. Engine now owns the canonical chain-transition flow via the
// hook supplied by retirement.DefaultHooks().
