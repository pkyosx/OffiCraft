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

圓點畫在哪裡（owner rc-734cffef6221、c-3d68c2b097e7）：成員的三個身分面——左欄清單列、聊天室上方 header、詳情面板最上面的身分卡——正職與外包一律把圓點畫在**頭像右下角**，而且只經 `PresenceAvatar` 這一個元件畫；名字**前面**是同樣只經 `NameWithActivity` 畫的運作狀態圖示——圖示放名字前還是後只由這個元件決定，各面不另排順序。圖示的有無依 presence 決定（與頭像圓點同一個 presence 值，各面一律傳 `presence`）：online 與 stopping（換手中，session 仍在跑、仍在回報）照實際回報畫；waking 一律畫未知圖示，即使手上有 activity——server 只在 report_waking 才清掉舊回報，那可能是上一個 session 的；offline、stopped 就畫灰色月亮（`activity-icon--offline`，`--color-text-muted`，不動，是最安靜的狀態），即使手上還留著舊的 activity 也一樣，免得灰點旁邊還寫著工作中；月亮是 InstantHint 的 span、`focusable={false}`、`aria-hidden`，hover 或點一下只顯示圓點那個狀態字（`t.office.presence`，應景主題的覆寫跟著走），不是按鈕、不開彈窗、點擊不冒泡到列——可聚焦、會報讀 presence 的是旁邊的圓點，月亮不要再做一份。online 或 stopping 但 activity 還是 null（尚未回報）、以及 waking 時，三處都畫灰色的未知圖示（`activity-icon--unknown`，圓圈內問號，與月亮同色 `--color-text-muted`、同樣不動），和月亮走同一條分支：InstantHint 的 span、`focusable={false}`、`aria-hidden`，hover 或點一下只顯示 `t.activity.unknownHint`（「狀態未知，成員重新啟動後就會顯示」）；waking 改用 `t.activity.unknownWakingHint`（「狀態未知，成員醒來回報後就會顯示」），兩者應景主題都有覆寫，不是按鈕、不開彈窗、點擊不冒泡到列。所以每一列都有一個看得見的圖示，沒有隱藏空位。左欄兩份清單（正職、外包）傳 `layout="list"`；月亮與未知圖示都帶按鈕的盒子（`activity-icon--box`，只有幾何，沒有按鈕的 hover 框），整欄的圖示與名字各自對齊同一條 x；清單名字會換行，圖示固定在第一行。聊天室 header 與詳情身分卡只有一個名字、沒有欄可對齊，月亮與未知圖示照畫；身分卡那一行會把 id badge 換到下一行，所以傳 `layout="group"` 把圖示和名字包成一個盒子，免得圖示單獨留在上一行。可點的圖示平時只有字形，沒有框與底色（邊框保留為透明，盒子大小不變），hover 與鍵盤 focus 才出現綠框與光暈；觸控螢幕平時一樣沒有框。三個運作狀態字形都會動（月亮與未知圖示不動）：工作中的星芒閃動（1.6 秒）；等待中的沙漏每 3 秒翻轉半圈、其餘時間靜止；閒置的咖啡杯上三條熱氣慢慢上升淡出（2.8 秒），杯身不動。等待與閒置比星芒慢、幅度小，工作中仍是最搶眼的；`prefers-reduced-motion` 時三個都靜止。清單列與聊天室 header 傳 `memberName`，圖示變成按鈕，點下開運作狀況彈窗（內容與詳情面板的運作狀況卡共用 `MemberActivityBody`，不另寫一份）；彈窗 portal 到 body，而 React 會把 portal 裡的點擊與按鍵冒泡回所在的列，所以彈窗根節點擋下 click 與 keydown，否則點 ✕ 或在彈窗裡按 Enter 會開聊天或詳情。activity 變成 null（或換了成員）時彈窗直接關掉、不留開啟狀態，activity 回來不會自己再開、也不搶焦點；Tab／Shift+Tab 只在彈窗裡繞；關閉時焦點交回當下掛著的那顆圖示（ref，不存舊節點）。詳情身分卡不傳，正下方就是那張卡，點擊照舊只釘住摘要。圖示按鈕的手機觸控範圍以 ::after 擴到至少 32×32，主要往上擴、往下只擴 4px：再往下就碰到第二行驚嘆號的觸控範圍（驚嘆號就在圖示正下方）；往左只擴 4px：頭像右下角圓點的觸控範圍伸出頭像約 4px，圖示與頭像之間只有 10–11px，擴更多會和圓點重疊。這三處的第二行（`PresenceBadge dot={false}`、`OutsourceTaskLine mark`）只剩驚嘆號與角色／任務，不再有圓點。監控頁仍用 PresenceBadge 預設的行內圓點。不要在任何一個身分面另寫一份圓點或圖示：owner 要的是「從程式面都是同一條路」。圓點是頭像按鈕的**兄弟**，不能放進按鈕裡（按鈕裡不能再有可聚焦元素）。畫面取 hub.is_online 與 server lifecycle，不取 DB member.online；那個欄位只供 reconcile fallback。外包 worker 也不能因為仍在任務列就畫成 online。

