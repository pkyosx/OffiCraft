#!/usr/bin/env bash
# bin/tests/uninstall-guard.sh — HERMETIC unit tests for bin/install.sh's
# `--uninstall` / `--purge` / `--dry-run` removal path (T-3ef9).
#
# THE CAPABILITY UNDER TEST
# -------------------------
# `--uninstall` is a NEW destructive capability: it stops a launchd job and
# moves (or, with --purge, deletes) files under $HOME/.officraft. Getting its
# ownership check wrong is a strictly worse failure mode than the live-service
# gate in install-guard.sh (that one blocks an install; this one can destroy
# data), so every assertion here has a matching NEGATIVE case: something the
# guard must refuse to touch, with the refusal message asserted, not just the
# exit code.
#
# WHY THE SHIM IS SHAPED THIS WAY (same reasoning as install-guard.sh)
# ---------------------------------------------------------------------
# HOME is redirected to a temp dir for every case (so $ROOT_DIR/$PLIST resolve
# into the sandbox), but the safety net is NOT "we used a temp HOME" — it is
# that OC_LAUNCHD_LABEL is ALSO always set to a per-suite test label, so even
# if the ownership check had a bug, the worst it could do is bootout/register
# a fake label under `launchctl`, which is itself stubbed here and never talks
# to the real launchd domain. `plutil` is deliberately NOT stubbed — like
# install-guard.sh, it only ever reads a plist this suite wrote inside its own
# temp dir, so the real tool gives a more faithful test at zero risk.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/../install.sh"
[[ -f "$SCRIPT" ]] || { echo "FATAL: install.sh not found at $SCRIPT" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi; }

WORK="$(mktemp -d -t oc-uninstall-guard.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

SHIMDIR="$WORK/shim"
FAKEHOME="$WORK/home"
mkdir -p "$SHIMDIR"

LABEL="com.officraft.serve.uninstallguardtest"
TARGET="gui/$(id -u)/$LABEL"

# launchctl: same oracle shape as install-guard.sh — SHIM_JOB selects the
# state, every call is recorded in the tripwire so a case can assert WHICH
# label was asked about / booted out, not merely that something happened.
cat > "$SHIMDIR/launchctl" <<SH
#!/usr/bin/env bash
echo "launchctl \$*" >> "$WORK/.tripwire"
case "\${1:-}" in
  print)
    [[ "\${2:-}" == "$TARGET" ]] || exit 1
    case "\${SHIM_JOB:-absent}" in
      running)
        cat <<OUT
$TARGET = {
	state = running
	program = /path/to/ocserverd
	pid = 4242
}
OUT
        exit 0 ;;
      loaded)
        cat <<OUT
$TARGET = {
	state = not running
	program = /path/to/ocserverd
}
OUT
        exit 0 ;;
      *) exit 1 ;;
    esac ;;
  bootout) touch "$WORK/.booted-out"; exit 0 ;;
esac
exit 0
SH
chmod +x "$SHIMDIR/launchctl"

tripwire_has() { grep -qF -e "$1" "$WORK/.tripwire" 2>/dev/null; }
booted_out()   { [[ -f "$WORK/.booted-out" ]] && echo yes || echo no; }

