import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { DiskUsageBreakdown, DiskUsageCell } from "./DiskUsageCell";
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
    row.querySelector(".disk-usage__label")?.textContent ?? "",
    row.querySelector(".disk-usage__value")?.textContent ?? "",
  ]);
}

/** Each bar segment's category and width in percent. */
function segments(): [string, number][] {
  return screen
    .queryAllByTestId("disk-usage-seg")
    .map((seg) => [seg.getAttribute("data-segment") ?? "", parseFloat(seg.style.width)]);
}

/** The categories whose list row carries a colour swatch. */
function swatches(): string[] {
  return Array.from(document.querySelectorAll(".disk-usage__swatch")).map(
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
    expect(trigger.getAttribute("aria-label")).toBe("磁碟用量明細 41.7 GB");
    expect(trigger.getAttribute("aria-haspopup")).toBe("dialog");
    expect(screen.queryByTestId("disk-usage-row")).toBeNull();
    fireEvent.click(trigger);
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(onHostClick).not.toHaveBeenCalled();
  });

  it("under a measurement whose total is null it reads a dash and still opens", () => {
    const { onOpen } = mountCell({ ...EMPTY, databaseBytes: kib(2417536) });
    const trigger = screen.getByTestId("disk-usage-trigger");
    expect(trigger.textContent).toBe("—");
    fireEvent.click(trigger);
    expect(onOpen).toHaveBeenCalledTimes(1);
  });
});

describe("DiskUsageBreakdown", () => {
  it("lists the whole breakdown in order, top five members with 已離開 marks and the rest summed, then when it was measured", () => {
    mountBreakdown(FULL);
    expect(rows()).toEqual([
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

  it("draws one bar of the five top-level categories, each as wide as its share of the total, with a matching swatch on its row", () => {
    mountBreakdown(FULL);
    const total = 43748512;
    const expected: [string, number][] = [
      ["database", (2417536 / total) * 100],
      ["backups", (13349048 / total) * 100],
      ["workspaces", (16306716 / total) * 100],
      ["conversations", (8116668 / total) * 100],
      ["other", (3558544 / total) * 100],
    ];
    const got = segments();
    expect(got.map(([key]) => key)).toEqual(expected.map(([key]) => key));
    got.forEach(([, width], i) => expect(width).toBeCloseTo(expected[i][1], 2));
    // Members and the Claude/Codex split are inside their category, not segments of their own.
    expect(swatches()).toEqual(["database", "backups", "workspaces", "conversations", "other"]);
    const bar = screen.getByRole("img");
    expect(bar.getAttribute("data-testid")).toBe("disk-usage-bar");
    expect(bar.getAttribute("aria-label")).toBe(
      "OffiCraft 磁碟用量組成：資料庫 2.3 GB (6%)、備份 12.7 GB (31%)、成員 workspace 合計 15.6 GB (37%)、對話紀錄 7.7 GB (19%)、其他 3.4 GB (8%)",
    );
  });

  it("leaves a category of zero or unmeasured bytes out of the bar and its swatch off the row", () => {
    mountBreakdown({ ...FULL, backupsBytes: 0, otherBytes: null });
    expect(segments().map(([key]) => key)).toEqual(["database", "workspaces", "conversations"]);
    expect(swatches()).toEqual(["database", "workspaces", "conversations"]);
    expect(rows().map(([label]) => label)).toContain("備份");
    expect(rows().map(([label]) => label)).not.toContain("其他");
  });

  it("under categories adding up to less than the total, each is a share of the total and the rest of the bar stays empty", () => {
    mountBreakdown({ ...EMPTY, totalBytes: 100, databaseBytes: 25, otherBytes: 25 });
    expect(segments()).toEqual([
      ["database", 25],
      ["other", 25],
    ]);
  });

  it("under categories adding up to more than the total, the bar is shared out of their sum and never overflows", () => {
    mountBreakdown({
      ...EMPTY,
      totalBytes: 100,
      databaseBytes: 60,
      workspaceBytes: 90,
    });
    const got = segments();
    expect(got.map(([key]) => key)).toEqual(["database", "workspaces"]);
    expect(got[0][1]).toBeCloseTo(40, 5);
    expect(got[1][1]).toBeCloseTo(60, 5);
  });

  it("under exactly five members there is no 其餘 row", () => {
    mountBreakdown({ ...FULL, members: SEVEN_MEMBERS.slice(0, 5) });
    expect(rows().map(([label]) => label)).toEqual([
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
    mountBreakdown({ ...FULL, members: [...SEVEN_MEMBERS].reverse() });
    expect(rows().slice(3, 9)).toEqual([
      ["Mira", "7.9 GB"],
      ["O-179", "5.7 GB"],
      ["O-151已離開", "3.9 GB"],
      ["ow-91c4已離開", "2.4 GB"],
      ["O-163已離開", "1.8 GB"],
      ["其餘 2 位合計", "1.5 GB"],
    ]);
  });

  it("under a measurement whose total is null, there is no bar and only the measured rows", () => {
    mountBreakdown({
      ...EMPTY,
      databaseBytes: kib(2417536),
      backupsBytes: kib(13349048),
      databaseMeasuredAt: NOW_S - 3 * 3600,
      diskFreeBytes: 412316860416,
    });
    expect(screen.queryByTestId("disk-usage-bar")).toBeNull();
    expect(swatches()).toEqual([]);
    expect(rows()).toEqual([
      ["資料庫", "2.3 GB"],
      ["備份", "12.7 GB"],
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
      mountBreakdown({ ...EMPTY, totalBytes: kib(4194304), databaseBytes: kib(1048576), otherBytes: kib(3145728) });
      expect(screen.getByRole("img").getAttribute("aria-label")).toBe(
        "OffiCraft disk usage by category: Database 1.0 GB (25%), Other 3.0 GB (75%)",
      );
    } finally {
      window.localStorage.removeItem("oc.language");
    }
  });
});
