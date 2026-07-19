#!/usr/bin/env bash
# OffiCraft — one-command installer (ships INSIDE the release tarball).
#
# Usage (from the unpacked officraft-<tag>-darwin-arm64/ directory):
#   ./install.sh          # install to ~/.officraft/bin, migrate, start serve
#
# What it does, in order:
#   1. platform gate      — macOS Apple Silicon only (darwin/arm64).
#   2. install binaries   — ocserverd/ocwarden/ocagent → ~/.officraft/bin
#                           (ocserverd embeds the SPA + seeds + warden/agent
#                           binaries; ocwarden/ocagent ship alongside for
#                           direct CLI use).
#   3. migrate            — `ocserverd migrate` (goose; creates
#                           ~/.officraft/server/data/officraft.db on first run).
#   4. serve (foreground) — `ocserverd serve`. On a fresh install the server
#                           mints a one-time claim code and opens (or prints)
#                           http://127.0.0.1:<port>/?code=… where the owner
#                           sets the password. Ctrl-C stops the service;
#                           re-run `~/.officraft/bin/ocserverd serve` to start
#                           it again (launchd autostart is a follow-up ticket).
#
# Config: the server reads $OC_CONFIG (or ./oc.toml in CWD) for overrides —
# default port is 8770, default data root ~/.officraft. No config is written
# by this installer; convention defaults just work on a clean machine.
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" || "$(uname -m)" != "arm64" ]]; then
  echo "[install] FATAL: OffiCraft v0.x supports macOS Apple Silicon (darwin/arm64) only." >&2
  echo "[install]        This machine reports: $(uname -s)/$(uname -m)" >&2
  exit 1
fi

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="$HOME/.officraft/bin"

for b in ocserverd ocwarden ocagent; do
  if [[ ! -f "$HERE/$b" ]]; then
    echo "[install] FATAL: $b missing next to install.sh — run this from the unpacked release directory." >&2
    exit 1
  fi
done

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

# Effective port: $OC_CONFIG / ./oc.toml [server].port override, else 8770.
PORT=8770
for cfg in "${OC_CONFIG:-}" "./oc.toml"; do
  if [[ -n "$cfg" && -f "$cfg" ]]; then
    p="$(sed -n 's/^[[:space:]]*port[[:space:]]*=[[:space:]]*\([0-9]\{2,5\}\).*/\1/p' "$cfg" | head -1)"
    [[ -n "$p" ]] && PORT="$p"
    break
  fi
done
echo
echo "[install] ✅ installed. Starting OffiCraft in the foreground…"
echo "[install]    open  http://127.0.0.1:${PORT}/  in your browser."
echo "[install]    (first run: the serve log below prints a one-time setup link"
echo "[install]     with ?code=… — open it to set your owner password.)"
echo "[install]    Ctrl-C stops the service; restart later with:"
echo "[install]      $BIN_DIR/ocserverd serve"
echo
exec "$BIN_DIR/ocserverd" serve
