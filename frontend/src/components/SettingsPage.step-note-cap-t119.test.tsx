// T-119 — the cockpit half: the step note's size cap is an adjustable setting,
// and its row is NOT one of the document caps.
//
// Two things here fail silently without a test:
//
//  1. THE ROW EXISTS AND READS THE LIVE VALUE. The cap was a server-side
//     constant, so "the settings page can move it" is the whole ticket; a page
//     that renders the shipped default no matter what the server says looks
//     identical until someone changes it and nothing happens.
//  2. ITS FLOOR IS NOT ITS DEFAULT. Every doc-cap row refuses anything below its
//     shipped default because lowering a document cap strands existing
//     documents. A step note is measured only on WRITE, so lowering this one
//     strands nothing — copying the doc-cap rule here would make the thing the
//     owner asked for impossible while the row still looked and saved fine.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";
import {
  STEP_NOTE_CAP_CHARS_DEFAULT,
  STEP_NOTE_CAP_CHARS_MAX,
  STEP_NOTE_CAP_CHARS_MIN,
} from "../api/stepNoteCap";
import { CHAT_BUDGET_CHARS_DEFAULT } from "../api/chatBudget";
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
  await utils.findByLabelText(s.stepNoteCap);
  return utils;
}

describe("T-119 — the step note size cap is an adjustable setting", () => {
  // The three numbers in this file's mirror are the ones the field refuses an
  // out-of-range value against before the owner ever reaches the server's 422.
  // Every other assertion here compares them against each other or against a
  // value derived from them, so all three would drift together in silence; this
  // is the one that names what the server actually ships.
  it("mirrors the server's shipped default and range", () => {
    expect(STEP_NOTE_CAP_CHARS_DEFAULT).toBe(10000);
    expect(STEP_NOTE_CAP_CHARS_MIN).toBe(1000);
    expect(STEP_NOTE_CAP_CHARS_MAX).toBe(100000);
  });

  // The owner reviewed the shipped row and asked for this position by name:
  // directly under the last task-manual cap, so the size caps read as one
  // group. That is an acceptance condition, not styling — styling someone says
  // again, an acceptance condition nobody says twice once it has been signed
  // off. Nothing else here would notice the row drifting back down the page.
  it("sits directly under the task-manual SOP cap", async () => {
    const utils = await openParamsPage();
    const rowOf = (label: string) =>
      utils.getByLabelText(label).closest(".param-row");
    const manualSop = rowOf(s.docCapManualSop);
    expect(manualSop).not.toBeNull();
    expect(manualSop!.nextElementSibling).toBe(rowOf(s.stepNoteCap));
  });

  it("the row shows the LIVE value, not the shipped default", async () => {
    await mockApi.patchServerSettings({ stepNoteCapChars: 4000 });
    const utils = await openParamsPage();
    expect(
      (utils.getByLabelText(s.stepNoteCap) as HTMLInputElement).value
    ).toBe("4000");
  });

  it("editing the row moves ONLY the step note cap", async () => {
    // The failure this catches is a row wired to a neighbouring field — most
    // plausibly the chat budget it was copied from: it looks right, saves
    // without error, and moves someone else's number.
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.stepNoteCap);
    fireEvent.change(input, { target: { value: "42000" } });
    fireEvent.blur(input);

    const after = await mockApi.getServerSettings();
    expect(after.stepNoteCapChars).toBe(42000);
    expect(after.chatBudgetChars).toBe(CHAT_BUDGET_CHARS_DEFAULT);
    expect(after.docCapCharsDuty).toBe(DOC_CAP_CHARS_DEFAULTS.duty);
  });

  it("accepts a value BELOW the shipped default — the knob turns down", async () => {
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.stepNoteCap);
    expect(STEP_NOTE_CAP_CHARS_MIN).toBeLessThan(STEP_NOTE_CAP_CHARS_DEFAULT);

    fireEvent.change(input, {
      target: { value: String(STEP_NOTE_CAP_CHARS_MIN) },
    });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).stepNoteCapChars).toBe(
      STEP_NOTE_CAP_CHARS_MIN
    );
  });

  it("refuses values outside its own range and writes nothing", async () => {
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.stepNoteCap);

    fireEvent.change(input, {
      target: { value: String(STEP_NOTE_CAP_CHARS_MIN - 1) },
    });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).stepNoteCapChars).toBe(
      STEP_NOTE_CAP_CHARS_DEFAULT
    );

    fireEvent.change(input, {
      target: { value: String(STEP_NOTE_CAP_CHARS_MAX + 1) },
    });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).stepNoteCapChars).toBe(
      STEP_NOTE_CAP_CHARS_DEFAULT
    );

    // And the ceiling itself is reachable — otherwise "refuses 100001" would be
    // satisfied by a row that refuses everything up there.
    fireEvent.change(input, {
      target: { value: String(STEP_NOTE_CAP_CHARS_MAX) },
    });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).stepNoteCapChars).toBe(
      STEP_NOTE_CAP_CHARS_MAX
    );
  });
});
