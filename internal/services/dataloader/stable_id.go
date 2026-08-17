package dataloader

import (
	"path/filepath"

	"budget2/internal/models"
)

// unassignedAccountPrefix is what fills the accountID slot of a StableID for a
// row whose CSV matched no account. Usable, but not durable across a file
// rename -- which is why the UI nudges toward assigning the file first.
const unassignedAccountPrefix = "file:"

// stableAccountSlot returns the accountID component of a row's StableID.
func stableAccountSlot(t models.Transaction) string {
	if t.AccountID != "" {
		return t.AccountID
	}
	return unassignedAccountPrefix + filepath.Base(t.SourceFile)
}

// identityKey is the key a transaction's decisions should be stored under:
// its StableID, or its legacy content Hash for a row that has none (a
// hand-built transaction in a unit test, or one that never went through
// assignStableIDs).
func identityKey(t models.Transaction) string {
	if t.StableID != "" {
		return t.StableID
	}
	return t.Hash
}

// identityMatchesSuppressed reports whether key identifies the side of a
// pair that a kept_winner decision suppressed. It is the equality analogue of
// idxByIdentity: a stored decision's SuppressedHash may be either a StableID
// (the form SaveDuplicateDecision rekeys to via canonicalKey) or a legacy
// content hash (a decision written before StableID, or one whose row is
// outside the loaded set), and the row being compared carries a legacy Hash
// plus, usually, a StableID. Matching either form is what keeps the
// Left = kept / Right = suppressed role swap correct for both on-disk shapes.
func identityMatchesSuppressed(t models.Transaction, key string) bool {
	if key == "" {
		return false
	}
	if t.StableID != "" && key == t.StableID {
		return true
	}
	return key == t.Hash
}

// assignStableIDs stamps StableID on every row in slice order -- which is file
// order, because LoadDataContext appends whole files at a time -- and returns
// the legacy Hash -> StableID index for those same rows.
//
// It runs after the sign flip and account attribution (both per file, in
// loadCSVFileForAccount) and before dedup, so the amount it encodes is the
// post-flip one and a row's occurrence index does not move when a later stage
// drops rows.
//
// The index maps FIRST occurrence wins: two rows with the same content Hash
// are exact duplicates, and dedup keeps the first, so pointing the legacy hash
// at the second row's StableID would rekey a pin onto a row that is about to
// disappear.
func assignStableIDs(txns []models.Transaction) map[string]string {
	occurrences := make(map[string]int, len(txns))
	index := make(map[string]string, len(txns))
	for i := range txns {
		t := &txns[i]
		slot := stableAccountSlot(*t)
		cents := models.AmountCents(t.Amount)
		// The n=0 form doubles as the occurrence bucket key: it is
		// exactly the three components rows must share to collide.
		bucket := models.StableIDFor(slot, t.Date, cents, 0)
		n := occurrences[bucket]
		occurrences[bucket] = n + 1
		t.StableID = models.StableIDFor(slot, t.Date, cents, n)
		if t.Hash != "" {
			if _, seen := index[t.Hash]; !seen {
				index[t.Hash] = t.StableID
			}
		}
	}
	return index
}

// setStableIndex publishes the index the load that just finished produced.
// Every LoadDataContext exit path goes through it, so a load that finds no
// files clears the previous load's index instead of leaving it stale.
func (dl *DataLoader) setStableIndex(index map[string]string) {
	dl.stateMu.Lock()
	dl.stableByHash = index
	dl.stateMu.Unlock()
}

// stableIDIndex returns the most recent load's legacy Hash -> StableID index.
// The map is built fresh by each load and never mutated after publication, so
// the returned value is safe to read without holding stateMu.
//
// Lock order: sidecar sequences take writeMu (via beginWrite) and then call
// this, which takes stateMu. Nothing takes writeMu while holding stateMu, so
// the two never invert.
func (dl *DataLoader) stableIDIndex() map[string]string {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	return dl.stableByHash
}

// stablePairKeyIndex returns the most recent load's legacy pair key ->
// StableID-derived pair key index. Like stableIDIndex, the map is published
// once per load and never mutated afterwards.
func (dl *DataLoader) stablePairKeyIndex() map[string]string {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	return dl.stablePairKeys
}

// legacyPairKeysFor returns the pre-StableID pair keys that alias key in the
// most recent load. A caller deleting a decision has to delete these too:
// the UI hands back the current key, but the entry on disk may still be
// filed under the key the pair had before StableID existed, and leaving it
// would resurrect the decision on the next load.
func (dl *DataLoader) legacyPairKeysFor(key string) []string {
	var out []string
	for legacy, stable := range dl.stablePairKeyIndex() {
		if stable == key && legacy != key {
			out = append(out, legacy)
		}
	}
	return out
}

// canonicalKey maps a caller-supplied identity key to the StableID of the row
// it names. Callers still speak legacy hashes -- the explorer renders
// Transaction.Hash and the pin/unpin routes echo it back -- so normalizing on
// the way in is what keeps a pin and its later unpin landing on the same entry
// once the store has been rekeyed. A key that is already a StableID, or that
// names no loaded row, comes back unchanged.
func (dl *DataLoader) canonicalKey(key string) string {
	if key == "" {
		return key
	}
	if stable, ok := dl.stableIDIndex()[key]; ok {
		return stable
	}
	return key
}

// rekeyToStable rewrites legacy-Hash keys in an identity-keyed map to the
// StableID of the row each hash belongs to, per the index. It reports whether
// anything moved.
//
// Two keys are deliberately left alone: one that is already a StableID, and a
// legacy one the index does not resolve. The latter is the important case --
// it usually means the row is outside the loaded date range or its file is
// disabled, not that the decision is stale -- so dropping it would silently
// discard a user decision. The legacy dependency decays as rows come back into
// range; it is never cut by force.
//
// When both forms are present for one row the StableID entry wins, matching
// the read precedence in models.ResolveByIdentity.
func rekeyToStable[V any](m map[string]V, index map[string]string) bool {
	if len(m) == 0 || len(index) == 0 {
		return false
	}
	changed := false
	for key, value := range m {
		stable, ok := index[key]
		if !ok || stable == key {
			continue
		}
		delete(m, key)
		changed = true
		if _, taken := m[stable]; taken {
			continue
		}
		m[stable] = value
	}
	return changed
}
