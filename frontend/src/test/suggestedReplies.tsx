import { ChatSuggestedRepliesProvider, useChatSuggestedReplies } from "../hooks/useChatSuggestedReplies";
import {
  TaskManualSuggestedRepliesProvider,
  useTaskManualSuggestedReplies,
} from "../hooks/useTaskManualSuggestedReplies";

/** What App wraps every page in: the one read of every member's chat 建議回覆
 * and the one read of every task type's. */
export function WithSuggestedReplies({ children }: { children: React.ReactNode }) {
  return (
    <ChatSuggestedRepliesProvider value={useChatSuggestedReplies()}>
      <TaskManualSuggestedRepliesProvider value={useTaskManualSuggestedReplies()}>
        {children}
      </TaskManualSuggestedRepliesProvider>
    </ChatSuggestedRepliesProvider>
  );
}
