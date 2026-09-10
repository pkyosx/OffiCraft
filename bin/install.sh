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
#                              # right now — see the live-service gate below.
#                              # NOT implied by --force.
#      ./install.sh --namespace <ns> --port <port>
#                              # a SECOND, fully isolated instance: one key
#                              # derives root, label, config, db. --port is
#                              # REQUIRED; empty namespace = main, unchanged.
#                              # THE ONLY real isolation here (a redirected HOME
#                              # is not one — the launchd label is a uid-keyed
#                              # singleton). See docs/guide/install.md.
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
#   1. preflight          — darwin/arm64, tmux, claude OR codex — or STOP untouched.
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
#   3. install binaries   — ocserverd/ocwarden/ocagent/officraft → ~/.officraft/bin
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
# default port 7755, default data root ~/.officraft. No config is written by
# this installer; convention defaults just work on a clean machine.
#
#   C. removal — the reverse of A/B, recognised BEFORE any mode detection or
#      download so it never fetches a release just to delete one:
#      curl -fsSL … | bash -s -- --uninstall            # stop + move to a backup
#      curl -fsSL … | bash -s -- --uninstall --dry-run  # print only, nothing changes
#      curl -fsSL … | bash -s -- --uninstall --purge --yes   # DELETE, no way back
#      curl -fsSL … | bash -s -- --uninstall --namespace <ns>  # that one only
#      (targets ~/.officraft-<ns> + com.officraft.serve.<ns>; without it the
#      MAIN instance, as before.)
#      Default keeps the database — it MOVES ~/.officraft's release-path pieces
#      to ~/.officraft.bak-<timestamp> (plist inside it, under launchd/) rather
#      than deleting them, and prints a restore command that puts back both the
#      files and the launchd registration. --purge deletes instead.
#      --purge asks you to type "purge" unless --yes — but over `curl … | bash`
#      that prompt reads the pipe carrying this script, so it cannot be typed
#      into and always aborts: over a pipe, --purge does nothing without --yes.
#      Full mechanism: `--uninstall --help`, and the note at the confirmation.
#      Ownership is decided from ~/.officraft/bin/ocserverd (this installer's own
#      signature — a from-source `bin/ocserver install` never writes there) and
#      from the launchd plist's ProgramArguments[0] (OC_LAUNCHD_LABEL is
#      respected); a label claimed by a different program is refused, loudly,
#      untouched.
#
#      SCOPE — removal touches ONLY what this installer created: bin/, the
#      release-path pieces of server/ (data, oc.toml, log), and the launchd
#      job. Everything else under ~/.officraft is created at RUNTIME by other
#      programs and is LEFT IN PLACE, by name: agents/ (every agent workspace
#      on this machine) and warden/ (plus its own com.officraft.ocwarden job,
#      which this installer never registered and therefore never removes —
#      `~/.officraft/warden/ocwarden teardown --canonical` is that subsystem's
#      own removal path; the absolute path matters because bin/ moves into the
#      backup and this installer never puts it on PATH, and --canonical matters
#      because teardown refuses an IMPLICIT target (T-2257)). A from-source
#      install sharing the same ~/.officraft/server root (visible as
#      server/repo/) is likewise never touched. This is not a courtesy: an
#      earlier version moved the WHOLE of ~/.officraft aside while printing
#      "nothing was deleted", which silently took every agent workspace and the
#      warden with it (T-fa39).
set -euo pipefail

# ── `curl … | bash` leaves the download half writing into a pipe nobody reads ─
# Every early exit in this file (--help, "already clean", a refused gate, the
# whole --uninstall path) USED TO end the script while curl still had bytes in
# flight. The reading end of the pipe then closed under it, so curl's next write
# failed and it printed
#   curl: (23|56) Failure writing output to destination, passed N returned 0
# to stderr — AFTER our own output, so a completely successful run ended on a red
# line that read like a failure.
#
# ⚠️ THE PRIMARY CURE IS NO LONGER THIS DRAIN — it is the oc_main() wrap further
# down (T-4358): the body is one function the LAST line calls, and nothing before
# it prints or exits, so the file is delivered in full before anything is
# observable. Measured with the drain forced inert, a real 150,000 B curl|bash at
# 5 KB/s still delivers 149,999/149,999 with curl rc=0 — this drain contributes
# nothing to that path any more. It is kept as a second line of defence for a
# writer that sends MORE than the script, and because retiring it is a larger
# change than T-4358's scope (bin/tests/stdin-drain-guard.sh cases 6/6b pin its
# trap shape). Structural guard: bin/tests/curl-bash-read-before-execute-guard.sh.
#
# Three guards, and the reason for each:
#   1. Only when the SCRIPT ITSELF arrived on stdin (`curl … | bash`, `bash -s`).
#      The flag is computed HERE, at top level, because what BASH_SOURCE[0] reads
#      as inside a function is VERSION-DEPENDENT: measured, stock macOS
#      /bin/bash 3.2.57 leaves it empty there, while bash 5.x reports a non-empty
#      value (it is $0). Measured consequence on bash 5.x: re-deriving the flag
#      inside the drain means it is never set and the drain becomes DEAD CODE
#      that still looks installed. What that move does on 3.2.57 depends on how
#      the script was invoked and is deliberately NOT asserted — a previous draft
#      claimed "always-on" there and review falsified it. Computing at top level
#      sidesteps the question.
#   2. Only when fd 0 is a PIPE. `bash install.sh`, `bash -s < file`, and an
#      interactive terminal all fall out here, so this can never sit waiting for
#      a human to type — the failure mode that has no way to report itself.
#   3. BOUNDED TWO WAYS. `read -t 5` is an IDLE timeout, not a budget: it
#      restarts on every line, so a writer that dribbles one line every 3s keeps
#      it alive forever (measured: a 30s dribbler held it 36.5s — the very hang
#      an unbounded `cat >/dev/null` was rejected for). So there is also a TOTAL
#      deadline. Giving up either way is not a regression: it just means curl
#      prints the same line it prints today. A cosmetic wart is never worth a
#      hang.
#
# The exit status of the whole script flows THROUGH this handler: a trap whose
# last statement is non-zero rewrites it (measured: `trap` returning 3 turns
# `exit 7` into 3), and an `exit` inside it would flatten every failure to that
# code. Hence the unconditional `return 0`, and the test that a FAILING piped
# run keeps its status.
_OC_SCRIPT_FROM_STDIN=0
[[ -z "${BASH_SOURCE[0]:-}" ]] && _OC_SCRIPT_FROM_STDIN=1
_OC_DRAIN_IDLE_TIMEOUT=5
_OC_DRAIN_TOTAL_TIMEOUT=10

oc_drain_stdin() {
  [[ "$_OC_SCRIPT_FROM_STDIN" == "1" ]] || return 0
  [[ -p /dev/stdin ]] || return 0
  local _discard _deadline=$((SECONDS + _OC_DRAIN_TOTAL_TIMEOUT))
  # `if`, not `(( … )) && break`. Two comments before this one tried to explain
  # WHY in general terms and both were falsified by measurement, so this one
  # states only what was measured, on this machine's /bin/bash 3.2.57:
  #   - `set -euo pipefail; printf … | while read; do (( 0 )) && break; done`
  #     exits 1. The AND-list is the loop body's last command and its false
  #     branch is a non-zero status.
  #   - `set -e` is NOT suspended inside a trap handler: a `false` mid-handler
  #     truncates the handler and rewrites the script's status 0 → 1.
  # `if` produces no non-zero status in either place, so it needs no argument
  # about which one applies here — which is the point.
  while IFS= read -r -t "$_OC_DRAIN_IDLE_TIMEOUT" _discard; do
    if (( SECONDS >= _deadline )); then break; fi
  done
  return 0
}
# Non-enumerative on purpose: an EXIT trap covers every exit in this file,
# including ones added after this comment. A list of "drain here, and here" call
# sites would be zero-coverage for whatever the next person adds.
trap oc_drain_stdin EXIT

# ── plutil readers ───────────────────────────────────────────────────────────
# ONE reader, because both callers had the SAME defect and only one of them was
# fixed. rc decides, not output: on macOS 15 plutil prints its "no value at that
# key path" error on STDOUT (macOS 26 puts it on stderr), so `2>/dev/null || true`
# hands that error TEXT back as if it were the value. T-5831 fixed plist_env this
# way and left plist_program on the old shape, which then printed a plutil error
# line in the field where the operator is told to read a program path.
plist_raw() {
  local v; v="$(plutil -extract "$2" raw -o - "$1" 2>/dev/null)" || return 0
  printf '%s' "$v"
}
# ProgramArguments[0] of an existing plist, or "" if unreadable/absent. Shared by
# --uninstall's ownership check below and the install-time ownership gate further
# down (package mode) — one definition, same semantics both places.
plist_program() { plist_raw "$1" ProgramArguments.0; }
# EnvironmentVariables.<key> of a plist, or "" when absent. Used by the
# relocation gate far below.
plist_env() { plist_raw "$1" "EnvironmentVariables.$2"; }

