// Package merchants provides pure, side-effect-free functions for
// normalizing transaction descriptions into merchant keys and grouping
// those keys (and the transactions that carry them) into merchant
// groups. It has no I/O and no package-level mutable state; every
// function is deterministic given its inputs.
//
// # Normalization
//
// Normalize uppercases a description, fuses standalone "&" tokens with
// their neighbors (Ruling 2026-08-12c, see fuseAmpersands), drops any
// standalone token that contains no letters at all — all-digit tokens
// (order/terminal numbers), punctuation-only tokens (e.g. a standalone
// "*"), and digit/punctuation mixes such as a check number like
// "#996581" (Ruling 2026-08-12b: generalizes the two earlier drop rules
// into one letter test; without it, paper-check rows like "CHECK
// #996581" each keep a unique trailing token and never group together,
// and a bare processor marker like "SQ *" could combine with a
// punctuation-only token to form a 2-token degenerate key that bridges
// unrelated merchants) — collapses whitespace, and trims the result.
//
// Fusion of standalone "&" tokens with neighbors on both sides runs
// before the letterless drop (Ruling 2026-08-12c): "H & M" becomes the
// single token "H&M", matching the no-spaces spelling of the same
// brand. Fusion additionally requires both neighbor tokens to contain a
// letter (Ruling 2026-08-12d), so "H & 123" normalizes like its "H 123"
// sibling and spaced/unspaced double-ampersands ("A & & B" / "A && B")
// normalize identically. This closes a two-letter brand bridge —
// without fusion, "H & M" would normalize (via the letterless drop
// alone) to the 2-token key {H, M}, which could subset-merge into any
// unrelated key containing both a standalone "H" token and a standalone
// "M" token elsewhere. See fuseAmpersands for the token-level algorithm.
//
// # Merge rule and the degenerate-key guard
//
// Two normalized keys are considered the same merchant when one key's
// token set is a subset of the other's, provided the smaller of the two
// token sets has at least two tokens. This "token-subset" rule lets
// "ACME COFFEE" merge with "ACME COFFEE STORE" and "ACME COFFEE STORE
// #12", capturing the common pattern where point-of-sale systems append
// store numbers, city names, or channel markers to a stable merchant
// name.
//
// Without a floor on the smaller set's size, the rule collapses under
// transitive union-find: a single-token key like "SQ" (a common Square
// payment-processor prefix) or an empty key (produced by an all-digit
// description that normalizes to "") is a subset of practically every
// other key, so it would silently bridge together every merchant that
// happens to share that processor, merging otherwise-unrelated vendors
// into one group. This was observed and ruled against in the b2
// analytics build: "SQ *COFFEE SHOP A" and "SQ *COFFEE SHOP B" must stay
// separate merchants, and a bare "SQ" row must not drag every
// Square-processed merchant into one group.
//
// The guard: keys with zero or one token (degenerate keys) only merge
// with other keys that are EXACTLY equal. A rich key (two or more
// tokens) can still absorb a degenerate key's transactions only via
// exact string equality, never via subset containment. This keeps
// bare-prefix and empty keys isolated to their own group unless another
// transaction shares that exact degenerate key.
package merchants

import (
	"strings"
	"unicode"

	"budget2/internal/models"
)

// Normalize converts a raw transaction description into a normalized
// merchant key: uppercase, standalone "&" tokens with a lettered
// neighbor on both sides fused into one token (e.g. "H & M" -> "H&M" —
// Ruling 2026-08-12c/d, see fuseAmpersands), standalone letterless tokens
// removed (any token containing no letter at all — all-digit
// order/terminal numbers like "123", punctuation-only tokens like "*"
// or "#" (including an unfused "&"), and digit/punctuation mixes like a
// check number "#996581" — Ruling 2026-08-12b), whitespace collapsed to
// single spaces, and the result trimmed. Digits and punctuation
// embedded in an alphanumeric token (e.g. "7-ELEVEN", "AMZN *12-34" ->
// "AMZN") are left alone since the token as a whole carries a letter
// and is not standalone numeric or punctuation noise.
func Normalize(description string) string {
	upper := strings.ToUpper(description)
	fields := strings.Fields(upper)
	fields = fuseAmpersands(fields)
	kept := make([]string, 0, len(fields))
	for _, tok := range fields {
		if hasNoLetters(tok) {
			continue
		}
		kept = append(kept, tok)
	}
	return strings.Join(kept, " ")
}

