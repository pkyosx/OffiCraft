import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, screen, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import type { TaskView } from "../api/adapter";

vi.mock("../api", () => ({
  api: {
    subscribeEvents: () => () => {},
    getOutsourceWorker: () => Promise.reject(new Error("not found")),
  },
}));

// 2026-10-24 10:00:00 Asia/Taipei (the suite's TZ).
const NOW = 1792807200;
const OCT_30_END = 1793375999;
const OCT_30_1000 = 1793325600;
const OCT_30_1430 = 1793341800;
const OCT_30_0900 = 1793322000;
const OCT_29_END = 1793289599;
const OCT_25_END = 1792943999;
const OCT_25_0900 = 1792890000;
const OCT_25_1541 = 1792914060;
const OCT_24_END = 1792857599;
const OCT_24_0800 = 1792800000;
const OCT_22_END = 1792684799;

function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "task-1",
    taskNo: "T-1",
    title: "到期日",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "staff",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: NOW - 3600,
    updatedTs: NOW - 60,
    closedTs: null,
    dueTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    ...over,
  };
}

const NOOP = async () => {};

function cardElement(
  task: TaskView,
  onSetDue: ReturnType<typeof vi.fn<(id: string, dueTs: number | null, leadMinutes?: number | null) => Promise<void>>> | null = vi.fn(async () => {}),
  dueSoonLeadMinutes?: number
) {
  return (
    <I18nProvider>
      <TaskCard
        task={task}
        allTasks={[task]}
        members={[]}
        workers={[]}
        nowTs={NOW}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onSetDue={onSetDue ?? undefined}
        dueSoonLeadMinutes={dueSoonLeadMinutes}
        onReassign={NOOP as never}
        onSendMessage={NOOP as never}
        onHydrate={(async () => task) as never}
      />
    </I18nProvider>
  );
}

function renderCard(
  task: TaskView,
  onSetDue: ReturnType<typeof vi.fn<(id: string, dueTs: number | null, leadMinutes?: number | null) => Promise<void>>> | null = vi.fn(async () => {}),
  dueSoonLeadMinutes?: number
) {
  return { ...render(cardElement(task, onSetDue, dueSoonLeadMinutes)), onSetDue };
}

const leadInput = () => screen.getByTestId("task-due-lead") as HTMLInputElement;
const leadUnit = () => screen.getByTestId("task-due-lead-unit") as HTMLSelectElement;
const dateInput = () => screen.getByTestId("task-due-input") as HTMLInputElement;
const timeInput = () => screen.getByTestId("task-due-time-input") as HTMLInputElement;
const datePick = () => screen.getByTestId("task-due-date-pick");
const timePick = () => screen.getByTestId("task-due-time-pick");

const chip = () => screen.getByTestId("task-due");
const dueStatus = () => screen.queryByTestId("task-due-alert");
const remaining = () => screen.queryByTestId("task-due-remaining");
const toneOf = (el: HTMLElement) =>
  el.className.match(/task-card__due-date--(\w+)/)?.[1] ?? null;
const metaLabels = () =>
  Array.from(document.querySelectorAll(".task-card__meta > .task-card__meta-label")).map(
    (el) => el.textContent
  );

beforeEach(() => {
  vi.useFakeTimers({ now: NOW * 1000 });
});
afterEach(() => {
  vi.useRealTimers();
  delete (HTMLInputElement.prototype as { showPicker?: unknown }).showPicker;
});

function mockShowPicker() {
  const showPicker = vi.fn(function (this: HTMLInputElement) {});
  Object.defineProperty(HTMLInputElement.prototype, "showPicker", {
    configurable: true,
    value: showPicker,
  });
  return showPicker;
}

