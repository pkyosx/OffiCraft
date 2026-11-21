#!/usr/bin/env bash
# bin/tests/install-claude-stamp.sh — HERMETIC unit tests for the serve-plist
# RUNTIME stamps (claude AND codex) written by bin/install.sh (T-ba62) and by
# bin/ocserver install (T-ff48). The filename is historical; the suite covers
# both installers and both runtimes.
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
for b in ocserverd ocwarden ocagent officraft; do
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

# tmux: the tool preflight (T-7f38) refuses the install without it. Stubbed, never
# invoked — this suite is about the claude stamp, not about tmux.
printf '#!/usr/bin/env bash\nexit 0\n' > "$SHIMDIR/tmux"

chmod +x "$SHIMDIR"/uname "$SHIMDIR"/launchctl "$SHIMDIR"/lsof "$SHIMDIR"/tmux

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

# write_codex <path> <mode> — the codex under test. Same three modes and the same
# reason for baking the mode in as a literal (see write_claude): the installer
# probes with `env -i`, which would wipe an env-driven switch.
write_codex() {
  local path="$1" mode="$2" dir; dir="$(cd "$(dirname "$path")" && pwd)"
  case "$mode" in
    ok)     printf '#!/usr/bin/env bash\necho "codex-cli 1.2.3"\nexit 0\n' > "$path" ;;
    shim)   printf '#!/usr/bin/env bash\ncase ":$PATH:" in *":%s:"*) echo "codex-cli 1.2.3"; exit 0;; esac\nexit 127\n' "$dir" > "$path" ;;
    *)      printf '#!/usr/bin/env bash\nexit 1\n' > "$path" ;;
  esac
  chmod +x "$path"
}

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

echo "serve-plist runtime stamps (claude + codex, both installers) — hermetic tests"

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

# ── 2. no runtime at all → the install is REFUSED before anything is written ─
# CONTRACT CHANGE (T-7f38): this case used to assert "install still succeeds (the
# server works without claude)". It no longer does — the tool preflight stops a
# run that cannot ever start a member, and it stops it BEFORE the plist is
# rendered, so the assertion is "nothing was written", not "written without the
# stamp". The positive control the old case provided (a plist that renders with a
# PATH but no OC_CLAUDE_BIN) now lives in case 5, which reaches the renderer.
#
# NOTE the shape: the preflight wants claude OR CODEX, so "no claude" alone is no
# longer a refusal (bin/tests/install-preflight-guard.sh case 5b covers the
# codex-only machine). This suite's shim PATH carries neither, which is why the
# run below is still refused — the run_install helper never puts codex on PATH.
if [[ -x /opt/homebrew/bin/claude || -x /usr/local/bin/claude \
   || -x /opt/homebrew/bin/codex  || -x /usr/local/bin/codex ]]; then
  # The preflight mirrors ocwarden's resolution order, which includes ABSOLUTE
  # paths that no PATH shim can hide. On a host carrying one of them this case
  # cannot construct "no runtime anywhere", so it is skipped rather than faked —
  # a green here would have meant nothing.
  echo "  skip — an absolute claude/codex exists on this host; 'no runtime anywhere' is not constructible"
else
  reset_fixture
  run_install 0
  check "no runtime anywhere: the install is refused" "1" "$RC"
  if [[ -f "$FAKEHOME/$PLIST_REL" ]]; then
    bad "no runtime: a serve plist was written anyway — the refusal came too late"
  else
    ok "no runtime: NO serve plist was written (the refusal precedes the renderer)"
  fi
  case "$OUT" in
    *"curl -fsSL https://claude.ai/install.sh | bash"*)
      ok "no runtime: the refusal names Claude Code AND how to install it" ;;
    *) bad "no runtime: the refusal does not say how to fix it:
$OUT" ;;
  esac
fi

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
# POSITIVE CONTROL for every "plist carries OC_CLAUDE_BIN" assertion above (it
# moved here from case 2 when the no-claude run stopped reaching the renderer):
# this run DOES render a plist, and that plist has a PATH and no stamp — so the
# greps above are discriminating, not matching anything.
check "unstampable path: the install itself still succeeds" "0" "$RC"
if plist_has "<key>PATH</key>"; then
  ok "unstampable path: the plist still renders, with a (minimal) PATH"
else
  bad "unstampable path: no plist rendered — this case can no longer serve as the control:
