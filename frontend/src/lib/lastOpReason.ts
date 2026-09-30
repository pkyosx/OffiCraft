import type { Dict } from "../i18n/locales/zh";

const FAMILY_MISSING =
  /^codex_model_family_unavailable: this machine's Codex \(version ([^)]+)\) lists no (\w+) model; available: (.*)$/;
const FAMILY_UNREADABLE =
  /^codex_model_family_unavailable: could not read the model list of this machine's Codex \(version ([^)]+)\) to pick the newest (\w+) model$/;

/** The warden writes last_op_reason in English; the few reasons an owner acts on
 * directly are re-worded in the viewer's language here. The code prefix is kept
 * so the line can still be looked up in the troubleshooting guide. Any reason
 * this does not recognise is returned untouched. The patterns mirror the
 * sentences in cli/ocwarden/codex_models.go. */
export function localizeLastOpReason(reason: string, mp: Dict["mp"]): string {
  const version = (raw: string) => (raw === "unknown" ? null : raw);
  const missing = FAMILY_MISSING.exec(reason);
  if (missing) {
    return `codex_model_family_unavailable: ${mp.lastOpCodexFamilyMissing(
      version(missing[1]),
      missing[2],
      missing[3],
    )}`;
  }
  const unreadable = FAMILY_UNREADABLE.exec(reason);
  if (unreadable) {
    return `codex_model_family_unavailable: ${mp.lastOpCodexFamilyUnreadable(
      version(unreadable[1]),
      unreadable[2],
    )}`;
  }
  return reason;
}
