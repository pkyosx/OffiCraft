import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { DiskUsageCell } from "./DiskUsageCell";
import type { MachineDiskUsageMemberView, MachineDiskUsageView } from "../types";

const NOW = new Date(2026, 9, 4, 10, 0, 0, 0);
const NOW_S = NOW.getTime() / 1000;
const kib = (n: number) => n * 1024;

const member = (
  memberId: string,
  name: string | null,
  rosterStatus: MachineDiskUsageMemberView["rosterStatus"],
  totalKib: number,
): MachineDiskUsageMemberView => ({
  memberId,
  name,
  rosterStatus,
  workspaceBytes: null,
  conversationBytes: null,
  totalBytes: kib(totalKib),
});

const SEVEN_MEMBERS = [
  member("mira", "Mira", "active", 8300000),
  member("ow-179", "O-179", "active", 6000000),
  member("ow-151", "O-151", "removed", 4100000),
  member("ow-91c4", null, "unknown", 2500000),
  member("ow-163", "O-163", "removed", 1900000),
  member("ow-170", "O-170", "removed", 1023384),
  member("ow-172", "O-172", "removed", 600000),
];

const FULL: MachineDiskUsageView = {
  measuredAt: NOW_S - 12 * 60,
  totalBytes: kib(43748512),
  databaseBytes: kib(2417536),
  backupsBytes: kib(13349048),
  databaseMeasuredAt: NOW_S - 5 * 60,
  workspaceBytes: kib(16306716),
  conversationBytes: kib(8116668),
  claudeConversationBytes: kib(3663836),
  codexConversationBytes: kib(4452832),
  otherBytes: kib(3558544),
  members: SEVEN_MEMBERS,
  diskFreeBytes: 412316860416,
  diskTotalBytes: 1000240963584,
};

const EMPTY: MachineDiskUsageView = {
  measuredAt: null,
  totalBytes: null,
  databaseBytes: null,
  backupsBytes: null,
  databaseMeasuredAt: null,
  workspaceBytes: null,
  conversationBytes: null,
  claudeConversationBytes: null,
  codexConversationBytes: null,
  otherBytes: null,
  members: [],
  diskFreeBytes: null,
  diskTotalBytes: null,
};

function mount(usage: MachineDiskUsageView | null | undefined, onHostClick = vi.fn()) {
  render(
    <I18nProvider>
      <div data-testid="host" onClick={onHostClick}>
        <DiskUsageCell usage={usage} />
      </div>
      <button type="button" data-testid="elsewhere">elsewhere</button>
    </I18nProvider>,
  );
  return onHostClick;
}

function panelRows(): [string, string][] {
  return screen.getAllByTestId("disk-usage-row").map((row) => [
    row.querySelector(".disk-usage__label")?.textContent ?? "",
    row.querySelector(".disk-usage__value")?.textContent ?? "",
  ]);
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(NOW);
});

afterEach(() => {
  vi.useRealTimers();
});

