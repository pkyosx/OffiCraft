// T-a3e4 — what the tasks fetch actually puts ON THE WIRE.
//
// The ticket is a payload ticket, so the thing that must be pinned is the REQUEST
// URL, not "the adapter was called with an options object": a page can hold the
// right filter state and still ask the server for everything. These tests read
// the real `Request` openapi-fetch builds, so the serialisation (one repeated
// `statuses=` per state — form/explode, what the spec declares) is under test
// too, not assumed.
//
// Baseline being replaced (measured on origin/main e7120c5, same DB):
//   GET /api/tasks           → 408,482 B (703 rows)
//   GET /api/tasks?open=true →  17,295 B
// …and the page asked for the FIRST one on every task SSE delta, because a
// clause in TasksPage widened the fetch whenever any live task carried a dep.
//
// Also pinned here: `dep_tasks` reaches TaskView.depTasks as the server sent it,
// including the two DIFFERENT silences (an entry with an empty status = the dep's
// task is gone; no entry at all = this server does not resolve deps). Collapsing
// those two is how a healthy closed dep would get labelled 查無此任務.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const fetchMock = vi.fn(async () => jsonResponse([]));

function lastUrl(): URL {
  const calls = fetchMock.mock.calls as unknown as [Request][];
  return new URL(calls[calls.length - 1][0].url);
}

beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockImplementation(async () => jsonResponse([]));
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("httpApi.listTasks · the status set goes on the wire (T-a3e4)", () => {
  it("sends one repeated statuses= per ticked state, with the EXACT values", async () => {
    await httpApi.listTasks({
      statuses: ["not_started", "in_progress", "waiting_owner"],
    });
    const u = lastUrl();
    expect(u.pathname).toBe("/api/tasks");
    // The VALUES, not just "a statuses param exists" — asking for the wrong
    // three states is the same bug as asking for all of them.
    expect(u.searchParams.getAll("statuses")).toEqual([
      "not_started",
      "in_progress",
      "waiting_owner",
    ]);
    // Repeated params, not a comma-joined single value: that is the shape
    // spec/openapi.json declares and the generated Go binder reads.
    expect(u.search).toContain("statuses=not_started&statuses=in_progress");
  });

  it("carries `reassigning` verbatim — the dropdown's vocabulary, not the DB's", async () => {
    // T-9ca5 made reassigning a LOCK, but the 狀態 dropdown still lists it, so
    // the request must too (the server matches it against task.lock). Dropping
    // it client-side would silently hide every 轉派中 row; translating it into
    // "no filter" would put the whole archive back on the wire.
    await httpApi.listTasks({ statuses: ["in_progress", "reassigning"] });
    expect(lastUrl().searchParams.getAll("statuses")).toEqual([
      "in_progress",
      "reassigning",
    ]);
  });

  it("with ids alongside the status set → one repeated ids= per task next to the statuses", async () => {
    await httpApi.listTasks({ statuses: ["done"], ids: ["T-3", "T-9"] });
    expect(lastUrl().search).toBe("?statuses=done&ids=T-3&ids=T-9");
  });

  it("sends the page's sets, q, group_by, lane, limit and offset as the spec names them", async () => {
    await httpApi.listTasks({
      statuses: ["done"],
      executors: ["outsource", "m-1"],
      types: ["adhoc"],
      due: ["overdue", "soon"],
      q: " T-12 ",
      groupBy: "due",
      lane: "overdue",
      limit: 50,
      offset: 100,
    });
    expect(lastUrl().search).toBe(
      "?statuses=done&executors=outsource&executors=m-1&types=adhoc&due=overdue&due=soon" +
        "&q=T-12&group_by=due&lane=overdue&limit=50&offset=100"
    );
  });

  it("omits the param entirely for an EMPTY set (所有狀態) and for no opts", async () => {
    await httpApi.listTasks({ statuses: [], ids: [] });
    expect(lastUrl().search).toBe("");
    await httpApi.listTasks();
    expect(lastUrl().search).toBe("");
  });

  it("still sends ?open=true alone, unchanged (T-2b9d's caller is untouched)", async () => {
    await httpApi.listTasks({ open: true });
    const u = lastUrl();
    expect(u.searchParams.get("open")).toBe("true");
    expect(u.searchParams.getAll("statuses")).toEqual([]);
  });
});

describe("httpApi.listTasks · dep_tasks arrives as the server's answer (T-a3e4)", () => {
  const row = {
    id: "t-11111111aaaa",
    task_no: "T-1111",
    status: "in_progress",
    priority: "mid",
    executor_kind: "staff",
    closed_ts: null,
    deps: ["t-2222bbbbcccc", "t-3333ddddeeee"],
    progress_done: 0,
    progress_total: 1,
  };

  it("maps each dep's task_no / title / status through, closed deps included", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse([
        {
          ...row,
          dep_tasks: [
            {
              id: "t-2222bbbbcccc",
              task_no: "T-2222",
              title: "先把 SSE 重連補起來",
              status: "done",
            },
            // The dep's task is GONE: task_no still derived, nothing else.
            { id: "t-3333ddddeeee", task_no: "T-3333", title: "", status: "" },
          ],
        },
      ])
    );
    const [task] = await httpApi.listTasks({ statuses: ["in_progress"] });
    // A DONE dep is named in full even though the request asked only for
    // in_progress — the resolution is the server's, off the whole table.
    expect(task.depTasks).toEqual([
      {
        id: "t-2222bbbbcccc",
        taskNo: "T-2222",
        title: "先把 SSE 重連補起來",
        status: "done",
      },
      { id: "t-3333ddddeeee", taskNo: "T-3333", title: "", status: "" },
    ]);
  });

  it("leaves depTasks UNDEFINED when the field is absent — not []", async () => {
    // Absence means "this server does not resolve deps" (cannot name it yet);
    // an empty array would mean "every dep is unresolvable" (查無此任務). The
    // card renders those two differently, so `?? []` here would be a lie.
    fetchMock.mockImplementation(async () => jsonResponse([row]));
    const [task] = await httpApi.listTasks();
    expect(task.depTasks).toBeUndefined();
    expect(task.deps).toEqual(["t-2222bbbbcccc", "t-3333ddddeeee"]);
  });
});

describe("httpApi.getTaskPageCount", () => {
  it("asks the count route with the page's params, group_by always present, and maps the list fields", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        open: 3,
        total: 40,
        overdue: 1,
        due_soon: 0,
        due_next_change_ts: null,
        executor_counts: { "m-1": 2 },
        lanes: [],
        list_total: 2,
        now: 1792850000,
        list_due_next_change_ts: 1792851000,
      })
    );
    const got = await httpApi.getTaskPageCount({ statuses: ["in_progress"], executors: [] });
    expect(lastUrl().pathname).toBe("/api/tasks/count");
    expect(lastUrl().search).toBe("?statuses=in_progress&group_by=none");
    expect(got).toEqual({
      open: 3,
      total: 40,
      overdue: 1,
      dueSoon: 0,
      dueNextChangeTs: null,
      executorCounts: { "m-1": 2 },
      lanes: [],
      listTotal: 2,
      now: 1792850000,
      listDueNextChangeTs: 1792851000,
    });
  });
});
