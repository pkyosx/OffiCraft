// 定期訊息 · ScheduledMessagesCard (T-f059).
//
// Locked here:
//   1. Every schedule the list returns is rendered.
//   2. A create is reflected in the list WITHOUT remounting the panel — the
//      acceptance wording is 「改完立即生效、不需重開成員」, and the only thing
//      that makes it true is the hook refetching after each mutation. The
//      stateful mock below is what turns that into an observable: it returns a
//      fresh list on every GET, so a component that skipped the refetch would
//      keep showing the pre-create list and this test would red.
//   3. Deleting goes through the shared ConfirmModal, not straight to the wire.
//   4. Picking a day of month that some months lack (29/30/31) shows the skip
//      warning, and picking a day every month HAS does not. BOTH directions are
//      asserted: a hint that is always on would satisfy the first alone.
//   5. 🔴 The OUTSOURCE panel grows the card too. `WorkerDetailPanel` had no
//      `extraExpandCards` caller at all before this ticket, which is exactly how
//      the webhook section ended up existing on only one of the two panels.
//   6. An existing schedule can be EDITED — every setting a create can reach —
//      and the saved values appear without a remount. Cancel keeps the stored
//      values; a rejected save stays on screen as an error and never lets the
//      row read as saved.
//   7. A long message is collapsed on the row and can be opened per row. The
//      collapse is a CLASS on the full string: the whole text still goes back
//      over the wire when that row is edited.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { makeMessages } from "../i18n/compose";
import { ScheduledMessagesCard } from "./ScheduledMessagesCard";
import { MemberDetailPanel } from "./MemberDetailPanel";
import { WorkerDetailPanel } from "./WorkerDetailPanel";
import type { Member } from "../types";
import type {
  OutsourceWorkerView,
  ScheduledMessage,
  ScheduledMessageCreateInput,
  ScheduledMessageUpdate,
} from "../api/adapter";

// ── stateful schedule store (the server's list is the single source) ──
let store: ScheduledMessage[] = [];
let nextId = 0;

function mkSchedule(over: Partial<ScheduledMessage> = {}): ScheduledMessage {
  nextId += 1;
  return {
    id: `sch-00000000000${nextId}`,
    memberId: "mira",
    label: `排程 ${nextId}`,
    body: `訊息 ${nextId}`,
    cadence: "daily",
    dayOfWeek: 0,
    dayOfMonth: 1,
    hour: 9,
    minute: 0,
    customMonths: [],
    customDays: [],
    customHours: [],
    customMinutes: [],
    timezone: "Asia/Taipei",
    status: "enabled",
    lastFiredSlot: "2026-08-10T09:00+08:00",
    lastFiredTs: 0,
    createdTs: 0,
    ...over,
  };
}

const createScheduledMessage = vi.fn(
  async (memberId: string, input: ScheduledMessageCreateInput) => {
    const created = mkSchedule({
      memberId,
      label: input.label ?? "",
      body: input.body,
      cadence: input.cadence,
      dayOfWeek: input.dayOfWeek ?? 0,
      dayOfMonth: input.dayOfMonth ?? 1,
      hour: input.hour ?? 0,
      minute: input.minute ?? 0,
      customMonths: input.customMonths ?? [],
      customDays: input.customDays ?? [],
      customHours: input.customHours ?? [],
      customMinutes: input.customMinutes ?? [],
      timezone: input.timezone,
    });
    store = [...store, created];
    return { ...created };
  }
);
const updateScheduledMessage = vi.fn(
  async (_memberId: string, scheduleId: string, patch: ScheduledMessageUpdate) => {
    const s = store.find((x) => x.id === scheduleId)!;
    // Applies EVERY field the patch carries, not just `status` — otherwise a
    // component that saved an edit and a component that dropped it on the floor
    // would produce the same list, and the edit assertions below could not tell
    // them apart.
    Object.assign(s, patch);
    return { ...s };
  }
);
const deleteScheduledMessage = vi.fn(
  async (_memberId: string, scheduleId: string) => {
    store = store.filter((x) => x.id !== scheduleId);
  }
);

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
    patchMember: () => Promise.resolve(mkMember()),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
    listWebhooks: () => Promise.resolve([]),
    listScheduledMessages: (memberId: string) =>
      Promise.resolve(
        store.filter((s) => s.memberId === memberId).map((s) => ({ ...s }))
      ),
    createScheduledMessage: (
      memberId: string,
      input: ScheduledMessageCreateInput
    ) => createScheduledMessage(memberId, input),
    updateScheduledMessage: (
      memberId: string,
      scheduleId: string,
      patch: ScheduledMessageUpdate
    ) => updateScheduledMessage(memberId, scheduleId, patch),
    deleteScheduledMessage: (memberId: string, scheduleId: string) =>
      deleteScheduledMessage(memberId, scheduleId),
    subscribeEvents: () => () => {},
  },
}));

function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    name: "Mira",
    role: "assistant",
    status: "offline",
    lifecycle: "offline",
    model: "opus",
    effort: "medium",
    kind: "staff",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-mira",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  };
}

function mkWorker(over: Partial<OutsourceWorkerView> = {}): OutsourceWorkerView {
  return {
    id: "ow-7788",
    codename: "O-7788",
    model: "Opus 4.6",
    effort: "high",
    status: "active",
    taskId: "task-1",
    taskTitle: "",
    taskStatus: "in_progress",
    createdTs: 0,
    presence: undefined,
    machine: "",
    desiredMachineId: "",
    account: null,
    contextPct: null,
    cost: null,
    bankedCost: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpReason: "",
    lastOpAt: null,
    creatorId: "",
    delegatedBy: "",
    ...over,
  };
}

