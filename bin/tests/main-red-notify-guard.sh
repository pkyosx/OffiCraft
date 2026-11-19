#!/usr/bin/env bash
# bin/tests/main-red-notify-guard.sh — the notify-on-red job in
# .github/workflows/ci.yml is an ENUMERATION, and this is what stops the
# enumeration from going stale in silence.
#
# WHY THIS EXISTS (T-5d3b)
# The workflow's notify job depends on `needs: [<every other job>]`. GitHub has
# no wildcard for "all jobs", so adding ANOTHER job and forgetting this line
# does not fail anything: the new job's red simply stops being reported, and the
# workflow stays green-looking in exactly the way this feature exists to prevent.
# A comment asking people to remember is not a mechanism. This is.
#
# EVERY other job means every one, including a job that is not a gate: T-9fe3's
# auto-beta publishes the release the station actually runs, so a failure there is
# "merged" and "reachable" drifting apart — the very silence auto-beta was added
# to end. The edge only goes this way; auto-beta must NOT need this job, or the
# two would form a `needs` cycle and GitHub would schedule zero jobs. That half is
# bin/tests/auto-beta-guard.sh's to enforce, not this file's.
#
# WHAT IT ASSERTS
#   0. the workflow file PARSES as YAML, under ONE named parser. Not a
#      formality: this ticket shipped a ci.yml whose `run:` block scalar was
#      ended early by a continuation line at column 1. Every local gate and
#      every guard here passed it, because nothing local had ever parsed the
#      file; GitHub answered with a startup failure in which zero jobs ran and
#      the pull request carried zero checks.
#      ⚠️ WHAT THIS PROVES, EXACTLY: that one YAML parser can read the file.
#      GitHub's workflow parser is neither psych nor PyYAML, so a pass here is
#      NECESSARY, NOT SUFFICIENT — it catches the class of breakage that
#      shipped; it does not promise GitHub will accept the file.
#   1. the notify job exists, exactly once
#   2. its gate is `failure() && github.ref == 'refs/heads/main'`
#      — widened to always() it would message every pull request; widened by
#        dropping the ref test it would message every working branch.
#      An outer `${{ }}` wrapper is normalised away first (both spellings are
#      legal GitHub); everything inside is compared verbatim.
#   3. every OTHER job declared in the file appears in its `needs:` list
#   4. the webhook is reached through a repository secret, and this file carries
#      no URL literal outside comments (the URL is the credential: the inlet
#      authenticates on a token inside it, so a literal here is a public one)
#
#   ⚠️ HONESTY ABOUT 4's SECOND HALF. "no URL literal" was ALREADY TRUE before
#   this ticket — the parent commit's ci.yml has no `://` outside comments. It
#   is a PRESERVING invariant (a regression fence for the future), not a
#   property T-5d3b established. Assertions 1, 3 and the first half of 4 are the
#   ones that were false before the change and are true after it.
#
# WHAT IT DOES NOT ASSERT — read this before trusting a green
#   It never sends anything and never talks to GitHub. It cannot tell you that
#   the secret is set on the repository, that the endpoint is alive, or that a
#   message arrived: the inlet answers an identical silent 200 to every request
#   INCLUDING one with a bad token, so TOKEN VALIDITY IS ONLY OBSERVABLE AT THE
#   RECEIVING END. This guard covers the shipped CONDITION; the delivery half is
#   verified by actually receiving a message, and that evidence lives on the
#   ticket, not here.
#
#   The job-name scan is line-shaped (two-space keys under `jobs:`) rather than
#   a walk of the parsed document. That would be a silent hole — a job written
#   in a shape the scan does not know would simply not be counted — so the scan
#   is paired with a DELIBERATELY LOOSE second pass: every two-space-indented,
#   non-comment line in the jobs region must be parseable by the strict pattern.
#   One line that the loose pass sees and the strict pass cannot read is a FAIL,
#   not a shrug. The guard is allowed to not understand a line; it is not
#   allowed to not understand it QUIETLY and still claim coverage.
#   ⚠️ That pass says NO LINE WAS SKIPPED IN SILENCE. It does NOT say every line
#   it read is a real job — a two-space key is only a job id because the region
#   is bounded to the `jobs:` block (below), not because the scan can tell.
#
# 🔴 THIS FILE MUST PARSE UNDER APPLE'S BASH 3.2, NOT JUST YOURS.
# bin/tests/run.sh dispatches guards with `bash <file>`, which on a developer's
# Mac is usually a homebrew bash 5.x on PATH — but on the runner that executes
# this suite (the bin-guards cell since T-4d88; macos-host-gates before it)
# it is /bin/bash 3.2.57. That difference is invisible to a local green run.
# It has already bitten this file once: an awk program embedded as $(cat <<'AWK'
# ...) contained a literal apostrophe inside a character class, and bash 3.2's
# parser — which mishandles quotes inside a heredoc nested in a command
# substitution — died with "unexpected EOF while looking for matching ')'".
# Locally: rc=0. On the runner: the whole guard aborted and macos-host-gates went
# red. Apostrophes in the awk source are therefore written \047, and any change
# here must be re-run with an EXPLICIT `/bin/bash bin/tests/main-red-notify-guard.sh`
# before it is believed. Do not use bash-4-only syntax (${var,,}, declare -A, &>>).
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
WF="$REPO_ROOT/.github/workflows/ci.yml"
NOTIFY_JOB="notify-main-red"
SECRET_NAME="OC_MAIN_RED_WEBHOOK_URL"
WANT_IF="failure() && github.ref == 'refs/heads/main'"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