# ── --uninstall: reverse of this installer, never needs a download ──────────
# See mode C in the header above for the flag shapes. Called from the top-level
# dispatch below, before IN_PACKAGE is even resolved — uninstalling touches only
# what a PAST run of this installer left behind, so it has no use for a tarball.
cmd_uninstall_release() {
  local purge=0 dryrun=0 yes=0 ns="${OC_NAMESPACE:-}"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --uninstall) ;; # matched here (in ANY position), not shifted off separately —
                       # a blind leading shift previously assumed --uninstall was
                       # always $1, which silently ate whatever flag WAS first
                       # (e.g. `--dry-run --uninstall` lost --dry-run and performed
                       # a real deletion)
      --purge)     purge=1 ;;
      --dry-run)   dryrun=1 ;;
      --yes)       yes=1 ;;
      # Removal is the exact inverse of install (same key → root + label), or
      # --namespace would be a one-way door.
      --namespace)   shift; ns="${1:-}" ;;
      --namespace=*) ns="${1#*=}" ;;
      -h|--help)
        cat <<'EOF'
Removes an OffiCraft install created by THIS installer (curl | bash, or a
package-mode ./install.sh run).

  curl -fsSL … | bash -s -- --uninstall              # stop + move to a backup
  curl -fsSL … | bash -s -- --uninstall --dry-run    # print only, change nothing
  curl -fsSL … | bash -s -- --uninstall --purge      # DELETE (asks unless --yes)
  curl -fsSL … | bash -s -- --uninstall --namespace <ns>   # one namespaced one

--namespace <ns> ([a-z0-9-]{1,16}, or OC_NAMESPACE) targets ~/.officraft-<ns> and
com.officraft.serve.<ns>, nothing else. Omitted = the main instance.

WHAT IT TOUCHES — only what this installer itself created:
  ~/.officraft/bin/                        the WHOLE directory, not just the
                                           three binaries it put there — if you
                                           keep anything else in bin/ (old
                                           backups, rollback copies), it goes too
  ~/.officraft/server/data, oc.toml, log   (the database lives in data/)
  the launchd job + plist for OC_LAUNCHD_LABEL (default com.officraft.serve)

WHAT IT LEAVES ALONE — created at runtime by other programs, never by this
installer, so removal has no business moving them:
  ~/.officraft/agents/   EVERY agent workspace on this machine
  ~/.officraft/warden/   plus its own com.officraft.ocwarden launchd job,
                         which stays registered — remove it with
                         `ocwarden teardown --canonical` (namespaced:
                         `OC_NAMESPACE=<ns> ocwarden teardown`), not with
                         this script
  ~/.officraft/server/repo/  a from-source `bin/ocserver install` sharing
                             this root
  ~/.officraft itself is never removed.

Default: stops the launchd job, MOVES the pieces listed above to
~/.officraft.bak-<timestamp> (the plist goes inside it, under launchd/) and
prints a restore command that puts back both the files AND the launchd
registration — the database is kept, just relocated, not deleted.

--purge deletes them instead (no backup, no way back). It asks you to type
"purge" to confirm unless --yes is given — BUT note that the confirmation reads
stdin, which over `curl … | bash` is the pipe carrying this script, so typing
cannot reach it and the gate always aborts. Over a pipe, --purge therefore does
nothing unless you also pass --yes. Save the script to a file and run it if you
want the interactive confirmation.

