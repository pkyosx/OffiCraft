#!/usr/bin/env bash
# OffiCraft — one-command installer. DUAL MODE:
#
#   A. package mode (ships INSIDE the release tarball, binaries next to it):
#      ./install.sh            # install to ~/.officraft/bin, migrate, start serve
#                              # as a launchd BACKGROUND service (survives closing
#                              # the terminal; restarts on crash and at login)
#      ./install.sh --force    # overwrite an EXISTING install without the prompt
#                              # (the only way to proceed non-interactively)
#      ./install.sh --foreground
#                              # developer mode: run serve in THIS terminal
#                              # (Ctrl-C stops it), no launchd job is created
#
#   B. standalone mode (this file alone — no ocserverd next to it), e.g.:
#      curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
#      It bootstraps: resolves the latest release tag (override with
#      OC_INSTALL_TAG=vX.Y.Z or `--tag vX.Y.Z`), downloads the official
#      officraft-<tag>-darwin-arm64.tar.gz + checksums.txt from GitHub
#      Releases, verifies sha256 (a mismatch ABORTS before anything touches
#      the machine), unpacks to a temp dir and DELEGATES to the install.sh
#      inside the package — one code path, no duplicated install logic.
#      `--force` is forwarded. Piped stdin is not a tty, so over an existing
#      install the safe default still applies: abort unless --force.
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
#   6. launchd service    — render ~/Library/LaunchAgents/com.officraft.serve.plist
#                           (RunAtLoad + KeepAlive), bootout→poll→bootstrap→
#                           kickstart, then WAIT for the port to actually listen
#                           before reporting success. Re-running is idempotent:
#                           the old registration is dropped first, so no second
#                           copy is ever stacked on. The label is only adopted
#                           when an existing plist already points at OUR binary
#                           — an unrecognised same-label job is a hard stop that
#                           changes nothing (override: OC_LAUNCHD_LABEL=…).
#                           On a fresh install the server mints a one-time claim
#                           code; the installer prints the resulting
#                           http://127.0.0.1:<port>/?code=… setup link.
#                           With --foreground: serve runs in the terminal
#                           instead and NO launchd job is touched.
#
# Config: the server reads $OC_CONFIG (or ./oc.toml in CWD) for overrides —
# default port is 8780 (the OffiCraft standard; 8770 belongs to the retired
# open-company station), default data root ~/.officraft. No config is written
# by this installer; convention defaults just work on a clean machine.
set -euo pipefail

# ── mode detection ───────────────────────────────────────────────────────────
# Package mode = this file sits next to the three release binaries (unpacked
# tarball). Anything else — including `curl … | bash`, where BASH_SOURCE is
# empty because the script comes from stdin — is standalone bootstrap mode.
SELF="${BASH_SOURCE[0]:-}"
IN_PACKAGE=0
if [[ -n "$SELF" && -f "$SELF" ]]; then
  SELF_DIR="$(cd "$(dirname "$SELF")" && pwd)"
  if [[ -f "$SELF_DIR/ocserverd" && -f "$SELF_DIR/ocwarden" && -f "$SELF_DIR/ocagent" ]]; then
    IN_PACKAGE=1
  fi
fi

if [[ "$IN_PACKAGE" == 0 ]]; then
  # ── standalone bootstrap (curl | bash) ─────────────────────────────────────
  OC_REPO="${OC_INSTALL_REPO:-pkyosx/OffiCraft}"
  TAG="${OC_INSTALL_TAG:-}"
  FWD=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --tag)
        if [[ $# -lt 2 || -z "$2" ]]; then
          echo "[install] FATAL: --tag needs a value (e.g. --tag v0.4.1)" >&2
          exit 2
        fi
        TAG="$2"; shift 2 ;;
      --tag=*) TAG="${1#--tag=}"; shift ;;
      --force) FWD+=("--force"); shift ;;
      --foreground) FWD+=("--foreground"); shift ;;
      -h|--help)
        cat <<'EOF'
OffiCraft standalone installer — downloads the latest official release,
verifies its sha256 against the published checksums.txt, unpacks it and runs
the packaged install.sh (which installs to ~/.officraft/bin, migrates the
database, then starts the server as a launchd BACKGROUND service — it keeps
running after you close the terminal, and comes back after a crash or reboot).

  curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
  curl -fsSL … | bash -s -- --force            # overwrite an existing install
  curl -fsSL … | bash -s -- --foreground       # run serve in this terminal instead
  curl -fsSL … | bash -s -- --tag v0.4.1       # install a specific release
  OC_INSTALL_TAG=v0.4.1 curl -fsSL … | bash    # same, via environment

