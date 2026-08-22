// Pins the boot-document WIRE contract of the real-backend adapter (T-791e,
// rewritten for the generic route family in T-3201).
//
// The routes are in the frozen spec, so these methods ride the schema-typed
// openapi-fetch client and a BE verb/path rename is a tsc error before anything
// runs. What is asserted here is the half tsc cannot see: which URL a given
// (kind, key) lands on, which body rides the POST, and that nothing branches on
// the kind any more.
//
// The contract, as the backend serves it:
//   GET  /api/boot-docs                     — which documents exist (NO text)
//   GET  /api/boot-docs/{kind}/{key}        — read one, folded
//   POST /api/boot-docs/{kind}/{key}        — whole-document replace {text}
//   POST /api/boot-docs/{kind}/{key}/reset  — restore the factory version
//
// 🔴 WHY THE OLD ASSERTIONS ARE GONE RATHER THAN LOOSENED. This file used to
// pin TWO route families with different shapes — a keyless singleton and a
// keyed one — because composing one template string for both had shipped
// system_interaction a `/global` segment the server has no route for. T-3201
// removed the shapes rather than the risk: one family, one path template, every
// document addressed the same way. The full path is still asserted on every
// call, so a composed-wrong URL is still caught; there is simply no second
// shape left for it to be wrong about.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";
import { codeForStatus } from "./errorCodes";

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
  read_only: false,
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

/** Reply with a listing rather than a document, for the one method that reads
 * one. Two rows, one of them read-only, so the mapper is exercised on both. */
function replyWithListing(): void {
  fetchMock.mockResolvedValueOnce(
    new Response(
      JSON.stringify({
        documents: [
          {
            kind: "offboard",
            key: "global",
            doc_name: "offboard sequence",
            read_only: false,
            size_chars: 120,
            cap_chars: 15000,
            is_default: true,
            has_seed: true,
          },
          {
            kind: "task_unblocked",
            key: "global",
            doc_name: "dependency-released notice",
            read_only: true,
            size_chars: 80,
            cap_chars: 15000,
            is_default: true,
            has_seed: true,
          },
        ],
      }),
      { status: 200, headers: { "Content-Type": "application/json" } }
    )
  );
}

describe("httpApi · boot-document wire methods", () => {
  it("listBootDocs GETs the listing and carries read_only through", async () => {
    replyWithListing();
    const rows = await httpApi.listBootDocs();
    const { url, method, body } = await lastCall();
    expect(url).toBe("/api/boot-docs");
    expect(method).toBe("GET");
    expect(body).toBeUndefined();
    expect(rows.map((r) => r.kind)).toEqual(["offboard", "task_unblocked"]);
    expect(rows.map((r) => r.readOnly)).toEqual([false, true]);
    expect(rows[0].docName).toBe("offboard sequence");
  });

  it("getBootDoc GETs the document's own kind/key path", async () => {
    const view = await httpApi.getBootDoc("boot_sequence", "claude");
    const { url, method, body } = await lastCall();
    expect(url).toBe("/api/boot-docs/boot_sequence/claude");
    expect(method).toBe("GET");
    expect(body).toBeUndefined();
    expect(view.text).toBe(WIRE_DOC.text);
    expect(view.capChars).toBe(15000);
    expect(view.hasSeed).toBe(true);
    expect(view.readOnly).toBe(false);
  });

  it("addresses every kind through the SAME path template", async () => {
    // The property that replaced "which of two families does this kind land
    // on": there is one family, so a kind that ships tomorrow needs no code.
    for (const kind of [
      "system_interaction",
      "offboard",
      "accelerated_stop",
      "task_closeout",
      "task_reassign_predecessor",
      "task_takeover_with_predecessor",
      "task_takeover_fresh",
      "task_unblocked",
    ] as const) {
      await httpApi.getBootDoc(kind, "global");
      expect((await lastCall()).url).toBe(`/api/boot-docs/${kind}/global`);
    }
  });

  it("saveBootDoc POSTs {text, allow_shrink} to the document's own path (NOT PUT)", async () => {
    await httpApi.saveBootDoc("boot_sequence", "codex", "新的內容");
    const { url, method, body } = await lastCall();
    expect(url).toBe("/api/boot-docs/boot_sequence/codex");
    expect(method).toBe("POST");
    // allow_shrink is FALSE here, the opposite of saveGlobalContext: emptying a
    // boot sequence ships agents with no instructions, and the way back to a
    // small document is the factory reset, not a whole-document wipe.
    expect(JSON.parse(String(body))).toEqual({
      text: "新的內容",
      allow_shrink: false,
    });

    await httpApi.saveBootDoc("system_interaction", "global", "新的內容");
    expect((await lastCall()).url).toBe(
      "/api/boot-docs/system_interaction/global"
    );
  });

  it("resetBootDoc POSTs …/reset with no body (NOT DELETE on the doc path)", async () => {
    await httpApi.resetBootDoc("boot_sequence", "claude");
    const claude = await lastCall();
    expect(claude.url).toBe("/api/boot-docs/boot_sequence/claude/reset");
    expect(claude.method).toBe("POST");
    expect(claude.body).toBeUndefined();

    await httpApi.resetBootDoc("system_interaction", "global");
    const sys = await lastCall();
    expect(sys.url).toBe("/api/boot-docs/system_interaction/global/reset");
    expect(sys.method).toBe("POST");
  });

  it("keeps the claude and codex keys apart across all three verbs", async () => {
    // The paired control for the ticket's headline risk: the two boot sequences
    // are different documents, so no verb may reach one while addressing the
    // other. Every URL below carries exactly the key it was handed.
    for (const key of ["claude", "codex"] as const) {
      const other = key === "claude" ? "codex" : "claude";
      await httpApi.getBootDoc("boot_sequence", key);
      expect((await lastCall()).url).toBe(`/api/boot-docs/boot_sequence/${key}`);
      expect((await lastCall()).url).not.toContain(other);
      await httpApi.saveBootDoc("boot_sequence", key, "x");
      expect((await lastCall()).url).toBe(`/api/boot-docs/boot_sequence/${key}`);
      expect((await lastCall()).url).not.toContain(other);
      await httpApi.resetBootDoc("boot_sequence", key);
      expect((await lastCall()).url).toBe(
        `/api/boot-docs/boot_sequence/${key}/reset`
      );
      expect((await lastCall()).url).not.toContain(other);
    }
  });

  it("throws the shared ApiError off the unified envelope, with .status", async () => {
    // Callers branch on `.status` (isHttpStatus), never on the message.
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error: { code: codeForStatus(400), message: "over the character limit" },
        }),
        { status: 400, headers: { "Content-Type": "application/json" } }
      )
    );
    await expect(
      httpApi.saveBootDoc("boot_sequence", "claude", "x")
    ).rejects.toMatchObject({
      status: 400,
      code: codeForStatus(400),
      serverMessage: "over the character limit",
    });
  });

  it("surfaces a read-only document's 405 as an ApiError carrying the server's words", async () => {
    // The refusal says what the document IS, not that the caller lacks a
    // permission — no principal may edit it, so pointing at authz would send an
    // owner looking for a role to grant. The cockpit quotes it verbatim.
    const refusal =
      "the dependency-released notice is a read-only document — nothing was written";
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({ error: { code: codeForStatus(405), message: refusal } }),
        { status: 405, headers: { "Content-Type": "application/json" } }
      )
    );
    await expect(
      httpApi.saveBootDoc("task_unblocked", "global", "x")
    ).rejects.toMatchObject({
      status: 405,
      code: codeForStatus(405),
      serverMessage: refusal,
    });
  });
});
