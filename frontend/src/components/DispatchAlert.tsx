import { useI18n } from "../i18n";
import "./dispatch-alert.css";

/**
 * DispatchAlert (T-7fa1) — the visible end of the `*_pending` family.
 *
 * 🔴 WHY THIS EXISTS. `POST /activate` and `POST /relocate` always answer 200:
 * the owner's intent is persisted BEFORE anything is dispatched, so the request
 * genuinely cannot fail. That makes "a START went out" and "nothing was
 * dispatched and nothing will be until the next cadence tick" arrive at the UI
 * as the same success — and the wake surfaces' optimistic 「喚醒中…」 bridge
 * only ever clears when the server-driven lifecycle leaves offline/stopped.
 * A wake that was never sent therefore sat on 「喚醒中…」 FOREVER, and the
 * owner had no channel at all: the reconcile's `wake_timeout` receipt is only
 * written for a START that WAS dispatched, so the never-dispatched case did not
 * even leave a trace in 「最近操作」.
 *
 * The server has reported the difference since T-ba62 / T-8655
 * (`activation_pending` / `relocation_pending`); this component is where the
 * cockpit finally reads it.
 *
 * 🔴 WHY THE COPY IS THIS CAUTIOUS. The response is a BOOL, and it is a
 * NEGATIVE catch-all: `api_members.go` sets it whenever the reconcile did not
 * decide a START and the member is not already online — the handler's own
 * comment lists "backoff, circuit-open, and failure modes not yet invented".
 * Review r1 (BLOCKER-1) proved with two server probes that it also fires when
 *   (a) a START from a PREVIOUS click is still in flight inside the 90s window
 *       — and the wake button is deliberately still offered there
 *       (MemberActionButtons' `waking: ["cancel", "spawn"]` rescue path), and
 *   (b) the member is inside retry backoff or circuit-open AFTER a START that
 *       WAS dispatched and timed out — where `last_op_reason` already carries
 *       the correct and OPPOSITE diagnosis ("the START was dispatched but the
 *       agent never came online — check that claude runs and is logged in").
 * The first version of this copy said "nothing reached the target machine" and
 * sent the owner to check the machine registry. In case (b) that CONTRADICTS
 * 「最近操作」 on the same panel and points at the wrong fix. Replacing a silent
 * lie with a confident wrong one is worse than the silence this ticket exists
 * to remove — so the copy now claims only what the bool actually knows
 * ("nothing went out on THIS attempt; intent saved; the server retries"), gives
 * the two possible causes in PARALLEL rather than asserting one, and points
 * back at `last_op_reason`, which is more precise whenever it exists.
 *
 * Single component, two kinds: wake and relocate render the SAME shape with
 * different leading text — the two surfaces must never drift apart.
 */
export type DispatchAlertKind = "wake" | "relocate";

export function DispatchAlert({
  kind,
  testId = "dispatch-alert",
}: {
  kind: DispatchAlertKind;
  /** Overridden per surface so a test can pin WHICH surface raised it. */
  testId?: string;
}) {
  const { t } = useI18n();
  const a = t.dispatchAlert;
  return (
    // role="status" (not "alert"): the owner's own click is what produced this,
    // so it is a polite result, not an interruption — same register as the
    // onboarding banner.
    <div className="dispatch-alert" role="status" data-testid={testId}>
      <strong className="dispatch-alert__title">
        {kind === "wake" ? a.wakeTitle : a.relocateTitle}
      </strong>
      <p className="dispatch-alert__body">
        {kind === "wake" ? a.wakeBody : a.relocateBody}
      </p>
      <ul className="dispatch-alert__steps">
        <li>{a.step1}</li>
        <li>{a.step2}</li>
      </ul>
    </div>
  );
}
