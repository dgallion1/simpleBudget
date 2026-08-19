package ledger

import "math"

// round2 rounds v to two decimal places, the cents precision the ledger
// stores. Mirrors spend.round2 so tool outputs read the same.
func round2(v float64) float64 { return math.Round(v*100) / 100 }
