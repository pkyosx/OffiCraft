// T-17be — the three specs that live in the DOM/text layer and are therefore
// actually testable under vitest+jsdom. Locked here:
//
//   ① 負責人 == 建立者 → the 建立者 row is gone. The rule compares executorId
//     to creatorId — IDS, never display names (two members can share a name;
//     a name match would hide a genuinely different creator). Empty is not an
//     identity: creatorId "" still renders 「—」 even when executorId is ""
//     too.
//   ② The 任務類型 chip is on ROW 1 (.task-card__badge-row) as a coloured
//     .task-badge beside 優先權/狀態 — not in the .task-card__meta grid, not a
//     neutral .task-card__chip. Its 齒輪 deep-link survived the move.
//   ③ zh filterExecutorAll reads 「所有負責人」, not the ambiguous 「所有人」.
//
// Plus a behavioural guard the icon swap could have silently broken:
//   ④ The expand chevron is an INDICATOR, not a control — clicking it still
//     expands the card through the whole-card toggle. (T-bc90 called this out
//     specifically: an icon is easy to "improve" into a <button>, which would
//     make closest() veto the toggle and the click do nothing.)
//
// ── WHAT THIS FILE CANNOT SEE ─────────────────────────────────────────────
// vite.config.ts sets environment:"jsdom", which parses no stylesheet and
// computes no layout. So every LOOK/FIT question in T-17be — "is the badge
// actually teal", "does the type name wrap at 390px", "is the dep block blue
// rather than purple", "is the chevron 18px" — is INVISIBLE here. Those were
// settled on the owner-approved 390/1280 screenshots
// (kyle-17be-shots/candA-badge-wrap-*), and that is the only evidence for
// them. Nothing below pretends otherwise: these assertions are about STRUCTURE
// (which element, which parent, which text, which class), which is exactly the
// half jsdom can prove. A decorative assertion that can never go red would be
// worse than no assertion — it would launder an unverified claim as a test.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { TaskCard } from "./TaskCard";
import {
  __resetMock,
  __injectMockTask,
  __injectMockTaskType,
} from "../api/mock";
import type { Member } from "../types";
import type { TaskView, OutsourceWorkerView } from "../api/adapter";

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

/** Render ONE TaskCard directly against a hand-built roster. The mock API has
 * no member-injection hook and ships a single member, so this is the only way
 * to build the roster the id-vs-name test needs: two DIFFERENT members who
 * share a display name. */
function renderCardWithRoster(task: TaskView, members: Member[]) {
  const noop = async () => {};
  const workers: OutsourceWorkerView[] = [];
  return render(
    <I18nProvider>
      <TaskCard
        task={task}
        allTasks={[task]}
        members={members}
        workers={workers}
        nowTs={Date.now() / 1000}
        onTerminate={noop as never}
        onMarkDuplicate={noop as never}
        onSetPriority={noop as never}
        onReassign={noop as never}
        onSendMessage={noop as never}
        onHydrate={(async () => task) as never}
      />
    </I18nProvider>
  );
}

const byTitle = (cards: HTMLElement[], title: string) =>
  cards.find((c) =>
    c.querySelector(".task-card__title")?.textContent?.includes(title)
  )!;

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

