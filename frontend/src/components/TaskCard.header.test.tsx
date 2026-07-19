// TaskCard — 卡頭對齊 owner spec (T-705e, supersedes T-e987's layout). Locked:
//   1. No avatar (the old person-icon / briefcase circle is gone).
//   2. task id is a bordered mono badge "☑ #T-xxxx" (checkbox glyph + #no), not
//      the old "[T-xxxx]" plain-text prefix.
//   3. 負責人 / 建立者 chat-bubble icon rides INSIDE the value chip, after the
//      name — never in front of the row label.
//   4. the executor/creator/type/key VALUES are chips (pill), not flat text; a
//      member is the bare name (no "· 成員" — the 負責人 label already says it).
//   5. every value row is preceded by its field label (任務類型 / 負責人 /
//      建立者 / 識別鍵) in a shared max-content label column (the .task-card__meta
//      grid) so the chips line up.
//   Jump behaviour is unchanged from T-e987 (verified below): 類型 chip →
//   #settings/manuals/<typeKey>; 負責人/建立者 chip → that peer's chat compose.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import {
  __resetMock,
  __injectMockTask,
  __injectMockTaskType,
  __injectMockOutsourceWorker,
} from "../api/mock";
import type { TaskView } from "../api/adapter";

// Released-worker codename cache (T-3ed8): the REAL hook lazily fetches
// GET /api/outsource-workers/{id} (which serves released rows); here it is a
// fixed map (the hook has its own tests) — only "ow-rel" resolves, so the
// unresolvable-raw-id cases below stay honest.
vi.mock("../hooks/useWorkerCodenames", () => ({
  useWorkerCodenames: (ids: readonly string[]) =>
    new Map(ids.filter((id) => id === "ow-rel").map((id) => [id, "R-2"])),
}));

let seq = 0;

function mkTask(over: Partial<TaskView>): TaskView {
  seq += 1;
  return {
    id: `task-${seq}`,
    taskNo: `T-${1000 + seq}`,
    title: `任務 ${seq}`,
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "member",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: Date.now() / 1000 - 3600,
    updatedTs: Date.now() / 1000 - 60,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    ...over,
  };
}

