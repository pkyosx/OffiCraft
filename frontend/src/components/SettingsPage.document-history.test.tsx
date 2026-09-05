// 版本紀錄 (T-7d33) on the Settings doc pages: the retained revisions of each
// editable long-form document, and the owner's restore.
//
// The three things worth pinning:
//   1. the list renders one row per retained revision, with WHEN, WHO and a
//      preview of the content that tells two versions apart;
//   2. restore is CONFIRMED first (it overwrites the live doc), then rides the
//      adapter and refreshes BOTH the visible document and the list;
//   3. every string comes from the dictionary — no surface holds literals.
//
// T-1f39 moved restore off the row and into the modal the row opens. The owner's
// 2026-07-31 ruling then moved the history ITSELF: it is no longer a card under
// the editor but a 版本紀錄 button standing where 重置 stood, in the EDIT
// toolbar, and 重置 survives as the 初始版本 row at the bottom of the list it
// opens. Three consequences are pinned here on top of everything above:
//   - the history is not fetched until that button is clicked (「有點選的時候再
//     打 API 就可以」);
//   - 初始版本 appears exactly where a file seed exists and nowhere else;
//   - resetting now goes through that row, and through the same destructive
//     confirmation the restore uses — it has no button of its own left.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";
import { mockApiError } from "../api/errorCodes";
import { DOC_CAP_CHARS_DEFAULT } from "../api/docCap";
import { runeLength } from "../api/docCap";
import type { DocumentKind } from "../types";

const s = zh.settings;

type Utils = ReturnType<typeof render>;

/** Render Settings and land on 全域情境 › 使用者自訂 — the global-context block,
 * the simplest of the documents that carry a history. */
async function openUserCustomDoc() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.globalContext));
  await utils.findByText(s.systemName);
  fireEvent.click(utils.getByText(s.customName));
  await utils.findByText(s.edit);
  return utils;
}

/** Enter edit mode on the FIRST doc card of the page (the role page's second
 * editor is the lessons card, which has its own entry). */
function startEditing(utils: Utils) {
  fireEvent.click(utils.getAllByText(s.edit)[0]);
}

/** 編輯 → 版本紀錄: the only route to the history since the owner's ruling. */
async function openHistory(utils: Utils, kind: string) {
  startEditing(utils);
  fireEvent.click(utils.getByTestId(`doc-history-entry-${kind}`));
  return utils.findByTestId("doc-history-list");
}

beforeEach(() => {
  __resetMock();
  // Several cases below make an adapter method REJECT. A case that fails before
  // its own mockRestore would otherwise hand that rejection to every test after
  // it, turning one red into a cascade that hides which line actually broke.
  vi.restoreAllMocks();
});

