package whatif

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
)

// WS1 attempt 2 (checker-tests C6) — a GENERIC render→submit round trip:
// render a /whatif card on an off-grid plan, parse every stored-value
// <form> out of the rendered HTML exactly the way a browser would build a
// submission for it (named inputs' rendered values; a NAMED range or an
// off-grid NAMED number whose step isn't "any" is a hard failure, since a
// real browser would either silently snap it or refuse to submit the whole
// form), POST that submission to the form's own endpoint, and assert the
// saved plan is unchanged. Unlike the hand-written url.Values tests in
// ws1_value_fidelity_test.go, this one never lists which field it expects
// to be exact — it derives the submission FROM the markup, so any stored
// numeric field with the same bug (on a sibling the spec didn't happen to
// name) fails it too.

// ── HTML parsing (regex-based — Go's html/template guarantees no literal
//    unescaped '"' inside an attribute value it renders, which is the one
//    property this needs to be safe) ─────────────────────────────────────

var (
	reFormOpen = regexp.MustCompile(`(?s)<form\b[^>]*>`)
	reInputTag = regexp.MustCompile(`(?s)<input\b[^>]*>`)
	reSelectEl = regexp.MustCompile(`(?s)<select\b.*?</select>`)
	reOptionEl = regexp.MustCompile(`(?s)<option\b.*?</option>`)
	reAttrPair = regexp.MustCompile(`([a-zA-Z_:][-a-zA-Z0-9_:.]*)(?:\s*=\s*"([^"]*)")?`)
)

// parseTagAttrs returns the attribute map of a single opening tag (e.g.
// "<input type=\"number\" name=\"x\" checked>"). A boolean attribute (no
// "=value") and an attribute explicitly set to "" both map to "" — callers
// that need to distinguish "present" from "has this value" use the second
// (presence) return of a map lookup, which parseTagAttrs guarantees is true
// for both.
func parseTagAttrs(tag string) map[string]string {
	body := tag
	if i := strings.IndexAny(body, " \t\n"); i != -1 {
		body = body[i:]
	} else {
		body = ""
	}
	body = strings.TrimSuffix(strings.TrimSpace(body), ">")
	body = strings.TrimSuffix(strings.TrimSpace(body), "/")
	attrs := map[string]string{}
	for _, m := range reAttrPair.FindAllStringSubmatch(body, -1) {
		attrs[strings.ToLower(m[1])] = m[2]
	}
	return attrs
}

// browserField is one <input>/<select> a real browser would associate with
// a <form>, in document order.
type browserField struct {
	kind    string // "input" or "select"
	typ     string // input type ("checkbox", "radio", "range", "number", "hidden", "text", ...); "" for select
	name    string
	hasName bool
	value   string
	step    string
	hasStep bool
	min     string
	hasMin  bool
	max     string
	hasMax  bool
	checked bool
}

// browserForm is one <form hx-post=".."|hx-put="..."> with its fields, plus
// the method/endpoint it targets — a direct analog of what probe.js's
// STORED regex finds in real Chromium.
type browserForm struct {
	method   string
	endpoint string
	fields   []browserField
}

