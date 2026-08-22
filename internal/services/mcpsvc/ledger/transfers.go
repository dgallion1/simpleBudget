package ledger

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// transferRow is one transfer flow in get_transfers' result. A transfer is
// neither income nor expense; it is money moving between the user's own
// accounts (paired) or to/from an account whose CSV is not loaded
// (external). See GLOSSARY.md ("Transfer", "TransferClass").
type transferRow struct {
	// Date is the transaction date, YYYY-MM-DD.
	Date string `json:"date"`
	// Description is the transaction's display label (Label()).
	Description string `json:"description"`
	// AccountID is the account this leg belongs to.
	AccountID string `json:"account_id"`
	// Amount is SIGNED in bank convention: positive = money received into
	// this account, negative = money sent out of this account.
	Amount float64 `json:"amount"`
	// Class is "paired" (both legs loaded, linked by a shared pair key) or
	// "external" (the counterparty's CSV is not loaded). GLOSSARY.md
	// "TransferClass".
	Class string `json:"class"`
	// PairKey is the shared TransferPairKey for a paired transfer, empty for
	// external. Either leg of a paired transfer resolves the pair.
	PairKey string `json:"pair_key,omitempty"`
	// CounterpartyAccountID is the other leg's account for a paired
	// transfer; empty for external (the counterparty is not loaded).
	CounterpartyAccountID string `json:"counterparty_account_id,omitempty"`
	// Category is the source CSV's free-text category, if any.
	Category string `json:"category,omitempty"`
}

type getTransfersInput struct {
	StartDate   string `json:"start_date,omitempty" jsonschema:"earliest date to include, inclusive, YYYY-MM-DD"`
	EndDate     string `json:"end_date,omitempty" jsonschema:"latest date to include, inclusive, YYYY-MM-DD"`
	Institution string `json:"institution,omitempty" jsonschema:"restrict to legs whose account's Institution matches (case-insensitive); omit for all"`
	AccountID   string `json:"account_id,omitempty" jsonschema:"restrict to legs whose AccountID matches; omit for all. A paired transfer's other leg is unaffected, so filtering by the source account shows the outflow leg only."`
}

type getTransfersOutput struct {
	Count         int           `json:"count"`
	TotalIn       float64       `json:"total_in"`
	TotalOut      float64       `json:"total_out"`
	Net           float64       `json:"net"`
	PairedCount   int           `json:"paired_count"`
	ExternalCount int           `json:"external_count"`
	Transfers     []transferRow `json:"transfers"`
}