圓點旁的驚嘆號（RuntimeLoginWarningMark）合併兩種來源：runtime_login_warnings（機器回報未登入）與 model_call_warnings（模型呼叫失敗），一個原因一行；兩者都空才不畫。rate_limit 不論幾筆都只畫一行（server 刻意會送成員自己無重置時間的一筆加帳號共用有重置時間的一筆），有重置時間就取最晚的；有未登入或登入失效時，最後按 runtime 各加一行「到監控頁登入」的指引。點驚嘆號會釘住提示（再點、點外面——包括點另一個驚嘆號——、Esc、捲動、resize，或另一個 InstantHint 被打開（含 hover／focus）才收；全畫面同時最多一個提示；hover 自己這顆不會解除釘住），點擊不往上冒泡，不會打開所在的列；手機沒有 hover，這是唯一看得到原因的方法（owner 要求），別恢復成讓點擊穿到列上。

圓點（LifecycleDot）本身就是同一種 InstantHint 觸發點：hover 立刻顯示狀態字、點擊釘住且不冒泡（不進聊天或詳情），別改回原生 title（電腦要等一秒、手機根本看不到）。狀態字同時是圓點的 aria-label，圓點是唯一一層可聚焦元素，不要再包一層可聚焦的 wrapper。手機觸控範圍（hover: none）：行內圓點（監控頁）只往左、往下擴（用 ::after，不用 padding，免得色點變大、提示位置偏掉）；驚嘆號不往左擴。兩者加大的範圍不可重疊（中間空隙不屬於任何一方的加大範圍，點下去由瀏覽器的觸控修正交給較近的那一個）——往右或往上擴會讓瀏覽器的觸控修正把點名字或點驚嘆號的手指吸到圓點上。頭像角上的圓點則往上、往左擴（蓋在頭像上），不往下擴，否則會伸進清單下一列。

presenceVisual 是唯一視覺映射，顏色只來自 CSS token，不准在元件裡塞色值；inline 色值會繞過 token guard，讓不同 lifecycle 共用錯誤顏色。toPresence 是 member 與 worker 共用的唯一 wire seam：未知 member wire 值落到 offline；未知 worker 值保持 undefined 並誠實畫 offline。不要加 default 把未知值偽裝成線上；否則會繞過 no-default 的錯誤訊號，畫面可能只剩沒有顏色或可及性名稱的假元件。

badge 的三個色槽要分清 danger badge、on-danger 文字與 ring；ring 預設沿用 var(--color-bg)。不要重新加已撤回的 ring 對比下限；只有文字對填色的可讀性契約仍有效。視覺守衛要讀 computed color 並掃 source，不能只看 class。

## 未讀

未讀數一律使用 server member.unreadCount；0 不畫，超過 99 畫 99+。只由 member→owner 的 server watermark 決定，前端不得用訊息數自行計算。清 watermark 只由明確的 mark-read 呼叫做（T-48：讀取聊天本身不再標已讀，任何路徑都不會）；目前選取中的聊天且視窗 active 時壓掉 badge；mock 必須同規則。

## 自報值與設定值

監控頁的 model、effort、runtime 是 MonitoringSessionDTO 的自報事實，不是 member 設定，也不以設定值 fallback。顯示 model.id，不猜 display_name；worker 以 ow- id join，沒有 session 就留空，不冒充 worker 設定。回報值也要落 durable actual 欄位，因為只留在 in-memory telemetry 會在 re-exec 後清空；fallback 到設定值又會把「已改但尚未生效」和「已生效」畫成同一件事。詳情面板的模型／思考強度維持唯讀，設定只在設定／更改 dialog，別把已移除的面板內編輯器復活。缺 effort 但仍有其他 telemetry 時顯示 stale。

機器表使用 server 的 hardware_stale 與 runtime_capabilities_stale，不由 client clock 重算；`true` 才代表過期，`false` 是活樣本中的誠實缺值，`null` 是從未量過。hardware stale 的值改畫 dash 加 stale 標記；runtime 版本遵循共享 capability map 與 installed/logged-in 狀態，Claude 才可使用 registry claudeVersion fallback，Codex 不可。stale runtime 值保留但標示 stale。登入標示只有一種：`stale === false` 且 logged_in 為 false 時顯示「未登入」（無括號），不加 hover、title 或 focus（owner 裁定，字本身已說清楚）；已登入、未回報、stale 都不顯示（owner 裁定，stale 時沒有登入狀態的來源）；machine 與 member id 要同時顯示。桌面 machine 欄 nowrap 的斷點與手機換行要維持現有版面契約。
