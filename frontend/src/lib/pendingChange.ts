// pendingChange — the ONE rule behind every "changed, not applied yet" hint on
// the member and outsource detail panels (T-7f28).
//
// The panels show four configurable cells (執行環境 / 模型 / 思考強度 / 機器).
// Each has an owner-CONFIGURED value and a REPORTED one, and almost no launch
// change takes effect immediately: a live agent gets a wind-down window first,
// an offline one applies it at the next wake. So "the configured value differs
// from the running one" is the NORMAL state for a while, and the panel has to
// say so rather than letting the two blur into one another.
//
// Both wrappers route through here so the rule cannot drift between the staff
// panel and the outsource panel — they had drifted before (the staff panel had
// a transition hint on one cell, the outsource panel had none at all).

/** Decide one cell's pending hint.
 *
 * Returns "" — meaning render NOTHING — in two cases, and the second is the one
 * that matters:
 *
 *  1. configured === reported: nothing is pending.
 *  2. reported is UNKNOWN (""): nothing has ever reported this value, so there
 *     is no evidence a change is outstanding. Claiming one would be a guess,
 *     and the cell already reads "—" (unknown) beside it. This is the honest
 *     half of the ticket's red line: a missing report must never be dressed up
 *     as either state.
 *
 * `label` receives the value being changed TO — the configured one — already
 * resolved to whatever the cell displays (a machine's display name, not its id).
 */
export function pendingChangeHint(
  configured: string,
  reported: string,
  label: (configuredDisplay: string) => string,
  configuredDisplay: string = configured,
): string {
  if (!reported) return "";
  if (!configured) return "";
  if (configured === reported) return "";
  return label(configuredDisplay);
}

/** The machine cell's twist: its reported value has two sources.
 *
 * `observed` is where the agent is running RIGHT NOW and goes blank the moment
 * it stops; `lastObserved` is the durable last landing, which survives. Using
 * only the live one would make a pending relocation vanish the instant the
 * member went offline — which is exactly when the owner most wants to see that
 * the move has not happened yet.
 */
export function reportedMachine(observed: string, lastObserved: string): string {
  return observed || lastObserved;
}
