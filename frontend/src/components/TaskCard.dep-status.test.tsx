// T-c21e — owner 2026-07-20, on the T-1d82 驗收 card:
//   「還是這些任務可以直接在最前面接 label 顯示這些 task 目前的狀態?」
//
// So a dep row now LEADS with the dep task's own 狀態 badge. What this file
// locks is not "a badge exists" but the four things that make it honest:
//
//   ① it is the DEP's status, not this card's — a done card blocked by an
//     in_progress dep must show 進行中 on the row and 已完成 on row 1;
//   ② it reuses the ONE task-level vocabulary (task-badge--status-* +
//     t.tasks.status), so it can never drift from row 1's chip;
//   ③ an unresolvable dep gets NO badge — there is no status to show, and a
//     defaulted or blank pill would be a confident lie (the same red line the
//     unresolved/missing split was drawn for);
//   ④ the badge does NOT displace the dep title from the accessibility tree.
//
// ── WHAT THIS FILE CANNOT SEE ─────────────────────────────────────────────
// jsdom parses no stylesheet, so the badge's COLOUR and its size trim
// (.task-card__dep-status) are invisible here — as is whether the extra pill
// still fits a 390px row. Nothing below asserts a look; ② asserts the CLASS
// that carries the colour, which is the part a mutant can break in TS. The
// 390px fit is the CT guard's job (taskcard-longtoken-wrap.ct.spec.tsx) and
// the look is owner's 座艙驗收.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { TaskCard } from "./TaskCard";
import { __resetMock, __injectMockTask } from "../api/mock";
import type { TaskView } from "../api/adapter";

let seq = 0;

