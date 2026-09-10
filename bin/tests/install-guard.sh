#!/usr/bin/env bash
# bin/tests/install-guard.sh — HERMETIC unit tests for bin/install.sh's
# LIVE-SERVICE GATE (T-eefc).
#
# THE DEFECT UNDER TEST
# ---------------------
# install.sh's pre-existing gates all reason about FILES: is there a binary, a
# database, a plist naming our program, a config that would move. On the
# maintainer's own machine every one of them answers "same paths, same port,
# same config — plain reload" and waves the run through to `launchctl bootout`,
# which drops the RUNNING server and every cockpit/agent attached to it. The
# existing-install prompt even promised the opposite ("keeps serving its old
# code until its next restart"), and --force — documented as being about
# overwriting files — silently authorized the outage.
#
# WHY THE SHIM IS SHAPED THIS WAY (read before "simplifying" it)
# --------------------------------------------------------------
# A launchd label is a singleton in the user's GUI DOMAIN, keyed on UID. It does
# NOT follow $HOME. So the obvious way to make these tests "safe" — point HOME at
# a scratch dir — relocates the files but leaves the job target resolving to the
# REAL station, and the equally obvious fix — set OC_LAUNCHD_LABEL to something
# harmless — silently stops testing the path an actual user walks, because a
# user never sets that variable. A green run under an overridden label proves
# nothing about the default one.
#
# So this suite does neither. The label stays the DEFAULT com.officraft.serve —
# the exact identity a real re-install resolves — and `launchctl` itself is
# replaced on PATH by a stub that IS the oracle: it reports whatever job state a
# case asks for and records every bootout/bootstrap in a tripwire. Nothing in the
# real launchd domain is read or written, no process is signalled, and the
# assertions are about the DEFAULT-label decision. HOME is still redirected, but
# only so the file-side gates have a sandbox to look at — never as the safety
# mechanism.
#
# plutil runs REAL by default — it only ever reads a plist this suite wrote
# inside its own temp dir, so the real tool gives the more faithful test at zero
# risk. It is stubbed only when a case opts in via SHIM_PLUTIL_STDOUT_ERR, to
# reproduce a host-version behaviour the runner's own macOS may not have; see
# the relocation case at the bottom.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/../install.sh"
[[ -f "$SCRIPT" ]] || { echo "FATAL: install.sh not found at $SCRIPT" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi; }

WORK="$(mktemp -d -t oc-install-guard.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

SHIMDIR="$WORK/shim"
PKG="$WORK/pkg"
FAKEHOME="$WORK/home"
mkdir -p "$SHIMDIR" "$PKG"

# ── the package under test: install.sh + its four sibling binaries ───────────
cp "$SCRIPT" "$PKG/install.sh"
for b in ocwarden ocagent officraft; do
  printf '#!/usr/bin/env bash\nexit 0\n' > "$PKG/$b"
  chmod +x "$PKG/$b"
done
# ocserverd is the ONE stub that records itself (T-164). The installer both
# `migrate`s and, under --foreground, `exec`s it — and what the exec CARRIES is
# the whole question: an exec with no OC_CONFIG silently lands the daemon on the
# convention-default database and port instead of the instance the operator
# named. From outside the process there is nothing else to look at, so the stub
# writes its argv and the config it inherited into the tripwire. It still
# exits 0, so the ~40 cases that predate this see no change.
cat > "$PKG/ocserverd" <<'SH'
#!/usr/bin/env bash
printf 'ocserverd %s OC_CONFIG=[%s]\n' "$*" "${OC_CONFIG:-}" >> "${SHIM_TRIPWIRE:-/dev/null}"
exit 0
SH
chmod +x "$PKG/ocserverd"

# ── PATH shims ───────────────────────────────────────────────────────────────
# uname: pin darwin/arm64 so the platform gate passes on any CI host.
cat > "$SHIMDIR/uname" <<'SH'
#!/usr/bin/env bash
case "${1:-}" in
  -s) echo Darwin ;;
  -m) echo arm64 ;;
  *)  echo Darwin ;;
esac
SH

# launchctl: the oracle. SHIM_JOB selects the job state a case is asserting on.
#   absent  → nothing registered under the label (fresh machine)
#   loaded  → registered but NOT running (no pid line) — booting it out is harmless
#   running → registered AND serving (pid line present) — this is the outage case
# Output mimics the real `launchctl print` layout, including the nested
# `state = active` entries that the pid parser must not be confused by.
#
# LABEL-AWARE ON PURPOSE. An earlier version of this stub ignored $2 entirely,
# which quietly weakened every assertion below: a label-blind oracle answers
# "running" no matter WHICH job the script asks about, so the suite could only
# prove "install.sh consults launchctl and obeys the answer" — never "it asks
# about the right label". Since the whole defect is that the default label
# collides with the live station's, that is the property most worth pinning.
# Now anything other than the expected target reads as NOT REGISTERED, so a
# script that resolved the wrong label sees "absent", skips the gate, and the
# live-job cases go red.
cat > "$SHIMDIR/launchctl" <<'SH'
#!/usr/bin/env bash
echo "launchctl $*" >> "$SHIM_TRIPWIRE"
case "${1:-}" in
  print)
    # A BOOTSTRAPPED LABEL IS REGISTERED. Without this the stub answered "gone"
    # for the rest of the run, which is not what launchd does and would make the
    # post-bootstrap registration poll (T-908d) time out on every green case.
    # SHIM_BOOTSTRAP_REGISTERS=0 is the opt-in for the failure being guarded:
    # bootstrap exits 0 and registers NOTHING.
    if [[ -f "$SHIM_STATE/.bootstrapped" ]]; then
      [[ "${SHIM_BOOTSTRAP_REGISTERS:-1}" == "1" ]] || exit 1
      # A SEPARATE target from SHIM_EXPECT_TARGET, and deliberately so. Those are
      # two different questions: SHIM_EXPECT_TARGET answers "is something ALREADY
      # running under the label the live-service gate is asking about" — which the
      # namespace cases point at the MAIN label ON PURPOSE, so a namespaced run
      # sees its own job as absent. SHIM_REGISTERED_TARGET answers "which label did
      # this bootstrap actually register", which for those same runs is the
      # NAMESPACED one. Collapsing the two left every namespaced case unable to
      # confirm its own registration, i.e. zero coverage of the healthy path they
      # walk. Still LABEL-AWARE: a poll that asks about the wrong target (say, a
      # label with the gui domain dropped) must read as "not registered".
      [[ "${2:-}" == "${SHIM_REGISTERED_TARGET:-$SHIM_EXPECT_TARGET}" ]] || exit 1
      printf '%s = {\n\tstate = running\n\tpid = 4242\n}\n' "${2:-}"
      exit 0
    fi
    # once booted out, the label really is gone — lets the poll loop exit
    [[ -f "$SHIM_STATE/.booted-out" ]] && exit 1
    # asked about a label that is not the one under test → nothing registered
    [[ "${2:-}" == "$SHIM_EXPECT_TARGET" ]] || exit 1
    case "${SHIM_JOB:-absent}" in
      running)
        cat <<'OUT'
gui/501/com.officraft.serve = {
	active count = 1
	state = running
	program = /path/to/ocserverd
	pid = 4242
	spawn type = daemon
	endpoints = {
		"com.officraft.serve" = {
			state = active
			active count = 1
		}
	}
}
OUT
        exit 0 ;;
      loaded)
        cat <<'OUT'
gui/501/com.officraft.serve = {
	active count = 0
	state = not running
	program = /path/to/ocserverd
}
OUT
        exit 0 ;;
      *) exit 1 ;;
    esac ;;
  bootout)    touch "$SHIM_STATE/.booted-out";   exit 0 ;;
  bootstrap)  touch "$SHIM_STATE/.bootstrapped"; exit 0 ;;
  kickstart)  exit 0 ;;
esac
exit 0
SH

