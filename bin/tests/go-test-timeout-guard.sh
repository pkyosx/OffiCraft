#!/usr/bin/env bash
# The `go test` per-package timeout is a CHOSEN number, and its relationship to
# the CI job that runs it is a CONTRACT. This guard pins that contract.
#
# ── WHAT THE CONTRACT IS, AND WHY IT MATTERS (T-cf93) ────────────────────────
# Two deadlines can end a `make test-go` run, and they end it very differently:
#
#   * The GITHUB JOB deadline (`timeout-minutes:` on the go-checks job in
#     .github/workflows/ci.yml). GitHub kills the whole job. There is no
#     goroutine dump, no name of the test that was still running — the cell is
#     simply cancelled, and the log ends mid-sentence.
#   * GO'S OWN deadline (`go test -timeout …` in the Makefile's test-go recipe).
#     Go prints `panic: test timed out after 15m0s` and dumps EVERY RUNNING
#     GOROUTINE, naming the test that hung. That dump is the only artifact in
#     this system that says WHAT was slow.
#
# So the ordering is load-bearing: GO MUST HIT ITS LIMIT FIRST. Whenever go's
# per-package timeout is >= the job's ceiling, the diagnosable door is unreachable
# and every hang degrades into an un-diagnosable cancelled cell. Nothing else in
# the tree compares those two numbers: they live in two files, in two languages,
# owned by two different habits (one is "make the tests pass", the other is
# "make the cell fit"), and either can move without the other noticing.
#
# ── WHAT IT ASSERTS ──────────────────────────────────────────────────────────
#  T1  The Makefile's `go test` call site(s) actually pass `-timeout`. A missing
#      flag means the number is go's 10m default rather than a decision, and the
#      relation below stops being anybody's property.
#  T2  go's per-package timeout is STRICTLY LESS than the EFFECTIVE ceiling on
#      the go-checks job — `min(job timeout-minutes, the timeout-minutes on the
#      step that runs test-go)`. GitHub enforces both levels and a step-level one
#      kills the test-go step just as bluntly, so the smaller of the two is the
#      deadline that actually binds. Which level supplied it is reported.
#  T3  Both numbers are read from THEIR OWN file — the timeout out of the
#      Makefile, the ceiling out of ci.yml — and NEITHER is written down here.
#      A guard that hard-codes "15" and "25" goes green after someone edits
#      either file, which is the same as having no guard. See the parser
#      controls: they are what prove each extractor really reads its file.
#      Make-variable resolution is NOT reimplemented here. This guard used to
#      parse the assignment lines itself and follow make's last-assignment rule
#      by hand; that parser is gone. The timeout and the warning threshold are
#      both read out of `make -n` output, i.e. out of the already-expanded
#      recipe make itself produced. See ASK MAKE, DO NOT PARSE MAKE below.
#  T4  The extractors are exercised red AND green on synthetic fixtures BEFORE
#      any claim about the real tree, because "no violation" is also what an
#      extractor that finds nothing at all reports — the vacuous-empty-set
#      green this repo keeps relearning.
#  T5  Three mutants built from the REAL files are each caught: -timeout removed,
#      -timeout raised above the ceiling, ceiling lowered below the timeout.
#      They are SKIPPED, not run, when the real tree does not parse — a mutant is
#      a differential claim and has nothing to say about a tree that is already
#      failing.
#  T6  Sentinel: the real tree passes. A guard that is red on a healthy tree gets
#      deleted rather than fixed.
#  T7  GO_TEST_TOTAL_WARN — the Makefile's warning line for the SUM of the
#      modules' elapsed time — leaves room for a dump: WARN + the per-package
#      timeout stays strictly under the effective ceiling. Comparing WARN alone
#      accepted 24m under a 25m ceiling.
#  T7b The alarm FIRES, measured by RUNNING the recipe (`make -n` expanded, with a
#      stub `go` that sleeps a different amount per module) and watching for the
#      warning: it must trip on a line under the modules' summed seconds and stay
#      silent on one above. A pair, because the positive half alone passes on a
#      recipe that shouts unconditionally. This replaced a text check that greps
#      the recipe — four separate mutants kept its strings in place while the
#      alarm was dead. See item 1 below for why the sum of GREEN time, not the sum
#      of timeouts, is the one that can kill the cell.
#
# ── ONE DURATION SEMANTICS ───────────────────────────────────────────────────
# `<N>m` or `<N>s` with an integer N, and nothing else — deliberately NARROWER
# than go. It is the exact set the Makefile's test-go recipe accepts, because a
# guard whose green does not imply the recipe can even start is a guard that
# lies. Widening one side means widening both. See dur_to_seconds.
#
# ── WHAT THIS GUARD DOES *NOT* COVER (read before trusting it) ───────────────
#  1. THE SUM OVER MODULES — but read WHICH sum, because the obvious answer is
#     wrong. `make test-go` runs `go test ./...` once per module, sequentially,
#     each with the full per-package timeout, and it is tempting to write that
#     the budget therefore holds only "while at most one module ever runs to the
#     limit". IT IS NOT THAT. Timeouts CANNOT accumulate: the recipe is one shell
#     command under `set -e`, so the first module whose `go test` times out ends
#     the recipe there — measured with GO_TEST_TIMEOUT=50s, the failing round did
#     not even print its own `finished in Xs` line and the loop never reached the
#     next module. That door is shut.
#     THE OPEN DOOR IS THE SUM OF *GREEN* TIME. Four modules at 9 minutes each,
#     all passing, zero timeouts ⇒ 36 minutes ⇒ GitHub cancels go-checks at its
#     ceiling with no goroutine dump, while every per-module number stays far
#     under its 15m limit and this comparison stays green. That is what
#     GO_TEST_TOTAL_WARN and T7 exist for. What is still NOT asserted here is that
#     the warning line is well placed for today's suite — only that the recipe
#     really fires on the summed seconds (T7b runs it), and that WARN + the
#     per-package timeout leaves a hung run room to dump. The recipe warns;
#     nothing fails the build on it.
#  2. THE REST OF THE CELL'S BUDGET. go-checks also runs lint-go-naming,
#     lint-go-fmt, lint-go-vet, build-go and test-system-interaction-examples.
#     Their cost is in the Makefile's prose budget, not in any assertion here.
#     Strictly-less is necessary, never sufficient.
#  3. OTHER JOBS. Only the job that RUNS test-go is examined. Another cell that
#     grew its own `go test` call site would be found by the call-site scan (T1
#     covers the Makefile, which is the one place a check's HOW is written), but
#     its ceiling is not compared to anything.
#  4. RUNTIME OVERRIDES. `GOFLAGS=-timeout=1h`, a `-timeout` appended by a
#     wrapper, `make GO_TEST_TIMEOUT=1h`, or a job re-declaring its ceiling in a
#     reusable workflow all defeat the intent while keeping the text. Nothing
#     here looks at the environment, at make's command line, or at any workflow
#     other than ci.yml.
#     NOT in this list, though an earlier version of this comment said so:
#     `override` and `ifeq` blocks. Those ARE caught, because the value comes
#     from `make -n` rather than from a parser of our own — make applies them
#     before we ever see the recipe. Measured, not reasoned: appending
#     `override GO_TEST_TIMEOUT := 40m` to the Makefile makes `make -pn` report
#     40m and this guard exit 1 with `VIOLATION go=2400s ceiling=1500s`.
#     The stale wording cost real work: it sends a reader off to hand-test a
#     case that is already covered, which is the same waste the deleted T7a
#     text-check note used to cause.
#  5. WHETHER 15m IS *ENOUGH*. It asserts a relation between two numbers, not
#     that either is well chosen. A suite that legitimately needs 40m fails
#     under this contract and should — but this file cannot tell you that.
#  6. NON-GO SUITES. vitest / playwright / the e2e station have their own
#     ceilings and their own failure modes. Out of scope.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
MAKEFILE="$ROOT/Makefile"
CI_YML="$ROOT/.github/workflows/ci.yml"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1" >&2; }
SKIP=0
skip(){ SKIP=$((SKIP+1)); printf '  skip — %s\n' "$1"; }

