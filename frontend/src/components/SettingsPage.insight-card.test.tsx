// T-3809 gate B: the Insight card is the ONLY observable this ticket ships.
//
// The ticket's whole delivery is "the role journal is three blocks, not two",
// and the owner's ruling was explicitly for the version that makes that VISIBLE
// (the alternative — ship the write path and say out loud that nothing is
// observable — was rejected). Without a gate here, an implementation that wires
// up every route, every MCP tool and the migration, and simply never mounts the
// card, is green everywhere: the server tests pass, tsc passes, and the only way
// anyone finds out is by opening the cockpit months later.
//
// Three things are pinned, and each one is here because it can fail QUIETLY:
//
//  1. THE CARD IS MOUNTED, on a role page reached the way an owner reaches it.
//     Rendering the real SettingsPage rather than <InsightCard> in isolation is
//     the point — a card that renders perfectly but is never placed delivers
//     nothing, and an isolated render cannot tell the two apart.
//  2. THE EMPTY STATE IS ITS OWN READING. This doc has no file seed, so empty is
//     the honest answer to "has this role moved anything over yet?". If it ever
//     renders as the load state or the error state instead, the one question
//     this release lets the owner ask stops being answerable — with no error
//     anywhere.
//  3. size_chars / cap_chars ARE ON THE HEADER. cap_chars is the live
//     doc.cap_chars setting, and the settings surface that otherwise shows it is
//     admin-only; this header is the only place it is readable without being
//     refused by it first. It is also the field most likely to be dropped as
//     "bookkeeping noise" while mapping the wire — LessonsView drops exactly
//     these two, and copying that mapper is the natural mistake.
//
// ⚠️ What this file does NOT cover, stated so nobody reads it as more: it does
// not prove the cockpit hears an `insight` SSE delta. The server test proves the
// frame is published; nothing automated joins the two ends.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";

const s = zh.settings;
const mp = zh.mp;

beforeEach(() => {
  __resetMock();
});

/** Walk to one role's page the way an owner does: 設定 › 角色誌 › <role>. */
async function openRolePage(roleLabel: string) {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.roles));
  fireEvent.click(await utils.findByText(roleLabel));
  await utils.findAllByText(s.edit);
  return utils;
}

const insightCard = (utils: { container: HTMLElement }) =>
  utils.container.querySelector(".mp-insight") as HTMLElement | null;

describe("SettingsPage · InsightCard (T-3809)", () => {
  it("mounts on a SEED role's page, beside — not instead of — the lessons card", async () => {
    const utils = await openRolePage(zh.office.role.assistant);

    const card = insightCard(utils);
    expect(card).toBeTruthy();
    expect(within(card!).getByText(mp.insight)).toBeTruthy();

    // Beside, not instead of: the lessons card must still be there. A fragment
    // that replaced rather than appended would satisfy every assertion above.
    const lessons = utils.container.querySelector(
      ".mp-lessons:not(.mp-insight)"
    );
    expect(lessons).toBeTruthy();
    // Insight goes AFTER Learning (Duty → Learning → Insight).
    expect(
      lessons!.compareDocumentPosition(card!) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
  });

  it("mounts on a CUSTOM role's page too", async () => {
    // The DoD says "every role", and a seed-only implementation passes a
    // seed-only test. Custom roles are the half a keyed-off-the-seed-list
    // mistake would drop.
    const { role } = await mockApi.createRole({ name: "臨時角色" });
    const utils = await openRolePage(role.name);
    expect(insightCard(utils)).toBeTruthy();
  });

  it("an untouched doc reads as EMPTY — not as loading, not as an error", async () => {
    const utils = await openRolePage(zh.office.role.assistant);
    const card = insightCard(utils)!;

    expect(within(card).getByText(mp.insightEmpty)).toBeTruthy();
    // The three states are distinct strings on purpose; the empty one must not
    // be reachable by rendering either of the other two.
    expect(within(card).queryByText(mp.insightLoading)).toBeNull();
    expect(within(card).queryByText(mp.insightError)).toBeNull();
  });

  it("shows content once the role has moved something over", async () => {
    await mockApi.saveInsight("assistant", "# 我的判準\n\n速度優先於完備。");
    const utils = await openRolePage(zh.office.role.assistant);
    const card = insightCard(utils)!;

    expect(within(card).getByText("速度優先於完備。")).toBeTruthy();
    // VALUE half of the pair above: the empty copy is really gone, so the
    // empty-state assertion is measuring a state, not a constant.
    expect(within(card).queryByText(mp.insightEmpty)).toBeNull();
  });

  it("the header carries size_chars / cap_chars — including at zero", async () => {
    const utils = await openRolePage(zh.office.role.assistant);
    const size = insightCard(utils)!.querySelector(".mp-insight__size");
    // Zero is exactly when someone is about to write the first thing into the
    // doc, so it is the worst moment to hide the limit.
    expect(size?.textContent?.replace(/\s+/g, " ").trim()).toBe("0 / 10000");
  });

  it("the header's numbers are the SERVED ones, not recomputed locally", async () => {
    // A card that counted `text.length` itself would pass a fixed-string test
    // and then disagree with the server on every multi-byte doc and on any cap
    // the owner has raised. Both numbers must come off the wire.
    await mockApi.patchServerSettings({ docCapChars: 12345 });
    await mockApi.saveInsight("assistant", "判準");
    const utils = await openRolePage(zh.office.role.assistant);
    const size = insightCard(utils)!.querySelector(".mp-insight__size");
    expect(size?.textContent?.replace(/\s+/g, " ").trim()).toBe("2 / 12345");
  });

  it("says out loud that Insight is separate, NOT private", async () => {
    // The delivery sentence. READ is unrestricted by owner ruling, and a card
    // that implies a confidentiality boundary this system does not keep is
    // worse than no card at all.
    const utils = await openRolePage(zh.office.role.assistant);
    const note = within(insightCard(utils)!).getByText(mp.insightShared);
    expect(note.textContent).toContain("目前不是私有的");
  });

  it("carries a version-history entry keyed on the BARE role_key", async () => {
    // 🔴 This entry is the trigger face of the ticket's one silent bug: a
    // restore that publishes no `insight` delta still returns 200 and still
    // writes the database. Keying it on the lessons-style composite
    // "<role>::<task_type>" would address a document that does not exist —
    // which also fails silently, as an empty version list.
    await mockApi.saveInsight("assistant", "第零版");
    await mockApi.saveInsight("assistant", "第一版");
    const utils = await openRolePage(zh.office.role.assistant);
    const card = insightCard(utils)!;

    fireEvent.click(within(card).getByText(s.edit));
    fireEvent.click(within(card).getByTestId("doc-history-entry-insight"));

    const list = await utils.findByTestId("doc-history-list");
    expect(
      list.querySelectorAll(".doc-hist__item:not(.doc-hist__item--seed)").length
    ).toBeGreaterThan(0);
  });
});
