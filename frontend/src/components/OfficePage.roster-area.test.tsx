// T-66a8 辦公室側欄 正職／外包 tab 切換 (owner mockup 2026-07-18). The old
// two-stacked-groups rail (正職 collapse header + 外包 panel head) is replaced
// by a TOP text-tab switcher. Locked here through the REAL OfficePage (mock
// adapter roster):
//   · each tab shows a red unread-count badge = that area's members' total
//     unread (0 → no badge);
//   · a count sub-line sits under each tab — 正職「N 人」, 外包「N 人 · 上限 M」
//     (the 上限 suffix omitted when settings are not loaded — honest);
//   · the 招攬新成員 button at the sidebar bottom routes by the active tab:
//     正職 → #settings/roles/new (角色誌 create mode), 外包 → the cap popover.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, waitFor, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import {
  __resetMock,
  __injectMockChat,
  __injectMockOutsourceWorker,
} from "../api/mock";

function renderOffice() {
  return render(
    <I18nProvider>
      <OfficePage />
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
});

describe("OfficePage — 正職 tab (T-66a8)", () => {
  it("shows the 「N 人」 count sub-line (seed roster = 1 assistant)", async () => {
    const { findByTestId } = renderOffice();
    const sub = await findByTestId("staff-tab-sub");
    await waitFor(() => expect(sub.textContent).toBe("1 人"));
  });

  it("renders NO unread badge when the 正職 area has no unread", async () => {
    const { findByTestId, queryByTestId } = renderOffice();
    // The count settles first…
    await findByTestId("staff-tab-sub");
    // …and with no injected inbound chat, the seed member's unread is 0 → the
    // red tab badge must not render at all (mockup: 0 → hidden).
    expect(queryByTestId("staff-tab-unread")).toBeNull();
  });

  it("sums the 正職 area unread into the tab badge when a member has unread", async () => {
    // Two inbound member→owner messages past the (absent) read watermark — the
    // seed member (Mira) accumulates unread the same way the roster badge does.
    for (const [id, body] of [
      ["m-1", "回報 1"],
      ["m-2", "回報 2"],
    ]) {
      __injectMockChat({
        id,
        from: "mira",
        to: "owner",
        body,
        ts: Date.now() / 1000 - 30,
        attachments: [],
        replyCardId: null,
      });
    }
    const { findByTestId } = renderOffice();
    const badge = await findByTestId("staff-tab-unread");
    await waitFor(() => expect(badge.textContent).toBe("2"));
  });
});

describe("OfficePage — 外包 tab count sub-line (T-66a8)", () => {
  it("shows 「N 人 · 上限 M」 when settings loaded (cap default 3, no workers)", async () => {
    const { findByTestId } = renderOffice();
    const sub = await findByTestId("outsource-tab-sub");
    // 外包 0 人 — the empty area still reads honestly (mock cap default 3).
    await waitFor(() => expect(sub.textContent).toBe("0 人 · 上限 3"));
  });

  it("counts live outsource workers in the 外包 tab sub-line", async () => {
    __injectMockOutsourceWorker({
      id: "ow-1",
      codename: "O-1",
      model: "Opus 4.6",
      effort: "high",
      taskId: "t-1",
      taskTitle: "",
      taskStatus: "in_progress",
      createdTs: Date.now() / 1000 - 600,
    });
    const { findByTestId } = renderOffice();
    const sub = await findByTestId("outsource-tab-sub");
    await waitFor(() => expect(sub.textContent).toBe("1 人 · 上限 3"));
  });
});

describe("OfficePage — 招攬新成員 routing (T-66a8 req 4)", () => {
  it("routes to #settings/roles/new (角色誌 create mode) on the 正職 tab", async () => {
    const { findByTestId } = renderOffice();
    const recruit = await findByTestId("office-recruit");
    expect(recruit.querySelector("svg")).not.toBeNull();

    fireEvent.click(recruit);
    // Through the single hash seam (a refresh restores it), straight into
    // CREATE mode — the same deep link the old 正職 ➕👤 button used.
    await waitFor(() =>
      expect(window.location.hash).toBe("#settings/roles/new")
    );
  });

  it("opens the 外包上限設定 popover on the 外包 tab (not a role route)", async () => {
    const { findByTestId, getByTestId, queryByTestId } = renderOffice();
    fireEvent.click(getByTestId("office-tab-outsource"));
    // No popover until the button is pressed.
    expect(queryByTestId("outsource-cap-pop")).toBeNull();
    fireEvent.click(await findByTestId("office-recruit"));
    // The cap popover opens; the hash never changes to a role route.
    expect(getByTestId("outsource-cap-pop").textContent).toContain(
      "外包上限設定"
    );
    expect(window.location.hash).toBe("");
  });
});
