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

# reset_fixture <none|clean-install|source-coexist> [job-state]
#   none           — nothing under $FAKEHOME/.officraft at all.
#   clean-install  — a plain release-path install: bin/ + server/{data,oc.toml,log}.
#   source-coexist — clean-install PLUS server/repo/ (a from-source install
#                    sharing the same root — must survive untouched).
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
    clean-install|source-coexist)
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
case "$OUT" in *"restore: mv"*) ok "default uninstall: prints a restore command";; *) bad "default uninstall: no restore command in output";; esac

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

echo "uninstall-guard tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
