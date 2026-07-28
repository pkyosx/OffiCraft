# T-ed38 · Step 4 — 問題框定、範圍與成功條件

基準 commit：`6158b32ec39a4c69b3b874783e1a213f43cd265c`（== `origin/main` == live `git_sha`，server `v0.5.45`）
來源：`baseline.md` + `frontend-baseline.md` + `backend-baseline.md`（兩份 evidence 的關鍵斷言我已親自抽驗，見 §1 標註）

---

## 1. 現況（以直接證據描述）

以下每一條都有原始碼證據；標 ✔️ 者是我（主 agent）親自回讀過原碼、不只採信 sub-agent 回報。

### 1.1 成員列表怎麼排

- server 端排序在 SQL：`server/ocserverd/dal.go:135` `ORDER BY name COLLATE NOCASE`。handler `api_members.go:84-128` **沒有任何 Go 端排序**，**後端也完全沒有「助理特別處理」**。
- ✔️ 前端唯一的 comparator 在 `frontend/src/components/OfficePage.tsx:131-138`，是一個穩定 sort 疊在 server 序之上：
  ```
  .sort((a, b) => (a.role === "assistant" ? 0 : 1) - (b.role === "assistant" ? 0 : 1))
  ```
- ✔️ **置頂條件比「助理」寬**：`frontend/src/api/mappers.ts:141` 是 `role: (w.role_key || "assistant") as RoleKey`，所以 **`role_key` 為空的成員也會被歸成 assistant 而一起置頂**。任務輸入寫的「僅助理角色置頂」因此是**部分正確**。

  **空 `role_key` 是常態值、不是邊角情形**（Iris 的複核者補強，證據比我原本的硬）：
  - `migrations/00001_schema.sql:34` 是 `role_key TEXT NOT NULL DEFAULT ''`
  - `api_tasks_test.go:760` 註解原文：「a plain 正職 leaves it empty」
  - `authz.go:70` 是 `role_key=="assistant" → admin_agent`

  → **現行的「置頂組」實質上是「Mira ＋ 所有沒設角色的人」**，而不是「助理們」。這一條要進 owner gate。

  **同一個 fallback 還有第二個消費者**（我原本漏掉）：`avatarKind.ts:15` 的 `if (m.role === "assistant")` 吃同一個值 —— 無角色成員**不只被置頂，還戴著助理的頭像**。若把置頂組視覺化成一個分區，這批冒牌助理會同時出現在排序與頭像兩處。
- ⚠️ **這是需求沒寫、但必然被改到的行為**：✔️ `OfficePage.tsx:236-238` 的預設選中是 `roster[0]` —— **改排序就等於改「桌機一進辦公室預設打開誰的聊天室」**。這一點交由 owner 拍板，不自行決定（見 §5-Q2）。
- 現行排序**沒有任何測試護欄**（前端 evidence D 節）。這代表改動它時沒有既有測試會告訴我弄壞了什麼，測試策略必須自己補齊。

### 1.2 紅燈（unread）怎麼來

- 全鏈是 **server 算好、FE 純 passthrough**：`domain.go:411-425 UnreadCounts`（chat_read watermark 的反相計數）→ `api_members.go:95-109` 注入 → `wire.ts:30` → `mappers.ts:213` → `types.ts:119` → `useMembers.ts` → `MemberCard.tsx:111-115`。
- **範圍要窄化**：計數條件是 `m.Recipient == currentActor(r)`，也就是「**收件人 == 呼叫者**」，**不是硬寫死 owner**。chat 是嚴格 1:1（`api_chat.go:107-121`），無廣播。
  → 設計意涵：roster 上的每一個 per-peer 訊號都應該是「**對呼叫者而言**」的，`last_activity_at` 若要加，語意必須跟 `unread_count` 對齊（caller ↔ peer 的互動），否則同一列上兩個訊號各講各的。
