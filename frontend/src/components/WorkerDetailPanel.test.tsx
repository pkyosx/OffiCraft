// The outsource-worker detail panel. Since T-f190 it MIRRORS the member detail
// panel: the SAME machine / Claude account / context% / est.$ / 最近操作 cards,
// fed by the worker DTO's runtime fold, PLUS the worker-specific bits — the
// anonymous codename, the ONE delegated task (clickable → #tasks/<id>) with its
// REAL delegator, the owner 改機器 operation, and the worker-<id> tmux command.
//
// Rendered through OfficePage so the hash → resolve → panel chain is the REAL
// wiring, not a stub. Runtime facts and the four honest spawn states are driven
// by fixtures injected into the mock adapter (the same wire→view mapper the http
// adapter uses).
//
// jsdom scope note: these assertions are text/DOM presence + the real
// mock-adapter relocate round-trip. Pure visual styling (the stuck warn tint,
// the picker's dark theme) is NOT asserted here — jsdom does not compute it.

import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { zh } from "../i18n/locales/zh";
import { ApiError } from "../api/errors";
import { OfficePage } from "./OfficePage";
import {
  __resetMock,
  __injectMockTask,
  __injectMockChat,
  __injectMockOutsourceWorker,
  __injectMockTaskType,
  __injectMockMonitoringSession,
  __setMockMemberOnline,
} from "../api/mock";
import type { TaskView, OutsourceWorkerView } from "../api/adapter";
import type { WireMonSession } from "../api/wire";

let seq = 0;

function mkTask(over: Partial<TaskView>): TaskView {
  seq += 1;
  return {
    id: `task-${seq}`,
    taskNo: `T-${1000 + seq}`,
    title: `任務 ${seq}`,
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "outsource",
    executorId: `ow-${seq}`,
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: Date.now() / 1000 - 3600,
    updatedTs: Date.now() / 1000 - 60,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    ...over,
  };
}

function mkWorker(over: Partial<OutsourceWorkerView>): OutsourceWorkerView {
  seq += 1;
  return {
    id: `ow-${seq}`,
    codename: `O-${seq}`,
    model: "Opus 4.6",
    effort: "high",
    status: "active",
    taskId: `task-${seq}`,
    taskTitle: "",
    taskStatus: "in_progress",
    createdTs: Date.now() / 1000 - 600,
    // T-f190 runtime fold — honest defaults (nothing reported): the mapper's
    // null/"" shape. Individual tests override the fields under test.
    presence: undefined,
    machine: "",
    desiredMachineId: "",
    account: null,
    contextPct: null,
    cost: null,
    bankedCost: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpReason: "",
    lastOpAt: null,
    creatorId: "",
    delegatedBy: "",
    ...over,
  };
}

/** Absence probe. `findByTestId` can only prove presence; a "the cell is gone"
 * assertion needs a query that RESOLVES TO NULL instead of throwing, or the
 * test cannot tell "not rendered" from "rendered but unmatched". */
function queryTestId(root: ParentNode, testId: string): HTMLElement | null {
  return root.querySelector<HTMLElement>(`[data-testid="${testId}"]`);
}

/** Land a telemetry row for `id` reporting `model` (+ optional effort).
 *
 * T-e12c: the 模型/思考強度 cells read the SELF-REPORTED pair, so a fixture that
 * only configures a worker leaves those cells at the honest dash. Tests whose
 * subject is something else (the runtime cells, the absence of an in-place
 * editor, the dialog lifecycle) say "and it is running <model>" with this,
 * rather than reaching back to the configured value the cells no longer show. */
function reportsModel(id: string, model: string, effort = "high") {
  __injectMockMonitoringSession({
    id,
    name: id,
    role: "",
    runtime: "claude",
    model,
    effort,
    machine: "",
    account: "",
    presence: "online",
    context_pct: null,
    cost: null,
    banked_cost: null,
    tokens: null,
  });
}

function renderOfficeAt(hash: string) {
  window.location.hash = hash;
  return render(
    <I18nProvider>
      <OfficePage />
    </I18nProvider>,
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
});

// Restore adapter spies even when the assertion that follows them throws — a
// per-test mockRestore() is skipped on failure and leaks a mocked rejection
// into the next test, turning one red into two and hiding which one is real.
afterEach(() => {
  vi.restoreAllMocks();
});

describe("WorkerDetailPanel — aligned real info (T-f190 item 1)", () => {
  it("shows the worker's REAL machine / account / context% / est.$ when reported", async () => {
    __injectMockTask(mkTask({ id: "t-1", taskNo: "T-9c21", title: "查帳單" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        codename: "O-7",
        taskId: "t-1",
        taskTitle: "查帳單",
        presence: "online",
        machine: "Warden · mbp5",
        account: "team-a@corp",
        contextPct: 42,
        cost: 3.5,
      }),
    );
    reportsModel("ow-1", "Opus 4.6");

    const { findByTestId, container } = renderOfficeAt("#office/worker/ow-1");
    await findByTestId("worker-detail-task");
    const text = container.textContent ?? "";
    expect(text).toContain("O-7");
    expect(text).toContain("Opus 4.6");
    // The aligned member-parity fields are now PRESENT (reversing the old
    // lean-panel design where they were intentionally absent).
    expect(text).toContain("Claude Account");
    expect((await findByTestId("worker-detail-machine")).textContent).toBe(
      "Warden · mbp5",
    );
    expect(text).toContain("team-a@corp");
    expect((await findByTestId("worker-detail-context")).textContent).toBe(
      "42%",
    );
    expect((await findByTestId("worker-detail-cost")).textContent).toBe("$4");
  });

  it("shows honest dashes / 尚未分配, never fabricated values, when nothing reported", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1" }), // all runtime fields at honest empty
    );

    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-machine")).textContent).toBe(
      "尚未分配",
    );
    expect((await findByTestId("worker-detail-context")).textContent).toBe("—");
    expect((await findByTestId("worker-detail-cost")).textContent).toBe("—");
  });
});

