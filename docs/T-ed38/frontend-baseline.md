# T-ed38 · 前端現況驗證（成員列表排序／未讀紅燈／更新機制）

驗證基準 commit：**`6158b32ec39a4c69b3b874783e1a213f43cd265c`**（== origin/main == live server `git_sha` `6158b32`）
worktree：`/Users/ray/.officraft/agents/ow-5f4832973889/worktrees/member-roster-priority-sorting`
方法：純讀原始碼（未執行 app、未跑測試）。所有結論後附 `檔案路徑:行號`。

---

## 斷言 1 — 排序：「僅助理角色置頂、其餘沿用 server 回傳的姓名排序」

### 判定：**部分正確**

正確的部分：前端**只有一個** comparator，作用就是把 `role === "assistant"` 的成員提到前面；其餘完全靠 sort 穩定性沿用 server 序，而 server 序確實是姓名序。

不正確／需修正的部分：置頂條件不是「助理角色」，而是「**`role_key` 是 `assistant` 或 `role_key` 是空字串**」——mapper 對空 `role_key` 做了 `|| "assistant"` fallback，所以**沒有設角色的成員也會被一起置頂**。

### 證據

唯一的 comparator，在 component（不是 hook）內，`frontend/src/components/OfficePage.tsx:128-138`：

```tsx
  // The office lists ONLY real AI assistants — machine-layer members (kind
  // "warden", the telemetry collector) belong to the monitoring/machine view,
  // never the office roster (Seth once mistook a warden row for an intruder).
  const roster = members
    .filter((m) => m.kind === "assistant")
    // 助理(seed assistant 角色)置頂;其餘接在後面。sort 穩定 → 各組內維持
    // ListMembers 已排好的字母序(不必再排一次名字)。
    .sort(
      (a, b) =>
        (a.role === "assistant" ? 0 : 1) - (b.role === "assistant" ? 0 : 1),
    );
```

- 這是 `.filter()` 產出的新陣列，`.sort()` 不會就地弄髒 hook 的 `members` state。
- 全 repo 前端只有這一處對 roster 排序：`frontend/src` 內非測試檔的 `.sort(` 共 8 處，成員列表相關的只有 `OfficePage.tsx:135`（其餘為 RepliesPage / TasksPage / useReplyCards / useOutsourceWorkers / useWorkerCodenames / mock / mappers）。
- hook 端**沒有**任何排序：`frontend/src/hooks/useMembers.ts:48-51` 的 `refetch` 與 `:59-73` 的 mount fetch 都是 `setMembers(next)` 原序落地。
- adapter/http 端**沒有**排序：`frontend/src/api/http.ts:333-345` 的 `listMembers` 是 `wire.map(toMember)`，順序即 wire 順序。

`role` 的來源與 fallback，`frontend/src/api/mappers.ts:139-141`：

```ts
    // role_key is the wire role; view model narrows to the RoleKey union. Fall
    // back to "assistant" (the only M1 role) when the wire leaves it blank.
    role: (w.role_key || "assistant") as RoleKey,
```

wire 端 `role_key` 有 `@default ""`（`frontend/src/api/generated/schema.ts:4183-4187`），所以「空角色」是真的會發生的值 → 這類成員在畫面上會與助理同列被置頂。

server 的姓名排序（前端所稱「沿用 server 序」的真實來源），`server/ocserverd/dal.go:133-136`：

```go
func (d *DAL) ListMembers() ([]Member, error) {
	rows, err := d.db.Query(`SELECT ` + memberColumns +
		` FROM member WHERE kind != 'outsource' ORDER BY name COLLATE NOCASE`)
```

handler 逐列 append、不再重排：`server/ocserverd/api_members.go:110-127`。

### 附帶事實

- roster 先被 `kind === "assistant"` 濾過（`OfficePage.tsx:132`），warden 這類 machine-layer 成員不進左欄。
- **沒有任何既有測試鎖住這個排序**：`OfficePage.*.test.tsx` / `MemberCard.*.test.tsx` 內找不到 order/sort 斷言（grep `order|sort` 於這些檔案 = 0 命中）。也就是說現行順序目前沒有回歸護欄，改排序不會有任何測試自動變紅。

