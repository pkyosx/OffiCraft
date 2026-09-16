// The cutover-effect mark on a machine row — Monitor §2.
//
// FOUR states, and the whole file is about which of them is allowed to SPEAK.
// 🔴 The rule CHANGED on 2026-08-04 (owner, rc-aaa0e7967f8a). It is now:
//
//   "not_effective" proven otherwise → the short amber mark — the ONLY state
//                   that renders anything at all
//   "effective"     proven in effect → nothing rendered
//   "unproven"      the machine checked and could not tell → nothing rendered
//   null            the machine has never reported → nothing rendered
//
// ⚠️ The previous rule was the opposite for the last two: they each had their
// own grey sentence, and this file asserted that the three non-effective states
// could NOT share one blank. That assertion was guarding a real incident — four
// states sharing one blank is how a machine whose cutover had NOT taken effect
// looked healthy for three hours — so it was not wrong to have it. The owner
// deliberately narrowed what that guard protects: the incident's distinction
// (measured-and-FAILED vs everything else) still stands and is asserted below;
// what was given up is telling apart the two states that are the ABSENCE of an
// answer, because a reader who finishes either sentence can do nothing with it.
//
// ⛔ So the assertions here were REWRITTEN, not relaxed. Nothing is commented
// out, nothing was widened to "some text or none". What is pinned now:
//   (1) "not_effective" renders the mark, and that mark is its own dictionary
//       string (not some other state's text);
//   (2) "unproven" and null render NO cutover element at all — asserted as the
//       element being ABSENT, never as "its text is empty", because an empty
//       element still costs a row and still hides the mark's meaning;
//   (3) "effective" renders no cutover element either — the cell is character
//       for character what it is with no mark;
//   (4) the copy carries no internal vocabulary and tells nobody to restart.

import { readFile } from "node:fs/promises";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { en } from "../i18n/locales/en";
import { zh } from "../i18n/locales/zh";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView, CutoverEffect } from "../types";

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () =>
      Promise.resolve({ accounts: [], sessions: [], machines: [] }),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    getBackupHealth: () =>
      Promise.resolve({
        status: "healthy",
        code: "",
        detail: "",
        newestBackupTs: 1785600000,
        newestBackupAgeSecs: 3600,
        staleAfterSecs: 43200,
        sinceTs: null,
        checkedTs: 1785603600,
      }),
    subscribeEvents: () => () => {},
  },
}));

const machine = (cutoverEffect: CutoverEffect): MachineView => ({
  machineId: "m-under-test",
  displayName: "m-under-test",
  online: true,
  isSelf: false,
  binStatus: null,
  wardenShape: null,
  cutoverEffect,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});

/** The three states that render NOTHING, named for the failure messages below.
 * null is a peer entry, not a default — "this warden never reported" is a fact
 * about the machine, not a gap in the fixture. "not_effective" is deliberately
 * absent: it is the one state with a face, and it is asserted on its own. */
const SILENT_STATES: ReadonlyArray<readonly [name: string, effect: CutoverEffect]> =
  [
    ["proven effective", "effective"],
    ["checked, could not prove", "unproven"],
    ["never reported (null)", null],
  ];

/** Render ONE machine in the given state and return the whole text of the cell
 * the line lives in. The cell, not the line: a test that reads only the line's
 * own element cannot see the state that renders no line at all, which is the
 * one this ticket is about. Rendered one at a time so a row can never borrow a
 * sibling row's markup. */
async function cellTextFor(effect: CutoverEffect) {
  listMachines.mockResolvedValue([machine(effect)]);
  render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
  const idBadge = await screen.findByTestId("mon-machine-id");
  const cell = idBadge.closest("td");
  if (cell === null) throw new Error("the machine id badge is not inside a cell");
  const text = (cell.textContent ?? "").trim();
  cleanup();
  return text;
}

