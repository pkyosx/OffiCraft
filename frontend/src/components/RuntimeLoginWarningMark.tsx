import { useI18n } from "../i18n";
import type { RuntimeLoginWarning } from "../types";
import { AlertTriangleIcon } from "./icons";
import { InstantHint } from "./InstantHint";
import "./runtime-login-warning.css";

/** The exclamation beside a presence dot: a machine this member runs on, or is
 * about to, reports that runtime logged out. One icon however many pairs; the
 * hover hint carries one line per distinct "<machine> 未登入 <runtime>". */
export function RuntimeLoginWarningMark({
  warnings,
}: {
  warnings: RuntimeLoginWarning[] | undefined;
}) {
  const { msg } = useI18n();
  if (!warnings || warnings.length === 0) return null;
  // A current and a pending pair on the same machine and runtime read alike;
  // show that line once.
  const hint = [
    ...new Set(warnings.map((w) => msg.runtimeLoginWarning(w.machineName, w.runtime))),
  ].join("\n");
  return (
    <InstantHint
      hint={hint}
      className="runtime-login-warning"
      data-testid="runtime-login-warning"
      role="img"
      aria-label={hint}
    >
      <AlertTriangleIcon size={14} />
    </InstantHint>
  );
}
