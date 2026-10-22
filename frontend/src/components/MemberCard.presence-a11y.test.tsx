// Roster presence: the DOT is the whole signal — and it says so out loud.
//
// Owner 2026-07-17: "成員離線時,綠點會變成灰點,因此不用特別顯示離線". The dot
// is also the only presence fact a screen reader can reach (this repo has no
// sr-only/visually-hidden utility), so these tests pin:
//
//   1. the dot renders with the colour class for its state, and
//   2. every one of the five lifecycle states is readable as text via the
//      dot's accessible name.
//
// (2) is queried through getByRole(..., { name }) on purpose: role queries read
// the ACCESSIBILITY TREE, so re-adding `aria-hidden` to the dot drops it from
// that tree and these fail — which is the exact regression to catch. A
// getAttribute("aria-label") check would NOT catch it (the attribute survives
// aria-hidden; the label just stops being reachable).

import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberCard } from "./MemberCard";
import { MemberActivityCard } from "./MemberActivity";
import type { Member } from "../types";

function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    name: "Mira",
    role: "assistant",
    status: "offline",
    lifecycle: "offline",
    model: "opus",
    effort: "medium",
    kind: "staff",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    terminalAttachCommand: "tmux -L officraft attach -t member-mira",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  };
}

function renderCard(lifecycle: Member["lifecycle"]) {
  return render(
    <I18nProvider>
      <MemberCard
        member={mkMember({ lifecycle })}
        selected={false}
        onOpenDetail={() => {}}
        onChat={() => {}}
      />
    </I18nProvider>,
  );
}

// zh is the default locale (I18nProvider falls back to "zh"), so these are the
// zh strings from i18n/locales/zh.ts — the copy an owner actually sees.
const ZH_PRESENCE: Record<Member["lifecycle"], string> = {
  offline: "離線",
  waking: "喚醒中",
  online: "線上",
  stopping: "停止中",
  stopped: "已停止",
};

// lifecycle → the dot's colour class. `online` is surfaced as `online-awake`
// (PresenceBadge.lifecycleVisual); the other four map through unchanged.
const DOT_CLASS: Record<Member["lifecycle"], string> = {
  offline: "lifecycle-dot--offline",
  waking: "lifecycle-dot--waking",
  online: "lifecycle-dot--online-awake",
  stopping: "lifecycle-dot--stopping",
  stopped: "lifecycle-dot--stopped",
};

const ALL: Member["lifecycle"][] = [
  "offline",
  "waking",
  "online",
  "stopping",
  "stopped",
];

