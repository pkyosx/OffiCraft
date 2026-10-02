// Delete-machine confirm · the dialog has to name the consequence (T-9cf8).
//
// 🔴 WHY A TEST ABOUT WORDS — same reason as i18n/dispatch-alert-copy.test.ts,
// and the same precedent this repo already booked: an install gate promised it
// would not interrupt service, and did. Consent obtained from an inaccurate
// description is not consent.
//
// T-9cf8 made the machine roster the authority over machine credentials: taking
// a machine off the roster revokes its token on the very next request, and any
// agent still pinned to that machine loses access with it. The dialog's old
// text — "this only removes the machine's record from the list" — described a
// bookkeeping edit. It is now a description of a cheaper action than the one
// the button performs, so it has to change WITH the behaviour.
//
// TWO HALVES, because either alone is decoration:
//   1. the copy in EVERY locale carries the three consequences (credentials die
//      / the agents on it are affected / it cannot be undone, recovery = a
//      re-install), asserted by concept substrings rather than whole sentences
//      so the prose stays free to improve;
//   2. that copy actually REACHES the dialog — read off the rendered
//      `mon-delete-confirm` node, never off the source file. A test that reads
//      the dictionary alone would still pass if the dialog rendered something
//      else entirely.
//
// Mutant: put the old body back ("這只會從清單移除該機器的記錄…" /
// "This only removes the machine's record from the list") and both halves fail.
//
// Deliberately NOT here: a "type the machine name to confirm" gate. Nobody
// asked for one, and it is a different decision from telling the truth.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import type { Dict } from "../i18n/locales/zh";
import { makeMessages } from "../i18n/compose";
import type { Member, MachineView } from "../types";
import { machineAction } from "./machineActions.testHelper";

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

/** One consequence the copy must state, and the substrings that state it in
 * this locale. Any ONE alternative counts, so wording may be rephrased; the
 * consequence itself may not be dropped. */
interface Consequence {
  what: string;
  anyOf: string[];
}

interface LocaleCase {
  name: "zh" | "en";
  dict: Dict;
  must: Consequence[];
  /** The pre-T-9cf8 body, which described a strictly cheaper action. */
  bannedUnderstatement: string[];
}

const LOCALES: LocaleCase[] = [
  {
    name: "zh",
    dict: zh,
    must: [
      { what: "憑證會失效（機器無法再連線）", anyOf: ["憑證", "token", "無法連線"] },
      { what: "而且是立刻", anyOf: ["立刻", "馬上", "即刻"] },
      { what: "機器上的 agent 受影響", anyOf: ["agent", "成員", "代理"] },
      { what: "不可復原", anyOf: ["無法復原", "不可復原", "無法還原"] },
      { what: "恢復要重裝", anyOf: ["重新安裝", "重裝"] },
    ],
    // 舊文案:「這只會從清單移除該機器的記錄」
    bannedUnderstatement: ["只會從清單移除"],
  },
  {
    name: "en",
    dict: en,
    must: [
      { what: "the credentials die (the machine can no longer connect)", anyOf: ["credential", "token", "can no longer connect"] },
      { what: "and immediately", anyOf: ["immediately", "at once", "right away"] },
      { what: "the agents on it are affected", anyOf: ["agent", "member"] },
      { what: "it cannot be undone", anyOf: ["cannot be undone", "irreversible"] },
      { what: "recovery is a re-install", anyOf: ["installing it again", "re-install", "reinstall"] },
    ],
    // old copy: "This only removes the machine's record from the list"
    bannedUnderstatement: ["only removes the machine's record"],
  },
];

const MACHINE_NAME = "Alpha";

describe("delete-machine confirm copy · it must name what deleting costs (T-9cf8)", () => {
  for (const loc of LOCALES) {
    const body = makeMessages(loc.dict, loc.name).machineDeleteConfirmBody(
      MACHINE_NAME
    );
    const lower = body.toLowerCase();

    it(`${loc.name}: names the machine and every consequence`, () => {
      expect(body).toContain(MACHINE_NAME);
      for (const c of loc.must) {
        const hit = c.anyOf.some((s) => lower.includes(s.toLowerCase()));
        expect(
          hit,
          `${loc.name} deleteConfirmBody must state "${c.what}" (any of ${JSON.stringify(
            c.anyOf
          )}), got: ${body}`
        ).toBe(true);
      }
    });

    it(`${loc.name}: no longer describes a cheaper action than the button performs`, () => {
      for (const banned of loc.bannedUnderstatement) {
        expect(
          lower.includes(banned.toLowerCase()),
          `${loc.name} deleteConfirmBody fell back to the pre-T-9cf8 understatement ${JSON.stringify(
            banned
          )}: ${body}`
        ).toBe(false);
      }
    });
  }
});

describe("delete-machine confirm dialog · the consequence reaches the screen", () => {
  beforeEach(() => {
    listMachines.mockResolvedValue([
      {
        machineId: "m-1",
        displayName: MACHINE_NAME,
        online: true,
        isSelf: false,
        binStatus: null,
        wardenShape: null,
        cutoverEffect: null,
        claudeVersion: null,
        claudeCredSource: null,
        claudeSubReadable: null,
      },
    ]);
    listMembers.mockResolvedValue([]);
  });

  it("renders the credential/irreversibility consequence in the confirm box", async () => {
    render(
      <I18nProvider>
        <MonitorPage />
      </I18nProvider>
    );
    fireEvent.click(await machineAction("mon-delete-btn", 0));

    // Read the RENDERED dialog, not the dictionary: this is the half that
    // proves the copy is wired to the button the owner actually presses.
    const dialog = await screen.findByTestId("mon-delete-confirm");
    const text = (dialog.textContent ?? "").toLowerCase();
    // The suite renders in zh (see MonitorPage.uninstall-guard.test.tsx).
    for (const c of LOCALES[0].must) {
      const hit = c.anyOf.some((s) => text.includes(s.toLowerCase()));
      expect(
        hit,
        `the rendered delete dialog must state "${c.what}", got: ${dialog.textContent}`
      ).toBe(true);
    }
    expect(text).not.toContain("只會從清單移除");
  });
});