const s = zh.mp.schedmsg;
const m = makeMessages(zh, "zh");

/** The FULL sets. Spelled out because that is what the wire carries: "every
 * day" IS the list of every day, and an implementation that shipped a marker
 * instead would satisfy any assertion written the short way. */
const ALL_MONTHS = Array.from({ length: 12 }, (_, i) => i + 1);
const ALL_DAYS = Array.from({ length: 31 }, (_, i) => i + 1);
const ALL_HOURS = Array.from({ length: 24 }, (_, i) => i);
/** The minute cells the group offers by default: 0, 5, 10 … 55. */
const DEFAULT_MINUTES = Array.from({ length: 12 }, (_, i) => i * 5);

// Frozen clock for the last-sent assertions: 2026-08-10 10:00 local, with a
// delivery the evening before.
const NOW = new Date(2026, 7, 10, 10, 0, 0, 0);
const FIRED_AT = new Date(2026, 7, 9, 21, 30, 0, 0);
const TIME_SHAPE = /\d{1,2}\/\d{1,2}\s\d{2}:\d{2}/;

/** Longer than the row shows collapsed. No edge whitespace, so a trim on the
 * way to the wire cannot be mistaken for the truncation being asserted against. */
const LONG_BODY = Array.from(
  { length: 12 },
  (_, i) => `第 ${i + 1} 行:早安,請看一下昨天的 CI 有沒有紅的。`
).join("\n");

beforeEach(() => {
  store = [];
  nextId = 0;
  vi.clearAllMocks();
});

afterEach(() => {
  vi.useRealTimers();
});

/** Render the card alone and open it (it ships collapsed, like the webhook
 * section beside it). */
async function renderOpenCard(memberId = "mira") {
  const view = render(
    <I18nProvider>
      <ScheduledMessagesCard memberId={memberId} />
    </I18nProvider>
  );
  fireEvent.click(await view.findByTestId("mp-schedmsg-toggle"));
  return view;
}

/** Fill in the create form's required fields and submit. */
async function createVia(
  view: Awaited<ReturnType<typeof renderOpenCard>>,
  body: string
) {
  fireEvent.click(await view.findByTestId("mp-schedmsg-add"));
  fireEvent.change(await view.findByTestId("mp-schedmsg-body-input"), {
    target: { value: body },
  });
  fireEvent.click(await view.findByTestId("mp-schedmsg-create"));
}

