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
# ⚠️ A REASON IS MANDATORY, and that is the whole difference between this and a
# bare allowlist. An independent review (Joey) showed the obvious attack: add a
# gate that runs nothing, then add its name here, and the guard goes quiet. That
# cannot be prevented outright — an exemption list that cannot be edited is not
# an exemption list. What CAN be done is make the edit expensive to write and
# impossible to hide: it is a change to a Python file (a reviewer sees it in the
# diff, unlike a line buried in a workflow cell), and the entry does not compile
# into an exemption unless someone writes down WHY in the same commit.
# A stale entry — one naming a job that is no longer a gate — is itself an error,
# so the list cannot quietly accumulate cover for jobs that no longer exist.
#
# THIS REMAINS A NAMED DESIGN BOUNDARY, not a closed door: anyone who can land a
# reviewed commit can widen it. It is called out in the PR description as such.
EXEMPT_GATES = {
    "macos-e2e": (
        "It runs the macOS end-to-end suite directly and has never gone through "
        "bin/run-checks.sh, so it owns no lane in the round list. Corroborated "
        "outside the repo: main's branch protection requires 11 contexts, the "
        "round list carries 10 lanes, and macos-e2e is the whole of the difference."
    ),
}

# Command-position tokenisation, same rules as bin/tests/ci-run-checks-entrypoint-guard.sh.
SEPARATORS = re.compile(r"\|\||&&|[;|&]")
LEADERS = {
    "if", "then", "else", "elif", "fi", "do", "done", "while", "until", "case",
    "esac", "sudo", "time", "exec", "nohup", "command", "env", "set", "eval",
    "source", ".",
}
SHELLS = {"bash", "sh", "zsh"}

# THE ONE FORM A GATE MAY ROUTE IN. Not a style rule — the only thing that makes
# "this cell really runs its lane" DECIDABLE.
#
# ⚠️ WHY EXACT EQUALITY AND NOT A TOKEN TEST. The previous rule asked whether the
# wrapper stood in COMMAND POSITION, judged line by line. An independent review
# (Lumi, on cc5f543c) wrapped the route in `if false; then … fi`: the wrapper is
# in command position on its own line, so the guard counted it as routed, said so
# in as many words, and the cell ran nothing. A line-by-line tokeniser CANNOT see
# control flow — `while false`, `case`, `[ 1 = 2 ] &&` are the same hole wearing
# other clothes, so blacklisting `if false` would only move it.
#
# Requiring the script to BE this string makes the whole family unwriteable
# rather than detectable, which is the criterion the owner set for this ticket.
# Measured at the time of writing: all ten gate cells already carry this line
# verbatim, so the rule needs no exception list at all.
#
# THE COST, STATED: a cell that one day genuinely needs an env prefix or a
# condition cannot get one by editing the workflow — someone must change THIS
# file on purpose. That is the intended trade, not an oversight.
CANONICAL_ROUTE = 'bash bin/run-checks.sh --lane "${{ github.job }}"'

# The only keys a gate job may carry. An ALLOWLIST for the same reason the route
# is pinned rather than inspected: it refuses the key nobody has thought of yet.
GATE_JOB_KEYS = {"runs-on", "steps", "timeout-minutes"}

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
  # WHOLE step mappings, not just each step's `run:` string. A step is a NODE:
  # `if:` beside a byte-perfect `run:` makes GitHub skip the step entirely, and
  # a guard that reads only `run` cannot see it (found by an independent review
  # on 83ab4b19, immediately after the `run:` string itself was pinned).
  out[name] = {
    'steps' => steps.map { |st| st.is_a?(Hash) ? st : {} },
    'job_keys' => job.keys,
  }