describe("WorkerDetailPanel — honest presence states (A案 P6 member vocabulary)", () => {
  // T-7526 (owner 2026-07-31): there is no 狀態 cell any more, so presence is
  // read where it is now the ONLY copy — the identity card's LifecycleDot, whose
  // aria-label is the shared `office.presence.*` wording.
  async function presenceLabelFor(over: Partial<OutsourceWorkerView>) {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", ...over }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    return (await findByTestId("worker-detail-header-dot")).getAttribute(
      "aria-label",
    );
  }

  it("未分配機器: machine cell shows 尚未分配 (presence waking, never dispatched)", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        status: "assigned",
        presence: "waking",
        machine: "",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-machine")).textContent).toBe(
      "尚未分配",
    );
    expect(
      (await findByTestId("worker-detail-header-dot")).getAttribute("aria-label"),
    ).toBe(zh.office.presence.waking);
  });

  // owner 2026-07-31 (rc-b7d1c642f2d2): ONE verb for this action on BOTH
  // panels. The worker receipt said 啟動 while the wake button said 喚醒 — the
  // member panel had the identical split (see
  // MemberDetailPanel.lastop-reason.test.tsx), so fixing one alone leaves the
  // two panels disagreeing again.
  it("names the start op 喚醒 on the worker receipt too, matching the member panel", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        lastOp: "worker_start",
        lastOpOk: true,
        lastOpAt: 1_752_400_000,
      }),
    );
    const { container } = renderOfficeAt("#office/worker/ow-1");
    await waitFor(() => {
      expect(container.querySelector(".mp-lastop__verb")?.textContent).toBe("喚醒");
    });
  });

  it("離線: the dot reads 離線 and the structured reason survives the 狀態 cell's removal", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        status: "assigned",
        presence: "offline",
        machine: "Warden · mbp5",
        lastOpReason: "spawn_timeout: no active flip in 270s",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect(
      (await findByTestId("worker-detail-header-dot")).getAttribute("aria-label"),
    ).toBe(zh.office.presence.offline);
    // 🔴 The one thing the 狀態 cell carried that the dot does NOT: WHY it is
    // grey. `lastOp` is blank here (a start that was never dispatched), so the
    // 最近操作 receipt card does not render at all — this is the only copy.
    expect(
      (await findByTestId("worker-detail-stuck-reason")).textContent,
    ).toContain("spawn_timeout");
    expect(queryTestId(document.body, "worker-detail-lastop-reason")).toBeNull();
  });

  it("運行中: presence online reads the online label", async () => {
    expect(await presenceLabelFor({ status: "active", presence: "online" })).toBe(
      zh.office.presence["online-awake"],
    );
  });

  // owner 2026-07-31:「為什麼從不同進入頁面會有不同的顯示方式?不是應該要一致嗎」
  // A released worker is reachable from exactly two entries — the chat room and
  // the detail panel — and both must say the SAME sentence from the SAME leaf.
  it("released: the panel says 已結案 in the SAME words the chat room uses", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", status: "released" }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    await findByTestId("worker-detail-released");
    // 🔴 The SAME leaf the chat header reads — asserted against the dictionary,
    // not against a literal copied into this test. A second string added for the
    // panel would still satisfy a hard-coded literal here; it cannot satisfy this.
    expect(
      (await findByTestId("worker-detail-released-sub")).textContent,
    ).toBe(zh.office.outsource.releasedSub);
    // The lifecycle affordances are gone — every worker endpoint 404s on a
    // released worker, so any of them would be a dead affordance.
    expect(queryTestId(document.body, "worker-detail-change")).toBeNull();
    expect(queryTestId(document.body, "worker-detail-wake")).toBeNull();
    expect(queryTestId(document.body, "worker-detail-stop")).toBeNull();
  });

  it("released vs merely OFFLINE are told apart, though both project the same grey dot", async () => {
    // 🔴 The negative control that makes the test above mean something.
    // `presence` is undefined for a released worker AND for one that was never
    // dispatched, so `presenceVisual` maps both to `offline` — the dot CANNOT
    // distinguish them. Without this case, "the panel says 已結案" would be
    // satisfiable by a panel that says it for every grey worker.
    __injectMockTask(mkTask({ id: "t-2" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-2",
        taskId: "t-2",
        status: "active",
        presence: "offline",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-2");
    // The ordinary panel, NOT the released view…
    expect(
      (await findByTestId("worker-detail-header-dot")).getAttribute("aria-label"),
    ).toBe(zh.office.presence.offline);
    expect(queryTestId(document.body, "worker-detail-released")).toBeNull();
    // …and it says nothing about being released.
    expect(document.body.textContent).not.toContain(
      zh.office.outsource.releasedSub,
    );
    // The wake affordance IS there — a dead session is revivable; a released
    // worker is not. That difference is the whole point of telling them apart.
    await findByTestId("worker-detail-wake");
  });
});

describe("WorkerDetailPanel — real delegator (T-f190 item 2)", () => {
  it("shows the RESOLVED member name when the creator is a member", async () => {
    __injectMockTask(mkTask({ id: "t-1", creatorId: "m-xiao" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        creatorId: "m-xiao",
        delegatedBy: "小明",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-delegator")).textContent).toBe(
      "小明",
    );
  });

  it("shows the owner label when the owner created the task", async () => {
    __injectMockTask(mkTask({ id: "t-1", creatorId: "owner" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        creatorId: "owner",
        delegatedBy: "",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-delegator")).textContent).toBe(
      "系統 Owner",
    );
  });

  it("falls back to 系統排程 (not a fabricated name) for a blank creator", async () => {
    __injectMockTask(mkTask({ id: "t-1", creatorId: "" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", creatorId: "", delegatedBy: "" }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-delegator")).textContent).toBe(
      "系統排程",
    );
  });
});

// ── T-7526: the panel is READ-ONLY and every setting goes through the 更改
// dialog (the member panel's shape since T-927a). These replace the old 改機器
// in-place-button suite: that control no longer exists, so its assertions are
// not merely red, they are unrepresentable.
describe("WorkerDetailPanel — 設定改走喚醒區 (T-7526 parity)", () => {
  it("renders the 模型 and 機器 cells with NO in-place editor on either", async () => {
    __setMockMemberOnline("warden-mbp5", true);
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", model: "Opus 4.6" }),
    );
    reportsModel("ow-1", "Opus 4.6");
    const { findByTestId, queryByTestId } = renderOfficeAt("#office/worker/ow-1");
    // Positive control FIRST: both cells really are on screen holding real
    // values. Without it "no edit button" would also pass on a panel that
    // failed to render the cells at all.
    const cell = await findByTestId("worker-detail-model-effort-cell");
    expect(cell.textContent).toContain("Opus 4.6");
    expect(await findByTestId("worker-detail-machine")).toBeTruthy();
    // …and the settings entry that replaced them is live.
    await findByTestId("worker-detail-change");
    // The two in-place editors are gone.
    expect(queryByTestId("worker-detail-model-effort-edit")).toBeNull();
    expect(queryByTestId("worker-detail-relocate")).toBeNull();
  });

  it("更改 → changing the machine REACHES relocateWorker and the 機器 cell adopts it", async () => {
    __setMockMemberOnline("warden-mbp5", true);
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", machine: "", desiredMachineId: "" }),
    );
    const relocate = vi.spyOn(api, "relocateWorker");
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-change"));
    const select = (await findByTestId(
      "worker-detail-settings-machine",
    )) as HTMLSelectElement;
    await waitFor(() =>
      expect(
        Array.from(select.options).map((o) => o.value),
      ).toContain("warden-mbp5"),
    );
    fireEvent.change(select, { target: { value: "warden-mbp5" } });
    fireEvent.click(await findByTestId("worker-detail-settings-confirm"));
    // FIRED, not merely rendered: the adapter saw the call with the chosen id…
    await waitFor(() =>
      expect(relocate).toHaveBeenCalledWith("ow-1", "warden-mbp5"),
    );
    // …and the round-trip lands on the cell.
    await waitFor(async () =>
      expect((await findByTestId("worker-detail-machine")).textContent).toBe(
        "Warden · mbp5",
      ),
    );
  });

  it("a rejected submit keeps the dialog open and shows the server's own message", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", model: "Opus 4.6" }),
    );
    vi.spyOn(api, "setWorkerModel").mockRejectedValue(
      new ApiError(
        "http 409 for POST /api/outsource-workers/ow-1/model",
        409,
        "conflict",
        "這個外包已經被釋放了",
      ),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-change"));
    const input = (await findByTestId("me-model-input")) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "claude-opus-4-8" } });
    fireEvent.click(await findByTestId("worker-detail-settings-confirm"));
    expect((await findByTestId("worker-detail-settings-error")).textContent).toBe(
      "這個外包已經被釋放了",
    );
    // The dialog stays up so the owner can retry — a closed dialog would read
    // as a save that worked.
    await findByTestId("worker-detail-settings-dialog");
  });

  it("saves the launch settings BEFORE relocating, so the respawn uses the new model", async () => {
    __setMockMemberOnline("warden-mbp5", true);
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", model: "Opus 4.6", desiredMachineId: "" }),
    );
    // A relocate kills the session and re-dispatches on the new machine, so a
    // relocate that goes first spawns on the OLD model and the owner's edit only
    // lands one respawn later.
    const order: string[] = [];
    vi.spyOn(api, "setWorkerModel").mockImplementation(async (id) => {
      order.push("model");
      return (await api.getOutsourceWorker(id)) as never;
    });
    vi.spyOn(api, "relocateWorker").mockImplementation(async (id) => {
      order.push("relocate");
      return (await api.getOutsourceWorker(id)) as never;
    });
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-change"));
    const input = (await findByTestId("me-model-input")) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "claude-opus-4-8" } });
    const select = (await findByTestId(
      "worker-detail-settings-machine",
    )) as HTMLSelectElement;
    await waitFor(() =>
      expect(Array.from(select.options).map((o) => o.value)).toContain(
        "warden-mbp5",
      ),
    );
    fireEvent.change(select, { target: { value: "warden-mbp5" } });
    fireEvent.click(await findByTestId("worker-detail-settings-confirm"));
    await waitFor(() => expect(order).toHaveLength(2));
    expect(order).toEqual(["model", "relocate"]);
  });

  it("closes an open dialog when the panel switches to another worker", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockTask(mkTask({ id: "t-2" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", model: "Opus 4.6" }),
    );
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-2", taskId: "t-2", model: "Sonnet 4.6" }),
    );
    reportsModel("ow-1", "Opus 4.6");
    reportsModel("ow-2", "Sonnet 4.6");
    const { findByTestId, queryByTestId, rerender } =
      renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-change"));
    await findByTestId("worker-detail-settings-dialog");

    // Neither caller passes a `key`, so this is a PROP change, not a remount: a
    // surviving dialog would still hold ow-1's draft and one confirm would write
    // those values onto ow-2.
    window.location.hash = "#office/worker/ow-2";
    window.dispatchEvent(new HashChangeEvent("hashchange"));
    rerender(
      <I18nProvider>
        <OfficePage />
      </I18nProvider>,
    );
    // Positive control: the panel really did move to ow-2 …
    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-model-effort-cell")).textContent,
      ).toContain("Sonnet 4.6"),
    );
    // … and it moved there with the dialog closed.
    expect(queryByTestId("worker-detail-settings-dialog")).toBeNull();
  });
});

