#!/usr/bin/env bash
# e2e_test/lib/tmux.sh — carry and manage the isolated serve between agent execs.
#
# setup.sh and teardown.sh both source this file.  The tmux socket/session are
# per-run names, never the shared `officraft` socket.  Keeping the validation in
# one helper matters: a setup that starts on a private socket but a teardown that
# accepts an arbitrary state-file value is still a production teardown hazard.

oc_e2e_tmux_validate_name() {
  local role="$1" name="$2"
  if [[ "$name" =~ ^oc-e2e-[0-9a-f]{32}$ ]]; then
    return 0
  fi
  echo "[e2e] FATAL: tmux $role '$name' is not an isolated oc-e2e namespace; refusing to touch a shared or production socket/session." >&2
  return 2
}

oc_e2e_tmux_require() {
  if ! command -v tmux >/dev/null 2>&1; then
    echo "[e2e] FATAL: tmux is required for the isolated e2e server; setup must keep ocserverd alive across independent agent execs." >&2
    return 2
  fi
  return 0
}

# oc_e2e_tmux_start SOCKET SESSION REPO_ROOT SERVER LOG -> pane pid on stdout.
# The command is intentionally launched through a private tmux socket.  The
# ambient-fleet scrub is emitted by common.sh's single source of truth; keeping
# it out of this helper prevents the two launch paths from drifting. The pane
# pid is returned only as a diagnostic/launch pid; setup still records the
# actual socket holder after the identity-bound health check.
oc_e2e_tmux_start() {
  local socket="$1" session="$2" repo_root="$3" server="$4" log="$5" pane_pid command env_prefix
  oc_e2e_tmux_validate_name socket "$socket" || return $?
  oc_e2e_tmux_validate_name session "$session" || return $?
  oc_e2e_tmux_require || return $?

  if ! declare -F oc_e2e_scrub_env_command_prefix >/dev/null 2>&1; then
    echo "[e2e] FATAL: common.sh's environment scrub helper is unavailable; refusing to start the isolated serve without fleet isolation." >&2
    return 2
  fi
  env_prefix="$(oc_e2e_scrub_env_command_prefix)" || return $?
  command="cd \"$repo_root\" && exec $env_prefix \"$server\" serve >\"$log\" 2>&1"
  if ! tmux -L "$socket" -f /dev/null new-session -d -s "$session" -c "$repo_root" "$command"; then
    echo "[e2e] FATAL: tmux could not create isolated session '$session' on socket '$socket'." >&2
    return 1
  fi

  if ! pane_pid="$(tmux -L "$socket" -f /dev/null display-message -p -t "$session" '#{pane_pid}')"; then
    echo "[e2e] FATAL: tmux created '$session' but could not read its pane pid." >&2
    return 1
  fi
  case "$pane_pid" in
    ''|*[!0-9]*)
      echo "[e2e] FATAL: tmux session '$session' returned a non-numeric pane pid '$pane_pid'." >&2
      return 1
      ;;
  esac
  printf '%s\n' "$pane_pid"
}

# oc_e2e_tmux_stop SOCKET SESSION -> 0 when the exact private session is gone.
# A missing session is a clean no-op; a failed kill is loud and leaves the
# caller's later exact-pid/port checks as a second line of defence.
oc_e2e_tmux_stop() {
  local socket="$1" session="$2"
  oc_e2e_tmux_validate_name socket "$socket" || return $?
  oc_e2e_tmux_validate_name session "$session" || return $?
  if ! oc_e2e_tmux_require; then
    echo "[teardown] WARN: cannot inspect tmux session '$session' because tmux is unavailable." >&2
    return 1
  fi
  if ! tmux -L "$socket" -f /dev/null has-session -t "$session" >/dev/null 2>&1; then
    echo "[teardown] tmux session '$session' already gone (socket '$socket')"
    return 0
  fi
  if tmux -L "$socket" -f /dev/null kill-session -t "$session" >/dev/null 2>&1; then
    echo "[teardown] stopped tmux session '$session' (socket '$socket')"
    return 0
  fi
  echo "[teardown] WARN: failed to stop exact tmux session '$session' on socket '$socket'" >&2
  return 1
}
