#!/usr/bin/env python3
"""Positive AND negative control for bin/shadow-claim-guard.py.

WHY THIS FILE EXISTS. The guard reddens on a CONJUNCTION — an ungated warden
dispatch AND an unqualified universal sentence — so it has two ways to go quiet
without anybody noticing: narrow the sentence patterns, or widen the qualifier
patterns. Neither shows up as anything but a continued green. So both halves get
committed controls here, and the negative cases matter as much as the positive
ones: this guard's whole job is to tell the four sentences T-941e deleted apart
from the seven that say something similar and are TRUE. A guard that reddens on
both is worse than none — it would push someone to "fix" spec/lifecycle.md §4.1,
which is the one place in this tree that states this correctly.

FOUR CLASSES OF CASE, and a future edit must keep all four:

  * REVIVED  — the exact sentences T-941e deleted, replanted verbatim. Each must
               go red and must NAME its file:line ("went red" alone is satisfied
               by a guard reddening for an unrelated reason).
  * TRUE     — the sentences that survived T-941e because their scope is right.
               Each must stay GREEN. This is the false-positive control.
  * GATED    — an ungated-free tree plus a universal sentence: must stay GREEN.
               This proves the guard tracks the CODE and does not enshrine the
               2026-08-18 ruling as a permanent ban on the sentence.
  * HOLE     — a paraphrase the guard is documented NOT to catch. It must stay
               GREEN, and that green is committed here on purpose: the docstring
               claims this hole out loud, and a test that pins it keeps a later
               reader from mistaking the guard's silence for coverage.

Run: python3 bin/tests/shadow-claim-guard-selftest.py
"""

import re
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import List, Optional, Tuple

ROOT = Path(__file__).resolve().parents[2]
GUARD = ROOT / "bin" / "shadow-claim-guard.py"

# Every helper in DISPATCH_HELPERS appears here, and enqueueWorkerStop is the
# reason: it is the helper on the chain this whole ticket is about (the owner
# pressing stop on an outsource worker), and an earlier version of this file
# exercised only enqueueToWarden. Deleting enqueueWorkerStop from the guard left
# both the guard and this control GREEN — the one deletion that matters most was
# the one nothing here noticed.
UNGATED_GO = '''package ocserverd

func (s *apiServer) enqueueToWarden(memberID, warden string, frame []byte) bool {
	return true
}

func (s *apiServer) enqueueWardenFrame(memberID string, frame []byte) bool {
	return s.enqueueToWarden(memberID, "", frame)
}

func (s *apiServer) enqueueWorkerStop(target, workerID string) bool {
	return s.enqueueToWarden(workerID, target, nil)
}

func (s *apiServer) stopWorkerSessionOrPark(target, workerID string) bool {
	return s.enqueueWorkerStop(target, workerID)
}

func (s *apiServer) reconcileOne(memberID string) bool {
	return s.enqueueWardenFrame(memberID, nil)
}
'''

GATED_GO = '''package ocserverd

func (s *apiServer) enqueueToWarden(memberID, warden string, frame []byte) bool {
	return true
}

func (s *apiServer) enqueueWorkerStop(target, workerID string) bool {
	return s.enqueueToWarden(workerID, target, nil)
}

func (s *apiServer) stopWorkerSessionOrPark(target, workerID string) bool {
	if s.noReconcile {
		return false
	}
	return s.enqueueWorkerStop(target, workerID)
}
'''

