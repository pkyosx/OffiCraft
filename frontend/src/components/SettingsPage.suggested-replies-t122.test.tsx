// T-122 — the 參數設定 face of the two 建議回覆 lists.
//
// 🔴 THE ASSERTION THIS FILE EXISTS FOR IS THAT THERE ARE **TWO** LISTS AND
// THEY DO NOT TOUCH. The first cut of this feature shipped ONE shared list and
// both boxes read it; the owner ruled otherwise (chat c-85c28e708b81「任務跟請
// 示卡要是不同的參數設定」). A single-list regression looks perfect in any test
// that seeds both lists to the same sentences, so every case below seeds them
// DIFFERENTLY and names which one it expects.
//
// The other two things that fail silently:
//  * THE EMPTY LIST IS A REAL SAVED VALUE. Deleting the last row must send [],
//    not "leave it alone" — [] is what turns the chips off, and a row editor
//    that treats empty as "nothing to save" can never turn them off again.
//  * A SAVE CARRIES ONE FIELD. A row wired to its neighbour would rewrite the
//    other box's sentences while looking entirely correct on screen.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";
import { api } from "../api";
import { SUGGESTED_REPLY_MAX_LEN } from "../api/suggestedReplies";

const s = zh.settings;

beforeEach(() => {
  __resetMock();
});

async function openParamsPage() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByTestId("settings-params-entry"));
  await utils.findByText(s.suggestedRepliesReplyCard);
  return utils;
}

const cardRow = (n: number) => `${s.suggestedRepliesReplyCard} ${n}`;
const taskRow = (n: number) => `${s.suggestedRepliesTaskMessage} ${n}`;

