---
paths:
  - "src/api/**"
  - "src/hooks/**"
  - "src/lib/deltaSink.ts"
  - "src/lib/ownerUnread.ts"
  - "src/lib/sharedSnapshot.ts"
  - "src/components/OnboardingBanner*"
  - "src/types.ts"
---

# Data layer

## 單向 seam

資料只能沿 wire → mappers → types → adapter → mock/http → hooks → component 流動。api/index.ts 的 USE_MOCK 是唯一切換點；元件不可直接 fetch。新端點要同時補 wire、mapper、型別、adapter、mock、http 與必要測試。

所有非 2xx 都轉成 ApiError，讀 server 的 error.code 與 error.message；不要用 message regex 判斷錯誤。mock 必須回同一個 envelope。

`error.code` 只有一份表：`docs/design/api-error-envelope.codes.json`，由 `api/errorCodes.ts` 的 `codeForStatus` 讀入（server 的 `errorCodeForStatus` 與 conformance 的 `CODE_BY_STATUS` 也各自對它斷言）。mock 與測試都不得手寫 code：mock 用 `mockApiError(message, status, serverMessage)`，不 import `ApiError`；測試裡造假回應寫 `codeForStatus(status)`。手寫過的 `bad_request` 是 server 從來不會送的字串，讓依 code 分支的元件測試跟一個不存在的 server 達成一致。`ApiError.code` 本身仍是自由字串——它裝的是線上真回應，server 日後新增 code 不能讓前端編不過。

## SSE 與 refetch

api/http.ts 的 toSseDelta 只投影事件身分欄：id、from、to、reader、peer；其他 status／priority 等欄位在 seam 就丟掉，讓下游型別上不可能偷偷 merge payload。姓名任一缺少就完整 refetch，不可當成「無變更」：串流沒有 replay，空名字代表不知道漏了什麼。

帶 `requestVersion` 的 hook（useMachines、useMonitoring）：那個版本號**只能由真的送出請求的路徑遞增**。事件幀不得「遞增版本卻不發請求」——那會作廢每一支在飛的請求、卻沒有任何請求負責補回來，畫面只能等下一次 trailing 重抓。事件幀的意思是「可能有更新的答案」，不是「在飛的這個答案是錯的」：讓它照自己的版本落地，另外排程 coalesced 的後續重抓。版本號守的是**送出順序**（後送出的才准 setState，與回應先後無關），那條保證仍在，不要連它一起拆掉。

downlink 只有一條、且是 module-level singleton。瀏覽器只會自己重試「暫時性」的斷線（`readyState` 回到 CONNECTING）；non-200、401、`Content-Type` 不對會讓它**永久 CLOSED 且再也不重試**。那一格是前端自己的責任：偵測到 CLOSED 就把 `sseSource` 清成 null（不清 ⇒ 之後每次 `ensureSseSource()` 都會 early-return 在那具屍體上）、backoff 重建，而**重建出來的那條連線第一次 open 必須 resync**。只重連不 resync 比不重連更糟：串流沒有 replay，斷線期間的 delta 就此永久消失，而畫面看起來完全正常。

401 不走重連：先問 `/api/events` 本身拿到什麼狀態碼（`onerror` 不帶狀態碼，猜就是二選一都會錯——猜過期會被伺服器抖一下就登出，猜抖動會對已經說不的伺服器無限重打），401／403 ⇒ `handleUnauthorized()` 停止重試。連不到（fetch reject）不是 401。

**連線健康度要 publish 給 UI**（`Api.subscribeConnection`）。整個座艙都是 delta 驅動的，所以「連線死掉」跟「今天很安靜」在畫面上長得一模一樣；靜默自癒等於把一個看得見的停頓換成一個看不見的洞。mock 沒有 transport 可以掉，只回一次 `live`。

