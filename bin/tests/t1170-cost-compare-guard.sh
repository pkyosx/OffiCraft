#!/usr/bin/env bash
# Hermetic guard for bin/measure/t1170-list-cost/compare.py — the cross-arm
# refusal.
#
# WHY THIS FILE EXISTS. The T-1170 cost figures were reported with the claim
# that "a script asserts both arms used the same caps and the same corpus".
# That assertion did not exist: the harness printed those fields into each arm's
# JSON and said the rest in a COMMENT, and nothing ever read the two files
# together. An independent reviewer falsified it by raising doc.cap_chars.duty
# to 2000 on the after arm only — the run exited 0, warned about nothing, and
# still produced a flattering before/after story out of two different worlds.
#
# So the refusal now exists in code, and this guard is what stops it quietly
# becoming a comment again: case 2 IS the reviewer's counterexample, and it
# fails if compare.py ever returns 0 for it. No server, no network — the
# comparator is a pure function over two JSON files.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
COMPARE="$REPO_ROOT/bin/measure/t1170-list-cost/compare.py"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

echo "[t1170-cost-compare] cross-arm refusal (bin/measure/t1170-list-cost/compare.py)"

if [[ ! -f "$COMPARE" ]]; then
  echo "  FAIL — $COMPARE is missing (renamed? then the cost claim lost its assertion)"
  exit 1
fi

# Own tempdir, never the repo tree. It is left behind on purpose: this suite
# deletes nothing, so a failure can be inspected after the fact.
WORK="$(mktemp -d -t oc-t1170-guard.XXXXXX)"

# One arm's JSON. $1 duty cap, $2 definition_md corpus size, $3..$5 the counts.
arm_json() {
  cat <<EOF
{
  "_caps": {"doc_cap_chars_duty": $1, "doc_cap_chars_manual_sop": 15000,
            "doc_cap_chars_manual_learnings": 15000},
  "_corpus_chars": {"definition_md": $2, "sop_md": 13500, "learnings": 13500,
                    "global_context": 3001},
  "list_document_history": {"chars": $3, "chars_min": $3, "chars_max": $3, "reads": 3},
  "list_task_manuals":     {"chars": $4, "chars_min": $4, "chars_max": $4, "reads": 3},
  "list_roles":            {"chars": $5, "chars_min": $5, "chars_max": $5, "reads": 3}
}
EOF
}

# ── case 1: the arms ARE the same experiment ⇒ compares, exits 0 ─────────────
arm_json 1000 900 9566 84048 3753 > "$WORK/before.json"
arm_json 1000 900  319   719  583 > "$WORK/after.json"
out="$(python3 "$COMPARE" "$WORK/before.json" "$WORK/after.json" 2>&1)"; rc=$?
if [[ $rc -eq 0 ]]; then ok "matched arms compare (rc=0)"
else bad "matched arms were REFUSED (rc=$rc) — the guard's own baseline is broken:
$out"; fi
if grep -q "list_task_manuals" <<<"$out"; then ok "the table names each endpoint"
else bad "the table did not name list_task_manuals"; fi

# ── case 2: THE REVIEWER'S COUNTEREXAMPLE ────────────────────────────────────
# doc.cap_chars.duty raised to 2000 on the AFTER arm only, which doubles the
# corpus sized off it. Before the fix this exited 0 and told a flattering story.
arm_json 1000  900 9566 84048 3753 > "$WORK/before.json"
arm_json 2000 1800  320   719  583 > "$WORK/after.json"
out="$(python3 "$COMPARE" "$WORK/before.json" "$WORK/after.json" 2>&1)"; rc=$?
if [[ $rc -ne 0 ]]; then ok "a cap raised on one arm only is REFUSED (rc=$rc)"
else bad "THE ORIGINAL DEFECT IS BACK: arms with different caps compared cleanly (rc=0).
$out"; fi
if grep -q "doc_cap_chars_duty" <<<"$out"; then ok "the refusal NAMES the field that differs"
else bad "the refusal did not name doc_cap_chars_duty — 'something differs' is not actionable"; fi
if grep -q "_corpus_chars.definition_md" <<<"$out"; then ok "the refusal also names the corpus that moved with it"
else bad "the refusal did not name the differing corpus field"; fi
# A refusal must not ALSO print the numbers: a table under a warning is what
# gets copied into a report.
if grep -qE '^list_(roles|task_manuals) ' <<<"$out"; then
  bad "the refusal STILL printed the comparison table — that is what gets quoted"
else ok "a refused comparison prints no table"; fi

# ── case 3: corpus differs while caps agree ⇒ still refused ─────────────────
arm_json 1000 900 9566 84048 3753 > "$WORK/before.json"
arm_json 1000 450  319   719  583 > "$WORK/after.json"
if ! python3 "$COMPARE" "$WORK/before.json" "$WORK/after.json" >/dev/null 2>&1
then ok "same caps but a different corpus is REFUSED"
else bad "a different corpus compared cleanly — caps alone do not pin the experiment"; fi

# ── case 4: a NEW cap setting present on one arm only ⇒ refused ─────────────
# Whole-dict comparison, not a hand-listed key set: a cap added later must break
# this loudly rather than sit outside a list nobody extended.
arm_json 1000 900 9566 84048 3753 > "$WORK/before.json"
python3 - "$WORK/before.json" "$WORK/after.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1], encoding="utf-8"))
d["_caps"]["doc_cap_chars_brand_new"] = 500
json.dump(d, open(sys.argv[2], "w", encoding="utf-8"))
PY
if ! python3 "$COMPARE" "$WORK/before.json" "$WORK/after.json" >/dev/null 2>&1
then ok "a cap key present on only one arm is REFUSED (no hand-listed key set)"
else bad "a new cap key on one arm only passed — the comparison is key-listed, not whole-dict"; fi

# ── case 5: a missing endpoint is refused, not silently skipped ─────────────
arm_json 1000 900 9566 84048 3753 > "$WORK/before.json"
python3 - "$WORK/before.json" "$WORK/after.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1], encoding="utf-8"))
del d["list_roles"]
json.dump(d, open(sys.argv[2], "w", encoding="utf-8"))
PY
if ! python3 "$COMPARE" "$WORK/before.json" "$WORK/after.json" >/dev/null 2>&1
then ok "an endpoint missing from one arm is REFUSED"
else bad "a missing endpoint was skipped silently"; fi

echo
if [[ $FAIL -gt 0 ]]; then
  echo "[t1170-cost-compare] FAIL — $FAIL failed, $PASS passed"; exit 1
fi
echo "[t1170-cost-compare] all green ($PASS checks)"
