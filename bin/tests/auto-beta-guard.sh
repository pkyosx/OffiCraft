#!/usr/bin/env bash
# bin/tests/auto-beta-guard.sh — guards the AUTOMATIC BETA path (T-9fe3): the
# auto-beta job in .github/workflows/ci.yml, and the version rule in
# bin/next-beta-tag that feeds it.
#
# WHY A WORKFLOW FILE NEEDS A GUARD AT ALL
# ---------------------------------------
# ⚠️ NOTHING ELSE IN THIS REPO PARSES .github/workflows/*.yml. That is not a
# theoretical gap: a change to this workflow has already gone green through every
# local gate and then produced a GitHub STARTUP FAILURE — zero jobs, no checks
# run, and a commit list that looks exactly like a commit nobody pushed. bin/ci.sh
# cannot catch it because it never reads the file. So the first assertion here is
# simply "it parses", and the parser is a HARD dependency: a guard that skips when
# it cannot find a YAML parser reports the same silence as a guard that found no
# problems.
#
# WHY ruby AND NOT PyYAML
# The hosted macOS runner this suite runs on has NO PyYAML, and this file is
# dispatched by bin/tests/run.sh, which `make test-bin-guards` runs there. Ruby with
# its bundled psych IS present on macOS (/usr/bin/ruby) and on the dev machine, so
# it is the one parser both environments actually have. It is resolved by absolute
# path fallback for the same reason bin/ci.sh resolves go/npm/gitleaks that way —
# a minimal-PATH caller must not be able to turn this into a skip.
#
# 🔴 "NO PARSER FOUND" IS A FAILURE ON PURPOSE. DO NOT TURN IT INTO A SKIP.
# It is the same discipline bin/ci.sh applies to gitleaks and npm: an unrun check
# and a passing check are indistinguishable from the outside, so absence has to be
# loud. If you are here because this reddened on some machine, install ruby — the
# failure message names what is missing and how — rather than making the guard
# tolerate its own absence. A tolerant version of this line would have let the
# startup failure described above through silently, which is the whole reason the
# assertion exists.
# ⚠️ What is NOT claimed anywhere here: that any particular hosted runner image
# has ruby. That is answered by the cloud round itself, not by a local assumption.
#
# WHAT THE ASSERTIONS ARE AIMED AT
# Each one is a way the automatic path could go quietly wrong rather than loudly:
#   W1  a `needs` list that falls behind a newly added gate — the job would then
#       publish a commit only SOME of the checks passed. Written as a set
#       DIFFERENCE in both directions, deliberately not as a list to compare
#       against: an enumeration in the test is a second copy of the answer, and it
#       goes stale in exactly the same way the one in the workflow does.
#       ⚠️ The set differenced against is the GATE jobs, not "all other jobs".
#       That changed with T-5d3b's notify-main-red, and the reason is not taste:
#       notify runs only when a round FAILED, so an auto-beta that needed it would
#       never run — while notify needs auto-beta (a publish failing on main is the
#       silence this whole ticket is about). Requiring both would be a `needs`
#       cycle, which GitHub answers by scheduling ZERO jobs. What W1 is actually
#       protecting is unchanged and still complete in both directions: a gate
#       missing from `needs` reddens, and so does a `needs` entry that is not a
#       gate.
#   W1r THE CLASSIFICATION ITSELF, because W1 is only worth as much as it. A
#       hardcoded list of which jobs are gates would be the same stale second copy
#       W1 refuses to be, so each job DECLARES its role in a marker comment and
#       this asserts every job has exactly one readable marker. A job it cannot
#       classify is a FAILURE THAT NAMES THE LINES — never a silent default into
#       either bucket. ("I could not read it" printed as "nothing to report" is
#       the one thing this file's header has argued against throughout.)
#       The marker is also corroborated so it cannot be a free pass: a not-a-gate
#       must pin itself to refs/heads/main (it produces no verdict a pull request
#       could ever see), and a gate must NOT — otherwise hanging an `if` on a real
#       gate would relabel it out of the required set, which is exactly the bypass
#       W1 exists to stop.
#   W1x THE EXEMPTION ROLL-CALL. W1r stops a job being mislabelled; it does not
#       stop one being honestly labelled a non-gate and thereby leaving the set
#       auto-beta waits for. AGENTS.md's land criteria answer that with a ruling —
#       the exemption list is a hardcoded roll-call and a third member needs its
#       own owner ruling — and this is what makes that sentence a mechanism rather
#       than prose. The enumeration is the point here, not a smell: it cannot go
#       stale in silence, because the only way past it is to edit it.
#       ⚠️ Corroboration on the GATE side is an ALLOWLIST, not a ref spelling: a
#       gate may carry no job-level `if`, or one that is exactly on a roll-call in
#       this file. Matching one literal spelling was a hole — `github.ref_name ==
#       'main'` and `startsWith(github.ref, …)` both took a real gate off pull
#       requests while this stayed green — and the first fix for that (ban every
#       job-level `if`) reddened `draft != true` too, which is not a bypass.
#       It also refuses `continue-on-error` on ANY job: that field makes a failing
#       job report success outward, which satisfies auto-beta's `needs` AND makes
#       notify-main-red's `failure()` false. Nothing here used to read it.
#   W2  the `if` compared EXACTLY against the canonical condition. Asking whether
#       both halves appear was a hole: `(<canonical>) || github.event_name ==
#       'pull_request'` contains both substrings and puts the only job holding
#       `contents: write` on pull requests, publishing unmerged code as a real
#       prerelease.
#   W3  `contents: write` leaking to another job or to the workflow default: the
#       three gate jobs run untrusted fork code and must not be able to write.
#   W4  GA becoming automatic. The beta→final flip is a human decision (owner
#       ruling), so nothing under `.github/` may even NAME that subcommand — the
#       scope is the whole directory, not just `workflows/`, because a composite
#       action under `.github/actions/` is not a workflow file and slipped past.
#       W4d then refuses the REFERENCE rather than a location, because a
#       composite action kept OUTSIDE `.github/` walked past both the grep scope
#       and the earlier directory ban. It refuses a `uses:` pointing at this
#       repo's own code either by relative path (`./…`, `../…`) or by naming this
#       repo in the `{owner}/{repo}/{path}@{ref}` form, and it reads the PARSED
#       document rather than lines — a value written on the following line is
#       legal YAML and no line-oriented grep can ever see it. Both of those
#       spellings were live bypasses against the previous version.
#       W4pc / W4dpc / W4dpf / W4dtpc prove those scans match planted examples
#       (and leave real third-party actions alone) rather than reporting an empty
#       result for free.
#       ⚠️ W4d is NOT a proof that a local action cannot be reached. What it does
#       not cover is listed beside the assertion itself.
#   W5  the publish call losing --no-settle (every unattended run would then fail
#       on a station it cannot reach) or losing --target (it would publish
#       whatever main points at when the runner starts, not the commit that was
#       checked).
#   W6  a shallow checkout: `git worktree add --detach <sha>` and the tag
#       comparison both need real history.
#   W7  the `on:` block, which NOTHING here used to read: a filter there
#       (`paths-ignore: ['**']`) makes every gate skip on every pull request
#       without touching a job, and every assertion above stays green. Same shape
#       as the gate-`if` roll-call: the VALUE of each canonical trigger is pinned
#       verbatim, while a short allowlist of trigger KEYS may be added without
#       editing this guard. Pinning the whole mapping instead reddened for
#       `workflow_dispatch`, which is a legitimate edit.
#       ⚠️ AN EARLIER VERSION OF THIS COMMENT SAID `on:` IS WHAT DECIDES WHETHER A
#       GATE IS SCHEDULED, "not any field inside a job". That is false, and it is
#       measurable in this very file: grep it for `concurrency` → 0 hits,
#       `strategy` → 0 hits (`needs` → 21, the positive control that says the
#       query works). WHAT THIS FILE READS is exactly: `on:` (W7), job-level `if`
#       (W1/W1r/W2), `needs` (W1), `permissions` (W3), job-level
#       `continue-on-error`, and the `# oc-job-role:` markers. THREE WAYS A GATE
#       CAN END UP WITH NO VERDICT THAT NOTHING HERE WATCHES, listed because the
#       list being written down is the only thing standing in for a check:
#         • a gate whose `needs` points at a job that is skipped on pull requests
#           — the dependent skips with it;
#         • `strategy.matrix` with an `exclude` that reduces a gate to zero jobs;
#         • `concurrency` — ci.yml pins `group: ci-${{ github.ref }}` with
#           `cancel-in-progress: false` on main, and the pending slot holds ONE
#           run, so a displaced trunk commit gets conclusion=cancelled: no
#           verdict, and no beta.
#       None of the three is tested on GitHub by anyone; the first two are not
#       tested at all, and the third is read off ci.yml's own comment. Whether
#       this guard should grow to cover them is a decision outside this pack.
#   N*  the version rule itself, driven hermetically with fabricated tag lists.
#
# HERMETICITY: nothing here runs a workflow, contacts GitHub, or invokes gh/git
# tag-mutating commands. The N cases `source` bin/next-beta-tag (its dispatch is
# guarded on "am I being executed") and call its functions with tag lists on
# stdin, so they test the REAL rule rather than a restatement of it.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
WF_DIR="$ROOT/.github/workflows"
WF="$WF_DIR/ci.yml"
NBT="$ROOT/bin/next-beta-tag"
JOB="auto-beta"
ROLE_MARKER="oc-job-role"

PASS=0; FAIL=0; UNCORROB=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ # check DESC EXPECTED ACTUAL
  if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi
}
# A THIRD outcome, and the reason it exists is narrow enough to state exactly:
# one assertion here (W4ds) corroborates a hardcoded constant against evidence
# that a legitimate checkout may simply not carry. "Could not run" is neither a
# pass nor a violation, and the two previous shapes were both wrong for it: ok()
# prints a check that never happened as a check that passed, and bad() refuses a
# working copy that has done nothing wrong (it refused the SUPPORTED concurrent-CI
# flow — see W4ds). So it gets its own line, its own counter, and a summary that
# names it; what it does NOT get is a contribution to the exit status.
# ⚠️ USE THIS FOR "the evidence is absent", NEVER for "the evidence disagrees".
# An assertion whose evidence is present and says no is a bad(). If you find
# yourself reaching for this to quiet a red, that red is the finding.
uncorrob(){ UNCORROB=$((UNCORROB+1)); printf '  ⚠️ NOT CORROBORATED — %s\n' "$1"; }
summary() { # summary EXTRA_FAILS
  local f=$((FAIL + ${1:-0}))
  if [[ "$UNCORROB" == "0" ]]; then
    echo "auto-beta guard: $PASS ok, $f failed"
  else
    echo "auto-beta guard: $PASS ok, $f failed, $UNCORROB not corroborated (NOT passes — read the ⚠️ line(s) above)"
  fi
}
fatal() { printf '  FAIL — %s\n' "$1"; summary 1; exit 1; }

[[ -f "$WF" ]] || fatal "$WF is missing — the automatic beta path cannot exist without it"
[[ -f "$NBT" ]] || fatal "$NBT is missing — the auto-beta job calls it to compute the tag"

echo "auto-beta path guard (workflow shape + version rule; no network, no gh, no workflow run)"

# ── the parser, fail-closed ─────────────────────────────────────────────────
# OC_AUTO_BETA_RUBY is a TEST SEAM: pointing it at a nonexistent path is how the
# "no parser is a FAILURE, never a skip" branch below gets exercised for real.
#
# ⚠️ IT IS NOT HARMLESS, AND AN EARLIER VERSION OF THIS COMMENT CLAIMED IT WAS.
# It said the seam "cannot weaken the guard — anything that is not a working YAML
# parser produces unusable JSON". A reviewer disproved that in one go: a fake
# `ruby` that ignores its input and writes a hand-made JSON blob to the output
# path let a workflow file of literal garbage score 19 ok / rc=0. So the seam CAN
# hand this guard a lie, and the honest statement is narrower: nothing in any CI
# path sets it, and the positive control below makes a fake parser have to work at
# being convincing rather than merely exist.
RUBY="${OC_AUTO_BETA_RUBY:-}"
if [[ -z "$RUBY" ]]; then
  RUBY="$(command -v ruby 2>/dev/null || true)"
  if [[ -z "$RUBY" ]]; then
    for cand in /usr/bin/ruby /opt/homebrew/bin/ruby /usr/local/bin/ruby; do
      [[ -x "$cand" ]] && { RUBY="$cand"; break; }
    done
  fi
fi
if [[ -z "$RUBY" || ! -x "$RUBY" ]]; then
  NO_PARSER_MSG='no YAML parser found, so .github/workflows/ci.yml cannot be PARSED.
    WHAT IS MISSING: a ruby executable. Looked for: `ruby` on PATH, then
      /usr/bin/ruby (Apple system ruby, present on every macOS), then
      /opt/homebrew/bin/ruby, then /usr/local/bin/ruby.
    HOW TO FIX: install ruby (macOS ships one at /usr/bin/ruby; Homebrew:
      `brew install ruby`; Debian/Ubuntu: `apt-get install -y ruby`). psych is
      bundled with ruby, so nothing else is needed. PyYAML is deliberately NOT
      used here — the hosted macOS runner does not have it.
    WHY THIS IS A FAILURE AND NOT A SKIP (deliberate — do not "fix" it by
      skipping): nothing else in this repo parses a workflow file. A skip and a
      clean parse produce the same silence, and what that silence hides is a
      GitHub startup failure — zero jobs, no checks run, and a commit list that
      looks exactly like a commit that passed. That has already happened once.'
  fatal "$NO_PARSER_MSG"
fi

WORK="$(mktemp -d -t oc-auto-beta-guard.XXXXXX)"
trap 'rm -rf "$WORK" 2>/dev/null || true' EXIT
JSON="$WORK/ci.json"

# parse_yaml <in.yml> <out.json> — the ONE parser invocation, used by both the
# real parse and the positive control below. psych is YAML 1.1, so the `on:` key
# comes back as the boolean true; nothing here needs it and the JSON dump just
# carries it as the string "true".
# ⚠️ ONE PROPERTY OF `safe_load` WORTH KNOWING BEFORE IT SURPRISES SOMEBODY: Psych's
# `safe_load` defaults to `aliases: false`, so a YAML anchor/alias (`&x` / `*x`)
# anywhere in the file makes the whole parse RAISE — which lands as W0 "does not
# parse" and fails. That is the fail-CLOSED direction (an aliased workflow cannot
# slip past unexamined), and it is worth stating because a legitimate edit using an
# anchor would be refused here with a message about invalid YAML rather than about
# aliases. ⚠️ READ OFF THE PSYCH DOCUMENTATION, NOT VERIFIED WITH A FIXTURE HERE —
# nobody has fed this guard an anchored workflow to watch what it says.
parse_yaml() {
  "$RUBY" -ryaml -rjson -e '
    doc = YAML.safe_load(File.read(ARGV[0]))
    abort "top level is not a mapping" unless doc.is_a?(Hash)
    abort "no jobs mapping" unless doc["jobs"].is_a?(Hash)
    File.write(ARGV[1], JSON.dump(doc))
  ' "$1" "$2" 2>"$WORK/parse.err"
}

# ── W0pc: the parser is REAL — a positive control on the tool itself ─────────
# Every other assertion in this file is downstream of one belief: that whatever
# answered as the parser actually parsed the file. A reviewer broke exactly that
# belief — a fake `ruby` that ignores its input and writes a hand-made JSON blob
# scored 19 ok / rc=0 against a ci.yml containing literal garbage. "It resolved to
# something executable" is not the same claim as "it parses YAML".
#
# So: feed it a document that is definitively NOT valid YAML and require it to
# REFUSE. A parser that accepts this is not a parser, and every verdict it would
# have produced about the real workflow is worthless — hence fatal, not a bad().
# This has to come BEFORE the real parse: an unreal parser must not get the chance
# to answer questions about ci.yml at all.
printf '%s\n' 'this: is: not: valid: yaml: [' '  "unterminated' > "$WORK/invalid.yml"
if parse_yaml "$WORK/invalid.yml" "$WORK/invalid.json"; then
  fatal "W0pc the YAML parser ($RUBY) ACCEPTED a file that is not valid YAML. It is not really parsing, so no verdict in this guard about .github/workflows/ci.yml means anything. (A fake parser behind OC_AUTO_BETA_RUBY looks exactly like this.)"
else
  ok "W0pc the parser REJECTS invalid YAML (it is really parsing, not rubber-stamping)"
fi
# …and the other half of the control: it must ACCEPT a minimal valid workflow.
# Without this, "rejects everything" would pass W0pc and then fail W0 with a
# message blaming ci.yml for a broken parser.
printf '%s\n' 'name: t' 'jobs:' '  a:' '    runs-on: ubuntu-latest' > "$WORK/valid.yml"
if parse_yaml "$WORK/valid.yml" "$WORK/valid.json"; then
  ok "W0pc …and ACCEPTS a minimal valid workflow (it is not simply refusing everything)"
else
  fatal "W0pc the YAML parser ($RUBY) rejected a MINIMAL VALID workflow, so a red W0 below would be the parser's fault and not ci.yml's: $(tr '\n' ' ' < "$WORK/parse.err")"
fi

if parse_yaml "$WF" "$JSON"; then
  ok "W0 .github/workflows/ci.yml PARSES as YAML and carries a jobs mapping"
else
  fatal "W0 .github/workflows/ci.yml does NOT parse (GitHub would report a startup failure: zero jobs, no checks): $(tr '\n' ' ' < "$WORK/parse.err")"
fi

# q — ask one question of the parsed workflow. Each assertion is its own tiny
# query so a red names one property rather than "something in the job is wrong".
q() { JOB="$JOB" python3 "$WORK/q.py" "$JSON" "$@"; }
cat > "$WORK/q.py" <<'PY'
import json, os, re, sys

doc = json.load(open(sys.argv[1]))
what = sys.argv[2]
jobs = doc.get("jobs") or {}
job = os.environ["JOB"]
me = jobs.get(job) or {}

