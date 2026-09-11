// T-ae38: the cockpit half — the usage readouts that did not exist, and the
// independent knobs that replaced one.
//
// Each block below pins something that can fail SILENTLY:
//
//  1. THE INSIGHT CARD SHOWS ITS USAGE against its OWN cap, so an agent finds
//     out it is nearly full before being refused rather than by being refused.
//  2. THE ROLE-DEFINITION EDITOR SHOWS ITS USAGE. Duty carried NEITHER field on
//     the wire before this ticket, so an agent that had just condensed its own
//     role definition had to ask someone else to measure the doc.
//  3. EACH SETTINGS ROW WRITES ITS OWN KEY, and every row goes DOWN as well as
//     up — one shared floor of DOC_CAP_CHARS_MIN since owner 2026-09-07 (card
//     rc-5b66ba099e28 option [1]). A row that reads one setting and PATCHes
//     another is invisible until someone notices the wrong number moved; a row
//     that silently kept its old raise-only floor would look identical on
//     screen to one that turns both ways.
//
// 🔴 THE CAPS ARE SET TO DIFFERENT NUMBERS throughout. Before T-ae38 there was
// ONE setting, so every "the cap shown is N" assertion was equally true of
// every document — with equal numbers this whole file would pass against the
// old single-cap cockpit.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";
import {
  DOC_CAP_CHARS_DEFAULTS,
  DOC_CAP_CHARS_MIN,
  capForKind,
} from "../api/docCap";

const s = zh.settings;
const mp = zh.mp;

// Deliberately far apart, and none of them equal to another (see the header).
// These sit ABOVE their shipped defaults, which since owner 2026-09-07 is
// a free choice rather than a constraint — the one floor every row shares is
// DOC_CAP_CHARS_MIN, so a smaller number would be legal too. They stay written
// relative to DOC_CAP_CHARS_DEFAULTS so that these readout fixtures keep saying
// "not the default" whichever way a default later moves.
const DUTY_CAP = DOC_CAP_CHARS_DEFAULTS.duty + 500;
const INSIGHT_CAP = DOC_CAP_CHARS_DEFAULTS.insight + 2000;

beforeEach(() => {
  __resetMock();
});

async function openRolePage(roleLabel: string) {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.roles));
  fireEvent.click(await utils.findByText(roleLabel));
  await utils.findAllByText(s.edit);
  return utils;
}

/** Walk to 參數調整 the way an owner does, and WAIT for the settings fetch: the
 * card renders nothing at all until it lands, so a synchronous query would fail
 * on an empty page rather than on a missing row. */
async function openParamsPage() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByTestId("settings-params-entry"));
  await utils.findByLabelText(zh.settings.docCapDuty);
  return utils;
}

const cardWithTitle = (utils: { container: HTMLElement }, title: string) =>
  Array.from(utils.container.querySelectorAll<HTMLElement>(".mp-lessons")).find(
    (el) => el.querySelector(".mp-lessons__title")?.textContent?.includes(title)
  );

async function setCaps() {
  await mockApi.patchServerSettings({
    docCapCharsDuty: DUTY_CAP,
    docCapCharsInsight: INSIGHT_CAP,
  });
}

