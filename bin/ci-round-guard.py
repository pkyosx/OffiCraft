#!/usr/bin/env python3
"""lint-ci-round — the round list and the Makefile must name the same checks.

WHY THIS EXISTS (T-127)
-----------------------
Before T-127 the set of checks was written down twice — bin/ci.sh's OC_ROUND and
ten hand-typed argument lists in .github/workflows/ci.yml — and nothing compared
them. Three checks had already been missed that way. Not one of the three was
caught by a mechanism: each was noticed by a person, afterwards, by chance.
`lint-chat-pushdown` was still missing from every cloud cell on 0f84a859, so the
round that decides whether a change may land never ran it.

T-127 deleted the workflow's copy. The cells now ask for their own lane and
bin/lib/ci-round.txt is the only place a target is written down, so "the cloud
forgot to add it" is no longer a thing anyone can do.

WHAT IS LEFT, AND WHY IT NEEDS THIS FILE
----------------------------------------
One half of the mistake did NOT become impossible: a Makefile target can still be
added without a line in the round list. That is a different and much narrower
slip than the one T-127 removed, but it is the same shape — a check that exists
and runs nowhere — and it would be just as silent. This guard is the mechanism
that was missing for all three of the historical misses.

It takes the set difference BOTH WAYS, deliberately:
  * Makefile target with no round line  ⇒ a check that runs nowhere.
  * round line with no Makefile target  ⇒ a lane that will fail at `make` time,
    or worse, a name that quietly matches nothing.

WHY BOTH DIRECTIONS RATHER THAN "IS EVERYTHING COVERED"
-------------------------------------------------------
A one-way containment check passes over an empty set. If the round list were
emptied — or if this file's parser stopped finding targets for any reason — a
one-way check would report a clean green, because nothing uncovered was found.
Every count below is therefore asserted to be non-zero before any difference is
reported, and the denominators are printed on success.
"""
import json
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

# argv[1] overrides the root ONLY so bin/tests/ci-round-guard-selftest.py can
# point this guard at a mutated copy of the tree. Nothing in CI passes it, so the
# real round is always checked against the real Makefile.
ROOT = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else Path(__file__).resolve().parent.parent
ROUND = ROOT / "bin" / "lib" / "ci-round.txt"
MAKEFILE = ROOT / "Makefile"
CI_YML = ROOT / ".github" / "workflows" / "ci.yml"

# Gate jobs that legitimately have NO lane. macos-e2e is the real-browser round;
# it does not go through bin/run-checks.sh and never has.
#
# 🔴 THIS IS A LIST YOU HAVE TO EDIT ON PURPOSE, which is the point. Without it,
# "every gate job runs its lane" would have to be weakened to "gate jobs that
# happen to have a lane run it", and a NEW gate job that runs nothing at all
# would sail through — which is one keystroke away from the hole this guard
# exists to close. Same shape as auto-beta-guard's W1x exemption set.
EXEMPT_GATES = {"macos-e2e"}

# Command-position tokenisation, same rules as bin/tests/ci-run-checks-entrypoint-guard.sh.
SEPARATORS = re.compile(r"\|\||&&|[;|&]")
LEADERS = {
    "if", "then", "else", "elif", "fi", "do", "done", "while", "until", "case",
    "esac", "sudo", "time", "exec", "nohup", "command", "env", "set", "eval",
    "source", ".",
}
SHELLS = {"bash", "sh", "zsh"}
ASSIGN = re.compile(r"\A[A-Za-z_][A-Za-z0-9_]*=")

FAILURES: list[str] = []


def fail(msg: str) -> None:
    FAILURES.append(msg)