def norm(s):
    # Collapse shell line continuations and whitespace runs so a `run:` block
    # split over several lines compares as the one command it is.
    return re.sub(r"\s+", " ", str(s).replace("\\\n", " ")).strip()

DQUOTE_MARK = "DOUBLE-QUOTED-LITERAL!"

def dq_as_delimiter(expr):
    """Is there a double quote WHERE A STRING DELIMITER GOES, i.e. outside every
    single-quoted literal?

    🔴 THE FIRST VERSION ASKED `'"' in expr` AND THAT WAS A FALSE RED WE SHIPPED.
    An independent review found it: in an Actions expression a single-quoted literal
    may contain arbitrary characters, so

        if: contains(github.event.head_commit.message, '"skip-ci"')

    is a legal condition — the double quotes are ordinary characters inside the
    literal, not delimiters — and the character test refused it (measured at
    43 ok / 2 failed). That is the exact failure this whole round is treating: a
    guard that reddens for a legal edit teaches people to route around the guard.
    So the question is not "is there a double quote" but "is there one in delimiter
    position".

    ⚠️ WHAT IS AND IS NOT MEASURED HERE. That the DELIMITER form kills the whole
    workflow is measured on GitHub (see flag_dquotes). That the INSIDE-A-LITERAL
    form is fine is NOT measured on GitHub: it is read off the expression grammar,
    and what the review actually measured was that this guard refused it — not what
    GitHub does with it. Stated that way on purpose; this ticket has a history of
    claims outrunning their evidence.

    A single quote inside a single-quoted literal is written by doubling it (''),
    which is why the inner loop consumes pairs rather than closing on them.
    """
    s = str(expr)
    i, n = 0, len(s)
    while i < n:
        c = s[i]
        if c == "'":
            i += 1
            closed = False
            while i < n:
                if s[i] == "'":
                    if i + 1 < n and s[i + 1] == "'":
                        i += 2
                        continue
                    i += 1
                    closed = True
                    break
                i += 1
            if not closed:
                # UNTERMINATED literal: we cannot say where it would have ended, so
                # we cannot say which quotes are delimiters. Fall back to the
                # conservative answer for the remainder rather than treating an
                # unclosed literal as swallowing everything after it — otherwise
                # `'oops && x == "push"` would hide a real delimiter quote.
                return '"' in s[i:] or '"' in s
            continue
        if c == '"':
            return True
        i += 1
    return False

def flag_dquotes(cond):
    """Prefix a marker when the condition uses a double quote as a delimiter.

    🔴 THIS USED TO REWRITE `"push"` INTO `'push'` AND COMPARE THEM AS EQUAL. That
    was wrong, and it was wrong in the expensive direction. The belief was that the
    two spellings are the same condition and that reddening for a no-op rewrite
    teaches people to edit the guard until it is quiet (a real hazard, measured
    once at 36 ok / 1 failed). Then it was measured ON GITHUB, two minimal
    workflows differing only in the quotes:
      `if: github.event_name == 'push'`  → the run happened, `name:` resolved.
      `if: github.event_name == "push"`  → STARTUP FAILURE, `jobs: []`, zero jobs,
                                           run named after the file path because
                                           `name:` was never read.
    So the double-quoted form is not a rewrite of the condition; it is a workflow
    that never starts, and on a pull request that shows up as NO checks — not a red
    one, which is strictly worse to notice. Refusing it was RIGHT; the "false red"
    was a true red. What the guard owes that case is not tolerance but an
    explanation, which is why this returns a marker the caller turns into a message
    naming the startup failure.

    ⚠️ Whitespace and the outer `${{ }}` are still normalised (those really are
    the same condition either way). Nothing else is forgiven: an added clause, a
    different operator, a negation, a renamed context all still redden, because
    everything else is compared byte for byte.

    ⚠️ DELIMITER POSITION ONLY — see dq_as_delimiter. A double quote INSIDE a
    single-quoted literal (`contains(msg, '"skip-ci"')`) is an ordinary character
    and is not flagged; an earlier version flagged it and that was a false red of
    our own making.
    """
    return (DQUOTE_MARK + cond) if dq_as_delimiter(cond) else cond

def unwrap(cond):
    """Strip the optional outer `${{ }}` — both spellings are legal and equal."""
    m = re.fullmatch(r"\$\{\{\s*(.*?)\s*\}\}", cond)
    return m.group(1) if m else cond

def publish_steps():
    return [s for s in (me.get("steps") or [])
            if "bin/release publish" in str(s.get("run", ""))]

def publish_runs():
    return [norm(s.get("run", "")) for s in publish_steps()]

def resolves_to(step, token, wanted):
    """Does `token`, as written in a step's `run:`, carry the value `wanted`?

    Accepts BOTH plumbings, because they are the same claim about the value and a
    guard that hardcodes one of them turns the other into a red — which is how a
    guard ends up blocking its own hardening. Interpolated directly into the run
    body (`${{ github.sha }}`), or passed through the step's env (`"$OC_SHA"`
    where env.OC_SHA is `${{ github.sha }}`). Anything else — a branch name, a
    ref expression, an env var bound to something other than `wanted` — is not."""
    token = unquote(token.strip())
    if token == wanted:
        return True
    m = re.fullmatch(r'\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?', token)
    if not m:
        return False
    return str(((step.get("env") or {}).get(m.group(1), ""))).strip() == wanted

def unquote(tok):
    if len(tok) >= 2 and tok[0] == tok[-1] and tok[0] in "\"'":
        return tok[1:-1]
    return tok

def flag_value(cmd, flag):
    """The value token following `flag` in a normalised command line, or ''.

    `${{ … }}` is ONE token even though it contains spaces — an earlier version
    split on whitespace, so `--target ${{ github.sha }}` came back as the string
    `${{`. That made the direct-interpolation shape fail W5, i.e. the assertion
    accepted only the env plumbing, which is the same one-shape-hardcoded defect
    in the other direction. Caught by the mutant that reverts to interpolation and
    must still PASS."""
    m = re.search(re.escape(flag) + r"\s+(\$\{\{.*?\}\}|\"[^\"]*\"|'[^']*'|\S+)", cmd)
    return m.group(1) if m else ""

if what == "job-present":
    print("yes" if job in jobs else "no")

elif what == "if-raw":
    # THE WHOLE CONDITION, normalised only for whitespace and for the optional
    # outer `${{ }}` wrapper (both really are the same condition either way).
    # Compared VERBATIM by the caller. A double-quoted literal is NOT normalised
    # into the single-quoted spelling — it comes back marked, so the caller can
    # say why it is refused; see flag_dquotes for what was measured on GitHub.
    #
    # ⚠️ IT USED TO ASK "are these two substrings present?" AND THAT WAS A HOLE.
    # A reviewer turned the condition into
    #   (github.event_name == 'push' && github.ref == 'refs/heads/main') || github.event_name == 'pull_request'
    # and the guard stayed 35 ok / 0 failed: both substrings were still there, and
    # nothing looked at the BOOLEAN STRUCTURE around them. That mutant makes the
    # one job holding `contents: write` run on pull requests, so an unmerged branch
    # publishes a real prerelease. Substring presence cannot see a disjunction
    # bolted on beside it; an exact comparison can, and it also reddens for `||`,
    # for a third clause, and for a negation. If you need auto-beta to run on
    # another trigger, change WANT_IF in this guard IN THE SAME COMMIT — that edit
    # is the deliberate act this assertion exists to force.
    print(flag_dquotes(unwrap(norm(me.get("if", "")))) or "-")

elif what == "on-shape":
    # ── THE TRIGGERS THEMSELVES, which NOTHING in this file used to read ───────
    # Every gate assertion above is downstream of one unexamined belief: that the
    # gate jobs run on a pull request at all. They are filtered by the `on:`
    # block, not by anything inside a job, so a reviewer put
    # `paths-ignore: ['**']` under `on.pull_request` and every gate stopped
    # producing a verdict on every pull request while this guard reported
    # 37 ok, 0 failed. That is the same silence as a skipped gate — a check-runs
    # list with no entry looks exactly like a check that did not go red — and it
    # is reached without touching a single job.
    #
    # ── KEY ALLOWLIST, VALUES PINNED VERBATIM — the same shape as the gate-`if`
    # roll-call, and deliberately so. The first version of this pinned the WHOLE
    # `on:` mapping byte for byte, and a reviewer showed what that costs: adding
    # `workflow_dispatch:` — a legitimate edit that ci.yml's own comments say is
    # expected — reddened at 38 ok / 1 failed. That is the identical mistake the
    # gate-`if` rule had already made and already backed out of ("a rule that
    # forbids a legitimate change outright gets edited away by whoever needs it
    # next"), and having a door on one side of the same file and a wall on the
    # other had no reason behind it beyond which one was written first.
    #
    # So the test is not "how many legal variants are there" but "is this edit
    # legitimate", and the two halves separate cleanly:
    #   • A key in PINNED_ON is pinned WHOLE, value included. Every suppressor
    #     lives INSIDE the trigger it applies to — paths, paths-ignore, branches,
    #     branches-ignore, types, tags — so pinning the value is what stops
    #     `paths-ignore: ['**']` and `branches: [release]` from making every gate
    #     silent on every pull request. Enumerating the suppressor keys instead
    #     would be the losing game the gate-`if` rule already lost once.
    #   • A key in ALLOWED_ON_KEYS may be ADDED without touching this guard. What
    #     makes that safe is not that the key looks harmless, and it is NOT the
    #     blanket "a separate trigger cannot filter an existing one" this comment
    #     used to assert — that sentence is false in the direction it did not
    #     consider. The claim, at the width it actually holds: adding
    #     workflow_dispatch cannot stop the gates from being scheduled ON A PULL
    #     REQUEST, because a PR run has a different `github.ref` and therefore a
    #     different concurrency group from anything a dispatch can occupy
    #     (reasoned from ci.yml's `group: ci-${{ github.ref }}`, NOT tested on
    #     GitHub). And auto-beta's `github.event_name == 'push'` half separately
    #     stops a dispatched run from publishing.
    #     ⚠️ ON MAIN IT DOES NOT HOLD: main runs with cancel-in-progress: false
    #     queue in one shared group with a single pending slot, so a manually
    #     dispatched run against main can displace a queued trunk commit, whose
    #     conclusion is then `cancelled` — not red, no verdict, no beta. Read off
    #     ci.yml's own concurrency comment; also not tested on GitHub.
    #   • Anything else — pull_request_target above all, which runs with a write
    #     token against fork code — is refused. Fail-closed: an unlisted trigger
    #     cannot arrive without an edit to THIS LINE, and that edit is the review.
    # ⚠️ WHAT THE DOOR DOES NOT BUY, same caveat as the gate-`if` roll-call: a key
    # somebody ADDS to ALLOWED_ON_KEYS is trusted completely, and its value is not
    # read at all. The mechanism forces the conversation; it does not have it.
    PINNED_ON = {"pull_request": None, "push": {"branches": ["main"]}}
    ALLOWED_ON_KEYS = frozenset(("workflow_dispatch",))
    # Same trap as ALLOWED_GATE_IF, same guard: `frozenset(("x"))` without the
    # trailing comma is a set of single characters, and `("x")` is a string whose
    # `in` is substring containment. Fail loudly rather than silently widen.
    if not isinstance(ALLOWED_ON_KEYS, frozenset) or any(
            not isinstance(k, str) or len(k) < 2 for k in ALLOWED_ON_KEYS):
        print("ALLOWED_ON_KEYS is not a frozenset of whole key names (%r) — "
              "almost always a missing trailing comma" % (ALLOWED_ON_KEYS,))
        sys.exit(0)

    # psych is YAML 1.1, so an unquoted `on:` key comes back as the boolean true;
    # a quoted `"on":` stays the string. Both are read, and BOTH PRESENT is a
    # failure rather than a pick — the loser's triggers would go unexamined.
    found = [k for k in ("true", "on") if k in doc]
    if len(found) != 1:
        print("on-keys=%s (want exactly one)" % (",".join(found) or "none"))
        sys.exit(0)
    got = doc[found[0]]
    if not isinstance(got, dict):
        # `on: [push, pull_request]` is legal and drops push.branches, so main's
        # post-merge round would fire on every branch. Not a mapping, not ok.
        print("on-is-not-a-mapping=%s" % json.dumps(got, sort_keys=True,
                                                    separators=(",", ":")))
        sys.exit(0)
    def j(v):
        return json.dumps(v, sort_keys=True, separators=(",", ":"))
    missing = sorted(k for k in PINNED_ON if k not in got)
    changed = sorted("%s:%s" % (k, j(got[k])) for k in PINNED_ON
                     if k in got and got[k] != PINNED_ON[k])
    unexpected = sorted(k for k in got if k not in PINNED_ON
                        and k not in ALLOWED_ON_KEYS)
    if missing or changed or unexpected:
        print("missing=%s changed=%s unexpected=%s" % (
            ",".join(missing) or "-", ",".join(changed) or "-",
            ",".join(unexpected) or "-"))
    else:
        print("ok")

elif what == "job-permissions":
    # The publishing job's OWN permissions block, whole and sorted. A job-level
    # block REPLACES the set wholesale, so what matters is the complete map, not
    # whether some key is present somewhere in it.
    perms = ((jobs.get(os.environ["JOB"]) or {}).get("permissions")) or {}
    if not isinstance(perms, dict):
        print("not-a-mapping:%r" % (perms,))
    else:
        print(",".join("%s=%s" % (k, perms[k]) for k in sorted(perms)))

elif what == "write-holders":
    # Every job whose own permissions grant contents: write, plus the workflow
    # default. The answer must be exactly this one job.
    holders = [n for n, j in sorted(jobs.items())
               if str(((j or {}).get("permissions") or {}).get("contents", "")) == "write"]
    print("jobs=%s default=%s" % (",".join(holders) or "-",
                                  (doc.get("permissions") or {}).get("contents", "<unset>")))

elif what == "publish-call-count":
    print(len(publish_runs()))

elif what == "publish-call-shape":
    # Asserted by MEANING, not by the literal text. The earlier version required
    # the substring "--target ${{ github.sha }}", which pinned interpolation-into-
    # the-run-body as the only passing shape — so moving the values into `env:`
    # (the safer plumbing) turned the guard red, i.e. the guard was standing in
    # front of its own hardening. What matters is: the target is the commit that
    # triggered this run, and the tag is the one the compute step produced.
    steps = publish_steps()
    step = steps[0] if steps else {}
    cmd = publish_runs()[0] if steps else ""
    have = []
    if "--no-settle" in cmd:
        have.append("no-settle")
    if resolves_to(step, flag_value(cmd, "--target"), "${{ github.sha }}"):
        have.append("target-sha")
    if resolves_to(step, flag_value(cmd, "--beta"), "${{ steps.tag.outputs.tag }}"):
        have.append("beta-from-tag-step")
    print(",".join(have) or "-")

elif what == "publish-gated-on-freshness":
    # The publish step must be conditional on the staleness check, or a re-run of
    # an OLD main run republishes old code under a HIGHER version number — and the
    # station picks by semver, not by date, so it would move BACKWARDS in behaviour
    # while moving forwards in version.
    steps = publish_steps()
    cond = norm((steps[0] if steps else {}).get("if", ""))
    print("gated" if re.search(r"steps\.\w+\.outputs\.\w+", cond) else (cond or "-"))

elif what == "freshness-gate-ref":
    # Read the gate off the PUBLISH step rather than hardcoding "freshness": the
    # thing being pinned is that publishing is bound to a real staleness verdict,
    # so the step id, the output name and the value that OPENS the gate all come
    # from the workflow itself. Renaming the step is fine; severing the binding is
    # not, and nothing downstream can be satisfied by a step this does not find.
    steps = publish_steps()
    cond = norm(unwrap((steps[0] if steps else {}).get("if", "")))
    m = re.search(r"steps\.(\w+)\.outputs\.(\w+)\s*==\s*'([^']*)'", cond)
    print("%s %s %s" % m.groups() if m else "-")

elif what == "step-run":
    # The `run:` body of the step with this id, plus its env bindings, emitted as
    # a shell-sourceable fixture so the guard can EXECUTE the real comparison
    # instead of pattern-matching it. `${{ github.sha }}` — in an env value or
    # interpolated straight into the body — is rewritten to $OC_PC_TRIGGER_SHA,
    # which is the one value the harness varies.
    want = sys.argv[3]
    hit = [s for s in (me.get("steps") or []) if str(s.get("id", "")) == want]
    if not hit:
        sys.exit("no step with id %r in job %r" % (want, job))
    step = hit[0]
    SHA_RX = re.compile(r"\$\{\{\s*github\.sha\s*\}\}")
    for k, v in (step.get("env") or {}).items():
        v = str(v)
        if SHA_RX.fullmatch(v.strip()):
            print("export %s=\"$OC_PC_TRIGGER_SHA\"" % k)
        else:
            print("export %s=%s" % (k, json.dumps(v)))
    print(SHA_RX.sub('"$OC_PC_TRIGGER_SHA"', str(step.get("run", ""))))

elif what == "checkout-with":
    # fetch-depth alone is not the whole requirement: tags have to arrive too, and
    # a tagless checkout is rc=0 + zero tags, which bin/next-beta-tag now refuses.
    outs = []
    for s in (me.get("steps") or []):
        if not str(s.get("uses", "")).startswith("actions/checkout"):
            continue
        w = s.get("with") or {}
        outs.append("depth=%s,tags=%s" % (w.get("fetch-depth", "<unset>"),
                                         w.get("fetch-tags", "<unset>")))
    print(";".join(outs) or "-")

else:
    sys.exit("unknown query %r" % what)
PY

check "W0b the $JOB job exists in the workflow" "yes" "$(q job-present)"

