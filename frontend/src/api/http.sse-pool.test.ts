// Pins the SHARED SSE downlink of the real-backend adapter (connection pool).
//
// The old subscribeEvents opened one EventSource PER subscriber; with the App
// badge + useMembers + useMonitoring + useChat already holding 4, a chat
// thread with ≥2 inline reply cards exhausted Chromium's 6-connections-per-
// host pool and every further fetch (the answer POST included) hung forever.
// These tests pin the fix: ALL subscribers share ONE EventSource fanned out
// client-side (the mock adapter's emitTopic shape); the connection closes when
// the LAST subscriber unsubscribes and lazily reopens on the next subscribe
// (fresh ownerToken read). The black-box counterpart is
// e2e_test/tests/13_reply_cards.spec.js (雙卡同房 leg).

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  closed = false;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onopen: (() => void) | null = null;
  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }
  close(): void {
    this.closed = true;
  }
  // Simulate the browser firing the native `open` event — once on the initial
  // connect, again on every transparent auto-reconnect.
  open(): void {
    this.onopen?.();
  }
  emit(data: unknown): void {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent);
  }
}

// The closed SSE topic vocabulary the reconnect resync replays (spec §3.1/§4.1).
const CLOSED_TOPICS = [
  "member",
  "chat",
  "chat_read",
  "reply_card",
  "task",
  "outsource_worker",
  "task_manual",
  "global_context",
  "role_def",
  "lessons",
  "context",
  "monitoring",
];

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal("EventSource", FakeEventSource);
  localStorage.setItem("oc_token", "test-owner-jwt");
});

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.removeItem("oc_token");
});

describe("httpApi · shared SSE downlink (subscribeEvents pool)", () => {
  it("N subscribers share ONE EventSource; topics fan out to all of them", () => {
    const seenA: string[] = [];
    const seenB: string[] = [];
    const seenC: string[] = [];
    const offA = httpApi.subscribeEvents((t) => seenA.push(t));
    const offB = httpApi.subscribeEvents((t) => seenB.push(t));
    const offC = httpApi.subscribeEvents((t) => seenC.push(t));

    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0].url).toBe(
      "/api/events?token=test-owner-jwt"
    );

    FakeEventSource.instances[0].emit({ topic: "reply_card" });
    expect(seenA).toEqual(["reply_card"]);
    expect(seenB).toEqual(["reply_card"]);
    expect(seenC).toEqual(["reply_card"]);

    offA();
    offB();
    offC();
  });

  it("unsubscribing SOME subscribers keeps the connection; the LAST one closes it; the next subscribe reopens", () => {
    const offA = httpApi.subscribeEvents(() => {});
    const offB = httpApi.subscribeEvents(() => {});
    expect(FakeEventSource.instances).toHaveLength(1);
    const first = FakeEventSource.instances[0];

    offA();
    expect(first.closed).toBe(false); // B still listening

    offB();
    expect(first.closed).toBe(true); // last subscriber gone → closed

    const offC = httpApi.subscribeEvents(() => {});
    expect(FakeEventSource.instances).toHaveLength(2); // lazily reopened
    expect(FakeEventSource.instances[1].closed).toBe(false);
    offC();
  });

  it("unsubscribe is idempotent and per-subscription (the SAME fn twice = two subscriptions)", () => {
    const seen: string[] = [];
    const cb = (t: string) => seen.push(t);
    const off1 = httpApi.subscribeEvents(cb);
    const off2 = httpApi.subscribeEvents(cb);
    expect(FakeEventSource.instances).toHaveLength(1);
    const es = FakeEventSource.instances[0];

    off1();
    off1(); // double-unsubscribe must not touch the other subscription
    expect(es.closed).toBe(false);
    es.emit({ topic: "chat" });
    expect(seen).toEqual(["chat"]); // the second subscription still fires once

    off2();
    expect(es.closed).toBe(true);
  });

  it("no owner token → honest no-op subscription, NO connection", () => {
    localStorage.removeItem("oc_token");
    const off = httpApi.subscribeEvents(() => {});
    expect(FakeEventSource.instances).toHaveLength(0);
    off(); // must not throw
  });

  it("non-JSON keepalive frames are ignored without breaking the fan-out", () => {
    const seen: string[] = [];
    const off = httpApi.subscribeEvents((t) => seen.push(t));
    const es = FakeEventSource.instances[0];
    es.onmessage?.({ data: ": keepalive" } as MessageEvent);
    es.emit({ topic: "monitoring" });
    expect(seen).toEqual(["monitoring"]);
    off();
  });

  // T-db62: the stream has NO replay (spec §2.1); a delta emitted during the
  // drop→reconnect gap is lost, so the CLIENT must full-resync on every
  // reconnect (spec §2.2). The bug: no resync → a lone reply-card badge stayed
  // blank until a manual reload. These pin the resync onto the reconnect event.
  it("RECONNECT (a second open) fans a full resync — one delta per closed topic — to every subscriber", () => {
    const seen: string[] = [];
    const off = httpApi.subscribeEvents((t) => seen.push(t));
    const es = FakeEventSource.instances[0];

    es.open(); // FIRST connect: hooks already refetched on mount → NO resync
    expect(seen).toEqual([]);

    es.open(); // RECONNECT: the missed-gap correction fires
    expect(seen).toEqual(CLOSED_TOPICS);
    // reply_card is in the replay — the badge hook (refetch on reply_card) heals.
    expect(seen).toContain("reply_card");
    off();
  });

  it("reconnect resync reaches EVERY subscriber, not just one", () => {
    const a: string[] = [];
    const b: string[] = [];
    const offA = httpApi.subscribeEvents((t) => a.push(t));
    const offB = httpApi.subscribeEvents((t) => b.push(t));
    const es = FakeEventSource.instances[0];

    es.open(); // connect
    es.open(); // reconnect
    expect(a).toEqual(CLOSED_TOPICS);
    expect(b).toEqual(CLOSED_TOPICS);
    offA();
    offB();
  });
});
