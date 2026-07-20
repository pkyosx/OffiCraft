// Pins the /api/global-context WIRE contract of the real-backend adapter.
//
// The frozen route surface (spec/openapi.json) registers:
//   GET  /api/global-context        — read the user-custom additive block
//   POST /api/global-context        — whole-block replace {text}
//   POST /api/global-context/reset  — idempotent tombstone reset
//
// A previous version of http.ts sent PUT /api/global-context and
// DELETE /api/global-context — both 405 against the real backend. These tests
// pin the corrected method + path so the drift cannot silently come back.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";

const WIRE_DOC = {
  text: "owner additions",
  owner_id: "owner",
  schema_version: 3,
  is_default: false,
};

// The openapi-fetch client drives a real `Request` through global fetch and
// parses a real `Response` — a plain {ok, json} stub object breaks inside it,
// so the stub returns `new Response(...)` (fresh per call; a body is one-shot).
const fetchMock = vi.fn(
  async () =>
    new Response(JSON.stringify(WIRE_DOC), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    })
);

beforeEach(() => {
  fetchMock.mockClear();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/** Normalise the last fetch call for assertions. Every httpApi method rides
 * the openapi-fetch client, which calls fetch with ONE `Request` argument. */
async function lastCall(): Promise<{
  url: string;
  method: string;
  body: string | undefined;
}> {
  const calls = fetchMock.mock.calls as unknown as [Request][];
  const req = calls[calls.length - 1][0];
  const u = new URL(req.url);
  const text = await req.clone().text();
  return {
    url: u.pathname + u.search,
    method: req.method,
    body: text || undefined,
  };
}

describe("httpApi · global-context wire methods", () => {
  it("getGlobalContext GETs /api/global-context", async () => {
    const view = await httpApi.getGlobalContext();
    const { url, method } = await lastCall();
    expect(url).toBe("/api/global-context");
    expect(method).toBe("GET");
    expect(view.text).toBe(WIRE_DOC.text);
    expect(view.isDefault).toBe(false);
  });

  it("saveGlobalContext POSTs /api/global-context with {text, allow_shrink} (NOT PUT)", async () => {
    await httpApi.saveGlobalContext("owner additions");
    const { url, method, body } = await lastCall();
    expect(url).toBe("/api/global-context");
    expect(method).toBe("POST");
    // allow_shrink (T-2d99): the server refuses a non-empty → empty whole-doc
    // replace unless the caller opts in. That guard is aimed at BLIND agent
    // write-backs; the owner clearing this textarea has already expressed the
    // intent, so this seam always opts in.
    expect(JSON.parse(String(body))).toEqual({
      text: "owner additions",
      allow_shrink: true,
    });
  });

  it("resetGlobalContext POSTs /api/global-context/reset (NOT DELETE)", async () => {
    await httpApi.resetGlobalContext();
    const { url, method, body } = await lastCall();
    expect(url).toBe("/api/global-context/reset");
    expect(method).toBe("POST");
    expect(body).toBeUndefined(); // the reset route takes no body
  });
});