end
puts JSON.generate(out)
"""


def ruby_bin() -> str | None:
    return shutil.which("ruby")


def parse_workflow_jobs(path: Path, ruby: str) -> dict[str, dict] | None:
    """job id -> {'steps': [whole step mappings], 'job_keys': [...]}, via ruby + psych.

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
        # The control asserts the SHAPE the checks below rely on: whole step
        # mappings (so a sibling `if:` is visible) plus the job's own key list.
        # It is deliberately exact — when the dump grew from `run:` strings to
        # whole nodes, this control failed first and said so, which is the job
        # a positive control exists to do.
        good.write_text(
            "jobs:\n  a:\n    if: always()\n    steps:\n"
            "      - run: echo hi\n      - if: false\n        run: echo no\n"
        )
        parsed = parse_workflow_jobs(good, ruby)
        if not isinstance(parsed, dict) or set(parsed) != {"a"}:
            return False
        got = parsed["a"]
        return (
            got.get("steps") == [{"run": "echo hi"}, {"if": False, "run": "echo no"}]
            and set(got.get("job_keys", [])) == {"if", "steps"}
        )


def lane_invocations(script: str) -> list[str]:
    """The lanes this `run:` script names in COMMAND POSITION, line by line.

    ⚠️ READ WHAT THIS CAN AND CANNOT ANSWER. It is a SECOND layer, kept for depth
    and for naming what a broken cell invokes instead. It is NOT what decides
    "this cell really runs its lane" — it cannot be, because it judges each line
    on its own and therefore cannot see control flow: `if false; then <route>; fi`
    satisfies it (measured on cc5f543c, by two independent reviewers separately).
    The load-bearing rule is the CANONICAL_ROUTE equality above. Do not restore
    this function to that role.

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

        # (a) EXACTLY ONE `run:` step that IS the canonical route, character for
        #     character. This is what closes the control-flow family; the
        #     command-position test below is kept as a second, independent layer.
        steps = jobs[lane].get("steps", [])
        job_keys = set(jobs[lane].get("job_keys", []))

        # (0) THE JOB'S OWN KEYS, as an ALLOWLIST — deliberately not a list of
        #     banned ones. A blacklist only ever closes the key somebody already
        #     thought of: `if:` skips every step, `continue-on-error:` makes the
        #     cell green regardless, an empty `strategy.matrix` produces no
        #     instance at all, `needs:` on a skipped job skips this one too — and
        #     the next such key ships whenever GitHub decides. An allowlist
        #     refuses all of them, including the ones nobody has named yet.
        #     Measured: across all eleven gate jobs the keys in use are exactly
        #     these three, so this costs nothing today.
        #     ⚠️ NOT closed by this: `runs-on` naming a label no runner carries.
        #     That is an allowed key with a bad value, and it fails SAFE — the
        #     job never starts, so its required context never reports and the
        #     pull request cannot merge. A cell that runs nothing while going
        #     GREEN is the failure this guard exists for; one that never reports
        #     blocks by itself.
        stray_job_keys = sorted(job_keys - GATE_JOB_KEYS)
        if stray_job_keys:
            fail(
                f"gate job `{lane}` carries {', '.join('`' + k + ':`' for k in stray_job_keys)} at the "
                f"job level. A gate job may only carry {', '.join(sorted(GATE_JOB_KEYS))} — anything "
                f"else can stop the cell from running while it still reports success (`if:` skips it, "
                f"`continue-on-error:` discards the verdict, an empty `strategy.matrix` produces no "
                f"instance). If this job genuinely needs that key, add it to GATE_JOB_KEYS in "
                f"{Path(__file__).name} in the same commit, where a reviewer sees it."
            )

        # (1) EXACTLY ONE step that IS the route — and the step is judged as a
        #     WHOLE NODE, not by its `run:` string. `run` must be its only key:
        #     `if:` beside a byte-perfect `run:` skips the step, `env:`/`shell:`
        #     change what actually executes, `continue-on-error:` discards the
        #     verdict. Requiring the node's shape closes all of them at once
        #     instead of naming them one at a time.
        canonical = [
            st for st in steps
            if isinstance(st, dict)
            and str(st.get("run", "")).strip() == CANONICAL_ROUTE
            and set(st.keys()) == {"run"}
        ]
        route_text_only = [
            st for st in steps
            if isinstance(st, dict) and str(st.get("run", "")).strip() == CANONICAL_ROUTE
        ]
        if len(canonical) != 1 and len(route_text_only) == 1:
            extra = sorted(set(route_text_only[0].keys()) - {"run"})
            fail(
                f"gate job `{lane}`'s route step carries {', '.join('`' + k + ':`' for k in extra)} "
                f"beside its `run:`. The command is byte-perfect and still does not run as written: "
                f"`if:` SKIPS the step outright, `continue-on-error:` throws the verdict away, "
                f"`env:`/`shell:` change what executes. A gate's route step must carry `run:` and "
                f"nothing else. IF YOU MEANT THIS: the route's shape is pinned in "
                f"{Path(__file__).name} (CANONICAL_ROUTE and the key check beside it) — widen it "
                f"there, in the same commit, so the change is reviewed rather than buried in a "
                f"workflow cell."
            )
            continue
        if len(canonical) != 1:
            near = [str(st.get("run", "")).strip() for st in steps
                    if isinstance(st, dict) and "run-checks.sh" in str(st.get("run", ""))]
            if not near:
                detail = "it has no `run:` step mentioning bin/run-checks.sh at all."
            else:
                shown = near[0].replace("\n", "\\n")
                if len(shown) > 160:
                    shown = shown[:160] + "…"
                detail = (
                    f"it has {len(near)} step(s) that MENTION the wrapper but "
                    f"{len(canonical)} that ARE the route. Closest: `{shown}`"
                )
            fail(
                f"gate job `{lane}` does not route through the one permitted form — {detail} "
                f"A gate's route step must be EXACTLY `{CANONICAL_ROUTE}` and nothing else: "
                f"no condition, no `||`, no backgrounding, no extra lines. Anything else can be "
                f"green while the cell runs nothing (measured: `if false; then <route>; fi` passed "
                f"the older command-position test). Change {Path(__file__).name} on purpose if this "
                f"cell truly needs another shape."
            )
            continue

        # (b) And no OTHER step in the same job may mention the wrapper, so a
        #     decoy cannot sit beside the real one and confuse a later reader.
        strays = [str(st.get("run", "")).strip() for st in steps
                  if isinstance(st, dict) and "run-checks.sh" in str(st.get("run", ""))
                  and str(st.get("run", "")).strip() != CANONICAL_ROUTE]
        if strays:
            fail(
                f"gate job `{lane}` routes correctly but has {len(strays)} OTHER `run:` step(s) "
                f"mentioning bin/run-checks.sh; a second mention is either a dead decoy or a second "
                f"round nobody counted. Remove it."
            )

        hits: list[str] = []
        for script in [str(st.get("run", "")) for st in steps if isinstance(st, dict)]:
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
        if job in round_lanes:
            continue
        if job in EXEMPT_GATES:
            if not EXEMPT_GATES[job].strip():
                fail(
                    f"gate job `{job}` is in EXEMPT_GATES with an EMPTY reason. An exemption nobody "
                    f"had to justify is a silent channel; write why it owns no lane."
                )
            continue
        fail(
            f"gate job `{job}` has no lane in {ROUND.name} and is not in EXEMPT_GATES — "
            f"a required check that runs none of this repo's checks. Give it a lane, or add it to "
            f"EXEMPT_GATES in {Path(__file__).name} on purpose."
        )

    # An exemption for a job that is no longer a gate is dead cover: it stops
    # meaning anything, and the next gate to take that name inherits the silence.
    for job in sorted(set(EXEMPT_GATES) - set(gates)):
        fail(
            f"EXEMPT_GATES names `{job}`, which is not a gate job in ci.yml. A stale exemption is "
            f"cover for a job that no longer exists — remove it, or fix the name."
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
        f"each routing its own lane through the one permitted form (verified verbatim, not by shape)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
