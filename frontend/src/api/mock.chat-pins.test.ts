import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockOutsourceWorker } from "./mock";
import { ApiError } from "./errors";
import type { OutsourceWorkerView } from "./adapter";

const worker = (id: string) =>
  ({
    id,
    codename: "O-1",
    model: "opus",
    effort: "medium",
    taskId: `t-${id}`,
    taskTitle: "",
    taskStatus: "in_progress",
    createdTs: 0,
  }) as OutsourceWorkerView;

async function refusal(ids: string[]) {
  const e = await mockApi.putChatPins(ids).catch((err: unknown) => err);
  expect(e).toBeInstanceOf(ApiError);
  return { status: (e as ApiError).status, message: (e as ApiError).serverMessage };
}

describe("mock chat pins", () => {
  beforeEach(() => {
    __resetMock();
    __injectMockOutsourceWorker(worker("ow-a"));
  });

  it("starts with Mira pinned", async () => {
    expect(await mockApi.getChatPins()).toEqual(["mira"]);
  });

  it("a PUT replaces the list in the given order, answers it, and announces chat_pin", async () => {
    const topics: string[] = [];
    const off = mockApi.subscribeEvents((t) => topics.push(t));
    expect(await mockApi.putChatPins(["ow-a", "mira"])).toEqual(["ow-a", "mira"]);
    off();
    expect(await mockApi.getChatPins()).toEqual(["ow-a", "mira"]);
    expect(topics).toEqual(["chat_pin"]);
  });

  it("a repeated id is a 422 and nothing is written", async () => {
    expect(await refusal(["mira", "mira"])).toEqual({
      status: 422,
      message: "member_ids lists 'mira' more than once",
    });
    expect(await mockApi.getChatPins()).toEqual(["mira"]);
  });

  it("an unknown id or a non staff / outsource member is a 422 and nothing is written", async () => {
    expect(await refusal(["ow-a", "nobody"])).toEqual({
      status: 422,
      message: "member_ids: 'nobody' is not an active staff or outsource member",
    });
    expect(await refusal(["warden-mbp5"])).toEqual({
      status: 422,
      message: "member_ids: 'warden-mbp5' is not an active staff or outsource member",
    });
    expect(await mockApi.getChatPins()).toEqual(["mira"]);
  });

  it("a dismissed member drops out of the list the next read returns", async () => {
    await mockApi.putChatPins(["mira", "ow-a"]);
    await mockApi.dismissMember("mira");
    expect(await mockApi.getChatPins()).toEqual(["ow-a"]);
  });
});
