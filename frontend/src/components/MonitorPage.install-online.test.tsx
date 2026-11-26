// Install button on an ONLINE machine — Monitor §2 machine panel.
//
// The button used to be `disabled={(m.isSelf && bootstrapBusy) || m.online}`,
// which collapsed two actions of wildly different danger into one gate:
//   • server-self → a real in-place reinstall that overwrites the LIVE warden
//     (every member on that box drops; irreversible)
//   • any other machine → shows a command to copy; installs nothing
// so a perfectly healthy remote machine could not even show its install command.
//
// The gate is gone. The dangerous half is now guarded by a confirm dialog whose
// wording names the real consequence — and the assertion this file exists for is
// that CANCELLING that dialog fires NO request at all.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, render, screen, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView, MonMachineView } from "../types";
import { machineAction } from "./machineActions.testHelper";

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);
const getMonitoring = vi.fn(async () => ({
  accounts: [],
  sessions: [],
  machines: [] as MonMachineView[],
}));
const bootstrapOnServer = vi.fn(
  async (_id: string) => ({ ok: true, exitCode: 0, log: "" })
);
const getMachineBootCommand = vi.fn(async (_id: string) => ({
  command: "curl … | sh",
  expiresInSecs: 600,
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
    bootstrapOnServer: (id: string) => bootstrapOnServer(id),
    getMachineBootCommand: (id: string) => getMachineBootCommand(id),
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

const selfOnline: MachineView = {
  machineId: "m-server-self",
  displayName: "本機",
  online: true,
  isSelf: true,
  ...base,
};

const remoteOnline: MachineView = {
  machineId: "m-alpha",
  displayName: "Alpha",
  online: true,
  isSelf: false,
  ...base,
};

/** Which icon a dialog button shows, read off the svg itself (CheckIcon is a
 * single tick path, CopyIcon has a rect, RefreshIcon neither). */
function iconOf(button: Element): "check" | "copy" | "other" | "none" {
  const svg = button.querySelector("svg");
  if (!svg) return "none";
  if (svg.querySelector('path[d="M20 6 9 17l-5-5"]')) return "check";
  if (svg.querySelector("rect")) return "copy";
  return "other";
}

/** The always-mounted status line under the box that holds `inside`. */
function statusUnder(inside: Element): HTMLElement {
  const line = inside.closest(".mon-install__cmd-line")!;
  return line.querySelector('[role="status"]') as HTMLElement;
}

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

describe("MonitorPage install on an online machine", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listMembers.mockResolvedValue([]);
    getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: [] });
    bootstrapOnServer.mockResolvedValue({ ok: true, exitCode: 0, log: "" });
    getMachineBootCommand.mockResolvedValue({ command: "curl … | sh", expiresInSecs: 600 });
  });

  describe("server-self row, machine online (the destructive half)", () => {
    beforeEach(() => {
      listMachines.mockResolvedValue([selfOnline]);
    });

    it("is clickable and opens a confirm naming the real consequence", async () => {
      renderMonitor();
      const btn = await machineAction("mon-install-btn");
      expect(btn.disabled).toBe(false);

      fireEvent.click(btn);

      const dialog = await screen.findByTestId("mon-bootstrap-confirm");
      // Not a shared canned line: it must say what is actually lost.
      expect(dialog.textContent).toContain("覆蓋");
      expect(dialog.textContent).toContain("斷線");
      expect(dialog.textContent).toMatch(/不可逆|無法復原/);
      expect(dialog.textContent).toContain("本機");
      // Nothing has been asked of the server yet — the dialog is a question.
      expect(bootstrapOnServer).not.toHaveBeenCalled();
      // The classes the CT theme guard measures. jsdom cannot compute colour,
      // but it CAN pin that this dialog wears the already-themed containers —
      // which is what makes the guard's measurements about this dialog.
      expect(dialog.querySelector(".mon-confirm__title")).toBeTruthy();
      expect(dialog.querySelector(".mon-confirm__body")).toBeTruthy();
      // No colour of its own anywhere in the dialog (token layer only).
      for (const el of dialog.querySelectorAll<HTMLElement>("*")) {
        expect(el.style.color).toBe("");
        expect(el.style.backgroundColor).toBe("");
      }
    });

    // 🔴 THE assertion of this file.
    it("sends NO request when the confirm is cancelled", async () => {
      renderMonitor();
      fireEvent.click(await machineAction("mon-install-btn"));
      await screen.findByTestId("mon-bootstrap-confirm");

      fireEvent.click(screen.getByTestId("mon-bootstrap-cancel-btn"));

      expect(bootstrapOnServer).not.toHaveBeenCalled();
      expect(screen.queryByTestId("mon-bootstrap-confirm")).toBeNull();
    });

    it("runs the in-place install exactly once when confirmed", async () => {
      renderMonitor();
      fireEvent.click(await machineAction("mon-install-btn"));
      await screen.findByTestId("mon-bootstrap-confirm");

      fireEvent.click(screen.getByTestId("mon-bootstrap-confirm-btn"));

      expect(bootstrapOnServer).toHaveBeenCalledTimes(1);
      expect(bootstrapOnServer).toHaveBeenCalledWith("m-server-self");
      expect(screen.queryByTestId("mon-bootstrap-confirm")).toBeNull();
    });
  });

  describe("remote row, machine online (the harmless half)", () => {
    beforeEach(() => {
      listMachines.mockResolvedValue([remoteOnline]);
    });

    it("can be clicked and shows the install command dialog", async () => {
      renderMonitor();
      const btn = await machineAction("mon-install-btn");
      expect(btn.disabled).toBe(false);

      fireEvent.click(btn);

      const dialog = await screen.findByTestId("mon-install-dialog");
      expect(dialog.textContent).toContain("Alpha");
      await waitFor(() =>
        expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl … | sh")
      );
      expect(screen.queryByTestId("mon-bootstrap-confirm")).toBeNull();
      expect(bootstrapOnServer).not.toHaveBeenCalled();
      expect(getMachineBootCommand).toHaveBeenCalledTimes(1);
      expect(getMachineBootCommand).toHaveBeenCalledWith("m-alpha");
    });

    it("copies exactly the command shown in the dialog, without fetching another one", async () => {
      const writeText = vi.fn(async (_text: string) => {});
      Object.defineProperty(navigator, "clipboard", {
        value: { writeText },
        configurable: true,
      });
      renderMonitor();
      fireEvent.click(await machineAction("mon-install-btn"));
      await waitFor(() =>
        expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl … | sh")
      );

      fireEvent.click(screen.getByTestId("mon-copy-boot-cmd-btn"));

      await waitFor(() => expect(writeText).toHaveBeenCalledWith("curl … | sh"));
      expect(getMachineBootCommand).toHaveBeenCalledTimes(1);
      await screen.findByRole("button", { name: "已複製" });
      expect(screen.getByTestId("mon-copy-boot-cmd-btn").textContent).toBe("");
    });

    for (const which of ["install", "wsl"] as const) {
      it(`shows a check mark on the ${which} copy button after a copy, says 已複製 to a screen reader, and returns to the copy icon after 1.6s`, async () => {
        vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setTimeout", "clearTimeout"] });
        try {
          const writeText = vi.fn(async (_text: string) => {});
          Object.defineProperty(navigator, "clipboard", {
            value: { writeText },
            configurable: true,
          });
          renderMonitor();
          fireEvent.click(await machineAction("mon-install-btn"));
          await waitFor(() =>
            expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl … | sh")
          );
          fireEvent.click(screen.getByTestId("mon-install-tab-windows"));
          const button =
            which === "install"
              ? screen.getByTestId("mon-copy-boot-cmd-btn")
              : (screen
                  .getByTestId("mon-install-wsl")
                  .querySelector(".mon-install__cmd-line button") as HTMLElement);
          expect(iconOf(button)).toBe("copy");
          expect(statusUnder(button).textContent).toBe("");

          fireEvent.click(button);

          await waitFor(() => expect(iconOf(button)).toBe("check"));
          expect(button.getAttribute("aria-label")).toBe("已複製");
          expect(statusUnder(button).textContent).toBe("已複製");
          expect(statusUnder(button).querySelector(".mon-install__sr-text")).toBeTruthy();

          await act(async () => {
            vi.advanceTimersByTime(1600);
          });

          expect(iconOf(button)).toBe("copy");
          expect(button.getAttribute("aria-label")).toBe("複製");
          expect(statusUnder(button).textContent).toBe("");
        } finally {
          vi.useRealTimers();
        }
      });
    }

    it("shows every copy button as an icon named 複製, on every OS tab, the WSL step's included", async () => {
      renderMonitor();
      fireEvent.click(await machineAction("mon-install-btn"));
      await waitFor(() =>
        expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl … | sh")
      );
      for (const os of ["mac", "linux", "windows"]) {
        fireEvent.click(screen.getByTestId(`mon-install-tab-${os}`));
        const copies = screen.getAllByRole("button", { name: "複製" });
        expect(copies.length).toBe(os === "windows" ? 2 : 1);
        for (const b of copies) {
          expect(b.textContent).toBe("");
          expect(b.getAttribute("title")).toBe("複製");
          expect(b.querySelector("svg")).toBeTruthy();
        }
      }
    });

    it("puts the copy-failure text under the command box and keeps the button an icon", async () => {
      const writeText = vi.fn(async (_text: string) => {
        throw new Error("denied");
      });
      Object.defineProperty(navigator, "clipboard", {
        value: { writeText },
        configurable: true,
      });
      renderMonitor();
      fireEvent.click(await machineAction("mon-install-btn"));
      await waitFor(() =>
        expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl … | sh")
      );
      fireEvent.click(screen.getByTestId("mon-copy-boot-cmd-btn"));

      const msg = await screen.findByText("複製失敗，請直接選取上方的安裝指令");
      const line = screen.getByTestId("mon-boot-cmd").closest(".mon-install__cmd-line")!;
      expect(msg.closest('[role="status"]')).toBe(line.lastElementChild);
      expect(iconOf(screen.getByTestId("mon-copy-boot-cmd-btn"))).toBe("copy");
      expect(screen.getByTestId("mon-copy-boot-cmd-btn").textContent).toBe("");
      expect(screen.getByTestId("mon-copy-boot-cmd-btn").getAttribute("aria-label")).toBe("複製");

      fireEvent.click(screen.getByTestId("mon-install-tab-windows"));
      const wsl = screen.getByTestId("mon-install-wsl");
      fireEvent.click(wsl.querySelector(".mon-install__cmd-line button") as HTMLButtonElement);
      const wslMsg = await waitFor(() => {
        const el = wsl.querySelector(".mon-install__cmd-msg") as HTMLElement;
        expect(el.textContent).toBe("複製失敗");
        return el;
      });
      expect(wslMsg.getAttribute("role")).toBe("status");
      expect(wslMsg.previousElementSibling?.querySelector("code")?.textContent).toBe(
        "wsl --install -d Ubuntu"
      );
    });

    it("never shows a check mark and a copy failure together: a failing copy right after a successful one shows only the failure", async () => {
      let fail = false;
      const writeText = vi.fn(async (_text: string) => {
        if (fail) throw new Error("denied");
      });
      Object.defineProperty(navigator, "clipboard", {
        value: { writeText },
        configurable: true,
      });
      renderMonitor();
      fireEvent.click(await machineAction("mon-install-btn"));
      await waitFor(() =>
        expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl … | sh")
      );
      const button = screen.getByTestId("mon-copy-boot-cmd-btn");
      fireEvent.click(button);
      await waitFor(() => expect(iconOf(button)).toBe("check"));

      fail = true;
      fireEvent.click(button);

      await screen.findByText("複製失敗，請直接選取上方的安裝指令");
      expect(iconOf(button)).toBe("copy");
      expect(button.getAttribute("aria-label")).toBe("複製");
    });

    it("ignores a copy of the old command that finishes only after the command was regenerated", async () => {
      const start = Date.now();
      const now = vi.spyOn(Date, "now").mockReturnValue(start);
      try {
        let finishCopy!: () => void;
        const writeText = vi.fn(
          (_text: string) =>
            new Promise<void>((resolve) => {
              finishCopy = resolve;
            })
        );
        Object.defineProperty(navigator, "clipboard", {
          value: { writeText },
          configurable: true,
        });
        getMachineBootCommand
          .mockResolvedValueOnce({ command: "curl first | bash", expiresInSecs: 600 })
          .mockResolvedValueOnce({ command: "curl second | bash", expiresInSecs: 600 });
        renderMonitor();
        fireEvent.click(await machineAction("mon-install-btn"));
        await waitFor(() =>
          expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl first | bash")
        );
        const button = screen.getByTestId("mon-copy-boot-cmd-btn");
        // The clipboard write hangs (a permission prompt, a slow first write).
        fireEvent.click(button);
        expect(writeText).toHaveBeenCalledWith("curl first | bash");

        now.mockReturnValue(start + 600_000);
        fireEvent(document, new Event("visibilitychange"));
        await waitFor(() => expect(button.getAttribute("aria-label")).toBe("重新產生指令"));
        fireEvent.click(button);
        await waitFor(() =>
          expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl second | bash")
        );

        await act(async () => {
          finishCopy();
        });

        expect(iconOf(button)).toBe("copy");
        expect(button.getAttribute("aria-label")).toBe("複製");
        expect(statusUnder(button).textContent).toBe("");
      } finally {
        now.mockRestore();
      }
    });

    it("restarts the 1.6s check-mark clock on each new copy", async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setTimeout", "clearTimeout"] });
      try {
        const writeText = vi.fn(async (_text: string) => {});
        Object.defineProperty(navigator, "clipboard", {
          value: { writeText },
          configurable: true,
        });
        renderMonitor();
        fireEvent.click(await machineAction("mon-install-btn"));
        await waitFor(() =>
          expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl … | sh")
        );
        const button = screen.getByTestId("mon-copy-boot-cmd-btn");
        fireEvent.click(button);
        await waitFor(() => expect(iconOf(button)).toBe("check"));
        await act(async () => {
          vi.advanceTimersByTime(1200);
        });

        fireEvent.click(button);
        await waitFor(() => expect(writeText).toHaveBeenCalledTimes(2));
        await act(async () => {
          vi.advanceTimersByTime(1000);
        });
        // 2.2s after the first copy, 1.0s after the second: still checked.
        expect(iconOf(button)).toBe("check");

        await act(async () => {
          vi.advanceTimersByTime(600);
        });
        expect(iconOf(button)).toBe("copy");
      } finally {
        vi.useRealTimers();
      }
    });

    it("does not carry a check mark over to a regenerated command", async () => {
      const start = Date.now();
      const now = vi.spyOn(Date, "now").mockReturnValue(start);
      try {
        const writeText = vi.fn(async (_text: string) => {});
        Object.defineProperty(navigator, "clipboard", {
          value: { writeText },
          configurable: true,
        });
        getMachineBootCommand
          .mockResolvedValueOnce({ command: "curl first | bash", expiresInSecs: 600 })
          .mockResolvedValueOnce({ command: "curl second | bash", expiresInSecs: 600 });
        renderMonitor();
        fireEvent.click(await machineAction("mon-install-btn"));
        await waitFor(() =>
          expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl first | bash")
        );
        const button = screen.getByTestId("mon-copy-boot-cmd-btn");
        fireEvent.click(button);
        await waitFor(() => expect(iconOf(button)).toBe("check"));

        // Expires inside the 1.6s check-mark window; the user regenerates at once.
        now.mockReturnValue(start + 600_000);
        fireEvent(document, new Event("visibilitychange"));
        await waitFor(() => expect(button.getAttribute("aria-label")).toBe("重新產生指令"));
        fireEvent.click(button);
        await waitFor(() =>
          expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl second | bash")
        );

        expect(iconOf(button)).toBe("copy");
        expect(button.getAttribute("aria-label")).toBe("複製");
        expect(statusUnder(button).textContent).toBe("");
      } finally {
        now.mockRestore();
      }
    });

    it("tells where to paste the install command for the chosen operating system", async () => {
      const writeText = vi.fn(async (_text: string) => {});
      Object.defineProperty(navigator, "clipboard", {
        value: { writeText },
        configurable: true,
      });
      renderMonitor();
      fireEvent.click(await machineAction("mon-install-btn"));
      const steps = await screen.findByTestId("mon-install-steps");
      expect(screen.getByTestId("mon-install-paste-hint").textContent).toBe(
        "在「終端機」貼上並執行："
      );
      expect(steps.textContent).toContain("需求：tmux，以及已登入的 Claude Code 或 Codex。");

      fireEvent.click(screen.getByTestId("mon-install-tab-windows"));

      expect(screen.getByTestId("mon-install-tab-windows").getAttribute("aria-selected")).toBe("true");
      expect(screen.getByTestId("mon-install-paste-hint").textContent).toBe(
        "在「Ubuntu」視窗貼上並執行（不是 PowerShell）："
      );
      const wsl = screen.getByTestId("mon-install-wsl");
      expect(wsl.textContent).toContain("以系統管理員身分開啟 PowerShell");
      fireEvent.click(wsl.querySelector(".mon-install__cmd-line button") as HTMLButtonElement);
      await waitFor(() => expect(writeText).toHaveBeenCalledWith("wsl --install -d Ubuntu"));
      expect(getMachineBootCommand).toHaveBeenCalledTimes(1);
    });

    it("dims the command, says it has expired under the box and turns the copy button into a 重新產生指令 icon once the command expires — by its timer, or on a copy after the deadline when the timer has not fired — and shows the regenerated command ready to copy", async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setTimeout", "clearTimeout"] });
      const start = Date.now();
      const now = vi.spyOn(Date, "now").mockReturnValue(start);
      try {
        const writeText = vi.fn(async (_text: string) => {});
        Object.defineProperty(navigator, "clipboard", {
          value: { writeText },
          configurable: true,
        });
        getMachineBootCommand
          .mockResolvedValueOnce({ command: "curl first | bash", expiresInSecs: 600 })
          .mockResolvedValueOnce({ command: "curl second | bash", expiresInSecs: 600 })
          .mockResolvedValueOnce({ command: "curl third | bash", expiresInSecs: 600 });
        renderMonitor();
        fireEvent.click(await machineAction("mon-install-btn"));
        await waitFor(() =>
          expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl first | bash")
        );
        expect(screen.getByTestId("mon-copy-boot-cmd-btn").getAttribute("aria-label")).toBe("複製");
        expect(screen.queryByText("指令已過期，請重新產生。")).toBeNull();

        // The deadline passes while the timer is held back (background tab, sleep).
        now.mockReturnValue(start + 600_000);
        fireEvent.click(screen.getByTestId("mon-copy-boot-cmd-btn"));

        await waitFor(() =>
          expect(screen.getByTestId("mon-copy-boot-cmd-btn").getAttribute("aria-label")).toBe(
            "重新產生指令"
          )
        );
        expect(screen.getByTestId("mon-copy-boot-cmd-btn").getAttribute("title")).toBe("重新產生指令");
        expect(screen.getByTestId("mon-copy-boot-cmd-btn").textContent).toBe("");
        expect(
          screen.getByTestId("mon-boot-cmd").closest(".mon-install__cmd-line")!.lastElementChild!
            .textContent
        ).toBe("指令已過期，請重新產生。");
        expect(
          screen.getByTestId("mon-boot-cmd").classList.contains("mon-install__cmd--expired")
        ).toBe(true);
        expect(writeText.mock.calls).toEqual([]);

        fireEvent.click(screen.getByTestId("mon-copy-boot-cmd-btn"));
        await waitFor(() =>
          expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl second | bash")
        );

        await act(async () => {
          vi.advanceTimersByTime(600_000);
        });

        const shown = screen.getByTestId("mon-boot-cmd");
        expect(shown.textContent).toBe("curl second | bash");
        expect(shown.classList.contains("mon-install__cmd--expired")).toBe(true);
        const button = screen.getByTestId("mon-copy-boot-cmd-btn");
        expect(button.getAttribute("aria-label")).toBe("重新產生指令");
        expect(screen.getByText("指令已過期，請重新產生。").closest('[role="status"]')).toBeTruthy();

        fireEvent.click(button);

        await waitFor(() =>
          expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl third | bash")
        );
        expect(getMachineBootCommand).toHaveBeenCalledTimes(3);
        const copy = screen.getByTestId("mon-copy-boot-cmd-btn");
        expect(copy.getAttribute("aria-label")).toBe("複製");
        expect(screen.queryByText("指令已過期，請重新產生。")).toBeNull();
        fireEvent.click(copy);
        await waitFor(() => expect(writeText).toHaveBeenCalledWith("curl third | bash"));
      } finally {
        now.mockRestore();
        vi.useRealTimers();
      }
    });

    it("offers to generate the command again when fetching it fails", async () => {
      getMachineBootCommand.mockRejectedValueOnce(new Error("offline"));
      renderMonitor();
      fireEvent.click(await machineAction("mon-install-btn"));
      const retry = await screen.findByRole("button", { name: "重新產生指令" });
      expect(retry.getAttribute("data-testid")).toBe("mon-copy-boot-cmd-btn");
      expect(screen.getByText("取得指令失敗").closest('[role="status"]')).toBeTruthy();
      expect(screen.queryByText("指令已過期，請重新產生。")).toBeNull();
      fireEvent.click(retry);

      await waitFor(() =>
        expect(screen.getByTestId("mon-boot-cmd").textContent).toBe("curl … | sh")
      );
      expect(getMachineBootCommand).toHaveBeenCalledTimes(2);
      expect(screen.getByTestId("mon-copy-boot-cmd-btn").getAttribute("aria-label")).toBe("複製");
      expect(screen.queryByText("取得指令失敗")).toBeNull();
    });
  });
});
