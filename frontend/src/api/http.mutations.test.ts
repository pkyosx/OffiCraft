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
import { codeForStatus } from "./errorCodes";

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

describe("httpApi · owner avatar mutations", () => {
  it("PUT sends the File bytes raw with source metadata in the query", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        member_id: "m-1",
        avatar_url: "/api/chat/attachment/ava-1",
        mime: "image/png",
        filename: "臉.png",
      })
    );
    const bytes = new Uint8Array([0x89, 0x50, 0x4e, 0x47]);
    const file = new File([bytes], "臉.png", { type: "image/png" });

    await expect(httpApi.updateMemberAvatar("m-1", file)).resolves.toBe(
      "/api/chat/attachment/ava-1"
    );

    const calls = fetchMock.mock.calls as unknown as [Request][];
    const req = calls[calls.length - 1][0];
    const url = new URL(req.url);
    expect(req.method).toBe("PUT");
    expect(url.pathname).toBe("/api/members/m-1/avatar");
    expect(url.searchParams.get("filename")).toBe("臉.png");
    expect(url.searchParams.get("mime")).toBe("image/png");
    expect(req.headers.get("Content-Type")).toBe("application/octet-stream");
    expect(new Uint8Array(await req.clone().arrayBuffer())).toEqual(bytes);
  });

  it("DELETE uses the same stable member id and sends no body", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        member_id: "m-1",
        avatar_url: null,
        mime: "",
        filename: "",
      })
    );
    await httpApi.removeMemberAvatar("m-1");
    const { url, method, body } = await lastRequest();
    expect(url).toBe("/api/members/m-1/avatar");
    expect(method).toBe("DELETE");
    expect(body).toBeUndefined();
  });
});