# reset_fixture <none|clean-install|source-coexist|station> [job-state]
#   none           — nothing under $FAKEHOME/.officraft at all.
#   clean-install  — a plain release-path install: bin/ + server/{data,oc.toml,log}.
#                    NOTE this shape has NO agents/ or warden/ — it is the
#                    deliberate CONTROL for the station shape below, so that
#                    "agents/ survived" cannot pass vacuously on a fixture that
#                    never had agents/ in the first place.
#   source-coexist — clean-install PLUS server/repo/ (a from-source install
#                    sharing the same root — must survive untouched).
#   station        — clean-install PLUS the runtime state a machine that is BOTH
#                    server and worker accumulates: warden/ and agents/<id>/.
#                    This is the shape production actually has, and the shape
#                    the old code moved wholesale into the backup while printing
#                    "nothing was deleted" (T-fa39).
# job-state (default "absent") feeds SHIM_JOB for launchctl print, and — when
# not "absent" — a plist pointing at OUR binary is written so the ownership
# check adopts the label.
reset_fixture() {
  local shape="$1" job="${2:-absent}"
  rm -rf "$WORK/.tripwire" "$WORK/.booted-out" "$FAKEHOME"
  : > "$WORK/.tripwire"
  mkdir -p "$FAKEHOME/Library/LaunchAgents"
  SHIM_JOB="$job"

  case "$shape" in
    clean-install|source-coexist|station)
      mkdir -p "$FAKEHOME/.officraft/bin" "$FAKEHOME/.officraft/server/data" "$FAKEHOME/.officraft/server/log"
      printf '#!/usr/bin/env bash\nexit 0\n' > "$FAKEHOME/.officraft/bin/ocserverd"
      chmod +x "$FAKEHOME/.officraft/bin/ocserverd"
      printf 'FAKE-DB-CONTENT' > "$FAKEHOME/.officraft/server/data/officraft.db"
      printf 'port = 17999\n' > "$FAKEHOME/.officraft/server/oc.toml"
      printf 'FAKE-LOG\n' > "$FAKEHOME/.officraft/server/log/serve.log"
      if [[ "$shape" == "source-coexist" ]]; then
        mkdir -p "$FAKEHOME/.officraft/server/repo/.git"
        printf 'FROM-SOURCE-MARKER' > "$FAKEHOME/.officraft/server/repo/marker.txt"
      fi
      if [[ "$shape" == "station" ]]; then
        mkdir -p "$FAKEHOME/.officraft/warden/log"
        printf 'WARDEN-TOKEN-MARKER' > "$FAKEHOME/.officraft/warden/exec-warden.tok"
        printf 'WARDEN-LOG\n' > "$FAKEHOME/.officraft/warden/log/warden.log"
        local a
        for a in ow-aaa111 ow-bbb222 m-ccc333; do
          mkdir -p "$FAKEHOME/.officraft/agents/$a/tmp"
          printf 'WORKSPACE-OF-%s' "$a" > "$FAKEHOME/.officraft/agents/$a/persona.md"
        done
      fi
      ;;
    none) : ;;
    *) echo "FATAL: unknown fixture shape '$shape'" >&2; exit 2 ;;
  esac

  if [[ "$job" != "absent" ]]; then
    cat > "$FAKEHOME/Library/LaunchAgents/$LABEL.plist" <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key><array><string>$FAKEHOME/.officraft/bin/ocserverd</string><string>serve</string></array>
</dict></plist>
PL
  fi
}

write_foreign_plist() {
  cat > "$FAKEHOME/Library/LaunchAgents/$LABEL.plist" <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key><array><string>/some/totally/different/program</string></array>
</dict></plist>
PL
}

# run_uninstall [args…] — stdin defaults to /dev/null (non-interactive shape);
# pass STDIN_ANSWER to feed the --purge confirmation prompt instead.
run_uninstall() {
  local stdin_src=/dev/null
  [[ -n "${STDIN_ANSWER:-}" ]] && stdin_src="$WORK/.stdin"
  [[ -n "${STDIN_ANSWER:-}" ]] && printf '%s\n' "$STDIN_ANSWER" > "$WORK/.stdin"
  OUT="$(cd "$WORK" && env -i \
    PATH="$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$FAKEHOME" OC_LAUNCHD_LABEL="$LABEL" SHIM_JOB="$SHIM_JOB" \
    bash "$SCRIPT" --uninstall "$@" <"$stdin_src" 2>&1)"
  RC=$?
}

echo "bin/install.sh --uninstall — hermetic tests (label=$LABEL, stubbed launchd)"

# ── 1. clean machine: nothing to remove, exit 0, loud but not an error ──────
reset_fixture none
run_uninstall
check "clean machine: exits 0 (idempotent, not an error)" "0" "$RC"
case "$OUT" in *"nothing to remove"*"Already clean"*) ok "clean machine: says so";; *) bad "clean machine: message ('$OUT')";; esac
check "clean machine: never calls launchctl" "" "$(cat "$WORK/.tripwire" 2>/dev/null)"

# ── 2. NEGATIVE: label belongs to a DIFFERENT program → refuse, exit 1 ─────
# The dangerous case: our bin/ exists (so the run is not a no-op), but the
# label this run would manage is registered under someone else's program.
# Ownership of the whole install is ambiguous — must refuse EVERYTHING,
# loudly, and touch nothing (not the plist, not $ROOT_DIR).
reset_fixture clean-install
write_foreign_plist
run_uninstall
check "foreign label: refuses (exit 1)" "1" "$RC"
case "$OUT" in *"DIFFERENT program"*) ok "foreign label: names the conflict";; *) bad "foreign label: message ('$OUT')";; esac
case "$OUT" in *"NOTHING was changed"*) ok "foreign label: says nothing changed";; *) bad "foreign label: missing the nothing-changed statement";; esac
check "foreign label: leaves our own binary untouched" "1" "$([[ -x "$FAKEHOME/.officraft/bin/ocserverd" ]] && echo 1 || echo 0)"
check "foreign label: leaves the foreign plist untouched" "1" "$([[ -f "$FAKEHOME/Library/LaunchAgents/$LABEL.plist" ]] && echo 1 || echo 0)"
check "foreign label: never calls launchctl bootout" "no" "$(booted_out)"

