package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	backupsvc "budget2/internal/services/backup"
	"budget2/internal/services/mcpsvc/confirm"
	"budget2/internal/services/restore"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type restoreBackupInput struct {
	Name         string `json:"name" jsonschema:"the archive filename exactly as list_backups reported it, e.g. budget_backup_20260801_030000.zip"`
	ConfirmToken string `json:"confirm_token,omitempty" jsonschema:"the token returned by a previous call for this same archive; omit it to get the preview and a fresh token"`
}

type restoreBackupOutput struct {
	Confirmed    bool   `json:"confirmed"`
	Name         string `json:"name,omitempty"`
	ConfirmToken string `json:"confirm_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`

	WhatWouldHappen string `json:"what_would_happen,omitempty"`

	// HumanApproval is what an actual person said: "approved" when one was
	// shown the consequences and agreed, "refused" when they did not, and
	// "not asked" when this client cannot prompt anybody. The last of those is
	// the honest admission that the token alone authorized the write; it is a
	// string rather than a bool so an unasked operation cannot be skimmed as
	// an approved one.
	HumanApproval string `json:"human_approval,omitempty"`

	// Counts, populated only on a confirmed restore. Restored counts archive
	// entries written; Pruned counts live files deleted because the archive
	// did not contain them -- the number the user most needs to see.
	Restored         int `json:"restored,omitempty"`
	Pruned           int `json:"pruned,omitempty"`
	SkippedProtected int `json:"skipped_protected,omitempty"`
	PruneFailures    int `json:"prune_failures,omitempty"`

	Note string `json:"note,omitempty"`
}

// restoreConsequences describes the blast radius in the concrete terms the
// user needs to agree to. It names the prune explicitly: replacing files is
// the obvious half of a restore, and deleting files the archive never had is
// the half that surprises people.
func restoreConsequences(a backupsvc.Archive) string {
	return fmt.Sprintf(
		"every file in the user's data directory would be rewritten from the archive %s (taken %s UTC, %d bytes), "+
			"and ANY FILE THE ARCHIVE DOES NOT CONTAIN WOULD BE DELETED -- including bank CSVs imported since it "+
			"was taken, duplicate-resolution decisions, major-expense definitions and the saved retirement plan. "+
			"A safety snapshot of the current data is taken first and lands in the backup directory as a new "+
			"archive, so the present state is recoverable only by restoring THAT archive afterwards; there is no "+
			"undo tool here. Any browser tab the user has open keeps showing the old data until they reload it",
		a.Name, a.TS.UTC().Format(time.RFC3339), a.Bytes)
}

