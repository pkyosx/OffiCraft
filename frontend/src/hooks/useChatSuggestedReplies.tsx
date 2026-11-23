import { createContext, useContext } from "react";
import { api } from "../api";
import {
  KEYED_SUGGESTED_REPLIES_NOT_PROVIDED,
  useKeyedSuggestedReplies,
  type KeyedSuggestedRepliesSource,
  type UseKeyedSuggestedReplies,
} from "./useKeyedSuggestedReplies";

/** Keyed by member id. */
export type UseChatSuggestedReplies = UseKeyedSuggestedReplies;

const CHAT_SOURCE: KeyedSuggestedRepliesSource = {
  name: "useChatSuggestedReplies",
  topic: "chat_suggested_reply",
  get: () => api.getChatSuggestedReplies(),
  put: (memberId, replies) => api.putChatSuggestedReplies(memberId, replies),
};

/** The owner's chat 建議回覆 (GET /api/chat/suggested-replies), re-read on every
 * `chat_suggested_reply` delta. */
export function useChatSuggestedReplies(): UseChatSuggestedReplies {
  return useKeyedSuggestedReplies(CHAT_SOURCE);
}

// A context, not a second hook call: <ChatArea> may not fetch while it paints
// (ChatArea.quote-no-fetch.test.tsx), and the reply cards inside it need the
// same lists the chat room shows.
const ChatSuggestedRepliesContext = createContext<UseChatSuggestedReplies>(KEYED_SUGGESTED_REPLIES_NOT_PROVIDED);

export function ChatSuggestedRepliesProvider({
  value,
  children,
}: {
  value: UseChatSuggestedReplies;
  children: React.ReactNode;
}) {
  return <ChatSuggestedRepliesContext.Provider value={value}>{children}</ChatSuggestedRepliesContext.Provider>;
}

/** App's one `useChatSuggestedReplies`. Outside a provider: nothing loaded, so
 * no chips and no editor. */
export function useChatSuggestedRepliesContext(): UseChatSuggestedReplies {
  return useContext(ChatSuggestedRepliesContext);
}