echo "[main-red-notify-guard] $WF"

if [[ ! -f "$WF" ]]; then
  echo "  FAIL — workflow file not found: $WF" >&2
  echo "[main-red-notify-guard] 0 ok, 1 failed"
  exit 1
fi

# ── 0. the file parses as YAML ──────────────────────────────────────────────
# ONE parser, named, with NO fallback. An earlier cut tried python3+PyYAML and
# fell back to ruby+psych, and the two DISAGREE on real files: an unknown tag
# (`runs-on: !Weird ubuntu-latest`) is a hard error to PyYAML and a clean parse
# to psych; a `%YAML 1.2` directive is the reverse. Same file, same guard,
# rc flipped between 1 and 0 depending on which interpreter the host happened to
# have. A second parser is a second opinion, and a gate whose verdict depends on
# what is installed is not a gate.
#
# ruby is the one that is always there. This guard's ONLY execution surface is
# macOS — bin/tests/run.sh is reached through bin/ci.sh locally and through the
# bin-guards cell in the cloud, which runs on macOS. macOS ships ruby; the macOS
# runner ships ruby and does NOT have PyYAML. Missing ruby is a FAIL with instructions, never a skip and never
# a quiet downgrade to some other parser: "no parser" must not look like "file
# is fine", and neither must "a different parser".
#
# unsafe_load is deliberate: safe_load rejects aliases and anchors, which are
# legal in a workflow file. The method is CHOSEN by respond_to? rather than by
# rescuing NoMethodError (Ruby 2.6's psych has no unsafe_load_file) — a rescue
# would attach that NoMethodError to any later syntax error as its `cause`, and
# print a backtrace that reads like a broken guard instead of a broken workflow.
#
# Exactly ONE document is required. psych reads a multi-document file by
# silently taking the first one, which would let a second `---` document below
# carry jobs that parse clean here and are invisible to everything downstream —
# and GitHub reads a workflow as a single document in any case.
#
# ⚠️ The ruby source below is ASCII-only ON PURPOSE. `ruby -e` takes its source
# as US-ASCII on the system ruby (2.6), so one em dash in a message here is not
# a typo, it is `invalid multibyte char` — the guard then reports the WORKFLOW
# as unparseable when the workflow is fine. Keep messages plain ASCII.
YAML_VIA="ruby+psych"
RUBY_YAML_PROG='
  n = Psych.parse_stream(File.read(ARGV[0])).children.size
  abort("file contains #{n} YAML documents, not 1: GitHub reads a workflow as a single document") if n != 1
  m = YAML.respond_to?(:unsafe_load_file) ? :unsafe_load_file : :load_file
  YAML.public_send(m, ARGV[0])
'

