# DI6 isolated demo release — lead integration evidence

Source: baseline 69e484a plus only cumulative DI6.2 manifest paths, copied
under the explicit freeze agreement to /tmp/budget2-DI6-release.XArdi6.
Unfinished DI1/DI2 changes are excluded. This is evidence, not a checker verdict.

Initial `make check` caught stale generated CSS after vet, staticcheck and
govulncheck. The primary checker independently reproduced the same delta.
Attempt 2 adds only the generated outline utility and an epoch regression test;
production Go/JS and markup are unchanged from attempt 1.

Executed with `rtk proxy` in the release snapshot:

- `go test ./...`: exit 0, all packages passed (storage 79.303s).
- `make COMMIT=69e484a-DI6.2 check`: exit 0 after the correction;
  `tailwind.css is up to date`, full Go suite and required uncached accounts
  check passed, ending `✓ all checks passed`. Unchanged packages used Go's
  valid cache from the preceding full run. Vet/staticcheck passed. govulncheck
  reported no called vulnerable symbols; uncalled dependency advisories remain.
- `node --test web/static/js/page-refresh.test.cjs`: 7 passed, 0 failed.
- `go build -ldflags="-X budget2/internal/version.Version=demo-refresh -X budget2/internal/version.Commit=69e484a-DI6.2" -o budget2-demo-refresh ./cmd/server`: exit 0.

Built binary SHA256:
49c559915bf8f6659df8e8a7242f93e0076ea117233b69e9a18c567ba578c6bd

Demo pre-deployment data hashes captured in the lead tool session. Verified
data path /home/darrell/bin/ai/budget2-demo/data. Real server remains PID
2290994; read-only health response identifies v1.4.0-1095-gf5e68e0. Demo PID
2309826 independently identified by BUDGET_DATA_DIR and listen address :8081.
Revalidate exact process before any restart. No data settings modified.

## Deployment verification

Both named reviewers passed attempt 2; gate check exited 0:
`OK: DI6 accepted at tier 2 (attempt 2)`.

Installed the exact binary above at
/home/darrell/bin/ai/budget2-demo/releases/refresh-DI6.2-20260906/budget2.
Backed up the previous launcher to
/home/darrell/bin/ai/budget2-demo/backups/before-refresh-tool-20260906/run-demo.sh.
The demo launcher now selects this release and disables debug filesystem assets.
Validated launcher with bash -n. Revalidated PID 2309826's exact demo data/listen
environment, stopped ONLY that process and restarted :8081. Persistent launcher
session 57391, output /tmp/budget2-demo-DI6.log.

HTTP /api/health returns commit 69e484a-DI6.2 and status ok. Dashboard HTML
includes the refresh script exactly once. Demo MCP get_status independently
returns data_dir /home/darrell/bin/ai/budget2-demo/data, settings under that
directory, five CSVs, unchanged plan revision 0, version demo-refresh.
Demo MCP refresh_pages returned requested:true, revision:1; HTTP /api/ui-refresh
then returned revision:1 with the same new server epoch. This confirms the
request path, not delivery to every open browser tab.

Before/after SHA256 and inventory for all nine demo data files match exactly,
including whatif settings, accounts and Major Expenses. Real PID 2290994 remains
alive, with unchanged health build v1.4.0-1095-gf5e68e0. No real-data reads/writes
were used for fixture verification; only real-server health/process checks.
Existing demo tabs need one manual reload to load the listener. Larger DI2–5
changes are not part of this deployed binary and remain in progress.