Respects OC_LAUNCHD_LABEL if the install used a non-default label. Refuses,
loudly and without changing anything, if that label belongs to a different
program than the one this installer would have put there.
EOF
        exit 0 ;;
      *)
        echo "[install] FATAL: unknown --uninstall flag '$1' (supported: --purge, --dry-run, --yes, --namespace <ns>)" >&2
        exit 2 ;;
    esac
    shift
  done

  local ns_dot="" ns_dash=""
  if [[ -n "$ns" ]]; then
    if ! printf '%s' "$ns" | grep -qE '^[a-z0-9-]{1,16}$'; then
      echo "[install] FATAL: --namespace must match [a-z0-9-]{1,16}, got: '$ns'" >&2
      echo "[install]        Refusing to fall back to the MAIN instance's root/label." >&2
      exit 2
    fi
    ns_dot=".$ns"; ns_dash="-$ns"
  fi

  local LABEL LA_DIR PLIST GUI TARGET ROOT_DIR BIN_DIR SERVER_DIR
  LABEL="${OC_LAUNCHD_LABEL:-com.officraft.serve$ns_dot}"
  LA_DIR="$HOME/Library/LaunchAgents"
  PLIST="$LA_DIR/$LABEL.plist"
  GUI="gui/$(id -u)"
  TARGET="$GUI/$LABEL"
  ROOT_DIR="$HOME/.officraft$ns_dash"
  BIN_DIR="$ROOT_DIR/bin"
  SERVER_DIR="$ROOT_DIR/server"

  # run: perform a mutating command, or in dry-run just print it. Read-only
  # probes for control flow do NOT go through it.
  run() {
    if [[ "$dryrun" == "1" ]]; then
      echo "[install] DRYRUN would run: $*" >&2
    else
      "$@"
    fi
  }

  # ── ownership ──────────────────────────────────────────────────────────────
  # "ours" for FILES is decided from the binary, not the label: a from-source
  # `bin/ocserver install` never writes ~/.officraft/bin, so its presence here is
  # this installer's own signature whichever label ended up owning the launchd
  # job. "ours" for the JOB is the same ProgramArguments[0] check install uses at
  # install time — a label pointing at a different program is a different install
  # (or someone else's job entirely) and must not be touched, matching
  # bin/ocserver's `OC_LAUNCHD_LABEL` respect for alt-labelled second installs.
  local have_files=0
  [[ -x "$BIN_DIR/ocserverd" ]] && have_files=1

  local job_pid="" plist_ours=0 plist_foreign=0
  if [[ -f "$PLIST" ]]; then
    if [[ "$(plist_program "$PLIST")" == "$BIN_DIR/ocserverd" ]]; then
      plist_ours=1
      # `|| true` is LOAD-BEARING: under `set -euo pipefail`, `launchctl print`
      # exits NON-ZERO for the ordinary "plist on disk, job not loaded" state, so
      # pipefail+set -e killed the run and --uninstall exited 1 printing NOTHING.
      # No pid is an ANSWER ("not running"), not an error.
      job_pid="$(launchctl print "$TARGET" 2>/dev/null | sed -n 's/^[[:space:]]*pid = \([0-9]*\).*/\1/p' | head -1 || true)"
    else
      plist_foreign=1
    fi
  fi

  if [[ "$have_files" == 0 && "$plist_ours" == 0 ]]; then
    if [[ "$plist_foreign" == 1 ]]; then
      echo "[install] nothing OF OURS to remove: no $BIN_DIR/ocserverd, and launchd label '$LABEL' belongs to a different program ($PLIST) — not touching it." >&2
      echo "[install]   If that is actually your OffiCraft install under a different label, point OC_LAUNCHD_LABEL at it and re-run." >&2
      exit 0
    fi
    echo "[install] nothing to remove — no $BIN_DIR/ocserverd and no '$LABEL' launchd job. Already clean."
    exit 0
  fi

  if [[ "$plist_foreign" == 1 ]]; then
    echo "[install] FATAL: launchd label '$LABEL' is registered but points at a DIFFERENT program:" >&2
    echo "[install]        plist:    $PLIST" >&2
    local prog; prog="$(plist_program "$PLIST")"
    echo "[install]        program:  ${prog:-<unreadable>}" >&2
    echo "[install]        expected: $BIN_DIR/ocserverd" >&2
    echo "[install]        Refusing to touch a job this installer did not create, and refusing to remove" >&2
    echo "[install]        $ROOT_DIR since ownership of this machine's install is now ambiguous. NOTHING was changed." >&2
    echo "[install]        If this label is actually yours under a different program on purpose, retarget with:" >&2
    echo "[install]          OC_LAUNCHD_LABEL=<the label THIS install used> curl -fsSL … | bash -s -- --uninstall" >&2
    exit 1
  fi

  # server/repo/ only ever exists under a from-source `bin/ocserver install` —
  # this installer never creates it. If present, the two installs share this
  # root; remove only what is release-path OURS (bin/ + server/{data,oc.toml,log}),
  # leave repo/ (and anything else under $SERVER_DIR) alone.
  local coexists_source=0
  [[ -d "$SERVER_DIR/repo" ]] && coexists_source=1

  echo "[install] resolved: label=$LABEL root=$ROOT_DIR purge=$purge dryrun=$dryrun"
  if [[ "$coexists_source" == 1 ]]; then
    echo "[install] NOTE: $SERVER_DIR/repo exists — a from-source install shares this root. Leaving repo/ (and anything else under $SERVER_DIR that isn't ours) untouched."
  fi

  # ── what we are NOT touching ───────────────────────────────────────────────
  # $ROOT_DIR holds runtime state this installer never created — agents/ (one
  # workspace per agent ever run here) and warden/ (a separate daemon with its
  # OWN launchd job). A previous version moved the whole root aside while
  # printing "nothing was deleted", so the blast radius silently covered both.
  # We enumerate what stays, BY NAME, so the message is sufficient to predict
  # the outcome (T-fa39).
  local kept=() base
  shopt -s nullglob dotglob
  local top
  for top in "$ROOT_DIR"/*; do
    base="$(basename "$top")"
    case "$base" in
      bin|server) ;; # ours (server/ only partially — see the repo/ note above)
      *) kept+=("$base") ;;
    esac
  done
  shopt -u nullglob dotglob

  # -H so that an agents/ that is itself a symlink is followed: `find` does not
  # descend the argument's own symlink otherwise, so this would report 0 for a
  # directory holding dozens of workspaces — under-reporting in exactly the
  # place the whole disclosure hangs on. `|| agent_count=unknown` because the
  # pipeline returns non-zero when agents/ is unreadable, and under
  # `set -euo pipefail` that would kill the script here with no output.
  local agent_count=0
  if [[ -d "$ROOT_DIR/agents" ]]; then
    agent_count="$(find -H "$ROOT_DIR/agents" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')" || agent_count="unknown"
    [[ -n "$agent_count" ]] || agent_count="unknown"
  fi

  # Announce what is actually PRESENT, not the full menu: on a half-installed
  # machine a fixed list names things that are not there — the same over-claiming
  # the "what moved" line below was fixed for. Both ends describe one reality.
  local will=() ent
  [[ -e "$BIN_DIR" ]] && will+=("$BIN_DIR (the whole directory)")
  for ent in data oc.toml log; do
    [[ -e "$SERVER_DIR/$ent" ]] && will+=("$SERVER_DIR/$ent")
  done
  [[ -f "$PLIST" ]] && will+=("launchd job $LABEL and its plist")
  if [[ "${#will[@]}" -gt 0 ]]; then
    echo "[install] will touch:"
    for ent in "${will[@]}"; do echo "[install]     $ent"; done
  else
    echo "[install] will touch: nothing — none of this installer's files or its plist are present"
  fi
  if [[ "${#kept[@]}" -gt 0 ]]; then
    echo "[install] will NOT touch, under $ROOT_DIR — left exactly where they are:"
    # One per line: joining with "${kept[*]}" renders a name containing a space
    # or newline as if it were several entries.
    for base in "${kept[@]}"; do echo "[install]     $base"; done
    if [[ -d "$ROOT_DIR/agents" ]]; then
      echo "[install]   agents/ holds $agent_count agent workspace(s) on this machine — this script never removes them"
    fi
    if [[ -d "$ROOT_DIR/warden" ]]; then
      # Probe the warden's job rather than asserting it: claiming "still
      # registered and RUNNING" without looking is this ticket's own defect
      # class (saying more than was measured).
      local warden_label="com.officraft.ocwarden$ns_dot"
      if [[ -f "$LA_DIR/$warden_label.plist" ]]; then
        echo "[install]   warden/ belongs to the ocwarden daemon; its own launchd job ($warden_label) is"
        echo "[install]   registered and this script leaves it that way."
      else
        echo "[install]   warden/ belongs to the ocwarden daemon; this script never touches it."
        echo "[install]   (no $warden_label job is registered for this user right now)"
      fi
      # Absolute path on purpose: bin/ is about to move into the backup and this
      # installer never puts ~/.officraft/bin on PATH, so bare `ocwarden` would be
      # command-not-found. The warden installs its own stable copy here.
      # The hint must be RUNNABLE (T-2257). `ocwarden teardown` now fails closed
      # on an IMPLICIT target: bare, it refuses rather than resolve the canonical
      # warden. So the canonical branch has to spell --canonical, and the
      # namespaced branch must NOT (the flag and OC_NAMESPACE are mutually
      # exclusive — passing both is also a refusal).
      local ns_env="" ns_flag=" --canonical"
      if [[ -n "$ns" ]]; then ns_env="OC_NAMESPACE=$ns "; ns_flag=""; fi
      echo "[install]   to remove the warden too, afterwards: $ns_env$ROOT_DIR/warden/ocwarden teardown$ns_flag"
    fi
  fi
  echo "[install] $ROOT_DIR itself is never removed."

  if [[ "$purge" == "1" && "$dryrun" != "1" && "$yes" != "1" ]]; then
    echo "[install] --purge will DELETE $BIN_DIR and the release-path parts of $SERVER_DIR, INCLUDING the database (your owner credential hash lives there). Irreversible." >&2
    # KNOWN LIMITATION, also stated out loud in --help and the header rather than
    # papered over: this reads fd 0, and under the documented
    # `curl … | bash -s -- --uninstall --purge` shape fd 0 is the pipe carrying
    # THIS SCRIPT. bash reads a non-seekable script line by line, so `read`
    # consumes the script's NEXT LINE, never anything you type and never
    # "purge" — over a pipe this gate ALWAYS aborts and --purge is unreachable
    # without --yes.
    #
    # That is fail-closed and audible (the abort message below), which is why it
    # is deliberately NOT "fixed" here. Reading /dev/tty would make this
    # destructive path genuinely reachable for the first time, and this suite
    # cannot drive a controlling terminal — the newly-reachable branch would ship
    # with zero coverage, and an earlier revision of this very change also hung
    # `bin/ci.sh` forever on any machine that had a tty. Tracked separately;
    # doing it right needs pty-based tests, its own change.
    #
    # `|| answer=""` matters: on EOF (`./install.sh --uninstall --purge </dev/null`)
    # a bare `read` returns non-zero and `set -e` would kill the script right
    # here with NO output at all. Falling through to the abort message keeps the
    # refusal audible — silence is the failure mode this ticket exists to remove.
    local answer=""
    printf '[install] type "purge" to confirm: ' >&2
    read -r answer || answer=""
    [[ "$answer" == "purge" ]] || { echo "[install] aborted — nothing was changed." >&2; exit 1; }
  fi

  # 1. launchd job: stop it (if running) and take the plist out of the way.
  #    On the default (move) path the plist is MOVED into the backup rather than
  #    deleted: `rm`-ing it made the printed restore command a lie, since putting
  #    the files back could not re-register a service whose plist no longer
  #    existed (T-fa39). --purge deletes it — that path promises no way back.
  #    The plist is kept INSIDE the backup (launchd/ subdir), not beside it as
  #    "$backup.plist": that sibling name matches the same ".officraft.bak-*"
  #    glob the backup dir uses, so anything globbing backups would see two
  #    where there is one.
  local backup="" plist_backup=""
  if [[ "$purge" != "1" ]]; then
    backup="$ROOT_DIR.bak-$(date +%Y%m%d%H%M%S)"
    plist_backup="$backup/launchd/$LABEL.plist"
  fi

  if [[ -n "$job_pid" ]]; then
    echo "[install] stopping launchd job $LABEL (pid $job_pid)"
    run launchctl bootout "$TARGET"
  elif [[ "$plist_ours" == "1" ]]; then
    if [[ "$purge" == "1" ]]; then
      echo "[install] launchd job $LABEL is registered but not running — removing its plist"
    else
      echo "[install] launchd job $LABEL is registered but not running — moving its plist into the backup"
    fi
  fi
  local plist_saved=0
  if [[ -f "$PLIST" ]]; then
    if [[ "$purge" == "1" ]]; then
      run rm -f "$PLIST"
    else
      run mkdir -p "$backup/launchd"
      run mv "$PLIST" "$plist_backup"
      plist_saved=1
    fi
  fi

  # 2. files: bin/ + server/{data,oc.toml,log}. NEVER server/repo, NEVER
  #    agents/, NEVER warden/, and never $ROOT_DIR itself. There is deliberately
  #    no "and if the root looks empty, remove it too" branch any more: that let
  #    the blast radius depend on what happened to be lying around, and it also
  #    under-reported itself under --dry-run (the preceding delete had only been
  #    printed, so the root never looked empty and the rm -rf was silently
  #    omitted from the preview). Same list, both modes.
  local entry
  if [[ "$purge" == "1" ]]; then
    [[ -e "$BIN_DIR" ]] && run rm -rf "$BIN_DIR"
    for entry in data oc.toml log; do
      [[ -e "$SERVER_DIR/$entry" ]] && run rm -rf "$SERVER_DIR/$entry"
    done
    echo "[install] purge complete — $BIN_DIR and this release's server data are gone."
    if [[ "${#kept[@]}" -gt 0 ]]; then
      echo "[install]   $ROOT_DIR was NOT removed — it still holds:"
      for base in "${kept[@]}"; do echo "[install]     $base"; done
    else
      echo "[install]   $ROOT_DIR was NOT removed."
    fi
  else
    # Record what ACTUALLY moved rather than announcing a fixed list (the same
    # rule as the "will touch" list above, pointed the other way): on a
    # partially-installed machine — plist ours, bin/ or server/ already gone by
    # hand — the fixed wording claimed moves that never happened.
    local moved=() restore_files=()
    if [[ -e "$BIN_DIR" ]]; then
      run mkdir -p "$backup"
      run mv "$BIN_DIR" "$backup/bin"
      moved+=("bin/")
      restore_files+=("cp -R \"$backup/bin\" \"$ROOT_DIR/\"")
    fi
    local moved_server=0
    for entry in data oc.toml log; do
      if [[ -e "$SERVER_DIR/$entry" ]]; then
        run mkdir -p "$backup/server"
        run mv "$SERVER_DIR/$entry" "$backup/server/$entry"
        moved+=("server/$entry")
        moved_server=1
      fi
    done
    [[ "$moved_server" == "1" ]] && restore_files+=("mkdir -p \"$SERVER_DIR\"" "cp -R \"$backup/server/.\" \"$SERVER_DIR/\"")

    if [[ "${#moved[@]}" -gt 0 || "$plist_saved" == "1" ]]; then
      echo "[install] moved to $backup — the service is stopped but nothing was deleted."
      if [[ "${#moved[@]}" -gt 0 ]]; then
        echo "[install]   what moved: ${moved[*]}"
      else
        echo "[install]   what moved: the launchd plist only — no installed files were present."
      fi
    else
      echo "[install] nothing needed moving — no installed files and no plist of ours were present."
    fi
    if [[ "${#kept[@]}" -gt 0 ]]; then
      echo "[install]   what stayed in $ROOT_DIR (untouched, still in place):"
      for base in "${kept[@]}"; do echo "[install]     $base"; done
    fi

    # The restore line has to put back BOTH halves — the files AND the launchd
    # registration — or it hands you a way back that does not work. It is
    # assembled from what was really moved: a fixed line referencing
    # "$backup/bin" breaks its own `&&` chain on the first cp when bin/ was never
    # there, and the plist half then never runs at all.
    [[ "$plist_saved" == "1" ]] && restore_files+=("cp \"$plist_backup\" \"$PLIST\"" "launchctl bootstrap \"$GUI\" \"$PLIST\"")
    if [[ "${#restore_files[@]}" -gt 0 ]]; then
      local restore_cmd="${restore_files[0]}" i
      for ((i = 1; i < ${#restore_files[@]}; i++)); do restore_cmd+=" && ${restore_files[$i]}"; done
      echo "[install]   restore: $restore_cmd"
      # cp overwrites. Restoring ON TOP of a fresh install would silently put the
      # old database back over the new one, so say so instead of letting someone
      # find out afterwards.
      echo "[install]   (only run that if you have NOT reinstalled since — it overwrites, it does not merge)"
    fi
  fi

  if [[ "$dryrun" == "1" ]]; then
    echo "[install] DRYRUN complete — nothing on the machine was changed."
  fi
}

# ── mode detection ───────────────────────────────────────────────────────────
# Package mode = this file sits next to the three release binaries (unpacked
# tarball). Anything else — including `curl … | bash`, where BASH_SOURCE is empty
# because the script comes from stdin — is standalone bootstrap mode.
# TOP LEVEL, and it stays there for the same reason the from-stdin flag near the
# top of this file does: this is the OTHER read of BASH_SOURCE[0], and what that
# reads as inside a function is version-dependent. Everything below uses $SELF
# rather than re-reading it. Pure computation — no output, no exit — so it cannot
# break the read-before-execute property oc_main exists for.
SELF="${BASH_SOURCE[0]:-}"
IN_PACKAGE=0
if [[ -n "$SELF" && -f "$SELF" ]]; then
  SELF_DIR="$(cd "$(dirname "$SELF")" && pwd)"
  if [[ -f "$SELF_DIR/ocserverd" && -f "$SELF_DIR/ocwarden" && -f "$SELF_DIR/ocagent" && -f "$SELF_DIR/officraft" ]]; then
    IN_PACKAGE=1
  fi
fi

# ── everything that ACTS lives in oc_main, called on the LAST line ───────────
# `curl … | bash` feeds this file to bash on a pipe, and bash executes a piped
# script INCREMENTALLY: it reads a chunk, runs what parsed, reads the next. So
# `--help` used to print and exit while curl still had ~70 KB in flight, the read
# end closed under it, and a SUCCESSFUL run ended on curl: (23). A function
# definition is ONE command, so bash cannot run any of THIS BODY until it has
# read the closing brace.
#
# ⚠️ STATE THE GUARANTEE EXACTLY. It is NOT "bash reads the whole file before it
# executes anything" — the prologue above (set -euo pipefail, the from-stdin
# flag, these function definitions, the trap, the mode detection, which really
# does fork a subshell for SELF_DIR) all runs as it is parsed. What holds, and is
# all this needs, is that none of it can PRINT or EXIT:
#     install.sh produces no output and does not terminate
#     until it has been read in full.
# bin/tests/curl-bash-read-before-execute-guard.sh asserts both halves — the tail
# shape, and that nothing before this line prints or exits.
#
# NOT re-indented, on purpose. The wrap has to be provably structure-only, and a
# body at the original indentation makes that readable straight off `git diff`.
# Nothing at body level is `local` and nothing `return`s: every variable stays
# global exactly as before, `exit` stays the only way out, and the file's last
# statement is still the same `echo`, so the exit status is unchanged on every
# path. Those three are asserted statically by the guard above, not just meant.
oc_main() {

for _oc_arg in "$@"; do
  if [[ "$_oc_arg" == "--uninstall" ]]; then
    cmd_uninstall_release "$@"
    exit $?
  fi
done
unset _oc_arg

# ── preflight: platform + tools ──────────────────────────────────────────────
# Everything that must hold BEFORE the machine is touched, both modes. tmux is
# unconditional (both runtimes go through one tmuxNewSession, spawn.go:950); the
# runtime is claude OR codex, like `ocwarden install`'s runtime_bin_unresolved gate.
# PRESENCE only, no version — but an OC_*_BIN override must be an executable FILE
# (transport.go), else a stale export buys a green install nothing honours. Why it
# is fail-closed at all: bin/tests/install-preflight-guard.sh.
oc_preflight() {
  local miss=() c x p
  if [[ "$(uname -s)" != "Darwin" || "$(uname -m)" != "arm64" ]]; then
    echo "[install] FATAL: OffiCraft v0.x supports macOS Apple Silicon (darwin/arm64) only." >&2
    echo "[install]        This machine reports: $(uname -s)/$(uname -m)" >&2
    exit 1
  fi
  command -v tmux >/dev/null 2>&1 || miss+=("tmux — brew install tmux")
  c="${OC_CLAUDE_BIN:-}"; [[ -f "$c" && -x "$c" ]] || c="$(command -v claude 2>/dev/null || true)"
  x="${OC_CODEX_BIN:-}"; [[ -f "$x" && -x "$x" ]] || x="$(command -v codex 2>/dev/null || true)"
  for p in "$HOME/.local/bin" "$HOME/.npm-global/bin" /opt/homebrew/bin /usr/local/bin; do
    [[ -z "$c" && -x "$p/claude" ]] && c="$p/claude"
    [[ -z "$x" && -x "$p/codex" ]] && x="$p/codex"
  done
  [[ -n "$c$x" ]] || miss+=("an agent runtime: claude (npm install -g @anthropic-ai/claude-code) or codex")
  # BOTH resolutions are carried out (T-ff48). Exporting only the claude one made
  # the plist stamp below structurally claude-only: a host whose codex lives under
  # a version manager passed this preflight, installed, went online, and then
  # refused every codex spawn with runtime_bin_unresolved — while a claude member
  # on the SAME machine was rescued by the stamp.
  [[ ${#miss[@]} -eq 0 ]] && { OC_PF_CLAUDE="$c"; OC_PF_CODEX="$x"; return 0; }
  echo "[install] FATAL: a member is claude or codex inside tmux — without these one" >&2
  echo "[install]        never leaves 「waking」. NOTHING was installed; install and re-run:" >&2
  printf '[install]          %s\n' "${miss[@]}" >&2
  exit 1
}

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
      # Forwarded verbatim: the namespace decision belongs to the packaged
      # installer (one derivation point); this half only carries it across.
      --namespace|--port)
        # ARITY only — charset/pairing belongs to the packaged installer. Still
        # needed here: `--namespace --port 7756` would otherwise forward
        # "--port" AS THE NAMESPACE.
        [[ $# -ge 2 && -n "$2" ]] || { echo "[install] FATAL: $1 needs a value" >&2; exit 2; }
        FWD+=("$1" "$2"); shift 2 ;;
      --namespace=*|--port=*) FWD+=("$1"); shift ;;
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

To remove an install this installer made, see --uninstall:

  curl -fsSL … | bash -s -- --uninstall --help
EOF
        exit 0 ;;
      *)
        echo "[install] FATAL: unknown argument '$1' (supported: --force, --foreground, --relocate, --restart-live, --namespace <ns>, --port <port>, --tag <vX.Y.Z>)" >&2
        exit 2 ;;
    esac
  done

  oc_preflight  # before the download

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
  # With --foreground the delegated installer ends in `exec ocserverd serve` in a
  # CHILD process, so this shell stays alive as its parent and the trap fires
  # when serve exits (Ctrl-C).
  # Re-arms the stdin drain installed at the top of this file: a bare
  # `trap … EXIT` REPLACES the handler, so leaving this one as `rm -rf "$TMP"`
  # alone would silently retire the drain for the whole bootstrap path — the
  # path a `curl … | bash` user is actually on.
  #
  # EXIT and INT/TERM are deliberately SPLIT. An INT handler that does not exit
  # RETURNS TO THE SCRIPT, and when the script came in on stdin the rest of the
  # program is still sitting in that pipe — draining it there would delete the
  # remainder of the installer (demonstrated on a minimal repro: the commands
  # after the interrupted step simply never run). That is unreachable today only
  # because this whole `if` block is parsed up front and every path in it exits,
  # an invariant nothing states or tests. An INT that does not exit still reaches
  # the EXIT trap afterwards, so splitting costs nothing.
  trap 'rm -rf "$TMP"' INT TERM
  trap 'rm -rf "$TMP"; oc_drain_stdin' EXIT

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

# ── package mode (original in-tarball flow) ──────────────────────────────────
FORCE=0
FOREGROUND=0
RELOCATE=0
RESTART_LIVE=0
NS="${OC_NAMESPACE:-}"
PORT_FLAG=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --force) FORCE=1 ;;
    --foreground) FOREGROUND=1 ;;
    --relocate) RELOCATE=1 ;;
    --restart-live) RESTART_LIVE=1 ;;
    --namespace)   shift; NS="${1:-}" ;;
    --namespace=*) NS="${1#*=}" ;;
    --port)        shift; PORT_FLAG="${1:-}" ;;
    --port=*)      PORT_FLAG="${1#*=}" ;;
    -h|--help)
      sed -n '2,/^set -/p' "$SELF" | sed '$d; s/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "[install] FATAL: unknown argument '$1' (supported: --force, --foreground, --relocate, --restart-live, --namespace <ns> --port <port>)" >&2
      exit 2
      ;;
  esac
  shift
done

# The OffiCraft standard port — NOT 8770 (the retired open-company station's,
# which collides on transition-period machines) and NOT the previous 8780.
# Defined here rather than beside the port probe below because the namespace
# gate has to name it.
DEFAULT_PORT=7755

# ── namespace: isolation BY CONSTRUCTION, not by the caller's discipline ─────
# ONE key decides WHICH station a run touches ("" → ~/.officraft +
# com.officraft.serve; "x" → ~/.officraft-x + com.officraft.serve.x), and a
# namespaced run OWNS ITS CONFIG so a stray $OC_CONFIG/./oc.toml cannot steer it.
# Isolation used to mean OC_LAUNCHD_LABEL + a redirected HOME: two knobs the
# CALLER must remember, and label-without-HOME writes into the FIRST instance's
# root. THE EMPTY NAMESPACE CHANGES NOTHING (pinned by install-guard.sh §10).
# Charset mirrors cli/ocwarden/namespace.go, server/ocserverd/config.go and
# bin/ocserver — held together by bin/tests/fixtures/namespace-axes.tsv.
NS_DOT=""
NS_DASH=""
if [[ -n "$NS" ]]; then
  if ! printf '%s' "$NS" | grep -qE '^[a-z0-9-]{1,16}$'; then
    echo "[install] FATAL: --namespace must match [a-z0-9-]{1,16}, got: '$NS'" >&2
    echo "[install]        Refusing to fall back to the MAIN instance's root/label." >&2
    exit 2
  fi
  if [[ -z "$PORT_FLAG" ]]; then
    echo "[install] FATAL: --namespace requires --port (the default $DEFAULT_PORT is the MAIN instance's port)." >&2
    echo "[install]        e.g. ./install.sh --namespace $NS --port 7756" >&2
    exit 2
  fi
  NS_DOT=".$NS"
  NS_DASH="-$NS"
elif [[ -n "$PORT_FLAG" ]]; then
  echo "[install] FATAL: --port is only valid together with --namespace (the main instance's port comes from OC_CONFIG/oc.toml)." >&2
  exit 2
fi
if [[ -n "$PORT_FLAG" ]] && ! printf '%s' "$PORT_FLAG" | grep -qE '^[0-9]{2,5}$'; then
  echo "[install] FATAL: --port must be a number, got: '$PORT_FLAG'" >&2
  exit 2
fi

oc_preflight

HERE="$(cd "$(dirname "$SELF")" && pwd)"
ROOT_DIR="$HOME/.officraft$NS_DASH"  # NS_DASH="" ⇒ the historical literal
BIN_DIR="$ROOT_DIR/bin"
DB_PATH="$ROOT_DIR/server/data/officraft.db"
NS_CFG="$ROOT_DIR/server/oc.toml"

config_port() {
  # [server].port out of a config file, or "" when it does not set one.
  sed -n 's/^[[:space:]]*port[[:space:]]*=[[:space:]]*\([0-9]\{2,5\}\).*/\1/p' "$1" 2>/dev/null | head -1
}

