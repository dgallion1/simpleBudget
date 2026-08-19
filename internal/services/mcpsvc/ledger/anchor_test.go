package ledger

import (
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/accounts"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- set_balance_anchor ----------------------------------------------------

// The first call MUST NOT write: it returns a preview and a token.
func TestSetBalanceAnchorFirstCallDoesNotWrite(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
	}})
	cs := connect(t, deps)

	out := decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id": "checking",
		"date":       "2026-08-15",
		"amount":     4210.55,
	}))
	if out.Confirmed {
		t.Error("confirmed = true on the first (preview) call")
	}
	if out.ConfirmToken == "" {
		t.Error("no confirm_token returned; the operation can never be confirmed")
	}
	if out.WhatWouldHappen == "" {
		t.Error("no what_would_happen returned; the user has nothing to agree to")
	}

	// Nothing was written: the account still has no anchors.
	accts, err := accounts.Load(deps.Store)
	if err != nil {
		t.Fatalf("accounts.Load: %v", err)
	}
	for _, a := range accts {
		if a.ID == "checking" && len(a.Anchors) != 0 {
			t.Errorf("anchor was written on the preview call; anchors = %v", a.Anchors)
		}
	}
	_ = dir
}

// The second call WITH the token writes the anchor.
func TestSetBalanceAnchorSecondCallWithTokenWrites(t *testing.T) {
	deps, _ := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
	}})
	cs := connect(t, deps)

	first := decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id": "checking",
		"date":       "2026-08-15",
		"amount":     4210.55,
		"note":       "July statement",
	}))
	out := decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id":    "checking",
		"date":          "2026-08-15",
		"amount":        4210.55,
		"note":          "July statement",
		"confirm_token": first.ConfirmToken,
	}))
	if !out.Confirmed {
		t.Fatal("confirmed = false after redeeming a valid token")
	}
	if out.AnchorDate != "2026-08-15" {
		t.Errorf("anchor_date = %q, want 2026-08-15", out.AnchorDate)
	}

	// The anchor is on disk.
	accts, err := accounts.Load(deps.Store)
	if err != nil {
		t.Fatalf("accounts.Load: %v", err)
	}
	var found bool
	for _, a := range accts {
		if a.ID != "checking" {
			continue
		}
		if len(a.Anchors) != 1 {
			t.Fatalf("anchors = %d, want 1", len(a.Anchors))
		}
		if a.Anchors[0].Amount != 4210.55 {
			t.Errorf("anchor amount = %.2f, want 4210.55", a.Anchors[0].Amount)
		}
		found = true
	}
	if !found {
		t.Fatal("checking account not found after the write")
	}
}

// A bad token MUST be refused and nothing is written.
func TestSetBalanceAnchorRefusesABadToken(t *testing.T) {
	deps, _ := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
	}})
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id":    "checking",
		"date":          "2026-08-15",
		"amount":        4210.55,
		"confirm_token": "not-a-real-token",
	}))
	if msg == "" {
		t.Error("no error message explaining the refusal")
	}

	// Nothing was written.
	accts, _ := accounts.Load(deps.Store)
	for _, a := range accts {
		if a.ID == "checking" && len(a.Anchors) != 0 {
			t.Errorf("anchor was written despite a bad token; anchors = %v", a.Anchors)
		}
	}
}

// A replayed token MUST be refused.
func TestSetBalanceAnchorRefusesAReplayedToken(t *testing.T) {
	deps, _ := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
	}})
	cs := connect(t, deps)

	first := decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id": "checking",
		"date":       "2026-08-15",
		"amount":     4210.55,
	}))
	// First redeem succeeds.
	call(t, cs, "set_balance_anchor", map[string]any{
		"account_id":    "checking",
		"date":          "2026-08-15",
		"amount":        4210.55,
		"confirm_token": first.ConfirmToken,
	})
	// Replay must fail.
	res := call(t, cs, "set_balance_anchor", map[string]any{
		"account_id":    "checking",
		"date":          "2026-08-15",
		"amount":        4210.55,
		"confirm_token": first.ConfirmToken,
	})
	if !res.IsError {
		t.Fatal("a replayed token was accepted")
	}
}

// A token is bound to its arguments: changing the amount is refused.
func TestSetBalanceAnchorRefusesATokenForDifferentArguments(t *testing.T) {
	deps, _ := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
	}})
	cs := connect(t, deps)

	first := decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id": "checking",
		"date":       "2026-08-15",
		"amount":     4210.55,
	}))
	res := call(t, cs, "set_balance_anchor", map[string]any{
		"account_id":    "checking",
		"date":          "2026-08-15",
		"amount":        9999.99, // different amount
		"confirm_token": first.ConfirmToken,
	})
	if !res.IsError {
		t.Fatal("a token minted for one amount was accepted for a different amount")
	}
}

