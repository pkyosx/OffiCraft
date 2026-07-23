#!/usr/bin/env python3
"""Run a command under a wall-clock ceiling, in its own process group, and
guarantee the WHOLE subtree is reaped on timeout, on SIGINT/SIGTERM, and on
every other exit path.

Why this exists (T-1a54): the mutation-testing guards under bin/tests/ spawn
scripts-under-test — some deliberately broken "mutants". A mutant that turns
into a busy-loop must not run forever, and nothing it (or the guard) forked may
survive as an orphan. A seth-m5 orphan burned a whole core for ~46h because a
mutant leaked. `timeout(1)` is not present on macOS, so this is the portable
primitive the harness dispatches every guard through.

Usage:  run_bounded.py TIMEOUT_SECS CMD [ARG...]
Exit:   the child's exit code; 124 if the ceiling fired (GNU timeout
        convention); 128+signum if the child died on a signal or we were
        interrupted.

The child is started in a NEW SESSION, so its pgid equals its pid and every
descendant inherits that group: a single killpg() reaps the entire subtree,
which a plain kill of the direct child cannot do.
"""
import os
import signal
import subprocess
import sys
import time


def _log(msg):
    sys.stderr.write("[run_bounded] " + msg + "\n")
    sys.stderr.flush()


def main():
    if len(sys.argv) < 3:
        _log("usage: run_bounded.py TIMEOUT_SECS CMD [ARG...]")
        return 2
    try:
        timeout = float(sys.argv[1])
    except ValueError:
        _log("TIMEOUT_SECS must be a number, got %r" % (sys.argv[1],))
        return 2
    cmd = sys.argv[2:]

    p = subprocess.Popen(cmd, start_new_session=True)
    pgid = os.getpgid(p.pid)

    def reap(grace=3.0):
        # TERM the whole group first (lets well-behaved children clean up), then
        # KILL anything that ignored it — a mutant may trap TERM, and the point
        # of this primitive is that it can ALWAYS collect what it started.
        for sig in (signal.SIGTERM, signal.SIGKILL):
            try:
                os.killpg(pgid, sig)
            except ProcessLookupError:
                return  # group already gone
            deadline = time.time() + grace
            while time.time() < deadline:
                if p.poll() is not None:
                    # Leader is gone; sweep the group once more with KILL to
                    # collect any lingering grandchildren, then stop.
                    if sig is signal.SIGTERM:
                        try:
                            os.killpg(pgid, signal.SIGKILL)
                        except ProcessLookupError:
                            pass
                    return
                time.sleep(0.05)
        try:
            p.wait(timeout=1)
        except Exception:
            pass

    def on_signal(signum, _frame):
        reap()
        os._exit(128 + signum)

    signal.signal(signal.SIGTERM, on_signal)
    signal.signal(signal.SIGINT, on_signal)

    try:
        rc = p.wait(timeout=timeout)
        # POSIX: a child killed by signal N reports returncode -N.
        return rc if rc >= 0 else 128 - rc
    except subprocess.TimeoutExpired:
        _log("timed out after %gs — killing process group %d" % (timeout, pgid))
        reap()
        return 124
    finally:
        if p.poll() is None:
            reap()


if __name__ == "__main__":
    sys.exit(main())