describe("T-ae38 — the journal blocks each show their OWN budget", () => {
  it("the Insight card shows size / cap, and it is the INSIGHT cap", async () => {
    // Multi-byte on purpose: the server counts RUNES, and `String.length` would
    // agree with a rune count on ASCII. "環境筆記" is 4 runes / 12 bytes.
    await setCaps();
    await mockApi.saveInsight("assistant", "環境筆記");
    await mockApi.saveRole("assistant", { definitionMd: "職責：守門" });

    const utils = await openRolePage(zh.office.role.assistant);
    const card = cardWithTitle(utils, mp.insight);
    expect(card).toBeTruthy();
    const size = card!.querySelector(".mp-insight__size");
    expect(size?.textContent?.replace(/\s+/g, " ").trim()).toBe(
      `4 / ${INSIGHT_CAP}`
    );
    // The negative half: the Duty readout on the same page keeps its OWN cap,
    // so a cockpit that went back to one shared number reddens here.
    expect(
      utils.getByTestId("doc-card-usage").textContent?.replace(/\s+/g, " ").trim()
    ).toBe(`5 / ${DUTY_CAP}`);
  });

  it("the role-definition editor shows its length against the DUTY cap", async () => {
    await setCaps();
    await mockApi.saveRole("assistant", { definitionMd: "職責：守門" });

    const utils = await openRolePage(zh.office.role.assistant);
    const usage = utils.getByTestId("doc-card-usage");
    expect(usage.textContent?.replace(/\s+/g, " ").trim()).toBe(
      `5 / ${DUTY_CAP}`
    );
  });

  it("the Duty readout stays up while EDITING — that is when it is wanted", async () => {
    // A readout that vanishes the moment you open the editor answers the
    // question everywhere except where it is asked.
    await setCaps();
    await mockApi.saveRole("assistant", { definitionMd: "職責：守門" });

    const utils = await openRolePage(zh.office.role.assistant);
    fireEvent.click(utils.getAllByText(s.edit)[0]);
    expect(
      utils.getByTestId("doc-card-usage").textContent?.replace(/\s+/g, " ").trim()
    ).toBe(`5 / ${DUTY_CAP}`);
  });

  it("the global-context editor shows NO budget — it genuinely has no cap", async () => {
    // T-ae38's scope line: global_context is still uncapped on purpose. A
    // "0 / 0" here would invent a limit the server does not enforce, and the
    // owner has been promised in docs/guide/settings.md that it has none.
    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.globalContext));
    fireEvent.click(await utils.findByText(s.customName));
    await utils.findAllByText(s.edit);
    expect(utils.queryByTestId("doc-card-usage")).toBeNull();
  });
});

describe("T-ae38 / T-30f1 — one knob per capped document, each writing its own key", () => {
  it("renders one row per cap, each seeded from its own setting", async () => {
    await setCaps();
    const utils = await openParamsPage();

    const read = (label: string) =>
      (utils.getByLabelText(label) as HTMLInputElement).value;
    expect(read(s.docCapDuty)).toBe(String(DUTY_CAP));
    expect(read(s.docCapInsight)).toBe(String(INSIGHT_CAP));
    // Untouched by setCaps → still the shipped default, which proves the rows
    // are not all reading one value.
    expect(read(s.docCapManualSop)).toBe(
      String(DOC_CAP_CHARS_DEFAULTS.manualSop)
    );
  });

  it("editing the Duty row moves ONLY the Duty setting", async () => {
    // The failure this catches is a row wired to the wrong field: it looks
    // right, saves without error, and moves someone else's cap.
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.docCapDuty);
    fireEvent.change(input, { target: { value: "2500" } });
    fireEvent.blur(input);

    const after = await mockApi.getServerSettings();
    expect(after.docCapCharsDuty).toBe(2500);
    expect(after.docCapCharsInsight).toBe(DOC_CAP_CHARS_DEFAULTS.insight);
    expect(after.docCapCharsManualSop).toBe(DOC_CAP_CHARS_DEFAULTS.manualSop);
  });

  it("editing the manual SOP row moves ONLY the SOP cap", async () => {
    // The same failure in its manual-shaped form: a row that looks independent
    // but writes a neighbour's key saves without error and moves the wrong
    // budget.
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.docCapManualSop);
    fireEvent.change(input, { target: { value: "40000" } });
    fireEvent.blur(input);

    const after = await mockApi.getServerSettings();
    expect(after.docCapCharsManualSop).toBe(40000);
    expect(after.docCapCharsInsight).toBe(DOC_CAP_CHARS_DEFAULTS.insight);
    expect(after.docCapCharsDuty).toBe(DOC_CAP_CHARS_DEFAULTS.duty);
  });

  it("the Duty row goes BELOW its shipped default, down to the shared floor", async () => {
    // 🔴 Owner 2026-09-07 (card rc-5b66ba099e28 option [1]): the floor is
    // DOC_CAP_CHARS_MIN for every row, not each row's own shipped default. So
    // a value UNDER the shipped Duty default is legal now, and the row must
    // store it — the assertion that used to sit here refused 999 and would
    // pass against a knob that is still raise-only.
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.docCapDuty);
    expect(999).toBeLessThan(DOC_CAP_CHARS_DEFAULTS.duty);
    fireEvent.change(input, { target: { value: "999" } });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).docCapCharsDuty).toBe(999);

    // CONTROL, so the pass above is not just "this row never validates":
    // under the SHARED floor is still refused locally and writes nothing.
    fireEvent.change(input, { target: { value: String(DOC_CAP_CHARS_MIN - 1) } });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).docCapCharsDuty).toBe(999);
  });

  it("the Insight row takes 1200 too — the floor is shared, not per-row", async () => {
    // The other side of the same coin. 1200 is far below Insight's own
    // shipped default and was refused while each row carried its own floor;
    // one shared floor is what makes it legal, and this is the row where the
    // gap between the two rules is widest.
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.docCapInsight);
    expect(1200).toBeLessThan(DOC_CAP_CHARS_DEFAULTS.insight);
    fireEvent.change(input, { target: { value: "1200" } });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).docCapCharsInsight).toBe(1200);

    // CONTROL: the shared floor is a real floor, not an absent one.
    fireEvent.change(input, { target: { value: String(DOC_CAP_CHARS_MIN - 1) } });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).docCapCharsInsight).toBe(1200);
  });
});

