import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";
import { api } from "../api";

const chatRow = (n: number) => `聊天室建議回覆 ${n}`;

async function openParamsPage() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>,
  );
  fireEvent.click(utils.getByTestId("settings-params-entry"));
  await utils.findByText("請示卡建議回覆");
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
    fireEvent.click(await findByLabelText("聊天室建議回覆 刪除這一句 1"));
    await waitFor(async () => expect((await api.getServerSettings()).suggestedRepliesChat).toEqual([]));
  });

  it("an over-long sentence is refused with the reason and nothing is saved", async () => {
    await mockApi.patchServerSettings({ suggestedRepliesChat: ["舊"] });
    const { findByLabelText, findByRole } = await openParamsPage();
    const input = (await findByLabelText(chatRow(1))) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "句".repeat(121) } });
    fireEvent.blur(input);
    expect((await findByRole("alert")).textContent).toBe(
      "這一句 121 字,超過上限 120 字。改短一點才會存檔——超過的部分不會被自動截掉,也不會存進去。",
    );
    expect((await api.getServerSettings()).suggestedRepliesChat).toEqual(["舊"]);
  });
});