# ── ns_relocation_gate: changing an instance's port is a relocation ──────────
# Runs HERE, before anything is written: a REFUSAL that fires after the binaries
# are swapped cannot honestly say "NOTHING was changed", and it said exactly that
# from a point where it was already false. The gate moved to where its claim is
# true rather than the claim being softened to fit the gate.
if [[ -n "$NS" && -f "$NS_CFG" ]]; then
  ns_old_port="$(config_port "$NS_CFG")"
  if [[ -n "$ns_old_port" && "$ns_old_port" != "$PORT_FLAG" ]]; then
    echo "[install] FATAL: instance '$NS' is configured for port $ns_old_port but this run says --port $PORT_FLAG." >&2
    echo "[install]        Changing an instance's port is a relocation, not a reload — NOTHING was changed." >&2
    echo "[install]        Re-run with --port $ns_old_port, or edit $NS_CFG deliberately." >&2
    exit 1
  fi
fi

for b in ocserverd ocwarden ocagent officraft; do
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
# singleton in launchd's GUI DOMAIN, and that domain is keyed on the UID — it
# does NOT follow $HOME. A scratch HOME relocates ROOT_DIR/BIN_DIR/PLIST but
# leaves TARGET resolving to the SAME job the real station runs under, so a run
# that believes it is sandboxed can still bootout the live service. Only the
# LABEL decides which job is at stake — hence --namespace suffixes it (NS_DOT)
# rather than leaving that to the caller: a namespaced run targets
# com.officraft.serve.<ns> BY CONSTRUCTION and can no longer bootout the main
# station. Explicit OC_LAUNCHD_LABEL still wins (an existing alt-labelled
# install must stay addressable).
LABEL="${OC_LAUNCHD_LABEL:-com.officraft.serve$NS_DOT}"
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
  # that rc escapes the command substitution and `set -e` kills the installer —
  # turning "no job is registered", the most ordinary state on a clean machine,
  # into a silent abort with no diagnostic.
  local out
  out="$(launchctl print "$1" 2>/dev/null || true)"
  printf '%s\n' "$out" | sed -n 's/^[[:space:]]*pid = \([0-9]*\).*/\1/p' | head -1
}

