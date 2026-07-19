// Pins the cross-cutting contracts of the openapi-fetch client (client.ts):
//
//   1. auth header — the owner JWT rides every request as `Authorization:
//      Bearer` (onRequest middleware); with NO token the header is absent
//      (honest 401, never fabricated auth).
//   2. 401 — handleUnauthorized(): the dead token is cleared and
//      `oc-auth-expired` is dispatched (AuthGate bounces to login), and the
//      call STILL rejects (no fake empty state).
//   3. error contract — every non-2xx throws an ApiError (api/errors.ts)
//      carrying the server's unified error envelope
//      `{"error":{"code","message"}}` (docs/design/api-error-envelope.md) as
//      `.status`/`.code`/`.serverMessage`. LOAD-BEARING: SettingsPage's
//      isHttpStatus(e, 409) branches on `.status` to surface
//      「有成員在線上，無法刪除」 on deleteRole. Error.message keeps the
//      historical `http <status> for <METHOD> <path>` format verbatim.
//
// openapi-fetch drives a real `Request` through global fetch, so the stub must
// return a real `new Response(...)` (a plain {ok, json} object breaks inside
// the client) — the same rule http.global-context.test.ts follows.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { client } from "./client";
import { ApiError } from "./errors";
import { TOKEN_KEY } from "./auth";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** The unified error envelope exactly as service.errors emits it. */
function errorResponse(status: number, code: string, message: string): Response {
  return jsonResponse({ error: { code, message } }, status);
}

const fetchMock = vi.fn(async () => jsonResponse([]));

function lastRequest(): Request {
  const calls = fetchMock.mock.calls as unknown as [Request][];
  return calls[calls.length - 1][0];
}

beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockImplementation(async () => jsonResponse([]));
  vi.stubGlobal("fetch", fetchMock);
  localStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe("client · auth middleware", () => {
  it("attaches the owner JWT as Authorization: Bearer", async () => {
    localStorage.setItem(TOKEN_KEY, "tok-123");
    await client.GET("/api/members");
    const req = lastRequest();
    expect(req.method).toBe("GET");
    expect(new URL(req.url).pathname).toBe("/api/members");
    expect(req.headers.get("Authorization")).toBe("Bearer tok-123");
  });

  it("sends NO Authorization header without a token (honest 401 path)", async () => {
    await client.GET("/api/members");
    expect(lastRequest().headers.get("Authorization")).toBeNull();
  });

  it("401 clears the token, dispatches oc-auth-expired AND rejects", async () => {
    localStorage.setItem(TOKEN_KEY, "expired-tok");
    const onExpired = vi.fn();
    window.addEventListener("oc-auth-expired", onExpired);
    fetchMock.mockImplementation(async () =>
      errorResponse(401, "unauthorized", "missing credentials")
    );
    await expect(client.GET("/api/members")).rejects.toThrow(
      "http 401 for GET /api/members"
    );
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();
    expect(onExpired).toHaveBeenCalledTimes(1);
    window.removeEventListener("oc-auth-expired", onExpired);
  });
});

describe("client · non-2xx error contract (unified envelope → ApiError)", () => {
  it("throws an ApiError carrying status/code/serverMessage off the envelope", async () => {
    fetchMock.mockImplementation(async () =>
      errorResponse(409, "conflict", "role 'qa' has online member(s)")
    );
    const err = await client
      .DELETE("/api/roles/{role}", { params: { path: { role: "qa" } } })
      .then(
        () => null,
        (e: unknown) => e
      );
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).code).toBe("conflict");
    expect((err as ApiError).serverMessage).toBe("role 'qa' has online member(s)");
    // The historical message format is kept verbatim (logs / legacy matching).
    expect((err as ApiError).message).toBe("http 409 for DELETE /api/roles/qa");
  });

  it("is honest-empty when the body carries no envelope (proxy error page)", async () => {
    fetchMock.mockImplementation(
      async () => new Response("<html>bad gateway</html>", { status: 502 })
    );
    const err = await client.GET("/api/members").then(
      () => null,
      (e: unknown) => e
    );
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(502);
    expect((err as ApiError).code).toBe("");
    expect((err as ApiError).serverMessage).toBe("");
    expect((err as ApiError).message).toBe("http 502 for GET /api/members");
  });

  it("includes the query string in the thrown path (listChat parity)", async () => {
    fetchMock.mockImplementation(async () =>
      errorResponse(500, "internal_error", "internal server error")
    );
    await expect(
      client.GET("/api/chat", {
        params: { query: { with: "mira", limit: 5 } },
      })
    ).rejects.toThrow("http 500 for GET /api/chat?with=mira&limit=5");
  });
});