// parseBrowserForms finds every <form> in html that carries hx-post or
// hx-put and returns its fields in document order. Forms without either
// attribute (none expected in these partials) are skipped.
func parseBrowserForms(t *testing.T, html string) []browserForm {
	t.Helper()
	var out []browserForm
	for _, loc := range reFormOpen.FindAllStringIndex(html, -1) {
		openTag := html[loc[0]:loc[1]]
		attrs := parseTagAttrs(openTag)
		var method, endpoint string
		if v, ok := attrs["hx-post"]; ok {
			method, endpoint = http.MethodPost, v
		} else if v, ok := attrs["hx-put"]; ok {
			method, endpoint = http.MethodPut, v
		} else {
			continue
		}
		closeRel := strings.Index(html[loc[1]:], "</form>")
		if closeRel == -1 {
			t.Fatalf("unterminated <form> for %s %s", method, endpoint)
		}
		body := html[loc[1] : loc[1]+closeRel]

		f := browserForm{method: method, endpoint: endpoint}
		for _, tag := range reInputTag.FindAllString(body, -1) {
			a := parseTagAttrs(tag)
			typ := a["type"]
			if typ == "" {
				typ = "text"
			}
			name, hasName := a["name"]
			step, hasStep := a["step"]
			min, hasMin := a["min"]
			max, hasMax := a["max"]
			_, checked := a["checked"]
			f.fields = append(f.fields, browserField{
				kind: "input", typ: typ, name: name, hasName: hasName,
				value: a["value"], step: step, hasStep: hasStep,
				min: min, hasMin: hasMin, max: max, hasMax: hasMax, checked: checked,
			})
		}
		for _, sel := range reSelectEl.FindAllString(body, -1) {
			openEnd := strings.Index(sel, ">")
			sa := parseTagAttrs(sel[:openEnd+1])
			name, hasName := sa["name"]
			chosen, have := "", false
			for _, opt := range reOptionEl.FindAllString(sel, -1) {
				oEnd := strings.Index(opt, ">")
				oa := parseTagAttrs(opt[:oEnd+1])
				if !have {
					chosen, have = oa["value"], true
				}
				if _, sel := oa["selected"]; sel {
					chosen = oa["value"]
					break
				}
			}
			f.fields = append(f.fields, browserField{kind: "select", name: name, hasName: hasName, value: chosen})
		}
		out = append(out, f)
	}
	return out
}

// offGrid reports whether value is not an exact multiple-of-step offset
// from min (the HTML5 step-mismatch rule: step base = min). A blank or
// unparseable value/step is never flagged here — an optional field left
// empty is not this test's concern.
func offGrid(value, step, min string) bool {
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return false
	}
	s, err := strconv.ParseFloat(step, 64)
	if err != nil || s <= 0 {
		return false
	}
	m := 0.0
	if min != "" {
		if mv, err := strconv.ParseFloat(min, 64); err == nil {
			m = mv
		}
	}
	ratio := (v - m) / s
	return math.Abs(ratio-math.Round(ratio)) > 1e-9
}

// clampedOut reports whether value falls outside [min, max] — the other
// half of what a browser silently rewrites on a range at parse time.
func clampedOut(value, min string, hasMin bool, max string, hasMax bool) bool {
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return false
	}
	if hasMin {
		if m, err := strconv.ParseFloat(min, 64); err == nil && v < m {
			return true
		}
	}
	if hasMax {
		if x, err := strconv.ParseFloat(max, 64); err == nil && v > x {
			return true
		}
	}
	return false
}

// namedRangeCollidesWithSibling reports whether name is used by fld (a
// NAMED range) AND by at least one OTHER field in fields carrying a
// numerically different value. The established WS1 fix pattern is a
// hidden canonical input plus an UNNAMED mirror range sharing the same
// data-quick-adjust-key; if a range is (re-)named, it now collides with
// that hidden sibling under one name. Whether that collision is
// observable in THIS fixture depends on luck — e.g. a value already on
// the range's own step grid renders identically either way, and even an
// off-grid value can be masked because Go's r.FormValue reads the FIRST
// of several same-named values, which happens to be the hidden field
// here — so this check does not rely on grid alignment: a live name
// collision with a diverging value is flagged unconditionally, the same
// way a real browser sending duplicate names and a DIFFERENT server (or
// a future refactor that reorders the two inputs) could not be trusted
// to keep picking the hidden field's value.
func namedRangeCollidesWithSibling(fld browserField, fields []browserField) bool {
	if fld.kind != "input" || fld.typ != "range" || !fld.hasName {
		return false
	}
	for _, other := range fields {
		if other.kind != "input" || !other.hasName || other.name != fld.name {
			continue
		}
		if (other.kind == fld.kind && other.typ == fld.typ) && other.value == fld.value {
			continue // the range compared to itself
		}
		if other.value == fld.value {
			continue
		}
		fv, ferr := strconv.ParseFloat(fld.value, 64)
		ov, oerr := strconv.ParseFloat(other.value, 64)
		if ferr == nil && oerr == nil && math.Abs(fv-ov) <= 1e-9 {
			continue
		}
		return true
	}
	return false
}

