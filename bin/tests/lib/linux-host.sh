# bin/tests/lib/linux-host.sh — sourced by the install.sh guard suites to run
# bin/install.sh as if on a Linux host, on any machine.
#
#   linux_host_setup <dir>   builds <dir>/shim and <dir>/bin and sets LINUX_PATH
#
# <dir>/bin links only the host tools a stock Linux userland also has, so
# launchctl, plutil, lsof, shasum and xattr do not resolve. <dir>/shim stands in
# for the Linux-only tools:
#   uname      answers $SHIM_UNAME_S / $SHIM_UNAME_M (default Linux / x86_64)
#   systemctl  a --user manager whose state lives under $SHIM_STATE/systemd:
#              loaded/<unit> is the definition daemon-reload read, active/<unit>
#              holds the main pid; "enabled" is the default.target.wants link in
#              the unit directory, as on a real host. start/enable only know a
#              unit daemon-reload has read. Without XDG_RUNTIME_DIR it fails the
#              way the real one does. On start it appends $SHIM_CLAIM_URL (when
#              set) to the unit's StandardOutput file, standing in for serve.
#   loginctl   linger flag at $SHIM_STATE/systemd/linger; SHIM_LINGER_DENIED=1
#              makes enable-linger fail as an unprivileged polkit refusal does
#   ss         any active unit listens on $SHIM_SS_SERVE_PORT (default 7755);
#              $SHIM_SS_FOREIGN_PORT is held by a process whose pid is not shown
#   sha256sum  GNU coreutils' interface, backed by the host's shasum
#   tmux, claude  present and runnable

linux_host_setup() {
  local root="$1" t p
  mkdir -p "$root/shim" "$root/bin"
  for t in bash sh env sed grep head tail wc tr sort cut mktemp tar gzip cp mv rm \
           mkdir chmod cat date id dirname basename seq find ln touch awk ls \
           readlink sleep printf kill true false; do
    p="$(command -v "$t" 2>/dev/null)" || continue
    [[ "$p" == /* ]] && ln -sf "$p" "$root/bin/$t"
  done

  cat > "$root/shim/uname" <<'SH'
#!/usr/bin/env bash
case "${1:-}" in
  -m) echo "${SHIM_UNAME_M:-x86_64}" ;;
  *)  echo "${SHIM_UNAME_S:-Linux}" ;;
esac
SH

  cat > "$root/shim/systemctl" <<'SH'
#!/usr/bin/env bash
st="$SHIM_STATE/systemd"
mkdir -p "$st/loaded" "$st/active"
udir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
[[ "${1:-}" == "--user" ]] || { echo "systemctl shim: only --user is modelled" >&2; exit 2; }
shift
if [[ -z "${XDG_RUNTIME_DIR:-}" ]]; then
  echo "Failed to connect to bus: No medium found" >&2
  exit 1
fi
cmd="${1:-}"; shift
now=0
if [[ "${1:-}" == "--now" ]]; then now=1; shift; fi
start_unit() {
  [[ -f "$st/loaded/$1" ]] || { echo "Failed to start $1: Unit $1 not found." >&2; return 5; }
  echo "${SHIM_MAIN_PID:-4242}" > "$st/active/$1"
  if [[ -n "${SHIM_CLAIM_URL:-}" ]]; then
    local log
    log="$(sed -n 's/^StandardOutput=append://p' "$st/loaded/$1" | head -1)"
    [[ -n "$log" ]] && echo "[serve] first run — open $SHIM_CLAIM_URL to set the owner password" >> "$log"
  fi
  return 0
}
case "$cmd" in
  daemon-reload)
    rm -f "$st/loaded/"*
    for f in "$udir"/*.service; do
      [[ -f "$f" ]] && cp "$f" "$st/loaded/${f##*/}"
    done
    exit 0 ;;
  enable)
    [[ -f "$st/loaded/$1" ]] || { echo "Failed to enable unit: Unit file $1 does not exist." >&2; exit 1; }
    mkdir -p "$udir/default.target.wants"
    ln -sf "$udir/$1" "$udir/default.target.wants/$1"
    echo "Created symlink $udir/default.target.wants/$1 → $udir/$1." >&2
    if [[ "$now" == 1 ]]; then start_unit "$1" || exit $?; fi
    exit 0 ;;
  disable)
    if [[ -L "$udir/default.target.wants/$1" ]]; then
      rm -f "$udir/default.target.wants/$1"
      echo "Removed \"$udir/default.target.wants/$1\"." >&2
    fi
    [[ "$now" == 1 ]] && rm -f "$st/active/$1"
    exit 0 ;;
  start|restart) start_unit "$1"; exit $? ;;
  stop) rm -f "$st/active/$1"; exit 0 ;;
  is-enabled)
    if [[ -L "$udir/default.target.wants/$1" ]]; then echo enabled; exit 0; fi
    echo disabled; exit 1 ;;
  is-active)
    if [[ -f "$st/active/$1" ]]; then echo active; exit 0; fi
    echo inactive; exit 3 ;;
  show)
    prop=""; unit=""
    while [[ $# -gt 0 ]]; do
      case "$1" in
        -p) prop="$2"; shift 2 ;;
        --value) shift ;;
        *) unit="$1"; shift ;;
      esac
    done
    case "$prop" in
      MainPID)   if [[ -f "$st/active/$unit" ]]; then cat "$st/active/$unit"; else echo 0; fi ;;
      LoadState) if [[ -f "$st/loaded/$unit" ]]; then echo loaded; else echo not-found; fi ;;
      *) echo "systemctl shim: property $prop is not modelled" >&2; exit 2 ;;
    esac
    exit 0 ;;
