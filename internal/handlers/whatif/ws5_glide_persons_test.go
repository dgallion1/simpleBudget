package whatif

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// ── D6: glide path ──────────────────────────────────────────────────────

// Mutation kill (a): re-adding onchange="this.form.requestSubmit()" on the
// glide checkbox. The client-side reveal-only behavior itself is covered by
// the node test (whatif-rate-assumptions.test.cjs); this pins the rendered
// markup so a template regression is caught too.
func TestGlidePathCheckboxNeverAutoSubmitsOnChange(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := renderer.RenderToString("whatif-rate-assumptions", map[string]any{"Settings": s})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	checkbox := regexp.MustCompile(`(?s)<input type="checkbox" name="enabled".*?>`).FindString(out)
	if checkbox == "" {
		t.Fatal("glide checkbox not found in rendered output")
	}
	if strings.Contains(checkbox, "requestSubmit()") {
		t.Errorf("glide checkbox must never auto-submit on a bare tick, got: %s", checkbox)
	}
	if !strings.Contains(checkbox, `onchange="toggleGlidePathFields(this)"`) {
		t.Errorf("glide checkbox must reveal its fields via toggleGlidePathFields(this), got: %s", checkbox)
	}
}

// ACCESSIBILITY.md point 6 / WCAG 3.3.2: required is stated in text as soon
// as the fields are revealed (not only reactively inside a failed-submit
// error), and the three inputs are `required` ONLY while revealed --
// `disabled` while hidden, so an untick still submits (and saves
// enabled=false) even though the fields are blank (a disabled control is
// excluded from both constraint validation and the submitted form data).
func TestGlidePathFieldsRequiredWhenRevealedDisabledWhenHidden(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	assertField := func(t *testing.T, out, name string, wantRequired, wantDisabled bool) {
		t.Helper()
		tag := regexp.MustCompile(`<input [^>]*name="` + name + `"[^>]*>`).FindString(out)
		if tag == "" {
			t.Fatalf("input %s not found in rendered output", name)
		}
		if hasRequired := regexp.MustCompile(`\brequired\b`).MatchString(tag); hasRequired != wantRequired {
			t.Errorf("%s: required=%v, want %v; tag: %s", name, hasRequired, wantRequired, tag)
		}
		if hasDisabled := regexp.MustCompile(`\bdisabled\b`).MatchString(tag); hasDisabled != wantDisabled {
			t.Errorf("%s: disabled=%v, want %v; tag: %s", name, hasDisabled, wantDisabled, tag)
		}
	}

	// Hidden state (no glide path configured).
	s := models.DefaultWhatIfSettings()
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := renderer.RenderToString("whatif-rate-assumptions", map[string]any{"Settings": s})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, "required to apply a glide path") {
		t.Error("expected a persistent, visible instruction that all three fields are required, stated in text even while hidden -- not only reactively inside a failed-submit error")
	}
	for _, name := range []string{"start_stock_pct", "end_stock_pct", "transition_years"} {
		assertField(t, out, name, false, true)
	}

	// Revealed state (an already-enabled glide path).
	s.GlidePath = &models.GlidePathConfig{Enabled: true, StartStockPct: 70, EndStockPct: 40, TransitionYears: 10}
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err = renderer.RenderToString("whatif-rate-assumptions", map[string]any{"Settings": s})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	for _, name := range []string{"start_stock_pct", "end_stock_pct", "transition_years"} {
		assertField(t, out, name, true, false)
	}
}