# lsof: two distinct queries.
#   -p <pid>        → the live-service gate's cosmetic "listening on port(s) …"
#   -iTCP:<port>    → the port gate (before bootstrap: free) and the health gate
#                     (after bootstrap: listening). Keyed on the bootstrap flag
#                     so one stub serves both without a call counter.
#
# EXIT CODES MATTER MORE THAN OUTPUT HERE. The real lsof exits NON-ZERO when a
# query matches nothing, including the ordinary case of a running pid that holds
# no listening socket. An earlier stub returned 0 unconditionally for -p, which
# is LOOSER than the real tool — and that single mismatch hid a fatal bug: the
# gate's own port-lookup aborted the whole installer under `set -e -o pipefail`,
# producing exit 1 with a blank screen, and the suite still went green. A stub
# that is more forgiving than the tool it stands in for does not test the code,
# it tests a fiction. SHIM_NO_LISTEN=1 reproduces that real-world state.
# PORT-AWARE ON PURPOSE (default-port change to 7755). The stub used to print a
# HARDCODED `127.0.0.1:8780` for every query, which made it blind to WHICH port
# install.sh probed: a regression that moved the effective default back would
# still see a listener and still read the same line back. Now the stub echoes
# the port it was actually ASKED about, and every invocation is recorded in the
# tripwire — so a case can assert the probe went to the port the default
# resolves to, not merely that a probe happened.
cat > "$SHIMDIR/lsof" <<'SH'
#!/usr/bin/env bash
echo "lsof $*" >> "$SHIM_TRIPWIRE"
QPORT=""
for a in "$@"; do
  case "$a" in
    -p)
      [[ "${SHIM_NO_LISTEN:-0}" == "1" ]] && exit 1
      echo "ocserverd 4242 tester 5u IPv4 0x0 0t0 TCP 127.0.0.1:${SHIM_LIVE_PORT:-7755} (LISTEN)"
      exit 0 ;;
    -iTCP:*) QPORT="${a#-iTCP:}" ;;
  esac
done
# A job launchd never registered cannot be listening. Keeping this stub's
# answer tied to .bootstrapped ALONE would let the "registered nothing" case
# sail through the health gate and never reach the message under test.
if [[ -f "$SHIM_STATE/.bootstrapped" && "${SHIM_BOOTSTRAP_REGISTERS:-1}" == "1" ]]; then
  echo "ocserverd 4242 tester 5u IPv4 0x0 0t0 TCP 127.0.0.1:${QPORT:-7755} (LISTEN)"
  exit 0
fi
exit 1
SH
# tmux + claude: the tool preflight (T-7f38) refuses to install when either is
# missing, so both are stubbed here to keep THIS suite about the live-service
# gate. They are never invoked — the preflight only asks whether they resolve.
# (Deliberately not the real binaries: a host without tmux must not silently
# change what these cases test.)
# plutil: real by default. Under SHIM_PLUTIL_STDOUT_ERR=1 it reproduces macOS
# 15, which prints "no value at that key path" on STDOUT and exits non-zero —
# macOS 26 prints that on stderr. Without this shim the relocation coverage
# below only has teeth on a macOS 15 host, so the fix it guards could be
# reverted and every newer machine's CI would stay green.
cat > "$SHIMDIR/plutil" <<'SH'
#!/usr/bin/env bash
[[ -n "${SHIM_PLUTIL_LOG:-}" ]] && printf '%s\n' "$*" >> "$SHIM_PLUTIL_LOG"
if [[ "${SHIM_PLUTIL_STDOUT_ERR:-0}" == 1 ]]; then
  if out="$(/usr/bin/plutil "$@" 2>/dev/null)"; then
    printf '%s\n' "$out"; exit 0
  fi
  printf '%s: Could not extract value, error: No value at that key path or invalid key path: %s\n' "${!#}" "${2:-}"
  exit 1
fi
exec /usr/bin/plutil "$@"
SH
printf '#!/usr/bin/env bash\nexit 0\n' > "$SHIMDIR/tmux"
printf '#!/usr/bin/env bash\necho "9.9.9 (Claude Code)"\nexit 0\n' > "$SHIMDIR/claude"

# sleep: the REAL tool unless a case opts out. The bounded-poll cases below burn
# 25 x 0.2s + 50 x 0.2s of wall clock waiting for two timeouts whose LENGTH is not
# what they assert on — SHIM_FAST_SLEEP=1 removes the wait without removing a
# single assertion. Off by default so no other case silently stops waiting.
cat > "$SHIMDIR/sleep" <<'SH'
#!/usr/bin/env bash
[[ "${SHIM_FAST_SLEEP:-0}" == "1" ]] && exit 0
exec /bin/sleep "$@"
SH

chmod +x "$SHIMDIR"/uname "$SHIMDIR"/launchctl "$SHIMDIR"/lsof "$SHIMDIR"/tmux "$SHIMDIR"/claude "$SHIMDIR"/plutil "$SHIMDIR"/sleep

PLIST_REL="Library/LaunchAgents/com.officraft.serve.plist"

# reset_fixture <preinstalled|fresh>
#   preinstalled → the re-install shape: binaries + database + a plist that
#                  points at OUR binary (so the ownership gate adopts the label
#                  and the relocation gate sees no move — i.e. every pre-existing
#                  gate says "plain reload", which is exactly the situation the
#                  live-service gate has to catch).
#   fresh        → clean machine.
reset_fixture() {
  rm -rf "$FAKEHOME"
  mkdir -p "$FAKEHOME/Library/LaunchAgents"
  rm -f "$WORK/.booted-out" "$WORK/.bootstrapped" "$WORK/.tripwire"
  : > "$WORK/.tripwire"
  if [[ "$1" == "preinstalled" ]]; then
    mkdir -p "$FAKEHOME/.officraft/bin" "$FAKEHOME/.officraft/server/data"
    printf '#!/usr/bin/env bash\nexit 0\n' > "$FAKEHOME/.officraft/bin/ocserverd"
    chmod +x "$FAKEHOME/.officraft/bin/ocserverd"
    printf 'OLD-BINARY' > "$FAKEHOME/.officraft/bin/ocwarden"
    : > "$FAKEHOME/.officraft/server/data/officraft.db"
    cat > "$FAKEHOME/$PLIST_REL" <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.officraft.serve</string>
  <key>ProgramArguments</key>
  <array>
    <string>$FAKEHOME/.officraft/bin/ocserverd</string>
    <string>serve</string>
  </array>
  <key>RunAtLoad</key><true/>
</dict>
</plist>
PL
  fi
}

# run_install <job-state> [args…] — NOTE: OC_LAUNCHD_LABEL is never set, so the
# run resolves the DEFAULT label, exactly as a real user's re-install does.
# stdin is </dev/null → non-interactive, the `curl | bash` shape.
# The target the script MUST resolve when nobody overrides the label. Every
# assertion about the gate firing is therefore also an assertion that it asked
# launchd about THIS job and no other.
EXPECT_TARGET="gui/$(id -u)/com.officraft.serve"
GUI_EXPECT="gui/$(id -u)"

run_install() {
  local job="$1"; shift
  OUT="$(cd "$WORK" && env -i \
    PATH="$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$FAKEHOME" SHIM_JOB="$job" SHIM_TRIPWIRE="$WORK/.tripwire" SHIM_STATE="$WORK" \
    SHIM_EXPECT_TARGET="$EXPECT_TARGET" SHIM_NO_LISTEN="${SHIM_NO_LISTEN:-0}" \
    SHIM_PLUTIL_STDOUT_ERR="${SHIM_PLUTIL_STDOUT_ERR:-0}" SHIM_PLUTIL_LOG="${SHIM_PLUTIL_LOG:-}" \
    SHIM_BOOTSTRAP_REGISTERS="${SHIM_BOOTSTRAP_REGISTERS:-1}" SHIM_FAST_SLEEP="${SHIM_FAST_SLEEP:-0}" \
    SHIM_REGISTERED_TARGET="${SHIM_REGISTERED_TARGET:-$EXPECT_TARGET}" \
    bash "$PKG/install.sh" "$@" </dev/null 2>&1)"
  RC=$?
}

booted_out() { [[ -f "$WORK/.booted-out" ]] && echo yes || echo no; }
# The tripwire records every launchctl invocation. Asserting ON it is what turns
# "the script did something" into "the script did it TO THE RIGHT JOB".
# `-e` is not optional: needles that START WITH A DASH (an lsof flag such as
# -iTCP:7755) are otherwise parsed by grep as options and the call fails with a
# usage error, which reads as "not found" — a false FAIL that looks like a real
# one.
tripwire_has() { grep -qF -e "$1" "$WORK/.tripwire"; }
# EXACT-LINE variant. tripwire_has is a SUBSTRING match, which silently lies for
# namespaced labels: "launchctl bootout gui/501/com.officraft.serve" is a prefix
# of "…serve.lab", so asking "did it boot out the MAIN station?" answers yes for
# a run that correctly booted out only its own namespaced job. Anything asserting
# about one label NOT being addressed must use this.
tripwire_has_exact() { grep -qxF -e "$1" "$WORK/.tripwire"; }

echo "install.sh live-service gate — hermetic tests (default label, stubbed launchd)"

