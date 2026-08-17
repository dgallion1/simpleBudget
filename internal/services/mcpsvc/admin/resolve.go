package admin

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"budget2/internal/services/dataloader"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type resolveInput struct {
	PairKey        string `json:"pair_key" jsonschema:"the pair_key from list_duplicates"`
	Outcome        string `json:"outcome" jsonschema:"kept_winner to keep one side and exclude the other from every total, or kept_both to declare them two genuinely separate payments"`
	KeptHash       string `json:"kept_hash,omitempty" jsonschema:"required for kept_winner: the hash of the side to KEEP, from list_duplicates"`
	SuppressedHash string `json:"suppressed_hash,omitempty" jsonschema:"required for kept_winner: the hash of the side to EXCLUDE, from list_duplicates"`
}

type resolveOutput struct {
	PairKey             string   `json:"pair_key"`
	Outcome             string   `json:"outcome"`
	SuppressedHash      string   `json:"suppressed_hash,omitempty" jsonschema:"for kept_winner, the stored identity of the suppressed side as it is persisted in duplicate_decisions.json; SaveDuplicateDecision rekeys the posted hash to the row's StableID, so this may be a StableID rather than the legacy content hash list_duplicates reports"`
	UnresolvedRemaining int      `json:"unresolved_remaining"`
	SnapshotPaths       []string `json:"snapshot_paths,omitempty"`
	Note                string   `json:"note,omitempty"`
}

// findPair locates a candidate pair by key.
func findPair(pairs []dataloader.DuplicatePair, key string) (dataloader.DuplicatePair, bool) {
	for _, p := range pairs {
		if p.Key == key {
			return p, true
		}
	}
	return dataloader.DuplicatePair{}, false
}

// availableKeys renders the currently-resolvable keys for an error message, so
// a model handed a stale or invented key can correct itself in one turn.
func availableKeys(pairs []dataloader.DuplicatePair) string {
	if len(pairs) == 0 {
		return "no pairs are currently awaiting review"
	}
	keys := make([]string, 0, len(pairs))
	for _, p := range pairs {
		keys = append(keys, p.Key)
	}
	return "currently awaiting review: " + strings.Join(keys, ", ")
}

// ensureDecisionsSnapshot copies duplicate_decisions.json aside before a
// write. A file that does not exist yet is not an error -- a first decision
// on a fresh install has nothing to lose -- but any OTHER failure aborts,
// because it is not evidence the file is absent and overwriting it would be
// unrecoverable. This mirrors curate/delete.go.
func ensureDecisionsSnapshot(deps Deps) (paths []string, note string, err error) {
	if deps.Snapshots == nil {
		return nil, "", fmt.Errorf("refusing to write: no snapshot directory is configured on this server")
	}
	p, err := deps.Snapshots.Ensure(duplicateDecisionsFile, time.Now())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "no .bak was taken: " + duplicateDecisionsFile + " did not exist yet, so there was no prior state to protect", nil
		}
		return nil, "", fmt.Errorf("refusing to write: could not back up %s: %w", duplicateDecisionsFile, err)
	}
	return []string{p}, "", nil
}

