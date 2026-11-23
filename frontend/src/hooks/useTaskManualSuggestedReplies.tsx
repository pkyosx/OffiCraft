import { createContext, useContext } from "react";
import { api } from "../api";
import {
  KEYED_SUGGESTED_REPLIES_NOT_PROVIDED,
  useKeyedSuggestedReplies,
  type KeyedSuggestedRepliesSource,
  type UseKeyedSuggestedReplies,
} from "./useKeyedSuggestedReplies";

/** Keyed by task type (`type_key`). */
export type UseTaskManualSuggestedReplies = UseKeyedSuggestedReplies;

const TASK_MANUAL_SOURCE: KeyedSuggestedRepliesSource = {
  name: "useTaskManualSuggestedReplies",
  topic: "task_manual_suggested_reply",
  get: () => api.getTaskManualSuggestedReplies(),
  put: (typeKey, replies) => api.putTaskManualSuggestedReplies(typeKey, replies),
};

/** The owner's 建議回覆 per task type (GET /api/task-manual-suggested-replies),
 * re-read on every `task_manual_suggested_reply` delta. */
export function useTaskManualSuggestedReplies(): UseTaskManualSuggestedReplies {
  return useKeyedSuggestedReplies(TASK_MANUAL_SOURCE);
}

const TaskManualSuggestedRepliesContext = createContext<UseTaskManualSuggestedReplies>(
  KEYED_SUGGESTED_REPLIES_NOT_PROVIDED,
);

export function TaskManualSuggestedRepliesProvider({
  value,
  children,
}: {
  value: UseTaskManualSuggestedReplies;
  children: React.ReactNode;
}) {
  return (
    <TaskManualSuggestedRepliesContext.Provider value={value}>{children}</TaskManualSuggestedRepliesContext.Provider>
  );
}

/** App's one `useTaskManualSuggestedReplies`. Outside a provider: nothing
 * loaded, so no chips and no editor. */
export function useTaskManualSuggestedRepliesContext(): UseTaskManualSuggestedReplies {
  return useContext(TaskManualSuggestedRepliesContext);
}
