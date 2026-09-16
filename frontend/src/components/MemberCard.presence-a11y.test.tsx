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

import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
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
