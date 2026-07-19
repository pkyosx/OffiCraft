#!/usr/bin/env bash
# OffiCraft — one-command installer (ships INSIDE the release tarball).
#
# Usage (from the unpacked officraft-<tag>-darwin-arm64/ directory):
#   ./install.sh            # install to ~/.officraft/bin, migrate, start serve
#   ./install.sh --force    # overwrite an EXISTING install without the prompt
#                           # (the only way to proceed non-interactively)
#
# What it does, in order:
#   1. platform gate      — macOS Apple Silicon only (darwin/arm64).
#   2. existing-install gate — if ~/.officraft already carries an install
#                           (binaries or a database), say so LOUDLY and ask
#                           before overwriting: a running server would be
#                           swapped out under its own feet on its next
#                           restart. Interactive = y/N prompt (default NO);
#                           non-interactive = abort unless --force.
#   3. install binaries   — ocserverd/ocwarden/ocagent → ~/.officraft/bin
#                           (ocserverd embeds the SPA + seeds + warden/agent
#                           binaries; ocwarden/ocagent ship alongside for
#                           direct CLI use).
#   4. migrate            — `ocserverd migrate` (goose; creates
#                           ~/.officraft/server/data/officraft.db on first run).
#   5. port gate          — the effective serve port (default 8780) must be
#                           FREE: a taken port fails here with a clear message
#                           instead of a silent install that cannot start.
#   6. serve (foreground) — `ocserverd serve`. On a fresh install the server
#                           mints a one-time claim code and opens (or prints)
#                           http://127.0.0.1:<port>/?code=… where the owner
#                           sets the password. Ctrl-C stops the service;
#                           re-run `~/.officraft/bin/ocserverd serve` to start
#                           it again (launchd autostart is a follow-up ticket).
#
# Config: the server reads $OC_CONFIG (or ./oc.toml in CWD) for overrides —
# default port is 8780 (the OffiCraft standard; 8770 belongs to the retired
# open-company station), default data root ~/.officraft. No config is written
# by this installer; convention defaults just work on a clean machine.
set -euo pipefail

FORCE=0
for arg in "$@"; do
  case "$arg" in
    --force) FORCE=1 ;;
    -h|--help)
      sed -n '2,37p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "[install] FATAL: unknown argument '$arg' (supported: --force)" >&2
      exit 2
      ;;
  esac
done

if [[ "$(uname -s)" != "Darwin" || "$(uname -m)" != "arm64" ]]; then
  echo "[install] FATAL: OffiCraft v0.x supports macOS Apple Silicon (darwin/arm64) only." >&2
  echo "[install]        This machine reports: $(uname -s)/$(uname -m)" >&2
  exit 1
fi

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$HOME/.officraft"
BIN_DIR="$ROOT_DIR/bin"
DB_PATH="$ROOT_DIR/server/data/officraft.db"

for b in ocserverd ocwarden ocagent; do
  if [[ ! -f "$HERE/$b" ]]; then
    echo "[install] FATAL: $b missing next to install.sh — run this from the unpacked release directory." >&2
    exit 1
  fi
done

# ── existing-install gate ────────────────────────────────────────────────────
# An existing binary OR an existing database means this machine already runs
# (or ran) OffiCraft: overwriting silently would swap the binary out from
# under a live server on its next restart. Be loud, require an explicit yes.
EXISTING=""
[[ -x "$BIN_DIR/ocserverd" ]] && EXISTING="binary $BIN_DIR/ocserverd"
if [[ -f "$DB_PATH" ]]; then
  EXISTING="${EXISTING:+$EXISTING + }database $DB_PATH"
fi
if [[ -n "$EXISTING" ]]; then
  echo "[install] ⚠️  EXISTING OffiCraft install detected: $EXISTING"
  echo "[install]    Continuing will OVERWRITE the installed binaries (the database is kept"
  echo "[install]    and migrated in place). A currently running server keeps serving its old"
  echo "[install]    code until its next restart, which then comes up on the new binaries."
  if [[ "$FORCE" == 1 ]]; then
    echo "[install]    --force given — continuing."
  elif [[ -t 0 ]]; then
    printf '[install]    Overwrite this install? [y/N] '
    read -r reply
    case "$reply" in
      y|Y|yes|YES) echo "[install]    confirmed — continuing." ;;
      *) echo "[install] aborted — nothing was changed."; exit 1 ;;
    esac
  else
    echo "[install] FATAL: non-interactive run over an existing install — re-run with --force to overwrite. Nothing was changed." >&2
    exit 1
  fi
fi

echo "[install] installing binaries → $BIN_DIR"
mkdir -p "$BIN_DIR"
for b in ocserverd ocwarden ocagent; do
  # Install via copy-then-rename so a running old binary is never truncated.
  cp "$HERE/$b" "$BIN_DIR/$b.new"
  chmod +x "$BIN_DIR/$b.new"
  # Defensive: strip a quarantine flag if a browser download attached one
  # (curl/tar does not; Safari does). Best-effort — absence is the norm.
  xattr -d com.apple.quarantine "$BIN_DIR/$b.new" 2>/dev/null || true
  mv "$BIN_DIR/$b.new" "$BIN_DIR/$b"
done

echo "[install] migrating database (goose)…"
"$BIN_DIR/ocserverd" migrate

# Effective port: $OC_CONFIG / ./oc.toml [server].port override, else 8780
# (the OffiCraft standard port — NOT 8770, which belongs to the retired
# open-company station and collides on transition-period machines).
PORT=8780
for cfg in "${OC_CONFIG:-}" "./oc.toml"; do
  if [[ -n "$cfg" && -f "$cfg" ]]; then
    p="$(sed -n 's/^[[:space:]]*port[[:space:]]*=[[:space:]]*\([0-9]\{2,5\}\).*/\1/p' "$cfg" | head -1)"
    [[ -n "$p" ]] && PORT="$p"
    break
  fi
done

# ── port gate ────────────────────────────────────────────────────────────────
# Serving on a taken port would just die with EADDRINUSE after a seemingly
# successful install — check first and fail with an actionable message.
if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "[install] FATAL: port $PORT is already in use on this machine:" >&2
  lsof -nP -iTCP:"$PORT" -sTCP:LISTEN 2>/dev/null | sed 's/^/[install]   /' >&2 || true
  echo "[install] The binaries are installed and the database is migrated, but the server was" >&2
  echo "[install] NOT started. Pick a free port by writing an oc.toml next to where you run serve:" >&2
  echo "[install]   printf '[server]\\nport = <free-port>\\n' > oc.toml" >&2
  echo "[install]   $BIN_DIR/ocserverd serve" >&2
  exit 1
fi

echo
echo "[install] ✅ installed. Starting OffiCraft in the foreground…"
echo "[install]    open  http://127.0.0.1:${PORT}/  in your browser."
echo "[install]    (first run: the serve log below prints a one-time setup link"
echo "[install]     with ?code=… — open it to set your owner password.)"
echo "[install]    Ctrl-C stops the service; restart later with:"
echo "[install]      $BIN_DIR/ocserverd serve"
echo
exec "$BIN_DIR/ocserverd" serve
