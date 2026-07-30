# T-ed38 · Step 5 — 技術影響面與相容性盤點

基準 commit：`6158b32ec39a4c69b3b874783e1a213f43cd265c`（== `origin/main` == live `git_sha`，`v0.5.45`）

---

## 1. 前端改動點

| # | 改動點 | 實際路徑 | 源頭證據 | 性質 |
|---|---|---|---|---|
| F1 | roster comparator | `frontend/src/components/OfficePage.tsx:131-138` | 親讀：唯一 sort，穩定 sort 疊在 server 序上 | **必改** |
| F2 | 預設選中列 | `frontend/src/components/OfficePage.tsx:236-238` | 親讀：`selectedId ? roster.find(...) : roster[0]` | **被連帶影響**，待 owner 拍板是否接受 |
| F3 | `Member` view model 加欄 | `frontend/src/types.ts:119` 附近 | 前端 evidence：目前只有 `unreadCount`，無互動時間 | 必改（若 `last_activity_at` 上 wire） |
| F4 | wire → view 映射 | `frontend/src/api/wire.ts:30`、`frontend/src/api/mappers.ts:213` | 前端 evidence 的 seam 鏈路 | 必改 |
| F5 | mock adapter 同步 | `frontend/src/api/adapter.ts:834` | `frontend/CLAUDE.md`：mock 必須以同規則 live 計算，與 http 行為一致 | 必改 |
| F6 | http adapter | `frontend/src/api/http.ts:333-345` | 同上 | 必改 |
| F7 | 生成 schema | `frontend/src/api/generated/schema.ts` | **生成物，不可手改** —— 由 `npm run gen:api`（`openapi-typescript ../spec/openapi.json`）重生 | 生成 |
| F8 | 置頂狀態的讀寫 seam | 新檔（hook）或既有 settings 路徑 | 見 §4 | 依方向而定 |
| F9 | 置頂動作 UI 入口 | `frontend/src/components/MemberCard.tsx` | 目前列上只有未讀 badge，無 kebab（`frontend/CLAUDE.md` 外包面板節記載「聊聊鈕已移除」） | 待 Iris 定 |
| F10 | i18n 文案 | `frontend/src/i18n/locales/*` + `messageKeys.generated.ts` | `frontend/CLAUDE.md` i18n 節：帶參數文案走 `compose.ts`，靜態葉子才可被主題覆寫；`messageKeys.generated.ts` 是**生成物** | 必改 |

**不需要改的**：`useMembers.ts` 的 SSE 訂閱與 refetch 鏈路 —— `ROSTER_TOPICS = {member, chat, chat_read, role_def}` 已涵蓋所有會影響排序的事件（`useMembers.ts:32,77-89`）。**「不新增輪詢」這條限制沿用既有鏈路即自動滿足**，不需要新增 topic。

---

## 2. 後端改動點（僅在 `last_activity_at` 上 wire 時適用）

| # | 改動點 | 實際路徑 | 源頭證據 | 性質 |
|---|---|---|---|---|
| B1 | spec 先行 | `spec/openapi.json` 的 `MemberDTO` | 親讀：帶 `additionalProperties: false`，且 `conformance/schema_check.py` 有實作 → **不改 spec 必紅** | **必須第一步** |
| B2 | 生成 Go wire types | `server/ocserverd/ocapi_gen.go` | **生成物，不可手改** —— `bash bin/gen-ocapi` 重生；CI step 1g 驗 byte-identical | 生成 |
| B3 | 手寫 response DTO | `server/ocserverd/wire.go:119 memberDTO` | 親讀：25 欄，這才是上線的那一份（`server/CLAUDE.md:15` 明載，且 `api_members.go` 全部 20 個 write 點都用它） | 必改 |
| B4 | roster handler 注入 | `server/ocserverd/api_members.go:95-109`（`unread_count` 注入處） | 後端 evidence | 必改 |
| B5 | 聚合查詢 | `server/ocserverd/dal.go` / `domain.go` | 後端 evidence：`chat_message.ts` + 1:1 語意可 `MAX(ts) GROUP BY peer`；現行 `ListChat()` 全表無 LIMIT（`dal.go:257-259`）不宜沿用 | 必改，**形狀待 Atlas 定** |
| B6 | 索引 | `server/ocserverd/migrations/00041_*.sql`（若需要） | 現有索引只有 `idx_chat_message_ts`，**無 sender/recipient 索引** | 待 Atlas 定 |

