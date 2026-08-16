package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	backupsvc "budget2/internal/services/backup"
	"budget2/internal/services/mcpsvc/confirm"
	"budget2/internal/services/restore"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// rawJSON renders a result's structured content as the client would see it on
// the wire, for assertions about null-vs-empty that a decode into a Go struct
// erases.
func rawJSON(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	return string(b)
}

// restoreDeps wires the real restore service over the same temp data
// directory and the same *backup.Service the admin deps already hold --
// mirroring cmd/server, where both the /restore route and this tool are
// handed one instance.
func restoreDeps(t *testing.T) (Deps, string, *backupsvc.Service) {
	t.Helper()
	deps, dir := newLiveDeps(t)
	svc, ok := deps.Backups.(*backupsvc.Service)
	if !ok {
		t.Fatalf("Backups is %T, want *backup.Service", deps.Backups)
	}
	deps.Restores = restore.New(restore.Deps{
		DataDir:   dir,
		BackupDir: svc.BackupDir(),
		Store:     deps.Store,
		Backups:   svc,
	})
	deps.Confirm = confirm.NewRegistry(time.Minute)
	return deps, dir, svc
}

// takeRealBackup snapshots the data directory as it stands and returns the
// archive's name. A hand-written zip would test the tool against a fixture;
// this tests it against what the app actually writes.
func takeRealBackup(t *testing.T, svc *backupsvc.Service) string {
	t.Helper()
	if err := svc.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	archives, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(archives) == 0 {
		t.Fatal("no archive on disk after a snapshot")
	}
	return archives[0].Name
}

// plantFakeArchives writes files named like archives so listing can be tested
// at more than one timestamp without waiting a second between snapshots.
func plantFakeArchives(t *testing.T, dir string, stamps ...string) []string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	names := make([]string, 0, len(stamps))
	for _, s := range stamps {
		name := "budget_backup_" + s + ".zip"
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		names = append(names, name)
	}
	return names
}

func TestListBackupsReportsArchivesNewestFirst(t *testing.T) {
	deps, _, svc := restoreDeps(t)
	names := plantFakeArchives(t, svc.BackupDir(), "20260101_030000", "20260801_030000")
	cs := connect(t, deps)

	out := decodeToolResult[listBackupsOutput](t, call(t, cs, "list_backups", map[string]any{}))
	if out.Count != 2 || len(out.Archives) != 2 {
		t.Fatalf("count = %d, archives = %+v, want 2", out.Count, out.Archives)
	}
	if out.Archives[0].Name != names[1] {
		t.Errorf("archives[0] = %q, want the NEWEST (%q)", out.Archives[0].Name, names[1])
	}
	if out.Archives[0].TS != "20260801_030000" {
		t.Errorf("ts = %q, want 20260801_030000 (the format get_status reports)", out.Archives[0].TS)
	}
	if out.Archives[0].TSISO != "2026-08-01T03:00:00Z" {
		t.Errorf("ts_iso = %q, want 2026-08-01T03:00:00Z", out.Archives[0].TSISO)
	}
	if out.Dir != svc.BackupDir() {
		t.Errorf("dir = %q, want %q", out.Dir, svc.BackupDir())
	}
}

// Empty must read as "none", not as "unknown": a null archives field and a
// bare zero count are both things a model reads as an error it should retry.
func TestListBackupsOnAnEmptyDirectorySaysSo(t *testing.T) {
	deps, _, _ := restoreDeps(t)
	cs := connect(t, deps)

	res := call(t, cs, "list_backups", map[string]any{})
	out := decodeToolResult[listBackupsOutput](t, res)
	if out.Count != 0 {
		t.Fatalf("count = %d, want 0", out.Count)
	}
	if out.Note == "" {
		t.Error("no note explaining that there is nothing to restore")
	}
	if !strings.Contains(rawJSON(t, res), `"archives":[]`) {
		t.Errorf("archives serialized as null rather than []: %s", rawJSON(t, res))
	}
}