describe("WorkerDetailPanel — worker-specific bits carry over", () => {
  it("clicking the delegated task navigates to that task", async () => {
    __injectMockTask(mkTask({ id: "t-1", taskNo: "T-9c21", title: "查帳單" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", taskTitle: "查帳單" }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-task"));
    await waitFor(() => expect(window.location.hash).toBe("#tasks/t-1"));
  });

  it("shows the member-<id> tmux attach command (P5b session naming)", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(mkWorker({ id: "ow-1", taskId: "t-1" }));
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    const copy = await findByTestId("worker-detail-copy");
    const cmd = copy
      .closest(".mp-terminal__row")
      ?.querySelector(".mp-terminal__cmd");
    expect(cmd?.textContent).toContain("member-ow-1");
  });
});

// ── T-32e1/T-f190 lifecycle ops (換手 / 停止・喚醒 / 換 model) ──────────────────
// Rendered through OfficePage → the real mock-adapter round-trip (the mock models
// the server's observable outcome). jsdom scope: DOM presence + state transitions
// are asserted; pure styling (the refocus pulse tint) is NOT (jsdom does not
// compute it) — an honest "測不到" gap, not a decorative assertion.
describe("WorkerDetailPanel — header matches the sidebar 外包 row (T-f190 UI spec)", () => {
  it("header shows codename + clickable task chip + a SHORT task-type label (same as the roster row, T-b0e3) + a real (online) dot", async () => {
    // The detail page's `worker` comes from the SAME useOutsourceWorkers() join
    // the roster reads (OfficePage.tsx) — taskTypeName/Key are derived there
    // from the task's typeKey + the registered manual, overwriting anything set
    // directly on the injected worker fixture. So exercising the real label
    // means registering the manual, not stubbing taskTypeName on the worker.
    __injectMockTaskType({
      typeKey: "tm-officraft-dev",
      displayName: "OffiCraft 開發",
      purpose: "",
    });
    __injectMockTask(
      mkTask({
        id: "t-1",
        taskNo: "T-e9f4",
        title: "Planning for big change",
        typeKey: "tm-officraft-dev",
      }),
    );
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        codename: "O-19",
        taskId: "t-1",
        taskNo: "T-e9f4",
        taskTitle: "Planning for big change",
        presence: "online",
      }),
    );
    const { findByTestId, findByText } = renderOfficeAt("#office/worker/ow-1");
    const header = await findByTestId("worker-detail-header-task");
    // The codename line shows the outsource identity label 「外包 · 代號」,
    // matching the sidebar 外包 row (T-3ed8, owner 2026-07-20 完全一致).
    await findByText("外包 · O-19");
    expect((await findByTestId("worker-detail-header-chip")).textContent).toBe("T-e9f4");
    // T-b0e3: the slot that used to hold the FULL task title now renders the
    // SAME short type label the roster row shows (taskTypeName), never the
    // full title/description sentence.
    expect(header.textContent).toContain("OffiCraft 開發");
    expect(header.textContent).not.toContain("Planning for big change");
    // The old raw ow-id chip is gone (the header no longer renders worker.id).
    expect(header.textContent).not.toContain("ow-1");
    // Real presence: online → the shared lifecycle dot's ONLINE class (the
    // colour comes from --color-dot-online, never an inline literal).
    const dot = await findByTestId("worker-detail-header-dot");
    expect(dot.className).toBe("lifecycle-dot lifecycle-dot--online-awake");
    expect(dot.getAttribute("style")).toBeNull();
    // Clicking the chip routes to the bound task.
    fireEvent.click(await findByTestId("worker-detail-header-chip"));
    await waitFor(() => expect(window.location.hash).toBe("#tasks/t-1"));
  });

  it("header falls back to 自由代辦 when the task has no type (adhoc, blank typeKey)", async () => {
    __injectMockTask(
      mkTask({ id: "t-1", taskNo: "T-adhc", title: "隨手需求", typeKey: "" }),
    );
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        taskNo: "T-adhc",
        taskTitle: "隨手需求",
        presence: "online",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    const header = await findByTestId("worker-detail-header-task");
    expect(header.textContent).toContain("自由代辦");
    expect(header.textContent).not.toContain("隨手需求");
  });

  // T-59d6: the header dot is the SAME shared LifecycleDot the rail row and the
  // 正職 roster render — so every non-online state gets its OWN colour class
  // (not one shared grey) and its own label. Asserting the exact class is what
  // makes a mutant that repaints any of these green go RED here too.
  it.each([
    ["online", "online-awake"],
    ["waking", "waking"],
    ["stopping", "stopping"],
    ["stopped", "stopped"],
    ["offline", "offline"],
    [undefined, "offline"],
  ] as ReadonlyArray<[OutsourceWorkerView["presence"], string]>)(
    "header dot for presence %s is the %s lifecycle dot",
    async (presence, visual) => {
      __injectMockTask(mkTask({ id: "t-1" }));
      __injectMockOutsourceWorker(
        mkWorker({ id: "ow-1", taskId: "t-1", presence }),
      );
      const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
      const dot = await findByTestId("worker-detail-header-dot");
      expect(dot.className).toBe(`lifecycle-dot lifecycle-dot--${visual}`);
      expect(dot.getAttribute("style")).toBeNull();
      if (presence !== "online") {
        expect(dot.className).not.toContain("online-awake");
      }
    },
  );
});