def read_round() -> list[tuple[int, str, str]]:
    rows = []
    # newline="" DISABLES universal-newline translation. Without it Python turns
    # \r\n into \n while reading, so the CRLF check below looks for a character
    # the reader already removed and passes on every file — the check would be
    # structurally incapable of failing. (Written that way first; caught by
    # running it against a real CRLF file and getting a green.)
    with ROUND.open(newline="") as fh:
        text = fh.read()
    # ⚠️ CRLF IS REFUSED, NOT TOLERATED, and the reason is that tolerating it
    # here is what created a divergence in the first place: Python's
    # .strip()/.split() drop a trailing \r silently, so this guard read a clean
    # `lint-ts` while bin/lib/ci-round.sh's awk read `lint-ts\r` and handed that
    # to make. The guard went GREEN and the round died later, on a target name
    # nobody could find in the Makefile. Found by an independent review, not by
    # anything here. The reader now strips \r so the round degrades rather than
    # exploding; this refusal is what makes someone fix the file.
    if "\r" in text:
        # split("\n"), NOT splitlines(): splitlines() breaks on \r as well, so
        # every piece it hands back is already \r-free and the list comes out
        # EMPTY — a refusal that names no line, which is barely a refusal.
        bad_lines = [str(i) for i, ln in enumerate(text.split("\n"), 1) if "\r" in ln]
        fail(
            f"{ROUND.name} contains carriage returns (CRLF) on line(s) {', '.join(bad_lines[:10])}"
            f"{' …' if len(bad_lines) > 10 else ''} — the round list must be LF-only, because a \\r "
            f"rides along with the target name into make while this guard never sees it."
        )
    for n, raw in enumerate(text.splitlines(), 1):
        line = raw.split("#", 1)[0].strip()
        if not line:
            continue
        parts = line.split()
        if len(parts) != 2:
            fail(f"{ROUND.name}:{n} is not `<lane> <target>`: {raw!r}")
            continue
        rows.append((n, parts[0], parts[1]))
    return rows


def read_makefile_targets() -> list[str]:
    # The same shape CONTRIBUTING.md tells a reader to use:
    #   grep -nE '^[a-z][a-z0-9-]*:' Makefile
    return [
        m.group(1)
        for m in (
            re.match(r"^([a-z][a-z0-9-]*):", line) for line in MAKEFILE.read_text().splitlines()
        )
        if m
    ]


def read_gate_jobs() -> list[str]:
    """Job ids in ci.yml that declare `# oc-job-role: gate`.

    Line-based on purpose: the hosted macOS runner has no PyYAML, and this guard
    must not be the reason a lane cannot be checked. The marker sits inside the
    job's own block, so the job id is the last 2-space key seen above it.
    """
    gates, current = [], None
    for line in CI_YML.read_text().splitlines():
        m = re.match(r"^  ([A-Za-z0-9_-]+):\s*$", line)
        if m:
            current = m.group(1)
            continue
        if re.match(r"^\s*#\s*oc-job-role:\s*gate\s*$", line) and current:
            gates.append(current)
    return gates


RUBY_DUMP = r"""
require 'yaml'
require 'json'
doc = begin
  YAML.safe_load(File.read(ARGV[0], mode: 'r:UTF-8'), aliases: false)
rescue => e
  warn "PARSE-ERROR #{e.class}: #{e.message}"
  exit 3
end
jobs = (doc && doc['jobs']) || {}
out = {}
jobs.each do |name, job|
  next unless job.is_a?(Hash)
  steps = job['steps'].is_a?(Array) ? job['steps'] : []
  out[name] = steps.map { |st| st.is_a?(Hash) ? st['run'].to_s : '' }
end
puts JSON.generate(out)
"""


def ruby_bin() -> str | None:
    return shutil.which("ruby")


def parse_workflow_jobs(path: Path, ruby: str) -> dict[str, list[str]] | None:
    """job id -> list of `run:` scripts, via ruby + psych.

    Line scanning is not good enough for this question. `echo bash
    bin/run-checks.sh --lane` and a real invocation differ only in COMMAND
    POSITION, and deciding that needs the step's script as the YAML actually
    parses it — a text scan sees the same tokens in both. PyYAML is not on the
    hosted macOS runner; ruby's psych is on both, which is why every other
    workflow-reading guard in this repo uses it.
    """
    r = subprocess.run([ruby, "-e", RUBY_DUMP, str(path)], capture_output=True, text=True)
    if r.returncode != 0:
        return None
    try:
        return json.loads(r.stdout)
    except json.JSONDecodeError:
        return None