describe("MonitorPage cutover-effect line", () => {
  beforeEach(() => {
    listMembers.mockResolvedValue([]);
  });

  it("adds the mark to the cell on a proven failure, and only there", async () => {
    // The proven failure's cell is the silent cell PLUS the mark, so the silent
    // cell is the common prefix and can be pinned as "the cell with nothing
    // added" without hard-coding what the mark contains.
    const silent = await cellTextFor("effective");
    const failing = await cellTextFor("not_effective");
    expect(
      failing.startsWith(silent),
      `"proven not effective" changed the rest of the cell rather than adding to it`
    ).toBe(true);
    expect(
      failing.length,
      `"proven not effective" added nothing — it is indistinguishable from a machine with no problem`
    ).toBeGreaterThan(silent.length);
  });

  it("gives the three states with no answer to give the SAME blank cell", async () => {
    // ⚠️ This is the inverse of what this test used to assert, and it is the
    // owner's decision, not a slip: "measured and fine", "measured and could not
    // tell" and "never measured" are all states the reader can do nothing with,
    // so they now read identically. The distinction the incident was about —
    // measured and FAILED vs everything else — is the test above, and it stands.
    const texts = new Map<string, string>();
    for (const [name, effect] of SILENT_STATES) {
      texts.set(name, await cellTextFor(effect));
    }
    const [[firstName, firstText], ...rest] = [...texts];
    for (const [name, text] of rest) {
      expect(
        text,
        `"${name}" says something "${firstName}" does not — the three states ` +
          `with no answer are meant to be one and the same blank`
      ).toBe(firstText);
    }
  });

  it("binds the mark to the failure state's OWN dictionary string", async () => {
    // Rendering "some text" is not the requirement: the mark has to be the
    // string that means "not in effect". Asserted against the actual copy so
    // that wiring the row to any other key fails here.
    //
    // I18nProvider defaults to zh (i18n/index.tsx), so zh is what renders here.
    const text = await cellTextFor("not_effective");
    expect(text, "the proven failure did not render its own mark").toContain(
      zh.monitor.machine.cutoverNotInEffect
    );
  });

  it("paints the failure amber", async () => {
    // jsdom does not apply the imported CSS, so the stylesheet is the only place
    // this distinction exists. The one state with a face keeps the alarm colour
    // and takes it from the theme token, never a literal — the cockpit has
    // user-authored themes.
    // Read from the repo path rather than through `import.meta.url`: vitest
    // does not hand test modules a file: URL, so resolving against it throws.
    const css = await readFile("src/components/monitor.css", "utf8");
    const ruleFor = (cls: string) => {
      const m = css.match(new RegExp(`\\.${cls}\\s*\\{([^}]*)\\}`));
      return m === null ? null : m[1];
    };
    const warn = ruleFor("mon-cutover-warn");
    if (warn === null) throw new Error("no .mon-cutover-warn rule in monitor.css");
    expect(warn).toContain("var(--color-warn-fg)");
  });

  it("renders no cutover element whatsoever on the three silent states", async () => {
    // ⛔ Absence of the ELEMENT, not emptiness of its text: a zero-width or
    // empty-text placeholder would still hold a row of space and would still
    // satisfy a "renders no text" assertion.
    for (const [name, effect] of SILENT_STATES) {
      listMachines.mockResolvedValue([machine(effect)]);
      render(
        <I18nProvider>
          <MonitorPage />
        </I18nProvider>
      );
      await screen.findByTestId("mon-machine-id");
      expect(
        screen.queryByTestId("mon-cutover-warning"),
        `"${name}" rendered a cutover element; it must render none at all`
      ).toBeNull();
      cleanup();
    }
  });

  it("still renders the element on the one state that speaks", async () => {
    // The control for the test above: "no element anywhere, ever" would pass it
    // while silently deleting the only face the incident left us.
    listMachines.mockResolvedValue([machine("not_effective")]);
    render(
      <I18nProvider>
        <MonitorPage />
      </I18nProvider>
    );
    const warn = await screen.findByTestId("mon-cutover-warning");
    expect(warn.className).toContain("mon-cutover-warn");
    expect(warn.textContent).toBe(zh.monitor.machine.cutoverNotInEffect);
  });
});

describe("cutover-effect copy", () => {
  // Checked against the DICTIONARIES rather than the rendered page, because the
  // page renders one locale and the rule applies to every one of them — a zh
  // string that leaks the vocabulary would sail past a render-only check.
  const strings = [en, zh].map((dict) => dict.monitor.machine.cutoverNotInEffect);

  it("carries no internal vocabulary", () => {
    // Owner, verbatim: nobody outside this codebase knows what "anchor" is. A
    // warning nobody can read is not a warning.
    for (const text of strings) {
      for (const word of ["anchor", "legacy", "plist", "launchd", "tmux"]) {
        expect(text.toLowerCase(), `"${text}" leaks "${word}"`).not.toContain(word);
      }
    }
  });

  it("tells nobody to restart anything", () => {
    // This surface makes the state VISIBLE and stops there — deciding when to
    // act is a person's call, deliberately, and an automatic-repair suggestion
    // is the option the owner ruled out.
    for (const text of strings) {
      for (const word of ["restart", "reboot", "kill ", "重啟", "重新啟動"]) {
        expect(
          text.toLowerCase(),
          `"${text}" instructs the reader to ${word.trim()} something`
        ).not.toContain(word);
      }
    }
  });

  it("stays a MARK in every locale — non-empty, and short", () => {
    // ⚠️ This replaces "says something different in each state": there is only
    // one state that says anything now, so that rule has nothing left to range
    // over. What took its place is the property the owner actually bought — the
    // reason the three sentences were dropped is that they were long enough to
    // eat the machine's row. A regression back to prose would slip past every
    // other test in this file, so the length is pinned here.
    //
    // The cap is generous on purpose (it is a smell test, not a design token):
    // "Not in effect" is 13 and 未生效 is 3, while the sentences it replaced ran
    // 120+. Anything that trips this is a paragraph, not a mark.
    for (const text of strings) {
      expect(text.trim(), "the mark must not be empty").not.toBe("");
      expect(
        text.length,
        `"${text}" is a sentence again, not a mark`
      ).toBeLessThanOrEqual(24);
    }
  });
});
