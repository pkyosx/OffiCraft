// components/BootDocPage.test.tsx — what the three boot-context blocks are
// bought on (T-791e, re-cut by T-c33e).
//
// Each assertion is written to fail on its own: they bind to different pieces
// of behaviour, so one of them going green cannot be mistaken for the others
// still being true.
//
//   1. Saving calls the REPLACE endpoint and the new content comes back onto
//      the page.
//   2. Version history lists the past versions.
//   3. The restore button calls the RESTORE endpoint, not the replace one.
//   4. Editing the claude document never sends the codex key (paired control:
//      the codex page shows the codex document).
//   5. Over the character limit is blocked IN THE COCKPIT, showing both the
//      current size and the limit — never a silent truncation.
//   6. seeds.ts's `?raw` text is used only as the FACTORY version; the page
//      body renders what the API currently holds (control: the API answers
//      with a string that is not the seed, and that string is what is on
//      screen).
//   7. A retained revision the server WOULD refuse is marked un-restorable
//      before the click.
//
// 🔴 AND THE T-c33e CLAIM, which is what the ticket is bought on: THESE THREE
// PAGES HAVE NO EDITOR OF THEIR OWN. They draw the shared <DocCard> — one
// textarea over the whole document, the same as 角色定義 and 使用者自訂 — and
// the per-section paste/apply/preview surface is gone. Two of the cases below
// assert that against the RENDERED page rather than against the source, because
// a page that grew its own editor back would still import DocCard.
//
// Everything runs against `api/mock.ts` — the shared adapter, never a
// hand-rolled fake. A fake that answered these calls itself would be measuring
// a server that does not exist, which is the failure mode api/dtoParity.ts was
// written about.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { BootDocPage } from "./BootDocPage";
import { api } from "../api";
import { __resetMock } from "../api/mock";
import {
  SEED_BOOT_SEQUENCE_MD,
  SEED_BOOT_SEQUENCE_CODEX_MD,
  SEED_SYSTEM_INTERACTION_MD,
} from "../api/seeds";
import { BOOT_DOC_CAP_CHARS_DEFAULTS } from "../api/docCap";
import { runeLength } from "../api/docCap";

const s = zh.settings;

function renderClaude() {
  return render(
    <I18nProvider>
      <BootDocPage
        kind="boot_sequence"
        docKey="claude"
        title={s.bootClaudeName}
        historyTitle={s.historyBootClaudeTitle}
        crumbs={[{ label: s.title }]}
      />
    </I18nProvider>
  );
}

function renderCodex() {
  return render(
    <I18nProvider>
      <BootDocPage
        kind="boot_sequence"
        docKey="codex"
        title={s.bootCodexName}
        historyTitle={s.historyBootCodexTitle}
        crumbs={[{ label: s.title }]}
      />
    </I18nProvider>
  );
}

function renderSystem() {
  return render(
    <I18nProvider>
      <BootDocPage
        kind="system_interaction"
        docKey="global"
        title={s.systemName}
        historyTitle={s.historyBootSystemTitle}
        crumbs={[{ label: s.title }]}
      />
    </I18nProvider>
  );
}

/** Open the editor once the document has landed and type `text` over the whole
 * of it — which is the only editing gesture this surface has now. */
