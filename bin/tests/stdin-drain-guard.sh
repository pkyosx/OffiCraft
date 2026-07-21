#!/usr/bin/env bash
# bin/tests/stdin-drain-guard.sh — HERMETIC unit tests for bin/install.sh's
# EXIT-time stdin drain (T-fa39, owner decision on rc-a75094435df1).
#
# THE DEFECT UNDER TEST
# ---------------------
# `curl … | bash -s -- --uninstall --dry-run` is the documented invocation.
# Every early exit in install.sh ends the script while the transfer may still be
# in flight; the reading end of the pipe closes, and curl's next write fails:
#
# NOT a file-size story, though the first version of this header said it was:
# a 53.8 KB release (comfortably under the 65,536 B pipe capacity here)
# reproduces the red line 5/5 at 20 KB/s, while the 65.7 KB "just over the
# buffer" version reproduces it 0/5 at loopback speed. The operative condition
# is "the writer still had bytes when the reader left" — i.e. delivery latency.
# The size story predicts the wrong remedy (trim the file; it would not help).
#
#   curl: (23|56) Failure writing output to destination, passed 1418 returned 0
#
# printed to stderr AFTER our own output. So a run that SUCCEEDED — the owner's
# real screenshot ended with "DRYRUN complete — nothing on the machine was
# changed" — finishes on a red line that reads like a failure. That is the same
# disease the rest of this file's --uninstall rewrite was for: a message that
# does not let the reader predict what actually happened.
#
# WHAT IS ASSERTED, AND WHY IN THIS SHAPE
# ---------------------------------------
# curl is not the subject here — the pipe is. The observable is the WRITER's
# exit status: a writer that gets EPIPE dies on SIGPIPE (141), and 141 is
# exactly the condition curl reports as (23|56). So the tests feed install.sh
# on stdin from a writer that still has bytes to push, and assert the writer
# survives. Case 1b is the POSITIVE CONTROL for that probe: the same harness
# against a script WITHOUT the drain must produce 141, otherwise the probe has
# no discriminating power and case 1 is green for the wrong reason.
#
# The other half of the suite is the hang side. The drain reads fd 0, and "sat
# waiting for a human to type" is a failure mode that cannot report itself — a
# previous round of this very ticket shipped exactly that (a /dev/tty read that
# was green in a background shell and wedged forever under a pty), so the two
# skip-guards get their own cases, one of them through a REAL pty, plus a
# positive control proving the pty runner can actually detect a hang.
#
# HERMETIC: temp HOME, a per-suite fake OC_LAUNCHD_LABEL, and a PATH-shimmed
# launchctl, so nothing here can reach the real launchd domain or a real
# ~/.officraft. Every case runs --dry-run, which changes nothing by design.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/../install.sh"
[[ -f "$SCRIPT" ]] || { echo "FATAL: install.sh not found at $SCRIPT" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi; }

WORK="$(mktemp -d -t oc-stdin-drain-guard.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

SHIMDIR="$WORK/shim"; FAKEHOME="$WORK/home"
mkdir -p "$SHIMDIR" "$FAKEHOME"
LABEL="com.officraft.serve.stdindrainguardtest"

# launchctl is shimmed to "this label is not registered" for every subcommand.
# --dry-run never mutates anyway; this is the belt to that suspenders.
cat > "$SHIMDIR/launchctl" <<'SHIM'
#!/usr/bin/env bash
exit 113
SHIM
chmod +x "$SHIMDIR/launchctl"

# Runs install.sh in the sandbox. Callers supply how stdin is wired.
run_env() { HOME="$FAKEHOME" OC_LAUNCHD_LABEL="$LABEL" PATH="$SHIMDIR:$PATH" "$@"; }

echo "== install.sh stdin drain (curl | bash) =="

# Cases 1–2 (the defect itself) live BELOW, after runner.py is written: every
# case in this suite goes through that runner so that a wedged drain fails CI
# with a TIMEOUT instead of hanging it. An earlier version fed the script from a
# bare shell pipeline, which had no bound at all — in a suite whose whole subject
# is boundedness.