Over an existing install (~/.officraft binaries or database) the installer
aborts unless --force — piped stdin is non-interactive, so nothing is ever
overwritten silently.
EOF
        exit 0 ;;
      *)
        echo "[install] FATAL: unknown argument '$1' (supported: --force, --foreground, --tag <vX.Y.Z>)" >&2
        exit 2 ;;
    esac
  done

  if [[ "$(uname -s)" != "Darwin" || "$(uname -m)" != "arm64" ]]; then
    echo "[install] FATAL: OffiCraft v0.x supports macOS Apple Silicon (darwin/arm64) only." >&2
    echo "[install]        This machine reports: $(uname -s)/$(uname -m)" >&2
    exit 1
  fi

  if [[ -z "$TAG" ]]; then
    echo "[install] resolving latest release of $OC_REPO …"
    api_json="$(curl -fsSL "https://api.github.com/repos/$OC_REPO/releases/latest")" || {
      echo "[install] FATAL: could not query GitHub for the latest release of $OC_REPO." >&2
      echo "[install]        Check network access, or pin a version: --tag vX.Y.Z / OC_INSTALL_TAG=vX.Y.Z" >&2
      exit 1
    }
    TAG="$(printf '%s' "$api_json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
    if [[ -z "$TAG" ]]; then
      echo "[install] FATAL: could not parse tag_name from the GitHub API response." >&2
      exit 1
    fi
  fi

  ASSET="officraft-$TAG-darwin-arm64.tar.gz"
  # OC_INSTALL_BASE_URL: optional override of the asset download base
  # (mirrors / hermetic tests). Default: the official GitHub release.
  BASE_URL="${OC_INSTALL_BASE_URL:-https://github.com/$OC_REPO/releases/download/$TAG}"

  TMP="$(mktemp -d -t officraft-install.XXXXXX)"
  # Default (launchd) path: the delegated installer returns once the service is
  # registered and listening, and the trap removes the temp dir then — safe,
  # because launchd re-execs the binary from ~/.officraft/bin, not from here.
  # With --foreground the delegated installer ends in `exec ocserverd serve` in
  # a CHILD process, so this shell stays alive as its parent and the trap fires
  # when serve exits (Ctrl-C).
  trap 'rm -rf "$TMP"' EXIT INT TERM

  echo "[install] downloading $ASSET (release $TAG)…"
  curl -fSL --progress-bar -o "$TMP/$ASSET" "$BASE_URL/$ASSET" || {
    echo "[install] FATAL: download failed: $BASE_URL/$ASSET" >&2
    exit 1
  }
  echo "[install] downloading checksums.txt…"
  curl -fsSL -o "$TMP/checksums.txt" "$BASE_URL/checksums.txt" || {
    echo "[install] FATAL: download failed: $BASE_URL/checksums.txt" >&2
    exit 1
  }

  # sha256 gate — a bad or missing checksum ABORTS; nothing is installed.
  CHECK_LINE="$(grep -F "  $ASSET" "$TMP/checksums.txt" | head -1 || true)"
  if [[ -z "$CHECK_LINE" ]]; then
    echo "[install] FATAL: checksums.txt has no entry for $ASSET — refusing to install." >&2
    exit 1
  fi
  if ! (cd "$TMP" && printf '%s\n' "$CHECK_LINE" | shasum -a 256 -c - >/dev/null 2>&1); then
    echo "[install] FATAL: sha256 verification FAILED for $ASSET." >&2
    echo "[install]        expected: $CHECK_LINE" >&2
    echo "[install]        actual:   $(cd "$TMP" && shasum -a 256 "$ASSET")" >&2
    echo "[install]        The download is corrupt or tampered with — NOTHING was installed." >&2
    exit 1
  fi
  echo "[install] sha256 verified OK."

  tar -xzf "$TMP/$ASSET" -C "$TMP"
  PKG_DIR="$TMP/officraft-$TAG-darwin-arm64"
  if [[ ! -f "$PKG_DIR/install.sh" ]]; then
    echo "[install] FATAL: unpacked package has no install.sh at $PKG_DIR — refusing to continue." >&2
    exit 1
  fi

  echo "[install] delegating to the packaged installer…"
  bash "$PKG_DIR/install.sh" ${FWD[@]+"${FWD[@]}"}
  exit $?
