// T-33 — the THIRD 建議回覆 list: the one under the 傳承 entry's message box.
//
// It is a third ROW on 參數調整, saving a third field, and everything that made
// the first two two-and-not-one applies again: asking the writer of a 傳承 entry
// whether what they wrote still holds is not answering a 請示卡 and not steering
// a task in progress, so one list's sentences are wrong in another's box.
//
// 🔴 EVERY CASE SEEDS THE THREE LISTS DIFFERENTLY. A regression that wired the
// new row to an existing field — or that made all three read one shared list —
// looks flawless on any screen where the sentences match, which is why the
// T-122 file next door was written the same way.
//
// The bounds are the SAME bounds, and that is a claim worth a test rather than
// a comment: a third row that quietly accepted 21 sentences or a 121-rune one
// would be refused by the server AFTER the owner had typed them.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";
import { api } from "../api";
import {
  SUGGESTED_REPLIES_MAX_ENTRIES,
  SUGGESTED_REPLY_MAX_LEN,
} from "../api/suggestedReplies";

const s = zh.settings;

beforeEach(() => {
  __resetMock();
});

async function openParamsPage() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>,
  );
  fireEvent.click(utils.getByTestId("settings-params-entry"));
  // 🔴 WAIT ON A ROW THAT IS NOT THE ONE UNDER TEST. Waiting on the 傳承 label
  // here would move every red in this file into the HELPER — a spec whose
  // failure message is "the page never loaded" says nothing about which row is
  // missing. The reply-card row predates this ticket and is what says the
  // 參數調整 page has rendered; each case below asserts the new row itself.
  await utils.findByText(s.suggestedRepliesReplyCard);
  return utils;
}

const loreRow = (n: number) => `${s.suggestedRepliesLoreMessage} ${n}`;
const cardRow = (n: number) => `${s.suggestedRepliesReplyCard} ${n}`;
const taskRow = (n: number) => `${s.suggestedRepliesTaskMessage} ${n}`;

describe("T-33 — 傳承訊息建議回覆 是自己的一份參數設定", () => {
  it("draws a THIRD row showing its own sentences, beside the other two", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["請示卡用的"],
      suggestedRepliesTaskMessage: ["任務用的"],
      suggestedRepliesLoreMessage: ["傳承用的一", "傳承用的二"],
    });
    const utils = await openParamsPage();

    // The row is ON the page, under its own label — asserted here rather than
    // inherited from the helper's wait.
    expect(utils.queryByText(s.suggestedRepliesLoreMessage)).not.toBeNull();
    expect((utils.getByLabelText(loreRow(1)) as HTMLInputElement).value).toBe(
      "傳承用的一",
    );
    expect((utils.getByLabelText(loreRow(2)) as HTMLInputElement).value).toBe(
      "傳承用的二",
    );
    // The neighbours still hold their own single sentence — a row rendering
    // someone else's list would show up here as an extra row over there.
    expect((utils.getByLabelText(cardRow(1)) as HTMLInputElement).value).toBe(
      "請示卡用的",
    );
    expect((utils.getByLabelText(taskRow(1)) as HTMLInputElement).value).toBe(
      "任務用的",
    );
    expect(utils.queryByLabelText(cardRow(2))).toBeNull();
    expect(utils.queryByLabelText(taskRow(2))).toBeNull();
  });

  it("editing the 傳承 list moves ONLY that field", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["請示卡用的"],
      suggestedRepliesTaskMessage: ["任務用的"],
      suggestedRepliesLoreMessage: ["傳承用的"],
    });
    const utils = await openParamsPage();

    const input = utils.getByLabelText(loreRow(1)) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "改過的傳承句子" } });
    fireEvent.blur(input);

    await waitFor(async () => {
      const after = await mockApi.getServerSettings();
      expect(after.suggestedRepliesLoreMessage).toEqual(["改過的傳承句子"]);
    });
    const after = await mockApi.getServerSettings();
    expect(after.suggestedRepliesReplyCard).toEqual(["請示卡用的"]);
    expect(after.suggestedRepliesTaskMessage).toEqual(["任務用的"]);
  });

  it("saves the EMPTY list when the last sentence is deleted — that is what turns the chips off", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesTaskMessage: ["任務用的"],
      suggestedRepliesLoreMessage: ["傳承用的"],
    });
    const utils = await openParamsPage();

    fireEvent.click(
      utils.getByLabelText(
        `${s.suggestedRepliesLoreMessage} ${s.suggestedReplyRemove} 1`,
      ),
    );
    await waitFor(async () => {
      const after = await mockApi.getServerSettings();
      expect(after.suggestedRepliesLoreMessage).toEqual([]);
    });
    // 「留空是合法的」 for this list too, and it did not take the neighbour's
    // sentences with it.
    expect(
      (await mockApi.getServerSettings()).suggestedRepliesTaskMessage,
    ).toEqual(["任務用的"]);
  });

  it("refuses a sentence over the 120-rune bound instead of truncating it", async () => {
    // Same bound, same refusal, same reason as T-122: a shortened sentence is
    // one the owner never wrote, sitting one tap from being sent. The
    // `maxlength` assertion is the half that sees the real regression — jsdom
    // does not enforce the attribute on a programmatic change, so the
    // behavioural assertions below stay green if it is put back.
    const patch = vi.spyOn(api, "patchServerSettings");
    const utils = await openParamsPage();
    const addButtons = utils.getAllByText(s.suggestedReplyAdd);
    // The 傳承 editor is the THIRD one on the page, in the order the rows are
    // mounted; picking it by index here is what keeps this spec about the new
    // row rather than about whichever editor happens to come first.
    fireEvent.click(addButtons[2]);
    const input = (await utils.findByLabelText(loreRow(1))) as HTMLInputElement;
    expect(input.hasAttribute("maxlength")).toBe(false);

    const pasted = "水".repeat(150);
    expect(pasted.length).toBeGreaterThan(SUGGESTED_REPLY_MAX_LEN);
    fireEvent.change(input, { target: { value: pasted } });
    fireEvent.blur(input);

    expect(input.value).toBe(pasted);
    expect(
      utils.getByText(s.suggestedRepliesTooLong(150, SUGGESTED_REPLY_MAX_LEN)),
    ).toBeTruthy();
    expect(patch).not.toHaveBeenCalled();
    expect(
      (await mockApi.getServerSettings()).suggestedRepliesLoreMessage,
    ).toEqual([]);
    patch.mockRestore();
  });

  it("stops at 20 sentences and says why", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesLoreMessage: Array.from(
        { length: SUGGESTED_REPLIES_MAX_ENTRIES },
        (_, i) => `第 ${i + 1} 句`,
      ),
    });
    const utils = await openParamsPage();
    // The full list is the 傳承 one, so it is the THIRD editor whose add button
    // is dead — the other two are empty and theirs must still be alive. Both
    // directions, because "everything is disabled" would pass a one-sided check.
    const addButtons = utils.getAllByText(s.suggestedReplyAdd);
    expect(addButtons[2]).toHaveProperty("disabled", true);
    expect(addButtons[0]).toHaveProperty("disabled", false);
    expect(addButtons[1]).toHaveProperty("disabled", false);
    expect(utils.getAllByText(s.suggestedRepliesFull).length).toBeGreaterThan(0);
    expect(
      utils.getByLabelText(loreRow(SUGGESTED_REPLIES_MAX_ENTRIES)),
    ).toBeTruthy();
  });
});
