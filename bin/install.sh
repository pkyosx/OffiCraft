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
#      ./install.sh --relocate # consent to MOVING an existing service to a
#                              # different port/config (see the relocation gate)
#      ./install.sh --restart-live
#                              # consent to RESTARTING a service that is LIVE
#                              # right now (see the live-service gate). NOT
#                              # implied by --force: overwriting files and
#                              # dropping every connected client are different
#                              # acts and take separate consent.
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
#   2. live-service gate  — if the launchd label this run would claim is
#                           ALREADY REGISTERED AND RUNNING, installing means
#                           bootout+bootstrap: a REAL restart that DISCONNECTS
#                           every open cockpit and every connected agent. That
#                           is an outage, so it takes its own consent:
#                           interactive = y/N prompt (default NO);
#                           non-interactive = ABORT (fail-closed); explicit
#                           override = --restart-live. Deliberately NOT covered
#                           by --force, which speaks only about files on disk.
#                           Runs BEFORE anything is written, so declining leaves
#                           the machine byte-for-byte untouched.
#   2b. existing-install gate — if ~/.officraft already carries an install
#                           (binaries or a database), say so LOUDLY and ask
#                           before overwriting. Interactive = y/N prompt
#                           (default NO); non-interactive = abort unless
#                           --force.
#   3. install binaries   — ocserverd/ocwarden/ocagent → ~/.officraft/bin
#                           (ocserverd embeds the SPA + seeds + warden/agent
#                           binaries; ocwarden/ocagent ship alongside for
#                           direct CLI use).
#   4. migrate            — `ocserverd migrate` (goose; creates
#                           ~/.officraft/server/data/officraft.db on first run).
#   5. port gate          — the effective serve port (default 7755) must be
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
#   6b. relocation gate   — replacing a job of OURS must be a reload, not a
#                           move. If this run resolved a different port or a
#                           different config than the job already running, that
#                           is a relocation (usually a different database too)
#                           and it STOPS unless --relocate is given. This
#                           matters because the port probe falls back to
#                           ./oc.toml in the CURRENT DIRECTORY: standing in a
#                           checkout with a stray oc.toml would otherwise be
#                           enough to silently move the owner's live server.
#                           A run that resolves no config of its own INHERITS
#                           the existing job's OC_CONFIG rather than dropping it.
#
# Config: the server reads $OC_CONFIG (or ./oc.toml in CWD) for overrides —
# default port is 7755 (the OffiCraft standard; 8770 belongs to the retired
# open-company station and 8780 was the previous standard), default data root
# ~/.officraft. No config is written
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
      --relocate) FWD+=("--relocate"); shift ;;
      --restart-live) FWD+=("--restart-live"); shift ;;
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

If a launchd OffiCraft service is RUNNING right now, re-installing restarts it
and disconnects every open cockpit and connected agent. That is gated
separately and --force does NOT authorize it; a piped run aborts unless you
pass --restart-live:

  curl -fsSL … | bash -s -- --force --restart-live
EOF
        exit 0 ;;
      *)
        echo "[install] FATAL: unknown argument '$1' (supported: --force, --foreground, --relocate, --restart-live, --tag <vX.Y.Z>)" >&2
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
RELOCATE=0
RESTART_LIVE=0
for arg in "$@"; do
  case "$arg" in
    --force) FORCE=1 ;;
    --foreground) FOREGROUND=1 ;;
    --relocate) RELOCATE=1 ;;
    --restart-live) RESTART_LIVE=1 ;;
    -h|--help)
      sed -n '2,89p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "[install] FATAL: unknown argument '$arg' (supported: --force, --foreground, --relocate, --restart-live)" >&2
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

