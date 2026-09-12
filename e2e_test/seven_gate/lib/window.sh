#!/usr/bin/env bash
# e2e_test/seven_gate/lib/window.sh — the run's clock, in ONE place.
#
# 🔴 THE BUG THIS FILE EXISTS TO MAKE IMPOSSIBLE. The collector's window and the
# actor's patience used to be two independent constants: collect.py was started
# with `--seconds 900`, while actors/live.sh would wait up to
# 30 + 120 + 1800 + 300 ≈ 2250s. On DEFAULTS, the collector therefore stopped
# sampling ~22 minutes before the actor stopped working, and every fact that
# landed after that instant was invisible to judge.py.
#
# That does not read as "the harness stopped watching". It reads as
# 「按下結案 FAIL — the agent never closed the task」: A RED POINTING AT THE AGENT
# FOR SOMETHING THE HARNESS DID. It is the same disease as the swallowed curl
# output — a harness fault wearing an agent fault's face — and it is worse here,
# because the run that produces it looks complete.
#
# The first person to hit it (this round) worked around it by hand, by knowing
# to raise OC_SG_MAX_SECONDS. THE NEXT PERSON WILL NOT KNOW THAT FLAG EXISTS.
# So the fix is not a bigger number: it is that the collector window is DERIVED
# from the actor budget and can never be smaller. Two constants a human has to
# keep in sync is exactly the shape that rots.
#
# CONSEQUENCE FOR EVERY CALLER: the waits below have their ONE definition here.
# run.sh, actors/stub.sh and actors/live.sh source this file and use the
# variables bare — `$OC_SG_LIVE_WAIT`, never `${OC_SG_LIVE_WAIT:-1800}`. A second
# `:-default` anywhere is a second constant, which is the thing being removed.
# Guarded by tests_guard case (22), including a mutant that severs the
# derivation and must go red.

# Every knob is an override-if-unset, so the environment still wins and one
# `export` at the top of a run reaches the actor subprocess too.
: "${OC_SG_MACHINE_WAIT:=30}"      # live: seconds for the warden to come online
: "${OC_SG_SPAWN_WAIT:=120}"       # live: seconds for the tmux session to appear
: "${OC_SG_LIVE_WAIT:=1800}"       # live: seconds to let the agent walk the path
: "${OC_SG_FRICTION_WAIT:=300}"    # live: seconds to wait for the friction answers
: "${OC_SG_CARD_WAIT:=30}"         # stub: polls (of 2s) waiting for the owner's answer
: "${OC_SG_SETTLE:=3}"             # run.sh: settle before the collector is stopped
: "${OC_SG_COLLECT_MARGIN:=120}"   # slack on top of the budget, never the budget itself
: "${OC_SG_INTERVAL:=1}"           # collector poll interval
export OC_SG_MACHINE_WAIT OC_SG_SPAWN_WAIT OC_SG_LIVE_WAIT OC_SG_FRICTION_WAIT \
       OC_SG_CARD_WAIT OC_SG_SETTLE OC_SG_COLLECT_MARGIN OC_SG_INTERVAL

# sg_actor_budget_secs — the longest an actor may legitimately take, from the
# same knobs the actors actually obey. The stub's card wait is polls × 2s (its
# loop sleeps 2); the live waits are already seconds.
sg_actor_budget_secs() {
  echo $(( OC_SG_MACHINE_WAIT + OC_SG_SPAWN_WAIT + OC_SG_LIVE_WAIT
           + OC_SG_FRICTION_WAIT + OC_SG_CARD_WAIT * 2 ))
}

# sg_collect_seconds — the collector window. DERIVED, never independently set:
# the budget, plus the settle the run adds after the actor, plus slack. This is
# the only expression allowed to decide how long collect.py runs.
sg_collect_seconds() {
  echo $(( $(sg_actor_budget_secs) + OC_SG_SETTLE + OC_SG_COLLECT_MARGIN ))
}

# sg_assert_collection_window — the invariant, checked at runtime as well as in
# CI. Stated as a RELATION between the two, not as a floor under a number: any
# future knob that lengthens the actor lengthens the budget, and this keeps
# holding without anyone remembering it.
#
#   collector window  >=  actor budget
#
# rc 0 = holds. rc 1 = the caller must refuse to run: a run started under a
# violated window can only produce a verdict nobody may trust.
sg_assert_collection_window() {
  local budget window
  budget="$(sg_actor_budget_secs)"
  window="$(sg_collect_seconds)"
  if [[ "$window" -lt "$budget" ]]; then
    echo "[seven_gate] FATAL: the collector would stop before the actor does — window ${window}s < actor budget ${budget}s." >&2
    echo "[seven_gate] A run like that goes RED on whatever lands after the collector quits, and the red NAMES THE AGENT for the harness's own gap. Refusing to start." >&2
    return 1
  fi
  return 0
}