**migration 序號**：`origin/main` 最高 `00039`；`00038` 是**刻意空號、不可補**（`server/CLAUDE.md:83`：撞號會讓後 merge 的 DDL 靜默不執行，比跳號更糟）；PR #12 已佔 `00040`。

> ⚠️ **更正（Atlas 2026-07-28 指出，我原本的判斷是錯的）**
> 我原先寫「本票安全號碼 `00041`，兩種先後順序下都安全」。**這是錯的**：我只想到「序號不撞」，沒想到 bare `goose.Up`（無 `WithAllowMissing`）對 **out-of-order** 一樣拒絕起站 —— 若本票先以 `00041` 上線、DB 到版本 41，PR #12 之後才帶入 `00040`，server 會因 missing/out-of-order migration **拒絕啟動**。
> **正解：本票不做任何 migration**，順序依賴因此整個消失。未來真要加索引時，從當時 `main` 的最高號重取，**不預留號碼**。

---

## 3. 相容性

| 面向 | 結論 |
|---|---|
| **對外 DTO 相容** | root `CLAUDE.md` §12 要求「對外 DTO 加欄一律 optional」。`last_activity_at` 必須是 optional／有預設，**不可列入 `required`**（目前 `MemberDTO.required` 只有 `id`、`name`）。 |
| **舊 client 打新 server** | 舊 FE 不認得新欄位 → 忽略，行為不變。✅ 安全。 |
| **新 client 打舊 server** | 欄位缺席 → comparator 必須有 fallback（S5）。**這是必測項，不是理論風險**：座艙與 server 各自更新，兩者版本落差是常態。 |
| **`additionalProperties: false`** | ⚠️ 這條讓「先改 wire.go 試試看」的做法一定紅。順序必須是 spec → gen → wire.go → FE gen。 |
| **既有資料** | 若 `last_activity_at` 從 `chat_message` 推導 → **零資料遷移**。若走新表存置頂 → 新表為空即預設無置頂，**無回填需求**。 |
| **多 runtime / 多機器** | **不適用**。本改動全在 server + 座艙 SPA，不涉及 warden/agent runtime 行為。 |
| **部署順序** | 單一 binary 出貨（server go:embed SPA），FE 與 server 同一顆 binary 一起上 → **無跨服務部署順序問題**。 |
| **權限** | roster 讀取端點 `GET /api/members` 是 `machine` 地板；`last_activity_at` 與 `unread_count` 同樣是「對呼叫者而言」的 per-peer 訊號（計數條件 `m.Recipient == currentActor(r)`），**不新增任何跨 caller 的資料外洩面**——每個 caller 只看得到自己與該 peer 的互動時間。 |

---

## 4. 置頂狀態要存哪 —— 兩個既有先例（這改變了成本估算）

任務輸入把 localStorage 標為「低難度單瀏覽器」、server-backed 標為「中低難度跨裝置」。**盤點後我認為 server-backed 的實際成本比這個標示低**，理由是 repo 裡已經有兩個現成模板：

### 先例 A — dual-layer（server 真相 + 本地 cache）
`frontend/src/api/adapter.ts:608-626`：`displayTheme` / `displayLanguage` / `displayWide` 都存在 settings，`""` = never set，前端保留 localStorage cache/default，登入時 reconcile。原文：

> Server = cross-device truth, reconciled in at login

### 先例 B — 純 server、**刻意丟掉 localStorage**
`frontend/src/hooks/useOrgName.ts:1-12`。原文（重點是它自己寫下的理由）：