// buildBrowserSubmission builds the url.Values an untouched browser
// submission of f would send, or reports the blocking violations that mean
// a real browser could never submit it at all: a NAMED range whose value
// would actually be snapped/clamped, a NAMED range colliding with a
// same-named sibling of a different value, or a NAMED number whose
// EFFECTIVE step (an absent step attribute defaults to 1, the real HTML5
// rule — WS1 R-TEST) leaves its value off-grid, or whose value falls
// outside its own min/max — HTML5 checkValidity() false blocks the entire
// form, so nothing is sent for ANY field on it.
func buildBrowserSubmission(f browserForm) (url.Values, []string) {
	values := url.Values{}
	var violations []string
	for _, fld := range f.fields {
		if fld.kind == "select" {
			if fld.hasName {
				values.Add(fld.name, fld.value)
			}
			continue
		}
		switch fld.typ {
		case "checkbox", "radio":
			if !fld.hasName || !fld.checked {
				continue
			}
			v := fld.value
			if v == "" {
				v = "on" // HTML default checked-box value when value= is absent
			}
			values.Add(fld.name, v)
		case "range":
			if fld.hasName {
				bad := offGrid(fld.value, fld.step, fld.min) || clampedOut(fld.value, fld.min, fld.hasMin, fld.max, fld.hasMax)
				if bad {
					violations = append(violations, fmt.Sprintf(
						"NAMED range %s: value=%q min=%q max=%q step=%q — a browser silently snaps/clamps this on load and resubmits the wrong figure (must be unnamed with a hidden canonical sibling)",
						fld.name, fld.value, fld.min, fld.max, fld.step))
				} else if namedRangeCollidesWithSibling(fld, f.fields) {
					violations = append(violations, fmt.Sprintf(
						"NAMED range %s: value=%q collides with a same-named sibling field carrying a different value — this range must be unnamed (a mirror), not sharing its canonical hidden sibling's name",
						fld.name, fld.value))
				}
			}
			// An unnamed range is never submitted — correct, it's a mirror.
		case "number":
			if fld.hasName {
				// WS1 R-TEST: a number input's step defaults to 1 (the
				// HTML5 spec default) when the attribute is absent — a
				// mutation that DROPS step="any" rather than setting some
				// other value must still be caught, so an absent step is
				// never treated as "no constraint" the way offGrid's own
				// unparseable-step case is.
				effStep := fld.step
				if !fld.hasStep {
					effStep = "1"
				}
				if effStep != "any" && offGrid(fld.value, effStep, fld.min) {
					violations = append(violations, fmt.Sprintf(
						"NAMED number %s: value=%q step=%q (effective %q) is off its grid — HTML5 checkValidity() is false, which blocks the WHOLE form from submitting anything",
						fld.name, fld.value, fld.step, effStep))
					continue
				}
				// R-TEST: min/max validity, the other half of what a real
				// browser blocks a number input on (rangeUnderflow/
				// rangeOverflow) — a narrowed max below the stored value
				// must fail here even when step="any" already holds.
				if clampedOut(fld.value, fld.min, fld.hasMin, fld.max, fld.hasMax) {
					violations = append(violations, fmt.Sprintf(
						"NAMED number %s: value=%q is outside min=%q/max=%q — HTML5 checkValidity() is false (rangeUnderflow/rangeOverflow), which blocks the WHOLE form from submitting anything",
						fld.name, fld.value, fld.min, fld.max))
					continue
				}
				values.Add(fld.name, fld.value)
			}
		default:
			if fld.hasName {
				values.Add(fld.name, fld.value)
			}
		}
	}
	return values, violations
}

// ── JSON structural diff (float-tolerant), the Go analog of check.py's
//    diff() — used to assert "the saved plan is unchanged". ───────────────

