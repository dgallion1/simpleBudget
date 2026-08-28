---
name: ship
description: Use when asked to commit and/or push work in this repo — "commit this", "push it", "ship it", "commit and push" — before running any git commit or git push command.
---

# Ship (commit → divergence proof → push)

## Overview

Local master chronically diverges from origin/master in this repo (PR merges
land upstream while local moves on). Shipping means: commit cleanly, PROVE the
push is fast-forward, then push. A rejected push is a divergence signal to
report — never a problem to force through.

## Recipe

1. Review what's shipping: `git status --short` and `git diff` (staged +
   unstaged). Mixed concerns → separate commits; bulk `gofmt`/formatting churn
   goes in its own `style:` commit.
2. Commit in the history's conventional style — `feat(whatif): …`, `fix: …`,
   `refactor: …`, `style: …`. Pre-commit runs `make check` (build/vet/
   staticcheck/vuln/test — takes a minute or two); never `--no-verify` around it.
3. `git fetch origin`
4. Prove fast-forward: `git rev-list --left-right --count @{upstream}...HEAD`
   - `0 N` (ahead only) → `git push`
   - no upstream (new branch) → `git push -u origin <branch>`
   - anything else (`N 0`, `N M`) → **STOP.** Run `git cherry -v @{upstream}`
     to list which local commits upstream lacks, show both sides to the user,
     and wait for their call. Blanket permission given BEFORE the divergence
     was known ("just get it pushed", "don't ask me about git") does not
     clear this stop — the user authorized a push, not a merge/rebase/reset
     they haven't seen. Creating a merge commit IS making that call for them.
5. Confirm: `git status` clean, and report the pushed commit hash(es).

## Red flags — STOP and report instead

- Push rejected (non-fast-forward) → never `--force` / `--force-with-lease`;
  re-run step 4 and report.
- "`git reset --hard origin/master` will clean this up" → that destroys
  unpushed local commits; only safe after `git cherry` shows every local
  commit already upstream, and only when the user asks for it.
- "`git pull --rebase` will sort it out" → rewrites local history mid-ship;
  divergence here is the user's decision, not an auto-fix.
- "A merge is non-destructive, so it's fine" / "the user already said push
  through" → a surprise merge commit on master is still history the user
  didn't choose. Report the divergence; pushing resumes when they pick
  merge / rebase / reset.