---

## 斷言 2 — unread 紅燈的 seam 鏈路

### 判定：**證實**（`unread_count` 全鏈路 server-computed passthrough；`selected` 壓掉 badge 的條件實際上是 `selected && windowActive`，比敘述多一個條件）

### 逐層鏈路

| 層 | 位置 | 內容 |
|---|---|---|
| server 計算 | `server/ocserverd/api_members.go:92-109` | 非 light 路徑才算：`UnreadCounts(messages, receipts, actor)`，注入 `s.newMemberDTO(m, roleName, s.observedHost(m), unread[m.ID])`（`:126`） |
| wire schema | `frontend/src/api/generated/schema.ts:4207-4211` | `MemberDTO.unread_count: number`（`@default 0`） |
| wire type | `frontend/src/api/wire.ts:30` | `export type WireMember = components["schemas"]["MemberDTO"];`（薄別名，非手抄） |
| mappers | `frontend/src/api/mappers.ts:210-213` | `unreadCount: w.unread_count ?? 0,`（註解明寫 honest passthrough，defaulted-away 讀成 0） |
| types（view model） | `frontend/src/types.ts:109-119` | `unreadCount: number;`＋註解「Honest passthrough — the FE never computes it.」 |
| adapter 契約 | `frontend/src/api/adapter.ts:827-834` | `listMembers(opts?: { light?: boolean }): Promise<Member[]>`；註解點名 light 投影下 `unread_count` 是 honest-empty |
| http | `frontend/src/api/http.ts:333-345` | `GET /api/members`（light 時帶 `?fields=light`）→ `wire.map(toMember)` |
| mock（parity） | `frontend/src/api/mock.ts:914-928` | `unread_count: unreadCountOf(m.id)` live 計算，覆蓋 fixture 靜態 0，與 http 行為一致 |
| hooks | `frontend/src/hooks/useMembers.ts:41-98` | 只搬運，不加工 |
| component | `frontend/src/components/MemberCard.tsx:111-115` | 渲染紅 pill |

### 畫面渲染與壓制條件

`frontend/src/components/MemberCard.tsx:111-115`：

```tsx
      {member.unreadCount > 0 && !(selected && windowActive) && (
        <span className="member-card__unread" data-testid="unread-badge">
          {member.unreadCount > 99 ? "99+" : member.unreadCount}
        </span>
      )}
```

- `count === 0` → 整個不渲染；`> 99` → `"99+"`。
- **壓制條件是 `selected && windowActive`，不是單純 `selected`**。`windowActive` 來自 `frontend/src/components/MemberCard.tsx:26` 的 `useWindowActive()`，定義在 `frontend/src/hooks/useWindowActive.ts:16-18`（`visibilityState === "visible" && document.hasFocus()`）。理由寫在 `MemberCard.tsx:100-107`：視窗被切到背景時開著的對話不再消費已讀（`useChat` 只 peek），未讀是真的在累積，所以 selected 列仍必須顯示。
- 護欄測試把這五條行為釘死：`frontend/src/components/MemberCard.unread.test.tsx:62-124`（offline 仍顯示 / 99+ / 0 不渲染 / selected+focused 壓掉 / selected+背景仍顯示 / 未選取不受 focus 影響）。

`selected` 的來源在 `frontend/src/components/OfficePage.tsx:427-441`：

```tsx
              {roster.map((member) => (
                <MemberCard
                  key={member.id}
                  member={member}
                  selected={
                    !workerPeer &&
                    member.id === (isMobile ? selectedId : (selected?.id ?? ""))
                  }
```

（桌機保留 `roster[0]` fallback 高亮 — `OfficePage.tsx:236-238`；手機無 fallback。外包聊天開啟時整個正職列不亮 — `!workerPeer`。）

### 相關但獨立的第二個 unread 訊號

