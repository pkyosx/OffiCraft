// lib/runtime.ts — client-side join of live session telemetry onto a roster
// member.
//
// A member DTO carries NO runtime telemetry: `contextPct` / `estimatedCost` are
// honest-null (see mappers.toMember). The real source is the monitoring session
// — the SAME feed the Monitor page's "AI 會話" rows read (GET /api/monitoring →
// MonitoringDTO.sessions). The member-detail panel opened from either page must
// show the SAME value the monitor row shows, so we join from that ONE source
// (never a second, divergent one), matched by the stable member id.
//
// Honest: if there is no matching session (or its field is null/empty) we pass
// the member's own value through unchanged — we never fabricate a number.
// `machine` / `account` are joined the same way (the detail panel's runtime
// header reads both): an empty "" session field falls back to the member's own.
// `bankedCost` (persistent cumulative cost) is joined the same way as the live
// `estimatedCost` — via `??`, since 0 is a valid banked value.

import type { Member, MonSessionView } from "../types";

export function joinSessionRuntime(
  member: Member,
  sessions: MonSessionView[]
): Member {
  const s = sessions.find((x) => x.id === member.id);
  if (!s) return member;
  return {
    ...member,
    machine: s.machine || member.machine,
    account: s.account || member.account,
    contextPct: s.contextPct ?? member.contextPct,
    estimatedCost: s.cost ?? member.estimatedCost,
    bankedCost: s.bankedCost ?? member.bankedCost,
  };
}
