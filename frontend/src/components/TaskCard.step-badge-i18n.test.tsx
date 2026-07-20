// T-6f11 — 座艙 step 狀態徽章必須套翻譯,不得漏出原始英文 status 值。
//
// Owner bug:step 徽章顯示 raw "waiting_external",任務層徽章卻正常顯
// 「等待外部」。DOM 層釘住兩件事:
//   ① 六個 step 狀態(closed set,STEP_STATUSES)在時間軸上各渲染出 zh 翻譯
//      —— 特別是 waiting_external 顯「等待外部」,與任務層徽章同詞(兩層一致)。
//   ② 沒有任何 step 徽章的文字長得像 raw status key(^[a-z_]+$)。
// resolver 層的枚舉守衛(lib/stepBadge.i18n.test.ts)涵蓋三語與全輸入空間;
// 這檔證的是「真的 render 出來的字」,防 render site 繞過 resolver / map 的
// 那一類回歸。特殊徽章語意(gate 預告/已回覆/已過期/等待外部)由既有
// step-binding / t17be 測試連同本檔末條共同釘住。
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

describe("TaskCard step 徽章 i18n (T-6f11)", () => {
  it("六態 step 各渲染 zh 翻譯;waiting_external 與任務層同顯「等待外部」", async () => {
    __injectMockTask({
      id: "task-i18n",
      taskNo: "T-6111",
      title: "step 徽章翻譯",
      typeKey: "",
      description: "",
      status: "waiting_external",
      priority: "mid",
      executorKind: "member",
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
        mkStep({ status: s, orderIdx: i })
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
        container.querySelectorAll(".task-step-badge").length
      ).toBeGreaterThanOrEqual(STEP_STATUSES.length);
    });

    const texts = [...container.querySelectorAll(".task-step-badge")].map(
      (b) => b.textContent?.trim() ?? ""
    );
    // ② 任何 step 徽章都不得長得像 raw status key。
    for (const text of texts) {
      expect(text, `step 徽章漏出 raw key: "${text}"`).not.toMatch(RAW_KEY);
    }
    // ① 六態各自的 zh 文字都在(waiting_external 走特殊徽章 = 等待外部)。
    expect(texts).toContain(zh.tasks.stepStatus.pending); // 待辦
    expect(texts).toContain(zh.tasks.stepStatus.in_progress); // 進行中
    expect(texts).toContain(zh.tasks.stepStatus.waiting_owner); // 等我回覆
    expect(texts).toContain(zh.tasks.stepWaitingExternal); // 等待外部
    expect(texts).toContain(zh.tasks.stepStatus.done); // 完成
    expect(texts).toContain(zh.tasks.stepStatus.superseded); // 已取代

    // 兩層一致:任務層徽章與 step 層的等待外部同詞。
    const taskBadge = await findByTestId("task-status");
    expect(taskBadge.textContent).toContain(
      zh.tasks.status.waiting_external
    );
    expect(zh.tasks.status.waiting_external).toBe(
      zh.tasks.stepWaitingExternal
    );

    // 語意零回歸:waiting_external 走的是自己的特殊徽章 class,
    // 不是 plain status 徽章。
    expect(
      container.querySelector(".task-step-badge--waiting-external")
    ).not.toBeNull();
  });
});