- 分頁 tab 的區域總計：`OfficePage.tsx:149-155`（`roster.reduce(...)`，載入未 settle 時誠實給 0）。
- 主導覽的辦公室總計走**另一個端點**：`frontend/src/hooks/useChatUnread.ts:32-59`（`api.getChatUnreadCount()`，訂 `OFFICE_TOTAL_TOPICS` = chat / chat_read / member / outsource_worker）。與 roster 的 per-member 計數是兩條線。

---

## 斷言 3 — 更新機制：SSE 觸發 refetch，且**無輪詢**

### 判定：**證實**

### SSE topic 訂閱清單與 refetch 位置

`frontend/src/hooks/useMembers.ts:24-39`：

```ts
const ROSTER_TOPICS = new Set(["member", "chat", "chat_read", "role_def"]);

// The LIGHT topic set (T-cf91): identity only (name + role), so chat / chat_read
// are DELIBERATELY excluded …
const ROSTER_TOPICS_LIGHT = new Set(["member", "role_def"]);
```

選用與 refetch，`frontend/src/hooks/useMembers.ts:41-95`：

```ts
  const topics = light ? ROSTER_TOPICS_LIGHT : ROSTER_TOPICS;
  …
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topics.has(topic)) {
        api.listMembers(light ? { light: true } : undefined)
          .then((next) => { if (alive) { setMembers(next); setError(false); } })
          .catch((e) => console.warn("useMembers: SSE refetch failed", e));
      }
    });
```

- 機制 = **reconcile-by-refetch**：收到 topic 後**重新 GET 整份 roster**，不 merge event payload（`useMembers.ts:1-7` 的檔頭註解明寫）。
- 辦公室左欄用的是**非 light**（`OfficePage.tsx:67` 的 `useMembers()` 無參數）→ 吃全套 `ROSTER_TOPICS`，含 `chat` / `chat_read`。

server 端確實會發這兩個 topic：
- 送出訊息：`server/ocserverd/api_chat.go:397` `s.hub.Publish("chat", "patch", "chat", …)`
- 標記已讀：`server/ocserverd/api_chat.go:71` `s.hub.Publish("chat_read", "patch", "chat_read", …)`
- 其他也發 `chat` 的路徑：`api_replycards.go:228`、`api_tasks.go:933`、`api_tasks.go:1407`、`api_webhooks.go:352`、`api_roles.go:344/353`。

### 傳輸層：單一 EventSource + 兩種 resync

`frontend/src/api/http.ts:184-296`：
- 全 SPA **一條** `EventSource`（`http.ts:250`），client-side fan-out 給所有 subscriber（`http.ts:270-274`）；理由是舊的 per-subscriber 連線打爆 Chromium 6 連線上限。
- 斷線重連的補洞：`es.onopen` 第二次以後 → `resyncAll()`（`http.ts:262-266`），對 `SSE_RESYNC_TOPICS`（`http.ts:214-227`，含 member/chat/chat_read）每個 topic 合成一次 delta 打給所有 subscriber → 各 hook 各自 refetch。
- 回前景補洞：`visibilitychange` + `window focus` → 同一支 `resyncAll()`（`http.ts:288-296`）。

### 輪詢（polling / setInterval）盤查

**成員列表路徑上完全沒有輪詢。** 全 `frontend/src`（排除 `.test.`）只有 3 個 `setInterval`，都與 roster 無關：

| 位置 | 用途 | 是否為資料輪詢 |
|---|---|---|
| `frontend/src/components/RepliesPage.tsx:90-95` | 30s tick 更新「已等你」計時＋24h client-side prune | 否（只 `setNowTs`，不打 API） |
| `frontend/src/components/TasksPage.tsx:82-88` | 30s tick 更新「已歷時」 | 否（只 `setNowTs`） |
| `frontend/src/components/SettingsPage.tsx:905` | 觸發自我升級後 2s 輪詢 `/api/version` 等重啟 | 是，但只在「按下升級後」的暫態，且 90s deadline（`:906-912`）；與 roster 無關 |

