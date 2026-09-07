// The mock's UNRESOLVABLE-dep number (mock.ts `deriveMockTaskNo`), pinned.
//
// WHY THIS FILE EXISTS (T-5291 round 2). `deriveMockTaskNo` was the THIRD copy
// of the old four-hex projection (`T-${taskId.slice(2, 6)}`). This ticket
// collapsed it to "return the id", but review measured that nothing was
// holding it there: reverting the body to the old slice left ALL 2356 frontend
// tests green, while replacing it with a `throw` reddened 5 of them. That pair
// of measurements is the whole diagnosis — the branch IS executed, its output
// was simply never looked at by any assertion. A guard whose value nobody
// reads is not a guard.
//
// So this file asserts the OUTPUT, on the one path that reaches it: a task
// whose `deps` names an id with no task row (the card's 查無此任務 row). The
// resolved half is covered elsewhere and is not the risk — a resolved dep
// copies the server's `task_no` verbatim, so it cannot drift on its own.
//
// The pin is "the number IS the id, byte for byte", which is the ruling
// (server/ocserverd/domain.go TaskNo, spec/openapi.json TaskDepRefDTO). The
// extra shape assertions below are not decoration: `slice(2, 6)` and a "T-"
// re-casing are the two specific regressions this ticket removed, and naming
// them makes a reintroduction fail with the reason rather than a diff.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockTask } from "./mock";
import type { TaskView } from "./adapter";

beforeEach(() => {
  __resetMock();
});

// A dep id long enough that a four-hex truncation is unmistakable, and whose
// first four hex chars ("72dd") are a real prefix of it — so a `startsWith`
// style assertion could NOT tell the two apart. Only equality can.
const GHOST_DEP = "t-72dd79b666d0";
const HOLDER = "t-5291aabbccdd";

function seedHolder(): void {
  __injectMockTask({
    id: HOLDER,
    taskNo: HOLDER,
    title: "擋著的那張",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "high",
    executorKind: "staff",
    executorId: "mira",
    creatorId: "owner",
    dedupeKey: "",
    deps: [GHOST_DEP],
    waitingReason: "",
    duplicateOf: "",
    createdTs: 1000,
    updatedTs: 2000,
    closedTs: null,
    progressDone: 0,
    progressTotal: 1,
    steps: [],
  } as TaskView);
}

async function ghostDepRef() {
  seedHolder();
  const rows = await mockApi.listTasks();
  const holder = rows.find((t) => t.id === HOLDER);
  // Non-vacuity: if the fixture stops producing a dep_tasks entry, every
  // assertion below would vanish rather than fail. This is the trap the whole
  // header is about, so it is checked first and separately.
  expect(holder, "the seeded holder task is missing from listTasks").toBeTruthy();
  const refs = holder!.depTasks;
  expect(refs, "listTasks returned no dep_tasks join at all").toBeTruthy();
  expect(refs!.map((d) => d.id)).toEqual([GHOST_DEP]);
  return refs![0];
}

describe("mock dep_tasks: the number of an unresolvable dep", () => {
  it("IS the dep id itself, byte for byte", async () => {
    const ref = await ghostDepRef();
    expect(ref.taskNo).toBe(GHOST_DEP);
  });

  it("is not the old four-hex short code, and is not re-cased", async () => {
    const ref = await ghostDepRef();
    // The exact expression that used to live in mock.ts. Named so that a
    // reintroduction reddens with its own source rather than a bare diff.
    const oldProjection = `T-${GHOST_DEP.slice(2, 6)}`;
    expect(
      ref.taskNo,
      `the mock is back on the retired projection (${oldProjection}) — the ` +
        `number a user copies off the card must be the id they can paste back`
    ).not.toBe(oldProjection);
    expect(
      ref.taskNo.startsWith("T-"),
      "task lookup is byte-exact (id TEXT PRIMARY KEY, no COLLATE NOCASE), " +
        "so an upper-cased prefix makes the displayed number un-pasteable"
    ).toBe(false);
    expect(
      ref.taskNo.length,
      "the number was truncated — a shortened number cannot be pasted back"
    ).toBe(GHOST_DEP.length);
  });

  it("still reports the dep as unresolvable (title/status stay empty)", async () => {
    // The number being present must not be read as "the dep resolved". This is
    // the honest 查無此任務 row, and it is the reason the mock fills a number
    // here at all rather than leaving it blank.
    const ref = await ghostDepRef();
    expect(ref.title).toBe("");
    expect(ref.status).toBe("");
  });
});
