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

import { describe, it, expect, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberCard } from "./MemberCard";
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
  it("under a runtime login warning, the card shows one exclamation right after the dot, whose hint shows at once on hover or focus, and Enter/Space on the focused mark do not open the row", () => {
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
    expect(marks[0].previousElementSibling).toBe(container.querySelector(".lifecycle-dot"));
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

  it("under only a model-call warning, the card shows one exclamation right after the dot naming that reason", () => {
    const { getAllByTestId, container } = render(
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
    expect(marks[0].previousElementSibling).toBe(container.querySelector(".lifecycle-dot"));
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

  it("keeps the state as the dot's own accessible name on the one focusable element, with no focusable layer inside it", () => {
    const { dot, getByRole } = renderRow({ lifecycle: "offline" });
    expect(getByRole("img", { name: "離線" })).toBe(dot);
    expect(dot.tabIndex).toBe(0);
    expect(dot.querySelectorAll("*")).toHaveLength(0);
  });
});
