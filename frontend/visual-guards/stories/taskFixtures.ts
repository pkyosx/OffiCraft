// Shared TaskCard fixtures for the visual guards. These mirror the shapes the
// jsdom suite (TaskCard.progress-bar.test.tsx) already pins, so the guards test
// the SAME cards — only in a real browser where layout is observable.
import type { Member } from "../../src/types";
import type {
  TaskView,
  TaskStepView,
  OutsourceWorkerView,
} from "../../src/api/adapter";

let seq = 0;
export function mkStep(over: Partial<TaskStepView>): TaskStepView {
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

export function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "t-ad21",
    taskNo: "T-ad21",
    title: "進度條任務",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "high",
    executorKind: "member",
    executorId: "mira",
    creatorId: "owner",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: 1000,
    updatedTs: 2000,
    closedTs: null,
    progressDone: 0,
    progressTotal: 1,
    steps: [],
    ...over,
  };
}

// owner's exact shape: re-planned to 5 nodes reporting 2/5, 3 superseded nodes
// excluded from the server counts (T-1aea). The bar must fill 40%.
export const REPLANNED_2_OF_5: TaskView = mkTask({
  progressDone: 2,
  progressTotal: 5,
  steps: [
    mkStep({ status: "done" }),
    mkStep({ status: "done" }),
    mkStep({ status: "in_progress" }),
    mkStep({ status: "pending" }),
    mkStep({ status: "pending" }),
    mkStep({ status: "superseded" }),
    mkStep({ status: "superseded" }),
    mkStep({ status: "superseded" }),
  ],
});

// (T-c514 removed WAITING_SHORT here — the task-level always-stack fixture. Its
// consumer, card-reflow.ct.spec.tsx, went with the task-level waiting block it
// measured. The always-stack argument it carried lives on in
// STEP_WAITING_EXTERNAL below, on the row that still renders.)

// T-9ca5 轉派中 LOCK overlay: a reassigned task keeps its honest derived status
// (in_progress) AND carries lock="reassigning". The guard proves the lock badge
// renders BESIDE the status badge (orthogonal), within the card at every width.
export const LOCKED_REASSIGNING: TaskView = mkTask({
  status: "in_progress",
  lock: "reassigning",
  priority: "high",
});

// T-9ca5 step-level 等待外部: an expanded card whose live step is waiting_external
// with a SHORT 3-char reason — it trivially FITS beside the 等待中 label, so if
// it lands on its own line that can only be because flex-basis:100% put it
// there. Since T-c514 this is the ONLY fixture carrying that always-stack
// argument (the task-level WAITING_SHORT was removed with its block).
export const STEP_WAITING_EXTERNAL: TaskView = mkTask({
  id: "t-swx1",
  taskNo: "T-swx1",
  title: "步驟等待外部任務",
  status: "waiting_external",
  waitingReason: "等外部",
  progressDone: 1,
  progressTotal: 2,
  steps: [
    mkStep({ status: "done" }),
    mkStep({
      status: "waiting_external",
      waitingReason: "等外部",
    }),
  ],
});

export const MIRA = { id: "mira", name: "Mira", kind: "agent" } as unknown as Member;
export const NOOP = async () => {};
export const WORKERS: OutsourceWorkerView[] = [];

// T-3dc5 artifact-set fixtures. WITH_ARTIFACTS carries all three kinds so the
// popover's 檔案/圖片/連結 tabs each have a row; NO_ARTIFACTS asserts the
// empty-set case (count 0 ⇒ the badge must NOT render at all).
export const WITH_ARTIFACTS: TaskView = mkTask({
  artifactCount: 3,
  artifacts: [
    {
      id: "ta-file",
      kind: "file",
      url: "/api/chat/attachment/ta-file",
      label: "",
      filename: "design.md",
      mime: "text/markdown",
      isImage: false,
      attachmentId: "ta-file",
      createdTs: 0,
      createdBy: "mira",
    },
    {
      id: "ta-img",
      kind: "image",
      url: "/api/chat/attachment/ta-img",
      label: "",
      filename: "shot.png",
      mime: "image/png",
      isImage: true,
      attachmentId: "ta-img",
      createdTs: 0,
      createdBy: "mira",
    },
    {
      id: "ta-link",
      kind: "link",
      url: "https://github.com/x/y/pull/123",
      label: "PR #123",
      filename: "",
      mime: "",
      isImage: false,
      attachmentId: "",
      createdTs: 0,
      createdBy: "mira",
    },
  ],
});

export const NO_ARTIFACTS: TaskView = mkTask({ artifactCount: 0, artifacts: [] });

// T-90df ragged-row fixture (owner 2026-07-20): the reported bug was that the
// 檔案 tab's chips sized to their filenames, so a short name and a long one
// produced different-width chips and the trailing 預覽/移除 buttons came out
// ragged. This set pairs a SHORT and an OVERLONG filename in the same tab —
// the only shape that can prove equal chip widths and a single action column
// in real layout (jsdom computes none). One image + one link ride along so the
// cross-tab shape can be checked too.
export const RAGGED_ARTIFACTS: TaskView = mkTask({
  artifactCount: 4,
  artifacts: [
    {
      id: "ta-short",
      kind: "file",
      url: "/api/chat/attachment/ta-short",
      label: "",
      filename: "a.md",
      mime: "text/markdown",
      isImage: false,
      attachmentId: "ta-short",
      createdTs: 0,
      createdBy: "mira",
    },
    {
      id: "ta-long",
      kind: "file",
      url: "/api/chat/attachment/ta-long",
      label: "",
      filename:
        "2026-07-20-座艙產物彈窗列表對齊-超長檔名回歸測試用-really-long-artifact-filename.md",
      mime: "text/markdown",
      isImage: false,
      attachmentId: "ta-long",
      createdTs: 0,
      createdBy: "mira",
    },
    {
      id: "ta-img-long",
      kind: "image",
      url: "/api/chat/attachment/ta-img-long",
      label: "",
      filename: "一張檔名也很長的截圖-artifacts-popover-alignment-before.png",
      mime: "image/png",
      isImage: true,
      attachmentId: "ta-img-long",
      createdTs: 0,
      createdBy: "mira",
    },
    {
      id: "ta-link-long",
      kind: "link",
      url: "https://github.com/hardcoretech/officraft/pull/12345",
      label: "PR #12345 — 一個標籤也很長的連結產物用來驗證截斷與對齊",
      filename: "",
      mime: "",
      isImage: false,
      attachmentId: "",
      createdTs: 0,
      createdBy: "mira",
    },
  ],
});

// The 負責人 + 轉派 icon stress fixture (owner 2026-07-18): a live card whose
// assignee display name is long enough to force the chip to ellipse at 390px.
// The 轉派 icon shares the 負責人 value cell — the guard proves a long name
// ellipses (chip shrinks) rather than shoving the icon off-row or off-card (the
// flex:1 trap the CSS comment warns against). A member so the chip is the
// reachable link variant (task-assignee-link).
export const LONG_MEMBER = {
  id: "long-assignee",
  name: "非常長的負責人顯示名稱用來逼出省略號與換行壓力測試ABCDEFG",
  kind: "agent",
} as unknown as Member;
export const LONG_ASSIGNEE: TaskView = mkTask({
  executorKind: "member",
  executorId: "long-assignee",
});
