// stepBadge — the ONE decision source for the status a task step is drawn with
// (its timeline icon, the icon's name and the step box's tint).
//
// Owner invariant: a step shows only its own stage; the bound reply card's
// answered/expired state lives on the embedded card, never here. A step the
// server still holds at waiting_owner after its card was answered or expired
// shows 進行中 — the work is back with the member. The card status is the
// server's read-time join (`reply_card_status` → TaskStepView.replyCardStatus);
// an unknown join (null) falls back to the plain step status.
//
// Every render site must go through this resolver — no divergent copies of the
// decision.

import type { TaskStepView } from "../api/adapter";

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
): string {
  if (
    step.status === "waiting_owner" &&
    step.replyCardId !== "" &&
    (step.replyCardStatus === "answered" || step.replyCardStatus === "expired")
  ) {
    return "in_progress";
  }
  return step.status;
}
