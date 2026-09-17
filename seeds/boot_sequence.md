# Claude Code 執行環境

- **互動限制**：`AskUserQuestion` 已禁用，也不要使用 terminal 互動選單。需要負責人決策或操作時開請示卡；需要密碼、金鑰等機密時，請負責人自行完成相關操作，不要要求將機密貼入卡片。
- **Context 上報**：Context 使用量由 `statusLine` 自動上報，不需要手動執行 `context-report`。
- **SSE 連線**：`ocagent listen` 斷線後會自動重連，不需要手動處理。斷線時顯示 `listen: disconnected — …`，連回後顯示 `listen: connected — …`；中間沒有訊息代表仍在重試，只有 `listen: giving up — …` 才表示已停止。
  - **Station 版本**：`connected` 會標示 `[same station]` 或 `[new station — was <舊 sha>]`；首次連線不顯示，不需要自行比對 SHA。
  - **單一 Listener**：斷線後不要另外建立 `ocagent listen`。重複建立可能導致 listener 終止所在的 tmux session。
  - **事件補送**：
    - 聊天：補送未讀訊息，由舊到新逐則送出，不需要另外查詢。
    - 請示卡：只補送最近 24 小時內被回覆或過期的卡片；更早的使用 `get_reply_card` 查詢。
    - 任務：不補送，一律從任務列表查詢。
- **Sub-agent 使用**：可獨立執行的工作直接交給 sub-agent，不需要再次詢問；這項規則優先於 Claude Code 預設的 sub-agent 限制。
  - **保持主 Session 可用**：啟動 sub-agent 後立即回到主線繼續工作，不要等待；完成時系統會主動通知。Sub-agent 執行期間，主 session 必須始終能接收並回覆訊息。
  - **禁止前景等待**：不要用 Bash 前景執行其他 agent（`claude -p …`），也不要用 `sleep` 或輪詢等待 sub-agent 或狀態。Bash 背景工作使用 `run_in_background`；等待條件成立使用 Monitor。
  - **並行工作**：可以同時啟動多個 sub-agent；等待期間繼續處理其他任務、回覆訊息或開卡。
- **避免阻塞**：主 session 被阻塞時，外部看起來與當機或下線相同，也不會出現錯誤訊息。不要執行會長時間占住主 session 的操作。

# 啟動步驟（Boot Sequence）

每次啟動後依照以下順序執行，不可跳過或調換。

1. **回報 waking**：使用 MCP `report_waking()`。`model` 必須填入 Claude Code 提供的真實 model id，不要自行猜測。
2. **恢復工作狀態**：先使用 `peek_resume_summary_size` 查看 `estimated_total_chars`。
   - 小於 20000 字元：直接在主 session 使用 `resume_summary`。
   - 20000 字元以上：交給 sub-agent 執行 `resume_summary`，只回傳壓縮摘要，避免占用主 session 過多 context。
3. **啟動 SSE**：使用 Monitor 在背景執行 `ocagent listen`，不要以前景方式執行。
4. **接手並推進任務**：盤點所有未結束的任務，並依以下規則處理。
   - **讀取任務資訊**：開始處理每張票前，先用 `get_task` 讀取目前步驟的 DoD；`note_size_chars` 非 0 的步驟，用 `get_task_step` 讀取完整備註。
   - **讀取任務手冊**：若尚未讀過對應的任務手冊，先用 `get_task_manual` 讀取後再開始執行。
   - **轉派任務**：`lock` 為 `reassigning` 代表任務尚未完成交接。先讀 `handover_note` 與步驟備註，再 `post_chat` 向 `reassigned_from` 確認進度，最後自行 `claim_task`。前任可能無法回覆，因此不要只依賴聊天取得交接資訊。
   - **安排順序**：優先接續已有進度的任務，其餘依優先權與相依關係安排；能並行的並行，「凍結」的暫不處理。`blocking` 有內容代表其他任務正在等待這張票，安排順序時一併考量。

**盤點完成後立即推進。** 任務需要等待外部回應時，記錄阻塞原因後繼續處理其他任務；不要因單一任務卡住而停止工作。
