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

  it("asks for both sides in one request, with the labels and share link the url carried", async () => {
    const fetchMock = stubFetch(ok(BODY));

    await httpApi.getDiff({
      ...PARAMS,
      labelBefore: "改動前",
      labelAfter: "改動後",
      link: "s+g/1",
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    const query = new URL(url, "http://localhost").searchParams;
    expect(query.get("before")).toBe(PARAMS.before);
    expect(query.get("after")).toBe(PARAMS.after);
    expect(query.get("label_before")).toBe("改動前");
    expect(query.get("label_after")).toBe("改動後");
    expect(query.get("link")).toBe("s+g/1");
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

    const pair = await httpApi.getDiff({ ...PARAMS, link: "server-minted" });

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

  it.each([
    [401, { sig: "bad" }],
    [410, { link: "revoked" }],
  ])("does NOT end the session when an external link is refused (%i)", async (status, credential) => {
    const fetchMock = stubFetch(refused(status));
    localStorage.setItem(TOKEN_KEY, "owner-token");
    const expired = vi.fn();
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);

    await expect(httpApi.getDiff({ ...PARAMS, ...credential })).rejects.toMatchObject({
      status,
    });

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

const WIRE_LINK = {
  id: "sl-0123456789ab",
  kind: "diff",
  attachment_id: null,
  attachment_filename: null,
  before: PARAMS.before,
  after: PARAMS.after,
  label_before: "改動前",
  label_after: "",
  created_by: "owner",
  created_ts: 1792650600,
  expires_ts: null,
};

// The typed openapi-fetch client hands `fetch` a Request, not a url string.
const askedRequest = (fetchMock: { mock: { calls: unknown[][] } }) =>
  fetchMock.mock.calls[0][0] as Request;

describe("createDiffShareLink", () => {
  const MINTED = "/diff?after=doc%3Ax&before=att-0123456789ab&link=server-made";

  it("posts both sides, the labels and the expiry, and returns the url once", async () => {
    const fetchMock = stubFetch(ok({ url: MINTED, share_link: WIRE_LINK }));

    const created = await httpApi.createDiffShareLink(
      { ...PARAMS, labelBefore: "改動前", link: "one-the-caller-was-holding" },
      1792737000,
    );

    expect(created.url).toBe(MINTED);
    expect(created.link).toMatchObject({ id: "sl-0123456789ab", kind: "diff", expiresTs: null });
    const req = askedRequest(fetchMock);
    expect(req.method).toBe("POST");
    expect(new URL(req.url).pathname).toBe("/api/diff/share-link");
    expect(await req.json()).toEqual({
      before: PARAMS.before,
      after: PARAMS.after,
      label_before: "改動前",
      label_after: "",
      expires_ts: 1792737000,
    });
  });

  it("sends null for a link that never expires", async () => {
    const fetchMock = stubFetch(ok({ url: MINTED, share_link: WIRE_LINK }));
    await httpApi.createDiffShareLink(PARAMS, null);
    expect((await askedRequest(fetchMock).json()).expires_ts).toBeNull();
  });

  it("rejects with the server's message for an expiry it refuses", async () => {
    stubFetch(refused(422));
    await expect(httpApi.createDiffShareLink(PARAMS, 1)).rejects.toMatchObject({
      status: 422,
      serverMessage: "nope",
    });
  });
});

describe("createAttachmentShareLink", () => {
  it("posts the expiry to the attachment's share-link route", async () => {
    const fetchMock = stubFetch(
      ok({
        url: "/api/chat/attachment/att-0123456789ab?link=t",
        share_link: { ...WIRE_LINK, kind: "attachment", attachment_id: "att-0123456789ab", attachment_filename: "a.md", before: null, after: null, label_before: null, label_after: null },
      }),
    );

    const created = await httpApi.createAttachmentShareLink("att-0123456789ab", null);

    const req = askedRequest(fetchMock);
    expect(new URL(req.url).pathname).toBe("/api/chat/attachments/att-0123456789ab/share-link");
    expect(await req.json()).toEqual({ expires_ts: null });
    expect(created.link.attachmentFilename).toBe("a.md");
  });
});

describe("listShareLinks", () => {
  it.each([
    [undefined, {}],
    [{ attachmentId: "att-0123456789ab" }, { attachment_id: "att-0123456789ab" }],
    [{ before: PARAMS.before, after: PARAMS.after }, { before: PARAMS.before, after: PARAMS.after }],
  ])("filters by %j", async (filter, query) => {
    const fetchMock = stubFetch(ok({ share_links: [WIRE_LINK] }));

    const links = await httpApi.listShareLinks(filter);

    const url = new URL(askedRequest(fetchMock).url);
    expect(url.pathname).toBe("/api/share-links");
    expect(Object.fromEntries(url.searchParams)).toEqual(query);
    expect(links.map((l) => l.id)).toEqual(["sl-0123456789ab"]);
  });
});

describe("revokeShareLink", () => {
  it("deletes exactly that link", async () => {
    const fetchMock = stubFetch(ok(WIRE_LINK));
    await httpApi.revokeShareLink("sl-0123456789ab");
    const req = askedRequest(fetchMock);
    expect(req.method).toBe("DELETE");
    expect(new URL(req.url).pathname).toBe("/api/share-links/sl-0123456789ab");
  });
});

describe("getChatAttachmentPreviewLink", () => {
  it("asks the preview-link route and returns its url unchanged", async () => {
    const PREVIEW = "/api/chat/attachment/att-0123456789ab?sig=s&exp=1792650600";
    const fetchMock = stubFetch(ok({ url: PREVIEW }));
    expect(await httpApi.getChatAttachmentPreviewLink("att-0123456789ab")).toBe(PREVIEW);
    expect(new URL(askedRequest(fetchMock).url).pathname).toBe(
      "/api/chat/attachments/att-0123456789ab/preview-link",
    );
  });
});

describe("mockApi share links", () => {
  it("mints a /diff url that parses back with a link, lists it for its target, and revokes only it", async () => {
    const created = await mockApi.createDiffShareLink(
      { ...PARAMS, labelBefore: "改動前" },
      Date.now() / 1000 + 3600,
    );
    const other = await mockApi.createAttachmentShareLink("att-0123456789ab", null);

    const parsed = parseDiffParams(created.url.slice(created.url.indexOf("?")));
    expect(created.url.startsWith(DIFF_PATH)).toBe(true);
    expect(parsed).toMatchObject({ before: PARAMS.before, after: PARAMS.after, labelBefore: "改動前" });
    expect(parsed?.link).toBeTruthy();

    expect(
      (await mockApi.listShareLinks({ before: PARAMS.before, after: PARAMS.after })).map((l) => l.id),
    ).toEqual([created.link.id]);
    await mockApi.revokeShareLink(created.link.id);
    expect((await mockApi.listShareLinks()).map((l) => l.id)).toEqual([other.link.id]);
  });

  it("refuses an expiry more than 365 days ahead", async () => {
    await expect(
      mockApi.createDiffShareLink(PARAMS, Date.now() / 1000 + 366 * 86_400),
    ).rejects.toMatchObject({ status: 422 });
  });
});
