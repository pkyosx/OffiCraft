// stepBadge — the ONE decision source for a task step's status badge (T-d64f).
//
// Owner invariant: the step-level 等我回覆 badge must agree with the Ask side —
// it shows ONLY while the bound reply card is really WAITING for the owner.
// A step the server still holds at waiting_owner whose card was already
// answered shows the answered badge (已回覆 — the owner replied; deliberately
// NO pickup promise in the copy, since legacy data / a failed hold-release can
// leave this state standing with no pickup ever coming — same semantic-death
// call as T-2b14); an EXPIRED card (terminal, T-1aa4) shows 已過期. The card status is the server's read-time join
// (`reply_card_status` → TaskStepView.replyCardStatus); an unknown join (null —
// legacy data / hand-built fixture) falls back to the plain step status, as it
// did before the join existed.
//
// Every step-badge render site must go through this resolver — no divergent
// copies of the decision.

import type { TaskStepView } from "../api/adapter";

export type StepBadge =
  /** Announced gate — dashed 等我回覆 preview (no card armed yet). */
  | { kind: "gate-announced" }
  /** Bound card answered while the step still waits for pickup. */
  | { kind: "card-answered" }
  /** Bound card expired (terminal) while the step still shows waiting. */
  | { kind: "card-expired" }
  /** The step is parked on an EXTERNAL wait (等待外部) — its own badge, distinct
   * from the owner-facing 等我回覆; the timeline shows its waiting_reason. */
  | { kind: "waiting-external" }
  /** The plain step-status badge (t.tasks.stepStatus[status]). */
  | { kind: "status"; status: string };

/** The step's terminal statuses (done / superseded — T-1aea): nothing waits
 * on these any more, so the announced-gate preview must not render. */
const TERMINAL_STEP = new Set(["done", "superseded"]);

export function resolveStepBadge(
  step: Pick<
    TaskStepView,
    "status" | "isGate" | "replyCardId" | "replyCardStatus"
  >
): StepBadge {
  if (step.isGate && step.replyCardId === "" && !TERMINAL_STEP.has(step.status)) {
    // ANNOUNCED gate — dashed 等我回覆 preview (spec: 提前看到哪個 step 之後
    // 會需要你回覆), regardless of the step's pending/running status. A
    // TERMINAL gate drops the preview (nothing waits any more) — done keeps
    // its permanent 審批 marker instead, and a superseded gate is frozen
    // replan history that will never arm (T-1aea).
    return { kind: "gate-announced" };
  }
  if (step.status === "waiting_owner" && step.replyCardId !== "") {
    if (step.replyCardStatus === "answered") return { kind: "card-answered" };
    if (step.replyCardStatus === "expired") return { kind: "card-expired" };
  }
  // A step waiting on an EXTERNAL dependency (T-9ca5): its own 等待外部 badge,
  // distinct from the owner-facing waiting_owner/card badges above.
  if (step.status === "waiting_external") return { kind: "waiting-external" };
  // superseded deliberately falls through to the plain status badge
  // (t.tasks.stepStatus.superseded — 已取代): the frozen step's own Q&A stays
  // visible through the embedded card, whose answered/expired branches above
  // only fire for waiting_owner and thus never mask it.
  return { kind: "status", status: step.status };
}