describe("SettingsPage · 版本紀錄", () => {
  // The ruling's own words: 「有點選的時候再打 API 就可以」. Opening an editor is
  // not opening the history, and a card that loaded on mount would look
  // identical on screen — this is the assertion that tells them apart.
  it("asks the server for nothing until the entry is clicked", async () => {
    await mockApi.saveGlobalContext("第零版");
    await mockApi.saveGlobalContext("第一版");
    const list = vi.spyOn(mockApi, "listDocumentHistory");

    const utils = await openUserCustomDoc();
    expect(list).not.toHaveBeenCalled();
    // Not even entering edit mode, which is where the entry lives.
    startEditing(utils);
    expect(utils.getByTestId("doc-history-entry-global_context")).toBeTruthy();
    expect(list).not.toHaveBeenCalled();

    fireEvent.click(utils.getByTestId("doc-history-entry-global_context"));
    await waitFor(() =>
      expect(list).toHaveBeenCalledWith("global_context", "global")
    );
    list.mockRestore();
  });

  // The relocation is only real if the old surface is GONE. A card still
  // mounted under the editor would keep every other test here passing while
  // the owner still met the version list he asked to have moved.
  it("shows no version history under the editor, in view or in edit mode", async () => {
    await mockApi.saveGlobalContext("第零版");
    await mockApi.saveGlobalContext("第一版");

    const utils = await openUserCustomDoc();
    expect(utils.queryByTestId("doc-history-list")).toBeNull();
    expect(utils.container.querySelector(".doc-hist__list")).toBeNull();
    startEditing(utils);
    expect(utils.container.querySelector(".doc-hist__list")).toBeNull();
  });

  it("says a never-edited document has no retained revisions", async () => {
    const utils = await openUserCustomDoc();
    await openHistory(utils, "global_context");
    expect(utils.getByText(s.historyGlobalTitle)).toBeTruthy();
    // The global block HAS a seed, so its list is never truly empty — it holds
    // the 初始版本 row and nothing else.
    expect(utils.container.querySelectorAll(".doc-hist__item")).toHaveLength(1);
    expect(utils.getByTestId("doc-history-seed")).toBeTruthy();
    // The global-context block cannot be deleted, so the delete-scope footnote
    // would be false here and is not shown.
    expect(utils.queryByTestId("doc-history-scope-note")).toBeNull();
  });

  it("lists each retained revision by WHEN and WHO — the content stays one click deeper", async () => {
    // The FIRST write retains nothing (a default document has no previous
    // version), so the retained set starts at the second one.
    await mockApi.saveGlobalContext("第零版：草稿");
    await mockApi.saveGlobalContext("第一版：多用 emoji");
    await mockApi.saveGlobalContext("第二版：少用 emoji");
    await mockApi.resetGlobalContext();
    await mockApi.saveGlobalContext("第三版：重寫");

    const utils = await openUserCustomDoc();
    await openHistory(utils, "global_context");
    const rows = await waitFor(() => {
      const found = utils.container.querySelectorAll(
        ".doc-hist__item:not(.doc-hist__item--seed)"
      );
      expect(found).toHaveLength(3);
      return found;
    });

    // Newest first: the top row is what the LAST write replaced — the state the
    // reset left behind, so the seed-state badge says so.
    expect(within(rows[0] as HTMLElement).getByText(s.historyDefaultBadge)).toBeTruthy();
    // Who wrote it, through the dictionary label (never a bare id on its own).
    expect((rows[1] as HTMLElement).textContent).toContain(s.historyByLabel);
    // The list is a PICKER (owner 2026-07-31): NO revision's content is on it,
    // so three long revisions cannot bury the one the reader came for.
    for (const row of rows) {
      expect((row as HTMLElement).textContent).not.toContain("第二版：少用 emoji");
      expect((row as HTMLElement).textContent).not.toContain("第一版：多用 emoji");
    }

    // Distinguishable all the same — one click deeper, the row opens ITS OWN
    // revision, which is what the on-list preview used to prove.
    fireEvent.click(
      (rows[1] as HTMLElement).querySelector(".doc-hist__row") as HTMLElement
    );
    // …and it FETCHES that revision (T-1170) rather than showing something the
    // list already had — the await is the round trip, and the pane says
    // 「載入中」 in the meantime instead of drawing an empty document.
    expect(
      utils.getByTestId("doc-history-content-loading")
    ).toBeTruthy();
    await waitFor(() =>
      expect(utils.getByTestId("doc-history-modal").textContent).toContain(
        "第二版：少用 emoji"
      )
    );
  });

  it("restore asks first, then rides the adapter and refreshes doc and list", async () => {
    await mockApi.saveGlobalContext("原本的內容");
    await mockApi.saveGlobalContext("後來改壞的內容");
    const restore = vi.spyOn(mockApi, "restoreDocumentHistory");

    const utils = await openUserCustomDoc();
    await utils.findByText("後來改壞的內容");
    await openHistory(utils, "global_context");

    const [target] = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    // The row opens the version; restore lives inside it (T-1f39).
    fireEvent.click(await utils.findByTestId(`doc-history-open-${target.id}`));
    fireEvent.click(
      within(utils.getByTestId("doc-history-modal")).getByTestId(
        "doc-history-modal-restore"
      )
    );

    // Nothing has fired yet — the confirmation is the gate, not a courtesy.
    expect(restore).not.toHaveBeenCalled();
    const modal = utils.getByTestId("doc-history-restore-confirm");
    expect(within(modal).getByText(s.historyRestoreConfirmAction)).toBeTruthy();

    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    await waitFor(() =>
      expect(restore).toHaveBeenCalledWith("global_context", "global", target.id)
    );
    // The visible document is re-read, not assumed — and the editor's draft,
    // which is now a pending overwrite of the version just restored, is gone
    // with it. Scoped to the doc body: the restored text is ALSO in the
    // history preview, and asserting on the page as a whole would pass even if
    // the editor never refreshed.
    await waitFor(() =>
      expect(
        utils.container.querySelector(".doc-card__body .doc-md")?.textContent
      ).toContain("原本的內容")
    );
    expect(utils.queryByTestId("doc-history-restore-confirm")).toBeNull();
    // A finished restore closes the reader with its dialog, and the list with
    // it: leaving them up would show the version the owner just replaced as if
    // it were still pending.
    expect(utils.queryByTestId("doc-history-modal")).toBeNull();
    expect(utils.queryByTestId("doc-history-list")).toBeNull();

    // …and the list, reopened, now holds a revision carrying the content the
    // restore overwrote — read where content lives since the list became a
    // picker: inside the version.
    await openHistory(utils, "global_context");
    const newest = await waitFor(() => {
      const row = utils.container.querySelector(
        ".doc-hist__item:not(.doc-hist__item--seed) .doc-hist__row"
      );
      expect(row).toBeTruthy();
      return row as HTMLElement;
    });
    fireEvent.click(newest);
    await waitFor(() =>
      expect(utils.getByTestId("doc-history-modal").textContent).toContain(
        "後來改壞的內容"
      )
    );
    restore.mockRestore();
  });

  // The move is only real if the old entry point is GONE. A row that still
  // carried a restore button would leave the destructive action with two doors
  // — and every other test here would keep passing through the new one.
  it("offers no restore control on the row itself — only inside the version", async () => {
    await mockApi.saveGlobalContext("原本的內容");
    await mockApi.saveGlobalContext("後來改壞的內容");

    const utils = await openUserCustomDoc();
    await openHistory(utils, "global_context");
    const [target] = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    const row = await utils.findByTestId(`doc-history-item-${target.id}`);
    expect(within(row).queryByText(s.historyRestore)).toBeNull();
    expect(utils.queryByTestId(`doc-history-restore-${target.id}`)).toBeNull();

    fireEvent.click(utils.getByTestId(`doc-history-open-${target.id}`));
    expect(
      within(utils.getByTestId("doc-history-modal")).getByText(s.historyRestore)
    ).toBeTruthy();
    // Reading one version is a step INTO the list, and there is a step back:
    // comparing a second version must not mean leaving the history and
    // re-entering it through the editor.
    fireEvent.click(utils.getByTestId("doc-history-modal-back"));
    expect(utils.queryByTestId("doc-history-modal")).toBeNull();
    expect(utils.getByTestId("doc-history-list")).toBeTruthy();
  });

  // The modal's diff needs the document the page is ALREADY rendering — no
  // second fetch, no new route. That thread is invisible until something
  // compares: a page that forgets to pass it degrades to 「還沒載入」, which
  // looks like a slow network rather than a missing prop.
  it("diffs a version against the document the page itself is showing", async () => {
    await mockApi.saveGlobalContext("第一行\n原本的第二行\n第三行");
    await mockApi.saveGlobalContext("第一行\n改壞的第二行\n第三行");

    const utils = await openUserCustomDoc();
    await openHistory(utils, "global_context");
    const [target] = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    fireEvent.click(await utils.findByTestId(`doc-history-open-${target.id}`));
    fireEvent.click(utils.getByTestId("doc-history-pane-diff"));

    // The `-` side is the revision's OWN text, which is fetched (T-1170), so
    // the diff exists only once that read lands.
    await waitFor(() =>
      expect(
        utils.container.querySelectorAll(".diff-view__row").length
      ).toBeGreaterThan(0)
    );
    const rows = Array.from(
      utils.container.querySelectorAll(".diff-view__row")
    ).map((row) =>
      Array.from(row.querySelectorAll("td")).map((td) => td.textContent ?? "")
    );
    expect(rows).toEqual([
      ["1", "1", " ", "第一行"],
      ["2", "", "-", "原本的第二行"],
      ["", "2", "+", "改壞的第二行"],
      ["3", "3", " ", "第三行"],
    ]);
  });

  it("diffs a role definition against the role on screen", async () => {
    const { roleKey } = await mockApi.createRole({ name: "臨時角色" });
    await mockApi.saveRole(roleKey, { definitionMd: "第一版定義" });
    await mockApi.saveRole(roleKey, { definitionMd: "第二版定義" });

    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.roles));
    fireEvent.click(await utils.findByText("臨時角色"));
    await utils.findAllByText(s.edit);
    await openHistory(utils, "role_definition");

    const [target] = await mockApi.listDocumentHistory(
      "role_definition",
      roleKey
    );
    fireEvent.click(await utils.findByTestId(`doc-history-open-${target.id}`));
    fireEvent.click(utils.getByTestId("doc-history-pane-diff"));

    await waitFor(() =>
      expect(
        utils.getByTestId("doc-history-diff-definition_md").textContent
      ).toContain("第二版定義")
    );
    expect(utils.queryByTestId("doc-history-diff-pending")).toBeNull();
  });

  it("cancelling the confirmation leaves the document alone", async () => {
    await mockApi.saveGlobalContext("更早的內容");
    await mockApi.saveGlobalContext("目前的內容");
    const restore = vi.spyOn(mockApi, "restoreDocumentHistory");

    const utils = await openUserCustomDoc();
    await openHistory(utils, "global_context");
    const [target] = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    fireEvent.click(await utils.findByTestId(`doc-history-open-${target.id}`));
    fireEvent.click(utils.getByTestId("doc-history-modal-restore"));
    fireEvent.click(
      within(utils.getByTestId("doc-history-restore-confirm")).getByText(
        s.cancel
      )
    );

    expect(restore).not.toHaveBeenCalled();
    expect(utils.queryByTestId("doc-history-restore-confirm")).toBeNull();
    // Cancelling the CONFIRMATION drops back to reading the version — it does
    // not also close the reader the owner opened.
    expect(utils.getByTestId("doc-history-modal")).toBeTruthy();
    expect((await mockApi.getGlobalContext()).text).toBe("目前的內容");
    restore.mockRestore();
  });

  it("keeps a failed restore honest: the dialog stays open with the reason", async () => {
    await mockApi.saveGlobalContext("更早的內容");
    await mockApi.saveGlobalContext("目前的內容");
    const restore = vi
      .spyOn(mockApi, "restoreDocumentHistory")
      .mockRejectedValue(new Error("boom"));

    const utils = await openUserCustomDoc();
    await openHistory(utils, "global_context");
    const [target] = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    fireEvent.click(await utils.findByTestId(`doc-history-open-${target.id}`));
    fireEvent.click(utils.getByTestId("doc-history-modal-restore"));
    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    await utils.findByText(s.historyRestoreError);
    expect(utils.getByTestId("doc-history-restore-confirm")).toBeTruthy();
    // …and the reader stays up behind it: closing on failure would take the
    // reason, and the version, off the screen together.
    expect(utils.getByTestId("doc-history-modal")).toBeTruthy();
    restore.mockRestore();
  });

  // The other half of "honest": the restore POST landed and the document IS
  // overwritten, only the list re-read behind it failed. Reporting that as
  // 還原失敗 leaves the pre-restore content on screen under a failure message,
  // and the owner's rational answer — confirm again — restores a second time
  // and burns one of the three retained slots on a no-op.
  it("does not report a restore that SUCCEEDED as failed when the list refresh behind it fails", async () => {
    await mockApi.saveGlobalContext("原本的內容");
    await mockApi.saveGlobalContext("後來改壞的內容");
    const restore = vi.spyOn(mockApi, "restoreDocumentHistory");
    const list = vi.spyOn(mockApi, "listDocumentHistory");

    const utils = await openUserCustomDoc();
    await utils.findByText("後來改壞的內容");
    await openHistory(utils, "global_context");
    const [target] = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    fireEvent.click(await utils.findByTestId(`doc-history-open-${target.id}`));
    fireEvent.click(utils.getByTestId("doc-history-modal-restore"));

    // Fails from here on — the restore itself still goes through.
    list.mockRejectedValue(new Error("boom"));
    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    await waitFor(() => expect(restore).toHaveBeenCalledTimes(1));
    // A restore that landed closes its dialog and the reader above it, exactly
    // as it does when the refresh works.
    await waitFor(() =>
      expect(utils.queryByTestId("doc-history-restore-confirm")).toBeNull()
    );
    expect(utils.queryByText(s.historyRestoreError)).toBeNull();
    expect(utils.queryByTestId("doc-history-modal")).toBeNull();
    expect(utils.queryByTestId("doc-history-list")).toBeNull();
    // The document is re-read and shows the restored text: the visible doc does
    // not hang on a list the owner is no longer looking at.
    await waitFor(() =>
      expect(
        utils.container.querySelector(".doc-card__body .doc-md")?.textContent
      ).toContain("原本的內容")
    );
    // …and above all: exactly ONE restore. A second one is a lost version.
    expect(restore).toHaveBeenCalledTimes(1);
    list.mockRestore();
    restore.mockRestore();
  });

  // ── 初始版本 (owner 2026-07-31, reshaped by T-40f0 rc-28885813e065 ①) ─────
  // 重置 lost its button; the seed became the list's last row. Two halves have
  // to hold at once, and a test that only checked one would let the other rot:
  // the row must BE there where a seed exists, and must NOT be there where the
  // server would 404 the reset.
  //
  // T-40f0 changed WHAT the row opens, not where it is: it used to jump straight
  // to the reset confirmation (the only row in the list that did), because the
  // seed's content was never sent to the cockpit at all. It now opens the same
  // reader every other row opens, so the owner can SEE what going back would
  // change before deciding.

  // 🔴 RED LINE. Looking is not restoring. Opening the row, reading its content
  // and comparing it against the live document must not write anything — a
  // reset replaces every word the owner has ever written into that block.
  it("opens 初始版本 for READING, and looking at it restores nothing", async () => {
    await mockApi.saveGlobalContext("寫壞的內容");
    const reset = vi.spyOn(mockApi, "resetGlobalContext");

    const utils = await openUserCustomDoc();
    await utils.findByText("寫壞的內容");
    await openHistory(utils, "global_context");
    fireEvent.click(await utils.findByTestId("doc-history-seed-open"));

    // The SAME reader every other version opens, named 初始版本 rather than
    // given a fabricated timestamp and 修改者.
    const modal = await utils.findByTestId("doc-history-modal");
    expect(within(modal).getByText(s.historySeedTitle)).toBeTruthy();
    expect(modal.textContent).not.toContain(s.historyByLabel);

    // And the comparison the owner asked for. Side convention is the SAME as
    // every other row's — `-` is the version being looked at, `+` is what the
    // server stores now — so with an EMPTY default the owner's whole block shows
    // up as `+`: everything restoring would take away.
    fireEvent.click(utils.getByTestId("doc-history-pane-diff"));
    const diffRow = await utils.findByTestId("diff-view-row");
    expect(diffRow.getAttribute("data-kind")).toBe("added");
    expect(diffRow.textContent).toContain("寫壞的內容");

    // Nothing was written by any of the above: no reset call, and the document
    // is still the owner's text rather than the default.
    expect(reset).not.toHaveBeenCalled();
    const live = await mockApi.getGlobalContext();
    expect(live.text).toBe("寫壞的內容");
    expect(live.isDefault).toBe(false);
    reset.mockRestore();
  });

  it("resets through the 初始版本 row, and only after the same confirmation", async () => {
    await mockApi.saveGlobalContext("寫壞的內容");
    const reset = vi.spyOn(mockApi, "resetGlobalContext");

    const utils = await openUserCustomDoc();
    await utils.findByText("寫壞的內容");
    // 重置 no longer exists as a control of its own — this is the whole shape
    // of the ruling, and without this line the row could be a second door.
    startEditing(utils);
    expect(utils.queryByText(s.reset)).toBeNull();

    fireEvent.click(utils.getByTestId("doc-history-entry-global_context"));
    fireEvent.click(await utils.findByTestId("doc-history-seed-open"));
    // Inside the reader the restore is labelled for what it does to THIS entry.
    const restore = await utils.findByTestId("doc-history-modal-restore");
    expect(restore.textContent).toBe(s.historySeedRestore);
    fireEvent.click(restore);

    // Destructive, so it is confirmed exactly like a restore — the reset used
    // to be a single click, and moving it must not have made it cheaper.
    expect(reset).not.toHaveBeenCalled();
    const confirm = utils.getByTestId("doc-history-restore-confirm");
    expect(within(confirm).getByText(s.historyRestoreConfirmAction)).toBeTruthy();
    expect(confirm.textContent).toContain(s.historySeedConfirm);

    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));
    await waitFor(() => expect(reset).toHaveBeenCalledTimes(1));
    // The whole history surface closes with it, and the editor is gone: the
    // document on screen is the seed again, not the draft that preceded it.
    await waitFor(() =>
      expect(utils.queryByTestId("doc-history-modal")).toBeNull()
    );
    expect(utils.queryByTestId("doc-history-list")).toBeNull();
    expect((await mockApi.getGlobalContext()).isDefault).toBe(true);
    reset.mockRestore();
  });

  // 初始版本 is now the ONLY way to reset, and it needs nothing from the
  // server. Rendering it only in the success branch of the list load made 重置
  // the hostage of an unrelated GET: one failed request and a seeded document
  // could not be put back on its default at all.
  it("keeps 初始版本 reachable when the version list fails to load", async () => {
    await mockApi.saveGlobalContext("寫壞的內容");
    const list = vi
      .spyOn(mockApi, "listDocumentHistory")
      .mockRejectedValue(new Error("boom"));
    const reset = vi.spyOn(mockApi, "resetGlobalContext");

    const utils = await openUserCustomDoc();
    await utils.findByText("寫壞的內容");
    await openHistory(utils, "global_context");

    // The failure is still reported — the seed row must not paper over it.
    await utils.findByText(s.historyError);

    fireEvent.click(await utils.findByTestId("doc-history-seed-open"));
    fireEvent.click(await utils.findByTestId("doc-history-modal-restore"));
    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));
    await waitFor(() => expect(reset).toHaveBeenCalledTimes(1));
    expect((await mockApi.getGlobalContext()).isDefault).toBe(true);
    list.mockRestore();
    reset.mockRestore();
  });

  // The SECOND hostage risk T-40f0 introduces: the seed now has a GET of its
  // own. If that request fails, the reader must say so — never claim the
  // default is empty — and the restore must STILL be reachable, because putting
  // the document back on its default needs nothing from this client.
  it("keeps 初始版本 restorable when its own content cannot be read", async () => {
    await mockApi.saveGlobalContext("寫壞的內容");
    const seed = vi
      .spyOn(mockApi, "getDocumentSeed")
      .mockRejectedValue(new Error("boom"));
    const reset = vi.spyOn(mockApi, "resetGlobalContext");

    const utils = await openUserCustomDoc();
    await openHistory(utils, "global_context");
    fireEvent.click(await utils.findByTestId("doc-history-seed-open"));

    // The honest sentence, NOT 「這個版本沒有內容」 — that is a different and
    // false claim about the document.
    const notice = await utils.findByTestId("doc-history-seed-unavailable");
    expect(notice.textContent).toBe(s.historySeedUnavailable);
    expect(utils.queryByText(s.historyModalEmpty)).toBeNull();

    fireEvent.click(utils.getByTestId("doc-history-modal-restore"));
    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));
    await waitFor(() => expect(reset).toHaveBeenCalledTimes(1));
    expect((await mockApi.getGlobalContext()).isDefault).toBe(true);
    seed.mockRestore();
    reset.mockRestore();
  });

  it("cancelling the 初始版本 confirmation resets nothing", async () => {
    await mockApi.saveGlobalContext("寫壞的內容");
    const reset = vi.spyOn(mockApi, "resetGlobalContext");

    const utils = await openUserCustomDoc();
    await openHistory(utils, "global_context");
    fireEvent.click(await utils.findByTestId("doc-history-seed-open"));
    fireEvent.click(await utils.findByTestId("doc-history-modal-restore"));
    fireEvent.click(
      within(utils.getByTestId("doc-history-restore-confirm")).getByText(s.cancel)
    );

    expect(reset).not.toHaveBeenCalled();
    expect(utils.queryByTestId("doc-history-restore-confirm")).toBeNull();
    // Back to the reader, not out of the history altogether — and the step back
    // into the list is still there.
    expect(utils.getByTestId("doc-history-modal")).toBeTruthy();
    expect(utils.getByTestId("doc-history-modal-back")).toBeTruthy();
    expect((await mockApi.getGlobalContext()).text).toBe("寫壞的內容");
    reset.mockRestore();
  });

  // The equivalence, across every surface at once: 初始版本 exists exactly
  // where a file seed does. Pinning only the positive side would let the row
  // appear on a custom role, where clicking it produces a 404 nobody expects;
  // pinning only the negative side would let it quietly vanish from the two
  // documents whose reset it now IS.
  it("carries 初始版本 exactly where the document has a file seed", async () => {
    const { roleKey: customKey } = await mockApi.createRole({
      name: "臨時角色",
    });
    const manual = await mockApi.createTaskManual("週報");
    // EVERY surface probed below must have retained revisions of its own. A
    // document with none renders the 「還沒有保留任何版本」 line instead of the
    // list, and a seed row wrongly rendered inside that list would then be
    // invisible to the negative half of this equivalence — which is exactly
    // how a mutant that shows the row everywhere survived once.
    await mockApi.saveGlobalContext("第零版");
    await mockApi.saveGlobalContext("第一版");
    await mockApi.saveRole("assistant", { definitionMd: "第零版定義" });
    await mockApi.saveRole("assistant", { definitionMd: "第一版定義" });
    await mockApi.saveRole(customKey, { definitionMd: "第零版定義" });
    await mockApi.saveRole(customKey, { definitionMd: "第一版定義" });
    await mockApi.saveLessons(customKey, "第零版經驗");
    await mockApi.saveLessons(customKey, "第一版經驗");
    await mockApi.saveInsight(customKey, "第零版判準");
    await mockApi.saveInsight(customKey, "第一版判準");
    await mockApi.updateTaskManual(manual.typeKey, {
      sopMd: "第零版 SOP",
      learnings: "第零版經驗",
    });
    await mockApi.updateTaskManual(manual.typeKey, {
      sopMd: "第一版 SOP",
      learnings: "第一版經驗",
    });

    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    const goSettingsRoot = () => {
      const [root] = utils.container.querySelectorAll(".crumbs__seg button");
      if (root) fireEvent.click(root);
    };
    /** Open one surface's history and report whether it holds the seed row. */
    const probe = async (surface: string, kind: string) => {
      const list = await utils.findByTestId("doc-history-list");
      // Guards the guard: without a rendered list of real revisions, the
      // absence of a seed row proves nothing at all.
      await waitFor(() =>
        expect(
          list.querySelectorAll(".doc-hist__item:not(.doc-hist__item--seed)")
            .length
        ).toBeGreaterThan(0)
      );
      const seedRow = within(list).queryByTestId("doc-history-seed");
      // The seed is the OLDEST thing this document has ever been and the list
      // runs newest-first, so it belongs LAST. Nothing on screen distinguishes
      // a seed row at the top from one at the bottom except this line, and a
      // reset offered above the owner's own revisions reads as the newest
      // state rather than the original one.
      const items = Array.from(list.querySelectorAll(".doc-hist__item"));
      if (seedRow) expect(items.at(-1)).toBe(seedRow);
      const seeded = seedRow !== null;
      fireEvent.click(utils.getByTestId("doc-history-list-close"));
      void kind;
      return { surface, seeded };
    };

    // 使用者自訂 — the global block ships with a seed the reset restores.
    goSettingsRoot();
    fireEvent.click(utils.getByText(s.globalContext));
    fireEvent.click(await utils.findByText(s.customName));
    await utils.findByText(s.edit);
    await openHistory(utils, "global_context");
    expect(await probe(s.customName, "global_context")).toEqual({
      surface: s.customName,
      seeded: true,
    });

    // A SEED role's definition has a file seed…
    goSettingsRoot();
    fireEvent.click(utils.getByText(s.roles));
    fireEvent.click(await utils.findByText(zh.office.role.assistant));
    await utils.findAllByText(s.edit);
    await openHistory(utils, "role_definition");
    expect(await probe("seed role", "role_definition")).toEqual({
      surface: "seed role",
      seeded: true,
    });

    // …a CUSTOM role's does not: its doc IS its only truth, and the server
    // 404s the reset, so a row offering one would be a dead affordance.
    goSettingsRoot();
    fireEvent.click(utils.getByText(s.roles));
    fireEvent.click(await utils.findByText("臨時角色"));
    await utils.findAllByText(s.edit);
    await openHistory(utils, "role_definition");
    expect(await probe(customKey, "role_definition")).toEqual({
      surface: customKey,
      seeded: false,
    });

    // Lessons, insight, and both of a task manual's documents have no seed
    // either.
    //
    // ⚠️ These two cards are picked BY CARD, not by position. This used to be
    // `getAllByText(s.edit).at(-1)` with a comment asserting the page's last
    // 編輯 was the lessons card's — true only while lessons was the last card
    // on the persona page. T-3809 put InsightCard after it and the selector
    // silently started opening the wrong document; the failure surfaced three
    // steps later as a missing lessons history entry, naming neither card.
    const cardEdit = (cls: string) =>
      within(
        utils.container.querySelector(cls) as HTMLElement
      ).getByText(s.edit);

    fireEvent.click(cardEdit(".mp-lessons:not(.mp-insight)"));
    fireEvent.click(utils.getByTestId("doc-history-entry-lessons"));
    expect(await probe("lessons", "lessons")).toEqual({
      surface: "lessons",
      seeded: false,
    });

    // Insight (T-3809): NO file seed, deliberately — that is what lets an
    // untouched doc read as genuinely empty and makes "has this role moved
    // anything over yet?" answerable. So it belongs on the negative side of
    // this equivalence, and a seed row appearing here would offer a reset the
    // server has nothing to reset to.
    fireEvent.click(cardEdit(".mp-insight"));
    fireEvent.click(utils.getByTestId("doc-history-entry-insight"));
    expect(await probe("insight", "insight")).toEqual({
      surface: "insight",
      seeded: false,
    });

    goSettingsRoot();
    fireEvent.click(utils.getByText(s.manuals));
    fireEvent.click(await utils.findByTestId(`manual-open-${manual.typeKey}`));
    fireEvent.click(await utils.findByTestId("manual-entry-definition"));
    // The SOP's version entry lives in block ③'s edit row (owner 2026-07-31 P1).
    fireEvent.click(await utils.findByTestId("manual-def-edit-3"));
    fireEvent.click(utils.getByTestId("doc-history-entry-task_manual_sop"));
    expect(await probe("manual SOP", "task_manual_sop")).toEqual({
      surface: "manual SOP",
      seeded: false,
    });

    goSettingsRoot();
    fireEvent.click(utils.getByText(s.manuals));
    fireEvent.click(await utils.findByTestId(`manual-open-${manual.typeKey}`));
    fireEvent.click(await utils.findByTestId("manual-entry-learnings"));
    fireEvent.click(await utils.findByTestId("manual-learnings-edit"));
    fireEvent.click(utils.getByTestId("doc-history-entry-task_manual_learnings"));
    expect(await probe("manual learnings", "task_manual_learnings")).toEqual({
      surface: "manual learnings",
      seeded: false,
    });
  });

  it("marks an over-cap revision un-restorable up front, with the reason", async () => {
    // The revision the server WOULD refuse with a 400: a lessons doc that is
    // over the cap and not shrinking. Before this, the owner only found out by
    // clicking — which reads as a broken system rather than a stated limit.
    const overCap = "字".repeat(DOC_CAP_CHARS_DEFAULT + 1);
    // The doc's first write retains nothing, so the over-cap text has to be the
    // SECOND one for it to become a retained revision at all.
    await mockApi.saveLessons("assistant", "原始經驗");
    await mockApi.saveLessons("assistant", overCap);
    await mockApi.saveLessons("assistant", "短");

    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.roles));
    fireEvent.click(await utils.findByText(zh.office.role.assistant));
    // The LESSONS card, addressed by its own class rather than by position:
    // the role page's card order is owner-ruled and has moved once already
    // (2026-08-03), and an index silently lands on the wrong card when it does.
    await utils.findAllByText(s.edit);
    fireEvent.click(
      within(
        utils.container.querySelector(".mp-lessons:not(.mp-insight)")!
      ).getByText(s.edit)
    );
    fireEvent.click(utils.getByTestId("doc-history-entry-lessons"));

    const [target] = await mockApi.listDocumentHistory(
      "lessons",
      "assistant"
    );
    expect(target.sizes.text).toBe(runeLength(overCap));

    const row = await utils.findByTestId(`doc-history-item-${target.id}`);
    // Listed, never hidden — this row is the only place that text still exists.
    expect(row).toBeTruthy();
    expect(within(row).getByText(s.historyBlockedBadge)).toBeTruthy();
    // The reason is IN the row, and names the field and the cap.
    const reason = utils.getByTestId(`doc-history-blocked-${target.id}`);
    expect(reason.textContent).toContain(s.historyField.text);
    expect(reason.textContent).toContain(String(DOC_CAP_CHARS_DEFAULT));

    // …and opening it does not become a way around that: the modal repeats the
    // verdict and its restore control is dead too, so the 400 is unreachable
    // from either surface.
    fireEvent.click(utils.getByTestId(`doc-history-open-${target.id}`));
    const modal = utils.getByTestId("doc-history-modal");
    expect(within(modal).getByText(s.historyBlockedBadge)).toBeTruthy();
    expect(
      utils.getByTestId("doc-history-modal-blocked").textContent
    ).toContain(s.historyField.text);
    expect(
      (utils.getByTestId("doc-history-modal-restore") as HTMLButtonElement)
        .disabled
    ).toBe(true);
  });

  it("follows the owner's raised cap instead of the shipped default", async () => {
    // T-3aeb: the cap is a SETTING. A revision that is over the DEFAULT but
    // under the owner's raised cap is one the server would ACCEPT, so marking
    // it un-restorable would be the cockpit lying — and it is the direction
    // that matters, because the cap can only ever be raised.
    const overDefault = "字".repeat(DOC_CAP_CHARS_DEFAULT + 100);
    await mockApi.patchServerSettings({ docCapCharsLearning: 50000 });
    await mockApi.saveLessons("assistant", "原始經驗");
    await mockApi.saveLessons("assistant", overDefault);
    await mockApi.saveLessons("assistant", "短");

    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.roles));
    fireEvent.click(await utils.findByText(zh.office.role.assistant));
    // The LESSONS card, addressed by class — see the note above.
    await utils.findAllByText(s.edit);
    fireEvent.click(
      within(
        utils.container.querySelector(".mp-lessons:not(.mp-insight)")!
      ).getByText(s.edit)
    );
    fireEvent.click(utils.getByTestId("doc-history-entry-lessons"));

    const [target] = await mockApi.listDocumentHistory(
      "lessons",
      "assistant"
    );
    expect(target.sizes.text).toBe(runeLength(overDefault));

    const row = await utils.findByTestId(`doc-history-item-${target.id}`);
    expect(within(row).queryByText(s.historyBlockedBadge)).toBeNull();
    expect(utils.queryByTestId(`doc-history-blocked-${target.id}`)).toBeNull();

    // …and the control the ruling actually left is LIVE: restore lives in the
    // modal now, so that is where "the server would take this" has to show.
    fireEvent.click(utils.getByTestId(`doc-history-open-${target.id}`));
    await waitFor(() =>
      expect(
        (utils.getByTestId("doc-history-modal-restore") as HTMLButtonElement)
          .disabled
      ).toBe(false)
    );
  });

  it("leaves an ordinary revision restorable — the mark is not blanket", async () => {
    await mockApi.saveLessons("assistant", "第零版經驗");
    await mockApi.saveLessons("assistant", "第一版經驗");
    await mockApi.saveLessons("assistant", "第二版經驗");

    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.roles));
    fireEvent.click(await utils.findByText(zh.office.role.assistant));
    // The LESSONS card, addressed by class — see the note above.
    await utils.findAllByText(s.edit);
    fireEvent.click(
      within(
        utils.container.querySelector(".mp-lessons:not(.mp-insight)")!
      ).getByText(s.edit)
    );
    fireEvent.click(utils.getByTestId("doc-history-entry-lessons"));

    const [target] = await mockApi.listDocumentHistory(
      "lessons",
      "assistant"
    );
    fireEvent.click(await utils.findByTestId(`doc-history-open-${target.id}`));
    const button = utils.getByTestId(
      "doc-history-modal-restore"
    ) as HTMLButtonElement;
    expect(button.disabled).toBe(false);
    expect(utils.queryByTestId(`doc-history-blocked-${target.id}`)).toBeNull();
    expect(utils.queryByTestId("doc-history-modal-blocked")).toBeNull();
  });

  // The footnote states what history does NOT cover, so it is only true on a
  // document that can actually be deleted whole. Pinning it as an EQUIVALENCE —
  // the note is present exactly where the delete control is — is what makes the
  // condition survive: showing it everywhere and dropping it from a deletable
  // document are both drifts nobody would see on screen, and neither would move
  // a test that only checked one surface.
  it("shows the delete-scope footnote exactly where a delete control exists", async () => {
    const created = await mockApi.createRole({ name: "臨時角色" });
    // T-91: the create receipt carries ids only, so the "this is a CUSTOM role"
    // property is read back rather than taken from the write.
    expect((await mockApi.getRole(created.roleKey)).isSeed).toBe(false);
    const manual = await mockApi.createTaskManual("週報");

    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );

    // Back up to 設定 from wherever the previous probe ended — the trail's root
    // crumb is the first clickable segment on every settings sub-page.
    const goSettingsRoot = () => {
      const [root] = utils.container.querySelectorAll(".crumbs__seg button");
      if (root) fireEvent.click(root);
    };
    const readNote = async (surface: string, deletable: boolean) => {
      await utils.findByTestId("doc-history-list");
      const noted = utils.queryByTestId("doc-history-scope-note") !== null;
      fireEvent.click(utils.getByTestId("doc-history-list-close"));
      return { surface, deletable, noted };
    };

    // The delete control lives on the LIST, the footnote inside the version
    // history, so each probe reads one on each and reports them together.
    // Asserting the EQUIVALENCE is what makes this survive: showing the note
    // everywhere and dropping it from a document that really is deletable are
    // both invisible on screen, and a test that only looked at one surface
    // would move for neither. The surface travels in the result so a failure
    // names WHICH page drifted.
    const probeRole = async (label: string, roleKey: string) => {
      goSettingsRoot();
      fireEvent.click(utils.getByText(s.roles));
      await utils.findByText(label);
      const deletable = utils.queryByTestId(`role-delete-${roleKey}`) !== null;
      fireEvent.click(utils.getByText(label));
      await utils.findAllByText(s.edit);
      await openHistory(utils, "role_definition");
      return readNote(`${s.roles} › ${label}`, deletable);
    };

    // A task manual carries the same footnote on BOTH of its document
    // sub-pages: deleting the type takes every revision of both with it.
    const probeManual = async (entry: "definition" | "learnings") => {
      goSettingsRoot();
      fireEvent.click(utils.getByText(s.manuals));
      await utils.findByTestId(`manual-open-${manual.typeKey}`);
      const deletable =
        utils.queryByTestId(`manual-delete-${manual.typeKey}`) !== null;
      fireEvent.click(utils.getByTestId(`manual-open-${manual.typeKey}`));
      fireEvent.click(await utils.findByTestId(`manual-entry-${entry}`));
      // 任務定義's SOP entry lives in block ③'s edit row (owner 2026-07-31 P1);
      // 學習經驗 still has one card-level switch.
      fireEvent.click(
        await utils.findByTestId(
          entry === "definition" ? "manual-def-edit-3" : "manual-learnings-edit"
        )
      );
      fireEvent.click(
        utils.getByTestId(
          `doc-history-entry-task_manual_${entry === "definition" ? "sop" : "learnings"}`
        )
      );
      // T-91: createTaskManual answers the minted type_key only, so the
      // display name is the one this test passed in.
      return readNote(`${s.manuals} › 週報 › ${entry}`, deletable);
    };

    // A seed role cannot be deleted, so the note — which says what history does
    // NOT cover — would be a false statement there.
    expect(await probeRole(zh.office.role.assistant, "assistant")).toEqual({
      surface: `${s.roles} › ${zh.office.role.assistant}`,
      deletable: false,
      noted: false,
    });
    // A custom role can be deleted whole, and that delete keeps no history.
    expect(await probeRole("臨時角色", created.roleKey)).toEqual({
      surface: `${s.roles} › 臨時角色`,
      deletable: true,
      noted: true,
    });
    // Every task manual can be deleted whole — so the note belongs on both of
    // its document pages, and the same equivalence binds them.
    expect(await probeManual("definition")).toEqual({
      surface: `${s.manuals} › 週報 › definition`,
      deletable: true,
      noted: true,
    });
    expect(await probeManual("learnings")).toEqual({
      surface: `${s.manuals} › 週報 › learnings`,
      deletable: true,
      noted: true,
    });
  });

  // T-1f39 — the manual's two document pages read two SEPARATE series, and the
  // 任務定義 page also edits 用途／識別鍵, which are no longer versioned at all.
  // A list there still headed plain 「版本紀錄」 would claim a history those
  // edits do not have, so the heading and the note both name the SOP.
  it("gives the manual's SOP and learnings their own history, and says so", async () => {
    const manual = await mockApi.createTaskManual("週報");
    await mockApi.updateTaskManual(manual.typeKey, {
      sopMd: "第一版 SOP",
      learnings: "第一版經驗",
    });
    await mockApi.updateTaskManual(manual.typeKey, {
      sopMd: "第二版 SOP",
      learnings: "第二版經驗",
    });
    // A purpose-only edit after them: not versioned, so neither list may grow.
    await mockApi.updateTaskManual(manual.typeKey, { purpose: "每週回顧" });

    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    const openManualPage = async (entry: "definition" | "learnings") => {
      const [root] = utils.container.querySelectorAll(".crumbs__seg button");
      if (root) fireEvent.click(root);
      fireEvent.click(utils.getByText(s.manuals));
      fireEvent.click(await utils.findByTestId(`manual-open-${manual.typeKey}`));
      fireEvent.click(await utils.findByTestId(`manual-entry-${entry}`));
      fireEvent.click(
        await utils.findByTestId(
          entry === "definition" ? "manual-def-edit-3" : "manual-learnings-edit"
        )
      );
      fireEvent.click(
        utils.getByTestId(
          `doc-history-entry-task_manual_${entry === "definition" ? "sop" : "learnings"}`
        )
      );
      return waitFor(() => {
        const rows = utils.container.querySelectorAll(".doc-hist__item");
        expect(rows).toHaveLength(1);
        return rows[0] as HTMLElement;
      });
    };

    const sopRow = await openManualPage("definition");
    expect(utils.getByText(s.historySopTitle)).toBeTruthy();
    expect(utils.getByText(s.historySopSub)).toBeTruthy();
    // The SOP series holds the SOP alone: no 學習經驗 field, and — the point of
    // the split — no 用途 either. Read inside the version, since the list no
    // longer previews content (owner 2026-07-31).
    fireEvent.click(sopRow.querySelector(".doc-hist__row") as HTMLElement);
    const sopModal = utils.getByTestId("doc-history-modal");
    // No field LABEL to look for: a single-field kind IS its one field, and
    // the modal deliberately drops the heading there. The text arrives from
    // the revision's own read (T-1170), so it is awaited.
    await waitFor(() =>
      expect(within(sopModal).getByText("第一版 SOP")).toBeTruthy()
    );
    expect(sopModal.textContent).not.toContain("第一版經驗");
    expect(sopModal.textContent).not.toContain("每週回顧");
    fireEvent.click(within(sopModal).getByTestId("doc-history-modal-close"));

    const learningsRow = await openManualPage("learnings");
    // Every list names its own document (owner 2026-07-31): a heading that
    // only said 「版本紀錄」 left the reader guessing which of the page's
    // documents it held.
    expect(utils.getByText(s.historyManualLearningsTitle)).toBeTruthy();
    fireEvent.click(learningsRow.querySelector(".doc-hist__row") as HTMLElement);
    const learnModal = utils.getByTestId("doc-history-modal");
    await waitFor(() =>
      expect(within(learnModal).getByText("第一版經驗")).toBeTruthy()
    );
    expect(learnModal.textContent).not.toContain("第一版 SOP");
  });

  it("shows the same entry on a role definition, keyed to that role", async () => {
    await mockApi.saveRole("assistant", { definitionMd: "角色定義初稿" });
    await mockApi.saveRole("assistant", { definitionMd: "角色定義改寫" });

    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.roles));
    fireEvent.click(await utils.findByText(zh.office.role.assistant));
    await utils.findAllByText(s.edit);
    await openHistory(utils, "role_definition");

    await waitFor(() =>
      expect(
        utils.container.querySelectorAll(
          ".doc-hist__item:not(.doc-hist__item--seed)"
        ).length
      ).toBeGreaterThan(0)
    );
    // The role page carries TWO versioned documents, so the definition's list
    // must say it is the definition's.
    expect(utils.getAllByText(s.historyRoleDefTitle).length).toBeGreaterThan(0);
    // A seed role has no delete affordance, so the delete-scope footnote —
    // which states what history does NOT cover — stays off this list.
    expect(utils.queryByTestId("doc-history-scope-note")).toBeNull();
  });
});