describe("WorkerDetailPanel — lifecycle ops (T-32e1/T-f190)", () => {
  it("refocus is disabled off-line and enabled online", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", presence: "offline" }),
    );
    const { findByTestId, rerender } = renderOfficeAt("#office/worker/ow-1");
    const btn = (await findByTestId("worker-detail-refocus")) as HTMLButtonElement;
    expect(btn.disabled).toBe(true); // offline: online-only gate mirrored client-side

    __resetMock();
    __injectMockTask(mkTask({ id: "t-2" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-2", taskId: "t-2", presence: "online" }),
    );
    window.location.hash = "#office/worker/ow-2";
    rerender(
      <I18nProvider>
        <OfficePage />
      </I18nProvider>,
    );
    const btn2 = (await findByTestId("worker-detail-refocus")) as HTMLButtonElement;
    expect(btn2.disabled).toBe(false);
  });

  it("refocus round-trips: clicking online surfaces the sent acknowledgement", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", presence: "online" }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-refocus"));
    // The mock stamps refocus_since; the panel keeps the persistent "sent" note.
    await findByTestId("worker-detail-refocus-note");
  });

  it("stop → the dot flips to 已停止 and the row swaps 更改／停止 for 喚醒", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", presence: "online" }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    // Live: 更改 ＋ 停止 side by side (owner 2026-07-31 「左右並排」).
    const change = await findByTestId("worker-detail-change");
    const stop = await findByTestId("worker-detail-stop");
    expect(stop.textContent).toBe(zh.workerDetail.stop);
    expect(change.parentElement).toBe(stop.parentElement);
    expect(stop.parentElement?.className).toContain("mp-identity__buttons");
    // 更改 is written FIRST so the row reads 更改 ＋ 停止, in that order.
    expect(
      change.compareDocumentPosition(stop) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();

    fireEvent.click(stop);
    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-header-dot")).getAttribute("aria-label"),
      ).toBe(zh.office.presence.stopped),
    );
    // …and the pair collapses to the ONE wake button.
    expect((await findByTestId("worker-detail-wake")).textContent).toBe(
      zh.lifecycle.action.spawn,
    );
    expect(queryTestId(document.body, "worker-detail-change")).toBeNull();
    expect(queryTestId(document.body, "worker-detail-stop")).toBeNull();
  });

  it("喚醒 ASKS FIRST: it opens the settings dialog and reaches NO endpoint until confirmed", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        presence: "stopped",
        desiredState: "offline",
      }),
    );
    const restart = vi.spyOn(api, "restartWorker");
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    // Nothing is open before the click — otherwise "the dialog is open after"
    // would be true no matter what the button does.
    expect(queryTestId(document.body, "worker-detail-settings-dialog")).toBeNull();

    fireEvent.click(await findByTestId("worker-detail-wake"));
    // 🔴 The dialog opened AND the wake endpoint was NOT reached: the whole
    // point of the ruling is that the click asks before it sends.
    await findByTestId("worker-detail-settings-dialog");
    expect(restart).not.toHaveBeenCalled();
    // The dialog says what the confirm will actually DO (喚醒, not 更改).
    expect(
      (await findByTestId("worker-detail-settings-confirm")).textContent,
    ).toBe(zh.lifecycle.action.spawn);

    fireEvent.click(await findByTestId("worker-detail-settings-confirm"));
    await waitFor(() => expect(restart).toHaveBeenCalledWith("ow-1"));
    // Accepting the prefilled values UNCHANGED must still wake — the no-edit
    // early-return belongs to 更改, never to 喚醒.
    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-header-dot")).getAttribute("aria-label"),
      ).toBe(zh.office.presence.waking),
    );
  });

  it("喚醒's four cells are PRE-SEEDED with the worker's own current settings", async () => {
    // A live online machine must exist, or "the pin survived" is vacuously true:
    // a seed that PREFERS the first online machine has nothing to prefer.
    __setMockMemberOnline("warden-mbp5", true);
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        presence: "offline",
        desiredState: "online",
        runtime: "codex",
        model: "gpt-5-codex",
        effort: "low",
        desiredMachineId: "m-asleep",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-wake"));
    // owner 2026-07-31:「應該先預設跟原本一樣」— every one of the four.
    const runtime = (await findByTestId("me-runtime-select")) as HTMLSelectElement;
    const effort = (await findByTestId("me-effort-select")) as HTMLSelectElement;
    const machine = (await findByTestId(
      "worker-detail-settings-machine",
    )) as HTMLSelectElement;
    // codex runtime ⇒ the model field is the CodexModelSelect's free input.
    const model = (await findByTestId(
      "me-codex-model-select-input",
    )) as HTMLInputElement;
    expect(runtime.value).toBe("codex");
    expect(model.value).toBe("gpt-5-codex");
    expect(effort.value).toBe("low");
    expect(machine.value).toBe("m-asleep");
    // …and they are EDITABLE, not pinned (owner: 「不是釘死」). Changing the
    // runtime is the strongest proof: it is the cell a "seed it and freeze it"
    // reading would have locked hardest.
    fireEvent.change(runtime, { target: { value: "claude" } });
    expect(
      ((await findByTestId("me-runtime-select")) as HTMLSelectElement).value,
    ).toBe("claude");
    fireEvent.change(effort, { target: { value: "high" } });
    expect(
      ((await findByTestId("me-effort-select")) as HTMLSelectElement).value,
    ).toBe("high");
    const input = (await findByTestId("me-model-input")) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "claude-opus-4-8" } });
    expect(input.value).toBe("claude-opus-4-8");
    // owner 2026-07-31 (rc-b7d1c642f2d2): ONE verb. The note under these cells
    // said 下次啟動生效 while the member panel's identical note said
    // 下次喚醒生效 — literal, not zh.*, or the assertion moves with the string.
    expect(
      (await findByTestId("worker-detail-settings-note")).textContent,
    ).toContain("下次喚醒生效");
  });

  it("喚醒 stores the launch settings and the pin BEFORE it wakes, so the new session boots as described", async () => {
    __setMockMemberOnline("warden-mbp5", true);
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        presence: "stopped",
        desiredState: "offline",
        model: "Opus 4.6",
        desiredMachineId: "",
      }),
    );
    const order: string[] = [];
    const setModel = vi
      .spyOn(api, "setWorkerModel")
      .mockImplementation(async () => {
        order.push("model");
        return mkWorker({ id: "ow-1" });
      });
    const relocate = vi
      .spyOn(api, "relocateWorker")
      .mockImplementation(async () => {
        order.push("relocate");
        return mkWorker({ id: "ow-1" });
      });
    const restart = vi
      .spyOn(api, "restartWorker")
      .mockImplementation(async () => {
        order.push("wake");
        return mkWorker({ id: "ow-1" });
      });

    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-wake"));
    const input = (await findByTestId("me-model-input")) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "claude-opus-4-8" } });
    const select = (await findByTestId(
      "worker-detail-settings-machine",
    )) as HTMLSelectElement;
    await waitFor(() =>
      expect(Array.from(select.options).map((o) => o.value)).toContain(
        "warden-mbp5",
      ),
    );
    const target = "warden-mbp5";
    fireEvent.change(select, { target: { value: target } });
    fireEvent.click(await findByTestId("worker-detail-settings-confirm"));

    await waitFor(() => expect(restart).toHaveBeenCalledWith("ow-1"));
    expect(setModel).toHaveBeenCalledWith(
      "ow-1",
      expect.objectContaining({ model: "claude-opus-4-8" }),
    );
    expect(relocate).toHaveBeenCalledWith("ow-1", target);
    // 🔴 The wake is LAST. Waking first boots the OLD model on the OLD machine
    // and the owner's edit only lands one respawn later.
    expect(order).toEqual(["model", "relocate", "wake"]);
  });

  it("opening 喚醒 on a worker pinned to a SLEEPING machine never silently re-pins it", async () => {
    // The defect this rule exists for: seeding the machine cell with "the first
    // ONLINE machine" makes machineChanged unconditionally true for a worker
    // parked on a machine that is merely asleep — so editing the MODEL moves it.
    // 🔴 The online machine is REQUIRED, not scenery: with an empty registry the
    // buggy seed falls through to the pin anyway and this test passes on the
    // defect. There must be somewhere else to be re-pinned TO.
    __setMockMemberOnline("warden-mbp5", true);
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        presence: "stopped",
        desiredState: "offline",
        model: "Opus 4.6",
        desiredMachineId: "m-asleep",
      }),
    );
    const relocate = vi.spyOn(api, "relocateWorker");
    const restart = vi.spyOn(api, "restartWorker");
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-wake"));
    // The sleeping pin is still the selected value (and offered, disabled) —
    // not swapped for whichever machine happens to be up.
    const select = (await findByTestId(
      "worker-detail-settings-machine",
    )) as HTMLSelectElement;
    await waitFor(() =>
      expect(Array.from(select.options).map((o) => o.value)).toContain(
        "warden-mbp5",
      ),
    );
    expect(select.value).toBe("m-asleep");
    // Edit ONLY the model, then confirm.
    fireEvent.change((await findByTestId("me-model-input")) as HTMLInputElement, {
      target: { value: "claude-opus-4-8" },
    });
    fireEvent.click(await findByTestId("worker-detail-settings-confirm"));
    await waitFor(() => expect(restart).toHaveBeenCalledWith("ow-1"));
    // 🔴 Positive control on the TARGET, not on "something happened": the wake
    // went out, and relocate — the one call that would have moved him — did not.
    expect(relocate).not.toHaveBeenCalled();
  });

  it("換 model: 更改 → save persists the new model via the adapter", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", model: "Opus 4.6", presence: "online" }),
    );
    const setModel = vi.spyOn(api, "setWorkerModel");
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    // The ONE settings entry (T-7526) — the model cell itself is read-only.
    fireEvent.click(await findByTestId("worker-detail-change"));
    // The shared ModelEffortEditor's free custom-model input (data-testid pinned).
    const input = (await findByTestId("me-model-input")) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "claude-opus-4-8" } });
    fireEvent.click(await findByTestId("worker-detail-settings-confirm"));
    // The endpoint was actually REACHED with the edited value…
    await waitFor(() =>
      expect(setModel).toHaveBeenCalledWith(
        "ow-1",
        expect.objectContaining({ model: "claude-opus-4-8" }),
      ),
    );
    // …and the panel adopts it after the outsource_worker refetch. Read back
    // from the DIALOG, not the 模型 cell: since T-e12c that cell states the
    // SELF-REPORTED model, which does not change just because a new launch
    // intent was stored (the running session is still on the old one until it
    // respawns). The dialog is where the configured value lives, so that is
    // where "the save landed and the UI adopted it" is honestly observable.
    fireEvent.click(await findByTestId("worker-detail-change"));
    await waitFor(async () =>
      expect(
        ((await findByTestId("me-model-input")) as HTMLInputElement).value,
      ).toBe("claude-opus-4-8"),
    );
  });
});

