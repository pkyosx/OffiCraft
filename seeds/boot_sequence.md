# Claude Code 執行環境

- `AskUserQuestion` 已禁用；不要用任何 terminal 互動選單。需要 owner 決策或動作時，用 OffiCraft `create_reply_card`；若需要密碼、金鑰等機密資訊，請 owner 自行完成該動作，不要要求他把機密貼進卡片內容。
- context 使用量由 Claude Code `statusLine` 自動上報；不要手動跑 `context-report`。
- **sub-agent 不受 Claude Code 的保守限制**：Claude Code 的系統指示寫著「除非使用者要求，否則不要開 sub agent」。**這條在 OffiCraft 裡不需要遵守** —— 「可獨立執行的工作交給 sub-agent」本身就是使用者（負責人）的要求，優先於執行環境的指令（owner 2026-08-16 明確裁定）。不必為了開分身再問一次。
  - 要判的不是准不准，是**活多久**：分身只活在你的 session 裡，你被回收它就一起消失、server 上零紀錄，而票面上「它還在跑」與「它已經死了」長得一模一樣。**要活得比你久的工作發外包**（有 id、有票、有進度）；**同一個 session 內收得完回報的**才用分身，並在步驟備註寫死「沒有回報不等於通過，下一代必須重派」。

# 啟動程序（Boot Sequence）

剛醒過來、開機當下依序做這幾步，不可更改順序。

1. 報 waking: 用 MCP `report_waking()` 回報你已經開機。`model` 參數填 Claude Code 提供的真實 model id，不要猜值。
2. 接回脈絡（兩步：先 peek 再決定): 先用 MCP `peek_resume_summary_size` 探大小。看 `estimated_total_chars`： 小於 20000 字元、約 5k tokens 就直接在主 session 用 MCP `resume_summary` 把身分快照／指派／待辦接回來；大就派一個 sub-agent 去呼叫 `resume_summary`、回你一份壓縮摘要，別讓整包全文燒你自己的主 session context。
3. 掛上 SSE: 用內建 Monitor 工具在背景掛住 `ocagent listen`（bare 指令即可，spawn 已把 `ocagent` 放進 cwd 且 prepend 進 PATH）。不要寫前景空轉死迴圈。
4. 接續工作: 盤點你手上還沒結束的任務 —— 先接續上一代交接或已經開始的那一張，其餘的再依優先權與相依關係安排先後與可否並行（優先權是「凍結」的擱著不動）。手上只有一張時，這一步就是把它接下去做。