describe("ScheduledMessagesCard", () => {
  it("renders every schedule the list returns", async () => {
    store = [
      mkSchedule({ label: "每日巡檢", body: "看一下 CI" }),
      mkSchedule({
        label: "週報",
        body: "整理本週進度",
        cadence: "weekly",
        dayOfWeek: 5,
        hour: 17,
        minute: 30,
      }),
      mkSchedule({
        label: "月結",
        body: "對帳",
        cadence: "monthly",
        dayOfMonth: 5,
        status: "disabled",
      }),
    ];
    const view = await renderOpenCard();

    for (const row of store) {
      const el = await view.findByTestId(`mp-schedmsg-row-${row.id}`);
      expect(within(el).getByText(row.label)).toBeTruthy();
      expect(within(el).getByText(row.body)).toBeTruthy();
    }
    // The when-line states the cadence in words plus the wall clock and the
    // ZONE — a time with no zone beside it is the ambiguity this feature exists
    // to remove.
    const weekly = await view.findByTestId(`mp-schedmsg-row-${store[1].id}`);
    expect(within(weekly).getByText(s.weeklyOn(s.weekdayFri))).toBeTruthy();
    expect(within(weekly).getByText("17:30")).toBeTruthy();
    expect(within(weekly).getByText("Asia/Taipei")).toBeTruthy();
    const monthly = await view.findByTestId(`mp-schedmsg-row-${store[2].id}`);
    expect(within(monthly).getByText(s.monthlyOn(5))).toBeTruthy();
    expect(within(monthly).getByText(s.disabled)).toBeTruthy();
  });

  it("shows a newly created schedule in the list without a remount", async () => {
    const view = await renderOpenCard();
    expect(await view.findByTestId("mp-schedmsg-empty")).toBeTruthy();

    await createVia(view, "早安,請看一下昨天的 CI");

    await waitFor(() => expect(createScheduledMessage).toHaveBeenCalledTimes(1));
    // The evidence is the LIST, not the create call: only a post-mutation
    // refetch can put the new row on screen without the owner reopening the
    // member.
    const created = await view.findByText("早安,請看一下昨天的 CI");
    expect(created).toBeTruthy();
    expect(view.queryByTestId("mp-schedmsg-empty")).toBeNull();
  });

  it("sends only the day field the chosen cadence reads", async () => {
    const view = await renderOpenCard();
    fireEvent.click(await view.findByTestId("mp-schedmsg-add"));
    fireEvent.change(await view.findByTestId("mp-schedmsg-body-input"), {
      target: { value: "月結對帳" },
    });
    fireEvent.change(await view.findByTestId("mp-schedmsg-cadence"), {
      target: { value: "monthly" },
    });
    fireEvent.change(await view.findByTestId("mp-schedmsg-dayofmonth"), {
      target: { value: "5" },
    });
    fireEvent.click(await view.findByTestId("mp-schedmsg-create"));

    await waitFor(() => expect(createScheduledMessage).toHaveBeenCalledTimes(1));
    const input = createScheduledMessage.mock.calls[0][1];
    expect(input.cadence).toBe("monthly");
    expect(input.dayOfMonth).toBe(5);
    expect(input.dayOfWeek).toBeUndefined();
    expect(input.timezone).toBe("Asia/Taipei");
    expect(input.hour).toBe(9);
    expect(input.minute).toBe(0);
  });

  it("keeps the default timezone editable", async () => {
    const view = await renderOpenCard();
    fireEvent.click(await view.findByTestId("mp-schedmsg-add"));
    const tz = await view.findByTestId("mp-schedmsg-timezone");
    expect((tz as HTMLInputElement).value).toBe("Asia/Taipei");
    fireEvent.change(tz, { target: { value: "Europe/Berlin" } });
    fireEvent.change(await view.findByTestId("mp-schedmsg-body-input"), {
      target: { value: "guten morgen" },
    });
    fireEvent.click(await view.findByTestId("mp-schedmsg-create"));

    await waitFor(() => expect(createScheduledMessage).toHaveBeenCalledTimes(1));
    expect(createScheduledMessage.mock.calls[0][1].timezone).toBe(
      "Europe/Berlin"
    );
  });

  it("warns that 29/30/31 skip the months that lack the day, and stays quiet on a day every month has", async () => {
    const view = await renderOpenCard();
    fireEvent.click(await view.findByTestId("mp-schedmsg-add"));
    fireEvent.change(await view.findByTestId("mp-schedmsg-cadence"), {
      target: { value: "monthly" },
    });

    fireEvent.change(await view.findByTestId("mp-schedmsg-dayofmonth"), {
      target: { value: "31" },
    });
    const hint = await view.findByTestId("mp-schedmsg-skip-hint");
    expect(hint.textContent).toBe(s.dayOfMonthSkipHint);

    // The other direction — a hint that is simply always on would pass the
    // assertion above on its own.
    fireEvent.change(await view.findByTestId("mp-schedmsg-dayofmonth"), {
      target: { value: "15" },
    });
    expect(view.queryByTestId("mp-schedmsg-skip-hint")).toBeNull();
  });

  it("deletes only after the confirm modal is confirmed", async () => {
    store = [mkSchedule({ label: "每日巡檢" })];
    const target = store[0];
    const view = await renderOpenCard();

    fireEvent.click(await view.findByTestId(`mp-schedmsg-delete-${target.id}`));
    // The click alone must not have reached the wire.
    expect(deleteScheduledMessage).not.toHaveBeenCalled();
    const modal = await view.findByTestId("mp-schedmsg-delete-confirm");
    expect(within(modal).getByText(s.deleteConfirm)).toBeTruthy();

    fireEvent.click(await view.findByTestId("mp-schedmsg-delete-confirm-ok"));
    await waitFor(() =>
      expect(deleteScheduledMessage).toHaveBeenCalledWith("mira", target.id)
    );
    await waitFor(() =>
      expect(view.queryByTestId(`mp-schedmsg-row-${target.id}`)).toBeNull()
    );
  });

  it("flips a schedule between enabled and disabled through the row toggle", async () => {
    store = [mkSchedule({ label: "每日巡檢" })];
    const target = store[0];
    const view = await renderOpenCard();

    fireEvent.click(await view.findByTestId(`mp-schedmsg-status-${target.id}`));
    await waitFor(() =>
      expect(updateScheduledMessage).toHaveBeenCalledWith("mira", target.id, {
        status: "disabled",
      })
    );
    const row = await view.findByTestId(`mp-schedmsg-row-${target.id}`);
    await waitFor(() => expect(within(row).getByText(s.disabled)).toBeTruthy());
  });

  it("shows when a schedule last delivered", async () => {
    vi.useFakeTimers({ now: NOW, toFake: ["Date"] });
    store = [
      mkSchedule({ label: "每日巡檢", lastFiredTs: FIRED_AT.getTime() / 1000 }),
    ];
    const view = await renderOpenCard();

    const line = await view.findByTestId(`mp-schedmsg-lastfired-${store[0].id}`);
    expect(line.textContent).toContain(s.lastFiredLabel);
    expect(line.textContent).toContain("8/9 21:30");
  });

  it("says a schedule has never delivered instead of printing a time", async () => {
    vi.useFakeTimers({ now: NOW, toFake: ["Date"] });
    store = [mkSchedule({ label: "剛建好的排程", lastFiredTs: 0 })];
    const view = await renderOpenCard();

    const line = await view.findByTestId(`mp-schedmsg-lastfired-${store[0].id}`);
    expect(line.textContent).toContain(s.lastFiredNever);
    // ts 0 is a real epoch, so formatting it unconditionally would print
    // 1/1 08:00 and read as a delivery that never happened.
    expect(line.textContent).not.toMatch(TIME_SHAPE);
  });

  it("edits every setting of an existing schedule and shows the result without a remount", async () => {
    store = [mkSchedule({ label: "每日巡檢", body: "看一下 CI" })];
    const target = store[0];
    const view = await renderOpenCard();

    fireEvent.click(await view.findByTestId(`mp-schedmsg-edit-${target.id}`));
    const p = `mp-schedmsg-edit-${target.id}`;
    // The editor opens on what the server currently holds — an editor that
    // opened blank would silently blank whatever the owner did not retype.
    expect(
      (view.getByTestId(`${p}-label-input`) as HTMLInputElement).value
    ).toBe("每日巡檢");
    expect(
      (view.getByTestId(`${p}-body-input`) as HTMLTextAreaElement).value
    ).toBe("看一下 CI");

    fireEvent.change(view.getByTestId(`${p}-label-input`), {
      target: { value: "週報" },
    });
    fireEvent.change(view.getByTestId(`${p}-body-input`), {
      target: { value: "整理本週進度" },
    });
    fireEvent.change(view.getByTestId(`${p}-cadence`), {
      target: { value: "weekly" },
    });
    fireEvent.change(view.getByTestId(`${p}-dayofweek`), {
      target: { value: "5" },
    });
    fireEvent.change(view.getByTestId(`${p}-hour`), { target: { value: "17" } });
    fireEvent.change(view.getByTestId(`${p}-minute`), {
      target: { value: "30" },
    });
    fireEvent.change(view.getByTestId(`${p}-timezone`), {
      target: { value: "Europe/Berlin" },
    });
    fireEvent.click(view.getByTestId(`mp-schedmsg-edit-save-${target.id}`));

    await waitFor(() => expect(updateScheduledMessage).toHaveBeenCalledTimes(1));
    expect(updateScheduledMessage.mock.calls[0][2]).toEqual({
      label: "週報",
      body: "整理本週進度",
      cadence: "weekly",
      dayOfWeek: 5,
      hour: 17,
      minute: 30,
      timezone: "Europe/Berlin",
    });
    // Same rule as the create path: the day field the cadence does not read
    // must not ride along.
    expect(updateScheduledMessage.mock.calls[0][2].dayOfMonth).toBeUndefined();

    // The evidence is the ROW, not the call: only a post-mutation refetch puts
    // the saved values on screen without the owner reopening the member.
    const row = await view.findByTestId(`mp-schedmsg-row-${target.id}`);
    expect(within(row).getByText("週報")).toBeTruthy();
    expect(within(row).getByText("整理本週進度")).toBeTruthy();
    expect(within(row).getByText(s.weeklyOn(s.weekdayFri))).toBeTruthy();
    expect(within(row).getByText("17:30")).toBeTruthy();
    expect(within(row).getByText("Europe/Berlin")).toBeTruthy();
  });

  it("throws the draft away when an edit is cancelled", async () => {
    store = [mkSchedule({ label: "每日巡檢", body: "看一下 CI" })];
    const target = store[0];
    const view = await renderOpenCard();
    const p = `mp-schedmsg-edit-${target.id}`;

    fireEvent.click(await view.findByTestId(`mp-schedmsg-edit-${target.id}`));
    fireEvent.change(view.getByTestId(`${p}-body-input`), {
      target: { value: "改到一半反悔" },
    });
    fireEvent.click(view.getByTestId(`mp-schedmsg-edit-cancel-${target.id}`));

    expect(updateScheduledMessage).not.toHaveBeenCalled();
    const row = await view.findByTestId(`mp-schedmsg-row-${target.id}`);
    expect(within(row).getByText("看一下 CI")).toBeTruthy();
    // Reopening must show the ORIGINAL, not the abandoned draft — a cancel that
    // only hid the form would hand the next edit a value nobody chose.
    fireEvent.click(view.getByTestId(`mp-schedmsg-edit-${target.id}`));
    expect(
      (view.getByTestId(`${p}-body-input`) as HTMLTextAreaElement).value
    ).toBe("看一下 CI");
  });

  it("keeps a failed save on screen as an error instead of as a saved row", async () => {
    store = [mkSchedule({ label: "每日巡檢", body: "看一下 CI" })];
    const target = store[0];
    const view = await renderOpenCard();
    const p = `mp-schedmsg-edit-${target.id}`;
    updateScheduledMessage.mockRejectedValueOnce(new Error("server said no"));

    fireEvent.click(await view.findByTestId(`mp-schedmsg-edit-${target.id}`));
    fireEvent.change(view.getByTestId(`${p}-body-input`), {
      target: { value: "會被拒絕的內容" },
    });
    fireEvent.click(view.getByTestId(`mp-schedmsg-edit-save-${target.id}`));

    expect(
      await view.findByTestId(`mp-schedmsg-edit-error-${target.id}`)
    ).toBeTruthy();
    // Still in the editor, still holding the rejected draft: closing it would
    // read as "saved", and the row underneath would then be the only thing on
    // screen — showing the OLD text with no sign the save was lost.
    expect(view.getByTestId(`mp-schedmsg-editform-${target.id}`)).toBeTruthy();
    expect(
      (view.getByTestId(`${p}-body-input`) as HTMLTextAreaElement).value
    ).toBe("會被拒絕的內容");
    expect(view.queryByTestId(`mp-schedmsg-row-${target.id}`)).toBeNull();
  });

  it("collapses a long message per row and reveals the whole text on demand", async () => {
    store = [
      mkSchedule({ label: "長的", body: LONG_BODY }),
      mkSchedule({ label: "也很長", body: LONG_BODY }),
      mkSchedule({ label: "短的", body: "看一下 CI" }),
    ];
    const [first, second, short] = store;
    const view = await renderOpenCard();

    const text = await view.findByTestId(`mp-schedmsg-text-${first.id}`);
    expect(text.className).toContain("mp-schedmsg__text--clamped");
    // Collapsed is a CLASS on the full string, never a shortened one.
    expect(text.textContent).toBe(LONG_BODY);

    fireEvent.click(view.getByTestId(`mp-schedmsg-text-toggle-${first.id}`));
    expect(
      view.getByTestId(`mp-schedmsg-text-${first.id}`).className
    ).not.toContain("mp-schedmsg__text--clamped");
    // Per ROW: the second long message is still collapsed. One shared switch
    // would have opened both.
    expect(
      view.getByTestId(`mp-schedmsg-text-${second.id}`).className
    ).toContain("mp-schedmsg__text--clamped");

    fireEvent.click(view.getByTestId(`mp-schedmsg-text-toggle-${first.id}`));
    expect(view.getByTestId(`mp-schedmsg-text-${first.id}`).className).toContain(
      "mp-schedmsg__text--clamped"
    );

    // The other direction — a message that fits is neither clamped nor offered
    // a control, otherwise the clamp class above would be unconditional.
    expect(
      view.getByTestId(`mp-schedmsg-text-${short.id}`).className
    ).not.toContain("mp-schedmsg__text--clamped");
    expect(
      view.queryByTestId(`mp-schedmsg-text-toggle-${short.id}`)
    ).toBeNull();
  });

  it("sends a collapsed message back whole when its row is edited", async () => {
    store = [mkSchedule({ label: "長的", body: LONG_BODY })];
    const target = store[0];
    const view = await renderOpenCard();

    // Left collapsed on purpose: the row shows a few lines, and the save must
    // still carry every character. "只看到前幾行" must not become "只送出前幾行".
    expect(
      (await view.findByTestId(`mp-schedmsg-text-${target.id}`)).className
    ).toContain("mp-schedmsg__text--clamped");
    fireEvent.click(view.getByTestId(`mp-schedmsg-edit-${target.id}`));
    fireEvent.click(view.getByTestId(`mp-schedmsg-edit-save-${target.id}`));

    await waitFor(() => expect(updateScheduledMessage).toHaveBeenCalledTimes(1));
    expect(updateScheduledMessage.mock.calls[0][2].body).toBe(LONG_BODY);
  });

  it("creates a custom-cadence schedule from the four sets and sends no wall-clock reading", async () => {
    const view = await renderOpenCard();
    fireEvent.click(await view.findByTestId("mp-schedmsg-add"));
    fireEvent.change(await view.findByTestId("mp-schedmsg-body-input"), {
      target: { value: "每 20 分鐘看一次佇列" },
    });
    fireEvent.change(await view.findByTestId("mp-schedmsg-cadence"), {
      target: { value: "custom" },
    });
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-months-all"));
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-days-all"));
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-hours-all"));
    // Minutes are ticked one cell at a time — there is no interval shortcut to
    // press any more, and "every 20 minutes" is what those three cells MEAN.
    for (const min of [0, 20, 40])
      fireEvent.click(view.getByTestId(`mp-schedmsg-custom-minutes-${min}`));
    fireEvent.click(view.getByTestId("mp-schedmsg-create"));

    await waitFor(() => expect(createScheduledMessage).toHaveBeenCalledTimes(1));
    const input = createScheduledMessage.mock.calls[0][1];
    expect(input.cadence).toBe("custom");
    // 🔴 Months ride EXPLICITLY. Omitting them would mean "every month" on the
    // wire too, so an implementation that never sent them would look right on
    // this fixture — the assertion is that the form STATES what it was asked.
    expect(input.customMonths).toEqual(ALL_MONTHS);
    expect(input.customDays).toEqual(ALL_DAYS);
    expect(input.customHours).toEqual(ALL_HOURS);
    expect(input.customMinutes).toEqual([0, 20, 40]);
    // custom reads none of these four; sending a reading nothing applies is the
    // required-but-ignored ambiguity the DTO removed on purpose.
    expect(input.hour).toBeUndefined();
    expect(input.minute).toBeUndefined();
    expect(input.dayOfWeek).toBeUndefined();
    expect(input.dayOfMonth).toBeUndefined();
  });

  it("blocks a custom schedule with an empty set before it reaches the wire", async () => {
    const view = await renderOpenCard();
    fireEvent.click(await view.findByTestId("mp-schedmsg-add"));
    fireEvent.change(await view.findByTestId("mp-schedmsg-body-input"), {
      target: { value: "沒有選任何時間" },
    });
    fireEvent.change(await view.findByTestId("mp-schedmsg-cadence"), {
      target: { value: "custom" },
    });

    const submit = view.getByTestId("mp-schedmsg-create") as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    expect(view.getByTestId("mp-schedmsg-custom-empty-hint").textContent).toBe(
      s.customEmptyHint
    );
    // Pressing it anyway must not reach the wire — a disabled attribute alone
    // would leave the guard to the DOM.
    fireEvent.click(submit);
    expect(createScheduledMessage).not.toHaveBeenCalled();

    // THREE of four is still empty: the block is about the INTERSECTION, so it
    // must not lift until every set has something in it. Months are checked
    // last on purpose — they are the set the wire lets a caller omit, and a
    // form that inherited that permission would let this submit early.
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-days-all"));
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-hours-all"));
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-minutes-0"));
    expect(
      (view.getByTestId("mp-schedmsg-create") as HTMLButtonElement).disabled
    ).toBe(true);

    // The other direction — a block that never lifted would satisfy everything
    // above on its own.
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-months-1"));
    expect(
      (view.getByTestId("mp-schedmsg-create") as HTMLButtonElement).disabled
    ).toBe(false);
    expect(view.queryByTestId("mp-schedmsg-custom-empty-hint")).toBeNull();
    fireEvent.click(view.getByTestId("mp-schedmsg-create"));
    await waitFor(() => expect(createScheduledMessage).toHaveBeenCalledTimes(1));
  });

  // 🔴 Round 1 put the sixty minute boxes behind a 「細部選擇」 disclosure with a
  // row of 每 5/10/15/20/30 分 shortcut buttons standing in for them, and the
  // owner read the group as "intervals only — I cannot choose WHICH minute".
  // Both are gone: the twelve cells are the control, reachable with no
  // intermediate click. (This is the DOM half; whether they are actually
  // VISIBLE and hittable is geometry, and only
  // visual-guards/scheduled-message-custom-sets.ct.spec.tsx can answer it.)
  it("offers minute 0, 5 … 55 with nothing to expand and no interval shortcut to press", async () => {
    const view = await renderOpenCard();
    fireEvent.click(await view.findByTestId("mp-schedmsg-add"));
    fireEvent.change(await view.findByTestId("mp-schedmsg-cadence"), {
      target: { value: "custom" },
    });

    const grid = view.getByTestId("mp-schedmsg-custom-minutes-grid");
    const cells = within(grid).getAllByRole("checkbox");
    expect(cells).toHaveLength(DEFAULT_MINUTES.length);
    expect(
      DEFAULT_MINUTES.map((min) =>
        Number(
          view
            .getByTestId(`mp-schedmsg-custom-minutes-${min}`)
            .closest("label")!
            .textContent!.trim()
        )
      )
    ).toEqual(DEFAULT_MINUTES);
    // The two controls that made the group read as interval-only are gone, in
    // BOTH forms — the create form here and every row editor.
    expect(
      view.queryByTestId("mp-schedmsg-custom-minutes-detail-toggle")
    ).toBeNull();
    for (const step of [5, 10, 15, 20, 30])
      expect(
        view.queryByTestId(`mp-schedmsg-custom-minutes-step-${step}`)
      ).toBeNull();

    // …and a single cell really is one pick, not an interval: ticking 20 alone
    // sends exactly [20].
    fireEvent.change(await view.findByTestId("mp-schedmsg-body-input"), {
      target: { value: "每小時的第 20 分" },
    });
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-months-all"));
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-days-all"));
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-hours-all"));
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-minutes-20"));
    fireEvent.click(view.getByTestId("mp-schedmsg-create"));
    await waitFor(() => expect(createScheduledMessage).toHaveBeenCalledTimes(1));
    expect(createScheduledMessage.mock.calls[0][1].customMinutes).toEqual([20]);
  });

  // 🔴 THE ROUND-TRIP. `custom_minutes` is still the closed set 0-59 on the
  // wire — round 2 narrowed what the GRID OFFERS, not what the wire accepts —
  // so rows sitting on minute 7 exist and must not be quietly rewritten by a
  // form that has no cell for them. The owner opens the editor to change
  // something else, or nothing at all, and saves: every value has to come back
  // byte-identical.
  it("keeps a stored minute the twelve cells do not offer and saves it back unchanged", async () => {
    const STORED_MINUTES = [7, 30];
    store = [
      mkSchedule({
        label: "第 7 分",
        cadence: "custom",
        customMonths: [2, 5],
        customDays: [1, 9],
        customHours: [8],
        customMinutes: STORED_MINUTES,
      }),
    ];
    const target = store[0];
    const view = await renderOpenCard();
    const p = `mp-schedmsg-edit-${target.id}`;
    fireEvent.click(await view.findByTestId(`mp-schedmsg-edit-${target.id}`));

    // It is ON SCREEN and ticked — an off-grid value that rendered nowhere
    // would be invisible right up to the moment it disappeared from the wire.
    const seven = view.getByTestId(`${p}-custom-minutes-7`) as HTMLInputElement;
    expect(seven.checked).toBe(true);
    // …in its sorted place among the twelve, as a THIRTEENTH cell rather than
    // in place of one of them.
    const cells = within(view.getByTestId(`${p}-custom-minutes-grid`))
      .getAllByRole("checkbox")
      .map((c) => Number(c.getAttribute("data-testid")!.split("-").pop()));
    expect(cells).toEqual([0, 5, 7, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55]);

    // Touch NOTHING, save.
    fireEvent.click(view.getByTestId(`mp-schedmsg-edit-save-${target.id}`));
    await waitFor(() => expect(updateScheduledMessage).toHaveBeenCalledTimes(1));
    const patch = updateScheduledMessage.mock.calls[0][2];
    expect(patch.customMinutes).toEqual(STORED_MINUTES);
    expect(patch.customMonths).toEqual([2, 5]);
    expect(patch.customDays).toEqual([1, 9]);
    expect(patch.customHours).toEqual([8]);
    expect(patch.cadence).toBe("custom");
    expect(patch.body).toBe(target.body);
    expect(patch.timezone).toBe(target.timezone);
  });

  it("selects and clears a whole set, and says in words what is selected", async () => {
    const view = await renderOpenCard();
    fireEvent.click(await view.findByTestId("mp-schedmsg-add"));
    fireEvent.change(await view.findByTestId("mp-schedmsg-cadence"), {
      target: { value: "custom" },
    });

    // 全選 means the set is LISTED whole — the wire has no "all" value, so an
    // implementation that stored a marker instead would show up right here.
    expect(view.getByTestId("mp-schedmsg-custom-hours-summary").textContent).toBe(
      s.customNone
    );
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-hours-all"));
    expect(view.getByTestId("mp-schedmsg-custom-hours-summary").textContent).toBe(
      m.schedCustomHours(ALL_HOURS)
    );
    for (const h of ALL_HOURS) {
      expect(
        (view.getByTestId(`mp-schedmsg-custom-hours-${h}`) as HTMLInputElement)
          .checked
      ).toBe(true);
    }

    fireEvent.click(view.getByTestId("mp-schedmsg-custom-hours-clear"));
    expect(view.getByTestId("mp-schedmsg-custom-hours-summary").textContent).toBe(
      s.customNone
    );
    expect(
      (view.getByTestId("mp-schedmsg-custom-hours-0") as HTMLInputElement).checked
    ).toBe(false);

    // The months group is the newest one and gets the same two buttons: 全選
    // lists all twelve (never an omitted-means-all shorthand), 清除 empties it
    // back to the state the submit guard refuses.
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-months-all"));
    expect(
      view.getByTestId("mp-schedmsg-custom-months-summary").textContent
    ).toBe(m.schedCustomMonths(ALL_MONTHS));
    for (const mo of ALL_MONTHS) {
      expect(
        (view.getByTestId(`mp-schedmsg-custom-months-${mo}`) as HTMLInputElement)
          .checked
      ).toBe(true);
    }
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-months-clear"));
    expect(
      view.getByTestId("mp-schedmsg-custom-months-summary").textContent
    ).toBe(s.customNone);
    expect(
      (view.getByTestId("mp-schedmsg-custom-months-1") as HTMLInputElement)
        .checked
    ).toBe(false);

    // …and 清除 hit one group, not all four.
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-days-all"));
    fireEvent.click(view.getByTestId("mp-schedmsg-custom-hours-clear"));
    expect(view.getByTestId("mp-schedmsg-custom-days-summary").textContent).toBe(
      m.schedCustomDays(ALL_DAYS)
    );
  });

  it("names each of the four sets as a group instead of leaving bare checkboxes", async () => {
    const view = await renderOpenCard();
    fireEvent.click(await view.findByTestId("mp-schedmsg-add"));
    fireEvent.change(await view.findByTestId("mp-schedmsg-cadence"), {
      target: { value: "custom" },
    });

    // Read the way a screen reader does: by accessible name, not by class. The
    // day grid is thirty-one checkboxes labelled 1…31; without a named group
    // there is nothing on the page saying they are days, and the months, hours
    // and minutes grids sound the same.
    //
    // The labels are the owner's own words (round 2) and are asserted through
    // the dictionary, so re-wording them stays a one-place edit — but the four
    // being DISTINCT is checked below, which is what a copy-paste would break.
    for (const [name, label] of [
      ["months", s.customMonthsLabel],
      ["days", s.customDaysLabel],
      ["hours", s.customHoursLabel],
      ["minutes", s.customMinutesLabel],
    ] as const) {
      const group = view.getByRole("group", { name: label });
      expect(group).toBe(view.getByTestId(`mp-schedmsg-custom-${name}`));
    }

    // …and the four names are distinct, so "named" is not one name four times.
    const named = view
      .getAllByRole("group")
      .map((g) => g.getAttribute("aria-labelledby"));
    expect(new Set(named).size).toBe(named.length);
    expect(
      new Set([
        s.customMonthsLabel,
        s.customDaysLabel,
        s.customHoursLabel,
        s.customMinutesLabel,
      ]).size
    ).toBe(4);
  });

  it("edits a custom schedule from its stored sets and shows the result without a remount", async () => {
    store = [
      mkSchedule({
        label: "佇列巡檢",
        cadence: "custom",
        customMonths: ALL_MONTHS,
        customDays: ALL_DAYS,
        customHours: ALL_HOURS,
        customMinutes: [0, 30],
      }),
    ];
    const target = store[0];
    const view = await renderOpenCard();
    const p = `mp-schedmsg-edit-${target.id}`;

    fireEvent.click(await view.findByTestId(`mp-schedmsg-edit-${target.id}`));
    // The editor opens on what the SERVER holds — an editor that opened with
    // empty sets would silently blank a schedule the owner only came to rename.
    expect(view.getByTestId(`${p}-custom-minutes-summary`).textContent).toBe(
      m.schedCustomMinutes([0, 30])
    );
    expect(view.getByTestId(`${p}-custom-months-summary`).textContent).toBe(
      m.schedCustomMonths(ALL_MONTHS)
    );
    expect(
      (view.getByTestId(`${p}-custom-minutes-30`) as HTMLInputElement).checked
    ).toBe(true);

    // Narrow the year to the quarters, and fill the hour out to every 15.
    for (const mo of [1, 2, 4, 5, 7, 8, 10, 11])
      fireEvent.click(view.getByTestId(`${p}-custom-months-${mo}`));
    for (const min of [15, 45])
      fireEvent.click(view.getByTestId(`${p}-custom-minutes-${min}`));
    fireEvent.click(view.getByTestId(`mp-schedmsg-edit-save-${target.id}`));

    await waitFor(() => expect(updateScheduledMessage).toHaveBeenCalledTimes(1));
    expect(updateScheduledMessage.mock.calls[0][2].customMinutes).toEqual([
      0, 15, 30, 45,
    ]);
    expect(updateScheduledMessage.mock.calls[0][2].customMonths).toEqual([
      3, 6, 9, 12,
    ]);
    expect(updateScheduledMessage.mock.calls[0][2].hour).toBeUndefined();

    const row = await view.findByTestId(`mp-schedmsg-row-${target.id}`);
    expect(
      within(row).getByText(
        m.schedCustomSummary([3, 6, 9, 12], ALL_DAYS, ALL_HOURS, [0, 15, 30, 45])
      )
    ).toBeTruthy();
  });

  // 🔴 The list summary used to be if-weekly / if-monthly / else-daily. Without
  // a custom branch a schedule that fires 72 times a day is DRAWN as 「每天」,
  // with nothing on the row admitting it — the silent lie this case exists for.
  // 🔴 Round 2 adds the second half: NO combination of the four sets may draw
  // as 每天 either. A quarterly schedule whose day/hour/minute sets happen to be
  // full is the shape that would slip through a branch that only looked at days.
  it("states a custom schedule's own times in the list instead of drawing it as 每天", async () => {
    store = [
      mkSchedule({ label: "真的每天", cadence: "daily" }),
      mkSchedule({
        label: "每 20 分",
        cadence: "custom",
        customMonths: ALL_MONTHS,
        customDays: ALL_DAYS,
        customHours: ALL_HOURS,
        customMinutes: [0, 20, 40],
      }),
      mkSchedule({
        label: "每季一號",
        cadence: "custom",
        customMonths: [3, 6, 9, 12],
        customDays: [1],
        customHours: [9],
        customMinutes: [0],
      }),
    ];
    const [daily, custom, quarterly] = store;
    const view = await renderOpenCard();

    const when = await view.findByTestId(`mp-schedmsg-when-${custom.id}`);
    expect(when.textContent).toContain(
      m.schedCustomSummary(ALL_MONTHS, ALL_DAYS, ALL_HOURS, [0, 20, 40])
    );
    // The daily row is the CONTROL: it proves 每天 is what a daily schedule
    // really renders, so the custom rows differing from it is a real difference
    // and not an artefact of the fixture.
    const dailyWhen = await view.findByTestId(`mp-schedmsg-when-${daily.id}`);
    expect(dailyWhen.textContent).toContain(s.cadenceDaily);
    expect(when.textContent).not.toBe(dailyWhen.textContent);

    // 🔴 The quarterly row is the one that catches a months-blind summary: it
    // fires four times a YEAR, and a summary that ignored `customMonths` would
    // print exactly what a schedule firing every single day prints.
    const quarterWhen = await view.findByTestId(
      `mp-schedmsg-when-${quarterly.id}`
    );
    expect(quarterWhen.textContent).toContain(m.schedCustomMonths([3, 6, 9, 12]));
    expect(quarterWhen.textContent).not.toBe(dailyWhen.textContent);

    // custom reads no single wall-clock reading, so the row prints none — the
    // stored 09:00 belongs to a cadence this schedule is not on.
    expect(dailyWhen.textContent).toContain("09:00");
    expect(when.textContent).not.toContain("09:00");
  });

  it("says the load failed instead of reading as honest-empty", async () => {
    const view = render(
      <I18nProvider>
        <ScheduledMessagesCard memberId="nobody" />
      </I18nProvider>
    );
    fireEvent.click(await view.findByTestId("mp-schedmsg-toggle"));
    // "nobody" has no rows AND the mock resolves, so this member must read
    // empty rather than failed — the failure branch is not a catch-all.
    expect(await view.findByTestId("mp-schedmsg-empty")).toBeTruthy();
  });
});

