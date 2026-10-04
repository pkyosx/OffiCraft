import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ApiError } from "../api/errors";
import { codeForStatus } from "../api/errorCodes";
import type { RuntimeUpgradeView } from "../types";
import { RuntimeUpgradeDialog } from "./RuntimeUpgradeDialog";

const startRuntimeUpgrade = vi.fn();
const getRuntimeUpgrade = vi.fn();
let topicHandler: ((topic: string) => void) | null = null;

vi.mock("../api", () => ({
  api: {
    startRuntimeUpgrade: (...a: unknown[]) => startRuntimeUpgrade(...a),
    getRuntimeUpgrade: (...a: unknown[]) => getRuntimeUpgrade(...a),
    subscribeEvents: (cb: (topic: string) => void) => {
      topicHandler = cb;
      return () => {
        topicHandler = null;
      };
    },
  },
}));

const upgrade = (patch: Partial<RuntimeUpgradeView>): RuntimeUpgradeView => ({
  upgradeId: "ru-1",
  machineId: "m-box",
  runtime: "claude",
  state: "starting",
  fromVersion: null,
  toVersion: null,
  reason: null,
  startedTs: 1_800_000_000,
  updatedTs: 1_800_000_000,
  ...patch,
});

async function flush() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function emit(next: RuntimeUpgradeView, topic = "runtime_upgrade") {
  getRuntimeUpgrade.mockResolvedValueOnce(next);
  await act(async () => {
    topicHandler?.(topic);
  });
  await flush();
}

function mount(onClose: () => void = () => {}) {
  return render(
    <I18nProvider>
      <RuntimeUpgradeDialog machineId="m-box" machineName="工作站" onClose={onClose} />
    </I18nProvider>
  );
}

const text = (id: string) => screen.getByTestId(id).textContent;

beforeEach(() => {
  startRuntimeUpgrade.mockReset().mockResolvedValue(upgrade({}));
  getRuntimeUpgrade.mockReset();
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("RuntimeUpgradeDialog", () => {
  it("under an upgrade the warden carries through, it shows preparing, then running with the old version, then old → new", async () => {
    mount();
    expect(startRuntimeUpgrade).toHaveBeenCalledWith("m-box", "claude");
    await flush();
    expect(screen.getByRole("dialog").getAttribute("aria-label")).toBe("升級 Claude Code");
    expect(text("runtime-upgrade-subtitle")).toBe("在 工作站 上執行 claude update，升級成員使用的 Claude Code");
    expect(text("runtime-upgrade-preparing")).toBe("正在請 工作站 開始升級…");

    await emit(upgrade({ state: "running", fromVersion: "2.1.200" }));
    expect(getRuntimeUpgrade).toHaveBeenCalledWith("m-box", "ru-1");
    expect(text("runtime-upgrade-running")).toBe("升級中…");
    expect(text("runtime-upgrade-from")).toBe("目前版本：2.1.200");

    await emit(upgrade({ state: "succeeded", fromVersion: "2.1.200", toVersion: "2.1.300" }));
    expect(text("runtime-upgrade-succeeded")).toBe("升級完成：2.1.200 → 2.1.300");
    expect(screen.getByText("已在執行的成員繼續使用原本的版本，之後啟動的成員才會用新版本")).toBeTruthy();
    expect(screen.queryByTestId("runtime-upgrade-restart")).toBeNull();
  });

  it("under a signal on another topic, it does not refetch", async () => {
    mount();
    await flush();
    await act(async () => {
      topicHandler?.("runtime_login");
    });
    await flush();
    expect(getRuntimeUpgrade).not.toHaveBeenCalled();
    expect(text("runtime-upgrade-preparing")).toBe("正在請 工作站 開始升級…");
  });

  it("under a failed upgrade, it shows the warden's reason, and 重新升級 starts again", async () => {
    mount();
    await flush();
    await emit(
      upgrade({
        state: "failed",
        fromVersion: "2.1.300",
        toVersion: "2.1.300",
        reason: "版本沒有改變（仍是 2.1.300）：已是最新版，或 `claude update` 升級的不是成員使用的那一份 Claude Code",
      })
    );
    expect(text("runtime-upgrade-failed")).toBe(
      "升級失敗原因：版本沒有改變（仍是 2.1.300）：已是最新版，或 `claude update` 升級的不是成員使用的那一份 Claude Code"
    );
    startRuntimeUpgrade.mockResolvedValueOnce(upgrade({ upgradeId: "ru-2" }));
    fireEvent.click(screen.getByTestId("runtime-upgrade-restart"));
    await flush();
    expect(startRuntimeUpgrade).toHaveBeenCalledTimes(2);
    expect(text("runtime-upgrade-preparing")).toBe("正在請 工作站 開始升級…");
  });

  it("under an expired upgrade, it says the machine stopped reporting", async () => {
    mount();
    await flush();
    await emit(upgrade({ state: "expired", reason: "no report from the machine for 20 minutes" }));
    expect(text("runtime-upgrade-failed")).toBe("這台機器太久沒有回報，升級結果不明，請重新升級");
  });

  it("under a state this build does not know, it keeps showing the upgrade as in flight", async () => {
    mount();
    await flush();
    await emit(upgrade({ state: "verifying" as RuntimeUpgradeView["state"], fromVersion: "2.1.200" }));
    expect(text("runtime-upgrade-running")).toBe("升級中…");
    expect(screen.queryByTestId("runtime-upgrade-restart")).toBeNull();
  });

  it("under an offline warden (409 on start), it says the machine is offline", async () => {
    startRuntimeUpgrade.mockRejectedValue(
      new ApiError("http 409 for POST", 409, codeForStatus(409), "machine is offline; its warden cannot run an upgrade")
    );
    mount();
    await flush();
    expect(text("runtime-upgrade-offline")).toBe("工作站 目前離線，無法升級");
    expect(screen.getByTestId("runtime-upgrade-restart")).toBeTruthy();
  });

  it("under an upgrade still starting after 30s, it gives up saying the machine's warden must be updated first", async () => {
    vi.useFakeTimers();
    mount();
    await flush();
    await act(async () => {
      vi.advanceTimersByTime(29_999);
    });
    expect(screen.getByTestId("runtime-upgrade-preparing")).toBeTruthy();
    await act(async () => {
      vi.advanceTimersByTime(1);
    });
    expect(text("runtime-upgrade-no-response")).toBe(
      "工作站 沒有回應升級要求這台機器上的 warden 版本太舊，還不支援從這裡升級 Claude Code；請先更新這台機器的 warden。"
    );
    expect(screen.getByTestId("runtime-upgrade-restart")).toBeTruthy();
  });

  it("under an upgrade that moved past starting, the 30s give-up never fires", async () => {
    vi.useFakeTimers();
    mount();
    await flush();
    await emit(upgrade({ state: "running", fromVersion: "2.1.200" }));
    await act(async () => {
      vi.advanceTimersByTime(60_000);
    });
    expect(text("runtime-upgrade-running")).toBe("升級中…");
    expect(screen.queryByTestId("runtime-upgrade-no-response")).toBeNull();
  });

  it("under 關閉 while the upgrade runs, it closes without any further request", async () => {
    const onClose = vi.fn();
    mount(onClose);
    await flush();
    await emit(upgrade({ state: "running", fromVersion: "2.1.200" }));
    fireEvent.click(screen.getByTestId("runtime-upgrade-close"));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(startRuntimeUpgrade).toHaveBeenCalledTimes(1);
    expect(getRuntimeUpgrade).toHaveBeenCalledTimes(1);
  });
});
