#!/usr/bin/env bash
# e2e_test/seven_gate/lib/friction.sh — the ONE reader of the two follow-up
# questions.
#
# The wording lives in friction.md and nowhere else (AGENTS.md 〈friction〉:
# "問法逐字寫死在 friction.md"，tests_guard case 21d pins both questions
# verbatim and bans the pleasantry forms). Two callers now need them — run.sh
# prints them for a human to ask, actors/live.sh puts them to the real agent —
# so the EXTRACTION lives here once. A second copy of this sed is a second copy
# of the questions waiting to drift, and the one that drifts is the one that
# gets asked.

# sg_friction_questions PATH_TO_friction.md — prints the questions, verbatim,
# one per line, in file order. Nothing here rewrites, numbers, or prefixes them.
sg_friction_questions() {
  local src="$1"
  [[ -f "$src" ]] || { echo "[friction] FATAL: no such file: $src" >&2; return 2; }
  local out
  out="$(sed -n '/^## 逐字問句/,/^## 怎麼用/p' "$src" | sed '/^## /d;/^$/d')"
  if [[ -z "$out" ]]; then
    echo "[friction] FATAL: extracted ZERO questions from $src — the section headings moved. Refusing to ask nothing and call it asked." >&2
    return 2
  fi
  printf '%s\n' "$out"
}