WORK="$(mktemp -d -t oc-gotest-timeout.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

echo "go test timeout ↔ CI job ceiling contract tests"

# ── side A: the Makefile ─────────────────────────────────────────────────────
# COMMAND-POSITION parsing, not `grep -F -- -timeout`. This file, the Makefile's
# own comment block and the recipe's `echo` line all spell `-timeout`, and the
# recipe's echo spells `go test` too. A substring grep matches every one of them
# and is a check with no discriminating power. So: split each line into shell
# segments, strip make's recipe prefixes, skip env-assignment prefixes, and only
# then ask whether the command word is go and its first non-flag argument is
# `test`. Same shape as bin/tests/go-test-nocache-guard.sh, which pins -count=1
# on the same call site.
#
# Emits one TSV record per call site: <label>\t<lineno>\t<timeout-token|->\t<seg>
makefile_go_test_sites() { # FILE [LABEL]
  local file="$1" label="${2:-$1}"
  awk -v LABEL="$label" '
    /^[[:space:]]*#/ { next }
    {
      n = split($0, segs, /(&&|\|\||;|\|)/)
      for (i = 1; i <= n; i++) {
        seg = segs[i]
        sub(/^[[:space:]]+/, "", seg)
        sub(/^[-@+]+/, "", seg)
        gsub(/^[[:space:]]*[({][[:space:]]*/, "", seg)
        sub(/^[[:space:]]+/, "", seg)
        if (seg == "") continue
        m = split(seg, tok, /[[:space:]]+/)
        j = 1
        while (j <= m) {
          t = tok[j]
          if (t == "env") { j++; continue }
          if (t ~ /^[A-Za-z_][A-Za-z0-9_]*=/) { j++; continue }
          break
        }
        if (j > m) continue
        cmd = tok[j]
        gsub(/["'"'"']/, "", cmd)
        sub(/^\$\$/, "$", cmd)          # make doubles every shell $
        isgo = (cmd == "go" || cmd == "$GO" || cmd == "${GO}" || cmd ~ /\/go$/)
        if (!isgo) continue
        sub_cmd = ""
        for (k = j + 1; k <= m; k++) {
          a = tok[k]; gsub(/["'"'"']/, "", a)
          if (a == "") continue
          if (a ~ /^-/) continue
          sub_cmd = a; break
        }
        if (sub_cmd != "test") continue
        # the -timeout argument, in either spelling go accepts.
        # 🔴 THE LAST ONE WINS. The go flag package takes the last occurrence, so a
        # command line carrying `-timeout $(GO_TEST_TIMEOUT) -timeout 40m` runs
        # under 40m — measured: `go test -timeout 5s -timeout 40m ./...` ran for
        # 34s instead of failing at 5s. Scanning for the first one reported the
        # variable and called the contract satisfied on a number go never used,
        # which is the same defect as reading the first make assignment.
        val = "-"
        for (k = j + 1; k <= m; k++) {
          a = tok[k]; gsub(/["'"'"']/, "", a)
          if (a == "-timeout" || a == "--timeout") {
            if (k + 1 <= m) { val = tok[k+1]; gsub(/["'"'"']/, "", val) }
            continue
          }
          if (a ~ /^--?timeout=/) { b = a; sub(/^--?timeout=/, "", b); val = b }
        }
        printf "%s\t%d\t%s\t%s\n", LABEL, NR, val, seg
      }
    }
  ' "$file"
}

# THE ONE DURATION SEMANTICS (T-cf93 follow-up). Deliberately NARROWER than go:
# only `<N>m` or `<N>s` with an integer N. That is exactly the form the
# Makefile's test-go recipe accepts, and the two must not disagree — before this
# was converged, `1.5m` and `10m30s` were green here (90s / 630s) while the
# recipe died on them with `syntax error` and `value too great for base`: the
# guard's green did not imply `make test-go` could even start, and the error it
# eventually produced named nothing anybody could act on.
#
# Direction of the convergence: the guard was widened-then-narrowed to the
# recipe's form rather than the recipe widened to go's, because the recipe has to
# do ARITHMETIC on the number (the elapsed-time warnings) and integer minutes /
# seconds is what shell arithmetic can carry. Go accepts `1h30m10s`; if this repo
# ever needs one, BOTH sides change together — that is the point of there being
# one semantics.
#
# Anything else is refused loudly. A silent 0 here would make every comparison
# below trivially true.
dur_to_seconds() { # DURATION -> seconds on stdout, rc 1 if unparseable
  awk -v D="$1" '
    BEGIN {
      # 🔴 NO LEADING ZEROS. awk reads "08" as 8; bash $(( )) reads it as OCTAL
      # and dies with `value too great for base` — the very error string this
      # convergence was written to eliminate, and it comes out of the arithmetic
      # before the friendly refusal in the recipe can print. `015m` was worse: the
      # guard said 900s, the recipe computed 780s, and go used 900s. One string,
      # three consumers, two answers.
      if (D ~ /^(0|[1-9][0-9]*)m$/) { sub(/m$/, "", D); printf "%d\n", D * 60; exit 0 }
      if (D ~ /^(0|[1-9][0-9]*)s$/) { sub(/s$/, "", D); printf "%d\n", D + 0;  exit 0 }
      exit 1
    }'
}

# The whole side-A extractor: Makefile in, seconds out (or MISSING / MULTIPLE /
# UNPARSEABLE:<tok>). One string, so a fixture and the real file go through the
# exact same code path.
# ═════════════════════════════════════════════════════════════════════════════
# 🔴 ASK MAKE, DO NOT PARSE MAKE. Every version of this extractor that read the
# Makefile as text was defeated by a detail of make's own grammar that the reader
# did not model, and each fix moved the boundary one character rather than
# removing it:
#   * anchored at column 0        ⇒ `␣␣GO_TEST_TIMEOUT := 40m` invisible
#   * then "only a TAB is a recipe body" ⇒ a TAB-indented assignment AFTER another
#     assignment is still an assignment, and that was invisible too
#   * first `-timeout` on the line ⇒ go takes the LAST one
# The decisive measurement: the SAME mutant text appended at the end of the file
# resolved to 15m, and inserted after an assignment resolved to 40m. The rule was
# never about tabs — it is about make's parser state, which nothing short of make
# knows. So the value now comes from `make -n`, which prints the recipe with every
# variable ALREADY EXPANDED: that string is what the shell will run, so it is what
# go will receive, by construction.
#
# What this buys beyond correctness: overrides, `override`, `ifeq` branches,
# `define` blocks, `.RECIPEPREFIX` and included makefiles are all handled by make
# rather than modelled here. What it does NOT cover is the command line
# (`make GO_TEST_TIMEOUT=1h`) and the environment, because this invokes make
# without them — see header item 4.
make_expanded_recipe() { # FILE TARGET -> the expanded shell, or empty on failure
  local file="$1" target="$2" dir base
  dir="$(cd "$(dirname "$file")" 2>/dev/null && pwd)" || return 1
  base="$(basename "$file")"
  (cd "$dir" && make -f "$base" -n "$target" 2>/dev/null)
}

# The go test invocations in that expanded text, EXCLUDING the ones that only
# PRINT such a string (echo, printf). A recipe that mentions -timeout in a log
# line while running without it would otherwise read as compliant — and the log
# would be the thing lying. make does not emit comment lines, so those need no
# exclusion here; the A4 decoy fixture holds all three shapes.
#
# 🔴 `&&` SEPARATES COMMANDS TOO, NOT JUST `;`. Splitting on `;` alone left
# `echo "…" && (cd … "$GO" test -timeout 40m ./...)` as ONE line beginning with
# echo, so the print-exclusion swallowed the whole thing: the real invocation
# vanished and the guard reported a clean tree it had never looked at.
# The split is bash's own `${text//&&/$nl}` rather than a `sed s/&&/\n/g` so that
# no sed dialect is in the answer at all. `\n` on the replacement side is not in
# POSIX; whether a given sed emits a newline or a literal `n` is that build's
# business, and a sed that emitted `n` would fail SILENTLY here — the split just
# would not happen, which reads as rc=0 on a clean tree, indistinguishable from
# the false green this line exists to close. Bash's expansion cannot vary.
go_test_commands() { # EXPANDED -> one command fragment per line
  # Quotes are stripped first: the real call site is `("$GO" test …)`, and a
  # pattern that did not account for the closing quote between $GO and `test`
  # matched nothing and reported NO-CALL-SITE on a healthy tree.
  local text="$1" nl=$'\n'
  text="${text//&&/$nl}"
  printf '%s\n' "$text" | tr ';' '\n' | sed 's/"//g' |
    grep -E '(^|[^[:alnum:]_])(\$\{?GO\}?|go)[[:space:]]+test([[:space:]]|$)' |
    grep -vE '^[[:space:]]*(echo|printf)([[:space:]]|$)'
}

makefile_timeout_seconds() { # FILE
  local file="$1" expanded cmds vals secs
  expanded="$(make_expanded_recipe "$file" test-go)" || { echo "UNRESOLVED:make-failed"; return; }
  [[ -n "$(printf '%s' "$expanded" | tr -d '[:space:]')" ]] || { echo "NO-CALL-SITE"; return; }
  cmds="$(go_test_commands "$expanded")"
  [[ -n "$(printf '%s' "$cmds" | tr -d '[:space:]')" ]] || { echo "NO-CALL-SITE"; return; }
  # 🔴 PER SITE, NOT A UNION. Taking every -timeout in the recipe and sort -u'ing
  # them makes "a call site that contributes no value" and "no such call site"
  # indistinguishable: review added a second `go test -tags slow ./...` with NO
  # -timeout and this reported 900s and called the contract satisfied, while that
  # second invocation ran under go's 10m default. T1 says call SITE(S), plural.
  # Each site is asked separately and a site with no -timeout is MISSING, whatever
  # its neighbours carry.
  #
  # Within one site the LAST -timeout wins, because that is the one the go flag
  # package keeps: `go test -timeout 5s -timeout 40m` runs for 40m.
  local per_site
  per_site="$(printf '%s\n' "$cmds" | while IFS= read -r cmd; do
    [[ -n "$(printf '%s' "$cmd" | tr -d '[:space:]')" ]] || continue
    v="$(printf '%s\n' "$cmd" | sed -nE 's/.*[[:space:]]--?timeout[= ]+([^[:space:]]+).*/\1/p' | tail -1)"
    printf '%s\n' "${v:--}"
  done)"
  if printf '%s\n' "$per_site" | grep -qx -- '-'; then echo "MISSING"; return; fi
  vals="$(printf '%s\n' "$per_site" | sed -E 's/^"//; s/"$//' | sort -u)"
  if [[ -z "$(printf '%s' "$vals" | tr -d '[:space:]')" ]]; then echo "MISSING"; return; fi
  if [[ "$(printf '%s\n' "$vals" | grep -c .)" != "1" ]]; then
    echo "MULTIPLE:$(printf '%s\n' "$vals" | tr '\n' ',')"; return
  fi
  secs="$(dur_to_seconds "$vals")" || { echo "UNPARSEABLE:$vals"; return; }
  [[ -n "$secs" ]] || { echo "UNPARSEABLE:$vals"; return; }
  # 🔴 `-timeout 0` DISABLES go's timeout entirely. It parses, it compares as
  # smaller than every ceiling, and it is precisely the disaster this file exists
  # to prevent: a hang with no goroutine dump, killed by GitHub with no name for
  # what was slow. Rejected here rather than left to the comparison, which would
  # have called it the safest value possible.
  if [[ "$secs" == "0" ]]; then echo "DISABLED:$vals"; return; fi
  echo "$secs"
}

# ── side B: .github/workflows/ci.yml ─────────────────────────────────────────
# ruby+psych, for the same reason bin/tests/auto-beta-guard.sh and
# ci-run-checks-entrypoint-guard.sh use it: the hosted macOS runner has no
# PyYAML, and a line-oriented grep cannot see a `run:` written as a block scalar
# or a `timeout-minutes` that moved by two spaces. And — the point of this
# guard — the JOB is found by asking which job's run steps actually invoke
# test-go in COMMAND POSITION, never by hard-coding the name `go-checks`.
RUBY="$(command -v ruby 2>/dev/null || true)"
if [[ -z "$RUBY" ]]; then
  for cand in /usr/bin/ruby /opt/homebrew/bin/ruby /usr/local/bin/ruby; do
    [[ -x "$cand" ]] && { RUBY="$cand"; break; }
  done
fi
if [[ -z "$RUBY" ]]; then
  echo "FATAL: no ruby found, so .github/workflows/ci.yml cannot be PARSED." >&2
  echo "  macOS ships one at /usr/bin/ruby; Homebrew: brew install ruby." >&2
  echo "  A guard that cannot parse must be loud, not silently green." >&2
  exit 2
fi

SCANNER="$WORK/ci-scan.rb"
cat >"$SCANNER" <<'RUBY_EOF'
require 'yaml'

TARGET = ARGV[1] || 'test-go'
# The lane TARGET belongs to, when the caller knows it (T-127). Since the cloud
# cells stopped naming targets, the step that runs test-go says only
# `--lane "${{ github.job }}"` — so "which job runs it" is answered by the round
# list plus the job's own id, not by finding the word in the run script. The
# fixtures below still spell targets out, and that path is untouched: this is an
# ADDITIONAL way for a step to count, never a replacement.
LANE = ARGV[2].to_s.empty? ? nil : ARGV[2]
text = File.read(ARGV[0], mode: 'r:UTF-8')
doc = begin
  YAML.safe_load(text, aliases: false)
rescue => e
  warn "PARSE-ERROR #{e.class}: #{e.message}"
  exit 3
end
jobs = (doc && doc['jobs']) || {}

SEPARATORS = /(?:\|\||&&|[;|&])/
LEADERS = %w[if then else elif fi do done while until case esac sudo time exec env nohup].freeze
# Echoing the target's NAME is not running it. Command-position tokenisation is
# what separates `bash bin/run-checks.sh … test-go` from a step called
# "explain test-go" or a comment about it.
NON_RUNNERS = %w[echo printf : true false].freeze

# A gate cell reaching its checks through the lane door: `bin/run-checks.sh
# --lane <something>`. It says nothing about WHICH lane — the caller pairs it
# with the job id, because that is what the workflow interpolates.
def lane_door?(script)
  script.to_s.each_line do |line|
    line = line.sub(/#.*\z/, '')
    line.split(SEPARATORS).each do |seg|
      toks = seg.strip.sub(/\A[({]\s*/, '').split(/\s+/)
      next if toks.empty?
      return true if toks.any? { |t| t.end_with?('bin/run-checks.sh') } && toks.include?('--lane')
    end
  end
  false
end

def runs_target?(script, target)
  script.to_s.each_line do |line|
    line = line.sub(/#.*\z/, '')
    line.split(SEPARATORS).each do |seg|
      seg = seg.strip.sub(/\A[({]\s*/, '')
      toks = seg.split(/\s+/)
      toks.shift while toks.first && (LEADERS.include?(toks.first) || toks.first =~ /\A[A-Za-z_][A-Za-z0-9_]*=/)
      next if toks.empty?
      cmd = File.basename(toks.first.to_s)
      next if NON_RUNNERS.include?(cmd)
      return true if toks[1..].to_a.include?(target)
    end
  end
  false
end

# ── THE EFFECTIVE CEILING IS min(job, step) ─────────────────────────────────
# GitHub enforces `timeout-minutes` at BOTH levels, and a step-level one is a
# real second deadline: it kills the step that is running `test-go` with exactly
# the failure this guard exists to prevent — the cell stops mid-sentence, no
# goroutine dump, no name for whatever was still running. Reading only
# `job['timeout-minutes']` meant `timeout-minutes: 5` on the test-go step was
# invisible: the guard still reported the job's 25 and declared the contract
# satisfied. So the binding number is the SMALLER of the two, and WHICH LEVEL it
# came from is reported, because that is where an editor has to go to change it.
hits = []
jobs.each do |name, job|
  next unless job.is_a?(Hash)
  steps = job['steps']
  next unless steps.is_a?(Array)
  job_tm = job['timeout-minutes']
  job_tm = nil unless job_tm.is_a?(Numeric)
  running = []
  steps.each_with_index do |s, i|
    next unless s.is_a?(Hash)
    direct = runs_target?(s['run'], TARGET)
    via_lane = LANE && name == LANE && lane_door?(s['run'])
    next unless direct || via_lane
    st = s['timeout-minutes']
    st = nil unless st.is_a?(Numeric)
    running << [i, s['name'], st]
  end
  next if running.empty?
  hits << [name, job_tm, running]
end

if hits.empty?
  puts "NO-JOB"
elsif hits.size > 1
  puts "MULTIPLE-JOBS:" + hits.map(&:first).join(',')
else
  name, job_tm, running = hits.first
  # One row per step that runs the target: the deadline that fires first ON THAT
  # STEP. Several such steps in one job would each have their own; the guard is
  # held to the tightest, because that is the one that can cut a run short.
  per = running.map do |i, sname, st|
    cands = [job_tm, st].compact
    [i, sname, st, cands.min]
  end
  if per.any? { |row| row[3].nil? }
    puts "NO-CEILING:#{name}"
  else
    idx, sname, st, eff = per.min_by { |row| row[3] }
    origin =
      if st && (job_tm.nil? || st < job_tm)
        label = sname ? " #{sname.inspect}" : ""
        "step##{idx}#{label}"
      else
        "job"
      end
    puts "#{name}\t#{eff}\t#{origin}"
  end
end
RUBY_EOF

# The lane a target is assigned to, straight out of THE round list — the same
# file bin/ci.sh and every cloud cell read. Empty when the target has no line,
# which the scanner then treats as "no lane known" rather than guessing.
round_lane_of() { # TARGET -> lane | ""
  local f="$ROOT/bin/lib/ci-round.txt"
  [[ -f "$f" ]] || return 0
  awk -v t="$1" '{sub(/#.*/,"")} NF==2 && $2==t {print $1; exit}' "$f"
}

ci_job_ceiling() { # FILE [TARGET] -> "<job>\t<minutes>\t<job|step#N>" or a NO-*/MULTIPLE-* word
  local target="${2:-test-go}"
  "$RUBY" "$SCANNER" "$1" "$target" "$(round_lane_of "$target")" 2>&1
}

# ── the verdict, one function, used by fixtures / mutants / the real tree ────
# Both numbers come from their OWN file; nothing expected is written down here.
verdict() { # MAKEFILE CI_YML -> "OK <go_s> <job_s> <job> <origin>" | "VIOLATION …" | "BROKEN …"
  local mk="$1" ci="$2" go_s job_line job_name job_min job_src job_s
  go_s="$(makefile_timeout_seconds "$mk")"
  case "$go_s" in
    ''|*[!0-9]*) echo "BROKEN makefile-side=$go_s"; return ;;
  esac
  job_line="$(ci_job_ceiling "$ci")"
  case "$job_line" in
    *$'\t'*$'\t'*) : ;;
    *) echo "BROKEN ci-side=$job_line"; return ;;
  esac
  job_name="$(printf '%s' "$job_line" | cut -f1)"
  job_min="$(printf '%s' "$job_line" | cut -f2)"
  job_src="$(printf '%s' "$job_line" | cut -f3-)"
  case "$job_min" in ''|*[!0-9]*) echo "BROKEN ci-side-minutes=$job_min"; return ;; esac
  job_s=$(( job_min * 60 ))
  if (( go_s < job_s )); then
    echo "OK $go_s $job_s $job_name $job_src"
  else
    echo "VIOLATION go=${go_s}s ceiling=${job_s}s(job $job_name, set at $job_src) — go must hit its limit FIRST"
  fi
}

# ── the SECOND relation: the SUM of the modules' GREEN time (T-cf93 follow-up)
# `make test-go` runs `go test ./...` once per module, sequentially, and the
# recipe accumulates their elapsed seconds. GO_TEST_TOTAL_WARN is the line where
# that running total starts shouting — see the Makefile's comment for why the
# number is what it is. It is read out of the Makefile, compared against the
# ceiling read out of ci.yml, and it has to be strictly under it for the same
# reason go's per-package timeout does: a warning that only trips after the cell
# is already dead is not a warning.
# 🔴 THERE IS NO TEXT CHECK FOR THE ALARM ANY MORE, ON PURPOSE. There was one:
# it grepped the recipe for an accumulate line and a compare line. Review defeated
# it four times while both strings stayed in place (a `total=0` reset inside the
# loop, `elapsed=0`, `if false && …`, and warn_s fed a literal while the constant
# stayed put), and then it went the other way and FAILED a recipe that was correct
# — `total=$$(( $$total + $$elapsed ))`, the same arithmetic with sigils — while
# printing "the recipe never adds each module's elapsed time", which was simply
# false about that tree.
#
# Both directions are the same defect: it measured the spelling and reported a
# conclusion about the behaviour. T7b runs the recipe instead, so the text check
# had nothing left to add and one thing left to get wrong. Deleted rather than
# disclaimed — a check that can hand the next person a confident wrong diagnosis
# is worse than no check, because they will believe it.
# 🔴 ASK MAKE FOR THIS ONE TOO. The first version of this read the value through
# a text parser over the Makefile's assignment lines, the same shape this file
# gave up on above. Only the -timeout half was moved, so review walked straight in
# through the half that was left: a TAB-indented second `GO_TEST_TOTAL_WARN := 24m`
# after an assignment, and a plain `ifeq (1,0)` dead branch with no tabs at all,
# both had make using 24m while this reported 300s and declared the relation
# satisfied. 1440 + 900 = 2340 > 1500 — the only thing T7 exists to prevent,
# passed green with a confident wrong number.
#
# The value comes from the EXPANDED recipe, which is also where the recipe itself
# gets it: `dur_s "<value>" GO_TEST_TOTAL_WARN`. That closes the other half of the
# same hole review found on the timeout side — the constant a check compares and
# the value the recipe uses coming apart. Here they cannot: it is one string.
makefile_named_duration_seconds() { # FILE NAME -> seconds | UNSET | UNPARSEABLE:<lit>
  local file="$1" name="$2" expanded lit secs
  expanded="$(make_expanded_recipe "$file" test-go)" || { echo "UNSET"; return; }
  lit="$(printf '%s\n' "$expanded" |
    sed -nE "s/.*dur_s[[:space:]]+\"([^\"]*)\"[[:space:]]+$name([^A-Za-z0-9_]|\$).*/\1/p" |
    tail -1)"
  [[ -n "$lit" ]] || { echo "UNSET"; return; }
  secs="$(dur_to_seconds "$lit")" || { echo "UNPARSEABLE:$lit"; return; }
  [[ -n "$secs" ]] || { echo "UNPARSEABLE:$lit"; return; }
  echo "$secs"
}

# ═════════════════════════════════════════════════════════════════════════════
# PARSER CONTROLS — red AND green, BEFORE any claim about the real tree.
# Fixtures are written through printf so their literals never sit in command
# position in THIS file, and each shell-ish fixture is single-segment because
# the parser splits on && (the same trap go-test-nocache-guard.sh documents).
# ═════════════════════════════════════════════════════════════════════════════
AND='&&'

# --- A1: a literal -timeout is FOUND and converted --------------------------
printf '%s\n' 'test-go:' "	@(cd x $AND go test -count=1 -timeout 7m ./...)" > "$WORK/lit.mk"
GOT="$(makefile_timeout_seconds "$WORK/lit.mk")"
if [[ "$GOT" == "420" ]]; then
  ok "Makefile side: a literal '-timeout 7m' in a make recipe reads as 420s"
else
  bad "Makefile side: a literal '-timeout 7m' should read as 420s (got: $GOT)"
fi

# --- A2: a make VARIABLE is resolved, not echoed back ------------------------
printf '%s\n' 'MYTO := 12m' 'test-go:' "	@(cd x $AND \"\$\$GO\" test -count=1 -timeout \$(MYTO) ./...)" > "$WORK/var.mk"
GOT="$(makefile_timeout_seconds "$WORK/var.mk")"
if [[ "$GOT" == "720" ]]; then
  ok "Makefile side: '-timeout \$(MYTO)' with 'MYTO := 12m' resolves to 720s"
else
  bad "Makefile side: '-timeout \$(MYTO)' with 'MYTO := 12m' should resolve to 720s (got: $GOT)"
fi

# --- A2b: the LAST assignment wins, exactly as make expands it --------------
# A recipe expands its variables when it RUNS, so a later `MYTO := 40m` is the
# value go test receives. Reading the FIRST match is not a near miss — it makes
# the guard report a number the build never uses and call the contract satisfied.
printf '%s\n' 'MYTO := 12m' 'MYTO := 40m' 'test-go:' "	@(cd x $AND go test -timeout \$(MYTO) ./...)" > "$WORK/var-last.mk"
GOT="$(makefile_timeout_seconds "$WORK/var-last.mk")"
if [[ "$GOT" == "2400" ]]; then
  ok "Makefile side: a SECOND 'MYTO := 40m' overrides the first, as make does — 2400s, not 720s"
else
  bad "Makefile side: the LAST assignment must win (expected 2400, got: $GOT)"
fi

# --- A2c: `?=` does NOT override, because make says it does not --------------
printf '%s\n' 'MYTO := 12m' 'MYTO ?= 40m' 'test-go:' "	@(cd x $AND go test -timeout \$(MYTO) ./...)" > "$WORK/var-qeq.mk"
GOT="$(makefile_timeout_seconds "$WORK/var-qeq.mk")"
if [[ "$GOT" == "720" ]]; then
  ok "Makefile side: a later '?=' leaves an already-set variable alone — still 720s"
else
  bad "Makefile side: '?=' must not override an assigned variable (expected 720, got: $GOT)"
fi

# --- A3: the RED direction — a call site with no -timeout is reported --------
printf '%s\n' 'test-go:' "	@(cd x $AND go test -count=1 ./...)" > "$WORK/bare.mk"
GOT="$(makefile_timeout_seconds "$WORK/bare.mk")"
if [[ "$GOT" == "MISSING" ]]; then
  ok "Makefile side: a 'go test' call site with NO -timeout is reported MISSING"
else
  bad "Makefile side: a 'go test' call site with NO -timeout must be MISSING (got: $GOT)"
fi

# --- A4: decoys — the exact strings a substring grep would swallow -----------
printf '%s\n' \
  '# a comment about go test -timeout 99m that must not count' \
  'test-go:' \
  '	@echo "[test-go] go test -count=1 -timeout 99m $$dir"' \
  '	@printf "%s\n" "go test -timeout 99m"' \
  "	@(cd x $AND go build -o y ./...)" > "$WORK/decoy.mk"
GOT="$(makefile_timeout_seconds "$WORK/decoy.mk")"
if [[ "$GOT" == "NO-CALL-SITE" ]]; then
  ok "Makefile side: comments, echo/printf strings and 'go build' are NOT call sites"
else
  bad "Makefile side: comments/echo/printf/'go build' must not be call sites (got: $GOT)"
fi

# --- A5: garbage is refused, not silently zeroed ----------------------------
printf '%s\n' 'test-go:' "	@(cd x $AND go test -timeout soon ./...)" > "$WORK/junk.mk"
GOT="$(makefile_timeout_seconds "$WORK/junk.mk")"
if [[ "$GOT" == UNPARSEABLE:* ]]; then
  ok "Makefile side: an unparseable duration is refused ($GOT), never read as 0"
else
  bad "Makefile side: an unparseable duration must be refused (got: $GOT)"
fi

# --- A5b: ONE duration semantics — the recipe's narrow form, nothing wider ---
# `1.5m` and `10m30s` are legal go durations and the guard used to accept them
# (90s / 630s, sentinel green) while `make test-go` died on them with
# `1.5 * 60 : syntax error` and `10m30: value too great for base`. A guard whose
# green does not imply the recipe can start is worse than no guard, and those
# errors name nothing. Both sides now accept only <N>m / <N>s.
for BAD_DUR in 1h 1.5m 10m30s 90ms; do
  printf '%s\n' 'test-go:' "	@(cd x $AND go test -timeout $BAD_DUR ./...)" > "$WORK/wide.mk"
  GOT="$(makefile_timeout_seconds "$WORK/wide.mk")"
  if [[ "$GOT" == "UNPARSEABLE:$BAD_DUR" ]]; then
    ok "Makefile side: '$BAD_DUR' is a legal GO duration but NOT a legal GO_TEST_TIMEOUT — refused ($GOT), same as the recipe refuses it"
  else
    bad "Makefile side: '$BAD_DUR' must be UNPARSEABLE — the recipe cannot run it (got: $GOT)"
  fi
done

# --- B1: the ci.yml side finds the RIGHT job, among decoys ------------------
# Two jobs with DIFFERENT ceilings; only one of them runs test-go, and a third
# only mentions it in an echo. A parser that grabbed the first timeout-minutes
# it saw, or that grepped for the word, returns 5 or 99 instead of 25.
cat >"$WORK/two-jobs.yml" <<'YML_EOF'
name: fixture
on: [push]
jobs:
  decoy-first:
    runs-on: macos-15
    timeout-minutes: 5
    steps:
      - run: bash bin/run-checks.sh lint-ts test-frontend-unit
  talks-about-it:
    runs-on: macos-15
    timeout-minutes: 99
    steps:
      - run: echo "next we would run test-go"
  the-real-one:
    runs-on: macos-15
    timeout-minutes: 25
    steps:
      - run: bash bin/run-checks.sh lint-go-vet build-go test-go
YML_EOF
GOT="$(ci_job_ceiling "$WORK/two-jobs.yml")"
if [[ "$GOT" == $'the-real-one\t25\tjob' ]]; then
  ok "ci.yml side: picks the job that RUNS test-go (the-real-one, 25, set at job level) past two decoy jobs"
else
  bad "ci.yml side: should pick 'the-real-one' with 25 from the job level (got: ${GOT//$'\t'/ })"
fi

# --- B1b: a STEP-level timeout-minutes on the step that runs it BINDS --------
# GitHub enforces this one too, and it kills the test-go step with no goroutine
# dump — the exact failure this guard exists for. A scanner that only reads
# job['timeout-minutes'] reports 25 here and calls the contract satisfied.
cat >"$WORK/step-cap.yml" <<'YML_EOF'
name: fixture
on: [push]
jobs:
  the-real-one:
    runs-on: macos-15
    timeout-minutes: 25
    steps:
      - run: bash bin/run-checks.sh lint-go-vet
      - name: go checks
        timeout-minutes: 5
        run: bash bin/run-checks.sh build-go test-go
YML_EOF
GOT="$(ci_job_ceiling "$WORK/step-cap.yml")"
if [[ "$GOT" == $'the-real-one\t5\tstep#1 "go checks"' ]]; then
  ok "ci.yml side: a step-level 'timeout-minutes: 5' on the test-go step BINDS over the job's 25, and the step is named"
else
  bad "ci.yml side: step-level timeout-minutes must bind and be attributed (got: ${GOT//$'\t'/ })"
fi

# --- B1c: the job level still wins when it is the SMALLER of the two ---------
cat >"$WORK/step-loose.yml" <<'YML_EOF'
name: fixture
on: [push]
jobs:
  the-real-one:
    runs-on: macos-15
    timeout-minutes: 7
    steps:
      - name: go checks
        timeout-minutes: 90
        run: bash bin/run-checks.sh test-go
YML_EOF
GOT="$(ci_job_ceiling "$WORK/step-loose.yml")"
if [[ "$GOT" == $'the-real-one\t7\tjob' ]]; then
  ok "ci.yml side: with a LOOSER step cap (90) the job's 7 is the effective ceiling — min(job, step), not last-seen"
else
  bad "ci.yml side: min(job, step) must pick the job's 7 (got: ${GOT//$'\t'/ })"
fi

# --- B1d: no job ceiling at all, but the step carries one -------------------
cat >"$WORK/step-only.yml" <<'YML_EOF'
name: fixture
on: [push]
jobs:
  uncapped-job:
    runs-on: macos-15
    steps:
      - name: go checks
        timeout-minutes: 12
        run: bash bin/run-checks.sh test-go
YML_EOF
GOT="$(ci_job_ceiling "$WORK/step-only.yml")"
if [[ "$GOT" == $'uncapped-job\t12\tstep#0 "go checks"' ]]; then
  ok "ci.yml side: a job with NO ceiling but a capped test-go step reports the STEP's 12, not NO-CEILING"
else
  bad "ci.yml side: a step-only ceiling must be reported (got: ${GOT//$'\t'/ })"
fi

# --- B2: the RED direction — no job runs it ---------------------------------
cat >"$WORK/no-job.yml" <<'YML_EOF'
name: fixture
on: [push]
jobs:
  only-decoy:
    runs-on: macos-15
    timeout-minutes: 5
    steps:
      - run: echo "test-go is not run here"
YML_EOF
GOT="$(ci_job_ceiling "$WORK/no-job.yml")"
if [[ "$GOT" == "NO-JOB" ]]; then
  ok "ci.yml side: a workflow where nothing RUNS test-go reports NO-JOB, not a number"
else
  bad "ci.yml side: a workflow where nothing runs test-go must report NO-JOB (got: $GOT)"
fi

# --- B3: a job that runs it with no ceiling at all --------------------------
cat >"$WORK/no-ceiling.yml" <<'YML_EOF'
name: fixture
on: [push]
jobs:
  uncapped:
    runs-on: macos-15
    steps:
      - run: bash bin/run-checks.sh test-go
YML_EOF
GOT="$(ci_job_ceiling "$WORK/no-ceiling.yml")"
if [[ "$GOT" == "NO-CEILING:uncapped" ]]; then
  ok "ci.yml side: a test-go job with NO timeout-minutes is reported, not treated as infinite-and-fine"
else
  bad "ci.yml side: a test-go job with no timeout-minutes must be reported (got: $GOT)"
fi

# --- C: the verdict function itself, both ways, on synthetic pairs ----------
# Without this the mutants below could all be "caught" by a verdict() that
# always says VIOLATION, and the sentinel could be met by one that never does.
printf '%s\n' 'test-go:' "	@(cd x $AND go test -timeout 1m ./...)" > "$WORK/v-green.mk"
printf '%s\n' 'test-go:' "	@(cd x $AND go test -timeout 30m ./...)" > "$WORK/v-red.mk"
cat >"$WORK/v.yml" <<'YML_EOF'
name: fixture
on: [push]
jobs:
  cell:
    runs-on: macos-15
    timeout-minutes: 25
    steps:
      - run: bash bin/run-checks.sh test-go
YML_EOF
GOT="$(verdict "$WORK/v-green.mk" "$WORK/v.yml")"
if [[ "$GOT" == "OK 60 1500 cell job" ]]; then
  ok "verdict: 1m timeout under a 25m ceiling is OK (and both numbers came from their own file)"
else
  bad "verdict: 1m under 25m should be 'OK 60 1500 cell job' (got: $GOT)"
fi
GOT="$(verdict "$WORK/v-red.mk" "$WORK/v.yml")"
if [[ "$GOT" == VIOLATION* ]]; then
  ok "verdict: 30m timeout under a 25m ceiling is a VIOLATION"
else
  bad "verdict: 30m under 25m must be a VIOLATION (got: $GOT)"
fi

# ═════════════════════════════════════════════════════════════════════════════
# THE REAL TREE
# ═════════════════════════════════════════════════════════════════════════════
REAL_GO="$(makefile_timeout_seconds "$MAKEFILE")"
REAL_CI="$(ci_job_ceiling "$CI_YML")"

case "$REAL_GO" in
  ''|*[!0-9]*) bad "Makefile: test-go must pass a parseable -timeout to go test (got: $REAL_GO)" ;;
  *)           ok  "Makefile: the go test call site passes -timeout — ${REAL_GO}s, a chosen number rather than go's 10m default" ;;