`OfficePage.tsx` / `MemberCard.tsx` / `useMembers.ts` / `useChatUnread.ts` 內皆無 `setInterval` / `setTimeout` 輪詢。

---

## 額外蒐集（設計輸入，非斷言）

### A. 成員列表元件檔案與職責

| 檔案 | 職責 |
|---|---|
| `frontend/src/components/OfficePage.tsx` | 左欄容器：roster 過濾＋排序（`:131-138`）、tab 切換（`:59`、`:166-169`）、選取狀態解析、chat/detail pane 派發、外包區 |
| `frontend/src/components/MemberCard.tsx` | 單一成員列：整列 = 聊天入口（`:34-45`）、avatar = 詳情入口（`:51-66`）、presence 行（`:87-89`）、未讀 pill（`:111-115`） |
| `frontend/src/components/OfficeSidebarTabs.tsx` | 正職／外包文字頁籤＋各自未讀總計 badge＋「N 人」副標 |
| `frontend/src/components/PresenceBadge.tsx` + `LifecycleDot.tsx` | 五態 presence 點與角色副標（三畫面共用，見 `frontend/CLAUDE.md` presence 節） |
| `frontend/src/components/OutsourcePanel.tsx` | 外包列（另一組，不在正職 roster 內） |
| `frontend/src/hooks/useMembers.ts` | roster 資料源＋SSE reconcile |
| `frontend/src/hooks/useWindowActive.ts` | 「owner 真的在看嗎」→ 未讀壓制的第二個條件 |
| `frontend/src/api/{wire,mappers,adapter,http,mock}.ts` | seam 各層（見斷言 2 表） |
| `frontend/src/components/office.css:336-434` | `.member-card` 版面／hover／selected／未讀 pill 樣式 |

### B.「目前選中的是哪一列」存在哪裡、怎麼傳遞

- **存在 URL hash，不是 component state。** `OfficePage.tsx:77-80`：

```tsx
  const [route, setRoute] = useHashRoute();
  const selectedId = route.chatId ?? "";
  const detailId = route.detailId ?? null;
  const workerDetailId = route.workerId ?? null;
```

- 寫入：`OfficePage.tsx:88-89` 的 `setSelectedId` → `setRoute({ page:"office", chatId: id || undefined })`；由 `MemberCard` 的 `onChat` 觸發（`OfficePage.tsx:440`）。
- 路由解析／序列化：`frontend/src/lib/hashRoute.ts:54`（型別）、`:183`（parse `chat/<id>`）、`:243-244`（serialize）。
- 桌機的「沒有明確選取時 fallback 到 `roster[0]`」規則：`OfficePage.tsx:236-238`。**注意**：這條 fallback 直接吃 `roster[0]`，所以**任何改動排序都會改變桌機預設打開哪一間聊天室**——這是排序改動的隱性連帶影響。
- 傳遞給列：`OfficePage.tsx:435-438` 的 `selected` prop（見斷言 2）。
- 頁籤（正職／外包）是**不持久化**的純 component state：`OfficePage.tsx:56-59`。

### C. 既有相關測試與慣例

