package whatif

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

var errLifetimeGuardRequired = errors.New("expected scenario and revision are required")

type lifetimeMutationRequest struct {
	ExpectedScenario string                   `json:"expected_scenario"`
	ExpectedRevision *int                     `json:"expected_revision"`
	Lifetime         *models.LifetimeSettings `json:"lifetime"`
	RetirementMonths map[string]string        `json:"retirement_months"`
}

type lifetimeRemoveRequest struct {
	ExpectedScenario string `json:"expected_scenario"`
	ExpectedRevision *int   `json:"expected_revision"`
	Confirmed        bool   `json:"confirmed"`
}

type lifetimePreviewRow struct {
	RuleID              string `json:"rule_id"`
	Employee            string `json:"employee"`
	Employer            string `json:"employer"`
	EmployeeTraditional string `json:"employee_traditional"`
	EmployeeRoth        string `json:"employee_roth"`
	EmployerTraditional string `json:"employer_traditional"`
	EmployerRoth        string `json:"employer_roth"`
	Requested           string `json:"requested"`
	Permitted           string `json:"permitted"`
}

type lifetimeAnnualPreview struct {
	CalendarYear    int                                   `json:"calendar_year"`
	Period          string                                `json:"period"`
	AmountsByRule   map[string]models.ContributionAmounts `json:"amounts_by_rule"`
	PolicyYear      int                                   `json:"policy_year"`
	FrozenLimits    bool                                  `json:"frozen_limits"`
	LimitSourceYear int                                   `json:"limit_source_year,omitempty"`
	Rows            []lifetimePreviewRow                  `json:"rows"`
}

type lifetimeResponse struct {
	OK       bool                   `json:"ok"`
	Message  string                 `json:"message,omitempty"`
	Field    string                 `json:"field,omitempty"`
	Revision int                    `json:"revision,omitempty"`
	Preview  *lifetimeAnnualPreview `json:"preview,omitempty"`
}

func decodeLifetimeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeLifetimeJSON(w, http.StatusBadRequest, lifetimeResponse{Message: "Invalid lifetime plan: " + err.Error()})
		return false
	}
	return true
}