// ── ① 負責人 == 建立者 → 藏掉建立者列 ──────────────────────────────────────
describe("T-17be ① 負責人 == 建立者 時藏掉建立者列", () => {
  it("hides the 建立者 row when executorId === creatorId (owner's actual card)", async () => {
    __injectMockTask(
      mkTask({ title: "同一人", executorId: "mira", creatorId: "mira" })
    );
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");

    // POSITIVE CONTROL, and not optional. Every assertion in this test is a
    // negative ("the 建立者 row is absent"), and absence is exactly what a
    // broken render, a crashed card, or a mistyped selector also produce. So
    // first prove the card rendered and the row ABOVE the missing one is
    // really there — otherwise "建立者 is gone" is green on a blank page.
    expect(card.querySelector('[data-testid="task-assignee-link"]')).toBeTruthy();
    expect(
      card.querySelector('[data-testid="task-executor"]')?.textContent
    ).toBe("Mira");

    // The spec.
    expect(card.querySelector('[data-testid="task-creator-row"]')).toBeNull();
    expect(card.querySelector('[data-testid="task-creator-link"]')).toBeNull();
    expect(card.querySelector('[data-testid="task-creator"]')).toBeNull();

    // The label goes too — a bare 「建立者」 with no value would be worse than
    // the duplication it replaced. Asserted as the EXACT label list (a bare
    // not.toContain("建立者") would stay green if the whole meta grid vanished;
    // this cannot).
    const labels = Array.from(
      card.querySelectorAll(".task-card__meta-label")
    ).map((n) => n.textContent);
    expect(labels).toEqual(["負責人"]);
  });

  it("keeps the 建立者 row when the ids differ but the DISPLAY NAMES are identical", async () => {
    // THE test for "id, not name". Two different members who are both called
    // "Mira": the rendered assignee and creator strings are indistinguishable,
    // so a rule written against the display name (or against `creator.text`,
    // which is one plausible refactor away) hides a real, different creator —
    // the card would silently drop a fact. Only an id compare survives this.
    const roster = [
      { id: "mira", name: "Mira", kind: "agent" },
      { id: "mira-2", name: "Mira", kind: "agent" },
    ] as unknown as Member[];
    const { getByTestId, queryByTestId } = renderCardWithRoster(
      mkTask({ title: "同名不同人", executorId: "mira", creatorId: "mira-2" }),
      roster
    );

    // Positive control: the two rows really do render the SAME string, so the
    // trap this test sets is armed. If these ever differ, the test below stops
    // being about names and quietly weakens.
    expect(getByTestId("task-executor").textContent).toBe("Mira");
    expect(getByTestId("task-creator").textContent).toBe("Mira");

    // …and despite the identical text, the row stays: the ids differ.
    expect(queryByTestId("task-creator-link")).toBeTruthy();
    const labels = Array.from(
      document.querySelectorAll(".task-card__meta-label")
    ).map((n) => n.textContent);
    expect(labels).toEqual(["負責人", "建立者"]);
  });

  it('keeps the 「—」 row when creatorId is "" (T-5012-shaped task) — empty is not an identity', async () => {
    __injectMockTask(
      mkTask({ title: "沒有建立者", executorId: "mira", creatorId: "" })
    );
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");

    const row = card.querySelector('[data-testid="task-creator-row"]')!;
    expect(row).toBeTruthy();
    // toBe on the bare word, never not.toContain(…): the row must SAY 「—」.
    expect(
      row.querySelector('[data-testid="task-creator"]')?.textContent
    ).toBe("—");
  });

  it('keeps the 「—」 row when BOTH creatorId and executorId are "" (unassigned 外包)', async () => {
    // The collision the `!== ""` guard exists for: "" === "" is true, so a
    // naive id compare would eat this row.
    __injectMockTask(
      mkTask({
        title: "未指派又無建立者",
        executorKind: "outsource",
        executorId: "",
        creatorId: "",
      })
    );
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");

    expect(
      card.querySelector('[data-testid="task-executor"]')?.textContent
    ).toBe("未指派");
    expect(
      card
        .querySelector('[data-testid="task-creator-row"]')
        ?.querySelector('[data-testid="task-creator"]')?.textContent
    ).toBe("—");
  });

  it("hides the row for an 外包 executor who is also the creator (id match across kinds)", async () => {
    __injectMockTask(
      mkTask({
        title: "外包自建",
        executorKind: "outsource",
        executorId: "ow-3",
        creatorId: "ow-3",
      })
    );
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    // Positive control first — the assignee row proves the card rendered.
    expect(card.querySelector('[data-testid="task-executor"]')).toBeTruthy();
    expect(card.querySelector('[data-testid="task-creator"]')).toBeNull();
  });

  it("keeps the 建立者 row for an 外包 executor created by a MEMBER (T-184c's boundary)", async () => {
    __injectMockTask(
      mkTask({
        title: "外包執行成員建立",
        executorKind: "outsource",
        executorId: "ow-3",
        creatorId: "mira",
      })
    );
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    expect(
      card.querySelector('[data-testid="task-creator"]')?.textContent
    ).toBe("Mira");
  });
});

