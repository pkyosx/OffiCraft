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
//     doc.cap_chars.insight setting, and the settings surface that otherwise shows it is
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
import { DOC_CAP_CHARS_DEFAULTS } from "../api/docCap";

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
    // Insight goes BEFORE Learning (Duty → Insight → Learning, owner ruling
    // 2026-08-03).
    expect(
      lessons!.compareDocumentPosition(card!) &
        Node.DOCUMENT_POSITION_PRECEDING
    ).toBeTruthy();
  });

  it("mounts on a CUSTOM role's page too", async () => {
    // The DoD says "every role", and a seed-only implementation passes a
    // seed-only test. Custom roles are the half a keyed-off-the-seed-list
    // mistake would drop.
    // T-91: createRole answers the minted ids only, so the display name the
    // page is opened by is the one this test passed in.
    await mockApi.createRole({ name: "臨時角色" });
    const utils = await openRolePage("臨時角色");
    expect(insightCard(utils)).toBeTruthy();
  });

  it("an untouched doc with NO seed reads as EMPTY — not as loading, not as an error", async () => {
    // ⚠️ CONTRACT CHANGE (T-e1e3): this used to use `assistant`. It cannot any
    // more — the assistant now ships with a FACTORY insight seed, so its
    // untouched doc is deliberately non-empty. The property being pinned is
    // unchanged and still real: a role with no seed of its own reads empty, and
    // that empty must be its own reading rather than the load or error state.
    await mockApi.createRole({ name: "無 seed 的角色" });
    const utils = await openRolePage("無 seed 的角色");
    const card = insightCard(utils)!;

    expect(within(card).getByText(mp.insightEmpty)).toBeTruthy();
    // The three states are distinct strings on purpose; the empty one must not
    // be reachable by rendering either of the other two.
    expect(within(card).queryByText(mp.insightLoading)).toBeNull();
    expect(within(card).queryByText(mp.insightError)).toBeNull();
    // And an absence is never labelled 「預設」 — that badge names FACTORY
    // wording, not the lack of anything.
    expect(within(card).queryByTestId("insight-default-badge")).toBeNull();
  });

  it("the assistant's untouched doc serves the FACTORY seed, badged 「預設」", async () => {
    // 🔴 ACCEPTANCE #4 — "the cockpit must not show factory content as if a
    // person wrote it". Before T-e1e3 this card never read `isDefault` at all,
    // so shipped wording would have rendered exactly like an authored document
    // with nothing anywhere to say otherwise.
    const utils = await openRolePage(zh.office.role.assistant);
    const card = insightCard(utils)!;

    // The seed is really being served (anti-tautology for the badge assertion:
    // a badge on an empty card would prove nothing).
    expect(within(card).queryByText(mp.insightEmpty)).toBeNull();
    const badge = within(card).getByTestId("insight-default-badge");
    expect(badge.textContent).toBe(s.defaultBadge);
  });

  it("the badge disappears once the role writes its own", async () => {
    // The VALUE half: a badge that is always rendered would satisfy the
    // assertion above while telling the owner nothing.
    await mockApi.saveInsight("assistant", "# 我自己寫的判準\n");
    const utils = await openRolePage(zh.office.role.assistant);
    const card = insightCard(utils)!;
    expect(within(card).queryByTestId("insight-default-badge")).toBeNull();
  });

  it("a seed is PER-ROLE — a custom role never inherits the assistant's", async () => {
    // 🔴 The shape this ticket is most likely to be got wrong, mirrored on the
    // client: `api/mock.ts` must fold a MAP keyed by role, not one shared
    // constant. A mock that copied the lessons shape would make the cockpit
    // look correct against a server that is wrong in the same way.
    const { roleKey } = await mockApi.createRole({ name: "測試員" });
    const mine = await mockApi.getInsight(roleKey);
    const assistant = await mockApi.getInsight("assistant");
    expect(assistant.text.trim()).not.toBe("");
    expect(mine.text).toBe("");
    expect(mine.isDefault).toBe(true);
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
    // ⚠️ Moved off `assistant` for the same reason as the empty-state test: its
    // untouched doc is no longer zero-length. Zero is exactly when someone is
    // about to write the first thing into the doc, so it is the worst moment to
    // hide the limit — a role with no seed is where that state now lives.
    await mockApi.createRole({ name: "零字角色" });
    const utils = await openRolePage("零字角色");
    const size = insightCard(utils)!.querySelector(".mp-insight__size");
    expect(size?.textContent?.replace(/\s+/g, " ").trim()).toBe(
      `0 / ${DOC_CAP_CHARS_DEFAULTS.insight}`
    );
  });

  it("the header's numbers are the SERVED ones, not recomputed locally", async () => {
    // A card that counted `text.length` itself would pass a fixed-string test
    // and then disagree with the server on every multi-byte doc and on any cap
    // the owner has raised. Both numbers must come off the wire.
    // A cap the owner RAISED — derived from the shipped default so it stays
    // above the floor (the knob only ever goes up) whatever that default is.
    const raised = DOC_CAP_CHARS_DEFAULTS.insight + 2345;
    await mockApi.patchServerSettings({ docCapCharsInsight: raised });
    await mockApi.saveInsight("assistant", "判準");
    const utils = await openRolePage(zh.office.role.assistant);
    const size = insightCard(utils)!.querySelector(".mp-insight__size");
    expect(size?.textContent?.replace(/\s+/g, " ").trim()).toBe(`2 / ${raised}`);
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

  // T-6501: the 初始版本 row is the ONLY way back to the factory insight, and
  // DocumentHistoryEntry only grows it where `onReset` is wired.
  //
  // 🔴 BOTH DIRECTIONS ARE ASSERTED, and the negative one is what makes this
  // worth having: an implementation that wires `onReset` UNCONDITIONALLY passes
  // the positive test perfectly, and the only symptom is that every custom role
  // is offered a reset the server 404s. One assertion here would ship that.
  it("offers 初始版本 on a role that HAS a factory insight", async () => {
    // Written first, so this also pins that the row survives the role having
    // its own doc — hasSeed answers what exists to fall back TO, not what is
    // being read, and that is exactly when a reset is worth offering.
    await mockApi.saveInsight("assistant", "這一份是角色自己寫的");
    const utils = await openRolePage(zh.office.role.assistant);
    const card = insightCard(utils)!;

    fireEvent.click(within(card).getByText(s.edit));
    fireEvent.click(within(card).getByTestId("doc-history-entry-insight"));

    const list = await utils.findByTestId("doc-history-list");
    expect(within(list).getByTestId("doc-history-seed")).toBeTruthy();
  });

  it("offers NO 初始版本 on a role with no factory insight", async () => {
    await mockApi.createRole({ name: "沒有出廠判準的角色" });
    const utils = await openRolePage("沒有出廠判準的角色");
    const card = insightCard(utils)!;

    fireEvent.click(within(card).getByText(s.edit));
    fireEvent.click(within(card).getByTestId("doc-history-entry-insight"));

    const list = await utils.findByTestId("doc-history-list");
    expect(within(list).queryByTestId("doc-history-seed")).toBeNull();
  });

  // ⚠️ CONTRACT CHANGE (T-40f0, owner rc-28885813e065 ①): the 初始版本 row no
  // longer jumps straight to a reset confirmation of its own. It opens the same
  // reader every other row opens, and the reset now rides the ONE destructive
  // confirm that lives in DocumentHistoryModal (`doc-history-restore-confirm`),
  // shared with every restore. The capability pinned here is UNCHANGED and is
  // the InsightCard-specific half of it, which no other file covers:
  // SettingsPage.document-history.test.tsx walks this flow on 全域情境 and on a
  // role DEFINITION, and neither of those touches `resetInsight`, `hasSeed`, or
  // the Insight card's own re-read. What this test owns is the HOST WIRING —
  // that InsightCard's `onReset` reaches the insight reset for THIS role, and
  // that the card on screen follows it.
  it("the 初始版本 row restores the factory insight, behind the shared confirm", async () => {
    const seed = (await mockApi.getInsight("assistant")).text;
    const written = "這一份是角色自己寫的，不是出廠版。";
    expect(written).not.toBe(seed); // anti-tautology: the reset must MOVE something
    await mockApi.saveInsight("assistant", written);

    const utils = await openRolePage(zh.office.role.assistant);
    const card = insightCard(utils)!;
    expect(within(card).getByText(written)).toBeTruthy();

    fireEvent.click(within(card).getByText(s.edit));
    fireEvent.click(within(card).getByTestId("doc-history-entry-insight"));
    fireEvent.click(await utils.findByTestId("doc-history-seed-open"));

    // Reading is not restoring. Opening the seed row wires straight into
    // InsightCard's `onReset`, so a host that fired it on open — or a modal
    // whose restore button skipped the confirm — would wipe the role's own
    // judgement with no one asked. The document must still be the owner's.
    const restore = await utils.findByTestId("doc-history-modal-restore");
    expect((await mockApi.getInsight("assistant")).text).toBe(written);

    fireEvent.click(restore);
    // The confirm is the ONE shared destructive dialog in DocumentHistoryModal
    // — the Duty row, every ordinary restore and this reset go through the same
    // implementation, so there is no second one to keep in step.
    expect(utils.getByTestId("doc-history-restore-confirm")).toBeTruthy();
    expect((await mockApi.getInsight("assistant")).text).toBe(written);
    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    expect((await mockApi.getInsight("assistant")).text).toBe(seed);
    // And the card followed: the written doc is gone and the 「預設」 badge is
    // back (is_default flipped). Asserting the seed's own prose instead would
    // be brittle — the renderer splits markdown across nodes — and would say
    // less: the badge IS the "this is factory wording" claim.
    expect(
      await utils.findByTestId("insight-default-badge")
    ).toBeTruthy();
    expect(within(insightCard(utils)!).queryByText(written)).toBeNull();
  });
});
