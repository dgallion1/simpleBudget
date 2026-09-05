package dataloader

import (
	"budget2/internal/models"
	"testing"
)

func TestSameDayReimportKeepsAccountsSeparate(t *testing.T) {
	makeRow := func(account, description string) models.Transaction {
		row := makeTxOriginal("2026-09-01", -19.99, description, "Posted", "STREAMING MONTHLY")
		row.AccountID = account
		row.Hash = row.ComputeHash()
		return row
	}
	a := makeRow("card-a", "Streaming")
	b := makeRow("card-b", "Streaming")
	if pairs := detectNearDuplicatePairs([]models.Transaction{a, b}); len(pairs) != 0 {
		t.Fatalf("independent payments from different accounts paired: %+v", pairs)
	}
	reimport := makeRow("card-a", "Streaming subscription")
	pairs := detectNearDuplicatePairs([]models.Transaction{a, b, reimport})
	if len(pairs) != 1 || pairs[0].Left.AccountID != "card-a" || pairs[0].Right.AccountID != "card-a" {
		t.Fatalf("cross-account payment stole the real reimport match: %+v", pairs)
	}
}
