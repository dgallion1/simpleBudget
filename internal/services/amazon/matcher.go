package amazon

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"
)

// MatchOptions tunes matching behavior. A zero value picks safe defaults
// (5-day window, 1-cent amount tolerance).
type MatchOptions struct {
	WindowDays      int     // default 5
	AmountTolerance float64 // default 0.01
	MaxLabelLen     int     // default 80
}

// MatchResult is one tx-to-shipment binding.
type MatchResult struct {
	TxHash   string
	Label    string
	OrderIDs []string
}

// Match links bank transactions to Amazon shipments and returns a label
// for each matched transaction. Transactions whose description contains
// neither "amazon" nor "amzn" are ignored — we only enrich Amazon
// charges, not random merchants. Each shipment is consumed by at most
// one transaction so multi-charge orders don't double-attribute.
func Match(transactions []models.Transaction, shipments []Shipment, opts MatchOptions) []MatchResult {
	if opts.WindowDays <= 0 {
		opts.WindowDays = 5
	}
	if opts.AmountTolerance <= 0 {
		opts.AmountTolerance = 0.01
	}
	if opts.MaxLabelLen <= 0 {
		opts.MaxLabelLen = 80
	}

	candidates := make([]int, 0, len(transactions))
	for i, t := range transactions {
		if isAmazonDesc(t.Description) {
			candidates = append(candidates, i)
		}
	}

	// Process oldest-first so earlier charges claim earlier shipments —
	// this makes matching deterministic when several txns could match
	// the same shipment.
	sort.Slice(candidates, func(a, b int) bool {
		return transactions[candidates[a]].Date.Before(transactions[candidates[b]].Date)
	})

	consumed := make([]bool, len(shipments))
	out := make([]MatchResult, 0, len(candidates))

	for _, idx := range candidates {
		tx := transactions[idx]
		amt := math.Abs(tx.Amount)

		ship, multi := findMatch(tx.Date, amt, shipments, consumed, opts)
		if len(ship) == 0 {
			continue
		}
		for _, s := range ship {
			consumed[s] = true
		}
		out = append(out, MatchResult{
			TxHash:   tx.Hash,
			Label:    formatLabel(collectShipments(shipments, ship), opts.MaxLabelLen, multi),
			OrderIDs: uniqOrderIDs(shipments, ship),
		})
	}

	return out
}

// findMatch picks shipment indices that explain a transaction. It
// returns indices into the shipments slice plus a "multi" flag noting
// whether multiple shipments were summed.
//
// Three strategies, tried in order:
//  1. Exact single-shipment amount match within the date window. If
//     more than one candidate ties, we refuse to guess and return none.
//  2. Sum across shipments sharing one Order ID (split shipments
//     billed as one bank charge).
//  3. Order ID substring appearing in the bank description.
func findMatch(txDate time.Time, amt float64, shipments []Shipment, consumed []bool, opts MatchOptions) ([]int, bool) {
	window := time.Duration(opts.WindowDays) * 24 * time.Hour
	tol := opts.AmountTolerance

	var single []int
	for i, s := range shipments {
		if consumed[i] {
			continue
		}
		if absDuration(txDate.Sub(s.ShipDate)) > window {
			continue
		}
		if math.Abs(s.Total-amt) <= tol {
			single = append(single, i)
		}
	}
	if len(single) == 1 {
		return single, false
	}
	if len(single) > 1 {
		// Ambiguous on amount alone; skip rather than mis-attribute.
		return nil, false
	}

	// Strategy 2: sum within one Order ID across shipments in window.
	byOrder := map[string][]int{}
	for i, s := range shipments {
		if consumed[i] {
			continue
		}
		if absDuration(txDate.Sub(s.ShipDate)) > window {
			continue
		}
		byOrder[s.OrderID] = append(byOrder[s.OrderID], i)
	}
	for _, idxs := range byOrder {
		if len(idxs) < 2 {
			continue
		}
		var sum float64
		for _, i := range idxs {
			sum += shipments[i].Total
		}
		if math.Abs(sum-amt) <= tol {
			return idxs, true
		}
	}

	return nil, false
}

// MatchByDescription scans for Order IDs embedded in bank descriptions.
// Some statements include the full Amazon Order ID (e.g.
// 111-1234567-1234567); when present this is unambiguous and bypasses
// amount/window checks. Run it AFTER Match for transactions that came
// back unmatched.
//
// consumedOrderIDs lists Order IDs already attributed to a transaction
// in the prior pass; matches against them are skipped so a second
// transaction whose description happens to contain the same Order ID
// does not get the same shipment's label re-applied.
func MatchByDescription(transactions []models.Transaction, shipments []Shipment, alreadyMatched map[string]bool, consumedOrderIDs map[string]bool, opts MatchOptions) []MatchResult {
	if opts.MaxLabelLen <= 0 {
		opts.MaxLabelLen = 80
	}
	byID := map[string]int{}
	for i, s := range shipments {
		byID[s.OrderID] = i
	}

	var out []MatchResult
	for _, tx := range transactions {
		if alreadyMatched[tx.Hash] {
			continue
		}
		if !isAmazonDesc(tx.Description) {
			continue
		}
		desc := tx.Description
		for id, idx := range byID {
			if id == "" {
				continue
			}
			if consumedOrderIDs[id] {
				continue
			}
			if strings.Contains(desc, id) {
				out = append(out, MatchResult{
					TxHash:   tx.Hash,
					Label:    formatLabel([]Shipment{shipments[idx]}, opts.MaxLabelLen, false),
					OrderIDs: []string{id},
				})
				break
			}
		}
	}
	return out
}

func collectShipments(all []Shipment, idxs []int) []Shipment {
	out := make([]Shipment, 0, len(idxs))
	for _, i := range idxs {
		out = append(out, all[i])
	}
	return out
}

func uniqOrderIDs(all []Shipment, idxs []int) []string {
	seen := map[string]bool{}
	var out []string
	for _, i := range idxs {
		id := all[i].OrderID
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// formatLabel turns one or more shipments into "Amazon: <first product>"
// or "Amazon: <first product> +N more". The first product is truncated
// to maxLen runes (with an ellipsis suffix on truncation) so very long
// product names don't blow out table layouts. The "+N more" count
// reflects total *additional* products across all matched shipments.
func formatLabel(shipments []Shipment, maxLen int, _ bool) string {
	var products []string
	for _, s := range shipments {
		products = append(products, s.Products...)
	}
	if len(products) == 0 {
		// Defensive: fall back to a recognizable order-id stub.
		if len(shipments) > 0 {
			return "Amazon: Order " + shipments[0].OrderID
		}
		return "Amazon"
	}
	first := truncate(products[0], maxLen)
	if len(products) == 1 {
		return "Amazon: " + first
	}
	return fmt.Sprintf("Amazon: %s +%d more", first, len(products)-1)
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimRight(string(r[:max]), " ") + "…"
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func isAmazonDesc(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "amazon") || strings.Contains(low, "amzn")
}