// fuseAmpersands scans tokens left to right and fuses every standalone
// "&" token that has a neighbor token on both sides into one token:
// left + "&" + right, with no spaces (Ruling 2026-08-12c) — but ONLY
// when BOTH the preceding output token and the following raw token
// contain at least one letter (Ruling 2026-08-12d). A "&" that fails
// this condition (either neighbor is letterless, e.g. "H & 123" or
// "A & & B") is left standalone here and removed by the later
// letterless drop like any other punctuation-only token. This keeps
// "H & 123" consistent with its "H 123" sibling spelling (both reduce
// to "H") and makes spaced ("A & & B") and unspaced ("A && B") runs of
// ampersands normalize identically (both reduce to "A B").
//
// Processing is strictly left to right and each fusion consumes its
// right-hand neighbor, so a run like "A & B & C" fuses in two steps —
// "A&B" then "A&B&C" — into a single token, matching the no-spaces
// spelling "A&B&C" a source system might use for the same merchant.
//
// This closes the two-letter brand bridge from the pre-fusion behavior:
// "H & M" used to normalize (via the letterless drop alone) to the
// 2-token key {H, M}, which could subset-merge into any unrelated key
// that happened to contain both a standalone "H" token and a standalone
// "M" token. Fusing "H & M" into the single token "H&M" makes it a
// 1-token degenerate key, which the merge guard in shouldMerge only
// merges on exact equality — never via subset containment — closing the
// bridge.
func fuseAmpersands(fields []string) []string {
	out := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		tok := fields[i]
		if tok == "&" && len(out) > 0 && i+1 < len(fields) &&
			!hasNoLetters(out[len(out)-1]) && !hasNoLetters(fields[i+1]) {
			out[len(out)-1] = out[len(out)-1] + "&" + fields[i+1]
			i++ // right neighbor consumed into the fused token
			continue
		}
		out = append(out, tok)
	}
	return out
}

// hasNoLetters reports whether s is non-empty and contains no
// unicode.IsLetter rune. Such a token — whether all-digit ("123"),
// punctuation-only ("*", "#"), or a digit/punctuation mix ("#996581")
// — carries no merchant-identifying information on its own and is
// dropped by Normalize (Ruling 2026-08-12b, generalizes the earlier
// separate all-digit and punctuation-only checks into one rule so that
// tokens like paper-check numbers collapse the same way).
func hasNoLetters(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// tokenSet returns the distinct tokens of a normalized key as a set.
func tokenSet(key string) map[string]struct{} {
	fields := strings.Fields(key)
	set := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		set[f] = struct{}{}
	}
	return set
}

// isSubset reports whether every token in a is present in b.
func isSubset(a, b map[string]struct{}) bool {
	for tok := range a {
		if _, ok := b[tok]; !ok {
			return false
		}
	}
	return true
}