# ── W1r: every job DECLARES whether it is a gate, and the declaration holds ──
# The marker is a comment, so it cannot come from the parsed document — psych
# throws comments away. It is scanned out of the raw text instead, and the two
# views are then required to AGREE on the job set: the parser is the authority on
# which jobs exist, the scan is the authority on what they declared, and any job
# one sees and the other does not means the scan's verdict covers less than it
# claims. That is reported, not shrugged at.
#
# WHY A COMMENT AND NOT A KEY: GitHub rejects an unrecognised key inside a job
# ("Unexpected value"), so a real YAML field would be a workflow that does not
# start. W4b already scans this file as text with comments included, so a comment
# is not a second-class carrier here.
cat > "$WORK/roles.py" <<'PY'
import json, re, sys

RAW, JSONF = sys.argv[1], sys.argv[2]
MARKER = "oc-job-role"
GATE, NOTGATE = "gate", "not-a-gate"

lines = open(RAW, encoding="utf-8").read().splitlines()
doc = json.load(open(JSONF, encoding="utf-8"))
parsed = set(doc.get("jobs") or {})

problems = []
def problem(msg):
    problems.append(msg)

# The jobs region: the top-level `jobs:` key up to the NEXT top-level key. Not to
# end of file — a `defaults:` or `env:` block written after `jobs:` is legal there
# and its two-space keys are not job names. (Same bounding, and the same reason,
# as bin/tests/main-red-notify-guard.sh.)
starts = [i for i, l in enumerate(lines) if l.rstrip() == "jobs:"]
if len(starts) != 1:
    problem("the file has %d top-level 'jobs:' keys (want exactly 1) — the parser "
            "takes one and this scan reads the other, so the markers in the loser "
            "would be attributed to nothing" % len(starts))
    print("PROBLEM:%s" % problems[0])
    sys.exit(0)

region = []
for i in range(starts[0] + 1, len(lines)):
    if lines[i] and not lines[i][0].isspace() and not lines[i].lstrip().startswith("#"):
        break
    region.append((i + 1, lines[i]))          # 1-based line numbers, for naming lines

JOB_KEY = re.compile(r'^  (["\']?)([_A-Za-z][A-Za-z0-9_-]*)\1:\s*(#.*)?$')
MARKER_LINE = re.compile(r'^    #\s*' + MARKER + r':\s*(\S+)\s*$')
# Job-level `continue-on-error` is a four-space key inside a job body. Scanned as
# text ONLY to name the line in a red; whether it is THERE is answered by the
# parser below, so an indentation this pattern misses cannot hide it.
COE = "continue-on-error"
COE_LINE = re.compile(r'^    ' + COE + r':\s*(.*?)\s*(?:#.*)?$')

scanned = []          # job ids in file order
declared_at = {}      # job -> the line its key is on, so a red can name it
markers = {}          # job -> list of (lineno, value)
coe_at = {}           # job -> list of (lineno, raw value) for continue-on-error
current = None
for lineno, text in region:
    if text.strip() == "" or re.match(r'^\s*#', text):
        pass
    elif re.match(r'^  [^ \t]', text):
        m = JOB_KEY.match(text)
        if not m:
            # Loud, not skipped: a job written in a shape this scan cannot read
            # would otherwise simply not be classified, and W1 would difference
            # against a set that quietly excludes it.
            problem("line %d is a two-space key in the jobs region that this scan "
                    "cannot read as a job id, so nothing here classifies the job it "
                    "declares: %s" % (lineno, text.rstrip()))
            current = None
            continue
        current = m.group(2)
        scanned.append(current)
        declared_at.setdefault(current, lineno)
        markers.setdefault(current, [])
        coe_at.setdefault(current, [])
        continue
    mc = COE_LINE.match(text)
    if mc and current is not None:
        coe_at.setdefault(current, []).append((lineno, mc.group(1)))
    if MARKER in text:
        m = MARKER_LINE.match(text)
        if current is None:
            problem("line %d mentions %s but is not inside any job this scan could "
                    "attribute it to: %s" % (lineno, MARKER, text.rstrip()))
        elif not m:
            problem("line %d looks like a %s marker for job '%s' but is not one this "
                    "scan can read (it must be exactly four spaces, '# %s: <role>', "
                    "nothing else on the line): %s"
                    % (lineno, MARKER, current, MARKER, text.rstrip()))
        else:
            markers[current].append((lineno, m.group(1)))

# The two views must describe the same jobs.
if set(scanned) != parsed:
    only_scan = ",".join(sorted(set(scanned) - parsed)) or "-"
    only_parse = ",".join(sorted(parsed - set(scanned))) or "-"
    problem("the raw-text scan and the YAML parser disagree about which jobs exist "
            "(scan-only=%s parser-only=%s) — the classification below would cover "
            "less than it claims" % (only_scan, only_parse))
dupes = [j for j in scanned if scanned.count(j) > 1]
if dupes:
    problem("job(s) declared more than once in the jobs region: %s"
            % ",".join(sorted(set(dupes))))

roles = {}
marked_at = {}        # job -> the line its marker is on
for job in sorted(parsed):
    found = markers.get(job) or []
    if len(found) == 0:
        problem("job '%s' (declared on line %s) carries no %s marker, so this guard "
                "cannot tell whether it is a gate. It is NOT assumed to be either one. "
                "Add '    # %s: %s' or '    # %s: %s' as the first line of its body."
                % (job, declared_at.get(job, "?"), MARKER, MARKER, GATE, MARKER, NOTGATE))
        continue
    if len(found) > 1:
        problem("job '%s' (declared on line %s) carries %d %s markers (lines %s) — "
                "which one is the answer is exactly the question this marker exists to "
                "settle" % (job, declared_at.get(job, "?"), len(found), MARKER,
                            ", ".join(str(n) for n, _ in found)))
        continue
    lineno, value = found[0]
    marked_at[job] = lineno
    if value not in (GATE, NOTGATE):
        problem("job '%s' declares an unrecognised role '%s' on line %d (want '%s' or "
                "'%s') — an unreadable answer is not a pass"
                % (job, value, lineno, GATE, NOTGATE))
        continue
    roles[job] = value

# ── the marker is corroborated, so relabelling is not a free pass ────────────
# A not-a-gate is a job no pull request can ever get a verdict from; a gate is one
# every pull request does. Pinning is the observable half of that, and it cuts the
# way that matters: a real gate cannot be excused from auto-beta's `needs` by
# declaring it a not-a-gate, because it would then also have to stop running on
# pull requests — which reddens here, and is a change nobody makes by accident.
# ⚠️ This is corroboration, NOT the definition. A main-pinned job is not thereby a
# non-gate; the marker is what says so, and a human review of that marker is what
# AGENTS.md's exemption rule is about.
#
# ⚠️ THE GATE SIDE USED TO BE SPELLING-DEPENDENT, AND THAT WAS A HOLE. It asked
# only whether the ONE literal `github.ref == 'refs/heads/main'` appeared, so
# `if: github.ref_name == 'main'` and `if: startsWith(github.ref, 'refs/heads/main')`
# — two mutants a reviewer actually ran, both 35 ok / 0 failed — took a real gate
# off every pull request while this guard reported the marker as corroborated.
# Enumerating ref spellings is a losing game (there is also github.head_ref,
# github.event_name, contains(), a matrix expression, an env lookup).
#
# THE FIRST ANSWER TO THAT WAS "A GATE MAY CARRY NO JOB-LEVEL `if` AT ALL", AND
# THAT WAS TOO STRONG. It is fail-closed, which is right, but it also reddens for
# conditions that are not bypasses at all — `github.event.pull_request.draft
# != true` is the obvious one, and a rule that forbids a legitimate change
# outright gets edited away by whoever needs it next, which costs more than it
# buys. Measured: that condition scored 35 ok / 2 failed.
#
# So the rule is an ALLOWLIST, not a ban and not a spelling denylist: a gate may
# carry NO job-level `if`, or one that is EXACTLY a member of the roll-call
# below — compared whole, after normalising whitespace and the `${{ }}` wrapper
# (quoting is NOT normalised: a double-quoted literal is refused by name, because
# it does not mean the same thing, it stops the workflow from starting — see
# canon() and DQUOTE_WHY), so no spelling gets in that a reviewer did not read.
# This is the same
# device as RULED_EXEMPT and WANT_IF, for the same reason: an unlisted condition
# — bypass or not — cannot pass without an edit to THIS LINE, and that edit is
# the deliberate, reviewable act. Fail-closed either way; what changes is that
# the legitimate case now has a door instead of a wall.
# ⚠️ AND WHAT THIS DOES NOT BUY, because the roll-call is only as good as the
# reading: a condition somebody ADDS here is trusted completely. `draft != true`
# does stop a gate running on draft pull requests, and if the land criteria ever
# accept a draft PR as merge-ready that entry is the hole. The mechanism forces
# the conversation; it does not have the conversation.
# Step-level `if:` is untouched — several gates use it and it cannot skip the job.
#
# 🔴 THE CONTAINER TYPE IS PART OF THE ASSERTION, NOT A STYLE CHOICE. This was
# written as a parenthesised tuple and the roll-call was EMPTY, so the door had
# never once been executed — and the first person to open it would most likely
# open it wrong. A one-entry tuple needs a trailing comma; without it
# `("…")` is a plain STRING, and `x in "some string"` is SUBSTRING containment,
# not equality. A reviewer measured the consequence: with
#   ALLOWED_GATE_IF = ("github.event_name == 'pull_request' || github.ref == 'refs/heads/main'")
# a gate carrying `if: github.ref == 'refs/heads/main'` — the exact bypass this
# whole ticket started from, a real gate taken off every pull request — passed at
# 39 ok / 0 failed, because the bypass is a substring of the listed entry.
# frozenset() cannot degrade that way: a missing comma yields
# `frozenset("…")`, a set of single CHARACTERS, and a whole condition can never
# be a member of that — it fails CLOSED instead of open. The explicit shape check
# below then turns that closed failure into a message that says what happened,
# rather than a red that reads as if the workflow were at fault.
ALLOWED_GATE_IF = frozenset((
))
if not isinstance(ALLOWED_GATE_IF, frozenset) or any(
        not isinstance(c, str) or len(c) < 2 for c in ALLOWED_GATE_IF):
    problem("ALLOWED_GATE_IF is not a frozenset of whole conditions (it is %r). "
            "That is almost always a missing trailing comma: `frozenset((\"x\"))` "
            "iterates the string and gives a set of single characters, and "
            "`(\"x\")` is not a container at all but a string, which turns the "
            "membership test into SUBSTRING containment and lets a condition that "
            "merely APPEARS INSIDE a listed entry through. Write each entry as its "
            "own element with a trailing comma: frozenset((\"a\", \"b\",))"
            % (ALLOWED_GATE_IF,))

def canon(expr):
    """Whitespace and the `${{ }}` wrapper normalised; everything else verbatim.

    ⚠️ QUOTING IS NO LONGER NORMALISED HERE, and the reason is a measurement, not
    a preference. This used to rewrite double-quoted literals as single-quoted on
    the belief that `== "push"` and `== 'push'` are the same condition. THEY ARE
    NOT. Measured on GitHub with two minimal workflows differing only in the
    quotes: the single-quoted one ran and its `name:` resolved; the double-quoted
    one produced a STARTUP FAILURE with `jobs: []` — zero jobs, and the run named
    after the file path because `name:` was never read. A double quote in an
    Actions expression does not change what the condition means; it stops the
    whole workflow from being scheduled, which on a pull request looks like NO
    checks rather than a red one. Folding the two spellings together was therefore
    a WIDENING dressed as a no-op rewrite. Double quotes are now refused by name
    in the loop below, before anything is compared.
    """
    e = re.sub(r"\s+", " ", str(expr)).strip()
    m = re.fullmatch(r"\$\{\{\s*(.*?)\s*\}\}", e)
    if m:
        e = m.group(1)
    return e

DQUOTE_WHY = (
    "a double quote used as a STRING DELIMITER (%s). A GitHub Actions expression "
    "delimits string literals with SINGLE quotes; a double quote in that position does "
    "not change what the condition means, it stops the WHOLE WORKFLOW from starting. "
    "Measured on GitHub — the double-quoted form was a startup failure with ZERO jobs "
    "and the run named after the file path because `name:` was never parsed, while the "
    "single-quoted control ran one job to success. On a pull request that is not a red "
    "check, it is NO checks, which looks exactly like nothing being wrong. Rewrite the "
    "literal with SINGLE quotes. ⚠️ THIS IS ABOUT DELIMITER POSITION, NOT ABOUT THE "
    "CHARACTER: a double quote INSIDE a single-quoted literal — contains(msg, "
    "'\"skip-ci\"') — is an ordinary character and is NOT refused. An earlier version "
    "of this guard said \"double quotes are not valid in an expression\" and refused "
    "that too, which was a false red of our own making."
)

# ── double quotes in an EXPRESSION, at any position in the file ───────────────
# 🔴 THE SCOPE OF THIS RULE IS DRAWN BY THE CONSEQUENCE, NOT BY THE LAYER, and the
# first version drew it by the layer. It checked job-level `if` only, on the
# inherited reasoning that step-level `if` "cannot skip a whole job so it is not a
# bypass". True of skipping — and irrelevant here, because a double quote does not
# skip anything: it stops the WHOLE FILE from being scheduled. Measured on GitHub,
# three minimal workflows:
#   step-level `if: github.event_name == "push"`              → startup failure, 0 jobs
#   `env: WHO: ${{ github.event_name == "push" }}`            → startup failure, 0 jobs
#   the same two written with single quotes (control)         → success, 1 job, success
# The control having a job that ran is what makes the other two findings rather than
# a broken probe. So both positions are refused, and the rule reads: a double quote
# inside an expression, wherever the expression lives.
#
# ⚠️ WHAT MUST STAY GREEN, because refusing it would be the same disease this whole
# round is treating — a guard that reddens for a legal edit:
#   • shell double quotes in `run:` (`run: echo "hi"`) are ordinary shell;
#   • ordinary YAML string values (`env: FOO: "bar"`, `with: args: "x"`) are legal
#     YAML — and note the parser makes this one free: quoting is consumed by the
#     YAML parse, so a plain quoted scalar arrives here with no `"` in it at all.
#     What survives into a VALUE is what this reads.
# Positive controls for both directions are asserted in W1rq; they are not a
# comment's promise.
#
# ⚠️ THREE POSITIONS ARE MEASURED (job `if`, step `if`, `${{ }}` interpolation).
# Other expression positions exist in the Actions grammar and are NOT measured; the
# scan below covers every `${{ }}` in the parsed document, which reaches them by
# construction, but the CLAIM about what GitHub does with them stops at those three.
DQ_EXPR = re.compile(r"\$\{\{(.*?)\}\}", re.S)

def outside_exprs(s):
    return DQ_EXPR.sub("", str(s))

def dq_as_delimiter(expr):
    """Is there a double quote WHERE A STRING DELIMITER GOES — outside every
    single-quoted literal?

    🔴 THE FIRST VERSION ASKED `'"' in …` AND THAT WAS A FALSE RED WE SHIPPED. An
    Actions expression delimits strings with single quotes, and a single-quoted
    literal may contain arbitrary characters, so
        if: contains(github.event.head_commit.message, '"skip-ci"')
    is legal — those double quotes are ordinary characters — and the character test
    refused it (an independent review measured 43 ok / 2 failed). A guard that
    reddens for a legal edit is the disease this whole round is treating, so the
    question is delimiter POSITION, not the presence of the character.

    ⚠️ EVIDENCE, SPLIT HONESTLY: that the delimiter form kills the whole workflow is
    measured on GitHub. That the inside-a-literal form is fine is NOT — it is read
    off the expression grammar, and what the review measured was that THIS GUARD
    refused it, not what GitHub does with it.

    A single quote inside a single-quoted literal is written by doubling it ('').
    """
    s = str(expr)
    i, n = 0, len(s)
    while i < n:
        c = s[i]
        if c == "'":
            i += 1
            closed = False
            while i < n:
                if s[i] == "'":
                    if i + 1 < n and s[i + 1] == "'":
                        i += 2
                        continue
                    i += 1
                    closed = True
                    break
                i += 1
            if not closed:
                # Unterminated literal: there is no telling where it would have
                # ended, so answer conservatively rather than let an unclosed quote
                # swallow a real delimiter later in the string.
                return '"' in s[i:] or '"' in s
            continue
        if c == '"':
            return True
        i += 1
    return False

def dq_flag(where, text):
    problem(("%s carries " + DQUOTE_WHY) % (where, str(text).strip()))

def scan_dquote_exprs():
    """Every `if:` (any layer) and every `${{ }}` body in the whole document."""
    jobs = doc.get("jobs") or {}
    hit = set()
    for job in sorted(jobs):
        spec = jobs[job] if isinstance(jobs[job], dict) else {}
        cond = str(spec.get("if", ""))
        # An `if:` is an expression in its ENTIRETY, `${{ }}` or not — so a quote
        # anywhere outside an interpolation counts here, and one inside is caught
        # by the document walk below (checked separately so neither is reported twice).
        if dq_as_delimiter(outside_exprs(cond)):
            dq_flag("job '%s' job-level `if`" % job, cond)
            hit.add(job)
        steps = spec.get("steps") or []
        for n, step in enumerate(steps, 1):
            if not isinstance(step, dict):
                continue
            scond = str(step.get("if", ""))
            if dq_as_delimiter(outside_exprs(scond)):
                dq_flag("job '%s' step %d (%s) step-level `if`"
                        % (job, n, str(step.get("name") or step.get("uses") or "unnamed")[:48]),
                        scond)
    def walk(node, path):
        if isinstance(node, dict):
            for k, v in node.items():
                walk(v, "%s.%s" % (path, k))
        elif isinstance(node, list):
            for i, v in enumerate(node):
                walk(v, "%s[%d]" % (path, i))
        elif isinstance(node, str):
            for body in DQ_EXPR.findall(node):
                if dq_as_delimiter(body):
                    dq_flag("the `${{ … }}` expression at %s" % (path.lstrip(".") or "(root)"),
                            "${{%s}}" % body)
    walk(doc, "")
    return hit

