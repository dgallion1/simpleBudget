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

	// pendingPostedWindowDays bounds the third candidate shape (see
	// isPendingPostedPair): the pending side and the posted side of the
	// same settlement are usually captured on the same day or the next,
	// but bank settlement lag occasionally pushes it out a couple more
	// days, so 3 days balances catching real settlements against pairing
	// unrelated same-amount charges.
	pendingPostedWindowDays = 3

	// pendingPostedPrefixMinLen is the minimum shared byte-wise prefix
	// length (after normalization) required for the pending→posted
	// shape's description-affinity check when neither normalized string
	// is a prefix of the other. 12 bytes is enough to require matching
	// on a real merchant-name fragment (not just a shared first word)
	// while tolerating a rewritten tail.
	pendingPostedPrefixMinLen = 12
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

	// scheduledPaidTokenStopwords are dropped from the token-affinity check
	// in isScheduledSettledPair because they're either payment-mechanics
	// noise (pay, pmt, bill, autopay, online, recurring, scheduled, check)
	// or too generic to signal a shared merchant (the, and, for, inc, llc,
	// com, www).
	scheduledPaidTokenStopwords = map[string]bool{
		"the": true, "and": true, "for": true, "inc": true, "llc": true,
		"pay": true, "pmt": true, "pmts": true, "bill": true, "payment": true,
		"autopay": true, "online": true, "recurring": true, "scheduled": true,
		"check": true, "com": true, "www": true,
	}
)

// DuplicatePair is the public-facing detection result. Order of Left
// and Right is stable for a given input but is not otherwise
// meaningful — UI should treat them symmetrically.
type DuplicatePair struct {
	Key   string
	Left  models.Transaction
	Right models.Transaction
}

// detectNearDuplicatePairs scans transactions for near-duplicate candidate
// pairs as defined in spec §2: a scheduled bill pay settling as a posted
// check, a same-day (or next-day) re-export of the same bank row under a
// rewritten Description, or a pending charge settling as a posted charge
// with both Description and Original Description rewritten.
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

// isCandidatePair returns true if (a, b) look like a near-duplicate by
// any of four shapes:
//   - exactly one of (a, b) looks like a scheduled bill pay AND the other
//     looks like a posted check, or
//   - both are a same-day (or next-day) re-export of the same bank row:
//     their Original Description values match once whitespace and case
//     differences are ignored, even though Description itself was
//     rewritten between exports, or
//   - one is a Pending charge and the other is the same charge settled
//     Posted, with both Description and Original Description rewritten
//     by the bank in between (see isPendingPostedPair), or
//   - one is a scheduled bill pay / recurring autopay and the other is the
//     same payment settled Posted as an ordinary ACH/autopay charge rather
//     than a physical check (see isScheduledSettledPair).
//
// Callers only reach this with rows already bucketed by matching sign,
// amount-in-cents, and TransactionType (see detectNearDuplicatePairs).
func isCandidatePair(a, b models.Transaction) bool {
	aBP, aPC := classify(a)
	bBP, bPC := classify(b)
	if (aBP && bPC) || (aPC && bBP) {
		return true
	}
	if isSameDayReimportPair(a, b) {
		return true
	}
	if isPendingPostedPair(a, b) {
		return true
	}
	return isScheduledSettledPair(a, b)
}

// isSameDayReimportPair implements the second candidate shape: a
// transaction that was re-exported with a rewritten Description but an
// unchanged Original Description column. Restricted to a 1-day window
// (tighter than the bill-pay/check shape's duplicateWindowDays) because,
// unlike a bill-pay clearing, this shape has no status signal to lean on
// -- an exact Original Description match plus same amount is the only
// evidence, so the date window stays narrow.
func isSameDayReimportPair(a, b models.Transaction) bool {
	if dayDiff(a.Date, b.Date) > 1 {
		return false
	}
	if a.OriginalDescription == "" || b.OriginalDescription == "" {
		return false
	}
	return normalizeOriginalDescription(a.OriginalDescription) == normalizeOriginalDescription(b.OriginalDescription)
}

// isPendingPostedPair implements the third candidate shape: a card swipe
// captured once while Pending and again after it settles Posted, where the
// bank rewrites BOTH Description and Original Description between the two
// exports (so isSameDayReimportPair's Original Description match can never
// fire). The only remaining signals are the status transition itself, the
// account, and whatever fragment of the merchant name survives the
// rewrite -- so this shape is deliberately narrower than the other two: the
// window is 3 days (pendingPostedWindowDays, wider than the 1-day reimport
// window because bank settlement lag varies more than a same-day
// re-export), the status split must be exact (one side "pending", the
// other a postedStatusKeywords match, neither side's Status empty -- an
// empty Status is ambiguous here in a way it isn't for classify(), because
// there is no description-shape signal like "Check #NNN" to fall back on),
// and the same account is required to avoid pairing coincidental same-
// amount charges on different cards. The affinity check itself is tried
// twice: first on Description, and -- because some banks prettify the
// posted side's Description enough to defeat the prefix rule while leaving
// a whitespace-normalized prefix relationship intact in OriginalDescription
// -- again on OriginalDescription if the first attempt fails.
func isPendingPostedPair(a, b models.Transaction) bool {
	if dayDiff(a.Date, b.Date) > pendingPostedWindowDays {
		return false
	}
	if a.AccountID != b.AccountID {
		return false
	}
	aPending, bPending := isPendingStatus(a.Status), isPendingStatus(b.Status)
	aPosted, bPosted := isPostedStatus(a.Status), isPostedStatus(b.Status)
	if !((aPending && bPosted) || (bPending && aPosted)) {
		return false
	}
	if hasPendingPostedAffinity(a.Description, b.Description) {
		return true
	}
	// Gap A: USAA's posted exports frequently prettify Description (e.g.
	// "BJS WHOLESALE #0075" -> "BJ's Wholesale"), which kills the prefix
	// rule above even though the two rows are the same settlement. The
	// pending side's raw bank text usually survives untouched in
	// OriginalDescription, and the posted side's OriginalDescription
	// carries the same raw text with a location suffix appended, so the
	// same affinity rule applied there still catches the pair.
	return hasPendingPostedAffinity(a.OriginalDescription, b.OriginalDescription)
}

