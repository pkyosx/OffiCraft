// 新增機器 / 上線 — the INLINE onboard row (owner-aligned pattern, mirrors
// 角色誌's 新增角色定義): the add entry button grows one editable row with a
// single machine-name field — Enter/確認 creates (onboardMachine with the TYPED
// name), Esc/取消 collapses the row and creates nothing.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView, OnboardResultView } from "../types";

const m = zh.monitor.machine;

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);
const onboardMachine = vi.fn(
  async (_name: string): Promise<OnboardResultView> => ({
    memberId: "m-new",
    machineId: "m-new",
    token: "",
    expiresIn: 0,
    bootCommand: "curl … (never rendered)",
  })
);

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () =>
      Promise.resolve({ accounts: [], sessions: [], machines: [] }),
    onboardMachine: (name: string) => onboardMachine(name),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    subscribeEvents: () => () => {},
  },
}));

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

describe("MonitorPage inline machine onboarding", () => {
  beforeEach(() => {
    listMembers.mockResolvedValue([]);
    listMachines.mockResolvedValue([]);
    onboardMachine.mockClear();
  });

  it("grows an inline name row; Enter creates with the typed name", async () => {
    renderMonitor();
    const entry = await screen.findByText(`+ ${m.onboardEntry}`);
    expect(screen.queryByTestId("mon-onboard-row")).toBeNull();
    // Owner feedback (M2): the add entry is the ONLY frame — it renders
    // standalone below the machine table, never boxed inside the table card.
    expect(entry.closest("table")).toBeNull();
    // 修仙 batch 1: it wears the SHARED `.add-entry` silhouette (centered
    // low-key neutral row, no accent green) — one class for both 新增 buttons,
    // identical to 角色誌's 新增角色定義.
    expect((entry.closest("button") as HTMLButtonElement).className).toBe(
      "add-entry"
    );

    fireEvent.click(entry);
    const input = screen.getByTestId("mon-onboard-name");
    fireEvent.change(input, { target: { value: "Kyle-M9" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => expect(onboardMachine).toHaveBeenCalledWith("Kyle-M9"));
    // The row collapsed back to the add entry after the create.
    await waitFor(() =>
      expect(screen.queryByTestId("mon-onboard-row")).toBeNull()
    );
  });

  it("Esc collapses the row and onboards nothing; blank Enter is a no-op", async () => {
    renderMonitor();
    fireEvent.click(await screen.findByText(`+ ${m.onboardEntry}`));
    const input = screen.getByTestId("mon-onboard-name");

    // Blank Enter: nothing created, the row stays open for typing.
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onboardMachine).not.toHaveBeenCalled();
    expect(screen.getByTestId("mon-onboard-row")).toBeTruthy();

    fireEvent.change(input, { target: { value: "draft" } });
    fireEvent.keyDown(input, { key: "Escape" });
    expect(screen.queryByTestId("mon-onboard-row")).toBeNull();
    expect(onboardMachine).not.toHaveBeenCalled();
  });
});