// ── T-ba6b convergence: the worker now renders through the SHARED
// AgentDetailPanel (same component + view model as the member), so these three
// assert the convergence-specific behaviour: the readable Claude account (with a
// negative control that no raw internal identifier reaches the page), the
// live+banked cost 口徑, and the initial-prompt preview with its honest caveat.
describe("WorkerDetailPanel — Claude Account is readable, raw keys never leak (T-ba6b)", () => {
  it("shows the resolved account name AND never renders a raw credential key / internal id", async () => {
    __injectMockTask(mkTask({ id: "t-1", taskNo: "T-9c21", title: "查帳單" }));
    __injectMockOutsourceWorker(
      mkWorker({
        // A distinctive raw-key-shaped id: if any card fell back to an internal
        // identifier for the account, this string would surface on the page.
        id: "ow-5e163893-a1b2-4c3d-raw-key",
        codename: "O-7",
        taskId: "t-1",
        taskTitle: "查帳單",
        presence: "online",
        account: "shawn-claude", // the server-resolved readable alias
      }),
    );
    const { findByTestId, container } = renderOfficeAt(
      "#office/worker/ow-5e163893-a1b2-4c3d-raw-key",
    );
    // POSITIVE CONTROL: the readable account name is present in its cell.
    expect((await findByTestId("worker-detail-account")).textContent).toBe(
      "shawn-claude",
    );
    // NEGATIVE: the raw internal identifier appears NOWHERE on the page — not in
    // the account cell, not the header, not the tmux command's rendered id chip.
    // (The tmux attach line legitimately contains worker-<id>; scope the raw-key
    // check to everything OUTSIDE the terminal command.)
    const text = container.textContent ?? "";
    expect(text).toContain("shawn-claude");
    const account = await findByTestId("worker-detail-account");
    expect(account.textContent).not.toContain("raw-key");
    const header = await findByTestId("worker-detail-header-task");
    expect(header.textContent ?? "").not.toContain("raw-key");
  });

  it("renders an honest dash (never a raw key) when the account is unresolved", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      // account null = the server could resolve no alias/label; the panel must
      // show a bare dash, NEVER the raw telemetry key the server withheld.
      mkWorker({ id: "ow-1", taskId: "t-1", presence: "online", account: null }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-account")).textContent).toBe("—");
  });
});