describe("MemberCard presence — the dot carries it", () => {
  // (1) the visual signal must be state-specific — a dot stuck on one colour
  // would "pass" a mere presence check.
  it.each(ALL)("renders the presence dot with its %s colour class", (lifecycle) => {
    const { container } = renderCard(lifecycle);
    const dot = container.querySelector(".lifecycle-dot");
    expect(dot).not.toBeNull();
    expect(dot!.className).toContain(DOT_CLASS[lifecycle]);
  });

  // (2) the a11y channel. Read via the a11y tree.
  it.each(ALL)(
    "exposes lifecycle=%s to screen readers as the dot's accessible name",
    (lifecycle) => {
      const { getByRole } = renderCard(lifecycle);
      const dot = getByRole("img", { name: ZH_PRESENCE[lifecycle] });
      expect(dot.className).toContain(DOT_CLASS[lifecycle]);
    },
  );

  // The five labels must be five DIFFERENT words — otherwise a screen-reader
  // user can't tell the states apart, which is the same failure as having no
  // label at all (and would survive every per-state check above if they all
  // read e.g. "線上").
  it("under a runtime login warning, the card leads its role line with one exclamation, whose hint shows at once on hover or focus, and Enter/Space on the focused mark do not open the row", () => {
    const onChat = vi.fn();
    const onOpenDetail = vi.fn();
    const { getAllByTestId, container } = render(
      <I18nProvider>
        <MemberCard
          member={mkMember({
            lifecycle: "online",
            runtimeLoginWarnings: [
              { machineId: "mac-1", machineName: "mac-1", runtime: "codex", pending: false },
            ],
          })}
          selected={false}
          onOpenDetail={onOpenDetail}
          onChat={onChat}
        />
      </I18nProvider>,
    );
    const marks = getAllByTestId("runtime-login-warning");
    expect(marks).toHaveLength(1);
    expect(marks[0].getAttribute("aria-label")).toBe("mac-1 未登入 Codex\n可到「監控」頁的機器資訊，在 Codex 欄按版本號 →「登入」");
    expect(marks[0].hasAttribute("title")).toBe(false);
    expect(marks[0].previousElementSibling).toBeNull();
    expect(marks[0].closest(".member-card__presence")).not.toBeNull();
    expect(screen.queryByRole("tooltip")).toBeNull();

    fireEvent.mouseOver(marks[0]);
    fireEvent.mouseEnter(marks[0]);
    expect(Array.from(screen.getByRole("tooltip").children).map((l) => l.textContent)).toEqual(["mac-1 未登入 Codex", "可到「監控」頁的機器資訊，在 Codex 欄按版本號 →「登入」"]);
    fireEvent.mouseLeave(marks[0]);
    expect(screen.queryByRole("tooltip")).toBeNull();

    act(() => marks[0].focus());
    expect(document.activeElement).toBe(marks[0]);
    expect(Array.from(screen.getByRole("tooltip").children).map((l) => l.textContent)).toEqual(["mac-1 未登入 Codex", "可到「監控」頁的機器資訊，在 Codex 欄按版本號 →「登入」"]);
    fireEvent.keyDown(marks[0], { key: "Enter" });
    fireEvent.keyDown(marks[0], { key: " " });
    expect(onChat).not.toHaveBeenCalled();
    expect(onOpenDetail).not.toHaveBeenCalled();

    // Positive control: the same key on the card itself does open the chat.
    fireEvent.keyDown(container.querySelector(".member-card")!, { key: "Enter" });
    expect(onChat).toHaveBeenCalledTimes(1);
    expect(onOpenDetail).not.toHaveBeenCalled();

    act(() => marks[0].blur());
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  it("under a click on the warning mark, the card shows the hint and opens neither the chat nor the detail, while a click elsewhere on the card still opens the chat", () => {
    const onChat = vi.fn();
    const onOpenDetail = vi.fn();
    const { getByTestId, getByText } = render(
      <I18nProvider>
        <MemberCard
          member={mkMember({
            lifecycle: "online",
            runtimeLoginWarnings: [
              { machineId: "mac-1", machineName: "mac-1", runtime: "codex", pending: false },
            ],
          })}
          selected={false}
          onOpenDetail={onOpenDetail}
          onChat={onChat}
        />
      </I18nProvider>,
    );
    fireEvent.click(getByTestId("runtime-login-warning"));
    expect(Array.from(screen.getByRole("tooltip").children).map((l) => l.textContent)).toEqual(["mac-1 未登入 Codex", "可到「監控」頁的機器資訊，在 Codex 欄按版本號 →「登入」"]);
    expect(onChat).not.toHaveBeenCalled();
    expect(onOpenDetail).not.toHaveBeenCalled();

    fireEvent.click(getByText("Mira"));
    expect(onChat).toHaveBeenCalledTimes(1);
    expect(onOpenDetail).not.toHaveBeenCalled();
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  it("under only a model-call warning, the card leads its role line with one exclamation naming that reason", () => {
    const { getAllByTestId } = render(
      <I18nProvider>
        <MemberCard
          member={mkMember({
            lifecycle: "online",
            runtimeLoginWarnings: [],
            modelCallWarnings: [
              {
                runtime: "claude",
                kind: "rate_limit",
                code: "rate_limit",
                resetsAt: null,
                sinceTs: 1_790_000_000,
                accountWide: true,
              },
            ],
          })}
          selected={false}
          onOpenDetail={() => {}}
          onChat={() => {}}
        />
      </I18nProvider>,
    );
    const marks = getAllByTestId("runtime-login-warning");
    expect(marks).toHaveLength(1);
    expect(marks[0].getAttribute("aria-label")).toBe("Claude 已達用量上限");
    expect(marks[0].previousElementSibling).toBeNull();
    expect(marks[0].closest(".member-card__presence")).not.toBeNull();
  });

  it("draws the dot on the avatar's corner, beside the detail button rather than inside it, and not on the role line", () => {
    const { container } = renderCard("online");
    const dot = container.querySelector(".lifecycle-dot")!;
    const corner = dot.closest(".presence-avatar")!;
    expect(corner.querySelector(":scope > .member-card__avatar")).not.toBeNull();
    expect(dot.closest("button")).toBeNull();
    expect(container.querySelector(".member-card__presence .lifecycle-dot")).toBeNull();
    expect(container.querySelector(".member-card__presence")!.textContent).toBe("特助");
  });

  it("gives each of the five lifecycle states a distinct label", () => {
    const labels = ALL.map((lifecycle) => {
      const { getByRole, unmount } = renderCard(lifecycle);
      const name = getByRole("img").getAttribute("aria-label");
      unmount();
      return name;
    });
    expect(new Set(labels).size).toBe(ALL.length);
  });
});

describe("MemberCard presence dot hint", () => {
  const tooltipLines = () =>
    screen.queryAllByRole("tooltip").map((h) => Array.from(h.children).map((l) => l.textContent));

  function renderRow(over: Partial<Member> = {}) {
    const onChat = vi.fn();
    const onOpenDetail = vi.fn();
    const utils = render(
      <I18nProvider>
        <MemberCard
          member={mkMember(over)}
          selected={false}
          onOpenDetail={onOpenDetail}
          onChat={onChat}
        />
      </I18nProvider>,
    );
    const dot = utils.container.querySelector(".lifecycle-dot") as HTMLElement;
    return { ...utils, dot, onChat, onOpenDetail };
  }

  it.each(ALL)("under a hover on the %s dot, its state shows at once and leaves with the pointer", (lifecycle) => {
    const { dot } = renderRow({ lifecycle });
    expect(dot.hasAttribute("title")).toBe(false);
    expect(tooltipLines()).toEqual([]);
    fireEvent.mouseEnter(dot);
    expect(tooltipLines()).toEqual([[ZH_PRESENCE[lifecycle]]]);
    fireEvent.mouseLeave(dot);
    expect(tooltipLines()).toEqual([]);
  });

  it("under English, the hovered dot reads its state in English", () => {
    window.localStorage.setItem("oc.language", "en");
    try {
      const { dot } = renderRow({ lifecycle: "stopping" });
      fireEvent.mouseEnter(dot);
      expect(tooltipLines()).toEqual([["Stopping"]]);
    } finally {
      window.localStorage.removeItem("oc.language");
    }
  });

  it("under a click or tap on the dot, the state shows pinned and neither the chat nor the detail opens; a second click closes it", () => {
    const { dot, onChat, onOpenDetail } = renderRow({ lifecycle: "online" });
    fireEvent.mouseEnter(dot);
    fireEvent.focus(dot);
    fireEvent.click(dot);
    expect(tooltipLines()).toEqual([["線上"]]);
    expect(screen.getByRole("tooltip").className).toBe("instant-hint instant-hint--pinned");
    fireEvent.mouseLeave(dot);
    fireEvent.blur(dot);
    expect(tooltipLines()).toEqual([["線上"]]);
    expect(onChat).not.toHaveBeenCalled();
    expect(onOpenDetail).not.toHaveBeenCalled();

    fireEvent.click(dot);
    expect(tooltipLines()).toEqual([]);
    expect(onChat).not.toHaveBeenCalled();
    expect(onOpenDetail).not.toHaveBeenCalled();
  });

  it("under Enter or Space on the focused dot, the chat does not open", () => {
    const { dot, onChat } = renderRow({ lifecycle: "waking" });
    act(() => dot.focus());
    expect(document.activeElement).toBe(dot);
    expect(tooltipLines()).toEqual([["喚醒中"]]);
    fireEvent.keyDown(dot, { key: "Enter" });
    fireEvent.keyDown(dot, { key: " " });
    expect(onChat).not.toHaveBeenCalled();
  });

  it("under a click on the name or the role text, the chat still opens, and an open dot hint closes", () => {
    const { dot, onChat, onOpenDetail, getByText } = renderRow({ lifecycle: "stopped" });
    fireEvent.click(dot);
    expect(tooltipLines()).toEqual([["已停止"]]);

    fireEvent.click(getByText("Mira"));
    expect(onChat).toHaveBeenCalledTimes(1);
    expect(tooltipLines()).toEqual([]);

    fireEvent.click(getByText("特助"));
    expect(onChat).toHaveBeenCalledTimes(2);
    expect(onOpenDetail).not.toHaveBeenCalled();
  });

  const WARNED: Partial<Member> = {
    lifecycle: "offline",
    runtimeLoginWarnings: [
      { machineId: "mac-1", machineName: "mac-1", runtime: "codex", pending: false },
    ],
  };
  const MARK_LINES = ["mac-1 未登入 Codex", "可到「監控」頁的機器資訊，在 Codex 欄按版本號 →「登入」"];

  it("under a click on the dot while the warning hint is pinned, only the dot's hint stays open", () => {
    const { dot, getByTestId, onChat, onOpenDetail } = renderRow(WARNED);
    fireEvent.click(getByTestId("runtime-login-warning"));
    expect(tooltipLines()).toEqual([MARK_LINES]);
    fireEvent.click(dot);
    expect(tooltipLines()).toEqual([["離線"]]);
    expect(onChat).not.toHaveBeenCalled();
    expect(onOpenDetail).not.toHaveBeenCalled();
  });

  it("under a click on the warning mark while the dot hint is pinned, only the warning hint stays open", () => {
    const { dot, getByTestId, onChat, onOpenDetail } = renderRow(WARNED);
    fireEvent.click(dot);
    expect(tooltipLines()).toEqual([["離線"]]);
    fireEvent.click(getByTestId("runtime-login-warning"));
    expect(tooltipLines()).toEqual([MARK_LINES]);
    expect(onChat).not.toHaveBeenCalled();
    expect(onOpenDetail).not.toHaveBeenCalled();
  });

  it("under a hover on the dot while the warning hint is pinned, only the dot's hint is on screen", () => {
    const { dot, getByTestId } = renderRow(WARNED);
    fireEvent.click(getByTestId("runtime-login-warning"));
    expect(tooltipLines()).toEqual([MARK_LINES]);
    fireEvent.mouseEnter(dot);
    expect(tooltipLines()).toEqual([["離線"]]);
  });

  it("under a hover on the warning mark while the dot hint is pinned, only the warning hint is on screen", () => {
    const { dot, getByTestId } = renderRow(WARNED);
    fireEvent.click(dot);
    expect(tooltipLines()).toEqual([["離線"]]);
    fireEvent.mouseEnter(getByTestId("runtime-login-warning"));
    expect(tooltipLines()).toEqual([MARK_LINES]);
  });

  it("under keyboard focus moving between the pinned dot and the warning mark, only the focused one's hint is on screen", () => {
    const { dot, getByTestId } = renderRow(WARNED);
    const mark = getByTestId("runtime-login-warning");
    fireEvent.click(dot);
    act(() => mark.focus());
    expect(tooltipLines()).toEqual([MARK_LINES]);

    fireEvent.click(mark);
    act(() => dot.focus());
    expect(tooltipLines()).toEqual([["離線"]]);
  });

  it("keeps the state as the dot's own accessible name on the one focusable element, with no focusable layer inside it", () => {
    const { dot, getByRole } = renderRow({ lifecycle: "offline" });
    expect(getByRole("img", { name: "離線" })).toBe(dot);
    expect(dot.tabIndex).toBe(0);
    expect(dot.querySelectorAll("*")).toHaveLength(0);
  });
});

describe("MemberCard activity icon", () => {
  const cardWith = (activity: Member["activity"], onChat: () => void, name = "Mira") => (
    <I18nProvider>
      <MemberCard
        member={mkMember({ name, lifecycle: activity ? "online" : "offline", activity })}
        selected={false}
        onOpenDetail={() => {}}
        onChat={onChat}
      />
    </I18nProvider>
  );
  const renderWith = (activity: Member["activity"], onChat = vi.fn()) => render(cardWith(activity, onChat));

  it.each([
    ["busy", "工作中，查看運作狀況", "working", "主 agent 工作中（已 0 秒）"],
    ["waiting", "等待中，查看運作狀況", "waiting", "主 agent 與 sub-agent 都沒有在工作，正在等 1 個背景檢查或排程"],
    ["idle", "閒置，查看運作狀況", "idle", "主 agent 與 sub-agent 都沒有在工作"],
  ] as const)("under %s, the icon right after the name is a button named %s, draws its own glyph and hints the summary", (state, name, glyph, hint) => {
    const { getByTestId, getByRole } = renderWith({
      state,
      busySince: state === "busy" ? Date.now() / 1000 + 5 : null,
      subagents: [],
      waits: state === "waiting" ? [{ kind: "background_command", description: "", nextTs: null, recurring: false }] : [],
    });
    const icon = getByTestId("activity-icon-mira");
    expect(icon.previousElementSibling!.textContent).toBe("Mira");
    expect(getByRole("button", { name })).toBe(icon);
    expect(icon.querySelector(`.activity-icon__glyph--${glyph}`)).not.toBeNull();
    fireEvent.mouseEnter(icon);
    expect(screen.getByRole("tooltip").textContent).toBe(hint);
  });

  it("draws 工作中 as the sparkle glyph", () => {
    const { getByTestId } = renderWith({ state: "busy", busySince: Date.now() / 1000, subagents: [], waits: [] });
    const glyph = getByTestId("activity-icon-mira").querySelector(".activity-icon__glyph--working")!;
    expect(glyph.querySelector(".sparkle-icon__star")).not.toBeNull();
    expect(glyph.querySelectorAll(".sparkle-icon__twinkle")).toHaveLength(2);
  });

  describe("運作狀況 dialog", () => {
    const NOW = 1_800_000_000;
    const activity: Member["activity"] = {
      state: "busy",
      busySince: NOW - 180,
      subagents: [
        { id: "s1", parentId: null, name: "ci-watch", description: "查 CI 結果", startedTs: NOW - 45 },
        { id: "s2", parentId: "s1", name: "log-reader", description: "讀 log", startedTs: NOW - 12 },
      ],
      waits: [{ kind: "schedule", description: "每 10 秒檢查 CI", nextTs: null, recurring: true }],
    };
    const dialog = () => screen.queryByRole("dialog");
    const rows = (root: HTMLElement) =>
      Array.from(root.querySelectorAll(".mp-activity__row")).map((r) => r.textContent);

    beforeEach(() => {
      vi.useFakeTimers({ now: NOW * 1000 });
    });
    afterEach(() => {
      vi.useRealTimers();
    });

    it("tapping the icon opens the dialog with the title, state, summary and tree, without opening the chat", () => {
      const onChat = vi.fn();
      const { getByTestId } = renderWith(activity, onChat);
      fireEvent.click(getByTestId("activity-icon-mira"));
      expect(onChat).not.toHaveBeenCalled();
      expect(screen.queryByRole("tooltip")).toBeNull();
      const d = screen.getByRole("dialog", { name: "Mira · 運作狀況" });
      expect(d.getAttribute("aria-modal")).toBe("true");
      expect(d.querySelector(".mp-activity__head")!.textContent).toBe("目前狀態工作中");
      expect(d.querySelector(".mp-activity__state--busy")).not.toBeNull();
      expect(d.querySelector(".mp-activity__summary")!.textContent).toBe(
        "主 agent 工作中（已 3 分鐘），另有 2 個 sub-agent 工作中，正在等 1 個背景檢查或排程",
      );
      expect(rows(d)).toEqual([
        "主 agent已 3 分鐘",
        "查 CI 結果ci-watch45 秒",
        "讀 loglog-reader12 秒",
        "每 10 秒檢查 CI排程 · 重複",
      ]);
      expect(document.activeElement).toBe(screen.getByRole("button", { name: "關閉" }));
    });

    it("renders the very body the detail panel's 運作狀況 card renders", () => {
      const { getByTestId } = renderWith(activity);
      fireEvent.click(getByTestId("activity-icon-mira"));
      const card = render(
        <I18nProvider>
          <MemberActivityCard activity={activity} testId="card" />
        </I18nProvider>,
      ).getByTestId("card");
      const d = screen.getByRole("dialog");
      for (const sel of [".mp-activity__summary", ".mp-activity__tree"]) {
        expect(d.querySelector(sel)!.outerHTML, sel).toBe(card.querySelector(sel)!.outerHTML);
      }
    });

    it("keeps counting while open", () => {
      const { getByTestId } = renderWith(activity);
      fireEvent.click(getByTestId("activity-icon-mira"));
      act(() => {
        vi.advanceTimersByTime(5_000);
      });
      expect(rows(screen.getByRole("dialog"))[2]).toBe("讀 loglog-reader17 秒");
    });

    it.each([
      ["Esc", () => fireEvent.keyDown(document.activeElement!, { key: "Escape" })],
      ["the backdrop", () => fireEvent.click(dialog()!)],
      ["✕", () => fireEvent.click(screen.getByRole("button", { name: "關閉" }))],
    ])("closes on %s, hands focus back to the icon and leaves the chat closed", (_, close) => {
      const onChat = vi.fn();
      const { getByTestId } = renderWith(activity, onChat);
      const icon = getByTestId("activity-icon-mira");
      fireEvent.click(icon);
      act(() => close());
      expect(dialog()).toBeNull();
      expect(document.activeElement).toBe(icon);
      expect(onChat).not.toHaveBeenCalled();
    });

    it("a click or Enter inside the dialog stays there", () => {
      const onChat = vi.fn();
      const { getByTestId } = renderWith(activity, onChat);
      fireEvent.click(getByTestId("activity-icon-mira"));
      const summary = dialog()!.querySelector(".mp-activity__summary")!;
      fireEvent.click(summary);
      fireEvent.keyDown(summary, { key: "Enter" });
      expect(dialog()).not.toBeNull();
      expect(onChat).not.toHaveBeenCalled();
    });

    it.each([["Enter", "Enter"], ["Space", " "]])("%s on the icon opens the dialog, not the chat", (_, key) => {
      const onChat = vi.fn();
      const { getByTestId } = renderWith(activity, onChat);
      const icon = getByTestId("activity-icon-mira");
      fireEvent.keyDown(icon, { key });
      fireEvent.keyUp(icon, { key });
      fireEvent.click(icon);
      expect(onChat).not.toHaveBeenCalled();
      expect(dialog()).not.toBeNull();
    });

    it("Tab and Shift+Tab keep focus inside the dialog", () => {
      const { getByTestId } = renderWith(activity);
      fireEvent.click(getByTestId("activity-icon-mira"));
      const close = screen.getByRole("button", { name: "關閉" });
      for (const shiftKey of [false, true]) {
        const moved = fireEvent.keyDown(close, { key: "Tab", shiftKey });
        expect(moved, shiftKey ? "Shift+Tab" : "Tab").toBe(false);
        expect(document.activeElement).toBe(close);
      }
      act(() => getByTestId("activity-icon-mira").focus());
      fireEvent.keyDown(dialog()!, { key: "Tab" });
      expect(document.activeElement, "focus outside is pulled back in").toBe(close);
    });

    it("closes when the activity goes away, stays closed without taking focus when it returns, and a later close focuses the live icon", () => {
      const onChat = vi.fn();
      const { getByTestId, rerender } = renderWith(activity, onChat);
      fireEvent.click(getByTestId("activity-icon-mira"));
      expect(dialog()).not.toBeNull();
      rerender(cardWith(null, onChat));
      expect(dialog()).toBeNull();
      rerender(cardWith(activity, onChat));
      expect(dialog()).toBeNull();
      const icon = getByTestId("activity-icon-mira");
      expect(icon.getAttribute("aria-expanded")).toBe("false");
      expect(document.activeElement).toBe(document.body);
      fireEvent.click(icon);
      fireEvent.click(screen.getByRole("button", { name: "關閉" }));
      expect(dialog()).toBeNull();
      expect(document.activeElement).toBe(icon);
    });

    it("closes when the card turns to another member", () => {
      const onChat = vi.fn();
      const { getByTestId, rerender } = renderWith(activity, onChat);
      fireEvent.click(getByTestId("activity-icon-mira"));
      rerender(cardWith(activity, onChat, "Nova"));
      expect(dialog()).toBeNull();
    });
  });

  it("an offline member (no activity) has no icon and no way into the dialog", () => {
    const { queryByTestId } = renderWith(null);
    expect(queryByTestId("activity-icon-mira")).toBeNull();
    expect(screen.queryByRole("button", { name: /查看運作狀況/ })).toBeNull();
  });
});
