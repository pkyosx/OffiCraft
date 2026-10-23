"""Minimal black-box SSE client for the conformance suite (stdlib only).

Opens ``GET /api/events`` against ``OC_TARGET_URL`` and parses the raw SSE wire
into event dicts on a background thread, pushing them onto a queue the test
consumes with SHORT bounded waits. Design goals:

  * pure HTTP — no server-implementation imports (black-box iron rule);
  * bounded time — every wait is an explicit ``timeout`` (default 5 s); the
    suite never blocks on the 15 s heartbeat cadence because tests always
    TRIGGER the event they wait for (a write fans the delta within ms);
  * raw fidelity — the parser keeps the ``id:`` line / comment / ``data:``
    fields separate so tests can assert frame SHAPE (e.g. "directed band frames
    carry no id line"), not just payload content.

Parsed event dict:  {"comment": str|None, "id": str|None, "data": str|None}
(one SSE event = the lines up to a blank line; multiple data lines are joined
with "\n" per the SSE spec, though the server never emits multi-line data).
"""

from __future__ import annotations

import http.client
import json
import queue
import socket
import threading
from typing import Any
from urllib.parse import urlsplit

CONNECT_TIMEOUT = 5.0
# Longer than the 15 s heartbeat so a healthy stream never times out mid-read.
READ_TIMEOUT = 20.0