describe("WorkerDetailPanel — cost 口徑 = live + banked (T-ba6b, member parity)", () => {
  it("sums the live session cost and the banked historical cost", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        presence: "online",
        cost: 2, // current live session
        bankedCost: 5, // banked across prior kill+respawn handovers
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    // 2 + 5 = 7 → formatCost → "$7" (NOT "$2": a converged panel must not drop
    // the banked spend — the pre-convergence worker panel showed live only).
    expect((await findByTestId("worker-detail-cost")).textContent).toBe("$7");
  });

  it("shows banked-only cost when there is no live session (handed-over worker)", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        presence: "waking",
        cost: null, // no live session yet
        bankedCost: 4,
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-cost")).textContent).toBe("$4");
  });
});

describe("WorkerDetailPanel — initial-prompt preview (T-ba6b)", () => {
  it("expands to the boot-context preview and carries the honest re-assembly caveat", async () => {
    __injectMockTask(
      mkTask({ id: "t-1", taskNo: "T-9c21", title: "查帳單對帳" }),
    );
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        codename: "O-42",
        taskId: "t-1",
        taskTitle: "查帳單對帳",
        presence: "online",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    // Lazy-fetched on first expand (the mock re-assembles from current rows).
    fireEvent.click(await findByTestId("worker-detail-prompt-toggle"));
    const body = await findByTestId("worker-detail-prompt-body");
    // POSITIVE control: the real assembly landed, not an empty body — the two
    // shared slots a worker actually receives.
    await waitFor(() =>
      expect(body.textContent ?? "").toContain("Global Context"),
    );
    expect(body.textContent ?? "").toContain("啟動程序");
    // T-4595: this used to assert the codename and the bound task title were in
    // the preview. Both are gone — a worker's boot context is the staff fold
    // minus the persona slot, with no identity block, no task and no manual —
    // so asserting their ABSENCE is what keeps the cockpit honest about what it
    // is showing.
    expect(body.textContent ?? "").not.toContain("O-42");
    expect(body.textContent ?? "").not.toContain("查帳單對帳");
    // The honesty caveat is present (目前版本重組, 非派工當下逐字版).
    const note = await findByTestId("worker-detail-prompt-note");
    expect(note.textContent ?? "").toContain("非派工當下");
  });

  // T-7526: the shared card's load lifecycle. `vm.prompt.fetch` is an inline
  // arrow OfficePage rebuilds on every render, so a repaint mid-read used to
  // cancel the read AND leave a "loaded" stamp behind — the card sat on
  // 「載入中…」 for good. The member half of this proof lives in
  // MemberDetailPanel.initial-prompt.test.tsx.
  it("still shows the prompt when the panel repaints while the read is in flight", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(mkWorker({ id: "ow-1", taskId: "t-1" }));
    let land: (v: string) => void = () => {};
    const boot = vi
      .spyOn(api, "getWorkerBootContext")
      .mockImplementation(
        () => new Promise<string>((resolve) => (land = resolve)),
      );

    const { findByTestId, rerender } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-prompt-toggle"));
    // Positive control: the read really is under way, so the repaint below has
    // something to interrupt.
    expect(
      (await findByTestId("worker-detail-prompt-body")).textContent,
    ).toContain(zh.mp.promptLoading);

    // A repaint — an ordinary SSE delta is enough in the running app.
    rerender(
      <I18nProvider>
        <OfficePage />
      </I18nProvider>,
    );
    land("外包啟動指示");

    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-prompt-body")).textContent,
      ).toContain("外包啟動指示"),
    );
    // A repaint is not a reason to re-read either — the ONE read that was
    // already under way is the one that lands.
    expect(boot).toHaveBeenCalledTimes(1);
  });

  it("a failed read shows the error with a retry that actually re-reads", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(mkWorker({ id: "ow-1", taskId: "t-1" }));
    const boot = vi
      .spyOn(api, "getWorkerBootContext")
      .mockRejectedValueOnce(new Error("boom"))
      .mockResolvedValueOnce("外包啟動指示");

    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-prompt-toggle"));
    const err = await findByTestId("worker-detail-prompt-error");
    expect(err.textContent).toContain(zh.mp.promptError);

    fireEvent.click(await findByTestId("worker-detail-prompt-retry"));
    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-prompt-body")).textContent,
      ).toContain("外包啟動指示"),
    );
    expect(boot).toHaveBeenCalledTimes(2);
  });

  // The owner's actual symptom: 「關掉重開也救不回來」. The loaded stamp used to be
  // written when the read STARTED, so once a read had been fired the effect bailed
  // out for good — collapsing and re-expanding could not get past it either.
  it("re-expanding after a failed read reads again instead of resurrecting 載入中", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(mkWorker({ id: "ow-1", taskId: "t-1" }));
    const boot = vi
      .spyOn(api, "getWorkerBootContext")
      .mockRejectedValueOnce(new Error("boom"))
      .mockResolvedValueOnce("外包啟動指示");

    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    const toggle = await findByTestId("worker-detail-prompt-toggle");
    fireEvent.click(toggle);
    await findByTestId("worker-detail-prompt-error");
    // Collapse, re-expand — the recovery path the owner actually tried.
    fireEvent.click(toggle);
    fireEvent.click(toggle);
    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-prompt-body")).textContent,
      ).toContain("外包啟動指示"),
    );
    expect(boot).toHaveBeenCalledTimes(2);
  });
});

