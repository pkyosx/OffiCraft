// 帳號詳情 modal (T-a9a7) — Monitor §1 account card "詳情" entry.
//
// The modal shows the REAL identity behind a claude account row: the stable
// key 全文, the userID hash / org-uuid dimension split (T-f694: the key is
// "<userID>/<orgUuid>", the plan never joins it), and the email/org derived
// from the OWNER-ONLY account_label. Honesty lock: a null label renders the
// email/org rows as "—" (never guessed from displayName).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import {
  MonitorPage,
  splitAccountKey,
  parseAccountLabel,
} from "./MonitorPage";
import type { Member, MachineView, MonAccountView } from "../types";

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);
const getMonitoring = vi.fn(async () => ({
  accounts: [] as MonAccountView[],
  sessions: [],
  machines: [],
}));

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () => getMonitoring(),
    // The AI Sessions table now also lists outsource workers via
    // useOutsourceWorkers — stub its data sources so this member-focused test
    // renders without the hook rejecting.
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    subscribeEvents: () => () => {},
  },
}));

const acct = (over: Partial<MonAccountView> = {}): MonAccountView => ({
  account: "acct-123/9f8e-uuid",
  accountLabel: "eva@example.test(Example Org)",
  displayName: "Eva 的帳號",
  machine: "mbp5",
  cost: 1234.5,
  fiveHour: null,
  sevenDay: null,
  ...over,
});

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

async function openDetail() {
  fireEvent.click(await screen.findByTestId("mon-acct-detail-open"));
  return screen.findByTestId("mon-acct-detail-modal");
}

beforeEach(() => {
  getMonitoring.mockResolvedValue({
    accounts: [acct()],
    sessions: [],
    machines: [],
  });
});

