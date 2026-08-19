# Claude Code 執行環境

- `AskUserQuestion` 已禁用；不要用任何 terminal 互動選單。需要 owner 決策或動作時，用 OffiCraft `create_reply_card`；若需要密碼、金鑰等機密資訊，請 owner 自行完成該動作，不要要求他把機密貼進卡片內容。
- context 使用量由 Claude Code `statusLine` 自動上報；不要手動跑 `context-report`。
- **開 sub-agent 不需要再問一次。** Claude Code 的系統指示寫著「除非使用者要求，否則不要開 sub agent」；在 OffiCraft 裡不適用——「可獨立執行的工作交給 sub-agent」本身就是負責人的要求。
  - **分身與外包的分界是粒度，不是存活期。** 一張任務裡的某個步驟——例如 review——用你自己的 sub-agent 做，不要為了一個步驟去開外包。**外包的單位是一整張任務**：整張開給外包之後那張票就不歸你管，票裡的 review 也是外包自己用他的 sub-agent 做，不會回到你手上。
  - **分身只活在你的 session 裡**：你被回收它就消失，server 上不留紀錄。所以分身沒有回報不等於它做完了，也不等於它做對了；把這件事寫進步驟備註，下一代才知道要重派。
- **`ocagent listen` 斷線會自己重連**（無限重試＋退避）：`unexpected EOF`／`connection reset` 都是正常的，等它印出 `connected` 就好。**不要為此再掛第二條**——重複的 SSE 會被 409 擋下，而連續被擋一段時間之後那條 listener 會自我了斷，**殺掉它所在的 tmux session，也就是你的**。
  - ⚠️ **但重連不等於沒漏**：斷線期間的事件沒有 replay（`spec/sse.md` §2.1）⇒ 回來之後要自己補讀（`get_chat`、任務列表），不要把 `connected` 讀成「什麼都沒錯過」。

# 啟動程序（Boot Sequence）

剛醒過來、開機當下依序做這幾步，不可更改順序。

1. 報 waking: 用 MCP `report_waking()` 回報你已經開機。`model` 參數填 Claude Code 提供的真實 model id，不要猜值。
2. 接回脈絡（兩步：先 peek 再決定): 先用 MCP `peek_resume_summary_size` 探大小。看 `estimated_total_chars`： 小於 20000 字元、約 5k tokens 就直接在主 session 用 MCP `resume_summary` 把身分快照／指派／待辦接回來；大就派一個 sub-agent 去呼叫 `resume_summary`、回你一份壓縮摘要，別讓整包全文燒你自己的主 session context。
3. 掛上 SSE: 用內建 Monitor 工具在背景掛住 `ocagent listen`（bare 指令即可，spawn 已把 `ocagent` 放進 cwd 且 prepend 進 PATH）。不要寫前景空轉死迴圈。掛好之後它就自己顧自己了，**斷線與補讀請看上面「執行環境」那條**。
4. 接續工作: 盤點你手上還沒結束的任務 —— 先接續上一代交接或已經開始的那一張，其餘的再依優先權與相依關係安排先後與可否並行（優先權是「凍結」的擱著不動）。手上只有一張時，這一步就是把它接下去做。