describe("httpApi · perf-light query contracts (T-2b9d/cf91/ec2c)", () => {
  it("listChat pulls the peer's window and sends NO peek parameter (T-48), NOT the whole history", async () => {
    fetchMock.mockImplementation(async () => jsonResponse({ messages: [] }));
    await httpApi.listChat("m-1");
    const { url, method } = await lastRequest();
    const q = new URLSearchParams(url.split("?")[1] ?? "");
    expect(method).toBe("GET");
    expect(url.split("?")[0]).toBe("/api/chat");
    // LOAD-BEARING: scoped to the peer, and the recent window is the SERVER's
    // default (no `limit` on the wire) — never the old whole-company
    // `limit=-1` pull. MUTANT: give this call `{ limit: -1 }` and these go red.
    expect(q.get("with")).toBe("m-1");
    expect(q.get("limit")).toBeNull();
    // T-48 removed ?peek= from the wire: GET /api/chat marks nothing read on
    // any path, so the opt-out has nothing to opt out of — which is also why
    // the `peekChat` twin of this method is gone. Asserting the parameter's
    // ABSENCE rather than deleting the check keeps a re-added parameter —
    // which would now be silently ignored by the server — from going unnoticed.
    expect(q.get("peek")).toBeNull();
    // caller_only went the same way with T-48 (owner ruling rc-09f6d801b2b8),
    // and this route now REFUSES a parameter it does not declare with a 400
    // naming it — so a re-added one would break the cockpit rather than be
    // quietly ignored. Asserting its absence here is what catches that before
    // a user does.
    expect(q.get("caller_only")).toBeNull();
  });

  it("listChat reads `messages` out of the T-48 envelope and ignores next_cursor", async () => {
    // The wire answers an OBJECT since T-48. This pins that the adapter reads
    // the envelope rather than the old bare array — and that a next_cursor it
    // does not use cannot leak into the rows. MUTANT: `return wire.messages`
    // →`return wire` and the shape assertion below goes red.
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        messages: [
          {
            id: "c-9",
            from: "m-1",
            from_name: "",
            to: "owner",
            to_name: "",
            body: "hi",
            ts: 1,
            ts_display: "",
            meta: {},
            reply_card_id: "",
            reply_card_status: "",
            reply_to: "",
            body_omitted_chars: 0,
          },
        ],
        next_cursor: "b3wAMQBjLTk",
      }),
    );
    const got = await httpApi.listChat("m-1");
    expect(got.map((m) => m.id)).toEqual(["c-9"]);
  });

  it("listChat(-1) still asks for the WHOLE history (the gallery path)", async () => {
    fetchMock.mockImplementation(async () => jsonResponse({ messages: [] }));
    await httpApi.listChat("m-1", -1);
    const q = new URLSearchParams(
      (await lastRequest()).url.split("?")[1] ?? "",
    );
    expect(q.get("with")).toBe("m-1");
    expect(q.get("limit")).toBe("-1");
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

  it("listTaskTypes asks for the manuals plainly — the light shape is the DEFAULT now", async () => {
    fetchMock.mockImplementation(async () => jsonResponse([]));
    await httpApi.listTaskTypes();
    // T-1170 retired `?view=list`. The directory IS the answer, so sending a
    // flag would be asking for a shape the route no longer offers — and the
    // flag was itself the defect: an opt-in escape hatch leaves the expensive
    // shape pointed at every caller that does not know to ask.
    expect((await lastRequest()).url).toBe("/api/task-manuals");
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

// ── T-e271 描述更正的 wire 形狀 ─────────────────────────────────────────────
describe("httpApi · updateTaskDescription wire shape", () => {
  const WIRE_TASK = {
    id: "t-1",
    task_no: "T-0001",
    status: "in_progress",
    priority: "high",
    executor_kind: "member",
    closed_ts: null,
    deps: [],
    steps: [],
    progress_done: 0,
    progress_total: 0,
    description: "corrected",
  };

  it("POSTs the task's description route with the text in the body", async () => {
    fetchMock.mockImplementation(async () => jsonResponse(WIRE_TASK));
    await httpApi.updateTaskDescription("t-1", "corrected");
    const { url, method, body } = await lastRequest();
    expect(url).toBe("/api/tasks/t-1/description");
    expect(method).toBe("POST");
    expect(JSON.parse(String(body))).toEqual({ description: "corrected" });
  });

  it("sends an EXPLICIT empty string when clearing — never an absent field", async () => {
    // The wire reads an ABSENT `description` as "change nothing" and an
    // explicit "" as "clear it". If this seam ever dropped the empty value
    // (the shape patchMember deliberately uses for its optional fields), a
    // clear would answer 200 and leave the old text standing — a write the
    // owner is told succeeded and did not happen.
    fetchMock.mockImplementation(async () =>
      jsonResponse({ ...WIRE_TASK, description: "" })
    );
    await httpApi.updateTaskDescription("t-1", "");
    const { body } = await lastRequest();
    expect(JSON.parse(String(body))).toEqual({ description: "" });
    expect(Object.keys(JSON.parse(String(body)))).toContain("description");
  });
});

// ── T-2ebe 標題更正的 wire 形狀 ─────────────────────────────────────────────
describe("httpApi · updateTaskTitle wire shape", () => {
  const WIRE_TASK = {
    id: "t-1",
    task_no: "T-0001",
    title: "corrected",
    status: "in_progress",
    priority: "high",
    executor_kind: "member",
    closed_ts: null,
    deps: [],
    steps: [],
    progress_done: 0,
    progress_total: 0,
    description: "",
  };

  it("POSTs the task's title route with the text in the body", async () => {
    fetchMock.mockImplementation(async () => jsonResponse(WIRE_TASK));
    // T-91: the write answers a receipt and this seam returns nothing — what
    // this test pins is the REQUEST it sends, which is all it ever really
    // measured (the old `task.title` assertion just read back the fixture the
    // fetch mock had been handed).
    await httpApi.updateTaskTitle("t-1", "corrected");
    const { url, method, body } = await lastRequest();
    expect(url).toBe("/api/tasks/t-1/title");
    expect(method).toBe("POST");
    expect(JSON.parse(String(body))).toEqual({ title: "corrected" });
  });

  it("rejects a blank title with the server's 400 rather than refusing locally", async () => {
    // The seam does NOT pre-empt the refusal by trimming to a no-op: the
    // cockpit's job is to surface which door said no, and a locally invented
    // success would be a lie. The 400 arrives as an ApiError carrying the
    // unified envelope, which is what the card's editor branches on.
    fetchMock.mockImplementation(async () =>
      jsonResponse(
        { error: { code: codeForStatus(400), message: "title must not be blank" } },
        400
      )
    );
    const err = await httpApi.updateTaskTitle("t-1", "   ").catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(400);
    expect((err as ApiError).serverMessage).toBe("title must not be blank");
    // And the blank really went out — a seam that swallowed it would never
    // learn the server's verdict at all.
    expect(JSON.parse(String((await lastRequest()).body))).toEqual({
      title: "   ",
    });
  });
});

// 🔴 `custom_months` (T-49e7 round 2) is the ONE scheduled-message field whose
// ABSENCE means something on the wire: omitted = every month (that is what a
// client written before round 2 always meant), `[]` = a schedule that never
// fires, which the server answers with 422. So the http seam has to keep
// `undefined` and `[]` apart all the way to the socket — a `?? []` anywhere on
// this path would turn every omitted-months create into a refusal, and a
// `?? allMonths()` would turn a refusable request into a silently different one.
//
// The two body types in http.ts are hand-written inline unions with no import
// from the generated schema, so nothing but this file notices a field that was
// added to one and not the other.
describe("httpApi · scheduled-message custom_months keeps undefined ≠ []", () => {
  const BASE = {
    body: "巡檢",
    cadence: "custom" as const,
    timezone: "Asia/Taipei",
    customDays: [1],
    customHours: [9],
    customMinutes: [0],
  };

  it("POSTs custom_months when the caller states it — including the empty set", async () => {
    await httpApi.createScheduledMessage("mira", {
      ...BASE,
      customMonths: [3, 6, 9, 12],
    });
    expect(JSON.parse((await lastRequest()).body!).custom_months).toEqual([
      3, 6, 9, 12,
    ]);

    await httpApi.createScheduledMessage("mira", { ...BASE, customMonths: [] });
    const emptied = JSON.parse((await lastRequest()).body!);
    // Present-and-empty, not absent: the server owes this request a 422, and it
    // can only give one if the field arrives.
    expect("custom_months" in emptied).toBe(true);
    expect(emptied.custom_months).toEqual([]);
  });

  it("leaves custom_months OFF the POST body when the caller omits it", async () => {
    await httpApi.createScheduledMessage("mira", BASE);
    const sent = JSON.parse((await lastRequest()).body!);
    expect("custom_months" in sent).toBe(false);
    // The control: the sets that were supplied really did ride, so "absent"
    // above is about this one field and not about a body that carried nothing.
    expect(sent.custom_days).toEqual([1]);
  });

  it("PATCHes custom_months only when the patch carries it", async () => {
    await httpApi.updateScheduledMessage("mira", "sch-1", {
      customMonths: [2],
    });
    expect(JSON.parse((await lastRequest()).body!).custom_months).toEqual([2]);

    await httpApi.updateScheduledMessage("mira", "sch-1", { label: "改名" });
    const renamed = JSON.parse((await lastRequest()).body!);
    // Absent means "leave the stored months alone" — sending [] here would ask
    // the server to refuse a rename.
    expect("custom_months" in renamed).toBe(false);
    expect(renamed.label).toBe("改名");
  });
});

// T-4e95 r16 — the WRITE half of the reply link, which nothing was standing on.
//
// r15 found the READ half (`mappers.ts`) had no witness and pinned it. The
// write half is the mirror image and had none either: change this one line to
// send `""` and 「回覆這則」 is gone in the real app — the banner still shows,
// the send still succeeds, and the server stores an ordinary message. All 2258
// tests stayed green on that change.
//
// Nothing else in the jsdom suite covers it, and that is a measured claim, not
// an impression:
// ChatArea's tests mock the whole `useChat` seam; `mock.reply-to.test.ts` drives
// the MOCK adapter, which never reaches this file; conformance drives real HTTP
// from Python, which proves the SERVER is right and says nothing about what the
// browser sends.
describe("httpApi · postChat carries the reply link (T-4e95)", () => {
  const WIRE_MSG = {
    id: "c-2",
    from: "owner",
    to: "m1",
    body: "回你這句",
    ts: 2,
    meta: {},
    reply_card_id: "",
    reply_card_status: "",
    reply_to: "c-1",
    body_omitted_chars: 0,
  };

  it("sends reply_to when the composer is aimed at a message", async () => {
    fetchMock.mockImplementation(async () => jsonResponse(WIRE_MSG));
    await httpApi.postChat({ to: "m1", body: "回你這句", replyTo: "c-1" });
    const { url, method, body } = await lastRequest();
    expect(url).toBe("/api/chat");
    expect(method).toBe("POST");
    expect(JSON.parse(String(body)).reply_to).toBe("c-1");
  });

  it("sends the empty string — the wire's 'replies to nothing' — otherwise", async () => {
    // Both directions matter: always-"" and always-a-target are each a way to
    // break this, and one assertion alone cannot tell them apart.
    fetchMock.mockImplementation(async () =>
      jsonResponse({ ...WIRE_MSG, reply_to: "" }),
    );
    await httpApi.postChat({ to: "m1", body: "普通訊息" });
    expect(JSON.parse(String((await lastRequest()).body)).reply_to).toBe("");
  });
});

describe("httpApi · reply-card answer body (ReplyCardAnswerPostDTO)", () => {
  const WIRE_CARD = {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "這批要走哪幾條線？",
    body: "",
    options: [
      { text: "走海運", ai_pick: false },
      { text: "走空運", ai_pick: true },
      { text: "先擱著", ai_pick: false },
    ],
    select_mode: "multi",
    status: "answered",
    attachments: [],
    created_ts: 1,
    answered_ts: 2,
    expired_ts: null,
    chat_message_id: "cm-1",
    answer: { option_idxs: [0, 2], text: "", attachments: [] },
    task: null,
  };

  it("sends the circled indices deduped and ascending, whichever order they arrive in", async () => {
    // Two owners who ticked the same boxes in different orders must produce a
    // byte-identical body. The server dedupes and sorts too, but the two
    // requests would still differ on the wire, and one decision must not be two
    // payloads.
    fetchMock.mockImplementation(async () => jsonResponse(WIRE_CARD));
    await httpApi.answerReplyCard("rc-1", { optionIdxs: [2, 0, 2] });
    const first = await lastRequest();
    expect(first.url).toBe("/api/reply-cards/rc-1/answer");
    expect(first.method).toBe("POST");
    expect(JSON.parse(String(first.body))).toEqual({
      option_idxs: [0, 2],
      text: "",
    });

    await httpApi.answerReplyCard("rc-1", { optionIdxs: [0, 2] });
    expect(JSON.parse(String((await lastRequest()).body))).toEqual({
      option_idxs: [0, 2],
      text: "",
    });
  });

  it("sends null — the wire's 'circled nothing' — for a text-only answer", async () => {
    // Both directions matter, and the wrong one here is not silent-but-equal:
    // `option_idxs: []` is a 400 server-side, deliberately, so a seam that
    // flattened an absent list to `[]` would turn every typed answer into an
    // error.
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        ...WIRE_CARD,
        answer: { option_idxs: null, text: "收件人是誰？", attachments: [] },
      }),
    );
    await httpApi.answerReplyCard("rc-1", { text: "收件人是誰？" });
    expect(JSON.parse(String((await lastRequest()).body))).toEqual({
      option_idxs: null,
      text: "收件人是誰？",
    });
  });

  it("revises through PUT with the exact same body shape", async () => {
    fetchMock.mockImplementation(async () => jsonResponse(WIRE_CARD));
    await httpApi.reanswerReplyCard("rc-1", { optionIdxs: [2, 0] });
    const { url, method, body } = await lastRequest();
    expect(url).toBe("/api/reply-cards/rc-1/answer");
    expect(method).toBe("PUT");
    expect(JSON.parse(String(body))).toEqual({
      option_idxs: [0, 2],
      text: "",
    });
  });
});
