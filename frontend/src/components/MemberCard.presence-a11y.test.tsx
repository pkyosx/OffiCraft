// Roster presence: the DOT is the whole signal — and it says so out loud.
//
// Owner 2026-07-17: "成員離線時,綠點會變成灰點,因此不用特別顯示離線" — the
// text badge beside the name restated what the dot's colour had already said,
// so it's gone.
//
// The catch this file exists to hold down: that badge was ALSO the only
// presence fact a screen reader could reach, because the dot was `aria-hidden`
// and this repo has no sr-only/visually-hidden utility. Deleting the badge on
// its own would have handed screen-reader users a roster with NO presence at
// all. So the dot names itself now, and these tests pin BOTH halves together:
//
//   1. the text badge is gone for offline AND stopped (the owner's ask),
//   2. the dot still renders with the colour class for its state (the visual
//      signal the owner is relying on didn't regress), and
//   3. every one of the five lifecycle states is readable as text via the
//      dot's accessible name (the a11y channel the badge used to be).
//
// (3) is queried through getByRole(..., { name }) on purpose: role queries read
// the ACCESSIBILITY TREE, so re-adding `aria-hidden` to the dot drops it from
// that tree and these fail — which is the exact regression to catch. A
// getAttribute("aria-label") check would NOT catch it (the attribute survives
// aria-hidden; the label just stops being reachable).

import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberCard } from "./MemberCard";
import type { Member } from "../types";

function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    memberId: "MB-AST001",
    name: "Mira",
    role: "assistant",
    status: "offline",
    lifecycle: "offline",
    model: "opus",
    effort: "medium",
    kind: "assistant",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-mira",
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
        currentTaskTitle=""
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

describe("MemberCard presence — no text badge, dot carries it", () => {
  // (1) the owner's actual ask. Both states that used to paint the badge.
  it.each(["offline", "stopped"] as const)(
    "renders no 離線 text badge when lifecycle=%s",
    (lifecycle) => {
      const { queryByTestId, queryByText } = renderCard(lifecycle);
      expect(queryByTestId("offline-badge")).toBeNull();
      // Not just the testid: the WORD must be gone from the card's text too,
      // so re-adding the badge under a different testid (or none) still fails.
      // The dot's label is an aria-label, not text, so it doesn't collide.
      expect(queryByText("離線")).toBeNull();
    },
  );

  // (2) the visual signal the owner is leaning on must still be there, and
  // must still be state-specific — a dot stuck on one colour would "pass" a
  // mere presence check while destroying the thing that replaced the badge.
  it.each(ALL)("renders the presence dot with its %s colour class", (lifecycle) => {
    const { container } = renderCard(lifecycle);
    const dot = container.querySelector(".lifecycle-dot");
    expect(dot).not.toBeNull();
    expect(dot!.className).toContain(DOT_CLASS[lifecycle]);
  });

  // (3) the a11y channel the badge used to be. Read via the a11y tree.
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
