// components/DocCard.test.tsx — the shared editable-document shell (T-c33e).
//
// Two things are pinned here, and they pull in opposite directions on purpose.
//
// 1. THE EXISTING CALLERS DID NOT MOVE. 角色定義 and 使用者自訂 were the only
//    users of this card while it was SettingsPage's un-exported `DocDetail`.
//    Lifting it out is worth nothing if the pages that already used it changed,
//    so these cases render them THROUGH SettingsPage — the real call sites, not
//    a hand-built prop bag — and assert that every affordance the boot-context
//    pages brought with them is ABSENT: no standing notes, no top-level factory
//    restore, no save confirmation, no whole-replace note, and 完成編輯 still
//    goes through on a draft nobody changed.
//
// 2. THE OVER-CAP DOOR IS NEW, AND IT IS THE ONE SANCTIONED DIFFERENCE. On the
//    untouched tree the role definition's cap was unenforceable from the
//    cockpit and unreportable afterwards: `DocDetail.commit()` was `try/finally`
//    with no `catch`, so a 4,000-character definition against a 1,000-character
//    cap left 完成編輯 enabled, left the usage readout frozen at the STORED size
//    (measured: "310 / 1000" while the draft held 4,000 characters), sent the
//    write, and turned the server's refusal into an unhandled promise rejection
//    and nothing on screen. Both halves are asserted below — the cockpit-side
//    refusal, and the server's own words when a save fails anyway.
//
// 🔴 使用者自訂 PASSES NO `usage` AND IS THEREFORE UNTOUCHED BY ALL OF IT. That
// is the control: global_context genuinely has no cap (docCap.ts's
// CAPPED_FIELDS.global_context is empty), so a card that grew a cap door for
// everyone would be inventing a limit the server does not enforce.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { api } from "../api";
import { __resetMock, mockApi } from "../api/mock";
import { ApiError } from "../api/errors";
import { DOC_CAP_CHARS_DEFAULTS } from "../api/docCap";

const s = zh.settings;

/** A refusal shaped like the server's doc-cap guard: the instructions are the
 * payload, and they are what must survive the trip to the screen. */
const REASON =
  "角色定義超過上限 4000 字（上限 1000 字）。已存的內容不會被截斷；請先刪掉過時的段落再寫入。";

