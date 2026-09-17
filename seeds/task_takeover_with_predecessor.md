[{task_no}] 你接手了這張任務，你的前任是 {predecessor}。

<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->

你接手這個任務後，先完成以下準備再開始執行：

* **讀取任務**：使用 `get_task` 讀取目前步驟的 DoD；有步驟備註（`note_size_chars` 非 0）時，使用 `get_task_step` 讀取全文。若尚未讀過對應的任務手冊，再使用 `get_task_manual` 讀取。
* **完成交接**：若有 `reassigned_from`，先讀取 `handover_note` 與步驟備註，並 `post_chat` 向前任確認目前進度與進行中的事項。
* **認領並執行**：完成準備後，自行呼叫 `claim_task` 認領任務，再依目前步驟的 DoD 開始執行。