# ── launchd job identity ─────────────────────────────────────────────────────
# Resolved FIRST, before any gate and before a single byte is written. Two
# things depend on it: the live-service gate immediately below (which must be
# able to abort while the machine is still untouched) and the ownership/port
# gates further down.
#
# NOTE, and this is the whole reason the gate below exists: the label names a
# singleton in the user's launchd GUI DOMAIN, and that domain is keyed on the
# UID — it does NOT follow $HOME. Pointing HOME at a scratch directory relocates
# ROOT_DIR/BIN_DIR/PLIST but leaves TARGET resolving to the SAME job the real
# station runs under. "I ran it with a different HOME" is therefore NOT
# isolation, and a run that believes it is sandboxed can still bootout the live
# service. Only OC_LAUNCHD_LABEL actually changes which job is at stake.
LABEL="${OC_LAUNCHD_LABEL:-com.officraft.serve}"
LA_DIR="$HOME/Library/LaunchAgents"
PLIST="$LA_DIR/$LABEL.plist"
GUI="gui/$(id -u)"
TARGET="$GUI/$LABEL"
LOG_DIR="$ROOT_DIR/server/log"
SERVE_LOG="$LOG_DIR/serve.log"

job_pid_of() {
  # pid of a registered launchd job, or "" when absent/loaded-but-not-running.
  # launchd prints a nested `state = ` for subordinate entries, so anchor on the
  # top-level `pid = ` line and take the first match only.
  #
  # `launchctl print` is run SEPARATELY rather than at the head of the pipe on
  # purpose: an unregistered label exits non-zero, and under `set -o pipefail`
  # that rc propagates out of the command substitution and `set -e` kills the
  # installer — turning "no job is registered", the most ordinary state on a
  # clean machine, into a silent abort with no diagnostic at all.
  local out
  out="$(launchctl print "$1" 2>/dev/null || true)"
  printf '%s\n' "$out" | sed -n 's/^[[:space:]]*pid = \([0-9]*\).*/\1/p' | head -1
}

listening_ports_of() {
  # Space-separated TCP ports a pid holds in LISTEN, or "" when it holds none.
  #
  # Same pipefail trap as job_pid_of, spelled out again because this file has
  # now fallen into it TWICE: real lsof exits NON-ZERO when the pid has no
  # listening socket, and that is an ordinary state — a job still starting up,
  # one that is crash-looping, or one bound only to a unix socket. Left at the
  # head of a pipeline under `set -o pipefail`, that rc propagates out of the
  # command substitution and `set -e` kills the installer RIGHT HERE, before a
  # single one of the warning lines below has run. The operator gets exit 1 and
  # a COMPLETELY BLANK screen — the worst possible outcome for a gate whose
  # entire job is to explain itself. The port list is cosmetic; it must never
  # be able to abort the run.
  local out
  out="$(lsof -nP -iTCP -sTCP:LISTEN -a -p "$1" 2>/dev/null || true)"
  printf '%s\n' "$out" \
    | sed -n 's/.*:\([0-9]\{2,5\}\) (LISTEN).*/\1/p' | sort -u | tr '\n' ' ' | sed 's/ $//'
}

# ── live-service gate ────────────────────────────────────────────────────────
# The gates that already existed here reason about FILES: is there a binary, a
# database, a plist naming our program, a config that would move. None of them
# ask the one question that decides whether re-installing is disruptive — IS A
# SERVER SERVING RIGHT NOW? On the maintainer's own machine every file-based
# gate answers "same paths, same port, same config, this is a plain reload" and
# waves the run through to `launchctl bootout` (line ~640), which drops the
# running process and every client attached to it.
#
# That outage was reachable through a gate whose text explicitly PROMISED it
# would not happen ("keeps serving its old code until its next restart"), and
# through --force, whose documented meaning is only about overwriting files.
# Consent obtained against a wrong description of the harm is not consent, so
# the restart now gets a gate of its own, phrased in the currency the operator
# actually cares about: connected clients.
#
# Ordering is deliberate — this runs BEFORE the binaries are copied and before
# `ocserverd migrate` touches the database, so declining leaves the machine
# byte-for-byte as it was. The ownership gate downstream stays exactly as it is;
# it answers "is this job MINE", which is a different question from "is it BUSY",
# and a hijack of someone else's job must keep failing closed on its own terms.
#
# --foreground is exempt: it never registers or boots out a launchd job. A live
# service still blocks it, but as a port conflict, which is the honest diagnosis
# there.
LIVE_PID=""
if [[ "$FOREGROUND" == 0 ]]; then
  LIVE_PID="$(job_pid_of "$TARGET")"