// ── T-e12c: the 模型/思考強度 cell states what is RUNNING; the 更改／喚醒 dialog
// owns the SETTING. Owner ruling 2026-07-31:「成員面板以及監控台，一定要顯示
// 回報回來的狀態，不能顯示設定值」. The two halves are pinned together on
// purpose — the readout must never fall back to the configured pair, and the
// dialog must never seed from (and therefore save back) a telemetry value or a
// blank. T-7526 moved the editor out of the cell and into the dialog; these
// pin the RULE, so they follow it there rather than the markup it used to have.
describe("WorkerDetailPanel — reported state vs configured launch intent (T-e12c)", () => {
  function liveWorkerReporting(over: Partial<WireMonSession> = {}) {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        presence: "online",
        model: "Opus 4.6",
        effort: "high",
      }),
    );
    __injectMockMonitoringSession({
      id: "ow-1",
      name: "O-1",
      role: "",
      runtime: "claude",
      model: "claude-sonnet-4.9",
      effort: "low",
      machine: "",
      account: "",
      presence: "online",
      context_pct: null,
      cost: null,
      banked_cost: null,
      tokens: null,
      ...over,
    });
  }

  it("reads out the REPORTED model/effort while the dialog holds the configured pair", async () => {
    liveWorkerReporting();

    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-model-value")).textContent).toBe(
      "claude-sonnet-4.9",
    );
    expect(
      (await findByTestId("worker-detail-effort-value")).textContent,
    ).toContain("low");

    // The setting lives in the 更改 dialog and is seeded from the WORKER, not
    // from what the session happens to be running.
    fireEvent.click(await findByTestId("worker-detail-change"));
    expect(
      ((await findByTestId("me-model-input")) as HTMLInputElement).value,
    ).toBe("Opus 4.6");
    expect(
      ((await findByTestId("me-effort-select")) as HTMLSelectElement).value,
    ).toBe("high");
  });

  it("blanks the readout when nothing was reported, even though the worker IS configured", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        presence: "online",
        model: "Opus 4.6",
        effort: "high",
      }),
    );

    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-model-value")).textContent).toBe(
      "—",
    );
    expect((await findByTestId("worker-detail-effort-value")).textContent).toBe(
      "—",
    );
    // …and the configured pair is intact, just not on the readout: the dialog
    // still opens on it. A blank must never reach the save body either.
    fireEvent.click(await findByTestId("worker-detail-change"));
    expect(
      ((await findByTestId("me-model-input")) as HTMLInputElement).value,
    ).toBe("Opus 4.6");
    expect(
      ((await findByTestId("me-effort-select")) as HTMLSelectElement).value,
    ).toBe("high");
  });

  it("writes nothing when the dialog is confirmed unchanged, so no telemetry value can reach the save", async () => {
    liveWorkerReporting();
    const setModel = vi.spyOn(api, "setWorkerModel");

    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-change"));
    // Seeded from the worker, so a no-edit confirm is a true no-op. Were the
    // dialog seeded from the REPORTED pair instead, launchChanged would be true
    // and this click would silently overwrite the owner's intent with telemetry.
    fireEvent.click(await findByTestId("worker-detail-settings-confirm"));

    await waitFor(async () =>
      expect(await findByTestId("worker-detail-change")).toBeTruthy(),
    );
    expect(setModel).not.toHaveBeenCalled();
    const workers = await api.listOutsourceWorkers();
    const saved = workers.find((w) => w.id === "ow-1")!;
    expect(saved.model).toBe("Opus 4.6");
    expect(saved.effort).toBe("high");
  });
});