# ── 1. THE DEFECT: live job + piped/non-interactive run → fail CLOSED ────────
# Every pre-existing gate passes here (same binary, same port, no config move).
# Only the new gate stands between this run and a bootout of the live service.
reset_fixture preinstalled
run_install running
check "live job + non-interactive: aborts" "1" "$RC"
check "live job + non-interactive: never boots the service out" "no" "$(booted_out)"
case "$OUT" in *"A LIVE OffiCraft service is running"*) ok "live job: says WHAT it detected";; *) bad "live job: says WHAT it detected ($OUT)";; esac
case "$OUT" in *DISCONNECTED*) ok "live job: says what the CONSEQUENCE is (clients dropped)";; *) bad "live job: names the consequence";; esac
case "$OUT" in *"--restart-live"*) ok "live job: says what the operator CAN DO (--restart-live)";; *) bad "live job: offers --restart-live";; esac
case "$OUT" in *"OC_LAUNCHD_LABEL"*) ok "live job: offers the install-alongside escape hatch";; *) bad "live job: offers the install-alongside escape hatch";; esac

# Aborting must leave the machine byte-for-byte untouched — the gate runs BEFORE
# the binaries are copied, so a declined run cannot even have half-installed.
check "live job + non-interactive: leaves the old binaries untouched" "OLD-BINARY" "$(cat "$FAKEHOME/.officraft/bin/ocwarden")"

# THE LABEL ITSELF. Everything above would pass just as happily if install.sh
# interrogated some other job and got lucky; this pins that the job it asked
# about is the DEFAULT label — the one the live station actually runs under.
if tripwire_has "launchctl print $EXPECT_TARGET"; then
  ok "live job: install.sh interrogated the DEFAULT label ($EXPECT_TARGET)"
else
  bad "live job: install.sh interrogated the DEFAULT label ($EXPECT_TARGET) — tripwire: $(cat "$WORK/.tripwire")"
fi

# ── 1b. the gate must survive a live pid that holds NO listening socket ──────
# Real lsof exits non-zero for such a pid (a job still starting, crash-looping,
# or bound only to a unix socket). The port list is decoration; if looking it up
# can abort the run, the gate dies before printing anything and the operator
# gets exit 1 against a blank screen — strictly worse than the bug being fixed.
reset_fixture preinstalled
SHIM_NO_LISTEN=1 run_install running
unset SHIM_NO_LISTEN
check "live job w/o listening socket: still aborts" "1" "$RC"
check "live job w/o listening socket: never boots the service out" "no" "$(booted_out)"
case "$OUT" in *"A LIVE OffiCraft service is running"*) ok "live job w/o listening socket: STILL EXPLAINS ITSELF (does not die silently)";; *) bad "live job w/o listening socket: gate produced no explanation (output was: '$OUT')";; esac
if [[ -n "$OUT" ]]; then ok "live job w/o listening socket: output is not empty"; else bad "live job w/o listening socket: output was EMPTY — the gate aborted before it could speak"; fi

# ── 2. --force must NOT authorize an outage ─────────────────────────────────
# The conflation that made this reachable: --force is documented as overwriting
# FILES. Letting it also drop every live connection is the bug, not the feature.
reset_fixture preinstalled
run_install running --force
check "live job + --force: still aborts (force is about files, not uptime)" "1" "$RC"
check "live job + --force: never boots the service out" "no" "$(booted_out)"
case "$OUT" in *"--force alone does NOT authorize"*) ok "live job + --force: explains why --force was not enough";; *) bad "live job + --force: explains why --force was not enough";; esac

# ── 3. the explicit override works (a gate, not a wall) ─────────────────────
reset_fixture preinstalled
run_install running --force --restart-live
check "live job + --restart-live: proceeds" "0" "$RC"
check "live job + --restart-live: boots the old job out" "yes" "$(booted_out)"
case "$OUT" in *"--restart-live given"*) ok "live job + --restart-live: announces the restart";; *) bad "live job + --restart-live: announces the restart";; esac
# and it boots out THAT job, by its full target — not merely "some job".
if tripwire_has "launchctl bootout $EXPECT_TARGET"; then
  ok "live job + --restart-live: bootout targeted the DEFAULT label ($EXPECT_TARGET)"
else
  bad "live job + --restart-live: bootout targeted the DEFAULT label — tripwire: $(cat "$WORK/.tripwire")"
fi
if tripwire_has "launchctl bootstrap"; then
  ok "live job + --restart-live: the service is bootstrapped back up (not left down)"
else
  bad "live job + --restart-live: the service is bootstrapped back up"
fi

# ── 4. no job registered → gate stays silent (fresh install must not regress) ─
reset_fixture fresh
run_install absent
check "no job: fresh install succeeds" "0" "$RC"
case "$OUT" in *"A LIVE OffiCraft service"*) bad "no job: gate must stay silent";; *) ok "no job: gate stays silent";; esac

# ── 5. registered but NOT running → gate stays silent ───────────────────────
# Nothing is serving, so bootout costs nobody a session. Gating here would train
# the operator to type y reflexively, which is how a real gate stops working.
reset_fixture preinstalled
run_install loaded --force
check "loaded-but-stopped job: proceeds without prompting" "0" "$RC"
case "$OUT" in *"A LIVE OffiCraft service"*) bad "loaded-but-stopped: gate must stay silent";; *) ok "loaded-but-stopped: gate stays silent";; esac
case "$OUT" in *"No OffiCraft service is currently running"*) ok "loaded-but-stopped: existing-install prompt tells the truth about uptime";; *) bad "loaded-but-stopped: existing-install prompt tells the truth about uptime";; esac

# ── 6. --foreground never boots a job out, so the gate must not fire ────────
reset_fixture preinstalled
run_install running --force --foreground
check "--foreground + live job: live-service gate does not fire" "no" "$(booted_out)"
case "$OUT" in *"A LIVE OffiCraft service"*) bad "--foreground: gate must not fire (nothing is booted out)";; *) ok "--foreground: gate does not fire";; esac

# ── 7. the misleading reassurance is gone ──────────────────────────────────
# The old text promised "keeps serving its old code until its next restart" on a
# path that restarts it immediately. Consent given against a false description of
# the harm is not consent, so this exact sentence must never come back.
# Matched on an EMITTED line (not a comment), so install.sh may keep explaining
# in prose why the old sentence was wrong without tripping its own guard.
if grep -qE '^[^#]*keeps serving its old' "$SCRIPT"; then
  bad "install.sh no longer promises a running server is left alone (that promise was false on the launchd path)"
else
  ok "install.sh no longer promises a running server is left alone"
fi

# ── 8. interactive shape: a tty must default to NO ──────────────────────────
# `script` allocates a pty so `[[ -t 0 ]]` is genuinely true — this exercises the
# prompt branch rather than asserting it exists. Skipped (loudly) if the host's
# script(1) does not take the BSD form, so a missing pty can never read as a pass.
# The answer is delayed rather than piped straight in: script(1) forwards stdin
# and closes the pty as soon as it drains, so an immediately-available answer can
# reach the child before `read` is blocking on it — the child then sees EOF,
# takes the default, and the case passes for entirely the wrong reason. Waiting
# until the prompt is genuinely on screen is what makes this a test of the
# prompt rather than a test of EOF handling. The trailing sleep keeps the pty
# open long enough for the rest of the run to finish.
run_interactive() {
  local job="$1" answer="$2"; shift 2
  OUT="$(cd "$WORK" && { sleep 0.6; printf '%s\n' "$answer"; sleep 1.2; } | env \
    PATH="$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$FAKEHOME" SHIM_JOB="$job" SHIM_TRIPWIRE="$WORK/.tripwire" SHIM_STATE="$WORK" \
    SHIM_EXPECT_TARGET="$EXPECT_TARGET" SHIM_NO_LISTEN="${SHIM_NO_LISTEN:-0}" \
    script -q /dev/null bash "$PKG/install.sh" "$@" 2>&1)"
  RC=$?
}
reset_fixture preinstalled
run_interactive running n --force
if [[ "$OUT" == *"Restart the running service?"* ]]; then
  check "interactive + 'n': aborts" "1" "$RC"
  check "interactive + 'n': never boots the service out" "no" "$(booted_out)"
  case "$OUT" in *"the running service was NOT touched"*) ok "interactive + 'n': confirms nothing was touched";; *) bad "interactive + 'n': confirms nothing was touched";; esac

  # bare Enter = decline. The default must be the safe one.
  reset_fixture preinstalled
  run_interactive running "" --force
  check "interactive + bare Enter: defaults to NO" "1" "$RC"
  check "interactive + bare Enter: never boots the service out" "no" "$(booted_out)"

  # and 'y' really does proceed — otherwise the above could pass on a script
  # that simply always aborts.
  reset_fixture preinstalled
  run_interactive running y --force
  check "interactive + 'y': proceeds" "0" "$RC"
  check "interactive + 'y': boots the old job out" "yes" "$(booted_out)"
