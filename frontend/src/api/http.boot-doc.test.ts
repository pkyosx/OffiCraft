// Pins the boot-context block WIRE contract of the real-backend adapter
// (T-791e).
//
// The three routes are in the frozen spec now, so these methods ride the
// schema-typed openapi-fetch client like every other method in http.ts and a
// BE verb/path rename is a tsc error before anything runs. What is asserted
// here is the half tsc cannot see: WHICH of the two route families a given
// kind lands on, and which key rides the path.
//
// The contract, as the backend actually serves it:
//   GET  /api/system-interaction               — read (a singleton; NO key segment)
//   POST /api/system-interaction               — whole-document replace {text}
//   POST /api/system-interaction/reset         — restore the factory version
//   GET  /api/boot-sequence/{runtime_key}      — read one runtime's document
//   POST /api/boot-sequence/{runtime_key}      — whole-document replace {text}
//   POST /api/boot-sequence/{runtime_key}/reset— restore the factory version
//
// 🔴 The two families are NOT the same shape, and an earlier version of this
// adapter composed one template string for both — giving system_interaction a
// `/global` segment the server has no route for. Every call below therefore
// asserts the full path, not a prefix.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";

const WIRE_DOC = {
  kind: "boot_sequence",
  key: "claude",
  text: "# 啟動程序",
  owner_id: "owner",
  schema_version: 3,
  size_chars: 6,
  cap_chars: 15000,
  is_default: false,
  has_seed: true,
};

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

/** Normalise the last fetch call. These methods ride the openapi-fetch client,
 * which calls fetch with ONE `Request` argument. */
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

describe("httpApi · boot-context block wire methods", () => {
  it("getBootDoc GETs the runtime's own boot-sequence route", async () => {
    const view = await httpApi.getBootDoc("boot_sequence", "claude");
    const { url, method, body } = await lastCall();
    expect(url).toBe("/api/boot-sequence/claude");
    expect(method).toBe("GET");
    expect(body).toBeUndefined();
    expect(view.text).toBe(WIRE_DOC.text);
    expect(view.capChars).toBe(15000);
    expect(view.hasSeed).toBe(true);
  });

  it("reads system_interaction as a singleton, with no key segment in the path", async () => {
    // The key ("global") is implied by the kind — the server serves ONE
    // system-interaction document and its route carries no parameter. Sending
    // /api/system-interaction/global is a 404, and it is a 404 no test that
    // runs against a mock can see.
    await httpApi.getBootDoc("system_interaction", "global");
    expect((await lastCall()).url).toBe("/api/system-interaction");
  });

  it("saveBootDoc POSTs {text, allow_shrink} to the document's own path (NOT PUT)", async () => {
    await httpApi.saveBootDoc("boot_sequence", "codex", "新的內容");
    const { url, method, body } = await lastCall();
    expect(url).toBe("/api/boot-sequence/codex");
    expect(method).toBe("POST");
    // allow_shrink is FALSE here, the opposite of saveGlobalContext: emptying a
    // boot sequence ships agents with no instructions, and the way back to a
    // small document is the factory reset, not a whole-document wipe.
    expect(JSON.parse(String(body))).toEqual({
      text: "新的內容",
      allow_shrink: false,
    });

    await httpApi.saveBootDoc("system_interaction", "global", "新的內容");
    expect((await lastCall()).url).toBe("/api/system-interaction");
  });

  it("resetBootDoc POSTs …/reset with no body (NOT DELETE on the doc path)", async () => {
    await httpApi.resetBootDoc("boot_sequence", "claude");
    const claude = await lastCall();
    expect(claude.url).toBe("/api/boot-sequence/claude/reset");
    expect(claude.method).toBe("POST");
    expect(claude.body).toBeUndefined();

    await httpApi.resetBootDoc("system_interaction", "global");
    const sys = await lastCall();
    expect(sys.url).toBe("/api/system-interaction/reset");
    expect(sys.method).toBe("POST");
  });

  it("keeps the claude and codex keys apart across all three verbs", async () => {
    // The paired control for the ticket's headline risk: the two boot sequences
    // are different documents, so no verb may reach one while addressing the
    // other. Every URL below carries exactly the key it was handed.
    for (const key of ["claude", "codex"] as const) {
      const other = key === "claude" ? "codex" : "claude";
      await httpApi.getBootDoc("boot_sequence", key);
      expect((await lastCall()).url).toBe(`/api/boot-sequence/${key}`);
      expect((await lastCall()).url).not.toContain(other);
      await httpApi.saveBootDoc("boot_sequence", key, "x");
      expect((await lastCall()).url).toBe(`/api/boot-sequence/${key}`);
      expect((await lastCall()).url).not.toContain(other);
      await httpApi.resetBootDoc("boot_sequence", key);
      expect((await lastCall()).url).toBe(`/api/boot-sequence/${key}/reset`);
      expect((await lastCall()).url).not.toContain(other);
    }
  });

  it("throws the shared ApiError off the unified envelope, with .status", async () => {
    // Callers branch on `.status` (isHttpStatus), never on the message.
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error: { code: "bad_request", message: "over the character limit" },
        }),
        { status: 400, headers: { "Content-Type": "application/json" } }
      )
    );
    await expect(
      httpApi.saveBootDoc("boot_sequence", "claude", "x")
    ).rejects.toMatchObject({
      status: 400,
      code: "bad_request",
      serverMessage: "over the character limit",
    });
  });
});
