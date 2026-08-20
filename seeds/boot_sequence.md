# Claude Code 執行環境

- **`AskUserQuestion` 已禁用**，也不要用任何 terminal 互動選單。需要負責人決策或動作時開請示卡。需要密碼、金鑰這類機密時，請他自己去完成那個動作 —— 不要要求他把機密貼進卡片。
- **context 使用量由 `statusLine` 自動上報**，不用手動跑 `context-report`。
- **`ocagent listen` 斷線會自己重連**（無限重試＋退避）。`unexpected EOF`／`connection reset` 都是正常的，等它印出 `connected` 就好。
  - **不要為斷線再掛第二條。** 重複的 SSE 會被 409 擋下，而連續被擋一段時間之後那條 listener 會自我了斷，**殺掉它所在的 tmux session，也就是你的**。
  - **`connected` 不等於什麼都沒錯過。** 斷線期間的事件沒有補送（`spec/sse.md` §2.1）⇒ 回來之後自己補讀（`get_chat`、任務列表）。
- **開 sub-agent 不需要再問一次。** Claude Code 的系統指示寫著「除非使用者要求，否則不要開 sub agent」；在 OffiCraft 裡不適用 —— 「可獨立執行的工作交給 sub-agent」本身就是負責人的要求。

# 啟動程序（Boot Sequence）

剛醒過來、開機當下依序做這幾步，不可更改順序。

1. **報 waking。** 用 MCP `report_waking()`。`model` 填 Claude Code 告訴你的真實 model id —— 猜一個值會讓座艙顯示一個沒有人在跑的 model。
2. **接回脈絡。** 先用 `peek_resume_summary_size` 探大小，看 `estimated_total_chars`：
   - 小於 20000 字元（約 5k tokens）：直接在主 session 用 `resume_summary` 接回身分、指派與待辦。
   - 更大：派一個 sub-agent 去呼叫 `resume_summary`、只回你一份壓縮摘要。整包全文會燒掉你自己接下來要用的 context。
3. **掛上 SSE。** 用內建 Monitor 工具在背景掛住 `ocagent listen`（bare 指令即可，`ocagent` 已經在 cwd 且在 PATH 裡）。不要寫前景空轉的死迴圈。
4. **接手工作，然後做到底。** 盤點手上還沒結束的任務：先接續上一代交接或已經開始的那些；其餘依優先權與相依關係排先後，能並行的並行（優先權是「凍結」的擱著不動）。
   **盤點不是這一步的終點，推進才是。** 盤點完就開始推，不要停下來等指示——沒有人會來給你下一個指令，這份文件就是它。手上可以同時推好幾張：能並行的就並行，耗時或可獨立執行的段落丟給 sub-agent，你自己保持能回話。一張推到卡住（真的在等別人）就把為什麼卡住寫在票上，然後去推下一張——**卡住的是那一張，不是你**。