DQ_JOBS = scan_dquote_exprs()

PIN = re.compile(r"github\.ref\s*==\s*['\"]refs/heads/main['\"]")
for job, role in sorted(roles.items()):
    cond = str(((doc.get("jobs") or {}).get(job) or {}).get("if", ""))
    if job in DQ_JOBS:
        # Already refused above by name. Skipped here so the role checks do not pile
        # a second, misleading message on top: `PIN` accepts either quote, so a
        # not-a-gate pinned with `github.ref == "refs/heads/main"` would otherwise
        # read as correctly pinned while scheduling nothing at all.
        continue
    pinned = bool(PIN.search(cond))
    if role == NOTGATE and not pinned:
        problem("job '%s' declares itself %s, but its `if` does not pin "
                "github.ref == 'refs/heads/main' (found: %s) — a job that can run on "
                "a pull request produces a verdict there, and this label would "
                "quietly take it out of the set auto-beta must wait for"
                % (job, NOTGATE, cond.strip() or "(no if:)"))
    if role == GATE and cond.strip() and canon(cond) not in ALLOWED_GATE_IF:
        problem("job '%s' declares itself a %s and carries a job-level `if` that is "
                "not on the reviewed roll-call (found: %s; the roll-call holds: %s). "
                "A gate must produce a verdict on every pull request, so a condition "
                "on whether it runs is where the bypass lives, whatever it is spelled "
                "with (github.ref, ref_name, startsWith(), event_name, head_ref, an "
                "env lookup, …) — which is why this is an allowlist and not a list of "
                "bad spellings. A gate that skips on pull requests is still a gate. "
                "If the condition is genuinely not a bypass (skipping DRAFT pull "
                "requests is the case this door was opened for), add it verbatim to "
                "ALLOWED_GATE_IF in this guard in the SAME commit — that edit is the "
                "review. If this job is genuinely not a check, mark it %s — and then "
                "W1x will ask for the owner ruling that costs."
                % (job, GATE, cond.strip(),
                   ", ".join("`%s`" % c for c in ALLOWED_GATE_IF) or "(nothing yet)",
                   NOTGATE))

# ── no job may excuse itself from failing (continue-on-error) ────────────────
# ⚠️ NOTHING IN THIS FILE READ THIS FIELD, and one line walked through the gap:
# a reviewer added `continue-on-error: true` to a gate and scored 35 ok / 0 failed.
# GitHub's own description of the job-level field is "prevents a workflow run from
# failing when a job fails", so the job reports success outward — which satisfies
# auto-beta's `needs` (it publishes off a red trunk) AND makes notify-main-red's
# `failure()` false (nobody is told). Both defences fail at once, silently, from
# one line that reads like a kindness to a flaky test.
# It is banned on EVERY job, not only gates: auto-beta's own failure is the
# "merged and reachable drifting apart" silence notify-main-red exists to break,
# and notify's failure is the notification not arriving.
# The PARSER answers whether it is there (an indentation the text scan misses
# cannot hide it); the text scan only supplies the line number.
# An explicit `continue-on-error: false` is allowed — it is the default, stated.
for job in sorted(parsed):
    spec = (doc.get("jobs") or {}).get(job) or {}
    if COE not in spec:
        continue
    val = spec[COE]
    if val is False:
        continue
    where = ", ".join(str(n) for n, _ in coe_at.get(job, [])) or "?"
    problem("job '%s' carries a job-level `%s: %s` (line %s) — that makes the job "
            "report SUCCESS outward when it fails, which (a) satisfies auto-beta's "
            "`needs` so a red trunk still publishes a real prerelease, and (b) makes "
            "notify-main-red's failure() false so nobody is told. Both defences go "
            "down together and neither of them reddens. Remove it; if a specific "
            "step is allowed to fail, put the field on THAT step (step-level "
            "continue-on-error cannot make the job lie about its own conclusion)."
            % (job, COE, val, where))

# ── the exemption list is a DELIBERATE enumeration, and that IS the point ────
# Everything above stops a job from being MISclassified. It does not stop a job
# from being HONESTLY classified as a non-gate and thereby leaving auto-beta's
# required set — declare a new job not-a-gate, pin it to main, and both rules
# above are satisfied while the trunk quietly stops waiting for it.
#
# AGENTS.md answers that with a ruling, not a rule of thumb: the exemption list is
# a hardcoded roll-call, and a third member needs its OWN owner ruling. Until now
# that sentence was prose with nothing behind it. This is the mechanism. Yes, it is
# an enumeration, and yes, this file argues against those everywhere else — the
# difference is what an enumeration COSTS. W1's list would go stale silently and
# be wrong for free; this one cannot go stale in silence, because the only way to
# add a member is to edit this line, which is exactly the deliberate act the ruling
# is asking for. Shaped as "this query returns nothing", so the red names the job
# and its lines rather than a count.
RULED_EXEMPT = ("notify-main-red", "auto-beta")
for job in sorted(j for j, r in roles.items() if r == NOTGATE):
    if job in RULED_EXEMPT:
        continue
    print("EXEMPTFAIL:job '%s' (declared on line %s, marker on line %s) declares itself "
          "%s, which takes it out of the set auto-beta waits for. The jobs ruled exempt "
          "are exactly: %s. A third one needs its OWN owner ruling (AGENTS.md land "
          "criteria) — the analogy to the existing two is explicitly not enough. If you "
          "have that ruling, add the name to RULED_EXEMPT in this guard and to the "
          "exemption list in AGENTS.md in the SAME commit."
          % (job, declared_at.get(job, "?"), marked_at.get(job, "?"), NOTGATE,
             ", ".join(RULED_EXEMPT)))

for p in problems:
    print("PROBLEM:%s" % p)
if problems:
    sys.exit(0)

gates = sorted(j for j, r in roles.items() if r == GATE)
declared = set((((doc.get("jobs") or {}).get("auto-beta") or {}).get("needs")) or [])
print("GATES:%s" % (",".join(gates) or "-"))
print("NEEDSDIFF:missing=%s extra=%s" % (
    ",".join(sorted(set(gates) - declared)) or "-",
    ",".join(sorted(declared - set(gates))) or "-"))
PY

ROLES_OUT="$(python3 "$WORK/roles.py" "$WF" "$JSON" 2>&1)"
ROLES_RC=$?
ROLE_PROBLEMS="$(printf '%s\n' "$ROLES_OUT" | sed -n 's/^PROBLEM://p')"
if [[ "$ROLES_RC" != "0" ]]; then
  fatal "W1r the job-role scan itself failed to run, so nothing below classifies anything: $(printf '%s' "$ROLES_OUT" | tr '\n' ' ')"
elif [[ -n "$ROLE_PROBLEMS" ]]; then
  bad "W1r every job must DECLARE whether it is a gate, readably — these could not be classified, and are NOT being defaulted into a bucket:$(printf '%s\n' "$ROLE_PROBLEMS" | sed 's/^/ | /' | tr '\n' ' ')"
else
  ok "W1r every job carries exactly one readable $ROLE_MARKER marker, the parser and the text scan agree on the job set, every marker is corroborated (a not-a-gate pins refs/heads/main; a gate carries either no job-level if or one on the reviewed roll-call), and no job excuses itself with continue-on-error ($(printf '%s\n' "$ROLES_OUT" | sed -n 's/^GATES:/gates: /p'))"
fi

# ── W1rq: the double-quote rule is ALIVE, and it does not overreach ──────────
# Two fixtures through the SAME scanner, because "no double quotes were reported"
# and "the scanner stopped looking" print identically — the same reason W4dpc
# exists. And the second fixture is not decoration: the failure mode this rule is
# one step away from is REFUSING A LEGAL EDIT, which is the disease the whole round
# is treating. So one fixture must be caught in all three measured positions, and
# one fixture full of perfectly legal double quotes must come back clean.
DQ_DIR="$WORK/dq"; mkdir -p "$DQ_DIR"
cat > "$DQ_DIR/red.yml" <<'YML'
name: dq-red
on:
  pull_request:
jobs:
  a:
    # oc-job-role: gate
    if: github.event_name == "pull_request"
    runs-on: ubuntu-latest
    steps:
      - name: step level
        if: github.event_name == "push"
        run: echo hi
      - name: inside an interpolation
        env:
          WHO: ${{ github.event_name == "push" }}
        run: echo hi
YML
cat > "$DQ_DIR/green.yml" <<'YML'
name: dq-green
on:
  pull_request:
jobs:
  a:
    # oc-job-role: gate
    runs-on: ubuntu-latest
    env:
      PLAIN: "a double-quoted YAML scalar is legal"
    steps:
      - name: shell quotes are shell
        run: |
          echo "hello world"
          test "$PLAIN" = "$PLAIN" && echo "ok"
      - name: double quotes INSIDE a single-quoted literal are ordinary characters
        if: contains(github.event.head_commit.message, '"skip-ci"')
        run: echo skipped
      - name: same, inside an interpolation, and with a doubled '' escape
        env:
          A: ${{ contains(github.event.head_commit.message, '"skip-ci"') }}
          B: ${{ contains(github.event.head_commit.message, 'it''s "quoted"') }}
        run: echo hi
      - name: single-quoted expression, and a bare interpolation
        if: github.event_name == 'pull_request'
        env:
          SHA: ${{ github.sha }}
          COND: ${{ github.event_name == 'push' }}
        with_note: "not a real key, just another quoted scalar"
        run: echo "$SHA"
