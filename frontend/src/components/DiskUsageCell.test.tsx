import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { DiskUsageBreakdown, DiskUsageCell } from "./DiskUsageCell";
import type { MachineDiskUsageCategoryView, MachineDiskUsageMemberView, MachineDiskUsageView } from "../types";

const NOW = new Date(2026, 9, 4, 10, 0, 0, 0);
const NOW_S = NOW.getTime() / 1000;
const kib = (n: number) => n * 1024;

const member = (
  memberId: string,
  name: string | null,
  rosterStatus: MachineDiskUsageMemberView["rosterStatus"],
  workspaceKib: number | null,
): MachineDiskUsageMemberView => ({
  memberId,
  name,
  rosterStatus,
  workspaceBytes: workspaceKib === null ? null : kib(workspaceKib),
});

const cat = (key: string, bytes: number | null, parentKey: string | null = null, inRoot = true): MachineDiskUsageCategoryView => ({
  key,
  parentKey,
  bytes,
  inRoot,
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

// The server's own machine, in the order the server sends: 41.7 GB in all.
const CATEGORIES = [
  cat("database", kib(2417536)),
  cat("backups", kib(13349048)),
  cat("workspaces", kib(24423384)),
  cat("logs", kib(360000)),
  cat("old_version_backups", kib(2000000)),
  cat("other", kib(1198544)),
];
const TOTAL_KIB = 43748512;

const FULL: MachineDiskUsageView = {
  measuredAt: NOW_S - 12 * 60,
  totalBytes: kib(TOTAL_KIB),
  databaseMeasuredAt: NOW_S - 5 * 60,
  categories: CATEGORIES,
  members: SEVEN_MEMBERS,
  diskFreeBytes: 412316860416,
  diskTotalBytes: 1000240963584,
};

const EMPTY: MachineDiskUsageView = {
  measuredAt: null,
  totalBytes: null,
  databaseMeasuredAt: null,
  categories: [],
  members: [],
  diskFreeBytes: null,
  diskTotalBytes: null,
};

function mountCell(usage: MachineDiskUsageView | null | undefined) {
  const onOpen = vi.fn();
  const onHostClick = vi.fn();
  render(
    <I18nProvider>
      <div data-testid="host" onClick={onHostClick}>
        <DiskUsageCell usage={usage} onOpen={onOpen} />
      </div>
    </I18nProvider>,
  );
  return { onOpen, onHostClick };
}

function mountBreakdown(usage: MachineDiskUsageView) {
  render(
    <I18nProvider>
      <DiskUsageBreakdown usage={usage} />
    </I18nProvider>,
  );
}

function rows(): [string, string][] {
  return screen.getAllByTestId("disk-usage-row").map((row) => [
    row.querySelector(".disk-usage__name")?.textContent ?? "",
    row.querySelector(".disk-usage__value")?.textContent ?? "",
  ]);
}

/** Each row's name and its grey description ("" when none). */
function descriptions(): [string, string][] {
  return screen.getAllByTestId("disk-usage-row").map((row) => [
    row.querySelector(".disk-usage__name")?.textContent ?? "",
    row.querySelector(".disk-usage__desc")?.textContent ?? "",
  ]);
}

function shape(): [string, string][] {
  return screen.getAllByTestId("disk-usage-row").map((row) => [
    row.querySelector(".disk-usage__name")?.textContent ?? "",
    row.classList.contains("disk-usage__row--sub") ? "sub" : row.classList.contains("disk-usage__row--section") ? "section" : "",
  ]);
}

/** Each bar segment's category and share in percent. */
function segments(): [string, number][] {
  return screen
    .queryAllByTestId("disk-usage-seg")
    .map((seg) => [seg.getAttribute("data-segment") ?? "", parseFloat(seg.style.flexBasis)]);
}

/** The categories whose list row carries a colour swatch. */
function swatches(): string[] {
  return Array.from(document.querySelectorAll(".disk-usage__swatch[data-segment]")).map(
    (el) => el.getAttribute("data-segment") ?? "",
  );
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
          <DiskUsageCell usage={usage} onOpen={vi.fn()} />
        </I18nProvider>,
      );
      const cell = screen.getByTestId("disk-usage-unmeasured");
      expect(cell.textContent).toBe("尚未量測");
      expect(cell.className).toBe("disk-usage__unmeasured");
      expect(screen.queryByRole("button")).toBeNull();
      unmount();
    }
  });

  it("shows only the total, with a chevron, as a button that opens the dialog and does not reach the host row", () => {
    const { onOpen, onHostClick } = mountCell(FULL);
    const trigger = screen.getByTestId("disk-usage-trigger");
    expect(trigger.textContent).toBe("41.7 GB");
    expect(trigger.querySelector(".disk-usage__chevron svg")).toBeTruthy();
    expect(trigger.getAttribute("aria-label")).toBe("機器詳情：磁碟用量 41.7 GB");
    expect(trigger.getAttribute("aria-haspopup")).toBe("dialog");
    expect(screen.queryByTestId("disk-usage-row")).toBeNull();
    fireEvent.click(trigger);
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(onHostClick).not.toHaveBeenCalled();
  });

  it("under a measurement whose total is null it reads a dash and still opens", () => {
    const { onOpen } = mountCell({ ...EMPTY, categories: [cat("database", kib(2417536))] });
    const trigger = screen.getByTestId("disk-usage-trigger");
    expect(trigger.textContent).toBe("—");
    fireEvent.click(trigger);
    expect(onOpen).toHaveBeenCalledTimes(1);
  });
});