# ── 3. milder negative: NO bin/ of ours, but the label is foreign ──────────
# Nothing of ours exists at all — this is "found nothing to do", not "found
# something I'm unsure about". Message and exit code must read differently
# from case 2, or the two DoD-mandated negative shapes collapse into one.
reset_fixture none
write_foreign_plist
run_uninstall
check "no files + foreign label: exits 0 (nothing OF OURS)" "0" "$RC"
case "$OUT" in *"nothing OF OURS"*) ok "no files + foreign label: distinguishes itself from case 2's wording";; *) bad "no files + foreign label: message ('$OUT')";; esac
check "no files + foreign label: leaves the foreign plist untouched" "1" "$([[ -f "$FAKEHOME/Library/LaunchAgents/$LABEL.plist" ]] && echo 1 || echo 0)"

# ── 4. --dry-run over a real install: prints intent, changes NOTHING ───────
reset_fixture clean-install running
run_uninstall --dry-run
check "dry-run: exits 0" "0" "$RC"
case "$OUT" in *"DRYRUN would run: mv"*) ok "dry-run: announces the move it would make";; *) bad "dry-run: no announced mv ('$OUT')";; esac
case "$OUT" in *"DRYRUN complete"*) ok "dry-run: says nothing was changed";; *) bad "dry-run: missing completion line";; esac
check "dry-run: never calls launchctl bootout" "no" "$(booted_out)"
check "dry-run: binary still present" "1" "$([[ -x "$FAKEHOME/.officraft/bin/ocserverd" ]] && echo 1 || echo 0)"
check "dry-run: database still present, untouched" "FAKE-DB-CONTENT" "$(cat "$FAKEHOME/.officraft/server/data/officraft.db" 2>/dev/null)"
check "dry-run: no .bak- directory was created" "0" "$(find "$FAKEHOME" -maxdepth 1 -name '.officraft.bak-*' 2>/dev/null | wc -l | tr -d ' ')"

# ── 5. default uninstall over a RUNNING job: stop + move, DB preserved ─────
reset_fixture clean-install running
run_uninstall
check "default uninstall: exits 0" "0" "$RC"
check "default uninstall: calls launchctl bootout on OUR target" "yes" "$(tripwire_has "bootout $TARGET" && echo yes || echo no)"
check "default uninstall: removes the plist" "0" "$([[ -f "$FAKEHOME/Library/LaunchAgents/$LABEL.plist" ]] && echo 1 || echo 0)"
check "default uninstall: bin/ is gone from the live path" "0" "$([[ -e "$FAKEHOME/.officraft/bin" ]] && echo 1 || echo 0)"
BAK="$(find "$FAKEHOME" -maxdepth 1 -name '.officraft.bak-*' 2>/dev/null | head -1)"
check "default uninstall: exactly one backup dir was created" "1" "$(find "$FAKEHOME" -maxdepth 1 -name '.officraft.bak-*' 2>/dev/null | wc -l | tr -d ' ')"
check "default uninstall: the database moved into the backup, byte-identical" "FAKE-DB-CONTENT" "$(cat "$BAK/server/data/officraft.db" 2>/dev/null)"
check "default uninstall: the binary moved into the backup too" "1" "$([[ -x "$BAK/bin/ocserverd" ]] && echo 1 || echo 0)"
case "$OUT" in *"restore: "*) ok "default uninstall: prints a restore command";; *) bad "default uninstall: no restore command in output";; esac
# The restore command must put back BOTH halves. Moving the files back is not a
# restore if the launchd registration cannot come back with them — the plist was
# rm'd, so the old one-liner (`mv $backup $ROOT_DIR`) left the service dead and
# said nothing about it. Asserting the FILES half alone would re-accept exactly
# that bug, so both are pinned, plus the plist actually being in the backup.
case "$OUT" in *"launchctl bootstrap"*) ok "default uninstall: restore re-registers the launchd job, not just the files";; *) bad "default uninstall: restore command does not re-register the service ('$OUT')";; esac
check "default uninstall: the plist was preserved in the backup (not just deleted)" "1" "$([[ -f "$BAK/launchd/$LABEL.plist" ]] && echo 1 || echo 0)"

# ── 6. job registered but NOT running: plist removed, bootout NOT called ───
# Booting out a job with no pid is harmless in reality, but asserting we don't
# even try to is what proves job_pid (not just plist presence) gates the call.
reset_fixture clean-install loaded
run_uninstall
check "loaded-not-running: exits 0" "0" "$RC"
check "loaded-not-running: does NOT call launchctl bootout" "no" "$(booted_out)"
check "loaded-not-running: still removes the plist" "0" "$([[ -f "$FAKEHOME/Library/LaunchAgents/$LABEL.plist" ]] && echo 1 || echo 0)"