// The first call is the whole guard. If it restores, the two-step protocol is
// decorative -- so this asserts on the data directory, not just the flag.
func TestRestoreFirstCallDoesNotRestore(t *testing.T) {
	deps, dir, svc := restoreDeps(t)
	name := takeRealBackup(t, svc)
	added := filepath.Join(dir, "added-after-the-backup.csv")
	if err := os.WriteFile(added, []byte("Date,Amount\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{"name": name}))
	if out.Confirmed {
		t.Error("confirmed = true on the first call")
	}
	if out.ConfirmToken == "" {
		t.Error("no confirm_token returned, so the restore could never be confirmed")
	}
	if out.WhatWouldHappen == "" {
		t.Fatal("no what_would_happen returned; the user has nothing to agree to")
	}
	// The prune is the surprising half of a restore. If the preview does not
	// say files get deleted, the user is agreeing to the wrong thing.
	if !strings.Contains(strings.ToUpper(out.WhatWouldHappen), "DELETE") {
		t.Errorf("what_would_happen does not mention deletion: %q", out.WhatWouldHappen)
	}
	if !strings.Contains(out.WhatWouldHappen, name) {
		t.Errorf("what_would_happen does not name the archive: %q", out.WhatWouldHappen)
	}
	if _, err := os.Stat(added); err != nil {
		t.Fatalf("the preview call touched the data directory: %v", err)
	}
}

func TestRestoreSecondCallWithTheTokenRestoresAndPrunes(t *testing.T) {
	deps, dir, svc := restoreDeps(t)
	name := takeRealBackup(t, svc)
	added := filepath.Join(dir, "added-after-the-backup.csv")
	if err := os.WriteFile(added, []byte("Date,Amount\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cs := connect(t, deps)

	first := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{"name": name}))
	out := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{
		"name":          name,
		"confirm_token": first.ConfirmToken,
	}))
	if !out.Confirmed {
		t.Fatal("confirmed = false after redeeming a valid token")
	}
	if out.Restored == 0 {
		t.Error("restored = 0, want the archive's files written back")
	}
	if out.Pruned != 1 {
		t.Errorf("pruned = %d, want 1 (the file added after the backup)", out.Pruned)
	}
	if _, err := os.Stat(added); !os.IsNotExist(err) {
		t.Errorf("the file added after the backup survived the restore (stat err = %v)", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "checking.csv")); err != nil {
		t.Errorf("the archived CSV was not restored: %v", err)
	}
	if out.Note == "" {
		t.Error("no note telling the user how to get back to where they were")
	}
}