// T-7f28 — the outsource panel had NO "changed, not applied yet" marks at all,
// not even for 機器, which the member panel has had all along. These pin the
// four cells it now carries, plus the no-clutter condition the owner attached
// to the request (「但又不想要畫面太雜亂」).
describe("WorkerDetailPanel — pending launch changes (T-7f28)", () => {
  const CELLS = ["runtime", "model", "effort", "machine"] as const;

  it("marks each cell whose reported value differs from the configured one", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        // configured (the settings dialog's round-trip values)…
        runtime: "codex",
        model: "Opus 4.6",
        effort: "high",
        desiredMachineId: "warden-mbp5",
        // …versus what the worker's session actually reported. `machine` is the
        // server-RESOLVED display name; `actualMachine` is a raw id, like the
        // pin — the panel has to resolve before it compares.
        actualRuntime: "claude",
        actualModel: "claude-sonnet-4-5",
        actualEffort: "low",
        machine: "",
        actualMachine: "warden-elsewhere",
      }),
    );

    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect(
      (await findByTestId("worker-detail-runtime-pending")).textContent,
    ).toContain("Codex");
    expect(
      (await findByTestId("worker-detail-model-pending")).textContent,
    ).toContain("Opus 4.6");
    expect(
      (await findByTestId("worker-detail-effort-pending")).textContent,
    ).toContain("high");
    expect(
      (await findByTestId("worker-detail-machine-pending")).textContent,
    ).toContain("Warden · mbp5");
    // The READOUTS stay on the reported side — the whole point is that the two
    // are legible as different things at the same time.
    expect((await findByTestId("worker-detail-runtime-value")).textContent).toBe(
      "Claude Code",
    );
  });

  it("adds nothing to the panel when every reported value already agrees", async () => {
    __injectMockTask(mkTask({ id: "t-2" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-2",
        runtime: "claude",
        actualRuntime: "claude",
        model: "Opus 4.6",
        actualModel: "Opus 4.6",
        effort: "high",
        actualEffort: "high",
        // 🔴 The regression this case exists for: the pin is a raw id and the
        // OBSERVED machine arrives already resolved to its display name. A
        // comparison that forgets to resolve marks every correctly placed
        // worker as mid-relocation.
        desiredMachineId: "warden-mbp5",
        machine: "Warden · mbp5",
        actualMachine: "warden-mbp5",
      }),
    );

    const { findByTestId, queryByTestId } = renderOfficeAt(
      "#office/worker/ow-1",
    );
    await findByTestId("worker-detail-task");
    for (const cell of CELLS) {
      expect(queryByTestId(`worker-detail-${cell}-pending`)).toBeNull();
    }
  });

  it("stays silent when the worker has reported nothing, rather than echoing the settings", async () => {
    // 🔴 The reason this ticket exists. `mkWorker` leaves every actual_* blank —
    // an unreported worker. Marking a pending change here would be a guess, and
    // showing the configured value as the readout (the old behaviour) would be
    // a claim that it is already running.
    __injectMockTask(mkTask({ id: "t-3" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-3", runtime: "codex", model: "Opus 4.6" }),
    );

    const { findByTestId, queryByTestId } = renderOfficeAt(
      "#office/worker/ow-1",
    );
    expect((await findByTestId("worker-detail-runtime-value")).textContent).toBe(
      "—",
    );
    for (const cell of CELLS) {
      expect(queryByTestId(`worker-detail-${cell}-pending`)).toBeNull();
    }
  });
});

// ── the resume summary card (T-4595) ────────────────────────────────────────
//
// The owner released ONE member verb to workers — reading a worker's resume
// summary — so the outsource panel now renders the same 履歷摘要 card the staff
// panel does, from the same component.
//
// This test exists because NOTHING else covered the slot: swapping the panel's
// old notHere() placeholder for the real card turned zero tests red. A slot
// whose content no test asserts is a slot that can quietly go back to being
// empty.
//
// 🔴 THE SHELL IS NOT THE PAYLOAD. `mp-resume-body` renders in the ERROR state
// too (ResumeSummaryCard puts `mp-resume-error` INSIDE it), so a test that stops
// at "the container appeared" + "the spy was called with ow-r1" is satisfied by
// a card drawing 讀取喚醒快照失敗 — which is exactly what the mock produced
// before the `ow-` id was let through `findResumeSummaryTarget`. So the
// assertions below reach for the OVERVIEW GRID and the individual stat values,
// and they pin the worker's OWN numbers (its own chat line, its own bound task)
// rather than a shape any snapshot would satisfy.
describe("WorkerDetailPanel · 履歷摘要", () => {
  it("renders the shared resume summary card, and fetches only on expand", async () => {
    __injectMockTask(
      mkTask({
        id: "t-r1",
        taskNo: "T-4595",
        title: "履歷摘要",
        // This worker's OWN task. The mock's task block matches on executorId
        // alone (server parity — dal ListOpenTasksByExecutor filters on
        // executor_id and nothing else), so an executorKind gate reappearing
        // there would empty this list while everything else stayed green.
        executorId: "ow-r1",
      }),
    );
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-r1", codename: "O-9", taskId: "t-r1", taskTitle: "履歷摘要" }),
    );
    // One inbound line from THIS worker, so chatCount/chatChars are its own and
    // not a number every agent in the mock would share.
    __injectMockChat({
      id: "m-r1",
      from: "ow-r1",
      to: "owner",
      body: "回報",
      ts: Date.now() / 1000 - 30,
      attachments: [],
      replyCardId: null,
    });
    const spy = vi.spyOn(api, "getMemberResumeSummary");

    const { findByTestId, queryByTestId } = renderOfficeAt("#office/worker/ow-r1");
    const toggle = await findByTestId("mp-resume-toggle");

    // Collapsed: the card is there, the request is NOT. This half is the one
    // the staff panel's own comment calls a HARD REQUIREMENT — a panel that
    // pulls a wake snapshot on every open would make opening the panel
    // expensive for every agent in the roster.
    expect(queryByTestId("mp-resume-body")).toBeNull();
    expect(spy).not.toHaveBeenCalled();

    fireEvent.click(toggle);
    await findByTestId("mp-resume-body");
    // Fetched for THIS worker — an id mix-up would show a stranger's snapshot
    // under this worker's name, which reads as truth.
    expect(spy).toHaveBeenCalledWith("ow-r1");

    // The PAYLOAD arrived and was drawn: the overview grid only renders on the
    // success branch, and the error branch renders `mp-resume-error` instead.
    await findByTestId("mp-resume-overview");
    expect(queryByTestId("mp-resume-error")).toBeNull();

    // …and the numbers in it are THIS worker's snapshot, not an empty husk.
    expect((await findByTestId("mp-resume-stat-chatCount")).textContent).toBe("1");
    expect((await findByTestId("mp-resume-stat-tasksReturned")).textContent).toBe("1");
    expect((await findByTestId("mp-resume-stat-tasksOpenTotal")).textContent).toBe("1");
    // The bound task's own row, by its task number — the task block really
    // resolved to the row this worker executes.
    expect((await findByTestId("mp-resume-body")).textContent).toContain("T-4595");

    spy.mockRestore();
  });
});
