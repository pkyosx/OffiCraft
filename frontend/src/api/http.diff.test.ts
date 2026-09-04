// The compare read (`GET /api/diff`, T-59) is one of the few calls that skip the
// typed openapi-fetch client, so the cross-cutting behaviour that client owns
// has to be pinned HERE instead of inherited:
//
//   1. the query it puts on the wire, including the two optional labels and the
//      signature;
//   2. exactly ONE credential rides: the owner token as `Authorization` on the
//      unsigned (session) flavour, and NOTHING but the `sig` on the signed one
//      — the server judges a bearer that is present and never falls through to
//      the signature, so a stale token beside a good sig would refuse the link;
//   3. a SIGNED call's 401 must NOT clear the session — the signature failed,
//      not the login, and a reader following a link may have no session to lose;
//      an unsigned call's 401 must still bounce to the auth layer;
//   4. every non-2xx becomes an ApiError carrying the unified error envelope;
//   5. a side the server marks `gone` survives the mapping as `gone`, not as an
//      empty text — the compare screen branches on exactly that.
//
// The MINT (`GET /api/diff/share-link`) is the other half of the same feature
// and is pinned at the bottom of this file, in both faces — the http adapter's
// and the offline mock's.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";
import { mockApi } from "./mock";
import { DIFF_PATH, parseDiffParams } from "../lib/diffLink";
import { ApiError } from "./errors";
import { codeForStatus } from "./errorCodes";
import { AUTH_EXPIRED_EVENT } from "./client";
import { TOKEN_KEY } from "./auth";

const PARAMS = {
  before: "att-0123456789ab",
  after: "doc:global_context/global/current/text",
};

const BODY = {
  before: { address: "att-0123456789ab", text: "alpha", label: "改動前", gone: false },
  after: { address: "doc:x", text: "beta", label: "改動後", gone: false },
};

function stubFetch(response: Response) {
  const fetchMock = vi.fn(async () => response.clone());
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const ok = (body: unknown) =>
  new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });

const refused = (status: number) =>
  new Response(
    JSON.stringify({ error: { code: codeForStatus(status), message: "nope" } }),
    { status, headers: { "Content-Type": "application/json" } },
  );

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe("getDiff", () => {
  const sentHeaders = (fetchMock: { mock: { calls: unknown[][] } }) =>
    ((fetchMock.mock.calls[0][1] as RequestInit).headers ?? {}) as Record<string, string>;

  it("asks for both sides in one request, with the labels and signature the url carried", async () => {
    const fetchMock = stubFetch(ok(BODY));

    await httpApi.getDiff({
      ...PARAMS,
      labelBefore: "改動前",
      labelAfter: "改動後",
      sig: "s+g/1",
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    const query = new URL(url, "http://localhost").searchParams;
    expect(query.get("before")).toBe(PARAMS.before);
    expect(query.get("after")).toBe(PARAMS.after);
    expect(query.get("label_before")).toBe("改動前");
    expect(query.get("label_after")).toBe("改動後");
    expect(query.get("sig")).toBe("s+g/1");
  });

  it("sends the session token on the unsigned flavour, where it IS the credential", async () => {
    const fetchMock = stubFetch(ok(BODY));
    localStorage.setItem(TOKEN_KEY, "owner-token");

    await httpApi.getDiff(PARAMS);

    expect(sentHeaders(fetchMock).Authorization).toBe("Bearer owner-token");
  });

  it("opens a SIGNED link on a browser whose token has expired", async () => {
    // The regression this file exists to catch. The server judges a bearer that
    // is PRESENT and never falls through to the sig, so a dead token sent
    // alongside a perfectly good signature is a 401 the reader can never get
    // past: the signed page mounts ahead of the auth wall, so nothing ever
    // probes the session or clears the dead token, and no retry follows.
    // The fix is that the signed flavour sends no Authorization at all.
    localStorage.setItem(TOKEN_KEY, "expired-token");
    const fetchMock = vi.fn(async (_url: string, init: RequestInit) =>
      (init.headers as Record<string, string>).Authorization
        ? refused(401)
        : ok(BODY),
    );
    vi.stubGlobal("fetch", fetchMock);

    const pair = await httpApi.getDiff({ ...PARAMS, sig: "server-minted" });

    expect(pair.before.text).toBe("alpha");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(sentHeaders(fetchMock).Authorization).toBeUndefined();
    // The token was never the credential here, so it was never judged either.
    expect(localStorage.getItem(TOKEN_KEY)).toBe("expired-token");
  });

  it("keeps a GONE side gone instead of comparing against an empty text", async () => {
    stubFetch(
      ok({
        before: { address: PARAMS.before, gone: true, gone_reason: "pruned" },
        after: { address: PARAMS.after, text: "beta", label: "", gone: false },
      }),
    );
    const pair = await httpApi.getDiff(PARAMS);
    expect(pair.before.gone).toBe(true);
    expect(pair.before.goneReason).toBe("pruned");
    // An EMPTY label is the server saying "the url gave none" — it must reach
    // the screen as absent, so the reader writes its own heading rather than
    // drawing a blank one.
    expect(pair.after).toEqual({
      address: PARAMS.after,
      text: "beta",
      label: undefined,
      gone: false,
      goneReason: undefined,
    });
  });

  it("rejects with the server's error envelope", async () => {
    stubFetch(refused(404));
    await expect(httpApi.getDiff(PARAMS)).rejects.toBeInstanceOf(ApiError);
    stubFetch(refused(404));
    await expect(httpApi.getDiff(PARAMS)).rejects.toMatchObject({
      status: 404,
      code: codeForStatus(404),
      serverMessage: "nope",
    });
  });

  it("does NOT end the session when a SIGNED link is refused", async () => {
    const fetchMock = stubFetch(refused(401));
    localStorage.setItem(TOKEN_KEY, "owner-token");
    const expired = vi.fn();
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);

    await expect(httpApi.getDiff({ ...PARAMS, sig: "bad" })).rejects.toBeInstanceOf(
      ApiError,
    );

    window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
    // The signature really is the only credential this call carried, so the
    // 401 really is the signature being refused — and blaming the session for
    // it would log the owner out over someone else's stale link, or out of a
    // tab that never had a session at all.
    expect(sentHeaders(fetchMock).Authorization).toBeUndefined();
    expect(expired).not.toHaveBeenCalled();
    expect(localStorage.getItem(TOKEN_KEY)).toBe("owner-token");
  });

  it("DOES end the session when the same 401 answers a session call", async () => {
    stubFetch(refused(401));
    localStorage.setItem(TOKEN_KEY, "owner-token");
    const expired = vi.fn();
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);

    await expect(httpApi.getDiff(PARAMS)).rejects.toBeInstanceOf(ApiError);

    window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
    expect(expired).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();
  });
});