YML
# The phrase these controls count. It has to appear in DQUOTE_WHY verbatim, and it
# is deliberately the SHORTEST stable fragment of it: when the message was reworded
# this line did not follow, both counts came back 0, and W1rq reddened saying the
# rule catches nothing. That red was correct in form (a control that stops matching
# must not read as a pass) but it pointed at the matcher, not at the rule — so if
# you reword DQUOTE_WHY, reword this in the same edit.
DQ_MARK="used as a STRING DELIMITER"
dq_problems() { # dq_problems <fixture.yml> — count of double-quote refusals
  parse_yaml "$1" "$DQ_DIR/f.json" 2>/dev/null || { echo PARSEFAIL; return; }
  python3 "$WORK/roles.py" "$1" "$DQ_DIR/f.json" 2>&1 \
    | sed -n 's/^PROBLEM://p' | grep -c "$DQ_MARK" || true
}
DQ_RED_N="$(dq_problems "$DQ_DIR/red.yml")"
DQ_RED_WHERE="$(parse_yaml "$DQ_DIR/red.yml" "$DQ_DIR/f.json" 2>/dev/null; python3 "$WORK/roles.py" "$DQ_DIR/red.yml" "$DQ_DIR/f.json" 2>&1 | sed -n 's/^PROBLEM://p' | grep -o "job-level \`if\`\|step-level \`if\`\|\`\${{ … }}\` expression" | sort -u | tr '\n' ' ')"
check "W1rq the double-quote rule catches all THREE measured positions in a planted fixture (job-level if, step-level if, inside a \${{ }} interpolation) — found: ${DQ_RED_WHERE:-none}" \
  "3" "$DQ_RED_N"
DQ_GREEN_N="$(dq_problems "$DQ_DIR/green.yml")"
check "W1rq-neg and it refuses NONE of the legal double quotes — shell quotes in \`run:\`, plain quoted YAML scalars, single-quoted expressions, AND a double quote INSIDE a single-quoted literal (\`contains(msg, '\"skip-ci\"')\`, in an \`if:\` and in a \${{ }}, one with a doubled '' escape). That last one is a false red this guard SHIPPED; a rule that reddens for a legal edit is the defect this round is treating" \
  "0" "$DQ_GREEN_N"

# ── W1x: the not-a-gate set is exactly the two jobs that were ruled exempt ──
# Deliberately a roll-call. See the note beside RULED_EXEMPT above for why this one
# enumeration earns its keep where W1's would not.
EXEMPT_HITS="$(printf '%s\n' "$ROLES_OUT" | sed -n 's/^EXEMPTFAIL://p')"
EXEMPT_N="$(printf '%s\n' "$EXEMPT_HITS" | grep -c . || true)"
if [[ "$EXEMPT_N" == "0" ]]; then
  ok "W1x the jobs declaring themselves not-a-gate are exactly the ones ruled exempt (a third would need its own ruling)"
else
  bad "W1x $EXEMPT_N job(s) declare themselves not-a-gate without a ruling saying they may:$(printf '%s\n' "$EXEMPT_HITS" | sed 's/^/ | /' | tr '\n' ' ')"
fi

# ── W1: needs covers every GATE job, and nothing else ───────────────────────
# Only reachable when the classification above stood up; otherwise the gate set is
# not known and a verdict here would be invented.
if [[ -n "$ROLE_PROBLEMS" ]]; then
  bad "W1 $JOB needs EXACTLY the declared gate jobs (both directions) — NOT CHECKED, because the job roles could not be established (see W1r). This is not a pass."
else
  check "W1 $JOB needs EXACTLY the declared gate jobs (both directions)" \
    "missing=- extra=-" "$(printf '%s\n' "$ROLES_OUT" | sed -n 's/^NEEDSDIFF://p')"
fi

# ── W1b: bin/release's required-job list equals the declared gate set ───────
# T-7e6c moved the release gate off "run bin/ci.sh again on the runner" and onto
# "read the conclusions of THIS commit's GitHub Actions round". That gives
# bin/release a list of job names it requires, and a list of names is exactly the
# kind of thing that rots: rename a job, add a gate, drop macos-e2e from `needs`,
# and the release command goes on demanding — or silently stops demanding — a set
# that no longer matches the workflow. Nothing else in the tree compares the two.
#
# THIS IS A DRIFT CHECK, NOT THE GATE'S BEHAVIOURAL COVER. What the release
# command DOES with that list (refuse an absent job, refuse a non-"success"
# conclusion, refuse a round bound to another commit) is asserted by driving the
# real command in bin/tests/release-guard.sh section G. This case only pins that
# the two declarations still name the same eleven jobs, in both directions.
#
# Read out of bin/release as text on purpose: it is a shell here-string of names,
# and sourcing bin/release to read the variable would drag the whole script's
# `set -euo pipefail` and dispatch guard into this guard for one string.
REQ_JOBS="$(sed -n '/^REQUIRED_CI_JOBS="/,/"$/p' "$ROOT/bin/release" \
  | sed -e 's/^REQUIRED_CI_JOBS="//' -e 's/"$//' | grep -c . || true)"
RELEASE_JOBS="$(sed -n '/^REQUIRED_CI_JOBS="/,/"$/p' "$ROOT/bin/release" \
  | sed -e 's/^REQUIRED_CI_JOBS="//' -e 's/"$//' | grep . | LC_ALL=C sort | tr '\n' ',' | sed 's/,$//')"
if [[ -n "$ROLE_PROBLEMS" ]]; then
  bad "W1b bin/release's REQUIRED_CI_JOBS equals the declared gate set — NOT CHECKED, because the job roles could not be established (see W1r). This is not a pass."
elif [[ "$REQ_JOBS" == "0" ]]; then
  bad "W1b could not read REQUIRED_CI_JOBS out of bin/release at all — the release gate's required-job list is then unpinned, and a rename or a dropped job would go unnoticed."
else
  check "W1b bin/release's REQUIRED_CI_JOBS is EXACTLY the declared gate set (drop macos-e2e from either side and this reddens)" \
    "$(printf '%s\n' "$ROLES_OUT" | sed -n 's/^GATES://p')" "$RELEASE_JOBS"
fi

# ── W2: the trigger condition is EXACTLY the canonical one ─────────────────
# Not "both halves are mentioned somewhere in it" — see the note in the `if-raw`
# query for the mutant that walked through that. One authority, exact comparison,
# which is this repo's existing style for a condition that must not drift
# (bin/tests/main-red-notify-guard.sh's WANT_IF does the same for notify).
WANT_IF="github.event_name == 'push' && github.ref == 'refs/heads/main'"
IF_RAW="$(q if-raw)"
if [[ "$IF_RAW" == DOUBLE-QUOTED-LITERAL!* ]]; then
  # Its own branch rather than a want/got, because the reason matters more than the
  # diff: this spelling does not run the job differently, it stops the workflow
  # from starting at all.
  bad "W2 $JOB's \`if\` uses a double quote as a STRING DELIMITER: \`${IF_RAW#DOUBLE-QUOTED-LITERAL!}\`. An Actions expression delimits strings with SINGLE quotes; a double quote in that position makes the WHOLE WORKFLOW fail to start. Measured on GitHub with two minimal workflows differing only in the quotes: the double-quoted one gave a startup failure with \`jobs: []\`, zero jobs scheduled, and a run named after the file path because \`name:\` was never parsed. On a pull request that is NO checks, not a red check, which is harder to notice than a failure. Rewrite it with single quotes: \`$WANT_IF\`. (⚠️ Delimiter position only — a double quote INSIDE a single-quoted literal, \`contains(msg, '\"skip-ci\"')\`, is an ordinary character and is not refused.)"
else
  check "W2 $JOB's if is EXACTLY \`$WANT_IF\` (a bolted-on \`|| …\` would publish from a pull request)" \
    "$WANT_IF" "$IF_RAW"
fi

# ── W7: the workflow's TRIGGERS ─────────────────────────────────────────────
# The one assertion that reads `on:`. A filter here (`paths-ignore: ['**']` is the
# cheap one) makes every gate skip on every pull request with no job touched and
# no assertion above reddening — measured at 37 ok / 0 failed before W7 existed.
# ⚠️ `on:` IS ONE OF THE THINGS THAT DECIDE WHETHER A GATE IS SCHEDULED, NOT THE
# ONLY ONE — an earlier version of this comment said otherwise. `needs` pointing
# at a job that skips, a `strategy.matrix` `exclude` that empties a gate, and
# `concurrency` displacing a queued run on main all end in the same place, and
# this file does not read the last two at all. The roll-call at the top of the
# file lists them by name and says what has and has not been tested.
#
# Shaped like the gate-`if` roll-call, not like a whole-mapping pin: the VALUE of
# each canonical trigger is pinned verbatim (that is what refuses a filter), and
# a short allowlist of trigger KEYS may be added without editing this guard (that
# is what stops a legitimate `workflow_dispatch` from reddening it). See the
# `on-shape` query for both lists, and for how far the "an added trigger cannot
# suppress an existing one" reason actually reaches — it holds for pull requests
# and NOT for main, and this line used to state it without that limit.
check "W7 the workflow's pinned \`on:\` triggers are unchanged and no unlisted trigger was added (a path/branch filter here makes every gate skip, and no other assertion reads it)" \
  "ok" "$(q on-shape)"

# ── W3: the elevated permission is scoped to this job ──────────────────────
check "W3 contents:write is held by $JOB ALONE and the workflow default stays read" \
  "jobs=$JOB default=read" "$(q write-holders)"

# ── W3b: the publishing job's permission set is EXACTLY these two ──────────
# A job-level `permissions:` block REPLACES the set wholesale — every key not
# listed is `none` for that job, whatever the workflow default says. So this is
# an exact comparison of the whole map, not a "contains" test, and BOTH keys are
# load-bearing in different directions:
#
#   • contents: write — the release cannot be created without it. Its blast
#     radius is why W3 above pins that no OTHER job holds it.
#   • actions:  read  — bin/release's CI gate reads THIS run's own job
#     conclusions from /actions/runs/<id> and .../jobs, and authenticates that
#     read with the job's token. Drop this key and the token is rejected, the
#     gate fails CLOSED, and NO beta can be published at all. That failure is
#     loud but total, and nothing else in the tree would have said a word: W3
#     only ever looked at `contents`, so `actions: read` could be deleted with
#     the whole suite staying green. This line is what makes that deletion red.
#
# Exact, and written out rather than derived: adding a third permission to the
# one job that can write to this repo is a decision, and editing this line is the
# review it should get.
check "W3b $JOB's permission block is EXACTLY \`actions: read\` + \`contents: write\` (a job-level block replaces the set wholesale; dropping actions:read fails the release gate closed and publishes nothing)" \
  "actions=read,contents=write" "$(q job-permissions)"

# ── W4: GA is automated through ONE named, actor-gated workflow — and nowhere
#        else ────────────────────────────────────────────────────────────────
# Scanned as TEXT over every file under .github/, comments included, rather than
# through the parser: a mention in a comment is one edit away from being a step.
#
# ⚠️ TWO RULES, because the first one alone was NOT what it looked like. Banning
# the subcommand's NAME is what this started as, and a reviewer walked straight
# past it: `gh release edit '<tag>' --prerelease=false --latest` automates GA
# perfectly without ever typing that word — 19 ok, rc=0. So the second rule bans
# the flags that MOVE THE LATEST POINTER, which is the thing actually being
# protected. Neither rule is a claim that no workflow can ever reach GA by some
# other route (a script the workflow calls, `gh api`, a curl to the REST API);
# what they buy is that the two shapes anyone would actually reach for both redden.
#
# ⚠️ THE SCOPE USED TO BE `.github/workflows` AND THAT WAS A HOLE — a third shape
# nobody had listed. A reviewer added `.github/actions/finalise/action.yml` whose
# composite step runs `bin/release promote "$OC_TAG"`, referenced it from auto-beta
# as `uses: ./.github/actions/finalise`, and scored 35 ok / 0 failed: a composite
# action is not a workflow file, so all three rules below looked straight past it —
# while the step ran inside the one job holding `contents: write`. The scope is
# therefore the WHOLE of `.github/`, and W4d below refuses local actions outright.
#
# 🔴 THESE TWO RULES USED TO ADMIT NO EXCEPTION WHATSOEVER, on the ruling that the
# beta→final flip is a human decision that may never be automated. THAT RULING WAS
# OVERTURNED BY THE OWNER ON 2026-08-28 IN rc-f9d284c0aef7, with one condition:
#
#     "決定是我下的 然後我會讓你幫我執行 UI 上可以有這個 action
#      但是只有 seth / eva 可以跑這個"
#
# GA may therefore be automated — through EXACTLY ONE file, and only while that
# file keeps the condition he attached. What changed is the SHAPE of the rule, not
# its strength:
#   • before — "nothing under .github/ may name the flip": a location ban.
#   • now    — "only $GA_ALLOWED_REL may name it, and it must be gated on a live
#     read of the actor's repo permission": the same ban, plus a positive
#     obligation (W4e).
#
# 🔴 THE EXEMPTION IS ONE PATH, NOT A CATEGORY. Every other file under .github/ —
# a second promote workflow under any other name included — still reddens W4/W4b
# exactly as it did before, and W4pcx below proves that with a planted example
# rather than asserting it. W4e then makes the exempt file PAY for its exemption:
# lose the actor gate, stop asking GitHub who the actor is, or let it fall open
# when the answer does not arrive, and this guard goes red.
#
# ⚠️ DO NOT "TIDY THIS UP" BY DROPPING W4e AND KEEPING THE EXEMPTION. The two are
# one rule written in two halves — the exemption on its own is just a hole, and it
# is a hole in the exact place this pack exists to watch. If the workflow is ever
# deleted, delete the exemption in the same commit (W4e reddens if you do not).
GA_SCAN_DIR="$ROOT/.github"
GA_ALLOWED_REL=".github/workflows/ga-promote.yml"
GA_ALLOWED_ABS="$ROOT/$GA_ALLOWED_REL"
# 🔴 THERE IS DELIBERATELY NO ROLL-CALL CONSTANT HERE ANY MORE.
# This used to pin `pkyosx 8thEdition` verbatim and compare it to a matching string
# in the workflow. Two copies of a list is two things to forget, and the list was
# never the real rule — the owner's rule is "the people I trust with this repo",
# which GitHub already knows. The workflow now ASKS GitHub
# (repos/{owner}/{repo}/collaborators/{actor}/permission) and admits admin/write
# only, so W4e below pins the MECHANISM — that the gate really queries, and that
# every non-answer refuses — instead of pinning two usernames.
# Excuse ONLY that exact path. -F -x so a near-miss name (ga-promote.yml.bak, or a
# copy one directory over) is NOT excused by looking similar.
ga_except() { grep -Fxv "$GA_ALLOWED_ABS" || true; }

GA_HITS="$(grep -rilE 'promote' "$GA_SCAN_DIR" 2>/dev/null | ga_except)"
if [[ -z "$GA_HITS" ]]; then
  ok "W4 no file under .github/ other than $GA_ALLOWED_REL names the beta→final flip subcommand"
else
  bad "W4 a file under .github/ other than $GA_ALLOWED_REL names the beta→final flip subcommand — GA is automated through that ONE actor-gated workflow and nowhere else: $(printf '%s ' $GA_HITS)"
fi
# `gh release edit` is legitimate for nothing else under here, so the flags are
# matched rather than the command: --prerelease=false and --latest each promote a
# beta to GA on their own, and --draft=false publishes a hidden one.
# ⚠️ `=` IS IN THE TRAILING CLASS ON PURPOSE. The previous spelling was
# `--latest([[:space:]]|$)`, which does not match `--latest=true` — the exact form
# bin/release itself uses and the form anyone copying it would write. That was a
# live bypass: `gh release edit <tag> --latest=true` alone moved the pointer past
# this rule. Found while writing $GA_ALLOWED_REL's roll-back instructions, which
# is where that string had to be spelled out.
GA_FLAG_RX='--prerelease=false|--latest([[:space:]=]|$)|--draft=false'
GA_FLAG_HITS="$(grep -rlE -- "$GA_FLAG_RX" "$GA_SCAN_DIR" 2>/dev/null | ga_except)"
if [[ -z "$GA_FLAG_HITS" ]]; then
  ok "W4b no file under .github/ other than $GA_ALLOWED_REL carries a flag that moves the Latest pointer (--prerelease=false / --latest / --draft=false)"
else
  bad "W4b a file under .github/ other than $GA_ALLOWED_REL carries a GA-promoting flag (--prerelease=false / --latest / --draft=false) — that flips a beta to GA without ever naming the subcommand: $(printf '%s ' $GA_FLAG_HITS)"
fi

# ── W4pcx: the exemption is ONE PATH, and that is demonstrated, not asserted ──
# W4/W4b now report "clean" while a file under .github/ DOES legitimately contain
# both shapes, so "the scan found nothing" is a weaker statement than it used to
# be — a broken `ga_except` that excused everything would look identical. Plant a
# SECOND promote workflow beside the real one and require both rules to still
# catch it. This is the assertion that separates "one file is excused" from "the
# rule was taken out".
GA_X_DIR="$WORK/ga-except/.github/workflows"
mkdir -p "$GA_X_DIR"
cp "$GA_ALLOWED_ABS" "$GA_X_DIR/ga-promote.yml" 2>/dev/null || true
printf '%s\n' 'run: bin/release promote "$T"' 'run: gh release edit "$T" --latest=true' \
  > "$GA_X_DIR/ga-promote-2.yml"
GA_X_EXC="$ROOT/$GA_ALLOWED_REL"
GA_X_A="$(grep -rilE 'promote' "$WORK/ga-except/.github" 2>/dev/null | grep -Fxv "$GA_X_EXC" | sed "s|^$WORK/ga-except/.github/workflows/||" | LC_ALL=C sort | tr '\n' ' ' | sed 's/ $//')"
GA_X_B="$(grep -rlE -- "$GA_FLAG_RX" "$WORK/ga-except/.github" 2>/dev/null | grep -Fxv "$GA_X_EXC" | sed "s|^$WORK/ga-except/.github/workflows/||" | LC_ALL=C sort | tr '\n' ' ' | sed 's/ $//')"
check "W4pcx a SECOND promote workflow planted beside the exempt one is still caught by BOTH rules (the exemption is one path, not a category)" \
  "subcommand=ga-promote-2.yml ga-promote.yml flags=ga-promote-2.yml ga-promote.yml" \
  "subcommand=$GA_X_A flags=$GA_X_B"

# ── W4e: the exempt workflow PAYS for its exemption ──────────────────────────
# The owner overturned "GA may never be automated" and replaced it with "only seth
# / eva may run it". W4/W4b above implement the first half by standing down for one
# path; this is the second half, and without it that path is simply unguarded.
#
# ⚠️ GITHUB DOES NOT ENFORCE THE CONDITION. There is no per-workflow dispatch ACL:
# every account with write access can press Run workflow. The allowlist therefore
# has to live INSIDE the job, and something has to stop it being quietly widened —
# which is what this is.
#
# 🔴 THE VERDICTS BELOW ARE DRIVEN, NOT READ, for the reason W5c gives about the
# freshness step: asserting that the YAML mentions `github.actor` is a statement
# about WIRING, and wiring survives everything that matters (invert the comparison,
# `exit 0` on no match, compare with `==` against a glob). So W4eD lifts the gate's
# own `run:` body verbatim out of the workflow and EXECUTES it against a table of
# actors. A gate that lets a stranger through fails here even if it reads perfectly.
if [[ ! -f "$GA_ALLOWED_ABS" ]]; then
  bad "W4e $GA_ALLOWED_REL is EXEMPTED by W4/W4b above but does not exist. Delete the exemption in the same commit as the workflow — an exemption with no file behind it is a pre-opened hole for the next file to be dropped into that path."
else
  GAP_JSON="$WORK/ga-promote.json"
  if ! parse_yaml "$GA_ALLOWED_ABS" "$GAP_JSON"; then
    bad "W4e $GA_ALLOWED_REL does not PARSE as YAML (GitHub would report a startup failure — and it is the one file here allowed to touch GA): $(tr '\n' ' ' < "$WORK/parse.err")"
  else
    cat > "$WORK/gap.py" <<'PY'
import json, sys

doc = json.load(open(sys.argv[1]))
what = sys.argv[2]
jobs = doc.get("jobs") or {}

GATE_NAME = "authorize actor"

def is_gate(s):
    return isinstance(s, dict) and str(s.get("name", "")).strip() == GATE_NAME

def gate_steps():
    # The FIRST step of every job. A gate that runs second is not a gate: the step
    # before it already ran with the job's `contents: write` token.
    out = []
    for name, j in sorted(jobs.items()):
        steps = (j or {}).get("steps") or []
        out.append((name, steps[0] if steps else {}))
    return out

if what == "gate-position":
    # Every job in this file must open with the authorize step, and there must be
    # at least one job. Reported per job so a red names which one.
    if not jobs:
        print("no-jobs")
    else:
        print(",".join(
            "%s=%s" % (n, "authorize" if is_gate(s) else "NOT-THE-GATE")
            for n, s in gate_steps()))

elif what == "actor-source":
    # The gate must read the REAL actor. Anything else (an input, a hardcoded
    # string) makes the whole thing self-certifying.
    vals = sorted({str((s.get("env") or {}).get("ACTOR", ""))
                   for _, s in gate_steps() if isinstance(s, dict)})
    print("|".join(vals) if vals else "-")

elif what == "gate-body":
    for _, s in gate_steps():
        if is_gate(s):
            sys.stdout.write(str(s.get("run", "")))
            break

elif what == "tag-is-an-input":
    # "決定是我下的" — the workflow executes a decision, it never makes one. The
    # tag must arrive as the dispatch input; a workflow that looks one up has
    # taken the choice back off the human.
    ins = ((doc.get(True) or doc.get("true") or doc.get("on") or {})
           .get("workflow_dispatch") or {}).get("inputs") or {}
    bodies = " ".join(str(st.get("run", ""))
                      for j in jobs.values()
                      for st in ((j or {}).get("steps") or []))
    print("inputs=%s uses-input=%s" % (
        ",".join(sorted(ins)) or "-",
        "yes" if "inputs.tag" in bodies or "inputs.tag" in json.dumps(doc) else "no"))
PY
    gap() { python3 "$WORK/gap.py" "$GAP_JSON" "$@"; }

    gap gate-body > "$WORK/gate.sh"

    check "W4e2 the gate reads the REAL dispatcher (github.actor), not an input it could be told" \
      '${{ github.actor }}' "$(gap actor-source)"
    check "W4e3 the authorize gate is the FIRST step of every job (a gate that runs second has already let a step run with contents: write)" \
      "ga-promote=authorize" "$(gap gate-position)"
    check "W4e4 the tag is a dispatch INPUT and the workflow never picks one itself (the owner makes the call; this only executes it)" \
      "inputs=tag uses-input=yes" "$(gap tag-is-an-input)"

    if [[ ! -s "$WORK/gate.sh" ]]; then
      bad "W4eD could not find the authorize gate's run body in $GA_ALLOWED_REL — the gate this pack claims to pin is not there to be driven, and W4e2/W4e3 above were reading an absent step. The gate is identified by its step name ('authorize actor'); renaming it without updating this guard is the same as deleting it."
    else
      # ── W4e/W4e1: the gate ASKS, and carries no list ─────────────────────────
      # The owner's rule is "the people I trust with this repo". GitHub already
      # holds that answer, so the gate must go and get it. A gate that instead
      # carries usernames is the shape this change removed, and re-introducing one
      # is how it would quietly come back.
      if grep -qE 'collaborators/.*ACTOR.*/permission' "$WORK/gate.sh"; then
        ok "W4e the gate ASKS GitHub for THIS actor's permission on this repo (collaborators/<actor>/permission) rather than carrying a name list"
      else
        bad "W4e the gate does not read collaborators/<actor>/permission — 'who may press' is supposed to BE the repo's collaborator list, asked at run time. Body: $(tr '\n' ' ' < "$WORK/gate.sh")"
      fi
      if grep -qEi 'pkyosx|8thEdition|ALLOWED_ACTORS' "$WORK/gate.sh"; then
        bad "W4e1 the gate hardcodes an account name / allowlist again. That is what this rule replaced: a list in the file goes stale against the repo's real collaborators and has to be edited in two places. Change who has write access instead. Body: $(tr '\n' ' ' < "$WORK/gate.sh")"
      else
        ok "W4e1 the gate carries NO hardcoded account list (nothing to widen in a diff nobody reads)"
      fi

      # ── W4eD: the gate, EXECUTED against a stubbed GitHub ─────────────────────
      # 🔴 DRIVEN, NOT READ, for the reason W5c gives about the freshness step:
      # asserting the YAML mentions the endpoint is a statement about WIRING, and
      # wiring survives everything that matters (invert the case, add `read` to the
      # admitted set, fall open when the read fails). So the gate's own `run:` body
      # is lifted verbatim and executed with a fake `gh` on PATH that returns a
      # chosen permission and a chosen exit status. No network, no real repo.
      #
      # The actor used throughout is 'mallory' — a name on nobody's list. If the
      # allow rows pass with that actor, the verdict really did come from the
      # permission GitHub reported and not from who was asking.
      GA_STUB_DIR="$WORK/ga-stub"
      mkdir -p "$GA_STUB_DIR"
      cat > "$GA_STUB_DIR/gh" <<'STUB'
#!/bin/sh
printf '%s\n' "$*" >> "$OC_STUB_LOG"
[ "${OC_STUB_RC:-0}" = 0 ] || exit "$OC_STUB_RC"
[ -z "${OC_STUB_OUT:-}" ] || printf '%s\n' "$OC_STUB_OUT"
exit 0
STUB
      chmod +x "$GA_STUB_DIR/gh"

      GATE_OUT=""
      # permission gh reports | gh exit status | expected verdict
      while IFS='|' read -r PERM_IN RC_IN WANT; do
        [[ -n "$PERM_IN$RC_IN$WANT" ]] || continue
        : > "$WORK/stub.log"
        if PATH="$GA_STUB_DIR:$PATH" OC_STUB_LOG="$WORK/stub.log" \
             OC_STUB_RC="$RC_IN" OC_STUB_OUT="$PERM_IN" \
             ACTOR="mallory" REPO="pkyosx/OffiCraft" \
             bash "$WORK/gate.sh" >/dev/null 2>&1; then
          GOT=allow
        else
          GOT=refuse
        fi
        GATE_OUT+="${PERM_IN:-<empty>}/rc$RC_IN=$GOT "
      done <<'TABLE'
admin|0|allow
write|0|allow
read|0|refuse
none|0|refuse
triage|0|refuse
|0|refuse
null|0|refuse
admin|1|refuse
admin|127|refuse
not json at all|0|refuse
TABLE
      check "W4eD the gate's OWN run body, executed against a stubbed gh: admin/write proceed, read/none/triage refuse, and EVERY non-answer (empty, null, API error, gh missing, garbage) refuses — fail-closed" \
        "admin/rc0=allow write/rc0=allow read/rc0=refuse none/rc0=refuse triage/rc0=refuse <empty>/rc0=refuse null/rc0=refuse admin/rc1=refuse admin/rc127=refuse not json at all/rc0=refuse " \
        "$GATE_OUT"

      # …and it has to have actually ASKED. A gate that admits admin/write from a
      # variable it set itself would pass the table above without ever talking to
      # GitHub; this is the row that separates "queried" from "decided locally".
      : > "$WORK/stub.log"
      PATH="$GA_STUB_DIR:$PATH" OC_STUB_LOG="$WORK/stub.log" OC_STUB_RC=0 OC_STUB_OUT="admin" \
        ACTOR="mallory" REPO="pkyosx/OffiCraft" bash "$WORK/gate.sh" >/dev/null 2>&1 || true
      if grep -q 'repos/pkyosx/OffiCraft/collaborators/mallory/permission' "$WORK/stub.log"; then
        ok "W4eD3 the gate really CALLED the permission endpoint for the dispatching actor on the dispatching repo (both interpolated, neither hardcoded)"
      else
        bad "W4eD3 the gate allowed 'mallory' without asking GitHub for repos/pkyosx/OffiCraft/collaborators/mallory/permission — it decided locally. gh saw: $(tr '\n' ' ' < "$WORK/stub.log")"
      fi

      # …and the refusal has to SAY who was refused. A silent `exit 1` on the one
      # path a human meets only when something is wrong is how a real refusal gets
      # misread as an infra flake and re-run until someone widens the rule.
      : > "$WORK/stub.log"
      PATH="$GA_STUB_DIR:$PATH" OC_STUB_LOG="$WORK/stub.log" OC_STUB_RC=0 OC_STUB_OUT="read" \
        ACTOR="mallory" REPO="pkyosx/OffiCraft" bash "$WORK/gate.sh" >"$WORK/gate.out" 2>&1 || true
      if grep -q "mallory" "$WORK/gate.out"; then
        ok "W4eD2 the refusal NAMES the actor it refused (a bare exit 1 reads as an infra flake)"
      else
        bad "W4eD2 the gate refused 'mallory' without naming it in its output: $(tr '\n' ' ' < "$WORK/gate.out")"
      fi
    fi
  fi
fi

# ── W4pc: the two greps above are ALIVE ─────────────────────────────────────
# W4 and W4b both report "clean" as an EMPTY result, and an empty result is what a
# grep that matches nothing looks like — a mistyped pattern, a directory that moved,
# a `-r` that stopped recursing. So the same two invocations are pointed at a
# fixture that definitely contains both shapes, and are required to FIND them.
# Without this, "no hits under .github/" is a claim with no evidence behind it.
GA_PC_DIR="$WORK/ga-pc/nested"
mkdir -p "$GA_PC_DIR"
printf '%s\n' 'run: bash bin/release promote "$OC_TAG"' > "$GA_PC_DIR/action.yml"
printf '%s\n' 'run: gh release edit "$T" --prerelease=false --latest' > "$GA_PC_DIR/other.yml"
GA_PC_A="$(grep -rilE 'promote' "$WORK/ga-pc" 2>/dev/null | grep -c . || true)"
GA_PC_B="$(grep -rlE -- "$GA_FLAG_RX" "$WORK/ga-pc" 2>/dev/null | grep -c . || true)"
check "W4pc the W4/W4b scans find a planted GA automation in a nested dir (so an empty result over .github/ means something)" \
  "subcommand=1 flags=1" "subcommand=$GA_PC_A flags=$GA_PC_B"

# ── W4d: nothing under .github/ REFERENCES a local action or workflow ────────
# The composite-action bypass works because a local action is a SECOND home for
# steps: its own file, its own `run:` bodies, and — being reached through `uses:`
# — no obligation to look like anything this guard recognises. W4 and W4b now
# grep it, but "we grep it too" is a weaker promise than "it cannot be reached",
# because the next shape in such a file (a `gh api` PATCH, a node action, a
# script it shells out to) is invisible to a grep again.
#
# ⚠️ THIS USED TO BAN THE DIRECTORY `.github/actions/`, AND THAT WAS THE WRONG
# TARGET — it paid the whole price and bought almost nothing. A local action does
# not have to live under `.github/`: `action.yml` at the repo root referenced as
# `uses: ./`, or `tools/finalise/action.yml` as `uses: ./tools/finalise`, is an
# ordinary composite action that GitHub runs happily, that W4/W4b (scoped to
# `.github/`) never read, and that the directory ban never saw. Measured: that
# exact shape scored 37 ok / 0 failed while a step running `bin/release promote`
# sat inside auto-beta's `contents: write` job.
#
# So what is pinned is the REFERENCE, not a location: no file under `.github/`
# may point a `uses:` at this repo's own code — neither by relative path
# (`./…`, `../…`) nor by naming this repo in the `{owner}/{repo}/{path}@{ref}`
# form. Reaching a local action requires one of those, and so does calling a
# local REUSABLE WORKFLOW, which is a second way to add jobs W1's set difference
# cannot see. Adding one is a reasonable thing to want; it has to redden here and
# be decided, not arrive as a refactor.
#
# 🔴 THE PREVIOUS VERSION OF THIS BLOCK CLAIMED `uses: ./…` WAS "THE ONLY ENTRY
# POINT", AND A REVIEWER WALKED PAST IT TWICE — the claim was wider than the
# grep, which is the exact failure this whole pack was sent back to fix. Both
# bypasses put a `bin/release promote` step inside auto-beta's `contents: write`
# job and both scored 39 ok / 0 failed:
#   N-1  the VALUE ON THE NEXT LINE, which is an ordinary YAML plain scalar:
#            - uses:
#                ./tools/finalise
#        A LINE-ORIENTED grep cannot see this and never could: YAML's structure
#        is not a property of any single line. That is why the authority below is
#        the PARSER and not a regex — the fix is not a better pattern.
#   N-2  `uses: pkyosx/OffiCraft/tools/finalise@main` — a repo may reference
#        ITSELF through the {owner}/{repo}/{path}@{ref} form, which is the same
#        second home for steps wearing a third-party spelling.
#
# ⚠️ WHAT THIS STILL DOES NOT CLOSE, stated narrowly so nobody reads it wider —
# these are measured or reasoned gaps, not a rhetorical disclaimer:
#   (a) a `run:` step in ci.yml can call any script in this repo, and that script
#       can do anything, GA included. W4/W4b/W4d cover shapes that add a NEW HOME
#       FOR STEPS; they are not a proof that no workflow can reach GA.
#   (b) a THIRD-PARTY action (`someone-else/act@v1`), or this repo referenced
#       under a DIFFERENT slug (a fork, or after a rename that this file's
#       OC_REPO_SLUG was not updated for), is not refused here. The slug is
#       corroborated against `git remote` below, so a rename reddens in a tree
#       whose `origin` is a GitHub URL — but a tree whose origin cannot yield
#       {owner}/{repo} (no git metadata at all, or a LOCAL clone, whose origin is
#       a filesystem path) gets no corroboration, only the hardcoded constant.
#       That case prints a ⚠️ NOT CORROBORATED line and is counted separately; it
#       does not fail. W4ds spells out why that direction was chosen.
#   (c) a file under `.github/` that this scan cannot PARSE is reported as a
#       failure, not skipped — but a `uses:` written in a file the walk does not
#       REACH is only covered by the line-grep net in W4dt, which has N-1's blind
#       spot by construction. Two ways not to be reached: an extension other than
#       *.yml/*.yaml, and a SYMLINKED directory (Ruby's `**` does not traverse
#       one). Neither is measured here.
#   (d) an expression-valued `uses:` (`uses: ${{ env.X }}`) is classified by its
#       literal text, so a reference assembled at run time is not resolved here.
#
# The repo's own slug, as a hardcoded roll-call for the same reason RULED_EXEMPT
# is one: the only way past it is to edit it, and that edit is the review.
OC_REPO_SLUG="pkyosx/OffiCraft"
cat > "$WORK/uses.rb" <<'RB'
# Collect and classify every `uses:` in every YAML file under a directory tree.
# The PARSER is the authority: a `uses:` whose value sits on the following line
# is the same reference as one written inline, and only a parse can say so.
require "yaml"
root, slug = ARGV[0], ARGV[1].downcase

def each_uses(node, &blk)
  case node
  when Hash
    node.each do |k, v|
      blk.call(v) if k.to_s == "uses" && v.is_a?(String)
      each_uses(v, &blk)
    end
  when Array
    node.each { |v| each_uses(v, &blk) }
  end
end

def classify(value, slug)
  v = value.strip
  return "local-path" if v =~ %r{\A\.\.?(/|\z)}
  segs = v.split("@", 2)[0].split("/")
  return "self-repo-ref" if segs.length >= 2 && "#{segs[0]}/#{segs[1]}".downcase == slug
  nil
end

Dir.glob(File.join(root, "**", "*.{yml,yaml}")).sort.each do |path|
  rel = path.sub(/\A#{Regexp.escape(root)}\/?/, "")
  begin
    doc = YAML.safe_load(File.read(path))
  rescue => e
    # Unreadable is NOT clean. A file this scan cannot parse is a file whose
    # `uses:` references went unexamined, and that has to be loud.
    puts "PARSEFAIL\t#{rel}\t#{e.message.gsub(/\s+/, ' ')}"
    next
  end
  each_uses(doc) do |value|
    why = classify(value, slug)
    puts "#{why ? "SELF" : "OTHER"}\t#{rel}\t#{value.strip}\t#{why || '-'}"
  end
end
RB
USES_OUT="$("$RUBY" "$WORK/uses.rb" "$GA_SCAN_DIR" "$OC_REPO_SLUG" 2>&1)"
USES_BAD="$(printf '%s\n' "$USES_OUT" | grep -E '^(SELF|PARSEFAIL)' || true)"
if [[ -z "$USES_BAD" ]]; then
  ok "W4d no PARSED \`uses:\` under .github/ points at this repo's own code — not by relative path (\`./…\`, \`../…\`) and not by naming $OC_REPO_SLUG in the {owner}/{repo}/{path}@{ref} form — and every YAML file under .github/ parsed"
else
  bad "W4d a \`uses:\` under .github/ points at this repo's own code (or a file there could not be parsed, which is the same unexamined). That is a second home for steps, running inside auto-beta's \`contents: write\` job, whose contents W4/W4b can only see if it happens to sit under .github/ AND happens to use a shape they grep for. Adding one is a decision to make deliberately (and this guard has to be reworked to read the target) — not a refactor that silently widens what auto-beta may run: $(printf '%s\n' "$USES_BAD" | tr '\n\t' '  ')"
fi

# ── W4ds: the hardcoded slug is still this repo's slug ───────────────────────
# W4d's second half is only as true as OC_REPO_SLUG. A rename (or a copy of this
# file into another repo) would leave the constant naming somebody else, and the
# self-reference half would then match nothing while still reporting "clean".
# git is the source, when there is one; a tree without git metadata gets an
# honest "not corroborated" rather than a free pass dressed as a check.
#
# THREE OUTCOMES, AND THE MIDDLE ONE IS THE WHOLE POINT OF THIS BLOCK.
# The evidence this assertion needs is a `{owner}/{repo}` pair, and there are two
# distinct ways a perfectly legitimate working copy fails to carry one:
#   (i)  no git metadata at all — `git archive <sha> | tar -x`, a common way to
#        review this repo in isolation;
#   (ii) git is there, but `origin` is not a GitHub remote — and this is not a
#        corner case, it is THE SUPPORTED CONCURRENCY FLOW. bin/lib/ci-lock.sh
#        tells the user verbatim to `git clone <this repo> /path/to/another-copy
#        && bash bin/ci.sh`, and calls separate clones "the supported way". A
#        local clone's origin is a FILESYSTEM PATH, which does not parse to
#        {owner}/{repo}, so the equality below has nothing to compare against.
# Both are the SAME condition — "the corroboration could not run" — and they now
# get the same treatment and the same message. ⚠️ THEY DID NOT BEFORE, and that
# is what this rewrite is for: (i) was explained at length while (ii) fell into
# the MISMATCH branch and printed a bare want/got, so the supported flow read as
# "this pack names the wrong repo". Worse, both were bad(), which made
# `bin/tests/run.sh` fail and `bin/ci.sh` UNABLE TO REACH `[ci] all green` in any
# local clone — a guard blocking the flow its own repo documents.
#
# 🔴 THE CHOICE MADE HERE, STATED SO IT CAN BE ARGUED WITH: not-corroborated is
# reported LOUDLY but does NOT fail. The alternative (fail-loud, what the previous
# version did) was tried and its measured cost was the paragraph above. What it
# costs instead: a rename, or this file copied into another repo, does NOT redden
# in a tree whose origin is not a GitHub URL — the constant stands uncorroborated
# and W4d's self-reference half may then be naming a repo this is not. That
# residual is listed under (b) beside W4d. It is NOT a silent pass: the line is
# printed with a ⚠️, it is counted separately, and the summary line carries the
# count.
#
# ⚠️ HOW NARROW THE EXPOSURE ACTUALLY IS, because "it does not fail" invites a wider
# reading than the truth: the not-corroborated branch is reachable only where origin
# yields no {owner}/{repo} — a LOCAL run in a filesystem-origin clone, or a tree with
# no git metadata at all. In CI it is not that branch: `actions/checkout` leaves an
# https://github.com/… origin, so a runner takes the equality path, where a wrong slug
# is a plain red (mutant-tested). So the uncorroborated case is a local-execution
# posture, not a hole in the cloud rounds — and the residual it leaves is exactly one
# thing: a rename or a copy of this file into another repo goes unnoticed WHEN NOBODY
# RUNS IT ANYWHERE THAT HAS A GITHUB ORIGIN.
SLUG_REMOTE="$(git -C "$ROOT" config --get remote.origin.url 2>/dev/null || true)"
# A GitHub-shaped remote, or nothing. A local path is not "a slug that differs";
# it is an absence of the evidence, and conflating the two is the bug above.
SLUG_SEEN=""
case "$SLUG_REMOTE" in
  https://*|http://*|ssh://*|git://*|*@*:*)
    SLUG_SEEN="$(printf '%s' "$SLUG_REMOTE" \
      | sed -E 's#^[a-z]+://([^@/]+@)?[^/]+/##; s#^[^@/]+@[^:/]+:##; s#/+$##; s#\.git$##')"
    ;;
