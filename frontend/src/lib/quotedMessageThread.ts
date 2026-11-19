import type { ChatMessage } from "../api/adapter";
import { isSyntheticSender } from "./syntheticSender";

// Owner ruling rc-38583da201d3: prefer staying in the current room, then the
// sender's room. The owner and server-stamped senders have no room of their own.
export function quotedMessageThreadPeer(
  currentPeerId: string,
  original: Pick<ChatMessage, "from" | "to" | "deletedTs">,
): string | null {
  if ((original.deletedTs ?? 0) > 0) return null;
  if (original.from === currentPeerId || original.to === currentPeerId) {
    return currentPeerId;
  }
  if (!isSyntheticSender(original.from)) return original.from;
  if (!isSyntheticSender(original.to)) return original.to;
  return null;
}
