// T-c9b4 — the cockpit half: the wake snapshot's chat budget is an adjustable
// setting, and its row is NOT one of the document caps.
//
// Two things here fail silently without a test:
//
//  1. THE ROW EXISTS AND READS THE LIVE VALUE. The budget was a server-side
//     constant, so "the settings page can move it" is the whole ticket; a page
//     that renders the shipped default no matter what the server says looks
//     identical until someone changes it and nothing happens.
//  2. ITS RANGE IS ITS OWN, AND THE FLOOR IS NOT THE DEFAULT. Every doc-cap row
//     refuses anything below its shipped default, because lowering a document
//     cap strands existing documents. Copying that rule here would make the one
//     thing the owner asked for — dialling the budget DOWN — impossible, while
//     the row still looked and saved fine.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";
import {
  CHAT_BUDGET_CHARS_DEFAULT,
  CHAT_BUDGET_CHARS_MAX,
  CHAT_BUDGET_CHARS_MIN,
} from "../api/chatBudget";
import { DOC_CAP_CHARS_DEFAULTS } from "../api/docCap";

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
  await utils.findByLabelText(s.chatBudget);
  return utils;
}

describe("T-c9b4 — the wake chat budget is an adjustable setting", () => {
  it("the row shows the LIVE value, not the shipped default", async () => {
    // Set it to something nobody ships first: a row hardcoded to the default
    // would be indistinguishable from a correct one on a fresh install.
    await mockApi.patchServerSettings({ chatBudgetChars: 4000 });
    const utils = await openParamsPage();
    expect(
      (utils.getByLabelText(s.chatBudget) as HTMLInputElement).value
    ).toBe("4000");
  });

  it("editing the row moves ONLY the chat budget", async () => {
    // The failure this catches is a row wired to a neighbouring field: it looks
    // right, saves without error, and moves someone else's number.
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.chatBudget);
    fireEvent.change(input, { target: { value: "9000" } });
    fireEvent.blur(input);

    const after = await mockApi.getServerSettings();
    expect(after.chatBudgetChars).toBe(9000);
    expect(after.docCapCharsDuty).toBe(DOC_CAP_CHARS_DEFAULTS.duty);
    expect(after.docCapCharsLearning).toBe(DOC_CAP_CHARS_DEFAULTS.learning);
  });

  it("accepts a value BELOW the shipped default — the knob turns down", async () => {
    // 🔴 This is the assertion the ticket exists for. Under the doc-cap floor
    // rule (floor == shipped default) this write would be refused locally and
    // the row would silently snap back.
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.chatBudget);
    expect(CHAT_BUDGET_CHARS_MIN).toBeLessThan(CHAT_BUDGET_CHARS_DEFAULT);

    fireEvent.change(input, { target: { value: String(CHAT_BUDGET_CHARS_MIN) } });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).chatBudgetChars).toBe(
      CHAT_BUDGET_CHARS_MIN
    );
  });

  it("refuses values outside its own range and writes nothing", async () => {
    // The control for the case above, so "it accepts 1000" is not just "this
    // row never validates anything".
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.chatBudget);

    fireEvent.change(input, {
      target: { value: String(CHAT_BUDGET_CHARS_MIN - 1) },
    });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).chatBudgetChars).toBe(
      CHAT_BUDGET_CHARS_DEFAULT
    );

    fireEvent.change(input, {
      target: { value: String(CHAT_BUDGET_CHARS_MAX + 1) },
    });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).chatBudgetChars).toBe(
      CHAT_BUDGET_CHARS_DEFAULT
    );

    // And the ceiling itself is reachable — otherwise "refuses 13001" would be
    // satisfied by a row that refuses everything up there.
    fireEvent.change(input, { target: { value: String(CHAT_BUDGET_CHARS_MAX) } });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).chatBudgetChars).toBe(
      CHAT_BUDGET_CHARS_MAX
    );
  });
});
