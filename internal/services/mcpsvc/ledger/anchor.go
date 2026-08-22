package ledger

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"budget2/internal/services/mcpsvc/confirm"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// errAnchorAccountVanished is returned by applyAnchor's Mutate callback when
// the account named by a redeemed token is no longer present, so Mutate
// aborts the save instead of persisting a filtered/rebuilt account list that
// never actually gets to add the anchor.
var errAnchorAccountVanished = errors.New("ledger: account vanished before the write")

type setBalanceAnchorInput struct {
	AccountID string  `json:"account_id" jsonschema:"the account to record the anchor on, as reported by get_accounts"`
	Date      string  `json:"date" jsonschema:"the date the balance was stated, YYYY-MM-DD; the anchor records the balance as of the END of this day"`
	Amount    float64 `json:"amount" jsonschema:"the account balance as of the end of the date, in dollars (bank convention: positive = money you have)"`
	Note      string  `json:"note,omitempty" jsonschema:"optional note for this anchor, e.g. the source statement"`

	ConfirmToken string `json:"confirm_token,omitempty" jsonschema:"the token returned by a previous call for this same anchor; omit it to get the preview and a fresh token"`
}

type setBalanceAnchorOutput struct {
	Confirmed       bool     `json:"confirmed"`
	AccountID       string   `json:"account_id,omitempty"`
	ConfirmToken    string   `json:"confirm_token,omitempty"`
	ExpiresAt       string   `json:"expires_at,omitempty"`
	WhatWouldHappen string   `json:"what_would_happen,omitempty"`
	HumanApproval   string   `json:"human_approval,omitempty"`
	SnapshotPaths   []string `json:"snapshot_paths,omitempty"`
	AnchorDate      string   `json:"anchor_date,omitempty"`
	Note            string   `json:"note,omitempty"`
}

