/**
 * Layer-4 handover UI skeleton — presentation + its own accessible name.
 *
 * Renders one of the lifecycle *visual* states as a colored status dot. The
 * visual union mirrors the backend's real five-state presence
 * (offline/waking/online/stopping/stopped), with `online` surfaced as
 * `online-awake` (there is no awake/sleeping activity sub-axis). This is the
 * SINGLE presence dot in the app — it is the sole visual carrier of member
 * presence, rendered everywhere via the shared PresenceBadge.
 *
 * WHY it carries `role="img"` + `aria-label` (T-7e60): the dot's COLOUR is the
 * only presence signal on the roster, and colour is invisible to a screen
 * reader. The dot used to be `aria-hidden`, which was survivable only because
 * the roster ALSO painted an "離線" text badge beside the name — the one
 * presence fact a screen reader could reach. That badge is gone (owner
 * 2026-07-17: the dot already goes grey when a member drops offline, so the
 * word was a second copy of the same fact). Removing the badge over an
 * aria-hidden dot would have deleted presence outright for screen-reader
 * users, so the dot now names itself instead: same fact, both channels.
 * `aria-hidden` is deliberately NOT set here — it would make the label
 * unreachable (an aria-hidden node is dropped from the a11y tree, label and
 * all), which is exactly the bug this replaces.
 */
import { useI18n } from "../i18n";

/** UI-only lifecycle visual states — one per real backend presence state
 * (`online` → `online-awake`). Not the `MemberStatus` data contract. */
export type LifecycleVisualStatus =
  | "offline"
  | "waking"
  | "online-awake"
  | "stopping"
  | "stopped";

export function LifecycleDot({ status }: { status: LifecycleVisualStatus }) {
  const { t } = useI18n();
  // The dict's `presence` keys are exactly this visual union, so indexing it
  // with `status` is what enforces the pair: add a sixth visual state without
  // a label and this line stops compiling; drop a label from one locale and
  // that locale stops satisfying `Dict`. The a11y channel cannot silently
  // fall behind the colour channel.
  return (
    <span
      className={`lifecycle-dot lifecycle-dot--${status}`}
      role="img"
      aria-label={t.office.presence[status]}
    />
  );
}
