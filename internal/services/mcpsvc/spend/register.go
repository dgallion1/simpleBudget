// Package spend serves spending analysis over MCP: what the ledger says, as
// opposed to what the retirement projection assumes.
package spend

import (
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TransactionSource loads the full transaction history. *dataloader.DataLoader
// satisfies it via its existing LoadData method, so no adapter is needed in
// production. The interface exists so tests can substitute a canned
// models.TransactionSet directly -- constructing exact peer groups and planted
// anomalies through real CSV parsing, classification, and near-duplicate
// detection would be indirect and brittle.
type TransactionSource interface {
	LoadData() (*models.TransactionSet, error)
}

// Deps is what the spending tools need. Store is optional and used only to
// turn a locked store into a clear message instead of a parse failure.
type Deps struct {
	Transactions TransactionSource
	Store        *storage.Storage
}

func recoverToError(tool string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panicked: %v", tool, r)
	}
}

// load returns the full ledger, reporting a locked store as such rather than
// letting ciphertext surface as a parse error.
func (d Deps) load() (*models.TransactionSet, error) {
	if d.Store != nil && d.Store.IsEncrypted() && !d.Store.IsUnlocked() {
		return nil, fmt.Errorf(
			"cannot load transaction history: storage is encrypted and locked; unlock it via the budget2 web UI (/unlock) first")
	}
	return d.Transactions.LoadData()
}

// Register adds the spending tools to s.
func Register(s *mcp.Server, deps Deps) {
	registerSearch(s, deps)
	registerAnomalies(s, deps)
	registerPriceCreep(s, deps)
}