describe("TaskCard", () => {
  it("shows the due date with its weekday in a 到期日 row right below 建立者, in the plain text colour while the date is beyond the lead, followed by the calendar days left", () => {
    renderCard(mkTask({ dueTs: OCT_30_END }));
    expect(metaLabels()).toEqual(["負責人", "建立者", "到期日"]);
    expect(chip().closest(".task-card__meta")).not.toBeNull();
    expect(chip().previousElementSibling).toBeNull();
    expect(chip().parentElement!.previousElementSibling!.textContent).toBe("到期日");
    expect(chip().textContent).toBe("2026/10/30（五）");
    expect(toneOf(chip())).toBeNull();
    expect(dueStatus()).toBeNull();
    expect(chip().nextElementSibling).toBe(remaining());
    expect(remaining()!.textContent).toBe("還有 6 天");
    expect(toneOf(remaining()!)).toBeNull();
    expect(chip().getAttribute("aria-describedby")).toBe(remaining()!.id);
  });

  it("shows the date in the plain text colour for tomorrow 23:59:59 while it is still 38 hours away, with 1 day left", () => {
    renderCard(mkTask({ dueTs: OCT_25_END }));
    expect(chip().textContent).toBe("2026/10/25（日）");
    expect(toneOf(chip())).toBeNull();
    expect(dueStatus()).toBeNull();
    expect(remaining()!.textContent).toBe("還有 1 天");
  });

  it.each([
    ["later today", OCT_24_END, "2026/10/24（六）", "還有 13 小時"],
    ["within 24 hours on the next calendar day", OCT_25_0900, "2026/10/25（日） 09:00", "還有 23 小時"],
  ])("shows the date and the hours left in orange, read out with 快到期, for a due date %s", (_when, dueTs, date, left) => {
    renderCard(mkTask({ dueTs }));
    expect(chip().textContent).toBe(date);
    expect(toneOf(chip())).toBe("soon");
    expect(remaining()!.textContent).toBe(left);
    expect(toneOf(remaining()!)).toBe("soon");
    expect(dueStatus()!.textContent).toBe("快到期");
    expect(chip().getAttribute("aria-describedby")).toBe(`${remaining()!.id} ${dueStatus()!.id}`);
  });

  it("turns the date orange when the task's own lead reaches a date five days out", () => {
    renderCard(mkTask({ dueTs: OCT_29_END, dueSoonLeadMinutes: 10080 }));
    expect(chip().textContent).toBe("2026/10/29（四）");
    expect(dueStatus()!.textContent).toBe("快到期");
  });

  it("judges a task without its own lead by the workspace lead", () => {
    renderCard(mkTask({ dueTs: OCT_30_END }), undefined, 10080);
    expect(dueStatus()!.textContent).toBe("快到期");
  });

  it("keeps the date uncoloured when the task's own lead is shorter than the workspace lead", () => {
    renderCard(mkTask({ dueTs: OCT_30_END, dueSoonLeadMinutes: 1440 }), undefined, 10080);
    expect(toneOf(chip())).toBeNull();
    expect(dueStatus()).toBeNull();
  });

  it("shows the date and 已逾期 2 天 in red two calendar days after the due date", () => {
    renderCard(mkTask({ dueTs: OCT_22_END }));
    expect(chip().textContent).toBe("2026/10/22（四）");
    expect(toneOf(chip())).toBe("overdue");
    expect(remaining()!.textContent).toBe("已逾期 2 天");
    expect(toneOf(remaining()!)).toBe("overdue");
    expect(dueStatus()).toBeNull();
    expect(chip().getAttribute("aria-describedby")).toBe(remaining()!.id);
  });

  it("counts the hours overdue when the due date passed earlier the same day", () => {
    renderCard(mkTask({ dueTs: OCT_24_0800 }));
    expect(toneOf(chip())).toBe("overdue");
    expect(remaining()!.textContent).toBe("已逾期 2 小時");
  });

  it("shows a muted 未設定 in the 到期日 row of an open task with no due date, which opens the picker", () => {
    renderCard(mkTask({ dueTs: null }));
    expect(chip().tagName).toBe("BUTTON");
    expect(chip().textContent).toBe("未設定");
    expect(chip().className).toContain("task-card__due-date--unset");
    expect(chip().getAttribute("title")).toBe("設定到期日");
    expect(metaLabels()).toContain("到期日");
    expect(dueStatus()).toBeNull();
    expect(remaining()).toBeNull();
    fireEvent.click(chip());
    expect(screen.getByTestId("task-due-pop")).toBeTruthy();
  });

  it("shows no 到期日 row on a task with no due date that cannot be edited", () => {
    renderCard(mkTask({ dueTs: null }), null);
    expect(screen.queryByTestId("task-due")).toBeNull();
    expect(metaLabels()).not.toContain("到期日");
  });

  it("shows an uneditable open task's date in red without a button", () => {
    renderCard(mkTask({ dueTs: OCT_22_END }), null);
    expect(chip().tagName).toBe("SPAN");
    expect(chip().textContent).toBe("2026/10/22（四）");
    expect(toneOf(chip())).toBe("overdue");
    expect(remaining()!.textContent).toBe("已逾期 2 天");
  });

  it("stores 23:59:59 local of the picked date when Enter commits it with the time left empty", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: null }));
    fireEvent.click(chip());
    const input = screen.getByTestId("task-due-input");
    expect(timeInput().value).toBe("");
    fireEvent.change(input, { target: { value: "2026-10-30" } });
    expect(onSetDue).not.toHaveBeenCalled();
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_END);
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
  });

  it("keeps the editor open when focus moves from the date to the time, and stores that local minute on Enter", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: null }));
    fireEvent.click(chip());
    fireEvent.change(dateInput(), { target: { value: "2026-10-30" } });
    fireEvent.blur(dateInput(), { relatedTarget: timeInput() });
    expect(onSetDue).not.toHaveBeenCalled();
    expect(screen.getByTestId("task-due-pop")).toBeTruthy();
    expect(timeInput().getAttribute("aria-label")).toBe("到期時間");
    fireEvent.change(timeInput(), { target: { value: "14:30" } });
    expect(onSetDue).not.toHaveBeenCalled();
    fireEvent.keyDown(timeInput(), { key: "Enter" });
    expect(onSetDue).toHaveBeenCalledTimes(1);
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_1430);
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
  });

  it("shows the draft date as 2026/10/30 and its time as 14:30 on the picker fields, and a picked date and time replace them", () => {
    renderCard(mkTask({ dueTs: OCT_30_1430 }));
    fireEvent.click(chip());
    expect(datePick().textContent).toBe("2026/10/30");
    expect(timePick().textContent).toBe("14:30");
    fireEvent.change(dateInput(), { target: { value: "2026-12-31" } });
    fireEvent.change(timeInput(), { target: { value: "15:41" } });
    expect(datePick().textContent).toBe("2026/12/31");
    expect(timePick().textContent).toBe("15:41");
  });

  it("shows 未設定 on the date field and 不指定時間 on the time field of a task without a due date", () => {
    renderCard(mkTask({ dueTs: null }));
    fireEvent.click(chip());
    expect(datePick().textContent).toBe("未設定");
    expect(timePick().textContent).toBe("不指定時間");
    expect(timePick().className).toContain("task-card__due-pick--unset");
  });

  it("opens the date input's picker when the date field is clicked and the time input's picker when the time field is clicked", () => {
    const showPicker = mockShowPicker();
    renderCard(mkTask({ dueTs: OCT_30_END }));
    fireEvent.click(chip());
    fireEvent.click(datePick());
    expect(showPicker.mock.contexts).toEqual([dateInput()]);
    fireEvent.click(timePick());
    expect(showPicker.mock.contexts).toEqual([dateInput(), timeInput()]);
  });

  it("opens the picker on Space and swallows digits, letters and arrows typed into the date and time inputs", () => {
    const showPicker = mockShowPicker();
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_30_1430 }));
    fireEvent.click(chip());
    for (const key of ["1", "ArrowUp", "p"]) {
      expect(fireEvent.keyDown(dateInput(), { key })).toBe(false);
      expect(fireEvent.keyDown(timeInput(), { key })).toBe(false);
    }
    expect(showPicker).toHaveBeenCalledTimes(0);
    expect(fireEvent.keyDown(timeInput(), { key: " " })).toBe(false);
    expect(showPicker.mock.contexts).toEqual([timeInput()]);
    expect(fireEvent.keyDown(dateInput(), { key: "Tab" })).toBe(true);
    expect(dateInput().value).toBe("2026-10-30");
    expect(timeInput().value).toBe("14:30");
    fireEvent.keyDown(dateInput(), { key: "Enter" });
    expect(onSetDue).toHaveBeenCalledTimes(0);
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
  });

  it("focuses the input instead when its picker cannot be shown", () => {
    mockShowPicker().mockImplementationOnce(() => {
      throw new DOMException("no activation", "NotAllowedError");
    });
    renderCard(mkTask({ dueTs: OCT_30_END }));
    fireEvent.click(chip());
    fireEvent.click(datePick());
    expect(document.activeElement).toBe(dateInput());
  });

  it.each(["Delete", "Backspace"])(
    "takes the time off with %s on the time field, keeps the editor open, and stores 23:59:59 of that date on Enter",
    (key) => {
      const { onSetDue } = renderCard(mkTask({ dueTs: OCT_25_0900 }));
      fireEvent.click(chip());
      fireEvent.keyDown(timeInput(), { key });
      expect(timePick().textContent).toBe("不指定時間");
      expect(dateInput().value).toBe("2026-10-25");
      expect(screen.getByTestId("task-due-pop")).toBeTruthy();
      fireEvent.keyDown(timeInput(), { key: "Enter" });
      expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_25_END);
    }
  );

  it("keeps the editor open when a phone's own picker takes focus away, and saves on the next tap outside", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_25_0900 }));
    fireEvent.click(chip());
    fireEvent.change(timeInput(), { target: { value: "15:41" } });
    fireEvent.blur(timeInput(), { relatedTarget: null });
    expect(screen.getByTestId("task-due-pop")).toBeTruthy();
    fireEvent.mouseDown(document.body);
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_25_1541);
  });

  it("stores 23:59:59 of the same date when the time of a timed due date is emptied", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_25_0900 }));
    fireEvent.click(chip());
    fireEvent.change(timeInput(), { target: { value: "" } });
    fireEvent.blur(timeInput(), { relatedTarget: document.body });
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_25_END);
  });

  it("commits the draft when the input loses focus", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: null }));
    fireEvent.click(chip());
    const input = screen.getByTestId("task-due-input");
    fireEvent.change(input, { target: { value: "2026-10-30" } });
    fireEvent.blur(input, { relatedTarget: document.body });
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_END);
  });

  it("commits the draft once on an outside click", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_25_END }));
    fireEvent.click(chip());
    const input = screen.getByTestId("task-due-input");
    fireEvent.change(input, { target: { value: "2026-10-30" } });
    fireEvent.mouseDown(document.body);
    fireEvent.blur(input, { relatedTarget: document.body });
    expect(onSetDue).toHaveBeenCalledTimes(1);
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_END);
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
  });

  it("does not save an unchanged date on an outside click", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_30_END }));
    fireEvent.click(chip());
    fireEvent.mouseDown(document.body);
    expect(onSetDue).not.toHaveBeenCalled();
  });

  it("saves the drafts with the ✓ button and discards them with the ✗ button", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_25_END }));
    fireEvent.click(chip());
    fireEvent.change(dateInput(), { target: { value: "2026-10-30" } });
    fireEvent.click(screen.getByTestId("task-due-cancel"));
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
    expect(chip().textContent).toBe("2026/10/25（日）");

    fireEvent.click(chip());
    fireEvent.change(dateInput(), { target: { value: "2026-10-30" } });
    fireEvent.change(timeInput(), { target: { value: "10:00" } });
    fireEvent.click(screen.getByTestId("task-due-apply"));
    expect(onSetDue).toHaveBeenCalledTimes(1);
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_1000);
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
  });

  it("discards the date and lead drafts on Escape", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_25_END }));
    fireEvent.click(chip());
    const input = screen.getByTestId("task-due-input");
    fireEvent.change(input, { target: { value: "2026-10-30" } });
    fireEvent.keyDown(input, { key: "Escape" });
    expect(onSetDue).not.toHaveBeenCalled();
    expect(screen.queryByTestId("task-due-pop")).toBeNull();

    fireEvent.click(chip());
    fireEvent.change(leadInput(), { target: { value: "14" } });
    fireEvent.change(leadUnit(), { target: { value: "hour" } });
    fireEvent.keyDown(leadUnit(), { key: "Escape" });
    expect(onSetDue).not.toHaveBeenCalled();
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
    expect(chip().textContent).toBe("2026/10/25（日）");

    fireEvent.click(chip());
    expect(leadInput().value).toBe("");
    expect(leadUnit().value).toBe("day");
  });

  it("does not store the partial year a typed date passes through", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: null }));
    fireEvent.click(chip());
    const input = screen.getByTestId("task-due-input");
    fireEvent.change(input, { target: { value: "0002-10-30" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onSetDue).not.toHaveBeenCalled();
  });

  it("clears the date and its time with the date's ✕, keeps the popover open, and saves the clear only on ✓", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_30_1430 }));
    fireEvent.click(chip());
    const clear = screen.getByTestId("task-due-date-clear");
    expect(clear.getAttribute("aria-label")).toBe("清除到期日");
    expect(fireEvent.mouseDown(clear)).toBe(false);
    fireEvent.click(clear);
    expect((screen.getByTestId("task-due-input") as HTMLInputElement).value).toBe("");
    expect(timeInput().value).toBe("");
    expect(screen.getByTestId("task-due-date-pick").textContent).toBe("未設定");
    expect(screen.getByTestId("task-due-pop")).toBeTruthy();
    expect(onSetDue).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("task-due-apply"));
    expect(onSetDue).toHaveBeenCalledTimes(1);
    expect(onSetDue).toHaveBeenCalledWith("task-1", null);
  });

  it("clears only the time with the time's ✕, and ✓ saves the date at the end of its day", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_30_1430 }));
    fireEvent.click(chip());
    const clear = screen.getByTestId("task-due-time-clear");
    expect(clear.getAttribute("aria-label")).toBe("清除時間");
    fireEvent.click(clear);
    expect(timeInput().value).toBe("");
    expect((screen.getByTestId("task-due-input") as HTMLInputElement).value).toBe("2026-10-30");
    expect(onSetDue).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("task-due-apply"));
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_END);
  });

  it("offers the date's ✕ as soon as a date is picked on a task with none, and ✗ after it saves nothing", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: null }));
    fireEvent.click(chip());
    expect(screen.queryByTestId("task-due-date-clear")).toBeNull();
    fireEvent.change(screen.getByTestId("task-due-input"), { target: { value: "2026-10-30" } });
    fireEvent.click(screen.getByTestId("task-due-date-clear"));
    expect(screen.getByTestId("task-due-date-pick").textContent).toBe("未設定");
    fireEvent.click(screen.getByTestId("task-due-cancel"));
    expect(onSetDue).not.toHaveBeenCalled();
  });

  it("shows the task's own lead in the lead field and the workspace lead in its own unit as the empty field's placeholder", () => {
    const { unmount } = renderCard(mkTask({ dueTs: OCT_30_END, dueSoonLeadMinutes: 7200 }), undefined, 4320);
    fireEvent.click(chip());
    expect(leadInput().value).toBe("5");
    expect(leadUnit().value).toBe("day");
    expect(leadInput().placeholder).toBe("3");
    expect(Array.from(leadUnit().options).map((o) => o.textContent)).toEqual(["天", "小時", "分鐘"]);
    unmount();

    renderCard(mkTask({ dueTs: OCT_30_END }), undefined, 120);
    fireEvent.click(chip());
    expect(leadInput().value).toBe("");
    expect(leadUnit().value).toBe("hour");
    expect(leadInput().placeholder).toBe("2");
    expect(leadUnit().className).toBe(
      "task-card__due-input task-card__due-lead-unit task-card__due-lead-unit--default"
    );
    expect(leadUnit().nextElementSibling!.textContent).toBe("前");
    const description = document.getElementById(leadInput().getAttribute("aria-describedby")!)!;
    expect(description.textContent).toBe("全站設定 2 小時");
    fireEvent.change(leadInput(), { target: { value: "5" } });
    expect(leadUnit().className).toBe("task-card__due-input task-card__due-lead-unit");
  });

  it.each([
    [1440, "1", "day"],
    [43200, "30", "day"],
    [120, "2", "hour"],
    [90, "90", "minute"],
  ])("reopens a lead of %i minutes as %s in the %s unit", (minutes, amount, unit) => {
    renderCard(mkTask({ dueTs: OCT_30_END, dueSoonLeadMinutes: minutes }));
    fireEvent.click(chip());
    expect(leadInput().value).toBe(amount);
    expect(leadUnit().value).toBe(unit);
  });

  it("keeps the editor open while the unit changes and saves 2 小時 as 120 minutes once focus leaves the editor", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_30_END }));
    fireEvent.click(chip());
    fireEvent.change(leadInput(), { target: { value: "2" } });
    fireEvent.blur(leadInput(), { relatedTarget: leadUnit() });
    fireEvent.change(leadUnit(), { target: { value: "hour" } });
    expect(onSetDue).not.toHaveBeenCalled();
    expect(screen.getByTestId("task-due-pop")).toBeTruthy();
    fireEvent.blur(leadUnit(), { relatedTarget: document.body });
    expect(onSetDue).toHaveBeenCalledTimes(1);
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_END, 120);
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
  });

  it.each([
    ["1", "day", 1440],
    ["30", "day", 43200],
    ["43200", "minute", 43200],
    ["1", "minute", 1],
  ])("saves %s in the %s unit as %i minutes on Enter", (amount, unit, minutes) => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_30_END, dueSoonLeadMinutes: 120 }));
    fireEvent.click(chip());
    fireEvent.change(leadUnit(), { target: { value: unit } });
    fireEvent.change(leadInput(), { target: { value: amount } });
    fireEvent.keyDown(leadInput(), { key: "Enter" });
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_END, minutes);
  });

  it("keeps the editor open while a lead is typed and saves it with the task's unchanged due time once focus leaves the editor", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_30_1000 }));
    expect(chip().textContent).toBe("2026/10/30（五） 10:00");
    fireEvent.click(chip());
    expect(timeInput().value).toBe("10:00");
    expect(leadInput().value).toBe("");
    fireEvent.change(leadInput(), { target: { value: "1" } });
    fireEvent.change(leadInput(), { target: { value: "17" } });
    expect(onSetDue).not.toHaveBeenCalled();
    expect(screen.getByTestId("task-due-pop")).toBeTruthy();
    fireEvent.blur(leadInput(), {
      relatedTarget: screen.getByTestId("task-due-input"),
    });
    expect(onSetDue).not.toHaveBeenCalled();
    expect(screen.getByTestId("task-due-pop")).toBeTruthy();
    fireEvent.blur(leadInput(), { relatedTarget: document.body });
    expect(onSetDue).toHaveBeenCalledTimes(1);
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_1000, 24480);
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
  });

  it("returns a task's own lead and its unit to the workspace setting when the field is emptied, and saves that on Enter", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_30_END, dueSoonLeadMinutes: 7200 }));
    fireEvent.click(chip());
    fireEvent.change(leadInput(), { target: { value: "2" } });
    fireEvent.change(leadUnit(), { target: { value: "hour" } });
    fireEvent.change(leadInput(), { target: { value: "" } });
    expect(leadUnit().value).toBe("day");
    expect(leadUnit().className).toContain("task-card__due-lead-unit--default");
    expect(leadInput().placeholder).toBe("1");
    expect(onSetDue).not.toHaveBeenCalled();
    fireEvent.keyDown(leadInput(), { key: "Enter" });
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_END, null);
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
  });

  it("saves a new date and a picked lead together in one write on an outside click", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_25_0900 }));
    fireEvent.click(chip());
    fireEvent.change(screen.getByTestId("task-due-input"), {
      target: { value: "2026-10-30" },
    });
    const lead = leadInput();
    fireEvent.change(lead, { target: { value: "14" } });
    expect(onSetDue).not.toHaveBeenCalled();
    fireEvent.mouseDown(document.body);
    fireEvent.blur(lead, { relatedTarget: document.body });
    expect(onSetDue).toHaveBeenCalledTimes(1);
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_0900, 20160);
  });

  it.each([
    ["0", "minute"],
    ["31", "day"],
    ["721", "hour"],
    ["43201", "minute"],
    ["2.5", "day"],
    ["-3", "day"],
  ])(
    "saves the new date but keeps the task's lead and shows a range error when the typed lead is %s in the %s unit",
    async (typed, unit) => {
      const { onSetDue } = renderCard(mkTask({ dueTs: OCT_25_0900, dueSoonLeadMinutes: 7200 }));
      fireEvent.click(chip());
      fireEvent.change(screen.getByTestId("task-due-input"), {
        target: { value: "2026-10-30" },
      });
      fireEvent.change(leadUnit(), { target: { value: unit } });
      fireEvent.change(leadInput(), { target: { value: typed } });
      fireEvent.keyDown(leadInput(), { key: "Enter" });
      expect(onSetDue).toHaveBeenCalledTimes(1);
      expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_0900);
      await act(async () => {});
      expect(screen.getByText("快到期提醒要是 1 分鐘到 30 天之間的整數，這次沒有變更")).toBeTruthy();
    }
  );

  it("saves nothing and shows a range error when only the lead was changed to an out-of-range value, and drops the error once the editor is reopened", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_30_END, dueSoonLeadMinutes: 7200 }));
    fireEvent.click(chip());
    fireEvent.change(leadInput(), { target: { value: "45" } });
    fireEvent.blur(leadInput(), { relatedTarget: document.body });
    expect(onSetDue).not.toHaveBeenCalled();
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
    expect(screen.getByText("快到期提醒要是 1 分鐘到 30 天之間的整數，這次沒有變更")).toBeTruthy();

    fireEvent.click(chip());
    expect(leadInput().value).toBe("5");
    expect(screen.queryByText("快到期提醒要是 1 分鐘到 30 天之間的整數，這次沒有變更")).toBeNull();
    fireEvent.mouseDown(document.body);
    expect(onSetDue).not.toHaveBeenCalled();
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
    expect(screen.queryByText("快到期提醒要是 1 分鐘到 30 天之間的整數，這次沒有變更")).toBeNull();
  });

  it("keeps the time and the lead disabled until the draft has a date", () => {
    renderCard(mkTask({ dueTs: null }));
    fireEvent.click(chip());
    expect(timeInput().disabled).toBe(true);
    expect(leadInput().disabled).toBe(true);
    expect(leadUnit().disabled).toBe(true);
    fireEvent.change(screen.getByTestId("task-due-input"), {
      target: { value: "2026-10-30" },
    });
    expect(timeInput().disabled).toBe(false);
    expect(leadInput().disabled).toBe(false);
    expect(leadUnit().disabled).toBe(false);
  });

  it("opening the picker does not collapse or expand the card", () => {
    const { container } = renderCard(mkTask({ dueTs: OCT_30_END }));
    const card = container.querySelector(".task-card")!;
    const before = card.getAttribute("aria-expanded");
    fireEvent.click(chip());
    fireEvent.click(screen.getByTestId("task-due-pop"));
    expect(card.getAttribute("aria-expanded")).toBe(before);
  });

  it("shows a closed task's due date uncoloured, without the time left, and cannot be edited", () => {
    renderCard(
      mkTask({ status: "done", closedTs: NOW - 10, dueTs: OCT_22_END })
    );
    expect(chip().tagName).toBe("SPAN");
    expect(chip().textContent).toBe("2026/10/22（四）");
    expect(toneOf(chip())).toBeNull();
    expect(dueStatus()).toBeNull();
    expect(remaining()).toBeNull();
    fireEvent.click(chip());
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
  });

  it("shows no 到期日 row on a closed task without a due date", () => {
    renderCard(mkTask({ status: "terminated", closedTs: NOW - 10 }));
    expect(screen.queryByTestId("task-due")).toBeNull();
    expect(metaLabels()).not.toContain("到期日");
  });

  it("turns the date orange the moment the due date enters the 24-hour window", () => {
    renderCard(mkTask({ dueTs: OCT_25_END }));
    expect(toneOf(chip())).toBeNull();
    expect(dueStatus()).toBeNull();
    act(() => {
      vi.advanceTimersByTime((OCT_24_END - NOW) * 1000);
    });
    expect(dueStatus()!.textContent).toBe("快到期");
    expect(chip().textContent).toBe("2026/10/25（日）");
  });

  it("turns the date orange the moment the due date enters the task's own lead window", () => {
    renderCard(mkTask({ dueTs: OCT_30_END, dueSoonLeadMinutes: 4320 }));
    expect(toneOf(chip())).toBeNull();
    expect(dueStatus()).toBeNull();
    act(() => {
      vi.advanceTimersByTime((OCT_30_END - 3 * 86400 - NOW) * 1000);
    });
    expect(dueStatus()!.textContent).toBe("快到期");
  });

  it("turns a timed date from orange to red at that minute, not at the end of the day, stepping the time left from hours to minutes on the way", () => {
    const noon = NOW + 2 * 3600;
    renderCard(mkTask({ dueTs: noon }));
    expect(chip().textContent).toBe("2026/10/24（六） 12:00");
    expect(toneOf(chip())).toBe("soon");
    expect(remaining()!.textContent).toBe("還有 2 小時");
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(remaining()!.textContent).toBe("還有 1 小時");
    act(() => {
      vi.advanceTimersByTime(3600 * 1000);
    });
    expect(remaining()!.textContent).toBe("還有 59 分鐘");
    act(() => {
      vi.advanceTimersByTime((3600 - 2) * 1000);
    });
    expect(toneOf(chip())).toBe("soon");
    expect(remaining()!.textContent).toBe("還有 1 分鐘");
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(toneOf(chip())).toBe("overdue");
    expect(remaining()!.textContent).toBe("已逾期 1 分鐘");
    act(() => {
      vi.advanceTimersByTime(3600 * 1000);
    });
    expect(remaining()!.textContent).toBe("已逾期 1 小時");
  });
});