func diffJSONValues(a, b interface{}, path string, out *[]string) {
	switch av := a.(type) {
	case map[string]interface{}:
		bv, ok := b.(map[string]interface{})
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: %v -> %v", path, a, b))
			return
		}
		seen := map[string]bool{}
		for k := range av {
			seen[k] = true
		}
		for k := range bv {
			seen[k] = true
		}
		for k := range seen {
			av2, aok := av[k]
			bv2, bok := bv[k]
			if !aok || !bok {
				*out = append(*out, fmt.Sprintf("%s.%s: presence changed (had=%v have=%v)", path, k, aok, bok))
				continue
			}
			diffJSONValues(av2, bv2, path+"."+k, out)
		}
	case []interface{}:
		bv, ok := b.([]interface{})
		if !ok || len(av) != len(bv) {
			*out = append(*out, fmt.Sprintf("%s: %v -> %v", path, a, b))
			return
		}
		for i := range av {
			diffJSONValues(av[i], bv[i], fmt.Sprintf("%s[%d]", path, i), out)
		}
	case float64:
		// R-EXACT': identity is EXACT, no tolerance — a 1e-9 slop would
		// hide the exact defect class this rule exists to catch (a 1-ulp
		// SS COLA save drift, WS1.3 checker-second/oracle v4: 0.0145 ->
		// 0.014499999999999999 under an untouched save).
		bv, ok := b.(float64)
		if !ok || av != bv {
			*out = append(*out, fmt.Sprintf("%s: %v -> %v", path, a, b))
		}
	default:
		if a != b {
			*out = append(*out, fmt.Sprintf("%s: %v -> %v", path, a, b))
		}
	}
}

// snapshotJSON marshals a WhatIfSettings the same way it is persisted, for
// diffJSONValues to compare.
func snapshotJSON(t *testing.T, s *models.WhatIfSettings) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	return m
}

// ── Handler dispatch: POST/PUT a built submission to the endpoint its own
//    <form> named, exactly like probe.js's per-form htmx trigger. ─────────

func postBrowserForm(t *testing.T, f browserForm, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	newReq := func() *http.Request {
		req := httptest.NewRequest(f.method, f.endpoint, formBody(values))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req
	}
	switch {
	case f.endpoint == "/whatif/settings" && f.method == http.MethodPost:
		handleWhatIfSettings(w, newReq())
	case f.endpoint == "/whatif/glide-path" && f.method == http.MethodPost:
		handleWhatIfGlidePath(w, newReq())
	case f.endpoint == "/whatif/spending-phases" && f.method == http.MethodPost:
		handleWhatIfSpendingPhases(w, newReq())
	case f.endpoint == "/whatif/guardrails" && f.method == http.MethodPost:
		handleWhatIfGuardrails(w, newReq())
	case f.endpoint == "/whatif/roth-conversion" && f.method == http.MethodPost:
		handleWhatIfRothConversion(w, newReq())
	case f.endpoint == "/whatif/social-security" && f.method == http.MethodPost:
		handleWhatIfSocialSecurity(w, newReq())
	case strings.HasPrefix(f.endpoint, "/whatif/healthcare/") && f.method == http.MethodPut:
		id := strings.TrimPrefix(f.endpoint, "/whatif/healthcare/")
		req := chiRequest(http.MethodPut, f.endpoint, formBody(values), map[string]string{"id": id})
		handleWhatIfUpdateHealthcare(w, req)
	default:
		t.Fatalf("postBrowserForm: no handler wired for %s %s — extend the dispatch", f.method, f.endpoint)
	}
	return w
}

// numericArtifact matches a float-noise tail — six-or-more repeated 0s/9s
// followed by one off digit (e.g. "1.0004000000000002",
// "388354.5900000001") — the same pattern render_helpers_test.go's
// TestFormatExactNoArtifactsAcrossRange pins at the formatter level; this
// is the template-level counterpart, since a formatter revert (e.g.
// formatExactScaled -> formatExact on the ONE scaled COLA site) can render
// a noisy value the round-trip identity check alone does not observe
// whenever the SAVE path happens to clean the noise back up again (the
// server's own ÷100 rounding does exactly that here) — the noisy digits
// were still real, user-visible text in the meantime.
var numericArtifact = regexp.MustCompile(`\d\.\d*(?:0{6,}[1-9]|9{6,}\d)`)