esac
case "$REAL_CI" in
  *$'\t'*$'\t'*) ok "ci.yml: the job that RUNS test-go is '$(printf '%s' "$REAL_CI" | cut -f1)', effective ceiling $(printf '%s' "$REAL_CI" | cut -f2) min set at $(printf '%s' "$REAL_CI" | cut -f3-) level — found by parsing, not by name, and taken as min(job, step)" ;;
  *)             bad "ci.yml: could not identify the job that runs test-go and its effective timeout-minutes (got: $REAL_CI)" ;;
esac

REAL_VERDICT="$(verdict "$MAKEFILE" "$CI_YML")"
if [[ "$REAL_VERDICT" == OK\ * ]]; then
  read -r _ g j n src <<<"$REAL_VERDICT"
  ok "sentinel — go's per-package timeout (${g}s) is STRICTLY LESS than the effective $n ceiling (${j}s, set at $src level): go hits its limit first and dumps goroutines"
else
  bad "sentinel — the real tree must satisfy the contract: $REAL_VERDICT"
fi

# ── T7: the GREEN-TIME warning line is under the ceiling it protects ────────
# GO_TEST_TOTAL_WARN is the only thing in the tree watching the sum of the
# modules' PASSING time, which is the sum that can actually kill the cell (the
# timeout sum cannot — `set -e` ends the recipe on the first module that times
# out; see the Makefile comment). An alarm set at or above the ceiling is an
# alarm that first rings after the cell is already dead.
# ── T7b: RUN THE RECIPE AND WATCH THE ALARM ────────────────────────────────
# 🔴 A TEXT CHECK IS NOT ENOUGH, AND REVIEW PROVED IT FOUR TIMES. This used to be
# a grep over the recipe; every one of these kept its strings in place and left it
# green while the alarm was dead or measuring the wrong thing:
#   * `total=0` reset inside the loop  ⇒ only the LAST module is ever weighed
#   * `elapsed=0`                      ⇒ the total is permanently 0
#   * `if false && [[ … ]]`            ⇒ the branch cannot be taken
#   * warn_s fed a literal `60m` while GO_TEST_TOTAL_WARN stays 5m ⇒ the constant
#     T7 compares against and the value the recipe uses come apart entirely
# A greppable pattern can always be satisfied without the behaviour. So this runs
# the recipe — `make -n` prints it fully expanded, and swapping in a stub `go`
# that sleeps 1s per module makes a complete run cost ~4s instead of 230s.
#
# The pair is what makes it load-bearing: with the line at 3s the four stubbed
# modules (≈4s total) MUST trip it, and with the line at 30s they MUST NOT. The
# positive case alone would pass on a recipe that shouts unconditionally; the
# negative alone would pass on one that never shouts. Each of the four mutants
# above fails the POSITIVE case, because each leaves the running total below any
# threshold the sum should have crossed.
run_recipe_with_stub_go() { # WARN_VALUE -> the recipe's output
  # 🔴 THE STUB SLEEPS A DIFFERENT AMOUNT EACH CALL, and that is the whole design.
  # It used to sleep 1s every time, which made the four modules sum to 4 — the same
  # number as the module COUNT. Every assertion built on it therefore could not
  # tell "add each module's seconds" from "add one per module": review changed
  # `total + elapsed` to `total + 1` and this check still printed FIRES ON THE SUM.
  # In production that mutant is fatal to the alarm — four 9-minute modules would
  # total 4, and `4 >= 300` never fires, which is the one situation
  # GO_TEST_TOTAL_WARN exists for. Sleeping 1,2,3,4 makes the sum 10 against a
  # count of 4, so a threshold between them separates the two readings.
  local warn="$1" expanded stub
  expanded="$(cd "$ROOT" && make -n test-go GO_TEST_TOTAL_WARN="$warn" 2>/dev/null)" || return 1
  stub="$WORK/stub-go"
  rm -f "$WORK/stub-calls"
  {
    echo '#!/usr/bin/env bash'
    echo "n=\$(cat '$WORK/stub-calls' 2>/dev/null || echo 0)"
    echo "n=\$(( n + 1 ))"
    echo "printf '%s' \"\$n\" > '$WORK/stub-calls'"
    echo 'sleep "$n"'
    echo 'exit 0'
  } > "$stub"
  chmod +x "$stub"
  # Keep only the test-go recipe itself: `make -n` prints its prerequisite
  # (build-embed-assets) first, and that one really does stage artefacts.
  # LAST recipe only. `make -n` prints the prerequisite (build-embed-assets)
  # first, and that one really does stage artefacts. Each recipe begins with the
  # shared `set -euo pipefail` prologue, so keeping only what follows the final
  # one isolates test-go — including its `GO=` line, which is the whole point of
  # the extraction and which a naive cut at `dur_s()` leaves behind.
  printf '%s\n' "$expanded" |
    awk '/^set -euo pipefail/ { buf = "" } { buf = buf $0 "\n" } END { printf "%s", buf }' |
    sed "s|GO=\"\$(oc_go)\"|GO=\"$stub\"|" > "$WORK/recipe.sh"
  bash "$WORK/recipe.sh" 2>&1
}