// 🔴 The card is ONE component rendered by BOTH wrappers. Proving it on the
// member panel proves nothing about the worker panel: they are two different
// callers of AgentDetailPanel's `extraExpandCards` slot, and the worker one had
// no caller at all until this ticket. Turn either wrapper's `extraExpandCards`
// into `notHere(...)` and exactly one of these two reddens.
// ⚠️ This used to read "Drop `extraExpandCards` from either wrapper". Since
// T-0b4f the slots are an exhaustive map, so dropping a key is a COMPILE error,
// not a silent nothing — the mutant that still reaches the runtime is declining
// the slot on purpose. That change of wording IS the ticket: the shape this
// comment was warning about ("the worker one had no caller at all") can no
// longer happen by accident.
describe("both detail panels render the 定期訊息 card", () => {
  it("renders it on the member panel", async () => {
    store = [mkSchedule({ memberId: "mira", label: "正職排程" })];
    const view = render(
      <I18nProvider>
        <MemberDetailPanel member={mkMember()} onBack={() => {}} />
      </I18nProvider>
    );
    fireEvent.click(await view.findByTestId("mp-schedmsg-toggle"));
    expect(await view.findByText("正職排程")).toBeTruthy();
  });

  it("renders it on the outsource worker panel, bound to the ow- id", async () => {
    store = [mkSchedule({ memberId: "ow-7788", label: "外包排程" })];
    const view = render(
      <I18nProvider>
        <WorkerDetailPanel worker={mkWorker()} onBack={() => {}} />
      </I18nProvider>
    );
    fireEvent.click(await view.findByTestId("mp-schedmsg-toggle"));
    // Bound to the WORKER's own id — a card wired to some member id would list
    // the wrong agent's schedules and this row would never appear.
    expect(await view.findByText("外包排程")).toBeTruthy();
  });
});

// 🔴 jsdom evaluates NO CSS, so every assertion above can only see the class
// NAME. Delete the rule the class points at and the whole suite above stays
// green while the row goes back to rendering a wall of text — the T-7526 shape
// (found by looking at a screenshot). So the rule itself is checked where it is
// cheap and exact: at the source, like styleOwnership.test.ts.
describe("member-detail.css backs the collapsed-message class", () => {
  it("clamps .mp-schedmsg__text--clamped to a few lines and hides the rest", () => {
    const css = readFileSync(join(__dirname, "member-detail.css"), "utf8");
    const start = css.indexOf(".mp-schedmsg__text--clamped");
    expect(start).toBeGreaterThan(-1);
    const rule = css.slice(start, css.indexOf("}", start));
    expect(rule).toMatch(/-webkit-line-clamp:\s*\d+/);
    // Anchored on the declaration boundary: `\b` would have matched the
    // `-webkit-` line above and made this assertion a restatement of it.
    expect(rule).toMatch(/[;{\n]\s*line-clamp:\s*\d+/);
    // Without this the clamped box still lays out every line and simply spills.
    expect(rule).toMatch(/overflow:\s*hidden/);
  });
});
