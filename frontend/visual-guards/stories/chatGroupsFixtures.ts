import type { ChatGroup } from "../../src/lib/chatGroups";

/** Two staff groups, and two outsource groups owning the 前端 UI and Eva types. */
export const sampleGroups: ChatGroup[] = [
  { id: "g-lead", tab: "staff", name: "核心", memberIds: ["mira", "clara"], taskTypeKeys: [] },
  { id: "g-eng", tab: "staff", name: "工程", memberIds: ["beto", "dora"], taskTypeKeys: [] },
  { id: "g-ui", tab: "outsource", name: "前端外包", memberIds: ["ow-a18f"], taskTypeKeys: ["tm-fe"] },
  { id: "g-ops", tab: "outsource", name: "營運", memberIds: ["ow-71d0"], taskTypeKeys: ["tm-eva"] },
];