if ! command -v make >/dev/null 2>&1; then
  bad "green-time alarm — make is unavailable, so the recipe could not be run and nothing below was actually measured"
else
  # 7s sits ABOVE the module count (4) and BELOW the sum of their seconds (10),
  # so only a recipe that really adds up ELAPSED TIME crosses it.
  TRIPPED="$(run_recipe_with_stub_go 7s || true)"
  QUIET="$(run_recipe_with_stub_go 30s || true)"
  if ! printf '%s' "$TRIPPED" | grep -q 'all modules finished in'; then
    bad "green-time alarm — the recipe did not run to completion under a stub go, so neither case below measured anything (output: $(printf '%s' "$TRIPPED" | tail -2 | tr '\n' ' '))"
  elif ! printf '%s' "$TRIPPED" | grep -q 'TOTAL'; then
    bad "green-time alarm — RAN the recipe with the warning line at 7s and four stub modules taking 1+2+3+4=10s, and NO total-time warning was printed. The sum of green module time is what can kill the cell, and nothing is watching it. Causes review has actually produced: the running total reset inside the loop (only the last module weighed), elapsed forced to 0, the branch made unreachable, or the recipe reading a different value than GO_TEST_TOTAL_WARN"
  elif printf '%s' "$QUIET" | grep -q 'TOTAL'; then
    bad "green-time alarm — the warning fired with the line at 30s on a ~4s run, so it is unconditional. An alarm that is always on is one people learn to skip, which is the same as not having it"
  else
    ok "green-time alarm FIRES ON THE SUM OF SECONDS — measured by running the real recipe against a stub go whose modules take 1,2,3,4s: a 7s line (above the module COUNT of 4, below their 10s sum) trips it, a 30s line does not. Counting modules instead of adding their seconds fails the first half"
  fi
