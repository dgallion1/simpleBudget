package analysis

// expenseEndPtr returns the *int an ExpenseSource's EndMonth needs (nil means
// perpetual, so a real end month must be addressable).
func expenseEndPtr(month int) *int { return &month }
