import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";
import { ApiError } from "./errors";

async function refusal(typeKey: string, replies: string[]) {
  const e = await mockApi.putTaskManualSuggestedReplies(typeKey, replies).catch((err: unknown) => err);
  expect(e).toBeInstanceOf(ApiError);
  return [(e as ApiError).status, (e as ApiError).serverMessage];
}

function topicsDuring(run: () => Promise<unknown>): Promise<string[]> {
  const seen: string[] = [];
  const unsubscribe = mockApi.subscribeEvents((topic) => seen.push(topic));
  return run().then(() => {
    unsubscribe();
    return seen;
  });
}

describe("mock task manual suggested replies", () => {
  beforeEach(() => __resetMock());

  it("starts empty; a PUT stores trimmed text per type, built-in or created, and an empty list removes it", async () => {
    const { typeKey } = await mockApi.createTaskManual("報價");
    expect(await mockApi.getTaskManualSuggestedReplies()).toEqual({});
    expect(await mockApi.putTaskManualSuggestedReplies("builtin-role-design", ["  照手冊  ", "好"])).toEqual(["照手冊", "好"]);
    expect(await mockApi.putTaskManualSuggestedReplies(typeKey, ["報價單已附"])).toEqual(["報價單已附"]);
    expect(await mockApi.getTaskManualSuggestedReplies()).toEqual({
      "builtin-role-design": ["照手冊", "好"],
      [typeKey]: ["報價單已附"],
    });
    expect(await mockApi.putTaskManualSuggestedReplies(typeKey, [])).toEqual([]);
    expect(await mockApi.getTaskManualSuggestedReplies()).toEqual({ "builtin-role-design": ["照手冊", "好"] });
  });

  it("refuses what the server refuses and writes nothing", async () => {
    expect(await refusal("builtin-role-design", ["ok", "  "])).toEqual([422, "replies[1] is blank"]);
    expect(await refusal("builtin-role-design", ["句".repeat(121)])).toEqual([
      422,
      "replies must be at most 120 characters per entry",
    ]);
    expect(await refusal("builtin-role-design", Array.from({ length: 21 }, () => "s"))).toEqual([
      422,
      "replies must be at most 20 entries",
    ]);
    expect(await refusal("tm-ghost", ["x"])).toEqual([404, "task manual 'tm-ghost' not found"]);
    expect(await mockApi.getTaskManualSuggestedReplies()).toEqual({});
  });

  it("deleting a manual drops its list and tells the owner's other windows only when it had one", async () => {
    const withList = await mockApi.createTaskManual("報價");
    const emptied = await mockApi.createTaskManual("出貨");
    await mockApi.putTaskManualSuggestedReplies(withList.typeKey, ["報價單已附"]);
    await mockApi.putTaskManualSuggestedReplies(emptied.typeKey, ["先放著"]);
    await mockApi.putTaskManualSuggestedReplies(emptied.typeKey, []);

    expect(await topicsDuring(() => mockApi.deleteTaskManual(withList.typeKey))).toEqual([
      "task_manual",
      "task_manual_suggested_reply",
    ]);
    expect(await topicsDuring(() => mockApi.deleteTaskManual(emptied.typeKey))).toEqual(["task_manual"]);
    expect(await mockApi.getTaskManualSuggestedReplies()).toEqual({});
  });
});