func registerSetBalanceAnchor(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "set_balance_anchor",
		Description: "Record a BalanceAnchor on an account -- a user-entered {date, amount} stating the account's " +
			"balance as of the END of that day (GLOSSARY.md \"BalanceAnchor\"). Every balance and projection this " +
			"server reports is rolled forward from an anchor, so an anchor is load-bearing: a wrong amount makes " +
			"the dashboard lie about the user's money. THIS WRITES TO THE USER'S DATA (accounts.json). Two steps: " +
			"call it with account_id, date, and amount (no token) to get a description of what would happen plus a " +
			"confirm_token, SHOW THAT TO THE USER, and call again with the same arguments plus the token only after " +
			"they have actually said yes. The token is single-use, expires, and is bound to this tool AND to those " +
			"arguments -- change any of them and it is refused. Confirming twice yourself is not the user " +
			"agreeing; the second call is for after they have answered. On a client that can prompt, the second " +
			"call ALSO puts the question to the user directly and does nothing unless they say yes -- read " +
			"human_approval in the result: \"refused\" means they said no and you must not retry, \"not asked\" " +
			"means this client could not reach anybody so the token alone authorized the write, so say plainly what " +
			"was recorded. A second anchor on the same day OVERWRITES the first (the end-of-day balance is a " +
			"single fact), and an anchor dated earlier than existing anchors is inserted in date order. accounts.json " +
			"is copied to a .bak before the first change of a session that has a file to copy; on a fresh install " +
			"with no accounts file yet there is nothing to back up, so none is taken.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in setBalanceAnchorInput) (res *mcp.CallToolResult, out setBalanceAnchorOutput, err error) {
		defer recoverToError("set_balance_anchor", &err)

		if deps.Accounts == nil {
			return nil, setBalanceAnchorOutput{}, fmt.Errorf("no account store is configured on this server")
		}
		if deps.Confirm == nil {
			return nil, setBalanceAnchorOutput{}, fmt.Errorf("no confirmation registry is configured on this server, so this guarded tool cannot run")
		}

		id := strings.TrimSpace(in.AccountID)
		if id == "" {
			return nil, setBalanceAnchorOutput{}, fmt.Errorf("account_id is required; call get_accounts for the current IDs")
		}
		dateStr := strings.TrimSpace(in.Date)
		if dateStr == "" {
			return nil, setBalanceAnchorOutput{}, fmt.Errorf("date is required (YYYY-MM-DD)")
		}
		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return nil, setBalanceAnchorOutput{}, fmt.Errorf("date %q is not a valid date (want YYYY-MM-DD): %w", dateStr, err)
		}
		note := strings.TrimSpace(in.Note)

		// Validate the account exists before minting anything.
		accts, err := deps.Accounts.LoadAccounts()
		if err != nil {
			return nil, setBalanceAnchorOutput{}, fmt.Errorf("cannot load accounts: %w", err)
		}
		if _, ok := findAccount(accts, id); !ok {
			return nil, setBalanceAnchorOutput{}, fmt.Errorf("no account with id %q; call get_accounts for the current IDs", id)
		}

		token := strings.TrimSpace(in.ConfirmToken)
		if token == "" {
			// The minted args carry the identifying fields but not the token.
			fresh, expires, mintErr := deps.Confirm.Mint("set_balance_anchor", anchorTokenArgs{
				AccountID: id, Date: dateStr, Amount: in.Amount,
			})
			if mintErr != nil {
				return nil, setBalanceAnchorOutput{}, mintErr
			}
			return nil, setBalanceAnchorOutput{
				Confirmed:       false,
				AccountID:       id,
				ConfirmToken:    fresh,
				ExpiresAt:       expires.UTC().Format(time.RFC3339),
				WhatWouldHappen: anchorConsequences(id, dateStr, in.Amount),
				Note: "nothing has been written; show the user what_would_happen and call again with the same " +
					"arguments plus confirm_token ONLY if they agree",
			}, nil
		}

		tokenArgs := anchorTokenArgs{AccountID: id, Date: dateStr, Amount: in.Amount}

		answer, answered := req.Params.InputResponses[confirm.ApprovalID]
		if !answered {
			if err := deps.Confirm.Check(token, "set_balance_anchor", tokenArgs); err != nil {
				return nil, setBalanceAnchorOutput{}, err
			}
			if res, asked := askForApproval(deps, req, "set_balance_anchor", id, token,
				"Record a balance anchor on "+id+"?", anchorConsequences(id, dateStr, in.Amount)); asked {
				return res, setBalanceAnchorOutput{}, nil
			}
			if confirm.CanAsk(req.Session) {
				return &mcp.CallToolResult{
					InputRequests: mcp.InputRequestMap{
						confirm.ApprovalID: confirm.ApprovalRequest(
							"record a balance anchor on "+id,
							anchorConsequences(id, dateStr, in.Amount)),
					},
					RequestState: "set_balance_anchor:" + id,
				}, setBalanceAnchorOutput{}, nil
			}
			return applyAnchor(deps, id, date, in.Amount, note, tokenArgs, token, confirm.NotAsked)
		}

		if d, waitErr, viaBrowser := awaitApproval(ctx, deps, "set_balance_anchor", id, token); viaBrowser {
			if d != confirm.Approved {
				return nil, setBalanceAnchorOutput{
					Confirmed:     false,
					AccountID:     id,
					HumanApproval: confirm.Refused.String(),
					Note:          approvalRefusal("set_balance_anchor", waitErr),
				}, nil
			}
			return applyAnchor(deps, id, date, in.Amount, note, tokenArgs, token, confirm.Approved)
		}

		switch confirm.DecisionFrom(answer) {
		case confirm.Approved:
			return applyAnchor(deps, id, date, in.Amount, note, tokenArgs, token, confirm.Approved)
		default:
			return nil, setBalanceAnchorOutput{
				Confirmed:     false,
				AccountID:     id,
				HumanApproval: confirm.Refused.String(),
				Note: "the user was asked and did NOT approve, so nothing was written. " +
					"Do not retry this without being told to; ask them what they want instead",
			}, nil
		}
	})
}

// anchorTokenArgs are the fields a set_balance_anchor token is bound to. The
// note is intentionally excluded: it is presentation, not the load-bearing
// facts (which account, which day, which amount) that change the dashboard.
type anchorTokenArgs struct {
	AccountID string  `json:"account_id"`
	Date      string  `json:"date"`
	Amount    float64 `json:"amount"`
}

// anchorConsequences is the preview the user agrees to. It names the
// load-bearing facts concretely.
func anchorConsequences(id, date string, amount float64) string {
	return fmt.Sprintf(
		"a BalanceAnchor would be recorded on account %s stating its balance was %.2f as of the END of %s "+
			"(bank convention: positive = money you have). Every balance and projection this server reports "+
			"for that account is rolled forward from an anchor, so a wrong amount makes the dashboard lie about "+
			"the user's money. If an anchor already exists for this day it is OVERWRITTEN (the end-of-day "+
			"balance is a single fact); an earlier-dated anchor is inserted in date order. accounts.json is "+
			"copied to a .bak first when there is a prior file to protect",
		id, amount, date)
}