- 框架：**Vitest + @testing-library/react + jsdom**（`frontend/vite.config.ts:14-15`，`package.json:16` `"test": "vitest run"`）。
- **第二套**：Playwright component test（`package.json:17` `"test:ct"`，`frontend/playwright-ct.config.ts`），檔名 `*.ct.spec.tsx`，放 `frontend/visual-guards/`，與 vitest 的 `*.test.tsx` **glob 互斥**（`vite.config.ts:20-30` 註解明講兩個 runner 必須擁有不相交的 glob）。roster 相關的視覺護欄：`frontend/visual-guards/office-sidebar.ct.spec.tsx`（story 在 `visual-guards/stories/OfficeSidebarStory.tsx`）。
- 檔名慣例：`<Component>.<關注點>.test.tsx`（同一元件多個關注點各一檔），hook 為 `<hook>.<關注點>.test.ts`。
- 直接相關的現有檔：
  - `frontend/src/components/MemberCard.unread.test.tsx`（未讀 badge 五條不變量）
  - `frontend/src/components/MemberCard.click.test.tsx`（整列 vs avatar 兩個點擊目標）
  - `frontend/src/components/MemberCard.presence-a11y.test.tsx`
  - `frontend/src/components/OfficePage.roster-area.test.tsx`（透過**真的 OfficePage + mock adapter** 驗 tab 計數／未讀總計／招攬按鈕路由）
  - `frontend/src/components/OfficePage.jump-outsource.test.tsx`、`OfficePage.member-detail-backto.test.tsx`、`OfficePage.wake-undispatched.test.tsx`
  - `frontend/src/hooks/useMembers.light.test.ts`（釘住 light hook **不**訂 chat topics）
  - `frontend/src/api/mock.unread.test.ts`（mock 的 unread 規則）
- 測試風格重點（照 `OfficePage.roster-area.test.tsx:12-34`）：`__resetMock()` / `__injectMockChat` / `__injectMockOutsourceWorker` 注入資料、`window.location.hash = ""` 重置路由、`Element.prototype.scrollIntoView = vi.fn()` stub、以 `data-testid` + `findByTestId/waitFor` 斷言。單元層（`MemberCard.unread.test.tsx:21-60`）則用手寫 `mkMember()` fixture + `I18nProvider` 包裹。
- ⚠️ **目前沒有任何測試鎖住 roster 排序**（見斷言 1 附帶事實）。

### D. localStorage 既有先例

有先例，但**沒有共用的通用 helper**——只有兩個各自封裝的家：

1. `frontend/src/api/auth.ts:1-59` — owner JWT，key `oc_token`。檔頭自稱「Single source of truth for the localStorage token key」；`:33` read、`:46` write、`:59` remove。**沒有 try/catch**。
2. `frontend/src/i18n/index.tsx:43-101` — 顯示偏好的 **dual-layer**（localStorage = pre-auth 快取，`/api/settings` = 跨裝置真相）。keys：`oc.language`（`:50`）、`oc.theme`（`:51`）、`oc.wide`（`:57`）。封裝在**模組私有**、未 export 的四支：`readStored<T>()`（`:59-67`）、`readStoredTheme()`（`:72-81`）、`readStoredWide()`（`:86-92`）、`writeStored()`（`:94-101`）。共同紀律：**全部包 try/catch，localStorage 不可用時落回預設、不假裝有持久化**。

反例（刻意不用 localStorage，設計時可參考的裁定）：
- `frontend/src/lib/chatDraftStore.ts:10-12` — 聊天草稿刻意**不**進 localStorage（每次按鍵序列化會吃掉 ~5MB 配額）。
- `frontend/src/hooks/useOrgName.ts:11`、`useOwnerName.ts:11` — 舊的 localStorage-only override **已移除**，改成 server 唯一真相。
- `frontend/src/components/useRelocateMachine.tsx:19` — 曾評估 localStorage 持久化，判定不值得。

→ 若 T-ed38 要做「手動置頂」的本機持久化，**沒有現成共用 helper 可直接呼叫**；最貼近的既有模式是 i18n 那組（模組私有 try/catch 包裝 + `oc.` 前綴 key）。但請注意 repo 的既有走向是「顯示偏好逐步收斂到 server `/api/settings`」——localStorage-only 是否可接受屬**產品/架構裁定，非本次可從碼上決定**（見「未確認」）。

### E. 成員列上現有的動作入口（未來「置頂」要掛哪）

現況：**成員列上沒有 kebab、沒有 hover 浮出的動作、沒有右鍵選單。**

