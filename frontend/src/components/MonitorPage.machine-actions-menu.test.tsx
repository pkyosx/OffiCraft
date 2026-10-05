// The machine's name is the trigger of the row's operations menu: 詳情, 改名稱,
// then install / uninstall / delete with their enable rules. There is no pencil
// beside the name and no 操作 column.
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView, MonMachineView } from "../types";

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);
const patchMachine = vi.fn(async (_id: string, _body: { displayName: string }) => ({}));
const getMonitoring = vi.fn(async () => ({
  accounts: [],
  sessions: [],
  machines: [] as MonMachineView[],
}));

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () => getMonitoring(),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    patchMachine: (id: string, body: { displayName: string }) => patchMachine(id, body),
    bootstrapOnServer: () => Promise.resolve({ ok: true, exitCode: 0, log: "" }),
    getMachineBootCommand: () => Promise.resolve("curl … | sh"),
    getBackupHealth: () =>
      Promise.resolve({
        status: "healthy",
        code: "",
        detail: "",
        newestBackupTs: 1785600000,
        newestBackupAgeSecs: 3600,
        staleAfterSecs: 43200,
        sinceTs: null,
        checkedTs: 1785603600,
      }),
    subscribeEvents: () => () => {},
  },
}));

const base = {
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
};

function machine(over: Partial<MachineView>): MachineView {
  return {
    machineId: "m-alpha",
    displayName: "Alpha",
    online: false,
    isSelf: false,
    ...base,
    ...over,
  };
}

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

const TRIGGER = { name: "機器操作（Alpha）" };
const itemsOf = () =>
  (screen.getAllByRole("menuitem") as HTMLButtonElement[]).map((i) => [
    i.textContent,
    i.getAttribute("aria-disabled") === "true",
    i.title,
  ]);

