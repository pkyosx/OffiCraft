#!/usr/bin/env bash
# bin/tests/install-claude-stamp.sh — HERMETIC unit tests for bin/install.sh's
# serve-plist claude stamp (T-ba62).
#
# THE DEFECT UNDER TEST
# ---------------------
# The release one-click installer wrote a serve plist whose EnvironmentVariables
# carried only HOME / OC_CONFIG / OC_NO_OPEN_BROWSER. The cockpit's 「安裝」
# (POST /api/machines/{id}/bootstrap-here) passes the SERVER PROCESS's env
# straight into `ocwarden install`, so on a one-click host that install ran with
# no PATH and no OC_CLAUDE_BIN and could not resolve a version-manager claude.
# It then installed the warden anyway (WARNING + exit 0), the machine row went
# online, and every spawn afterwards failed with claude_bin_unresolved and zero
# owner-visible signal. bin/ocserver install (the source path) had carried this
# stamp all along — the one-click path, i.e. the path a NEW user walks, was the
# one missing it.
#
# WHY THE SHIM IS SHAPED THIS WAY
# -------------------------------
# Same discipline as install-guard.sh: launchctl/lsof/uname are replaced on PATH
# so nothing in the real launchd domain is read or written, and HOME is
# redirected purely to give the file-side gates a sandbox. What is NEW here is a
# stubbed `claude` on PATH whose `--version` behaviour is selectable, because the
# property under test is exactly "which claude did the installer resolve, and
# under which PATH did it prove it runs".
#
# The assertions read the RENDERED PLIST, not the installer's chatter: a log line
# saying "stamping OC_CLAUDE_BIN" and a plist that actually carries it are two
# different facts, and only the second one reaches the warden.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/../install.sh"
[[ -f "$SCRIPT" ]] || { echo "FATAL: install.sh not found at $SCRIPT" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi; }

WORK="$(mktemp -d -t oc-install-claude.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

SHIMDIR="$WORK/shim"
EXTRADIR="$WORK/versionmgr"   # stands in for an asdf/nvm shim dir
PKG="$WORK/pkg"
FAKEHOME="$WORK/home"
mkdir -p "$SHIMDIR" "$EXTRADIR" "$PKG"

cp "$SCRIPT" "$PKG/install.sh"
for b in ocserverd ocwarden ocagent; do
  printf '#!/usr/bin/env bash\nexit 0\n' > "$PKG/$b"
  chmod +x "$PKG/$b"
done

cat > "$SHIMDIR/uname" <<'SH'
#!/usr/bin/env bash
case "${1:-}" in
  -s) echo Darwin ;;
  -m) echo arm64 ;;
  *)  echo Darwin ;;
esac
SH

# launchctl: no job registered anywhere (fresh machine), and every call recorded.
cat > "$SHIMDIR/launchctl" <<'SH'
#!/usr/bin/env bash
echo "launchctl $*" >> "$SHIM_TRIPWIRE"
case "${1:-}" in
  print) exit 1 ;;
  bootstrap) touch "$SHIM_STATE/.bootstrapped"; exit 0 ;;
esac
exit 0
SH

# lsof: port free until bootstrap, listening afterwards (so the health gate passes).
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

chmod +x "$SHIMDIR"/uname "$SHIMDIR"/launchctl "$SHIMDIR"/lsof

# write_claude <path> <mode> — the claude under test. The mode is BAKED IN as a
# literal, never read from the environment: install.sh probes claude with
# `env -i PATH=… HOME=… claude --version`, which wipes every SHIM_* variable, so
# an env-driven stub would silently fall back to its default mode and the shim
# case would test the plain case instead (a false green that looked real).
#   ok     → runs under any PATH (plain install)
#   shim   → runs ONLY when its own dir is on PATH (asdf/nvm/volta shape) — the
#            case that must promote the installer PATH into the plist
#   broken → never runs (best-effort stamp)
write_claude() {
  local path="$1" mode="$2" dir; dir="$(cd "$(dirname "$path")" && pwd)"
  case "$mode" in
    ok)     printf '#!/usr/bin/env bash\necho "9.9.9 (Claude Code)"\nexit 0\n' > "$path" ;;
    shim)   printf '#!/usr/bin/env bash\ncase ":$PATH:" in *":%s:"*) echo "9.9.9 (Claude Code)"; exit 0;; esac\nexit 127\n' "$dir" > "$path" ;;
    *)      printf '#!/usr/bin/env bash\nexit 1\n' > "$path" ;;
  esac
  chmod +x "$path"
}
write_claude "$EXTRADIR/claude" ok

PLIST_REL="Library/LaunchAgents/com.officraft.serve.plist"

reset_fixture() {
  rm -rf "$FAKEHOME"
  mkdir -p "$FAKEHOME/Library/LaunchAgents"
  rm -f "$WORK/.bootstrapped" "$WORK/.tripwire"
  : > "$WORK/.tripwire"
}

