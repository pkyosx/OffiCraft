#!/usr/bin/env bash
# bin/tests/install-preflight-guard.sh — HERMETIC unit tests for bin/install.sh's
# TOOL PREFLIGHT (T-7f38).
#
# THE DEFECT UNDER TEST
# ---------------------
# Every member is a runtime session running inside tmux: `cli/ocwarden/spawn.go`
# builds a codex launch line (:930) or a claude one (:934) and hands EITHER to the
# same `tmuxNewSession` (:950), which fails as `spawn_exec_failed`. tmux therefore
# has no fallback and no runtime exemption. But install.sh never mentioned tmux at
# all: on a machine without it the install went GREEN, the cockpit opened, a hire
# was accepted — and the member then sat at 「waking」 forever while nothing in the
# installer, the server, or the cockpit ever named the cause. The owner's report
# was literally "always waking but no progress, it turns out there is no tmux
# installed".
#
# The RUNTIME half is deliberately weaker than tmux: claude OR codex satisfies it,
# because `ocwarden install`'s own gate (cli/ocwarden/install.go:1057-1061,
# runtime_bin_unresolved) refuses only when NEITHER resolves. A codex-only machine
# is a supported configuration and must install cleanly — case 5b pins that.
#
# WHAT THIS SUITE PINS
# --------------------
#   (a) a machine missing a required tool is REFUSED, and refused BEFORE anything
#       is written (no ~/.officraft, no launchd plist);
#   (b) the refusal names the tool AND the command that installs it, and offers
#       NO bypass — a gate with a documented escape hatch is a gate nobody hits;
#   (c) machines that are FINE install exactly as before — both the claude-shaped
#       one and the codex-only one. This is the sentinel every fail-closed gate
#       needs: "refuses when it should" is worthless without "does not refuse when
#       it should not";
#   (d) THE COUNTERFACTUAL: the same missing-tool run against a MUTANT install.sh
#       with the `oc_preflight` call sites deleted must SUCCEED. That mutant is
#       the pre-fix installer, so this case simultaneously reproduces the
#       original defect and proves case (a) is testing the check rather than
#       passing for some unrelated reason.
#
# WHY THE SHIM IS SHAPED THIS WAY
# -------------------------------
# Same discipline as install-guard.sh / install-claude-stamp.sh: uname, launchctl
# and lsof are replaced on PATH so nothing in the real launchd domain is read or
# written, HOME is redirected into a temp dir, and the package binaries are
# stubs. tmux and claude are stubbed too and NEVER invoked — the preflight only
# asks whether they resolve, so a stub is a faithful stand-in, and using the real
# ones would make the result depend on what the CI host happens to have.
#
# One honest limit: the runtime half of the preflight mirrors ocwarden's own
# resolution order, which ends in ABSOLUTE paths (/opt/homebrew/bin, /usr/local/bin
# for both claude and codex) that no PATH shim can hide. On a host carrying any of
# them, "no runtime anywhere" is not constructible and those cases SKIP rather than
# pretend. The tmux cases have no such hole (bare name, PATH only), which is why
# the counterfactual is built on tmux.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/../install.sh"
[[ -f "$SCRIPT" ]] || { echo "FATAL: install.sh not found at $SCRIPT" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi; }

WORK="$(mktemp -d -t oc-install-preflight.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

SHIMDIR="$WORK/shim"       # always on PATH: uname/launchctl/lsof
TOOLDIR="$WORK/tools"      # tmux + claude live here, so a case can drop them
PKG="$WORK/pkg"            # the "release tarball": install.sh + three binaries
MUTANT="$WORK/mutant"      # the same package with the preflight CALL removed
FAKEHOME="$WORK/home"
mkdir -p "$SHIMDIR" "$TOOLDIR" "$PKG" "$MUTANT"

cp "$SCRIPT" "$PKG/install.sh"
for b in ocserverd ocwarden ocagent officraft; do
  printf '#!/usr/bin/env bash\nexit 0\n' > "$PKG/$b"
  chmod +x "$PKG/$b"
