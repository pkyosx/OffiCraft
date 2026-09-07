// SettingsPage.lore-caps-t33.test.tsx — T-33: the four 傳承 caps on the
// parameters page.
//
// 🔴 WHAT IS ACTUALLY BEING LOCKED HERE IS THAT THEY GO DOWN. Four number
// fields that only ever go up would look identical on screen, pass a "the row
// renders" test, and be wrong — the whole reason these four are not rows of
// DOC_CAP_FIELDS is that every knob in that table has floor == its own shipped
// default, because lowering a document cap strands existing legal documents in
// shrink-only mode. A 傳承 entry has no edit path at all, so a smaller cap
// strands nothing; the owner lowered two of them himself the day they shipped
// (title 140 → 80, body 1000 → 500). A test that only typed a LARGER number
// would pass against the wrong implementation.
//
// Deliberately NOT locked: where these four sit in the list. T-119's row has a
// position assertion because the owner reviewed the page and asked for that
// position by name; nobody has said anything about these four, and inventing an
// acceptance condition is not the same as recording one.

import { describe, it, expect, beforeEach } from "vitest";
import { fireEvent, render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";
import { zh } from "../i18n/locales/zh";
import {
  LORE_CAP_CHARS_DEFAULTS,
  LORE_FOLD_CAP_CHARS_MIN,
  LORE_FOLD_CAP_CHARS_MAX,
  LORE_ENTRY_CAP_CHARS_MIN,
  LORE_ENTRY_CAP_CHARS_MAX,
} from "../api/loreCap";

const s = zh.settings;

/** The four rows, paired with the setting each one must read AND write. A row
 * that reads one setting and writes another is the failure this pairing exists
 * to catch, and it is invisible on screen. */
const ROWS = [
  { label: s.loreCapRole, field: "loreCapCharsRole", shipped: LORE_CAP_CHARS_DEFAULTS.role, lowered: 400 },
  { label: s.loreCapManual, field: "loreCapCharsManual", shipped: LORE_CAP_CHARS_DEFAULTS.manual, lowered: 300 },
  { label: s.loreCapTitle, field: "loreCapCharsTitle", shipped: LORE_CAP_CHARS_DEFAULTS.title, lowered: 40 },
  { label: s.loreCapBody, field: "loreCapCharsBody", shipped: LORE_CAP_CHARS_DEFAULTS.body, lowered: 120 },
] as const;

async function openParamsPage() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByTestId("settings-params-entry"));
  await utils.findByLabelText(s.loreCapRole);
  return utils;
}

describe("T-33 — the four 傳承 caps are adjustable in BOTH directions", () => {
  beforeEach(() => {
    __resetMock();
  });

  it("shows the LIVE value for each of the four, not the shipped default", async () => {
    await mockApi.patchServerSettings({ loreCapCharsBody: 250 });
    const utils = await openParamsPage();
    expect(
      (utils.getByLabelText(s.loreCapBody) as HTMLInputElement).value,
    ).toBe("250");
  });

  for (const row of ROWS) {
    it(`「${row.label}」accepts a value BELOW its shipped default`, async () => {
      const utils = await openParamsPage();
      const input = utils.getByLabelText(row.label) as HTMLInputElement;
      expect(input.value).toBe(String(row.shipped));
      expect(row.lowered).toBeLessThan(row.shipped);

      fireEvent.change(input, { target: { value: String(row.lowered) } });
      fireEvent.blur(input);

      await waitFor(async () => {
        const live = await mockApi.getServerSettings();
        expect(live[row.field]).toBe(row.lowered);
      });
      // And the shared range/save error banner is NOT raised — a lowered value
      // is legal here, so a page that stored it while also complaining would
      // still be wrong, and the banner is the only thing that would say so.
      expect(utils.queryByText(s.paramsSaveError)).toBeNull();
    });
  }

  it("the two FOLD budgets and the two ENTRY bounds carry DIFFERENT ranges", async () => {
    // Two ranges, not one: a document-sized budget and a sentence-sized bound
    // cannot share a range without either letting a title grow into a document
    // or stopping a fold from holding more than a paragraph.
    const utils = await openParamsPage();
    const rangeOf = (label: string) => {
      const el = utils.getByLabelText(label) as HTMLInputElement;
      return [Number(el.min), Number(el.max)];
    };
    expect(rangeOf(s.loreCapRole)).toEqual([
      LORE_FOLD_CAP_CHARS_MIN,
      LORE_FOLD_CAP_CHARS_MAX,
    ]);
    expect(rangeOf(s.loreCapManual)).toEqual([
      LORE_FOLD_CAP_CHARS_MIN,
      LORE_FOLD_CAP_CHARS_MAX,
    ]);
    expect(rangeOf(s.loreCapTitle)).toEqual([
      LORE_ENTRY_CAP_CHARS_MIN,
      LORE_ENTRY_CAP_CHARS_MAX,
    ]);
    expect(rangeOf(s.loreCapBody)).toEqual([
      LORE_ENTRY_CAP_CHARS_MIN,
      LORE_ENTRY_CAP_CHARS_MAX,
    ]);
    // The two ranges must not have collapsed into one — that is the state this
    // assertion exists to refuse, and it would be invisible on screen.
    expect(LORE_FOLD_CAP_CHARS_MIN).not.toBe(LORE_ENTRY_CAP_CHARS_MIN);
  });
});
