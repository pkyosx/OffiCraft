// The cutover-effect mark on a machine row — Monitor §2.
//
// FOUR states, and only one of them may show anything:
//
//   "not_effective" proven otherwise → the members' warning exclamation beside
//                   the online dot; its hint (hover, focus or a click / tap) and
//                   its accessible name are the full sentence
//   "effective"     proven in effect → nothing rendered
//   "unproven"      the machine checked and could not tell → nothing rendered
//   null            the machine has never reported → nothing rendered
//
// ⚠️ The proven failure must keep a visible mark: when all four states shared
// one blank, a machine whose cutover had not taken effect looked healthy. The
// silent states are asserted as the ELEMENT being absent, never as "its text is
// empty", because an empty element still holds space.
//
// The expected sentences below are written out by hand, not read from the
// dictionaries, so a wrong dictionary string fails here.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { en } from "../i18n/locales/en";
import { zh } from "../i18n/locales/zh";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView, CutoverEffect } from "../types";

const ZH_HINT =
  "未生效：這台機器的成員還在更新前啟動的環境裡執行，要先把這台機器的成員全部停止，再喚醒，才會生效。";
const EN_HINT =
  "Not in effect: the members on this machine are still running in an environment started before the update. Stop every member on this machine first, then wake them, for it to take effect.";

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

/** Render ONE machine in the given state; returns its 機器 cell. Rendered one
 * at a time so a row can never borrow a sibling row's markup. */
async function renderCell(effect: CutoverEffect) {
  listMachines.mockResolvedValue([machine(effect)]);
  render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
  const idBadge = await screen.findByTestId("mon-machine-id");
  const cell = idBadge.closest("td");
  if (cell === null) throw new Error("the machine id badge is not inside a cell");
  return cell;
}

/** What the 機器 cell shows as text and which marks it holds. */
async function cellFor(effect: CutoverEffect) {
  const cell = await renderCell(effect);
  const out = {
    text: (cell.textContent ?? "").trim(),
    marks: within(cell).queryAllByTestId("mon-cutover-warning").length,
  };
  cleanup();
  return out;
}

describe("MonitorPage cutover-effect mark", () => {
  beforeEach(() => {
    listMembers.mockResolvedValue([]);
    localStorage.removeItem("oc.language");
  });
  afterEach(() => {
    cleanup();
    localStorage.removeItem("oc.language");
  });

  it("a proven failure adds one mark and no text; the three states with no verdict add nothing", async () => {
    const cells = {
      not_effective: await cellFor("not_effective"),
      effective: await cellFor("effective"),
      unproven: await cellFor("unproven"),
      null: await cellFor(null),
    };
    expect(cells.effective.text, "control: the cell shows the name and id").toBe(
      "m-under-testm-under-test"
    );
    expect(cells).toEqual({
      not_effective: { text: "m-under-testm-under-test", marks: 1 },
      effective: { text: "m-under-testm-under-test", marks: 0 },
      unproven: { text: "m-under-testm-under-test", marks: 0 },
      null: { text: "m-under-testm-under-test", marks: 0 },
    });
  });

  it("no 未生效 / Not in effect text is shown on the row at rest, in either language", async () => {
    for (const [lang, word] of [
      ["zh", "未生效"],
      ["en", "Not in effect"],
    ] as const) {
      localStorage.setItem("oc.language", lang);
      const cell = await renderCell("not_effective");
      expect(within(cell).queryByTestId("mon-cutover-warning"), `control (${lang}): the mark is there`).not.toBeNull();
      expect(cell.textContent ?? "", `${lang}: the word is shown on the row`).not.toContain(word);
      cleanup();
    }
  });

  it("the mark is an image whose accessible name is the whole sentence (zh)", async () => {
    await renderCell("not_effective");
    const mark = screen.getByRole("img", { name: ZH_HINT });
    expect(mark.getAttribute("data-testid")).toBe("mon-cutover-warning");
  });

  it("the mark is an image whose accessible name is the whole sentence (en)", async () => {
    localStorage.setItem("oc.language", "en");
    await renderCell("not_effective");
    const mark = screen.getByRole("img", { name: EN_HINT });
    expect(mark.getAttribute("data-testid")).toBe("mon-cutover-warning");
  });

  it("hover shows the sentence and leaving hides it; a click keeps it after the pointer leaves", async () => {
    await renderCell("not_effective");
    const mark = screen.getByTestId("mon-cutover-warning");
    expect(screen.queryByRole("tooltip"), "control: nothing open at rest").toBeNull();
    fireEvent.mouseEnter(mark);
    expect(screen.getByRole("tooltip").textContent).toBe(ZH_HINT);
    fireEvent.mouseLeave(mark);
    expect(screen.queryByRole("tooltip")).toBeNull();
    fireEvent.click(mark);
    fireEvent.mouseLeave(mark);
    expect(screen.getByRole("tooltip").textContent, "a click pins it").toBe(ZH_HINT);
    fireEvent.click(document.body);
    expect(screen.queryByRole("tooltip"), "a click elsewhere closes it").toBeNull();
  });

  it("is the members' warning exclamation, not a look of its own", async () => {
    // The owner asked for the same mark the roster shows for a member's
    // warning; its classes are what give it that look and colour.
    await renderCell("not_effective");
    const mark = screen.getByTestId("mon-cutover-warning");
    expect({
      className: mark.className,
      icon: mark.querySelectorAll("svg").length,
      focusable: mark.tabIndex,
    }).toEqual({
      className: "runtime-login-warning runtime-login-warning--danger",
      icon: 1,
      focusable: 0,
    });
  });

  it("sits right after the online dot", async () => {
    await renderCell("not_effective");
    const mark = screen.getByTestId("mon-cutover-warning");
    expect(mark.previousElementSibling?.getAttribute("data-testid")).toBe("mon-machine-online");
  });
});

describe("cutover-effect copy", () => {
  it("the dictionaries hold the hand-written sentences", () => {
    expect({
      zh: zh.monitor.machine.cutoverNotInEffectHint,
      en: en.monitor.machine.cutoverNotInEffectHint,
    }).toEqual({ zh: ZH_HINT, en: EN_HINT });
  });

  it("carries no internal vocabulary", () => {
    // Nobody outside this codebase knows what an anchor is.
    for (const text of [zh, en].map((d) => d.monitor.machine.cutoverNotInEffectHint)) {
      for (const word of ["anchor", "legacy", "plist", "launchd", "tmux", "cutover", "warden"]) {
        expect(text.toLowerCase(), `"${text}" leaks "${word}"`).not.toContain(word);
      }
    }
  });

  it("says stop all then wake, never restart", () => {
    // Restarting members one at a time leaves the process that carries them
    // running, so a hint that says "restart" sends the reader down a path that
    // changes nothing.
    for (const text of [zh, en].map((d) => d.monitor.machine.cutoverNotInEffectHint)) {
      for (const word of ["restart", "reboot", "重啟", "重新啟動", "重開"]) {
        expect(text.toLowerCase(), `"${text}" says ${word}`).not.toContain(word);
      }
    }
  });
});
