// MemberDetailPanel · presence-gated runtime identity (T-2860).
//
// 機器 + Claude Account are RUNTIME facts: offline/stopped read a bare dash,
// never a desired_machine residual nor a stale/banked monitoring-session value
// that leaked through joinSessionRuntime. Online/waking show both; stopping
// shows the machine only (owner ruling rc-5a126c0e1b28).

import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
    listWebhooks: () => Promise.resolve([]),
    listScheduledMessages: () => Promise.resolve([]),
    createWebhook: () =>
      Promise.resolve({ endpointId: "", purpose: "", status: "enabled", createdTs: 0, token: "" }),
    updateWebhook: () =>
      Promise.resolve({ endpointId: "", purpose: "", status: "enabled", createdTs: 0, token: "" }),
    deleteWebhook: () => Promise.resolve(),
    subscribeEvents: () => () => {},
  },
}));

// A member carrying runtime identity (as joinSessionRuntime would have set it):
// machine + account are populated regardless of presence — the panel is what
// decides whether to surface them.
function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    name: "Mira",
    role: "assistant",
    status: "offline",
    lifecycle: "offline",
    model: "opus",
    effort: "medium",
    kind: "staff",
    desiredMachineId: "seth-m5",
    machine: "seth-m5",
    account: "eva-claude",
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    terminalAttachCommand: "tmux -L officraft attach -t member-mira",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  };
}

const dash = zh.mp.dash;

function renderPanel(member: Member) {
  return render(
    <I18nProvider>
      <MemberDetailPanel member={member} onBack={() => {}} />
    </I18nProvider>
  );
}

// The 機器 / Claude Account cell — the mp-field carrying both stacked values.
function runtimeIdentityCell(container: HTMLElement): HTMLElement {
  const label = within(container).getByText(zh.mp.claudeAccount);
  return label.closest(".mp-field") as HTMLElement;
}

function machineAndAccount(cell: HTMLElement): (string | null)[] {
  return [
    within(cell).getByTestId("mp-machine").textContent,
    within(cell).getByTestId("mp-account").textContent,
  ];
}

