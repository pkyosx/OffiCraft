import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockMember, __injectMockOutsourceWorker } from "./mock";
import { ApiError } from "./errors";
import type { OutsourceWorkerView } from "./adapter";

const worker = (id: string, status?: string) =>
  ({
    id,
    codename: id.toUpperCase(),
    model: "opus",
    effort: "medium",
    taskId: `t-${id}`,
    taskTypeKey: "",
    taskTitle: "",
    taskStatus: "in_progress",
    createdTs: 0,
    status,
  }) as OutsourceWorkerView;

async function refusal(memberId: string, replies: string[]) {
  const e = await mockApi.putChatSuggestedReplies(memberId, replies).catch((err: unknown) => err);
  expect(e).toBeInstanceOf(ApiError);
  return [(e as ApiError).status, (e as ApiError).serverMessage];
}

describe("mock chat suggested replies", () => {
  beforeEach(() => {
    __resetMock();
    __injectMockOutsourceWorker(worker("ow-a"));
    __injectMockOutsourceWorker(worker("ow-gone", "released"));
  });

  it("starts empty; a PUT stores trimmed text per member, for staff and outsource alike, and answers it", async () => {
    expect(await mockApi.getChatSuggestedReplies()).toEqual({});
    expect(await mockApi.putChatSuggestedReplies("mira", ["  繼續  ", "好"])).toEqual(["繼續", "好"]);
    expect(await mockApi.putChatSuggestedReplies("ow-a", ["先放著"])).toEqual(["先放著"]);
    expect(await mockApi.getChatSuggestedReplies()).toEqual({ mira: ["繼續", "好"], "ow-a": ["先放著"] });
  });

  it("an empty list removes the member from the answer", async () => {
    await mockApi.putChatSuggestedReplies("mira", ["繼續"]);
    expect(await mockApi.putChatSuggestedReplies("mira", [])).toEqual([]);
    expect(await mockApi.getChatSuggestedReplies()).toEqual({});
  });

  it("refuses what the server refuses and writes nothing", async () => {
    expect(await refusal("mira", ["ok", "  "])).toEqual([422, "replies[1] is blank"]);
    expect(await refusal("mira", ["句".repeat(121)])).toEqual([422, "replies must be at most 120 characters per entry"]);
    expect(await refusal("mira", Array.from({ length: 21 }, () => "s"))).toEqual([422, "replies must be at most 20 entries"]);
    expect(await refusal("ghost", ["x"])).toEqual([422, "member 'ghost' is not an active member"]);
    expect(await refusal("ow-gone", ["x"])).toEqual([422, "member 'ow-gone' is not an active member"]);
    expect(await mockApi.getChatSuggestedReplies()).toEqual({});
  });

  it("a warden cannot have a list", async () => {
    expect(await refusal("warden-mbp5", ["x"])).toEqual([422, "member 'warden-mbp5' is not an active member"]);
  });

  it("a member dismissed later is no longer listed and can no longer be written", async () => {
    __injectMockMember({ id: "rd", kind: "staff" });
    await mockApi.putChatSuggestedReplies("mira", ["繼續"]);
    await mockApi.putChatSuggestedReplies("rd", ["先放著"]);
    await mockApi.dismissMember("rd");
    expect(await mockApi.getChatSuggestedReplies()).toEqual({ mira: ["繼續"] });
    expect(await refusal("rd", ["x"])).toEqual([422, "member 'rd' is not an active member"]);
  });
});
