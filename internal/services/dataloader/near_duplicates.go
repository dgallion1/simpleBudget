package dataloader

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"
)

// Heuristic constants. Hardcoded for v1; spec §9 explicitly defers a
// settings UI until real-world false-positive data warrants tuning.
const (
	duplicateWindowDays = 7
)

// checkPrefixRE matches descriptions that look like a posted check
// reference. Anchored at the start (so it can't match arbitrary text
// containing "check #") but not at the end (so banks that append a
// payee or "cleared" suffix still match). Whitespace around the # is
// tolerated to handle export quirks like "CHECK # 996583".
var checkPrefixRE = regexp.MustCompile(`(?i)^check\s*#\s*\d+\b`)

var (
	billPayStatusKeywords = []string{"scheduled", "pending", "processing", "bill pay"}
	postedStatusKeywords  = []string{"posted", "cleared", "processed"}
)

// DuplicatePair is the public-facing detection result. Order of Left
// and Right is stable for a given input but is not otherwise
// meaningful — UI should treat them symmetrically.
type DuplicatePair struct {
	Key   string
	Left  models.Transaction
	Right models.Transaction
}

// detectNearDuplicatePairs scans transactions for bill-pay → posted-
// check candidate pairs as defined in spec §2.
//
// Pairing is greedy by smallest date difference: a transaction can
// appear in at most one pair, ties broken by lexicographically smaller
// partner hash for determinism.
func detectNearDuplicatePairs(txns []models.Transaction) []DuplicatePair {
	if len(txns) < 2 {
		return nil
	}

	// Index by amount-in-cents → outflow indexes only. Cents avoid
	// float-equality landmines; outflow filter avoids matching income.
	byCents := make(map[int64][]int)
	for i := range txns {
		t := txns[i]
		if t.TransactionType != models.Outflow {
			continue
		}
		if t.Amount >= 0 {
			continue
		}
		cents := int64(math.Round(math.Abs(t.Amount) * 100))
		byCents[cents] = append(byCents[cents], i)
	}

	used := make(map[int]bool)
	var pairs []DuplicatePair

	// Iterate cent buckets in deterministic key order.
	keys := make([]int64, 0, len(byCents))
	for k := range byCents {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	for _, cents := range keys {
		idxs := byCents[cents]
		if len(idxs) < 2 {
			continue
		}
		// For each unused candidate, find the best partner: smallest
		// date diff, then lexicographically smaller partner hash.
		for _, i := range idxs {
			if used[i] {
				continue
			}
			bestJ := -1
			bestDiff := duplicateWindowDays + 1
			for _, j := range idxs {
				if j == i || used[j] {
					continue
				}
				if !isCandidatePair(txns[i], txns[j]) {
					continue
				}
				diff := dayDiff(txns[i].Date, txns[j].Date)
				if diff < 0 || diff > duplicateWindowDays {
					continue
				}
				if diff < bestDiff {
					bestDiff = diff
					bestJ = j
				} else if diff == bestDiff && bestJ >= 0 {
					if txns[j].Hash < txns[bestJ].Hash {
						bestJ = j
					}
				}
			}
			if bestJ < 0 {
				continue
			}
			used[i] = true
			used[bestJ] = true
			pairs = append(pairs, DuplicatePair{
				// Keyed on StableID when the rows have one, so a
				// decision survives a description reformat. Rows
				// without one (unit fixtures) key on Hash exactly as
				// before; applyDuplicateDetection reads the legacy
				// hash-derived key as a fallback either way.
				Key:   pairKey(identityKey(txns[i]), identityKey(txns[bestJ])),
				Left:  txns[i],
				Right: txns[bestJ],
			})
		}
	}
	return pairs
}

// isCandidatePair returns true if exactly one of (a, b) looks like a
// scheduled bill pay AND the other looks like a posted check.
func isCandidatePair(a, b models.Transaction) bool {
	aBP, aPC := classify(a)
	bBP, bPC := classify(b)
	return (aBP && bPC) || (aPC && bBP)
}

func classify(t models.Transaction) (billPay, postedCheck bool) {
	descIsCheck := checkPrefixRE.MatchString(strings.TrimSpace(t.Description))
	statusLower := strings.ToLower(strings.TrimSpace(t.Status))
	statusEmpty := statusLower == ""

	if descIsCheck {
		// Posted-check side: status must be empty or contain a posted
		// keyword. A check description with a "Scheduled" status (rare
		// but possible in bill-pay-with-physical-check exports) is
		// still treated as posted because the description trumps.
		if statusEmpty || containsAny(statusLower, postedStatusKeywords) {
			postedCheck = true
		}
	} else {
		// Bill-pay side: description does NOT look like a check.
		// Status must be empty or contain a bill-pay keyword.
		if statusEmpty || containsAny(statusLower, billPayStatusKeywords) {
			billPay = true
		}
	}
	return
}

func containsAny(s string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

func dayDiff(a, b time.Time) int {
	const day = 24 * 60 * 60
	diff := a.Unix() - b.Unix()
	if diff < 0 {
		diff = -diff
	}
	return int(diff / day)
}

// pairKey is order-independent and deterministic. SHA-256 over
// `min|max` of the two identity keys ensures (A,B) and (B,A) hash
// identically. Callers pass StableIDs for loaded rows and legacy content
// hashes when reconstructing a pre-StableID key.
func pairKey(hashA, hashB string) string {
	lo, hi := hashA, hashB
	if hi < lo {
		lo, hi = hi, lo
	}
	sum := sha256.Sum256([]byte(lo + "|" + hi))
	return hex.EncodeToString(sum[:8])
}
