# NEXT — state of the File Manager swarm run

Written 2026-08-12 by the lead session that completed P12. A relaunched session
starts cold with no memory of that conversation; this file is the handoff.

Revised 2026-08-21: P16 added, and step 1 rewritten. It told you to verify the
session was routed through a LiteLLM gateway that no longer exists — following
it as written would have stopped a relaunched session from dispatching anything.

## Where things stand

`master` is at `10d4902`, carrying P12, the P15 oracle, and unrelated work
merged since. One unmerged branch: `claude/storage-cache-stale-data-i5lr29`,
which carries P16.

| Task | Scope | Tier | Status |
|------|-------|------|--------|
| P12 | Multi-file upload | 1 | **accepted**, merged, pushed |
| P13 | Sortable columns | 2 | pending — brief ready |
| P14 | `ImportDirectory` config + scan endpoint | 2 | pending — brief ready |
| P15 | Import execute + source delete | 3 | pending — oracle written |
| P16 | Storage read-cache write ordering | 3 | pending — **code written and pushed, unverified** |

Ledger: `.swarm/ledger.tsv`. Gate: `bash /home/darrell/work/agents2/swarm/gate.sh
check <task>` run with cwd set to this repo. `gate.sh done` currently fails, as
it should, listing P13–P16 as pending.

## Do this next

1. **Independence is a lane, not a vendor.** The LiteLLM gateway was dropped on
   2026-08-19; there is no proxy and no local endpoint, `ANTHROPIC_BASE_URL` is
   not something to check, and the `worker-glm` / `checker-glm` aliases are
   gone. Every agent runs on Claude, with the model chosen per agent in
   `.claude/agents/*.md` frontmatter. The second opinion now comes from a
   different **job** and a different **model tier**: the primary verifier asks
   "does this meet the criteria?" and cites the command proving each one;
   `checker-second` asks "what would make this wrong?", defaults to FAIL on
   ambiguity, and is doing its job badly if it never disagrees.

   `gate.sh` still enforces two distinct `FAMILY` values mechanically. Write
   `anthropic` for the primary verifier and `adversarial` for `checker-second`;
   judges write `anthropic`, `adversarial`, and `impact`. `glm` and `local`
   still validate only so pre-2026-08-19 verdicts keep parsing — writing either
   today satisfies the gate and verifies nothing. Two PASSes are weaker
   evidence than the old cross-vendor pair; that reduction was accepted
   deliberately (user decision 2026-08-19).

2. **P16 needs a decision before it needs work**, and it is independent of
   everything below — different subsystem, no ordering constraint. The code is
   already written, tested and pushed on
   `claude/storage-cache-stale-data-i5lr29`: `06176ef` orders the storage read
   cache against writes via a generation counter, `79ef006` covers Lock's bump.
   It was built directly in a lead session, so there is no worker manifest from
   a dispatch and no verdict from either lane.

   `.swarm/manifests/P16.1.files` names two files under
   `internal/services/storage/**`, which is in `critical.globs`, so
   `escalate-scan` flagged it and the ledger tier followed to 3. That is the
   problem: Tier 3 wants the oracle written *before* dispatch and two blind
   arms, and the code already exists. Either treat the branch as one arm and
   write `.swarm/tier3/P16/accept.sh` plus a genuinely blind second arm, or
   discard the branch and run P16 properly from the top. Do not backfill an
   oracle from the implementation and call it N-version — the oracle would
   inherit exactly the assumptions it is supposed to test.

3. **P13 and P14** are independent and can run as parallel background workers.
   Briefs are at `.swarm/briefs/P13.md` and `.swarm/briefs/P14.md`; each
   contains the exact text to paste into the `worker-coder` delegation message
   plus checker notes. Tier 2: the primary mechanical checker (lane
   `anthropic`) and `checker-second` (lane `adversarial`) both run, both must
   PASS, disagreement dispatches all three judges.

4. **P15 only after P14 has merged.** `swarm/tier3-setup.sh P15` cuts its two
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

5. **Final pass before `gate.sh done`.** Run `checker-a11y` across the File
   Manager page — P13 adds interactive sort controls, which is exactly what the
   standard exists to catch. Then review every file the lead authored: the
   design doc, the ledger, the P15 oracle, these briefs, and the P16 ledger row
   and manifest.

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