- ✔️ 壓制條件實際是 `selected && windowActive`（`MemberCard.tsx:111` + `useWindowActive.ts:16-18`），**不是單純 selected** —— 背景視窗下 selected 列仍會顯示紅燈。

### 1.3 列表怎麼更新（有無輪詢）

- `ROSTER_TOPICS = {member, chat, chat_read, role_def}`（`useMembers.ts:32`），SSE 收到即 **refetch 整份 roster**（`useMembers.ts:77-89`）；單一 EventSource，重連／回前景走 `resyncAll()`（`http.ts:214-296`）。
- **roster 路徑零輪詢**。
  → 「不可引入輪詢」這條發布限制，只要沿用既有 SSE→refetch 鏈路就自動滿足。

  > ⚠️ **更正（Iris 的複核者指出）**：本節原本寫「全 `frontend/src` 只有 3 個 `setInterval`，皆與 roster 無關」。**這句會誤導**——我的盤點只枚舉了 `setInterval`，因此漏掉 `OnboardingBanner.tsx:64-79` 的**遞迴 `setTimeout` 自我排程**，那是一個每輪真的打 `api.getServerSettings()` 的**真實 API 輪詢**。
  > **結論不變**（它打 `/api/settings`、不碰 roster，roster 路徑仍是零輪詢），但原本的句子很容易被下一個人讀成「全前端只有 3 個輪詢」，**那是錯的**。

### 1.4 `last_activity_at` 的資料源

- ✔️ 上線的 `MemberDTO` 是**手寫**的 `server/ocserverd/wire.go:119 memberDTO`（25 欄），**確無** `last_activity_at`；生成型別 `ocapi_gen.go:724 MemberDTO` 只被 `BootstrapResultDTO` 使用。
- ✔️ `spec/openapi.json` 的 `MemberDTO` 帶 **`additionalProperties: false`**，且 `conformance/schema_check.py` 有實作它 → **只改 `wire.go` 不改 spec 一定紅**；加欄必須走 spec-first（root `CLAUDE.md` §13）。
- **可以從現有資料推導，不需要新欄位**：`chat_message.ts`（`00001_schema.sql:72`）搭配 1:1 語意即可 `MAX(ts) GROUP BY peer`。
  - ⚠️ `chat_read.last_read_ts` **不能**當獨立時鐘 —— 它的值是「被讀訊息的 ts」，不是「讀取當下」。
  - ⚠️ 現行 handler 取得 chat 的方式是 `ListChat()` **全表無 LIMIT 拉取**（`dal.go:257-259`）；沿用它做聚合是效能陷阱。現有索引只有 `idx_chat_message_ts`，**無 sender/recipient 索引**。

### 1.5 併行工作與 migration 序號

- PR #12 `feat/member-custom-avatar`（**未合併，目前 CONFLICTING**）與本任務改動面高度重疊：`wire.go`、`dal.go`、`api_members.go`、`spec/openapi.json`、`ocapi_gen.go`。
- migration 現況：`origin/main` 最高 `00039`，`00038` 是**刻意空號**（`server/CLAUDE.md:83` 明令不可補：撞號會讓後 merge 的 DDL 靜默不執行，比跳號更糟），PR #12 已用 `00040`。**本票若需要 migration，安全號碼是 `00041`。**
- **關於 `server/CLAUDE.md:83` 的一則判定（避免被誤讀成 doc↔碼 衝突）**：該段寫「`00038` 已被 PR #12 佔用」，而 PR #12 實際已改用 `00040`。我判定**這不構成兩份權威打架、因此不開卡**，理由可查證：該段自己就寫明了兩條演變分支 ——「本票先落 ⇒ 由 PR #12 改成 00040（那是它的 rebase 責任）」，而 `00039` 已在 `origin/main`、PR #12 也確實已改成 `00040`，**現況正落在 doc 自己預告的狀態上**；doc 的規範性結論（00038 不可補、撞號比跳號更糟）與碼完全相容，過時的只有一句描述中間狀態的現在式。依 §8 護欄我**不改那段文字**，只記錄。若 reviewer 認為這個判定站不住，請直接挑戰——那就該轉成一張卡。