fi
if [[ -n "$LIVE_PID" ]]; then
  live_ports="$(listening_ports_of "$LIVE_PID")"
  echo "[install] ⚠️  A LIVE OffiCraft service is running on this machine RIGHT NOW."
  echo "[install]      launchd job: $LABEL (pid $LIVE_PID)"
  echo "[install]      plist:       ${PLIST}"
  [[ -n "$live_ports" ]] && echo "[install]      listening on: port(s) $live_ports"
  echo "[install]"
  echo "[install]    Installing REPLACES that job: this script runs 'launchctl bootout'"
  echo "[install]    and bootstraps it again. That is a REAL restart, not a hot swap —"
  echo "[install]    the server goes DOWN and comes back, so every open cockpit tab and"
  echo "[install]    every connected agent is DISCONNECTED for the duration."
  echo "[install]    The port and the database are inherited, so no data is lost; what"
  echo "[install]    you lose is uptime and every session attached to it."
  echo "[install]"
  if [[ "$RESTART_LIVE" == 1 ]]; then
    echo "[install]    --restart-live given — proceeding with the restart."
  elif [[ -t 0 ]]; then
    printf '[install]    Restart the running service? [y/N] '
    read -r reply
    case "$reply" in
      y|Y|yes|YES) echo "[install]    confirmed — continuing." ;;
      *) echo "[install] aborted — the running service was NOT touched and nothing was changed."; exit 1 ;;
    esac
  else
    # Fail CLOSED. `curl … | bash` has no tty, so silence here is not assent —
    # it is the absence of anyone to ask. Never treat it as a yes.
    echo "[install] FATAL: this run would restart the LIVE '$LABEL' service, but nothing is" >&2
    echo "[install]        attached to stdin to confirm it (piped/non-interactive run)." >&2
    echo "[install]        NOTHING was changed — no binaries, no database, no launchd job." >&2
    echo "[install]" >&2
    echo "[install]        Your options:" >&2
    echo "[install]          • re-run it in a terminal to get the confirmation prompt, or" >&2
    echo "[install]          • accept the restart explicitly:" >&2
    echo "[install]              curl -fsSL … | bash -s -- --force --restart-live" >&2
    echo "[install]          • install alongside it instead, leaving the live one running:" >&2
    echo "[install]              OC_LAUNCHD_LABEL=$LABEL.alt ./install.sh --force" >&2
    echo "[install]          • or stop it first, on your own schedule:" >&2
    echo "[install]              launchctl bootout $TARGET" >&2
    echo "[install]        (--force alone does NOT authorize this: it speaks about" >&2
    echo "[install]         overwriting files, not about dropping live connections.)" >&2
    exit 1
  fi
fi

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
  echo "[install]    and migrated in place)."
  # Do NOT reassure the operator that a running server keeps serving until some
  # later restart — on the launchd path this script boots it out a few hundred
  # lines below, so the restart is IMMEDIATE and this prompt used to describe
  # the wrong harm. When a live job exists the live-service gate above has
  # already obtained consent for that outage in the correct terms; say which
  # case applies rather than asserting the comfortable one.
  if [[ -n "$LIVE_PID" ]]; then
    echo "[install]    The live '$LABEL' service will be RESTARTED as part of this run"
    echo "[install]    (you confirmed that above)."
  else
    echo "[install]    No OffiCraft service is currently running, so nothing is interrupted;"
    echo "[install]    the new binaries take effect when the service is next started."
  fi
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