describe("MemberDetailPanel · presence-gated machine + account", () => {
  it("not awakened (offline) shows dash for both machine and Claude Account", async () => {
    const { container } = renderPanel(
      mkMember({ status: "offline", lifecycle: "offline" })
    );
    const cell = await waitFor(() => runtimeIdentityCell(container));

    // No stale residual leaks: neither the desired/observed machine id nor the
    // banked session account is rendered.
    expect(within(cell).queryByText("seth-m5")).toBeNull();
    expect(within(cell).queryByText("eva-claude")).toBeNull();
    // Both values read the honest dash.
    expect(machineAndAccount(cell)).toEqual([dash, dash]);
  });

  it("stopped (post-run) still shows dash — banked telemetry must not linger", async () => {
    const { container } = renderPanel(
      mkMember({ status: "offline", lifecycle: "stopped" })
    );
    const cell = await waitFor(() => runtimeIdentityCell(container));

    expect(within(cell).queryByText("seth-m5")).toBeNull();
    expect(within(cell).queryByText("eva-claude")).toBeNull();
    expect(machineAndAccount(cell)).toEqual([dash, dash]);
  });

  it("under stopping, the machine still shows while the Claude Account reads a dash", async () => {
    const { container } = renderPanel(
      mkMember({ status: "online", lifecycle: "stopping" })
    );
    const cell = await waitFor(() => runtimeIdentityCell(container));

    expect(within(cell).getByText("seth-m5")).toBeTruthy();
    expect(within(cell).queryByText("eva-claude")).toBeNull();
    expect(machineAndAccount(cell)).toEqual(["seth-m5", dash]);
  });

  it("under runtime login warnings, the presence line carries one exclamation whose hover hint lists each pair on its own line", async () => {
    const { container } = renderPanel(
      mkMember({
        status: "online",
        lifecycle: "online",
        runtimeLoginWarnings: [
          { machineId: "seth-m5", machineName: "seth-m5", runtime: "claude", pending: false },
          { machineId: "m-b", machineName: "Studio B", runtime: "codex", pending: true },
        ],
      })
    );
    const marks = await waitFor(() => {
      const found = within(container).getAllByTestId("runtime-login-warning");
      expect(found).toHaveLength(1);
      return found;
    });
    expect(marks[0].hasAttribute("title")).toBe(false);
    expect(marks[0].closest(".presence-badge")).not.toBeNull();
    fireEvent.mouseEnter(marks[0]);
    expect(
      Array.from(screen.getByRole("tooltip").children).map((line) => line.textContent),
    ).toEqual([
      "seth-m5 未登入 Claude",
      "Studio B 未登入 Codex",
      "可到「監控」頁的機器資訊，在 Claude 欄按版本號 →「登入」",
      "可到「監控」頁的機器資訊，在 Codex 欄按版本號 →「登入」",
    ]);
  });

  it("under only a model-call warning, the presence line carries the exclamation with that reason", async () => {
    const { container } = renderPanel(
      mkMember({
        status: "online",
        lifecycle: "online",
        runtimeLoginWarnings: [],
        modelCallWarnings: [
          {
            runtime: "claude",
            kind: "other",
            code: "invalid_request",
            resetsAt: null,
            sinceTs: 1_790_000_000,
            accountWide: false,
          },
        ],
      })
    );
    const mark = await waitFor(() => within(container).getByTestId("runtime-login-warning"));
    expect(mark.closest(".presence-badge")).not.toBeNull();
    fireEvent.mouseEnter(mark);
    expect(screen.getByRole("tooltip").textContent).toBe("Claude 模型呼叫失敗（invalid_request）");
  });

  it("under a reported success 3 minutes ago, the last-model-call row reads 3m 前, and with none reported it reads the dash", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date(1_790_000_000 * 1000));
    try {
      const recent = renderPanel(
        mkMember({ status: "online", lifecycle: "online", modelCallLastSuccessTs: 1_790_000_000 - 180 })
      );
      const row = await waitFor(() => within(recent.container).getByTestId("mp-model-call-last-success"));
      expect(row.textContent).toBe("3m 前");
      expect(row.previousElementSibling?.textContent).toBe("最後一次模型呼叫成功");
      recent.unmount();

      const none = renderPanel(
        mkMember({ status: "online", lifecycle: "online", modelCallLastSuccessTs: null })
      );
      const empty = await waitFor(() => within(none.container).getByTestId("mp-model-call-last-success"));
      expect(empty.textContent).toBe(dash);
    } finally {
      vi.useRealTimers();
    }
  });

  it("under neither a runtime login warning nor a model call warning, the presence line has no exclamation", async () => {
    const { container } = renderPanel(
      mkMember({ status: "online", lifecycle: "online", runtimeLoginWarnings: [], modelCallWarnings: [] })
    );
    await waitFor(() => runtimeIdentityCell(container));
    expect(within(container).queryByTestId("runtime-login-warning")).toBeNull();
  });

  it.each([
    ["online", "online", false, "重新聚焦"],
    ["stopping", "online", true, "僅線上可重新聚焦"],
    ["stopped", "offline", true, "僅線上可重新聚焦"],
    ["waking", "waking", true, "僅線上可重新聚焦"],
  ] as const)(
    "refocus on a %s member (status %s): disabled=%s, titled %s",
    (lifecycle, status, disabled, title) => {
      const { getByTestId } = render(
        <I18nProvider>
          <MemberDetailPanel
            member={mkMember({ lifecycle, status })}
            onBack={() => {}}
            onRefocus={vi.fn()}
          />
        </I18nProvider>,
      );
      const refocus = getByTestId("mp-refocus") as HTMLButtonElement;
      expect(refocus.disabled).toBe(disabled);
      expect(refocus.getAttribute("title")).toBe(title);
    },
  );

  const treeRows = (card: HTMLElement) =>
    Array.from(card.querySelectorAll<HTMLElement>(".mp-activity__row")).map((row) => {
      let depth = 0;
      for (let el = row.parentElement; el && el !== card; el = el.parentElement) {
        if (el.classList.contains("mp-activity__children")) depth++;
      }
      return [
        depth,
        row.querySelector(".activity-icon__glyph")!.className.replace("activity-icon__glyph activity-icon__glyph--", ""),
        row.querySelector(".mp-activity__label")!.textContent,
        row.querySelector(".mp-activity__aside")?.textContent ?? "",
      ];
    });

  it("an online member with activity gets a 目前狀態 box first in the 運行狀況 card, before its context and cost boxes: the state as the name-front glyph, no summary, then the tree nested by parent with waits under the main agent", () => {
    vi.useFakeTimers({ toFake: ["Date"], now: new Date(2026, 9, 22, 11, 0, 0) });
    const now = Date.now() / 1000;
    const nextRun = new Date(2026, 9, 22, 12, 15).getTime() / 1000;
    try {
      const { getByTestId, container } = renderPanel(
        mkMember({
          status: "online",
          lifecycle: "online",
          activity: {
            state: "busy",
            busySince: now - 600,
            subagents: [
              { id: "a1", parentId: null, name: "Explore", description: "盤點 Backlog 票", startedTs: now - 120 },
              { id: "a2", parentId: "a1", name: "general-purpose", description: "讀取錯誤紀錄", startedTs: now - 40 },
              { id: "p", parentId: "gone", name: "Plan", description: "", startedTs: now - 300 },
            ],
            waits: [
              { kind: "background_command", description: "每 30 秒檢查 CI", nextTs: null, recurring: false },
              { kind: "schedule", description: "檢查 PR 審查結果", nextTs: nextRun, recurring: true },
            ],
          },
        }),
      );
      const card = getByTestId("mp-activity");
      expect(card.className).toBe("mp-cell mp-activity");
      expect(card.parentElement!.className).toBe("mp-runtime__cells");
      expect(card.previousElementSibling).toBeNull();
      expect(card.nextElementSibling!.contains(getByTestId("mp-context"))).toBe(true);
      expect(card.querySelector(".mp-activity__head")!.textContent).toBe("目前狀態");
      const stateIcon = card.querySelector(".mp-activity__state-icon")!;
      expect(stateIcon.getAttribute("role")).toBe("img");
      expect(stateIcon.getAttribute("aria-label")).toBe("工作中");
      expect(stateIcon.querySelector(".activity-icon__glyph--working .sparkle-icon__star")).not.toBeNull();
      expect(card.querySelector(".mp-activity__state")).toBeNull();
      expect(card.querySelector(".mp-activity__summary")).toBeNull();
      expect(treeRows(card)).toEqual([
        [0, "working", "主 agent", "已 10 分鐘"],
        [1, "working", "Plan", "5 分鐘"],
        [1, "working", "盤點 Backlog 票Explore", "2 分鐘"],
        [2, "working", "讀取錯誤紀錄general-purpose", "40 秒"],
        [1, "waiting", "每 30 秒檢查 CI", "背景指令"],
        [1, "waiting", "檢查 PR 審查結果", "排程 · 下次 12:15 · 重複"],
      ]);
      expect(container.querySelectorAll(".mp-activity")).toHaveLength(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("with only sub-agents running the main agent reads 已回覆完畢, and a schedule with no next run reads just 排程", () => {
    const now = Date.now() / 1000;
    const { getByTestId } = renderPanel(
      mkMember({
        status: "online",
        lifecycle: "online",
        activity: {
          state: "busy",
          busySince: null,
          subagents: [{ id: "a1", parentId: null, name: "Explore", description: "", startedTs: now - 5 }],
          waits: [{ kind: "schedule", description: "", nextTs: null, recurring: false }],
        },
      }),
    );
    expect(treeRows(getByTestId("mp-activity"))).toEqual([
      [0, "waiting", "主 agent", "已回覆完畢"],
      [1, "working", "Explore", "5 秒"],
      [1, "waiting", "排程", ""],
    ]);
  });

  it("an idle member with nothing waiting gets only the 目前狀態 header with the 閒置 glyph", () => {
    const { getByTestId } = renderPanel(
      mkMember({
        status: "online",
        lifecycle: "online",
        activity: { state: "idle", busySince: null, subagents: [], waits: [] },
      }),
    );
    const card = getByTestId("mp-activity");
    expect(Array.from(card.children).map((c) => c.className)).toEqual(["mp-activity__head"]);
    expect(card.querySelector(".mp-activity__state-icon")!.getAttribute("aria-label")).toBe("閒置");
    expect(card.querySelector(".mp-activity__state-icon .activity-icon__glyph--idle")).not.toBeNull();
  });

  it("without activity the 運行狀況 card carries no activity section, the identity card leads the name with a still unknown mark hinting 狀態未知, and the 運行狀況 card still shows its context and cost cells", () => {
    const { queryByTestId, getByTestId } = renderPanel(
      mkMember({ status: "online", lifecycle: "online", activity: null }),
    );
    expect(queryByTestId("mp-activity")).toBeNull();
    expect(queryByTestId("mp-identity-activity")).toBeNull();
    expect(queryByTestId("mp-identity-activity-offline")).toBeNull();
    const unknown = getByTestId("mp-identity-activity-unknown");
    expect(unknown.className).toBe("activity-icon activity-icon--unknown");
    expect(unknown.parentElement!.className).toBe("activity-name");
    expect(unknown.hasAttribute("tabindex")).toBe(false);
    expect(unknown.getAttribute("aria-hidden")).toBe("true");
    expect(unknown.querySelector(".activity-icon__glyph--unknown svg")).not.toBeNull();
    fireEvent.mouseEnter(unknown);
    expect(screen.getByRole("tooltip").textContent).toBe("狀態未知");
    fireEvent.click(unknown);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(getByTestId("mp-context").closest(".mp-runtime")).not.toBeNull();
  });

  it("an offline member's identity card leads the name with a still moon that hints 離線 and is no button", () => {
    const { getByTestId } = renderPanel(mkMember({ status: "offline", lifecycle: "offline", activity: null }));
    const moon = getByTestId("mp-identity-activity-offline");
    expect(moon.className).toBe("activity-icon activity-icon--offline");
    expect(moon.parentElement!.className).toBe("activity-name");
    expect(moon.hasAttribute("tabindex")).toBe(false);
    expect(moon.querySelector(".activity-icon__glyph--offline svg")).not.toBeNull();
    fireEvent.mouseEnter(moon);
    expect(screen.getByRole("tooltip").textContent).toBe("離線");
  });

  it("the identity card carries the presence dot on the avatar corner and the status icon right before the name, which pins its hint instead of opening a dialog", () => {
    const { getByTestId, container } = renderPanel(
      mkMember({
        status: "online",
        lifecycle: "online",
        activity: { state: "waiting", busySince: null, subagents: [], waits: [] },
      }),
    );
    const dot = getByTestId("mp-identity-presence");
    expect(dot.closest(".presence-avatar")!.querySelector(".avatar")).not.toBeNull();
    expect(dot.getAttribute("aria-label")).toBe("線上");
    expect(container.querySelector(".mp-identity__status .lifecycle-dot")).toBeNull();
    const icon = getByTestId("mp-identity-activity");
    expect(icon.parentElement!.className).toBe("activity-name");
    expect(icon.parentElement!.parentElement!.classList.contains("mp-identity__line")).toBe(true);
    expect(icon.nextElementSibling!.textContent).toBe("Mira");
    expect(icon.getAttribute("aria-label")).toBe("等待中");
    expect(icon.getAttribute("role")).toBe("img");
    fireEvent.click(icon);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("tooltip").className).toContain("instant-hint--pinned");
  });

  it("awakened (online) shows the real running machine and its bound account", async () => {
    const { container } = renderPanel(
      mkMember({ status: "online", lifecycle: "online" })
    );
    const cell = await waitFor(() => runtimeIdentityCell(container));

    expect(within(cell).getByText("seth-m5")).toBeTruthy();
    expect(within(cell).getByText("eva-claude")).toBeTruthy();
  });

  it("labels a Codex runtime account as Codex, and puts runtime before model", async () => {
    const { container } = renderPanel(
      mkMember({
        status: "online",
        lifecycle: "online",
        runtime: "codex",
        account: "codex-account",
      })
    );

    await waitFor(() => within(container).getByText(zh.mp.codexAccount));
    expect(within(container).queryByText(zh.mp.claudeAccount)).toBeNull();

    const modelEffortCell = container.querySelector(
      '[data-testid="mp-model-effort-cell"]'
    )?.textContent ?? "";
    expect(modelEffortCell.indexOf(zh.mp.agentRuntime)).toBeLessThan(
      modelEffortCell.indexOf(zh.mp.model)
    );
  });

  it("shows the reported running model, never the configured launch model", async () => {
    const { container, rerender } = renderPanel(
      mkMember({
        status: "online",
        lifecycle: "online",
        model: "configured-launch-model",
        actualModel: "reported-runtime-model",
      }),
    );
    const readModel = () =>
      container.querySelector('[data-testid="mp-model-effort-cell"]')?.textContent ?? "";

    await waitFor(() => expect(readModel()).toContain("reported-runtime-model"));
    // The READOUT is the reported model. The configured one is not absent from
    // the cell any more (T-7f28 puts it in the 「→ 要換成」 pending line, since
    // the two differing IS the thing the owner needs to see) — but it must
    // never be what the value row itself says.
    expect(
      container.querySelector('[data-testid="mp-model-value"]')?.textContent ??
        "",
    ).not.toContain("configured-launch-model");
    expect(
      container.querySelector('[data-testid="mp-model-pending"]')?.textContent ??
        "",
    ).toContain("configured-launch-model");

    rerender(
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember({
            model: "configured-launch-model",
            actualModel: "reported-runtime-model",
          })}
          onBack={() => {}}
        />
      </I18nProvider>,
    );
    await waitFor(() =>
      expect(readModel()).not.toContain("reported-runtime-model"),
    );
  });

  it("tags the model row as a REPORTED value, and says so in the settings dialog too", async () => {
    // 🔴 One heading, three rows, two meanings: runtime and effort are what the
    // owner CONFIGURED, the model is what the agent REPORTED — and the dialog
    // edits the configured one. Without both labels the owner sees two different
    // values under one name and reasonably concludes the system is wrong. The
    // card tag alone fixes half of it, which is why the dialog note is asserted
    // in the same test: neither half is optional.
    const { container, getByTestId } = renderPanel(
      mkMember({
        status: "online",
        lifecycle: "online",
        model: "configured-launch-model",
        actualModel: "reported-runtime-model",
      }),
    );
    // 🔴 Scoped by POSITION, not by "somewhere in this cell": the cell holds
    // runtime, model and effort, so a whole-cell assertion would still pass if
    // the tag moved onto the 思考強度 row — which is exactly the confusion the tag
    // exists to remove.
    const cellText = async () => {
      await waitFor(() =>
        expect(
          container.querySelector('[data-testid="mp-model-effort-cell"]')?.textContent ?? "",
        ).toContain(zh.mp.modelReportedTag),
      );
      return (
        container.querySelector('[data-testid="mp-model-effort-cell"]')?.textContent ?? ""
      );
    };
    const text = await cellText();
    expect(text.indexOf("reported-runtime-model")).toBeGreaterThan(-1);
    expect(text.indexOf(zh.mp.modelReportedTag)).toBeGreaterThan(
      text.indexOf("reported-runtime-model"),
    );
    expect(text.indexOf(zh.mp.modelReportedTag)).toBeLessThan(
      text.indexOf(zh.mp.effort),
    );

    await waitFor(() =>
      expect((getByTestId("mp-change") as HTMLButtonElement).disabled).toBe(false),
    );
    fireEvent.click(getByTestId("mp-change"));
    expect(getByTestId("mp-settings-intent-note").textContent).toBe(
      "按下後，它會先把手上的事收尾，再用新設定重新開起來。" +
        " 上方顯示的是目前實際使用的模型，可能和這裡的設定不同。",
    );
  });

  it("leaves the model cell BLANK when nobody has reported one (never the configured value)", async () => {
    // 🔴 Red line (c) of the ticket: the reported-model field must not be
    // backfilled from the configured one, or "no report yet ⇒ blank" becomes
    // unreachable — and the panel would be back to showing an intention as if it
    // were an observation. The test above uses a member where BOTH fields are
    // set, so it cannot see a backfill: mutate the cell to
    // `member.actualModel || member.model` and it stays green. This one is the
    // guard for that mutation.
    const { container } = renderPanel(
      mkMember({
        status: "online",
        lifecycle: "online",
        model: "configured-launch-model",
        actualModel: "",
      }),
    );
    const readModel = () =>
      container.querySelector('[data-testid="mp-model-effort-cell"]')?.textContent ?? "";
    await waitFor(() => expect(readModel()).not.toBe(""));
    expect(readModel()).not.toContain("configured-launch-model");
    // …and nothing may be tagged as a reported value when there is no reported
    // value: 「— 最近一次開機回報」 would assert a report that never happened.
    expect(readModel()).not.toContain(zh.mp.modelReportedTag);
  });

});