func registerRestoreBackup(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "restore_backup",
		Description: "Overwrite the user's entire data directory with the contents of a backup archive. THIS IS " +
			"DESTRUCTIVE AND THERE IS NO UNDO TOOL: it rewrites every file from the archive AND DELETES every " +
			"file the archive does not contain -- CSVs imported since it was taken, duplicate decisions, major " +
			"expense definitions, and the saved retirement plan all go. A safety snapshot of the current data is " +
			"taken first and written to the backup directory, so the pre-restore state can only be recovered by " +
			"restoring that snapshot in turn. Get `name` from list_backups and pass it back verbatim; never " +
			"invent or edit one. Two steps: call it with `name` alone to get a description of exactly what would " +
			"be lost plus a confirm_token, SHOW THAT DESCRIPTION TO THE USER, and call again with the same name " +
			"plus the token only after they have actually said yes. The token is single-use, expires, and is " +
			"bound to this tool AND to that one archive name -- change the name and it is refused. Confirming " +
			"twice yourself is not the user agreeing; the second call is for after they have answered. On a " +
			"client that can prompt, the second call ALSO puts the question to the user directly and does " +
			"nothing unless they say yes -- if human_approval comes back \"refused\", they said no: do not try " +
			"again, ask them what they want instead. When human_approval says \"not asked\", this client could " +
			"not reach anybody and the token alone authorized the write, so say plainly what was overwritten. " +
			"Prefer run_backup first if there is any doubt: it costs a zip and makes the current state restorable.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in restoreBackupInput) (res *mcp.CallToolResult, out restoreBackupOutput, err error) {
		defer recoverToError("restore_backup", &err)

		// Fail before minting anything the server could never honor: a token
		// handed out by a server with no restore path is a promise it cannot
		// keep, which is how shutdown_server treats a nil Shutdown too.
		if deps.Restores == nil {
			return nil, restoreBackupOutput{}, fmt.Errorf("no restore service is configured on this server")
		}
		if deps.Backups == nil {
			return nil, restoreBackupOutput{}, fmt.Errorf("no backup service is configured on this server, so archives cannot be identified")
		}
		if deps.Confirm == nil {
			return nil, restoreBackupOutput{}, fmt.Errorf("no confirmation registry is configured on this server, so this guarded tool cannot run")
		}

		name := strings.TrimSpace(in.Name)
		if name == "" {
			return nil, restoreBackupOutput{}, fmt.Errorf("name is required: call list_backups and pass one of the names it reports")
		}

		token := strings.TrimSpace(in.ConfirmToken)
		if token == "" {
			archive, findErr := findArchive(deps, name)
			if findErr != nil {
				return nil, restoreBackupOutput{}, findErr
			}
			// The minted args carry the name but not the token: `in` at redeem
			// time carries the token itself, so hashing `in` would never match.
			fresh, expires, mintErr := deps.Confirm.Mint("restore_backup", restoreBackupInput{Name: name})
			if mintErr != nil {
				return nil, restoreBackupOutput{}, mintErr
			}
			return nil, restoreBackupOutput{
				Confirmed:       false,
				Name:            name,
				ConfirmToken:    fresh,
				ExpiresAt:       expires.UTC().Format(time.RFC3339),
				WhatWouldHappen: restoreConsequences(archive),
				Note: "nothing has been restored or deleted; show the user what_would_happen and call again with " +
					"the same name plus confirm_token ONLY if they agree",
			}, nil
		}

		// The token's minted args carry the name but not the token itself, so
		// every Check and Redeem below hashes the same value Mint recorded.
		tokenArgs := restoreBackupInput{Name: name}

		// A token is deliberateness. Before spending it, put the operation to
		// an actual person -- which is the thing the token cannot do, since a
		// model can mint and redeem one inside a single turn.
		//
		// The approval is a multi-round-trip request (SEP-2322): this handler
		// returns the question, the client asks the human, and the SAME call
		// is re-invoked with the answer. Which is why the token is only
		// Checked here and Redeemed on the way back -- redeeming now would
		// spend it before the retry could use it.
		answer, answered := req.Params.InputResponses[confirm.ApprovalID]
		if !answered {
			if err := deps.Confirm.Check(token, "restore_backup", tokenArgs); err != nil {
				return nil, restoreBackupOutput{}, err
			}
			archive, findErr := findArchive(deps, name)
			if findErr != nil {
				return nil, restoreBackupOutput{}, findErr
			}
			// Best rung first: the person answers on the app's own page,
			// where the whole operation is on screen.
			if res, asked := askForApproval(deps, req, "restore_backup", name,
				"Restore the backup "+name+"?", restoreConsequences(archive)); asked {
				return res, restoreBackupOutput{}, nil
			}
			if confirm.CanAsk(req.Session) {
				return &mcp.CallToolResult{
					InputRequests: mcp.InputRequestMap{
						confirm.ApprovalID: confirm.ApprovalRequest("restore this backup, losing everything it does not contain",
							restoreConsequences(archive)),
					},
					// Echoed back by the client on the retry. It is a
					// breadcrumb for a reader of the traffic, NOT a fact this
					// handler trusts: every check above re-runs on the retry
					// against the arguments, so forging it buys nothing.
					RequestState: "restore_backup:" + name,
				}, restoreBackupOutput{}, nil
			}
			// Nobody to ask. Proceed on the token alone and say so, rather
			// than failing on every client that has not implemented
			// elicitation.
			return runRestore(ctx, deps, name, tokenArgs, token, confirm.NotAsked)
		}

		// The answer came back. If it was a browser approval, the client's
		// response only says "I showed them the URL" -- the real decision is
		// the one the person clicked, which is waiting server-side.
		if d, waitErr, viaBrowser := awaitApproval(ctx, deps, "restore_backup", name); viaBrowser {
			if d != confirm.Approved {
				return nil, restoreBackupOutput{
					Confirmed:     false,
					Name:          name,
					HumanApproval: confirm.Refused.String(),
					Note:          approvalRefusal("restore_backup", waitErr),
				}, nil
			}
			return runRestore(ctx, deps, name, tokenArgs, token, confirm.Approved)
		}

		switch confirm.DecisionFrom(answer) {
		case confirm.Approved:
			return runRestore(ctx, deps, name, tokenArgs, token, confirm.Approved)
		default:
			// The token is left unspent: nothing happened, and the user may
			// well say yes to a different archive in a moment.
			return nil, restoreBackupOutput{
				Confirmed:     false,
				Name:          name,
				HumanApproval: confirm.Refused.String(),
				Note: "the user was asked and did NOT approve, so nothing was restored or deleted. " +
					"Do not retry this without being told to; ask them what they want instead",
			}, nil
		}
	})
}