esac
[[ "$SLUG_SEEN" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || SLUG_SEEN=""
if [[ -z "$SLUG_SEEN" ]]; then
  uncorrob "W4ds could not corroborate W4d's hardcoded OC_REPO_SLUG ($OC_REPO_SLUG): remote.origin.url is $(if [[ -z "$SLUG_REMOTE" ]]; then printf 'absent (no git metadata)'; else printf "'%s', which does not parse to {owner}/{repo}" "$SLUG_REMOTE"; fi). The {owner}/{repo} half of W4d may be naming a repo this is not — this is a check that COULD NOT RUN, not a pass. ⚠️ BOTH OF THESE ARE EXPECTED AND ARE NOT A DEFECT IN THIS PACK: (1) a \`git archive\` extraction has no .git; (2) a LOCAL clone (\`git clone <this repo> /path/to/another-copy\`, which bin/lib/ci-lock.sh names as the supported way to run CI twice at once) has a filesystem path for an origin. Re-run in a checkout cloned from GitHub if you need this one corroborated; every OTHER assertion in this file is unaffected either way."
else
  check "W4ds OC_REPO_SLUG matches remote.origin.url (W4d's self-reference half names THIS repo)" \
    "$(printf '%s' "$OC_REPO_SLUG" | tr 'A-Z' 'a-z')" "$(printf '%s' "$SLUG_SEEN" | tr 'A-Z' 'a-z')"
fi

# ── W4dpc: and W4d's scan is ALIVE, on every shape it claims to cover ────────
# W4d reports "clean" as an EMPTY result, exactly like W4/W4b — and exactly like a
# scan that stopped matching. Same treatment: point the SAME script at a fixture
# that definitely carries the shapes, in a nested directory, and require it to
# find every one of them. The two bypasses that got past the previous version are
# planted here BY NAME, so a regression to a line-oriented or `./`-only check
# cannot go quiet: it has to redden right here.
USES_PC_DIR="$WORK/uses-pc/nested"
mkdir -p "$USES_PC_DIR"
printf '%s\n' 'steps:' '  - uses: ./tools/finalise' > "$USES_PC_DIR/wf.yml"
printf '%s\n' 'steps:' "  - uses: './.github/actions/finalise'" \
              '  - uses: ../shared/action.yml' > "$USES_PC_DIR/wf2.yml"
# N-1: the value is a plain scalar on the NEXT line. Legal YAML, same reference,
# invisible to any line-oriented grep.
printf '%s\n' 'steps:' '  - uses:' '      ./tools/finalise' > "$USES_PC_DIR/wf3.yaml"
# N-2: this repo referencing itself the third-party way.
printf '%s\n' 'steps:' "  - uses: $OC_REPO_SLUG/tools/finalise@main" \
              "  - uses: $OC_REPO_SLUG@main" > "$USES_PC_DIR/wf4.yml"
# …and the other half of the control: a genuine third-party action must NOT be
# flagged, or "it finds everything" would pass this while making W4d useless.
printf '%s\n' 'steps:' '  - uses: actions/checkout@v5' \
              '  - uses: actions/setup-go@v6' > "$USES_PC_DIR/wf5.yml"
USES_PC_OUT="$("$RUBY" "$WORK/uses.rb" "$WORK/uses-pc" "$OC_REPO_SLUG" 2>&1)"
USES_PC_SELF="$(printf '%s\n' "$USES_PC_OUT" | grep -c '^SELF' || true)"
USES_PC_OTHER="$(printf '%s\n' "$USES_PC_OUT" | grep -c '^OTHER' || true)"
USES_PC_NL="$(printf '%s\n' "$USES_PC_OUT" | grep -c '^SELF.*wf3\.yaml' || true)"
USES_PC_SLUG="$(printf '%s\n' "$USES_PC_OUT" | grep -c '^SELF.*self-repo-ref' || true)"
check "W4dpc the W4d scan finds all 6 planted self-references (including N-1's next-line value and N-2's {owner}/{repo} self-reference) and flags NEITHER of the 2 real third-party actions" \
  "self=6 other=2 nextline=1 slugform=2" \
  "self=$USES_PC_SELF other=$USES_PC_OTHER nextline=$USES_PC_NL slugform=$USES_PC_SLUG"

# ── W4dpf: an unparseable file is REPORTED, not skipped ──────────────────────
# The one way this scan could report "clean" while reading nothing: a file it
# cannot parse. That must arrive as a PARSEFAIL line (which W4d treats as a
# failure), never as an absent result.
USES_PF_DIR="$WORK/uses-pf"
mkdir -p "$USES_PF_DIR"
printf '%s\n' 'this: is: not: valid: yaml: [' '  "unterminated' > "$USES_PF_DIR/broken.yml"
USES_PF_N="$("$RUBY" "$WORK/uses.rb" "$USES_PF_DIR" "$OC_REPO_SLUG" 2>&1 | grep -c '^PARSEFAIL' || true)"
check "W4dpf a YAML file the W4d scan cannot parse is REPORTED as unexamined (an unreadable file is not a clean one)" \
  "1" "$USES_PF_N"

# ── W4dt: the line-oriented text net, kept as a SECOND layer only ────────────
# This was the whole of W4d and it is now the lesser half: it reads files the
# YAML walk does not (anything under .github/ that is not *.yml/*.yaml) and it
# reads comments, but it is LINE-ORIENTED and therefore blind to N-1 by
# construction. It is kept because it costs nothing and covers a different set of
# files — NOT because it is a second opinion on the same question.
USES_LOCAL_RX='uses:[[:space:]]*["'"'"']?\.\.?/'
USES_LOCAL_HITS="$(grep -rnE -- "$USES_LOCAL_RX" "$GA_SCAN_DIR" 2>/dev/null || true)"
if [[ -z "$USES_LOCAL_HITS" ]]; then
  ok "W4dt no LINE under .github/ (any file type, comments included) spells a local \`uses: ./…\` — a text net beside W4d's parse, blind to a next-line value by construction"
else
  bad "W4dt a line under .github/ spells a local \`uses: ./…\`: $(printf '%s\n' "$USES_LOCAL_HITS" | tr '\n' ' ')"
fi
USES_PC_TXT="$(grep -rlE -- "$USES_LOCAL_RX" "$WORK/uses-pc" 2>/dev/null | grep -c . || true)"
check "W4dtpc the W4dt grep is alive (it finds the planted single-line local references)" \
  "2" "$USES_PC_TXT"

# ── W4c/W4f: the job universe this guard reasons about is really the whole one ─
# W1's difference is computed over the jobs in ci.yml, so "the trunk is green"
# means "every job IN THIS FILE passed". A SECOND workflow file would carry checks
# that auto-beta's `needs` cannot even name (GitHub has no cross-file `needs`), and
# a reviewer proved the gap by dropping in a second workflow whose job just
# `exit 1`s — this guard stayed green. So this used to pin `ci.yml` as the ONLY
# workflow file, full stop.
#
# 🔴 THAT PIN HAD TO MOVE when the owner's 2026-08-28 ruling (rc-f9d284c0aef7)
# admitted a second workflow, and the interesting part is WHAT IT MOVED TO. The
# reason above is not about GA at all — it is about the SET OF CHECKS A TRUNK
# COMMIT PRODUCES. Re-read it with that in mind and the real precondition is
# narrower than "one file":
#
#     a second workflow only widens the set auto-beta is failing to wait for if it
#     RUNS on the events auto-beta's `needs` are evaluated against — push and
#     pull_request.
#
# A workflow_dispatch-only file schedules ZERO jobs on a push and ZERO on a pull
# request. It contributes no check run to any trunk commit, so the set W1 reasons
# about and the set GitHub actually produces stay identical, and every W1 assertion
# means exactly what it meant before. That — not the filename — is the precondition,
# and W4f pins it directly.
#
# ⚠️ THE WEAKENING THIS DOES ACCEPT, stated rather than buried: `on:` is now the
# only thing standing between a second file and W1's blind spot. Add `push:` to
# ga-promote.yml and W4f reddens; that is the whole of the protection. W1 itself
# was NOT widened to look across files, and it still cannot: a second workflow that
# somehow ran on push would carry jobs no `needs` can name. If a future workflow
# needs a push or pull_request trigger, this is a decision to take deliberately —
# fold the job into ci.yml, or rework W1 to read every file — NOT a matter of
# adding a key to the allowlist below.
WF_FILES="$(cd "$WF_DIR" && ls -1 2>/dev/null | LC_ALL=C sort | tr '\n' ' ' | sed 's/ $//')"
check "W4c the workflow directory holds exactly ci.yml and the one actor-gated GA workflow" \
  "ci.yml ga-promote.yml" "$WF_FILES"

# W4f: …and everything that is NOT ci.yml fires on manual dispatch and nothing
# else. Read from the PARSED document, per file, so a trigger written in any legal
# spelling is seen; a file that cannot be parsed is reported, never skipped.
WF_TRIGGERS=""
for f in "$WF_DIR"/*; do
  b="$(basename "$f")"
  [[ "$b" == "ci.yml" ]] && continue
  if parse_yaml "$f" "$WORK/wf-trig.json"; then
    t="$(python3 - "$WORK/wf-trig.json" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1]))
found = [k for k in ("true", "on") if k in doc]
if len(found) != 1:
    print("on-keys=%s" % (",".join(found) or "none")); raise SystemExit
got = doc[found[0]]
if isinstance(got, dict):
    print(",".join(sorted(got)))
elif isinstance(got, list):
    print(",".join(sorted(str(x) for x in got)))
else:
    print(str(got))
PY
)"
  else
    t="PARSEFAIL"
  fi
  WF_TRIGGERS+="$b=$t "
done
check "W4f every workflow other than ci.yml is workflow_dispatch-ONLY (that is what keeps it out of every push/PR run, and therefore out of the job universe W1 reasons about)" \
  "ga-promote.yml=workflow_dispatch " "$WF_TRIGGERS"

# ── W5: the publish invocation's shape ─────────────────────────────────────
check "W5 exactly one step invokes bin/release publish" "1" "$(q publish-call-count)"
check "W5 that call carries --no-settle, --target = the triggering SHA, --beta = the computed tag" \
  "no-settle,target-sha,beta-from-tag-step" "$(q publish-call-shape)"
check "W5b the publish step is gated on the staleness check (a re-run of an old main run must not republish)" \
  "gated" "$(q publish-gated-on-freshness)"

# ── W5c: the staleness verdict is REAL, driven, not read ───────────────────
# W5b only says the publish step hangs off `steps.<x>.outputs.<y>`. That is a
# statement about WIRING and it survives everything that actually matters: gut
# the comparison to `fresh=yes` unconditionally, drop the `git rev-parse
# FETCH_HEAD` half, invert the branches — W5b stays green, and a re-run of an old
# main run publishes an OLDER tree under a HIGHER version number, which the
# station then admits by semver order (server/ocserverd/update_check.go).
#
# So this RUNS the step's own `run:` body, lifted verbatim out of ci.yml, against
# a `git` that is the oracle. The oracle keeps TWO WORLDS APART, and that is the
# whole of its value: what main's head is ON THE REMOTE, versus what this runner
# has CHECKED OUT (in a real run, `github.sha` — the very commit whose freshness
# is in question). A body that asks the remote gets a truthful answer about the
# remote; a body that asks the local tree gets the trigger commit back, exactly as
# real git would. The gate must OPEN when those two agree and CLOSE when they do
# not.
#
# 🔴 WHAT THIS DOES AND DOES NOT CATCH — the honest list, because the previous
# version of this comment claimed a general one and it was false.
#   CAUGHT, each verified against a mutant that was actually applied and run:
#     · `HEAD_SHA="$(git rev-parse HEAD)"` — dropping the remote half. Under an
#       oracle that answered every rev-parse with the same value this scored a
#       clean 51 ok / 0 failed, while in a real run it makes the comparison a
#       tautology (checkout sits at github.sha) and every re-run of an old run
#       republishes. It reddens now because HEAD is answered with the trigger sha.
#     · an extra `|| [[ "$GITHUB_REF_NAME" == "main" ]]` on the comparison — an
#       escape hatch that is inert in a bare environment and always-on in the real
#       one. The stale case is therefore driven a SECOND time under a populated
#       GitHub Actions environment (CI_ENV below).
#   NOT CAUGHT, known and deliberately not papered over:
#     · anything GitHub evaluates BEFORE the body exists — nothing local
#       evaluates Actions expressions, so W5b/W1r pin those as text, which is a
#       weaker claim. Measured: widening the publish gate to
#       `steps.freshness.outputs.fresh == 'yes' || github.event_name == 'push'`
#       leaves the gate permanently open on main and this whole file still scores
#       52 ok / 0 failed. Everything below drives the BODY; the `if:` that decides
#       whether the body's verdict is honoured is out of reach here.
#     · an escape hatch keyed on a variable CI_ENV does not name (an invented
#       `OC_FORCE_BETA`, a repository or organisation variable). The list below is
#       the documented Actions environment, not the set of all names.
#
# ⚠️ THE ASSERTION LIVES HERE AND NOT IN ci.yml ON PURPOSE. A check that a file
# contains X, kept inside that same file, is satisfied by whoever deletes X and
# leaves a bare X-shaped line behind — this repo has already been bitten by that
# exact shape (a marker living in the file it guarded). The workflow cannot
# execute a self-test of its own step either: the step only runs on a green push
# to main, which is precisely the run this is protecting.
GATE_REF="$(q freshness-gate-ref)"
if [[ "$GATE_REF" == "-" ]]; then
  bad "W5c the publish step's \`if\` does not compare a step output to a literal, so there is no staleness verdict to drive: $(q publish-gated-on-freshness)"
else
  read -r GATE_STEP GATE_OUT GATE_OPEN <<<"$GATE_REF"
  ok "W5c the publish gate reads steps.$GATE_STEP.outputs.$GATE_OUT == '$GATE_OPEN'"

  FRESHDIR="$WORK/freshness"; mkdir -p "$FRESHDIR/bin"
  # git: the oracle AND a tripwire. It records every call, so "the body never ran"
  # cannot be mistaken for "the body ran and agreed" — an empty or unextractable
  # run: would otherwise leave GITHUB_OUTPUT empty in BOTH cases and score as a
  # clean pass. And it ANSWERS BY REF, because a stub that returns one value for
  # every rev-parse cannot tell "did you ask the remote?" from "did you ask the
  # commit you are standing on?", which is the one distinction the whole step is
  # about. Refs that name the remote's main resolve to $OC_PC_MAIN_HEAD; refs that
  # name this checkout — HEAD, @, and the LOCAL main branch, which actions/checkout
  # leaves at github.sha on a push — resolve to $OC_PC_TRIGGER_SHA.
  #
  # A ref it does not recognise is a HARD ERROR, not a guess: if the step is
  # rewritten to ask git some other way, that has to be a conscious edit here
  # rather than a silent pass on a value the oracle made up.
  cat > "$FRESHDIR/bin/git" <<'GITSH'
#!/usr/bin/env bash
echo "git $*" >> "$OC_PC_GITLOG"
case "${1:-}" in
  rev-parse)
    shift
    ref=""
    for a in "$@"; do case "$a" in -*) ;; *) ref="$a"; break ;; esac; done
    case "$ref" in
      FETCH_HEAD|origin/main|origin/HEAD|refs/remotes/origin/main|remotes/origin/main)
        echo "$ref" >> "$OC_PC_REMOTELOG"
        printf '%s\n' "$OC_PC_MAIN_HEAD" ;;
      ""|HEAD|@|main|refs/heads/main)
        printf '%s\n' "$OC_PC_TRIGGER_SHA" ;;
      *)
        if [[ "$ref" =~ ^[0-9a-f]{40}$ ]]; then
          printf '%s\n' "$ref"
        else
          echo "oc-oracle: bin/tests/auto-beta-guard.sh's git stub does not know the ref '$ref'." >&2
          echo "oc-oracle: the freshness step now asks git something this guard cannot answer truthfully; teach the oracle whether '$ref' means the REMOTE's main or THIS checkout." >&2
          exit 128
        fi ;;
    esac ;;
  ls-remote)
    echo "ls-remote $*" >> "$OC_PC_REMOTELOG"
    printf '%s\trefs/heads/main\n' "$OC_PC_MAIN_HEAD" ;;
  *) : ;;
esac
exit 0
GITSH
  chmod +x "$FRESHDIR/bin/git"

  if ! q step-run "$GATE_STEP" > "$FRESHDIR/step.sh" 2>"$FRESHDIR/step.err"; then
    bad "W5c could not lift the '$GATE_STEP' step out of ci.yml: $(tr '\n' ' ' < "$FRESHDIR/step.err")"
  else
    # drive <main-head> <trigger-sha> — sets FRESH_RC, FRESH_VAL, FRESH_CALLS,
    # FRESH_REMOTE. Anything in FRESH_EXTRA_ENV is added to the child environment.
    FRESH_EXTRA_ENV=()
    drive() {
      : > "$FRESHDIR/out.txt"; : > "$FRESHDIR/git.log"; : > "$FRESHDIR/remote.log"
      env ${FRESH_EXTRA_ENV[@]+"${FRESH_EXTRA_ENV[@]}"} \
          PATH="$FRESHDIR/bin:$PATH" \
          GITHUB_OUTPUT="$FRESHDIR/out.txt" \
          OC_PC_GITLOG="$FRESHDIR/git.log" \
          OC_PC_REMOTELOG="$FRESHDIR/remote.log" \
          OC_PC_MAIN_HEAD="$1" \
          OC_PC_TRIGGER_SHA="$2" \
          bash "$FRESHDIR/step.sh" >"$FRESHDIR/step.log" 2>&1
      FRESH_RC=$?
      FRESH_VAL="$(sed -n "s/^$GATE_OUT=//p" "$FRESHDIR/out.txt" | tail -1)"
      FRESH_CALLS="$(grep -c . "$FRESHDIR/git.log" || true)"
      FRESH_REMOTE="$(grep -c . "$FRESHDIR/remote.log" || true)"
    }

    SHA_A="1111111111111111111111111111111111111111"
    SHA_B="2222222222222222222222222222222222222222"

    drive "$SHA_A" "$SHA_A"
    check "W5c FRESH (main's head IS this run's commit): the step exits clean" "0" "$FRESH_RC"
    check "W5c FRESH: it wrote $GATE_OUT=$GATE_OPEN, so the publish gate OPENS" \
      "$GATE_OPEN" "$FRESH_VAL"
    FRESH_CALLS_FRESH="$FRESH_CALLS"
    FRESH_REMOTE_FRESH="$FRESH_REMOTE"

    drive "$SHA_B" "$SHA_A"
    check "W5c STALE (main has moved on): the step still exits clean — a stale re-run SKIPS, it does not fail" \
      "0" "$FRESH_RC"
    if [[ "$FRESH_VAL" == "$GATE_OPEN" ]]; then
      bad "W5c STALE: the step still wrote $GATE_OUT=$GATE_OPEN — an old main run would republish OLDER code under a HIGHER version. The comparison is gone, inverted, or unconditional."
    else
      ok "W5c STALE: $GATE_OUT='${FRESH_VAL:-<unwritten>}' ≠ '$GATE_OPEN', so the publish gate CLOSES"
    fi

    # ── The same stale case again, this time inside a POPULATED GitHub Actions
    # environment. A condition widened with `|| [[ "$GITHUB_REF_NAME" == "main" ]]`
    # is invisible in a bare environment and permanently open in the real one, so
    # driving the body only bare answers the wrong question. This names the
    # documented Actions variables; it does not and cannot cover a hatch keyed on
    # a name that is not on this list.
    FRESH_EXTRA_ENV=(
      CI=true GITHUB_ACTIONS=true RUNNER_OS=Linux RUNNER_ARCH=X64
      GITHUB_EVENT_NAME=push GITHUB_REF=refs/heads/main GITHUB_REF_NAME=main
      GITHUB_REF_TYPE=branch GITHUB_BASE_REF= GITHUB_HEAD_REF=
      GITHUB_DEFAULT_BRANCH=main GITHUB_REPOSITORY=pkyosx/OffiCraft
      GITHUB_REPOSITORY_OWNER=pkyosx GITHUB_WORKFLOW=ci GITHUB_JOB="$JOB"
      GITHUB_RUN_ATTEMPT=2 GITHUB_ACTOR=someone GITHUB_TRIGGERING_ACTOR=someone
      GITHUB_SHA="$SHA_A" GITHUB_WORKSPACE=/home/runner/work/OffiCraft/OffiCraft
    )
    drive "$SHA_B" "$SHA_A"
    FRESH_EXTRA_ENV=()
    if [[ "$FRESH_VAL" == "$GATE_OPEN" ]]; then
      bad "W5c STALE under a real GitHub Actions environment (GITHUB_REF_NAME=main, event=push, run attempt 2): the step wrote $GATE_OUT=$GATE_OPEN even though main has moved on. If the bare stale case above passed, the comparison has an escape hatch that only opens on CI; if it failed too, read that one first — this case adds nothing to its diagnosis."
    else
      ok "W5c STALE under a populated GitHub Actions environment: still $GATE_OUT='${FRESH_VAL:-<unwritten>}' ≠ '$GATE_OPEN' — the verdict is not widened by any of the documented CI variables"
    fi

    # Positive control on the harness itself: the body must really have consulted
    # git in BOTH runs, and at least one of those calls must have asked for the
    # REMOTE's main. A verdict reached without ever asking the remote is a
    # comparison against the commit we are standing on, which is always equal to
    # itself.
    if [[ "$FRESH_CALLS_FRESH" -lt 1 || "$FRESH_CALLS" -lt 1 ]]; then
      bad "W5cpc the lifted '$GATE_STEP' body never invoked git (fresh=$FRESH_CALLS_FRESH stale=$FRESH_CALLS): the verdicts above were not reached by comparing against main's head"
    elif [[ "$FRESH_REMOTE_FRESH" -lt 1 ]]; then
      bad "W5cpc the lifted '$GATE_STEP' body called git but never resolved the REMOTE's main (FETCH_HEAD / origin/main / ls-remote): whatever it compared against, it was not what main points at now"
    else
      ok "W5cpc the lifted body really executed, consulted git ($FRESH_CALLS_FRESH and $FRESH_CALLS calls) and resolved the remote's main ($FRESH_REMOTE_FRESH times in the fresh case)"
    fi
  fi
fi

# ── W6: the checkout is deep AND brings tags ───────────────────────────────
check "W6 $JOB checks out with fetch-depth: 0 AND fetch-tags: true (worktree add <sha> + the tag union need both)" \
  "depth=0,tags=True" "$(q checkout-with)"

# ═══════════════════════════════════════════════════════════════════════════
# N — the version rule in bin/next-beta-tag, driven with fabricated tag lists.
# ═══════════════════════════════════════════════════════════════════════════
echo "── N: the next-beta-tag version rule"

# nbt — run ONE function out of the real bin/next-beta-tag with TAGS on stdin, in
# a subshell so its `set -e` / die-with-exit lands on the case and not here.
# stdout and stderr are captured together on purpose: the chosen BASE is reported
# on stderr, and it is the only observable that can tell semver ordering apart
# from string ordering (see N1).
nbt() { # nbt <tags-newline-separated> <fn> [args...]
  local tags="$1"; shift
  OUT="$(printf '%s\n' "$tags" | bash -c '
    set -uo pipefail
    source "$1" || exit $?
    fn="$2"; shift 2
    "$fn" "$@"
  ' _ "$NBT" "$@" 2>&1)"
  RC=$?
}
nbt_next() { # nbt_next <tags> — sets RC, TAG (stdout only) and BASE
  local tags="$1"
  TAG="$(printf '%s\n' "$tags" | bash -c '
    set -uo pipefail
    source "$1" || exit $?
    nbt_next_beta_tag
  ' _ "$NBT" 2>"$WORK/nbt.err")"
  RC=$?
  BASE="$(sed -n 's/^\[next-beta-tag\] base: \([^ ]*\).*/\1/p' "$WORK/nbt.err")"
}

# N0 — the TRANSITION shape this repo actually has on the tree right now: the
# greatest existing tag is an OLD-STYLE `-beta.1` (246 releases exist, a batch of
# them suffixed that way, and none of that history gets rewritten by this
# ruling). The rule is patch+1 rather than an in-place suffix bump precisely
# because `-beta.N` sorts BELOW the same X.Y.Z under semver, so anything that did
# not move the patch could not come out greater than its own base — and that
# still holds with the suffix gone from the OUTPUT: the computed tag has none,
# but the base that decided it can. The trailing non-semver tag is what pins the
# other half of the rule — such a tag must be IGNORED, not rejected: mutate the
# filter into a refusal and these three checks are the only ones that go red.
nbt_next "v0.5.77-beta.1
v0.5.78-beta.1
v0.5.9
some-tag"
check "N0 the repo's own transition shape: base is the greatest OLD-STYLE beta, next bumps the patch with NO suffix" "0" "$RC"
check "N0 …base chosen (still the suffixed tag — precedence reads it correctly)" "v0.5.78-beta.1" "$BASE"
check "N0 …next tag has NO -beta.N suffix" "v0.5.79" "$TAG"

# N1 — THE case that separates semver ordering from string ordering. As strings,
# "v1.0.0-beta.9" > "v1.0.0-beta.10"; under semver, numeric prerelease identifiers
# compare NUMERICALLY, so beta.10 wins. The computed tag is v1.0.1-beta.1 either
# way, which is exactly why this asserts the BASE: a `sort -V`-free string
# comparison would pass an assertion on the tag alone and be wrong.
nbt_next "v1.0.0-beta.9
v1.0.0-beta.10"
check "N1 beta.10 outranks beta.9 (numeric, not lexical, prerelease compare)" "v1.0.0-beta.10" "$BASE"
check "N1 …and the next tag is the patch bump off it, no suffix" "v1.0.1" "$TAG"

# N2 — a candidate set with NO prerelease at all. A release outranks any
# prerelease of the same X.Y.Z, and the patch numbers must compare numerically:
# as strings "v0.5.9" > "v0.5.78".
nbt_next "v0.5.7
v0.5.8
v0.5.9
v0.5.78"
check "N2 releases only: greatest is by NUMERIC patch, not string order" "v0.5.78" "$BASE"
check "N2 …and the next tag is a release above it, no suffix" "v0.5.79" "$TAG"

# N3 — an EMPTY candidate set is a hard failure, not "start from v0.0.1-beta.1".
# The realistic cause is a query that broke, and the friendly fallback would
# republish a version from the beginning of history over a repo with a hundred
# releases. Non-semver tags are ignored, so a list of only those is empty too.
nbt "some-tag
not-a-version" nbt_next_beta_tag
if [[ "$RC" == "0" ]]; then
  bad "N3 an empty candidate set must FAIL (it exited 0 and printed: $(printf '%s' "$OUT" | tr '\n' '|'))"
else
  case "$OUT" in
    *"no semver tag"*) ok "N3 an empty candidate set FAILS and says why (rc=$RC)" ;;
    *) bad "N3 an empty candidate set fails but does not name the reason (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
  esac