# Effective port: $OC_CONFIG / ./oc.toml [server].port override, else 7755
# (the OffiCraft standard port — NOT 8770, which belongs to the retired
# open-company station and collides on transition-period machines, and NOT the
# previous 8780 standard).
#
# CFG_ABS records WHICH config file that port came from, as an ABSOLUTE path.
# It matters for the launchd path: a relative ./oc.toml is resolved against the
# CWD of whoever ran the installer, and the daemon has a different CWD. Baking
# the absolute path into the plist's OC_CONFIG is what keeps the port the
# installer gated on and the port the daemon binds identical.
#
# CFG_SRC records WHERE it came from — env (explicit OC_CONFIG), cwd (a
# ./oc.toml that merely happened to be in the current directory), inherited
# (carried over from the job we are replacing), or none. "cwd" is the one that
# bites: it is the difference between a config the operator chose and a file
# they happen to be standing next to. See the relocation gate below.
config_port() {
  # [server].port out of a config file, or "" when it does not set one.
  sed -n 's/^[[:space:]]*port[[:space:]]*=[[:space:]]*\([0-9]\{2,5\}\).*/\1/p' "$1" 2>/dev/null | head -1
}
DEFAULT_PORT=7755
PORT="$DEFAULT_PORT"
CFG_ABS=""
CFG_SRC="none"
for cfg_kind in "env:${OC_CONFIG:-}" "cwd:./oc.toml"; do
  cfg="${cfg_kind#*:}"
  if [[ -n "$cfg" && -f "$cfg" ]]; then
    p="$(config_port "$cfg")"
    [[ -n "$p" ]] && PORT="$p"
    CFG_ABS="$(cd "$(dirname "$cfg")" && pwd)/$(basename "$cfg")"
    CFG_SRC="${cfg_kind%%:*}"
    break
  fi
done

# ── same-label ownership gate ────────────────────────────────────────────────
# (LABEL / PLIST / GUI / TARGET / LOG_DIR / SERVE_LOG were resolved at the top,
# ahead of the live-service gate, which has to be able to abort before anything
# is written. They are deliberately still resolved BEFORE the port gate: on a
# re-install the process holding the port is usually OUR OWN previous service,
# and that gate has to tell it apart from a genuine conflict.)
# The label is a machine-wide singleton in the user's gui domain. If something
# is ALREADY registered under it, blindly rendering over the plist and booting
# it out would silently hijack — or kill — a service this installer did not
# create (a hand-built job, or a repo-layout `bin/ocserver install` instance
# that runs serve from a completely different path). So: adopt the label only
# when the existing plist demonstrably points at the binary WE just installed;
# anything else is a hard stop that changes nothing.
#
# OURS=1 additionally licenses the port gate below to treat that job's own
# listener as "not a conflict".
plist_program() {
  # ProgramArguments[0] of an existing plist, or "" if unreadable/absent.
  plutil -extract ProgramArguments.0 raw -o - "$1" 2>/dev/null || true
}
OURS=0
if [[ "$FOREGROUND" == 0 ]]; then
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
    OURS=1
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
fi