// Mutation kill (b): the handler accepting an enable with missing fields.
// Reproduces the exact bare-tick request the old onchange used to send
// (enabled=on alone) against the live plan's leftover, disabled glide
// config -- the reviewer's End Balance $3.05M -> $0.71M scenario.
func TestHandleWhatIfGlidePath_EnableRejectsMissingFields_LeftoverConfigUntouched(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings.GlidePath = &models.GlidePathConfig{Enabled: false, StartStockPct: 0, EndStockPct: 0, TransitionYears: 1}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	form := url.Values{"enabled": {"on"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/glide-path", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGlidePath(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.GlidePath == nil {
		t.Fatal("GlidePath config must survive a rejected enable")
	}
	if loaded.GlidePath.Enabled {
		t.Error("a rejected enable must never activate the glide path")
	}
	if loaded.GlidePath.StartStockPct != 0 || loaded.GlidePath.EndStockPct != 0 || loaded.GlidePath.TransitionYears != 1 {
		t.Errorf("a rejected enable must not touch the leftover config, got %+v", loaded.GlidePath)
	}
}

// Same guard, one field at a time -- each subtest supplies the OTHER two
// fields and omits exactly one, so each of the three `== ""` checks in
// handleWhatIfGlidePath is independently necessary: deleting any ONE of
// them alone (leaving the other two) is caught by the subtest that omits
// exactly the field whose check was deleted, since the survivors still have
// two present fields that would otherwise satisfy the remaining checks.
func TestHandleWhatIfGlidePath_EnableRejectsEachFieldMissingIndividually(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
	}{
		{"start missing", url.Values{"enabled": {"on"}, "end_stock_pct": {"40"}, "transition_years": {"10"}}},
		{"end missing", url.Values{"enabled": {"on"}, "start_stock_pct": {"70"}, "transition_years": {"10"}}},
		{"years missing", url.Values{"enabled": {"on"}, "start_stock_pct": {"70"}, "end_stock_pct": {"40"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rm, cleanup := setupTestEnv(t)
			defer cleanup()

			settings, err := rm.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			settings.GlidePath = &models.GlidePathConfig{Enabled: false, StartStockPct: 0, EndStockPct: 0, TransitionYears: 1}
			if err := rm.Save(settings); err != nil {
				t.Fatalf("seed Save: %v", err)
			}

			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/whatif/glide-path", formBody(tc.form))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			handleWhatIfGlidePath(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
			}

			loaded, err := rm.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if loaded.GlidePath == nil || loaded.GlidePath.Enabled ||
				loaded.GlidePath.StartStockPct != 0 || loaded.GlidePath.EndStockPct != 0 || loaded.GlidePath.TransitionYears != 1 {
				t.Errorf("a rejected enable (%s) must not touch the leftover config, got %+v", tc.name, loaded.GlidePath)
			}
		})
	}
}

// Apply with all three valid still saves -- the existing (pre-WS5) happy
// path, re-asserted here against the new guard so it is pinned alongside
// the rejection tests above.
func TestHandleWhatIfGlidePath_EnableWithAllFieldsSaves(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"enabled":          {"on"},
		"start_stock_pct":  {"75"},
		"end_stock_pct":    {"35"},
		"transition_years": {"12"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/glide-path", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGlidePath(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.GlidePath == nil || !settings.GlidePath.Enabled {
		t.Fatal("GlidePath should be enabled")
	}
	if settings.GlidePath.StartStockPct != 75 || settings.GlidePath.EndStockPct != 35 || settings.GlidePath.TransitionYears != 12 {
		t.Errorf("unexpected saved GlidePath: %+v", settings.GlidePath)
	}
}

// ── D7: person removal ──────────────────────────────────────────────────

// Mutation kill (e): the new-row birth-month `required` dropped. This pins
// the SERVER-rendered input (an existing person's row); the JS-generated
// new-row markup is covered by the node test.
func TestPersonBirthMonthInputIsRequired(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := renderer.RenderToString("whatif-rate-assumptions", map[string]any{"Settings": s})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	m := regexp.MustCompile(`(?s)<input type="month"[^>]*name="person_birth_month\[\]"[^>]*>`).FindString(out)
	if m == "" {
		t.Fatal("birth month input not found in rendered output")
	}
	if !regexp.MustCompile(`\brequired\b`).MatchString(m) {
		t.Errorf("birth month input must be required (a name typed with no birth month must never reach the server), got: %s", m)
	}
}