// runRestore redeems the token and performs the restore. It is the only path
// that spends a token, and the only path that writes.
func runRestore(ctx context.Context, deps Deps, name string, tokenArgs restoreBackupInput, token string, approval confirm.Decision) (*mcp.CallToolResult, restoreBackupOutput, error) {
	if err := deps.Confirm.Redeem(token, "restore_backup", tokenArgs); err != nil {
		return nil, restoreBackupOutput{}, err
	}

	result, restoreErr := deps.Restores.FromArchive(ctx, name)
	if restoreErr != nil {
		return nil, restoreBackupOutput{}, fmt.Errorf("%s (the confirmation token has been spent either way; "+
			"call restore_backup with the name alone to preview again and get a new one): %w",
			restoreDamageReport(restoreErr), restoreErr)
	}

	out := restoreBackupOutput{
		Confirmed:        true,
		Name:             name,
		HumanApproval:    approval.String(),
		Restored:         result.Restored,
		Pruned:           result.Pruned,
		SkippedProtected: result.SkippedProtected,
		PruneFailures:    result.PruneFailures,
		Note: fmt.Sprintf("restored %d files from %s and deleted %d that the archive did not contain; "+
			"the data as it was a moment ago is in the safety snapshot this restore took, which is now the "+
			"newest entry in list_backups. Any open browser tab still shows the old data until reloaded",
			result.Restored, name, result.Pruned),
	}
	if result.PruneFailures > 0 {
		out.Note += fmt.Sprintf(". %d stale file(s) could not be deleted, so some data the archive did not "+
			"contain is still on disk; the server log has the details", result.PruneFailures)
	}
	if approval == confirm.NotAsked {
		out.Note += ". NO HUMAN WAS ASKED: this client cannot prompt anyone, so the confirmation token alone " +
			"authorized this. Tell the user plainly what was just overwritten"
	}
	return nil, out, nil
}

// findArchive resolves name against the archives actually on disk, so a
// preview is only ever minted for something restorable. Returning the archive
// (rather than a bool) is what lets the preview quote its real timestamp and
// size instead of echoing the name back.
func findArchive(deps Deps, name string) (backupsvc.Archive, error) {
	archives, err := deps.Backups.List()
	if err != nil {
		return backupsvc.Archive{}, fmt.Errorf("the backup directory could not be listed: %w", err)
	}
	for _, a := range archives {
		if a.Name == name {
			return a, nil
		}
	}
	if len(archives) == 0 {
		return backupsvc.Archive{}, fmt.Errorf("there are no backup archives in %s, so there is nothing to restore", deps.Backups.BackupDir())
	}
	return backupsvc.Archive{}, fmt.Errorf("no backup archive named %q is in %s; call list_backups for the %d that are there and use a name verbatim",
		name, deps.Backups.BackupDir(), len(archives))
}

// restoreDamageReport says whether the user's data can have changed. The
// restore service validates the whole archive, and takes its safety snapshot,
// before it writes anything -- so every failure except a write failure leaves
// the data directory exactly as it was. Saying "nothing changed" for all of
// them would be a lie in the one case that matters most.
func restoreDamageReport(err error) string {
	if errors.Is(err, restore.ErrWriteFailed) {
		return "the restore FAILED PART WAY THROUGH and the data directory may be partly rewritten; " +
			"the safety snapshot taken just before it is the newest archive in list_backups"
	}
	if errors.Is(err, backupsvc.ErrSnapshotInProgress) {
		return "the restore did not start because a backup was already running, so nothing was changed; " +
			"wait for it to finish and try again"
	}
	return "the restore did not start and nothing was changed"
}