# ── The runner ──────────────────────────────────────────────────────────────
# One runner for both: hard timeout, reports elapsed seconds, and can attach the
# child to a REAL pty. It never writes to the child's stdin, so anything that
# waits for typed input hangs until the timeout — which is the point.
cat > "$WORK/runner.py" <<'PY'
import os, pty, select, signal, socket, subprocess, sys, time
mode, timeout, cmd = sys.argv[1], float(sys.argv[2]), sys.argv[3]
start = time.time()
if mode == "sockhold":
    # fd 0 is a SOCKET: not a pipe (so `[[ -p /dev/stdin ]]` is false) and it
    # does not reach EOF while we hold our end. This is the only shape that
    # reaches the second guard — with `bash script.sh` the FIRST guard already
    # returns, so a pty case there proves nothing about this one.
    parent, child = socket.socketpair()
    p = subprocess.Popen(["bash", "-s", "--"] + sys.argv[5:], stdin=child,
                         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    child.close()
    try:
        parent.sendall(open(sys.argv[4], "rb").read())
    except (BrokenPipeError, ConnectionResetError):
        # The child exited before we finished pushing the script — which is the
        # very early-exit this suite is about. Not an error here: what we are
        # measuring is whether it exited AT ALL, and how fast.
        pass
    try:
        rc = p.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        p.kill(); p.wait(); rc = "TIMEOUT"
    parent.close()
    print(f"rc={rc} elapsed={time.time()-start:.1f}")
    sys.exit(0)
if mode == "pty":
    pid, fd = pty.fork()
    if pid == 0:
        os.execvp("bash", ["bash", "-c", cmd])
    rc = None
    while True:
        if time.time() - start > timeout:
            os.kill(pid, signal.SIGKILL); os.waitpid(pid, 0); rc = "TIMEOUT"; break
        r, _, _ = select.select([fd], [], [], 0.1)
        if r:
            try:
                if not os.read(fd, 65536):
                    pass
            except OSError:
                pass
        done, status = os.waitpid(pid, os.WNOHANG)
        if done:
            rc = os.waitstatus_to_exitcode(status); break
elif mode == "writer":
    # THE DEFECT ITSELF. We are the writer: feed the script plus padding (the
    # bytes curl still has in flight) and report whether our write got EPIPE —
    # the same condition curl reports as (23|56). Python ignores SIGPIPE, so we
    # observe it as an exception instead of dying at 141, which also names the
    # writer unambiguously (a shell brace-group reports the LAST command's
    # status, i.e. the padding generator, not the thing feeding the script).
    # Bounded, unlike a bare shell pipeline: a wedged drain fails CI, not hangs it.
    p = subprocess.Popen(["bash", "-s", "--"] + sys.argv[5:], stdin=subprocess.PIPE,
                         stdout=open(os.environ.get("OC_TEST_OUT", os.devnull), "wb"),
                         stderr=subprocess.STDOUT)
    epipe = 0
    try:
        p.stdin.write(open(sys.argv[4], "rb").read() + b"#" * 200000)
        p.stdin.flush()
    except (BrokenPipeError, OSError):
        epipe = 1
    try:
        p.stdin.close()
    except (BrokenPipeError, OSError):
        pass
    try:
        rc = p.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        p.kill(); p.wait(); rc = "TIMEOUT"
    print(f"rc={rc} epipe={epipe} elapsed={time.time()-start:.1f}")
    sys.exit(0)
elif mode == "ratelimit":
    # A writer delivering at a fixed byte rate — the PRODUCTION condition. Every
    # other writer in this suite runs at local-disk speed, where both timeout
    # constants can be shrunk to 1 without a single assertion moving. A trickle
    # discriminates: the drain has to still be draining when the last bytes
    # arrive, which is exactly what the two bounds are sized for.
    # chunk SHAPE matters as much as the rate. curl writes in ~16 KB blocks, so a
    # slow transfer is a big block every few SECONDS — that gap is what the idle
    # timeout is sized against. A writer that dribbles rate/10 every 100 ms
    # delivers the same bytes per second and never produces a gap at all, so it
    # can only ever pin the total deadline. Model the writer we actually have.
    rate = float(sys.argv[5])
    chunk = int(sys.argv[6]) if len(sys.argv) > 6 and sys.argv[6].isdigit() else 16384
    argv_rest = sys.argv[7:] if (len(sys.argv) > 6 and sys.argv[6].isdigit()) else sys.argv[6:]
    p = subprocess.Popen(["bash", "-s", "--"] + argv_rest, stdin=subprocess.PIPE,
                         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    data = open(sys.argv[4], "rb").read()
    epipe = 0
    try:
        for off in range(0, len(data), chunk):
            p.stdin.write(data[off:off + chunk]); p.stdin.flush()
            time.sleep(chunk / rate)
    except (BrokenPipeError, OSError):
        epipe = 1
    try:
        p.stdin.close()
    except (BrokenPipeError, OSError):
        pass
    try:
        rc = p.wait(timeout=max(1.0, timeout - (time.time() - start)))
    except subprocess.TimeoutExpired:
        p.kill(); p.wait(); rc = "TIMEOUT"
    print(f"rc={rc} epipe={epipe} elapsed={time.time()-start:.1f}")
    sys.exit(0)
elif mode == "argpipe":
    # The script is a FILE ARGUMENT (so guard 1 must skip the drain) while fd 0
    # is a pipe this parent holds open and never writes to — a wrapper script or
    # CI harness shape. This is the ONLY shape that reaches guard 1: with the
    # script on stdin, guard 2 has already decided, and with fd 0 a tty guard 2
    # returns first. Removing guard 1 stalls this case for the drain's bound.
    p = subprocess.Popen(["bash", sys.argv[4]] + sys.argv[5:], stdin=subprocess.PIPE,
                         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        rc = p.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        p.kill(); p.wait(); rc = "TIMEOUT"
    try:
        p.stdin.close()
    except (BrokenPipeError, OSError):
        pass
    print(f"rc={rc} elapsed={time.time()-start:.1f}")
    sys.exit(0)
elif mode == "dribble":
    # A writer that never goes silent for long enough to trip the IDLE timeout.
    # `read -t N` restarts its timer on every line, so only a TOTAL deadline
    # bounds this. Feeds the script, then one line every `interval` seconds.
    interval, span = float(sys.argv[5]), float(sys.argv[6])
    p = subprocess.Popen(["bash", "-s", "--"] + sys.argv[7:], stdin=subprocess.PIPE,
                         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        p.stdin.write(open(sys.argv[4], "rb").read()); p.stdin.flush()
        deadline = start + span
        while time.time() < deadline and p.poll() is None:
            time.sleep(interval)
            p.stdin.write(b"# dribble\n"); p.stdin.flush()
    except (BrokenPipeError, OSError):
        pass
    try:
        rc = p.wait(timeout=max(1.0, timeout - (time.time() - start)))
    except subprocess.TimeoutExpired:
        p.kill(); p.wait(); rc = "TIMEOUT"
    try:
        p.stdin.close()
    except (BrokenPipeError, OSError):
        pass
    print(f"rc={rc} elapsed={time.time()-start:.1f}")
    sys.exit(0)
else:  # mode == "hold": stdin is a pipe the parent deliberately keeps OPEN
    p = subprocess.Popen(["bash", "-c", cmd], stdin=subprocess.PIPE,
                         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        p.stdin.write(open(sys.argv[4], "rb").read()); p.stdin.flush()
    except BrokenPipeError:
        pass
    try:
        rc = p.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        p.kill(); p.wait(); rc = "TIMEOUT"
    try:
        p.stdin.close()
    except BrokenPipeError:
        pass
print(f"rc={rc} elapsed={time.time()-start:.1f}")
PY

# Pure parameter expansion on purpose: BSD sed (this is a macOS-only installer)
# has no \b, and a silently-empty field here would make every timing assertion
# below fail for a reason that has nothing to do with install.sh.
py_field() { local s="${1#*$2=}"; echo "${s%% *}"; }

# ── 1. The defect itself ────────────────────────────────────────────────────
# We are the writer: the script plus 200 KB of padding, standing in for the
# bytes curl still has in flight when we exit. `epipe=1` is the same condition
# curl reports as (23|56).
OUT="$(OC_TEST_OUT="$WORK/out.txt" run_env python3 "$WORK/runner.py" writer 30 "unused" "$SCRIPT" --uninstall --dry-run)"
check "curl|bash: the writer does NOT get EPIPE (no 'curl: (23|56)' for the user)" "0" "$(py_field "$OUT" epipe)"
check "curl|bash: and the run itself still succeeds" "0" "$(py_field "$OUT" rc)"

# ── 1b. POSITIVE CONTROL for the probe above ────────────────────────────────
# Without the drain the same harness MUST report epipe=1. If this ever goes
# green, the probe stopped discriminating and case 1 proves nothing.
cat > "$WORK/nodrain.sh" <<'NODRAIN'
#!/usr/bin/env bash
set -euo pipefail
echo "[nodrain] exiting early on purpose"
exit 0
NODRAIN
OUT="$(python3 "$WORK/runner.py" writer 30 "unused" "$WORK/nodrain.sh" --uninstall --dry-run)"
check "positive control: a script WITHOUT the drain DOES leave its writer with EPIPE" "1" "$(py_field "$OUT" epipe)"

# ── 1c. The exit status must survive the trap ───────────────────────────────
# The drain runs in an EXIT trap, and a trap whose last statement is non-zero
# REWRITES the script's status — while an `exit` inside it flattens every
# failure to that code. Without this case, turning the drain's `return 0` into
# `exit 0` makes every failing piped run report SUCCESS, and nothing notices:
# every other case here asserts rc==0 on a path that succeeds anyway.
OUT="$(run_env python3 "$WORK/runner.py" writer 30 "unused" "$SCRIPT" --uninstall --bogus-flag)"
check "a FAILING piped run keeps its exit status through the trap (rejected flag = 2)" "2" "$(py_field "$OUT" rc)"

# ── 2. Behaviour is otherwise unchanged ─────────────────────────────────────
# The drain must not swallow, reorder, or truncate what the user reads. This
# fake HOME has no install, so the "already clean" branch is the one on the
# early-exit path that started this whole ticket.
if grep -q "Already clean" "$WORK/out.txt"; then
  ok "curl|bash: the message the user reads is unchanged (still reaches 'Already clean.')"
else
  bad "curl|bash: expected the 'Already clean.' message; got: $(tail -1 "$WORK/out.txt")"
fi

# 3. POSITIVE CONTROL for the pty runner: it must be able to see a hang at all.
OUT="$(python3 "$WORK/runner.py" pty 4 'read -r -p "type: " x')"
check "positive control: the pty runner detects a script waiting for typed input" "TIMEOUT" "$(py_field "$OUT" rc)"

# The sandbox env goes into a wrapper script rather than into the command
# string: quoting it through bash -> python -> bash once produced a literal
# PATH of "…/shim:$PATH", the child died 127, and the timing assertion below
# went GREEN on 0.0s — fast because nothing ran. Timing is only meaningful
# once the run is known to have happened, so rc gates elapsed here (a control
# checked in the same breath as the sample, not bolted on afterwards).
cat > "$WORK/case_tty.sh" <<EOF
export HOME="$FAKEHOME" OC_LAUNCHD_LABEL="$LABEL" PATH="$SHIMDIR:\$PATH"
exec bash "$SCRIPT" --uninstall --dry-run
EOF
cat > "$WORK/case_hold.sh" <<EOF
export HOME="$FAKEHOME" OC_LAUNCHD_LABEL="$LABEL" PATH="$SHIMDIR:\$PATH"
exec bash -s -- --uninstall --dry-run
EOF

timing_case() { # $1 label ; $2 want-rc ; $3 bound ; $4 out ; $5 hint
  local rc elapsed
  rc="$(py_field "$4" rc)"; elapsed="$(py_field "$4" elapsed)"
  check "$1: install.sh still exits ($5)" "$2" "$rc"
  if [[ "$rc" != "$2" ]]; then
    bad "$1: timing VOID — the run did not complete normally (rc=$rc), so ${elapsed}s measures nothing"
  elif awk "BEGIN{exit !($elapsed < $3)}"; then
    ok "$1: within the bound (${elapsed}s < ${3}s)"
  else
    bad "$1: took ${elapsed}s (>= ${3}s)"
  fi
}

# 4. GUARD 1 (`the script itself came in on stdin`), exercised under a REAL pty
#    because that is where the previous round of this ticket wedged forever
#    while a background shell called it green. `bash install.sh` in a terminal
#    must not enter the drain at all.
#    NAMED FOR WHAT IT COVERS: this case never reaches guard 2 — guard 1
#    returns first — so it says nothing about the pipe check. Case 4b does.
OUT="$(python3 "$WORK/runner.py" pty 12 "bash '$WORK/case_tty.sh'")"
timing_case "guard 1 under a real pty (bash install.sh)" "0" "3" "$OUT" "never waits on the terminal; <3s means guard 1 skipped it, not merely bounded"

# ── 4b. GUARD 2, the only shape that actually reaches it ────────────────────
# The script arrives on fd 0 (so guard 1 passes) while fd 0 is a SOCKET, not a
# pipe — and the writer keeps it open, so it never reaches EOF. Without the
# `-p /dev/stdin` check the drain would run here and sit for its full 5s bound.
# The first version of this suite tried to cover guard 2 with the pty case
# above and did not: removing the check left the suite GREEN.
OUT="$(run_env python3 "$WORK/runner.py" sockhold 12 "unused" "$SCRIPT" --uninstall --dry-run)"
timing_case "guard 2: fd0 is a socket that never EOFs (not a pipe)" "0" "3" "$OUT" "the pipe check skipped the drain; >=3s means it ran and hit its bound"

# ── 4c. GUARD 1, the only shape that actually reaches IT ────────────────────
# The script is a FILE ARGUMENT (guard 1 must skip) while fd 0 is a pipe a
# parent holds open — a wrapper script or CI harness. Symmetric to 4b: with the
# script on stdin guard 2 decides, and under a pty guard 2 returns first, so
# neither of those can detect guard 1 going missing. Without it, every
# `bash install.sh …` under such a wrapper gains a stall at exit.
OUT="$(run_env python3 "$WORK/runner.py" argpipe 20 "unused" "$SCRIPT" --uninstall --dry-run)"
timing_case "guard 1: script is a file arg while fd0 is a held-open pipe" "0" "3" "$OUT" "the script-from-stdin check skipped the drain; >=3s means it ran"

# 5. Boundedness, easy half: stdin is a pipe whose writer stays open with
#    nothing more to send. The IDLE timeout must give up; an unbounded
#    `cat >/dev/null` would sit here for as long as the writer lives.
OUT="$(python3 "$WORK/runner.py" hold 20 "bash '$WORK/case_hold.sh'" "$SCRIPT")"
timing_case "writer holds the pipe open" "0" "12" "$OUT" "the drain is bounded, not a hang"

# ── 5b. Boundedness, HARD half: a writer that never goes idle ───────────────
# `read -t N` restarts its timer on every line, so an idle timeout alone bounds
# nothing against a dribbler: one line every 3s for 30s previously held the
# drain 36.5s — the exact hang the implementation comment cites as its reason
# for rejecting `cat >/dev/null`. Only the TOTAL deadline bounds this.
OUT="$(run_env python3 "$WORK/runner.py" dribble 40 "unused" "$SCRIPT" 2 30 --uninstall --dry-run)"
timing_case "writer dribbles a line every 2s for 30s" "0" "20" "$OUT" "the TOTAL deadline bounds it; an idle-only timeout would ride the dribbler out"

# ── 5c. THE PRODUCTION CONDITION: a slow transfer, shaped like curl ─────────
# Both timeout constants are load-bearing, and NOTHING above pins either of
# them: every other writer here runs at local-disk speed, where shrinking both
# to 1 leaves the whole suite green. Independent review demonstrated that and
# overturned the argument (mine) that no useful assertion existed.
#
# The shape is half the point. curl delivers ~16 KB blocks, so a slow transfer
# is a big block every few SECONDS — that gap is what the idle timeout is sized
# against. A writer emitting rate/10 every 100 ms moves the same bytes per
# second and never produces a gap, so it can only pin the total deadline.
#
# WHAT MAKES THIS GREEN — and an admission. THREE successive versions of this
# comment asserted a mechanism ("the drain still finishes"; "what is still owed
# fits in one pipe buffer"; "the drain gets through the whole file before its
# deadline") and independent review measured each one false. So this version
# states the OBSERVATIONS and stops there:
#   - green: at the moment the drain stops, the writer owes ZERO more bytes...
#   - ...yet the drain has left ~16 KB of the file UNREAD in the pipe.
#   - red: the writer owes as little as ONE byte and still gets EPIPE.
#   - the cliff sits at exactly 81,920 B = 5 x 16,384 (the writer's chunk size).
#
# Those coexist, and I do not have a verified account of why. Do not reason
# forward from a guess about it — three guesses have been wrong here, and the
# last one WAS committed as 6c's rationale for a round before review caught it. Treat 81,920 as a MEASURED
# boundary: reproducible, bisected twice, mechanism unexplained.
#
# Case 6c turns the dependency into an assertion that speaks, so a commit that
# grows install.sh past it fails with an explanation instead of
# making this case mysteriously flaky.
OUT="$(run_env python3 "$WORK/runner.py" ratelimit 60 "unused" "$SCRIPT" 5000 16384 --uninstall --dry-run)"
check "slow transfer (5 KB/s, curl-shaped 16 KB blocks): the writer still gets no EPIPE" "0" "$(py_field "$OUT" epipe)"
check "slow transfer: and the run still succeeds" "0" "$(py_field "$OUT" rc)"

# ── 6. STATIC drift-guard: every EXIT trap must carry the drain ─────────────
# A bare `trap … EXIT` REPLACES the handler, so any exit trap installed later in
# the file silently retires the drain for that whole path — and retiring it
# looks exactly like it working. Enumerating "the trap on line N" would be
# zero-coverage for the next one somebody adds, so assert the property over ALL
# of them instead.
TRAPS_TOTAL=0; TRAPS_WITH_DRAIN=0
while IFS= read -r line; do
  TRAPS_TOTAL=$((TRAPS_TOTAL+1))
  [[ "$line" == *oc_drain_stdin* ]] && TRAPS_WITH_DRAIN=$((TRAPS_WITH_DRAIN+1))
done < <(grep -E "^[[:space:]]*trap .*\bEXIT\b" "$SCRIPT")
if [[ "$TRAPS_TOTAL" -lt 2 ]]; then
  bad "static: expected at least 2 EXIT traps in install.sh (top-level + bootstrap), found $TRAPS_TOTAL — this guard is not looking at what it thinks"
else
  check "static: every EXIT trap in install.sh carries oc_drain_stdin ($TRAPS_TOTAL found)" "$TRAPS_TOTAL" "$TRAPS_WITH_DRAIN"
fi

# ── 6b. STATIC: the drain must NOT be armed on INT/TERM ────────────────────
# An INT handler that does not exit RETURNS TO THE SCRIPT, and when the script
# came in on stdin the rest of the program is still in that pipe — draining it
# there deletes the remainder of the installer. Unreachable in today's control
# flow, but that is a property of the surrounding block, not of the trap, so it
# gets an assertion rather than a comment.
SIGNAL_TRAPS_WITH_DRAIN=0
while IFS= read -r line; do
  [[ "$line" == *oc_drain_stdin* ]] && SIGNAL_TRAPS_WITH_DRAIN=$((SIGNAL_TRAPS_WITH_DRAIN+1))
done < <(grep -E "^[[:space:]]*trap .*\b(INT|TERM)\b" "$SCRIPT")
check "static: no INT/TERM trap arms the drain (it would truncate the script's own source)" "0" "$SIGNAL_TRAPS_WITH_DRAIN"

# Splitting that trap in two made deleting HALF of it a silent change: the
# EXIT half still cleans up on a normal exit, so nothing visibly breaks while
# Ctrl-C stops removing the temp dir. It was one line before this ticket and
# could not be half-deleted; now it can, so the other half gets an assertion.
# BOTH signals, checked separately: an earlier version counted "a trap naming
# INT or TERM", which a mutation to INT-only satisfied while SIGTERM silently
# went back to leaking the temp dir.
for sig in INT TERM; do
  if grep -E "^[[:space:]]*trap .*'[^']*rm -rf[^']*'.*\b$sig\b" "$SCRIPT" >/dev/null; then
    ok "static: SIG$sig still removes the temp dir (splitting the trap did not drop it)"
  else
    bad "static: no SIG$sig trap removes the temp dir — an interrupt during bootstrap now leaks it"
  fi
done

# ── 6c. STATIC: the slow-transfer case's margin ─────────────────────────────
# Case 5c passes only while install.sh stays under a MEASURED boundary — see the
# admission above 5c: the boundary is reproducible (81,920 B = 5 x 16,384, the
# writer's chunk size, bisected twice) but its mechanism is NOT established, and
# three attempts to state one were each measured false. This file is ~70 KB.
# Left implicit, growing install.sh would one day turn 5c red for a reason that
# looks like flakiness and has nothing to do with the drain.
SCRIPT_BYTES="$(wc -c < "$SCRIPT" | tr -d ' ')"
if [[ "$SCRIPT_BYTES" -le 78000 ]]; then
  ok "static: install.sh is ${SCRIPT_BYTES} B — inside the margin case 5c depends on (cliff measured at 81,920 B = 5 x 16,384)"
else
  # Deliberately NOT suggesting "raise _OC_DRAIN_TOTAL_TIMEOUT": measured, that
  # buys 5c room but pushes case 5b (the dribbler) to 23.0s and past ITS bound.
  # A remedy that moves the failure to a neighbouring case is not a remedy, and
  # an unverified suggestion in a failure message is how the next person spends
  # an hour.
  bad "static: install.sh has grown to ${SCRIPT_BYTES} B; case 5c's cliff is at 81,920 B (5 x 16,384). Trim the file, or re-measure the cliff AND case 5b's bound together and move both — do NOT just relax this number"
fi

# ── 7. STATIC: the from-stdin flag must stay at TOP LEVEL ───────────────────
# What BASH_SOURCE[0] reads as inside a function is VERSION-DEPENDENT: measured,
# stock macOS /bin/bash 3.2.57 leaves it empty there (when the script arrives on
# stdin), while bash 5.x reports a non-empty value. The measured consequence of
# moving this computation into oc_drain_stdin is on bash 5.x: the flag is never
# set and the drain becomes silently dead code. What the same move does on
# 3.2.57 varies with how the script was invoked and is NOT asserted — a previous
# round asserted it and review falsified it. Computing at top level sidesteps
# the question.
# (Two earlier rounds fixed a claim here and left install.sh saying the
# opposite, or the reverse. Both places now say only the 5.x half, which is the
# half that was measured end to end.)
if awk '/^oc_drain_stdin\(\)/{inf=1} inf && /BASH_SOURCE/{found=1} /^}/{inf=0} END{exit !found}' "$SCRIPT"; then
  bad "static: BASH_SOURCE is read INSIDE oc_drain_stdin — inside a function bash 5.x reports a NON-empty value, so the from-stdin flag would never be set and the drain becomes DEAD CODE there. Compute it at top level."
else
  ok "static: the from-stdin flag is computed at top level, where BASH_SOURCE is meaningful"
fi

echo
echo "stdin drain guard: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
