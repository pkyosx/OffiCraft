#!/usr/bin/env python3
"""e2e_test/seven_gate/collect.py — the server-fact collector.

Polls the ISOLATED server (:8791 by default, owner token) and appends one JSON
object per poll to <run-dir>/journal.ndjson. It decides nothing; judge.py does.
Keeping the two apart is what makes the verdict testable without a server.

It runs for the WHOLE run, starting BEFORE the agent boots, because ①'s fact
(presence=waking) exists only for as long as the agent has not mounted SSE. A
collector started late reads a green run as red.

  python3 collect.py --base http://127.0.0.1:8791 --token-file .state/owner.tok \\
                     --agent m-xxxx --run-dir runs/<stamp> --interval 1 \\
                     --seconds "$(sg_collect_seconds)"

--seconds is REQUIRED and has no default: the window is derived from the actor
budget in lib/window.sh, and a default here would be a second constant.

Stops early and cleanly on SIGTERM/SIGINT (run.sh terminates it once the actor
is done) — the journal is flushed per line, so a killed collector still leaves
every sample it took.
"""
import argparse
import json
import os
import signal
import ssl
import sys
import time
import urllib.error
import urllib.request

_stop = False


def _on_signal(_sig, _frame):
    global _stop
    _stop = True


def get(base, token, path):
    req = urllib.request.Request(base.rstrip("/") + path,
                                 headers={"Authorization": "Bearer " + token})
    ctx = ssl.create_default_context()
    try:
        with urllib.request.urlopen(req, timeout=10, context=ctx) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        return {"_http_error": exc.code, "_path": path}
    except Exception as exc:  # noqa: BLE001 — a poll failure must not end the run
        return {"_error": str(exc), "_path": path}


def _list(payload, *keys):
    """Unwrap {"members": [...]} / {"tasks": [...]} / a bare list, tolerantly —
    a shape surprise must degrade to 'no facts this poll', never to a crash that
    silently ends collection mid-run."""
    if isinstance(payload, list):
        return payload
    if isinstance(payload, dict):
        for k in keys:
            v = payload.get(k)
            if isinstance(v, list):
                return v
    return []


def sample(base, token, agent):
    member = None
    m = get(base, token, "/api/members/" + agent)
    if isinstance(m, dict) and m.get("id"):
        member = m
    # The WHOLE stream, explicitly bounded. Two reasons the limit is spelled out:
    # the route's default is 30 and a run now carries owner↔agent AND peer↔agent
    # traffic, and an unfiltered read (no `with=`) is the one that stamps nothing
    # — a collector must never mutate what it observes.
    chat = _list(get(base, token, "/api/chat?limit=500"), "messages", "chat")
    # BOTH panes. /api/reply-cards defaults to status=waiting, so a card the
    # owner answers between two polls would vanish from every later sample —
    # and ⑥'s evidence with it, for a reason that has nothing to do with the
    # agent. The answered pane keeps it readable.
    cards = []
    seen = set()
    for status in ("waiting", "answered"):
        for c in _list(get(base, token, "/api/reply-cards?status=" + status),
                       "cards", "reply_cards", "items"):
            cid = c.get("id") if isinstance(c, dict) else None
            if cid and cid in seen:
                continue
            if cid:
                seen.add(cid)
            cards.append(c)
    tasks = []
    for t in _list(get(base, token, "/api/tasks"), "tasks", "items"):
        tid = t.get("id")
        if not tid:
            continue
        # The list DTO carries no steps[], and those are ④⑤'s entire evidence
        # (⑦ reads status/closed_ts) — so every task is re-read in full.
        full = get(base, token, "/api/tasks/" + tid)
        tasks.append(full if isinstance(full, dict) and full.get("id") else t)
    return {"t": round(time.time(), 3), "member": member, "chat": chat,
            "tasks": tasks, "reply_cards": cards}


def main(argv=None):
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default=os.environ.get("OC_E2E_BASE", "http://127.0.0.1:8791"))
    ap.add_argument("--token-file", required=True)
    ap.add_argument("--agent", required=True)
    ap.add_argument("--run-dir", required=True)
    ap.add_argument("--interval", type=float, default=1.0)
    # NO DEFAULT — ON PURPOSE. This used to read `default=900.0`, and that 900
    # was the second half of the bug lib/window.sh exists to kill: the actor's
    # budget lives in one place while the collector's window was an independent
    # constant sitting HERE, so on DEFAULTS the collector stopped sampling ~22
    # minutes before the actor stopped working and every later fact became
    # invisible to judge.py — a red NAMING THE AGENT for the harness's own gap.
    # window.sh derived the caller's side of it; this line is the other side.
    # Deleting the default is what makes the two impossible to drift apart: the
    # window can now only arrive from sg_collect_seconds via the caller, and a
    # caller that forgets to pass it gets a loud argparse refusal instead of a
    # silent 900. Guarded by tests_guard case (22e/22f).
    ap.add_argument("--seconds", type=float, required=True)
    args = ap.parse_args(argv)

    signal.signal(signal.SIGTERM, _on_signal)
    signal.signal(signal.SIGINT, _on_signal)

    with open(args.token_file, encoding="utf-8") as fh:
        token = fh.read().strip()
    os.makedirs(args.run_dir, exist_ok=True)
    path = os.path.join(args.run_dir, "journal.ndjson")
    deadline = time.time() + args.seconds
    n = 0
    with open(path, "a", encoding="utf-8") as journal:
        while not _stop and time.time() < deadline:
            journal.write(json.dumps(sample(args.base, token, args.agent),
                                     ensure_ascii=False) + "\n")
            journal.flush()
            n += 1
            time.sleep(args.interval)
    print("[collect] wrote %d sample(s) → %s" % (n, path))
    return 0


if __name__ == "__main__":
    sys.exit(main())
