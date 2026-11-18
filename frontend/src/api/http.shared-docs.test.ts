import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

const fetchMock = vi.fn(async () => jsonResponse({}));

async function lastRequest() {
  const calls = fetchMock.mock.calls as unknown as [Request][];
  const req = calls[calls.length - 1]![0];
  const u = new URL(req.url);
  return {
    url: decodeURIComponent(u.pathname + u.search),
    method: req.method,
    contentType: req.headers.get("content-type"),
    body: await req.clone().text(),
  };
}

const WIRE_DOC = {
  id: "sd-0c41e97a2b58",
  kind: "link",
  attachment_id: "att-9e8d7c6b5a40",
  name: "API 規格",
  description: "",
  filename: "",
  mime: "text/uri-list",
  url: "https://example.com/spec",
  created_ts: 100,
  created_by: "mira",
  updated_ts: 200,
  updated_by: "owner",
  archived_ts: 300,
  archived_by: "owner",
  version_count: 2,
};

beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockImplementation(async () => jsonResponse({}));
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("httpApi · shared docs", () => {
  it("uploadAttachment sends the raw bytes named by ?filename= and answers the new id", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ id: "att-000000000001", mime: "text/markdown", filename: "notes.md" }),
    );
    const id = await httpApi.uploadAttachment(new Blob(["# hi"]), "notes.md");
    expect(id).toBe("att-000000000001");
    expect(await lastRequest()).toEqual({
      url: "/api/chat/attachments?filename=notes.md",
      method: "POST",
      contentType: "application/octet-stream",
      body: "# hi",
    });
  });

  it("listSharedDocs asks for archived ones only when told to, and maps every field", async () => {
    fetchMock.mockImplementation(async () => jsonResponse({ docs: [WIRE_DOC] }));
    expect(await httpApi.listSharedDocs(true)).toEqual([
      {
        id: "sd-0c41e97a2b58",
        kind: "link",
        attachmentId: "att-9e8d7c6b5a40",
        name: "API 規格",
        description: "",
        filename: "",
        mime: "text/uri-list",
        url: "https://example.com/spec",
        createdTs: 100,
        createdBy: "mira",
        updatedTs: 200,
        updatedBy: "owner",
        archivedTs: 300,
        archivedBy: "owner",
        versionCount: 2,
      },
    ]);
    expect((await lastRequest()).url).toBe("/api/shared-docs?include_archived=true");
    fetchMock.mockImplementation(async () => jsonResponse({ docs: [] }));
    await httpApi.listSharedDocs(false);
    expect((await lastRequest()).url).toBe("/api/shared-docs");
  });

  it("getSharedDoc carries the inline text and why it is omitted", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ ...WIRE_DOC, text: null, text_omitted: "too_large" }),
    );
    const doc = await httpApi.getSharedDoc("sd-0c41e97a2b58");
    expect([doc.text, doc.textOmitted]).toEqual([null, "too_large"]);
  });

  it("createSharedDoc sends the content field its kind uses and answers the new id", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({ id: "sd-000000000002", updated_ts: 1, version_count: 1, archived_ts: null }),
    );
    const id = await httpApi.createSharedDoc({
      kind: "link",
      name: "Board",
      description: "roadmap",
      url: "https://example.com/board",
    });
    expect(id).toBe("sd-000000000002");
    const req = await lastRequest();
    expect([req.method, req.url]).toEqual(["POST", "/api/shared-docs"]);
    expect(JSON.parse(req.body)).toEqual({
      kind: "link",
      name: "Board",
      description: "roadmap",
      attachment_id: null,
      url: "https://example.com/board",
    });
  });

  it("replaceSharedDoc sends null for every field it keeps", async () => {
    await httpApi.replaceSharedDoc("sd-1", { attachmentId: "att-000000000003" });
    const req = await lastRequest();
    expect([req.method, req.url]).toEqual(["POST", "/api/shared-docs/sd-1/replace"]);
    expect(JSON.parse(req.body)).toEqual({
      name: null,
      description: null,
      attachment_id: "att-000000000003",
      url: null,
    });
  });

  it("the version, restore, archive, unarchive and delete calls hit their routes", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        doc_id: "sd-1",
        versions: [
          {
            id: 7,
            kind: "file",
            attachment_id: "att-1",
            name: "Runbook",
            description: "",
            filename: "runbook.md",
            mime: "text/markdown",
            url: "/api/chat/attachment/att-1",
            created_ts: 5,
            created_by: "mira",
          },
        ],
      }),
    );
    expect((await httpApi.listSharedDocVersions("sd-1")).map((v) => [v.id, v.filename])).toEqual([
      [7, "runbook.md"],
    ]);
    const seen: string[] = [`${(await lastRequest()).method} ${(await lastRequest()).url}`];
    fetchMock.mockImplementation(async () => jsonResponse({}));
    for (const call of [
      () => httpApi.restoreSharedDocVersion("sd-1", 7),
      () => httpApi.archiveSharedDoc("sd-1"),
      () => httpApi.unarchiveSharedDoc("sd-1"),
      () => httpApi.deleteSharedDoc("sd-1"),
    ]) {
      await call();
      const req = await lastRequest();
      seen.push(`${req.method} ${req.url}`);
    }
    expect(seen).toEqual([
      "GET /api/shared-docs/sd-1/versions",
      "POST /api/shared-docs/sd-1/versions/7/restore",
      "POST /api/shared-docs/sd-1/archive",
      "POST /api/shared-docs/sd-1/unarchive",
      "DELETE /api/shared-docs/sd-1",
    ]);
  });
});