def parser_is_real(ruby: str) -> bool:
    """Positive control on the parser itself.

    "The parser parsed it" is the premise of every assertion below, and a
    reviewer of another guard in this repo once demonstrated a FAKE `ruby` that
    handed a broken ci.yml full marks. So: invalid YAML must be REFUSED and a
    minimal valid workflow must be ACCEPTED, before any verdict here counts.
    """
    with tempfile.TemporaryDirectory(prefix="oc-ci-round-parser.") as tmp:
        bad = Path(tmp) / "bad.yml"
        bad.write_text("jobs:\n  a:\n   - [unclosed\n")
        if parse_workflow_jobs(bad, ruby) is not None:
            return False
        good = Path(tmp) / "good.yml"
        good.write_text("jobs:\n  a:\n    steps:\n      - run: echo hi\n")
        parsed = parse_workflow_jobs(good, ruby)
        return parsed == {"a": ["echo hi"]}


def lane_invocations(script: str) -> list[str]:
    """The lanes this `run:` script REALLY invokes, in command position.

    Returns one entry per genuine `bin/run-checks.sh --lane <x>` call. The lane
    is returned verbatim, including `${{ github.job }}`.

    ⚠️ WHY COMMAND POSITION IS THE WHOLE POINT: an independent review turned a
    gate cell into `echo bash bin/run-checks.sh --lane`, and both existing
    guards still counted it as routed — they asked whether the wrapper's NAME
    appeared among the tokens. It appeared. Nothing ran. That cell was green,
    its required status check was green, and the beta shipped on a commit whose
    Go checks never executed.
    """
    found: list[str] = []
    for line in script.splitlines():
        line = re.sub(r"#.*\Z", "", line)
        # A GitHub expression contains spaces, so plain whitespace tokenisation
        # shreds `${{ github.job }}` into three tokens and the lane argument
        # comes out as `${{`. Collapse it to one token FIRST, then tokenise.
        line = re.sub(r"\$\{\{\s*([^}]*?)\s*\}\}", lambda m: "${{" + m.group(1) + "}}", line)
        for seg in SEPARATORS.split(line):
            seg = re.sub(r"\A[({]\s*", "", seg.strip())
            toks = seg.split()
            while toks and (toks[0] in LEADERS or ASSIGN.match(toks[0])):
                toks.pop(0)
            if not toks:
                continue
            # The wrapper must be what is being EXECUTED: either the head token
            # itself, or the script argument of a shell. `echo …` fails both.
            if Path(toks[0]).name in SHELLS and len(toks) > 1:
                wrapper_idx = 1
            else:
                wrapper_idx = 0
            if not toks[wrapper_idx].endswith("bin/run-checks.sh"):
                continue
            args = toks[wrapper_idx + 1:]
            for i, a in enumerate(args):
                if a == "--lane" and i + 1 < len(args):
                    found.append(args[i + 1].strip("\"'"))
    return found


def check_every_lane_is_really_invoked(round_lanes: list[str], gates: list[str]) -> None:
    """Each lane's gate job must invoke THAT lane, exactly once, for real.

    Three separate ways this used to pass while nothing ran, all found by an
    independent review on a candidate this file had already declared green:
      * the `run:` line deleted outright — the cell kept only its checkout;
      * the line pointing at somebody else's lane;
      * the line wrapped in `echo`.
    A membership test (`is this lane the name of a gate job?`) sees none of them,
    because in all three the job still exists and still declares itself a gate.
    """
    ruby = ruby_bin()
    if not ruby:
        fail(
            "no ruby on PATH, so .github/workflows/ci.yml cannot be PARSED and "
            "'every lane is really invoked' cannot be answered. That is a FAILURE, not a skip: "
            "an unanswerable question must not read as a green one."
        )
        return
    if not parser_is_real(ruby):
        fail(
            f"the YAML parser ({ruby}) failed its own positive control — it accepted invalid YAML "
            f"or mangled a minimal valid workflow. No verdict about ci.yml below would mean anything."
        )
        return
    jobs = parse_workflow_jobs(CI_YML, ruby)
    if jobs is None:
        fail("ci.yml did not parse as YAML (GitHub would report a startup failure: zero jobs, no checks)")
        return

    for lane in sorted(set(round_lanes)):
        if lane not in jobs:
            fail(f"lane `{lane}` names no job in ci.yml at all")
            continue
        hits: list[str] = []
        for script in jobs[lane]:
            for got in lane_invocations(script):
                # `${{ github.job }}` IS this job's own id, which is the lane.
                hits.append(lane if got in ("${{github.job}}", "${{ github.job }}") else got)
        mine = [h for h in hits if h == lane]
        if not mine:
            others = sorted(set(hits))
            detail = f" it invokes {', '.join(others)} instead." if others else " it invokes bin/run-checks.sh nowhere."
            fail(
                f"gate job `{lane}` never really runs its own lane —{detail} "
                f"Its checks would silently not run while the cell still went green. "
                f"(A name inside `echo` does not count: the wrapper must be in command position.)"
            )
        elif len(mine) > 1:
            fail(f"gate job `{lane}` invokes its own lane {len(mine)} times; expected exactly once")

    for job in sorted(set(gates)):
        if job in round_lanes or job in EXEMPT_GATES:
            continue
        fail(
            f"gate job `{job}` has no lane in {ROUND.name} and is not in EXEMPT_GATES — "
            f"a required check that runs none of this repo's checks. Give it a lane, or add it to "
            f"EXEMPT_GATES in {Path(__file__).name} on purpose."
        )