func writeLifetimeJSON(w http.ResponseWriter, status int, value lifetimeResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func applyRetirementMonths(s *models.WhatIfSettings, months map[string]string) error {
	if len(months) == 0 {
		return nil
	}
	known := make(map[string]bool, len(s.Persons))
	for i := range s.Persons {
		known[s.Persons[i].ID] = true
		if value, ok := months[s.Persons[i].ID]; ok {
			s.Persons[i].RetirementMonth = strings.TrimSpace(value)
		}
	}
	for id := range months {
		if !known[id] {
			return fmt.Errorf("retirement month owner %q not found", id)
		}
	}
	return nil
}

func proposedLifetimeSettings(base *models.WhatIfSettings, req lifetimeMutationRequest) (*models.WhatIfSettings, error) {
	if req.Lifetime == nil {
		return nil, errors.New("lifetime plan is required")
	}
	next, err := cloneWhatIfSettings(base)
	if err != nil {
		return nil, err
	}
	next.Lifetime = req.Lifetime
	if err := applyRetirementMonths(next, req.RetirementMonths); err != nil {
		return nil, err
	}
	if _, err := prepare.From(next); err != nil {
		return nil, err
	}
	return next, nil
}

func cloneWhatIfSettings(s *models.WhatIfSettings) (*models.WhatIfSettings, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var out models.WhatIfSettings
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func checkLifetimeGuard(r *http.Request, expected string, revision *int) (*models.WhatIfSettings, int, error) {
	if strings.TrimSpace(expected) == "" || revision == nil || *revision < 0 {
		return nil, 0, errLifetimeGuardRequired
	}
	s, current, err := retirementMgr.LoadContextWithRevision(r.Context())
	if err != nil {
		return nil, 0, err
	}
	if retirementMgr.ActiveFilename() != expected || current != *revision {
		return nil, current, &retirement.ScenarioConflictError{}
	}
	return s, current, nil
}

func annualContributionPreview(s *models.WhatIfSettings) (*lifetimeAnnualPreview, error) {
	if s.Lifetime == nil {
		return nil, errors.New("lifetime plan is required")
	}
	startMonth := 1
	year := 0
	if len(s.StartDate) >= 7 {
		year, _ = strconv.Atoi(s.StartDate[:4])
		startMonth, _ = strconv.Atoi(s.StartDate[5:7])
	}
	if year == 0 {
		return nil, errors.New("projection start month is required")
	}
	state := engine.ContributionYearState{}
	totals := make(map[string]models.ContributionAmounts)
	for month := 0; month <= 12-startMonth; month++ {
		result, err := engine.CalculateContributions(s, month, state)
		if err != nil {
			return nil, err
		}
		state = result.Next
		for id, a := range result.AmountsByRule {
			v := totals[id]
			v.Requested += a.Requested
			v.Permitted += a.Permitted
			v.Funded += a.Funded
			v.Unfunded += a.Unfunded
			v.EmployeeTraditional += a.EmployeeTraditional
			v.EmployeeRoth += a.EmployeeRoth
			v.EmployerTraditional += a.EmployerTraditional
			v.EmployerRoth += a.EmployerRoth
			totals[id] = v
		}
	}
	policy, err := engine.ContributionPolicyForYear(year)
	if err != nil {
		return nil, err
	}
	ids := StableLifetimeRuleIDs(totals)
	rows := make([]lifetimePreviewRow, 0, len(ids))
	for _, id := range ids {
		a := totals[id]
		rows = append(rows, lifetimePreviewRow{RuleID: id, Employee: fmt.Sprintf("$%.2f", a.EmployeeTraditional+a.EmployeeRoth), Employer: fmt.Sprintf("$%.2f", a.EmployerTraditional+a.EmployerRoth), EmployeeTraditional: fmt.Sprintf("$%.2f", a.EmployeeTraditional), EmployeeRoth: fmt.Sprintf("$%.2f", a.EmployeeRoth), EmployerTraditional: fmt.Sprintf("$%.2f", a.EmployerTraditional), EmployerRoth: fmt.Sprintf("$%.2f", a.EmployerRoth), Requested: fmt.Sprintf("$%.2f", a.Requested), Permitted: fmt.Sprintf("$%.2f", a.Permitted)})
	}
	return &lifetimeAnnualPreview{CalendarYear: year, Period: fmt.Sprintf("%04d-%02d through %04d-12", year, startMonth, year), AmountsByRule: totals, PolicyYear: policy.PolicyYear, FrozenLimits: policy.FrozenFromYear != 0, LimitSourceYear: policy.FrozenFromYear, Rows: rows}, nil
}

func handleWhatIfLifetimePreview(w http.ResponseWriter, r *http.Request) {
	var req lifetimeMutationRequest
	if !decodeLifetimeJSON(w, r, &req) {
		return
	}
	base, _, err := checkLifetimeGuard(r, req.ExpectedScenario, req.ExpectedRevision)
	if err != nil {
		writeLifetimeHandlerError(w, err)
		return
	}
	next, err := proposedLifetimeSettings(base, req)
	if err != nil {
		writeLifetimeJSON(w, http.StatusBadRequest, lifetimeResponse{Message: err.Error(), Field: lifetimeValidationField(err)})
		return
	}
	preview, err := annualContributionPreview(next)
	if err != nil {
		writeLifetimeJSON(w, http.StatusBadRequest, lifetimeResponse{Message: err.Error(), Field: lifetimeValidationField(err)})
		return
	}
	writeLifetimeJSON(w, http.StatusOK, lifetimeResponse{OK: true, Preview: preview})
}

func handleWhatIfLifetimeSave(w http.ResponseWriter, r *http.Request) {
	var req lifetimeMutationRequest
	if !decodeLifetimeJSON(w, r, &req) {
		return
	}
	base, _, err := checkLifetimeGuard(r, req.ExpectedScenario, req.ExpectedRevision)
	if err != nil {
		writeLifetimeHandlerError(w, err)
		return
	}
	next, err := proposedLifetimeSettings(base, req)
	if err != nil {
		writeLifetimeJSON(w, http.StatusBadRequest, lifetimeResponse{Message: err.Error(), Field: lifetimeValidationField(err)})
		return
	}
	revision, err := retirementMgr.SaveWithRevisionIfScenario(next, req.ExpectedScenario, *req.ExpectedRevision)
	if err != nil {
		writeLifetimeHandlerError(w, err)
		return
	}
	writeLifetimeJSON(w, http.StatusOK, lifetimeResponse{OK: true, Message: "Lifetime plan saved", Revision: revision})
}

func handleWhatIfLifetimeRemove(w http.ResponseWriter, r *http.Request) {
	var req lifetimeRemoveRequest
	if !decodeLifetimeJSON(w, r, &req) {
		return
	}
	if !req.Confirmed {
		writeLifetimeJSON(w, http.StatusBadRequest, lifetimeResponse{Message: "Confirm removal of account, job, and contribution settings"})
		return
	}
	base, _, err := checkLifetimeGuard(r, req.ExpectedScenario, req.ExpectedRevision)
	if err != nil {
		writeLifetimeHandlerError(w, err)
		return
	}
	next, err := cloneWhatIfSettings(base)
	if err != nil {
		writeLifetimeJSON(w, http.StatusInternalServerError, lifetimeResponse{Message: err.Error(), Field: lifetimeValidationField(err)})
		return
	}
	next.Lifetime = nil
	revision, err := retirementMgr.SaveWithRevisionIfScenario(next, req.ExpectedScenario, *req.ExpectedRevision)
	if err != nil {
		writeLifetimeHandlerError(w, err)
		return
	}
	writeLifetimeJSON(w, http.StatusOK, lifetimeResponse{OK: true, Message: "Lifetime settings removed", Revision: revision})
}

func lifetimeValidationField(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "ytd") || strings.Contains(message, "history"):
		return "lifetime-ytd-zero"
	case strings.Contains(message, "contribution") || strings.Contains(message, "tier") || strings.Contains(message, "rule"):
		return "lifetime-rules"
	case strings.Contains(message, "job") || strings.Contains(message, "employer"):
		return "lifetime-jobs"
	case strings.Contains(message, "cash") || strings.Contains(message, "reserve") || strings.Contains(message, "withdrawal"):
		return "lifetime-reserve-account"
	case strings.Contains(message, "account") || strings.Contains(message, "destination") || strings.Contains(message, "basis") || strings.Contains(message, "allocation"):
		return "lifetime-accounts"
	case strings.Contains(message, "retirement") || strings.Contains(message, "person") || strings.Contains(message, "owner"):
		return "lifetime-dialog"
	default:
		return ""
	}
}

func writeLifetimeHandlerError(w http.ResponseWriter, err error) {
	if errors.Is(err, errLifetimeGuardRequired) {
		writeLifetimeJSON(w, http.StatusBadRequest, lifetimeResponse{Message: err.Error(), Field: lifetimeValidationField(err)})
		return
	}
	var conflict *retirement.ScenarioConflictError
	if errors.As(err, &conflict) {
		writeLifetimeJSON(w, http.StatusConflict, lifetimeResponse{Message: "This scenario changed. Reopen the lifetime plan and try again."})
		return
	}
	writeLifetimeJSON(w, http.StatusInternalServerError, lifetimeResponse{Message: err.Error(), Field: lifetimeValidationField(err)})
}

// StableLifetimeRuleIDs is used by templates and tests to keep previews predictable.
func StableLifetimeRuleIDs(values map[string]models.ContributionAmounts) []string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