listening_ports_of() {
  # Space-separated TCP ports a pid holds in LISTEN, or "" when it holds none.
  #
  # Same pipefail trap as job_pid_of — this file has fallen into it TWICE. lsof
  # exits NON-ZERO when the pid has no listening socket, and that is an ordinary
  # state: a job still starting up, crash-looping, or bound only to a unix
  # socket. Left at the head of a pipeline under `set -o pipefail` that rc
  # escapes the command substitution and `set -e` kills the installer RIGHT
  # HERE, before a single warning line below has run — exit 1 and a COMPLETELY
  # BLANK screen, from a gate whose entire job is to explain itself. The port
  # list is cosmetic; it must never be able to abort the run.
  local out
  out="$(lsof -nP -iTCP -sTCP:LISTEN -a -p "$1" 2>/dev/null || true)"
  printf '%s\n' "$out" \
    | sed -n 's/.*:\([0-9]\{2,5\}\) (LISTEN).*/\1/p' | sort -u | tr '\n' ' ' | sed 's/ $//'
}

# ── live-service gate ────────────────────────────────────────────────────────
# The other gates here reason about FILES: is there a binary, a database, a
# plist naming our program, a config that would move. None of them ask the one
# question that decides whether re-installing is disruptive — IS A SERVER
# SERVING RIGHT NOW? On the maintainer's own machine every file-based gate
# answers "same paths, same port, same config, a plain reload" and waves the run
# through to `launchctl bootout`, which drops the running process and every
# client attached to it.
#
# That outage was reachable through a gate whose text explicitly PROMISED it
# would not happen ("keeps serving its old code until its next restart"), and
# through --force, whose documented meaning is only about overwriting files.
# Consent obtained against a wrong description of the harm is not consent, so
# the restart gets a gate of its own, phrased in the currency the operator
# actually cares about: connected clients.
#
# Ordering is deliberate — this runs BEFORE the binaries are copied and before
# `ocserverd migrate` touches the database, so declining leaves the machine
# byte-for-byte as it was. The ownership gate downstream is unchanged: it
# answers "is this job MINE", a different question from "is it BUSY", and a
# hijack of someone else's job must keep failing closed on its own terms.
#
# --foreground is exempt: it never registers or boots out a launchd job. A live
# service still blocks it, but as a port conflict — the honest diagnosis there.
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
# An existing binary OR an existing database means this machine already runs (or
# ran) OffiCraft: overwriting silently would swap the binary out from under a
# live server on its next restart. Be loud, require an explicit yes.
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
  # later restart — on the launchd path this script boots it out below, so the
  # restart is IMMEDIATE and this prompt used to describe the wrong harm. When a
  # live job exists the live-service gate above already took consent for that
  # outage in the correct terms; say which case applies rather than asserting
  # the comfortable one.
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
for b in ocserverd ocwarden ocagent officraft; do
  # copy-then-rename, so a running old binary is never truncated.
  cp "$HERE/$b" "$BIN_DIR/$b.new"
  chmod +x "$BIN_DIR/$b.new"
  # Strip a quarantine flag if a browser download attached one (curl/tar does
  # not; Safari does). Best-effort — absence is the norm.
  xattr -d com.apple.quarantine "$BIN_DIR/$b.new" 2>/dev/null || true
  mv "$BIN_DIR/$b.new" "$BIN_DIR/$b"
