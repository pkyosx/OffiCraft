// The delta downlink must survive ANYTHING the wire hands it.
//
// WHY THIS FILE EXISTS AS ITS OWN SUITE. `es.onmessage` is a defensive path: it
// is fed by a network socket, it runs outside any React boundary, and this app
// installs no window.onerror / "error" / "unhandledrejection" listener and no
// ErrorBoundary (measured — nothing collects what escapes here). A throw out of
// this handler is therefore both invisible and unrecoverable, which makes "no
// frame may escape" a property worth pinning by enumeration rather than by
// reasoning about it case by case.
//
// 🔴 IT IS ALSO A REGRESSION THIS BRANCH SHIPPED AND THEN HAD TO FIX. An earlier
// commit split one over-broad try/catch into a parse-guard plus an isolated
// fan-out. The split fixed a real defect (a subscriber's throw was being
// mislabelled in the log as a malformed frame) but narrowed the protection at
// the same time: the property access moved OUTSIDE the try, where the old catch
// had silently been covering it. `JSON.parse("null")` does not throw — it
// returns `null` — so a bare `null` frame started throwing a TypeError straight
// out of `onmessage`. Found by an independent reviewer's parity probe run
// against both commits, not by any test here, because before this file the
// entire tree had exactly ONE malformed-frame assertion (`": keepalive"`, in
// http.sse-pool.test.ts). One example is not a guard for a defensive path.
//
// The table below is the enumeration that example was standing in for. `null` is
// the interesting member and the reason for the whole file: every other shape is
// inert only by accident of JS semantics (reading `.topic` off a number, string,
// boolean or array yields `undefined` rather than throwing), while `null` is the
// one value that is neither a parse error nor something you may read a property
// from.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi, __resetSseDownlinkForTests } from "./http";
import { TOKEN_KEY } from "./auth";

// WHATWG EventSource readyState (HTML §9.2). Named, and named CORRECTLY: an
// earlier version of this fake had `readyState = 1` for a fresh (unopened)
// connection and set `0 // OPEN` in `open()`, i.e. CONNECTING and OPEN swapped.
// Harmless at the time — production reads only `!== CLOSED`, and CLOSED was the
// one constant that happened to be right — but a fake whose constants disagree
// with the spec answers a future `readyState === OPEN` check with a confident
// green for the wrong reason.
const CONNECTING = 0;
const OPEN = 1;
const CLOSED = 2;

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  closed = false;
  readyState: number = CONNECTING;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onopen: (() => void) | null = null;
  addEventListener(): void {}
  onerror: (() => void) | null = null;
  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }
  close(): void {
    this.closed = true;
    this.readyState = CLOSED;
  }
  open(): void {
    this.readyState = OPEN;
    this.onopen?.();
  }
  /** Deliver a RAW frame body, exactly as the socket would. */
  raw(data: string): void {
    this.onmessage?.({ data } as MessageEvent);
  }
}

/** [what the server sent, why it is worth naming]. */
const MALFORMED: [string, string][] = [
  [": keepalive", "an SSE comment line — the routine one"],
  ["", "an empty frame"],
  ["5", "valid JSON that is a number"],
  ['"hello"', "valid JSON that is a string"],
  ["[1,2]", "valid JSON that is an array"],
  ["true", "valid JSON that is a boolean"],
  ['{"data":{}}', "an object with no topic"],
  ["null", "🔴 valid JSON that is NULL — parses fine, and is not an object"],
  // ── FIELD shapes, not root shapes (added round 4) ────────────────────────
  // Everything above this line varies the shape of the WHOLE frame. Review
  // round 4 pointed out that the table stopped one level too early: a frame
  // may be a perfectly good object and still carry a `topic` that is not a
  // string. None of the three below throws, so the original `!evt.topic`
  // guard passed them all — they are truthy — and each then crossed the seam
  // as a TYPE LIE: `SseDelta.topic` is declared `string`, ~24 hooks compare it
  // with `===` against string literals, and a number/object/array matches none
  // of them. The failure is therefore silent, which is why enumeration had to
  // reach the field.
  ['{"topic":123}', "🔴 a NUMBER topic — truthy, so the old guard let it past"],
  ['{"topic":{"a":1}}', "🔴 an OBJECT topic — truthy, and stringifies to junk"],
  ['{"topic":["chat"]}', "🔴 an ARRAY topic — truthy, and `[\"chat\"] !== \"chat\"`"],
];

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal("EventSource", FakeEventSource);
  localStorage.setItem(TOKEN_KEY, "test-owner-jwt");
});

afterEach(() => {
  __resetSseDownlinkForTests();
  vi.unstubAllGlobals();
  localStorage.removeItem(TOKEN_KEY);
});

describe("SSE downlink · no malformed frame may escape onmessage", () => {
  for (const [frame, why] of MALFORMED) {
    it(`survives ${JSON.stringify(frame)} — ${why}`, () => {
      const seen: string[] = [];
      const off = httpApi.subscribeEvents((t) => seen.push(t));
      const es = FakeEventSource.instances[0];
      es.open();

      expect(
        () => es.raw(frame),
        "a throw here escapes into the browser's event dispatch, where nothing in this app is listening",
      ).not.toThrow();
      expect(seen, "a malformed frame must not be reported as a topic").toEqual([]);

      // And the downlink is still usable afterwards: surviving is not enough if
      // the connection is left in a state that drops the next real delta.
      es.raw(JSON.stringify({ topic: "chat", data: { payload: { id: "m-1" } } }));
      expect(seen, "the very next well-formed frame still arrives").toEqual(["chat"]);
      off();
    });
  }
});