describe("T-122 — 建議回覆 是兩份各自獨立的參數設定", () => {
  it("draws the two lists as two rows, each showing its OWN sentences", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["請示卡用的"],
      suggestedRepliesTaskMessage: ["任務用的一", "任務用的二"],
    });
    const utils = await openParamsPage();

    expect((utils.getByLabelText(cardRow(1)) as HTMLInputElement).value).toBe(
      "請示卡用的"
    );
    expect((utils.getByLabelText(taskRow(1)) as HTMLInputElement).value).toBe(
      "任務用的一"
    );
    expect((utils.getByLabelText(taskRow(2)) as HTMLInputElement).value).toBe(
      "任務用的二"
    );
    // The reply-card list has ONE row, so a second one would mean the editor is
    // rendering the other list's entries too.
    expect(utils.queryByLabelText(cardRow(2))).toBeNull();
  });

  it("editing the reply-card list moves ONLY that field", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["請示卡用的"],
      suggestedRepliesTaskMessage: ["任務用的"],
    });
    const utils = await openParamsPage();
    const before = await mockApi.getServerSettings();

    const input = utils.getByLabelText(cardRow(1));
    fireEvent.change(input, { target: { value: "改過的請示卡句子" } });
    fireEvent.blur(input);

    await waitFor(async () => {
      const after = await mockApi.getServerSettings();
      expect(after.suggestedRepliesReplyCard).toEqual(["改過的請示卡句子"]);
    });
    const after = await mockApi.getServerSettings();
    // Whole-object comparison, so a row wired to its neighbour names WHICH
    // field moved rather than merely failing on the list under test.
    expect({
      ...after,
      suggestedRepliesReplyCard: before.suggestedRepliesReplyCard,
    }).toEqual(before);
  });

  it("editing the task-message list leaves the reply-card one alone", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["請示卡用的"],
      suggestedRepliesTaskMessage: ["任務用的"],
    });
    const utils = await openParamsPage();

    const input = utils.getByLabelText(taskRow(1));
    fireEvent.change(input, { target: { value: "改過的任務句子" } });
    fireEvent.blur(input);

    await waitFor(async () => {
      const after = await mockApi.getServerSettings();
      expect(after.suggestedRepliesTaskMessage).toEqual(["改過的任務句子"]);
    });
    expect(
      (await mockApi.getServerSettings()).suggestedRepliesReplyCard
    ).toEqual(["請示卡用的"]);
  });

  it("adds a sentence to the list the button belongs to", async () => {
    const utils = await openParamsPage();

    // Two 新增一句 buttons — one per list. The FIRST is the reply-card row's.
    fireEvent.click(utils.getAllByText(s.suggestedReplyAdd)[0]);
    const added = await utils.findByLabelText(cardRow(1));
    fireEvent.change(added, { target: { value: "新寫的一句" } });
    fireEvent.blur(added);

    await waitFor(async () => {
      const after = await mockApi.getServerSettings();
      expect(after.suggestedRepliesReplyCard).toEqual(["新寫的一句"]);
    });
    expect(
      (await mockApi.getServerSettings()).suggestedRepliesTaskMessage
    ).toEqual([]);
  });

  it("SAVES the empty list when the last sentence is deleted", async () => {
    // The whole point of [] being a legal value: this is how the owner turns
    // the chips off again. An editor that skipped the save here would leave the
    // last sentence on the wire forever.
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["請示卡用的"],
      suggestedRepliesTaskMessage: ["任務用的"],
    });
    const utils = await openParamsPage();

    fireEvent.click(utils.getByLabelText(`${s.suggestedRepliesReplyCard} ${s.suggestedReplyRemove} 1`));

    await waitFor(async () => {
      const after = await mockApi.getServerSettings();
      expect(after.suggestedRepliesReplyCard).toEqual([]);
    });
    // ...and the other list is untouched, which is the failure a shared list
    // would produce here: both boxes would go silent at once.
    expect(
      (await mockApi.getServerSettings()).suggestedRepliesTaskMessage
    ).toEqual(["任務用的"]);
    expect(utils.getAllByText(s.suggestedRepliesEmpty).length).toBe(1);
  });

  it("keeps a PASTED over-long sentence WHOLE, says why, and sends nothing", async () => {
    // 🔴 owner rc-76ab62ceb3ff:「超過就直接拒絕存檔並告訴你為什麼,不會偷偷截
    // 斷」. The regression this pins is `<input maxLength={120}>`: the browser
    // enforces it by CUTTING A PASTE with no notice, and pasting is exactly how
    // a saved reply reaches this field — a 150-character sentence would be
    // stored 30 characters shorter than what the owner put in, and he would
    // have no way to tell. So the assertion is on the input's OWN value, not
    // just on the absence of a save.
    const patch = vi.spyOn(api, "patchServerSettings");
    const utils = await openParamsPage();
    fireEvent.click(utils.getAllByText(s.suggestedReplyAdd)[0]);
    const input = (await utils.findByLabelText(cardRow(1))) as HTMLInputElement;

    // 🔴 THE ATTRIBUTE ASSERTION IS NOT DECORATION — IT IS THE ONLY HALF THAT
    // SEES THIS MUTANT, AND THAT WAS MEASURED. jsdom does not enforce
    // `maxLength` on a programmatic value assignment, which is what
    // `fireEvent.change` performs, so putting `maxLength={120}` back leaves
    // every behavioural assertion below GREEN. The truncation is a real
    // BROWSER behaviour this environment cannot reproduce, so the thing to pin
    // is that the field does not hand its bound to the browser at all.
    expect(input.hasAttribute("maxlength")).toBe(false);

    const pasted = "水".repeat(150);
    expect(pasted.length).toBeGreaterThan(SUGGESTED_REPLY_MAX_LEN);
    fireEvent.change(input, { target: { value: pasted } });
    fireEvent.blur(input);

    expect(input.value).toBe(pasted);
    expect([...input.value].length).toBe(150);
    expect(
      utils.getByText(s.suggestedRepliesTooLong(150, SUGGESTED_REPLY_MAX_LEN))
    ).toBeTruthy();
    expect(patch).not.toHaveBeenCalled();
    expect(
      (await mockApi.getServerSettings()).suggestedRepliesReplyCard
    ).toEqual([]);
    patch.mockRestore();
  });

  it("says WHY the add button is dead once the list holds 20 sentences", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: Array.from(
        { length: 20 },
        (_, i) => `第 ${i + 1} 句`
      ),
    });
    const utils = await openParamsPage();
    // A disabled button that explains nothing reads as "broken", which is the
    // small version of the same principle as the case above.
    expect(utils.getAllByText(s.suggestedReplyAdd)[0]).toHaveProperty(
      "disabled",
      true
    );
    expect(utils.getAllByText(s.suggestedRepliesFull).length).toBeGreaterThan(0);
  });

  it("reorders within one list and saves the new order", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["第一句", "第二句"],
    });
    const utils = await openParamsPage();

    fireEvent.click(utils.getByLabelText(`${s.suggestedRepliesReplyCard} ${s.suggestedReplyMoveDown} 1`));

    await waitFor(async () => {
      const after = await mockApi.getServerSettings();
      expect(after.suggestedRepliesReplyCard).toEqual(["第二句", "第一句"]);
    });
  });

  // owner 2026-09-07 (c-bb83edad24bf ＋ rc-8bfa9288ad2c「我是指參數設定那邊」):
  // typing Chinese here and pressing Enter ONCE to pick the candidate saved the
  // half-typed line. Enter-to-save read the keystroke that CONFIRMS a CJK
  // candidate as "this sentence is finished". Every other Enter-to-submit field
  // on the site already guards this — the role-create field in this same file,
  // and the three composers via composerKeys — so this list was the one that
  // drifted. A miss here is silent: the value saved LOOKS like something the
  // owner typed.
  it("does not save on the Enter that CONFIRMS a CJK candidate", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["原本的"],
      suggestedRepliesTaskMessage: ["任務用的"],
    });
    const utils = await openParamsPage();

    const input = utils.getByLabelText(cardRow(1));
    fireEvent.change(input, { target: { value: "打到一半" } });

    // The three shapes a browser reports mid-composition. Each must be inert.
    fireEvent.keyDown(input, { key: "Enter", isComposing: true });
    fireEvent.keyDown(input, { key: "Enter", keyCode: 229 });
    fireEvent.compositionStart(input);
    fireEvent.keyDown(input, { key: "Enter" });

    await Promise.resolve();
    expect((await mockApi.getServerSettings()).suggestedRepliesReplyCard).toEqual([
      "原本的",
    ]);
  });

  it("saves on the Enter that follows a finished composition", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["原本的"],
      suggestedRepliesTaskMessage: ["任務用的"],
    });
    const utils = await openParamsPage();

    const input = utils.getByLabelText(cardRow(1));
    fireEvent.compositionStart(input);
    fireEvent.change(input, { target: { value: "打完了" } });
    fireEvent.compositionEnd(input, { target: { value: "打完了" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(async () => {
      expect(
        (await mockApi.getServerSettings()).suggestedRepliesReplyCard
      ).toEqual(["打完了"]);
    });
  });
});