func registerGetTransfers(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_transfers",
		Description: "Money moving between the user's own accounts: the transfer flows the ledger recorded. " +
			"A Transfer is the third transaction type (neither income nor expense) -- see GLOSSARY.md " +
			"(\"Transfer\", \"TransferClass\"). Class \"paired\" means both legs are loaded and linked by a " +
			"shared pair key; class \"external\" means the counterparty account's CSV is not loaded (e.g. a " +
			"Vanguard contribution whose receiving CSV was never imported), so only one leg appears. " +
			"amount is SIGNED in bank convention: positive = money received into this account, negative = " +
			"money sent out. total_in / total_out / net are the signed sums over the returned legs, so a " +
			"question like \"how much did I move into checking this year\" is answered by filtering to the " +
			"checking account and reading total_in. Filters combine with AND: start_date/end_date (inclusive, " +
			"YYYY-MM-DD), institution (case-insensitive match against the account's Institution), and " +
			"account_id (exact). A paired transfer's OTHER leg is not pulled in by an account_id filter, so " +
			"filtering by the source account shows the outflow leg only -- to see both legs of a pair, omit " +
			"account_id. No classification or pairing logic is computed here; the rows come straight from " +
			"the ledger's Transfer-typed transactions.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getTransfersInput) (res *mcp.CallToolResult, out getTransfersOutput, err error) {
		defer recoverToError("get_transfers", &err)

		start, err := parseDate("start_date", in.StartDate)
		if err != nil {
			return nil, getTransfersOutput{}, err
		}
		end, err := parseDate("end_date", in.EndDate)
		if err != nil {
			return nil, getTransfersOutput{}, err
		}
		if start != nil && end != nil && start.After(*end) {
			return nil, getTransfersOutput{}, fmt.Errorf("start_date %s is after end_date %s", in.StartDate, in.EndDate)
		}

		accountID := strings.TrimSpace(in.AccountID)
		institution := strings.TrimSpace(in.Institution)

		// Validate the account_id and institution filters against the
		// configured accounts, so a typo is a clear error rather than a
		// silently empty result.
		var accountsByID map[string]models.Account
		if deps.Accounts != nil {
			accts, aErr := deps.Accounts.LoadAccounts()
			if aErr != nil {
				return nil, getTransfersOutput{}, fmt.Errorf("cannot load accounts: %w", aErr)
			}
			accountsByID = make(map[string]models.Account, len(accts))
			for _, a := range accts {
				accountsByID[a.ID] = a
			}
			if accountID != "" {
				if _, ok := accountsByID[accountID]; !ok {
					return nil, getTransfersOutput{}, fmt.Errorf("no account with id %q; call get_accounts for the current IDs", accountID)
				}
			}
			if institution != "" {
				if !institutionExists(accountsByID, institution) {
					return nil, getTransfersOutput{}, fmt.Errorf("no account with institution %q", institution)
				}
			}
		}

		ts, err := deps.load()
		if err != nil {
			return nil, getTransfersOutput{}, err
		}
		var txns []models.Transaction
		if ts != nil {
			txns = ts.Transactions
		}

		rows := make([]transferRow, 0)
		var totalIn, totalOut float64
		paired, external := 0, 0
		for _, t := range txns {
			if t.TransactionType != models.Transfer {
				continue
			}
			if !inWindow(t.Date, start, end) {
				continue
			}
			if accountID != "" && t.AccountID != accountID {
				continue
			}
			if institution != "" {
				a, ok := accountsByID[t.AccountID]
				if !ok || !strings.EqualFold(a.Institution, institution) {
					continue
				}
			}
			row := transferRow{
				Date:        t.Date.Format("2006-01-02"),
				Description: t.Label(),
				AccountID:   t.AccountID,
				Amount:      round2(t.Amount),
				Class:       t.TransferClass,
				PairKey:     t.TransferPairKey,
				Category:    t.Category,
			}
			if t.TransferClass == "paired" {
				paired++
				if other := counterpartyAccountID(t, txns); other != "" {
					row.CounterpartyAccountID = other
				}
			} else {
				external++
			}
			rows = append(rows, row)
			if t.Amount > 0 {
				totalIn += t.Amount
			} else {
				totalOut += t.Amount
			}
		}

		// Newest-first, matching search_transactions.
		sort.SliceStable(rows, func(i, j int) bool {
			return rows[i].Date > rows[j].Date
		})

		return nil, getTransfersOutput{
			Count:         len(rows),
			TotalIn:       round2(totalIn),
			TotalOut:      round2(totalOut),
			Net:           round2(totalIn + totalOut),
			PairedCount:   paired,
			ExternalCount: external,
			Transfers:     rows,
		}, nil
	})
}

// counterpartyAccountID returns the account ID of the other leg of a paired
// transfer. A paired transfer has exactly two legs sharing a pair key; the
// counterparty is the leg whose AccountID differs from t's.
func counterpartyAccountID(t models.Transaction, txns []models.Transaction) string {
	if t.TransferPairKey == "" {
		return ""
	}
	for _, other := range txns {
		if other.TransferPairKey == t.TransferPairKey && other.AccountID != t.AccountID {
			return other.AccountID
		}
	}
	return ""
}

// institutionExists reports whether any account has the given institution
// (case-insensitive).
func institutionExists(accountsByID map[string]models.Account, institution string) bool {
	for _, a := range accountsByID {
		if strings.EqualFold(a.Institution, institution) {
			return true
		}
	}
	return false
}

// parseDate parses a YYYY-MM-DD value, returning nil for an empty string.
func parseDate(field, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, fmt.Errorf("%s %q is not a valid date (want YYYY-MM-DD): %w", field, value, err)
	}
	return &t, nil
}

// inWindow reports whether d falls within [start, end], both inclusive and
// either possibly nil for "unbounded on this side".
func inWindow(d time.Time, start, end *time.Time) bool {
	if start != nil {
		startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
		if d.Before(startDay) {
			return false
		}
	}
	if end != nil {
		endDay := time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 999999999, end.Location())
		if d.After(endDay) {
			return false
		}
	}
	return true
}
