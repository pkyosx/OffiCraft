// dtoParity.test.ts — THE FAKE MUST NOT BE MORE GENEROUS THAN THE SERVER.
//
// T-8115 shipped two regressions green because the hook tests' hand-rolled fake
// answered `GET /{id}` with the LIST row: against that fake a one-item refetch
// looked lossless, while AT THE TIME the real server returned a literal 0 for
// `unread_count` on `GET /api/members/{id}` and the wire carried no `dep_tasks`
// on `GET /api/tasks/{id}` at all. Neither tsc nor 1670 unit tests could see it,
// because nothing anywhere stated what those endpoints really return.
//
// PAST TENSE ON PURPOSE for the first half: the member endpoint was FIXED at the
// source (both handlers now share `unreadCountsForRequest`). The `dep_tasks` half
// is still true today. Do not read this paragraph as a description of the current
// server — it is the history that explains why this file exists.
//
// So `api/dtoParity.ts` states it once, and this file pins that statement against
// the two things a frontend test CAN hold it to:
//  1. `api/mock.ts` — the adapter the repo already curates as the client-side twin
//     of the wire (its `getTask` has always dropped `depTasks`, with a comment
//     saying why). Stop computing unread for a single member, or let `getTask`
//     keep the dep join, and these tests go red. NOTE the member case flipped
//     direction once the server was fixed: it now asserts the two AGREE, so the
//     drift it catches is "someone made the single-item path lossy again".
//  2. the GENERATED wire types (`generated/schema.ts`, kept honest by CI's spec
//     drift gate) — a compile-time pin that `TaskDTO` has no `dep_tasks` while
//     `TaskListItemDTO` does. Add the field to the spec and the pin fails to
//     compile, which is precisely when the gap table must change.
//
// 🔴 KNOWN LIMIT, named rather than papered over: nothing here notices the SERVER
// changing. If `GET /api/members/{id}` starts computing unread_count, this file
// keeps passing while the gap table goes stale. Only a conformance-level
// assertion (single item vs list, against a real ocserverd) can catch that
// direction, and that is the follow-up this file cannot substitute for.
import { beforeEach, describe, expect, it } from "vitest";
import type { components } from "./generated/schema";
import {
  __injectMockChat,
  __injectMockOutsourceWorker,
  __injectMockTask,
  __resetMock,
  mockApi,
} from "./mock";
import {
  PER_ITEM_DTO_GAPS,
  perItemRefetchIsFaithful,
  projectSingleItem,
} from "./dtoParity";

// ── compile-time pin (see 2. above) ──────────────────────────────────────────
type HasDepTasks<T> = "dep_tasks" extends keyof T ? true : false;
// The LIGHT list row carries the server-side dep join…
const _listItemResolvesDeps: HasDepTasks<
  components["schemas"]["TaskListItemDTO"]
> = true;
// …and the FULL task does not. This is the wire fact that keeps useTasks on the
// list path; if this line stops compiling, `PER_ITEM_DTO_GAPS.task` is stale.
const _fullTaskDoesNot: HasDepTasks<components["schemas"]["TaskDTO"]> = false;
void _listItemResolvesDeps;
void _fullTaskDoesNot;

const OWNER = "owner";

