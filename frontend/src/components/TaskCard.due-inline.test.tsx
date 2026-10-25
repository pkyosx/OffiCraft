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

const chip = () => screen.getByTestId("task-due");
const dueStatus = () => screen.queryByTestId("task-due-alert");
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
});

describe("TaskCard", () => {
  it("shows the due date with its weekday in a 到期日 row right below 建立者, in the plain text colour while the date is beyond the lead", () => {
    renderCard(mkTask({ dueTs: OCT_30_END }));
    expect(metaLabels()).toEqual(["負責人", "建立者", "到期日"]);
    expect(chip().closest(".task-card__meta")).not.toBeNull();
    expect(chip().previousElementSibling).toBeNull();
    expect(chip().parentElement!.previousElementSibling!.textContent).toBe("到期日");
    expect(chip().textContent).toBe("2026/10/30（五）");
    expect(toneOf(chip())).toBeNull();
    expect(dueStatus()).toBeNull();
  });

  it("shows the date in the plain text colour for tomorrow 23:59:59 while it is still 38 hours away", () => {
    renderCard(mkTask({ dueTs: OCT_25_END }));
    expect(chip().textContent).toBe("2026/10/25（日）");
    expect(toneOf(chip())).toBeNull();
    expect(dueStatus()).toBeNull();
  });

  it.each([
    ["later today", OCT_24_END, "2026/10/24（六）"],
    ["within 24 hours on the next calendar day", OCT_25_0900, "2026/10/25（日） 09:00"],
  ])("shows the date in orange, read out as 快到期, for a due date %s", (_when, dueTs, date) => {
    renderCard(mkTask({ dueTs }));
    expect(chip().textContent).toBe(date);
    expect(toneOf(chip())).toBe("soon");
    expect(dueStatus()!.textContent).toBe("快到期");
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

  it("shows the date in red, read out as 已逾期 2 天, two calendar days after the due date", () => {
    renderCard(mkTask({ dueTs: OCT_22_END }));
    expect(chip().textContent).toBe("2026/10/22（四）");
    expect(toneOf(chip())).toBe("overdue");
    expect(dueStatus()!.textContent).toBe("已逾期 2 天");
    expect(chip().getAttribute("aria-describedby")).toBe(dueStatus()!.id);
  });

  it("reads the red date out as 已逾期 when the due date passed earlier the same day", () => {
    renderCard(mkTask({ dueTs: OCT_24_0800 }));
    expect(toneOf(chip())).toBe("overdue");
    expect(dueStatus()!.textContent).toBe("已逾期");
  });

  it("shows a muted 未設定 in the 到期日 row of an open task with no due date, which opens the picker", () => {
    renderCard(mkTask({ dueTs: null }));
    expect(chip().tagName).toBe("BUTTON");
    expect(chip().textContent).toBe("未設定");
    expect(chip().className).toContain("task-card__due-date--unset");
    expect(chip().getAttribute("title")).toBe("設定到期日");
    expect(metaLabels()).toContain("到期日");
    expect(dueStatus()).toBeNull();
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
    expect(dueStatus()!.textContent).toBe("已逾期 2 天");
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

  it("stores 23:59:59 of the same date when the time of a timed due date is emptied", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_25_0900 }));
    fireEvent.click(chip());
    fireEvent.change(timeInput(), { target: { value: "" } });
    fireEvent.blur(timeInput());
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_25_END);
  });

  it("commits the draft when the input loses focus", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: null }));
    fireEvent.click(chip());
    const input = screen.getByTestId("task-due-input");
    fireEvent.change(input, { target: { value: "2026-10-30" } });
    fireEvent.blur(input);
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_END);
  });

  it("commits the draft once on an outside click", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_25_END }));
    fireEvent.click(chip());
    const input = screen.getByTestId("task-due-input");
    fireEvent.change(input, { target: { value: "2026-10-30" } });
    fireEvent.mouseDown(document.body);
    fireEvent.blur(input);
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

  it("clears the due date with 清除", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_30_END }));
    fireEvent.click(chip());
    expect(
      (screen.getByTestId("task-due-input") as HTMLInputElement).value
    ).toBe("2026-10-30");
    expect(timeInput().value).toBe("");
    const clear = screen.getByTestId("task-due-clear");
    expect(clear.textContent).toBe("清除");
    expect(fireEvent.mouseDown(clear)).toBe(false);
    fireEvent.click(clear);
    expect(onSetDue).toHaveBeenCalledTimes(1);
    expect(onSetDue).toHaveBeenCalledWith("task-1", null);
  });

  it("shows the task's own lead in the lead field and the workspace lead in its own unit as the empty field's placeholder", () => {
    const { unmount } = renderCard(mkTask({ dueTs: OCT_30_END, dueSoonLeadMinutes: 7200 }), undefined, 4320);
    fireEvent.click(chip());
    expect(leadInput().value).toBe("5");
    expect(leadUnit().value).toBe("day");
    expect(leadInput().placeholder).toBe("全站設定 3 天");
    expect(Array.from(leadUnit().options).map((o) => o.textContent)).toEqual(["天", "小時", "分鐘"]);
    unmount();

    renderCard(mkTask({ dueTs: OCT_30_END }), undefined, 120);
    fireEvent.click(chip());
    expect(leadInput().value).toBe("");
    expect(leadUnit().value).toBe("hour");
    expect(leadInput().placeholder).toBe("全站設定 2 小時");
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
    fireEvent.blur(leadUnit());
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
    fireEvent.blur(leadInput());
    expect(onSetDue).toHaveBeenCalledTimes(1);
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_1000, 24480);
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
  });

  it("returns a task's own lead to the workspace setting when the field is emptied and Enter is pressed", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_30_END, dueSoonLeadMinutes: 7200 }));
    fireEvent.click(chip());
    fireEvent.change(leadInput(), { target: { value: "" } });
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
    fireEvent.blur(lead);
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
    fireEvent.blur(leadInput());
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

  it("shows a closed task's due date uncoloured and cannot be edited", () => {
    renderCard(
      mkTask({ status: "done", closedTs: NOW - 10, dueTs: OCT_22_END })
    );
    expect(chip().tagName).toBe("SPAN");
    expect(chip().textContent).toBe("2026/10/22（四）");
    expect(toneOf(chip())).toBeNull();
    expect(dueStatus()).toBeNull();
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

  it("turns a timed date from orange to red at that minute, not at the end of the day", () => {
    const noon = NOW + 2 * 3600;
    renderCard(mkTask({ dueTs: noon }));
    expect(chip().textContent).toBe("2026/10/24（六） 12:00");
    expect(toneOf(chip())).toBe("soon");
    act(() => {
      vi.advanceTimersByTime((2 * 3600 - 1) * 1000);
    });
    expect(toneOf(chip())).toBe("soon");
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(toneOf(chip())).toBe("overdue");
    expect(dueStatus()!.textContent).toBe("已逾期");
  });
});