// assertStructuralInvariants asserts, for every NAMED <input type="number">
// whose current rendered value is fractional (i.e. it is a formatExact-
// rendered stored field, never an integer-domain one like an age or a
// count of years), the LITERAL structural invariants R-TEST' requires:
// step="any" — the literal attribute, not merely a coarser step under
// which THIS fixture's value happens to still validate (WS1.3
// checker-tests F1: a step="any" -> step="0.25" mutation on floor_cut_pct
// survived because the fixture's 10.25 happens to sit on a 0.25 grid too)
// — and min <= value <= max (independent of, and in addition to, the
// browser-simulated clampedOut check buildBrowserSubmission already runs
// before a form is submitted). Also asserts no exponent notation and no
// float-noise artifact, at any magnitude/precision.
func assertStructuralInvariants(t *testing.T, templateName string, forms []browserForm) {
	t.Helper()
	for i, f := range forms {
		for _, fld := range f.fields {
			if fld.kind != "input" || fld.typ != "number" || !fld.hasName || fld.value == "" {
				continue
			}
			// R-EXACT'/R-TEST': never exponent notation, at any magnitude
			// this field's rendered value happens to reach — checked
			// unconditionally (a whole-number exponent form like "1e+06"
			// carries no ".", so this must not be gated behind the
			// fractional-value check below).
			if strings.ContainsAny(fld.value, "eE") {
				t.Errorf("%s form #%d: NAMED number %s value %q is in exponent notation — formatExact/formatExactScaled must never emit one",
					templateName, i, fld.name, fld.value)
			}
			if numericArtifact.MatchString(fld.value) {
				t.Errorf("%s form #%d: NAMED number %s value %q looks like a float-noise artifact — a raw stored value must never be re-rounded (formatExact) and a scaled one must round its OWN noise away (formatExactScaled)",
					templateName, i, fld.name, fld.value)
			}
			if strings.Contains(fld.value, ".") {
				// A fractional value means this field IS formatExact-
				// rendered (never an integer-domain one like an age or a
				// count of years) — step="any" is required LITERALLY, not
				// merely a coarser step under which THIS fixture's value
				// happens to still validate (WS1.3 checker-tests F1: a
				// step="any" -> step="0.25" mutation on floor_cut_pct
				// survived because the fixture's 10.25 happens to sit on a
				// 0.25 grid too).
				if fld.step != "any" {
					t.Errorf("%s form #%d: NAMED number %s has a fractional stored value %q but step=%q — R-TEST' requires step=\"any\" literally, not merely a step this one value happens to validate under",
						templateName, i, fld.name, fld.value, fld.step)
				}
			}
			v, err := strconv.ParseFloat(fld.value, 64)
			if err != nil {
				continue
			}
			if fld.hasMin {
				if m, err := strconv.ParseFloat(fld.min, 64); err == nil && v < m-1e-9 {
					t.Errorf("%s form #%d: NAMED number %s value %v is below its own min=%s", templateName, i, fld.name, v, fld.min)
				}
			}
			if fld.hasMax {
				if x, err := strconv.ParseFloat(fld.max, 64); err == nil && v > x+1e-9 {
					t.Errorf("%s form #%d: NAMED number %s value %v exceeds its own max=%s", templateName, i, fld.name, v, fld.max)
				}
			}
		}
	}
}

// runGenericRoundTrip renders templateName with data, parses every
// stored-value <form> it contains, submits each one as an untouched
// browser would, and asserts the saved plan is unchanged. A blocking
// violation on any form is itself a failure (a real browser could never
// submit it), reported without attempting the POST.
func runGenericRoundTrip(t *testing.T, rm *retirement.SettingsManager, templateName string, data map[string]any) {
	t.Helper()
	baseline, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() baseline error: %v", err)
	}
	before := snapshotJSON(t, baseline)

	out, err := renderer.RenderToString(templateName, data)
	if err != nil {
		t.Fatalf("RenderToString(%s): %v", templateName, err)
	}

	forms := parseBrowserForms(t, out)
	if len(forms) == 0 {
		t.Fatalf("no stored-value <form hx-post|hx-put> found rendering %s", templateName)
	}
	assertStructuralInvariants(t, templateName, forms)

	anySubmitted := false
	for i, f := range forms {
		values, violations := buildBrowserSubmission(f)
		if len(violations) > 0 {
			t.Errorf("%s form #%d (%s %s) would never submit in a real browser:\n  %s",
				templateName, i, f.method, f.endpoint, strings.Join(violations, "\n  "))
			continue
		}
		w := postBrowserForm(t, f, values)
		if w.Code != http.StatusOK {
			t.Errorf("%s form #%d (%s %s): status = %d, want 200. body: %s",
				templateName, i, f.method, f.endpoint, w.Code, truncate(w.Body.String(), 500))
			continue
		}
		anySubmitted = true
	}
	if !anySubmitted {
		return // every form on this render was a blocking violation; already reported above.
	}

	after, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() after error: %v", err)
	}
	var diffs []string
	diffJSONValues(before, snapshotJSON(t, after), "", &diffs)
	if len(diffs) > 0 {
		t.Errorf("%s: saved plan changed after an untouched submission (%d field(s)):\n  %s",
			templateName, len(diffs), strings.Join(diffs, "\n  "))
	}
}