// applyAnchor redeems the token and writes the anchor. It is the only path
// that spends a token, and the only path that writes.
func applyAnchor(deps Deps, id string, date time.Time, amount float64, note string, tokenArgs anchorTokenArgs, token string, approval confirm.Decision) (*mcp.CallToolResult, setBalanceAnchorOutput, error) {
	if err := deps.Confirm.Redeem(token, "set_balance_anchor", tokenArgs); err != nil {
		return nil, setBalanceAnchorOutput{}, err
	}

	paths, snapNote, err := ensureSnapshot(deps, accountsFile)
	if err != nil {
		return nil, setBalanceAnchorOutput{}, err
	}

	// The load, the edit and the save happen inside one Mutate section, so a
	// concurrent writer or a restore landing between the load and the save
	// cannot lose or resurrect this anchor. ensureSnapshot above is
	// deliberately outside it: a transaction must not span a call into
	// another service (the snapshotter), and the snapshot has to be taken
	// against pre-write contents regardless of what Mutate later loads.
	loaded := false
	err = accounts.Mutate(deps.Store, func(accts []models.Account) ([]models.Account, error) {
		loaded = true
		found := false
		for i := range accts {
			if accts[i].ID != id {
				continue
			}
			// Replace any existing anchor on the same day (the end-of-day
			// balance is a single fact), then insert/append and re-sort.
			filtered := make([]models.BalanceAnchor, 0, len(accts[i].Anchors)+1)
			for _, a := range accts[i].Anchors {
				if !sameDay(a.Date, date) {
					filtered = append(filtered, a)
				}
			}
			filtered = append(filtered, models.BalanceAnchor{Date: date, Amount: amount, Note: note})
			sort.Slice(filtered, func(j, k int) bool { return filtered[j].Date.Before(filtered[k].Date) })
			accts[i].Anchors = filtered
			accts[i].UpdatedAt = time.Now()
			found = true
			break
		}
		if !found {
			return nil, errAnchorAccountVanished
		}
		return accts, nil
	})
	if err != nil {
		if errors.Is(err, errAnchorAccountVanished) {
			// The account vanished between the preview and the redeem. The
			// token is already spent, so say so plainly.
			return nil, setBalanceAnchorOutput{}, fmt.Errorf("account %q disappeared before the write (the confirmation token has been spent; call set_balance_anchor without a token to preview again): not found", id)
		}
		if !loaded {
			// The load itself failed -- distinct from a save/validation
			// failure, and from the vanished-account case above: neither
			// the write nor the token's effect happened here, so this does
			// NOT carry the "confirmation token has been spent" wording.
			return nil, setBalanceAnchorOutput{}, fmt.Errorf("cannot reload accounts before writing: %w", err)
		}
		return nil, setBalanceAnchorOutput{}, fmt.Errorf("%s (the confirmation token has been spent either way; call set_balance_anchor without a token to preview again and get a new one): %w",
			snapNote, err)
	}

	out := setBalanceAnchorOutput{
		Confirmed:     true,
		AccountID:     id,
		AnchorDate:    date.Format("2006-01-02"),
		HumanApproval: approval.String(),
		SnapshotPaths: paths,
		Note:          "anchor recorded; an already-open Accounts page does NOT refresh itself -- it shows stale data until reloaded",
	}
	if snapNote != "" {
		out.Note += "; " + snapNote
	}
	if approval == confirm.NotAsked {
		out.Note += ". NO HUMAN WAS ASKED: this client cannot prompt anyone, so the confirmation token alone " +
			"authorized this. Tell the user plainly what was just recorded"
	}
	return nil, out, nil
}

// ensureSnapshot copies the named sidecar aside before a write. A file that
// does not exist yet is not an error -- a first write on a fresh install has
// nothing to lose -- but any OTHER failure aborts, because it is not evidence
// the file is absent and overwriting it would be unrecoverable. Mirrors
// admin/resolve.go's ensureDecisionsSnapshot.
func ensureSnapshot(deps Deps, name string) (paths []string, note string, err error) {
	if deps.Snapshots == nil {
		return nil, "", fmt.Errorf("refusing to write: no snapshot directory is configured on this server")
	}
	p, err := deps.Snapshots.Ensure(name, time.Now())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "no .bak was taken: " + name + " did not exist yet, so there was no prior state to protect", nil
		}
		return nil, "", fmt.Errorf("refusing to write: could not back up %s: %w", name, err)
	}
	return []string{p}, "", nil
}

// sameDay reports whether a and b fall on the same calendar day.
func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}