# ── relocation gate ──────────────────────────────────────────────────────────
# The ownership gate above only established "this is the same BINARY". What it
# licenses, though, is far larger: bootout the running service and re-register
# it with WHATEVER port and config this run happened to resolve. Those two
# scopes are not the same size, and the gap is a foot-gun with a live trigger.
#
# The trigger is the ./oc.toml fallback. Standing in a directory that happens to
# contain an oc.toml — a repo checkout with a leftover e2e config, say — is
# enough to move the owner's running server to a different port AND point it at
# a different database. The port gate cannot catch it: it checks the NEW port,
# which is free. The result is a service that looks freshly installed and a
# cockpit that looks like all its data vanished, with nothing printed to connect
# the two. Before this installer created launchd jobs the same misread was
# harmless and reversible (it only affected a foreground process you could
# Ctrl-C); bootout + a persistent plist is what turned it into a destructive act.
#
# So when replacing a job of ours, compare the parameters that decide WHERE the
# service lives and WHAT data it serves:
#   - no config of our own + the old job had one  -> INHERIT it (a plain reload
#     must not silently drop the config pointer the running service relies on),
#     and re-derive the port from it so the port gate checks the real port.
#   - port or config would actually CHANGE       -> that is a relocation, not a
#     reload. Hard stop, print the before/after, require --relocate.
plist_env() {
  # EnvironmentVariables.<key> of a plist, or "" when absent.
  plutil -extract "EnvironmentVariables.$2" raw -o - "$1" 2>/dev/null || true
}
if [[ "$OURS" == 1 ]]; then
  OLD_CFG="$(plist_env "$PLIST" OC_CONFIG)"

  if [[ "$CFG_SRC" == "none" && -n "$OLD_CFG" && -f "$OLD_CFG" ]]; then
    CFG_ABS="$OLD_CFG"
    CFG_SRC="inherited"
    p="$(config_port "$CFG_ABS")"
    [[ -n "$p" ]] && PORT="$p"
    echo "[install] carrying over the existing service's config: $CFG_ABS (port $PORT)"
  fi

  # Old port = the old job's own config, else the default it would have bound.
  OLD_PORT="$DEFAULT_PORT"
  if [[ -n "$OLD_CFG" && -f "$OLD_CFG" ]]; then
    p="$(config_port "$OLD_CFG")"
    [[ -n "$p" ]] && OLD_PORT="$p"
  fi

  if [[ "$PORT" != "$OLD_PORT" || "$CFG_ABS" != "$OLD_CFG" ]] && [[ "$RELOCATE" == 1 ]]; then
    echo "[install] --relocate given — MOVING the '$LABEL' service:"
    echo "[install]        port:    $OLD_PORT  ->  $PORT"
    echo "[install]        config:  ${OLD_CFG:-<none>}  ->  ${CFG_ABS:-<none>}"
  elif [[ "$PORT" != "$OLD_PORT" || "$CFG_ABS" != "$OLD_CFG" ]]; then
    echo "[install] FATAL: this would MOVE the running '$LABEL' service, not just reload it." >&2
    echo "[install]        port:    $OLD_PORT  ->  $PORT" >&2
    echo "[install]        config:  ${OLD_CFG:-<none>}  ->  ${CFG_ABS:-<none>}" >&2
    echo "[install]        A different config usually means a different database, so the" >&2
    echo "[install]        server would come back up looking empty. NOTHING was changed." >&2
    if [[ "$CFG_SRC" == "cwd" ]]; then
      echo "[install]        This came from ./oc.toml in your CURRENT DIRECTORY:" >&2
      echo "[install]          $CFG_ABS" >&2
      echo "[install]        If you did not mean to move your service, cd elsewhere and re-run." >&2
    fi
    echo "[install]        If you really do want to move it, re-run with --relocate." >&2
    exit 1
  fi
fi

# ── port gate ────────────────────────────────────────────────────────────────
# Serving on a taken port would just die with EADDRINUSE after a seemingly
# successful install — check first and fail with an actionable message.
#
# EXCEPT when the listener is the service we are about to replace. Re-running
# the installer over a healthy install is supposed to be idempotent, but our own
# running job holds the port, so a naive gate turns every second run into a
# FATAL — an install that "already worked" reported as broken. When every
# listener PID belongs to our own launchd job the port is not contended: we
# boot that job out further down and the port is released before the new one
# binds. A foreign squatter — or ANY listener in --foreground mode, where
# nothing gets booted out — still fails exactly as before.
if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  port_conflict=1
  if [[ "$OURS" == 1 ]]; then
    job_pid="$(job_pid_of "$TARGET")"
    if [[ -n "$job_pid" ]]; then
      foreign="$(lsof -nP -iTCP:"$PORT" -sTCP:LISTEN -t 2>/dev/null | grep -vx "$job_pid" || true)"
      if [[ -z "$foreign" ]]; then
        port_conflict=0
        echo "[install] port $PORT is held by our own '$LABEL' job (pid $job_pid) — it will be replaced."
      fi
    fi
  fi
  if [[ "$port_conflict" == 1 ]]; then
    echo "[install] FATAL: port $PORT is already in use on this machine:" >&2
    lsof -nP -iTCP:"$PORT" -sTCP:LISTEN 2>/dev/null | sed 's/^/[install]   /' >&2 || true
    echo "[install] The binaries are installed and the database is migrated, but the server was" >&2
    echo "[install] NOT started. Pick a free port by writing an oc.toml next to where you run serve:" >&2
    echo "[install]   printf '[server]\\nport = <free-port>\\n' > oc.toml" >&2
    echo "[install]   $BIN_DIR/ocserverd serve" >&2
    exit 1
  fi
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
# Identity and the same-label ownership gate were resolved above the port gate.
mkdir -p "$LA_DIR" "$LOG_DIR"