function mkTask(over: Partial<TaskView>): TaskView {
  seq += 1;
  return {
    id: `task-${seq}`,
    taskNo: `T-${2000 + seq}`,
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

function byTitle(cards: HTMLElement[], title: string): HTMLElement {
  const card = cards.find((c) =>
    c.querySelector(".task-card__title")?.textContent?.includes(title)
  );
  if (!card) throw new Error(`no card titled ${title}`);
  return card;
}

/** The blocked card + its dep rows, after the page can resolve them. The wait
 * is load-bearing: a terminal dep needs the closed-inclusive fetch, and until
 * it lands every row honestly reports `unresolved` (see TaskCard.deps.test). */
async function cardWithDeps(title: string) {
  const { findAllByTestId } = render(
    <I18nProvider>
      <TasksPage />
    </I18nProvider>
  );
  const card = byTitle(await findAllByTestId("task-card"), title);
  const rows = () => [
    ...card.querySelectorAll<HTMLElement>('[data-testid="task-dep"]'),
  ];
  await waitFor(() => {
    expect(
      rows().filter((d) => d.getAttribute("data-dep-state") === "unresolved")
    ).toHaveLength(0);
  });
  return { card, deps: rows() };
}

const NOOP = async () => {};

/** TaskCard rendered directly, so `depsResolvable` can be pinned. */
function renderCard(task: TaskView, allTasks: TaskView[], resolvable: boolean) {
  return render(
    <I18nProvider>
      <TaskCard
        task={task}
        allTasks={allTasks}
        depsResolvable={resolvable}
        members={[]}
        workers={[]}
        nowTs={Date.now() / 1000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onReassign={NOOP as never}
        onSendMessage={NOOP as never}
        onHydrate={NOOP as never}
        onRemoveArtifact={NOOP as never}
      />
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});
afterEach(() => vi.restoreAllMocks());

describe("T-c21e ① 徽章講的是 dep 的狀態,不是本卡的", () => {
  // The whole reason the 2026-07-17 note kept deps off row 1 was that a badge
  // there would be read as a claim about THIS card. Moving it into the dep row
  // is only safe if the two never get confused — so assert them together, on a
  // card whose own status DIFFERS from every dep's.
  it("row 1 shows the card's status, each dep row shows its own dep's", async () => {
    const live = mkTask({ taskNo: "T-aaaa", title: "還在跑", status: "in_progress" });
    const asked = mkTask({
      taskNo: "T-bbbb",
      title: "等人回",
      status: "waiting_owner",
    });
    __injectMockTask(live);
    __injectMockTask(asked);
    __injectMockTask(
      mkTask({
        title: "自己等我回覆",
        status: "waiting_external",
        deps: [live.id, asked.id],
      })
    );

    const { card, deps } = await cardWithDeps("自己等我回覆");
    // 🔴 The card's own badge is untouched and still says the card's status.
    expect(card.querySelector('[data-testid="task-status"]')?.textContent).toBe(
      "等待外部"
    );
    expect(
      deps.map(
        (d) => d.querySelector('[data-testid="task-dep-status"]')?.textContent
      )
    ).toEqual(["進行中", "等我回覆"]);
  });

  it("a DONE dep on a live card reads 已完成 — the badge follows the dep through the closed fast path", async () => {
    const closed = mkTask({
      taskNo: "T-cccc",
      title: "做完的前置",
      status: "done",
      closedTs: Date.now() / 1000 - 100,
    });
    __injectMockTask(closed);
    __injectMockTask(mkTask({ title: "被完成的擋", deps: [closed.id] }));

    const { deps } = await cardWithDeps("被完成的擋");
    expect(
      deps[0].querySelector('[data-testid="task-dep-status"]')?.textContent
    ).toBe("已完成");
  });
});

describe("T-c21e ② 沿用既有詞彙與配色,不另發明", () => {
  it("wears the SAME class pair as row 1's 狀態 chip, so colour cannot drift", async () => {
    const blocker = mkTask({
      taskNo: "T-dddd",
      title: "終止掉的",
      status: "terminated",
      closedTs: Date.now() / 1000 - 100,
    });
    __injectMockTask(blocker);
    __injectMockTask(mkTask({ title: "被終止的擋", deps: [blocker.id] }));

    const { deps } = await cardWithDeps("被終止的擋");
    const badge = deps[0].querySelector('[data-testid="task-dep-status"]')!;
    // task-badge + task-badge--status-<status> is the single source of the
    // hue (tasks.css ~L426/L478). A bespoke class here would be a second
    // place deciding what 終止 looks like.
    expect(badge.classList.contains("task-badge")).toBe(true);
    expect(badge.classList.contains("task-badge--status-terminated")).toBe(
      true
    );
    // 🔴 NOT the step vocabulary (lib/stepBadge.ts). A dep is a TASK.
    expect(
      [...badge.classList].some((c) => c.startsWith("task-step"))
    ).toBe(false);
  });

  it("每個狀態都用 t.tasks.status 的既有文案,沒有新詞", async () => {
    // Enumerated rather than spot-checked: a hand-rolled label map would pass
    // a single-status test and diverge on the one status nobody sampled.
    const cases = [
      ["not_started", "尚未執行"],
      ["in_progress", "進行中"],
      ["waiting_owner", "等我回覆"],
      ["waiting_external", "等待外部"],
      ["done", "已完成"],
      ["terminated", "終止"],
      ["duplicated", "重複"],
    ] as const;
    for (const [status, label] of cases) {
      const blocker = mkTask({ title: `前置-${status}`, status });
      const blocked = mkTask({ title: `被擋-${status}`, deps: [blocker.id] });
      const { container, unmount } = renderCard(
        blocked,
        [blocked, blocker],
        true
      );
      expect(
        container.querySelector('[data-testid="task-dep-status"]')?.textContent,
        `dep status label for ${status}`
      ).toBe(label);
      unmount();
    }
  });
});

describe("T-c21e ③ 無法解析的 dep 沒有狀態可言 —— 不准編一個", () => {
  it("查無此任務 的列不掛任何徽章", async () => {
    __injectMockTask(mkTask({ title: "被鬼擋的", deps: ["t-deadbeefdead"] }));

    const { deps } = await cardWithDeps("被鬼擋的");
    expect(deps[0].getAttribute("data-dep-state")).toBe("missing");
    // 🔴 Not "a badge that says 不明" and not "an empty badge" — NO badge.
    // There is no dep object, so any pill here is invented. An empty one is
    // the worst of the three: it looks like a real status that happens to be
    // blank, which is a lie that renders convincingly.
    expect(
      deps[0].querySelector('[data-testid="task-dep-status"]')
    ).toBeNull();
    expect(deps[0].querySelector(".task-badge")).toBeNull();
    // …and specifically none of the seven real labels leaked in via a default.
    expect(deps[0].textContent).not.toContain("尚未執行");
  });

  it("母體還沒載齊的列(unresolved)同樣不掛徽章", async () => {
    // The subtler half: this dep is very likely a HEALTHY task that merely
    // closed. Guessing 尚未執行 for it would be wrong about a task that is in
    // fact 已完成 — the failure mode is not "vague", it is "inverted".
    const blocked = mkTask({ title: "還沒載到", deps: ["t-not-loaded-yet"] });
    const { container } = renderCard(blocked, [blocked], false);
    const dep = container.querySelector('[data-testid="task-dep"]')!;
    expect(dep.getAttribute("data-dep-state")).toBe("unresolved");
    expect(dep.querySelector('[data-testid="task-dep-status"]')).toBeNull();
    expect(dep.querySelector(".task-badge")).toBeNull();
  });

  it("一好一壞:壞的那列沒徽章,好的那列有,兩者不互相汙染", async () => {
    const good = mkTask({ taskNo: "T-eeee", title: "好的前置", status: "done" });
    __injectMockTask(good);
    __injectMockTask(
      mkTask({ title: "一好一壞badge", deps: ["t-missing-1", good.id] })
    );

    const { deps } = await cardWithDeps("一好一壞badge");
    expect(deps.map((d) => d.getAttribute("data-dep-state"))).toEqual([
      "missing",
      "closed",
    ]);
    expect(
      deps.map((d) => !!d.querySelector('[data-testid="task-dep-status"]'))
    ).toEqual([false, true]);
    expect(
      deps[1].querySelector('[data-testid="task-dep-status"]')?.textContent
    ).toBe("已完成");
  });
});

describe("T-c21e ④ 徽章不得吃掉 dep 標題的無障礙名稱", () => {
  // Naming note: what is asserted below is textContent plus the absence of an
  // aria-label / aria-hidden that would displace or hide it — the inputs the
  // accname algorithm reads for a <button> with no labelling override, not a
  // computed accessible name. No accname engine or screen reader was run here.
  it("badge text is part of the button's text; 編號 + 標題 survive it", async () => {
    const blocker = mkTask({
      taskNo: "T-ffff",
      title: "先把 SSE 重連補起來",
      status: "waiting_owner",
    });
    __injectMockTask(blocker);
    __injectMockTask(mkTask({ title: "a11y 被擋的", deps: [blocker.id] }));

    const { deps } = await cardWithDeps("a11y 被擋的");
    const dep = deps[0];
    // 🔴 The trap two reviewers caught on the previous cut: an aria-label WINS
    // over descendant text in accname computation, so labelling the row would
    // delete the dep title from the a11y tree — for exactly the users who
    // cannot see the visually truncated row. Adding a badge must not tempt
    // anyone to "tidy" the name with one.
    expect(dep.getAttribute("aria-label")).toBeNull();
    expect(dep.getAttribute("title")).toBe("跳到 T-ffff");
    // The badge is plain text, so a screen reader gets status THEN name —
    // added to the accname, not substituted for it. aria-hidden would make
    // the badge invisible to the very readers who cannot see its colour.
    expect(dep.querySelector('[data-testid="task-dep-status"]')?.getAttribute("aria-hidden")).toBeNull();
    expect(dep.textContent).toContain("等我回覆");
    expect(dep.textContent).toContain("T-ffff");
    expect(dep.textContent).toContain("先把 SSE 重連補起來");
  });

  it("徽章排在列的最前面(owner 的原話:直接在最前面接 label)", async () => {
    const blocker = mkTask({ taskNo: "T-9999", title: "擋路的", status: "in_progress" });
    __injectMockTask(blocker);
    __injectMockTask(mkTask({ title: "順序被擋的", deps: [blocker.id] }));

    const { deps } = await cardWithDeps("順序被擋的");
    const dep = deps[0];
    // Position is what fixes the badge's SUBJECT (it is about the dep named on
    // this line), so the order is part of the contract, not decoration.
    expect(dep.firstElementChild?.getAttribute("data-testid")).toBe(
      "task-dep-status"
    );
    const kids = [...dep.children];
    expect(
      kids.findIndex((k) => k.matches('[data-testid="task-dep-status"]'))
    ).toBeLessThan(kids.findIndex((k) => k.matches(".task-card__dep-no")));
  });
});