// personRemovalBlockedByHealthcare is the pure decision helper the handler
// consults before saving; exercised directly and via the full handler
// below.
func TestPersonRemovalBlockedByHealthcare(t *testing.T) {
	current := &models.WhatIfSettings{
		HealthcarePersons: []models.HealthcarePerson{
			{ID: "hc-1", Name: "Christine", PersonID: "p2"},
			{ID: "hc-2", Name: "Unlinked entry"},
		},
	}

	if msg := personRemovalBlockedByHealthcare(current, []models.Person{{ID: "p1"}}); msg != "Remove Christine's healthcare entry first" {
		t.Errorf("unexpected message removing the linked person: %q", msg)
	}
	if msg := personRemovalBlockedByHealthcare(current, []models.Person{{ID: "p1"}, {ID: "p2"}}); msg != "" {
		t.Errorf("expected no refusal when the linked person is kept, got %q", msg)
	}
}

// Mutation kill (d): a linked-person removal reaching a 500 instead of the
// named refusal.
func TestHandleWhatIfSettings_RemovePersonLinkedToHealthcare_Refused(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.MonthlyHealthcare = 0 // avoid the legacy migrated-user HealthcarePerson synthesis
	settings.Persons = []models.Person{
		{ID: "p1", Name: "You", BirthMonth: "1958-11", Role: models.PersonRolePrimary},
		{ID: "p2", Name: "Christine", BirthMonth: "1971-08", Role: models.PersonRoleSpouse},
	}
	prepare.ComputeAges(settings)
	if err := rm.Save(settings); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	hcPerson := models.HealthcarePerson{
		ID:                 "hc-1",
		Name:               "Christine",
		PersonID:           "p2",
		CurrentAge:         55,
		CurrentCoverage:    models.CoverageACA,
		CurrentMonthlyCost: 1100,
	}
	if _, err := rm.AddHealthcarePerson(hcPerson); err != nil {
		t.Fatalf("AddHealthcarePerson: %v", err)
	}

	// Submit the settings form with only the primary row -- exactly what
	// the page posts after removePersonRow deletes Christine's row.
	form := url.Values{
		"start_date":           {"2026-04"},
		"person_id[]":          {"p1"},
		"person_name[]":        {"You"},
		"person_birth_month[]": {"1958-11"},
		"person_role[]":        {"primary"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)

	if w.Code == http.StatusInternalServerError {
		t.Fatalf("a linked-person removal must never 500, got 500. body: %s", w.Body.String())
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Christine") || !strings.Contains(w.Body.String(), "healthcare entry") {
		t.Errorf("expected the refusal to name Christine's healthcare entry, got: %s", w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Persons) != 2 {
		t.Fatalf("a refused removal must save nothing -- expected both persons still saved, got %d: %+v", len(loaded.Persons), loaded.Persons)
	}
	if len(loaded.HealthcarePersons) != 1 {
		t.Fatalf("a refused removal must not touch the healthcare entry either, got %+v", loaded.HealthcarePersons)
	}
}

// "Never a 500": a person row posted DIRECTLY to /whatif/settings (bypassing
// the browser's own `required` on the birth-month field) with a name but no
// birth month must return 400 with a clear message naming the missing
// field -- not 500. Mutation kill: removing personsSaveErrorMessage's call
// site (or its "persons:"/"start_date:" prefix check) makes this 500 again.
func TestHandleWhatIfSettings_PersonNameNoBirthMonth_DirectPost_Never500(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"start_date":           {"2026-04"},
		"person_id[]":          {"p1"},
		"person_name[]":        {"You"},
		"person_birth_month[]": {""},
		"person_role[]":        {"primary"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)

	if w.Code == http.StatusInternalServerError {
		t.Fatalf("a person with a name but no birth month must never 500, got 500. body: %s", w.Body.String())
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "birth month") {
		t.Errorf("expected a message naming the missing birth month, got: %s", w.Body.String())
	}
}

// personsSaveErrorMessage is the pure classifier behind the 500 fix above --
// exercised directly against the TYPED errors prepare.ValidatePersons now
// returns (including the three DISTINCT "birth_month" shapes: missing,
// present-but-unparseable [checker-tests' WS5 attempt-3 FAIL], and
// present-but-after-start-date [checker-second's WS5 attempt-2 FAIL]), and
// against an unrelated error (which must fall through to the existing
// generic mapping instead of being reinterpreted as 400).
//
// This is attempt 5 (ruling 2026-09-24g/h/i): the FAILING row is identified
// ONLY by the typed error's PersonID -- never by re-matching Name, and
// never by regexing Error()'s (Go-escaped, %q) text -- because attempt 4's
// Name-based/regex approach broke on collisions and on names containing a
// quote, backslash or other character %q escapes.
func TestPersonsSaveErrorMessage(t *testing.T) {
	// Submitted birth month genuinely empty -> "needs a birth month",
	// worded from the SUBMITTED value found by PersonID, never from the
	// error's own wrapped text (deliberately unrelated here to prove
	// that).
	withEmpty := []models.Person{{ID: "p1", Name: "Alex", BirthMonth: "", Role: models.PersonRolePrimary}}
	emptyErr := &prepare.PersonBirthMonthError{PersonID: "p1", Name: "Alex", Err: fmt.Errorf("month is required")}
	if msg, ok := personsSaveErrorMessage(emptyErr, withEmpty); !ok || msg != "Alex needs a birth month" {
		t.Errorf("expected a missing-birth-month message naming Alex, got (%q, %v)", msg, ok)
	}

	// Submitted birth month present but unparseable (a five-digit year
	// typed into type=month, e.g. "19711-08") -> must name the person AND
	// the invalid value, and must NEVER say "needs a birth month" (that
	// would be false -- checker-tests' WS5 attempt-3 FAIL: both
	// ParseYearMonth error shapes were collapsed into one false message).
	withUnparseable := []models.Person{{ID: "p1", Name: "Alex", BirthMonth: "19711-08", Role: models.PersonRolePrimary}}
	unparseableErr := &prepare.PersonBirthMonthError{PersonID: "p1", Name: "Alex", Err: fmt.Errorf("invalid month %q", "19711-08")}
	if msg, ok := personsSaveErrorMessage(unparseableErr, withUnparseable); !ok ||
		strings.Contains(msg, "needs a birth month") || !strings.Contains(msg, "Alex") || !strings.Contains(msg, "19711-08") {
		t.Errorf("expected an accurate unparseable-birth-month message naming Alex and 19711-08, no false 'needs a birth month', got (%q, %v)", msg, ok)
	}
	// No matching ID in the submitted persons: never guess "missing"
	// without evidence.
	if msg, ok := personsSaveErrorMessage(unparseableErr, nil); !ok || strings.Contains(msg, "needs a birth month") {
		t.Errorf("expected no false 'needs a birth month' with no submitted persons to check against, got (%q, %v)", msg, ok)
	}

	// The DISTINCT "present but after start_date" shape must NEVER say
	// "needs a birth month" -- that is what checker-second's attempt-2 FAIL
	// caught (a bare `strings.Contains(msg, "birth_month")` collapsed both
	// shapes).
	afterStartErr := &prepare.PersonBirthMonthAfterStartError{PersonID: "p1", BirthMonth: "2030-01", StartDate: "2026-09"}
	if msg, ok := personsSaveErrorMessage(afterStartErr, nil); !ok || strings.Contains(msg, "needs a birth month") || !strings.Contains(msg, "after") {
		t.Errorf("expected an accurate after-start-date message (no false 'needs a birth month'), got (%q, %v)", msg, ok)
	}
	// With the submitting person (by ID) available, the message names them.
	withPerson := []models.Person{{ID: "p1", Name: "Future Kid", BirthMonth: "2030-01", Role: models.PersonRolePrimary}}
	if msg, ok := personsSaveErrorMessage(afterStartErr, withPerson); !ok || !strings.Contains(msg, "Future Kid") {
		t.Errorf("expected the after-start-date message to name the matching person, got (%q, %v)", msg, ok)
	}

	// InvalidStartDateError must be labelled as the projection start date,
	// never as "person data" (checker-tests' WS5 attempt-3 FAIL).
	startErr := &prepare.InvalidStartDateError{Err: fmt.Errorf("invalid month %q", "2026-13")}
	if msg, ok := personsSaveErrorMessage(startErr, nil); !ok ||
		!strings.Contains(msg, "start date") || strings.Contains(msg, "person data") {
		t.Errorf("expected a start-date-labelled message, not 'person data', got (%q, %v)", msg, ok)
	}

	if _, ok := personsSaveErrorMessage(fmt.Errorf("persons: name is required"), nil); !ok {
		t.Error("expected persons: errors to be recognized generally, not just the birth-month cases")
	}
	if _, ok := personsSaveErrorMessage(fmt.Errorf("disk full"), nil); ok {
		t.Error("an unrelated save error must NOT be reinterpreted as a persons validation error")
	}
	if _, ok := personsSaveErrorMessage(nil, nil); ok {
		t.Error("a nil error must report ok=false")
	}
}

// Table-driven, end-to-end through handleWhatIfSettings: every
// prepare.ValidatePersons/ParseYearMonth error class REACHABLE from this
// handler (the ones parsePersonsForm itself cannot already pre-empt -- "at
// least one person", "id is required" and "invalid role" are caught earlier
// in parsePersonsForm and so never reach ValidatePersons from this call
// site) gets a 400, never a 500, with a message that is accurate FOR THAT
// CLASS. Mutation kills: (1) collapsing the after-start case into the
// missing message -- caught by "birth month after start date (named)"; (2)
// dropping the after-start branch entirely -- same case, since the
// fallback's unenriched text does not name the person; (3) mapping a
// persons error to 500 -- caught by every case's status assertion; (4)
// collapsing "unparseable" into "missing" -- caught by "unparseable birth
// month (named, not missing)"; (5) dropping the start_date: branch --
// caught by "invalid projection start date".
func TestHandleWhatIfSettings_PersonsValidationErrorClasses(t *testing.T) {
	cases := []struct {
		name        string
		form        url.Values
		mustContain []string
		mustNotHave []string
	}{
		{
			name: "missing birth month",
			form: url.Values{
				"start_date":           {"2026-04"},
				"person_id[]":          {"p1"},
				"person_name[]":        {"You"},
				"person_birth_month[]": {""},
				"person_role[]":        {"primary"},
			},
			mustContain: []string{"needs a birth month"},
			mustNotHave: []string{"after"},
		},
		{
			// A five-digit year typed into a type="month" input
			// (checker-tests' WS5 attempt-3 FAIL: Chromium accepts
			// "19711-08" with no max on the field) -- present, just
			// unparseable. Must name the person and the bad value, never
			// claim it is missing.
			name: "unparseable birth month (named, not missing)",
			form: url.Values{
				"start_date":           {"2026-04"},
				"person_id[]":          {"p1"},
				"person_name[]":        {"Pat"},
				"person_birth_month[]": {"19711-08"},
				"person_role[]":        {"primary"},
			},
			mustContain: []string{"Pat", "19711-08"},
			mustNotHave: []string{"needs a birth month"},
		},
		{
			name: "birth month after start date (named)",
			form: url.Values{
				"start_date":           {"2026-09"},
				"person_id[]":          {"p1"},
				"person_name[]":        {"Future Kid"},
				"person_birth_month[]": {"2030-01"},
				"person_role[]":        {"primary"},
			},
			mustContain: []string{"Future Kid", "after", "start"},
			mustNotHave: []string{"needs a birth month"},
		},
		{
			// An invalid PROJECTION start date itself (never a per-person
			// issue) -- checker-tests' WS5 attempt-3 FAIL: this used to
			// come back labelled "Invalid person data".
			name: "invalid projection start date",
			form: url.Values{
				"start_date":           {"2026-13"},
				"person_id[]":          {"p1"},
				"person_name[]":        {"You"},
				"person_birth_month[]": {"1990-01"},
				"person_role[]":        {"primary"},
			},
			mustContain: []string{"start date"},
			mustNotHave: []string{"person data", "needs a birth month"},
		},
		{
			name: "duplicate person id",
			form: url.Values{
				"start_date":           {"2026-04"},
				"person_id[]":          {"dup", "dup"},
				"person_name[]":        {"A", "B"},
				"person_birth_month[]": {"1990-01", "1990-01"},
				"person_role[]":        {"primary", "spouse"},
			},
			mustContain: []string{"duplicate"},
			mustNotHave: []string{"needs a birth month", "after"},
		},
		{
			name: "name required",
			form: url.Values{
				"start_date":           {"2026-04"},
				"person_id[]":          {"p1"},
				"person_name[]":        {""},
				"person_birth_month[]": {"1990-01"},
				"person_role[]":        {"primary"},
			},
			mustContain: []string{"name is required"},
			mustNotHave: []string{"needs a birth month", "after"},
		},
		{
			name: "wrong primary count",
			form: url.Values{
				"start_date":           {"2026-04"},
				"person_id[]":          {"p1"},
				"person_name[]":        {"Spouse Only"},
				"person_birth_month[]": {"1990-01"},
				"person_role[]":        {"spouse"},
			},
			mustContain: []string{"primary"},
			mustNotHave: []string{"needs a birth month", "after"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, cleanup := setupTestEnv(t)
			defer cleanup()

			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/whatif/settings", formBody(tc.form))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			handleWhatIfSettings(w, req)

			if w.Code == http.StatusInternalServerError {
				t.Fatalf("must never 500, got 500. body: %s", w.Body.String())
			}
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
			}
			body := w.Body.String()
			for _, want := range tc.mustContain {
				if !strings.Contains(body, want) {
					t.Errorf("expected the message to contain %q, got: %s", want, body)
				}
			}
			for _, unwanted := range tc.mustNotHave {
				if strings.Contains(body, unwanted) {
					t.Errorf("expected the message to NOT contain %q (would be a false claim for this input), got: %s", unwanted, body)
				}
			}
		})
	}
}

// Removing an UNLINKED person persists (gone after reload).
func TestHandleWhatIfSettings_RemoveUnlinkedPerson_Persists(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.MonthlyHealthcare = 0
	settings.Persons = []models.Person{
		{ID: "p1", Name: "You", BirthMonth: "1958-11", Role: models.PersonRolePrimary},
		{ID: "p2", Name: "Sam", BirthMonth: "1971-08", Role: models.PersonRoleOther},
	}
	prepare.ComputeAges(settings)
	if err := rm.Save(settings); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	form := url.Values{
		"start_date":           {"2026-04"},
		"person_id[]":          {"p1"},
		"person_name[]":        {"You"},
		"person_birth_month[]": {"1958-11"},
		"person_role[]":        {"primary"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Persons) != 1 {
		t.Fatalf("expected the unlinked person to be gone, got %d: %+v", len(loaded.Persons), loaded.Persons)
	}
	if loaded.Persons[0].ID != "p1" {
		t.Errorf("expected the primary person to remain, got %+v", loaded.Persons[0])
	}
}

// ── WS5 attempt 5: identify the failing person by ID, never by parsing
// Error()'s text (ruling 2026-09-24i) ──────────────────────────────────

// extractRenderedErrorMessage pulls the user-facing message out of
// renderError's rendered <p> and HTML-unescapes it, i.e. what the user
// actually reads -- renderError itself HTML-escapes the message exactly
// once (html.EscapeString) before writing it, so this is the inverse.
func extractRenderedErrorMessage(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<p class="mt-2 text-body-sm text-negative">(.*?)</p>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no rendered error message found in body: %s", body)
	}
	return html.UnescapeString(m[1])
}

// nbspName is "Ann" + NBSP (U+00A0) + "Lee". Built as
// string(rune(0x00A0)) -- an ASCII-only integer literal in THIS SOURCE
// FILE, never a raw NBSP byte and never a backslash-u escape sequence
// typed into an edit tool call (checker-tests WS5 attempt 6 FAIL: that
// escape sequence gets decoded to the actual two-byte UTF-8 NBSP
// before it reaches the file, which made both this constant AND its
// own guard below a raw NBSP -- self-referential, so normalizing every
// NBSP in the file to a plain space left the guard silently passing).
// checker-tests WS5 attempt 5's FAIL was the same root cause one layer
// up: the permanent test had ASCII 0x20 where ruling 2026-09-24i's
// I5(a) list requires an actual NBSP.
const nbspName = "Ann" + string(rune(0x00A0)) + "Lee"

// I5(a): every combination of a representative set of names (plain, a
// quoted nickname, a name with a bare backslash, a name with an embedded
// tab, a name with an embedded NBSP, and a name with a trailing quote+colon)
// against every birth-month failure class (empty, unparseable, after start
// date) gets the exact, correctly-worded message -- through the REAL POST
// /whatif/settings handler path, HTML-unescaped as the user reads it.
func TestHandleWhatIfSettings_PersonsBirthMonthMessages_NamesXBirthMonths(t *testing.T) {
	// Guard: nbspName must carry an actual NBSP (U+00A0), never a plain
	// ASCII space -- an editor, a copy-paste, or a future edit could
	// silently turn nbspName's string(rune(0x00A0)) construction into a
	// plain space, which would make this case indistinguishable from the
	// ordinary "Ann Lee" name and silently drop I5(a)'s NBSP coverage.
	if !strings.ContainsRune(nbspName, rune(0x00A0)) {
		t.Fatal("nbspName must contain an actual NBSP (U+00A0) character, not a plain space")
	}

	const startDate = "2026-04"
	names := []string{
		"Alex",
		`Robert "Bob" Smith`,
		`Pat\X`,
		"Ann\tLee",
		nbspName,
		`Sam": X`,
	}
	births := []struct {
		label string
		value string
		want  func(name string) string
	}{
		{
			label: "empty",
			value: "",
			want: func(name string) string {
				return name + " needs a birth month"
			},
		},
		{
			label: "unparseable",
			value: "19711-08",
			want: func(name string) string {
				return fmt.Sprintf(`%s's birth month "19711-08" isn't a valid month (use YYYY-MM)`, name)
			},
		},
		{
			label: "after start",
			value: "2030-01",
			want: func(name string) string {
				return fmt.Sprintf("%s's birth month (2030-01) is after the plan's start date (%s)", name, startDate)
			},
		},
	}

	for _, name := range names {
		for _, bc := range births {
			t.Run(name+"/"+bc.label, func(t *testing.T) {
				_, cleanup := setupTestEnv(t)
				defer cleanup()

				form := url.Values{
					"start_date":           {startDate},
					"person_id[]":          {"p1"},
					"person_name[]":        {name},
					"person_birth_month[]": {bc.value},
					"person_role[]":        {"primary"},
				}
				w := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				handleWhatIfSettings(w, req)

				if w.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
				}
				got := extractRenderedErrorMessage(t, w.Body.String())
				want := bc.want(name)
				if got != want {
					t.Errorf("message = %q, want %q", got, want)
				}
			})
		}
	}
}

