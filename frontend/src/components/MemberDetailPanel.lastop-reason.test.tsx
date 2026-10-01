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
//   5. 喚醒 on a member that is already running is a SUCCESS that carries the
//      session_alive note: ✓ and the amber note, never the red ✗ 失敗. Built
//      from the mock adapter's own answer, so the mock cannot drift back to
//      a failure unseen.

import { describe, it, expect } from "vitest";
import { readFile } from "node:fs/promises";
import { render, waitFor } from "@testing-library/react";
import { vi } from "vitest";
import { I18nProvider } from "../i18n";
import { MemberDetailPanel } from "./MemberDetailPanel";
import { clearLocale, useEnglishLocale } from "../test/effortOptions";
import { mockApi, __resetMock, __setMockMemberOnline } from "../api/mock";
import type { Member } from "../types";

vi.mock("../api", () => ({
  api: {
    listMachines: () =>
      Promise.resolve([
        {
          machineId: "m-studio",
          displayName: 'Studio "A"',
          online: false,
          isSelf: false,
          binStatus: null,
          wardenShape: null,
          cutoverEffect: null,
          claudeVersion: null,
          claudeCredSource: null,
          claudeSubReadable: null,
        },
      ]),
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

  it("paints the note 喚醒 leaves on a member that is already running as ✓ 成功 with the amber note", async () => {
    __resetMock();
    __setMockMemberOnline("mira", true);
    await mockApi.activateMember("mira");
    const { getByTestId, container } = renderPanel(await mockApi.getMember("mira"));

    expect(container.querySelector(".mp-lastop__head--ok")).not.toBeNull();
    expect(container.querySelector(".mp-lastop__head--fail")).toBeNull();
    expect(container.querySelector(".mp-lastop__icon")?.textContent).toBe("✓");
    expect(container.querySelector(".mp-lastop__verb")?.textContent).toBe("喚醒");
    expect(container.querySelector(".mp-lastop__result")?.textContent).toBe("成功");
    const line = getByTestId("mp-lastop-reason");
    expect(line.className).toBe("mp-lastop__reason mp-lastop__reason--note");
    expect(line.textContent).toBe(
      "session_alive: it was already running — 喚醒 left that session alone and " +
        "dispatched nothing. Its work, and any 加速停止 or 重新聚焦 already under way on " +
        "it, are untouched. To end the current session and start a fresh one, press " +
        "強制停止 first, then 喚醒",
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

  it.each([
    [
      "zh",
      "codex_model_family_unavailable: this machine's Codex (version 0.153.4) lists no terra model; " +
        "available: gpt-6-astra, gpt-5.6-sol, gpt-5.5",
      "codex_model_family_unavailable: 這台機器的 Codex（版本 0.153.4）沒有 terra 系列的型號，" +
        "可用：gpt-6-astra, gpt-5.6-sol, gpt-5.5",
    ],
    [
      "en",
      "codex_model_family_unavailable: this machine's Codex (version 0.153.4) lists no terra model; " +
        "available: gpt-6-astra, gpt-5.6-sol, gpt-5.5",
      "codex_model_family_unavailable: This machine's Codex (version 0.153.4) has no terra model. " +
        "Available: gpt-6-astra, gpt-5.6-sol, gpt-5.5",
    ],
    [
      "zh",
      "codex_model_family_unavailable: could not read the model list of this machine's Codex " +
        "(version unknown) to pick the newest sol model",
      "codex_model_family_unavailable: 讀不到這台機器 Codex（版本不明）的型號清單，無法決定 sol 要用哪個型號",
    ],
    [
      "zh",
      "machine_unavailable: machine 'm-cx' runs a warden too old to resolve the Codex model family 'sol' " +
        "— upgrade that machine's warden, or set a full model id",
      "machine_unavailable: 機器「m-cx」上的 OffiCraft 程式是舊版，還不認得 Codex 型號系列 sol；" +
        "請更新那台機器上的 OffiCraft 程式，或改設完整的型號名稱",
    ],
    [
      "en",
      "machine_unavailable: machine 'm-cx' runs a warden too old to resolve the Codex model family 'sol' " +
        "— upgrade that machine's warden, or set a full model id",
      "machine_unavailable: Machine 'm-cx' runs an OffiCraft program too old to resolve the Codex model " +
        "family sol. Upgrade the OffiCraft program on that machine, or set a full model id",
    ],
    [
      "zh",
      "machine_unavailable: machine 'm-cx' is offline; no other machine is substituted",
      "machine_unavailable: machine 'm-cx' is offline; no other machine is substituted",
    ],
    [
      "zh",
      "machine_unavailable: machine 'm-cx' runs a warden too old to resolve the Codex model family 'sol' " +
        "— upgrade that machine's warden, or set a full model id; retry after the upgrade",
      "machine_unavailable: machine 'm-cx' runs a warden too old to resolve the Codex model family 'sol' " +
        "— upgrade that machine's warden, or set a full model id; retry after the upgrade",
    ],
    [
      "zh",
      "machine_unavailable: machine 'm-cx' runs a warden too old to resolve the Codex model family 'sol' " +
        "— upgrade that machine's warden, or set a full model id (warden log: ocwarden.out.log)",
      "machine_unavailable: machine 'm-cx' runs a warden too old to resolve the Codex model family 'sol' " +
        "— upgrade that machine's warden, or set a full model id (warden log: ocwarden.out.log)",
    ],
    [
      "zh",
      "wake_timeout: machine_unavailable: machine 'm-cx' runs a warden too old to resolve the Codex model family 'sol' " +
        "— upgrade that machine's warden, or set a full model id",
      "wake_timeout: machine_unavailable: machine 'm-cx' runs a warden too old to resolve the Codex model family 'sol' " +
        "— upgrade that machine's warden, or set a full model id",
    ],
  ])("a Codex model family or old-warden refusal is worded in the viewer's language, any other reason is shown as sent (%s)", (locale, reason, shown) => {
    if (locale === "en") useEnglishLocale();
    try {
      const { getByTestId } = renderPanel(mkMember({ lastOpReason: reason }));
      expect(getByTestId("mp-lastop-reason").textContent).toBe(shown);
    } finally {
      clearLocale();
    }
  });

  it.each([
    [
      "zh",
      "claude_not_logged_in: machine 'm-studio' is not logged in to claude",
      'Studio "A" 未登入 Claude',
    ],
    [
      "zh",
      "codex_not_logged_in: machine 'm-studio' is not logged in to codex",
      'Studio "A" 未登入 Codex',
    ],
    [
      "en",
      "claude_not_logged_in: machine 'm-studio' is not logged in to claude",
      'Studio "A" is not logged in to Claude',
    ],
    [
      "en",
      "codex_not_logged_in: machine 'm-studio' is not logged in to codex",
      'Studio "A" is not logged in to Codex',
    ],
    [
      "zh",
      "claude_not_logged_in: machine 'm-gone' is not logged in to claude",
      "m-gone 未登入 Claude",
    ],
    [
      "zh",
      "machine_unavailable: machine 'm-studio' is not logged in to claude; no other machine is substituted",
      "machine_unavailable: machine 'm-studio' is not logged in to claude; no other machine is substituted",
    ],
    [
      "zh",
      "machine_unavailable: machine 'm-studio' does not provide the 'codex' runtime; no other machine is substituted",
      "machine_unavailable: machine 'm-studio' does not provide the 'codex' runtime; no other machine is substituted",
    ],
    [
      "zh",
      "codex_not_logged_in: `codex login status` failed on this host",
      "codex_not_logged_in: `codex login status` failed on this host",
    ],
  ])("under a not-logged-in reason the line is the machine's name and the runtime, any other reason is shown as sent (%s: %s)", async (locale, reason, shown) => {
    if (locale === "en") useEnglishLocale();
    try {
      const { getByTestId } = renderPanel(mkMember({ lastOpReason: reason }));
      await waitFor(() =>
        expect(getByTestId("mp-lastop-reason").textContent).toBe(shown),
      );
    } finally {
      clearLocale();
    }
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
