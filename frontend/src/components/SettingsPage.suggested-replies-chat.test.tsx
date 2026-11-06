import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";
import { api } from "../api";

const s = zh.settings;
const chatRow = (n: number) => `${s.suggestedRepliesChat} ${n}`;

async function openParamsPage() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>,
  );
  fireEvent.click(utils.getByTestId("settings-params-entry"));
  await utils.findByText(s.suggestedRepliesReplyCard);
  return utils;
}

describe("聊天室建議回覆 in 參數調整", () => {
  beforeEach(() => __resetMock());

  it("draws its own row showing its own sentences, beside the other lists", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["請示卡用的"],
      suggestedRepliesChat: ["聊天用的"],
    });
    const { getByText, findByLabelText } = await openParamsPage();
    expect(getByText("聊天室建議回覆")).toBeTruthy();
    expect(((await findByLabelText(chatRow(1))) as HTMLInputElement).value).toBe("聊天用的");
  });

  it("an edit saves only its own field", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["請示卡用的"],
      suggestedRepliesChat: ["舊"],
    });
    const { findByLabelText } = await openParamsPage();
    const input = (await findByLabelText(chatRow(1))) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "新" } });
    fireEvent.blur(input);
    await waitFor(async () => expect((await api.getServerSettings()).suggestedRepliesChat).toEqual(["新"]));
    expect((await api.getServerSettings()).suggestedRepliesReplyCard).toEqual(["請示卡用的"]);
  });

  it("deleting the last sentence saves an empty list", async () => {
    await mockApi.patchServerSettings({ suggestedRepliesChat: ["舊"] });
    const { findByLabelText } = await openParamsPage();
    fireEvent.click(await findByLabelText(`${s.suggestedRepliesChat} ${s.suggestedReplyRemove} 1`));
    await waitFor(async () => expect((await api.getServerSettings()).suggestedRepliesChat).toEqual([]));
  });

  it("an over-long sentence is refused with the reason and nothing is saved", async () => {
    await mockApi.patchServerSettings({ suggestedRepliesChat: ["舊"] });
    const { findByLabelText, findByRole } = await openParamsPage();
    const input = (await findByLabelText(chatRow(1))) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "句".repeat(121) } });
    fireEvent.blur(input);
    expect((await findByRole("alert")).textContent).toContain("121");
    expect((await api.getServerSettings()).suggestedRepliesChat).toEqual(["舊"]);
  });
});