func registerResolve(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "resolve_duplicates",
		Description: "Settle one pair from list_duplicates. THIS WRITES TO THE USER'S DATA. Two outcomes: " +
			"kept_winner means the two rows are the same payment recorded twice -- the suppressed_hash side is " +
			"then EXCLUDED from every spending total, trend and analysis app-wide, though the row itself is " +
			"never deleted from the CSV; kept_both means they are genuinely two separate payments that happen " +
			"to match, and both stay counted, with the pair no longer re-flagged. kept_winner requires both " +
			"kept_hash and suppressed_hash and both must belong to THIS pair -- a hash from anywhere else is " +
			"refused, not written. kept_both ignores the hashes. A pair_key that is not currently awaiting " +
			"review is refused with the list of keys that are, so do not guess one: call list_duplicates first. " +
			"undo_resolve reverses this exactly. duplicate_decisions.json is copied to a .bak before the " +
			"first change of this session that has a file to copy: on a fresh install with no decisions file " +
			"yet there is nothing to back up, so none is taken and snapshot_paths comes back empty -- the NEXT " +
			"change is then the one that gets the .bak. Once a .bak exists for this session every later call " +
			"reuses it, reporting the same snapshot_paths, so those later changes are not separately " +
			"recoverable. An already-open Duplicates page does NOT refresh itself -- it shows stale data " +
			"until reloaded.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in resolveInput) (res *mcp.CallToolResult, out resolveOutput, err error) {
		defer recoverToError("resolve_duplicates", &err)

		key := strings.TrimSpace(in.PairKey)
		if key == "" {
			return nil, resolveOutput{}, fmt.Errorf("pair_key is required; call list_duplicates for the current keys")
		}
		if deps.Duplicates == nil || deps.Decisions == nil {
			return nil, resolveOutput{}, fmt.Errorf("no data loader is configured on this server")
		}

		outcome := strings.TrimSpace(in.Outcome)
		switch outcome {
		case dataloader.DuplicateOutcomeKeptWinner, dataloader.DuplicateOutcomeKeptBoth:
		default:
			return nil, resolveOutput{}, fmt.Errorf(
				"outcome %q is not one this app understands; use %q or %q",
				in.Outcome, dataloader.DuplicateOutcomeKeptWinner, dataloader.DuplicateOutcomeKeptBoth)
		}

		// Refresh detection, then validate the key against what is actually
		// pending. Writing a decision for a key no pair carries would leave a
		// dead entry the user can never see or clear from the page.
		if _, err := deps.load(); err != nil {
			return nil, resolveOutput{}, err
		}
		pending := deps.Duplicates.UnresolvedDuplicates()
		pair, ok := findPair(pending, key)
		if !ok {
			return nil, resolveOutput{}, fmt.Errorf(
				"pair_key %q is not awaiting review (it may already be resolved, or it may not exist); %s",
				key, availableKeys(pending))
		}

		decision := dataloader.DuplicateDecision{Outcome: outcome}
		if outcome == dataloader.DuplicateOutcomeKeptWinner {
			kept := strings.TrimSpace(in.KeptHash)
			suppressed := strings.TrimSpace(in.SuppressedHash)
			if kept == "" || suppressed == "" {
				return nil, resolveOutput{}, fmt.Errorf(
					"kept_winner requires both kept_hash and suppressed_hash; this pair's sides are %s and %s",
					pair.Left.Hash, pair.Right.Hash)
			}
			if kept == suppressed {
				return nil, resolveOutput{}, fmt.Errorf(
					"kept_hash and suppressed_hash are the same transaction (%s); they must be the two different sides of the pair", kept)
			}
			for _, h := range []string{kept, suppressed} {
				if h != pair.Left.Hash && h != pair.Right.Hash {
					return nil, resolveOutput{}, fmt.Errorf(
						"hash %q does not belong to pair %s; its two sides are %s and %s",
						h, key, pair.Left.Hash, pair.Right.Hash)
				}
			}
			decision.KeptHash = kept
			decision.SuppressedHash = suppressed
		}

		paths, note, err := ensureDecisionsSnapshot(deps)
		if err != nil {
			return nil, resolveOutput{}, err
		}

		if err := deps.Decisions.SaveDuplicateDecision(key, decision); err != nil {
			return nil, resolveOutput{}, err
		}

		// The save succeeded, so the decision is on disk. Report the identity
		// ACTUALLY persisted -- SaveDuplicateDecision rekeys the posted hash
		// to the row's StableID on the way in, and takes the decision by value
		// so `decision` here is still the untouched posted form. Echoing that
		// would mis-document a StableID as a legacy content hash.
		reportedSuppressed := decision.SuppressedHash
		if stored, rerr := deps.Decisions.LoadDuplicateDecisions(); rerr != nil {
			// The save itself succeeded; a read failure must not turn a
			// success into a failure. Fall back to the posted hash and note
			// the gap so a model does not treat it as the persisted value.
			if note == "" {
				note = "suppressed_hash is the posted value, not the persisted one: the post-save re-read failed: " + rerr.Error()
			} else {
				note += "; suppressed_hash is the posted value, not the persisted one: the post-save re-read failed: " + rerr.Error()
			}
		} else if sd, ok := stored[key]; ok {
			reportedSuppressed = sd.SuppressedHash
		} else {
			// The key is absent from the re-read despite a successful save.
			// SaveDuplicateDecision does not rekey the map KEY on its own
			// (that pass lives in writeDecisionsLocked, which SaveDuplicateDecision
			// also calls), so absence here would be a genuine surprise; treat
			// it like the read-failure branch rather than claiming a value we
			// cannot verify.
			if note == "" {
				note = "suppressed_hash is the posted value, not the persisted one: the saved decision was not found under the posted pair_key on re-read"
			} else {
				note += "; suppressed_hash is the posted value, not the persisted one: the saved decision was not found under the posted pair_key on re-read"
			}
		}

		out = resolveOutput{
			PairKey:        key,
			Outcome:        outcome,
			SuppressedHash: reportedSuppressed,
			SnapshotPaths:  paths,
			Note:           note,
		}
		// Re-load so the reported remainder reflects the decision just made.
		// A failed reload must NOT leave UnresolvedRemaining at its zero
		// value silently: a model reading that as "nothing left to resolve"
		// would stop looping early when the truth is just "unknown".
		if _, err := deps.load(); err != nil {
			reloadNote := "unresolved_remaining could not be recomputed after the write: " + err.Error()
			if out.Note == "" {
				out.Note = reloadNote
			} else {
				out.Note += "; " + reloadNote
			}
		} else {
			out.UnresolvedRemaining = deps.Duplicates.UnresolvedDuplicateCount()
		}
		return nil, out, nil
	})
}
