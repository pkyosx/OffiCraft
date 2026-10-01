// lib/syntheticSender.ts — chat senders the SERVER stamps that are not member
// ids, so no per-member read (GET /api/members/{id}) can ever resolve them.

import { OWNER_ID } from "./ownerUnread";

// ⚠️ Mirrors the server's literal senders: wireSystemSender (wire.go), the
// webhook relay `hook:<endpointId>` (api_webhooks.go) and the scheduled
// message `sched:<scheduleId>` (scheduled_message.go). A new server-stamped
// shape that is missing here costs one 404 read per id, not a wrong label.
export function isSyntheticSender(id: string): boolean {
  return (
    id === OWNER_ID ||
    id === "system" ||
    id.startsWith("hook:") ||
    id.startsWith("sched:")
  );
}