func TestRestoreRefusesAReplayedToken(t *testing.T) {
	deps, dir, svc := restoreDeps(t)
	name := takeRealBackup(t, svc)
	cs := connect(t, deps)

	first := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{"name": name}))
	call(t, cs, "restore_backup", map[string]any{"name": name, "confirm_token": first.ConfirmToken})

	// A file created between the legitimate restore and the replay: if the
	// replay runs, the prune deletes it.
	sentinel := filepath.Join(dir, "written-between-the-two-calls.csv")
	if err := os.WriteFile(sentinel, []byte("Date,Amount\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res := call(t, cs, "restore_backup", map[string]any{"name": name, "confirm_token": first.ConfirmToken})
	if !res.IsError {
		t.Fatal("a replayed token was accepted")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("the replayed call restored anyway: %v", err)
	}
}

// A token minted for one archive must not restore another. Without the args
// binding, "preview the small one, restore the big one" would work.
func TestRestoreTokenIsBoundToTheArchiveName(t *testing.T) {
	deps, dir, svc := restoreDeps(t)
	real := takeRealBackup(t, svc)
	other := plantFakeArchives(t, svc.BackupDir(), "20260101_030000")[0]
	sentinel := filepath.Join(dir, "must-survive.csv")
	if err := os.WriteFile(sentinel, []byte("Date,Amount\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cs := connect(t, deps)

	first := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{"name": other}))
	res := call(t, cs, "restore_backup", map[string]any{"name": real, "confirm_token": first.ConfirmToken})
	if !res.IsError {
		t.Fatal("a token minted for one archive was accepted for another")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("the mismatched call restored anyway: %v", err)
	}
}

func TestRestoreRejectsANameThatIsNotOnDisk(t *testing.T) {
	deps, _, svc := restoreDeps(t)
	plantFakeArchives(t, svc.BackupDir(), "20260101_030000")
	cs := connect(t, deps)

	for _, name := range []string{
		"budget_backup_20991231_235959.zip", // well-formed, absent
		"../../etc/passwd",                  // traversal
		"notes.txt",
	} {
		msg := toolErrorText(t, call(t, cs, "restore_backup", map[string]any{"name": name}))
		if !strings.Contains(msg, "list_backups") && !strings.Contains(msg, "no backup archive") {
			t.Errorf("error for %q = %q, want it to point at list_backups", name, msg)
		}
	}
}

// Retention can prune an archive between the preview and the confirmation.
// The confirm path re-resolves the archive before spending anything, so this
// is refused early: the data is untouched, the error points back at
// list_backups, and the token is not burned on an operation that could never
// have succeeded.
func TestRestoreRefusesWhenTheArchiveVanishedAfterThePreview(t *testing.T) {
	deps, dir, svc := restoreDeps(t)
	name := takeRealBackup(t, svc)
	sentinel := filepath.Join(dir, "must-survive.csv")
	if err := os.WriteFile(sentinel, []byte("Date,Amount\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cs := connect(t, deps)

	first := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{"name": name}))
	if err := os.Remove(filepath.Join(svc.BackupDir(), name)); err != nil {
		t.Fatalf("remove archive: %v", err)
	}

	msg := toolErrorText(t, call(t, cs, "restore_backup", map[string]any{
		"name":          name,
		"confirm_token": first.ConfirmToken,
	}))
	if !strings.Contains(msg, "nothing to restore") && !strings.Contains(msg, "list_backups") {
		t.Errorf("error = %q, want it to say the archive is gone and point back at list_backups", msg)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("the failed restore touched the data directory: %v", err)
	}
}

// The distinction this function draws is the whole point of it: a write
// failure is the one case where "nothing changed" would be a lie.
func TestRestoreDamageReportDistinguishesAPartialWrite(t *testing.T) {
	if got := restoreDamageReport(fmt.Errorf("wrapped: %w", restore.ErrWriteFailed)); !strings.Contains(got, "partly rewritten") {
		t.Errorf("ErrWriteFailed report = %q, want it to admit a partial rewrite", got)
	}
	if got := restoreDamageReport(fmt.Errorf("wrapped: %w", backupsvc.ErrSnapshotInProgress)); !strings.Contains(got, "nothing was changed") {
		t.Errorf("ErrSnapshotInProgress report = %q, want it to say nothing changed", got)
	}
	if got := restoreDamageReport(restore.ErrInvalidArchive); !strings.Contains(got, "nothing was changed") {
		t.Errorf("ErrInvalidArchive report = %q, want it to say nothing changed", got)
	}
}

func TestRestoreWithNoArchivesAtAllSaysSo(t *testing.T) {
	deps, _, _ := restoreDeps(t)
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "restore_backup", map[string]any{
		"name": "budget_backup_20260801_030000.zip",
	}))
	if !strings.Contains(msg, "no backup archives") {
		t.Errorf("error = %q, want it to say the backup directory is empty", msg)
	}
}

func TestRestoreRequiresAName(t *testing.T) {
	deps, _, _ := restoreDeps(t)
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "restore_backup", map[string]any{}))
	if !strings.Contains(msg, "name") {
		t.Errorf("error = %q, want it to name the missing argument", msg)
	}
}

// A nil dependency must fail this one call with a named error rather than
// panicking or -- worse -- minting a token no redeem could ever honor.
func TestRestoreWithoutARestoreServiceReportsIt(t *testing.T) {
	deps, _, svc := restoreDeps(t)
	name := takeRealBackup(t, svc)
	deps.Restores = nil
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "restore_backup", map[string]any{"name": name}))
	if !strings.Contains(msg, "restore service") {
		t.Errorf("error = %q, want it to name the missing restore service", msg)
	}
}

func TestRestoreWithoutAConfirmRegistryReportsIt(t *testing.T) {
	deps, _, svc := restoreDeps(t)
	name := takeRealBackup(t, svc)
	deps.Confirm = nil
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "restore_backup", map[string]any{"name": name}))
	if !strings.Contains(msg, "confirmation registry") {
		t.Errorf("error = %q, want it to name the missing confirmation registry", msg)
	}
}

func TestListBackupsWithoutABackupServiceReportsIt(t *testing.T) {
	deps, _ := newDeps(t, nil)
	deps.Backups = nil
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "list_backups", map[string]any{}))
	if !strings.Contains(msg, "backup service") {
		t.Errorf("error = %q, want it to name the missing backup service", msg)
	}
}

// connectAsking is connect() with a client that CAN prompt a human, answering
// every approval request with answer. The SDK drives the round-trip: the tool
// returns an input request, this handler answers it, and the tool call is
// re-invoked with the response -- so a test here exercises the real protocol
// path, not a stubbed decision.
//
// mustSay is asserted against the prompt the human would see. It is per-call
// because each guarded tool has its own consequence to state, and a prompt
// that does not state it is theater.
func connectAsking(t *testing.T, deps Deps, answer *mcp.ElicitResult, mustSay string) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v0.0.0"}, nil)
	Register(srv, deps)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			// The person must be shown what they are agreeing to.
			if !strings.Contains(req.Params.Message, mustSay) {
				t.Errorf("approval prompt does not mention %q: %q", mustSay, req.Params.Message)
			}
			return answer, nil
		},
	})
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}