fi

REAL_WARN="$(makefile_named_duration_seconds "$MAKEFILE" GO_TEST_TOTAL_WARN)"
case "$REAL_WARN" in
  ''|*[!0-9]*)
    bad "Makefile: GO_TEST_TOTAL_WARN must be a parseable <N>m/<N>s total-time warning line (got: $REAL_WARN)" ;;
  *)
    case "$REAL_CI" in
      *$'\t'*$'\t'*)
        CEIL_S=$(( $(printf '%s' "$REAL_CI" | cut -f2) * 60 ))
        # 🔴 WARN ALONE IS THE WRONG COMPARISON. The number that has to fit under
        # the ceiling is the warn line PLUS the per-package timeout: a run that
        # reaches the warn line and THEN hangs needs its full GO_TEST_TIMEOUT
        # inside the cell to print the goroutine dump. Comparing WARN alone
        # accepted 24m under a 25m ceiling — 24 + 15 = 39m, an alarm that first
        # rings 14 minutes after diagnosability is gone.
        # ⚠️ REAL_GO is only a number on a healthy tree; on MISSING / DISABLED:0m /
        # UNPARSEABLE it is a WORD, and `(( ))` under `set -u` then aborted the whole
        # script — no summary line, and M1/M2/M3 never ran, which made the careful
        # "mutants skipped and why" message from an earlier round dead code on
        # exactly the paths it was written for. The real-tree failure is already
        # reported above; this arm just declines to add a second, derived one.
        if [[ "$REAL_GO" =~ ^[0-9]+$ ]] && (( REAL_WARN + REAL_GO < CEIL_S )); then
          ok "green-time alarm — GO_TEST_TOTAL_WARN (${REAL_WARN}s) PLUS the per-package timeout (${REAL_GO}s) is STRICTLY LESS than the effective ceiling (${CEIL_S}s): a run warned about and then hung still has room to print its goroutine dump"
        elif [[ ! "$REAL_GO" =~ ^[0-9]+$ ]]; then
          skip "green-time alarm — cannot weigh GO_TEST_TOTAL_WARN (${REAL_WARN}s) against the ceiling because the per-package timeout did not resolve ($REAL_GO), which has already failed above. Restating it here would read like a second, separate defect"
        else
          bad "green-time alarm — GO_TEST_TOTAL_WARN (${REAL_WARN}s) + the per-package timeout (${REAL_GO}s) is not below the effective ceiling (${CEIL_S}s): by the time it rings, a hang can no longer produce a dump before GitHub cancels the cell"
        fi ;;
      *) skip "green-time alarm — no ceiling to compare GO_TEST_TOTAL_WARN (${REAL_WARN}s) against; the ci.yml side did not parse ($REAL_CI) and has already failed above. Restating it here would read like a second, separate defect." ;;
    esac ;;
