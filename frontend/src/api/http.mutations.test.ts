// Pins the mutation-side WIRE contracts of the real-backend adapter that the
// openapi-fetch migration was required to keep shape-identical:
//
//   1. PATCH semantics — patchMember/createRole: an UNSUPPLIED field must NOT
//      ride the body at all (not as null, not as undefined — the server would
//      reject / misread a null). Only supplied fields appear in the JSON.
//   2. activateMember body — always a PRESENT object (MemberActivateDTO):
//      `{}` is the honest "no machine override"; with a machineId it binds
//      `{machine_id}`.
//   3. deleteRole error contract — a 409 (member online) rejects with an
//      ApiError (api/errors.ts) carrying the unified error envelope
//      (`.status`/`.code`/`.serverMessage`); SettingsPage's isHttpStatus(e, 409)
//      branches on `.status` to surface 「有成員在線上，無法刪除」. The message
//      keeps the historical `http 409 for DELETE /api/roles/<key>` format.
//
// openapi-fetch drives a real `Request` through global fetch, so the stub
// returns real `new Response(...)` objects (fresh per call — a body is
// one-shot).

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";
import { ApiError } from "./errors";

const WIRE_MEMBER = {
  id: "m-1",
  name: "Mira",
  role: "assistant",
  online: false,
  presence: "offline",
  status: "active",
};

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const fetchMock = vi.fn(async () => jsonResponse(WIRE_MEMBER));

async function lastRequest(): Promise<{
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

beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockImplementation(async () => jsonResponse(WIRE_MEMBER));
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("httpApi · PATCH bodies carry ONLY the supplied fields", () => {
  it("patchMember {name} sends {name} — no model/effort keys, no nulls", async () => {
    await httpApi.patchMember("m-1", { name: "Ada" });
    const { url, method, body } = await lastRequest();
    expect(url).toBe("/api/members/m-1");
    expect(method).toBe("PATCH");
    expect(JSON.parse(String(body))).toEqual({ name: "Ada" });
  });

  it("patchMember {model, effort} leaves name off the body", async () => {
    await httpApi.patchMember("m-1", { model: "opus", effort: "high" });
    const { body } = await lastRequest();
    expect(JSON.parse(String(body))).toEqual({ model: "opus", effort: "high" });
  });

  it("createRole {name} sends {name} only — member_name/model/effort absent", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        role: { key: "r-1", name: "QA", definition_md: "", is_default: false },
        member: WIRE_MEMBER,
      })
    );
    await httpApi.createRole({ name: "QA" });
    const { url, method, body } = await lastRequest();
    expect(url).toBe("/api/roles");
    expect(method).toBe("POST");
    expect(JSON.parse(String(body))).toEqual({ name: "QA" });
  });
});

describe("httpApi · activateMember body (MemberActivateDTO)", () => {
  it("sends the honest {} when no machineId is given", async () => {
    await httpApi.activateMember("m-1");
    const { url, method, body } = await lastRequest();
    expect(url).toBe("/api/members/m-1/activate");
    expect(method).toBe("POST");
    expect(JSON.parse(String(body))).toEqual({});
  });

  it("binds {machine_id} when a machineId is given", async () => {
    await httpApi.activateMember("m-1", "mach-9");
    const { body } = await lastRequest();
    expect(JSON.parse(String(body))).toEqual({ machine_id: "mach-9" });
  });
});

describe("httpApi · perf-light query contracts (T-2b9d/cf91/ec2c)", () => {
  it("peekChat pulls the filtered+capped read-only window (peek=true), NOT the whole history", async () => {
    fetchMock.mockImplementation(async () => jsonResponse([]));
    await httpApi.peekChat("m-1");
    const { url, method } = await lastRequest();
    const q = new URLSearchParams(url.split("?")[1] ?? "");
    expect(method).toBe("GET");
    expect(url.split("?")[0]).toBe("/api/chat");
    // LOAD-BEARING: read-only (peek), scoped to the peer, capped — and NEVER
    // the old whole-company `limit=-1` pull. MUTANT: revert peekChat to
    // `{ limit: -1 }` and these go red.
    expect(q.get("peek")).toBe("true");
    expect(q.get("with")).toBe("m-1");
    expect(q.get("limit")).toBe("30");
    expect(q.get("limit")).not.toBe("-1");
  });

  it("listMembers({light}) sends fields=light; default omits it", async () => {
    fetchMock.mockImplementation(async () => jsonResponse([]));
    await httpApi.listMembers({ light: true });
    expect(
      new URLSearchParams((await lastRequest()).url.split("?")[1] ?? "").get(
        "fields"
      )
    ).toBe("light");
    await httpApi.listMembers();
    expect((await lastRequest()).url).toBe("/api/members");
  });

  it("listTasks({open}) sends open=true; default omits it (full population)", async () => {
    fetchMock.mockImplementation(async () => jsonResponse([]));
    await httpApi.listTasks({ open: true });
    expect(
      new URLSearchParams((await lastRequest()).url.split("?")[1] ?? "").get(
        "open"
      )
    ).toBe("true");
    await httpApi.listTasks();
    expect((await lastRequest()).url).toBe("/api/tasks");
  });

  it("listTaskTypes narrows the manuals read to view=list", async () => {
    fetchMock.mockImplementation(async () => jsonResponse([]));
    await httpApi.listTaskTypes();
    expect(
      new URLSearchParams((await lastRequest()).url.split("?")[1] ?? "").get(
        "view"
      )
    ).toBe("list");
  });
});

describe("httpApi · deleteRole 409 error contract (unified envelope)", () => {
  it("rejects with an ApiError carrying the envelope on member-online", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(
        { error: { code: "conflict", message: "role 'qa' has online member(s)" } },
        409
      )
    );
    const err = await httpApi.deleteRole("qa").then(
      () => null,
      (e: unknown) => e
    );
    // The exact predicate SettingsPage.tsx keys 有成員在線上 off (isHttpStatus).
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).code).toBe("conflict");
    expect((err as ApiError).serverMessage).toBe("role 'qa' has online member(s)");
    expect((err as ApiError).message).toBe("http 409 for DELETE /api/roles/qa");
  });
});