else
  echo "  skip — no usable pty via script(1); interactive prompt branch NOT verified here"
fi

# ── 9. the DEFAULT port a fresh install actually resolves is 7755 ───────────
# Not a string check on install.sh — this walks the real fresh-install path with
# NO config anywhere (env -i, temp HOME, CWD=$WORK which holds no oc.toml), so
# the port can only come from DEFAULT_PORT. Two independent witnesses, each its
# own assertion so a break names itself:
#   (a) the PORT GATE probe — install.sh asks lsof about the effective port
#       before writing anything; the port-aware stub records exactly which one.
#   (b) the SETUP LINK the operator is told to open at the end.
# Plus a negative: the retired 8780 must appear nowhere in the run's output. The
# negative alone would be satisfied by an installer that resolved some third
# port, and the positives alone would be satisfied by one that printed 7755
# while probing something else — together they pin the port end to end.
reset_fixture fresh
run_install absent
check "fresh install: succeeds on the default path" "0" "$RC"
if tripwire_has "-iTCP:7755"; then
  ok "fresh install: the port gate probed 7755 (the default is what gets gated)"
else
  bad "fresh install: the port gate did NOT probe 7755 — tripwire: $(cat "$WORK/.tripwire")"
fi
case "$OUT" in *"http://127.0.0.1:7755/"*) ok "fresh install: the setup link the operator is handed is on 7755";; *) bad "fresh install: setup link is not on 7755 (output was: '$OUT')";; esac
case "$OUT" in *8780*) bad "fresh install: the retired 8780 still appears in the run's output ('$OUT')";; *) ok "fresh install: no trace of the retired 8780 in the output";; esac
# THE POSITIVE CONTROL FOR THE REGISTRATION POLL (case 9b is the negative one).
# A poll that asks launchd the WRONG question — the bare label with the gui domain
# dropped, say — answers "not registered" on a machine that is perfectly healthy:
# every install then warns falsely, pays the full ~5s wait, and the moment the
# port gate trips it prints the "registered nothing" diagnosis about a job that IS
# registered. That is this ticket's own defect pointing the other way, and without
# this line nothing in the suite could see it (measured: 107 ok / 0 failed).
case "$OUT" in
  *"still does not know"*) bad "fresh install: warns that launchd does not know a label that IS registered — the poll is asking about the wrong target:
$OUT" ;;
  *) ok "fresh install: the registration poll confirms the healthy label (no false 'still does not know')" ;;
esac

# ── 9a. the rendered plist must pass `serve` EXPLICITLY (T-107) ─────────────
# ocserverd no longer implies the subcommand: a bare invocation prints usage and
# exits 2. So a plist that lost the word `serve` is not a cosmetic drift — the
# job never serves, and launchd's KeepAlive silently retries it every 10s
# forever with nothing anywhere raising a hand. Nothing else in this suite looks
# at ProgramArguments.1: plist_program reads index 0 (the binary) only, and the
# fresh-install cases stop at "a plist exists". Read from the plist case 9 just
# rendered, by index and verbatim, so neither a reordered array nor a `serve`
# that merely appears SOMEWHERE in the file can satisfy it.
PROG_ARG1="$(/usr/bin/plutil -extract ProgramArguments.1 raw -o - "$FAKEHOME/$PLIST_REL" 2>&1)"
check "fresh install: the rendered plist passes 'serve' as ProgramArguments.1" "serve" "$PROG_ARG1"

# ── 9b. bootstrap exits 0 and registers NOTHING → blame bootstrap (T-908d) ──
# THE DEFECT. `launchctl bootstrap` can exit 0 while registering no job at all.
# kickstart is swallowed here with `|| true`, so the first thing to notice was the
# PORT HEALTH GATE, which then printed "the '<label>' service was registered but
# nothing is listening on port …" — a sentence that is literally FALSE in this
# state — and handed the operator `launchctl bootout <target>`, which fails with
# the same "Could not find service" the message failed to mention.
#
# The assertions are about WHICH DIAGNOSIS the operator gets, and the negative one
# (the false "was registered" sentence) is the one carrying the defect: an
# implementation that merely ADDS a warning and leaves the old sentence in place
# still ships the lie, and only the negative catches that.
reset_fixture fresh
SHIM_BOOTSTRAP_REGISTERS=0 SHIM_FAST_SLEEP=1 run_install absent
check "bootstrap registered nothing: the install FAILS instead of reporting success" "1" "$RC"
case "$OUT" in
  *"registered nothing"*) ok "bootstrap registered nothing: the failure names the bootstrap step that actually failed" ;;
  *) bad "bootstrap registered nothing: the failure does not name the bootstrap step:
$OUT" ;;
esac
case "$OUT" in
  *"was registered but nothing is listening"*) bad "bootstrap registered nothing: still prints the FALSE 'was registered' sentence:
$OUT" ;;
  *) ok "bootstrap registered nothing: the false 'was registered' sentence is gone" ;;
esac
# The remediation has to WORK in the state it is printed in. bootout of a label
# launchd does not know fails, so this branch must not offer it.
case "$OUT" in
  *"launchctl bootout"*) bad "bootstrap registered nothing: offers a bootout that fails for the very reason being reported:
$OUT" ;;
  *) ok "bootstrap registered nothing: does not offer a bootout that cannot work here" ;;
esac
case "$OUT" in
  *"launchctl bootstrap $GUI_EXPECT $FAKEHOME/$PLIST_REL"*) ok "bootstrap registered nothing: offers a retry that names the real domain and plist" ;;
  *) bad "bootstrap registered nothing: no usable retry command naming the plist:
$OUT" ;;
esac
# The poll is BOUNDED and the kickstart still happens: the label lagging into
# registration must remain an ordinary successful install. Case 9 above is that
# control (same path, SHIM_BOOTSTRAP_REGISTERS defaulted to 1, rc 0) — this one
# only has to prove the registration was actually interrogated after the load.
if tripwire_has "launchctl bootstrap"; then
  ok "bootstrap registered nothing: the bootstrap was really attempted (not a refusal from an earlier gate)"
else
  bad "bootstrap registered nothing: no bootstrap in the tripwire — the run never reached the step under test"
fi

# ── 10. --namespace: isolation BY CONSTRUCTION (T-5047) ─────────────────────
# WHY THESE CASES EXIST. Before them, the 39 cases above passed identically with
# the namespace derivation DELETED — mutate ROOT_DIR back to "$HOME/.officraft"
# and LABEL back to "com.officraft.serve" (i.e. a namespaced install silently
# targets the MAIN instance's root and boots out the MAIN instance's job, the
# exact disaster the flag exists to prevent) and the whole suite stayed green.
# A guard suite that cannot see that is not guarding the flag, it is guarding
# the code that happened to be written first.
#
# The stub is LABEL-AWARE (see its header), which is what makes this testable:
# SHIM_EXPECT_TARGET is set to the MAIN label while the run asks about the
# NAMESPACED one, so a correct run sees "nothing registered" for its own job and
# the tripwire records which label it actually addressed.
NS="lab"
NS_PORT="7756"
MAIN_TARGET="gui/$(id -u)/com.officraft.serve"
NS_TARGET="gui/$(id -u)/com.officraft.serve.$NS"

# run_install_ns <job-state> <expect-target> [args…] — same as run_install but
# lets a case choose WHICH label the launchctl oracle is willing to answer for.
# The two SHIM_*_TARGET values differ here ON PURPOSE, and that is the whole point
# of this helper: SHIM_EXPECT_TARGET stays on the MAIN label so the live-service
# gate still sees the namespaced job as absent (10a), while SHIM_REGISTERED_TARGET
# names the label the namespaced bootstrap actually registers. `env -i` DROPS
# anything not listed, and until it was listed every namespaced case took the
# "registration unconfirmed" branch — zero coverage of the healthy path they walk,
# and 25 x 0.2s of real sleep each to get there (hence SHIM_FAST_SLEEP too).
run_install_ns() {
  local job="$1" expect="$2"; shift 2
  OUT="$(cd "$WORK" && env -i \
    PATH="$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$FAKEHOME" SHIM_JOB="$job" SHIM_TRIPWIRE="$WORK/.tripwire" SHIM_STATE="$WORK" \
    SHIM_EXPECT_TARGET="$expect" SHIM_NO_LISTEN="${SHIM_NO_LISTEN:-0}" \
    SHIM_REGISTERED_TARGET="$NS_TARGET" \
    SHIM_BOOTSTRAP_REGISTERS="${SHIM_BOOTSTRAP_REGISTERS:-1}" SHIM_FAST_SLEEP="${SHIM_FAST_SLEEP:-0}" \
    bash "$PKG/install.sh" "$@" </dev/null 2>&1)"
  RC=$?
}