# The sentences T-941e deleted, verbatim from cefc142. The fourth element is
# EVERY line the guard must flag, not merely one: a review found that asserting
# "the file was named" lets a whole pattern be deleted unnoticed whenever some
# OTHER line of the same block still matches. Dropping `\bEVERY\b` from the
# guard did exactly that here and this file stayed green.
REVIVED: List[Tuple[str, str, str, Tuple[int, ...]]] = [
    ("revived-api-machines", "server/ocserverd/api_machines.go",
     "\t// --no-reconcile is the shadow-deploy kill-switch over EVERY warden-command\n"
     "\t// dispatch (reconcileMemberNow / dispatchRobustStopNow posture) — a shadow\n"
     "\t// server must never command wardens, including this one-shot kick.\n", (1, 3)),
    ("revived-main", "server/ocserverd/main.go",
     "\t\t\t// disables the reconcile producer wholesale — cadence loop AND every\n"
     "\t\t\t// event-driven warden-command dispatch — so a shadow server can never\n"
     "\t\t\t// wake or kill a real agent. The rest of the server runs unchanged.\n", (2,)),
    ("revived-reconcile-header", "server/ocserverd/reconcile.go",
     "// --no-reconcile (serve flag) disables the producer WHOLESALE — the cadence\n"
     "// loop AND every event-driven warden-command dispatch — while the rest of the\n"
     "// server runs unchanged. This is the shadow-deployment kill-switch a shadow\n"
     "// server must never wake or kill a real agent.\n", (2, 4)),
    ("revived-api-stub", "server/ocserverd/api_stub.go",
     "\t// noReconcile is the --no-reconcile serve flag: disables the cadence loop\n"
     "\t// AND every event-driven warden-command dispatch (the shadow-deployment\n"
     "\t// kill-switch) while the rest of the server runs unchanged.\n", (2,)),
    ("revived-identity-sweep", "server/ocserverd/sweep.go",
     "// Deduped per identitySweepDedupeSecs. Gated OFF\n"
     "// wholesale by --no-reconcile (like every other warden-command dispatch).\n", (2,)),
    ("revived-offboard-doc", "docs/design/offboard-flow.md",
     "server 活著、但決策迴圈被整個關掉（`--no-reconcile`，shadow 部署用）——"
     "這時意圖寫得進去、presence 照跑、SSE 照送，**只是沒有人殺人、也沒有人重生**。\n", (1,)),
]

# The sentences that survived, verbatim. Every one of these is TRUE today.
TRUE_SENTENCES: List[Tuple[str, str, str]] = [
    ("true-spec-this-producer", "spec/lifecycle.md",
     "  passed. That flag is the shadow-deployment kill-switch for THIS producer: it disables it\n"
     "  WHOLESALE (the cadence loop AND every event-driven warden-command dispatch it owns), which\n"
     "  covers **every command this producer DISPATCHES** at a staff-member or warden row.\n"),
    ("true-spec-appendix", "spec/lifecycle.md",
     "   deployment of a second implementation (a shadow without it would spawn real agents), but\n"
     "   it is a deployment-mode flag, not part of the frozen production contract.\n"),
    ("true-cli-help", "server/ocserverd/main.go",
     '\tfmt.Fprintln(out, "  --no-reconcile   do not run the reconcile producer (no cadence loop,")\n'
     '\tfmt.Fprintln(out, "                   no warden-command dispatch) — the shadow-deploy kill-switch")\n'),
    ("true-serve-banner", "server/ocserverd/server.go",
     "\t// --no-reconcile (the shadow-deployment kill-switch — it also disables the\n"
     "\t// event-driven dispatch seams via api.noReconcile).\n"),
    ("true-no-outsource", "server/ocserverd/outsource_sched.go",
     "//   * --no-outsource (serve flag) disables the producer WHOLESALE — cadence\n"
     "//     AND the event-driven tick — the --no-reconcile mirror: a shadow server\n"
     "//     must never mint workers against the production queue.\n"),
    # Written FOR this control, not copied out of the tree. The five above all
    # happen to contain a SCOPED token already, so on their own they cannot tell
    # a working qualifier from a guard that never reddens on prose at all. These
    # three were each observed reddening a draft of the guard.
    ("true-zh-scoped", "docs/design/notes.md",
     "shadow server 永不由 reconcile producer 主動殺 agent，"
     "但 owner 手動停 worker 那條鏈不受這支旗標管。\n"),
    ("true-other-subject", "docs/design/notes2.md",
     "A shadow deployment must never be pointed at production data; "
     "--no-reconcile alone does not make that safe.\n"),
    ("true-explicitly-bounded", "server/ocserverd/notes.go",
     "// --no-reconcile gates EVERY dispatch inside reconcile.go, and nothing\n"
     "// outside it.\n"),
    ("true-cli-noun-phrase", "server/ocserverd/banner.go",
     '\tfmt.Fprintln(out, "[ocserverd] --no-reconcile: reconcile producer disabled '
     '(no cadence, no warden-command dispatch)")\n'),
]