# OC_CONFIG is emitted ONLY when a config file actually backed the port probe —
# pointing the daemon at a nonexistent path would be worse than the default.
cfg_entry=""
if [[ -n "$CFG_ABS" ]]; then
  cfg_entry="    <key>OC_CONFIG</key><string>$CFG_ABS</string>
"
fi

# Rendered to a sibling temp file first, so a write failure (read-only
# LaunchAgents, full disk) cannot leave a half-written plist where a valid one
# used to be. The explicit guard is what keeps this path speaking the same
# language as every other failure here — without it `set -e` aborts on a raw
# shell redirection error and the operator gets a bare "Permission denied".
if ! cat 2>/dev/null > "$PLIST.new" <<PLIST_EOF
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
then
  rm -f "$PLIST.new" 2>/dev/null || true
  echo "[install] FATAL: could not write the launchd plist: $PLIST.new" >&2
  echo "[install]        Check that $LA_DIR exists and is writable." >&2
  echo "[install]        The binaries are installed and the database is migrated, but no" >&2
  echo "[install]        service was registered and no existing job was touched." >&2
  exit 1
fi

if ! plutil -lint "$PLIST.new" >/dev/null 2>&1; then
  rm -f "$PLIST.new"
  echo "[install] FATAL: the rendered launchd plist failed plutil -lint — refusing to install it." >&2
  exit 1
fi
if ! mv "$PLIST.new" "$PLIST"; then
  rm -f "$PLIST.new" 2>/dev/null || true
  echo "[install] FATAL: could not install the launchd plist to $PLIST" >&2
  echo "[install]        No service was registered and no existing job was touched." >&2
  exit 1
fi

# bootout is ASYNC: the label can linger registered after the call returns, and
# bootstrapping into that window fails with "service already bootstrapped" AND
# can leave the job torn down but not re-registered — i.e. a re-run of the
# installer that ENDS with the server dead. Poll until launchd really lets go.

# Where the serve log ends RIGHT NOW, before this boot appends anything. The
# claim link is scraped from the bytes after this mark and nowhere else:
# StandardOutPath APPENDS, so every previous boot's setup link is still sitting
# in the file. Scanning the whole log means a re-install happily reprints the
# claim code from the very first install — long since consumed, so the link is
# dead — and tells the owner to go set a password they already set.
log_mark=0
[[ -f "$SERVE_LOG" ]] && log_mark="$(wc -c < "$SERVE_LOG" | tr -d ' ')"

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
# link when one exists. Keeps the closing report to three facts. Only THIS
# boot's output is searched (see log_mark) — an already-claimed server logs no
# new link, and then the plain URL is the honest thing to print.
URL="http://127.0.0.1:${PORT}/"
claim_url="$(tail -c "+$((log_mark + 1))" "$SERVE_LOG" 2>/dev/null \
  | grep -o "http://[^ ]*/?code=[A-Za-z0-9_-]*" | tail -1 || true)"
[[ -n "$claim_url" ]] && URL="$claim_url"

echo
echo "[install] ✅ OffiCraft is installed and running in the background (launchd job '$LABEL')."
echo
echo "  open:    $URL"
echo "  stop:    launchctl bootout $TARGET"
echo "  remove:  launchctl bootout $TARGET && rm -f $PLIST"
echo