fi

# ── package mode (original in-tarball flow, unchanged) ───────────────────────
FORCE=0
FOREGROUND=0
for arg in "$@"; do
  case "$arg" in
    --force) FORCE=1 ;;
    --foreground) FOREGROUND=1 ;;
    -h|--help)
      sed -n '2,60p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "[install] FATAL: unknown argument '$arg' (supported: --force, --foreground)" >&2
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
#
# CFG_ABS records WHICH config file that port came from, as an ABSOLUTE path.
# It matters for the launchd path: a relative ./oc.toml is resolved against the
# CWD of whoever ran the installer, and the daemon has a different CWD. Baking
# the absolute path into the plist's OC_CONFIG is what keeps the port the
# installer gated on and the port the daemon binds identical.
PORT=8780
CFG_ABS=""
for cfg in "${OC_CONFIG:-}" "./oc.toml"; do
  if [[ -n "$cfg" && -f "$cfg" ]]; then
    p="$(sed -n 's/^[[:space:]]*port[[:space:]]*=[[:space:]]*\([0-9]\{2,5\}\).*/\1/p' "$cfg" | head -1)"
    [[ -n "$p" ]] && PORT="$p"
    CFG_ABS="$(cd "$(dirname "$cfg")" && pwd)/$(basename "$cfg")"
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

if [[ "$FOREGROUND" == 1 ]]; then
  echo
  echo "[install] ✅ installed. Starting OffiCraft in the foreground (--foreground)…"
  echo "[install]    open  http://127.0.0.1:${PORT}/  in your browser."
  echo "[install]    (first run: the serve log below prints a one-time setup link"
  echo "[install]     with ?code=… — open it to set your owner password.)"
  echo "[install]    Ctrl-C stops the service; restart later with:"
  echo "[install]      $BIN_DIR/ocserverd serve"
  echo
  exec "$BIN_DIR/ocserverd" serve
fi

# ── launchd service install (default) ────────────────────────────────────────
# Registering serve as a launchd user agent is what makes "installed" mean the
# same thing an hour later: RunAtLoad starts it at login, KeepAlive restarts it
# after a crash, and it is not tied to the terminal that ran the installer.
LABEL="${OC_LAUNCHD_LABEL:-com.officraft.serve}"
LA_DIR="$HOME/Library/LaunchAgents"
PLIST="$LA_DIR/$LABEL.plist"
GUI="gui/$(id -u)"
TARGET="$GUI/$LABEL"
LOG_DIR="$ROOT_DIR/server/log"
SERVE_LOG="$LOG_DIR/serve.log"

# ── same-label ownership gate ────────────────────────────────────────────────
# The label is a machine-wide singleton in the user's gui domain. If something
# is ALREADY registered under it, blindly rendering over the plist and booting
# it out would silently hijack — or kill — a service this installer did not
# create (a hand-built job, or a repo-layout `bin/ocserver install` instance
# that runs serve from a completely different path). So: adopt the label only
# when the existing plist demonstrably points at the binary WE just installed;
# anything else is a hard stop that changes nothing.
plist_program() {
  # ProgramArguments[0] of an existing plist, or "" if unreadable/absent.
  plutil -extract ProgramArguments.0 raw -o - "$1" 2>/dev/null || true
}
if [[ -f "$PLIST" ]]; then
  existing_prog="$(plist_program "$PLIST")"
  if [[ "$existing_prog" != "$BIN_DIR/ocserverd" ]]; then
    echo "[install] FATAL: launchd label '$LABEL' is already taken by a DIFFERENT service." >&2
    echo "[install]        plist:   $PLIST" >&2
    echo "[install]        program: ${existing_prog:-<unreadable>}" >&2
    echo "[install]        expected: $BIN_DIR/ocserverd" >&2
    echo "[install]        Refusing to overwrite it — NOTHING was changed about that job." >&2
    echo "[install]        The binaries are installed and the database is migrated; either retire" >&2
    echo "[install]        that job yourself, or install under a different label:" >&2
    echo "[install]          OC_LAUNCHD_LABEL=com.officraft.serve.alt ./install.sh --force" >&2
    exit 1
  fi
  echo "[install] existing '$LABEL' job points at our binary — reloading it in place."