esac

# ═════════════════════════════════════════════════════════════════════════════
# MUTANTS, built from the REAL files. Each is one change and nothing else.
#
# EVERY mutant below is a DIFFERENTIAL claim: "change exactly this one thing in a
# tree that currently satisfies the contract, and the guard must go red." That
# claim is meaningless once the real tree does NOT satisfy it — the mutant would
# be red for the pre-existing reason, not for the mutation. Worse, the mutant
# GENERATORS locate their edit by what the parser found (REAL_TOKEN, REAL_JOB),
# so on a broken tree they produce noise that reads like an unrelated defect: run
# the guard against a ci.yml where two jobs run test-go and M2/M3 each announce
# `could not find MULTIPLE-JOBS:go-checks,contract-guards's timeout-minutes` —
# a message about a job name that does not exist, chasing a reader away from the
# one real finding above it. So when either side of the real tree failed to
# parse, the mutants are SKIPPED and say why. The run is still red: the real-tree
# assertions above already failed.
REAL_TOKEN="$(makefile_go_test_sites "$MAKEFILE" | awk -F'\t' 'NF { print $3; exit }')"
MUTANTS_MEANINGFUL=1
case "$REAL_GO" in ''|*[!0-9]*) MUTANTS_MEANINGFUL=0 ;; esac
case "$REAL_CI" in *$'\t'*$'\t'*) : ;; *) MUTANTS_MEANINGFUL=0 ;; esac
if (( ! MUTANTS_MEANINGFUL )); then
  skip "mutants M1/M2/M3 — the real tree does not parse (Makefile side: $REAL_GO; ci.yml side: $REAL_CI), so a red mutant would prove nothing about the mutation. Fix the real-tree failure above first."