// A second anchor on the same day overwrites the first.
func TestSetBalanceAnchorOverwritesSameDayAnchor(t *testing.T) {
	deps, _ := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
		Anchors: []models.BalanceAnchor{{
			// An anchor already exists for 2026-08-15 with a wrong amount.
			Amount: 1000,
		}},
	}})
	// Fix the existing anchor's date so sameDay matches.
	accts, _ := accounts.Load(deps.Store)
	for i := range accts {
		if accts[i].ID == "checking" {
			d, _ := time.Parse("2006-01-02", "2026-08-15")
			accts[i].Anchors[0].Date = d
		}
	}
	if err := accounts.Save(deps.Store, accts); err != nil {
		t.Fatalf("accounts.Save: %v", err)
	}
	cs := connect(t, deps)

	first := decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id": "checking",
		"date":       "2026-08-15",
		"amount":     4210.55,
	}))
	decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id":    "checking",
		"date":          "2026-08-15",
		"amount":        4210.55,
		"confirm_token": first.ConfirmToken,
	}))

	accts, _ = accounts.Load(deps.Store)
	for _, a := range accts {
		if a.ID != "checking" {
			continue
		}
		if len(a.Anchors) != 1 {
			t.Fatalf("anchors = %d, want 1 (same-day anchor overwritten)", len(a.Anchors))
		}
		if a.Anchors[0].Amount != 4210.55 {
			t.Errorf("anchor amount = %.2f, want 4210.55 (overwritten)", a.Anchors[0].Amount)
		}
	}
}

// A nil Confirm registry makes the tool refuse rather than run unguarded.
func TestSetBalanceAnchorWithoutConfirmRegistryReportsIt(t *testing.T) {
	deps, _ := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
	}})
	deps.Confirm = nil
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id": "checking",
		"date":       "2026-08-15",
		"amount":     4210.55,
	}))
	if !strings.Contains(msg, "confirmation registry") {
		t.Errorf("error = %q, want it to name the missing confirmation registry", msg)
	}
}

// The human-approval rung: a refusal leaves the data untouched.
func TestSetBalanceAnchorDoesNotWriteWhenTheUserRefuses(t *testing.T) {
	deps, _ := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
	}})
	cs := connectAsking(t, deps, &mcp.ElicitResult{Action: "decline"}, "BalanceAnchor")

	first := decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id": "checking",
		"date":       "2026-08-15",
		"amount":     4210.55,
	}))
	out := decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id":    "checking",
		"date":          "2026-08-15",
		"amount":        4210.55,
		"confirm_token": first.ConfirmToken,
	}))
	if out.Confirmed {
		t.Error("confirmed = true after the user refused")
	}
	if out.HumanApproval != "refused" {
		t.Errorf("human_approval = %q, want refused", out.HumanApproval)
	}

	// Nothing was written.
	accts, _ := accounts.Load(deps.Store)
	for _, a := range accts {
		if a.ID == "checking" && len(a.Anchors) != 0 {
			t.Errorf("anchor was written after a refusal; anchors = %v", a.Anchors)
		}
	}
}

// On a client that cannot prompt, the token alone authorizes the write and
// the answer admits it.
func TestSetBalanceAnchorOnAClientThatCannotPromptSaysNobodyWasAsked(t *testing.T) {
	deps, _ := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
	}})
	cs := connect(t, deps) // no elicitation handler

	first := decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id": "checking",
		"date":       "2026-08-15",
		"amount":     4210.55,
	}))
	out := decodeToolResult[setBalanceAnchorOutput](t, call(t, cs, "set_balance_anchor", map[string]any{
		"account_id":    "checking",
		"date":          "2026-08-15",
		"amount":        4210.55,
		"confirm_token": first.ConfirmToken,
	}))
	if !out.Confirmed {
		t.Fatalf("the write did not run on a client without elicitation (note: %q)", out.Note)
	}
	if out.HumanApproval != "not asked" {
		t.Errorf("human_approval = %q, want \"not asked\"", out.HumanApproval)
	}
	if !strings.Contains(out.Note, "NO HUMAN WAS ASKED") {
		t.Errorf("note = %q, want it to admit nobody was asked", out.Note)
	}
}