// ── ② task type chip 在第一排 ──────────────────────────────────────────────
describe("T-17be ② 任務類型 chip 移到第一排、改有色 badge", () => {
  it("puts the type badge on the badge row, out of the meta grid, as a .task-badge", async () => {
    __injectMockTaskType({
      typeKey: "develop-officraft",
      displayName: "",
      purpose: "",
    });
    __injectMockTask(
      mkTask({ title: "類型上第一排", typeKey: "develop-officraft" })
    );
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    const type = card.querySelector('[data-testid="task-type"]')!;

    // Positive control: the badge is really rendering the type name. Without
    // this, every "it is on the badge row" assertion below would also pass on
    // an empty badge.
    expect(type.textContent).toBe("develop-officraft");

    const badge = card.querySelector('[data-testid="task-type-link"]')!;
    // THE spec: row 1, beside 優先權/狀態 — and provably NOT in the meta grid.
    expect(badge.closest(".task-card__badge-row")).toBeTruthy();
    expect(badge.closest(".task-card__meta")).toBeNull();
    // Same badge vocabulary as its row-mates, not the old neutral chip.
    expect(badge.classList.contains("task-badge")).toBe(true);
    expect(badge.classList.contains("task-card__chip")).toBe(false);

    // It shares the row with the id/priority/status badges — asserted by
    // common parent, so "on the badge row" cannot be satisfied by some second
    // badge row that a refactor invents.
    const row = card.querySelector(".task-card__badge-row")!;
    for (const id of ["task-no", "task-priority", "task-status"]) {
      expect(row.contains(card.querySelector(`[data-testid="${id}"]`)!)).toBe(
        true
      );
    }
    expect(row.contains(badge)).toBe(true);
  });

  it("keeps the 齒輪 deep-link into the type's settings hub after the move", async () => {
    __injectMockTaskType({ typeKey: "review-pr", displayName: "", purpose: "" });
    __injectMockTask(mkTask({ title: "齒輪還在", typeKey: "review-pr" }));
    const { findByTestId } = renderPage();
    const link = await findByTestId("task-type-link");
    expect(link.querySelector("svg")).toBeTruthy(); // the gear rode along
    fireEvent.click(link);
    expect(window.location.hash).toBe("#settings/manuals/review-pr");
  });

  it("shows a LONG type name in full — no truncation, no ellipsis (owner accepted the wrap)", async () => {
    // 候選 B (truncate to 6 chars +「…」) was measured and STILL wrapped at
    // 390px, so it cost the name and bought nothing; owner picked 候選 A and
    // accepted the wrap. jsdom cannot see the wrap — but it CAN see that the
    // name is intact, which is the half of that ruling a regression would eat.
    const long = "develop-officraft";
    __injectMockTaskType({ typeKey: long, displayName: "", purpose: "" });
    __injectMockTask(mkTask({ title: "長類型名", typeKey: long }));
    const { findByTestId } = renderPage();
    const type = (await findByTestId("task-card")).querySelector(
      '[data-testid="task-type"]'
    )!;
    expect(type.textContent).toBe(long); // whole name, exactly
    expect(type.textContent).not.toContain("…");
  });

  it("shows a SHORT type name on the same badge row (the name length is not a branch)", async () => {
    __injectMockTaskType({ typeKey: "dev", displayName: "", purpose: "" });
    __injectMockTask(mkTask({ title: "短類型名", typeKey: "dev" }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    const type = card.querySelector('[data-testid="task-type"]')!;
    expect(type.textContent).toBe("dev");
    expect(
      card
        .querySelector('[data-testid="task-type-link"]')!
        .closest(".task-card__badge-row")
    ).toBeTruthy();
  });

  it("an ad-hoc task's type badge is plain: no gear, not a button, still on row 1", async () => {
    __injectMockTask(mkTask({ title: "自由代辦", typeKey: "" }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    const row = card.querySelector('[data-testid="task-type-row"]')!;
    expect(row).toBeTruthy();
    // POSITIVE CONTROL (review 2026-07-17): every other assertion here is a
    // negative — not a BUTTON, no gear, no deep-link — and all three stay
    // green on an EMPTY badge. Without this the ad-hoc case could render a
    // bare teal badge with no text on row 1 and the suite would not notice
    // (proved: emptying t.tasks.adhoc survived all 490 tests before this).
    expect(row.textContent).toBe("自由代辦");
    expect(row.tagName).not.toBe("BUTTON");
    expect(row.querySelector("svg")).toBeNull(); // no gear → no dead affordance
    expect(row.closest(".task-card__badge-row")).toBeTruthy();
    expect(card.querySelector('[data-testid="task-type-link"]')).toBeNull();
  });
});

// ── ③ filter 文案 ─────────────────────────────────────────────────────────
describe("T-17be ③ 執行者篩選文案「所有負責人」", () => {
  it("labels the executor filter 「所有負責人」 — the bare word, asserted positively", async () => {
    __injectMockTask(mkTask({ title: "篩選列" }));
    const { findByTestId } = renderPage();
    const filter = await findByTestId("filter-executor");

    // toBe on the whole rendered label, NOT not.toContain("所有人"): the
    // ambiguous string is a SUBSTRING of the correct one, so a "did it change"
    // negative would be red on the right answer and green on a deleted label.
    // Pin what it must SAY.
    expect(filter.textContent?.trim()).toBe("所有負責人");
  });

  it("leaves 所有類型 / 所有狀態 alone — the ambiguity was 「所有人」's alone", async () => {
    // Same-shape scan, written down so a later reader does not "finish the
    // job" by renaming these too: 所有類型/所有狀態 already name the noun they
    // filter, so they never had the 所有人 = 「所有的人」/「所有權人」 double
    // reading.
    __injectMockTask(mkTask({ title: "其它篩選" }));
    const { findByTestId } = renderPage();
    expect((await findByTestId("filter-type")).textContent?.trim()).toBe(
      "所有類型"
    );
    // 狀態 opens with 4 statuses preselected, so its trigger shows the
    // multi-select summary (「狀態 · 4」) rather than the all-label. Clear the
    // filters to make it fall back to the label this test is about.
    fireEvent.click(await findByTestId("clear-filters"));
    expect((await findByTestId("filter-status")).textContent?.trim()).toBe(
      "所有狀態"
    );
  });
});

// ── ④ 三角形換圖示後,「點三角形 = 展開卡片」不能壞 ─────────────────────────
describe("T-17be ④ 展開圖示仍是純指示、不是控制項", () => {
  it("clicking the chevron ICON still expands the card (it must not become a button)", async () => {
    __injectMockTask(mkTask({ title: "點圖示會展開" }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    const mark = await findByTestId("task-expand-mark");

    // Re-query every time: toggling swaps ChevronRight for ChevronDown, so
    // React REPLACES the svg node. A reference captured before the click goes
    // detached, and a click on a detached node bubbles to nothing — which
    // would look exactly like "the icon stopped toggling the card".
    const icon = () => mark.querySelector("svg")!;

    // Positive control: prove we are clicking the ICON at the settings-page
    // size, not the old text glyph — otherwise this test would pass unchanged
    // on the pre-T-17be ▸ and prove nothing about the swap.
    expect(icon()).toBeTruthy();
    expect(icon().getAttribute("width")).toBe("18");

    // Still not a control: no button/role of its own, so the card's closest()
    // interaction filter finds only the CARD and lets the click through.
    expect(mark.closest("button, [role='button']")).toBe(card);
    expect(mark.getAttribute("aria-hidden")).toBe("true");

    // THE behaviour: click the icon itself → the card expands.
    expect(card.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(icon());
    expect(card.getAttribute("aria-expanded")).toBe("true");
    // The EXPANDED chevron is a different component (ChevronDown), and its
    // size was unguarded: the width check above only ever saw ChevronRight,
    // so ChevronDown size={18}→{11} survived the whole suite (review
    // 2026-07-17). Both halves of the swap must hold the settings-page size,
    // or the icon shrinks the moment the card opens.
    expect(icon().getAttribute("width")).toBe("18");
    fireEvent.click(icon());
    expect(card.getAttribute("aria-expanded")).toBe("false");
    expect(icon().getAttribute("width")).toBe("18");
  });
});

// ── deps: in_progress AND blocked coexist; deps never join row 1 ───────────
describe("T-17be deps 區塊: 不是 status、不上第一排", () => {
  it("an in_progress card blocked by another shows BOTH — and the dep stays off the badge row", async () => {
    const blocker = mkTask({ title: "擋路的", taskNo: "T-70fb" });
    __injectMockTask(blocker);
    __injectMockTask(
      mkTask({
        title: "進行中又被擋",
        status: "in_progress",
        deps: [blocker.id],
      })
    );
    const { findAllByTestId } = renderPage();
    const card = byTitle(await findAllByTestId("task-card"), "進行中又被擋");

    // Both facts are on the card at once — that is the whole reason deps are
    // not a status. The status badge still says 進行中 (positive control: the
    // dep did not overwrite it)…
    expect(
      card.querySelector('[data-testid="task-status"]')?.textContent
    ).toBe("進行中");
    // …and the dep block says who is blocking.
    const dep = card.querySelector('[data-testid="task-dep"]')!;
    // T-1d82 split the row into 編號 + 標題 spans; T-17be's claim is about the
    // 編號 being present and off row 1, so it reads the 編號 span.
    expect(dep.querySelector(".task-card__dep-no")?.textContent).toBe(
      "等 T-70fb"
    );

    // 🔴 The line owner drew: the dep is NOT a row-1 badge. Standing it beside
    // the real 狀態 badge would make the card claim two statuses (候選 C —
    // rendered, shown, not picked).
    expect(dep.closest(".task-card__badge-row")).toBeNull();
    expect(dep.classList.contains("task-badge")).toBe(false);
    const row = card.querySelector(".task-card__badge-row")!;
    expect(row.contains(dep)).toBe(false);
    expect(row.textContent).not.toContain("等 T-70fb");
  });

  it("speaks the waiting block's visual language (⏱ + block), not the old mono key chip", async () => {
    const blocker = mkTask({ title: "擋路的2", taskNo: "T-70fc" });
    __injectMockTask(blocker);
    __injectMockTask(mkTask({ title: "被擋", deps: [blocker.id] }));
    const { findAllByTestId } = renderPage();
    const card = byTitle(await findAllByTestId("task-card"), "被擋");
    const dep = card.querySelector('[data-testid="task-dep"]')!;

    // Positive control before the negatives: it renders and says the right
    // thing.
    expect(dep.querySelector(".task-card__dep-no")?.textContent).toBe(
      "等 T-70fc"
    );
    // It borrows waiting's block shape + clock icon…
    expect(dep.classList.contains("task-card__waiting")).toBe(true);
    expect(dep.querySelector("svg")).toBeTruthy();
    // …via the dep MODIFIER, which is what carries the blue that keeps it from
    // impersonating the purple waiting_external STATUS. jsdom cannot see the
    // colour — it can see that the class carrying it is applied, which is the
    // part a refactor drops. (The colour itself: candA screenshots.)
    expect(dep.classList.contains("task-card__waiting--dep")).toBe(true);
    // The old grey mono chip is gone for good.
    expect(dep.classList.contains("task-key")).toBe(false);
    expect(dep.classList.contains("task-key--dep")).toBe(false);
    // A blocked card is NOT thereby waiting_external. This used to be pinned by
    // asserting the task-level waiting banner stayed absent; that banner was
    // removed outright in T-c514 (duplicate of the step's own reason), so the
    // absence became vacuous — it now holds for every card, dep or not, and
    // would keep passing even if the dep chip DID impersonate the status.
    // Assert the surviving carrier of that distinction instead: the 狀態 pill,
    // which still says 進行中 and must not have been flipped to 等待外部 by a
    // mere dep. Non-vacuous in both directions — a vanished pill fails too.
    expect(
      card.querySelector('[data-testid="task-status"]')?.textContent
    ).toBe("進行中");
  });
});