def main() -> int:
    for path in (ROUND, MAKEFILE, CI_YML):
        if not path.is_file():
            print(f"[lint-ci-round] FAIL — missing {path}", file=sys.stderr)
            return 2

    rows = read_round()
    round_targets = [t for _, _, t in rows]
    round_lanes = [l for _, l, _ in rows]
    mk_targets = read_makefile_targets()
    gates = read_gate_jobs()

    # ── denominators first: a zero here makes every difference below vacuous ──
    if not round_targets:
        fail("the round list names no checks at all — every check below would pass over an empty set")
    if not mk_targets:
        fail("no named targets were found in the Makefile — the target scan is broken, not the Makefile")
    if not gates:
        fail("no job in ci.yml declares `# oc-job-role: gate` — the lane check would pass over an empty set")
    if FAILURES:
        for f in FAILURES:
            print(f"[lint-ci-round] FAIL — {f}", file=sys.stderr)
        return 1

    # ── (iii) a target may be assigned exactly once ──────────────────────────
    seen: dict[str, int] = {}
    for n, _, t in rows:
        if t in seen:
            fail(f"{t} is assigned twice ({ROUND.name}:{seen[t]} and :{n}); it would run twice and mean one lane owns it and another does too")
        seen[t] = n

    # ── (i) set difference BOTH ways ─────────────────────────────────────────
    missing_from_round = sorted(set(mk_targets) - set(round_targets))
    missing_from_makefile = sorted(set(round_targets) - set(mk_targets))
    for t in missing_from_round:
        fail(
            f"Makefile target `{t}` has no line in {ROUND.name}, so NOTHING runs it — "
            f"not the local round, not any cloud lane. Add a line `<lane> {t}`."
        )
    for t in missing_from_makefile:
        fail(
            f"{ROUND.name} lists `{t}` but the Makefile has no such target — "
            f"the lane that names it would fail at make time."
        )

    # ── (ii) every lane is a declared gate job ───────────────────────────────
    for lane in sorted(set(round_lanes)):
        if lane not in gates:
            fail(
                f"lane `{lane}` is not a job declaring `# oc-job-role: gate` in ci.yml — "
                f"its checks would run locally and in no cloud cell. Gate jobs today: {', '.join(sorted(gates))}."
            )

    # ── (iv) every lane is REALLY invoked by its own gate job ────────────────
    check_every_lane_is_really_invoked(round_lanes, gates)

    if FAILURES:
        for f in FAILURES:
            print(f"[lint-ci-round] FAIL — {f}", file=sys.stderr)
        return 1

    print(
        f"[lint-ci-round] ok — {len(round_targets)} checks, "
        f"{len(set(round_lanes))} lanes, all present in the Makefile ({len(mk_targets)} targets) "
        f"and every lane is one of {len(gates)} declared gate jobs, "
        f"each really invoking its own lane in command position"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
