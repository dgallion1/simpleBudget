# T5 — auth.go's staging joins the atomicWrite regime

RULING 2026-08-26d: single-arm run with full dual-lane verification, the
session's established shape (rulings 2026-08-25a/26a/26b/26c precedent) —
N-version blind arms are disproportionate to a ~15-line staging refactor,
per the F3 report's own proportionality lesson. Tier 3 because auth.go is
under internal/services/storage/** (critical glob), and deservedly: this
file writes the encryption config.

## The defect (recorded by checker-second during F4 verification)

`saveConfig` (internal/services/storage/auth.go) stages with a FIXED name
(`configPath + ".tmp"`) via os.WriteFile + os.Rename, outside the
atomicWrite/StagingSuffix regime the F2 work established. Consequences: a
crash between write and rename leaves `encryption_config.json.tmp` orphaned
forever (the regime's orphan cleanup does not know this name), and two
concurrent saves collide on the same staging path.

## Why it bypassed the regime (legitimate, preserved)

saveConfig is package-level, not a Storage method: the encryption config is
the one file that must NEVER be encrypted (it is what enables decryption)
and can be written before Storage's write machinery is available. Routing
it through Storage.WriteFile would encrypt it. The fix therefore adopts the
regime's staging CONVENTION (random-suffixed StagingSuffix name via
os.CreateTemp, 0600, rename; orphan-cleanup coverage) without the
encrypting write path.

## Premise corrected during the build (worker finding, lead-verified framing)

There is NO orphan-deletion sweep in this codebase: "orphan cleanup" means
recognition/exclusion — `IsStagingName` keeps staging files out of backup
zips (backup.SkipPredicate) and protects them from restore's stale-extras
pruning; nothing deletes them. The legacy `.tmp` name was ALREADY recognized
via legacyStagingSuffix, so exclusion coverage is not what the fix buys.
What it actually buys: no fixed-name collision between concurrent saves;
error-path hygiene (a failed write/rename removes the staging file —
mutant-proven); and it makes storage.go's own comment true (it claims
nothing produces the legacy name anymore — auth.go did, until now).

RESOLUTION: single-arm run under ruling 2026-08-26d — no divergence to
resolve; the sole worker-coder implementation stands, subject to the
two-lane verification whose verdicts accompany the ledger attempt.