// hasPendingPostedAffinity applies the pending<->posted shape's shared
// description-affinity rule to any two raw strings: normalize both, then
// require one to be a prefix of the other or their common prefix to be at
// least pendingPostedPrefixMinLen bytes. An empty normalized string on
// either side makes the pair ineligible (never matches empty-vs-anything).
func hasPendingPostedAffinity(a, b string) bool {
	na := normalizeOriginalDescription(a)
	nb := normalizeOriginalDescription(b)
	if na == "" || nb == "" {
		return false
	}
	if strings.HasPrefix(na, nb) || strings.HasPrefix(nb, na) {
		return true
	}
	return commonPrefixLen(na, nb) >= pendingPostedPrefixMinLen
}

// isScheduledSettledPair implements the fourth candidate shape: a scheduled
// bill pay (or recurring autopay) that settles as an ordinary ACH/autopay
// posted charge rather than a physical check -- so classify()'s
// checkPrefixRE shape never fires and there is no "pending" status for
// isPendingPostedPair to key on. The only signals available are the status
// transition (one side scheduled, the other posted) and whatever merchant-
// name tokens survive the bank's rewrite between the two exports.
//
// Deliberately excludes anything classify() would already claim (neither
// side's Description may match checkPrefixRE) so check settlements keep
// their existing pair_keys byte-identical.
func isScheduledSettledPair(a, b models.Transaction) bool {
	if dayDiff(a.Date, b.Date) > duplicateWindowDays {
		return false
	}
	if a.AccountID != b.AccountID {
		return false
	}
	aSched, bSched := isScheduledStatus(a.Status), isScheduledStatus(b.Status)
	aPosted, bPosted := isPostedStatus(a.Status), isPostedStatus(b.Status)
	if !((aSched && bPosted) || (bSched && aPosted)) {
		return false
	}
	if checkPrefixRE.MatchString(strings.TrimSpace(a.Description)) ||
		checkPrefixRE.MatchString(strings.TrimSpace(b.Description)) {
		return false
	}
	return sharedTokenCount(a.Description, b.Description) >= 2
}

// isScheduledStatus reports whether a Status value marks the scheduled side
// of isScheduledSettledPair. An empty Status does not qualify.
func isScheduledStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	if s == "" {
		return false
	}
	return strings.Contains(s, "scheduled") || strings.Contains(s, "bill pay")
}

// tokenizeDescription lowercases s, splits on runs of non-alphanumeric
// bytes, and drops tokens shorter than 3 bytes and payment-mechanics
// stopwords, leaving only fragments likely to identify a merchant.
func tokenizeDescription(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) < 3 {
			continue
		}
		if scheduledPaidTokenStopwords[f] {
			continue
		}
		tokens = append(tokens, f)
	}
	return tokens
}

// sharedTokenCount returns the number of distinct tokens (per
// tokenizeDescription) present in both a and b.
func sharedTokenCount(a, b string) int {
	aTokens := make(map[string]bool)
	for _, tok := range tokenizeDescription(a) {
		aTokens[tok] = true
	}
	shared := 0
	seen := make(map[string]bool)
	for _, tok := range tokenizeDescription(b) {
		if aTokens[tok] && !seen[tok] {
			seen[tok] = true
			shared++
		}
	}
	return shared
}

// isPendingStatus reports whether a Status value marks the pending side of
// isPendingPostedPair. An empty Status does not qualify (see that
// function's doc comment).
func isPendingStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s != "" && strings.Contains(s, "pending")
}

// isPostedStatus reports whether a Status value marks the posted side of
// isPendingPostedPair. An empty Status does not qualify, and a status
// containing "pending" never qualifies even if it also contains a posted
// keyword.
func isPostedStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	if s == "" || strings.Contains(s, "pending") {
		return false
	}
	return containsAny(s, postedStatusKeywords)
}

// commonPrefixLen returns the length in bytes of the longest common prefix
// of a and b.
func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

// whitespaceRunRE collapses runs of whitespace to a single space so that
// incidental export-formatting differences (extra padding, tabs) in the
// Original Description column don't defeat the comparison.
var whitespaceRunRE = regexp.MustCompile(`\s+`)

func normalizeOriginalDescription(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return whitespaceRunRE.ReplaceAllString(s, " ")
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