function renderPage() {
  return render(
    <I18nProvider>
      <TasksPage />
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

describe("TaskCard 卡頭對齊 owner spec (T-705e)", () => {
  it("drops the avatar entirely", async () => {
    __injectMockTask(mkTask({ title: "無頭像" }));
    __injectMockTask(
      mkTask({ title: "外包無頭像", executorKind: "outsource", executorId: "" })
    );
    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    for (const card of cards) {
      expect(card.querySelector(".task-card__avatar")).toBeNull();
      expect(card.querySelector(".task-card__outsource-avatar")).toBeNull();
    }
  });

  it("renders the task id as a #T-xxxx badge with the checkbox glyph, on the badge row", async () => {
    const task = mkTask({ title: "識別碼徽章" });
    __injectMockTask(task);
    const { findByTestId } = renderPage();
    const badge = await findByTestId("task-no");
    // #T-xxxx, never the old bracketed prefix.
    expect(badge.textContent).toContain(`#${task.taskNo}`);
    expect(badge.textContent).not.toContain("[");
    expect(badge.classList.contains("task-card__id-badge")).toBe(true);
    // the ☑ glyph is an inline svg inside the badge.
    expect(badge.querySelector("svg")).toBeTruthy();
    // v2 (owner 2026-07-17): the badge lives on the fixed badge row between
    // the priority chip and the status badge — no field label, not on the
    // title line, not in the meta stack.
    expect(badge.closest(".task-card__badge-row")).toBeTruthy();
    expect(badge.closest(".task-card__title-line")).toBeNull();
    expect(badge.closest(".task-card__meta")).toBeNull();
  });

  it("#no and 識別鍵 live in the head; the dep marker keeps its own block", async () => {
    const blocker = mkTask({ title: "擋路的", taskNo: "T-7d40" });
    __injectMockTask(blocker);
    const task = mkTask({ title: "有識別鍵的", dedupeKey: "PO-42", deps: [blocker.id] });
    __injectMockTask(task);

    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    const card = cards.find((c) =>
      c.querySelector(".task-card__title")?.textContent?.includes("有識別鍵的")
    )!;

    // 識別鍵 value shown in the head (non-URL → plain span chip).
    expect(card.querySelector('[data-testid="task-key"]')?.textContent).toBe(
      "PO-42"
    );
    // The dep marker still says who is blocking (v6 restyled it — see
    // TaskCard.t17be.test.tsx — but the text is unchanged).
    expect(card.querySelector('[data-testid="task-dep"]')?.textContent).toBe(
      "等 T-7d40"
    );
  });

  it("shows every value under its field label in the shared label column", async () => {
    __injectMockTaskType({ typeKey: "sync-jira", displayName: "", purpose: "" });
    __injectMockTask(
      mkTask({
        title: "四欄齊全",
        typeKey: "sync-jira",
        // executorId's default is "mira"; a DIFFERENT creator keeps the 建立者
        // row on screen. Same-id would legitimately hide it (T-17be) — this
        // test is about the label column, not about that rule.
        creatorId: "owner",
        dedupeKey: "PROJ-1421",
      })
    );
    const { findByTestId } = renderPage();
    const meta = (await findByTestId("task-card")).querySelector(
      ".task-card__meta"
    )!;
    expect(meta).toBeTruthy();
    // The field labels are present in the grid (equal-width column) — the id
    // badge left the stack for the badge row (v2), and 任務類型 followed it
    // there (v6/T-17be), so 負責人 now leads.
    const labels = Array.from(
      meta.querySelectorAll(".task-card__meta-label")
    ).map((n) => n.textContent);
    expect(labels).toEqual(["負責人", "建立者", "識別鍵"]);
  });

  it("renders a URL 識別鍵 as an external-link chip in the head", async () => {
    __injectMockTask(
      mkTask({ title: "連結鍵", dedupeKey: "https://github.com/x/y/pull/9" })
    );
    const { findByTestId } = renderPage();
    const link = await findByTestId("task-key-link");
    expect(link.getAttribute("href")).toBe("https://github.com/x/y/pull/9");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.classList.contains("task-card__chip")).toBe(true);
  });

  it("任務類型 value is a chip with the gear inside; clicking jumps to the type's settings hub; ad-hoc is plain", async () => {
    __injectMockTaskType({ typeKey: "review-pr", displayName: "", purpose: "" });
    __injectMockTask(mkTask({ title: "有類型", typeKey: "review-pr" }));
    __injectMockTask(mkTask({ title: "自由代辦", typeKey: "" }));

    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    const byTitle = (title: string) =>
      cards.find((c) =>
        c.querySelector(".task-card__title")?.textContent?.includes(title)
      )!;

    // ad-hoc: plain chip, not a link.
    const adhoc = byTitle("自由代辦");
    const adhocRow = adhoc.querySelector('[data-testid="task-type-row"]')!;
    expect(adhocRow).toBeTruthy();
    expect(adhoc.querySelector('[data-testid="task-type-link"]')).toBeNull();
    // No settings page to jump to → no gear; otherwise the chip advertises a
    // click that does nothing (T-0f7c).
    expect(adhocRow.tagName).not.toBe("BUTTON");
    expect(adhocRow.querySelector("svg")).toBeNull();

    // typed: a badge carrying the gear icon that deep-links into settings.
    const typed = byTitle("有類型");
    const link = typed.querySelector<HTMLButtonElement>(
      '[data-testid="task-type-link"]'
    )!;
    // v6 (T-17be): a coloured .task-badge on row 1, no longer the neutral
    // .task-card__chip in the meta grid.
    expect(link.classList.contains("task-badge")).toBe(true);
    expect(link.querySelector("svg")).toBeTruthy(); // gear inside the badge
    fireEvent.click(link);
    expect(window.location.hash).toBe("#settings/manuals/review-pr");
  });

  it("任務類型 chip shows the manual's DISPLAY name; a keyless manual / deleted manual falls back to the raw key (T-fa76)", async () => {
    __injectMockTaskType({
      typeKey: "tm-aaaabbbbcccc",
      displayName: "審查 PR",
      purpose: "",
    });
    __injectMockTask(mkTask({ title: "有顯示名", typeKey: "tm-aaaabbbbcccc" }));
    __injectMockTask(mkTask({ title: "手冊已刪", typeKey: "gone-type" }));

    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    const byTitle = (title: string) =>
      cards.find((c) =>
        c.querySelector(".task-card__title")?.textContent?.includes(title)
      )!;

    // Display name in the chip text; the system key stays out of the UI.
    const named = byTitle("有顯示名").querySelector(
      '[data-testid="task-type"]'
    )!;
    expect(named.textContent).toBe("審查 PR");
    expect(named.getAttribute("title")).toBeNull();

    // No manual to resolve → honest raw-key fallback (jump still works).
    const gone = byTitle("手冊已刪").querySelector(
      '[data-testid="task-type"]'
    )!;
    expect(gone.textContent).toBe("gone-type");
  });

  it("負責人 chip shows the bare member name + chat icon, and opens the assignee's chat seeded with the task prefix", async () => {
    const task = mkTask({ title: "傳給負責人", executorId: "mira" });
    __injectMockTask(task);
    const { findByTestId } = renderPage();
    const link = await findByTestId("task-assignee-link");
    // bare name — the old "· 成員" role suffix is gone (the label carries it).
    expect(
      link.querySelector('[data-testid="task-executor"]')?.textContent
    ).toBe("Mira");
    expect(link.classList.contains("task-card__chip")).toBe(true);
    expect(link.querySelector("svg")).toBeTruthy(); // chat bubble inside the chip
    fireEvent.click(link);
    expect(window.location.hash).toBe(
      `#office/chat/mira/compose/${encodeURIComponent(task.taskNo)}`
    );
  });

  it("外包 executor keeps its 代號 · 模型 · 投入度 chip", async () => {
    const task = mkTask({
      title: "外包負責",
      executorKind: "outsource",
      executorId: "ow-7",
    });
    __injectMockTask(task);
    __injectMockOutsourceWorker({
      id: "ow-7",
      codename: "O-7",
      model: "Opus 4.6",
      effort: "high",
      taskId: task.id,
    });
    const { findByTestId } = renderPage();
    const link = await findByTestId("task-assignee-link");
    expect(
      link.querySelector('[data-testid="task-executor"]')?.textContent
    ).toBe("外包 O-7 · Opus 4.6 · 高投入");
  });

  it("released 外包 executor resolves its codename via the lazy cache", async () => {
    // "ow-rel" has NO live worker row (released) — the chip resolves the
    // codename through the per-id cache instead of the bare 外包 label.
    __injectMockTask(
      mkTask({
        title: "外包已釋出",
        executorKind: "outsource",
        executorId: "ow-rel",
      })
    );
    const { findByTestId } = renderPage();
    const link = await findByTestId("task-assignee-link");
    expect(
      link.querySelector('[data-testid="task-executor"]')?.textContent
    ).toBe("外包 R-2");
  });

  it("released 前任 resolves its codename via the lazy cache, not the raw id", async () => {
    __injectMockTask(
      mkTask({
        title: "轉派後",
        reassignedFrom: "ow-rel",
        reassignedFromKind: "outsource",
      })
    );
    const { findByTestId } = renderPage();
    const chip = await findByTestId("task-previous-assignee");
    expect(chip.textContent).toBe("外包 R-2");
  });

  it("未指派 assignee chip is plain text (no chat to open, no icon)", async () => {
    __injectMockTask(
      mkTask({ title: "未指派", executorKind: "outsource", executorId: "" })
    );
    const { findByTestId } = renderPage();
    const row = await findByTestId("task-assignee-row");
    expect(row.tagName).not.toBe("BUTTON");
    expect(row.querySelector("svg")).toBeNull();
    expect(
      row.querySelector('[data-testid="task-executor"]')?.textContent
    ).toBe("未指派");
  });

  // ── spec ① 兩排對調 (owner 2026-07-17「這兩排應該要對調」) ───────────────
  // Owner picked the A version off the A/B proposal, OVERRULING the B version a
  // previous round had recommended — and "B was recommended" is written down in
  // the history, so a later reader is one plausible-looking edit away from
  // putting the title back on top. Everything else about the head was already
  // pinned, but NOTHING asserted the one thing spec ① actually is: the badge row
  // comes FIRST, the title SECOND.
  //
  // The existing anchor pins (status-menu.test.tsx) only compare siblings INSIDE
  // .task-card__head-top — so hoisting .task-card__title-line back above
  // .task-card__head-top (i.e. restoring the v2 order: title on top, badges
  // under it) leaves head-top's children untouched and every one of them green.
  // These assertions therefore compare at the .task-card__head level, which is
  // the level the swap happens on.
  describe("spec ① 兩排對調: badge 列第一排、標題第二排", () => {
    const assertBadgeRowLeadsTitle = (card: Element) => {
      const head = card.querySelector(".task-card__head")!;
      const badgeRow = card.querySelector(".task-card__badge-row")!;
      const title = card.querySelector(".task-card__title")!;

      // Positive control — an order assertion over an empty/mis-scoped
      // selection is vacuously true forever. Prove the scope is real BEFORE
      // reading anything into the ordering below.
      expect(head).toBeTruthy();
      expect(badgeRow).toBeTruthy();
      expect(title).toBeTruthy();
      expect(head.contains(badgeRow)).toBe(true);
      expect(head.contains(title)).toBe(true);
      // …and that they are genuinely two separate rows. Nesting the title
      // inside the badge row would also satisfy DOCUMENT_POSITION_FOLLOWING,
      // so exclude containment explicitly rather than trusting the bitmask.
      expect(badgeRow.contains(title)).toBe(false);
      expect(title.closest(".task-card__badge-row")).toBeNull();

      // THE spec: badge row precedes the title in the head. Compare the head's
      // own children (the rows), not the leaves — that is the axis the v2→v4
      // swap moved.
      const rowOf = (n: Element) =>
        [...head.children].find((c) => c === n || c.contains(n))!;
      const rows = [...head.children];
      expect(rows.indexOf(rowOf(badgeRow))).toBeGreaterThanOrEqual(0);
      expect(rows.indexOf(rowOf(badgeRow))).toBeLessThan(
        rows.indexOf(rowOf(title))
      );
      expect(
        badgeRow.compareDocumentPosition(title) &
          Node.DOCUMENT_POSITION_FOLLOWING
      ).toBeTruthy();
      // And the v2 wrapper that used to hold the title on row 1 is gone for
      // good — its return is the shape the regression takes.
      expect(card.querySelector(".task-card__headings")).toBeNull();
    };

    it("a live card leads with the badge row and drops the title to row 2", async () => {
      __injectMockTask(mkTask({ title: "對調的活卡" }));
      const { findByTestId } = renderPage();
      assertBadgeRowLeadsTitle(await findByTestId("task-card"));
    });

    it("holds the swapped order while expanded", async () => {
      __injectMockTask(mkTask({ title: "展開仍對調" }));
      const { findByTestId } = renderPage();
      const card = await findByTestId("task-card");
      fireEvent.click(card.querySelector(".task-card__title")!);
      expect(card.getAttribute("aria-expanded")).toBe("true");
      assertBadgeRowLeadsTitle(card);
    });

    it("the ☑ #T-xxxx badge — the row's leader — sits above the title, not under it", async () => {
      const task = mkTask({ title: "編號在標題上面" });
      __injectMockTask(task);
      const { findByTestId } = renderPage();
      const card = await findByTestId("task-card");
      const badge = card.querySelector('[data-testid="task-no"]')!;
      const title = card.querySelector(".task-card__title")!;
      expect(badge).toBeTruthy();
      expect(title).toBeTruthy();
      expect(badge.contains(title)).toBe(false);
      expect(
        badge.compareDocumentPosition(title) & Node.DOCUMENT_POSITION_FOLLOWING
      ).toBeTruthy();
      // The whole badge row — 編號 · 優先權 · 狀態 — is above the title, not
      // just its first chip.
      for (const id of ["task-no", "task-priority", "task-status"]) {
        const chip = card.querySelector(`[data-testid="${id}"]`)!;
        expect(chip).toBeTruthy();
        expect(
          chip.compareDocumentPosition(title) &
            Node.DOCUMENT_POSITION_FOLLOWING
        ).toBeTruthy();
      }
    });
  });

  describe("建立者 resolution + fallback", () => {
    it('empty creator (old task) renders "—", not clickable', async () => {
      __injectMockTask(mkTask({ title: "老任務", creatorId: "" }));
      const { findByTestId } = renderPage();
      const row = await findByTestId("task-creator-row");
      expect(row.tagName).not.toBe("BUTTON");
      expect(
        row.querySelector('[data-testid="task-creator"]')?.textContent
      ).toBe("—");
    });

    it("a roster-member creator shows the name and opens their chat", async () => {
      // executorId must differ from creatorId or T-17be hides the row (that
      // rule has its own test); "Mira 建立" is about resolution + the jump.
      const task = mkTask({
        title: "Mira 建立",
        creatorId: "mira",
        executorId: "someone-else",
      });
      __injectMockTask(task);
      const { findByTestId } = renderPage();
      const link = await findByTestId("task-creator-link");
      expect(
        link.querySelector('[data-testid="task-creator"]')?.textContent
      ).toBe("Mira");
      fireEvent.click(link);
      expect(window.location.hash).toBe(
        `#office/chat/mira/compose/${encodeURIComponent(task.taskNo)}`
      );
    });

    it("a live outsource-worker creator shows 外包 代號 and is clickable", async () => {
      const task = mkTask({ title: "外包建立", creatorId: "ow-3" });
      __injectMockTask(task);
      __injectMockOutsourceWorker({
        id: "ow-3",
        codename: "O-3",
        model: "Opus 4.6",
        effort: "high",
        taskId: "some-other-task",
      });
      const { findByTestId } = renderPage();
      const link = await findByTestId("task-creator-link");
      expect(
        link.querySelector('[data-testid="task-creator"]')?.textContent
      ).toBe("外包 O-3");
      fireEvent.click(link);
      expect(window.location.hash).toBe("#office/chat/ow-3/compose/" +
        encodeURIComponent(task.taskNo));
    });

    it("a released outsource-worker creator resolves its codename via the lazy cache", async () => {
      const task = mkTask({ title: "已釋出建立", creatorId: "ow-rel" });
      __injectMockTask(task); // no live worker row — the codename cache resolves
      const { findByTestId } = renderPage();
      const link = await findByTestId("task-creator-link");
      expect(
        link.querySelector('[data-testid="task-creator"]')?.textContent
      ).toBe("外包 R-2");
    });

    it("an UNRESOLVABLE outsource creator falls back to the raw id, still clickable", async () => {
      const task = mkTask({ title: "查無建立者", creatorId: "ow-9" });
      __injectMockTask(task); // neither live nor cache-resolvable → raw id
      const { findByTestId } = renderPage();
      const link = await findByTestId("task-creator-link");
      expect(
        link.querySelector('[data-testid="task-creator"]')?.textContent
      ).toBe("ow-9");
    });

    it('a non-member/non-worker creator (e.g. "owner") is plain text, not clickable', async () => {
      __injectMockTask(mkTask({ title: "老闆建立", creatorId: "owner" }));
      const { findByTestId } = renderPage();
      const row = await findByTestId("task-creator-row");
      expect(row.tagName).not.toBe("BUTTON");
      expect(
        row.querySelector('[data-testid="task-creator"]')?.textContent
      ).toBe("owner");
    });
  });
});