done

# ── namespaced instance config ───────────────────────────────────────────────
# A namespaced instance needs its own config BEFORE migrate: the DSN default
# derives from [server].namespace, so without it migrate creates the MAIN
# instance's database. Written only when absent — re-running is a reload, never
# a silent retarget. (Port-mismatch refusal: ns_relocation_gate above.)
if [[ -n "$NS" ]]; then
  mkdir -p "$(dirname "$NS_CFG")"
  if [[ -f "$NS_CFG" ]]; then
    echo "[install] namespace '$NS': reusing existing config $NS_CFG"
  else
    printf '[server]\nnamespace = "%s"\nport = %s\n' "$NS" "$PORT_FLAG" > "$NS_CFG"
    echo "[install] namespace '$NS': wrote $NS_CFG (namespace + port $PORT_FLAG)"
  fi
fi

echo "[install] migrating database (goose)…"
if [[ -n "$NS" ]]; then
  # Explicit, not inherited: the namespaced instance migrates ITS OWN database.
  OC_CONFIG="$NS_CFG" "$BIN_DIR/ocserverd" migrate
else
  "$BIN_DIR/ocserverd" migrate
fi

# Effective port: $OC_CONFIG / ./oc.toml [server].port override, else
# $DEFAULT_PORT (defined above, with the 7755-not-8770/8780 reason).
#
# CFG_ABS records WHICH config file that port came from, as an ABSOLUTE path.
# It matters for the launchd path: a relative ./oc.toml resolves against the CWD
# of whoever ran the installer, and the daemon has a different CWD. Baking the
# absolute path into the plist's OC_CONFIG keeps the port the installer gated on
# and the port the daemon binds identical.
#
# CFG_SRC records WHERE it came from — env (explicit OC_CONFIG), cwd (a ./oc.toml
# that merely happened to be in the current directory), inherited (carried over
# from the job we are replacing), or none. "cwd" is the one that bites: the
# difference between a config the operator chose and a file they happen to be
# standing next to. See the relocation gate below.
PORT="$DEFAULT_PORT"
CFG_ABS=""
CFG_SRC="none"
if [[ -n "$NS" ]]; then
  # A namespaced instance OWNS its config, so the $OC_CONFIG / ./oc.toml probe is
  # SKIPPED: a stray oc.toml must not redirect an install that named its target.
  PORT="$PORT_FLAG"
  CFG_ABS="$NS_CFG"
  CFG_SRC="namespace"
else
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
fi

# ── SERVE_CMD: the one true "how to run this instance by hand" (T-164) ───────
# Every place below that tells the operator to start the server themselves used
# to print a bare `$BIN_DIR/ocserverd serve`. On a NAMESPACED install that
# instruction is a trap: run it and the daemon resolves its own config ($OC_CONFIG
# -> ./oc.toml, both empty), lands on the CONVENTION DEFAULTS, and migrates
# ~/.officraft/server/data/officraft.db on port 7755 - the MAIN instance, not the
# one whose failure the operator is reading about. Measured on the official
# v0.5.356 package, 2026-09-10; see the --foreground branch's header.
#
# The failure paths are where this bites hardest: the reader is already in
# trouble, is being handed a command by the tool, and has no reason to doubt it.
# Deriving all of them from the SAME string as the exec keeps the advice and the
# behaviour from drifting apart - they were independent claims before, and a
# mutant that broke one left the other green.
#
# Empty $CFG_ABS keeps the bare form ON PURPOSE: a config-less serve booting on
# the convention defaults is documented, deliberate behaviour (oc.toml.example).
if [[ -n "$CFG_ABS" ]]; then
  SERVE_CMD="OC_CONFIG=$CFG_ABS $BIN_DIR/ocserverd serve"
else
  SERVE_CMD="$BIN_DIR/ocserverd serve"
fi

# ── same-label ownership gate ────────────────────────────────────────────────
# (LABEL / PLIST / GUI / TARGET / LOG_DIR / SERVE_LOG were resolved at the top —
# see the note there for why, and the port gate below for why this must precede
# it.)
# The label is a machine-wide singleton (see the identity block above). If
# something is ALREADY registered under it, blindly rendering over the plist and
# booting it out would silently hijack — or kill — a service this installer did
# not create (a hand-built job, or a repo-layout `bin/ocserver install` instance
# serving from a completely different path). So: adopt the label only
# when the existing plist demonstrably points at the binary WE just installed;
# anything else is a hard stop that changes nothing.
#
# OURS=1 additionally licenses the port gate below to treat that job's own
# listener as "not a conflict". (plist_program() is defined near the top of
# this file, shared with --uninstall's ownership check.)
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
# The ownership gate above established only "this is the same BINARY", but what
# it licenses is far larger: bootout the running service and re-register it with
# WHATEVER port and config this run happened to resolve. That gap is a foot-gun
# with a live trigger.
#
# The trigger is the ./oc.toml fallback. Merely standing in a directory that
# holds one — a checkout with a leftover e2e config, say — is enough to move the
# owner's running server to a different port AND point it at a different
# database. The port gate cannot catch it: it checks the NEW port, which is
# free. The result is a service that looks freshly installed and a cockpit that
# looks like all its data vanished, with nothing printed to connect the two.
# Before this installer created launchd jobs the same misread was harmless and
# reversible (a foreground process you could Ctrl-C); bootout + a persistent
# plist is what turned it into a destructive act.
#
# So when replacing a job of ours, compare the parameters that decide WHERE the
# service lives and WHAT data it serves:
#   - no config of our own + the old job had one  -> INHERIT it (a plain reload
#     must not silently drop the config pointer the running service relies on),
#     and re-derive the port from it so the port gate checks the real port.
#   - port or config would actually CHANGE       -> that is a relocation, not a
#     reload. Hard stop, print the before/after, require --relocate.
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
# over a healthy install is supposed to be idempotent, but our own running job
# holds the port, so a naive gate turns every second run into a FATAL — an
# install that "already worked" reported as broken. When every listener PID
# belongs to our own launchd job the port is not contended: we boot that job out
# further down and it is released before the new one binds. A foreign squatter —
# or ANY listener in --foreground mode, where nothing gets booted out — still
# fails exactly as before.
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
    echo "[install]   $SERVE_CMD" >&2
    exit 1
  fi