# 10a. THE DISASTER CASE. A LIVE MAIN station is running, and an operator installs
# a SECOND instance by name. The namespaced run must address its OWN label and
# its OWN root — it must not so much as ask launchd about the main job, let alone
# boot it out. This is the case that goes red the moment the derivation is lost.
reset_fixture preinstalled
run_install_ns running "$MAIN_TARGET" --namespace "$NS" --port "$NS_PORT"
check "ns install alongside a LIVE main station: succeeds" "0" "$RC"
if tripwire_has_exact "launchctl bootout $MAIN_TARGET"; then
  bad "ns install BOOTED OUT THE MAIN STATION ($MAIN_TARGET) — tripwire: $(cat "$WORK/.tripwire")"
else
  ok "ns install never boots out the main station ($MAIN_TARGET)"
fi
if tripwire_has "launchctl bootstrap"; then
  ok "ns install did register a job (the case is not passing by doing nothing)"
else
  bad "ns install never bootstrapped anything — tripwire: $(cat "$WORK/.tripwire")"
fi
if tripwire_has "$NS_TARGET"; then
  ok "ns install addressed its OWN label ($NS_TARGET)"
else
  bad "ns install never addressed $NS_TARGET — tripwire: $(cat "$WORK/.tripwire")"
fi
# The MAIN instance's files must be untouched: the preinstalled fixture's marker
# binary is the witness. Under a lost NS_DASH this gets overwritten.
check "ns install leaves the MAIN instance's binaries untouched" "OLD-BINARY" "$(cat "$FAKEHOME/.officraft/bin/ocwarden" 2>/dev/null)"
if [[ -d "$FAKEHOME/.officraft-$NS" ]]; then
  ok "ns install created its own root (~/.officraft-$NS)"
else
  bad "ns install did NOT create ~/.officraft-$NS — it installed somewhere else"
fi
# Same positive control as case 9, on the NAMESPACED label — the one whose target
# is built from a different string. A poll hard-wired to the main label, or one
# that lost the gui domain, warns falsely here while case 9 stays green.
case "$OUT" in
  *"still does not know"*) bad "ns install: warns that launchd does not know $NS_TARGET, which it just registered:
$OUT" ;;
  *) ok "ns install: the registration poll confirms the NAMESPACED label" ;;
esac
if [[ -f "$FAKEHOME/Library/LaunchAgents/com.officraft.serve.$NS.plist" ]]; then
  ok "ns install wrote its own plist (com.officraft.serve.$NS.plist)"
else
  bad "ns install did not write com.officraft.serve.$NS.plist — plists: $(ls "$FAKEHOME/Library/LaunchAgents" 2>&1)"
fi

# 10b. the namespaced instance OWNS ITS CONFIG — written under its own root,
# carrying the namespace and the port it was asked for. Without this the migrate
# below would create the MAIN instance's database.
NS_CFG="$FAKEHOME/.officraft-$NS/server/oc.toml"
if [[ -f "$NS_CFG" ]]; then
  ok "ns install wrote its own config ($NS_CFG)"
  case "$(cat "$NS_CFG")" in *"namespace = \"$NS\""*) ok "ns config carries namespace = \"$NS\"";; *) bad "ns config lacks the namespace: $(cat "$NS_CFG")";; esac
  case "$(cat "$NS_CFG")" in *"port = $NS_PORT"*) ok "ns config carries port = $NS_PORT";; *) bad "ns config lacks port $NS_PORT: $(cat "$NS_CFG")";; esac
else
  bad "ns install wrote no config under its own root"
fi
# and the port GATE probed the namespaced port, not the main instance's 7755.
if tripwire_has "-iTCP:$NS_PORT"; then
  ok "ns install gated the port it was given ($NS_PORT)"
else
  bad "ns install did not probe $NS_PORT — tripwire: $(cat "$WORK/.tripwire")"
fi
case "$OUT" in *"http://127.0.0.1:$NS_PORT/"*) ok "ns install hands the operator a link on $NS_PORT";; *) bad "ns install setup link is not on $NS_PORT (output: '$OUT')";; esac

# 10c. a stray oc.toml in the CWD must NOT be able to redirect a run that asked
# for a specific instance by name. (The main instance deliberately still honours
# it — that is the relocation gate's job — but "I asked for instance lab" is not
# a question the file next to me gets to answer.)
reset_fixture fresh
printf '[server]\nport = 9999\n' > "$WORK/oc.toml"
run_install_ns absent "$MAIN_TARGET" --namespace "$NS" --port "$NS_PORT"
rm -f "$WORK/oc.toml"
check "ns install ignores a stray ./oc.toml: succeeds" "0" "$RC"
if tripwire_has "-iTCP:9999"; then
  bad "ns install was REDIRECTED by a stray ./oc.toml (probed 9999)"
else
  ok "ns install was not redirected by a stray ./oc.toml"
fi

# 10d. --port is REQUIRED with --namespace. Inheriting the default 7755 would
# either collide with the main instance at the port gate or, worse, look
# installed while fighting it for the socket.
reset_fixture fresh
run_install_ns absent "$MAIN_TARGET" --namespace "$NS"
check "--namespace without --port: refused (exit 2)" "2" "$RC"
case "$OUT" in *"requires --port"*) ok "--namespace without --port: says what is missing";; *) bad "--namespace without --port: unhelpful message ('$OUT')";; esac
if [[ -e "$FAKEHOME/.officraft" ]]; then bad "--namespace without --port still touched the MAIN root"; else ok "--namespace without --port touched nothing"; fi

# 10e. --port ALONE is refused too — otherwise it reads as "change the main
# instance's port", which is a relocation and not this flag's job.
reset_fixture fresh
run_install_ns absent "$MAIN_TARGET" --port "$NS_PORT"
check "--port without --namespace: refused (exit 2)" "2" "$RC"

# 10f. a MALFORMED namespace must be a hard error, never a silent fold back to
# the main instance's root and label — that fold is strictly worse than the error.
reset_fixture preinstalled
run_install_ns running "$MAIN_TARGET" --namespace "BAD_NS!" --port "$NS_PORT"
check "malformed --namespace: refused (exit 2)" "2" "$RC"
check "malformed --namespace: did NOT fall back to the main instance" "OLD-BINARY" "$(cat "$FAKEHOME/.officraft/bin/ocwarden" 2>/dev/null)"
check "malformed --namespace: never boots anything out" "no" "$(booted_out)"
case "$OUT" in *"[a-z0-9-]{1,16}"*) ok "malformed --namespace: names the accepted charset";; *) bad "malformed --namespace: does not name the charset ('$OUT')";; esac

# 10g. re-running the SAME instance with a DIFFERENT port is a relocation, not a
# reload: nothing may change until the operator says so deliberately.
reset_fixture fresh
run_install_ns absent "$MAIN_TARGET" --namespace "$NS" --port "$NS_PORT"
check "ns install (first run): succeeds" "0" "$RC"
printf 'MARKER-NS' > "$FAKEHOME/.officraft-$NS/bin/ocwarden"
run_install_ns absent "$MAIN_TARGET" --namespace "$NS" --port "7799" --force
check "ns re-install with a different port: refused (exit 1)" "1" "$RC"
# --force is passed so the existing-install prompt cannot be what stopped the
# run: the refusal under test has to be the PORT one.
check "ns port change: the refusal is not just the existing-install prompt" "MARKER-NS" "$(cat "$FAKEHOME/.officraft-$NS/bin/ocwarden")"
case "$OUT" in *"relocation, not a reload"*) ok "ns port change: explains it is a relocation";; *) bad "ns port change: unclear message ('$OUT')";; esac
case "$(cat "$NS_CFG")" in *"port = $NS_PORT"*) ok "ns port change: the existing config was NOT rewritten";; *) bad "ns port change: config was mutated to $(cat "$NS_CFG")";; esac

# 10h. THE EMPTY NAMESPACE CHANGES NOTHING — the sentinel. Every case above would
# also pass on a build that broke the MAIN path outright, so pin that a plain
# fresh install still lands on ~/.officraft / com.officraft.serve and creates NO
# namespaced root.
reset_fixture fresh
run_install absent
check "no --namespace: fresh install still succeeds" "0" "$RC"
if [[ -d "$FAKEHOME/.officraft" ]]; then ok "no --namespace: still installs to ~/.officraft"; else bad "no --namespace: ~/.officraft was not created"; fi
if compgen -G "$FAKEHOME/.officraft-*" >/dev/null; then bad "no --namespace: a namespaced root appeared out of nowhere"; else ok "no --namespace: no namespaced root is created"; fi
if [[ -f "$FAKEHOME/$PLIST_REL" ]]; then ok "no --namespace: still uses com.officraft.serve.plist"; else bad "no --namespace: default plist missing"; fi
if [[ -e "$FAKEHOME/.officraft/server/oc.toml" ]]; then bad "no --namespace: an instance config was invented for the MAIN instance"; else ok "no --namespace: no instance config is invented (main reads OC_CONFIG/./oc.toml as before)"; fi