fi
if (( MUTANTS_MEANINGFUL )); then

# M1 — strip -timeout from the Makefile's call site.
sed -E 's/ --?timeout[= ][^ ]+//g' "$MAKEFILE" > "$WORK/m1.mk"
if ! diff -q "$MAKEFILE" "$WORK/m1.mk" >/dev/null; then
  M1="$(verdict "$WORK/m1.mk" "$CI_YML")"
  if [[ "$M1" == BROKEN*MISSING* || "$M1" == BROKEN*NO-CALL-SITE* ]]; then
    ok "mutant M1 — Makefile with -timeout removed is CAUGHT ($M1)"
  else
    bad "mutant M1 — Makefile with -timeout removed was NOT caught (verdict: $M1)"
  fi
else
  bad "mutant M1 generator changed NOTHING — the Makefile carries no -timeout to strip, so the mutant proves nothing"
fi

# M2 — raise the timeout above the job ceiling. If the recipe reads the value
# from a make variable the assignment is what moves; if it is a literal, the
# literal is. Either way the guard must go red, and the guard finds out WHICH
# by re-reading the file rather than by assuming.
mutate_timeout() { # SRC DST NEWVAL
  local src="$1" dst="$2" new="$3" name
  case "$REAL_TOKEN" in
    '$('*')'|'${'*'}')
      name="${REAL_TOKEN#??}"; name="${name%?}"
      awk -v N="$name" -v V="$new" '
        $0 ~ "^" N "[[:space:]]*[:?+]?=" { print N " := " V; next } { print }' "$src" > "$dst"
      ;;
    *)
      sed -E "s/(--?timeout[= ])$REAL_TOKEN/\1$new/g" "$src" > "$dst"
      ;;
  esac
}
mutate_timeout "$MAKEFILE" "$WORK/m2.mk" "30m"
if ! diff -q "$MAKEFILE" "$WORK/m2.mk" >/dev/null; then
  M2="$(verdict "$WORK/m2.mk" "$CI_YML")"
  if [[ "$M2" == VIOLATION* ]]; then
    ok "mutant M2 — timeout raised to 30m (above the job ceiling) is CAUGHT ($M2)"
  else
    bad "mutant M2 — timeout raised to 30m was NOT caught (verdict: $M2)"
  fi