// The MINT (`GET /api/diff/share-link`, T-59) — the external, signed flavour of
// the same url. Unlike the read above it DOES ride the typed client, so what is
// pinned here is the query it asks for and the one value it must never forward.
describe("getDiffShareLink", () => {
  const MINTED = "/diff?before=att-0123456789ab&after=doc%3Ax&sig=server-made";

  // The typed openapi-fetch client hands `fetch` a Request, not a url string —
  // reading `calls[0][0]` as a string stringifies the object instead.
  const asked = (fetchMock: { mock: { calls: unknown[][] } }) =>
    new URL((fetchMock.mock.calls[0][0] as Request).url, "http://x");

  it("asks for both sides and the labels, and returns the server-relative url", async () => {
    const fetchMock = stubFetch(ok({ url: MINTED }));

    const url = await httpApi.getDiffShareLink({
      ...PARAMS,
      labelBefore: "改動前",
      labelAfter: "改動後",
    });

    expect(url).toBe(MINTED);
    const sent = asked(fetchMock);
    expect(sent.pathname).toBe("/api/diff/share-link");
    expect(sent.searchParams.get("before")).toBe(PARAMS.before);
    expect(sent.searchParams.get("after")).toBe(PARAMS.after);
    expect(sent.searchParams.get("label_before")).toBe("改動前");
    expect(sent.searchParams.get("label_after")).toBe("改動後");
  });

  it("never forwards a signature — the signature is what this call PRODUCES", async () => {
    const fetchMock = stubFetch(ok({ url: MINTED }));

    await httpApi.getDiffShareLink({ ...PARAMS, sig: "one-the-caller-was-holding" });

    const sent = asked(fetchMock);
    // The route declares four query parameters and `sig` is not one of them:
    // asking the server to sign a query that already carries a signature is a
    // question it was never given.
    expect(sent.searchParams.get("sig")).toBeNull();
    expect(sent.searchParams.get("label_before")).toBeNull();
  });

  it("rejects with an ApiError rather than a url the caller would paste", async () => {
    stubFetch(refused(422));
    await expect(httpApi.getDiffShareLink(PARAMS)).rejects.toBeInstanceOf(ApiError);
  });
});

// The OFFLINE cockpit's face of the same mint. It has no secret and therefore
// no verifiable credential, so what it owes is the SHAPE: a /diff url the
// compare screen's own parser reads back, carrying a sig — otherwise the copy
// control cannot be exercised offline at all.
describe("mockApi.getDiffShareLink", () => {
  it("mints a /diff url that parses back, with a sig and without the caller's", async () => {
    const url = await mockApi.getDiffShareLink({
      ...PARAMS,
      labelBefore: "改動前",
      sig: "one-the-caller-was-holding",
    });

    const parsed = parseDiffParams(url.slice(url.indexOf("?")));
    expect(url.startsWith(DIFF_PATH)).toBe(true);
    expect(parsed).toMatchObject({
      before: PARAMS.before,
      after: PARAMS.after,
      labelBefore: "改動前",
    });
    expect(parsed?.sig).toBeTruthy();
    expect(parsed?.sig).not.toBe("one-the-caller-was-holding");
  });
});