# ── 7. --purge without --yes and a WRONG confirmation: aborts, deletes nothing ─
reset_fixture clean-install running
STDIN_ANSWER="nope" run_uninstall --purge
check "purge, wrong confirmation: aborts (exit 1)" "1" "$RC"
case "$OUT" in *"aborted"*) ok "purge, wrong confirmation: says aborted";; *) bad "purge, wrong confirmation: message ('$OUT')";; esac
check "purge, wrong confirmation: binary still present" "1" "$([[ -x "$FAKEHOME/.officraft/bin/ocserverd" ]] && echo 1 || echo 0)"
check "purge, wrong confirmation: never calls launchctl bootout" "no" "$(booted_out)"
unset STDIN_ANSWER

# ── 8. --purge --yes: real deletion, DB gone, no backup dir left behind ────
reset_fixture clean-install running
run_uninstall --purge --yes
check "purge --yes: exits 0" "0" "$RC"
check "purge --yes: calls launchctl bootout" "yes" "$(tripwire_has "bootout $TARGET" && echo yes || echo no)"
check "purge --yes: bin/ is gone" "0" "$([[ -e "$FAKEHOME/.officraft/bin" ]] && echo 1 || echo 0)"
check "purge --yes: database is gone" "0" "$([[ -e "$FAKEHOME/.officraft/server/data/officraft.db" ]] && echo 1 || echo 0)"
check "purge --yes: no backup dir was created (this is real deletion, not a move)" "0" "$(find "$FAKEHOME" -maxdepth 1 -name '.officraft.bak-*' 2>/dev/null | wc -l | tr -d ' ')"
case "$OUT" in *"purge complete"*) ok "purge --yes: says purge complete";; *) bad "purge --yes: missing completion line";; esac

# ── 9. source-path coexistence: repo/ (and its siblings) must SURVIVE ──────
# The one case where release-path removal shares $SERVER_DIR with a
# from-source install. Both the default (move) and --purge shapes must leave
# server/repo/ exactly as it was — this is the containment property the
# ticket calls out by name, so it gets its own dedicated assertion in BOTH
# destructive modes, not just one.
reset_fixture source-coexist running
run_uninstall
check "coexist + default uninstall: exits 0" "0" "$RC"
check "coexist + default uninstall: repo/ survives, untouched, in place (not moved)" "FROM-SOURCE-MARKER" "$(cat "$FAKEHOME/.officraft/server/repo/marker.txt" 2>/dev/null)"
check "coexist + default uninstall: bin/ was still removed from the live path" "0" "$([[ -e "$FAKEHOME/.officraft/bin" ]] && echo 1 || echo 0)"

reset_fixture source-coexist running
run_uninstall --purge --yes
check "coexist + purge: exits 0" "0" "$RC"
check "coexist + purge: repo/ survives byte-identical" "FROM-SOURCE-MARKER" "$(cat "$FAKEHOME/.officraft/server/repo/marker.txt" 2>/dev/null)"
check "coexist + purge: the database (release-path's own) IS gone" "0" "$([[ -e "$FAKEHOME/.officraft/server/data/officraft.db" ]] && echo 1 || echo 0)"

# ── 10. --uninstall is recognised before any download would happen ─────────
# Run with a PATH that has no curl at all — if the dispatch fell through to
# the standalone-bootstrap branch (which resolves a release tag over the
# network before doing anything else), this would fail on "curl: not found"
# instead of performing the removal.
reset_fixture clean-install running
OUT="$(cd "$WORK" && env -i PATH="$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
  HOME="$FAKEHOME" OC_LAUNCHD_LABEL="$LABEL" SHIM_JOB=running \
  bash "$SCRIPT" --uninstall --dry-run </dev/null 2>&1)"
RC=$?
check "no curl on PATH: --uninstall still runs to completion" "0" "$RC"
case "$OUT" in *"curl: not found"*|*"curl: command not found"*) bad "no curl on PATH: it tried to download something ('$OUT')";; *) ok "no curl on PATH: never touched the downloader";; esac

# ── 11. --uninstall is recognised in ANY position, not just $1 ─────────────
# Regression case: a prior version dropped the leading argument unconditionally
# assuming --uninstall was always first, which silently ate whatever flag WAS
# first when it wasn't (e.g. `--dry-run --uninstall` lost --dry-run and
# performed a real, unconfirmed deletion). This must hold for every flag
# ordering, and dry-run's "nothing changed" guarantee is what actually catches
# a regression here — an eaten --dry-run turns into a real purge.
reset_fixture clean-install running
# Direct invocation (bypassing run_uninstall's helper, which always puts
# --uninstall first) — --dry-run is $1 here, --uninstall is $2.
OUT="$(cd "$WORK" && env -i PATH="$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
  HOME="$FAKEHOME" OC_LAUNCHD_LABEL="$LABEL" SHIM_JOB="$SHIM_JOB" \
  bash "$SCRIPT" --dry-run --uninstall --purge --yes </dev/null 2>&1)"