fi

if [[ "$FOREGROUND" == 1 ]]; then
  # ── the config the foreground daemon must inherit (T-164) ──────────────────
  # THE BUG THIS CLOSES. This branch used to `exec ocserverd serve` with NOTHING
  # in front of it. Two lines above, the plist path bakes OC_CONFIG=$CFG_ABS into
  # the job, and the migrate above ran with OC_CONFIG=$NS_CFG as a one-shot
  # prefix — neither is an export, so the exec inherited no config at all. The
  # daemon then fell back through its own order ($OC_CONFIG → ./oc.toml, both
  # empty) to the CONVENTION DEFAULTS: ~/.officraft/server/data/officraft.db and
  # port 7755. Measured end to end on 2026-09-10 with the official v0.5.356
  # tarball: `--namespace X --port Y --foreground` created and MIGRATED the
  # DEFAULT database (goose 74 → 101 on a pre-existing one) and then tried to
  # bind 7755, while the line printed just above said "open http://127.0.0.1:Y/".
  # Nothing anywhere said the namespace had been dropped.
  #
  # 🔴 THE MIGRATION RAN BEFORE THE PORT CHECK. A live station already holding
  # 7755 does NOT protect its own database from this: the daemon migrated the
  # default DB first and only then died on the port. "The main instance is
  # running, so it would have been stopped" is the intuition this refutes.
  #
  # WHY $CFG_ABS AND NOT $NS_CFG. $CFG_ABS is the SAME value the launchd path
  # writes into the plist (resolved a few lines above): the namespace config for
  # a namespaced run, the absolute form of $OC_CONFIG / ./oc.toml otherwise, and
  # EMPTY when no config file backed the port probe. Reusing it keeps the two
  # launch paths saying the same thing by construction rather than by two
  # authors remembering to.
  #
  # 🔴 EMPTY MUST STAY EMPTY. A config-less `ocserverd serve` booting on the
  # convention defaults is a DOCUMENTED, deliberate capability - oc.toml.example
  # says "The file may be entirely ABSENT: every key has a convention default, so
  # a bare `ocserverd serve` boots with zero setup", and server/ocserverd/config.go
  # repeats it. Failing when nothing resolves would delete that. The fallback is
  # only a betrayal when the caller NAMED a target, which is what the guard below
  # covers and the only case where refusing is right.
  #
  # ⚠️ THE GUARD BELOW CANNOT FIRE ON TODAY'S CODE PATH, and saying so is the
  # point. A namespaced run always reaches this line with $NS_CFG on disk: the
  # block above writes it (or reports reusing an existing one) unconditionally
  # when $NS is set, and `set -e` aborts if that write fails. So this is a
  # TRIPWIRE for a future edit that stops writing $NS_CFG — not a check that
  # fires today, and no test exercises it. What actually defends the fix is the
  # install-guard case asserting the exec CARRIES the config; do not read this
  # block as the thing that makes the silent fallback impossible.
  if [[ -n "$NS" && ! -f "$NS_CFG" ]]; then
    echo "[install] FATAL: --namespace '$NS' was given but its config $NS_CFG is missing." >&2
    echo "[install]        Refusing to start: a config-less serve would silently use the DEFAULT" >&2
    echo "[install]        database ($HOME/.officraft/server/data/officraft.db) and the DEFAULT port" >&2
    echo "[install]        $DEFAULT_PORT — not the instance you named. NOTHING was started." >&2
    exit 1
  fi
  echo
  echo "[install] ✅ installed. Starting OffiCraft in the foreground (--foreground)…"
  echo "[install]    open  http://127.0.0.1:${PORT}/  in your browser."
  echo "[install]    (first run: the serve log below prints a one-time setup link"
  echo "[install]     with ?code=… — open it to set your owner password.)"
  echo "[install]    Ctrl-C stops the service; restart later with:"
  echo "[install]      $SERVE_CMD"
  echo
  if [[ -n "$CFG_ABS" ]]; then
    exec env OC_CONFIG="$CFG_ABS" "$BIN_DIR/ocserverd" serve
  fi
  exec "$BIN_DIR/ocserverd" serve
fi

# ── launchd service install (default) ────────────────────────────────────────
# Registering serve as a launchd user agent is what makes "installed" mean the
# same thing an hour later: RunAtLoad starts it at login, KeepAlive restarts it
# after a crash, and it is not tied to the terminal that ran the installer.
# Identity and the same-label ownership gate were resolved above the port gate.
mkdir -p "$LA_DIR" "$LOG_DIR"

# ── claude resolution for the fleet spawn chain (T-ba62) ─────────────────────
# WHY THIS EXISTS HERE. This installer is the ONE link of the release launchd
# chain that runs in the operator's interactive shell, with a rich PATH holding
# version-manager shims (asdf/nvm/volta). Nothing downstream does: the serve
# daemon runs under launchd's minimal env, and the cockpit's 「安裝」 (POST
# /api/machines/{id}/bootstrap-here) hands THAT env straight to `ocwarden
# install`. So a plist without PATH/OC_CLAUDE_BIN guaranteed that on every
# one-click install the warden could not resolve claude — and (before T-ba62's
# fail-closed change) installed anyway, went online, and refused every spawn
# with zero owner-visible signal. bin/ocserver install has carried this stamp
# for the source path all along; the release path was missing it, which is why
# ONE-CLICK was the more broken of the two.
#
# The stamping mirrors bin/ocserver's block deliberately (same XML hygiene, same
# two-probe shim detection) — keep them in step. The path comes from oc_preflight.
# oc_probe_runtime BIN PATHVAL — bounded, non-interactive `--version` probe.
# Both bounds are load-bearing and were both missing before T-ff48:
#   - `</dev/null` — a shim that prompts would otherwise wait on a terminal that,
#     for a `curl | bash` or cockpit-driven install, has nobody in front of it.
#     Go's exec.Cmd gets this for free from a nil Stdin.
#   - a TIME budget — a shim that re-shims on first call or waits on the network
#     hangs the whole installer with no output since the previous step, which for
#     a headless operator is indistinguishable from slow. The Go reference bounds
#     the same probe (claudeProbeBudget, cli/ocwarden/install.go).
# OC_PROBE_BUDGET_SECS exists so the guard suite can prove the bound with a stub
# that hangs on purpose, without a 20s test.
oc_probe_runtime() {
  local bin="$1" pathval="$2"
  local budget="${OC_PROBE_BUDGET_SECS:-20}" pid ticks=0 max
  max=$((budget * 5))
  # Own process group (set -m), so the timeout kill below reaps the shim's whole
  # subtree. A version-manager shim typically execs or forks a real binary; killing
  # only the direct child leaves that grandchild orphaned and running — which is
  # how a bounded probe still ends up burning a core for hours.
  set -m
  env -i PATH="$pathval" HOME="$HOME" "$bin" --version >/dev/null 2>&1 </dev/null &
  pid=$!
  set +m
  while kill -0 "$pid" 2>/dev/null; do
    if [[ "$ticks" -ge "$max" ]]; then
      kill -9 -- "-$pid" 2>/dev/null || kill -9 "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
      echo "[install] WARN: '$bin --version' did not answer within ${budget}s — treating it as unusable under this PATH" >&2
      return 1
    fi
    sleep 0.2
    ticks=$((ticks + 1))
  done
  wait "$pid"
}