fi

# N4 — the collision refusal, tested directly. See bin/next-beta-tag's honest note
# on reachability: the rule cannot currently produce a taken name, so this binds
# the FUNCTION rather than pretending to reach it through the computation.
nbt "v0.5.79-beta.1
v0.5.78-beta.1" nbt_assert_tag_absent v0.5.79-beta.1
if [[ "$RC" == "0" ]]; then
  bad "N4 a computed tag that already exists must be REFUSED (it exited 0)"
else
  case "$OUT" in
    *"ALREADY EXISTS"*) ok "N4 a tag that already exists is REFUSED, not incremented or overwritten (rc=$RC)" ;;
    *) bad "N4 refused but did not say the tag already exists (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
  esac
fi
# …and the sentinel in the other direction, so N4 is not passing on any error.
nbt "v0.5.78-beta.1
v0.5.77-beta.1" nbt_assert_tag_absent v0.5.79-beta.1
check "N4b sentinel — an unused tag passes the collision check" "0" "$RC"

# ═══════════════════════════════════════════════════════════════════════════
# T — THE TRANSITION: this repo's 246 existing releases include a batch tagged
# the OLD way (vX.Y.Z-beta.1), and the no-suffix ruling does not rewrite any of
# them. The day the new rule ships, the GREATEST existing tag can still be one
# of those old-style ones — auto-beta's very next run has to read it correctly
# and still hand back a bare, unsuffixed tag. N0 above already pins this shape
# once; T0/T1 pin it explicitly and separately so it cannot be dropped as
# collateral damage of some other N-case edit.
# ═══════════════════════════════════════════════════════════════════════════
echo "── T: transition — old-style suffixed tags still on the tree"

