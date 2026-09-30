// T-8 — the cockpit half: N (how many backups are kept) is an adjustable
// setting, and the copy beside it tells the truth about what N is.
//
// Three things here fail silently without a test:
//
//  1. THE ROW EXISTS AND READS THE LIVE VALUE. N was a Go constant, so "the
//     settings page can move it" is half the ticket; a row that renders the
//     shipped 5 no matter what the server says looks identical on a fresh
//     install and only diverges once somebody changes it.
//  2. THE SAVE CARRIES backup_retain AND NOTHING ELSE. This is the one row on
//     the page whose value DELETES files, so a row wired to a neighbouring
//     field would quietly destroy backups while looking correct.
//  3. THE COPY SAYS THE TWO THINGS THE INTEGER CANNOT. "N is versions, not
//     days" and "N is per pool, not per directory" are the two readings that
//     have already been got wrong, and they are only ever wrong in the
//     direction of someone believing they bought more coverage than they did.
//     A label that omits them is not a smaller label, it is a misleading one —
//     so the assertions below are on the RENDERED text, not on a constant.

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";
import {
  BACKUP_RETAIN_DEFAULT,
  BACKUP_RETAIN_MAX,
  BACKUP_RETAIN_MIN,
} from "../api/backupRetain";

const s = zh.settings;

/** The language preference lives in localStorage and I18nProvider reads it when
 *  it mounts (see i18n/index.tsx), so setting it before render is how a test
 *  gets the OTHER locale onto the screen. */
const LS_LANGUAGE = "oc.language";

beforeEach(() => {
  __resetMock();
});
afterEach(() => {
  window.localStorage.removeItem(LS_LANGUAGE);
});

async function openParamsPage() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByTestId("settings-params-entry"));
  await utils.findByLabelText(s.backupRetain);
  return utils;
}

/** The RENDERED sub-label under the retention row, in one locale.
 *
 * 🔴 WHY THIS EXISTS. The English half of these assertions used to read
 * `en.settings.backupRetainSub` — the CONSTANT. That checks the dictionary
 * against itself and never touches the screen: point the component at a
 * different key (as a mutant did) and every English assertion stays green while
 * the English cockpit shows the wrong paragraph entirely. The Chinese half was
 * always on `textContent`; this puts English on the same footing, by mounting
 * the provider in `en` and reading the node it actually painted. */
async function retainSubTextIn(locale: "zh" | "en"): Promise<string> {
  window.localStorage.setItem(LS_LANGUAGE, locale);
  const dict = locale === "zh" ? zh : en;
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByTestId("settings-params-entry"));
  await utils.findByLabelText(dict.settings.backupRetain);
  const text = utils.getByText(dict.settings.backupRetainSub).textContent ?? "";
  utils.unmount();
  return text;
}

describe("T-8 — backup retention N is an adjustable setting", () => {
  it("the row shows the LIVE value, not the shipped default", async () => {
    // Set it to something nobody ships first: a row hardcoded to the default is
    // indistinguishable from a correct one on a fresh install.
    await mockApi.patchServerSettings({ backupRetain: 9 });
    expect(BACKUP_RETAIN_DEFAULT).not.toBe(9);
    const utils = await openParamsPage();
    expect(
      (utils.getByLabelText(s.backupRetain) as HTMLInputElement).value
    ).toBe("9");
  });

  it("editing the row moves ONLY the retention count", async () => {
    const utils = await openParamsPage();
    const before = await mockApi.getServerSettings();

    const input = utils.getByLabelText(s.backupRetain);
    fireEvent.change(input, { target: { value: "3" } });
    fireEvent.blur(input);

    const after = await mockApi.getServerSettings();
    expect(after.backupRetain).toBe(3);
    // Everything else on the settings object is untouched. Named as a whole
    // object comparison so a row wired to the wrong field says WHICH field
    // moved instead of merely "3 !== 5".
    expect({ ...after, backupRetain: before.backupRetain }).toEqual(before);
  });

  it("refuses a value outside 1..20 without saving anything", async () => {
    const utils = await openParamsPage();
    const before = await mockApi.getServerSettings();

    for (const bad of [
      String(BACKUP_RETAIN_MIN - 1),
      String(BACKUP_RETAIN_MAX + 1),
    ]) {
      const input = utils.getByLabelText(s.backupRetain);
      fireEvent.change(input, { target: { value: bad } });
      fireEvent.blur(input);
      const after = await mockApi.getServerSettings();
      expect(after.backupRetain).toBe(before.backupRetain);
    }
    // 0 is refused deliberately: it would mean "keep nothing", i.e. delete the
    // snapshot that was just taken.
    expect(BACKUP_RETAIN_MIN).toBe(1);
  });

  it("the copy beside the field says N is VERSIONS, not days", async () => {
    // The failure: a reader sets 5 and believes they bought a fixed number of
    // days of history. They did not — the same 5 covered under three days on
    // this machine's busiest day and over a week on a quiet one.
    expect(await retainSubTextIn("zh")).toContain("算的是份數不是天數");
    expect(await retainSubTextIn("en")).toContain("counts backups, not days");
  });

  it("the copy beside the field says each backup type keeps N, so the disk holds up to twice as many", async () => {
    // The failure: a reader sets 5 and believes the directory holds 5 files. It
    // holds up to 10 — routine and pre-migration backups keep separate quotas —
    // so both the disk cost and the depth are double what they assumed.
    const zhSub = await retainSubTextIn("zh");
    expect(zhSub).toContain("日常備份與升級前備份各自保留這個份數");
    expect(zhSub).toContain("磁碟上最多會有兩倍的檔案");
    const enSub = await retainSubTextIn("en");
    expect(enSub).toContain("routine backups and pre-upgrade backups each keep this many");
    expect(enSub).toContain("up to twice as many files on disk");
  });

  it("the copy says the excess is DELETED, not moved aside", async () => {
    // The whole point of the ticket. Someone lowering this number is deleting
    // backups, and the page must say so before they do it, not after.
    const zhSub = await retainSubTextIn("zh");
    expect(zhSub).toContain("永久刪除");
    expect(await retainSubTextIn("en")).toContain("permanently deleted");
  });
});
