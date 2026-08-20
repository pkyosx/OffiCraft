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

deltaSink 每個 burst 只做一次同步決策；coalesce 留在決策層，不要跨 tick debounce。narrowToHeld 的語意固定為：null = 全量、非空 = 指定項目、空集合 = 只指向其他項目。task topic 仍需全量，因為清單可能新增列；chat、chat_read、roster 在沒有持有項目時可跳過。

只有在單筆回應是清單列的真正 superset 時才可 per-item：

- outsource worker 可按單筆更新，但 owner-unread 述詞要判斷是否真的可能改變；移動到 owner 才需要刷新。
- member 可按單筆更新，且要保留 server unread count；一次事件超過一個 id 就全量。
- task 不可按單筆更新：清單列有 dep_tasks，TaskDTO 沒有；TaskCard 的依賴 chip 以清單列為準。
- useChatUnread 只代表總聊天未讀；只有 chat 與 chat_read 都無持有項目時可跳過，其餘 topic 仍全量。

GET /api/chat?with= 會前進 watermark 並回 chat_read echo；chat 用 from/to 判斷 peer，chat_read 用 reader 判斷 peer。沒有 peer 名稱時走全量路徑。

owner unread 只由共享的 lib/ownerUnread.ts 判定：chat.to 或 chat_read.reader 指向 owner 才能改 owner badge（此述詞只回答「什麼能改 server 上的那個數字」；consumer 要據此跳過 refetch，還要自己持有的數字不是舊的——上一次 fetch 失敗過就不算，見 useChatUnread 的 staleRef）；owner 不算 roster member。每個 burst 有兩個以上候選時一律全量，單筆也不能只看事件數猜測；一則 agent↔agent 加一則給 owner 的事件會在同一 microtask 形成混合 burst，若逐則跳過會吃掉真正需要更新的那一半。

任何 per-item 路徑都先查 `api/dtoParity.ts` 與實際 Go handler，確認單筆 response 是清單列的真正 superset；測試 mock 不得比凍結 wire 慷慨，否則 jsdom 會用 server 不可能回的欄位把壞路徑驗綠。`TaskDTO` 沒有 `dep_tasks` 時，不能把 `narrowToHeld` 順手接回 task；依賴 chip 必須繼續讀有該欄位的清單列。

## 共享設定快照

所有 mount 都經 loadServerSettings，不可各自直接呼叫 settings API。共享層負責 single-flight、快取與 generation；同 tab 的 patch echo、登入變更與明確 refresh 要 invalidate。不要加 TTL，也不要假定跨 tab 即時同步。

舊 GET 不得覆寫已採用的 patch。onboarding: null 是正常終態；running 在 set-password 前要先持久化，輪詢直到 failed/success，讀取錯誤也要繼續輪詢。