// I5(b), first confusion pair: two DIFFERENT names that are near-identical
// (one bare backslash apart -- `Pat\X` vs `Pat\\X`) must never be confused.
// The first row's birth month is unparseable; the second row's birth month
// is valid. The message must name `Pat\X` and 19711-08 ONLY -- never
// `Pat\\X` or 1990-01 (attempt 4's FALSE-message defect: it regexed the
// error's Go-escaped %q text, under which both names print identically).
func TestHandleWhatIfSettings_PersonsBirthMonthMessage_BackslashConfusionPair(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"start_date":           {"2026-04"},
		"person_id[]":          {"p1", "p2"},
		"person_name[]":        {`Pat\X`, `Pat\\X`},
		"person_birth_month[]": {"19711-08", "1990-01"},
		"person_role[]":        {"primary", "spouse"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
	got := extractRenderedErrorMessage(t, w.Body.String())
	want := `Pat\X's birth month "19711-08" isn't a valid month (use YYYY-MM)`
	if got != want {
		t.Errorf("message = %q, want %q (must name Pat\\X and 19711-08 ONLY, never Pat\\\\X or 1990-01)", got, want)
	}
}

// I5(b), second confusion pair: two rows sharing the SAME Name ("Pat
// Twin"). The first row's birth month is valid; the second's is
// unparseable. The message must quote 19711-08 and never 1990-01 --
// attempt 4's FALSE-message defect (a Name-based match against two
// candidates fell back to a hedged, valueless message).
func TestHandleWhatIfSettings_PersonsBirthMonthMessage_SameNamePair(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"start_date":           {"2026-04"},
		"person_id[]":          {"p1", "p2"},
		"person_name[]":        {"Pat Twin", "Pat Twin"},
		"person_birth_month[]": {"1990-01", "19711-08"},
		"person_role[]":        {"primary", "spouse"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
	got := extractRenderedErrorMessage(t, w.Body.String())
	want := `Pat Twin's birth month "19711-08" isn't a valid month (use YYYY-MM)`
	if got != want {
		t.Errorf("message = %q, want %q (must quote 19711-08, never 1990-01)", got, want)
	}
	if strings.Contains(got, "1990-01") {
		t.Errorf("message must never mention the FIRST Pat Twin's valid birth month 1990-01, got: %q", got)
	}
}

// I5(a) supplement (ruling 2026-09-24k / checker-tests WS5 attempt 5 FAIL,
// finding 1): every other unparseable-birth-month case in this file uses
// the VALUE "19711-08", which %q formats identically to plain %s (no
// character %q escapes) -- so a mutation putting %q on the VALUE, rather
// than the Name, survived every prior test. These two values contain a
// character %q DOES escape (a bare backslash, then a double quote), so the
// exact, verbatim (never Go-escaped) message is the only one that can pass
// through the REAL POST /whatif/settings handler path.
func TestHandleWhatIfSettings_PersonsBirthMonthMessage_UnparseableValueSpecialChars(t *testing.T) {
	cases := []struct {
		value string
		want  string
	}{
		{
			value: `1971\08`,
			want:  `Ann's birth month "1971\08" isn't a valid month (use YYYY-MM)`,
		},
		{
			value: `19"71-08`,
			want:  `Ann's birth month "19"71-08" isn't a valid month (use YYYY-MM)`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			_, cleanup := setupTestEnv(t)
			defer cleanup()

			form := url.Values{
				"start_date":           {"2026-04"},
				"person_id[]":          {"p1"},
				"person_name[]":        {"Ann"},
				"person_birth_month[]": {tc.value},
				"person_role[]":        {"primary"},
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			handleWhatIfSettings(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
			}
			got := extractRenderedErrorMessage(t, w.Body.String())
			if got != tc.want {
				t.Errorf("message = %q, want %q", got, tc.want)
			}
		})
	}
}
