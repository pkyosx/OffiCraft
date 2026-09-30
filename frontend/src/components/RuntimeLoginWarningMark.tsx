import { useI18n } from "../i18n";
import type { RuntimeLoginWarning } from "../types";
import { AlertTriangleIcon } from "./icons";
import "./runtime-login-warning.css";

/** The exclamation beside a presence dot: a machine this member runs on, or is
 * about to, reports that runtime logged out. One icon however many pairs; the
 * native tooltip carries one line per pair. */
export function RuntimeLoginWarningMark({
  warnings,
}: {
  warnings: RuntimeLoginWarning[] | undefined;
}) {
  const { t, msg } = useI18n();
  if (!warnings || warnings.length === 0) return null;
  const title = warnings
    .map((w) =>
      w.pending
        ? msg.runtimeLoginPending(w.machineName, w.runtime)
        : t.lifecycle.loginWarning[w.runtime],
    )
    .join("\n");
  return (
    <span
      className="runtime-login-warning"
      data-testid="runtime-login-warning"
      role="img"
      aria-label={title}
      title={title}
    >
      <AlertTriangleIcon size={14} />
    </span>
  );
}