RC=$?
check "flag order: --uninstall not first still exits 0" "0" "$RC"
case "$OUT" in *"purge=1 dryrun=1"*) ok "flag order: both flags parsed regardless of position";; *) bad "flag order: wrong resolution ('$OUT')";; esac
case "$OUT" in *"DRYRUN would run"*) ok "flag order: dry-run guarantee held (only announced, did not purge)";; *) bad "flag order: dry-run guarantee broken ('$OUT')";; esac
check "flag order: database survives (dry-run must mutate nothing)" "FAKE-DB-CONTENT" "$(cat "$FAKEHOME/.officraft/server/data/officraft.db" 2>/dev/null)"

# ── 12. THE T-fa39 CASE: on a server+worker station, agents/ and warden/ must ─
# ── survive BOTH modes, and the message must name them ───────────────────────
# The old code took the non-coexist branch to `mv $ROOT_DIR $backup`, which
# swept up every agent workspace and the warden daemon's whole directory while
# printing "the service is stopped but nothing was deleted". The blast radius
# was larger than the message, which is the actual defect — the move was
# recoverable, the misleading message was not.
reset_fixture station running
run_uninstall
check "station/default: exits 0" "0" "$RC"
check "station/default: agents/ still in place (NOT moved into the backup)" "WORKSPACE-OF-ow-aaa111" "$(cat "$FAKEHOME/.officraft/agents/ow-aaa111/persona.md" 2>/dev/null)"
check "station/default: every agent workspace survives" "3" "$(find "$FAKEHOME/.officraft/agents" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')"
check "station/default: warden/ still in place" "WARDEN-TOKEN-MARKER" "$(cat "$FAKEHOME/.officraft/warden/exec-warden.tok" 2>/dev/null)"
check "station/default: \$ROOT_DIR itself still exists" "1" "$([[ -d "$FAKEHOME/.officraft" ]] && echo 1 || echo 0)"
BAK="$(find "$FAKEHOME" -maxdepth 1 -name '.officraft.bak-*' 2>/dev/null | head -1)"
check "station/default: exactly one backup dir" "1" "$(find "$FAKEHOME" -maxdepth 1 -name '.officraft.bak-*' 2>/dev/null | wc -l | tr -d ' ')"
check "station/default: agents/ did NOT leak into the backup" "0" "$([[ -e "$BAK/agents" ]] && echo 1 || echo 0)"
check "station/default: warden/ did NOT leak into the backup" "0" "$([[ -e "$BAK/warden" ]] && echo 1 || echo 0)"
check "station/default: the release-path DB still made it into the backup" "FAKE-DB-CONTENT" "$(cat "$BAK/server/data/officraft.db" 2>/dev/null)"
# Surviving is necessary but not sufficient: the ticket's defect was the MESSAGE
# under-describing the blast radius, so the disclosure is asserted on its own.
case "$OUT" in *"will NOT touch"*) ok "station/default: message states what it will not touch";; *) bad "station/default: no 'will NOT touch' disclosure ('$OUT')";; esac
case "$OUT" in *"agents/ holds 3 agent workspace(s)"*) ok "station/default: names the agent-workspace count";; *) bad "station/default: does not disclose the agent workspaces";; esac
case "$OUT" in *"ocwarden teardown"*) ok "station/default: points at the warden's own removal path";; *) bad "station/default: does not say how to remove the warden";; esac
case "$OUT" in *"com.officraft.ocwarden"*) ok "station/default: discloses the warden job it leaves registered";; *) bad "station/default: silent about the leftover warden job";; esac

# ── 13. same station, --purge: deletion must be just as contained ──────────────
reset_fixture station running
run_uninstall --purge --yes
check "station/purge: exits 0" "0" "$RC"
check "station/purge: the release DB really is gone" "0" "$([[ -e "$FAKEHOME/.officraft/server/data/officraft.db" ]] && echo 1 || echo 0)"
check "station/purge: agents/ survives deletion too" "WORKSPACE-OF-m-ccc333" "$(cat "$FAKEHOME/.officraft/agents/m-ccc333/persona.md" 2>/dev/null)"
check "station/purge: warden/ survives deletion too" "WARDEN-TOKEN-MARKER" "$(cat "$FAKEHOME/.officraft/warden/exec-warden.tok" 2>/dev/null)"
check "station/purge: \$ROOT_DIR is NOT removed" "1" "$([[ -d "$FAKEHOME/.officraft" ]] && echo 1 || echo 0)"
# Regression pin for the dry-run under-reporting bug (T-fa39 C1): the old code
# had a "if the root looks empty, rm -rf it" branch whose preview was silently
# omitted under --dry-run, because the delete before it had only been printed.
# The branch is gone; this asserts nobody reintroduces a root-level rm.
#
# It has to run under --dry-run: `run` only ECHOES the command in that mode, so
# dry-run output is the only place where the string "rm -rf <root>" can appear
# at all. An earlier version of this assertion sat in the --purge --yes case
# above, where `run` executes instead of printing — it could not have matched
# under any implementation, and a mutant reintroducing the root-level rm left it
# green. A guard that cannot go red is worse than no guard: it reads as coverage.
reset_fixture station running
run_uninstall --purge --yes --dry-run
check "station/purge/dry-run: exits 0" "0" "$RC"
case "$OUT" in
  *"rm -rf $FAKEHOME/.officraft"[[:space:]]*|*"rm -rf $FAKEHOME/.officraft")
    bad "station/purge/dry-run: announces a wholesale \$ROOT_DIR removal (the T-fa39 C1 branch is back)";;
  *) ok "station/purge/dry-run: no wholesale \$ROOT_DIR removal announced";;
