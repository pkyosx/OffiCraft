// The question of ReplyDeskStory's `questions` card, for the guards that read it back.
export const QUESTION_SUMMARY = "要把 migration 0142 排進今晚的部署嗎？";
export const QUESTION_BODY = [
  "請 [@Mira](/#office/member/mira) 先看 [這份計畫](https://example.com/plan) 再決定。",
  ...Array.from(
    { length: 16 },
    (_, i) => `第 ${i + 1} 點：這一步會改 chat_messages 的索引，上線前要先備份，失敗時照手冊第 ${i + 3} 節回滾。`,
  ),
].join("\n\n");