done

# ── the mutant: identical package, preflight CALL SITES deleted ──────────────
# Only the calls are removed, not the function — that is the smallest edit that
# turns the gate off, and it is exactly what the file looked like before T-7f38.
cp "$PKG"/* "$MUTANT/"
grep -vE '^[[:space:]]*oc_preflight([[:space:]]|$)' "$PKG/install.sh" > "$MUTANT/install.sh"
MUTANT_DELETED=$(( $(grep -cE '^[[:space:]]*oc_preflight([[:space:]]|$)' "$PKG/install.sh") ))
if [[ "$MUTANT_DELETED" -ge 1 ]]; then
  ok "mutant construction: $MUTANT_DELETED oc_preflight call site(s) removed from the copy"
else
  bad "mutant construction: found NO oc_preflight call site to remove — this suite is not testing what it claims"
fi
if bash -n "$MUTANT/install.sh" 2>/dev/null; then
  ok "mutant construction: the mutated install.sh still parses (the mutation is the gate, not a syntax break)"
else
  bad "mutant construction: the mutated install.sh no longer parses — its 'success' would prove nothing"
fi

cat > "$SHIMDIR/uname" <<'SH'
#!/usr/bin/env bash
case "${1:-}" in
  -m) echo "${SHIM_UNAME_M:-arm64}" ;;
  *)  echo "${SHIM_UNAME_S:-Darwin}" ;;
esac
SH

cat > "$SHIMDIR/launchctl" <<'SH'
#!/usr/bin/env bash
case "${1:-}" in
  print) exit 1 ;;
  bootstrap) touch "$SHIM_STATE/.bootstrapped"; exit 0 ;;
esac
exit 0
SH

cat > "$SHIMDIR/lsof" <<'SH'
#!/usr/bin/env bash
QPORT=""
for a in "$@"; do
  case "$a" in -iTCP:*) QPORT="${a#-iTCP:}" ;; esac
done
if [[ -f "$SHIM_STATE/.bootstrapped" ]]; then
  echo "ocserverd 4242 tester 5u IPv4 0x0 0t0 TCP 127.0.0.1:${QPORT:-7755} (LISTEN)"
  exit 0
fi
exit 1
SH

printf '#!/usr/bin/env bash\nexit 0\n' > "$TOOLDIR/tmux"
printf '#!/usr/bin/env bash\necho "9.9.9 (Claude Code)"\nexit 0\n' > "$TOOLDIR/claude"
printf '#!/usr/bin/env bash\necho "codex-cli 9.9.9"\nexit 0\n' > "$TOOLDIR/codex"
# curl: a TRIPWIRE, not a stub with an opinion. `env -i` clears the environment,
# it does not remove network access — measured: with the gate deleted, the
# standalone path really did reach api.github.com, download the release tarball,
# verify its sha256 and run the packaged installer. A suite that calls itself
# HERMETIC cannot leave that one regression away, so curl is replaced by something
# that can only record the attempt and fail the way an unreachable host does.
cat > "$SHIMDIR/curl" <<'SH'
#!/usr/bin/env bash
echo "curl $*" >> "$SHIM_STATE/.netwire"
exit 6
SH

chmod +x "$SHIMDIR"/uname "$SHIMDIR"/launchctl "$SHIMDIR"/lsof "$SHIMDIR"/curl \
         "$TOOLDIR"/tmux "$TOOLDIR"/claude "$TOOLDIR"/codex

PLIST_REL="Library/LaunchAgents/com.officraft.serve.plist"

reset_fixture() {
  rm -rf "$FAKEHOME"
  mkdir -p "$FAKEHOME/Library/LaunchAgents"
  rm -f "$WORK/.bootstrapped"
}

# run_install <pkgdir> <tools…> — a fresh non-interactive install (the
# `curl | bash` shape) with ONLY the named tools resolvable. Each tool is linked
# into a per-run dir, so "missing" means missing from PATH entirely rather than
# present-but-broken (which is a different failure with a different message).
run_install() {
  local pkgdir="$1"; shift
  local rundir="$WORK/run" t
  rm -rf "$rundir"; mkdir -p "$rundir"
  for t in "$@"; do ln -s "$TOOLDIR/$t" "$rundir/$t"; done
  OUT="$(cd "$WORK" && env -i \
    PATH="$rundir:$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$FAKEHOME" SHIM_STATE="$WORK" \
    bash "$pkgdir/install.sh" </dev/null 2>&1)"
  RC=$?
}

wrote_anything() { # did this run touch the machine at all?
  [[ -e "$FAKEHOME/.officraft" || -e "$FAKEHOME/$PLIST_REL" ]]
}

# "No runtime anywhere" is only constructible when the host carries neither an
# absolute claude nor an absolute codex (see the header note).
HAS_ABS_RUNTIME=0
for _abs in /opt/homebrew/bin/claude /usr/local/bin/claude \
            /opt/homebrew/bin/codex  /usr/local/bin/codex; do
  [[ -x "$_abs" ]] && HAS_ABS_RUNTIME=1
done

echo "install.sh tool preflight — hermetic tests"

# ── 1. tmux missing → refused, and refused before anything is written ────────
reset_fixture
run_install "$PKG" claude
check "no tmux: the install is refused" "1" "$RC"
if wrote_anything; then
  bad "no tmux: the machine was touched anyway (~/.officraft or the plist exists) — the gate fired too late"
else
  ok "no tmux: NOTHING was written (no ~/.officraft, no launchd plist)"
fi
case "$OUT" in
  *"tmux"*) ok "no tmux: the refusal names tmux" ;;
  *) bad "no tmux: the refusal never names tmux:
$OUT" ;;
esac
case "$OUT" in
  *"brew install tmux"*) ok "no tmux: the refusal says exactly how to fix it" ;;
  *) bad "no tmux: the refusal does not say how to fix it:
$OUT" ;;
esac
# The failure the owner actually hit is invisible, so the refusal has to connect
# the missing tool to the SYMPTOM — otherwise it is one more line nobody reads.
# This asserted *"waking"*|*"tmux"* for one round, which the assertion two lines
# above had already guaranteed: a tautology wearing the name of a real check.
case "$OUT" in
  *"waking"*) ok "no tmux: the refusal names the symptom the owner actually saw (「waking」)" ;;
  *) bad "no tmux: the refusal never connects the missing tool to the 「waking」 symptom:
$OUT" ;;
esac
# NO BYPASS. A gate that advertises its own escape hatch gets escaped, and the
# escapee lands back in exactly the silent 「waking」 state this fixes.
#
# GUARDED ON RC. Checking only for the ABSENCE of a string is conditionally
# vacuous: on a run that never refuses at all (the mutant below, or a future
# regression that turns the gate off) there is no message, so all six "the string
# is absent" checks pass and would go on certifying a gate that no longer exists.
# Assert them only where there IS a refusal to inspect.
if [[ "$RC" != 1 ]]; then
  bad "no bypass: cannot check the refusal text — this run did not refuse (rc=$RC)"
else
  for esc in "--skip" "--no-preflight" "OC_SKIP" "bypass" "ignore this" "at your own risk"; do
    case "$OUT" in
      *"$esc"*) bad "no tmux: the refusal advertises a bypass ('$esc') — it must not teach anyone around itself" ;;
      *) ok "no tmux: the refusal offers no bypass ('$esc' absent)" ;;
    esac
  done
fi

# ── 2. THE COUNTERFACTUAL: same run, preflight call removed → it installs ────
# If this went red, case 1 above would be proving nothing: the refusal could be
# coming from something else entirely. Green here means the pre-T-7f38 installer
# really did sail through a tmux-less machine — the defect, reproduced.
reset_fixture
run_install "$MUTANT" claude
check "MUTANT (no preflight call), no tmux: the install succeeds — this is the defect" "0" "$RC"
if [[ -f "$FAKEHOME/$PLIST_REL" ]]; then
  ok "MUTANT, no tmux: a serve plist was written — green install on a machine whose members can never start"
else
  bad "MUTANT, no tmux: no plist was written, so case 1's refusal is NOT attributable to the preflight:
$OUT"
fi

# ── 3. NO runtime at all (neither claude nor codex) → refused the same way ──
if [[ "$HAS_ABS_RUNTIME" == 1 ]]; then
  echo "  skip — an absolute claude/codex exists on this host; 'no runtime anywhere' is not constructible"
else
  reset_fixture
  run_install "$PKG" tmux
  check "no runtime: the install is refused" "1" "$RC"
  if wrote_anything; then
    bad "no runtime: the machine was touched anyway — the gate fired too late"
  else
    ok "no runtime: NOTHING was written"
  fi
  # BOTH ways out have to be on offer. Naming only claude would send a codex user
  # to install a runtime they deliberately did not choose.
  case "$OUT" in
    *"curl -fsSL https://claude.ai/install.sh | bash"*) ok "no runtime: the refusal gives the Claude Code install command" ;;
    *) bad "no runtime: the refusal does not say how to install Claude Code:
$OUT" ;;
  esac
  case "$OUT" in
    *"curl -fsSL https://chatgpt.com/codex/install.sh | sh"*) ok "no runtime: the refusal gives the Codex install command as the other way out" ;;
    *) bad "no runtime: the refusal does not say how to install Codex — a codex user is told to install the wrong thing:
$OUT" ;;
  esac
fi

# ── 4. tmux + NO runtime → both problems reported in one pass ───────────────
# Reporting one, exiting, and reporting the next only on the re-run is how a
# five-minute fix becomes three round trips.
if [[ "$HAS_ABS_RUNTIME" == 1 ]]; then
  echo "  skip — cannot construct 'no runtime' on this host (see above)"
else
  reset_fixture
  run_install "$PKG"
  check "neither tmux nor a runtime: the install is refused" "1" "$RC"
  if [[ "$OUT" == *"tmux"* && "$OUT" == *"claude"* && "$OUT" == *"codex"* ]]; then
    ok "neither: tmux AND the runtime choice are both named in one refusal"
  else
    bad "neither: the refusal does not name both problems:
$OUT"
  fi
fi

# ── 5. THE SENTINEL: a machine that is fine must NOT be blocked ────────────
# The whole point of a fail-closed gate is that it is invisible when the machine
# is fine. This repo has already shipped a tightening that reddened nothing in CI
# while killing four working paths, so the "does not misfire" half is asserted at
# the same rank as the "does refuse" half, not left to hand-waving.
reset_fixture
run_install "$PKG" tmux claude
check "tmux + claude: the install succeeds" "0" "$RC"
if [[ -f "$FAKEHOME/$PLIST_REL" ]]; then
  ok "tmux + claude: the serve plist was rendered (the install really ran, not just exited 0)"
else
  bad "tmux + claude: no serve plist — the install did not complete:
$OUT"
fi
case "$OUT" in
  *"FATAL"*) bad "tmux + claude: the run printed a FATAL on a perfectly good machine:
$OUT" ;;
  *) ok "tmux + claude: no FATAL printed" ;;
esac

# ── 5b. THE SECOND SENTINEL: codex-only is a SUPPORTED machine ──────────────
# We support two runtimes (cli/ocwarden/install.go:1057-1061 refuses only when
# NEITHER resolves), so a machine that deliberately runs codex and never installed
# claude must install cleanly. An earlier draft of this gate required claude
# outright and would have locked those machines out — this case is what stops that
# from coming back.
if [[ -x /opt/homebrew/bin/claude || -x /usr/local/bin/claude ]]; then
  echo "  skip — an absolute claude exists on this host; 'codex but no claude' is not constructible"
else
  reset_fixture
  run_install "$PKG" tmux codex
  check "codex only, no claude: the install succeeds" "0" "$RC"
  if [[ -f "$FAKEHOME/$PLIST_REL" ]]; then
    ok "codex only: the serve plist was rendered"
  else
    bad "codex only: no serve plist — a supported machine was blocked:
$OUT"
  fi
  case "$OUT" in
    *"FATAL"*) bad "codex only: the run printed a FATAL on a supported machine:
$OUT" ;;
    *) ok "codex only: no FATAL printed" ;;
  esac
  # …and it must not nag about the runtime it deliberately does not use.
  case "$OUT" in
    *"claude_bin_unresolved"*) bad "codex only: warned about claude_bin_unresolved on a host that runs codex:
$OUT" ;;
    *) ok "codex only: no claude nag (the plist simply carries no OC_CLAUDE_BIN)" ;;
  esac
fi

# ── 5c. the platform half of the preflight ─────────────────────────────────
# Every other case here runs as darwin/arm64. Linux on x86_64 is supported;
# --foreground needs no service manager, so it installs even where systemd is
# not PID 1, while the background-service install is refused there with the
# WSL2 fix. Other pairs are refused before anything is written.
# run_platform <uname -s> <uname -m> <tools…> -- <install.sh args…>
run_platform() {
  local s="$1" m="$2" rundir="$WORK/run"; shift 2
  rm -rf "$rundir"; mkdir -p "$rundir"
  while [[ $# -gt 0 && "$1" != "--" ]]; do ln -s "$TOOLDIR/$1" "$rundir/$1"; shift; done
  [[ $# -gt 0 ]] && shift
  OUT="$(cd "$WORK" && env -i PATH="$rundir:$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$FAKEHOME" SHIM_STATE="$WORK" SHIM_UNAME_S="$s" SHIM_UNAME_M="$m" \
    OC_SD_BOOTED_DIR="$WORK/no-systemd" \
    bash "$PKG/install.sh" ${1+"$@"} </dev/null 2>&1)"; RC=$?
}
reset_fixture
run_platform Linux x86_64 tmux claude -- --foreground
check "linux/x86_64 --foreground without systemd: the install succeeds" "0" "$RC"
case "$OUT" in
  *"✅ installed. Starting OffiCraft in the foreground (--foreground)"*) ok "linux/x86_64 --foreground: the install ran through to serve" ;;
  *) bad "linux/x86_64 --foreground: did not reach serve:
$OUT" ;;
esac
if wrote_anything; then ok "linux/x86_64 --foreground: installed into ~/.officraft"; else bad "linux/x86_64 --foreground: nothing was installed"; fi

reset_fixture
run_platform Linux x86_64 tmux claude --
check "linux without systemd as PID 1: the service install is refused" "1" "$RC"
case "$OUT" in
  *"systemd is not running as PID 1"*"/etc/wsl.conf"*"[boot]"*"systemd=true"*) ok "linux without systemd: the refusal gives the WSL2 /etc/wsl.conf fix" ;;
  *) bad "linux without systemd: no systemd refusal:
$OUT" ;;
esac
if wrote_anything; then bad "linux without systemd: the machine was touched before refusing"; else ok "linux without systemd: NOTHING was written"; fi

reset_fixture
run_platform Linux x86_64 claude -- --foreground
check "linux, no tmux: refused" "1" "$RC"
case "$OUT" in
  *"tmux — install it with your package manager, e.g. sudo apt install tmux"*) ok "linux, no tmux: the fix is a package-manager install, not brew" ;;
  *) bad "linux, no tmux: wrong or missing install hint:
$OUT" ;;
esac

reset_fixture
run_platform Linux i686 tmux claude -- --foreground
check "linux/i686: refused" "1" "$RC"
case "$OUT" in
  *"This machine reports: Linux/i686"*) ok "linux/i686: the refusal is the platform one, naming this machine" ;;
  *) bad "linux/i686: the platform gate did not fire:
$OUT" ;;
esac
if wrote_anything; then bad "linux/i686: the machine was touched before refusing"; else ok "linux/i686: NOTHING was written"; fi

# ── 5d. an OC_*_BIN override that points at NOTHING must not satisfy the gate ─
# Found by review, and it is the exact shape of the defect this ticket exists to
# kill: with OC_CODEX_BIN=/nonexistent/codex the run went rc=0, wrote the plist,
# and said NOTHING about a runtime — a green install whose members can never
# start. The override is now honoured only when it is an executable file, which is
# what cli/ocwarden/transport.go:446 has always done; anything else falls through
# to PATH and then to the fallback dirs, exactly like ocwarden.
#
# Not a hostile input: one stale `export OC_CODEX_BIN=…` in a shell profile does
# it, and docs/guide/troubleshooting.md actively teaches setting these.
if [[ "$HAS_ABS_RUNTIME" == 1 ]]; then
  echo "  skip — an absolute claude/codex exists on this host; the override cases need 'no runtime on PATH'"
else
  for v in OC_CODEX_BIN OC_CLAUDE_BIN; do
    reset_fixture
    rundir="$WORK/run"; rm -rf "$rundir"; mkdir -p "$rundir"
    ln -s "$TOOLDIR/tmux" "$rundir/tmux"
    OUT="$(cd "$WORK" && env -i PATH="$rundir:$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
      HOME="$FAKEHOME" SHIM_STATE="$WORK" "$v=/nonexistent/runtime" \
      bash "$PKG/install.sh" </dev/null 2>&1)"; RC=$?
    check "$v pointing at a nonexistent path: the install is refused" "1" "$RC"
    if [[ -f "$FAKEHOME/$PLIST_REL" ]]; then
      bad "$v=/nonexistent: a plist was written — a bogus override bought a green install"
    else
      ok "$v=/nonexistent: NOTHING was written"
    fi
  done
  # A non-executable file must not pass either — existence is not runnability.
  reset_fixture
  rundir="$WORK/run"; rm -rf "$rundir"; mkdir -p "$rundir"
  ln -s "$TOOLDIR/tmux" "$rundir/tmux"
  : > "$WORK/notexec"; chmod 644 "$WORK/notexec"
  OUT="$(cd "$WORK" && env -i PATH="$rundir:$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$FAKEHOME" SHIM_STATE="$WORK" OC_CODEX_BIN="$WORK/notexec" \
    bash "$PKG/install.sh" </dev/null 2>&1)"; RC=$?
  check "OC_CODEX_BIN pointing at a non-executable file: the install is refused" "1" "$RC"
fi

# 5e. SENTINEL for 5d: a VALID override with nothing on PATH must still install.
# Without this, 5d could be satisfied by ignoring OC_*_BIN altogether — which
# would break the documented recovery (docs/guide/troubleshooting.md) for anyone
# whose runtime lives outside PATH and the fallback dirs.
# The precondition is "NO runtime resolves except through the override", so the
# guard has to cover absolute CODEX as well — copied from 5b, where claude-only was
# correct, it silently became wrong here. Measured: on a host with a fallback-dir
# codex, a cheat mutant that ignores the override entirely still exits 0 and this
# case reports PASS — a false green precisely on the machines codex support is
# about. HAS_ABS_RUNTIME is the guard that matches this case's premise.
if [[ "$HAS_ABS_RUNTIME" == 1 ]]; then
  echo "  skip — an absolute claude/codex exists on this host; 'runtime ONLY via the override' is not constructible"
else
  reset_fixture
  rundir="$WORK/run"; rm -rf "$rundir"; mkdir -p "$rundir"
  ln -s "$TOOLDIR/tmux" "$rundir/tmux"
  OUT="$(cd "$WORK" && env -i PATH="$rundir:$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$FAKEHOME" SHIM_STATE="$WORK" OC_CODEX_BIN="$TOOLDIR/codex" \
    bash "$PKG/install.sh" </dev/null 2>&1)"; RC=$?
  check "a VALID OC_CODEX_BIN with no runtime on PATH: the install succeeds" "0" "$RC"
  if [[ -f "$FAKEHOME/$PLIST_REL" ]]; then
    ok "valid OC_CODEX_BIN: the serve plist was rendered"
  else
    bad "valid OC_CODEX_BIN: the documented override no longer gets you installed:
$OUT"
  fi
fi

# ── 5f. STANDALONE mode (curl | bash) is gated too ──────────────────────────
# Every case above runs the PACKAGE installer, but the path the docs push is
# `curl … | bash`, which takes a different branch (no sibling binaries ⇒
# IN_PACKAGE=0) with its own arg parser and its own call site. A gate that exists
# in only one of the two is a gate half the users never meet. The refusal must also
# land BEFORE the download, which is asserted two ways: the release-resolution line
# must be absent from the output, AND the curl tripwire must be empty. Neither
# depends on the network being down — that assumption was wrong when this case was
# written (see the curl shim above).
reset_fixture
: > "$WORK/.netwire"
STANDALONE="$WORK/standalone"; rm -rf "$STANDALONE"; mkdir -p "$STANDALONE"
cp "$PKG/install.sh" "$STANDALONE/install.sh"   # NO ocserverd/ocwarden/ocagent beside it
rundir="$WORK/run"; rm -rf "$rundir"; mkdir -p "$rundir"
ln -s "$TOOLDIR/claude" "$rundir/claude"        # runtime present, tmux absent
OUT="$(cd "$WORK" && env -i PATH="$rundir:$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
  HOME="$FAKEHOME" SHIM_STATE="$WORK" bash "$STANDALONE/install.sh" </dev/null 2>&1)"; RC=$?
check "standalone (curl|bash) shape, no tmux: the install is refused" "1" "$RC"
case "$OUT" in
  *"brew install tmux"*) ok "standalone: the same actionable refusal is printed" ;;
  *) bad "standalone: the preflight did not fire on the curl|bash path:
$OUT" ;;
esac
case "$OUT" in
  *"resolving latest release"*|*"downloading"*)
    bad "standalone: the run reached the download before refusing — the gate is too late:
$OUT" ;;
  *) ok "standalone: it refused BEFORE resolving/downloading a release" ;;
esac
if [[ -s "$WORK/.netwire" ]]; then
  bad "standalone: the run invoked curl — the refusal is late enough to touch the network:
$(cat "$WORK/.netwire")"
else
  ok "standalone: curl was never invoked (tripwire empty — this is what makes the case hermetic)"
fi

# ── 6. the preflight must not block the ways OUT ────────────────────────────
# Removal and --help have to keep working on the exact machine the gate refuses
# to install on: a user whose tmux-less box is half-configured still needs
# `--uninstall` to clean up, and still needs `--help` to read what is required.
reset_fixture
rundir="$WORK/run"; rm -rf "$rundir"; mkdir -p "$rundir"
OUT="$(cd "$WORK" && env -i PATH="$rundir:$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
  HOME="$FAKEHOME" SHIM_STATE="$WORK" \
  bash "$PKG/install.sh" --uninstall --dry-run </dev/null 2>&1)"; RC=$?
check "no tools at all: --uninstall --dry-run still works" "0" "$RC"
OUT="$(cd "$WORK" && env -i PATH="$rundir:$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
  HOME="$FAKEHOME" SHIM_STATE="$WORK" \
  bash "$PKG/install.sh" --help </dev/null 2>&1)"; RC=$?
check "no tools at all: --help still prints" "0" "$RC"
# --help IS the file's header comment block (install.sh sed-prints it), so a new
# hard requirement that never reaches it is undocumented from the user's side.
case "$OUT" in
  *"tmux"*) ok "--help documents the tmux requirement (the header block IS the help text)" ;;
  *) bad "--help never mentions tmux — the requirement is invisible to the user:
$OUT" ;;
esac

echo "install.sh tool preflight: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
