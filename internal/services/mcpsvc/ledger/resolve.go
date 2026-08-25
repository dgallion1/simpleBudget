package ledger

import (
	"context"
	"fmt"
	"strings"
	"time"

	"budget2/internal/services/mcpsvc/confirm"
	"budget2/internal/services/transfers"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type resolveTransferInput struct {
	PairKey string `json:"pair_key" jsonschema:"the pair_key from get_suspected_transfers (not from get_transfers, which only returns already-resolved pairs); the two legs of a suspected pair share this key"`
	Verdict string `json:"verdict" jsonschema:"confirm to mark the pair a real transfer (both legs are paired on the next load, pattern hit or not), or reject to mark it a coincidence (never suggested or auto-paired again)"`

	ConfirmToken string `json:"confirm_token,omitempty" jsonschema:"the token returned by a previous call for this same pair and verdict; omit it to get the preview and a fresh token"`
}

type resolveTransferOutput struct {
	Confirmed       bool     `json:"confirmed"`
	PairKey         string   `json:"pair_key,omitempty"`
	Verdict         string   `json:"verdict,omitempty"`
	ConfirmToken    string   `json:"confirm_token,omitempty"`
	ExpiresAt       string   `json:"expires_at,omitempty"`
	WhatWouldHappen string   `json:"what_would_happen,omitempty"`
	HumanApproval   string   `json:"human_approval,omitempty"`
	SnapshotPaths   []string `json:"snapshot_paths,omitempty"`
	Note            string   `json:"note,omitempty"`
}

func registerResolveTransfer(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "resolve_transfer",
		Description: "Confirm or reject a suspected transfer pair. A suspected pair is two cross-account, " +
			"opposite-sign, equal-amount rows inside the pairing window that no transfer pattern backs -- " +
			"coincidentally equal amounts are common, so they are only ever SUGGESTED, never auto-paired " +
			"(GLOSSARY.md \"Internal transfer\"). confirm marks the pair a real transfer: both legs become " +
			"Transfer/paired on the next load whether or not a pattern backs it. reject marks it a " +
			"coincidence: it is never suggested or auto-paired again. THIS WRITES TO THE USER'S DATA " +
			"(transfer_decisions.json). A confirmed pair silently erases real income or real spending if " +
			"the rows were not actually a transfer, so confirm is the load-bearing verdict. Two steps: " +
			"call it with pair_key and verdict (no token) to get a description of what would happen plus a " +
			"confirm_token, SHOW THAT TO THE USER, and call again with the same arguments plus the token only " +
			"after they have actually said yes. The token is single-use, expires, and is bound to this tool " +
			"AND to that pair_key and verdict -- change either and it is refused. Confirming twice yourself " +
			"is not the user agreeing; the second call is for after they have answered. On a client that can " +
			"prompt, the second call ALSO puts the question to the user directly and does nothing unless they " +
			"say yes -- read human_approval in the result: \"refused\" means they said no and you must not " +
			"retry, \"not asked\" means this client could not reach anybody so the token alone authorized the " +
			"write, so say plainly what was recorded. Never invent a pair_key; call get_suspected_transfers for " +
			"one (the same queue the /transfers page shows) -- get_transfers only returns already-resolved pairs " +
			"and its keys will be refused. " +
			"transfer_decisions.json is copied to a .bak before the first change of a " +
			"session that has a file to copy; on a fresh install with no decisions file yet there is nothing " +
			"to back up, so none is taken.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in resolveTransferInput) (res *mcp.CallToolResult, out resolveTransferOutput, err error) {
		defer recoverToError("resolve_transfer", &err)

		if deps.Transfers == nil {
			return nil, resolveTransferOutput{}, fmt.Errorf("no transfer source is configured on this server")
		}
		if deps.Confirm == nil {
			return nil, resolveTransferOutput{}, fmt.Errorf("no confirmation registry is configured on this server, so this guarded tool cannot run")
		}

		key := strings.TrimSpace(in.PairKey)
		if key == "" {
			return nil, resolveTransferOutput{}, fmt.Errorf("pair_key is required; call get_suspected_transfers for one (get_transfers only returns already-resolved pairs)")
		}
		verdict := strings.TrimSpace(in.Verdict)
		var v transfers.Verdict
		switch verdict {
		case string(transfers.VerdictConfirm):
			v = transfers.VerdictConfirm
		case string(transfers.VerdictReject):
			v = transfers.VerdictReject
		default:
			return nil, resolveTransferOutput{}, fmt.Errorf(
				"verdict %q is not recognized; use %q or %q",
				verdict, transfers.VerdictConfirm, transfers.VerdictReject)
		}

		// Validate the pair exists in the suspected queue before minting.
		// SuspectedTransfers() only reflects the most recent load, and
		// resolve_transfer otherwise never triggers one: on a fresh server
		// the queue would be empty and every valid key would be rejected,
		// and after the CSVs change the cached queue would be stale. load()
		// also reports a locked store properly rather than letting
		// ciphertext surface as a parse error.
		if _, err := deps.load(); err != nil {
			return nil, resolveTransferOutput{}, err
		}
		suspected := deps.Transfers.SuspectedTransfers()
		if !suspectedContains(suspected, key) {
			return nil, resolveTransferOutput{}, fmt.Errorf(
				"pair_key %q is not a suspected transfer awaiting review (it may already be resolved, or it may not exist); %s",
				key, availableSuspectedKeys(suspected))
		}

		token := strings.TrimSpace(in.ConfirmToken)
		if token == "" {
			fresh, expires, mintErr := deps.Confirm.Mint("resolve_transfer", resolveTokenArgs{
				PairKey: key, Verdict: verdict,
			})
			if mintErr != nil {
				return nil, resolveTransferOutput{}, mintErr
			}
			return nil, resolveTransferOutput{
				Confirmed:       false,
				PairKey:         key,
				Verdict:         verdict,
				ConfirmToken:    fresh,
				ExpiresAt:       expires.UTC().Format(time.RFC3339),
				WhatWouldHappen: resolveConsequences(key, verdict),
				Note: "nothing has been written; show the user what_would_happen and call again with the same " +
					"arguments plus confirm_token ONLY if they agree",
			}, nil
		}

		tokenArgs := resolveTokenArgs{PairKey: key, Verdict: verdict}

		answer, answered := req.Params.InputResponses[confirm.ApprovalID]
		if !answered {
			if err := deps.Confirm.Check(token, "resolve_transfer", tokenArgs); err != nil {
				return nil, resolveTransferOutput{}, err
			}
			if res, asked := askForApproval(deps, req, "resolve_transfer", key, token,
				"Resolve transfer pair "+key+" as "+verdict+"?", resolveConsequences(key, verdict)); asked {
				return res, resolveTransferOutput{}, nil
			}
			if confirm.CanAsk(req.Session) {
				return &mcp.CallToolResult{
					InputRequests: mcp.InputRequestMap{
						confirm.ApprovalID: confirm.ApprovalRequest(
							"resolve transfer pair "+key+" as "+verdict,
							resolveConsequences(key, verdict)),
					},
					RequestState: "resolve_transfer:" + key,
				}, resolveTransferOutput{}, nil
			}
			return applyResolveTransfer(deps, key, v, tokenArgs, token, confirm.NotAsked)
		}

		if d, waitErr, viaBrowser := awaitApproval(ctx, deps, "resolve_transfer", key, token); viaBrowser {
			if d != confirm.Approved {
				return nil, resolveTransferOutput{
					Confirmed:     false,
					PairKey:       key,
					Verdict:       verdict,
					HumanApproval: confirm.Refused.String(),
					Note:          approvalRefusal("resolve_transfer", waitErr),
				}, nil
			}
			return applyResolveTransfer(deps, key, v, tokenArgs, token, confirm.Approved)
		}

		switch confirm.DecisionFrom(answer) {
		case confirm.Approved:
			return applyResolveTransfer(deps, key, v, tokenArgs, token, confirm.Approved)
		default:
			return nil, resolveTransferOutput{
				Confirmed:     false,
				PairKey:       key,
				Verdict:       verdict,
				HumanApproval: confirm.Refused.String(),
				Note: "the user was asked and did NOT approve, so nothing was written. " +
					"Do not retry this without being told to; ask them what they want instead",
			}, nil
		}
	})
}