describe("T-ae38 — capForKind routes each document kind to its own cap", () => {
  it("maps every kind, and gives the uncapped ones no number at all", () => {
    // Transcribed from restoreDocumentHistory's switch. `undefined` for the
    // uncapped kinds is load-bearing: falling back to "whichever number is
    // nearest" would let the cockpit mark a global-context revision
    // un-restorable when the server would accept it.
    // DISTINCT numbers throughout: two segments sharing one would make
    // "routed to its own cap" and "routed to whichever cap" the same assertion.
    const caps = {
      duty: 1,
      insight: 2,
      manualSop: 4,
      systemInteraction: 6,
      bootSequence: 7,
      offboard: 8,
      taskEvent: 9,
    };
    expect(capForKind("role_definition", caps)).toBe(1);
    expect(capForKind("insight", caps)).toBe(2);
    expect(capForKind("task_manual_sop", caps)).toBe(4);
    // T-791e: the two boot-context blocks answer to their own
    // `doc.cap_chars.*` settings, so they route like every other capped kind
    // rather than abstaining — and they route to DIFFERENT numbers, because
    // the system block's default is four times the boot sequence's.
    expect(capForKind("system_interaction", caps)).toBe(6);
    expect(capForKind("boot_sequence", caps)).toBe(7);
    // T-c9c0: the 〈停止〉 document has its own knob too.
    expect(capForKind("offboard", caps)).toBe(8);
    // T-3201: 加速停止 shares 〈停止〉's knob — the server's registry row calls
    // `offboardCap()` for both — while the four task-event procedures answer to
    // the one task-event ceiling.
    expect(capForKind("accelerated_stop", caps)).toBe(8);
    expect(capForKind("task_closeout", caps)).toBe(9);
    expect(capForKind("task_reassign_predecessor", caps)).toBe(9);
    expect(capForKind("task_takeover_with_predecessor", caps)).toBe(9);
    expect(capForKind("task_takeover_fresh", caps)).toBe(9);
    expect(capForKind("task_unblocked", caps)).toBe(9);
    // The retired bundle kind has no restore path left; it takes the SOP's cap,
    // the one document the bundle still had when it was split.
    expect(capForKind("task_manual", caps)).toBe(4);
    expect(capForKind("global_context", caps)).toBeUndefined();
    expect(capForKind("task_description", caps)).toBeUndefined();
  });
});
