import type { Messages } from "../i18n/compose";
import { useI18n } from "../i18n";
import { formatClock } from "../lib/dateFormat";
import type { ModelCallFailureKind, ModelCallWarning, RuntimeLoginWarning } from "../types";
import { AlertTriangleIcon } from "./icons";
import { InstantHint } from "./InstantHint";
import "./runtime-login-warning.css";

type Reason = "login" | ModelCallFailureKind;

const REASON_ORDER: Reason[] = ["login", "auth", "other", "rate_limit", "server"];

/** Which colour the icon takes. Only `server` has a class of its own, so it can
 * be re-coloured on its own without touching the rest. */
function markTone(reasons: Set<Reason>): "danger" | "server" {
  return [...reasons].every((r) => r === "server") ? "server" : "danger";
}

function modelCallLine(w: ModelCallWarning, msg: Messages): string {
  switch (w.kind) {
    case "auth":
      return msg.modelCallAuthWarning(w.runtime);
    case "other":
      return msg.modelCallOtherWarning(w.runtime, w.code);
    case "rate_limit":
      return msg.modelCallRateLimitWarning(w.resetsAt === null ? null : formatClock(w.resetsAt));
    case "server":
      return msg.modelCallServerWarning(w.runtime);
  }
}

/** The exclamation beside a presence dot: a machine this member runs on, or is
 * about to, reports that runtime logged out, or the member's model calls are
 * failing. One icon however many reasons; the hover hint carries one line per
 * distinct reason, logged-out lines first. */
export function RuntimeLoginWarningMark({
  warnings,
  modelCallWarnings,
}: {
  warnings: RuntimeLoginWarning[] | undefined;
  modelCallWarnings: ModelCallWarning[] | undefined;
}) {
  const { msg } = useI18n();
  const entries: { reason: Reason; line: string }[] = [
    ...(warnings ?? []).map((w) => ({
      reason: "login" as const,
      line: msg.runtimeLoginWarning(w.machineName, w.runtime),
    })),
    ...(modelCallWarnings ?? []).map((w) => ({ reason: w.kind, line: modelCallLine(w, msg) })),
  ];
  if (entries.length === 0) return null;
  const sorted = [...entries].sort(
    (a, b) => REASON_ORDER.indexOf(a.reason) - REASON_ORDER.indexOf(b.reason),
  );
  // A current and a pending pair on the same machine and runtime read alike;
  // show that line once.
  const hint = [...new Set(sorted.map((e) => e.line))].join("\n");
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
