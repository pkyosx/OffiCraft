import type { AvatarKind } from "./themeBundle";

/** Resolve the per-role avatar kind for a 正職 render point from a member-like
 * object (T-ea81). An outsource worker (id "ow-…", the codebase's outsource-id
 * convention, or kind "outsource") → "outsource"; an assistant-role member →
 * "assistant"; anything else → "member". The owner has no member object — its
 * single avatar point (the profile pill) passes kind="owner" directly and never
 * routes through here. */
export function avatarKindForMember(m: {
  id?: string;
  kind?: string;
  role?: string;
}): AvatarKind {
  if (m.id?.startsWith("ow-") || m.kind === "outsource") return "outsource";
  if (m.role === "assistant") return "assistant";
  return "member";
}