# Documented holes. These MUST stay green; see the guard's docstring.
HOLES: List[Tuple[str, str, str]] = [
    ("hole-paraphrase", "docs/design/notes.md",
     "The rehearsal box is completely walled off from the real fleet — nothing it\n"
     "does can reach a production machine.\n"),
    ("hole-far-apart", "server/ocserverd/far.go",
     "// --no-reconcile is the shadow-deploy kill-switch.\n"
     "//\n"
     "//\n"
     "//\n"
     "// It covers EVERY warden-command dispatch.\n"),
]


# The guard also requires the two TRUE warnings to be present while an ungated
# dispatch exists (owner ruling, T-941e). Every fixture therefore has to carry
# them, or each case below would go red for a reason it is not testing. An extra
# aimed at one of these files APPENDS rather than replaces, so a case planting a
# revived sentence in api_stub.go still leaves the warning in place and reddens
# on the sentence alone.
WARNING_FILES = {
    "server/ocserverd/api_stub.go":
        "package ocserverd\n\n// A SHADOW SERVER WITH THIS FLAG SET STILL COMMANDS REAL WARDENS.\n",
    "docs/design/offboard-flow.md":
        "🔴 **演練站不是安全的沙盒**：命令會真的送到真的 warden。\n",
}


def stage(tmp: Path, go_src: str, extra: Optional[Tuple[str, str]] = None) -> Path:
    tree = tmp / "tree"
    (tree / "bin" / "tests").mkdir(parents=True)
    (tree / "server" / "ocserverd").mkdir(parents=True)
    (tree / "docs" / "design").mkdir(parents=True)
    (tree / "spec").mkdir(parents=True)
    (tree / "bin" / "shadow-claim-guard.py").write_text(
        GUARD.read_text(encoding="utf-8"), encoding="utf-8")
    (tree / "server" / "ocserverd" / "worker_spawn.go").write_text(go_src, encoding="utf-8")
    for rel, body in WARNING_FILES.items():
        (tree / rel).write_text(body, encoding="utf-8")
    if extra:
        rel, body = extra
        path = tree / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        if rel in WARNING_FILES:
            path.write_text(body + "\n" + WARNING_FILES[rel], encoding="utf-8")
        else:
            path.write_text(body, encoding="utf-8")
    subprocess.run(["git", "init", "-q"], cwd=tree, check=True)
    subprocess.run(["git", "add", "-A"], cwd=tree, check=True)
    return tree


def run(tree: Path) -> Tuple[int, str]:
    proc = subprocess.run(
        [sys.executable, str(tree / "bin" / "shadow-claim-guard.py")],
        capture_output=True, text=True)
    return proc.returncode, proc.stdout + proc.stderr


