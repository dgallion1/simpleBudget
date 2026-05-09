package models

// FloatPtr returns a pointer to the given float64 value. Convenience
// for constructing nullable numeric fields in tests and call sites
// where taking the address inline is awkward.
func FloatPtr(v float64) *float64 {
	return &v
}