else
  bad "mutant M2 generator changed NOTHING — cannot prove the guard notices a raised timeout"
fi

# M3 — lower the CI job's ceiling under go's timeout. The job block is located
# by the NAME THE PARSER FOUND, so this mutant follows a renamed job instead of
# quietly editing nothing.
REAL_JOB="${REAL_CI%%$'\t'*}"
awk -v JOB="$REAL_JOB" '
  $0 ~ "^  " JOB ":[[:space:]]*(#.*)?$" { injob = 1; print; next }
  injob && /^  [A-Za-z0-9_.-]+:/ { injob = 0 }
  injob && /^[[:space:]]*timeout-minutes:[[:space:]]*[0-9]+[[:space:]]*$/ {
    sub(/[0-9]+[[:space:]]*$/, "1"); print; next
  }
  { print }
' "$CI_YML" > "$WORK/m3.yml"
if ! diff -q "$CI_YML" "$WORK/m3.yml" >/dev/null; then
  M3="$(verdict "$MAKEFILE" "$WORK/m3.yml")"
  if [[ "$M3" == VIOLATION* ]]; then
    ok "mutant M3 — the $REAL_JOB job's timeout-minutes lowered to 1 is CAUGHT ($M3)"
  else
    bad "mutant M3 — a job ceiling below go's timeout was NOT caught (verdict: $M3)"
  fi
else
  bad "mutant M3 generator changed NOTHING — could not find $REAL_JOB's timeout-minutes to lower"
fi

fi  # MUTANTS_MEANINGFUL

if (( SKIP )); then
  echo "go test timeout ↔ CI job ceiling contract tests: $PASS ok, $FAIL failed, $SKIP skipped"
else
  echo "go test timeout ↔ CI job ceiling contract tests: $PASS ok, $FAIL failed"
fi
[[ "$FAIL" == "0" ]]