$PLIST_BODY"
fi
case "$OUT" in
  *"claude_bin_unresolved"*) ok "unstampable path: names the exact downstream failure code" ;;
  *) bad "unstampable path: does not name claude_bin_unresolved:
$OUT" ;;
esac

# ── 6. ASYMMETRY SENTINEL: same version-manager dir, BOTH runtimes stamped ───
# WHY THIS CASE IS THE POINT OF THE SUITE (T-ff48). A member is claude OR codex,
# and the installer's preflight has always accepted a codex-only host — but the
# serve plist only ever carried OC_CLAUDE_BIN. On an asdf/nvm/volta host that
# means the cockpit's 「安裝」 resolves claude and refuses every codex spawn with
# runtime_bin_unresolved, while a claude member on the SAME machine works. The
# defect is not "codex is unsupported", it is that the two runtimes are treated
# differently at the one link that can see the shim.
#
# Both are asserted in ONE case on purpose: a codex-only assertion would go green
# on a change that fixed codex by breaking claude, which is the same bug pointed
# the other way.
reset_fixture
write_claude "$EXTRADIR/claude" shim
write_codex  "$EXTRADIR/codex"  shim
run_install 1
check "claude+codex shim: install succeeds" "0" "$RC"
if plist_has "<key>OC_CLAUDE_BIN</key><string>$EXTRADIR/claude</string>"; then
  ok "claude+codex shim: serve plist carries OC_CLAUDE_BIN"
else
  bad "claude+codex shim: serve plist is MISSING OC_CLAUDE_BIN:
$PLIST_BODY"
fi
if plist_has "<key>OC_CODEX_BIN</key><string>$EXTRADIR/codex</string>"; then
  ok "claude+codex shim: serve plist carries OC_CODEX_BIN (symmetric with claude)"
else
  bad "claude+codex shim: ASYMMETRY — claude is stamped but OC_CODEX_BIN is not; a codex member on this host would hit runtime_bin_unresolved:
$PLIST_BODY"
fi
if plist_has "$EXTRADIR:"; then
  ok "claude+codex shim: the installer PATH (incl. the shim dir) is promoted into the plist"
else
  bad "claude+codex shim: the shim dir is NOT on the plist PATH:
$PLIST_BODY"
fi
rm -f "$EXTRADIR/codex"

# ── 6b. codex ALONE promotes the installer PATH (install.sh) ────────────────
# Case 6 cannot prove this: both stubs are shims there, so the claude block's
# promotion already satisfies the PATH assertion. Here claude runs fine under the
# minimal launchd PATH, so a promoted PATH can only have come from the codex side.
reset_fixture
write_claude "$EXTRADIR/claude" ok
write_codex  "$EXTRADIR/codex"  shim
run_install 1
check "codex-only shim: install succeeds" "0" "$RC"
if plist_has "$EXTRADIR:"; then
  ok "codex-only shim: the installer PATH is promoted by the codex block alone"
else
  bad "codex-only shim: the PATH was not promoted — a host whose codex alone is behind a version manager stamps a codex the warden cannot run:
$PLIST_BODY"
fi
rm -f "$EXTRADIR/codex"

# ── 7. codex-only host → OC_CODEX_BIN is stamped and the PATH is promoted ────
# The preflight already lets this host install (claude OR codex). Before the fix
# it installed a plist that named neither runtime.
# Skip-guard, same reason as case 2: the preflight also looks at ABSOLUTE paths
# no PATH shim can hide, so on a host carrying /opt/homebrew/bin/claude the run is
# not codex-only and the case would pass for a weaker reason than it advertises
# (and would exec the operator's real claude inside a suite that calls itself
# hermetic).
if [[ -x /opt/homebrew/bin/claude || -x /usr/local/bin/claude ]]; then
  echo "  skip — an absolute claude exists on this host; a codex-only host is not constructible"
else
reset_fixture
rm -f "$EXTRADIR/claude"
write_codex "$EXTRADIR/codex" shim
run_install 1
check "codex-only: install succeeds" "0" "$RC"
if plist_has "<key>OC_CODEX_BIN</key><string>$EXTRADIR/codex</string>"; then
  ok "codex-only: serve plist carries OC_CODEX_BIN"
else
  bad "codex-only: serve plist is MISSING OC_CODEX_BIN:
$PLIST_BODY"
fi
if plist_has "$EXTRADIR:"; then
  ok "codex-only: the installer PATH is promoted for the codex shim"