if ! command -v ruby >/dev/null 2>&1 || ! ruby -ryaml -e '' >/dev/null 2>&1; then
  bad "no \`ruby -ryaml\` on this host, so this guard cannot tell a valid workflow from an invalid one — and a missing parser must never look like 'file is fine'. This guard only runs on macOS, where ruby is part of the system; if it is gone here, put it back (\`xcode-select --install\`, or any ruby on PATH with psych). It is deliberately NOT allowed to fall back to another parser: two parsers disagree, and the disagreement would decide the verdict. A ci.yml that does not parse is a GitHub startup failure: zero jobs run, zero checks appear, and nothing notifies."
elif YAML_ERR="$(ruby -ryaml -e "$RUBY_YAML_PROG" "$WF" 2>&1)"; then
  ok "workflow file parses as YAML ($YAML_VIA) — one parser can read it; GitHub's own parser is a different one, so this is necessary, not sufficient"
else
  bad "workflow file is NOT valid YAML ($YAML_VIA) — GitHub answers this with a startup failure: zero jobs scheduled, zero checks on the PR, and no notification. Parser said: $(printf '%s' "$YAML_ERR" | tr '\n' ' ' | tail -c 300)"
fi

# ── the jobs region ─────────────────────────────────────────────────────────
# The top-level `jobs:` key up to THE NEXT TOP-LEVEL KEY (a column-0 line that
# is not a comment), not to end of file. Restricting to this region is what
# makes the two-space rule mean "job name": `on:` and `concurrency:` above it
# also carry two-space keys (pull_request:, push:, group:) — and so would a
# `defaults:` / `env:` / `run-name:` block written AFTER `jobs:`, all of which
# are legal there. Reading to EOF made `defaults:`'s own `  run:` key look like
# a job named `run`, which the guard then demanded appear in `needs:` — a red
# whose only "fix" is a workflow GitHub would reject. `jobs:` happening to be
# the last top-level key was an unwritten assumption; this removes it rather
# than documenting it.
REGION="$(awk '/^jobs:$/ { inj = 1; next } inj && /^[^ \t#]/ { exit } inj { print }' "$WF")"
if [[ -z "$REGION" ]]; then
  bad "no top-level 'jobs:' key found — the scanner is looking at the wrong shape, so nothing below proves anything"
  echo "[main-red-notify-guard] $PASS ok, $FAIL failed"
  exit 1
fi
# Bounding the region at the next top-level key is what makes a SECOND `jobs:`
# block worth checking for: psych accepts a duplicate top-level key (last one
# wins) and the scan now stops before it, so jobs declared there would be
# covered by nothing at all.
JOBS_KEYS="$(grep -c '^jobs:$' "$WF" || true)"
if [[ "$JOBS_KEYS" != "1" ]]; then
  bad "the file has $JOBS_KEYS top-level 'jobs:' keys (want exactly 1) — the parser takes one of them and this scan reads the other, so jobs in the loser would be covered by nothing"
  echo "[main-red-notify-guard] $PASS ok, $FAIL failed"
  exit 1
fi

