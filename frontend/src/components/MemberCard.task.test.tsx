// T-3451: the Staff roster row's CURRENT task title line. Locked here:
//   1. A member WITH an open task shows its real title (2-line clamp variant)
//      and carries the full text on the `title` tooltip (hover 全文).
//   2. A member with NO open task shows the muted empty state (無當前任務), not
//      blank chrome.
import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberCard } from "./MemberCard";
import type { Member } from "../types";

function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    memberId: "MB-AST001",
    name: "Mira",
    role: "assistant",
    status: "online",
    lifecycle: "online",
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

function renderCard(currentTaskTitle: string) {
  return render(
    <I18nProvider>
      <MemberCard
        member={mkMember()}
        selected={false}
        currentTaskTitle={currentTaskTitle}
        onOpenDetail={vi.fn()}
        onChat={vi.fn()}
      />
    </I18nProvider>,
  );
}

describe("MemberCard current-task title (T-3451)", () => {
  it("shows the task title (clamped) with the full text on hover when present", () => {
    const title = "重寫外包排程器的 admission 名額回收邏輯並補齊 CT 護欄";
    const { getByTestId } = renderCard(title);
    const el = getByTestId("member-task-mira");
    expect(el.textContent).toBe(title);
    expect(el.getAttribute("title")).toBe(title);
    expect(el.className).toContain("current-task-title--clamp");
  });

  it("shows the muted empty state when the member has no open task", () => {
    const { getByTestId } = renderCard("");
    const el = getByTestId("member-task-mira");
    expect(el.textContent).toBe(zh.office.noCurrentTask);
    expect(el.className).toContain("current-task-title--empty");
    // no tooltip / no clamp on the empty placeholder — there is no full text to
    // reveal.
    expect(el.getAttribute("title")).toBeNull();
    expect(el.className).not.toContain("current-task-title--clamp");
  });
});
