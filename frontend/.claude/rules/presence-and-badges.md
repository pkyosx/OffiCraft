---
paths:
  - "src/components/LifecycleDot.tsx"
  - "src/components/PresenceBadge.tsx"
  - "src/components/PresenceAvatar*"
  - "src/components/presence-avatar.css"
  - "src/components/MemberActivity*"
  - "src/components/member-activity.css"
  - "src/components/OutsourcePanel*"
  - "src/components/ChatArea.tsx"
  - "src/components/MemberCard*"
  - "src/components/MemberActionButtons*"
  - "src/components/*DetailPanel*"
  - "src/components/MonitorPage*"
  - "src/components/ModelEffortEditor*"
  - "src/components/OfficeSidebarTabs*"
  - "src/components/badgeRing*"
  - "src/components/office.css"
  - "src/components/chrome.css"
  - "src/components/monitor.css"
  - "src/api/mappers*"
  - "src/lib/runtime.ts"
  - "scripts/check-token-roles.mjs"
  - "visual-guards/badge-ring-token.ct.spec.tsx"
---

# Presence、badge 與監控 readout

## Presence

共用 LifecycleDot、PresenceBadge、MemberCard、MonitorPage、MemberDetailPanel 與外包列的唯一狀態聯集是 offline、waking、online、stopping、stopped。

圓點畫在哪裡（owner rc-734cffef6221、c-3d68c2b097e7）：成員的三個身分面——左欄清單列、聊天室上方 header、詳情面板最上面的身分卡——正職與外包一律把圓點畫在**頭像右下角**，而且只經 `PresenceAvatar` 這一個元件畫；名字後面緊接著同樣只經 `ActivityStatusIcon` 畫的運作狀態圖示（沒有 activity 就不畫）。清單列與聊天室 header 傳 `memberName`，圖示變成按鈕，點下開運作狀況彈窗（內容與詳情面板的運作狀況卡共用 `MemberActivityBody`，不另寫一份）；彈窗 portal 到 body，而 React 會把 portal 裡的點擊與按鍵冒泡回所在的列，所以彈窗根節點擋下 click 與 keydown，否則點 ✕ 或在彈窗裡按 Enter 會開聊天或詳情。詳情身分卡不傳，正下方就是那張卡，點擊照舊只釘住摘要。圖示按鈕的手機觸控範圍以 ::after 往上、左、右各擴 8px，往下只擴 5px，避開第二行驚嘆號的範圍。這三處的第二行（`PresenceBadge dot={false}`、`OutsourceTaskLine mark`）只剩驚嘆號與角色／任務，不再有圓點。監控頁仍用 PresenceBadge 預設的行內圓點。不要在任何一個身分面另寫一份圓點或圖示：owner 要的是「從程式面都是同一條路」。圓點是頭像按鈕的**兄弟**，不能放進按鈕裡（按鈕裡不能再有可聚焦元素）。畫面取 hub.is_online 與 server lifecycle，不取 DB member.online；那個欄位只供 reconcile fallback。外包 worker 也不能因為仍在任務列就畫成 online。

圓點旁的驚嘆號（RuntimeLoginWarningMark）合併兩種來源：runtime_login_warnings（機器回報未登入）與 model_call_warnings（模型呼叫失敗），一個原因一行；兩者都空才不畫。rate_limit 不論幾筆都只畫一行（server 刻意會送成員自己無重置時間的一筆加帳號共用有重置時間的一筆），有重置時間就取最晚的；有未登入或登入失效時，最後按 runtime 各加一行「到監控頁登入」的指引。點驚嘆號會釘住提示（再點、點外面——包括點另一個驚嘆號——、Esc、捲動、resize，或另一個 InstantHint 被打開（含 hover／focus）才收；全畫面同時最多一個提示；hover 自己這顆不會解除釘住），點擊不往上冒泡，不會打開所在的列；手機沒有 hover，這是唯一看得到原因的方法（owner 要求），別恢復成讓點擊穿到列上。

圓點（LifecycleDot）本身就是同一種 InstantHint 觸發點：hover 立刻顯示狀態字、點擊釘住且不冒泡（不進聊天或詳情），別改回原生 title（電腦要等一秒、手機根本看不到）。狀態字同時是圓點的 aria-label，圓點是唯一一層可聚焦元素，不要再包一層可聚焦的 wrapper。手機觸控範圍（hover: none）：行內圓點（監控頁）只往左、往下擴（用 ::after，不用 padding，免得色點變大、提示位置偏掉）；驚嘆號不往左擴。兩者加大的範圍不可重疊（中間空隙不屬於任何一方的加大範圍，點下去由瀏覽器的觸控修正交給較近的那一個）——往右或往上擴會讓瀏覽器的觸控修正把點名字或點驚嘆號的手指吸到圓點上。頭像角上的圓點則往上、往左擴（蓋在頭像上），不往下擴，否則會伸進清單下一列。

presenceVisual 是唯一視覺映射，顏色只來自 CSS token，不准在元件裡塞色值；inline 色值會繞過 token guard，讓不同 lifecycle 共用錯誤顏色。toPresence 是 member 與 worker 共用的唯一 wire seam：未知 member wire 值落到 offline；未知 worker 值保持 undefined 並誠實畫 offline。不要加 default 把未知值偽裝成線上；否則會繞過 no-default 的錯誤訊號，畫面可能只剩沒有顏色或可及性名稱的假元件。

badge 的三個色槽要分清 danger badge、on-danger 文字與 ring；ring 預設沿用 var(--color-bg)。不要重新加已撤回的 ring 對比下限；只有文字對填色的可讀性契約仍有效。視覺守衛要讀 computed color 並掃 source，不能只看 class。

## 未讀

未讀數一律使用 server member.unreadCount；0 不畫，超過 99 畫 99+。只由 member→owner 的 server watermark 決定，前端不得用訊息數自行計算。清 watermark 只由明確的 mark-read 呼叫做（T-48：讀取聊天本身不再標已讀，任何路徑都不會）；目前選取中的聊天且視窗 active 時壓掉 badge；mock 必須同規則。

## 自報值與設定值

監控頁的 model、effort、runtime 是 MonitoringSessionDTO 的自報事實，不是 member 設定，也不以設定值 fallback。顯示 model.id，不猜 display_name；worker 以 ow- id join，沒有 session 就留空，不冒充 worker 設定。回報值也要落 durable actual 欄位，因為只留在 in-memory telemetry 會在 re-exec 後清空；fallback 到設定值又會把「已改但尚未生效」和「已生效」畫成同一件事。詳情面板的模型／思考強度維持唯讀，設定只在設定／更改 dialog，別把已移除的面板內編輯器復活。缺 effort 但仍有其他 telemetry 時顯示 stale。

機器表使用 server 的 hardware_stale 與 runtime_capabilities_stale，不由 client clock 重算；`true` 才代表過期，`false` 是活樣本中的誠實缺值，`null` 是從未量過。hardware stale 的值改畫 dash 加 stale 標記；runtime 版本遵循共享 capability map 與 installed/logged-in 狀態，Claude 才可使用 registry claudeVersion fallback，Codex 不可。stale runtime 值保留但標示 stale。登入標示只有一種：`stale === false` 且 logged_in 為 false 時顯示「未登入」（無括號），不加 hover、title 或 focus（owner 裁定，字本身已說清楚）；已登入、未回報、stale 都不顯示（owner 裁定，stale 時沒有登入狀態的來源）；machine 與 member id 要同時顯示。桌面 machine 欄 nowrap 的斷點與手機換行要維持現有版面契約。
