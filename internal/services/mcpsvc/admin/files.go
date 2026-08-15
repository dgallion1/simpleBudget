package admin

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type filesInput struct{}

type dataFile struct {
	Name         string `json:"name"`
	SizeBytes    int64  `json:"size_bytes"`
	Enabled      bool   `json:"enabled"`
	Transactions int    `json:"transactions"`
	MinDate      string `json:"min_date,omitempty"`
	MaxDate      string `json:"max_date,omitempty"`
}

type filesOutput struct {
	Count int        `json:"count"`
	Files []dataFile `json:"files"`
	Note  string     `json:"note,omitempty"`
}

func registerFiles(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_data_files",
		Description: "List the CSV files in the user's data directory, with each one's size, date coverage " +
			"and row count. Use it to answer what periods the ledger actually covers and which bank exports are " +
			"loaded. IMPORTANT: transactions here is a RAW row count from a fast scan of the file, so the sum " +
			"across files will NOT match search_transactions -- loading drops internal transfers, merges exact " +
			"duplicates, and suppresses rows the user resolved as near-duplicates, and rows shared between two " +
			"overlapping exports are counted once per file here but once overall there. Treat it as the size of " +
			"the input, not the size of the ledger. enabled is false only when the user has explicitly narrowed " +
			"the selection on the Explorer page; with no selection every file is enabled. min_date/max_date are " +
			"YYYY-MM-DD and are empty for a file whose dates could not be parsed. This tool reads only -- it " +
			"changes nothing, and it does not tell you whether a file's contents are sound.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ filesInput) (res *mcp.CallToolResult, out filesOutput, err error) {
		defer recoverToError("list_data_files", &err)

		if deps.locked() {
			return nil, filesOutput{}, fmt.Errorf(
				"cannot read the data directory: storage is encrypted and locked; unlock it via the budget2 web UI (/unlock) first")
		}
		if deps.Files == nil {
			return nil, filesOutput{}, fmt.Errorf("no data loader is configured on this server")
		}

		infos, err := deps.Files.GetFileInfo()
		if err != nil {
			return nil, filesOutput{}, fmt.Errorf("read data directory: %w", err)
		}

		out = filesOutput{Files: make([]dataFile, 0, len(infos))}
		for _, i := range infos {
			out.Files = append(out.Files, dataFile{
				Name:         i.Name,
				SizeBytes:    i.Size,
				Enabled:      i.Enabled,
				Transactions: i.Transactions,
				MinDate:      i.MinDate,
				MaxDate:      i.MaxDate,
			})
		}
		out.Count = len(out.Files)
		if out.Count == 0 {
			out.Note = "the data directory contains no CSV files; the user has not imported any bank exports yet"
		}
		return nil, out, nil
	})
}
