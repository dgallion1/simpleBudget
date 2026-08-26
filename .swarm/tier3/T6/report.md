# T6 — the data directory does not honour symlinks

Doc-only task, single-arm with dual-lane verification (the T4 shape); Tier 3
only because the comment lives in internal/services/storage/** (critical
glob).

## The decision

RULING 2026-08-26e (user): atomicWrite publishes by staging + rename; a
symlinked destination is replaced by a regular file and the link's target
elsewhere goes silently stale. Flagged during F4 verification, deliberately
unbundled. The user ruled AGAINST resolving symlinks: honoring them would
relocate staging to the target's directory (reopening the F2 atomicity
analysis), break on cross-filesystem targets where rename cannot be atomic,
and introduce a resolve-to-rename race — real risk in the most
safety-critical write path, spent on a workflow the app never promised. The
contract instead: files in the data directory are real files; symlinks are
not honoured.

## The change

Comment at atomicWrite stating the contract and why; one sentence in the
README's data-directory documentation so users learn it before, not after,
a stale link. No behavior change.

## Attempt 1 → 2: the adversarial FAIL and how it was handled

Attempt 1 split: checker-tests PASS (behavioral proof that stage+rename
replaces a symlink and leaves the target untouched, dangling case included),
checker-second FAIL. The FAIL's counterexample is real and was reproduced
empirically: `internal/config/config.go` `SaveUserSettings` writes
`data/settings/user_settings.json` with a bare `os.WriteFile`, which writes
THROUGH a symlink (link survives, target keeps updating) — the inverse of
attempt 1's unconditional README promise about every file in `data/`. The
method has no call sites in the running server, but it is exported, tested,
and documented, so the blanket sentence overclaimed.

The lead concurred with the FAIL, so no judge panel was convened: a panel
exists to decide whether to accept despite a contested FAIL, and its UPHOLD
outcome — send the work back — is what happened directly. Attempt 2 is the
scoped fix, still docs-only under ruling 2026-08-26e: the README sentence now
attaches the claim to the save mechanism ("SimpleBudget saves by writing a
fresh file and renaming it into place, so when it saves over a symlink...")
instead of promising the behavior of any conceivable write into `data/`, and
`SaveUserSettings` gained a doc comment stating it is outside the
stage-and-rename contract and must be routed through Storage before being
wired into a handler. Both lanes re-ran in full at attempt 2.

Recorded observation from the adversarial lane (not a defect in this task,
logged for the backlog): backup's snapshot walk dereferences symlinked FILES
(archives the target's bytes under the link's name) while restore's prune
unlinks them — an asymmetry in the "symlinks are not honoured" framing that
touches reads, not saves.

## Attempt 2 → 3: a live counterexample, the hard stop, and ruling 2026-08-26f

Attempt 2's adversarial lane FAILed again, with a stronger counterexample
than attempt 1's: `HandlePlotly` (internal/handlers/backup/handlers.go:677,
wired into the live router at cmd/server/main.go:251) caches the Plotly JS
bundle with a bare `os.WriteFile` into `data/cache/plotly.min.js`. Reproduced:
a symlink at that path survives the write and its target keeps being updated —
falsifying attempt 2's rescoped sentence "SimpleBudget saves by writing a
fresh file and renaming it into place" as a general mechanism claim. The
attempt-2 primary lane was stopped mid-run once the FAIL landed (its verdict
file, if present at attempt 2, is from a superseded attempt and carries no
standing). Two failed Tier-3 attempts triggered the constitution's hard stop.

RULING 2026-08-26f (user): "push it through, I mostly take your advice" —
standing authorization to proceed past the hard stop on the lead's
recommended path. That path is attempt 3, still docs-only under 2026-08-26e:
stop making a general mechanism claim that every new write path can falsify,
and instead state the enumerable truth — saves don't follow symlinks (replace
or refuse), with the app's own download cache named as the one live
exception — plus a contract comment at the HandlePlotly cache write, matching
the one SaveUserSettings received in attempt 2.

## Attempt 3 → 4: the third bypass, and why the claim shape changed

Attempt 3's adversarial lane FAILed on a third live counterexample:
`internal/services/storage/migration.go` writes the `.encryption-verify`
(line ~49) and `.encrypted` (line ~101) marker files into the data directory
with bare `os.WriteFile`, reachable from `POST /encryption/enable`.
Reproduced: a symlink at `.encryption-verify` survives the write and the
link's target is silently overwritten — neither of the sentence's two arms,
and a second app-managed exception where the README promised "the one
exception". The primary lane, stopped after the FAIL landed, had
independently reached the same migration.go writes — corroboration, not
dispute. Recorded observation from the same sweep: mcpsvc snapshot's
`Ensure` also bare-writes into an app-managed subdirectory (timestamped
names, so a pre-placed symlink is impractical); every other swept write path
(saveConfig, age identity, backup zip/meta/enabled-flag) is tmp+rename or
O_EXCL, consistent with the contract.

Three attempts failed on the same defect class: a completeness claim
("every write behaves like X", "the only exception is Y") that each new
sweep falsifies. Attempt 4, still docs-only under rulings 2026-08-26e/f,
stops claiming completeness: the README states that symlinks in the data
directory are NOT SUPPORTED — intent, matching atomicWrite's "not honoured"
comment — and describes both proven failure modes as possibilities
("can replace the link... or write straight through it") rather than
enumerating which path does which. Both "can" claims are behaviorally
proven; no exhaustiveness is asserted, so a fourth undiscovered bare-write
path cannot falsify the sentence. The migration.go write sites gain the
same out-of-contract comment HandlePlotly and SaveUserSettings carry.

RESOLUTION: single-arm doc-only run under ruling 2026-08-26e — no divergence
to resolve; verification confirms the diff is docs-only and the claims are
true against the code. Attempt 2 rescoped the README claim and documented the
out-of-contract SaveUserSettings path after the adversarial lane falsified
attempt 1's blanket sentence.
