// Package completeness inspects a WhatIfSettings for silent zero-defaults
// and other invariant violations that would produce mathematically valid
// but materially incomplete projections. Findings surface to the user via
// a banner above the projection chart so the omission is visible before
// they trust the numbers.
package completeness

import "budget2/internal/models"

// Severity ranks findings from informational to outright inconsistent.
// SeverityError means the projection is internally inconsistent (e.g.
// MFJ filing status with no spouse Person — taxes are computed for two,
// IRMAA / RMD for one). Warn means a silent zero is likely material.
// Info is discoverability only.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarn
	SeverityError
)

// Finding describes one detected issue with a scenario.
//
// Code is a stable identifier safe to use as a test fixture, telemetry
// key, or i18n lookup. FormAnchor is the fragment id (without "#") on
// the what-if page that the user can be deep-linked to in order to fix
// the issue.
type Finding struct {
	Severity   Severity
	Code       string
	Title      string
	Detail     string
	FormAnchor string
	Action     string
}

// Check returns findings ordered errors-first, then warnings, then info.
// Within a severity tier, order matches the order the checks are listed
// below (which roughly corresponds to "tax-related → income → household").
//
// Check is pure: it never mutates settings, never reads disk, never calls
// the engine. nil settings yields a single SeverityError finding so the
// banner still renders something meaningful.
func Check(s *models.WhatIfSettings) []Finding {
	if s == nil {
		return []Finding{{
			Severity: SeverityError,
			Code:     "settings_nil",
			Title:    "Scenario could not be loaded",
			Detail:   "Settings are missing. The projection cannot run.",
			Action:   "Reload the page",
		}}
	}

	var findings []Finding
	findings = appendIfPresent(findings, checkStateTaxUnset(s))
	return sortBySeverity(findings)
}

// appendIfPresent skips nil findings so individual check functions can
// return (*Finding) and let the orchestrator drop "no finding" cases.
func appendIfPresent(findings []Finding, f *Finding) []Finding {
	if f == nil {
		return findings
	}
	return append(findings, *f)
}

// sortBySeverity stable-sorts findings by descending severity. Errors
// first, then warnings, then info. Within a tier, append order is
// preserved so individual checks can document their intended order.
func sortBySeverity(findings []Finding) []Finding {
	if len(findings) < 2 {
		return findings
	}
	out := make([]Finding, 0, len(findings))
	for _, sev := range []Severity{SeverityError, SeverityWarn, SeverityInfo} {
		for _, f := range findings {
			if f.Severity == sev {
				out = append(out, f)
			}
		}
	}
	return out
}