describe("the machine name's operations menu", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listMembers.mockResolvedValue([]);
    getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: [] });
  });

  it("under an online remote machine, the name opens 詳情, 改名稱, 重新安裝, 解除安裝 and 刪除, all enabled", async () => {
    listMachines.mockResolvedValue([machine({ online: true })]);
    renderMonitor();
    const trigger = await screen.findByRole("button", TRIGGER);
    expect(trigger.textContent, "the trigger shows the name").toBe("Alpha");
    expect(trigger.getAttribute("aria-haspopup")).toBe("menu");
    expect(screen.queryByRole("menu")).toBeNull();
    fireEvent.click(trigger);
    expect(itemsOf()).toEqual([
      ["詳情", false, ""],
      ["改名稱", false, ""],
      ["重新安裝", false, ""],
      ["解除安裝", false, ""],
      ["刪除", false, ""],
    ]);
  });

  it("under an offline machine gives 安裝 and a disabled 解除安裝 that says why", async () => {
    listMachines.mockResolvedValue([machine({ online: false })]);
    renderMonitor();
    fireEvent.click(await screen.findByRole("button", TRIGGER));
    expect(itemsOf()).toEqual([
      ["詳情", false, ""],
      ["改名稱", false, ""],
      ["安裝", false, ""],
      ["解除安裝", true, "機器離線，無法解除安裝"],
      ["刪除", false, ""],
    ]);
  });

  it("under the server-self row, 刪除 is disabled and 改名稱 is not", async () => {
    listMachines.mockResolvedValue([
      machine({ machineId: "m-server-self", displayName: "本機", online: true, isSelf: true }),
    ]);
    renderMonitor();
    fireEvent.click(await screen.findByRole("button", { name: "機器操作（本機）" }));
    expect(itemsOf()).toEqual([
      ["詳情", false, ""],
      ["改名稱", false, ""],
      ["重新安裝", false, ""],
      ["解除安裝", false, ""],
      ["刪除", true, ""],
    ]);
    fireEvent.click(screen.getByTestId("mon-delete-btn"));
    expect(screen.queryByTestId("mon-delete-confirm"), "a disabled 刪除 opens nothing").toBeNull();
  });

  it("has no pencil beside the name and no 操作 column", async () => {
    listMachines.mockResolvedValue([machine({ online: true })]);
    renderMonitor();
    await screen.findByRole("button", TRIGGER);
    expect(screen.queryByRole("button", { name: "機器改名" })).toBeNull();
    expect(document.querySelector(".mon-table--machines .inline-edit__iconbtn")).toBeNull();
    const heads = Array.from(document.querySelectorAll(".mon-table--machines thead th")).map((th) => th.textContent);
    expect(heads).not.toContain("操作");
    expect(screen.queryByTestId("mon-actions-menu")).toBeNull();
  });

  it("under 改名稱, the name becomes the rename field; Enter saves it and focus returns to the name", async () => {
    listMachines.mockResolvedValue([machine({ online: true })]);
    renderMonitor();
    fireEvent.click(await screen.findByRole("button", TRIGGER));
    fireEvent.click(screen.getByTestId("mon-rename-btn"));
    const field = screen.getByRole("textbox", { name: "機器改名" }) as HTMLInputElement;
    expect(field.value).toBe("Alpha");
    expect(document.activeElement).toBe(field);
    expect(screen.queryByRole("button", TRIGGER), "the trigger gives way to the field").toBeNull();
    expect(screen.queryByRole("menu")).toBeNull();
    fireEvent.change(field, { target: { value: "  Beta  " } });
    fireEvent.keyDown(field, { key: "Enter" });
    expect(patchMachine).toHaveBeenCalledWith("m-alpha", { displayName: "Beta" });
    expect(screen.queryByRole("textbox", { name: "機器改名" })).toBeNull();
    await waitFor(() => expect(listMachines.mock.calls.length).toBeGreaterThan(1));
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", TRIGGER)));
  });

  it("under 改名稱, ✓ saves and ✗ or Esc cancel without a request, each giving focus back to the name", async () => {
    listMachines.mockResolvedValue([machine({ online: true })]);
    renderMonitor();
    const open = async () => {
      fireEvent.click(await screen.findByRole("button", TRIGGER));
      fireEvent.click(screen.getByTestId("mon-rename-btn"));
      return screen.getByRole("textbox", { name: "機器改名" }) as HTMLInputElement;
    };
    let field = await open();
    fireEvent.change(field, { target: { value: "Gamma" } });
    fireEvent.keyDown(field, { key: "Escape" });
    expect(screen.queryByRole("textbox", { name: "機器改名" })).toBeNull();
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", TRIGGER)));

    field = await open();
    expect(field.value, "a new edit starts from the saved name").toBe("Alpha");
    fireEvent.change(field, { target: { value: "Gamma" } });
    fireEvent.mouseDown(screen.getByRole("button", { name: "取消" }));
    expect(screen.queryByRole("textbox", { name: "機器改名" })).toBeNull();
    expect(patchMachine).not.toHaveBeenCalled();
    await waitFor(() => expect(document.activeElement, "✗ gives focus back to the name").toBe(screen.getByRole("button", TRIGGER)));

    field = await open();
    fireEvent.change(field, { target: { value: "Delta" } });
    fireEvent.mouseDown(screen.getByRole("button", { name: "套用" }));
    expect(patchMachine).toHaveBeenCalledWith("m-alpha", { displayName: "Delta" });
    expect(screen.queryByRole("textbox", { name: "機器改名" })).toBeNull();
    await waitFor(() => expect(document.activeElement, "✓ gives focus back to the name").toBe(screen.getByRole("button", TRIGGER)));
  });

  it("under a chosen item that opens a dialog, focus is back on the name, and returns there once the dialog is closed with a click", async () => {
    listMachines.mockResolvedValue([machine({ online: true })]);
    renderMonitor();
    const trigger = await screen.findByRole("button", TRIGGER);
    fireEvent.click(trigger);
    fireEvent.click(screen.getByTestId("mon-install-btn"));
    const dialog = await screen.findByTestId("mon-install-dialog");
    expect(document.activeElement).toBe(trigger);
    // A click on the dialog's close button moves focus there before the dialog
    // goes away, as a real browser does.
    const closeBtn = dialog.querySelector("button.mon-cmd__close") as HTMLButtonElement;
    closeBtn.focus();
    fireEvent.click(closeBtn);
    await waitFor(() => expect(screen.queryByTestId("mon-install-dialog")).toBeNull());
    await waitFor(() => expect(document.activeElement).toBe(trigger));
  });

  it("under focus moved to another control while the dialog is open, closing the dialog leaves it there", async () => {
    listMachines.mockResolvedValue([machine({ online: true })]);
    renderMonitor();
    const trigger = await screen.findByRole("button", TRIGGER);
    fireEvent.click(trigger);
    fireEvent.click(screen.getByTestId("mon-install-btn"));
    const dialog = await screen.findByTestId("mon-install-dialog");
    const elsewhere = document.getElementById("mon-onboard-entry") as HTMLButtonElement;
    elsewhere.focus();
    fireEvent.click(dialog.querySelector("button.mon-cmd__close") as HTMLButtonElement);
    await waitFor(() => expect(screen.queryByTestId("mon-install-dialog")).toBeNull());
    await new Promise((r) => setTimeout(r, 20));
    expect(document.activeElement).toBe(elsewhere);
  });

  it("under arrow keys, ArrowDown on the name opens the menu, and a disabled item is reached like any other", async () => {
    listMachines.mockResolvedValue([machine({ online: false })]);
    renderMonitor();
    fireEvent.keyDown(await screen.findByRole("button", TRIGGER), { key: "ArrowDown" });
    const menu = screen.getByRole("menu");
    await waitFor(() => expect(document.activeElement).toBe(screen.getByTestId("mon-detail-btn")));
    fireEvent.keyDown(menu, { key: "ArrowUp" });
    expect(document.activeElement, "wraps to the last").toBe(screen.getByTestId("mon-delete-btn"));
    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement, "wraps to the first").toBe(screen.getByTestId("mon-detail-btn"));
    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByTestId("mon-rename-btn"));
    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByTestId("mon-install-btn"));
    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByTestId("mon-uninstall-btn"));
    expect(screen.getByTestId("mon-uninstall-btn").getAttribute("aria-disabled")).toBe("true");
    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByTestId("mon-delete-btn"));
  });

  it("under a disabled item, choosing it does nothing and the menu stays open", async () => {
    listMachines.mockResolvedValue([machine({ online: false })]);
    renderMonitor();
    fireEvent.click(await screen.findByRole("button", TRIGGER));
    fireEvent.click(screen.getByTestId("mon-uninstall-btn"));
    expect(screen.getByRole("menu")).toBeTruthy();
    expect(screen.queryByTestId("mon-uninstall-confirm")).toBeNull();
    expect(screen.queryByTestId("mon-uninstall-warn")).toBeNull();
  });

  it("under Esc closes the menu and focus goes back to the name", async () => {
    listMachines.mockResolvedValue([machine({ online: true })]);
    renderMonitor();
    const trigger = await screen.findByRole("button", TRIGGER);
    fireEvent.click(trigger);
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it("under a click outside, the menu closes", async () => {
    listMachines.mockResolvedValue([machine({ online: true })]);
    renderMonitor();
    fireEvent.click(await screen.findByRole("button", TRIGGER));
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole("menu")).toBeNull();
  });
});

