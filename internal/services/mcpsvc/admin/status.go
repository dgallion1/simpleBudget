package admin

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type statusInput struct{}

type backupStatus struct {
	Enabled       bool   `json:"enabled"`
	Dir           string `json:"dir"`
	LastBackupTS  string `json:"last_backup_ts,omitempty"`
	FileCount     int    `json:"file_count,omitempty"`
	TotalBytes    int64  `json:"total_bytes,omitempty"`
	Encrypted     bool   `json:"encrypted"`
	LastAttemptTS string `json:"last_attempt_ts,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

type planStatus struct {
	SettingsDir    string `json:"settings_dir"`
	Revision       int    `json:"revision"`
	ActiveScenario string `json:"active_scenario"`
}

type statusOutput struct {
	DataDir    string `json:"data_dir"`
	Encrypted  bool   `json:"encrypted"`
	Unlocked   bool   `json:"unlocked"`
	AuthMethod string `json:"auth_method,omitempty"`

	// UnresolvedDuplicates is a pointer so a locked store reports null --
	// "not knowable right now" -- rather than 0, which would read as "the
	// queue is empty" and is the opposite of the truth.
	UnresolvedDuplicates *int `json:"unresolved_duplicates"`
	CSVFileCount         *int `json:"csv_file_count"`

	Plan   planStatus   `json:"plan"`
	Backup backupStatus `json:"backup"`

	Notes []string `json:"notes,omitempty"`
}

func registerStatus(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_status",
		Description: "Report the budget2 server's own state: where its data lives, whether that data is " +
			"encrypted and currently unlocked, how the retirement plan's saved settings are versioned, when the " +
			"last backup ran, and how many near-duplicate transaction pairs are waiting for review. Call this " +
			"FIRST when another tool fails for a reason you cannot explain -- an encrypted store that is locked " +
			"makes every ledger-reading tool fail, and this is the only tool that still answers in that state. " +
			"When the store is locked, unresolved_duplicates and csv_file_count are null (meaning 'cannot be " +
			"determined right now'), NOT zero, and a note says so. auth_method names how the user unlocks it " +
			"(password, ssh, age, yubikey) and is empty when the store is not encrypted. revision counts saved " +
			"changes to the retirement plan and is the same counter apply_changes reports. backup.last_backup_ts " +
			"is the most recent SUCCESSFUL backup (format YYYYMMDD_HHMMSS, UTC); backup.last_attempt_ts and " +
			"backup.last_error describe the most recent ATTEMPT, so a non-empty last_error with an older " +
			"last_backup_ts means backups are currently failing. This tool reads only -- it changes nothing.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ statusInput) (res *mcp.CallToolResult, out statusOutput, err error) {
		defer recoverToError("get_status", &err)

		out = statusOutput{}
		if deps.Store != nil {
			out.DataDir = deps.Store.BaseDir()
			out.Encrypted = deps.Store.IsEncrypted()
			out.Unlocked = !deps.Store.IsEncrypted() || deps.Store.IsUnlocked()
			if out.Encrypted {
				out.AuthMethod = string(deps.Store.GetAuthMethod())
			}
		} else {
			out.Unlocked = true
			out.Notes = append(out.Notes, "no storage layer is configured on this server; encryption state is unknown")
		}

		if deps.Settings != nil {
			out.Plan = planStatus{
				SettingsDir:    deps.Settings.SettingsDir(),
				Revision:       deps.Settings.Revision(),
				ActiveScenario: deps.Settings.ActiveScenario(),
			}
		}

		if deps.Backups != nil {
			out.Backup.Enabled = deps.Backups.Enabled()
			out.Backup.Dir = deps.Backups.BackupDir()
			meta, metaErr := deps.Backups.Meta()
			if metaErr != nil {
				out.Notes = append(out.Notes, "the backup record could not be read: "+metaErr.Error())
			} else {
				out.Backup.LastBackupTS = meta.TS
				out.Backup.FileCount = meta.FileCount
				out.Backup.TotalBytes = meta.TotalBytes
				out.Backup.Encrypted = meta.Encrypted
				out.Backup.LastAttemptTS = meta.LastAttemptTS
				out.Backup.LastError = meta.LastError
			}
		} else {
			out.Notes = append(out.Notes, "no backup service is configured on this server")
		}

		// Everything below needs to read the data directory. On a locked
		// store that is impossible, and reporting 0 would be a lie the model
		// cannot detect -- so leave the counts null and say why.
		if deps.locked() {
			out.Notes = append(out.Notes,
				"storage is encrypted and locked, so the duplicate queue and CSV inventory could not be counted; unlock via the web UI (/unlock)")
			return nil, out, nil
		}

		if deps.Files != nil {
			infos, ferr := deps.Files.GetFileInfo()
			if ferr != nil {
				out.Notes = append(out.Notes, "the CSV inventory could not be read: "+ferr.Error())
			} else {
				n := len(infos)
				out.CSVFileCount = &n
			}
		}

		if deps.Duplicates != nil {
			// The duplicate queue is recomputed by LoadData and cached on the
			// loader; without this load the count reflects whatever the last
			// page request happened to leave behind, or nothing at all on a
			// server no one has browsed yet.
			if _, lerr := deps.load(); lerr != nil {
				out.Notes = append(out.Notes, "the duplicate queue could not be counted: "+lerr.Error())
			} else {
				n := deps.Duplicates.UnresolvedDuplicateCount()
				out.UnresolvedDuplicates = &n
			}
		}

		return nil, out, nil
	})
}