# 10i. --foreground MUST CARRY THE INSTANCE'S CONFIG INTO THE DAEMON (T-164).
#
# WHY THIS CASE EXISTS. Everything 10a-10h asserts is about what the INSTALLER
# writes — the root, the config, the label, the plist, the port it gated. All of
# it was already correct while the flag was still broken, because the defect was
# one line later: the --foreground branch ran `exec ocserverd serve` with nothing
# in front of it. The daemon then resolved its own way ($OC_CONFIG → ./oc.toml,
# both empty) to the convention defaults and — measured on the official v0.5.356
# package, 2026-09-10 — created and MIGRATED ~/.officraft/server/data/officraft.db
# (goose 74 → 101 on one that already existed) and went for port 7755, while the
# line printed just above said "open http://127.0.0.1:<the port you asked for>/".
# A whole namespaced install, correct in every artifact on disk, serving the MAIN
# instance's database. Nothing in this file could see it: the stub ocserverd said
# `exit 0` and threw its arguments away.
#
# So the stub records itself now (see the package block at the top) and these
# cases read the tripwire. What they pin is not "the installer did the right
# thing" but "the thing the installer HANDED OFF TO was told where to go".
reset_fixture fresh
run_install_ns absent "$MAIN_TARGET" --namespace "$NS" --port "$NS_PORT" --foreground
check "ns --foreground: succeeds" "0" "$RC"
NS_CFG_ABS="$FAKEHOME/.officraft-$NS/server/oc.toml"
# POSITIVE CONTROL FIRST: migrate was already carrying the config before this
# change. If this line is absent the case never ran the installer at all, and the
# serve assertion below would be reading an empty tripwire — which looks exactly
# like a regression it did not observe.
if tripwire_has "ocserverd migrate OC_CONFIG=[$NS_CFG_ABS]"; then
  ok "ns --foreground: migrate carried the instance config (the case really ran)"
else
  bad "ns --foreground: no migrate in the tripwire — the run never got that far: $(cat "$WORK/.tripwire")"
fi
if tripwire_has "ocserverd serve OC_CONFIG=[$NS_CFG_ABS]"; then
  ok "ns --foreground: the exec carries the instance config"
else
  bad "ns --foreground: serve was exec'd WITHOUT the instance config — the daemon would silently use the DEFAULT database and port, not instance '$NS': $(cat "$WORK/.tripwire")"
fi
if tripwire_has "ocserverd serve OC_CONFIG=[]"; then
  bad "ns --foreground: serve was exec'd with an EMPTY OC_CONFIG — that is the defect itself"
else
  ok "ns --foreground: no config-less serve was exec'd"
fi
# The operator is also TOLD how to restart it. That line used to print a bare
# `ocserverd serve`, which walks straight back into the same fallback — a trap
# the installer hands over in writing.
case "$OUT" in
  *"OC_CONFIG=$NS_CFG_ABS"*) ok "ns --foreground: the restart hint carries the config too" ;;
  *) bad "ns --foreground: the restart hint tells the operator to run a bare serve, which re-enters the fallback ('$OUT')" ;;
esac

# 10j. THE NEGATIVE CONTROL, and it is the half that decides the shape of the
# fix. A config-less `ocserverd serve` booting on the convention defaults is a
# DOCUMENTED capability — oc.toml.example: "The file may be entirely ABSENT:
# every key has a convention default, so a bare `ocserverd serve` boots with zero
# setup", repeated in server/ocserverd/config.go. So --foreground WITHOUT a
# namespace and without any config file must keep exec-ing exactly what it always
# did. "Refuse when nothing resolves" would have deleted that, and 10i alone
# would still have passed.
reset_fixture fresh
run_install absent --foreground
check "plain --foreground: succeeds" "0" "$RC"
if tripwire_has "ocserverd serve OC_CONFIG=[]"; then
  ok "plain --foreground: still exec's a config-less serve (zero-setup boot preserved)"
else
  bad "plain --foreground: the config-less serve is gone — zero-setup boot was broken: $(cat "$WORK/.tripwire")"
fi
if compgen -G "$FAKEHOME/.officraft-*" >/dev/null; then bad "plain --foreground: a namespaced root appeared"; else ok "plain --foreground: no namespaced root"; fi

# 10k. NO OPERATOR-FACING HINT MAY PRINT A BARE `ocserverd serve` (T-164).
#
# This one reads the SOURCE, not a run, and that is deliberate: the three other
# places that hand the operator a start command are all on FAILURE paths (port
# already held, launchctl bootstrap failed, bootstrap registered nothing). Each
# needs its own fixture to reach, and the reader there is already in trouble and
# has every reason to paste what the tool just told them — on a namespaced
# install a bare serve walks straight into the fallback this ticket closes, on
# the MAIN instance's database. A behavioural case per failure path would be the
# better test; this is the one that exists, and it is honest about being a
# source-text check: it cannot see a hint built some other way.
BARE_HINTS="$(grep -c 'echo "\[install\].*\$BIN_DIR/ocserverd serve' "$SCRIPT" || true)"
check "no operator-facing hint prints a bare 'ocserverd serve' (they must derive from \$SERVE_CMD)" "0" "$BARE_HINTS"
# Positive control for the line above: the pattern must be able to match at all,
# otherwise a typo in it reports 0 for a file full of bare hints.
SERVE_CMD_USES="$(grep -c 'SERVE_CMD' "$SCRIPT" || true)"
if [[ "$SERVE_CMD_USES" -ge 4 ]]; then
  ok "the hint sites really do go through \$SERVE_CMD ($SERVE_CMD_USES references)"
else
  bad "only $SERVE_CMD_USES \$SERVE_CMD references in install.sh — the check above is reading a file that does not use it"
fi

# ── 10. a reinstall is not a relocation, on EVERY macOS ─────────────────────
# The plist this suite writes has no EnvironmentVariables at all, so asking it
# for OC_CONFIG is the ordinary "key is absent" case. macOS 15's plutil answers
# that on STDOUT with a non-zero rc, so reading the OUTPUT rather than the rc
# captured the error TEXT as the old config — and install.sh then compared that
# text against the real one, called the difference a relocation, and refused
# every re-install over a live service on those machines. The port printed in
# its own refusal was identical before and after, which is what a relocation
# never looks like.
#
# The stub is what makes this case mean anything on a newer host: without it a
# macOS 26 runner takes the stderr path, passes no matter how plist_env is
# written, and the fix could be reverted with CI still green.
reset_fixture preinstalled
PLUTIL_LOG="$WORK/.plutil-calls"; : > "$PLUTIL_LOG"
SHIM_PLUTIL_STDOUT_ERR=1 SHIM_PLUTIL_LOG="$PLUTIL_LOG" run_install running --force --restart-live
# Positive control FIRST: if install.sh never consulted plutil through the stub,
# everything below would pass for the wrong reason.
# IT MUST NAME THE EXTRACT. An earlier version asked only "is the log non-empty",
# and the log is filled by calls that have nothing to do with this case —
# plist_program's ProgramArguments.0, and the `plutil -lint` on the rendered
# plist. Measured: with that control, replacing plist_env's whole body with
# `return 0` — deleting config inheritance and half the relocation gate — left
# this suite at 80 ok / 0 failed. A control that a total deletion satisfies is
# not a control.
case "$(cat "$PLUTIL_LOG")" in
  *"EnvironmentVariables.OC_CONFIG"*)
    ok "macOS-15 plutil: install.sh actually asked for EnvironmentVariables.OC_CONFIG (case is live)" ;;
  *)
    bad "macOS-15 plutil: no EnvironmentVariables.OC_CONFIG extract in the log — plist_env was never reached, so this case proves nothing. Log: $(cat "$PLUTIL_LOG")" ;;
esac
check "macOS-15 plutil: a re-install still proceeds" "0" "$RC"
case "$OUT" in
  *"would MOVE the running"*) bad "macOS-15 plutil: re-install misread as a relocation (gate fired on an absent-key error string)" ;;
  *) ok "macOS-15 plutil: re-install is not misread as a relocation" ;;
esac
check "macOS-15 plutil: the old job is booted out" "yes" "$(booted_out)"