func approve() *mcp.ElicitResult {
	return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}
}

// The point of the whole change: a human says no and the data survives.
func TestRestoreDoesNotRunWhenTheUserRefuses(t *testing.T) {
	deps, dir, svc := restoreDeps(t)
	name := takeRealBackup(t, svc)
	sentinel := filepath.Join(dir, "added-after-the-backup.csv")
	if err := os.WriteFile(sentinel, []byte("Date,Amount\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cs := connectAsking(t, deps, &mcp.ElicitResult{Action: "decline"}, "WOULD BE DELETED")

	first := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{"name": name}))
	out := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{
		"name":          name,
		"confirm_token": first.ConfirmToken,
	}))

	if out.Confirmed {
		t.Error("confirmed = true after the user refused")
	}
	if out.HumanApproval != "refused" {
		t.Errorf("human_approval = %q, want refused", out.HumanApproval)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("the refused restore deleted data anyway: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "checking.csv")); err != nil {
		t.Errorf("the data directory was disturbed by a refused restore: %v", err)
	}
}

func TestRestoreRunsWhenTheUserApproves(t *testing.T) {
	deps, dir, svc := restoreDeps(t)
	name := takeRealBackup(t, svc)
	added := filepath.Join(dir, "added-after-the-backup.csv")
	if err := os.WriteFile(added, []byte("Date,Amount\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cs := connectAsking(t, deps, approve(), "WOULD BE DELETED")

	first := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{"name": name}))
	out := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{
		"name":          name,
		"confirm_token": first.ConfirmToken,
	}))

	if !out.Confirmed {
		t.Fatalf("confirmed = false after the user approved (note: %q)", out.Note)
	}
	if out.HumanApproval != "approved" {
		t.Errorf("human_approval = %q, want approved", out.HumanApproval)
	}
	if _, err := os.Stat(added); !os.IsNotExist(err) {
		t.Errorf("the approved restore did not prune (stat err = %v)", err)
	}
}

// An accept whose form is empty is a client auto-accepting, not a person
// agreeing. It must not be enough to overwrite the data directory.
func TestRestoreRefusesAnEmptyAccept(t *testing.T) {
	deps, dir, svc := restoreDeps(t)
	name := takeRealBackup(t, svc)
	sentinel := filepath.Join(dir, "added-after-the-backup.csv")
	if err := os.WriteFile(sentinel, []byte("Date,Amount\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cs := connectAsking(t, deps, &mcp.ElicitResult{Action: "accept"}, "WOULD BE DELETED")

	first := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{"name": name}))
	out := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{
		"name":          name,
		"confirm_token": first.ConfirmToken,
	}))

	if out.Confirmed {
		t.Error("an empty accept was treated as approval")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("an empty accept deleted data: %v", err)
	}
}

// The documented fallback: a client that cannot prompt still works, and the
// answer says out loud that nobody was asked.
func TestRestoreOnAClientThatCannotPromptSaysNobodyWasAsked(t *testing.T) {
	deps, dir, svc := restoreDeps(t)
	name := takeRealBackup(t, svc)
	added := filepath.Join(dir, "added-after-the-backup.csv")
	if err := os.WriteFile(added, []byte("Date,Amount\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cs := connect(t, deps) // no elicitation handler

	first := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{"name": name}))
	out := decodeToolResult[restoreBackupOutput](t, call(t, cs, "restore_backup", map[string]any{
		"name":          name,
		"confirm_token": first.ConfirmToken,
	}))

	if !out.Confirmed {
		t.Fatalf("the restore did not run on a client without elicitation (note: %q)", out.Note)
	}
	if out.HumanApproval != "not asked" {
		t.Errorf("human_approval = %q, want \"not asked\"", out.HumanApproval)
	}
	if !strings.Contains(out.Note, "NO HUMAN WAS ASKED") {
		t.Errorf("note = %q, want it to admit nobody was asked", out.Note)
	}
	if _, err := os.Stat(added); !os.IsNotExist(err) {
		t.Errorf("the restore did not actually run (stat err = %v)", err)
	}
}
