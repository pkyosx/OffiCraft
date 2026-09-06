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

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";

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
});
