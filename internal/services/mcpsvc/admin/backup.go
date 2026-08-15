package admin

import (
	"context"
	"errors"
	"fmt"

	backupsvc "budget2/internal/services/backup"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type runBackupInput struct{}

type runBackupOutput struct {
	Ran         bool   `json:"ran"`
	Dir         string `json:"dir"`
	TS          string `json:"ts,omitempty"`
	FileCount   int    `json:"file_count,omitempty"`
	TotalBytes  int64  `json:"total_bytes,omitempty"`
	Encrypted   bool   `json:"encrypted"`
	AutoEnabled bool   `json:"auto_backup_enabled"`
	Note        string `json:"note,omitempty"`
}

func registerRunBackup(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "run_backup",
		Description: "Take one backup of the user's data directory right now: a timestamped, verified zip " +
			"written into the backup directory, which is OUTSIDE the data directory. This adds a file; it " +
			"changes nothing about the user's data and cannot lose anything. Use it before suggesting a change " +
			"the user might want to walk back. It is a full snapshot, not incremental, and old archives are " +
			"pruned by the app's retention policy, so running it repeatedly costs disk. If a scheduled backup " +
			"is already in flight this returns ran=false with a note rather than queuing a second one -- that " +
			"is a skip, not a failure. auto_backup_enabled reports whether the app also backs up on its own " +
			"schedule; a manual run here does NOT turn that back on if the user disabled it. When the data is " +
			"encrypted the archive contains ciphertext, so restoring it needs the same key.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ runBackupInput) (res *mcp.CallToolResult, out runBackupOutput, err error) {
		defer recoverToError("run_backup", &err)

		if deps.Backups == nil {
			return nil, runBackupOutput{}, fmt.Errorf("no backup service is configured on this server")
		}

		out = runBackupOutput{
			Dir:         deps.Backups.BackupDir(),
			AutoEnabled: deps.Backups.Enabled(),
		}

		if err := deps.Backups.Snapshot(ctx); err != nil {
			if errors.Is(err, backupsvc.ErrSnapshotInProgress) {
				out.Note = "a backup was already running, so this call did not start a second one; the in-flight backup is still completing"
				return nil, out, nil
			}
			return nil, runBackupOutput{}, fmt.Errorf("backup failed: %w", err)
		}

		out.Ran = true
		meta, metaErr := deps.Backups.Meta()
		if metaErr != nil {
			out.Note = "the backup completed but its record could not be read back: " + metaErr.Error()
			return nil, out, nil
		}
		out.TS = meta.TS
		out.FileCount = meta.FileCount
		out.TotalBytes = meta.TotalBytes
		out.Encrypted = meta.Encrypted
		return nil, out, nil
	})
}