describe("DiskUsageBreakdown", () => {
  it("lists the categories in the server's order, the top five members with 已離開 marks and the rest summed under 成員 workspace, then when it was measured", () => {
    mountBreakdown(FULL);
    expect(rows()).toEqual([
      ["資料庫", "2.3 GB"],
      ["資料庫自動備份", "12.7 GB"],
      ["成員 workspace", "23.3 GB"],
      ["Mira", "7.9 GB"],
      ["O-179", "5.7 GB"],
      ["O-151已離開", "3.9 GB"],
      ["ow-91c4已離開", "2.4 GB"],
      ["O-163已離開", "1.8 GB"],
      ["其餘 2 位合計", "1.5 GB"],
      ["日誌", "351.6 MB"],
      ["升級殘留", "1.9 GB"],
      ["其他", "1.1 GB"],
      ["硬碟剩餘／總容量", "384.0 GB / 931.5 GB"],
    ]);
    expect(screen.getAllByTestId("disk-usage-left").map((e) => e.textContent)).toEqual(["已離開", "已離開", "已離開"]);
    expect(screen.getByTestId("disk-usage-measured").textContent).toBe("量於 12m 前");
  });

  it("puts a grey line under each known category saying what it holds, and none under members or the disk row", () => {
    mountBreakdown({ ...FULL, members: SEVEN_MEMBERS.slice(0, 1), categories: [...CATEGORIES.slice(0, -1), cat("old_database_copies", 1, null, false), CATEGORIES[5]] });
    expect(descriptions()).toEqual([
      ["資料庫", "成員、任務、聊天等所有資料"],
      ["資料庫自動備份", "定期自動備份，保留最近幾份"],
      ["成員 workspace", "各成員的工作目錄"],
      ["Mira", ""],
      ["日誌", "OffiCraft 程式的執行紀錄"],
      ["升級殘留", "升級時留下的舊版程式與舊資料庫副本，不會自動刪除"],
      ["舊資料庫副本", "資料庫旁留下的舊副本，不會自動刪除"],
      ["其他", "目前版本的程式、設定檔，以及手動放進來的檔案"],
      ["硬碟剩餘／總容量", ""],
    ]);
  });

  it("under English, the labels and descriptions read in English", () => {
    window.localStorage.setItem("oc.language", "en");
    try {
      mountBreakdown({ ...FULL, members: [] });
      expect(descriptions()).toEqual([
        ["Database", "Members, tasks, chat and everything else"],
        ["Automatic database backups", "Taken automatically; the latest few are kept"],
        ["Member workspaces", "Each member's working directory"],
        ["Logs", "What the OffiCraft programs have logged"],
        ["Upgrade leftovers", "Old programs and database copies left by upgrades; never deleted automatically"],
        ["Other", "The current programs, settings, and files put here by hand"],
        ["Disk free / total", ""],
      ]);
    } finally {
      window.localStorage.removeItem("oc.language");
    }
  });

  it("indents the members under 成員 workspace, which opens a section, and the row after them opens one too", () => {
    mountBreakdown(FULL);
    expect(shape()).toEqual([
      ["資料庫", ""],
      ["資料庫自動備份", ""],
      ["成員 workspace", "section"],
      ["Mira", "sub"],
      ["O-179", "sub"],
      ["O-151已離開", "sub"],
      ["ow-91c4已離開", "sub"],
      ["O-163已離開", "sub"],
      ["其餘 2 位合計", "sub"],
      ["日誌", "section"],
      ["升級殘留", ""],
      ["其他", ""],
      ["硬碟剩餘／總容量", "section"],
    ]);
  });

  it("shows a key the page does not know as the key itself, in the neutral colour with no description, and a category's parts as sub rows after it", () => {
    mountBreakdown({
      ...EMPTY,
      totalBytes: 1000,
      categories: [cat("workspaces", 0), cat("cache", 300), cat("zeta", 150, null, false), cat("zeta-a", 100, "zeta", false), cat("zeta-b", 50, "zeta", false), cat("other", 550)],
    });
    expect(shape()).toEqual([
      ["成員 workspace", "section"],
      ["cache", ""],
      ["zeta", "section"],
      ["zeta-a", "sub"],
      ["zeta-b", "sub"],
      ["其他", "section"],
    ]);
    expect(descriptions().find(([name]) => name === "cache")).toEqual(["cache", ""]);
    const cache = document.querySelector('[data-testid="disk-usage-seg"][data-segment="cache"]')!;
    expect(cache.className).toBe("disk-usage__seg disk-usage__seg--unknown");
    expect(segments().map(([key]) => key)).toEqual(["cache", "zeta", "other"]);
  });

  it("reads a category that could not be measured as a dash, with no segment and no colour on its swatch", () => {
    mountBreakdown({ ...FULL, members: [], categories: [cat("workspaces", 0), cat("logs", null), cat("other", null)] });
    expect(rows()).toEqual([
      ["成員 workspace", "0.0 KB"],
      ["日誌", "—"],
      ["其他", "—"],
      ["硬碟剩餘／總容量", "384.0 GB / 931.5 GB"],
    ]);
    expect(segments()).toEqual([]);
  });

  it("keeps the swatch slot on every top-level row, coloured or not, and none on a sub row", () => {
    mountBreakdown({ ...FULL, members: SEVEN_MEMBERS.slice(0, 1), categories: CATEGORIES.map((c) => (c.key === "logs" ? cat("logs", 0) : c)) });
    const slots = screen.getAllByTestId("disk-usage-row").map((row) => {
      const slot = row.querySelector(".disk-usage__label > .disk-usage__swatch");
      return [row.querySelector(".disk-usage__name")?.textContent ?? "", slot === null ? "none" : slot.getAttribute("data-segment") ?? "empty"];
    });
    expect(slots).toEqual([
      ["資料庫", "database"],
      ["資料庫自動備份", "backups"],
      ["成員 workspace", "workspaces"],
      ["Mira", "none"],
      ["日誌", "empty"],
      ["升級殘留", "old_version_backups"],
      ["其他", "other"],
      ["硬碟剩餘／總容量", "empty"],
    ]);
  });

  it("draws one bar of the top-level categories, each as wide as its share of the total, with a matching swatch on its row", () => {
    mountBreakdown(FULL);
    const expected: [string, number][] = CATEGORIES.map((c) => [c.key, (c.bytes! / kib(TOTAL_KIB)) * 100]);
    const got = segments();
    expect(got.map(([key]) => key)).toEqual(expected.map(([key]) => key));
    got.forEach(([, width], i) => expect(width).toBeCloseTo(expected[i][1], 2));
    expect(swatches()).toEqual(["database", "backups", "workspaces", "logs", "old_version_backups", "other"]);
    const bar = screen.getByRole("img");
    expect(bar.getAttribute("data-testid")).toBe("disk-usage-bar");
    expect(bar.getAttribute("aria-label")).toBe(
      "OffiCraft 磁碟用量組成：資料庫 2.3 GB (6%)、資料庫自動備份 12.7 GB (31%)、成員 workspace 23.3 GB (56%)、日誌 351.6 MB (<1%)、升級殘留 1.9 GB (5%)、其他 1.1 GB (3%)",
    );
  });

  it("leaves a category of zero or unmeasured bytes out of the bar and its swatch off the row", () => {
    mountBreakdown({ ...FULL, categories: CATEGORIES.map((c) => (c.key === "backups" ? cat("backups", 0) : c.key === "other" ? cat("other", null) : c)) });
    expect(segments().map(([key]) => key)).toEqual(["database", "workspaces", "logs", "old_version_backups"]);
    expect(swatches()).toEqual(["database", "workspaces", "logs", "old_version_backups"]);
  });

  it("under categories adding up to less than the total, each is a share of the total and the rest of the bar stays empty", () => {
    mountBreakdown({ ...EMPTY, totalBytes: 100, categories: [cat("database", 25), cat("other", 25)] });
    expect(segments()).toEqual([
      ["database", 25],
      ["other", 25],
    ]);
  });

  it("under categories adding up to more than the total, the bar is shared out of their sum and never overflows", () => {
    mountBreakdown({ ...EMPTY, totalBytes: 100, categories: [cat("database", 60), cat("workspaces", 90)] });
    const got = segments();
    expect(got.map(([key]) => key)).toEqual(["database", "workspaces"]);
    expect(got[0][1]).toBeCloseTo(40, 5);
    expect(got[1][1]).toBeCloseTo(60, 5);
  });

  it("under exactly five members there is no 其餘 row", () => {
    mountBreakdown({ ...FULL, members: SEVEN_MEMBERS.slice(0, 5) });
    expect(rows().map(([label]) => label)).not.toContain("其餘 2 位合計");
    expect(rows().filter(([, value]) => value).length).toBe(12);
  });

  it("under a member list the server did not sort, the five largest are shown largest first and an unsized one last", () => {
    mountBreakdown({ ...FULL, members: [member("ow-x", "O-X", "active", null), ...[...SEVEN_MEMBERS].reverse()] });
    expect(rows().slice(3, 9)).toEqual([
      ["Mira", "7.9 GB"],
      ["O-179", "5.7 GB"],
      ["O-151已離開", "3.9 GB"],
      ["ow-91c4已離開", "2.4 GB"],
      ["O-163已離開", "1.8 GB"],
      ["其餘 3 位合計", "1.5 GB"],
    ]);
  });

  it("under a measurement whose total is null, there is no bar and the server's rows are listed", () => {
    mountBreakdown({
      ...EMPTY,
      categories: [cat("database", kib(2417536)), cat("backups", kib(13349048))],
      databaseMeasuredAt: NOW_S - 3 * 3600,
      diskFreeBytes: 412316860416,
    });
    expect(screen.queryByTestId("disk-usage-bar")).toBeNull();
    expect(swatches()).toEqual([]);
    expect(rows()).toEqual([
      ["資料庫", "2.3 GB"],
      ["資料庫自動備份", "12.7 GB"],
    ]);
    expect(screen.getByTestId("disk-usage-measured").textContent).toBe("量於 3h 前");
  });

  it("under an object with nothing measured there is no bar, no row and no measured line", () => {
    mountBreakdown(EMPTY);
    expect(screen.queryByTestId("disk-usage-bar")).toBeNull();
    expect(screen.queryAllByTestId("disk-usage-row")).toEqual([]);
    expect(screen.queryByTestId("disk-usage-measured")).toBeNull();
  });

  it("under English, the bar's summary reads in English", () => {
    window.localStorage.setItem("oc.language", "en");
    try {
      mountBreakdown({ ...EMPTY, totalBytes: kib(4194304), categories: [cat("database", kib(1048576)), cat("other", kib(3145728))] });
      expect(screen.getByRole("img").getAttribute("aria-label")).toBe(
        "OffiCraft disk usage by category: Database 1.0 GB (25%), Other 3.0 GB (75%)",
      );
    } finally {
      window.localStorage.removeItem("oc.language");
    }
  });
});
