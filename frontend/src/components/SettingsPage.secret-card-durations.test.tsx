// The two `secret` reply-card durations on the settings page: readable units, the
// saved value in seconds, and a refusal that states the range.

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
    </I18nProvider>,
  );
  fireEvent.click(utils.getByTestId("settings-params-entry"));
  await utils.findByLabelText(s.secretCardUnretrievedTtl);
  return utils;
}

const unitOf = (utils: Awaited<ReturnType<typeof openParamsPage>>, id: string) =>
  utils.container.querySelector(`#${id}-unit`) as HTMLSelectElement;

describe("secret card durations", () => {
  it("shows the shipped defaults in the largest whole unit", async () => {
    const utils = await openParamsPage();
    expect((utils.getByLabelText(s.secretCardUnretrievedTtl) as HTMLInputElement).value).toBe("1");
    expect(unitOf(utils, "param-secret-card-ttl").value).toBe("day");
    expect((utils.getByLabelText(s.secretCardRetention) as HTMLInputElement).value).toBe("10");
    expect(unitOf(utils, "param-secret-card-retention").value).toBe("minute");
  });

  it("saves in seconds, whichever unit the owner typed in", async () => {
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.secretCardUnretrievedTtl);
    fireEvent.change(input, { target: { value: "2" } });
    fireEvent.change(unitOf(utils, "param-secret-card-ttl"), { target: { value: "hour" } });
    fireEvent.blur(unitOf(utils, "param-secret-card-ttl"));
    await waitFor(async () =>
      expect((await mockApi.getServerSettings()).secretCardUnretrievedTtlSecs).toBe(7200),
    );
  });

  it("retention takes zero", async () => {
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.secretCardRetention);
    fireEvent.change(input, { target: { value: "0" } });
    fireEvent.blur(input);
    await waitFor(async () =>
      expect((await mockApi.getServerSettings()).secretCardRetrievedRetentionSecs).toBe(0),
    );
    expect((utils.getByLabelText(s.secretCardRetention) as HTMLInputElement).value).toBe("0");
  });

  it.each([
    ["ttl below a minute", s.secretCardUnretrievedTtl, "param-secret-card-ttl", "30", "second", s.secretCardUnretrievedTtlRange],
    ["ttl above thirty days", s.secretCardUnretrievedTtl, "param-secret-card-ttl", "31", "day", s.secretCardUnretrievedTtlRange],
    ["retention above a day", s.secretCardRetention, "param-secret-card-retention", "2", "day", s.secretCardRetentionRange],
  ])("refuses %s and states the range", async (_name, label, id, amount, unit, message) => {
    const before = await mockApi.getServerSettings();
    const utils = await openParamsPage();
    fireEvent.change(utils.getByLabelText(label), { target: { value: amount } });
    fireEvent.change(unitOf(utils, id), { target: { value: unit } });
    fireEvent.blur(unitOf(utils, id));
    expect(utils.getByTestId("secret-card-range-error").textContent).toBe(message);
    expect(message).toMatch(/\d.*(分鐘|秒).*\d?.*(天)/);
    const after = await mockApi.getServerSettings();
    expect(after.secretCardUnretrievedTtlSecs).toBe(before.secretCardUnretrievedTtlSecs);
    expect(after.secretCardRetrievedRetentionSecs).toBe(before.secretCardRetrievedRetentionSecs);
  });

  it("the mock refuses out-of-range writes in the server's own words", async () => {
    await expect(mockApi.patchServerSettings({ secretCardUnretrievedTtlSecs: 59 })).rejects.toMatchObject({
      message: expect.stringContaining("http 422"),
    });
    await expect(mockApi.patchServerSettings({ secretCardRetrievedRetentionSecs: -1 })).rejects.toMatchObject({
      message: expect.stringContaining("http 422"),
    });
  });
});
