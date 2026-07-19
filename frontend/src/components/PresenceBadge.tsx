/**
 * Shared presence badge — the SINGLE place that renders a member's live
 * presence line. It now renders exactly TWO things: the 5-state lifecycle dot
 * (the sole VISUAL carrier of presence — its colour distinguishes
 * offline/waking/online-awake/stopping/stopped) and the member's role.
 *
 * WHY the trim (Seth 定案): presence used to be expressed THREE times on one
 * line — the dot, a status word (`t.lifecycle.status[visual]`, e.g.
 * "Online · Awake"), AND a last-seen subtitle ("Last seen X" / "Never
 * online"). All three said the same "is the member online" thing → redundant;
 * worse, an online member still read "Never online" (a matching last-seen was
 * dishonest for a live member). Now the dot's colour is the only presence
 * signal and the text is just the role — no status word, no last-seen.
 *
 * The member's name is rendered on the identity line by the caller (not here).
 */
import { useI18n } from "../i18n";
import type { Member } from "../types";
import { LifecycleDot } from "./LifecycleDot";
import type { LifecycleVisualStatus } from "./LifecycleDot";

/** Map the REAL five-state lifecycle onto the one-per-state visual union the
 * lifecycle dot consumes (`online → online-awake`; no awake/sleeping sub-axis,
 * no `error` state — honest one-per-state). Kept identical to the mapping the
 * detail panel used, now shared so it can't drift. */
export function lifecycleVisual(member: Member): LifecycleVisualStatus {
  return member.lifecycle === "online" ? "online-awake" : member.lifecycle;
}

export function PresenceBadge({ member }: { member: Member }) {
  const { t } = useI18n();
  const visual = lifecycleVisual(member);
  // i18n label for a known seed key; a CUSTOM role (M2-2, open key space)
  // falls back to its server-resolved title, then the raw key (honest, never
  // blank chrome).
  const role =
    (t.office.role as Record<string, string>)[member.role] ??
    (member.roleName || member.role);
  return (
    <span className="presence-badge">
      <LifecycleDot status={visual} />
      {/* Role only — presence itself is carried entirely by the dot's colour.
       * No status word, no last-seen (that was the triple-expressed-presence
       * redundancy + the "online yet Never online" bug). */}
      <span className="presence-badge__sub">{role}</span>
    </span>
  );
}
