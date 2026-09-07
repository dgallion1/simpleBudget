# Lead oracle calibration — before final attempt dispatch

Oracle: accept.sh executable, SHA256
df6a89f3c22325ea688a28d1fabf56781e4f4ce75e449c4a87abfec91ee3612f.
focus-transitions.cjs SHA256
a5731b42dceba25f4bfefc2993142f7eba0e76f3470cd5f96561505fd82c1239.
No report.md is used in this Tier-3 directory.

The missing final repair is tested on immutable DI5.2 source
/tmp/budget2-final-DI5.2.wv3Nf7 at isolated synthetic 127.0.0.1:18859.
Exact command, from the implementation worktree (rtk proxy bash wrapper):

    set -o pipefail
    DI5_SOURCE_ROOT=/tmp/budget2-final-DI5.2.wv3Nf7 DI5_BASE=http://127.0.0.1:18859 DI5_OUTPUT=/tmp/DI5-oracle-final-red bash .swarm/DI-run/tier3/DI5/accept.sh 2>&1 | tee .swarm/DI-run/tier3/DI5/calibration-final-red.log

Exit1, assertion failure on actual focus behavior, not a missing route/runtime.
All750 transition assertions ran over nine consumers, two themes, three widths.
138 fail: 24 surviving-action focus losses across main inner/outer replacements;
6 dismissal fallback focus losses after original input replacement;108 focused
action epoch-removal losses (both actions across all54 page/theme/width states).
All other transition assertions pass. No ORACLE PASS. The final red JSON is
calibration/red-focus-transitions.json. Full command output is preserved.

Passing end: disposable copy /tmp/DI5-oracle-prototype.wqAPPw with a temporary
focus-ownership prototype (actual cleanup capture, conditional remount focus,
owned-focus dismissal/reset return with main fallback). No application worktree
source was changed. Prototype JS hash before discard:
4456432b76760186663004cea616d980be2fc998a8092b968154ccb094a31ae5.

    set -o pipefail
    DI5_SOURCE_ROOT=/tmp/DI5-oracle-prototype.wqAPPw DI5_BASE=http://127.0.0.1:18860 DI5_OUTPUT=/tmp/DI5-oracle-final-prototype bash .swarm/DI-run/tier3/DI5/accept.sh 2>&1 | tee .swarm/DI-run/tier3/DI5/calibration-final-prototype.log

Exit0. All750 transitions pass, VM10/10 pass, six all-scripts-active F1
keyboard traces show zero fully covered controls (46/46/54 tabs per theme),
12 main/fallback layout states pass, six refresh lifecycle states pass (four
loads, confirmation refusal/acceptance, URL/query/hash, dirty/dismiss/restart).
Log final line is ORACLE PASS. Raw JSON/screenshots/AX snapshots are copied
under calibration/prototype. No real financial data was accessed.

Initial calibration logs are retained. They exposed an oracle fixture mistake:
HTML serialization used an old value attribute; explicitly carrying the fixture
unsaved value corrected it. The complete corrected oracle was rerun at both
ends above. These are oracle calibrations before dispatch, not worker attempts.

Both dedicated test processes (validated2760945/2763409) were stopped. The
temporary prototype JS was deleted via apply_patch, not adopted as production.
The worker must implement independently from the contract and failing tests.
The actual attempt3 result still requires a lead-run oracle.3.log and all
three named independent verifier verdicts before the gate can accept it.

Shutdown diagnostic: both calibration processes read the same pre-existing
synthetic fixture directory and used its temporary backup directory. Their
simultaneous shutdown snapshots collided on a timestamped temporary ZIP, so
shutdown backup warnings were logged. This did not fail the browser oracle or
mutate live data, but it is not backup verification. Subsequent author/checker
runs must use distinct copied synthetic data AND backup/import directories.