// ── Fixtures + one subtest per template family ──────────────────────────

// baseFixture returns a settings struct pre-migrated the way an
// already-saved real plan would be: RMDTiming/SpouseSoleBeneficiary set and
// the legacy MonthlyHealthcare figure zeroed. Without this, the FIRST
// rm.Load() call in a fresh test returns the SettingsManager's in-memory
// cache untouched, while the handler's internal read-modify-write goes
// through the uncached disk path and runs initializeLoadedSettings's
// one-time migrations (RMDTiming "" -> "start_of_year", a legacy
// MonthlyHealthcare > 0 with no HealthcarePersons yet -> a synthesized
// "migrated-user" entry) — a real but WS1-unrelated cache/disk skew that
// would otherwise show up as a false "saved plan changed" on every family,
// not the value-fidelity regression this test exists to catch.
func baseFixture() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.UseCurrentMonth = false
	s.RMDTiming = models.RMDTimingStartOfYear
	spouseSoleBeneficiary := false
	s.SpouseSoleBeneficiary = &spouseSoleBeneficiary
	s.MonthlyHealthcare = 0
	return s
}

func TestWS1GenericRoundTrip_PortfolioSettings(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := baseFixture()
	s.PortfolioValue = 2437512.34
	s.MonthlyLivingExpenses = 10737.45
	s.MonthlyPropertyTax = 666.67
	s.PropertyTaxInflation = 2.25
	s.ProjectionYears = 30
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	runGenericRoundTrip(t, rm, "whatif-portfolio-settings", map[string]any{
		"Settings":                s,
		"LivingExpensesPhaseNote": buildLivingExpensesPhaseNote(s),
	})
}

func TestWS1GenericRoundTrip_HealthcarePerson(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := baseFixture()

	// R-TEST: every stored float field fractional, incl. ones a whole
	// fixture value would leave a %.0f mutation undetectable on (WS1.2
	// checker-tests G3: care_monthly_cost=4321 and medicare_monthly_cost=
	// 2150 were both whole here, so mutating their template formatter to
	// %.0f rendered the SAME string and survived).
	// R-TEST' fixture rule: every exact-render field needs >=2 GENUINE
	// decimals — 1655.30/612.40 have a shortest round-trip of only 1
	// decimal (trailing zero), so a %.1f mutation on current_monthly_cost
	// rendered the SAME string and survived (WS1.4 checker-tests
	// survivors, healthcare-person.html:84 and the aca_cost_after_employer
	// hidden input at :135, also 1655.30).
	aca := models.HealthcarePerson{
		ID:                    "hc-a",
		Name:                  "Alex",
		CurrentAge:            58,
		CurrentCoverage:       models.CoverageACA,
		CurrentMonthlyCost:    1655.35,
		PreMedicareInflation:  7.25,
		MedicareMonthlyCost:   2150.65,
		PostMedicareInflation: 4.15,
		MedicareEligibleAge:   65,
		CareStartAge:          85,
		CareMonthlyCost:       4321.75,
	}
	employer := models.HealthcarePerson{
		ID:                    "hc-b",
		Name:                  "Sam",
		CurrentAge:            55,
		CurrentCoverage:       models.CoverageEmployer,
		CurrentMonthlyCost:    612.45,
		EmployerCoverageYears: 3,
		ACACostAfterEmployer:  2344.65,
		PreMedicareInflation:  7.25,
		MedicareMonthlyCost:   1400.45,
		PostMedicareInflation: 4.15,
		MedicareEligibleAge:   65,
	}
	// U12 / WS1.4 checker-tests survivor: the Medicare-branch
	// post_medicare_inflation range (name= restored survives, as does
	// %.1f on its hidden input, healthcare-person.html:211) is only
	// rendered for a Medicare-COVERAGE person — no prior fixture ever
	// created one.
	medicare := models.HealthcarePerson{
		ID:                    "hc-c",
		Name:                  "Morgan",
		CurrentAge:            68,
		CurrentCoverage:       models.CoverageMedicare,
		CurrentMonthlyCost:    289.35,
		PostMedicareInflation: 4.35,
		MedicareEligibleAge:   65,
	}
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if _, err := rm.AddHealthcarePerson(aca); err != nil {
		t.Fatalf("AddHealthcarePerson(aca) error: %v", err)
	}
	if _, err := rm.AddHealthcarePerson(employer); err != nil {
		t.Fatalf("AddHealthcarePerson(employer) error: %v", err)
	}
	if _, err := rm.AddHealthcarePerson(medicare); err != nil {
		t.Fatalf("AddHealthcarePerson(medicare) error: %v", err)
	}

	for _, p := range []models.HealthcarePerson{aca, employer, medicare} {
		runGenericRoundTrip(t, rm, "whatif-healthcare-person", map[string]any{
			"Settings": s,
			"Person":   p,
		})
	}
}