# T0 — the greatest existing tag is an old-style beta, nothing newer exists at
# all (no plain release above it yet). The computed tag must still be bare.
nbt_next "v0.5.78-beta.1
v0.5.77
v0.5.9"
check "T0 greatest existing tag is old-style suffixed → still picked as base" "v0.5.78-beta.1" "$BASE"
case "$TAG" in
  *-beta*) bad "T0 next tag must carry NO suffix even though its base did (got: $TAG)" ;;
  "v0.5.79") ok "T0 next tag is bare: v0.5.79" ;;
  *) bad "T0 next tag wrong (got: $TAG, want: v0.5.79)" ;;
esac

# T1 — an old-style beta for the SAME X.Y.Z as a plain release that also exists.
# Semver precedence must still rank the release above the prerelease (this was
# already true before the ruling; the point here is that it stays true once the
# OUTPUT stops carrying the suffix that used to make the ordering easy to eyeball).
nbt_next "v2.1.4
v2.1.4-beta.1
v2.1.3"
check "T1 a release outranks an old-style beta of the same X.Y.Z" "v2.1.4" "$BASE"
case "$TAG" in
  *-beta*) bad "T1 next tag must carry NO suffix (got: $TAG)" ;;
  "v2.1.5") ok "T1 next tag is the bare patch bump: v2.1.5" ;;
  *) bad "T1 next tag wrong (got: $TAG, want: v2.1.5)" ;;
esac

# ═══════════════════════════════════════════════════════════════════════════
# S — WHO ASSEMBLED THE CANDIDATE SET. Everything above feeds a fabricated tag
# list straight into nbt_compute, so none of it asks the question that matters
# most: is the set complete?
#
# WHY THIS BLOCK EXISTS (it was MISSING, and three mutants walked through the
# gap): an independent review turned `gh release list … || nbt_die` into
# `|| true`, did the same to `git tag --list`, and then deleted the GitHub source
# altogether — all three passed the suite green. The two-source union and the
# "either source failing is fatal" rule are the properties bin/next-beta-tag's
# header spends a page arguing for, and nothing was holding them. Worse, when the
# set is truncated BOTH defences fail at once and silently: the base comes from
# half the world, and nbt_assert_tag_absent then checks the computed tag against
# that same half.
#
# These cases drive nbt_collect_candidate_tags for real — a throwaway git repo via
# OC_RELEASE_SRC, and a `gh` PATH shim. No network, no real releases.
echo "── S: the candidate set is assembled from BOTH sources, or it fails"

SWORK="$WORK/src-tags"; SSHIM="$WORK/src-shim"
mkdir -p "$SWORK" "$SSHIM"
(
  cd "$SWORK"
  git init -q .
  git config user.email guard@example.invalid
  git config user.name guard
  git commit -q --allow-empty -m fixture
) >/dev/null 2>&1 || fatal "S setup: could not build the fixture git repo"
git -C "$SWORK" tag v0.1.0 >/dev/null 2>&1 || fatal "S setup: could not tag the fixture repo"

# The gh shim answers from env, so each case controls rc and payload independently.
# It NEVER reaches the network; `gh` is not even consulted for anything else.
cat > "$SSHIM/gh" <<'SH'
#!/usr/bin/env bash
printf '%s' "${FAKE_GH_OUT:-}"
[[ -n "${FAKE_GH_OUT:-}" ]] && printf '\n'
exit "${FAKE_GH_RC:-0}"
SH
chmod +x "$SSHIM/gh"

# collect — run the REAL nbt_collect_candidate_tags (optionally piped into the
# real nbt_next_beta_tag) against the fixture repo and the shimmed gh.
collect() { # collect <src-dir> [--next]
  local src="$1" mode="${2:-}"
  OUT="$(PATH="$SSHIM:$PATH" OC_RELEASE_SRC="$src" OC_RELEASE_GH_REPO="guard/fixture" \
    FAKE_GH_OUT="${FAKE_GH_OUT:-}" FAKE_GH_RC="${FAKE_GH_RC:-0}" \
    bash -c '
      set -uo pipefail
      source "$1" || exit $?
      if [[ "${2:-}" == "--next" ]]; then
        nbt_collect_candidate_tags | nbt_next_beta_tag
      else
        nbt_collect_candidate_tags
      fi
    ' _ "$NBT" "$mode" 2>&1)"
  RC=$?
}

# S0 — the sentinel: both sources answer, and the result is really the UNION.
# Without this the failure cases below could all be passing for the wrong reason.
FAKE_GH_RC=0 FAKE_GH_OUT="v0.2.0" collect "$SWORK"
check "S0 both sources answer → exits 0" "0" "$RC"
check "S0 …and the candidate set is the UNION of both (git v0.1.0 + gh v0.2.0)" \
  "v0.1.0
v0.2.0" "$(printf '%s\n' "$OUT" | grep -v '^$')"

# S1 — a tag that exists ONLY on the GitHub side must move the base. This is the
# case that kills "delete the gh source entirely": with git alone the base would
# be v0.1.0, so the answer names which sources were really consulted.
FAKE_GH_RC=0 FAKE_GH_OUT="v9.9.0" collect "$SWORK" --next
check "S1 a release tag that exists ONLY on the gh side decides the base" "0" "$RC"
case "$OUT" in
  *"base: v9.9.0"*"v9.9.1"*"-beta"*) bad "S1 …base=v9.9.0 from the gh-only tag, next must be v9.9.1 with NO suffix (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
  *"base: v9.9.0"*"v9.9.1"*) ok "S1 …base=v9.9.0 from the gh-only tag, next=v9.9.1 (no suffix)" ;;
  *) bad "S1 …base=v9.9.0 from the gh-only tag, next=v9.9.1 (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
esac

# S1b — and the mirror, so S1 is not just "gh wins always": a git-only tag that is
# greater must decide it too.
git -C "$SWORK" tag v0.3.0 >/dev/null 2>&1 || fatal "S setup: could not add the second fixture tag"
FAKE_GH_RC=0 FAKE_GH_OUT="v0.2.0" collect "$SWORK" --next
case "$OUT" in
  *"base: v0.3.0"*) ok "S1b …and a git-only tag that is greater decides it as well" ;;
  *) bad "S1b …and a git-only tag that is greater decides it as well (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
esac

# S2 — gh exits non-zero. Fatal, and the message has to name gh, or the operator
# goes hunting the wrong source.
#
# The payload is deliberately NON-EMPTY (and well-formed), which is the only way
# this case reaches the rc rule ON ITS OWN: with an empty payload the very next
# rule ("returned rc=0 but ZERO releases") catches it too, and the two become
# indistinguishable from outside. Output from a command that failed is not an
# answer. (Same trick, same reason, as release-guard.sh's S1b.)
FAKE_GH_RC=1 FAKE_GH_OUT="v0.2.0" collect "$SWORK"
if [[ "$RC" == "0" ]]; then
  bad "S2 gh exiting non-zero must be FATAL (it exited 0: $(printf '%s' "$OUT" | tr '\n' '|'))"
else
  case "$OUT" in
    *"gh release list failed"*) ok "S2 gh exiting non-zero is FATAL and names gh (rc=$RC)" ;;
    *) bad "S2 gh exiting non-zero is fatal but does not name gh (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
  esac
fi

# S3 — THE SILENT ONE. gh exits ZERO with nothing to say. Falling back to the git
# tags alone is rc=0 and a wrong number nobody questions, so this must be fatal.
FAKE_GH_RC=0 FAKE_GH_OUT="" collect "$SWORK"
if [[ "$RC" == "0" ]]; then
  bad "S3 gh answering rc=0 with ZERO releases must be FATAL, not a silent single-source fallback (it exited 0: $(printf '%s' "$OUT" | tr '\n' '|'))"
else
  case "$OUT" in
    *"ZERO releases"*) ok "S3 gh answering rc=0 with ZERO releases is FATAL (no silent single-source fallback) (rc=$RC)" ;;
    *) bad "S3 gh rc=0 + empty is fatal but does not say why (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
  esac
fi

# S4 — the same silence on the git side, which is the shape a checkout without
# tags produces on a runner: rc=0, zero tags, and the whole version computed off
# GitHub releases alone.
SNOTAGS="$WORK/src-notags"; mkdir -p "$SNOTAGS"
(
  cd "$SNOTAGS"
  git init -q .
  git config user.email guard@example.invalid
  git config user.name guard
  git commit -q --allow-empty -m fixture
) >/dev/null 2>&1 || fatal "S setup: could not build the tagless fixture repo"
FAKE_GH_RC=0 FAKE_GH_OUT="v0.2.0" collect "$SNOTAGS"
if [[ "$RC" == "0" ]]; then
  bad "S4 git answering rc=0 with ZERO tags must be FATAL (a checkout without tags looks exactly like this) (it exited 0: $(printf '%s' "$OUT" | tr '\n' '|'))"
else
  case "$OUT" in
    *"ZERO tags"*) ok "S4 git answering rc=0 with ZERO tags is FATAL, and points at fetch-tags (rc=$RC)" ;;
    *) bad "S4 git rc=0 + no tags is fatal but does not say why (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
  esac
fi

# S5 — the git source erroring outright (here: not a repository at all).
# ⚠️ Unlike S2 this case CANNOT isolate its rule: a failing `git tag --list` prints
# nothing, so the empty-answer rule (S4's) would catch it as well. Both fail under
# messages naming the git source, so from outside the two are indistinguishable —
# recorded rather than papered over, because there is no way to make git exit
# non-zero AND emit a tag list.
FAKE_GH_RC=0 FAKE_GH_OUT="v0.2.0" collect "$WORK/definitely-not-a-repo"
if [[ "$RC" == "0" ]]; then
  bad "S5 git exiting non-zero must be FATAL (it exited 0)"
else
  case "$OUT" in
    *"could not list git tags"*) ok "S5 git exiting non-zero is FATAL and names the git source (rc=$RC)" ;;
    *) bad "S5 git exiting non-zero is fatal but does not name the git source (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
  esac
fi

summary
[[ "$FAIL" == "0" ]] || exit 1
exit 0