describe("per-item DTO gaps are what the adapter really does (T-8115 follow-up)", () => {
  beforeEach(() => {
    __resetMock();
  });

  it("GET /api/members/{id} serves the SAME unread badge as GET /api/members", async () => {
    const peer = (await mockApi.listMembers()).find((m) => m.id !== OWNER);
    expect(peer).toBeDefined();

    // Real unread, made the way the product makes it: an inbound message above
    // the owner's read watermark.
    __injectMockChat({
      id: "c-parity-1",
      from: peer!.id,
      to: OWNER,
      body: "an unread line",
      ts: Date.now() / 1000,
      attachments: [],
      replyCardId: null,
    });

    const listRow = (await mockApi.listMembers()).find(
      (m) => m.id === peer!.id
    );
    const single = await mockApi.getMember(peer!.id);

    // Both endpoints run the same computation (Go: unreadCountsForRequest, shared
    // by the list and single-member handlers; mock: unreadCountOf). The number has
    // to be REAL — a shared constant would satisfy equality — so the list value is
    // asserted non-zero first, and only then compared.
    expect(listRow!.unreadCount).toBeGreaterThan(0);
    expect(single.unreadCount).toBe(listRow!.unreadCount);
    // ⇒ the per-item refetch in useMembers is faithful.
    //
    // 🔴 WHAT THIS ASSERTION DOES *NOT* CATCH — measured 2026-08-01, do not
    // re-credit it: if the GO handler goes back to serving a literal 0, this
    // file stays GREEN (all 14 tests pass). It compares the MOCK against the
    // MOCK; nothing here can see the server. The guard that actually catches
    // that regression is `server/ocserverd/api_members_unread_parity_test.go`,
    // which reads the number out of a real response body. What THIS test
    // catches is the mock drifting away from the wire it is supposed to twin.
    expect(PER_ITEM_DTO_GAPS.member).toEqual([]);
    expect(perItemRefetchIsFaithful("member")).toBe(true);
  });

  it("serves an ow- row at BOTH doors — the list and the item can never disagree again", async () => {
    // T-26. The two doors used to answer opposite things about the SAME row:
    // the list carried contractors, the item door folded kind='outsource' into
    // 404. useMembers takes the ids from the list, so a chat delta naming one
    // contractor took the per-item fast path straight into a guaranteed 404 and
    // then re-pulled the whole roster — the "cheap" path costing double, on
    // every contractor chat line.
    const listed = (await mockApi.listMembers()).filter((m) =>
      m.id.startsWith("ow-")
    );
    // The fixture must carry one at all: with no contractor row, nothing below
    // is exercised and this test passes while proving nothing.
    expect(listed.length).toBeGreaterThan(0);

    const worker = listed[0];
    expect(worker.kind).toBe("outsource");

    const single = await mockApi.getMember(worker.id);
    expect(single.id).toBe(worker.id);
    expect(single.kind).toBe("outsource");
    // Same row through both doors — the property the 404 broke.
    expect(single.name).toBe(worker.name);
  });

  it("GET /api/tasks/{id} carries no dep join; GET /api/tasks does", async () => {
    __injectMockTask({
      ...blankTask("t-parity-dep"),
      title: "the blocker",
      status: "done",
    });
    __injectMockTask({ ...blankTask("t-parity"), deps: ["t-parity-dep"] });

    const listRow = (await mockApi.listTasks()).find(
      (t) => t.id === "t-parity"
    );
    expect(listRow).toBeDefined();
    // The light list resolves every dep server-side (T-a3e4) — that join is what
    // the card renders 「等 <task id> <標題>」 from.
    expect(listRow!.depTasks).toBeDefined();
    expect(listRow!.depTasks?.[0]?.title).toBe("the blocker");

    const single = await mockApi.getTask("t-parity");
    // TaskDTO has no dep_tasks field, so a per-task refetch loses the whole join
    // — and absence is NOT `[]`: the card then shows an unresolved short id
    // instead of a named blocker (or 查無此任務).
    expect(single.depTasks).toBeUndefined();
    expect(PER_ITEM_DTO_GAPS.task).toContain("depTasks");
    expect(perItemRefetchIsFaithful("task")).toBe(false);
  });

  it("GET /api/members/{id} faithfully serves an outsource member", async () => {
    __injectMockTask({ ...blankTask("t-parity-bound"), title: "bound" });
    __injectMockOutsourceWorker({
      id: "ow-parity",
      codename: "PARITY",
      status: "working",
      model: "claude-opus-5",
      effort: "medium",
      taskId: "t-parity-bound",
      taskTitle: "bound",
      taskTypeKey: "",
      taskTypeName: "",
      taskStatus: "in_progress",
      taskCreatedTs: 100,
      createdTs: 100,
      unreadCount: 0,
      machine: "",
      presence: "online",
    } as Parameters<typeof __injectMockOutsourceWorker>[0]);
    __injectMockChat({
      id: "c-parity-2",
      from: "ow-parity",
      to: OWNER,
      body: "worker line",
      ts: Date.now() / 1000,
      attachments: [],
      replyCardId: null,
    });

    const row = (await mockApi.listOutsourceWorkers()).find(
      (w) => w.id === "ow-parity"
    );
    expect(row).toBeDefined();
    const single = await mockApi.getOutsourceWorker("ow-parity");

    // Same server-side projection as the list (projectWorker with the same real
    // unread), so every field the rail renders survives a one-row read. This is
    // what keeps the REMAINING per-item path honest: break it and the rail loses
    // its badge exactly the way the roster would have.
    expect(row!.unreadCount).toBeGreaterThan(0);
    expect(single.unreadCount).toBe(row!.unreadCount);
    expect(single.codename).toBe(row!.codename);
    expect(single.taskId).toBe(row!.taskId);
    expect(PER_ITEM_DTO_GAPS.outsourceWorker).toEqual([]);
    expect(perItemRefetchIsFaithful("outsourceWorker")).toBe(true);
  });

  it("projectSingleItem drops exactly the gapped fields and nothing else", () => {
    // member has no gap any more, so its projection is the IDENTITY — a fake
    // built from it is exactly as generous as the wire, which is the point.
    const m = { id: "m-1", name: "n", unreadCount: 7, presence: "online" };
    expect(projectSingleItem("member", m)).toEqual(m);
    const t = { id: "t-1", status: "in_progress", depTasks: [{ id: "t-0" }] };
    expect(projectSingleItem("task", t).depTasks).toBeUndefined();
    expect(projectSingleItem("task", t).status).toBe("in_progress");
    // The faithful endpoint's projection must be the IDENTITY, or a test using it
    // would quietly under-report what the rail actually receives.
    const w = { id: "ow-1", unreadCount: 4 };
    expect(projectSingleItem("outsourceWorker", w)).toEqual(w);
  });
});

/** A task row with every field the views read, so a fixture omission cannot look
 * like a wire gap. */
function blankTask(id: string) {
  return {
    id,
    taskNo: "",
    title: id,
    typeKey: "",
    description: "",
    status: "in_progress",
    lock: "",
    priority: "medium",
    executorKind: "staff",
    executorId: "",
    creatorId: OWNER,
    reassignedFrom: "",
    reassignedFromKind: "",
    dedupeKey: "",
    deps: [] as string[],
    waitingReason: "",
    duplicateOf: "",
    createdTs: 100,
    updatedTs: 100,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    artifacts: [],
    artifactCount: 0,
  } as Parameters<typeof __injectMockTask>[0];
}