// TestWS1GenericRoundTrip_RateAssumptions covers all three <form>s the
// "whatif-rate-assumptions" partial renders in one pass (the 34-field
// settings form, the glide-path form, and the inflation/decline/investment
// -return sliders form) against one off-grid fixture that sets every field
// any of them owns, since runGenericRoundTrip exercises every stored-value
// form found in a render regardless of which one a narrower fixture meant
// to target.
func TestWS1GenericRoundTrip_RateAssumptions(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := baseFixture()
	// Phases disabled so the spending-decline-rate slider (not a
	// "using phase-based spending" message) is what renders in form #3.
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: false, Phases: models.DefaultSpendingPhases()}
	s.TaxDeferredPercent = 83.037
	// R-TEST' fixture rule: every exact-render field needs >=2 GENUINE
	// decimals (or 16-17 sig figs) — 0.5/70.5/60.5/1.5/99.5/0.5/95.5/0.3/
	// 70.5 (glide start) each have a shortest round-trip of only 1
	// decimal, so a %.1f mutation on any of them rendered the SAME string
	// and survived (WS1.4 checker-tests survivors, rate-assumptions.html:
	// 117/289/320/329/374/383/209/487/411). TaxDeferredCashPercent (2.25),
	// TaxableDividendYield (0.45), TaxableCapitalGainsDistributionRate
	// (1.25), GlidePath.EndStockPct (40.25) are untouched: already
	// genuinely 2-decimal.
	s.RothPercent = 0.55
	s.TaxDeferredStockPercent = 70.55
	s.TaxDeferredCashPercent = 2.25
	s.RothStockPercent = 60.55
	s.RothCashPercent = 1.55
	s.TaxableStockPercent = 99.35
	s.TaxableCashPercent = 0.55
	s.TaxableDividendYield = 0.45
	s.TaxableQualifiedDividendPercent = 95.55
	s.TaxableCapitalGainsDistributionRate = 1.25
	basis := 276146.86
	s.TaxableCostBasis = &basis
	s.TaxConfig = models.DefaultTaxConfig()
	rate := 5.525
	s.TaxConfig.StateIncomeTaxRate = &rate
	s.ACA = &models.ACAConfig{HouseholdSize: 2}
	credit := 10850.75 // R-TEST: fractional (was a whole 10850, WS1.2 checker-tests G3 finding F6)
	s.ACA.AnnualPremiumTaxCredit = &credit
	s.InflationRate = 3.85
	s.SpendingDeclineRate = 0.35
	s.InvestmentReturn = 6.25
	// R-TEST: an ENABLED glide path, fractional start/end — the glide-path
	// form's own number inputs never rendered on the old fixture (WS1.2
	// checker-tests G4), so a %.0f mutation on start_stock_pct (F8)
	// survived undetected.
	s.GlidePath = &models.GlidePathConfig{Enabled: true, StartStockPct: 70.55, EndStockPct: 40.25, TransitionYears: 10}
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	runGenericRoundTrip(t, rm, "whatif-rate-assumptions", map[string]any{"Settings": s})
}

