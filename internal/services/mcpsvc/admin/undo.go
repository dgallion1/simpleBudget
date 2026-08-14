package admin

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type undoInput struct {
	PairKey string `json:"pair_key" jsonschema:"the pair_key of a pair that was already resolved"`
}

type undoOutput struct {
	PairKey             string   `json:"pair_key"`
	PreviousOutcome     string   `json:"previous_outcome"`
	UnresolvedRemaining int      `json:"unresolved_remaining"`
	SnapshotPaths       []string `json:"snapshot_paths,omitempty"`
	Note                string   `json:"note,omitempty"`
}

func registerUndo(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "undo_resolve",
		Description: "Reverse one resolve_duplicates decision, putting the pair back in the review queue. " +
			"THIS WRITES TO THE USER'S DATA. It is the exact inverse: a suppressed transaction becomes live " +
			"again and re-enters every spending total, and the pair is re-flagged for review. This is the app's " +
			"own Undo button, not a general undo -- it reverses a duplicate decision and nothing else. A " +
			"pair_key with no decision recorded against it is refused rather than silently succeeding, so a " +
			"success here always means something actually changed. duplicate_decisions.json is copied to a .bak " +
			"before this session's first change to it when there is prior data on disk to protect; with no " +
			"decisions file yet there is nothing to back up, so none is taken and snapshot_paths comes back " +
			"empty. Later changes in the same session are not separately recoverable. An already-open Duplicates " +
			"page does NOT refresh itself -- it shows stale data until reloaded.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in undoInput) (res *mcp.CallToolResult, out undoOutput, err error) {
		defer recoverToError("undo_resolve", &err)

		key := strings.TrimSpace(in.PairKey)
		if key == "" {
			return nil, undoOutput{}, fmt.Errorf("pair_key is required; call list_duplicates with include_resolved to see what can be undone")
		}
		if deps.Decisions == nil {
			return nil, undoOutput{}, fmt.Errorf("no data loader is configured on this server")
		}

		// ClearDuplicateDecision is a silent no-op for an unknown key, which
		// would let this tool claim it undid something it did not.
		decisions, err := deps.Decisions.LoadDuplicateDecisions()
		if err != nil {
			return nil, undoOutput{}, err
		}
		prior, ok := decisions[key]
		if !ok {
			return nil, undoOutput{}, fmt.Errorf(
				"pair_key %q has no decision recorded against it, so there is nothing to undo; call list_duplicates with include_resolved to see what does", key)
		}

		paths, note, err := ensureDecisionsSnapshot(deps)
		if err != nil {
			return nil, undoOutput{}, err
		}

		if err := deps.Decisions.ClearDuplicateDecision(key); err != nil {
			return nil, undoOutput{}, err
		}

		out = undoOutput{
			PairKey:         key,
			PreviousOutcome: prior.Outcome,
			SnapshotPaths:   paths,
			Note:            note,
		}
		if _, err := deps.load(); err == nil && deps.Duplicates != nil {
			out.UnresolvedRemaining = deps.Duplicates.UnresolvedDuplicateCount()
		}
		return nil, out, nil
	})
}
