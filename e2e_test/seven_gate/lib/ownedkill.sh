#!/usr/bin/env bash
# e2e_test/seven_gate/lib/ownedkill.sh — the harness may only kill what it can
# PROVE it created.
#
# 🔴 WHAT THIS PREVENTS. actors/live.sh used to do its teardown on the tmux
# socket named `officraft` — THE LIVE FLEET'S SOCKET. Serving members' sessions
# live there, including the session of whoever is reading this. It killed only
# exact names, so nothing bad happened; but "nothing bad happened" was a property
# of the harness being CORRECT, and this harness has already shipped a one-letter
# typo whose trap killed the agent it had just spawned (tests_guard case 23). On
# the fleet socket, that same class of bug is not a red run. It is an
# irreversible kill of somebody ELSE's live agent.
#
# TWO INDEPENDENT LAYERS, because either alone is one bug away from the fleet:
#
#   1. PHYSICAL. The run gets its OWN tmux socket, via the warden's existing
#      instance namespace (OC_NAMESPACE → tmuxSocketFor → "officraft-<ns>", see
#      cli/ocwarden/namespace.go + tmux.go). A kill issued against
#      `officraft-<ns>` CANNOT REACH a session on `officraft` — not because we
#      are careful, but because they are different sockets.
#   2. OWNERSHIP. Even on our own socket, we kill only names this run WROTE DOWN
#      when it created them. No pattern, no glob, no "list the sessions and pick
#      the ones that look like ours". FAIL-CLOSED: no ledger, or an empty one,
#      means KILL NOTHING — the failure mode of a missing record must be a leaked
#      session (recoverable, visible), never a wrong kill (irreversible).
#
# Both layers are guarded in tests_guard case (24), each with a mutant applied to
# a COPY of the file it mutates:
#   * MUT-nosocket  — live.sh's TMUX_SOCKET put back to `${OC_SG_TMUX_SOCKET:-officraft}`.
#   * MUT-pgrep     — the exact-pid kill relaxed to `kill $(pgrep -f …)`.
#   * MUT-listpick  — the exact-name kill relaxed to list-the-sessions-and-pick.
#   * MUT-contkill  — the exact-pid kill left aimed at exactly the right pid but
#                     sent SIGCONT: a teardown that names its target and tears
#                     nothing down. It is here because that shape ONCE SCORED
#                     ALL GREEN on this suite (2026-08-27), so the case that
#                     detects it is itself kept honest by a mutant.
#   * MUT-lowerkill / MUT-sigprefix / MUT-numsig / MUT-dashdash / MUT-sepsig /
#     MUT-attachedsig — the SAME teardown, spelled `-term` / `-SIGTERM` / `-6` /
#                     `-- "$pid"` / `-s TERM` / `-sTERM`. One per normalisation
#                     token; case 24i carries the ①…⑥ list this mirrors, and
#                     NEITHER side writes a count — this line named only four of
#                     them for a round after the other two shipped (found in the
#                     EIGHTH review, 2026-08-27), which is what a hand-count
#                     beside a growing list does.
#                     These are libs that WORK; each one asserts that a correct
#                     teardown is CREDITED, because the guard reddening on a
#                     working lib is how the guard gets switched off.
#   * MUT-nosuchsig / MUT-stkflt — a signal name this host cannot resolve must
#                     not be credited. `STKFLT` is a Linux signal that macOS
#                     cannot resolve, and it was once credited HERE purely
#                     because its name appeared in the harness's own lethal set.
# Every mutant above is applied to a COPY and re-run by case 24 on EVERY run of
# tests_guard, so its detection is re-measured by the suite itself and no
# date-stamped hand-count is kept here. (An earlier note claimed "rc=1 with
# 4 / 2 / 2 named FAILs respectively (2026-08-10)" — a count beside a list that
# has since gained MUT-contkill, MUT-sigprefix, MUT-sepsig and MUT-nosuchsig /
# MUT-stkflt, and it went stale in silence because losing a pin costs no red. It
# is retracted in the EIGHTH review, 2026-08-27, rather than re-counted.)
# And a POSITIVE CONTROL, because every assertion here is an assertion of
# absence and "safe" is trivially achieved by killing nothing — that green looks
# exactly like the real one. So case 24 spawns a REAL process, records it, and
# requires it to actually die, while a byte-identical un-recorded process next to
# it must survive.

# The live fleet's socket name — cli/ocwarden/tmux.go `tmuxSocket`. Named here
# ONLY so it can be refused; nothing in this harness may ever target it.
SG_FLEET_SOCKET="officraft"

# sg_own_socket_assert SOCKET — refuse the fleet socket, loudly.
# This is the line that turns "we are careful" into "it cannot happen": every
# kill path that NAMES A SOCKET runs through it, so a future edit that points the
# harness back at the fleet dies here instead of taking a live agent with it.
# (sg_own_kill_pids does not, and cannot — a pid has no socket. Its whole
# protection is the ledger, which is why case 24 pins that half on real
# processes rather than on this assert.)
sg_own_socket_assert() {
  local socket="${1:-}"
  if [[ -z "$socket" ]]; then
    echo "[ownedkill] FATAL: empty tmux socket — refusing to operate on tmux's DEFAULT socket, which is shared." >&2
    return 2
  fi
  if [[ "$socket" == "$SG_FLEET_SOCKET" ]]; then
    echo "[ownedkill] FATAL: refusing to act on the LIVE FLEET socket '$SG_FLEET_SOCKET' — serving members' sessions live there. This harness must run on its own namespaced socket (OC_NAMESPACE)." >&2
    return 2
  fi
  return 0
}

# sg_own_record LEDGER VALUE — write down something we just created, at the
# moment we create it. The ledger is the ONLY authority for what may be killed.
sg_own_record() {
  local ledger="${1:?}" value="${2:?}"
  printf '%s\n' "$value" >> "$ledger"
}

# sg_own_kill_sessions SOCKET LEDGER — kill exactly the session names in the
# ledger, on the given socket, one explicit `-t <name>` each.
sg_own_kill_sessions() {
  local socket="${1:-}" ledger="${2:-}"
  sg_own_socket_assert "$socket" || return 2
  if [[ -z "$ledger" || ! -s "$ledger" ]]; then
    echo "[ownedkill] no session ledger at '${ledger:-<unset>}' — killing NOTHING. A leaked session is recoverable; a wrong kill is not." >&2
    return 0
  fi
  local name
  while IFS= read -r name; do
    [[ -n "$name" ]] || continue
    echo "[ownedkill] kill-session -t $name (socket $socket, from the ledger)" >&2
    tmux -L "$socket" kill-session -t "$name" 2>/dev/null
  done < "$ledger"
  return 0
}

# sg_own_kill_pids LEDGER — same rule for processes: exact pids we recorded.
# Never a name, never a pattern; `pkill -f` / `killall` are banned outright
# because the fleet runs the same binaries with the same argv.
sg_own_kill_pids() {
  local ledger="${1:-}"
  if [[ -z "$ledger" || ! -s "$ledger" ]]; then
    echo "[ownedkill] no pid ledger at '${ledger:-<unset>}' — killing NOTHING." >&2
    return 0
  fi
  local pid
  while IFS= read -r pid; do
    [[ "$pid" =~ ^[0-9]+$ ]] || continue
    echo "[ownedkill] kill $pid (from the ledger)" >&2
    kill "$pid" 2>/dev/null
  done < "$ledger"
  return 0
}