// GroupKeys clusters normalized merchant keys into groups using the
// token-subset merge rule with the degenerate-key guard documented in
// the package comment, implemented via union-find over the distinct
// input keys. It returns a map from every input key (duplicates
// included, each mapping to the same value) to its group's canonical
// key.
//
// The canonical key for a group is chosen deterministically: the member
// with the most tokens wins; ties are broken lexicographically
// (smallest string wins) so the result does not depend on map iteration
// or input ordering.
func GroupKeys(keys []string) map[string]string {
	// Distinct keys, in first-seen order (order does not affect the
	// resulting grouping, only affects nothing observable since the
	// algorithm considers every pair regardless of order).
	seen := make(map[string]bool)
	unique := make([]string, 0, len(keys))
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			unique = append(unique, k)
		}
	}

	n := len(unique)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(i, j int) {
		ri, rj := find(i), find(j)
		if ri != rj {
			parent[ri] = rj
		}
	}

	tokens := make([]map[string]struct{}, n)
	sizes := make([]int, n)
	for i, k := range unique {
		ts := tokenSet(k)
		tokens[i] = ts
		sizes[i] = len(ts)
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if shouldMerge(unique[i], unique[j], tokens[i], tokens[j], sizes[i], sizes[j]) {
				union(i, j)
			}
		}
	}

	// Determine canonical key per root: most tokens wins, ties broken
	// lexicographically.
	canonByRoot := make(map[int]string)
	sizeByRoot := make(map[int]int)
	for i := 0; i < n; i++ {
		r := find(i)
		cur, ok := canonByRoot[r]
		if !ok || sizes[i] > sizeByRoot[r] || (sizes[i] == sizeByRoot[r] && unique[i] < cur) {
			canonByRoot[r] = unique[i]
			sizeByRoot[r] = sizes[i]
		}
	}

	result := make(map[string]string, len(unique))
	for i, k := range unique {
		result[k] = canonByRoot[find(i)]
	}
	return result
}

// shouldMerge applies the token-subset rule with the degenerate-key
// guard to decide whether keys a and b belong to the same merchant
// group.
func shouldMerge(a, b string, tokensA, tokensB map[string]struct{}, sizeA, sizeB int) bool {
	if a == b {
		return true
	}
	smaller := sizeA
	if sizeB < smaller {
		smaller = sizeB
	}
	if smaller < 2 {
		// Degenerate key on the smaller side: exact equality only,
		// already checked above and failed, so no merge.
		return false
	}
	if sizeA <= sizeB {
		return isSubset(tokensA, tokensB)
	}
	return isSubset(tokensB, tokensA)
}

// DisplayLabel picks a human-readable label for a merchant group: the
// most frequent raw DisplayName-or-Description among its transactions
// (mirroring GroupTransactions' own key precedence), lowercased and
// trimmed. Ties are broken by first occurrence in group so the result is
// deterministic regardless of map iteration order.
//
// This MUST be used instead of a group's canonical normalized key
// (from GroupKeys/GroupTransactions) wherever a group's identity is
// shown to a user: that canonical key is uppercase-normalized purely
// for matching and would look like a bug if surfaced directly.
func DisplayLabel(group []models.Transaction) string {
	counts := make(map[string]int, len(group))
	firstSeen := make(map[string]int, len(group))
	for i, t := range group {
		raw := t.DisplayName
		if raw == "" {
			raw = t.Description
		}
		if _, ok := firstSeen[raw]; !ok {
			firstSeen[raw] = i
		}
		counts[raw]++
	}

	best := ""
	bestCount := -1
	bestSeen := -1
	for raw, c := range counts {
		seen := firstSeen[raw]
		if c > bestCount || (c == bestCount && seen < bestSeen) {
			best = raw
			bestCount = c
			bestSeen = seen
		}
	}
	return strings.ToLower(strings.TrimSpace(best))
}

// GroupTransactions groups transactions into merchant groups. The raw
// merchant key for each transaction is its DisplayName if non-empty,
// otherwise its Description; the key is normalized via Normalize and
// then clustered via GroupKeys. Transactions are returned under their
// group's canonical normalized key, preserving input order within each
// group.
//
// GroupTransactions does not filter or skip any input transaction
// (including Suppressed ones) — callers that need to exclude suppressed
// transactions from aggregation should call TransactionSet.Active()
// before passing transactions in.
func GroupTransactions(ts []models.Transaction) map[string][]models.Transaction {
	if len(ts) == 0 {
		return map[string][]models.Transaction{}
	}

	rawKeys := make([]string, len(ts))
	for i, t := range ts {
		raw := t.DisplayName
		if raw == "" {
			raw = t.Description
		}
		rawKeys[i] = Normalize(raw)
	}

	canon := GroupKeys(rawKeys)

	result := make(map[string][]models.Transaction)
	for i, t := range ts {
		key := canon[rawKeys[i]]
		result[key] = append(result[key], t)
	}
	return result
}