beforeEach(() => {
  __resetMock();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function openSettings() {
  return render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
}

/** Walk 設定 → 角色誌 → 助理 (the role definition), the way an owner does. */
async function openRoleDoc() {
  const utils = openSettings();
  fireEvent.click(utils.getByText(s.roles));
  fireEvent.click(await utils.findByText(zh.office.role.assistant));
  await utils.findAllByText(s.edit);
  return utils;
}

/** Walk 設定 → 角色誌 → 使用者自訂 (the uncapped global-context block). */
async function openCustomDoc() {
  const utils = openSettings();
  fireEvent.click(utils.getByText(s.roles));
  fireEvent.click(await utils.findByText(s.customName));
  await utils.findAllByText(s.edit);
  return utils;
}

describe("DocCard", () => {
  it("keeps the two original documents free of every boot-context affordance", async () => {
    for (const open of [openRoleDoc, openCustomDoc]) {
      const utils = await open();
      // No note about the save. `replaceNote` is opt-in, and these two pages
      // never asked for it — it must not arrive under them just because three
      // other documents now do. (The notes block and the top-level 還原出廠版
      // are not asserted here any more: neither exists on ANY page since
      // 2026-08-14, so an absence assertion for them is satisfied by every
      // possible implementation. What is left of those two removals is guarded
      // where it can still fail — in BootDocPage.test.tsx.)
      expect(utils.queryByTestId("doc-card-replace-note")).toBeNull();

      // 完成編輯 saves DIRECTLY — no confirmation step was inserted under them.
      fireEvent.click(utils.getAllByTestId("doc-card-edit")[0]);
      fireEvent.change(utils.getByTestId("doc-card-editor"), {
        target: { value: "改一句話" },
      });
      fireEvent.click(utils.getByTestId("doc-card-save"));
      expect(utils.queryByTestId("doc-card-save-confirm")).toBeNull();
      await waitFor(() =>
        expect(utils.queryByTestId("doc-card-editor")).toBeNull()
      );
      utils.unmount();
    }
  });

  it("still lets 完成編輯 through on a draft nobody changed", async () => {
    // `requireDirty` is opt-in. The boot-context pages turn it on because a
    // no-op write flips them out of 預設 for ever; these two have always let it
    // through, and quietly disabling their button would be a behaviour change
    // dressed up as a safety feature.
    const save = vi.spyOn(api, "saveRole");
    const utils = await openRoleDoc();
    fireEvent.click(utils.getAllByTestId("doc-card-edit")[0]);
    expect(
      (utils.getByTestId("doc-card-save") as HTMLButtonElement).disabled
    ).toBe(false);
    fireEvent.click(utils.getByTestId("doc-card-save"));
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
  });

  it("refuses an over-cap role definition in the cockpit, with both numbers", async () => {
    // 🔴 THE FIX. Measured before it: the button stayed enabled, the readout
    // stayed at the stored size, the write went out and the refusal reached
    // nothing.
    const save = vi.spyOn(api, "saveRole");
    const cap = DOC_CAP_CHARS_DEFAULTS.duty;
    const utils = await openRoleDoc();

    fireEvent.click(utils.getAllByTestId("doc-card-edit")[0]);
    fireEvent.change(utils.getByTestId("doc-card-editor"), {
      target: { value: "字".repeat(cap + 7) },
    });

    const notice = utils.getByTestId("doc-card-over-cap");
    expect(notice.textContent).toContain(String(cap));
    expect(notice.textContent).toContain(String(cap + 7));
    // The readout follows the DRAFT — a number frozen at the stored size cannot
    // say anything about the text that is about to be sent.
    expect(utils.getByTestId("doc-card-usage").textContent).toBe(
      `${cap + 7} / ${cap}`
    );
    const saveBtn = utils.getByTestId("doc-card-save") as HTMLButtonElement;
    expect(saveBtn.disabled).toBe(true);
    fireEvent.click(saveBtn);
    expect(save).not.toHaveBeenCalled();

    // CONTROL, so the pass above is not "this button is never enabled": back
    // under the cap the door opens again, and the refusal line goes away.
    fireEvent.change(utils.getByTestId("doc-card-editor"), {
      target: { value: "字".repeat(cap - 1) },
    });
    expect(utils.queryByTestId("doc-card-over-cap")).toBeNull();
    expect(
      (utils.getByTestId("doc-card-save") as HTMLButtonElement).disabled
    ).toBe(false);
  });

  it("lets an already-over-cap document be edited DOWNWARD", async () => {
    // Mirrors the server's own rule (docCapBlocked): over the cap is refused
    // unless the write is getting shorter. Freezing an over-cap document would
    // leave the owner no way back under the line — and over-cap documents exist
    // (raise the cap, write, lower the cap again).
    const cap = DOC_CAP_CHARS_DEFAULTS.duty;
    await mockApi.patchServerSettings({ docCapCharsDuty: cap + 5000 });
    await mockApi.saveRole("assistant", { definitionMd: "字".repeat(cap + 3000) });
    await mockApi.patchServerSettings({ docCapCharsDuty: cap });

    const utils = await openRoleDoc();
    fireEvent.click(utils.getAllByTestId("doc-card-edit")[0]);
    // Still over the cap, but shorter than what is stored ⇒ allowed.
    fireEvent.change(utils.getByTestId("doc-card-editor"), {
      target: { value: "字".repeat(cap + 100) },
    });
    expect(utils.queryByTestId("doc-card-over-cap")).toBeNull();
    expect(
      (utils.getByTestId("doc-card-save") as HTMLButtonElement).disabled
    ).toBe(false);

    // …and growing it further is still refused.
    fireEvent.change(utils.getByTestId("doc-card-editor"), {
      target: { value: "字".repeat(cap + 4000) },
    });
    expect(utils.getByTestId("doc-card-over-cap")).toBeTruthy();
  });

  it("shows the server's own words when a save fails anyway", async () => {
    // The cockpit-side door cannot be the only answer: the cap is a live
    // setting, so a refusal can arrive for a draft this page judged fine. What
    // must never happen again is the previous behaviour — nothing on screen at
    // all.
    vi.spyOn(api, "saveRole").mockRejectedValue(
      new ApiError(
        "http 400 for PATCH /api/roles/assistant",
        400,
        "doc_too_large",
        REASON
      )
    );
    const utils = await openRoleDoc();
    fireEvent.click(utils.getAllByTestId("doc-card-edit")[0]);
    fireEvent.change(utils.getByTestId("doc-card-editor"), {
      target: { value: "一段新的職責" },
    });
    fireEvent.click(utils.getByTestId("doc-card-save"));

    const line = await utils.findByTestId("doc-card-save-error");
    expect(line.textContent).toBe(REASON);
    // The draft is still there to fix: a failed save must not throw the text
    // away.
    expect(
      (utils.getByTestId("doc-card-editor") as HTMLTextAreaElement).value
    ).toBe("一段新的職責");
  });

  it("falls back to its own words when the failure has none to quote", async () => {
    // `""` is a real state — a network throw, a proxy error page with no
    // envelope. An empty red line would be worse than the silence it replaced.
    vi.spyOn(api, "saveRole").mockRejectedValue(new Error("offline"));
    const utils = await openRoleDoc();
    fireEvent.click(utils.getAllByTestId("doc-card-edit")[0]);
    fireEvent.change(utils.getByTestId("doc-card-editor"), {
      target: { value: "一段新的職責" },
    });
    fireEvent.click(utils.getByTestId("doc-card-save"));

    const line = await utils.findByTestId("doc-card-save-error");
    expect(line.textContent).toBe(s.docActionFailed);
  });

  it("invents no budget for the document that genuinely has no cap", async () => {
    // 使用者自訂 passes no `usage`, so none of the above reaches it: no readout,
    // no refusal, and an arbitrarily long draft still saves.
    const utils = await openCustomDoc();
    expect(utils.queryByTestId("doc-card-usage")).toBeNull();
    fireEvent.click(utils.getAllByTestId("doc-card-edit")[0]);
    fireEvent.change(utils.getByTestId("doc-card-editor"), {
      target: { value: "字".repeat(DOC_CAP_CHARS_DEFAULTS.duty * 50) },
    });
    expect(utils.queryByTestId("doc-card-over-cap")).toBeNull();
    expect(
      (utils.getByTestId("doc-card-save") as HTMLButtonElement).disabled
    ).toBe(false);
  });
});
