// T-13af — the task card's description and step DoD are owner/agent-authored
// free text that may carry markdown (headings/bold/lists/code/links); both
// must render through the shared, XSS-safe `Markdown` component (never raw
// text, never dangerouslySetInnerHTML).

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import type { Member } from "../types";
import type { TaskView, TaskStepView, OutsourceWorkerView } from "../api/adapter";

let seq = 0;
function mkStep(over: Partial<TaskStepView>): TaskStepView {
  seq += 1;
  return {
    id: `step-${seq}`, name: `節點-${seq}`, dod: "", status: "pending",
    isGate: false, replyCardId: "", parallelGroup: "", orderIdx: seq,
    startedTs: 0, finishedTs: 0, ...over,
  };
}
function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "T-13af", taskNo: "T-13af", title: "markdown render 任務", typeKey: "",
    description: "", status: "in_progress", priority: "high",
    executorKind: "member", executorId: "mira", creatorId: "", dedupeKey: "",
    deps: [], waitingReason: "", duplicateOf: "", createdTs: 1000, updatedTs: 2000,
    closedTs: null, progressDone: 0, progressTotal: 1, steps: [], ...over,
  };
}

const MIRA = { id: "mira", name: "Mira", kind: "agent" } as unknown as Member;
const noop = async () => {};
const workers: OutsourceWorkerView[] = [];

function renderCard(task: TaskView, onHydrate: (id: string) => Promise<TaskView>) {
  return render(
    <I18nProvider>
      <TaskCard
        task={task} allTasks={[task]} members={[MIRA]} workers={workers} nowTs={3000}
        onTerminate={noop as never} onMarkDuplicate={noop as never} onSetPriority={noop as never}
        onReassign={noop as never}
        onSendMessage={noop as never} onHydrate={onHydrate}
      />
    </I18nProvider>
  );
}

describe("TaskCard markdown render (T-13af)", () => {
  it("renders the description's markdown (heading/bold/list/code/link) as elements, not literal syntax", async () => {
    const description = [
      "## 說明",
      "**重點**是 `foo()`:",
      "- 一",
      "- 二",
      "見 [文件](https://example.com/doc)",
    ].join("\n");
    const detail = mkTask({ description });
    const onHydrate = vi.fn(async () => detail);
    const { container, findByTestId } = renderCard(mkTask({}), onHydrate);

    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});

    const desc = container.querySelector(".task-card__desc")!;
    expect(desc.querySelector("h2")?.textContent).toBe("說明");
    expect(desc.querySelector("strong")?.textContent).toBe("重點");
    expect(desc.querySelector("code")?.textContent).toBe("foo()");
    expect(desc.querySelectorAll("ul > li").length).toBe(2);
    const link = desc.querySelector("a");
    expect(link?.getAttribute("href")).toBe("https://example.com/doc");
    expect(link?.getAttribute("target")).toBe("_blank");
    // never falls back to raw markdown syntax
    expect(desc.textContent).not.toContain("**重點**");
    expect(desc.textContent).not.toContain("##");
  });

  it("sanitizes a malicious description — no raw HTML injected, script text stays literal", async () => {
    const detail = mkTask({
      description: "<img src=x onerror=alert(1)> and [bad](javascript:alert(1))",
    });
    const onHydrate = vi.fn(async () => detail);
    const { container, findByTestId } = renderCard(mkTask({}), onHydrate);

    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});

    const desc = container.querySelector(".task-card__desc")!;
    expect(desc.querySelector("img")).toBeNull();
    expect(desc.querySelector("a")).toBeNull();
    expect(desc.textContent).toContain("<img src=x onerror=alert(1)>");
    expect(desc.textContent).toContain("[bad](javascript:alert(1))");
  });

  it("renders a step's DoD markdown (list) under the DoD label", async () => {
    const step = mkStep({ dod: "- 條件一\n- 條件二", status: "in_progress" });
    const detail = mkTask({ steps: [step], progressDone: 0, progressTotal: 1 });
    const onHydrate = vi.fn(async () => detail);
    const { container, findByTestId } = renderCard(mkTask({}), onHydrate);

    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});

    const dod = container.querySelector(".task-step__dod")!;
    expect(dod.querySelectorAll("ul > li").length).toBe(2);
    expect(dod.textContent).not.toContain("- 條件一");
  });
});