// ── 還原之後的兩個 await (T-91) ──────────────────────────────────────────────
// A restore that LANDED rewrites the live document server-side. Two things then
// have to happen on screen: re-read the document, and leave edit mode — the
// draft in the editor is now a pending overwrite of the version just restored.
// They are promises about two different things: the re-read may blip (T-91
// wrapped it so a failed re-read stops being reported as 還原失敗), but leaving
// edit mode is unconditional. Sequencing them made the blip skip the exit and
// then get swallowed: the modal closed silently and the editor stayed open on
// the PRE-restore draft, which the owner's next 完成編輯 writes straight over
// the content he just restored.
describe("SettingsPage · 版本紀錄 — 還原後的離開編輯 (T-91)", () => {
  it("leaves edit mode even when the re-read after a landed restore fails", async () => {
    await mockApi.saveGlobalContext("原本的內容");
    await mockApi.saveGlobalContext("後來改壞的內容");
    const restore = vi.spyOn(mockApi, "restoreDocumentHistory");

    const utils = await openUserCustomDoc();
    await utils.findByText("後來改壞的內容");

    // Edit mode, holding a draft that is about to be superseded by the restore.
    startEditing(utils);
    fireEvent.change(utils.getByTestId("doc-card-editor"), {
      target: { value: "編輯到一半的草稿" },
    });
    fireEvent.click(utils.getByTestId("doc-history-entry-global_context"));
    await utils.findByTestId("doc-history-list");

    const [target] = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    fireEvent.click(await utils.findByTestId(`doc-history-open-${target.id}`));
    fireEvent.click(utils.getByTestId("doc-history-modal-restore"));

    // The document re-read fails from here on. The restore POST itself is
    // untouched, so it keeps landing — that is the whole premise.
    const read = vi
      .spyOn(mockApi, "getGlobalContext")
      .mockRejectedValue(mockApiError("read failed", 503, ""));
    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    await waitFor(() => expect(restore).toHaveBeenCalledTimes(1));
    // 🔴 The assertion this case exists for: the editor is CLOSED. The restore
    // is already on the server, so the draft above must not survive to be
    // written back over it by the next 完成編輯.
    await waitFor(() =>
      expect(utils.queryByTestId("doc-card-editor")).toBeNull()
    );
    expect(utils.getByTestId("doc-card-edit")).toBeTruthy();
    expect(utils.queryByText("編輯到一半的草稿")).toBeNull();
    // …and exactly one restore, with no 還原失敗: the re-read blip is not the
    // restore's failure (the other half of T-91, pinned above).
    expect(restore).toHaveBeenCalledTimes(1);
    expect(utils.queryByText(s.historyRestoreError)).toBeNull();

    read.mockRestore();
    restore.mockRestore();
  });

  // The same property on the FOUR other hosts that wire `onRestored`. Each one
  // owns its own exit — 手冊 SOP is `cancelEdit(3)`, 手冊學習經驗 and the two
  // journal cards are their own `setEditing(false)` / `cancelEdit()` — so the
  // DocCard case above proves nothing about any of them: a sequenced re-read
  // reintroduced on any single host is invisible to every other test in the
  // tree, and its only symptom is the owner's next 完成編輯 silently writing
  // the pre-restore draft over the version he just restored.
  //
  // 🔴 Each case mocks the host's OWN re-read (getTaskManual / getInsight /
  // getLessons) and nothing else: `restoreDocumentHistory` keeps landing, which
  // is the premise. None of those rejections drops the card from the screen —
  // the hooks keep the last good document on a failed refetch — so "the editor
  // is gone" is a real observation about edit mode, not about an unmounted card.

  /** 設定 › 任務手冊 › 週報, on the sub-page named by `entry`. */
  async function openManualPage(
    typeKey: string,
    entry: "definition" | "learnings"
  ) {
    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.manuals));
    fireEvent.click(await utils.findByTestId(`manual-open-${typeKey}`));
    fireEvent.click(await utils.findByTestId(`manual-entry-${entry}`));
    return utils;
  }

  /** Open one card's 版本紀錄, read its newest revision, and press 還原 —
   * stopping at the confirmation so the caller can break the re-read first. */
  async function armRestore(
    utils: Utils,
    kind: DocumentKind,
    docKey: string
  ) {
    fireEvent.click(utils.getByTestId(`doc-history-entry-${kind}`));
    await utils.findByTestId("doc-history-list");
    const [target] = await mockApi.listDocumentHistory(kind, docKey);
    fireEvent.click(await utils.findByTestId(`doc-history-open-${target.id}`));
    fireEvent.click(utils.getByTestId("doc-history-modal-restore"));
  }

  it("leaves the manual's SOP block even when the re-read after a landed restore fails", async () => {
    const manual = await mockApi.createTaskManual("週報");
    await mockApi.updateTaskManual(manual.typeKey, { sopMd: "第零版 SOP" });
    await mockApi.updateTaskManual(manual.typeKey, { sopMd: "第一版 SOP" });
    const restore = vi.spyOn(mockApi, "restoreDocumentHistory");

    const utils = await openManualPage(manual.typeKey, "definition");
    // Block ③ open, holding a draft the restore is about to supersede.
    fireEvent.click(await utils.findByTestId("manual-def-edit-3"));
    fireEvent.change(utils.getByTestId("manual-sop-input"), {
      target: { value: "編輯到一半的 SOP 草稿" },
    });
    await armRestore(utils, "task_manual_sop", manual.typeKey);

    const read = vi
      .spyOn(mockApi, "getTaskManual")
      .mockRejectedValue(mockApiError("read failed", 503, ""));
    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    await waitFor(() => expect(restore).toHaveBeenCalledTimes(1));
    // 🔴 Block ③ is CLOSED — its draft cannot survive to be written back over
    // the revision now on the server.
    await waitFor(() =>
      expect(utils.queryByTestId("manual-sop-input")).toBeNull()
    );
    expect(utils.getByTestId("manual-def-edit-3")).toBeTruthy();
    expect(utils.queryByText("編輯到一半的 SOP 草稿")).toBeNull();
    expect(restore).toHaveBeenCalledTimes(1);
    expect(utils.queryByText(s.historyRestoreError)).toBeNull();

    read.mockRestore();
    restore.mockRestore();
  });

  it("leaves the manual's 學習經驗 editor even when the re-read after a landed restore fails", async () => {
    const manual = await mockApi.createTaskManual("週報");
    await mockApi.updateTaskManual(manual.typeKey, { learnings: "第零版經驗" });
    await mockApi.updateTaskManual(manual.typeKey, { learnings: "第一版經驗" });
    const restore = vi.spyOn(mockApi, "restoreDocumentHistory");

    const utils = await openManualPage(manual.typeKey, "learnings");
    fireEvent.click(await utils.findByTestId("manual-learnings-edit"));
    fireEvent.change(utils.getByTestId("manual-learnings-input"), {
      target: { value: "編輯到一半的經驗草稿" },
    });
    await armRestore(utils, "task_manual_learnings", manual.typeKey);

    const read = vi
      .spyOn(mockApi, "getTaskManual")
      .mockRejectedValue(mockApiError("read failed", 503, ""));
    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    await waitFor(() => expect(restore).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(utils.queryByTestId("manual-learnings-input")).toBeNull()
    );
    expect(utils.getByTestId("manual-learnings-edit")).toBeTruthy();
    expect(utils.queryByText("編輯到一半的經驗草稿")).toBeNull();
    expect(restore).toHaveBeenCalledTimes(1);
    expect(utils.queryByText(s.historyRestoreError)).toBeNull();

    read.mockRestore();
    restore.mockRestore();
  });

  /** 設定 › 角色誌 › 助理 — the page that carries both journal cards. */
  async function openAssistantRolePage() {
    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.roles));
    fireEvent.click(await utils.findByText(zh.office.role.assistant));
    await utils.findAllByText(s.edit);
    return utils;
  }

  /** The journal cards are picked BY CARD, never by the position of a 編輯
   * button: Duty, Learning and Insight all carry one, and `.mp-insight` also
   * matches `.mp-lessons`. */
  const journalCard = (utils: Utils, cls: string) =>
    utils.container.querySelector(cls) as HTMLElement;

  it("leaves the Insight editor even when the re-read after a landed restore fails", async () => {
    await mockApi.saveInsight("assistant", "第零版判準");
    await mockApi.saveInsight("assistant", "第一版判準");
    const restore = vi.spyOn(mockApi, "restoreDocumentHistory");

    const utils = await openAssistantRolePage();
    const card = journalCard(utils, ".mp-insight");
    fireEvent.click(within(card).getByText(s.edit));
    fireEvent.change(within(card).getByPlaceholderText(s.editorPlaceholder), {
      target: { value: "編輯到一半的判準草稿" },
    });
    await armRestore(utils, "insight", "assistant");

    const read = vi
      .spyOn(mockApi, "getInsight")
      .mockRejectedValue(mockApiError("read failed", 503, ""));
    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    await waitFor(() => expect(restore).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(
        within(journalCard(utils, ".mp-insight")).queryByPlaceholderText(
          s.editorPlaceholder
        )
      ).toBeNull()
    );
    expect(
      within(journalCard(utils, ".mp-insight")).getByText(s.edit)
    ).toBeTruthy();
    expect(utils.queryByText("編輯到一半的判準草稿")).toBeNull();
    expect(restore).toHaveBeenCalledTimes(1);
    expect(utils.queryByText(s.historyRestoreError)).toBeNull();

    read.mockRestore();
    restore.mockRestore();
  });

  it("leaves the Lessons editor even when the re-read after a landed restore fails", async () => {
    await mockApi.saveLessons("assistant", "第零版經驗");
    await mockApi.saveLessons("assistant", "第一版經驗");
    const restore = vi.spyOn(mockApi, "restoreDocumentHistory");

    const utils = await openAssistantRolePage();
    const lessons = ".mp-lessons:not(.mp-insight)";
    const card = journalCard(utils, lessons);
    fireEvent.click(within(card).getByText(s.edit));
    fireEvent.change(within(card).getByPlaceholderText(s.editorPlaceholder), {
      target: { value: "編輯到一半的經驗草稿" },
    });
    await armRestore(utils, "lessons", "assistant");

    const read = vi
      .spyOn(mockApi, "getLessons")
      .mockRejectedValue(mockApiError("read failed", 503, ""));
    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    await waitFor(() => expect(restore).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(
        within(journalCard(utils, lessons)).queryByPlaceholderText(
          s.editorPlaceholder
        )
      ).toBeNull()
    );
    expect(within(journalCard(utils, lessons)).getByText(s.edit)).toBeTruthy();
    expect(utils.queryByText("編輯到一半的經驗草稿")).toBeNull();
    expect(restore).toHaveBeenCalledTimes(1);
    expect(utils.queryByText(s.historyRestoreError)).toBeNull();

    read.mockRestore();
    restore.mockRestore();
  });
});
