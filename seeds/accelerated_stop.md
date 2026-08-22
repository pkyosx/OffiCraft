{where} — offboard now: work the sequence below, then call report_stopped yourself. Your deadline is {deadline}.

<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->

# 下線程序

下線過程中，所有重要資料都不可以只留在本機 —— 重啟之後你可能在另一台機器上。

- git commit 要推送到 remote。
- 產物（artifact）要上傳到 task 底下。

## 1. 判斷通知

- **硬性**：通知裡出現一個具體的結束時刻 —— `Your deadline is` 後面接著一個 UTC 時間戳。
- **軟性**：沒有那樣的時刻。
- 這一輪只要收過一則硬性，之後一律當硬性。

## 2. 開始下線

1. 呼叫 MCP `report_stopping()`。
2. 用 MCP `post_chat` 發給自己：現況、在途工作、阻塞點、下一步，以及有哪些 sub agent 在做什麼、跑多久了。
3. 把在途的 sub agent 寫進 task step。**這一格沒被更新過，就代表它沒交件，下一代要重派。**

## 3. 結束 sub agent

- **軟性**：等 sub agent 自己完成。
- **硬性**：請 sub agent 立刻把手上的東西收尾並結束，把目前狀態寫回 server，至少包含已驗證的內容、證據位置、剩餘工作與未檢查範圍。
- **每個 sub agent 一結束就當場更新** `post_chat` 與 task step，不要留到最後。

## 4. 收尾

1. 用 `ocagent clean <path>` 移除暫存檔/資料夾（不要用 `rm -rf`，他可能讓你彈出確認視窗而停住）。
   - 它印出 `unknown subcommand "clean"`，代表你手上這支 ocagent 太舊。**跳過這一步、在交接裡記一句「暫存沒收，ocagent 太舊」，然後把下面兩步做完** —— 不要為了收暫存停在這裡。
   - **認那句話，不要認離開碼**：被拒絕（例如你指的路徑在工作目錄外面）也是同一個非 0。那不是太舊，是這次指錯了，換一個路徑再叫一次。
2. 把這一輪的重要教訓回寫到長期記憶。**要寫進哪一份、怎麼寫，看開機說明「記憶與學習」那一節，那裡是權威**；只送改動的那一段。
3. 呼叫 MCP `report_stopped()`。
