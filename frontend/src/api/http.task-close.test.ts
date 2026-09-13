// httpApi · the two task-close writes, at the WIRE (T-192).
//
// 🔴 WHY THIS FILE EXISTS. `forceTaskDone` decides, in product code with a
// paragraph of comment defending it, whether the owner's typed reason rides the
// request at all:
//
//     const trimmed = reason.trim();
//     body: trimmed === "" ? {} : { reason: trimmed }
//
// and until this file NOTHING anywhere in the repo called `httpApi.forceTaskDone`
// or `httpApi.markTaskDone`. An independent review replaced that body with a
// constant `{}` — every reason the owner types silently dropped on the floor,
// the force still succeeding, the dialog still closing — and the frontend suite
// stayed green, `src/api` included. A comment explaining a decision is not a
// guard on it.
//
// The three cells, which are the three the product code actually branches on:
//   1. a reason given      → body carries `reason`, TRIMMED;
//   2. blank / whitespace  → body carries NO `reason` KEY (not `reason: ""`);
//   3. `markTaskDone`      → POST to its own path with NO body at all.
//
// ⚠️ CELL 2 IS ABOUT THE KEY, NOT THE VALUE. `{reason: ""}` and `{}` both
// "close the task without a reason" server-side today, so an assertion on the
// stored outcome cannot tell them apart — but they say different things on the
// wire ("my reason is the empty string" vs "I gave none"), and it is the second
// that owner ruling rc-a92a6252c3bd made expressible. So the assertion is
// `Object.keys(body)`, and `toEqual({})` alone would NOT do it: `toEqual`
// treats an explicit `undefined` as absent, which is precisely the shape a
// half-broken omission would produce.
//
// Shape copied from http.mutations.test.ts (openapi-fetch drives a real
// `Request` through global fetch, so the stub returns real `Response` objects,
// fresh per call — a body is one-shot).

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";

/** TaskWriteReceiptDTO is what both routes answer; neither caller reads it
 * (useTasks awaits and refetches), so the minimum that parses is enough. */
const WIRE_RECEIPT = { task_id: "task-1", status: "done", title: "t" };

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const fetchMock = vi.fn(async () => jsonResponse(WIRE_RECEIPT));

async function lastRequest(): Promise<{
  url: string;
  method: string;
  body: string | undefined;
}> {
  const calls = fetchMock.mock.calls as unknown as [Request][];
  expect(calls.length).toBeGreaterThan(0);
  const req = calls[calls.length - 1][0];
  const u = new URL(req.url);
  const text = await req.clone().text();
  return { url: u.pathname + u.search, method: req.method, body: text || undefined };
}

beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockImplementation(async () => jsonResponse(WIRE_RECEIPT));
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("httpApi.forceTaskDone · the reason reaches the wire, or is honestly absent", () => {
  it("a reason GIVEN rides the body under `reason`", async () => {
    await httpApi.forceTaskDone("task-1", "負責人已離場,活早就交付了");
    const { url, method, body } = await lastRequest();
    expect(url).toBe("/api/tasks/task-1/force-done");
    expect(method).toBe("POST");
    expect(JSON.parse(String(body))).toEqual({
      reason: "負責人已離場,活早就交付了",
    });
  });

  it("a reason is TRIMMED before it is sent, not sent with the textarea's padding", async () => {
    await httpApi.forceTaskDone("task-1", "   worker 已經不在了  \n");
    const { body } = await lastRequest();
    expect(JSON.parse(String(body))).toEqual({ reason: "worker 已經不在了" });
  });

  it("an EMPTY reason omits the key — no `reason` on the body at all", async () => {
    await httpApi.forceTaskDone("task-1", "");
    const { url, method, body } = await lastRequest();
    expect(url).toBe("/api/tasks/task-1/force-done");
    expect(method).toBe("POST");
    // The key itself must be gone. `toEqual({})` would also accept
    // `{reason: undefined}`; `Object.keys` will not.
    const parsed = JSON.parse(String(body));
    expect(Object.keys(parsed)).toEqual([]);
    expect("reason" in parsed).toBe(false);
  });

  it("a WHITESPACE-ONLY reason is the empty case, not a reason made of spaces", async () => {
    await httpApi.forceTaskDone("task-1", "   \n\t  ");
    const { body } = await lastRequest();
    const parsed = JSON.parse(String(body));
    expect(Object.keys(parsed)).toEqual([]);
  });

  it("the task id is the one that lands in the PATH, not in the body", async () => {
    await httpApi.forceTaskDone("task-zzz", "理由");
    const { url, body } = await lastRequest();
    expect(url).toBe("/api/tasks/task-zzz/force-done");
    expect(Object.keys(JSON.parse(String(body)))).toEqual(["reason"]);
  });
});

describe("httpApi.markTaskDone · path-only, no body", () => {
  // Kept under test even though the cockpit no longer offers an ordinary-close
  // button (T-192 scope: that route 403s every principal this SPA can be). The
  // port binding is still the frontend's one statement of what the route looks
  // like, and it was equally unguarded.
  it("POSTs to the mark-done path and sends no body", async () => {
    await httpApi.markTaskDone("task-1");
    const { url, method, body } = await lastRequest();
    expect(url).toBe("/api/tasks/task-1/mark-done");
    expect(method).toBe("POST");
    expect(body).toBeUndefined();
  });
});