elif launchctl print "$TARGET" >/dev/null 2>&1; then
  # Registered with no plist on disk (bootstrapped from elsewhere). Same rule:
  # we cannot prove it is ours, so we do not touch it.
  echo "[install] FATAL: launchd label '$LABEL' is already registered (no plist at $PLIST)." >&2
  echo "[install]        Refusing to overwrite an unidentified job — NOTHING was changed." >&2
  echo "[install]        Install under a different label instead:" >&2
  echo "[install]          OC_LAUNCHD_LABEL=com.officraft.serve.alt ./install.sh --force" >&2
  exit 1
fi

mkdir -p "$LA_DIR" "$LOG_DIR"

# OC_CONFIG is emitted ONLY when a config file actually backed the port probe —
# pointing the daemon at a nonexistent path would be worse than the default.
cfg_entry=""
if [[ -n "$CFG_ABS" ]]; then
  cfg_entry="    <key>OC_CONFIG</key><string>$CFG_ABS</string>
"
fi

cat > "$PLIST.new" <<PLIST_EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>$BIN_DIR/ocserverd</string>
    <string>serve</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key><string>$HOME</string>
$cfg_entry    <key>OC_NO_OPEN_BROWSER</key><string>1</string>
  </dict>
  <key>WorkingDirectory</key><string>$ROOT_DIR</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardOutPath</key><string>$SERVE_LOG</string>
  <key>StandardErrorPath</key><string>$SERVE_LOG</string>
</dict>
</plist>
PLIST_EOF

if ! plutil -lint "$PLIST.new" >/dev/null 2>&1; then
  rm -f "$PLIST.new"
  echo "[install] FATAL: the rendered launchd plist failed plutil -lint — refusing to install it." >&2
  exit 1
fi
mv "$PLIST.new" "$PLIST"

# bootout is ASYNC: the label can linger registered after the call returns, and
# bootstrapping into that window fails with "service already bootstrapped" AND
# can leave the job torn down but not re-registered — i.e. a re-run of the
# installer that ENDS with the server dead. Poll until launchd really lets go.
launchctl bootout "$TARGET" >/dev/null 2>&1 || true
gone=0
for _ in $(seq 1 25); do
  launchctl print "$TARGET" >/dev/null 2>&1 || { gone=1; break; }
  sleep 0.2
done
[[ "$gone" == 1 ]] || echo "[install] WARN: '$LABEL' still registered ~5s after bootout; bootstrapping anyway." >&2

if ! launchctl bootstrap "$GUI" "$PLIST"; then
  echo "[install] FATAL: launchctl bootstrap failed for $PLIST" >&2
  echo "[install]        The binaries are installed and the database is migrated, but the" >&2
  echo "[install]        service is NOT running. Start it in the foreground with:" >&2
  echo "[install]          $BIN_DIR/ocserverd serve" >&2
  exit 1
fi
launchctl kickstart "$TARGET" >/dev/null 2>&1 || true

# Health gate: "bootstrap returned 0" only means launchd accepted the job, not
# that serve bound the port. Wait for the socket before claiming success, so a
# job that crash-loops on startup is reported as a failure here rather than as
# a URL that refuses connections.
up=0
for _ in $(seq 1 50); do
  if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then up=1; break; fi
  sleep 0.2
done
if [[ "$up" != 1 ]]; then
  echo "[install] FATAL: the '$LABEL' service was registered but nothing is listening on port $PORT after 10s." >&2
  echo "[install]        Check the log: $SERVE_LOG" >&2
  echo "[install]        Remove the job with: launchctl bootout $TARGET && rm -f $PLIST" >&2
  exit 1
fi

# On a FRESH install serve logs a one-time setup link carrying the claim code;
# without it the owner cannot claim the server, so the URL we print IS that
# link when one exists. Keeps the closing report to three facts.
URL="http://127.0.0.1:${PORT}/"
claim_url="$(grep -o "http://[^ ]*/?code=[A-Za-z0-9_-]*" "$SERVE_LOG" 2>/dev/null | tail -1 || true)"
[[ -n "$claim_url" ]] && URL="$claim_url"

echo
echo "[install] ✅ OffiCraft is installed and running in the background (launchd job '$LABEL')."
echo
echo "  open:    $URL"
echo "  stop:    launchctl bootout $TARGET"
echo "  remove:  launchctl bootout $TARGET && rm -f $PLIST"
echo