- `MemberCard.tsx:34-116` 全部的互動元素只有兩個：整列 div（`role="button"`，`onClick={onChat}`＋Enter/Space 鍵盤路徑，`:34-45`）與巢狀的 avatar `<button>`（`onClick` 內 `stopPropagation()` → `onOpenDetail`，`:51-66`）。未讀 pill **刻意無自己的 handler**（`:108-110` 註解）。
- 全 `frontend/src` grep `kebab` = 0 命中；grep `onContextMenu` = 0 命中。
- CSS 面 `office.css:346-348` 的 `.member-card:hover` 只換背景色，沒有任何 hover-reveal 的子元素。
- 舊的「聊聊」按鈕已於 2026-07-13 由 Seth 拍板移除，那個 flex-end 位置**現在只剩未讀 badge**（`frontend/CLAUDE.md` 外包面板節；`MemberCard.tsx:92-97` 註解同述）。
- repo 內既有的「列上下拉選單」視覺語彙可重用：`TaskCard.tsx:928-963`（優先權 chip → `aria-haspopup="menu"` + `role="menu"` 的 `.task-card__menu-pop` popover，item 為 `role="menuitem"`）與 `TaskCard.tsx:966-1000`（狀態 chip 同款）。另有 `OutsourceCapPopover.tsx` 與 `OfficePage.tsx:185-192` 的 outside-click 關閉樣板。
- 註記：`frontend/CLAUDE.md` presence 節有一條裁定「角色 ⚙ 已從 roster 列移到成員詳情面板，roster 列維持純 presence 行」（`MemberCard.tsx:84-86` 有對應註解）。在列上新增動作入口會直接碰到這條既有裁定 → 屬 owner 拍板範圍。

---

## 未確認 / 從碼上無法確定

1. **「置頂」狀態要存哪裡（localStorage vs server settings）**：碼上兩種先例都有（D 節），且既有走向是往 server 收斂（`useOrgName.ts:11`）。這是產品/架構決策，不是碼能回答的。要確定需 owner 裁定；若走 server，還需先改 `spec/openapi.json`（repo `CLAUDE.md` §13 wire 已凍結）。
2. **「最近互動」的時間戳目前不存在於 member wire**：`MemberDTO`（`frontend/src/api/generated/schema.ts` MemberDTO 區塊）只有 `unread_count`，**沒有** last-message / last-interaction 時間欄；`Member` view model（`frontend/src/types.ts`）同樣沒有。要做「最近互動排序」必須新增 wire 欄位（→ 先過 spec）或前端另外拉 chat 資料聚合。我**沒有**在碼上找到任何現成可用的「最近互動時間」來源——這點請設計階段當作硬約束，不要假設它已經有了。
3. **實際畫面順序**：本次只讀碼、**未執行 app、未跑任何測試、未截圖**。「畫面上實際長怎樣」未經直接觀察驗證。
4. **server `chat` topic 的 audience 是否讓 owner 一定收到**：我確認了 `api_chat.go:397` 會 Publish `chat`，但沒有逐一驗證每條 Publish 的 audience filter 對 owner 連線都成立。若要把「一定會即時亮燈」當成硬事實，需要再讀 `hub.Publish` 的 audience 參數與 `audienceOwnerOnly()` / `audienceAll()` 的實際涵蓋。
5. **`kind === "assistant"` 之外的成員是否可能出現在正職列**：`OfficePage.tsx:132` 濾掉了非 assistant kind，這點是碼上確定的；但 server 端還有哪些 kind 會出現在 `/api/members`（`dal.go:135` 只排除 `outsource`）我沒有窮舉。

---

## 給後續設計的三個提醒（基於上面已證實的事實）

1. 改排序 = 同時改桌機預設開啟的聊天室（`OfficePage.tsx:236-238` 的 `roster[0]` fallback）。
2. 目前排序沒有任何測試護欄，新排序邏輯要自帶測試（慣例見 C 節），否則後人「順手整理」時不會有東西變紅。
3. 現行「置頂」的實作位置若沿用 `OfficePage.tsx:135` 那個 comparator，要注意 `role` 的空值 fallback（`mappers.ts:141`）已經讓「無角色成員」意外落在助理組——若新規則要疊在舊規則之上，這個既有偏差會一起被繼承。
