import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockOutsourceWorker } from "./mock";
import { ApiError } from "./errors";
import type { ChatGroupWrite, OutsourceWorkerView } from "./adapter";

const worker = (id: string, taskTypeKey = "") =>
  ({
    id,
    codename: id.toUpperCase(),
    model: "opus",
    effort: "medium",
    taskId: `t-${id}`,
    taskTypeKey,
    taskTitle: "",
    taskStatus: "in_progress",
    createdTs: 0,
  }) as OutsourceWorkerView;
const w = (over: Partial<ChatGroupWrite>): ChatGroupWrite => ({
  id: null,
  tab: "staff",
  name: "核心",
  memberIds: [],
  taskTypeKeys: [],
  ...over,
});

async function refusal(groups: ChatGroupWrite[]) {
  const e = await mockApi.putChatGroups(groups).catch((err: unknown) => err);
  expect(e).toBeInstanceOf(ApiError);
  return [(e as ApiError).status, (e as ApiError).serverMessage];
}

describe("mock chat groups", () => {
  beforeEach(() => {
    __resetMock();
    __injectMockOutsourceWorker(worker("ow-a"));
  });

  it("starts with no groups; a PUT stores both tabs staff first, mints cg- ids for new groups, trims names and announces chat_group", async () => {
    expect(await mockApi.getChatGroups()).toEqual([]);
    const topics: string[] = [];
    const off = mockApi.subscribeEvents((t) => topics.push(t));
    const saved = await mockApi.putChatGroups([
      w({ tab: "outsource", name: "外包", memberIds: ["ow-a"], taskTypeKeys: ["builtin-role-design"] }),
      w({ name: " 核心 ", memberIds: ["mira"] }),
    ]);
    off();
    expect(saved).toEqual([
      { id: "cg-000000000002", tab: "staff", name: "核心", memberIds: ["mira"], taskTypeKeys: [] },
      {
        id: "cg-000000000001",
        tab: "outsource",
        name: "外包",
        memberIds: ["ow-a"],
        taskTypeKeys: ["builtin-role-design"],
      },
    ]);
    expect(await mockApi.getChatGroups()).toEqual(saved);
    expect(topics).toEqual(["chat_group"]);
  });

  it("refuses with the server's 422 messages and writes nothing", async () => {
    expect(await refusal([w({ name: "  " })])).toEqual([422, "groups[0].name is blank"]);
    expect(await refusal([w({ name: "字".repeat(41) })])).toEqual([422, "groups[0].name is longer than 40 characters"]);
    expect(await refusal([w({}), w({})])).toEqual([422, "the staff tab has two groups named '核心'"]);
    expect(await refusal([w({ memberIds: ["mira"] }), w({ name: "b", memberIds: ["mira"] })])).toEqual([
      422,
      "member 'mira' is listed more than once",
    ]);
    expect(await refusal([w({ taskTypeKeys: ["builtin-role-design"] })])).toEqual([
      422,
      "groups[0] is a staff group; task_type_keys must be empty",
    ]);
    expect(await refusal([w({ id: "cg-x" })])).toEqual([422, "group 'cg-x' does not exist"]);
    expect(await refusal([w({ memberIds: ["ow-a"] })])).toEqual([422, "member 'ow-a' is not an active member of the staff tab"]);
    expect(await refusal([w({ memberIds: ["warden-mbp5"] })])).toEqual([
      422,
      "member 'warden-mbp5' is not an active member of the staff tab",
    ]);
    expect(await refusal([w({ tab: "outsource", taskTypeKeys: ["nope"] })])).toEqual([422, "task type 'nope' names no task manual"]);
    expect(await mockApi.getChatGroups()).toEqual([]);
  });

  it("refuses moving an existing group to the other tab", async () => {
    const [saved] = await mockApi.putChatGroups([w({})]);
    expect(await refusal([w({ id: saved.id, tab: "outsource" })])).toEqual([
      422,
      `group '${saved.id}' is on the staff tab and cannot move to the outsource tab`,
    ]);
  });

  it("a read leaves out a dismissed member; a new worker whose type a rule names joins that group and announces chat_group", async () => {
    await mockApi.putChatGroups([
      w({ memberIds: ["mira"] }),
      w({ tab: "outsource", name: "設計", taskTypeKeys: ["builtin-role-design"] }),
    ]);
    await mockApi.dismissMember("mira");
    const topics: string[] = [];
    const off = mockApi.subscribeEvents((t) => topics.push(t));
    __injectMockOutsourceWorker(worker("ow-b", "builtin-role-design"));
    off();
    expect((await mockApi.getChatGroups()).map((g) => g.memberIds)).toEqual([[], ["ow-b"]]);
    expect(topics).toEqual(["member", "chat_group"]);
  });
});