esac
echo "systemctl shim: '$cmd' is not modelled" >&2
exit 2
SH

  cat > "$root/shim/loginctl" <<'SH'
#!/usr/bin/env bash
st="$SHIM_STATE/systemd"
mkdir -p "$st"
case "${1:-}" in
  show-user)
    if [[ -f "$st/linger" ]]; then echo yes; else echo no; fi
    exit 0 ;;
  enable-linger)
    if [[ "${SHIM_LINGER_DENIED:-0}" == 1 ]]; then
      echo "Could not enable linger: Access denied" >&2
      exit 1
    fi
    touch "$st/linger"
    exit 0 ;;
esac
echo "loginctl shim: '${1:-}' is not modelled" >&2
exit 2
SH

  cat > "$root/shim/ss" <<'SH'
#!/usr/bin/env bash
st="$SHIM_STATE/systemd"
procs=0; want=""
for a in "$@"; do
  case "$a" in
    -*p*) [[ "$a" != --* ]] && procs=1 ;;
    "sport = :"*) want="${a#sport = :}" ;;
  esac
done
emit() {
  [[ -z "$want" || "$want" == "$1" ]] || return 0
  if [[ "$procs" == 1 && -n "$2" ]]; then
    echo "LISTEN 0      4096       127.0.0.1:$1      0.0.0.0:*    users:((\"ocserverd\",pid=$2,fd=3))"
  else
    echo "LISTEN 0      4096       127.0.0.1:$1      0.0.0.0:*"
  fi
}
for f in "$st/active/"*; do
  [[ -f "$f" ]] && emit "${SHIM_SS_SERVE_PORT:-7755}" "$(cat "$f")"
done
[[ -n "${SHIM_SS_FOREIGN_PORT:-}" ]] && emit "$SHIM_SS_FOREIGN_PORT" ""
exit 0
SH

  printf '#!/usr/bin/env bash\nexec /usr/bin/shasum -a 256 "$@"\n' > "$root/shim/sha256sum"
  printf '#!/usr/bin/env bash\nexit 0\n' > "$root/shim/tmux"
  printf '#!/usr/bin/env bash\necho "9.9.9 (Claude Code)"\nexit 0\n' > "$root/shim/claude"
  chmod +x "$root/shim/"*
  LINUX_PATH="$root/shim:$root/bin"
}

# linux_unit_state <unit> — "<is-enabled> <is-active> linger=<yes|no>" as the
# shims report it; needs HOME and SHIM_STATE of the run being inspected.
linux_unit_state() {
  local e a l
  e="$(XDG_RUNTIME_DIR=/run/user/0 PATH="$LINUX_PATH" systemctl --user is-enabled "$1" 2>/dev/null)"
  a="$(XDG_RUNTIME_DIR=/run/user/0 PATH="$LINUX_PATH" systemctl --user is-active "$1" 2>/dev/null)"
  l="$(PATH="$LINUX_PATH" loginctl show-user x -p Linger --value 2>/dev/null)"
  printf '%s %s linger=%s' "$e" "$a" "$l"
}