COMMON_PATH="/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
CLAUDE_BIN="${OC_PF_CLAUDE:-}"   # resolved once, in oc_preflight
SERVE_PATH="$COMMON_PATH"
if [[ -n "$CLAUDE_BIN" ]]; then
  if [[ "$CLAUDE_BIN" != /* || "$CLAUDE_BIN" == *[\ \	\"\'\<\>\&]* ]]; then
    echo "[install] WARN: resolved claude path '$CLAUDE_BIN' is not stampable (must be absolute, no whitespace/XML-special chars) — not stamping OC_CLAUDE_BIN" >&2
    CLAUDE_BIN=""
  elif oc_probe_runtime "$CLAUDE_BIN" "$COMMON_PATH"; then
    echo "[install] claude resolved: $CLAUDE_BIN (runs under the minimal launchd PATH; stamping OC_CLAUDE_BIN)"
  elif oc_probe_runtime "$CLAUDE_BIN" "$PATH"; then
    if [[ "$PATH" == *[\"\'\<\>\&]* ]]; then
      echo "[install] WARN: installer PATH contains XML-special chars — cannot stamp it into the serve plist; stamping OC_CLAUDE_BIN only (the claude shim may fail under the minimal PATH)" >&2
    else
      SERVE_PATH="$PATH"
      echo "[install] claude resolved: $CLAUDE_BIN (version-manager shim — stamping OC_CLAUDE_BIN AND the full installer PATH)"
    fi
  else
    echo "[install] WARN: claude at $CLAUDE_BIN failed --version under both the minimal and the installer PATH — stamping OC_CLAUDE_BIN best-effort" >&2
  fi
fi
claude_entry=""
if [[ -n "$CLAUDE_BIN" ]]; then
  claude_entry="    <key>OC_CLAUDE_BIN</key><string>$CLAUDE_BIN</string>
"
else
  # Empty = claude absent (codex-only host: do NOT nag) or unstampable (WARN above).
  if [[ -n "${OC_PF_CLAUDE:-}" ]]; then
    echo "[install] WARNING: no stampable claude path — a claude member here may hit" >&2
    echo "[install]          claude_bin_unresolved. Fix: re-run with OC_CLAUDE_BIN=/abs/path/claude" >&2
  fi
fi

# ── codex resolution for the fleet spawn chain (T-ff48) ─────────────────────
# The block below is the DELIBERATE MIRROR of the claude one above: same XML/exec
# hygiene, same two-probe shim detection, same best-effort fallback — keep the two
# in step. It exists because a member is claude OR codex and the preflight has
# always accepted a codex-only host, but only claude was ever stamped. On a host
# whose codex sits under asdf/nvm/volta that asymmetry is the whole bug: the
# cockpit's 「安裝」 hands the serve env to `ocwarden install`, which then cannot
# resolve codex under launchd's minimal PATH and refuses every codex spawn with
# runtime_bin_unresolved — while claude on the same machine works.
#
# The reference implementation is cli/ocwarden/install.go's resolveCodex seam,
# which stamps OC_CODEX_BIN and promotes the installer PATH the same way.
CODEX_BIN="${OC_PF_CODEX:-}"   # resolved once, in oc_preflight
if [[ -n "$CODEX_BIN" ]]; then
  if [[ "$CODEX_BIN" != /* || "$CODEX_BIN" == *[\ \	\"\'\<\>\&]* ]]; then
    echo "[install] WARN: resolved codex path '$CODEX_BIN' is not stampable (must be absolute, no whitespace/XML-special chars) — not stamping OC_CODEX_BIN" >&2
    CODEX_BIN=""
  elif oc_probe_runtime "$CODEX_BIN" "$COMMON_PATH"; then
    echo "[install] codex resolved: $CODEX_BIN (runs under the minimal launchd PATH; stamping OC_CODEX_BIN)"
  elif oc_probe_runtime "$CODEX_BIN" "$PATH"; then
    if [[ "$PATH" == *[\"\'\<\>\&]* ]]; then
      echo "[install] WARN: installer PATH contains XML-special chars — cannot stamp it into the serve plist; stamping OC_CODEX_BIN only (the codex shim may fail under the minimal PATH)" >&2
    else
      SERVE_PATH="$PATH"
      echo "[install] codex resolved: $CODEX_BIN (version-manager shim — stamping OC_CODEX_BIN AND the full installer PATH)"
    fi
  else
    echo "[install] WARN: codex at $CODEX_BIN failed --version under both the minimal and the installer PATH — stamping OC_CODEX_BIN best-effort" >&2
  fi
fi
codex_entry=""
if [[ -n "$CODEX_BIN" ]]; then
  codex_entry="    <key>OC_CODEX_BIN</key><string>$CODEX_BIN</string>
"
else
  # Empty = codex absent (claude-only host: do NOT nag) or unstampable (WARN above).
  if [[ -n "${OC_PF_CODEX:-}" ]]; then
    echo "[install] WARNING: no stampable codex path — a codex member here may hit" >&2
    echo "[install]          runtime_bin_unresolved. Fix: re-run with OC_CODEX_BIN=/abs/path/codex" >&2
  fi
fi

# OC_CONFIG is emitted ONLY when a config file actually backed the port probe:
# pointing the daemon at a nonexistent path is worse than the default.
cfg_entry=""
if [[ -n "$CFG_ABS" ]]; then
  cfg_entry="    <key>OC_CONFIG</key><string>$CFG_ABS</string>
"
fi

# Rendered to a sibling temp file first, so a write failure (read-only
# LaunchAgents, full disk) cannot leave a half-written plist where a valid one
# used to be. The explicit guard keeps this path speaking the same language as
# every other failure here — without it `set -e` aborts on a raw shell
# redirection error and the operator gets a bare "Permission denied".
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
    <key>PATH</key><string>$SERVE_PATH</string>
    <key>HOME</key><string>$HOME</string>
$claude_entry$codex_entry$cfg_entry    <key>OC_NO_OPEN_BROWSER</key><string>1</string>
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
# can leave the job torn down but not re-registered — a re-run that ENDS with
# the server dead. Poll until launchd really lets go.

# Where the serve log ends RIGHT NOW, before this boot appends anything. The
# claim link is scraped from the bytes after this mark and nowhere else:
# StandardOutPath APPENDS, so every previous boot's setup link is still in the
# file, and scanning the whole log makes a re-install reprint the very first
# install's claim code — long since consumed, so the link is dead — telling the
# owner to set a password they already set.
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
  echo "[install]          $SERVE_CMD" >&2
  exit 1
fi
# CONFIRM the bootstrap actually registered the label, in the gap between the two
# verbs (T-908d). `launchctl bootstrap` can exit 0 and register NOTHING; the next
# verb, kickstart, is swallowed with `|| true` here, so the FIRST thing to notice
# used to be the port health gate below — which then reported "the service was
# registered but nothing is listening", a sentence that is literally FALSE in this
# state, and offered a `launchctl bootout` that fails for the same reason.
#
# BOUNDED AND NON-FATAL ON TIMEOUT (~5s = 25 x 0.2s, the bootout poll above run in
# the opposite direction): registration can merely LAG a bootstrap that exited 0,
# and failing here on the lag alone would turn machines that install fine today
# into machines that refuse to install. This only records WHICH diagnosis the port
# gate is allowed to print; a machine that comes up listening never reads it.
registered=0
for _ in $(seq 1 25); do
  if launchctl print "$TARGET" >/dev/null 2>&1; then registered=1; break; fi
  sleep 0.2
done
[[ "$registered" == 1 ]] || echo "[install] WARN: launchd still does not know '$LABEL' ~5s after bootstrap exited 0." >&2

launchctl kickstart "$TARGET" >/dev/null 2>&1 || true

# Health gate: "bootstrap returned 0" only means launchd accepted the job, not
# that serve bound the port. Wait for the socket before claiming success, so a
# job that crash-loops on startup is reported as a failure here rather than as a
# URL that refuses connections.
up=0
for _ in $(seq 1 50); do
  if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then up=1; break; fi
  sleep 0.2
done
if [[ "$up" != 1 ]]; then
  if [[ "$registered" != 1 ]]; then
    # NEVER claim the job was registered here — it was not, and the bootout the
    # other branch offers would fail with the same "Could not find service" that
    # this branch exists to explain.
    echo "[install] FATAL: launchctl bootstrap exited 0 but registered nothing — launchd does not know" >&2
    echo "[install]        '$LABEL', so the job was never loaded and nothing came up on port $PORT." >&2
    echo "[install]        The plist to look at: $PLIST" >&2
    echo "[install]        Retry the load with:  launchctl bootstrap $GUI $PLIST" >&2
    echo "[install]        Or run it in the foreground: $SERVE_CMD" >&2
    echo "[install]        Remove the plist with: rm -f $PLIST" >&2
    exit 1
  fi
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

}
oc_main "$@"