func TestWS1GenericRoundTrip_SpendingPhases(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := baseFixture()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			// R-TEST: fractional (was a whole 1.0) — every stored float
			// field, incl. this one, must be off-grid or a %.0f-style
			// mutation on it goes undetected.
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.015, Description: "Active retirement"},
			{Name: "Active", StartAge: 65, Multiplier: 0.93, Description: "Pacing slows"},
		},
	}
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	runGenericRoundTrip(t, rm, "whatif-spending-phases", map[string]any{"Settings": s})
}

func TestWS1GenericRoundTrip_Guardrails(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := baseFixture()
	s.Guardrails = &models.GuardrailConfig{
		Enabled: true,
		// 3 decimals AND >= 1e6: was step="0.01" (WS1.3 checker-tests
		// F-C2: blocks the whole guardrails form on a value like this),
		// rendered via a plain {{.}} (Go's %v, which switches to EXPONENT
		// notation above ~1e6 — this field has no max, so nothing bounds
		// it below that). Now step="any" + formatExact ('f', never
		// exponent), so this value round-trips exactly and plainly.
		MinMonthlySpendingReal: 1234567.555,
		// R-TEST' fixture rule: every one of these needs >=2 GENUINE
		// decimals — 5.5/10.5/2.5/0.5/120.5 each have a shortest
		// round-trip of only 1 decimal, so a %.1f mutation on any of them
		// rendered the SAME string and survived (WS1.4 checker-tests
		// survivors, guardrails.html:27/47/54/67/73). FloorCutPct (10.25)
		// is untouched: it is an exact .X5 TIE, already genuinely
		// 2-decimal (and already diverges from %.1f's half-even "10.2").
		FloorDropPct:    5.55,
		FloorCutPct:     10.25,
		CeilingRisePct:  10.55,
		CeilingRaisePct: 2.55,
		MinSpendingPct:  0.55,
		MaxSpendingPct:  120.55,
	}
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	runGenericRoundTrip(t, rm, "whatif-guardrails", map[string]any{"Settings": s})
}

func TestWS1GenericRoundTrip_RothConversion(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := baseFixture()
	s.RothConversion = &models.RothConversionConfig{
		Enabled: true,
		// R-TEST' fixture rule: >=2 genuine decimals (50000.5's shortest
		// round-trip has only 1, so a %.1f mutation rendered the SAME
		// string and survived — WS1.4 checker-tests survivor,
		// roth-conversion.html:51).
		AnnualAmount: 50000.55,
		StartYear:    0,
		EndYear:      0,
	}
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	runGenericRoundTrip(t, rm, "whatif-roth-conversion", map[string]any{"Settings": s})
}

func TestWS1GenericRoundTrip_SocialSecurity(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := baseFixture()
	s.Persons = append(s.Persons, models.Person{
		ID: "sp-1", Name: "Spouse", BirthMonth: "1962-01", Role: models.PersonRoleSpouse,
	})
	s.SocialSecurity = &models.SocialSecurityConfig{
		// R-TEST' fixture rule: >=2 genuine decimals (4114.9's shortest
		// round-trip has only 1 — a %.1f mutation on fra_benefit rendered
		// the SAME string and survived, WS1.4 checker-tests survivor,
		// social-security.html:17).
		FRABenefit: 4114.95,
		FRA:        67,
		// R-EXACT': 0.010004 exercises BOTH halves of the COLA round-trip
		// fix on one value — (a) COLARate*100 (1.0004000000000002) has
		// real multiplication noise formatExactScaled must round away to
		// render "1.0004" (a revert to formatExact, no rounding, would
		// submit the noisy digits back verbatim), and (b) even the CLEAN
		// "1.0004" parsed and divided by 100 lands 1 ulp off the original
		// (0.010003999999999999) unless the handler's own ÷100 also
		// rounds to 15 significant digits. (0.0145, an earlier fixture
		// value, happens to round-trip exactly through raw division and so
		// tested only half of this.)
		COLARate:         0.010004,
		COLARateSet:      true,
		SpouseFRABenefit: 1905.55,
		SpouseFRA:        67,
	}
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	runGenericRoundTrip(t, rm, "whatif-social-security-config", map[string]any{"Settings": s})
}
