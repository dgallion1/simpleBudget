package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

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

type listBackupsInput struct{}

type backupArchive struct {
	Name string `json:"name"`
	// Two renderings of one timestamp, on purpose. TS matches the format
	// get_status reports for the last backup, so a model can line the two up
	// without parsing; TSISO is unambiguous to reason about ordering and age
	// with.
	TS    string `json:"ts"`
	TSISO string `json:"ts_iso"`
	Bytes int64  `json:"bytes"`
}

type listBackupsOutput struct {
	Dir      string          `json:"dir"`
	Count    int             `json:"count"`
	Archives []backupArchive `json:"archives"`
	Note     string          `json:"note,omitempty"`
}

func registerListBackups(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_backups",
		Description: "List the backup archives on disk, NEWEST FIRST, so archives[0] is the most recent. Each " +
			"is a full timestamped zip of the user's data directory taken by run_backup or the automatic " +
			"schedule, not an incremental diff. A row's `name` is exactly what restore_backup takes -- pass it " +
			"back verbatim and never construct one, because a name that does not match an archive on disk is " +
			"refused. `ts` is UTC in the same YYYYMMDD_HHMMSS format get_status reports as " +
			"backup.last_backup_ts, so the two can be matched up; `ts_iso` is the same instant in RFC3339. " +
			"`bytes` is the archive's size on disk. An empty list means no backup has ever completed here, " +
			"which is also the state right after the backup directory is cleared -- it does NOT mean backups " +
			"are disabled (get_status reports that). Old archives are pruned automatically by a retention " +
			"policy, so a name read from an earlier answer can be gone by the time it is used; re-list rather " +
			"than trusting a remembered name. When the user's data is encrypted the archives hold ciphertext " +
			"and restoring one needs the same key. This tool reads only -- it changes nothing.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listBackupsInput) (res *mcp.CallToolResult, out listBackupsOutput, err error) {
		defer recoverToError("list_backups", &err)

		if deps.Backups == nil {
			return nil, listBackupsOutput{}, fmt.Errorf("no backup service is configured on this server")
		}

		// Never nil: a null archives field reads as "unknown" where the truth
		// is "none".
		out = listBackupsOutput{Dir: deps.Backups.BackupDir(), Archives: []backupArchive{}}

		archives, listErr := deps.Backups.List()
		if listErr != nil {
			return nil, listBackupsOutput{}, fmt.Errorf("the backup directory could not be listed: %w", listErr)
		}
		for _, a := range archives {
			out.Archives = append(out.Archives, backupArchive{
				Name:  a.Name,
				TS:    a.TS.UTC().Format("20060102_150405"),
				TSISO: a.TS.UTC().Format(time.RFC3339),
				Bytes: a.Bytes,
			})
		}
		out.Count = len(out.Archives)
		if out.Count == 0 {
			out.Note = "there are no backup archives in this directory, so there is nothing to restore; " +
				"run_backup takes one now"
		}
		return nil, out, nil
	})
}
