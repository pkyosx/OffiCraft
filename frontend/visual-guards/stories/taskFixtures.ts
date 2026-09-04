// Shared TaskCard fixtures for the visual guards. These mirror the shapes the
// jsdom suite (TaskCard.progress-bar.test.tsx) already pins, so the guards test
// the SAME cards — only in a real browser where layout is observable.
import type { Member } from "../../src/types";
import { api } from "../../src/api";
import { __injectMockTask } from "../../src/api/mock";
import type { MockTaskRow } from "../../src/api/mock";
import type {
  TaskView,
  TaskStepView,
  TaskStepDetailView,
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

// ── 步驟備註 ───────────────────────────────────────────────────────────────
//
// 🔴 A GUARD FIXTURE CANNOT CARRY NOTE TEXT ANY MORE, and that is why this
// exists. T-66 split the step note in two: the card payload (`TaskStepView`)
// keeps only `noteSizeChars`, and the text is fetched on click through
// `api.getTaskStep`. A fixture that still sets `note:` on a step therefore does
// NOTHING — the field is not on the type any more and the card draws the 備註
// entry from `(step.noteSizeChars ?? 0) > 0`, so no entry is drawn and every
// note guard fails on a locator that never appears.
//
// 🔴 AND TYPESCRIPT WILL NOT TELL YOU. This whole directory is outside every
// tsconfig `include` (tsconfig.json takes only `src`; tsconfig.guards.json
// takes `paint-guards` plus the two playwright configs), so `npm run typecheck`
// stays green over a stale `note:` here — that is exactly how these guards were
// left red by a change that type-checked. The only thing that checks this file
// is RUNNING it: `npx playwright test -c playwright-ct.config.ts <spec>`.
//
// So a note-carrying fixture has to say it in the two places the product now
// says it: a SIZE on the step, and TEXT behind the per-step read.
const STEP_NOTES = new Map<string, { step: TaskStepView; note: string }>();

/** A step that CARRIES A NOTE, in T-66's two halves: `noteSizeChars` on the
 * card payload (what draws the 右下角 備註 entry) and the text registered for
 * the per-step read that entry fires. Pass an explicit `id` — it is the key. */
export function mkNoteStep(
  over: Partial<TaskStepView>,
  note: string
): TaskStepView {
  const step = mkStep({ ...over, noteSizeChars: [...note].length });
  STEP_NOTES.set(step.id, { step, note });
  return step;
}

// The per-step read the note entry fires, answered from the registry above.
//
// This patches the `api` object (the SoftwareUpdateStory precedent) rather than
// landing the task in the mock store, because the mock's own `getTaskStep`
// answers `note: ""` by construction: a stored step is a `TaskStepView` and
// that type has no text field, so there is nothing for it to return. A step
// this registry does not know answers "" — the same honest empty.
api.getTaskStep = async (
  _taskId: string,
  stepId: string
): Promise<TaskStepDetailView> => {
  const hit = STEP_NOTES.get(stepId);
  const note = hit?.note ?? "";
  return {
    ...(hit?.step ?? mkStep({ id: stepId })),
    detailLevel: "full",
    note,
    noteSizeChars: [...note].length,
    noteCapChars: 4000,
  };
};

// ── 產物 ──────────────────────────────────────────────────────────────────
//
// 🔴 THE POPOVER FETCHES ITS ROWS (T-66) — it does not read them off the card,
// which since this ticket carries only an id+label index. So a story that wants
// a populated panel has to land its task in the MOCK STORE, which is what `api`
// resolves to under CT (playwright-ct.config.ts sets no VITE_USE_MOCK, so
// src/api/index.ts hands out mockApi). A fixture the store does not know makes
// `listTaskArtifacts` a 404 and the panel opens on its failure state while the
// badge still says N.
//
// Every task passed here needs its OWN id: the store is keyed by it and
// `findTask` answers the first match.
export function serveArtifacts<T extends MockTaskRow>(task: T): T {
  __injectMockTask(task);
  return task;
}

export function mkTask(over: Partial<MockTaskRow>): MockTaskRow {
  return {
    id: "t-ad215291a016",
    taskNo: "t-ad215291a016",
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
  id: "t-5b715291a017",
  taskNo: "t-5b715291a017",
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
export const WITH_ARTIFACTS: MockTaskRow = serveArtifacts(mkTask({
  id: "t-3dc55291a020",
  taskNo: "t-3dc55291a020",
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
}));

export const NO_ARTIFACTS: MockTaskRow = serveArtifacts(
  mkTask({
    id: "t-3dc55291a021",
    taskNo: "t-3dc55291a021",
    artifactCount: 0,
    artifacts: [],
  })
);

// T-90df ragged-row fixture (owner 2026-07-20): the reported bug was that the
// 檔案 tab's chips sized to their filenames, so a short name and a long one
// produced different-width chips and the trailing 預覽/移除 buttons came out
// ragged. This set pairs a SHORT and an OVERLONG filename in the same tab —
// the only shape that can prove equal chip widths and a single action column
// in real layout (jsdom computes none). One image + one link ride along so the
// cross-tab shape can be checked too.
export const RAGGED_ARTIFACTS: MockTaskRow = serveArtifacts(mkTask({
  id: "t-90df5291a022",
  taskNo: "t-90df5291a022",
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
}));

// T-6338 (owner 2026-07-20 report): two artifacts pinned under the IDENTICAL
// filename — the exact shape from the owner's screenshot (`DEMO-CUST_demo.mp4`
// twice, each row with its own delete button, nothing to tell them apart).
// `createdTs` is deliberately identical too — the worst case where the
// minute-resolution timestamp alone would ALSO print identically, which is
// exactly the failure mode the ticket calls out ("兩個時間剛好相近或格式讓人
// 看不出差異"). Only `id` differs, which is what the per-row ref tag must
// fall back on to keep the two rows provably distinct.
export const SAME_NAME_ARTIFACTS: MockTaskRow = serveArtifacts(mkTask({
  id: "t-63385291a023",
  taskNo: "t-63385291a023",
  artifactCount: 2,
  artifacts: [
    {
      id: "ta-demo1a2b3c",
      kind: "file",
      url: "/api/chat/attachment/ta-demo1a2b3c",
      label: "",
      filename: "DEMO-CUST_demo.mp4",
      mime: "video/mp4",
      isImage: false,
      attachmentId: "ta-demo1a2b3c",
      createdTs: 1784550000,
      createdBy: "mira",
    },
    {
      id: "ta-demo4d5e6f",
      kind: "file",
      url: "/api/chat/attachment/ta-demo4d5e6f",
      label: "",
      filename: "DEMO-CUST_demo.mp4",
      mime: "video/mp4",
      isImage: false,
      attachmentId: "ta-demo4d5e6f",
      createdTs: 1784550000,
      createdBy: "mira",
    },
  ],
}));

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
