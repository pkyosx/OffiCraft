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

function cardElement(task: TaskView, onSetDue = vi.fn(async () => {})) {
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
        onSetDue={onSetDue}
        onReassign={NOOP as never}
        onSendMessage={NOOP as never}
        onHydrate={(async () => task) as never}
      />
    </I18nProvider>
  );
}

function renderCard(task: TaskView, onSetDue = vi.fn(async () => {})) {
  return { ...render(cardElement(task, onSetDue)), onSetDue };
}

const chip = () => screen.getByTestId("task-due");

beforeEach(() => {
  vi.useFakeTimers({ now: NOW * 1000 });
});
afterEach(() => {
  vi.useRealTimers();
});

describe("TaskCard", () => {
  it("shows a gray month/day chip for a due date more than 24 hours away", () => {
    renderCard(mkTask({ dueTs: OCT_30_END }));
    expect(chip().textContent).toBe("10/30 到期");
    expect(chip().className).toContain("task-badge--due-later");
  });

  it("shows a date, not 明天到期, for tomorrow 23:59:59 while it is still 38 hours away", () => {
    renderCard(mkTask({ dueTs: OCT_25_END }));
    expect(chip().textContent).toBe("10/25 到期");
  });

  it("shows an orange 今天到期 for a due date later today", () => {
    renderCard(mkTask({ dueTs: OCT_24_END }));
    expect(chip().textContent).toBe("今天到期");
    expect(chip().className).toContain("task-badge--due-soon");
  });

  it("shows an orange 明天到期 for a due date within 24 hours on the next calendar day", () => {
    renderCard(mkTask({ dueTs: OCT_25_0900 }));
    expect(chip().textContent).toBe("明天到期");
    expect(chip().className).toContain("task-badge--due-soon");
  });

  it("shows a red 逾期 2 天 two calendar days after the due date", () => {
    renderCard(mkTask({ dueTs: OCT_22_END }));
    expect(chip().textContent).toBe("逾期 2 天");
    expect(chip().className).toContain("task-badge--due-overdue");
  });

  it("shows 已逾期 when the due date passed earlier the same day", () => {
    renderCard(mkTask({ dueTs: OCT_24_0800 }));
    expect(chip().textContent).toBe("已逾期");
    expect(chip().className).toContain("task-badge--due-overdue");
  });

  it("offers a faint ＋到期日 on an open task with no due date", () => {
    renderCard(mkTask({ dueTs: null }));
    expect(chip().textContent).toBe("＋到期日");
    expect(chip().className).toContain("task-badge--due-none");
  });

  it("stores 23:59:59 local of the picked date", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: null }));
    fireEvent.click(chip());
    fireEvent.change(screen.getByTestId("task-due-input"), {
      target: { value: "2026-10-30" },
    });
    expect(onSetDue).toHaveBeenCalledWith("task-1", OCT_30_END);
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
  });

  it("does not store the partial year a typed date passes through", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: null }));
    fireEvent.click(chip());
    fireEvent.change(screen.getByTestId("task-due-input"), {
      target: { value: "0002-10-30" },
    });
    expect(onSetDue).not.toHaveBeenCalled();
  });

  it("clears the due date with 清除", () => {
    const { onSetDue } = renderCard(mkTask({ dueTs: OCT_30_END }));
    fireEvent.click(chip());
    expect(
      (screen.getByTestId("task-due-input") as HTMLInputElement).value
    ).toBe("2026-10-30");
    const clear = screen.getByTestId("task-due-clear");
    expect(clear.textContent).toBe("清除");
    fireEvent.click(clear);
    expect(onSetDue).toHaveBeenCalledWith("task-1", null);
  });

  it("opening the picker does not collapse or expand the card", () => {
    const { container } = renderCard(mkTask({ dueTs: OCT_30_END }));
    const card = container.querySelector(".task-card")!;
    const before = card.getAttribute("aria-expanded");
    fireEvent.click(chip());
    fireEvent.click(screen.getByTestId("task-due-pop"));
    expect(card.getAttribute("aria-expanded")).toBe(before);
  });

  it("shows a closed task's due date as a plain gray date that cannot be edited", () => {
    renderCard(
      mkTask({ status: "done", closedTs: NOW - 10, dueTs: OCT_22_END })
    );
    expect(chip().tagName).toBe("SPAN");
    expect(chip().textContent).toBe("10/22 到期");
    expect(chip().className).not.toContain("task-badge--due-overdue");
    fireEvent.click(chip());
    expect(screen.queryByTestId("task-due-pop")).toBeNull();
  });

  it("shows no due chip on a closed task without a due date", () => {
    renderCard(mkTask({ status: "terminated", closedTs: NOW - 10 }));
    expect(screen.queryByTestId("task-due")).toBeNull();
  });

  it("turns orange the moment the due date enters the 24-hour window", () => {
    renderCard(mkTask({ dueTs: OCT_25_END }));
    expect(chip().textContent).toBe("10/25 到期");
    act(() => {
      vi.advanceTimersByTime((OCT_24_END - NOW) * 1000);
    });
    expect(chip().textContent).toBe("明天到期");
    expect(chip().className).toContain("task-badge--due-soon");
  });

  it("turns red the moment the due date passes", () => {
    const noon = NOW + 2 * 3600;
    renderCard(mkTask({ dueTs: noon }));
    expect(chip().textContent).toBe("今天到期");
    act(() => {
      vi.advanceTimersByTime(2 * 3600 * 1000);
    });
    expect(chip().textContent).toBe("已逾期");
    expect(chip().className).toContain("task-badge--due-overdue");
  });
});
