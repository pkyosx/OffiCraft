#!/usr/bin/env bash
# Keeps the boot pack from carrying a roster HEADCOUNT again (T-792e).
#
# ── the failure mode this guard exists to stop ────────────────────────────────
# seeds/system_interaction.md §11 used to say "你自己——這個工作室目前唯一的 AI
# member" and "今天基本上只有「你 + 一位人類 owner」". Both were true when they
# were written and both were false by 2026-07-28, when the roster held 11 active
# AI members. Nothing noticed. That is the whole point: a headcount frozen into
# the boot pack produces no error, no failing test, and no log line when it goes
# stale — it just quietly teaches every agent, on every boot, that it has no
# teammates to look for. The fix was to delete the standing-state claims and keep
# the durable part (who the roles are + go read the roster when you need names).
# This guard pins that shape so a well-meaning edit cannot put a snapshot back.
#
# ── WHAT THIS GUARD GUARANTEES ───────────────────────────────────────────────
#   * §11 exists and is extractable. If its heading is gone this guard FAILS —
#     it never degrades into "found no §11, therefore nothing to complain about".
#   * No seed file carries one of the KNOWN headcount phrasings — the two that
#     were removed, the near variants an editor would reach for first, AND the
#     future-tense form ("與未來的 AI 隊友", "未來 owner hire 的其他 agent") that
#     §1 世界觀 carried. That last group is the one this ticket missed on its
#     first pass: it reads as framing rather than as a headcount, so it survived
#     while the blunt version got fixed.
#   * seeds/worker_context.md is present, so the negative scan above actually
#     covers the worker overlay rather than reporting a clean sweep of a file it
#     never opened, and it still cross-references §11.
#   * §11 still carries the RATIONALE sentence — the one that says the boot pack
#     deliberately omits the roster snapshot. Without this half, deleting the
#     explanation and later "helpfully" re-adding a member list would be green
#     the whole way.
#   * The operational pointer survives in BOTH places that carry it (§3 and §11):
#     look the roster up, address by id. The negative check alone is satisfied by
#     deleting §11 outright, which would take the instruction down with the rot.
#
# ── WHAT THIS GUARD DOES *NOT* GUARANTEE (read before trusting a green) ──────
# A GREEN HERE IS NOT A PROOF THAT THE BOOT PACK CARRIES NO STALE CLAIM. It
# matches fixed strings. Someone can write a fresh headcount in words this file
# has never seen ("目前團隊規模是十餘位") and stay green. What it does buy is
# that the EXACT sentences this ticket removed, and the obvious rewordings of
# them, cannot come back unnoticed — and that the guidance which replaced them
# cannot be deleted without a red. Treat a red as "re-read §11 and §3", not as a
# verdict about meaning.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
SEEDS="$ROOT/seeds"
SYS="$SEEDS/system_interaction.md"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

echo "== seed roster-freshness guard =="

[[ -f "$SYS" ]] || { echo "FATAL: $SYS not found" >&2; exit 2; }

# ── §11 must be extractable ─────────────────────────────────────────────────
# Fail-closed anchor: everything below reasons about §11's body, so a missing
# heading has to be a hard failure rather than an empty haystack that no pattern
# can match.
SECTION11="$(awk '/^## 11\. /{f=1;next} f&&/^## /{exit} f' "$SYS")"
if [[ -z "${SECTION11//[[:space:]]/}" ]]; then
  bad "§11 is missing, empty, or RENUMBERED in seeds/system_interaction.md (this guard anchors on the literal '## 11.' heading; inserting a section upstream reads the same as deleting it — check which before assuming the worst)"
  echo "seed roster guard: $PASS ok, $FAIL failed"
  exit 1
fi
ok "§11 is present and extractable"