deltaSink 每個 burst 只做一次同步決策；coalesce 留在決策層，不要跨 tick debounce。narrowToHeld 的語意固定為：null = 全量、非空 = 指定項目、空集合 = 只指向其他項目。task topic 仍需全量，因為清單可能新增列；chat、chat_read、roster 在沒有持有項目時可跳過。

只有在單筆回應是清單列的真正 superset 時才可 per-item：

- outsource worker 可按單筆更新，但 owner-unread 述詞要判斷是否真的可能改變；移動到 owner 才需要刷新。
- member 可按單筆更新，且要保留 server unread count；一次事件超過一個 id 就全量。
- task 不可按單筆更新：清單列有 dep_tasks，TaskDTO 沒有；TaskCard 的依賴 chip 以清單列為準。
- useChatUnread 只代表總聊天未讀；只有 chat 與 chat_read 都無持有項目時可跳過，其餘 topic 仍全量。

GET /api/chat 在任何路徑都不會前進 watermark（T-48 owner 裁定：標已讀要由明確表達該意圖的 API 做，改為 POST /api/chat/mark-read；原本用來閃避這個副作用的 peek 參數同批刪除）；chat 用 from/to 判斷 peer，chat_read 用 reader 判斷 peer。沒有 peer 名稱時走全量路徑。

owner unread 只由共享的 lib/ownerUnread.ts 判定：chat.to 或 chat_read.reader 指向 owner 才能改 owner badge（此述詞只回答「什麼能改 server 上的那個數字」；consumer 要據此跳過 refetch，還要自己持有的數字不是舊的——上一次 fetch 失敗過就不算，見 useChatUnread 的 staleRef）；owner 不算 roster member。每個 burst 有兩個以上候選時一律全量，單筆也不能只看事件數猜測；一則 agent↔agent 加一則給 owner 的事件會在同一 microtask 形成混合 burst，若逐則跳過會吃掉真正需要更新的那一半。

任何 per-item 路徑都先查 `api/dtoParity.ts` 與實際 Go handler，確認單筆 response 是清單列的真正 superset；測試 mock 不得比凍結 wire 慷慨，否則 jsdom 會用 server 不可能回的欄位把壞路徑驗綠。`TaskDTO` 沒有 `dep_tasks` 時，不能把 `narrowToHeld` 順手接回 task；依賴 chip 必須繼續讀有該欄位的清單列。

## 共享設定快照

所有 mount 都經 loadServerSettings，不可各自直接呼叫 settings API。共享層負責 single-flight、快取與 generation；同 tab 的 patch echo、登入變更與明確 refresh 要 invalidate。不要加 TTL，也不要假定跨 tab 即時同步。

舊 GET 不得覆寫已採用的 patch。onboarding: null 是正常終態；running 在 set-password 前要先持久化，輪詢直到 failed/success，讀取錯誤也要繼續輪詢。

## owner 的顯示名字

`t.user` 是**主題**給「人類」的預設稱謂（預設「CEO（你）」、仙俠主題「市長（你）」），**不是 owner 設的名字**。owner 真正的暱稱在 server（`/api/settings` 的 `owner_name`），由 `useOwnerName` 讀。凡是把 owner 當**參與者**畫出來的面（聊天區 `nameOf` 的 owner 分支、文件歷史的修改者列），一律讀 `useOwnerDisplayName(t.user)`，不可直接印 `t.user` —— 兩種畫法並存時，同一個人會在同一個畫面上有兩個名字（2026-08-22 owner 實機回報）。

`useOwnerDisplayName` 走 `OwnerNameProvider`（App 提供，值來自那唯一一次 `useOwnerName`），**不是各自再 mount 一次 hook**：`<ChatArea>` 在畫一條對話時不准碰 api client（`ChatArea.quote-no-fetch.test.tsx` 斷言呼叫數為 0），再掛一個 mount-fetch 會直接把它弄紅。沒有 provider 時退回 `fallback`，那也正是「還沒載入」與「載入失敗」的答案 —— 失敗絕不可以偽裝成一個名字。
