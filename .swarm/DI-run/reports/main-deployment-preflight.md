# Main deployment preflight (not deployment evidence)

User explicitly authorized merge and deployment after completion. No main
restart, binary replacement, merge or push has occurred at this preflight.

Read-only observations:
- Main port 8080: PID 2290994, executable /home/darrell/bin/ai/budget2/budget2,
  cwd /home/darrell/bin/ai/budget2, command ./budget2; health commit
  v1.4.0-1095-gf5e68e0, status ok. Revalidate PID/executable immediately before
  stopping; do not rely on this recorded PID later.
- No explicit data/listen/debug/template/static/backup/import environment
  overrides in this process. Inspected config defaults: cwd/data, port8080,
  debug false. Data directory is git-ignored and has no tracked files.
- Existing launch is a detached nohup process, not an identified service unit.
  Preserve existing application environment without logging secret values.
- Main checkout is master at69e484a; origin/master last observed f5e68e0.
  Remote is https://github.com/dgallion1/simpleBudget.git. Recheck remote before
  merge/push; never force push. Hook path is the main repository .git/hooks.
- Main contains user-owned untracked tooling and overlapping run/spec files.
  Do not overwrite or remove these to make a merge work. Preserve overlapping
  untracked files in a dated backup before any authorized integration that
needs their paths, and disclose that move. Leave unrelated paths untouched.

2026-09-07 reopened-fix preflight: remote master remains
f5e68e0ba39f7f88e64b4417de1acdbd01114d45; main master69e484a still has no
tracked changes. Validated current listeners: main8080 PID2290994 and
demo8081 PID2538540. These observations are not authority to stop stale PIDs;
revalidate immediately at deployment. Main data is4.7M with no symlinks found.
Overlapping untracked main run evidence is28K and spec12K; preserve both before
fast-forward merge. Other untracked user paths remain untouched. The normal
pre-commit hook still runs make check and will not be bypassed. Worktree has
the pinned Tailwind executable cached. No merge/deployment has happened.

In-app browser runtime was retried and reports that trusted Node services
require process isolation unavailable with this sandbox's legacy Landlock.
Continue the established isolated headless browser tests for synthetic UI
verification; do not claim control of the user's existing browser tabs.

Release checklist:
1. DI4 and DI5 independent acceptance; full final tests, full-site a11y,
   self-review, gate done and stats. Gate-system smoketest rerun during this
   preflight exited0 with every suite ALL PASS; this is not product acceptance.
2. Stage only owned source/manifests/evidence/docs. Exclude generated ignored
   tmp build caches and unrelated user files; retain durable swarm evidence.
3. Commit with quality hook enabled, integrate into master and verify exact
   merged tree. Push normally only after verification succeeds.
4. Build a version-stamped reviewed binary without replacing the running
   executable in-place. Retain the current executable in a dated rollback
   directory, and safely snapshot real data/settings before restart without
   printing their contents. Synthetic demo files must never enter real data.
5. Revalidate target process, then stop only that main process and start new
   build from same main cwd with preserved configuration. Check health and
   commit identity, non-mutating Dashboard/Insights responses, refresh route,
   and pre/post saved-file hashes. Existing locked storage must stay locked;
   do not attempt to unlock or change credentials for verification.
6. If health/startup fails, restore the retained old binary and launch setup.
   Do not restore data blindly or erase intervening legitimate user changes.
7. Update demo separately against its own synthetic directory and verify its
   build/data identity. Report exact deployed commits and any remaining limits.
