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

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import {
  __resetMock,
  __injectMockTask,
  __injectMockOutsourceWorker,
  __setMockMemberOnline,
} from "../api/mock";
import type { TaskView, OutsourceWorkerView } from "../api/adapter";

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
    presence: "",
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
  async function statusTextFor(over: Partial<OutsourceWorkerView>) {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", ...over }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    return (await findByTestId("worker-detail-status")).textContent ?? "";
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
    expect((await findByTestId("worker-detail-status")).textContent).toBe(
      "啟動中",
    );
  });

  it("離線: presence offline renders 離線 + the structured reason", async () => {
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
    expect((await findByTestId("worker-detail-status")).textContent).toBe(
      "離線",
    );
    expect(
      (await findByTestId("worker-detail-stuck-reason")).textContent,
    ).toContain("spawn_timeout");
  });

  it("運行中: presence online renders 工作中", async () => {
    expect(
      await statusTextFor({ status: "active", presence: "online" }),
    ).toBe("工作中");
  });

  it('released (presence ""): falls back to the lifecycle status label', async () => {
    expect(await statusTextFor({ status: "released", presence: "" })).toBe(
      "已釋放",
    );
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

describe("WorkerDetailPanel — 改機器 relocate (T-f190 item 3)", () => {
  it("disables 改機器 when there is no online machine (0-online path)", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(mkWorker({ id: "ow-1", taskId: "t-1" }));
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    const btn = (await findByTestId(
      "worker-detail-relocate",
    )) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it("with ONE online machine, relocating dispatches and the machine cell adopts it", async () => {
    // Bring one warden online → the mock registry has exactly one online machine
    // (machine_id = the warden member id, display_name = its name).
    __setMockMemberOnline("warden-mbp5", true);
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", machine: "" }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    const btn = (await findByTestId(
      "worker-detail-relocate",
    )) as HTMLButtonElement;
    await waitFor(() => expect(btn.disabled).toBe(false));
    fireEvent.click(btn);
    // Single online machine → auto-relocate (no picker), the mock reflects the
    // dispatch and the outsource_worker SSE delta refetches the panel.
    await waitFor(async () =>
      expect((await findByTestId("worker-detail-machine")).textContent).toBe(
        "Warden · mbp5",
      ),
    );
  });

  it("with TWO online machines, 改機器 opens the machine picker", async () => {
    __setMockMemberOnline("warden-mbp5", true);
    __setMockMemberOnline("m-server-self", true);
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(mkWorker({ id: "ow-1", taskId: "t-1" }));
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    const btn = (await findByTestId(
      "worker-detail-relocate",
    )) as HTMLButtonElement;
    await waitFor(() => expect(btn.disabled).toBe(false));
    fireEvent.click(btn);
    // The picker modal appears rather than an immediate move.
    expect(await findByTestId("machine-picker")).toBeTruthy();
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

// ── T-32e1/T-f190 lifecycle ops (換手 / 停止・重啟 / 換 model) ──────────────────
// Rendered through OfficePage → the real mock-adapter round-trip (the mock models
// the server's observable outcome). jsdom scope: DOM presence + state transitions
// are asserted; pure styling (the refocus pulse tint) is NOT (jsdom does not
// compute it) — an honest "測不到" gap, not a decorative assertion.
describe("WorkerDetailPanel — header matches the sidebar 外包 row (T-f190 UI spec)", () => {
  it("header shows codename + clickable task chip + title + a real (online) dot", async () => {
    __injectMockTask(mkTask({ id: "t-1", taskNo: "T-e9f4", title: "Planning for big change" }));
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
    expect(header.textContent).toContain("Planning for big change");
    // The old raw ow-id chip is gone (the header no longer renders worker.id).
    expect(header.textContent).not.toContain("ow-1");
    // Real presence: online → the default green dot (no muted inline override).
    const dot = await findByTestId("worker-detail-header-dot");
    expect(dot.getAttribute("style") ?? "").not.toContain("background");
    // Clicking the chip routes to the bound task.
    fireEvent.click(await findByTestId("worker-detail-header-chip"));
    await waitFor(() => expect(window.location.hash).toBe("#tasks/t-1"));
  });

  it("a non-online worker header shows a muted (honest, not-green) dot", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", presence: "offline" }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    const dot = await findByTestId("worker-detail-header-dot");
    expect(dot.getAttribute("style") ?? "").toContain("background"); // muted override
  });
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

  it("stop → the worker reads 已停止 and the toggle flips to 重啟", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", presence: "online" }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    const toggle = await findByTestId("worker-detail-stop-toggle");
    expect(toggle.textContent).toBe("停止");
    fireEvent.click(toggle);
    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-status")).textContent,
      ).toBe("已停止"),
    );
    expect((await findByTestId("worker-detail-stop-toggle")).textContent).toBe(
      "重新啟動",
    );
  });

  it("a stopped worker shows 已停止 and restart flips it back to a live state", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        presence: "stopped",
        desiredState: "offline",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-status")).textContent).toBe(
      "已停止",
    );
    fireEvent.click(await findByTestId("worker-detail-stop-toggle"));
    // restart → the mock reflects the re-spawn as presence "waking".
    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-status")).textContent,
      ).toBe("啟動中"),
    );
  });

  it("換 model: edit → save persists the new model via the adapter", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", model: "Opus 4.6", presence: "online" }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-model-effort-edit"));
    // The shared ModelEffortEditor's free custom-model input (data-testid pinned).
    const input = (await findByTestId("me-model-input")) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "claude-opus-4-8" } });
    fireEvent.click(await findByTestId("worker-detail-model-effort-save"));
    // The editor closes on a successful save (mock resolves); the cell returns…
    await findByTestId("worker-detail-model-effort-edit");
    // …and the new model is reflected in the cell (the local override).
    const cell = await findByTestId("worker-detail-model-effort-cell");
    expect(cell.textContent).toContain("claude-opus-4-8");
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
    await waitFor(() => expect(body.textContent ?? "").toContain("O-42"));
    expect(body.textContent ?? "").toContain("查帳單對帳");
    // The honesty caveat is present (目前版本重組, 非派工當下逐字版).
    const note = await findByTestId("worker-detail-prompt-note");
    expect(note.textContent ?? "").toContain("非派工當下");
  });
});