# run_install [with_claude=0|1] [env-overrides…] — a FRESH install, default label,
# non-interactive (the `curl | bash` shape).
run_install() {
  local with_claude="$1"; shift
  local claudepath=""
  [[ "$with_claude" == 1 ]] && claudepath="$EXTRADIR:"
  OUT="$(cd "$WORK" && env -i \
    PATH="${claudepath}$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$FAKEHOME" SHIM_TRIPWIRE="$WORK/.tripwire" SHIM_STATE="$WORK" \
    "$@" \
    bash "$PKG/install.sh" </dev/null 2>&1)"
  RC=$?
  PLIST_BODY="$(cat "$FAKEHOME/$PLIST_REL" 2>/dev/null || true)"
}

plist_has() { [[ "$PLIST_BODY" == *"$1"* ]]; }

echo "install.sh serve-plist claude stamp — hermetic tests"

# ── 1. claude present and runnable → OC_CLAUDE_BIN + a PATH land in the plist ─
reset_fixture
write_claude "$EXTRADIR/claude" ok
run_install 1
check "claude on PATH: install succeeds" "0" "$RC"
if plist_has "<key>OC_CLAUDE_BIN</key><string>$EXTRADIR/claude</string>"; then
  ok "claude on PATH: serve plist carries OC_CLAUDE_BIN pointing at the resolved claude"
else
  bad "claude on PATH: serve plist is MISSING the OC_CLAUDE_BIN stamp:
$PLIST_BODY"
fi
if plist_has "<key>PATH</key>"; then
  ok "claude on PATH: serve plist carries a PATH (launchd grants none by default)"
else
  bad "claude on PATH: serve plist is MISSING PATH:
$PLIST_BODY"
fi

# ── 2. POSITIVE CONTROL for the assertions above ─────────────────────────────
# Without this, "the stamp is present" could not be distinguished from "the
# grep matches anything": the no-claude run must produce a plist that still
# renders (PATH present) but carries NO OC_CLAUDE_BIN.
reset_fixture
run_install 0
check "no claude anywhere: install still succeeds (the server works without claude)" "0" "$RC"
if plist_has "OC_CLAUDE_BIN"; then
  bad "no claude: plist must NOT fabricate an OC_CLAUDE_BIN stamp:
$PLIST_BODY"
else
  ok "no claude: plist correctly carries no OC_CLAUDE_BIN"
fi
if plist_has "<key>PATH</key>"; then
  ok "no claude: the plist still carries a (minimal) PATH"
else
  bad "no claude: plist lost its PATH entry"
fi
# and it must SAY SO — a missing claude is exactly the state that used to be
# silent all the way to "every spawn fails".
case "$OUT" in
  *"claude CLI not found"*) ok "no claude: the installer says claude is missing" ;;
  *) bad "no claude: the installer said nothing about claude:
$OUT" ;;
esac
case "$OUT" in
  *"claude_bin_unresolved"*) ok "no claude: names the exact downstream failure code" ;;
  *) bad "no claude: does not name claude_bin_unresolved:
$OUT" ;;
esac

# ── 3. version-manager shim → the FULL installer PATH is promoted ────────────
# A shim that only runs when its manager dir is on PATH is the common asdf/nvm/
# volta shape. Stamping only OC_CLAUDE_BIN would leave it un-runnable under
# launchd's minimal PATH, which is the failure this promotion exists to prevent.
reset_fixture
write_claude "$EXTRADIR/claude" shim
run_install 1
check "shim claude: install succeeds" "0" "$RC"
if plist_has "$EXTRADIR:" ; then
  ok "shim claude: the installer PATH (incl. the shim dir) is promoted into the plist"
else
  bad "shim claude: the shim dir is NOT on the plist PATH — the warden could never run it:
$PLIST_BODY"
fi

# ── 4. OC_CLAUDE_BIN override wins over PATH discovery ──────────────────────
reset_fixture
mkdir -p "$WORK/explicit"
cp "$EXTRADIR/claude" "$WORK/explicit/claude"
write_claude "$EXTRADIR/claude" ok
run_install 1 OC_CLAUDE_BIN="$WORK/explicit/claude"
check "explicit OC_CLAUDE_BIN: install succeeds" "0" "$RC"
if plist_has "<key>OC_CLAUDE_BIN</key><string>$WORK/explicit/claude</string>"; then
  ok "explicit OC_CLAUDE_BIN: the operator's path wins over PATH discovery"
else
  bad "explicit OC_CLAUDE_BIN: was not honoured:
$PLIST_BODY"
fi

# ── 5. an unstampable path must be REFUSED, not rendered into the plist ─────
# A path with XML-special chars would corrupt the plist; a relative one would
# resolve against launchd's cwd. Either way: drop the stamp and say why.
reset_fixture
mkdir -p "$WORK/bad dir"
cp "$EXTRADIR/claude" "$WORK/bad dir/claude"
write_claude "$EXTRADIR/claude" ok
run_install 1 OC_CLAUDE_BIN="$WORK/bad dir/claude"
if plist_has "OC_CLAUDE_BIN"; then
  bad "unstampable path: must NOT be rendered into the plist:
$PLIST_BODY"
else
  ok "unstampable path: refused (no OC_CLAUDE_BIN rendered)"
fi
case "$OUT" in
  *"not stampable"*) ok "unstampable path: the installer explains the refusal" ;;
  *) bad "unstampable path: refused silently:
$OUT" ;;
esac

echo "install.sh claude stamp: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