# ── 10b. plist_env must return the VALUE, not merely "not an error" ─────────
# Case 10 only ever asks plist_env about a key that is ABSENT, so it constrains
# one direction: do not hand back an error string. Nothing in this tree exercised
# the other direction — the branch where the key IS present and its value is what
# the installer then acts on. `grep -rn "carrying over" bin/tests/` had zero hits
# before this case, which is why `plist_env() { return 0; }` was a silent no-op
# against the whole suite.
#
# The observable is config INHERITANCE: a run that resolves no config of its own,
# over a job whose plist carries OC_CONFIG, must adopt that config AND re-derive
# the port from it. Asserting the PORT is the point — it can only come from
# reading the file plist_env pointed at, so an empty return cannot fake it.
reset_fixture preinstalled
INHERIT_CFG="$FAKEHOME/.officraft/server/inherited.toml"
printf '[server]\nport = 7799\n' > "$INHERIT_CFG"
cat > "$FAKEHOME/$PLIST_REL" <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.officraft.serve</string>
  <key>ProgramArguments</key>
  <array>
    <string>$FAKEHOME/.officraft/bin/ocserverd</string>
    <string>serve</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict><key>OC_CONFIG</key><string>$INHERIT_CFG</string></dict>
  <key>RunAtLoad</key><true/>
</dict>
</plist>
PL
PLUTIL_LOG="$WORK/.plutil-calls"; : > "$PLUTIL_LOG"
SHIM_PLUTIL_STDOUT_ERR=1 SHIM_PLUTIL_LOG="$PLUTIL_LOG" run_install loaded --force
case "$(cat "$PLUTIL_LOG")" in
  *"EnvironmentVariables.OC_CONFIG"*)
    ok "config inheritance: install.sh asked for EnvironmentVariables.OC_CONFIG (case is live)" ;;
  *)
    bad "config inheritance: no EnvironmentVariables.OC_CONFIG extract in the log — this case proves nothing" ;;
esac
check "config inheritance: the re-install proceeds" "0" "$RC"
case "$OUT" in
  *"carrying over the existing service's config: $INHERIT_CFG"*)
    ok "config inheritance: the existing job's OC_CONFIG is adopted, not dropped" ;;
  *)
    bad "config inheritance: expected 'carrying over the existing service's config: $INHERIT_CFG' — plist_env returned nothing usable ($OUT)" ;;
esac
case "$OUT" in
  *"(port 7799)"*) ok "config inheritance: the port is re-derived FROM that config (7799), so the value really was read" ;;
  *) bad "config inheritance: the inherited config's port 7799 never reached the port gate — an empty plist_env would look exactly like this ($OUT)" ;;
esac
case "$OUT" in
  *"would MOVE the running"*) bad "config inheritance: adopting the old config was misread as a relocation" ;;
  *) ok "config inheritance: adopting the old config is not a relocation" ;;
esac
if tripwire_has "lsof -nP -iTCP:7799"; then
  ok "config inheritance: the port gate probed 7799, the INHERITED port"
else
  bad "config inheritance: the port gate never probed 7799 — it checked a port the inherited config does not name"
fi

# ── 11. the SAME defect on the other plutil reader (T-4358) ─────────────────
# plist_env got the rc-decides treatment in T-5831; plist_program was left on
# `2>/dev/null || true`, which on macOS 15 captures plutil's error TEXT and hands
# it back as the value. The ownership gate then printed that error line in the
# field the operator is told to read a program path out of — the one fact the
# refusal exists to convey. Both now go through plist_raw, so this case is what
# stops the two from drifting apart again.
#
# The fixture is a plist with NO ProgramArguments at all: the ordinary
# "key is absent" answer, which is the shape the defect needs. As in case 10, the
# stub is what gives this teeth on a macOS 26 runner.
reset_fixture preinstalled
cat > "$FAKEHOME/$PLIST_REL" <<'PL'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.officraft.serve</string>
  <key>RunAtLoad</key><true/>
</dict>
</plist>
PL
PLUTIL_LOG="$WORK/.plutil-calls"; : > "$PLUTIL_LOG"
SHIM_PLUTIL_STDOUT_ERR=1 SHIM_PLUTIL_LOG="$PLUTIL_LOG" run_install absent --force
# Same rule as case 10's control: name the extract this case is about. "The log
# is non-empty" is satisfied by plutil -lint alone.
case "$(cat "$PLUTIL_LOG")" in
  *"ProgramArguments.0"*)
    ok "macOS-15 plutil / plist_program: install.sh asked for ProgramArguments.0 (case is live)" ;;
  *)
    bad "macOS-15 plutil / plist_program: no ProgramArguments.0 extract in the log — this case proves nothing. Log: $(cat "$PLUTIL_LOG")" ;;
esac
check "macOS-15 plutil / plist_program: an unidentifiable label is still refused" "1" "$RC"
case "$OUT" in
  *"Could not extract value"*)
    bad "macOS-15 plutil / plist_program: plutil's ERROR TEXT was reported as the program name" ;;
  *) ok "macOS-15 plutil / plist_program: the error text never reaches the report" ;;
esac
case "$OUT" in
  *"program: <unreadable>"*) ok "macOS-15 plutil / plist_program: an absent key reads as <unreadable>" ;;
  *) bad "macOS-15 plutil / plist_program: expected 'program: <unreadable>' in the refusal ($OUT)" ;;
esac
# The uninstall side shares the helper and the same field; it went blank rather
# than garbled before, which is the same failure to say what was found.
# (No `unset` after these: a prefix assignment on a FUNCTION call does not
# persist in bash — measured on 3.2.57 and 5.3 — so the variable is already gone.)
SHIM_PLUTIL_STDOUT_ERR=1 run_install absent --uninstall --dry-run
case "$OUT" in
  *"Could not extract value"*)
    bad "macOS-15 plutil / --uninstall: plutil's ERROR TEXT was reported as the program name" ;;
  *"program:  <unreadable>"*) ok "macOS-15 plutil / --uninstall: an absent key reads as <unreadable>" ;;
  *) bad "macOS-15 plutil / --uninstall: expected 'program:  <unreadable>' in the refusal ($OUT)" ;;
esac

# ═══════════════════════════════════════════════════════════════════════════
# THE sha256 GATE ON THE STANDALONE (curl | bash) PATH
# ═══════════════════════════════════════════════════════════════════════════
# WHAT WAS UNGUARDED. bin/install.sh's bootstrap half downloads a release tarball
# and checksums.txt and has TWO abort branches: no entry for the asset, and a
# digest that does not match. Nothing in bin/tests/ drove either of them —
# release-guard.sh is about whether the RELEASE SIDE writes a correct
# checksums.txt, which is a different claim from whether the INSTALL SIDE stops
# when the comparison fails. A supply-chain check nobody exercises is a check
# whose failure path has never once been observed to abort.
#
# WHY THESE ASSERTIONS LIVE HERE AND NOT IN bin/install.sh. A self-check written
# inside the script it guards is satisfied by whoever deletes the gate and leaves
# a gate-shaped line behind; this repo has already been bitten by a marker living
# in the file it was supposed to protect. And a script cannot testify that it
# aborts — only a caller that watches it abort can. So the oracle is out here.
#
# 🔴 WHAT IS PINNED, AND HOW — stated per case, because the previous version of
# this comment claimed "no branch is matched as text, keep refusing and you pass,
# stop refusing and you redden" and that was false in every direction.
#   · The SPINE of each case is behavioural: the exit status, and whether the
#     packaged installer was reached. Those hold whatever the wording is.
#   · The MESSAGE is also pinned, deliberately. On a `curl … | bash` install the
#     refusal text is the entire user interface of this gate — "it aborted" with
#     an unreadable reason is a support ticket, not a protection. Consequence,
#     stated plainly so nobody is surprised by it: REWORDING A REFUSAL REDDENS
#     HERE EVEN THOUGH THE BEHAVIOUR IS INTACT (verified: changing only the
#     no-entry wording gave 96 ok / 1 failed while the installer still exited 1).
#     That is a chosen cost, and the fix is to update this file in the same edit.
#   · NOT COVERED, known: an escape hatch keyed on an environment variable this
#     harness does not set (`[[ -n "$OC_SKIP_SHA256" ]] && …`) is invisible here —
#     these runs use `env -i` with a fixed list, so an invented name is simply
#     never present. Conditionalising the gate on properties of the DOWNLOADED
#     FILES is covered, because the cases below choose those bytes.
#
# HERMETIC. curl is replaced by a file-copying stub, so no network is touched and
# the suite chooses byte-for-byte what "the release" contains. The delegated
# packaged installer is a TRIPWIRE: if it is ever reached, the run left the
# verification behind and started installing, which is the outcome both branches
# exist to prevent.
echo
echo "install.sh sha256 gate on the standalone (curl|bash) path — hermetic"

