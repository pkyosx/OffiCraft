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
    ).toEqual(["seth-m5 未登入 Claude", "Studio B 未登入 Codex"]);
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
