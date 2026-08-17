// Pins the /api/document-history WIRE contract of the real-backend adapter
// (T-7d33). The frozen route surface (spec/openapi.json) registers:
//   GET  /api/document-history/{kind}/{key}              — the DIRECTORY
//   GET  /api/document-history/{kind}/{key}/{id}         — ONE revision's body
//   POST /api/document-history/{kind}/{key}/{id}/restore — restore one
//
// 🔴 THE FIELD NAMES ARE THE CONTRACT, and this file is where they are pinned.
// The directory row names its size map `field_chars`; the cockpit's view model
// calls it `sizes`. Reading the view name off the wire would throw nothing and
// fail nothing — the map would simply be missing, every size would read 0, and
// the version list would draw every revision as empty while marking every
// restore as safe. So the fixtures below speak the SERVER's names, and the
// assertions are on values only the server's names can produce.
//
// The two things that can silently drift here are the METHOD (a restore is a
// POST on its own sub-path, not a PUT on the revision) and how the composite
// lessons key "<role_key>::<task_type>" is placed — it is ONE path segment, so
// a naive split would address a route that does not exist.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";

/** ONE catalogue row, in the shape `DocumentHistoryDTO` declares: no text, the
 * per-field counts under `field_chars`, and `tombstoned` as its own boolean
 * rather than an entry of the map. */
const WIRE_ROW = {
  id: 7,
  created_ts: 1_753_000_000,
  actor_id: "owner",
  tombstoned: false,
  field_chars: { text: 16 },
};

/** The BODY of that revision (`DocumentHistoryVersionDTO`) — the only
 * document-history answer that carries prose. */
const WIRE_BODY = {
  kind: "global_context",
  key: "global",
  id: 7,
  content: { text: "an earlier draft", tombstoned: "false" },
};

/** The RESTORE RECEIPT (`DocumentHistoryRestoreDTO`) — deliberately not the
 * catalogue row: it says what the live document now holds. */
const WIRE_RESTORE = {
  id: 7,
  content: { text: "an earlier draft", tombstoned: "false" },
  created_ts: 1_753_000_000,
  actor_id: "owner",
};

let body: unknown = [WIRE_ROW];

const fetchMock = vi.fn(
  async () =>
    new Response(JSON.stringify(body), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    })
);

beforeEach(() => {
  body = [WIRE_ROW];
  fetchMock.mockClear();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/** Normalise the last fetch call — every httpApi method rides the
 * openapi-fetch client, which calls fetch with ONE `Request` argument. */
async function lastCall(): Promise<{
  url: string;
  method: string;
  body: string | undefined;
}> {
  const calls = fetchMock.mock.calls as unknown as [Request][];
  const req = calls[calls.length - 1][0];
  const u = new URL(req.url);
  return {
    url: u.pathname + u.search,
    method: req.method,
    body: (await req.clone().text()) || undefined,
  };
}

describe("httpApi · document-history wire methods", () => {
  it("listDocumentHistory GETs the kind/key path and answers the DIRECTORY, never the text", async () => {
    const versions = await httpApi.listDocumentHistory(
      "global_context",
      "global"
    );
    const { url, method } = await lastCall();
    expect(url).toBe("/api/document-history/global_context/global");
    expect(method).toBe("GET");
    // T-1170. `sizes` here can ONLY have come from the wire's `field_chars`:
    // the answer carries no text to measure, so an adapter reading any other
    // name produces `{}` — the silent 0 this pin exists to catch.
    expect(versions).toEqual([
      {
        id: 7,
        createdTs: 1_753_000_000,
        actorId: "owner",
        tombstoned: false,
        sizes: { text: 16 },
      },
    ]);
  });

  it("listDocumentHistory carries no text at all — the row has no content to read", async () => {
    const [row] = await httpApi.listDocumentHistory("global_context", "global");
    // The directory row is a picker entry. Nothing above this seam may reach a
    // revision's prose through it, which is what makes "read the text off the
    // list" a compile error rather than a blank pane.
    expect(row).not.toHaveProperty("content");
    // `tombstoned` is a FLAG, never an entry of the size map — counting the
    // characters of the string "true" would put a 4 where a reader looks for
    // how long a field of the document was.
    expect(row.sizes).not.toHaveProperty("tombstoned");
  });

  it("getDocumentRevision GETs the /{id} sub-path and answers that revision's text", async () => {
    body = WIRE_BODY;
    const revision = await httpApi.getDocumentRevision(
      "global_context",
      "global",
      7
    );
    const { url, method, body: sent } = await lastCall();
    // The id is a path segment of its own — this read NAMES one revision
    // instead of re-reading the list and picking, which is what stops three
    // documents being downloaded so that one can be shown.
    expect(url).toBe("/api/document-history/global_context/global/7");
    expect(method).toBe("GET");
    expect(sent).toBeUndefined();
    expect(revision).toEqual({
      id: 7,
      content: { text: "an earlier draft", tombstoned: "false" },
    });
  });

  it("listDocumentHistory keeps a lessons composite key in one path segment", async () => {
    await httpApi.listDocumentHistory("lessons", "assistant::general");
    const { url } = await lastCall();
    // "::" is not a path separator here — the server splits the key itself
    // (historyKeyParts), so the cockpit must not split it into two segments.
    expect(decodeURIComponent(url)).toBe(
      "/api/document-history/lessons/assistant::general"
    );
  });

  // T-40f0: the 初始版本 read. The METHOD is the contract here — this is the one
  // seam on which "look at the shipped default" must not be able to become
  // "write the shipped default", so a GET is asserted, not just the path.
  it("getDocumentSeed GETs the /seed sub-path and sends no body", async () => {
    body = {
      kind: "role_definition",
      key: "assistant",
      content: { definition_md: "the shipped persona", tombstoned: "true" },
    };
    const seed = await httpApi.getDocumentSeed("role_definition", "assistant");
    const { url, method, body: sent } = await lastCall();
    expect(url).toBe("/api/document-history/role_definition/assistant/seed");
    expect(method).toBe("GET");
    expect(sent).toBeUndefined();
    expect(seed).toEqual({
      kind: "role_definition",
      key: "assistant",
      content: { definition_md: "the shipped persona", tombstoned: "true" },
    });
  });

  it("restoreDocumentHistory POSTs the /{id}/restore sub-path with no body", async () => {
    body = WIRE_RESTORE;
    const restored = await httpApi.restoreDocumentHistory(
      "task_manual",
      "tm-abc123",
      7
    );
    const { url, method, body: sent } = await lastCall();
    expect(url).toBe("/api/document-history/task_manual/tm-abc123/7/restore");
    expect(method).toBe("POST");
    expect(sent).toBeUndefined();
    expect(restored.id).toBe(7);
  });
});