esac
# Positive control for the assertion above: prove the dry-run output really does
# contain rm -rf lines for the things that SHOULD be removed. Without this, the
# check would also pass on output that contained no rm at all (e.g. if the purge
# path stopped emitting anything), which is the vacuous way to be green.
case "$OUT" in *"DRYRUN would run: rm -rf $FAKEHOME/.officraft/bin"*) ok "station/purge/dry-run: control — it DOES announce the removals it should";; *) bad "station/purge/dry-run: no rm announced at all, so the negative check above proves nothing ('$OUT')";; esac

# ── 14. CONTROL for 12/13: the survival assertions must be able to FAIL ───────
# clean-install has no agents/ at all. If the checks above were passing merely
# because "the file we looked for was never there", this control would look
# identical. Asserting the count is 0 here — and 3 there — is what proves the
# station assertions have discriminating power rather than being vacuous.
reset_fixture clean-install running
run_uninstall
check "control (no agents/): fixture genuinely has none, so 12's '3' means something" "0" "$(find "$FAKEHOME/.officraft/agents" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')"
case "$OUT" in *"agent workspace(s)"*) bad "control (no agents/): claims agent workspaces on a fixture that has none";; *) ok "control (no agents/): stays silent about agents rather than inventing them";; esac

# ── 15. DoD#4 — the printed restore one-liner must ACTUALLY restore ───────────
# Not a re-implementation of it: the exact string the script printed is pulled
# out of the output and executed. A restore command that is merely plausible is
# the failure mode this case exists to catch (the previous one looked fine and
# could not re-register the service).
reset_fixture station running
run_uninstall
RESTORE_CMD="$(printf '%s\n' "$OUT" | sed -n 's/^\[install\][[:space:]]*restore: //p' | head -1)"
check "restore: a command line was actually printed" "1" "$([[ -n "$RESTORE_CMD" ]] && echo 1 || echo 0)"
check "restore: precondition — the live binary really is gone before restoring" "0" "$([[ -e "$FAKEHOME/.officraft/bin/ocserverd" ]] && echo 1 || echo 0)"
check "restore: precondition — the plist really is gone before restoring" "0" "$([[ -f "$FAKEHOME/Library/LaunchAgents/$LABEL.plist" ]] && echo 1 || echo 0)"
rm -f "$WORK/.tripwire"; : > "$WORK/.tripwire"
( export PATH="$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin"; eval "$RESTORE_CMD" ) >/dev/null 2>&1
RESTORE_RC=$?
check "restore: the printed one-liner runs clean" "0" "$RESTORE_RC"
check "restore: the binary is back on the live path" "1" "$([[ -x "$FAKEHOME/.officraft/bin/ocserverd" ]] && echo 1 || echo 0)"
check "restore: the database is back, byte-identical" "FAKE-DB-CONTENT" "$(cat "$FAKEHOME/.officraft/server/data/officraft.db" 2>/dev/null)"
check "restore: oc.toml is back" "1" "$([[ -f "$FAKEHOME/.officraft/server/oc.toml" ]] && echo 1 || echo 0)"
check "restore: the plist is back where launchd looks for it" "1" "$([[ -f "$FAKEHOME/Library/LaunchAgents/$LABEL.plist" ]] && echo 1 || echo 0)"
check "restore: it re-registers the job (this is the half the old command silently dropped)" "yes" "$(tripwire_has "bootstrap" && echo yes || echo no)"
check "restore: agents/ were never involved either way" "3" "$(find "$FAKEHOME/.officraft/agents" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')"