// resolveTokenArgs are the fields a resolve_transfer token is bound to.
type resolveTokenArgs struct {
	PairKey string `json:"pair_key"`
	Verdict string `json:"verdict"`
}

// resolveConsequences is the preview the user agrees to.
func resolveConsequences(key, verdict string) string {
	switch verdict {
	case string(transfers.VerdictConfirm):
		return fmt.Sprintf(
			"transfer pair %s would be CONFIRMED as a real transfer: both legs become Transfer/paired on the "+
				"next load whether or not a transfer pattern backs it. If these rows were NOT actually a "+
				"transfer, confirming them silently erases real income or real spending from the totals -- a "+
				"confirmed pair is the load-bearing verdict. transfer_decisions.json is copied to a .bak first "+
				"when there is a prior file to protect",
			key)
	default:
		return fmt.Sprintf(
			"transfer pair %s would be REJECTED as a coincidence: it is never suggested or auto-paired again, "+
				"and both rows stay typed as they were (income/outflow). This is the conservative verdict -- "+
				"if the rows really were a transfer, the totals keep counting both legs. "+
				"transfer_decisions.json is copied to a .bak first when there is a prior file to protect",
			key)
	}
}

// applyResolveTransfer redeems the token and writes the decision. It is the
// only path that spends a token, and the only path that writes.
func applyResolveTransfer(deps Deps, key string, v transfers.Verdict, tokenArgs resolveTokenArgs, token string, approval confirm.Decision) (*mcp.CallToolResult, resolveTransferOutput, error) {
	if err := deps.Confirm.Redeem(token, "resolve_transfer", tokenArgs); err != nil {
		return nil, resolveTransferOutput{}, err
	}

	// Re-validate against a fresh load before writing, not only before
	// minting: the approval round trip (browser or in-client prompt) means
	// time passes between the two, and the underlying CSVs may have changed
	// so the pair is no longer suspected. The token is already spent either
	// way; resolve_transfer without a token mints a fresh one.
	if _, err := deps.load(); err != nil {
		return nil, resolveTransferOutput{}, err
	}
	if suspected := deps.Transfers.SuspectedTransfers(); !suspectedContains(suspected, key) {
		return nil, resolveTransferOutput{}, fmt.Errorf(
			"pair_key %q is no longer a suspected transfer awaiting review (it may already be resolved, or the underlying data changed since it was confirmed); the confirmation token has been spent either way; call resolve_transfer without a token to preview again and get a new one",
			key)
	}

	paths, snapNote, err := ensureSnapshot(deps, transferDecisionsFile)
	if err != nil {
		return nil, resolveTransferOutput{}, err
	}

	if err := deps.Transfers.ResolveTransfer(key, v); err != nil {
		return nil, resolveTransferOutput{}, fmt.Errorf("%s (the confirmation token has been spent either way; call resolve_transfer without a token to preview again and get a new one): %w",
			snapNote, err)
	}

	out := resolveTransferOutput{
		Confirmed:     true,
		PairKey:       key,
		Verdict:       string(v),
		HumanApproval: approval.String(),
		SnapshotPaths: paths,
		Note:          "decision recorded; reload the ledger (or call get_transfers again) to see the updated queue",
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

// suspectedContains reports whether a pair key is in the suspected queue.
func suspectedContains(suspected []transfers.Suspected, key string) bool {
	for _, s := range suspected {
		if s.PairKey == key {
			return true
		}
	}
	return false
}

// availableSuspectedKeys renders the currently-suspected keys for an error
// message, so a model handed a stale or invented key can correct itself in
// one turn. Mirrors admin/resolve.go's availableKeys.
func availableSuspectedKeys(suspected []transfers.Suspected) string {
	if len(suspected) == 0 {
		return "no pairs are currently awaiting review"
	}
	keys := make([]string, 0, len(suspected))
	for _, s := range suspected {
		keys = append(keys, s.PairKey)
	}
	return "currently awaiting review: " + strings.Join(keys, ", ")
}
