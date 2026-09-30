import type { Dict } from "../i18n/locales/zh";

const FAMILY_MISSING =
  /^codex_model_family_unavailable: this machine's Codex \(version ([^)]+)\) lists no (\w+) model; available: (.*)$/;
const FAMILY_UNREADABLE =
  /^codex_model_family_unavailable: could not read the model list of this machine's Codex \(version ([^)]+)\) to pick the newest (\w+) model$/;
const FAMILY_OLD_WARDEN =
  /^machine_unavailable: machine '([^']+)' runs a warden too old to resolve the Codex model family '(\w+)' — upgrade that machine's warden, or set a full model id(; no other machine is substituted)?$/;

const NOT_LOGGED_IN =
  /^(?:machine_unavailable|claude_not_logged_in|codex_not_logged_in): machine '([^']+)' is not logged in to (claude|codex)(?:; no other machine is substituted)?$/;
const RUNTIME_NAME = { claude: "Claude", codex: "Codex" } as const;

/** The owner-ruled line for a wake refused because the machine is not logged in
 * — the machine's display name and the runtime, with no code prefix and nothing
 * else; null for any other reason. The pattern mirrors the sentences in
 * server/ocserverd/api_machines.go and api_monitoring.go. */
export function notLoggedInLine(
  reason: string,
  mp: Dict["mp"],
  machineName: (id: string) => string = (id) => id,
): string | null {
  const m = NOT_LOGGED_IN.exec(reason);
  if (!m) return null;
  return mp.lastOpNotLoggedIn(
    machineName(m[1]),
    RUNTIME_NAME[m[2] as keyof typeof RUNTIME_NAME],
  );
}

/** The warden and server write last_op_reason in English; the few reasons an owner acts on
 * directly are re-worded in the viewer's language here. The code prefix is kept
 * so the line can still be looked up in the troubleshooting guide, except on the
 * not-logged-in line, whose wording is the owner's. The patterns mirror the
 * sentences in cli/ocwarden/codex_models.go and server/ocserverd/api_machines.go;
 * nothing checks the two sides agree. */
export function localizeLastOpReason(
  reason: string,
  mp: Dict["mp"],
  machineName?: (id: string) => string,
): string {
  const notLoggedIn = notLoggedInLine(reason, mp, machineName);
  if (notLoggedIn !== null) return notLoggedIn;
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
  const oldWarden = FAMILY_OLD_WARDEN.exec(reason);
  if (oldWarden) {
    return `machine_unavailable: ${mp.lastOpCodexFamilyOldWarden(
      oldWarden[1],
      oldWarden[2],
      oldWarden[3] !== undefined,
    )}`;
  }
  return reason;
}