# ── negative: no headcount / uniqueness claim anywhere in seeds/ ─────────────
# The first two are verbatim what T-792e removed; the rest are the rewordings a
# future editor would most plausibly reach for.
#
# The 未來-* entries are the SAME claim in the future tense, which is how §1
# 世界觀 carried it: "與未來的 AI 隊友" / "未來 owner hire 的其他 agent". It was
# missed on the first pass of T-792e and found in review — it reads as harmless
# framing rather than a headcount, which is exactly why it survived while the
# blunt version got fixed. It is a standing-state claim all the same: teammates
# exist NOW.
STALE_PATTERNS=(
  '唯一的 AI member'
  '唯一的 AI 成員'
  '唯一一個 AI'
  '只有你 + 一位人類'
  '只有「你 + 一位人類'
  '只有你和一位人類'
  '目前這個工作室只有你'
  '未來的 AI 隊友'
  '未來 owner hire'
  '未來 owner 可以 hire'
  '沒有 roster 隊友關係'
)
for pat in "${STALE_PATTERNS[@]}"; do
  if hits="$(grep -rn -F "$pat" "$SEEDS" 2>/dev/null)"; then
    bad "seeds/ carries a roster headcount claim: '$pat'"
    printf '         %s\n' "$hits"
  else
    ok "no seed says '$pat'"
  fi
done

# ── positive: the rationale that replaced the headcount must stay ────────────
if grep -qF '這份開機包刻意不寫' <<<"$SECTION11"; then
  ok "§11 still explains WHY the roster snapshot is deliberately omitted"
else
  bad "§11 lost the 'the boot pack deliberately does not carry a roster snapshot' rationale — without it, the next editor has nothing telling them not to paste a member list back in"
fi

# ── positive: the operational pointer must survive in BOTH sites ────────────
# §3 tells the reader ids are looked up, §11 tells them the same at the point
# where they are thinking about teammates. Pinning only one lets the other be
# dropped, which is how the instruction would quietly follow the rot out.
if grep -qF '查 roster 拿 id' <<<"$SECTION11"; then
  ok "§11 keeps the 'look the roster up, address by id' pointer"
else
  bad "§11 lost the 'look the roster up, address by id' pointer — removing the stale headcount must not remove the instruction that replaced it"
fi

SECTION3="$(awk '/^## 3\. /{f=1;next} f&&/^## /{exit} f' "$SYS")"
if [[ -z "${SECTION3//[[:space:]]/}" ]]; then
  bad "§3 is missing or empty (the id-lookup instruction lives there)"
else
  # Pin the INSTRUCTION LINE, not words that appear in it. Two earlier drafts
  # each fell to a review mutant: grepping §3 for the bare word "roster" let a
  # stub that merely said the word pass with the lookup block deleted, and
  # then requiring "查 roster" + "定址" as two free-floating substrings let a
  # stub pass that contained both while saying the OPPOSITE ("你不需要查
  # roster，也不必用 id 定址——直接用人名就行") — i.e. green text instructing
  # exactly the mistake §3's own warning exists to prevent. Anchoring on the
  # instruction's own line prefix is what actually discriminates.
  if grep -qF '用 MCP 查 roster' <<<"$SECTION3" && grep -qF '定址' <<<"$SECTION3"; then
    ok "§3 keeps the roster-lookup instruction (look it up AND address by the id you find)"
  else
    bad "§3 no longer carries the roster-lookup instruction ('用 MCP 查 roster' … '定址')"
  fi
fi

# ── the worker overlay half of the same edit ────────────────────────────────
# seeds/worker_context.md §1 used to describe the worker's teammate model by
# pointing at §11's "you + owner" shape. §11 no longer presents that shape, so
# reverting that line would leave the overlay describing a section that does not
# exist any more. The negative patterns above already cover the old wording; this
# asserts the file is actually in the haystack, so "no hits" cannot mean "never
# looked".
WORKER="$SEEDS/worker_context.md"
if [[ ! -f "$WORKER" ]]; then
  bad "seeds/worker_context.md is missing or RENAMED — either way the negative checks above silently stop covering the worker overlay; if it moved, point this guard at the new path rather than deleting the check"
elif grep -qF '§11' "$WORKER"; then
  ok "worker overlay still cross-references §11 (and is covered by the checks above)"
else
  bad "seeds/worker_context.md no longer references §11 — re-read it: the overlay is what tells a worker which parts of §11 do not apply to it"
fi

echo "seed roster guard: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
