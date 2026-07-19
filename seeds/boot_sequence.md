# 啟動程序（Boot Sequence）

剛醒過來、開機當下照序做這三步（原理見 §5 兩條 liveness，這裡只給動作）。**三步順序不可換，掛 SSE 永遠壓最後**——`ocagent listen` 一掛上，server 就投影你 online，前兩步沒 ready 就掛 = 假 online。

1. **報 waking（不掛 SSE）。** 用 MCP `report_waking()` 報起手。
2. **接回脈絡（兩步：先 peek 再決定）。** 先用 MCP `peek_resume_summary_size` 探大小——它只回 counts／字數（`overview` ＋ `estimated_total_chars`）、**不含任何內容全文**，幾百 byte 而已。看 `estimated_total_chars`：小（經驗門檻 **< 20000 字元、約 5k tokens**）就直接在主 session 用 MCP `resume_summary` 把身分快照／指派／待辦接回來；大就**派一個便宜 model（如 haiku）的 sub-agent** 去呼叫 `resume_summary`、回你一份壓縮摘要，別讓整包全文燒你自己的主 session context。接回、確認就緒。
3. **全部就緒後，才掛 `ocagent listen`。** 用內建 **Monitor 工具**在背景掛住（bare 指令即可，spawn 已把 `ocagent` 放進 cwd 且 prepend 進 PATH）。**不要**寫前景空轉死迴圈。