describe("DiskUsageCell", () => {
  it("under no measurement it reads 尚未量測 in grey and offers nothing to open", () => {
    for (const usage of [null, undefined]) {
      const { unmount } = render(
        <I18nProvider>
          <DiskUsageCell usage={usage} />
        </I18nProvider>,
      );
      const cell = screen.getByTestId("disk-usage-unmeasured");
      expect(cell.textContent).toBe("尚未量測");
      expect(cell.className).toBe("disk-usage__unmeasured");
      expect(screen.queryByRole("button")).toBeNull();
      unmount();
    }
    mount(FULL);
    expect(screen.queryByTestId("disk-usage-unmeasured")).toBeNull();
    expect(screen.getByTestId("disk-usage-trigger").textContent).toBe("41.7 GB");
  });

  it("under a full measurement a click opens the whole breakdown in order, top five members with 已離開 marks and the rest summed", () => {
    mount(FULL);
    const trigger = screen.getByTestId("disk-usage-trigger");
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(trigger.getAttribute("aria-label")).toBe("磁碟用量明細 41.7 GB");
    expect(screen.queryByTestId("disk-usage-panel")).toBeNull();

    fireEvent.click(trigger);

    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    const panel = screen.getByRole("dialog", { name: "磁碟用量明細" });
    expect(panel.getAttribute("data-testid")).toBe("disk-usage-panel");
    expect(panelRows()).toEqual([
      ["資料庫", "2.3 GB"],
      ["備份", "12.7 GB"],
      ["成員 workspace 合計", "15.6 GB"],
      ["Mira", "7.9 GB"],
      ["O-179", "5.7 GB"],
      ["O-151已離開", "3.9 GB"],
      ["ow-91c4已離開", "2.4 GB"],
      ["O-163已離開", "1.8 GB"],
      ["其餘 2 位合計", "1.5 GB"],
      ["對話紀錄", "7.7 GB"],
      ["Claude", "3.5 GB"],
      ["Codex", "4.2 GB"],
      ["其他", "3.4 GB"],
      ["硬碟剩餘／總容量", "384.0 GB / 931.5 GB"],
    ]);
    expect(screen.getAllByTestId("disk-usage-left").map((e) => e.textContent)).toEqual([
      "已離開",
      "已離開",
      "已離開",
    ]);
    expect(screen.getByTestId("disk-usage-measured").textContent).toBe("量於 12m 前");
  });

  it("under exactly five members there is no 其餘 row", () => {
    mount({ ...FULL, members: SEVEN_MEMBERS.slice(0, 5) });
    fireEvent.click(screen.getByTestId("disk-usage-trigger"));
    expect(panelRows().map(([label]) => label)).toEqual([
      "資料庫",
      "備份",
      "成員 workspace 合計",
      "Mira",
      "O-179",
      "O-151已離開",
      "ow-91c4已離開",
      "O-163已離開",
      "對話紀錄",
      "Claude",
      "Codex",
      "其他",
      "硬碟剩餘／總容量",
    ]);
  });

  it("under a member list the server did not sort, the five largest are shown largest first", () => {
    mount({ ...FULL, members: [...SEVEN_MEMBERS].reverse() });
    fireEvent.click(screen.getByTestId("disk-usage-trigger"));
    expect(panelRows().slice(3, 9)).toEqual([
      ["Mira", "7.9 GB"],
      ["O-179", "5.7 GB"],
      ["O-151已離開", "3.9 GB"],
      ["ow-91c4已離開", "2.4 GB"],
      ["O-163已離開", "1.8 GB"],
      ["其餘 2 位合計", "1.5 GB"],
    ]);
  });

  it("under a measurement object whose total is null the cell reads a dash and the panel shows only the measured rows", () => {
    mount({
      ...EMPTY,
      databaseBytes: kib(2417536),
      backupsBytes: kib(13349048),
      databaseMeasuredAt: NOW_S - 3 * 3600,
      diskFreeBytes: 412316860416,
    });
    const trigger = screen.getByTestId("disk-usage-trigger");
    expect(trigger.textContent).toBe("—");
    fireEvent.click(trigger);
    expect(panelRows()).toEqual([
      ["資料庫", "2.3 GB"],
      ["備份", "12.7 GB"],
    ]);
    expect(screen.getByTestId("disk-usage-measured").textContent).toBe("量於 3h 前");
  });

  it("under an object with nothing measured the panel has no rows and no measured line", () => {
    mount(EMPTY);
    fireEvent.click(screen.getByTestId("disk-usage-trigger"));
    expect(screen.getByTestId("disk-usage-panel")).toBeTruthy();
    expect(screen.queryAllByTestId("disk-usage-row")).toEqual([]);
    expect(screen.queryByTestId("disk-usage-measured")).toBeNull();
  });

  it("closes on a second click, on Esc and on a press elsewhere, but not on a press inside the panel", () => {
    mount(FULL);
    const trigger = screen.getByTestId("disk-usage-trigger");

    fireEvent.click(trigger);
    expect(screen.getByTestId("disk-usage-panel")).toBeTruthy();
    fireEvent.click(trigger);
    expect(screen.queryByTestId("disk-usage-panel")).toBeNull();
    expect(trigger.getAttribute("aria-expanded")).toBe("false");

    fireEvent.click(trigger);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByTestId("disk-usage-panel")).toBeNull();
    expect(document.activeElement).toBe(trigger);

    fireEvent.click(trigger);
    fireEvent.mouseDown(screen.getAllByTestId("disk-usage-row")[0]);
    expect(screen.getByTestId("disk-usage-panel")).toBeTruthy();
    fireEvent.mouseDown(screen.getByTestId("elsewhere"));
    expect(screen.queryByTestId("disk-usage-panel")).toBeNull();
  });

  it("stays open while its own rows scroll and closes when the page or the table frame scrolls", () => {
    mount(FULL);
    const trigger = screen.getByTestId("disk-usage-trigger");

    fireEvent.click(trigger);
    expect(screen.getByTestId("disk-usage-panel")).toBeTruthy();
    fireEvent.scroll(screen.getByTestId("disk-usage-panel"));
    fireEvent.scroll(screen.getByTestId("disk-usage-panel").querySelector("ul")!);
    expect(screen.getByTestId("disk-usage-panel")).toBeTruthy();

    fireEvent.scroll(screen.getByTestId("host"));
    expect(screen.queryByTestId("disk-usage-panel")).toBeNull();

    fireEvent.click(trigger);
    expect(screen.getByTestId("disk-usage-panel")).toBeTruthy();
    fireEvent.scroll(window);
    expect(screen.queryByTestId("disk-usage-panel")).toBeNull();
  });

  it("clicks on the cell or inside its panel do not reach the host row", () => {
    const host = mount(FULL);
    fireEvent.click(screen.getByTestId("disk-usage-trigger"));
    fireEvent.click(screen.getAllByTestId("disk-usage-row")[0]);
    expect(host).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("host"));
    expect(host).toHaveBeenCalledTimes(1);
  });
});
