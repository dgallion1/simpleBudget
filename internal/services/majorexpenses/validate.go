package majorexpenses

import (
	"fmt"
	"strings"

	"budget2/internal/models"
)

// maxNameLen bounds a definition's display name.
const maxNameLen = 200

// Validate reports whether a major-expense definition is one the app will
// accept. It is the single source of these rules: the Major Expenses page
// applies them to a parsed HTML form, and the MCP curation tools apply them
// to a tool call, and the two must not drift.
//
// A definition is valid in exactly three configurations:
//
//  1. At least one keyword. An amount range is then optional and is used only
//     to flag anomalies, not to decide whether a transaction matches.
//  2. No keywords, but BOTH ExpectedMin and ExpectedMax set. This matches by
//     amount alone, which is how a fixed-dollar charge whose description
//     varies gets captured; setting them equal matches that one amount.
//  3. No keywords and no bounds at all: a pin-only target, which matches
//     nothing automatically and collects transactions the user pins to it by
//     hand.
//
// Setting exactly one bound with no keyword is the rejected case. It matches
// nothing on its own and almost always means the other bound was forgotten.
//
// Validate reads Name with surrounding whitespace ignored but does not modify
// its argument; callers that persist the definition are expected to have
// trimmed it already.
func Validate(me models.MajorExpense) error {
	name := strings.TrimSpace(me.Name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if len(name) > maxNameLen {
		return fmt.Errorf("name is too long (max %d chars)", maxNameLen)
	}
	if me.ExpectedMin < 0 {
		return fmt.Errorf("expected_min cannot be negative")
	}
	if me.ExpectedMax < 0 {
		return fmt.Errorf("expected_max cannot be negative")
	}
	if me.ExpectedMin > 0 && me.ExpectedMax > 0 && me.ExpectedMin > me.ExpectedMax {
		return fmt.Errorf("expected_min cannot exceed expected_max")
	}
	if len(me.Keywords) == 0 && (me.ExpectedMin > 0) != (me.ExpectedMax > 0) {
		return fmt.Errorf("set BOTH Min and Max to match by amount, or leave both blank to create a pin-only target")
	}
	// A transfer filter only makes sense if it can match something
	// automatically -- pin-only doesn't filter at load time. Require at least
	// a keyword or an amount rule.
	if me.IsInternalTransfer && len(me.Keywords) == 0 && me.ExpectedMin == 0 && me.ExpectedMax == 0 {
		return fmt.Errorf("internal-transfer filter needs at least one keyword or an amount range to match against")
	}
	return nil
}
