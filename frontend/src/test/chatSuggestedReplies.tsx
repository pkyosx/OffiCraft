import { ChatSuggestedRepliesProvider, useChatSuggestedReplies } from "../hooks/useChatSuggestedReplies";

/** What App wraps every page in: the one read of every member's chat 建議回覆. */
export function WithChatSuggestedReplies({ children }: { children: React.ReactNode }) {
  return <ChatSuggestedRepliesProvider value={useChatSuggestedReplies()}>{children}</ChatSuggestedRepliesProvider>;
}