---

## 2. 這是什麼類型的問題

**usability debt + 新功能能力**，不是 bug。現行排序（字母序 + 助理置頂）在成員數少時可用；成員增加後，「誰在等我」與「我剛剛在跟誰談」這兩個 owner 最常用的線索完全沒有被排序反映，owner 必須逐列掃視。

**使用者與情境**：owner 一人，在辦公室頁左欄同時管理多位正職與外包。摩擦點是「找人要用眼睛掃」；完成後可觀察的改善是「需要處理的人自動浮到頂端，且長期重要的人可以固定在最前面」。

---

## 3. 範圍

### In-scope

1. 辦公室左欄**正職成員列表**的排序規則（`OfficePage.tsx` 的 comparator）。
2. 讓「最近互動時間」成為前端可用的資料 —— 具體形狀待技術設計，但**必須走 spec-first**。
3. **手動置頂**：新增／取消，以及多人置頂時的群組內順序。
4. 排序改變後的**選中列穩定性**（不因 live reorder 誤點）。
5. 上述所有行為的測試護欄（現行零護欄）。
6. responsive（390 / 768 / desktop）與鍵盤可及性。

### Out-of-scope（發現但不順手做）

- **外包列表（`OutsourcePanel`）的排序**：現行是「綁定任務 created_ts 新→舊」，是另一套刻意的設計（`frontend/CLAUDE.md` 外包面板節）。需求只講正職成員列表。→ 另列候選。
- **`role_key` 空值被當 assistant 置頂**的既有行為：這是 `mappers.ts:141` 的既有 fallback，改它會影響 role 顯示等其他面。本票只在新 comparator 中處理它的效果，**不動那個 fallback 本身**。→ 另列候選。
- **`unread_count` 壓制條件 `selected && windowActive`**：既有行為，不動。
- **PR #12 的衝突解決**：不是我的票。
- **把 task 側活動（例如任務更新）算進「最近互動」**：需求原文是「再回到**最近交談**對象」，語意明確指向 chat。→ 本票只取 chat 訊息時間；這是有需求依據的局部可逆決定，記錄理由於此，不另外請示。

---

## 4. 主流程與錯誤流程

### 主流程

1. owner 打開辦公室 → 左欄成員列表已按「置頂 → 有紅燈 → 最近交談 → 既有穩定序」排好。
2. 某成員傳訊息進來 → SSE `chat` → roster refetch → 該成員亮紅燈並上浮到未讀組。
3. owner 點進該成員對話 → 已讀水位推進 → SSE `chat_read` → 紅燈滅、該列離開未讀組，落到「最近交談」組的最前面。
4. owner 對某成員執行「置頂」→ 該成員固定在最頂組，不再隨紅燈／時間變動而離開該組。
5. owner 取消置頂 → 該成員回到一般規則決定的位置。

### 錯誤與邊界流程

- **沒有任何互動記錄的成員**（新僱用、從未交談）：必須有穩定 fallback，不可排到隨機位置、不可 NaN 排序。
- **置頂了一個後來被解僱／移出名冊的成員**：置頂清單裡的孤兒 id 必須被安全忽略，不可讓列表壞掉或顯示幽靈列。
- **持久化讀取失敗**（localStorage 被停用／設定讀不到）：必須誠實降級成「無置頂」的一般排序，不可整個列表不渲染。
- **live reorder 撞上正要點擊的那一列**：這是本票最主要的誤操作風險，固定策略由 UX gate 決定。
- **舊 client 打新 server 或新 client 打舊 server**：`last_activity_at` 缺席時排序必須仍然確定且不崩。

---

## 5. 成功條件（逐條可客觀驗收）

對應任務輸入的「預期結果／驗收準則」，改寫成可判定的形式：

