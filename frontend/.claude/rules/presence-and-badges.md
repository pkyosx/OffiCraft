---
paths:
  - "src/components/LifecycleDot.tsx"
  - "src/components/PresenceBadge.tsx"
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

共用 LifecycleDot、PresenceBadge、MemberCard、MonitorPage、MemberDetailPanel 與外包列的唯一狀態聯集是 offline、waking、online、stopping、stopped。畫面取 hub.is_online 與 server lifecycle，不取 DB member.online；那個欄位只供 reconcile fallback。外包 worker 也不能因為仍在任務列就畫成 online。

presenceVisual 是唯一視覺映射，顏色只來自 CSS token，不准在元件裡塞色值；inline 色值會繞過 token guard，讓不同 lifecycle 共用錯誤顏色。toPresence 是 member 與 worker 共用的唯一 wire seam：未知 member wire 值落到 offline；未知 worker 值保持 undefined 並誠實畫 offline。不要加 default 把未知值偽裝成線上；否則會繞過 no-default 的錯誤訊號，畫面可能只剩沒有顏色或可及性名稱的假元件。

badge 的三個色槽要分清 danger badge、on-danger 文字與 ring；ring 預設沿用 var(--color-bg)。不要重新加已撤回的 ring 對比下限；只有文字對填色的可讀性契約仍有效。視覺守衛要讀 computed color 並掃 source，不能只看 class。

## 未讀

未讀數一律使用 server member.unreadCount；0 不畫，超過 99 畫 99+。只由 member→owner 的 server watermark 決定，前端不得用訊息數自行計算。讀取聊天清 watermark；目前選取中的聊天且視窗 active 時壓掉 badge；mock 必須同規則。

## 自報值與設定值

監控頁的 model、effort、runtime 是 MonitoringSessionDTO 的自報事實，不是 member 設定，也不以設定值 fallback。顯示 model.id，不猜 display_name；worker 以 ow- id join，沒有 session 就留空，不冒充 worker 設定。回報值也要落 durable actual 欄位，因為只留在 in-memory telemetry 會在 re-exec 後清空；fallback 到設定值又會把「已改但尚未生效」和「已生效」畫成同一件事。詳情面板的模型／投入度維持唯讀，設定只在設定／更改 dialog，別把已移除的面板內編輯器復活。缺 effort 但仍有其他 telemetry 時顯示 stale。

機器表使用 server 的 hardware_stale 與 runtime_capabilities_stale，不由 client clock 重算；`true` 才代表過期，`false` 是活樣本中的誠實缺值，`null` 是從未量過。hardware stale 的值改畫 dash 加 stale 標記；runtime 版本遵循共享 capability map 與 installed/logged-in 狀態，Claude 才可使用 registry claudeVersion fallback，Codex 不可。stale runtime 值保留但標示 stale；machine 與 member id 要同時顯示。桌面 machine 欄 nowrap 的斷點與手機換行要維持現有版面契約。