# ── 16. --dry-run on a station: announces, and touches nothing at all ─────────
reset_fixture station running
run_uninstall --dry-run
check "station/dry-run: exits 0" "0" "$RC"
check "station/dry-run: binary untouched" "1" "$([[ -x "$FAKEHOME/.officraft/bin/ocserverd" ]] && echo 1 || echo 0)"
check "station/dry-run: database untouched" "FAKE-DB-CONTENT" "$(cat "$FAKEHOME/.officraft/server/data/officraft.db" 2>/dev/null)"
check "station/dry-run: plist untouched" "1" "$([[ -f "$FAKEHOME/Library/LaunchAgents/$LABEL.plist" ]] && echo 1 || echo 0)"
check "station/dry-run: agents/ untouched" "3" "$(find "$FAKEHOME/.officraft/agents" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')"
check "station/dry-run: warden/ untouched" "WARDEN-TOKEN-MARKER" "$(cat "$FAKEHOME/.officraft/warden/exec-warden.tok" 2>/dev/null)"
check "station/dry-run: no backup dir created" "0" "$(find "$FAKEHOME" -maxdepth 1 -name '.officraft.bak-*' 2>/dev/null | wc -l | tr -d ' ')"
# The preview has to disclose the same scope the real run has, or it is not a
# preview. Both halves: what moves, and what is spared.
case "$OUT" in *"DRYRUN would run: mv"*) ok "station/dry-run: announces the moves";; *) bad "station/dry-run: no announced mv";; esac
case "$OUT" in *"will NOT touch"*) ok "station/dry-run: discloses the spared paths too";; *) bad "station/dry-run: preview omits the spared paths";; esac

# ── 17. fail-closed must be PRECISE: a pure worker machine is still clean ─────
# A machine that only ever ran agents has agents/ and warden/ but no bin/
# ocserverd and no plist of ours. The new disclosure/enumeration code walks
# $ROOT_DIR, so it must not turn this into an error, a prompt, or a removal —
# "already clean, exit 0" has to keep meaning exactly what it meant before.
# This is the second direction the ticket's DoD asks for: the guard must not be
# widened into blocking machines that were never in danger.
reset_fixture none
mkdir -p "$FAKEHOME/.officraft/agents/ow-worker1" "$FAKEHOME/.officraft/warden"
printf 'WORKER-ONLY' > "$FAKEHOME/.officraft/agents/ow-worker1/persona.md"
run_uninstall
check "worker-only machine: still exits 0 (not blocked by the new enumeration)" "0" "$RC"
case "$OUT" in *"nothing to remove"*"Already clean"*) ok "worker-only machine: still reports already-clean";; *) bad "worker-only machine: message changed ('$OUT')";; esac
check "worker-only machine: never calls launchctl" "" "$(cat "$WORK/.tripwire" 2>/dev/null)"
check "worker-only machine: its agent workspace is untouched" "WORKER-ONLY" "$(cat "$FAKEHOME/.officraft/agents/ow-worker1/persona.md" 2>/dev/null)"
check "worker-only machine: no backup dir was created" "0" "$(find "$FAKEHOME" -maxdepth 1 -name '.officraft.bak-*' 2>/dev/null | wc -l | tr -d ' ')"

# ── 18. partially-installed machine: plist is ours, but the files are gone ────
# Someone deleted ~/.officraft by hand, or an install half-finished. The old
# wording announced "what moved: bin/ and server/{...}" unconditionally — a move
# report for files that were never there. And the restore one-liner started with
# `cp -R "$backup/bin"`, which does not exist in this shape, so the && chain died
# on its first command and the plist half never ran: a restore command that
# looked complete and silently failed to re-register the service.
reset_fixture none running
run_uninstall
check "partial install: exits 0" "0" "$RC"
case "$OUT" in *"what moved: bin/"*) bad "partial install: claims it moved bin/ when there was no bin/ ('$OUT')";; *) ok "partial install: does not claim moves that did not happen";; esac
# The up-front announcement has to agree with the after-the-fact report. Naming
# bin/ in "will touch" and then not in "what moved" is the same over-claim, just
# earlier in the run.
case "$OUT" in *"will touch:"*"$FAKEHOME/.officraft/bin"*) bad "partial install: 'will touch' announces a bin/ that is not there ('$OUT')";; *) ok "partial install: 'will touch' only names what is actually present";; esac
case "$OUT" in *"will touch:"*"launchd job $LABEL and its plist"*) ok "partial install: 'will touch' does name the plist, which IS present (control)";; *) bad "partial install: 'will touch' omitted the plist it does touch ('$OUT')";; esac
case "$OUT" in *"the launchd plist only"*) ok "partial install: says the plist was the only thing moved";; *) bad "partial install: does not describe what actually moved ('$OUT')";; esac
BAK="$(find "$FAKEHOME" -maxdepth 1 -name '.officraft.bak-*' 2>/dev/null | head -1)"
check "partial install: the plist really is in the backup" "1" "$([[ -f "$BAK/launchd/$LABEL.plist" ]] && echo 1 || echo 0)"
check "partial install: no empty server/ dir was invented in the backup" "0" "$([[ -d "$BAK/server" ]] && echo 1 || echo 0)"
# The point of the case: the restore line must still work in this shape.
RESTORE_CMD="$(printf '%s\n' "$OUT" | sed -n 's/^\[install\][[:space:]]*restore: //p' | head -1)"
check "partial install: a restore command was printed" "1" "$([[ -n "$RESTORE_CMD" ]] && echo 1 || echo 0)"
rm -f "$WORK/.tripwire"; : > "$WORK/.tripwire"
( export PATH="$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin"; eval "$RESTORE_CMD" ) >/dev/null 2>&1
RESTORE_RC=$?
check "partial install: the restore one-liner still runs clean (no dangling cp of a missing bin/)" "0" "$RESTORE_RC"
check "partial install: the plist is back where launchd looks for it" "1" "$([[ -f "$FAKEHOME/Library/LaunchAgents/$LABEL.plist" ]] && echo 1 || echo 0)"
check "partial install: it still re-registers the job" "yes" "$(tripwire_has "bootstrap" && echo yes || echo no)"

