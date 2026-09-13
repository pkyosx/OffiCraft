// MemberDetailPanel · 最近操作 failure REASON (成員啟動失敗原因全鏈可見).
//
// Locked here:
//   1. A FAILED op whose receipt carried a structured reason renders it as an
//      always-visible one-line summary (no expand needed — the 2026-07-13
//      incident showed a bare "✕ 啟動 失敗" tells the owner nothing).
//   2. An old record WITHOUT a reason renders status-only, exactly as before
//      (no fabricated cause), while the collapsible log stays available.
//   3. A SUCCEEDED op that carried a reason RENDERS it too, in the amber
//      note style rather than the danger style (T-201: the pre-trust verdict
//      no longer refuses the spawn, it reports through last_op_reason, and
//      this panel is the only renderer a SUCCEEDED op's reason reaches —
//      gating the line on failure would leave the value visible to the API
//      and to nobody. WorkerDetailPanel renders the same field too, but only
//      when the worker reads offline, which is the never-dispatched-start
//      case this receipt block cannot cover).
//   4. A SUCCEEDED op with no reason still renders status-only.

import { describe, it, expect } from "vitest";
import { readFile } from "node:fs/promises";
import { render } from "@testing-library/react";
import { vi } from "vitest";
import { I18nProvider } from "../i18n";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
    listWebhooks: () => Promise.resolve([]),
    listScheduledMessages: () => Promise.resolve([]),
    createWebhook: () =>
      Promise.resolve({ endpointId: "", purpose: "", status: "enabled", createdTs: 0, token: "" }),
    updateWebhook: () =>
      Promise.resolve({ endpointId: "", purpose: "", status: "enabled", createdTs: 0, token: "" }),
    deleteWebhook: () => Promise.resolve(),
    subscribeEvents: () => () => {},
  },
}));

const REASON =
  'session_already_exists: tmux session "member-mira" is already live (clobber-guard refused to stomp it)';

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
    lastOp: "start",
    lastOpOk: false,
    lastOpLog: "spawn refused",
    lastOpReason: REASON,
    lastOpAt: 1_752_400_000,
    unreadCount: 0,
    ...over,
  };
}

function renderPanel(member: Member) {
  return render(
    <I18nProvider>
      <MemberDetailPanel member={member} onBack={() => {}} />
    </I18nProvider>,
  );
}

describe("MemberDetailPanel 最近操作 failure reason", () => {
  it("shows the structured reason as an always-visible one-line summary", () => {
    const { getByTestId } = renderPanel(mkMember());
    expect(getByTestId("mp-lastop-reason").textContent).toBe(REASON);
  });

  // owner 2026-07-31 (rc-b7d1c642f2d2): ONE verb for this action, and it is
  // 喚醒. The receipt used to say 啟動 while the button right above it said
  // 喚醒 — the same act under two names on one screen.
  it("names the start op with the same verb the button uses (喚醒, not 啟動)", () => {
    const { container } = renderPanel(mkMember());
    expect(container.querySelector(".mp-lastop__verb")?.textContent).toBe("喚醒");
  });

  it("renders status-only for an old record without a reason (never fabricated)", () => {
    const { queryByTestId, container } = renderPanel(
      mkMember({ lastOpReason: "" }),
    );
    expect(queryByTestId("mp-lastop-reason")).toBeNull();
    // The failure block itself still renders (status + collapsible log).
    expect(container.querySelector(".mp-lastop__head--fail")).not.toBeNull();
    expect(container.querySelector(".mp-lastop__toggle")).not.toBeNull();
  });

  it("renders no reason line on a successful op that carried none", () => {
    const { queryByTestId } = renderPanel(
      mkMember({ lastOpOk: true, lastOpLog: "", lastOpReason: "" }),
    );
    expect(queryByTestId("mp-lastop-reason")).toBeNull();
  });

  // T-201. The pre-trust verdict reports instead of refusing the spawn, so the
  // member comes up OK and the verdict rides along on the receipt. If this
  // line is gated on failure the owner sees only "✓ 喚醒 成功" and the verdict
  // exists solely in the database — a silent failure traded for a loud one.
  const VERDICT =
    "pretrust_unverified: claude did not confirm the pre-trust marker; claude said: (empty)";

  it("shows the reason on a SUCCEEDED op, styled as a note rather than a failure", () => {
    const { getByTestId, container } = renderPanel(
      mkMember({ lastOpOk: true, lastOpLog: "", lastOpReason: VERDICT }),
    );
    const line = getByTestId("mp-lastop-reason");
    expect(line.textContent).toBe(VERDICT);
    // Amber note, not the danger red of a failed op.
    expect(line.className).toContain("mp-lastop__reason--note");
    expect(container.querySelector(".mp-lastop__head--ok")).not.toBeNull();
    expect(container.querySelector(".mp-lastop__head--fail")).toBeNull();
  });

  it("keeps the failure reason in the danger style (no note modifier)", () => {
    const { getByTestId } = renderPanel(mkMember());
    expect(getByTestId("mp-lastop-reason").className).not.toContain(
      "mp-lastop__reason--note",
    );
  });

  // ⛔ THE CLASS NAME IS NOT THE COLOUR. jsdom applies no stylesheet, so every
  // assertion above can only see that the element carries
  // "mp-lastop__reason--note" — it is blind to whether that class resolves to
  // anything at all. Rename or delete the rule in member-detail.css and the
  // note line silently falls back to the BASE rule's --color-danger, i.e. it
  // becomes character-for-character the same red as a failed op, and the whole
  // point of T-201 (a succeeded-with-a-warning op must not read as a failure)
  // is gone with the suite still green. Same move as
  // MonitorPage.cutover-effect.test.tsx: read the stylesheet itself.
  // Read from the repo path, not through `import.meta.url` — vitest does not
  // hand test modules a file: URL.
  it("resolves the note modifier to the warn token, not the danger one", async () => {
    const css = await readFile("src/components/member-detail.css", "utf8");
    const ruleFor = (cls: string) => {
      const m = css.match(new RegExp(`\\.${cls}\\s*\\{([^}]*)\\}`));
      return m === null ? null : m[1];
    };

    const note = ruleFor("mp-lastop__reason--note");
    if (note === null) {
      throw new Error(
        "no .mp-lastop__reason--note rule in member-detail.css — the note line " +
          "renders in the failure colour, and no DOM assertion can see it",
      );
    }
    expect(note).toContain("var(--color-warn-fg)");
    expect(
      note,
      "the note modifier must OVERRIDE the base danger colour, not repeat it",
    ).not.toContain("var(--color-danger)");

    // The other direction: the base rule is what a FAILURE gets, and it must
    // stay the danger colour — otherwise the two states converge from the
    // other side.
    const base = ruleFor("mp-lastop__reason");
    if (base === null) {
      throw new Error("no .mp-lastop__reason rule in member-detail.css");
    }
    expect(base).toContain("var(--color-danger)");
  });

  it("offers the collapsible log on a SUCCEEDED op that carried one", () => {
    const { container } = renderPanel(
      mkMember({
        lastOpOk: true,
        lastOpReason: VERDICT,
        lastOpLog: "claude said: (empty)\nmarker file was never read",
      }),
    );
    expect(container.querySelector(".mp-lastop__toggle")).not.toBeNull();
  });
});