# Two passes over the same lines:
#   loose  — ANY two-space-indented, non-blank, non-comment line. Inside the
#            jobs region such a line can only be a job key.
#   strict — GitHub's job id grammar [_a-zA-Z][a-zA-Z0-9_-]*, optionally quoted,
#            with a trailing comment or a flow-style value after the colon.
# Every loose hit must be a strict hit. The gap between them is the set of lines
# this guard cannot read, and it is reported rather than dropped.
JOB_SCAN_AWK="$(cat <<'AWK'
function strip(s) { sub(/[[:space:]]+#.*$/, "", s); sub(/^[[:space:]]*#.*$/, "", s); return s }
/^  [^ \t#]/ {
  line = strip($0)
  if (line ~ /^[[:space:]]*$/) next
  if (line ~ /^  ["\047]?[_a-zA-Z][a-zA-Z0-9_-]*["\047]?:/) {
    id = line
    sub(/^  ["\047]?/, "", id)
    sub(/["\047]?:.*$/, "", id)
    print "JOB\t" id
  } else {
    print "UNPARSED\t" $0
  }
}
AWK
)"
SCAN="$(printf '%s\n' "$REGION" | awk "$JOB_SCAN_AWK")"
JOBS="$(printf '%s\n' "$SCAN" | sed -n 's/^JOB\t//p')"
UNPARSED="$(printf '%s\n' "$SCAN" | sed -n 's/^UNPARSED\t//p')"
JOB_COUNT="$(printf '%s\n' "$JOBS" | grep -c . || true)"

if [[ -n "$UNPARSED" ]]; then
  bad "the job-name scan cannot read these lines in the jobs region, so it does NOT claim to cover the jobs they declare:$(printf '%s\n' "$UNPARSED" | sed 's/^/ | /' | tr '\n' ' ')"
else
  ok "every two-space line in the jobs region is one this scan can read (no line skipped in silence — it does not claim every line it read is a real job)"
fi

# Positive control for the scan itself. A file with a notify job and nothing to
# notify about is not a state worth shipping, and a scanner that matches zero
# job names would otherwise pass every check below by vacuity. ⚠️ This control
# only catches a scan that has died ENTIRELY — it says nothing about a scan that
# misses exactly the newly added job, which is why the loose/strict pass above
# exists and is the check that actually covers that failure.
if [[ "$JOB_COUNT" -ge 2 ]]; then
  ok "job-name scan is alive ($JOB_COUNT jobs found)"
else
  bad "job-name scan found $JOB_COUNT jobs — the scan is broken (or the file has no gate jobs left); every check below would pass vacuously"
  echo "[main-red-notify-guard] $PASS ok, $FAIL failed"
  exit 1
fi

# ── 1. the notify job exists, exactly once ──────────────────────────────────
NOTIFY_DECLS="$(printf '%s\n' "$JOBS" | grep -cxF "$NOTIFY_JOB" || true)"
if [[ "$NOTIFY_DECLS" == "1" ]]; then
  ok "$NOTIFY_JOB is declared exactly once"
else
  bad "$NOTIFY_JOB is declared $NOTIFY_DECLS times (want exactly 1) — a red trunk would tell nobody, or tell them twice"
fi

# The notify job's own block: from its key to the next two-space key.
NOTIFY_BLOCK="$(printf '%s\n' "$REGION" | awk -v job="  ${NOTIFY_JOB}:" '
  $0 == job { inblock = 1; next }
  inblock && /^  [^ \t#]/ { inblock = 0 }
  inblock { print }
')"

# ── 2. the gate is the shipped condition ────────────────────────────────────
IF_LINE="$(printf '%s\n' "$NOTIFY_BLOCK" | grep -E '^    if:' || true)"
GATE="$(printf '%s' "$IF_LINE" | sed -E 's/^[[:space:]]*if:[[:space:]]*//; s/[[:space:]]*$//')"
# `if: failure() && ...` and `if: ${{ failure() && ... }}` are the same condition
# to GitHub. Only that wrapper is normalised — the condition itself still has to
# match byte for byte, so dropping either the failure() or the ref test reddens.
if [[ "$GATE" == '${{'*'}}' ]]; then
  GATE="$(printf '%s' "$GATE" | sed -E 's/^\$\{\{[[:space:]]*//; s/[[:space:]]*\}\}$//')"
fi
if [[ "$GATE" == "$WANT_IF" ]]; then
  ok "gate is exactly: $WANT_IF"
else
  bad "gate is not exactly \`$WANT_IF\` — widened to always() it messages every pull request; without the ref test it messages every working branch. Found: ${IF_LINE:-(no if: line at all)}"
fi

# ── 3. every other job is in `needs:` ───────────────────────────────────────
# Both legal spellings are read: the flow list `needs: [a, b]` and the block
# list (`needs:` followed by `- a` lines). A trailing `# comment` is stripped
# BEFORE membership is tested — otherwise `needs: [a] # TODO re-add b, c` reads
# as covering b and c, and the guard reports a count it invented.
NEEDS_AWK="$(cat <<'AWK'
function strip(s) { sub(/[[:space:]]+#.*$/, "", s); sub(/^[[:space:]]*#.*$/, "", s); return s }
function emit(s) { gsub(/^["\047]|["\047]$/, "", s); if (s != "") print s }
/^    needs:/ {
  line = strip($0)
  sub(/^    needs:[[:space:]]*/, "", line)
  if (line ~ /^[[:space:]]*$/) { inlist = 1; next }
  gsub(/[][,]/, " ", line)
  n = split(line, a, /[[:space:]]+/)
  for (i = 1; i <= n; i++) emit(a[i])
  next
}
inlist {
  line = strip($0)
  if (line ~ /^[[:space:]]*$/) next
  if (line ~ /^[[:space:]]+-[[:space:]]*/) {
    sub(/^[[:space:]]+-[[:space:]]*/, "", line)
    emit(line)
  } else {
    inlist = 0
  }
}
AWK
)"
HAS_NEEDS="$(printf '%s\n' "$NOTIFY_BLOCK" | grep -cE '^    needs:' || true)"
if [[ "$HAS_NEEDS" == "0" ]]; then
  bad "$NOTIFY_JOB has no \`needs:\` line — it would run before the gates it is reporting on"
else
  NEEDS="$(printf '%s\n' "$NOTIFY_BLOCK" | awk "$NEEDS_AWK")"
  NEEDS_COUNT="$(printf '%s\n' "$NEEDS" | grep -c . || true)"
  MISSING=""
  COVERED=0
  while IFS= read -r job; do
    [[ -n "$job" ]] || continue
    [[ "$job" == "$NOTIFY_JOB" ]] && continue
    if printf '%s\n' "$NEEDS" | grep -qxF "$job"; then
      COVERED=$((COVERED+1))
    else
      MISSING="$MISSING $job"
    fi
  done <<< "$JOBS"

  if [[ "$NEEDS_COUNT" == "0" ]]; then
    bad "the \`needs:\` line is present but no entry could be read out of it — the guard cannot claim 'nothing missing' from a list it did not parse. Found: $(printf '%s\n' "$NOTIFY_BLOCK" | grep -E '^    needs:' | head -1)"
  elif [[ -n "$MISSING" ]]; then
    bad "these jobs are NOT in $NOTIFY_JOB's needs:$MISSING — their red would be reported to nobody"
  else
    ok "every other job ($COVERED) is in $NOTIFY_JOB's needs"
  fi
fi

# ── 4. the webhook is a secret, and there is no URL literal in the file ─────
if printf '%s\n' "$NOTIFY_BLOCK" | grep -qF "secrets.${SECRET_NAME}"; then
  ok "webhook is read from repository secret ${SECRET_NAME}"
else
  bad "$NOTIFY_JOB does not read secrets.${SECRET_NAME} — the destination has to come from a secret, because the URL is itself the credential"
fi

# Comments are excluded on both shapes: a whole-line comment, and a trailing
# comment after real YAML. The second one matters — every author of every other
# job in this file is otherwise one `# see https://…` away from being accused of
# leaking a credential by a guard that is not about their job at all.
url_hits() {
  grep -nE '://' \
    | grep -vE '^[0-9]+:[[:space:]]*#' \
    | sed -E 's/[[:space:]]+#.*$//' \
    | grep -E '://' || true
}
# Positive control covers BOTH layers: the scheme pattern must still match a
# real literal, AND the comment filters must still drop a documentation link.
CTRL="$(printf 'x: https://example.invalid/in?t=tok\n      - name: n  # see https://example.invalid/doc\n# https://example.invalid/whole-line\n' | url_hits)"
CTRL_N="$(printf '%s\n' "$CTRL" | grep -c . || true)"
if [[ "$CTRL_N" == "1" && "$CTRL" == *"in?t=tok"* ]]; then
  URL_HITS="$(url_hits < "$WF")"
  if [[ -z "$URL_HITS" ]]; then
    ok "no URL literal outside comments (a preserving invariant — it held before this change too)"
  else
    bad "URL literal(s) outside comments — a URL in this public file is a public credential: $URL_HITS"
  fi
else
  bad "the URL-literal scan failed its own positive control (expected exactly the one non-comment literal, got $CTRL_N) — a clean result here would be meaningless"
fi

echo "[main-red-notify-guard] $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