def main() -> None:
    failures: List[str] = []

    # GREEN FIRST on the real tree. If this is already red, every "must stay
    # green" case below would fail for the wrong reason and every "must go red"
    # case would pass for the wrong reason.
    code, out = run(ROOT)
    if code != 0:
        print("FAIL — the real tree is not green; every case below would be "
              f"meaningless:\n{out}", file=sys.stderr)
        sys.exit(1)

    with tempfile.TemporaryDirectory() as td:
        # The staged tree with no planted sentence must also be green, or the
        # positive cases would redden on the fixture rather than on the mutant.
        tree = stage(Path(td) / "base", UNGATED_GO)
        code, out = run(tree)
        if code != 0:
            failures.append(f"baseline fixture is not green:\n{out}")

    for name, rel, body, want_lines in REVIVED:
        with tempfile.TemporaryDirectory() as td:
            tree = stage(Path(td) / name, UNGATED_GO, (rel, body))
            code, out = run(tree)
            if code == 0:
                failures.append(f"{name}: must go RED, stayed green")
                continue
            missing = [n for n in want_lines if f"{rel}:{n} " not in out]
            if missing:
                failures.append(
                    f"{name}: went red but never named {rel}:{missing} — some "
                    f"OTHER line of the same block is carrying this case, so a "
                    f"pattern can be deleted without this control noticing:\n{out}")

    for name, rel, body in TRUE_SENTENCES:
        with tempfile.TemporaryDirectory() as td:
            tree = stage(Path(td) / name, UNGATED_GO, (rel, body))
            code, out = run(tree)
            if code != 0:
                failures.append(
                    f"{name}: must stay GREEN (this sentence is TRUE — reddening "
                    f"on it would push someone to break a correct doc):\n{out}")

    for name, rel, body in HOLES:
        with tempfile.TemporaryDirectory() as td:
            tree = stage(Path(td) / name, UNGATED_GO, (rel, body))
            code, out = run(tree)
            if code != 0:
                failures.append(
                    f"{name}: expected GREEN — this is a hole the guard's "
                    f"docstring claims out loud. It just started catching this "
                    f"shape, which is good news: delete this case AND the matching "
                    f"bullet in the docstring, do not weaken the guard.\n{out}")

    # The TRUE warning is protected the same way the false promise is banned:
    # deleting it is silent, and its absence is the state that let the false one
    # live unexamined for as long as it did.
    for rel in WARNING_FILES:
        with tempfile.TemporaryDirectory() as td:
            tree = stage(Path(td) / "warn", UNGATED_GO)
            (tree / rel).write_text("(the warning was removed)\n", encoding="utf-8")
            code, out = run(tree)
            if code == 0:
                failures.append(
                    f"warning-gone:{rel}: deleting the true warning must go RED while "
                    "an ungated dispatch still exists — an operator rehearsing on a "
                    "shadow station has nothing else telling them which buttons are live")
            elif rel not in out:
                failures.append(f"warning-gone:{rel}: went red without naming the file:\n{out}")

    # GATED: the same universal sentence, in a tree where nothing is ungated.
    with tempfile.TemporaryDirectory() as td:
        tree = stage(Path(td) / "gated", GATED_GO, REVIVED[0][1:3])
        code, out = run(tree)
        if code != 0:
            failures.append(
                "gated-tree: the sentence is TRUE once every dispatch reads the "
                f"flag, so the guard must go quiet by itself:\n{out}")

    # The helper list is the guard's whole view of (A). If a name stops matching
    # the tree — renamed, or quietly deleted from DISPATCH_HELPERS — the guard
    # keeps printing green over a smaller set, which is the one failure a
    # scanner cannot notice about itself.
    # Read the TUPLE, not the file text: every one of these names also appears
    # in the guard's prose, so an `in guard_src` check passes even after the name
    # has been deleted from the list the scan actually uses.
    guard_src = GUARD.read_text(encoding="utf-8")
    decl = re.search(r"DISPATCH_HELPERS = \(([^)]*)\)", guard_src)
    declared = set(re.findall(r"\"(\w+)\"", decl.group(1))) if decl else set()
    helpers = [h for h in ("enqueueWardenFrame", "enqueueToWarden", "enqueueWorkerStop")
               if h in declared]
    if len(helpers) != 3:
        failures.append(
            "DISPATCH_HELPERS no longer names all three warden-dispatch helpers "
            f"(found {helpers}). If one was renamed in the server, rename it here; "
            "do NOT drop it — enqueueWorkerStop in particular is the chain T-941e "
            "is about.")
    for helper in helpers:
        hits = subprocess.run(
            ["git", "-C", str(ROOT), "grep", "-c", f"s.{helper}(", "--",
             "server/ocserverd"], capture_output=True, text=True)
        if hits.returncode != 0 or not hits.stdout.strip():
            failures.append(
                f"{helper}: named in the guard but matches nothing under "
                "server/ocserverd — the guard is scanning for a name that no "
                "longer exists, so (A) is measured over less than it claims.")

    if failures:
        print("FAIL — shadow-claim-guard controls:\n  " + "\n  ".join(failures),
              file=sys.stderr)
        sys.exit(1)
    print(f"[shadow-claim-guard-selftest] all green ({len(REVIVED)} revived "
          f"sentences caught and named, {len(TRUE_SENTENCES)} true sentences left "
          f"alone, {len(HOLES)} documented holes still open, 1 gated-tree case)")


if __name__ == "__main__":
    main()
