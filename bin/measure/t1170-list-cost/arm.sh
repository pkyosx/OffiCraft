#!/usr/bin/env bash
# One measurement ARM: build ocserverd from a given worktree, serve it in
# isolation, seed the corpus, measure the three list answers, tear it down.
#
#   arm.sh /path/to/worktree /path/to/out.json
#
# 🔴 TEARDOWN OWNS A REAL PID, and that is a fix rather than a flourish. The
# first version launched the daemon as `( cd … && nohup ocserverd serve ) &`
# and kept `$!` — which is the SUBSHELL's pid, not the daemon's. Killing it
# reaped the wrapper and left ocserverd reparented to init, so every arm leaked
# one server: an independent reviewer found six of them still listening, the
# oldest half an hour old, each holding a port and a deleted tempdir. `exec`
# below makes the pid we keep the pid we started, and the identity check right
# after it REFUSES to continue if it ever stops being true again — a leak that
# only shows up in someone else's `ps` is exactly the kind nobody notices.
#
# Nothing here is ever killed by NAME. `pkill -f ocserverd` on a developer
# machine would take out the station's own live server (and any colleague's
# run); every kill in this file is a pid whose argv[0] is provably this run's
# own throwaway binary under its own `mktemp -d`.
set -uo pipefail

WT="${1:-}"; OUT="${2:-}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -z "$WT" || -z "$OUT" ]]; then
  echo "usage: arm.sh WORKTREE OUT.json" >&2; exit 64
fi
[[ -d "$WT/server/ocserverd" ]] || { echo "[arm] not a worktree: $WT" >&2; exit 64; }

# ocserverd serves seeds/docs/warden EMBED-ONLY, so the dists must be staged
# before the build or the daemon boots without them.
for d in seedsdist docsdist; do
  if ! ls "$WT"/server/ocserverd/$d/*.md >/dev/null 2>&1; then
    (cd "$WT" && bash "bin/build-$d" >/dev/null 2>&1) \
      || { echo "[arm] build-$d failed" >&2; exit 1; }
  fi
done
if [[ ! -f "$WT/server/ocserverd/bindist/ocwarden" ]]; then
  (cd "$WT" && bash bin/build-bindist >/dev/null 2>&1) \
    || { echo "[arm] build-bindist failed" >&2; exit 1; }
fi

WORK="$(mktemp -d -t oc-t1170.XXXXXX)"
PW="t1170-$(od -An -N12 -tx1 /dev/urandom | tr -d ' \n')"
cat >"$WORK/oc.toml" <<EOF
[server]
port = 0

[storage]
dsn = "sqlite:///$WORK/t1170.db"
EOF

# Never let the operator's real warden credentials reach this throwaway server.
oc_env() { env -u OC_ID -u OC_TOKEN -u OC_BASE OC_CONFIG="$WORK/oc.toml" "$@"; }

SERVE_PID=""

_pid_alive() { # true only for a live, non-zombie pid (kill -0 alone is not a death test)
  local st; st="$(ps -p "$1" -o state= 2>/dev/null || true)"
  [[ -n "$st" && "$st" != Z* ]]
}

_stop_pid() { # _stop_pid PID — TERM, confirm, KILL, confirm. Never by name.
  local pid="$1"
  _pid_alive "$pid" || return 0
  kill "$pid" 2>/dev/null
  for _ in $(seq 1 10); do _pid_alive "$pid" || break; sleep 1; done
  if _pid_alive "$pid"; then kill -9 "$pid" 2>/dev/null; sleep 1; fi
  if _pid_alive "$pid"; then
    echo "[arm] WARN: pid $pid survived TERM and KILL — stop it by hand" >&2
    return 1
  fi
}

teardown() {
  [[ -n "$SERVE_PID" ]] && _stop_pid "$SERVE_PID"
  # Belt and braces: anything still running whose argv IS this run's throwaway
  # binary is provably ours (fresh mktemp -d per invocation), so it is safe to
  # reclaim and unsafe to leave. Anything else is left strictly alone.
  local stray
  for stray in $(ps -A -o pid=,command= 2>/dev/null \
                 | awk -v p="$WORK/ocserverd" '$2==p {print $1}'); do
    echo "[arm] reclaiming stray $stray (argv is $WORK/ocserverd — provably ours)" >&2
    _stop_pid "$stray"
  done
  if ps -A -o command= 2>/dev/null | grep -qF "$WORK/ocserverd"; then
    echo "[arm] WARN: a process still names $WORK/ocserverd" >&2
  else
    echo "[arm] teardown clean — no process names $WORK/ocserverd" >&2
  fi
}
trap teardown EXIT INT TERM

echo "[arm] building ocserverd from $WT" >&2
(cd "$WT/server/ocserverd" && go build -o "$WORK/ocserverd" .) || exit 1
(cd "$WT" && oc_env "$WORK/ocserverd" migrate >/dev/null 2>&1) || exit 1
(cd "$WT" && oc_env env OC_NEW_PASSWORD="$PW" "$WORK/ocserverd" set-password >/dev/null 2>&1) || exit 1

# `exec` — the subshell BECOMES ocserverd, so $! is the daemon itself.
(cd "$WT" && exec env -u OC_ID -u OC_TOKEN -u OC_BASE OC_CONFIG="$WORK/oc.toml" \
   "$WORK/ocserverd" serve >"$WORK/serve.log" 2>&1) &
SERVE_PID=$!

# The identity check that keeps the line above honest. If $SERVE_PID ever stops
# naming our binary, the pid we would "kill" at teardown is not the daemon and
# the arm leaks — so this is a hard failure, not a warning.
sleep 1
SERVE_CMD="$(ps -p "$SERVE_PID" -o command= 2>/dev/null || true)"
case "$SERVE_CMD" in
  "$WORK/ocserverd serve") : ;;
  *) echo "[arm] FATAL: pid $SERVE_PID is not our daemon (argv: ${SERVE_CMD:-<gone>})." >&2
     echo "[arm] Teardown would leak a server. Refusing to measure." >&2
     tail -20 "$WORK/serve.log" >&2 || true
     exit 1 ;;
esac

BASE=""
for _ in $(seq 1 40); do
  BASE="$(grep -Eo 'http://127\.0\.0\.1:[0-9]+' "$WORK/serve.log" 2>/dev/null | tail -1 || true)"
  [[ -n "$BASE" ]] && break
  _pid_alive "$SERVE_PID" || break
  sleep 1
done
if [[ -z "$BASE" ]]; then
  echo "[arm] FATAL: ocserverd announced no port. serve.log tail:" >&2
  tail -20 "$WORK/serve.log" >&2; exit 1
fi
for _ in $(seq 1 30); do curl -sf "$BASE/api/version" >/dev/null 2>&1 && break; sleep 1; done
echo "[arm] serving at $BASE (pid $SERVE_PID) from $WT" >&2

python3 "$HERE/measure.py" "$BASE" "$PW" > "$OUT"
