import type { Messages } from "../i18n/compose";
import { useI18n } from "../i18n";
import type { ModelCallFailureKind, ModelCallWarning, RuntimeLoginWarning } from "../types";
import { AlertTriangleIcon } from "./icons";
import { InstantHint } from "./InstantHint";
import "./runtime-login-warning.css";

type Reason = "login" | ModelCallFailureKind;
type Runtime = RuntimeLoginWarning["runtime"];

const REASON_ORDER: Reason[] = ["login", "auth", "other", "rate_limit", "server"];

/** Which colour the icon takes. Only `server` has a class of its own, so it can
 * be re-coloured on its own without touching the rest. */
function markTone(reasons: Set<Reason>): "danger" | "server" {
  return [...reasons].every((r) => r === "server") ? "server" : "danger";
}

function modelCallLine(w: ModelCallWarning, msg: Messages, now: number): string {
  switch (w.kind) {
    case "auth":
      return msg.modelCallAuthWarning(w.runtime);
    case "other":
      return msg.modelCallOtherWarning(w.runtime, w.code);
    case "rate_limit":
      return msg.modelCallRateLimitWarning(w.resetsAt, now);
    case "server":
      return msg.modelCallServerWarning(w.runtime);
  }
}

/** The server may send several rate_limit warnings for one member (its own
 * limit without a reset time beside an account-wide one with it); they are one
 * fact, so keep one, carrying the latest reset time any of them knows. */
function collapseRateLimits(warnings: ModelCallWarning[]): ModelCallWarning[] {
  const limits = warnings.filter((w) => w.kind === "rate_limit");
  if (limits.length <= 1) return warnings;
  const keep = limits.reduce((a, b) =>
    (b.resetsAt ?? -Infinity) > (a.resetsAt ?? -Infinity) ? b : a,
  );
  return warnings.filter((w) => w.kind !== "rate_limit" || w === keep);
}

/** The exclamation beside a presence dot: a machine this member runs on, or is
 * about to, reports that runtime logged out, or the member's model calls are
 * failing. One icon however many reasons; the hover hint carries one line per
 * distinct reason, logged-out lines first, then where to sign in again for each
 * runtime that is logged out or whose sign-in expired. */
export function RuntimeLoginWarningMark({
  warnings,
  modelCallWarnings,
}: {
  warnings: RuntimeLoginWarning[] | undefined;
  modelCallWarnings: ModelCallWarning[] | undefined;
}) {
  const { msg } = useI18n();
  const now = Date.now() / 1000;
  const entries: { reason: Reason; line: string; runtime: Runtime }[] = [
    ...(warnings ?? []).map((w) => ({
      reason: "login" as const,
      line: msg.runtimeLoginWarning(w.machineName, w.runtime),
      runtime: w.runtime,
    })),
    ...collapseRateLimits(modelCallWarnings ?? []).map((w) => ({
      reason: w.kind,
      line: modelCallLine(w, msg, now),
      runtime: w.runtime,
    })),
  ];
  if (entries.length === 0) return null;
  const sorted = [...entries].sort(
    (a, b) => REASON_ORDER.indexOf(a.reason) - REASON_ORDER.indexOf(b.reason),
  );
  // A current and a pending pair on the same machine and runtime read alike;
  // show that line once.
  const signIn = sorted
    .filter((e) => e.reason === "login" || e.reason === "auth")
    .map((e) => msg.runtimeSignInHint(e.runtime));
  const hint = [...new Set([...sorted.map((e) => e.line), ...signIn])].join("\n");
  const tone = markTone(new Set(entries.map((e) => e.reason)));
  return (
    <InstantHint
      hint={hint}
      className={`runtime-login-warning runtime-login-warning--${tone}`}
      data-testid="runtime-login-warning"
      role="img"
      aria-label={hint}
    >
      <AlertTriangleIcon size={14} />
    </InstantHint>
  );
}
