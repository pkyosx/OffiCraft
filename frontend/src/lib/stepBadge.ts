// stepBadge — the ONE decision source for a task step's status badge.
//
// Owner invariant: the step badge shows only the step's own stage; the bound
// reply card's answered/expired state lives on the embedded card, never here.
// A step the server still holds at waiting_owner after its card was answered or
// expired shows 進行中 — the work is back with the member. The card status is
// the server's read-time join (`reply_card_status` → TaskStepView.replyCardStatus);
// an unknown join (null) falls back to the plain step status.
//
// Every step-badge render site must go through this resolver — no divergent
// copies of the decision.

import type { TaskStepView } from "../api/adapter";

export type StepBadge =
  /** The step is parked on an EXTERNAL wait (等待外部) — its own badge, distinct
   * from the owner-facing 等我回覆; the timeline shows its waiting_reason. */
  | { kind: "waiting-external" }
  /** The plain step-status badge (t.tasks.stepStatus[status]). */
  | { kind: "status"; status: string };

/** The step-status CLOSED SET (mirrors server domain.go StepStatus* and the
 * TaskStepView doc) — the ONE enumeration the i18n guard test walks (T-6f11):
 * every value here must carry a translation in EVERY locale's
 * `tasks.stepStatus` map, so a newly added status without its translations
 * turns the suite red instead of leaking a raw key into the cockpit. */
export const STEP_STATUSES = [
  "pending",
  "in_progress",
  "waiting_owner",
  "waiting_external",
  "done",
  "superseded",
] as const;

export function resolveStepBadge(
  step: Pick<TaskStepView, "status" | "replyCardId" | "replyCardStatus">
): StepBadge {
  if (
    step.status === "waiting_owner" &&
    step.replyCardId !== "" &&
    (step.replyCardStatus === "answered" || step.replyCardStatus === "expired")
  ) {
    return { kind: "status", status: "in_progress" };
  }
  if (step.status === "waiting_external") return { kind: "waiting-external" };
  return { kind: "status", status: step.status };
}
