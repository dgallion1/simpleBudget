# NEXT — state of the File Manager swarm run

Written 2026-08-12 by the lead session that completed P12. A relaunched session
starts cold with no memory of that conversation; this file is the handoff.

## Where things stand

`master` is at the merge commit carrying P12 and the P15 oracle, pushed to
`origin/master`. Working tree clean, no feature branches.

| Task | Scope | Tier | Status |
|------|-------|------|--------|
| P12 | Multi-file upload | 1 | **accepted**, merged, pushed |
| P13 | Sortable columns | 2 | pending — brief ready |
| P14 | `ImportDirectory` config + scan endpoint | 2 | pending — brief ready |
| P15 | Import execute + source delete | 3 | pending — oracle written |

Ledger: `.swarm/ledger.tsv`. Gate: `bash /home/darrell/work/agents2/swarm/gate.sh
check <task>` run with cwd set to this repo. `gate.sh done` currently fails, as
it should, listing P13–P15 as pending.

## Do this next

1. **Confirm the session is actually routed through the gateway** before
   dispatching anything. `echo $ANTHROPIC_BASE_URL` must point at
   `http://localhost:4000`, not `https://api.anthropic.com`. The previous
   session was not routed and could not run any Tier 2+ task: the `worker-glm`,
   `checker-glm`, and `worker-local` model aliases only resolve through the
   proxy, and `gate.sh` enforces the two-family quorum mechanically. A
   Sonnet-backed `checker-second` writing `FAMILY: glm` would satisfy the gate
   and verify nothing.

2. **P13 and P14** are independent and can run as parallel background workers.
   Briefs are at `.swarm/briefs/P13.md` and `.swarm/briefs/P14.md`; each
   contains the exact text to paste into the `worker-coder` delegation message
   plus checker notes. Tier 2: the Anthropic mechanical checker and
   `checker-second` (GLM) both run, both must PASS, disagreement dispatches all
   three judges.

3. **P15 only after P14 has merged.** `swarm/tier3-setup.sh P15` cuts its two
   blind worktrees from `HEAD`. Starting before P14 lands gives both workers a
   tree with no `ImportDirectory` and no scan endpoint, and the oracle's scan
   check fails in both for reasons unrelated to either implementation.

   The oracle already exists at `.swarm/tier3/P15/accept.sh` — 13 checks,
   validated: 3 pass on a tree without the feature (build, unit tests, server
   boot, all genuinely true), 10 fail, output byte-identical across runs. It
   asserts on filesystem effects and HTTP status codes rather than response
   bodies, because `tier3-compare.sh` diffs the two worktrees' output literally
   and two implementations will format their outcome lists differently. Every
   safety check also requires a non-404 status so an unimplemented endpoint
   cannot satisfy it vacuously. The request wire format is pinned in the design
   doc §3 — both blind workers must be given it.

4. **Final pass before `gate.sh done`.** Run `checker-a11y` across the File
   Manager page — P13 adds interactive sort controls, which is exactly what the
   standard exists to catch. Then review every file the lead authored: the
   design doc, the ledger, the P15 oracle, these briefs.

## Decisions already made — do not relitigate

Recorded in `docs/superpowers/specs/2026-08-12-file-manager-import-design.md`.

- **Browsers cannot delete a file chosen via `<input type="file">`.** That is
  why the source delete needs a server-side folder import at all. The upload
  path keeps working and never offers a source delete.
- **The import folder is pinned to one configured directory**, defaulting to
  `~/Downloads`, overridable by `BUDGET2_IMPORT_DIR`. Not a UI text box, not a
  picklist. Deletion plus a free-form path is a footgun.
- **Name collisions skip.** No overwrite, no auto-rename, and the source is
  never deleted for a file that was not imported.
- **No file-level dedup.** `LoadData` already dedups transactions across all
  enabled files by `sha256(date|lowercased description|amount)`, so overlapping
  CSVs collapse automatically. Adding a second opinion would be redundant.
- **Ruling 2026-08-12a** on P12: `sanitizeUploadFilename` normalizes traversal
  names to their base rather than rejecting them. Pre-existing, shared with
  `handleFileDelete`, and the property that matters — no write escapes
  `DataDirectory` — was independently verified to hold. Left unchanged
  deliberately.

## Note for the user

P12 landed with **no visible change to the page**. `multiple` on a file input
renders identically; the difference only appears in the OS file dialog. The
visible work — sortable headers, the import panel — is P13 and P14.