# ── 19. the warden disclosure must be a PROBE, not an assertion ───────────────
# "its own launchd job is registered and this script leaves it that way" is a
# claim about external state. Printing it without looking is the same defect
# class this ticket is about (saying more than was measured), so both directions
# get pinned: a warden job that exists, and one that does not.
reset_fixture station running
run_uninstall --dry-run
case "$OUT" in *"no com.officraft.ocwarden job is registered"*) ok "warden probe: correctly reports NO warden job when none is registered";; *) bad "warden probe: asserts a warden job that this fixture does not have ('$OUT')";; esac
case "$OUT" in *"registered and this script leaves it that way"*) bad "warden probe: claims 'registered' without a plist present";; *) ok "warden probe: does not claim registration it never checked";; esac

reset_fixture station running
cat > "$FAKEHOME/Library/LaunchAgents/com.officraft.ocwarden.plist" <<'PL'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>com.officraft.ocwarden</string>
  <key>ProgramArguments</key><array><string>/somewhere/warden/ocwarden</string></array>
</dict></plist>
PL
run_uninstall --dry-run
case "$OUT" in *"registered and this script leaves it that way"*) ok "warden probe: reports the job as registered when its plist IS there";; *) bad "warden probe: failed to notice a registered warden job ('$OUT')";; esac
# The remediation command has to be runnable. bin/ is about to move into the
# backup and this installer never puts it on PATH, so a bare `ocwarden teardown`
# would be command-not-found — the message must give the warden's own copy.
case "$OUT" in *"$FAKEHOME/.officraft/warden/ocwarden teardown"*) ok "warden probe: teardown hint uses an absolute path that survives the uninstall";; *) bad "warden probe: teardown hint is not runnable after bin/ moves ('$OUT')";; esac
check "warden probe: the warden's own plist is left alone" "1" "$([[ -f "$FAKEHOME/Library/LaunchAgents/com.officraft.ocwarden.plist" ]] && echo 1 || echo 0)"

# ── 20. an unreadable agents/ must not kill the script silently ───────────────
# The count runs `find … | wc -l` under `set -euo pipefail`; when agents/ is
# unreadable the pipeline returns non-zero and the whole script died with rc=1
# and NO output — the silent failure this ticket exists to remove.
reset_fixture station running
chmod 000 "$FAKEHOME/.officraft/agents"
run_uninstall --dry-run
RC_UNREADABLE=$RC
chmod 755 "$FAKEHOME/.officraft/agents"
check "unreadable agents/: script still completes instead of dying silently" "0" "$RC_UNREADABLE"
case "$OUT" in *"agent workspace(s)"*) ok "unreadable agents/: still says something about the workspaces";; *) bad "unreadable agents/: went silent ('$OUT')";; esac
case "$OUT" in *"holds unknown agent workspace(s)"*) ok "unreadable agents/: reports the count as unknown rather than inventing 0";; *) bad "unreadable agents/: did not degrade honestly ('$OUT')";; esac

# ── 21. agents/ as a symlink must not be reported as empty ───────────────────
# `find <symlink> -mindepth 1` does not follow the argument itself, so without
# -H a relocated agents/ holding dozens of workspaces reports 0 — the disclosure
# would understate exactly the thing it exists to disclose.
reset_fixture station running
mv "$FAKEHOME/.officraft/agents" "$FAKEHOME/agents-real"
ln -s "$FAKEHOME/agents-real" "$FAKEHOME/.officraft/agents"
run_uninstall --dry-run
check "symlinked agents/: exits 0" "0" "$RC"
case "$OUT" in *"agents/ holds 3 agent workspace(s)"*) ok "symlinked agents/: counts through the symlink (not reported as 0)";; *) bad "symlinked agents/: miscounted through the symlink ('$OUT')";; esac
check "symlinked agents/: the real workspaces are untouched" "3" "$(find "$FAKEHOME/agents-real" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')"

echo "uninstall-guard tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