SHA_ROOT="$WORK/sha256"
SHA_SERVE="$SHA_ROOT/serve"       # what the fake release "publishes"
SHA_BIN="$SHA_ROOT/bin"           # curl + preflight tools, ahead of SHIMDIR
SHA_HOME="$SHA_ROOT/home"
SHA_ALONE="$SHA_ROOT/standalone"  # install.sh with NO siblings ⇒ IN_PACKAGE=0
mkdir -p "$SHA_SERVE" "$SHA_BIN" "$SHA_ALONE"
cp "$SCRIPT" "$SHA_ALONE/install.sh"

# curl: serves $SHA_SERVE by basename and logs every request. Unknown asset ⇒ 22,
# the same rc a real `curl -f` gives on a 404, so a missing file cannot be
# mistaken for a served one.
cat > "$SHA_BIN/curl" <<'SH'
#!/usr/bin/env bash
echo "curl $*" >> "$SHA_CURL_LOG"
url=""; out=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -*) shift ;;
    *)  url="$1"; shift ;;
  esac
done
src="$SHA_SERVE/${url##*/}"
[[ -f "$src" ]] || exit 22
if [[ -n "$out" ]]; then cp "$src" "$out"; else cat "$src"; fi
exit 0
SH
printf '#!/usr/bin/env bash\nexit 0\n'                       > "$SHA_BIN/tmux"
printf '#!/usr/bin/env bash\necho "9.9.9 (Claude Code)"\n'   > "$SHA_BIN/claude"
chmod +x "$SHA_BIN/curl" "$SHA_BIN/tmux" "$SHA_BIN/claude"

SHA_TAG="v9.9.9"
SHA_ASSET="officraft-$SHA_TAG-darwin-arm64.tar.gz"

# The tarball. Its packaged install.sh is the tripwire — reaching it means the
# run got PAST verification and began installing.
SHA_PKGSRC="$SHA_ROOT/pkgsrc/officraft-$SHA_TAG-darwin-arm64"
mkdir -p "$SHA_PKGSRC"
cat > "$SHA_PKGSRC/install.sh" <<'SH'
#!/usr/bin/env bash
echo "DELEGATED" >> "$SHA_DELEGATED"
echo "[packaged-installer-stub] reached"
exit 0
SH
(cd "$SHA_ROOT/pkgsrc" && tar -czf "$SHA_SERVE/$SHA_ASSET" "officraft-$SHA_TAG-darwin-arm64")
SHA_GOOD="$(shasum -a 256 "$SHA_SERVE/$SHA_ASSET" | cut -d' ' -f1)"

# sha_run <checksums.txt body> — drives the real bootstrap path end to end. The
# literal ABSENT publishes no checksums.txt at all, so curl 404s on it.
# Sets SHA_RC, SHA_OUT, SHA_DELEGATED_HITS, SHA_CURL_CALLS.
sha_run() {
  if [[ "$1" == "ABSENT" ]]; then
    rm -f "$SHA_SERVE/checksums.txt"
  else
    printf '%s' "$1" > "$SHA_SERVE/checksums.txt"
  fi
  rm -rf "$SHA_HOME"; mkdir -p "$SHA_HOME/Library/LaunchAgents"
  : > "$SHA_ROOT/.curl.log"; : > "$SHA_ROOT/.delegated"
  SHA_OUT="$(cd "$SHA_ROOT" && env -i \
    PATH="$SHA_BIN:$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$SHA_HOME" SHIM_JOB=absent SHIM_TRIPWIRE="$SHA_ROOT/.tripwire" \
    SHIM_STATE="$SHA_ROOT" SHIM_EXPECT_TARGET="$EXPECT_TARGET" \
    SHA_SERVE="$SHA_SERVE" SHA_CURL_LOG="$SHA_ROOT/.curl.log" \
    SHA_DELEGATED="$SHA_ROOT/.delegated" \
    OC_INSTALL_TAG="$SHA_TAG" OC_INSTALL_BASE_URL="https://example.invalid/rel" \
    bash "$SHA_ALONE/install.sh" </dev/null 2>&1)"
  SHA_RC=$?
  SHA_DELEGATED_HITS="$(grep -c . "$SHA_ROOT/.delegated" || true)"
  SHA_CURL_CALLS="$(grep -c . "$SHA_ROOT/.curl.log" || true)"
}

# ── SENTINEL FIRST. Without it "everything aborts" scores a perfect run, and a
# gate that refuses every download is not a gate, it is an outage. This is also
# the control that proves the harness can actually get a run all the way through
# verification: the two refusals below mean nothing if nothing ever passes.
sha_run "$SHA_GOOD  $SHA_ASSET"
check "sha256 sentinel: a matching digest is accepted and the run proceeds" "0" "$SHA_RC"
case "$SHA_OUT" in
  *"sha256 verified OK"*) ok "sha256 sentinel: verification reported OK" ;;
  *) bad "sha256 sentinel: no 'sha256 verified OK' — the harness never reached the gate:
$SHA_OUT" ;;
esac
check "sha256 sentinel: the packaged installer WAS reached (the tripwire can fire at all)" \
  "1" "$SHA_DELEGATED_HITS"

# ── BRANCH 1: checksums.txt carries no entry for this asset.
# The digest of a DIFFERENT file, so the file is not empty and a `grep` that
# stopped filtering by name would find something to compare against.
sha_run "$SHA_GOOD  some-other-file.tar.gz"
check "sha256 no-entry: an asset absent from checksums.txt ABORTS" "1" "$SHA_RC"
case "$SHA_OUT" in
  *"no entry for $SHA_ASSET"*) ok "sha256 no-entry: the refusal names the asset it could not find" ;;
  *) bad "sha256 no-entry: the refusal did not name the missing asset:
$SHA_OUT" ;;
esac
check "sha256 no-entry: NOTHING was installed — the packaged installer was never reached" \
  "0" "$SHA_DELEGATED_HITS"

# ── BRANCH 2: an entry exists and does not match. This is the tampered /
# corrupted download, and it is the branch a "verify then ignore the result"
# rewrite silently deletes while leaving branch 1 intact.
sha_run "0000000000000000000000000000000000000000000000000000000000000000  $SHA_ASSET"
check "sha256 mismatch: a digest that does not match ABORTS" "1" "$SHA_RC"
case "$SHA_OUT" in
  *"verification FAILED"*) ok "sha256 mismatch: the refusal says verification FAILED" ;;
  *) bad "sha256 mismatch: no 'verification FAILED' in the output:
$SHA_OUT" ;;
esac
case "$SHA_OUT" in
  *"NOTHING was installed"*) ok "sha256 mismatch: the refusal states nothing was installed" ;;
  *) bad "sha256 mismatch: the refusal does not tell the user nothing was installed:
$SHA_OUT" ;;
esac
check "sha256 mismatch: NOTHING was installed — the packaged installer was never reached" \
  "0" "$SHA_DELEGATED_HITS"
# Positive control on the harness: a refusal reached by never downloading at all
# (a broken stub, a wrong URL) would satisfy every assertion above for the wrong
# reason. Both files must really have been fetched.
check "sha256pc the mismatch run really downloaded the asset AND checksums.txt (2 curl calls)" \
  "2" "$SHA_CURL_CALLS"

# ── BRANCH 3: checksums.txt is EMPTY. Behaviourally this is the same refusal as
# branch 1, so on its own it looks redundant — it is not. The three cases above
# all hand the gate a NON-EMPTY list, so wrapping the whole verification in
# `if [[ -s "$TMP/checksums.txt" ]]; then … fi` leaves every one of them passing
# while an empty list installs unverified (measured: 97 ok / 0 failed, rc=0).
# Whoever publishes an empty checksums.txt by accident is precisely the case a
# release-side bug produces, and it is the one an attacker can also produce by
# truncating a response. Emptiness must not be a way past the gate.
sha_run ""
check "sha256 empty list: an EMPTY checksums.txt ABORTS — an unverifiable download is not an approved one" \
  "1" "$SHA_RC"
check "sha256 empty list: NOTHING was installed — the packaged installer was never reached" \
  "0" "$SHA_DELEGATED_HITS"

# ── BRANCH 4: checksums.txt is not published at all. Same reasoning one step
# earlier: skipping verification when the list cannot be FETCHED is the other
# half of "no list means no gate", and a 404 is what a half-finished release
# actually looks like.
sha_run "ABSENT"
check "sha256 absent list: a checksums.txt that 404s ABORTS" "1" "$SHA_RC"
check "sha256 absent list: NOTHING was installed — the packaged installer was never reached" \
  "0" "$SHA_DELEGATED_HITS"

echo "install-guard tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