> The name lives SERVER-SIDE now (DB org.name, behind /api/settings) so every device sees the same studio name … It replaces the old localStorage-only override (**client cache dropped: the server is now the single source of truth, so a stale per-browser copy could only mislead**).

> Owner-only surface: the whole cockpit is owner-authed, and /api/settings is governance-gated (owner / admin agent, T-6020) — **a plain agent never reaches this write path**.

### 由此得到的兩個修正

1. **我先前擔心的「權限級距落差」不成立。** settings 寫入門檻是 `admin_agent`，但**整個座艙就是 owner-authed**（`frontend/src/api/http.ts` 走 `ownerToken()`），所以座艙寫 settings 沒有障礙。這一點我已查證，會補正給 Atlas，不讓他花時間在一個不存在的問題上。
2. **server-backed 免 migration**：settings 表已有 per-owner 偏好以 **JSON 陣列**存放的先例（`display.custom_themes`），`pinned_member_ids` 可直接沿用同一形狀。

→ 這代表 owner 的取捨不再是「省事 vs 跨裝置」，而更接近「**要不要跨裝置**」這個純產品問題。我會照這個修正後的成本寫進 gate 卡，而不是沿用任務輸入的舊估算。**方向仍由 owner 拍板，我不自行擴大範圍。**

---

## 5. 測試面

- **現行排序零護欄**（前端 evidence）。所有排序行為的測試都是新增，不是修改。
- **命名慣例**（實測既有檔）：`<Component>.<feature>.test.tsx`，例如 `MemberCard.click.test.tsx`、`OfficePage.jump-outsource.test.tsx`、`MemberCard.presence-a11y.test.tsx`。→ 本票應為 `OfficePage.roster-order.test.tsx` 一類。
- 前端測試入口：`npm test`（vitest）、`npm run test:ct`（Playwright component test）、`npm run typecheck`。
- 權威 gate：`bash bin/ci.sh`，判準 **rc == 0 且輸出最後一行精確為 `[ci] all green`**（兩個條件都要）。
- 後端若動 wire：`conformance/` 是 wire 行為的回歸權威，schema 檢查會抓 `additionalProperties` 違規。

---

## 6. 併行 PR #12 的對策

PR #12 `feat/member-custom-avatar`（未合併、目前 **CONFLICTING**）與本票重疊於：`wire.go`、`dal.go`、`api_members.go`、`spec/openapi.json`、`ocapi_gen.go`。

對策（不是拍板，是我打算怎麼做）：
1. 本票**基於 `origin/main` 開發**，不去猜 PR #12 的最終形狀。
2. **本票不做 migration** —— 順序依賴消失（見上方更正）。誰先 merge-ready 誰先落，後落者負責 rebase；`spec/openapi.json` 保留雙方欄位（#12 是 `avatar_url`），生成物一律從合併後的 spec 重生。
3. `spec/openapi.json` 與 `ocapi_gen.go` 的衝突若發生，**從已合併的 source-of-truth 重新生成**，不手工拼接生成內容（這正是手冊 learnings 記過的坑）。
4. 若 PR #12 先合併，我 rebase 後**重跑完整 `bin/ci.sh`** 再交付 —— 舊基底上的綠燈對新基底無效力（手冊 learnings）。

**未確認**：PR #12 對 `MemberDTO` 具體加了哪些欄位（未查）。這會影響 spec 衝突的實際大小，但不影響上述對策。

---

## 7. 本節點的未確認項目

1. 正式站 `chat_message` 實際列數 —— 影響聚合查詢方案取捨，無 live DB 存取。已請 Atlas 提供量級。
2. `hub.Publish` 的 audience filter 是否保證 owner 一定收到每則 `chat` topic —— 未逐條驗證。若不成立，「即時亮燈」會有洞，但**不影響排序正確性**（refetch 時仍會拿到正確資料）。
3. PR #12 對 `MemberDTO` 的加欄內容 —— 未查。
4. 本盤點全部基於靜態讀碼，**尚未執行任何測試或跑起 app**。