// The name's last character and the chevron share an unbreakable element, so a
// wrapped name never leaves the chevron alone on a line (the layout itself is
// measured in visual-guards/monitor-machines-layout.ct.spec.tsx).
describe("the machine name's last character and the chevron", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listMembers.mockResolvedValue([]);
    getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: [] });
  });

  async function nameParts(displayName: string) {
    listMachines.mockResolvedValue([machine({ online: true, displayName })]);
    const view = renderMonitor();
    const trigger = await screen.findByRole("button", { name: `機器操作（${displayName}）` });
    const name = trigger.querySelector(".mon-table__strong")!;
    const tail = name.querySelector(".mon-machine-name__tail")!;
    const chevron = tail.querySelector(".runtime-menu__chevron")!;
    const parts = {
      text: name.textContent,
      tail: tail.textContent,
      chevronHidden: chevron.getAttribute("aria-hidden"),
      chevronText: chevron.textContent,
      chevronLast: tail.lastElementChild === chevron,
      triggerText: trigger.textContent,
    };
    view.unmount();
    return parts;
  }

  it("the button's accessible name is the whole machine name, in one piece, with no chevron text", async () => {
    listMachines.mockResolvedValue([machine({ online: true, displayName: "Seth 的 Mac Studio（辦公室三樓靠窗）" })]);
    renderMonitor();
    const trigger = await screen.findByRole("button", { name: "機器操作（Seth 的 Mac Studio（辦公室三樓靠窗））" });
    expect(trigger.getAttribute("aria-label")).toBe("機器操作（Seth 的 Mac Studio（辦公室三樓靠窗））");
    expect(trigger.textContent, "the visible text is the name alone").toBe("Seth 的 Mac Studio（辦公室三樓靠窗）");
  });

  it.each([
    ["a word", "Alpha", "a"],
    ["a full-width ） at the end", "Seth 的 Mac Studio（辦公室三樓靠窗）", "）"],
    ["one character", "A", "A"],
    ["one Han character", "機", "機"],
    ["an emoji made of several code points", "build box 👩‍💻", "👩‍💻"],
    ["a flag", "Taipei 🇹🇼", "🇹🇼"],
    ["a letter with a combining accent", "Café", "é"],
    ["a trailing space", "Alpha ", "a "],
  ])("with %s, the chevron's run holds exactly the last character", async (_, displayName, tail) => {
    expect(await nameParts(displayName)).toEqual({
      text: displayName,
      tail,
      chevronHidden: "true",
      chevronText: "",
      chevronLast: true,
      triggerText: displayName,
    });
  });

  it("an empty name still renders the chevron, alone in its run", async () => {
    // An empty name gives an empty accessible name; reach the trigger by test id.
    listMachines.mockResolvedValue([machine({ online: true, displayName: "" })]);
    renderMonitor();
    const trigger = await screen.findByTestId("mon-machine-menu");
    const tail = trigger.querySelector(".mon-table__strong .mon-machine-name__tail")!;
    expect(tail.textContent).toBe("");
    expect(tail.querySelector(".runtime-menu__chevron")).not.toBeNull();
  });

  it("without Intl.Segmenter the last code point is split off, a surrogate pair whole", async () => {
    vi.stubGlobal("Intl", { ...Intl, Segmenter: undefined });
    try {
      expect((await nameParts("build box 👍")).tail).toBe("👍");
      expect((await nameParts("Alpha")).tail).toBe("a");
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