else
  bad "codex-only: the shim dir is NOT on the plist PATH — the warden could never run codex:
$PLIST_BODY"
fi
fi
rm -f "$EXTRADIR/codex"
write_claude "$EXTRADIR/claude" ok

# ── 8. OC_CODEX_BIN override wins over PATH discovery ───────────────────────
reset_fixture
mkdir -p "$WORK/explicit-codex"
write_codex "$WORK/explicit-codex/codex" ok
write_codex "$EXTRADIR/codex" ok
run_install 1 OC_CODEX_BIN="$WORK/explicit-codex/codex"
check "explicit OC_CODEX_BIN: install succeeds" "0" "$RC"
if plist_has "<key>OC_CODEX_BIN</key><string>$WORK/explicit-codex/codex</string>"; then
  ok "explicit OC_CODEX_BIN: the operator's path wins over PATH discovery"
else
  bad "explicit OC_CODEX_BIN: was not honoured:
$PLIST_BODY"
fi
rm -f "$EXTRADIR/codex"

# ── 9. an unstampable codex path is dropped, not rendered ───────────────────
# Same XML/exec hygiene the claude side gets (case 5). The control that this
# grep discriminates is case 6, where OC_CODEX_BIN IS present.
reset_fixture
mkdir -p "$WORK/bad codex dir"
write_codex "$WORK/bad codex dir/codex" ok
run_install 1 OC_CODEX_BIN="$WORK/bad codex dir/codex"
check "unstampable codex path: the install itself still succeeds" "0" "$RC"
if plist_has "OC_CODEX_BIN"; then
  bad "unstampable codex path: must NOT be rendered into the plist:
$PLIST_BODY"
else
  ok "unstampable codex path: dropped (no OC_CODEX_BIN rendered)"
fi

# ── 10. bin/ocserver (the SOURCE install path) — driven, not grepped ─────────
# WHY THIS IS A REAL RUN AND NOT A GREP. The first version of this case asserted
# on bin/ocserver's SOURCE TEXT, because its plist renderer is a function local to
# `ocserver install` (which also builds binaries and talks to launchctl). An
# independent reviewer then proved the obvious hole: guarding the codex env line
# with `if false` left every one of those greps green while OC_CODEX_BIN was never
# emitted — the exact T-ff48 defect, shipping green on the source-install path. A
# grep for a mention cannot tell live code from dead code.
#
# So bin/ocserver grew `render-serve-plist`, a hidden seam in the same spirit as
# the `render-config` seam that bin/tests/port-default.sh drives. It calls the
# REAL renderer — `render_serve_plist`, the same function install step 6 hands to
# drop_and_load — and that function now resolves the runtimes itself. These cases
# therefore assert on the actual rendered plist, and there is no separate
# "resolve" call in cmd_install left to lose: a second reviewer showed that when
# there was one, deleting that single line reproduced the whole defect for BOTH
# runtimes with the suite still green.
OCSERVER_SRC="$HERE/../ocserver"
OCSHOME="$WORK/ocs-home"
OCSDIR="$WORK/ocs-versionmgr"

# ocs_render [env-overrides…] — drive the seam under a hermetic PATH/HOME.
ocs_render() {
  rm -rf "$OCSHOME"; mkdir -p "$OCSHOME"
  OCS_OUT="$(env -i PATH="$OCSDIR:$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$OCSHOME" "$@" bash "$OCSERVER_SRC" render-serve-plist "$OCSHOME" 2>/dev/null)"
}
ocs_has() { [[ "$OCS_OUT" == *"$1"* ]]; }

if [[ ! -f "$OCSERVER_SRC" ]]; then
  bad "bin/ocserver not found at $OCSERVER_SRC"
else
  mkdir -p "$OCSDIR"
  # 10a. THE ASYMMETRY SENTINEL for the source path: both runtimes behind the same
  # version manager, both must be stamped, and the shim dir must reach the plist.
  rm -f "$OCSDIR/claude" "$OCSDIR/codex"
  write_claude "$OCSDIR/claude" shim
  write_codex  "$OCSDIR/codex"  shim
  ocs_render
  if ocs_has "<key>OC_CLAUDE_BIN</key><string>$OCSDIR/claude</string>"; then
    ok "ocserver: claude is stamped into the serve plist env"
  else
    bad "ocserver: claude is NOT stamped — this control has rotted:
$OCS_OUT"
  fi
  if ocs_has "<key>OC_CODEX_BIN</key><string>$OCSDIR/codex</string>"; then
    ok "ocserver: codex is stamped too (symmetric with claude)"
  else
    bad "ocserver: ASYMMETRY — claude is stamped but OC_CODEX_BIN is not; a codex member on a source-installed host would hit runtime_bin_unresolved:
$OCS_OUT"
  fi
  if ocs_has "$OCSDIR:"; then
    ok "ocserver: the installer PATH (incl. the shim dir) is promoted"
  else
    bad "ocserver: the shim dir is NOT on the plist PATH — neither runtime could be run by the warden:
$OCS_OUT"
  fi

  # 10b. codex ALONE promotes the PATH. Case 10a cannot prove this: claude is a
  # shim there too, so its own promotion satisfies the assertion. Here claude runs
  # fine under the minimal PATH, so a promoted PATH can only have come from codex.
  rm -f "$OCSDIR/claude" "$OCSDIR/codex"
  write_claude "$OCSDIR/claude" ok
  write_codex  "$OCSDIR/codex"  shim
  ocs_render
  if ocs_has "$OCSDIR:"; then
    ok "ocserver: a codex shim promotes the installer PATH on its own"
  else
    bad "ocserver: only claude can promote the PATH — a host whose codex alone is behind a version manager stamps a codex the warden cannot run:
$OCS_OUT"
  fi

  # 10c. the OC_CODEX_BIN override is honoured (the documented escape hatch).
  rm -f "$OCSDIR/codex"
  mkdir -p "$WORK/ocs-explicit"
  write_codex "$WORK/ocs-explicit/codex" ok
  write_codex "$OCSDIR/codex" ok
  ocs_render OC_CODEX_BIN="$WORK/ocs-explicit/codex"
  if ocs_has "<key>OC_CODEX_BIN</key><string>$WORK/ocs-explicit/codex</string>"; then
    ok "ocserver: the OC_CODEX_BIN override wins over PATH discovery"
  else
    bad "ocserver: OC_CODEX_BIN was not honoured — the documented escape hatch does nothing:
$OCS_OUT"
  fi

  # 10d. an unstampable codex path is dropped, not interpolated.
  rm -f "$OCSDIR/codex"
  mkdir -p "$WORK/ocs bad dir"
  write_codex "$WORK/ocs bad dir/codex" ok
  ocs_render OC_CODEX_BIN="$WORK/ocs bad dir/codex"
  if ocs_has "OC_CODEX_BIN"; then
    bad "ocserver: an unstampable codex path was rendered into the plist:
$OCS_OUT"
  else
    ok "ocserver: an unstampable codex path is dropped (case 10a is the positive control)"
  fi

  # 10e. the .npm-global fallback — the standard non-root `npm install -g` prefix.
  # Asserted for BOTH runtimes: this list was claude-only-missing it, which is how
  # a "deliberate mirror" comment ends up describing two different resolvers.
  rm -f "$OCSDIR/claude" "$OCSDIR/codex"
  rm -rf "$OCSHOME"; mkdir -p "$OCSHOME/.npm-global/bin"
  write_claude "$OCSHOME/.npm-global/bin/claude" ok
  write_codex  "$OCSHOME/.npm-global/bin/codex"  ok
  OCS_OUT="$(env -i PATH="$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" HOME="$OCSHOME" \
    bash "$OCSERVER_SRC" render-serve-plist "$OCSHOME" 2>/dev/null)"
  if ocs_has "<key>OC_CLAUDE_BIN</key><string>$OCSHOME/.npm-global/bin/claude</string>"; then
    ok "ocserver: claude is found under ~/.npm-global/bin"
  else
    bad "ocserver: claude is NOT found under ~/.npm-global/bin, the standard non-root npm prefix:
$OCS_OUT"
  fi
  if ocs_has "<key>OC_CODEX_BIN</key><string>$OCSHOME/.npm-global/bin/codex</string>"; then
    ok "ocserver: codex is found under ~/.npm-global/bin"
  else
    bad "ocserver: codex is NOT found under ~/.npm-global/bin:
$OCS_OUT"
  fi

  # 10f. the rendered plist is a VALID plist, not just a string with the right
  # substrings. plutil is not shimmed in this suite, so this is the real linter —
  # the same gate drop_and_load applies before it hands the file to launchd.
  rm -f "$OCSDIR/claude" "$OCSDIR/codex"
  write_claude "$OCSDIR/claude" ok
  write_codex  "$OCSDIR/codex"  ok
  ocs_render
  if printf '%s\n' "$OCS_OUT" | plutil -lint - >/dev/null 2>&1; then
    ok "ocserver: the rendered serve plist passes plutil -lint with both stamps"
  else
    bad "ocserver: the rendered serve plist is NOT valid — launchd would refuse it:
$OCS_OUT"
  fi

  # 10g. the install path still renders through the function these cases drive.
  # HONEST ABOUT ITS STRENGTH: this one IS a grep, and it is the last unguarded
  # link — everything else about the stamp is now proven by running the renderer.
  # It is a much smaller hole than the one it replaced: the resolution no longer
  # has its own call site to lose (render_serve_plist resolves), so what is left
  # is "does install still render the serve plist at all", and a host that stopped
  # doing that has no serve job — a failure nobody could miss.
  OCS="$(cat "$OCSERVER_SRC")"
  if [[ "$OCS" == *'drop_and_load "$SERVE_LABEL" render_serve_plist'* ]]; then
    ok "ocserver: install renders the serve plist through render_serve_plist"
  else
    bad "ocserver: install no longer renders through render_serve_plist — the cases above are driving a function the install does not use"
  fi

  rm -rf "$OCSHOME"
fi

# ── 11. the runtime probe is bounded — a hanging shim must not hang the install ─
# WHY: the probe runs `<runtime> --version` from the operator's PATH. A shim that
# re-shims on first call, waits on the network, or reads stdin used to hang the
# installer forever with no output since the previous step — and for a `curl |
# bash` or cockpit-driven install there is nobody in front of that terminal.
#
# THIS CASE CARRIES ITS OWN WATCHDOG. Without one, a regression here does not fail
# the suite, it HANGS it — and a CI job stuck at 100% looks nothing like a red
# test, so nobody reads it as this contract breaking. The watchdog turns the
# regression back into a FAIL (verified: disabling the budget makes this case
# report the kill, not stall).
reset_fixture
# `exec` on purpose: without it the stub forks and the probe's kill reaps only
# the stub, leaving an orphaned `sleep 600` behind on EVERY green run — the repo's
# own no-orphans invariant (bin/tests/run_bounded.py), broken by the test that
# proves the timeout works.
printf '#!/usr/bin/env bash\nread -r _ 2>/dev/null\nexec sleep 600\n' > "$EXTRADIR/claude"
chmod +x "$EXTRADIR/claude"
HANG_OUT="$WORK/hang.out"
: > "$HANG_OUT"
(
  cd "$WORK" && env -i \
    PATH="$EXTRADIR:$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$FAKEHOME" SHIM_TRIPWIRE="$WORK/.tripwire" SHIM_STATE="$WORK" \
    OC_PROBE_BUDGET_SECS=1 \
    bash "$PKG/install.sh" </dev/null >"$HANG_OUT" 2>&1
) &
HANG_PID=$!
HANG_WAITED=0; HANG_KILLED=0
while kill -0 "$HANG_PID" 2>/dev/null; do
  if [[ "$HANG_WAITED" -ge 60 ]]; then
    HANG_KILLED=1
    kill -9 "$HANG_PID" 2>/dev/null || true
    # The stub itself is the thing actually sleeping; without this the leftover
    # `sleep 600` outlives the suite.
    pkill -9 -f "$EXTRADIR/claude" 2>/dev/null || true
    break
  fi
  sleep 1
  HANG_WAITED=$((HANG_WAITED + 1))
done
wait "$HANG_PID" 2>/dev/null || true
HANG_TEXT="$(cat "$HANG_OUT" 2>/dev/null || true)"
if [[ "$HANG_KILLED" == 0 ]]; then
  ok "hanging runtime: the install finished (${HANG_WAITED}s) instead of hanging on the probe"
else
  bad "hanging runtime: the install was still running after ${HANG_WAITED}s and had to be killed — the probe budget is not bounding it"
fi
case "$HANG_TEXT" in
  *"did not answer within"*) ok "hanging runtime: the installer says which binary stopped answering" ;;
  *) bad "hanging runtime: nothing in the output explains the stalled probe:
$HANG_TEXT" ;;
esac
write_claude "$EXTRADIR/claude" ok

echo "serve-plist runtime stamps: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
