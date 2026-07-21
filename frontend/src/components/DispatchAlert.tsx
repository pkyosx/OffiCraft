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
 * 🔴 WHY IT LISTS STEPS INSTEAD OF JUST SAYING "failed". The response is a
 * BOOL — the server does not tell us WHICH of the several undeliverable
 * conditions applies (machine offline, warden not installed, warden's SSE down,
 * an unbuildable start frame). Naming a cause we do not have would be a
 * fabrication, and「喚醒失敗」alone leaves the owner exactly as stuck as the
 * silence did. So it states what is true (nothing was sent, the intent is
 * saved, the server retries) and gives the two checks that actually resolve
 * every known cause, in the order they pay off.
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