describe("account detail modal", () => {
  it("opens from the 詳情 button and shows key/userID/orgUuid/email/org/cost", async () => {
    renderMonitor();
    const modal = await openDetail();
    const body = within(modal).getByTestId("mon-acct-detail-body");
    // key 全文 + the derived split
    expect(within(body).getByText("acct-123/9f8e-uuid")).toBeTruthy();
    expect(within(body).getByText("acct-123")).toBeTruthy();
    expect(within(body).getByText("9f8e-uuid")).toBeTruthy();
    expect(within(body).getByText("組織 UUID")).toBeTruthy();
    // email + org derived from the owner-only label; raw label listed verbatim
    expect(within(body).getByText("eva@example.test")).toBeTruthy();
    expect(within(body).getByText("Example Org")).toBeTruthy();
    expect(
      within(body).getByText("eva@example.test(Example Org)")
    ).toBeTruthy();
    // machines + estimated cost
    expect(within(body).getByText("mbp5")).toBeTruthy();
    expect(within(body).getByText("$1,235")).toBeTruthy();
  });

  it("renders 一 dashes for email/org/label when accountLabel is null (honest, never guessed)", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [acct({ accountLabel: null, cost: null, machine: "" })],
      sessions: [],
      machines: [],
    });
    renderMonitor();
    const modal = await openDetail();
    const body = within(modal).getByTestId("mon-acct-detail-body");
    // email / org / label 原文 / machines / cost all fall back to "—":
    expect(within(body).getAllByText("—").length).toBeGreaterThanOrEqual(5);
    // and the displayName must NOT leak into the email/org rows
    expect(within(body).queryByText("Eva 的帳號")).toBeNull();
  });

  it("renders 一 dash for the org UUID row on a bare-userID key", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [acct({ account: "acct-123", accountLabel: null })],
      sessions: [],
      machines: [],
    });
    renderMonitor();
    const modal = await openDetail();
    const body = within(modal).getByTestId("mon-acct-detail-body");
    expect(within(body).getByText("組織 UUID")).toBeTruthy();
    expect(within(body).queryByText("9f8e-uuid")).toBeNull();
  });

  // T-cb1f: the 帳號卡 used to carry a "· <machine>" chip next to the name. It
  // read the SAME MonAccountView.machine string this modal's 使用機器 row
  // renders, so the modal is a strict superset and the chip was pure
  // duplication. These two tests are a pair: the chip must stay gone ONLY for
  // as long as the modal row keeps showing the value in full.
  it("shows the 使用機器 value in full, including a comma-joined multi-machine string", async () => {
    // server-side, a multi-host account is collapsed to one comma-joined string
    // (api_monitoring.go: Machine: strings.Join(hostLabels, ", ")) — the modal
    // must render it VERBATIM, not truncated to the first host.
    getMonitoring.mockResolvedValue({
      accounts: [acct({ machine: "jason-m5, jason-mbp" })],
      sessions: [],
      machines: [],
    });
    renderMonitor();
    const modal = await openDetail();
    const body = within(modal).getByTestId("mon-acct-detail-body");
    // the 使用機器 row exists ...
    const row = within(body)
      .getByText("使用機器")
      .closest(".mon-detailrow") as HTMLElement;
    expect(row).toBeTruthy();
    // ... and carries the whole joined value, both hosts included
    expect(within(row).getByText("jason-m5, jason-mbp")).toBeTruthy();
  });

  // The chip must stay gone in BOTH shapes MonAccountView.machine can take.
  // These two cases are NOT redundant: a chip resurrected under a host-count
  // condition (e.g. `!account.machine.includes(",")`) is invisible to whichever
  // case is missing. The single-host one is the shape owner actually reported.
  async function cardOf(machine: string): Promise<HTMLElement> {
    getMonitoring.mockResolvedValue({
      accounts: [acct({ machine })],
      sessions: [],
      machines: [],
    });
    renderMonitor();
    // scope = the card, anchored off the 詳情 button that lives inside it
    const card = (await screen.findByTestId("mon-acct-detail-open")).closest(
      ".mon-acct"
    ) as HTMLElement;
    // positive control: the scope is live and still carries what we KEEP on the
    // card — without this, the negative assertions below could pass vacuously.
    expect(card).toBeTruthy();
    expect(within(card).getByText("Eva 的帳號")).toBeTruthy();
    return card;
  }

  it("does NOT render the machine name on the 帳號卡 itself — single host (chip removed)", async () => {
    // the shape owner circled in the screenshot: "· jason-m5", one machine.
    const card = await cardOf("jason-m5");
    // modal still closed, so the card is the only render site in play
    expect(card.textContent).not.toContain("jason-m5");
  });

  it("does NOT render the machine name on the 帳號卡 itself — multi host (chip removed)", async () => {
    const card = await cardOf("jason-m5, jason-mbp");
    expect(card.textContent).not.toContain("jason-m5");
    expect(card.textContent).not.toContain("jason-mbp");
  });

  it("closes via ✕, Esc, and the backdrop", async () => {
    renderMonitor();
    // ✕
    await openDetail();
    fireEvent.click(screen.getByTestId("mon-acct-detail-close"));
    expect(screen.queryByTestId("mon-acct-detail-modal")).toBeNull();
    // Esc
    await openDetail();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByTestId("mon-acct-detail-modal")).toBeNull();
    // backdrop (the box itself must NOT close)
    const modal = await openDetail();
    fireEvent.click(within(modal).getByTestId("mon-acct-detail-body"));
    expect(screen.queryByTestId("mon-acct-detail-modal")).toBeTruthy();
    fireEvent.click(modal);
    expect(screen.queryByTestId("mon-acct-detail-modal")).toBeNull();
  });
});

describe("splitAccountKey", () => {
  it("splits at the LAST slash", () => {
    expect(splitAccountKey("acct-123/9f8e-uuid")).toEqual({
      userId: "acct-123",
      orgUuid: "9f8e-uuid",
    });
    // a userID that itself contains a slash still splits at the last one
    expect(splitAccountKey("a/b/9f8e-uuid")).toEqual({
      userId: "a/b",
      orgUuid: "9f8e-uuid",
    });
  });
  it("bare userID (no slash) → orgUuid null", () => {
    expect(splitAccountKey("acct-123")).toEqual({
      userId: "acct-123",
      orgUuid: null,
    });
  });
});

describe("parseAccountLabel", () => {
  it("splits base + trailing (org)", () => {
    expect(parseAccountLabel("eva@example.test(Example Org)")).toEqual({
      base: "eva@example.test",
      org: "Example Org",
    });
    // nested parens: org is the LAST parenthesis group
    expect(parseAccountLabel("a(b)(c)")).toEqual({ base: "a(b)", org: "c" });
  });
  it("no trailing parenthesis → org null", () => {
    expect(parseAccountLabel("eva@example.test")).toEqual({
      base: "eva@example.test",
      org: null,
    });
    // a leading "(...)"-only label has no base prefix — kept whole, honest
    expect(parseAccountLabel("(Example Org)")).toEqual({
      base: "(Example Org)",
      org: null,
    });
  });
});