class SSEConnection:
    """One live /api/events connection; events arrive on ``self.events``."""

    def __init__(self, base_url: str, token: str) -> None:
        self.events: "queue.Queue[dict[str, Any]]" = queue.Queue()
        self.status_code: int | None = None
        self.headers: http.client.HTTPMessage | None = None
        self.error_body: bytes = b""
        self._closed = threading.Event()
        parsed = urlsplit(base_url)
        if parsed.scheme not in {"http", "https"} or not parsed.hostname:
            raise ValueError(f"SSEConnection needs an HTTP(S) base URL, got {base_url!r}")
        connection_type = (
            http.client.HTTPSConnection if parsed.scheme == "https" else http.client.HTTPConnection
        )
        self._client = connection_type(
            parsed.hostname, parsed.port, timeout=CONNECT_TIMEOUT
        )
        self._client.request("GET", "/api/events", headers={"Authorization": f"Bearer {token}"})
        # HTTPConnection uses its constructor timeout for both connect and
        # header reads. Preserve the old 5 s connect / 20 s read contract
        # before getresponse waits for the SSE response headers.
        if self._client.sock is not None:
            self._client.sock.settimeout(READ_TIMEOUT)
        self._resp = self._client.getresponse()
        self.status_code = self._resp.status
        self.headers = self._resp.headers
        if self._resp.status != 200:
            # Refused before any stream bytes (401 / 409): capture the JSON body
            # and DON'T start the reader thread.
            self.error_body = self._resp.read()
            self._client.close()
            self._closed.set()
            return
        self._thread = threading.Thread(target=self._pump, daemon=True)
        self._thread.start()

    # ── reader ────────────────────────────────────────────────────────────────
    def _pump(self) -> None:
        buf = b""
        try:
            while True:
                # read1 returns as soon as bytes are buffered; read(4096) is
                # allowed to wait for a full buffer and would hide small SSE
                # frames until the heartbeat fills it.
                chunk = self._resp.read1(4096)
                if not chunk:
                    break
                buf += chunk
                while b"\n\n" in buf:
                    raw, buf = buf.split(b"\n\n", 1)
                    self._emit(raw.decode("utf-8", errors="replace"))
        except Exception:
            pass  # closed / timed out — the queue simply stops growing
        finally:
            self._closed.set()

    def _emit(self, raw: str) -> None:
        event: dict[str, Any] = {"comment": None, "id": None, "data": None}
        data_lines: list[str] = []
        for line in raw.split("\n"):
            if line.startswith(":"):
                event["comment"] = line[1:].strip()
            elif line.startswith("id:"):
                event["id"] = line[3:].strip()
            elif line.startswith("data:"):
                data_lines.append(line[5:].lstrip())
        if data_lines:
            event["data"] = "\n".join(data_lines)
        self.events.put(event)

    # ── consumers ─────────────────────────────────────────────────────────────
    def next_event(self, timeout: float = 5.0) -> dict[str, Any]:
        """Next raw SSE event (comment or data), or raise on timeout."""
        return self.events.get(timeout=timeout)

    def wait_for(self, predicate, timeout: float = 5.0) -> dict[str, Any]:
        """Drain events until one satisfies ``predicate``; raise on timeout."""
        import time as _time

        deadline = _time.monotonic() + timeout
        while True:
            remaining = deadline - _time.monotonic()
            if remaining <= 0:
                raise TimeoutError(
                    "no matching SSE event within "
                    f"{timeout}s (predicate={predicate!r})"
                )
            try:
                event = self.events.get(timeout=remaining)
            except queue.Empty:
                raise TimeoutError(
                    f"no matching SSE event within {timeout}s "
                    f"(predicate={predicate!r})"
                ) from None
            if predicate(event):
                return event

    def wait_for_frame(self, topic: str, timeout: float = 5.0) -> dict[str, Any]:
        """Next DELTA frame (data event) whose JSON ``topic`` matches; returns
        {"event": <raw>, "frame": <parsed data json>}."""

        def _match(ev: dict[str, Any]) -> bool:
            if ev.get("data") is None:
                return False
            try:
                return json.loads(ev["data"]).get("topic") == topic
            except (ValueError, AttributeError):
                return False

        ev = self.wait_for(_match, timeout=timeout)
        return {"event": ev, "frame": json.loads(ev["data"])}

    def wait_for_command(self, member_id: str, timeout: float = 5.0) -> dict[str, Any]:
        """Next ``warden-command`` frame, skipping only a ``stop`` addressed
        to a member other than ``member_id``; same return shape as
        ``wait_for_frame``.

        A STOP no warden took is parked and re-resolved every reconcile tick,
        and with no machine to name it goes out to every online warden
        (lifecycle.md, sse.md §7), so a fresh machine's stream can carry a
        stop for a member an earlier test dismissed. Only that case is
        skipped: any other command, or a malformed one, is returned so the
        caller's assertion fails and prints it."""

        def _match(ev: dict[str, Any]) -> bool:
            if ev.get("data") is None:
                return False
            try:
                frame = json.loads(ev["data"])
            except ValueError:
                return False
            if not isinstance(frame, dict) or frame.get("topic") != "warden-command":
                return False
            try:
                cmd = frame["data"]
                return not (cmd["rpc"] == "stop" and cmd["args"]["member_id"] != member_id)
            except (KeyError, TypeError):
                return True

        ev = self.wait_for(_match, timeout=timeout)
        return {"event": ev, "frame": json.loads(ev["data"])}

    def drain_backlog(
        self, quiet_for: float = 1.0, timeout: float = 5.0, label: str = ""
    ) -> int:
        """BARRIER: SWALLOW every delta already on this connection and WAIT
        UNTIL the stream has been silent for ``quiet_for`` seconds, so that no
        frame produced before this point is still reachable by a later
        ``wait_for*`` call. Returns how many data events were swallowed.

        Why this exists (do not replace it with "just move the setup writes"):
        ``wait_for``/``wait_for_frame`` drain FROM THE FRONT of the queue and
        discard non-matching events, so they happily return a frame that was
        already sitting on the connection BEFORE the caller triggered anything.
        A test that opens its connection first and then does any roster/entity
        write during setup therefore hands its first ``wait_for_frame(topic)``
        a STALE frame, and that assertion becomes vacuously true — the publish
        seam it means to guard can be deleted outright and the row stays green
        (observed and reproduced: test_every_closed_topic_emits' ``member``
        row).

        ⚠️ THE LESSON THE FIRST VERSION PAID FOR — read this before "tightening"
        anything here. v1 drained once and then ASSERTED the stream was silent
        for ``quiet_for``; it turned the untouched baseline RED. The frames from
        the setup writes had not arrived yet at drain time (the fan rides a
        0.25 s poll, the drain ran a few ms after the write), so the sweep saw
        an EMPTY queue and the frames landed inside the assertion window.
        **A legitimate setup write's frame is SUPPOSED to arrive late; arriving
        late is not the error — not being swallowed is the error.** v1 confused
        "ASSERT it is quiet now" with "WAIT UNTIL it is quiet", and so it
        classified the normal case as a failure. Hence the loop below: every
        delta seen RESETS the quiet window instead of failing it. Only the outer
        ``timeout`` — meaning "this stream never went quiet at all", i.e.
        something is publishing continuously — is a red.

        The barrier is COUNT-INDEPENDENT by construction, which is the whole
        point: it swallows however many frames appear (0, 2 or 20) and returns
        only after a full silent window, so a future setup write added above it
        cannot silently re-poison the waits below. That is a property, not the
        convention "remember to keep setup to two writes".

        ⚠️ WHAT THIS CANNOT DO (do not design an experiment around it): ANY
        absorbing barrier is necessarily blind to writes that happen AFTER it
        returns. A setup write inserted BELOW this call and above the waits it
        protects will not be caught here, and the row it poisons will go GREEN —
        the vacuous kind of green. Do not "test" that case and read the green as
        reassurance; it carries no information. The guard for that direction is
        binding each observed frame to the VALUE the row's own write just set
        (see the ``member`` row of test_every_closed_topic_emits), because the
        payload is an eager snapshot taken inside hub.Publish and a frame
        published earlier cannot carry a value written later. NOT the subject:
        the polluting frame is usually about the SAME entity, so binding to the
        subject id proves nothing (measured — see that row's own note).

        ⚠️ KNOWN FALSE-RED WAVEFORM (measured with a fake queue, no server —
        review round 2). The budget is deliberate, not a bug, but know where to
        look when a red appears here on a loaded machine:

            silent stream                                  -> returns  ~1.13 s
            a delta every 0.5 s, forever                   -> RAISES   ~5.06 s
            noisy until t=4.6 s, then completely silent    -> RAISES   ~5.01 s

        The third one is the trap: the stream HAD settled, but not early enough
        to fit a full 1 s quiet window inside the 5 s budget, so this raises even
        though nothing is wrong except timing. Baseline lands at ~1.13 s, i.e.
        about 4 s of headroom, so this is a slow-CI / heavily-loaded-box failure
        mode. If you see it, look at what is delaying the fan poll — do NOT just
        widen the budget, which is how a real "something publishes forever" bug
        would get hidden.
        """
        import time as _time

        hard_deadline = _time.monotonic() + timeout
        swallowed = 0
        while True:
            now = _time.monotonic()
            window_end = now + quiet_for
            clamped = window_end > hard_deadline
            if clamped:
                window_end = hard_deadline
            saw_delta = False
            while True:
                remaining = window_end - _time.monotonic()
                if remaining <= 0:
                    break
                try:
                    ev = self.events.get(timeout=remaining)
                except queue.Empty:
                    break
                if ev.get("data") is None:
                    continue  # heartbeat / comment: not a delta
                swallowed += 1
                saw_delta = True
                break  # a delta RESETS the quiet window (late is fine)
            if not saw_delta and not clamped:
                return swallowed  # a FULL quiet window elapsed: the stream settled
            if _time.monotonic() >= hard_deadline:
                raise AssertionError(
                    f"SSE barrier{' ' + label if label else ''} never settled: "
                    f"the stream did not stay quiet for {quiet_for}s within a "
                    f"{timeout}s budget ({swallowed} deltas swallowed). Deltas "
                    f"are arriving continuously, so a wait after this point "
                    f"could still consume a frame it did not trigger. Something "
                    f"is publishing on this connection without the test asking "
                    f"— find it; do NOT just widen the budget."
                )

    def wait_closed(self, timeout: float = 5.0) -> bool:
        """True once the stream ENDED server-side (the pump thread finished —
        EOF / terminal chunk / read error). The §5.1 takeover observation
        point: a displaced listener's stream is terminated by the server."""
        return self._closed.wait(timeout=timeout)

    def assert_quiet(self, timeout: float = 1.0, ignore_comments: bool = True) -> None:
        """Assert NO (non-comment) event arrives within ``timeout`` seconds —
        the bounded negative wait for MUST-NOT-emit assertions."""
        import time as _time

        deadline = _time.monotonic() + timeout
        while True:
            remaining = deadline - _time.monotonic()
            if remaining <= 0:
                return
            try:
                ev = self.events.get(timeout=remaining)
            except queue.Empty:
                return
            if ignore_comments and ev.get("data") is None:
                continue
            raise AssertionError(f"expected quiet stream, got event: {ev}")

    # ── lifecycle ─────────────────────────────────────────────────────────────
    def close(self, wait: float = 0.0) -> None:
        """Close the connection (idempotent). The TCP close is what the SERVER
        observes (its disconnect edge fires on the socket drop); the local pump
        thread is a daemon that dies on its own — tests that depend on the
        server-side edge POLL the observable surface instead of waiting here."""
        # A cross-thread close of an HTTP/1.1 response need not interrupt a
        # blocked read on Linux. Shut down the socket first so this client's
        # pump wakes immediately and the server observes the TCP disconnect.
        sock = self._client.sock
        if sock is not None:
            try:
                sock.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
        try:
            self._resp.close()
        except Exception:
            pass
        self._client.close()
        if wait > 0:
            self._closed.wait(timeout=wait)

    def __enter__(self) -> "SSEConnection":
        return self

    def __exit__(self, *exc: Any) -> None:
        self.close()
