// T-6f11 — 座艙 step 狀態必須套翻譯,不得漏出原始英文 status 值。
//
// Owner bug:step 狀態曾顯示 raw "waiting_external",任務層卻正常顯
// 「等待外部」。DOM 層釘住兩件事:
//   ① 六個 step 狀態(closed set,STEP_STATUSES)在時間軸圖示上各帶 zh 名稱
//      —— waiting_external 是「等待外部」,與任務層同詞。
//   ② 沒有任何步驟圖示的名稱長得像 raw status key(^[a-z_]+$)。
// resolver 層的枚舉守衛(lib/stepBadge.i18n.test.ts)涵蓋全輸入空間;這檔證的
// 是「真的 render 出來的名稱」。
import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { TasksPage } from "./TasksPage";
import { __resetMock, __injectMockTask } from "../api/mock";
import { STEP_STATUSES } from "../lib/stepBadge";
import type { TaskView, TaskStepView } from "../api/adapter";

let seq = 0;
function mkStep(over: Partial<TaskStepView>): TaskStepView {
  seq += 1;
  return {
    id: `step-${seq}`,
    name: `節點-${seq}`,
    dod: "",
    status: "pending",
    isGate: false,
    replyCardId: "",
    parallelGroup: "",
    orderIdx: seq,
    startedTs: 0,
    finishedTs: 0,
    ...over,
  };
}

const RAW_KEY = /^[a-z_]+$/;

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

describe("TaskCard step 狀態 i18n (T-6f11)", () => {
  it("六態 step 各渲染 zh 翻譯;waiting_external 與任務層同顯「等待外部」", async () => {
    __injectMockTask({
      id: "task-i18n",
      taskNo: "T-6111",
      title: "step 徽章翻譯",
      typeKey: "",
      description: "",
      status: "waiting_external",
      priority: "mid",
      executorKind: "staff",
      executorId: "mira",
      creatorId: "",
      dedupeKey: "",
      deps: [],
      waitingReason: "等第三方開通",
      duplicateOf: "",
      createdTs: Date.now() / 1000 - 3600,
      updatedTs: Date.now() / 1000 - 60,
      closedTs: null,
      progressDone: 1,
      progressTotal: 6,
      steps: STEP_STATUSES.map((s, i) =>
        mkStep({ status: s, name: `節點-${s}`, orderIdx: i })
      ),
    } as TaskView);

    const { findByTestId, container } = render(
      <I18nProvider>
        <TasksPage />
      </I18nProvider>
    );
    fireEvent.click(await findByTestId("task-card"));
    await waitFor(() => {
      expect(
        container.querySelectorAll('[data-testid="step-marker"]').length
      ).toBe(STEP_STATUSES.length);
    });

    const names = [
      ...container.querySelectorAll('[data-testid="step-marker"]'),
    ].map((m) => m.getAttribute("aria-label") ?? "");
    for (const name of names) {
      expect(name, `步驟圖示漏出 raw key: "${name}"`).not.toMatch(RAW_KEY);
    }
    expect(names).toEqual([
      "待辦",
      "進行中",
      "等我回覆",
      "等待外部",
      "完成",
      "已取代",
    ]);
    expect(
      [...container.querySelectorAll('[data-testid="task-step"]')].map(
        (s) => s.querySelector(".task-step__row")!.textContent
      ),
      "the step row carries no status words"
    ).toEqual(STEP_STATUSES.map((s) => `節點-${s}`));

    const taskBadge = await findByTestId("task-status");
    expect(taskBadge.getAttribute("aria-label")).toBe(
      zh.tasks.status.waiting_external
    );
  });
});
