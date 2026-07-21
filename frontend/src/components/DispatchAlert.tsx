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
 * 🔴 WHY RELOCATE GETS ITS OWN STEPS, AND A CAUSE (review r2). The two flags are
 * NOT the same shape, so one set of steps cannot serve both:
 *   • `activation_pending` is the NEGATIVE catch-all above — it knows only that
 *     no START went out, never why. Its steps must stay parallel-hedged.
 *   • `relocation_pending` is `dec.DispatchUnlanded` (`api_members.go:370`),
 *     and reconcileOne sets THAT only where a decided STOP/START was refused by
 *     `enqueueToWarden` (`reconcile.go:686-691` / `:708-713`), whose only
 *     rejection is `warden == "" || !hub.IsOnline(warden)` (`reconcile.go:579`).
 *     So this half DOES know the cause: the machine that had to take the
 *     command is not connected. Hedging it into "maybe an earlier command is
 *     still retrying" told the owner LESS than the bool knows — the mirror of
 *     the r1 lie, and just as useless.
 * Deliberately NOT said: WHICH machine. A recycle STOP is addressed to the
 * machine the member is RUNNING on and the follow-up START to the pinned one
 * (`reconcile.go:702-706`), so the unreachable one can be either. Review r2
 * proved with a server probe that `relocation_pending` also fires while the
 * member is on NO machine at all (`running == ""`), which is why the copy no
 * longer says "the member is still on its old machine" — the same panel's 機器
 * cell renders 「—」 in exactly that state.
 * (Residual, accepted: `buildTargetFrame` failing would also set the flag
 * without a warden problem — but it only marshals `{rpc, {member_id}}`, which
 * cannot fail for a string field.)
 *
 * Single component, two kinds: wake and relocate render the SAME shape with
 * different leading text AND their own steps — the two surfaces must never
 * drift apart, and must never borrow each other's certainty.
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
  const wake = kind === "wake";
  return (
    // role="status" (not "alert"): the owner's own click is what produced this,
    // so it is a polite result, not an interruption — same register as the
    // onboarding banner.
    <div className="dispatch-alert" role="status" data-testid={testId}>
      <strong className="dispatch-alert__title">
        {wake ? a.wakeTitle : a.relocateTitle}
      </strong>
      <p className="dispatch-alert__body">
        {wake ? a.wakeBody : a.relocateBody}
      </p>
      <ul className="dispatch-alert__steps">
        <li>{wake ? a.wakeStep1 : a.relocateStep1}</li>
        <li>{wake ? a.wakeStep2 : a.relocateStep2}</li>
      </ul>
    </div>
  );
}