async function typeWholeDoc(
  utils: ReturnType<typeof renderClaude>,
  text: string
) {
  const edit = await utils.findByTestId("doc-card-edit");
  await waitFor(() => expect((edit as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(edit);
  fireEvent.change(utils.getByTestId("doc-card-editor"), {
    target: { value: text },
  });
}

beforeEach(() => {
  __resetMock();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("BootDocPage", () => {
  it("save calls the REPLACE endpoint and renders the new content back", async () => {
    const save = vi.spyOn(api, "saveBootDoc");
    const utils = renderClaude();

    const before = await api.getBootDoc("boot_sequence", "claude");
    // Everything except the document's first line — the part a whole-document
    // save has to carry along with the new heading.
    const rest = before.text.split("\n").slice(1).join("\n");
    await typeWholeDoc(utils, `# 全新的啟動程序標題\n\n${rest}`);
    fireEvent.click(utils.getByTestId("doc-card-save"));
    fireEvent.click(await utils.findByTestId("doc-card-save-confirm-btn"));

    await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
    const [kind, key, text] = save.mock.calls[0];
    expect(kind).toBe("boot_sequence");
    expect(key).toBe("claude");
    expect(text.startsWith("# 全新的啟動程序標題")).toBe(true);
    // The write is a WHOLE-DOCUMENT replace and always was — what the editor
    // holds is what lands, so the rest of the document has to be in it.
    //
    // Assert the WHOLE remainder rather than one hand-picked heading from it.
    // The old probe named 「Claude Code 執行環境」, which happened to sit in the
    // middle of the document when it was written; the owner later rewrote the
    // seed so that heading became LINE ONE — the one line this fixture replaces
    // — and the assertion started failing on a document that was carried
    // perfectly. A probe that names a line by its content is a probe that goes
    // false when the document is re-ordered, and it goes false LOUDLY, on a
    // package that changed no frontend code at all.
    // toContain("") is vacuously true, so pin that the remainder is non-empty —
    // otherwise a fixture that shrank to a single line would pass this silently.
    expect(rest.length).toBeGreaterThan(0);
    expect(text).toContain(rest);

    // And the page now RENDERS the saved document: the heading came back from
    // the adapter's response, not from local state that was never confirmed.
    await utils.findByText("全新的啟動程序標題");
    // Back out of edit mode, so a second save cannot ride the first one's draft.
    await waitFor(() =>
      expect(utils.queryByTestId("doc-card-editor")).toBeNull()
    );
  });

  it("says on screen that a save replaces the WHOLE document", async () => {
    // 🔴 T-c33e. The per-section editor made this implicit — a row was the unit,
    // so nothing else could be at risk. With one box over the whole text, the
    // failure it prevents (pasting one proposed block over a long document
    // document and saving the rest away) is silent and only recoverable through
    // history, so the page has to SAY it. All three blocks, and before the
    // editor is opened as well as after: the sentence is useless if it only
    // appears once the paste has already happened.
    for (const open of [renderSystem, renderClaude, renderCodex]) {
      const utils = open();
      const note = await utils.findByTestId("doc-card-replace-note");
      expect(note.textContent).toBe(s.docReplaceNote);
      fireEvent.click(await utils.findByTestId("doc-card-edit"));
      expect(utils.getByTestId("doc-card-replace-note").textContent).toBe(
        s.docReplaceNote
      );
      utils.unmount();
    }
  });

  it("edits the whole document in ONE box — no per-section surface survives", async () => {
    // 🔴 T-c33e's acceptance condition, asserted on the rendered page: these
    // three blocks have no editor implementation of their own. One textarea,
    // covering the whole document, reached the same way 角色定義 is — and none
    // of the per-section affordances (paste / apply / discard / preview /
    // pending badge) exist any more.
    const utils = renderSystem();
    const stored = (await api.getBootDoc("system_interaction", "global")).text;

    // Nothing to edit per section, before or after the editor opens.
    expect(utils.queryAllByTestId(/^boot-doc-sec/)).toEqual([]);
    await typeWholeDoc(utils, "# 只有一個編輯框\n");
    expect(utils.queryAllByTestId(/^boot-doc-sec/)).toEqual([]);

    const boxes = utils.container.querySelectorAll("textarea");
    expect(boxes.length).toBe(1);
    // …and that box was seeded with the WHOLE document, not a slice of it.
    fireEvent.click(utils.getByText(s.cancel));
    fireEvent.click(utils.getByTestId("doc-card-edit"));
    expect((utils.getByTestId("doc-card-editor") as HTMLTextAreaElement).value).toBe(
      stored
    );
  });

  it("holds no editor state of its own — the shell is the shared component", async () => {
    // The rendered-page assertions above cannot see one thing: a page that
    // re-grew its own editor while still importing DocCard. This is the source
    // check for that, and it is the file the acceptance sentence names.
    const src = readFileSync(join(__dirname, "BootDocPage.tsx"), "utf8");
    expect(src).toContain('from "./DocCard"');
    expect(src).toContain("<DocCard");
    for (const forbidden of ["<textarea", "useState", "docSections", "renderBody"]) {
      expect(src, `BootDocPage must not hold ${forbidden}`).not.toContain(
        forbidden
      );
    }
  });

  it("version history lists the past versions", async () => {
    // Two writes ⇒ the state each replaced is retained. (The FIRST write to a
    // document that has never been customised replaces nothing and retains
    // nothing — server parity, see recordDocumentHistory — so a single save
    // would list zero and the assertion would be measuring that rule instead.)
    await api.saveBootDoc("boot_sequence", "claude", "第一版\n");
    await api.saveBootDoc("boot_sequence", "claude", "第二版\n");

    const utils = renderClaude();
    // 版本紀錄 stands in the EDIT toolbar, where 重置 used to — the same place
    // every other document in 設定 keeps it (T-c33e made these three consistent
    // with it; before, they had no edit mode to put it behind).
    fireEvent.click(await utils.findByTestId("doc-card-edit"));
    fireEvent.click(await utils.findByText(s.historyTitle));
    const list = await utils.findByTestId("doc-history-list");
    await waitFor(() =>
      expect(within(list).queryByText(s.historyLoading)).toBeNull()
    );
    // The list is a PICKER (owner 2026-07-31: no content preview on the rows),
    // so "lists the versions" is a claim about ROWS: the revision the second
    // write retained, plus the 初始版本 row that is always there.
    const rows = within(list).getAllByTestId(/^doc-history-open-\d+$/);
    expect(rows.length).toBe(1);
    expect(within(list).getByTestId("doc-history-seed")).toBeTruthy();

    // And the row really holds the version it claims to.
    fireEvent.click(rows[0]);
    const modal = await utils.findByTestId("doc-history-modal");
    await waitFor(() => expect(modal.textContent ?? "").toContain("第一版"));

    // The note states this document's OWN retention — the default sentence
    // says three, which is true of every other document and false of this one.
    expect(list.textContent ?? "").toContain("10");
  });

  it("the restore calls the RESTORE endpoint, not the replace one — and lives only in the history list", async () => {
    await api.saveBootDoc("boot_sequence", "claude", "被改壞的啟動程序\n");
    const reset = vi.spyOn(api, "resetBootDoc");
    const save = vi.spyOn(api, "saveBootDoc");

    const utils = renderClaude();
    await utils.findAllByText(/被改壞的啟動程序/);

    // 還原出廠版 is NOT a control of its own on this page. It stood here as a
    // top-level button until the owner overrode that on 2026-08-14 (card
    // rc-f1950f4d286e, option 2: "完全照 insight"), so the only door is the one
    // every other editable document uses. Without this line the row below could
    // be a second door and the case would not notice.
    expect(utils.queryByTestId("doc-card-reset")).toBeNull();

    fireEvent.click(await utils.findByTestId("doc-card-edit"));
    fireEvent.click(utils.getByTestId("doc-history-entry-boot_sequence"));
    fireEvent.click(await utils.findByTestId("doc-history-seed-open"));
    fireEvent.click(await utils.findByTestId("doc-history-modal-restore"));
    // Destructive, so it is confirmed — moving it must not have made it cheaper.
    expect(reset).not.toHaveBeenCalled();
    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    await waitFor(() => expect(reset).toHaveBeenCalledTimes(1));
    expect(reset.mock.calls[0]).toEqual(["boot_sequence", "claude"]);
    // 🔴 The whole point: a "restore" implemented as a replace carrying the
    // seed text would look identical on screen and would leave the document
    // marked as owner-edited forever — and, on a server whose seed file has
    // since changed, would write back the WRONG text.
    expect(save).not.toHaveBeenCalled();

    // Back on the factory version, and the page says so.
    await utils.findByText(s.defaultBadge);
  });

  it("a failed read says so and offers NO recovery door — the accepted cost of putting the restore behind edit mode", async () => {
    // ⚠️ This case asserts a COST, not a virtue, and the sentence matters
    // because the next person will read the gap as a bug and "fix" it. The
    // restore used to stand outside edit mode precisely because a broken boot
    // sequence means agents never come online, so there is nobody left to fix
    // it. The owner was shown that on card rc-f1950f4d286e and chose option 2
    // ("完全照 insight") anyway. So: read fails ⇒ no editor ⇒ no history entry
    // ⇒ no restore, and the page says only that the read failed.
    vi.spyOn(api, "getBootDoc").mockRejectedValue(new Error("boom"));
    const utils = renderClaude();

    await utils.findByText(s.loadError);
    expect((utils.getByTestId("doc-card-edit") as HTMLButtonElement).disabled).toBe(
      true
    );
    expect(utils.queryByTestId("doc-card-reset")).toBeNull();
    expect(utils.queryByTestId("doc-history-entry-boot_sequence")).toBeNull();
    utils.unmount();
    vi.restoreAllMocks();

    // PAIRED CONTROL. Every assertion above is an absence, and a page that
    // rendered nothing at all would satisfy all of them. So a second page whose
    // read SUCCEEDS must reach the restore — otherwise this case would stay
    // green with the recovery path deleted outright rather than merely gated.
    const ok = renderClaude();
    fireEvent.click(await ok.findByTestId("doc-card-edit"));
    fireEvent.click(ok.getByTestId("doc-history-entry-boot_sequence"));
    expect(await ok.findByTestId("doc-history-seed-open")).toBeTruthy();
  });

  it("editing the claude document never sends the codex key", async () => {
    const save = vi.spyOn(api, "saveBootDoc");
    const claude = renderClaude();
    await typeWholeDoc(claude, "# 只改 claude 這一份\n");
    fireEvent.click(claude.getByTestId("doc-card-save"));
    fireEvent.click(await claude.findByTestId("doc-card-save-confirm-btn"));
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1));

    expect(save.mock.calls.map((c) => c[1])).toEqual(["claude"]);
    expect(save.mock.calls.some((c) => c[1] === "codex")).toBe(false);
    claude.unmount();

    // PAIRED CONTROL. Asserting only "codex was not written" is satisfied by a
    // page that writes nothing at all, so the other half has to hold too: the
    // codex document is untouched AND it is a different document — its own
    // text, not the claude one it sits next to in the settings list.
    const codex = renderCodex();
    await codex.findAllByText(/Codex App Server 執行環境/);
    expect(codex.container.textContent).not.toContain("只改 claude 這一份");
    expect(await api.getBootDoc("boot_sequence", "codex")).toMatchObject({
      text: SEED_BOOT_SEQUENCE_CODEX_MD.trim(),
      isDefault: true,
    });
  });

  it("blocks an over-limit document in the cockpit, naming the size and the limit", async () => {
    const save = vi.spyOn(api, "saveBootDoc");
    const cap = BOOT_DOC_CAP_CHARS_DEFAULTS.boot_sequence;
    const utils = renderClaude();

    await typeWholeDoc(utils, "超".repeat(cap + 50));

    const notice = await utils.findByTestId("doc-card-over-cap");
    // BOTH numbers on screen. "Too long" alone leaves the owner with nothing to
    // act on, and a cockpit that trimmed the text to fit would be worse still:
    // the agent would boot from a document the owner never wrote.
    expect(notice.textContent).toContain(String(cap));
    const shown = Number(/\d{4,}/.exec(notice.textContent ?? "")?.[0]);
    expect(shown).toBe(cap + 50);
    expect(shown).toBeGreaterThan(cap);
    expect(shown).toBeGreaterThan([...SEED_BOOT_SEQUENCE_MD.trim()].length);

    // Refused, not truncated: the save door is shut and clicking it sends
    // nothing.
    const saveBtn = utils.getByTestId("doc-card-save") as HTMLButtonElement;
    expect(saveBtn.disabled).toBe(true);
    fireEvent.click(saveBtn);
    expect(utils.queryByTestId("doc-card-save-confirm")).toBeNull();
    expect(save).not.toHaveBeenCalled();

    // The usage readout carries the same pair, and it follows the DRAFT — a
    // readout frozen at the stored size cannot say anything about the text
    // about to be sent.
    expect(utils.getByTestId("doc-card-usage").textContent).toBe(
      `${shown} / ${cap}`
    );
  });

  it("marks a revision the raised-then-lowered cap now refuses as un-restorable", async () => {
    // The only way an over-cap revision exists at all: the owner RAISED the
    // cap, wrote a long version, then put the cap back. Which is why the
    // marking cannot judge by the shipped default — it has to read the
    // `doc.cap_chars.system_interaction` setting that is in force NOW.
    const shipped = BOOT_DOC_CAP_CHARS_DEFAULTS.system_interaction;
    await api.patchServerSettings({
      docCapCharsSystemInteraction: shipped + 10000,
    });
    const overCap = "字".repeat(shipped + 5000);
    // The first write to an untouched document retains nothing, so the long
    // text has to be the one the SECOND write replaces.
    await api.saveBootDoc("system_interaction", "global", overCap);
    await api.saveBootDoc("system_interaction", "global", "短\n");
    await api.patchServerSettings({ docCapCharsSystemInteraction: shipped });

    const utils = renderSystem();
    fireEvent.click(await utils.findByTestId("doc-card-edit"));
    fireEvent.click(
      await utils.findByTestId("doc-history-entry-system_interaction")
    );
    const list = await utils.findByTestId("doc-history-list");
    await waitFor(() =>
      expect(within(list).queryByText(s.historyLoading)).toBeNull()
    );

    const [target] = await api.listDocumentHistory(
      "system_interaction",
      "global"
    );
    // The DIRECTORY row is what the marking reads since T-1170 — the list
    // never sees this text, only how much of it there is.
    expect(target.sizes.text).toBe(runeLength(overCap));

    const row = await utils.findByTestId(`doc-history-item-${target.id}`);
    expect(within(row).getByText(s.historyBlockedBadge)).toBeTruthy();
    const reason = utils.getByTestId(`doc-history-blocked-${target.id}`);
    expect(reason.textContent).toContain(s.historyField.text);
    expect(reason.textContent).toContain(String(shipped));

    // …and opening it is not a way around the verdict either.
    fireEvent.click(utils.getByTestId(`doc-history-open-${target.id}`));
    expect(
      (utils.getByTestId("doc-history-modal-restore") as HTMLButtonElement)
        .disabled
    ).toBe(true);
  });

  it("renders the API's current version; the ?raw seed is only the factory version", async () => {
    // The control: make the API answer with a document that is NOT the seed,
    // then look at what the page body actually renders. Before T-791e these
    // pages rendered SEED_SYSTEM_INTERACTION_MD directly, and that version of
    // this page would pass every other assertion in this file while showing
    // the owner a document no agent boots from.
    const OWNER_TEXT = "# 這是 owner 現在的版本\n\n只有 API 知道這一段。\n";
    await api.saveBootDoc("system_interaction", "global", OWNER_TEXT);

    const utils = renderSystem();
    await utils.findAllByText("這是 owner 現在的版本");
    expect(utils.container.textContent).toContain("只有 API 知道這一段。");
    // The seed's own opening heading is nowhere on the page.
    const seedHeading = SEED_SYSTEM_INTERACTION_MD.split("\n")[0].replace(
      /^#+ /,
      ""
    );
    expect(utils.container.textContent).not.toContain(seedHeading);
    // …and it is not the default any more, which is the same claim said in the
    // cockpit's own vocabulary.
    expect(utils.queryByText(s.defaultBadge)).toBeNull();

    // The seed did not stop existing — it is what 還原出廠版 goes back to.
    expect(
      await api.getDocumentSeed("system_interaction", "global")
    ).toMatchObject({ content: { text: SEED_SYSTEM_INTERACTION_MD.trim() } });
  });

  it("says nothing above the card — the standing notes block is gone, and its one surviving fact moved into the history list", async () => {
    // Three bullets used to stand here (what a save affects, how many revisions
    // are kept, what the cap does). The owner asked for them out on 2026-08-14
    // with an argument that generalises: if that explanation were needed, EVERY
    // editable context block would need one — and none of the others carry it.
    // So this page must not be special.
    const utils = renderSystem();
    expect(utils.queryByTestId("boot-doc-notes")).toBeNull();
    expect(utils.container.querySelector(".boot-doc__notes")).toBeNull();

    // PAIRED CONTROL, because the two lines above are absences. The retention
    // number was the one bullet stating something the reader genuinely cannot
    // derive (these three keep 10, everything else keeps 3), and it did not
    // vanish with the block — it is on the surface that USES it.
    fireEvent.click(await utils.findByTestId("doc-card-edit"));
    fireEvent.click(utils.getByTestId("doc-history-entry-system_interaction"));
    const list = await utils.findByTestId("doc-history-list");
    expect(list.textContent ?? "").toContain("10");
  });

  it("warns about the silent boot failure before saving a boot sequence, and does not cry wolf on the system block", async () => {
    const claude = renderClaude();
    await typeWholeDoc(claude, "# x\n");
    fireEvent.click(claude.getByTestId("doc-card-save"));
    const bootConfirm = await claude.findByTestId("doc-card-save-confirm");
    expect(bootConfirm.textContent).toContain(s.bootDocSaveConfirmBoot);
    claude.unmount();

    // The system-interaction block gets its OWN copy, because the boot-failure
    // sentence is not true of it — an agent with a mangled system block still
    // comes online. A warning that is false for the document on screen teaches
    // the reader to dismiss the one that is true.
    const system = renderSystem();
    await typeWholeDoc(system, "# y\n");
    fireEvent.click(system.getByTestId("doc-card-save"));
    const systemConfirm = await system.findByTestId("doc-card-save-confirm");
    expect(systemConfirm.textContent).toContain(s.bootDocSaveConfirmSystem);
    expect(systemConfirm.textContent).not.toContain(s.bootDocSaveConfirmBoot);
  });

  it("refuses a save that would change nothing", async () => {
    // A no-op write is not harmless here: it flips the document out of 預設 for
    // ever, and "is this still the factory version" is the question people ask
    // about these three. (角色定義 / 使用者自訂 keep their unconditional 完成
    // 編輯 — `requireDirty` is opt-in precisely so this does not arrive under
    // them.)
    const save = vi.spyOn(api, "saveBootDoc");
    const utils = renderClaude();
    fireEvent.click(await utils.findByTestId("doc-card-edit"));
    expect((utils.getByTestId("doc-card-save") as HTMLButtonElement).disabled).toBe(
      true
    );

    fireEvent.change(utils.getByTestId("doc-card-editor"), {
      target: { value: "# 真的改了\n" },
    });
    expect((utils.getByTestId("doc-card-save") as HTMLButtonElement).disabled).toBe(
      false
    );
    expect(save).not.toHaveBeenCalled();
  });
});