| # | 成功條件 | 怎麼判定 |
|---|---|---|
| S1 | 排序優先級為：手動置頂 → `unread_count > 0` → 最近互動時間新到舊 → 既有（助理／姓名）穩定順序 | comparator 的 unit test 針對每一層各給一組資料，斷言輸出順序 |
| S2 | 同權重時有 deterministic tie-breaker，同一組輸入永遠得到同一個順序 | unit test：打亂輸入陣列順序多次，斷言輸出恆等 |
| S3 | 不新增任何輪詢；更新沿用既有 SSE → refetch | 程式碼層面斷言 roster 路徑無新 `setInterval`／`setTimeout` 迴圈；沿用 `ROSTER_TOPICS` |
| S4 | 選中／正要操作的列不因 live reorder 造成誤點 | 具體策略經 UX gate 決定後，寫成 component test（模擬 reorder 期間的點擊）+ 實機操作驗證 |
| S5 | 無互動時間的成員與舊 client（欄位缺席）都有穩定、確定的 fallback | unit test：欄位為 `undefined` / `0` / 缺席三種輸入各驗一次 |
| S6 | 手動置頂可新增、可取消，且重整頁面後仍在 | 實機操作 + component test |
| S7 | 多人置頂時群組內順序有明確規則 | 規則經 UX 確認後寫成 unit test |
| S8 | 390px / 768px / desktop 三種寬度無裁切、溢位、遮擋、focus trap | 三種寬度截圖 + 逐項檢視 |
| S9 | 置頂動作可用鍵盤完成且有焦點回饋 | 實機鍵盤操作驗證 |
| S10 | `bin/ci.sh` rc=0 且輸出最後一行精確為 `[ci] all green` | 實際執行並保留輸出 |

---

## 6. 未確認項目（集中列出，不用推測填空）

繼承自兩份 evidence，加上我自己的：

**技術面**
1. 正式站 `chat_message` 的實際列數與表大小 —— 未確認，無 live DB 存取。這個數字會影響 `MAX(ts) GROUP BY peer` 的查詢方案取捨。
2. 是否有既有的 per-peer 最後訊息 helper 可複用 —— 後端道掃了 `dal.go`／`domain.go`／`api_chat.go` 沒找到，但**未做全 repo 窮舉**。
3. `hub.Publish` 的 audience filter 是否保證 owner 連線一定收到每一則 `chat` topic —— 未逐條驗證。若要把「一定即時亮燈」當硬事實需再查。
4. `/api/members` 實際會回哪些 `kind` —— `dal.go:135` 只排除 `outsource`，未窮舉。
5. PR #12 對 `MemberDTO` 加了哪些欄位 —— 未查。這會影響 rebase 時 spec／wire 的衝突面。
6. 兩份 evidence **都是靜態讀碼，沒有執行任何測試、沒有跑起 app、沒有截圖**。所有「畫面上長怎樣」的敘述都尚未經直接觀察驗證。

**待拍板（不是我能決定的）**
7. 持久化方向：localStorage（單瀏覽器、低難度）vs server-backed（跨裝置、中低難度）。→ owner gate。
8. 選中列固定策略的具體形狀。→ Iris 定 UI 契約 + owner gate。
9. 多人置頂的群組內順序、是否支援拖曳。→ Iris。
10. 改排序連帶改變「桌機預設打開哪個聊天室」（`roster[0]`）是否可接受。→ owner gate。
11. 若走 server-backed，`pinned_member_ids` 的持久化形狀。→ Atlas。
    - 已知落差（供 Atlas 參考）：settings 表有 per-owner 偏好先例（`display.custom_themes` 以 JSON 陣列存在 setting 表、**免 migration**），但 settings 寫入門檻是 `admin_agent`，而 `GET /api/members` 只到 `machine` 地板 —— 若把置頂塞進 settings，讀寫的權限級距不一致，需要 Atlas 判斷該怎麼收。
