# T-ed38 · Step 8 — 技術設計與實作計畫

基準 commit：`6158b32`
**方向已核可**：owner `rc-e925695724e3` → server-backed；owner `rc-563734cd294e` → 本票順手修兩處 doc。
每個改動點都標了它追溯到哪一條核可決定。

---

## 1. 排序 comparator 完整規格

### 1.1 位置與形狀

**抽成純函式**，不留在 component 內聯：
- 新檔 `frontend/src/lib/rosterOrder.ts` — 匯出 `compareMembers(pinIndex) => (a, b) => number`
- `OfficePage.tsx:131-138` 改為呼叫它

理由：現行 comparator 內聯在 component、且**零測試護欄**。抽成純函式才測得到（`lib/` 已有 `composerKeys.ts`、`duration.ts` 等純函式先例）。

### 1.2 四層規則（逐層短路）

```
compareMembers(pinIndex)(a, b):
  1. pinRank    : pinIndex.get(id) ?? +Infinity        // 小的在前
     └ 兩者都 pinned → 直接回傳 pinIndex 差值，【到此為止，不再往下比】
  2. hasUnread  : (unreadCount > 0) ? 0 : 1            // 0 在前
  3. lastActive : -(lastActivityAt ?? 0)                // 大的在前 → 取負
  4. roleRank   : (role === "assistant") ? 0 : 1        // 既有行為，不動
  5. tie-break  : id 的字典序（見 1.4）
```

**第 1 層的短路是契約 2 的要求，不是最佳化**：Iris 定「置頂組內按置頂時間新→舊，**不受 unread／最近互動攪動**」。若兩者都 pinned 卻繼續往下比 unread，就會把 owner 剛親手固定的順序又還給自動規則 —— 她的原話是「自我否定」。

`pinIndex` = `Map<memberId, arrayIndex>`，由 `pinned_member_ids` 陣列建。**陣列順序即顯示順序**（Atlas 契約），**新置頂 unshift 到開頭**（Iris 契約「新置頂在上」）→ 兩位架構師的契約在這裡咬合，不需要另存置頂時間戳。

### 1.3 第 3 層不用 `name`，第 4 層保留既有行為

第 4 層 `role === "assistant"` **原樣保留**，包含 `mappers.ts:141` 的 `(w.role_key || "assistant")` fallback 效果（空 `role_key` 也算 assistant）。
→ 這是既有行為，改它會連帶影響 `avatarKind.ts:15` 的頭像判定（Iris 的複核者指出的第二消費者）。**out-of-scope，不動。**

第 4 層之後**不比 `name`**：server 已 `ORDER BY name COLLATE NOCASE`，而 `Array.prototype.sort` 在 ES2019 起保證穩定，所以同層同值者自然維持 server 的姓名序。**再比一次 name 是多餘的第二份真相**（而且 JS 的字串比較與 SQLite 的 `COLLATE NOCASE` 規則不同，兩者會在非 ASCII 名字上分歧）。

### 1.4 tie-breaker（**已修正 —— 原版本自相矛盾**）

> 🔴 **這一節原本與 §1.3 互相矛盾，是我（O-3）寫錯的。** 原版寫「第 5 層用 `id` 字典序」，但 §1.3 又說「同層同值者維持 server 姓名序」—— 有了 id 層，1–4 層同分者就是 id 序**不是**姓名序，兩者不能並存。實作者照原版做了並主動標記出來，我拿去跟 Iris 確認後修正如下。**文件自相矛盾是寫文件的人負責，不是實作者的問題。**

**最終形狀**（Iris 2026-07-28 確認）：

```
5a. a.name.toLowerCase()  vs  b.name.toLowerCase()   // 見下方警告：不要用 localeCompare
5b. name 完全相同 → a.id vs b.id（字典序）
```

**為什麼要比 name 而不是直接跳 id**：同名是罕見情形，「1–4 層同分但不同名」才是常見情形。純 id 兜底等於為了罕見情形，把常見情形的姓名序破壞成使用者看不出規律的順序。

**Iris 補的、比我原本更強的理由**：
> 你原本的 §1.3（靠穩定 sort 繼承 server 姓名序），等於把「畫面順序正確」建立在「server 一定會照姓名排」這個**沒寫進任何契約的假設**上。`ORDER BY name COLLATE NOCASE` 今天在，但它不是對前端的承諾 —— 哪天有人加分頁、換 collation、或為了效能改索引，前端的順序就會無聲跟著變，而且**不會有任何測試變紅**。
> 加了第 5 層比 name 之後，**前端的排序變成自足的**：server 回什麼順序都不影響最終結果。這不只是修一個 tie-breaker，是把一條隱形的跨層依賴變成明碼。

→ 這也讓我原本「JS 與 SQLite collation 會分歧」的顧慮**問錯了方向**：前端自己比 name 之後，**server 的順序根本不再進入結果**，兩邊一不一致就不再是問題。

> 🔴 **必須用 `toLowerCase()` + 單純的 `<` / `>`，不可以用 `localeCompare`**（Iris 指出，我沒問到這一點）：
> `localeCompare` 的結果**依賴 runtime 的 ICU 資料**，不同瀏覽器／不同 Node 版本對同一組非 ASCII 名字可能給出不同順序。那會讓 S2 **在 jsdom 下綠燈、在真實瀏覽器上卻是另一個順序** —— 測試無法保護的那種不決定性，比排序不漂亮糟得多。
> `toLowerCase()` 是 locale-independent 的（`toLocaleLowerCase()` 才是 locale-aware，**別用那個**），純 code-unit 比較，跨環境完全一致。代價是非 ASCII 名字的順序不一定符合該語言直覺 —— **首要目標是決定性，不是完美的 locale 排序**。
> 佐證：`localeCompare` 在 `frontend/src` 是 **0 命中**，用它等於引進新慣例。

**驗收方式**（S2）：把輸入陣列打亂多次餵進 comparator，斷言輸出恆等。`(name, id)` 是全序（id 唯一），所以必然收斂。
⚠️ **S2 的 fixture 必須包含一對在 1–4 層完全同分的成員**，否則測試根本走不到第 5 層 —— 實作者的控制組驗證實測到這個漏洞（刪掉第 5 層，S2 照樣綠）。

### 1.5 fallback 語意（S5）

| 輸入 | 處理 | 理由 |
|---|---|---|
| `lastActivityAt` 為 `undefined`（舊 server 沒這欄） | 視為 `0` | 全體都 0 → 第 3 層整層失效 → 退回現行行為。**安全降級，不是壞掉。** |
| `lastActivityAt` 為 `0`（從未互動） | 排在有互動者之後、但仍在第 4 層規則內 | 不沉底、不置頂 |
| `unreadCount` 為 `undefined` | 視為 `0`（無未讀） | 與現行 FE passthrough 一致 |
| `pinned_member_ids` 讀取失敗 | 視為 `[]` | Atlas 契約：「settings 讀失敗就誠實降級 `[]`」 |
| pin 陣列含已解僱／未知 id | render 時與 active roster 取交集後忽略 | Atlas 契約：**不做隱藏 cleanup write** |

⚠️ **`?? 0` 不可寫成 `|| 0`**：`0` 是合法值（從未互動），`||` 會把它和 `undefined` 混為一談 —— 這裡剛好結果相同，但語意不同，且日後若 sentinel 改變會靜默出錯。

---

## 2. P2：當前對話不得被排序抽換（Iris 決策 1a）

**這是本票唯一一處導航語意的改動，也是最容易被實作者「順手做過頭」的地方。**

現況（Iris 親自回讀驗證）：`OfficePage.tsx:236-238` 的 `selected` 是**每次 render 重算的 derived const**，全檔零 `useMemo`／零 `useCallback`。

**改法**：把「空選擇時的預設對象」改成**首次拿到非空 roster 時解析一次、之後固定**（`useRef` 記住解析出的 id，roster 重排不重算）。

**三條明確的邊界**（違反任一條就是做過頭）：
1. **不動 T-661b 那條收窄** —— 「明確 chatId 解析不到時不得靜默落到 `roster[0]`」原封不動。只改「空選擇」那一支的**求值時機**，不碰**適用範圍**。
2. **不寫回 URL hash** —— 空選擇仍是空，hash 語意不變。
3. **不改 `setSelectedId` 的三個呼叫點**（:440/:455/:498）。

**驗證這個改動在現行排序下行為不變**：靜態 comparator 本來就不會換首位，所以這一步單獨看是 no-op。它是**為新排序預先拆掉的引信**。

> ⚠️ **實作第一件事：驗證 P2 的後果鏈**（Iris 明說那是推論、不是實測，並給了驗法）。
> 先**只**把 comparator 換成含 unread 的版本、不做任何其他改動，在未選取狀態下發一則訊息給非首位成員，看畫面會不會自己換走。
> **不論結果都要記進 `verification.md` 並回報 Iris。** 若沒發生 → 表示某處有我們沒讀到的 memo 或早退，那 P2 的改動理由要重寫，不能照抄。

---

## 3. 前後端契約（server-backed，Atlas 定稿）

### 3.1 wire 改動（**必須 spec-first**）

`spec/openapi.json`（**先改這裡，否則 conformance 必紅** —— `MemberDTO` 帶 `additionalProperties: false`）：

| DTO | 欄位 | 型別 | 語意 |
|---|---|---|---|
| `MemberDTO` | `last_activity_at` | optional `number`（epoch 秒） | caller ↔ peer 任一方向最後一則 `chat_message.ts`；無互動 = `0` |
| `SettingsDTO` | `pinned_member_ids` | optional `string[]` | 回應**永遠是 array**，缺 key = `[]` |
| `SettingsUpdateDTO` | `pinned_member_ids` | optional `string[]` | 省略 = 不改；`[]` = 清空 |

**schema description 要明寫**（Atlas 要求）：`last_activity_at` 的 `0` **可能是無互動，在 `?fields=light` 則是未計算**。這兩種情況在 wire 上無法分辨，必須寫在 description 裡，否則下一個人會把 light 的 0 當成「真的沒互動過」。

**兩處都不得加進 `required`**（root `CLAUDE.md` §12：對外 DTO 加欄一律 optional）。`MemberDTO.required` 維持只有 `id`、`name`。

### 3.2 生成物（**不可手改**）

改完 spec 後依序重生，三份必須逐字一致：
```
bash bin/gen-ocapi                                   # → server/ocserverd/ocapi_gen.go
cd frontend && npm run gen:api                       # → frontend/src/api/generated/schema.ts
```
CI step 1g 會驗 `ocapi_gen.go` 從凍結 spec byte-identical 重生；step 4 驗 FE schema drift。

### 3.3 server 端

**新增 DAL helper**（Atlas 指定形狀）：
```
ListMemberChatStats(actor) → map[peerID]MemberChatStats{ UnreadCount, LastActivityAt }
```
一次 SQL 同時算兩者：
```sql
WHERE sender = :actor OR recipient = :actor
peer     = CASE WHEN sender = :actor THEN recipient ELSE sender END
activity = MAX(ts)
unread   = SUM(CASE WHEN recipient = :actor AND ts > COALESCE(last_read_ts, 0) THEN 1 ELSE 0 END)
           -- LEFT JOIN chat_read ON (reader_id, peer_id)
GROUP BY peer
```

**它要取代**（不是並存）現行 roster handler 的 `ListChat() + ListChatReads() + UnreadCounts` 路徑。
> Atlas 原話：「不沿用 `ListChat()`，也不要在它旁邊再加第二條 activity query。」
> 這連帶修掉現行「全表無 LIMIT 搬進 Go」的問題（`dal.go:257-259`）。

**不加索引、不做 migration**（Atlas 依 live DB 實測決定：1,089 列 / 632 相關 / 10 peers / 8.55 MB，單掃描約 1 ms）。
> 「以這個量級，本票先不加 sender/recipient index；10× 仍有足夠餘裕，而且已比現行全列搬入 Go 輕。之後用實際 latency/rows 決定索引或 summary table，不先造第二真相。」

**⚠️ 不做 migration 是刻意的**：本票若佔用 `00041`，PR #12 之後帶入 `00040` 會讓 bare `goose.Up` 因 out-of-order 拒絕起站。不做 migration 就沒有順序依賴。

### 3.4 授權

- `GET /api/members`：地板不變（`machine`）。`last_activity_at` 與 `unread_count` 同為 caller-relative，**不新增任何跨 caller 的資料外洩面**。
- `GET/PATCH /api/settings`：`requires=admin_agent`（T-6020）不變，座艙以 owner token 走得通（`useOrgName.ts` 檔頭已載明）。
- **不新增端點、不新增路由表列** → 不觸發 `authz_surface_gate_test.go` 的清單維護。

### 3.5 寫入語意

整組原子 replace、last-write-wins（與既有 settings 一致）。**server 不做 merge** —— Atlas：「否則跨裝置同時改動時順序無法定義」。
驗證：空字串或重複 id → 422，**全部驗完才寫**。

---

## 4. 前端實作點

| # | 檔案 | 動作 | 追溯 |
|---|---|---|---|
| F1 | `lib/rosterOrder.ts`（新） | comparator 純函式 | §1 |
| F2 | `components/OfficePage.tsx:131-138` | 改呼叫 F1 | §1.1 |
| F3 | `components/OfficePage.tsx:236-238` | 空選擇預設對象改為 ref 固定 | §2 / Iris 1a |
| F4 | `api/wire.ts`、`api/mappers.ts`、`types.ts` | `lastActivityAt` 沿 seam 補一層 | §3.1 |
| F5 | `api/adapter.ts`、`api/http.ts` | settings 的 `pinnedMemberIds` 讀寫 | §3.1 |
| F6 | `api/adapter.ts`（mock） | mock 以同規則 live 計算，與 http 行為一致 | `frontend/CLAUDE.md` |
| F7 | `hooks/`（新 hook 或擴充既有 settings hook） | 置頂讀寫，比照 `useOrgName` 樂觀更新 + 失敗回退 | §3.5 |
| F8 | `components/MemberDetailPanel*` | 置頂／取消置頂入口 | Iris 3a |
| F9 | `components/OfficePage.tsx` + `office.css` | hairline 分隔 + `role="group"` wrapper | Iris 3b–3e |
| F10 | `i18n/locales/*` + `gen:msgkeys` | 三語文案（靜態葉子，不寫模板） | `frontend/CLAUDE.md` i18n 節 |
| F11 | `frontend/CLAUDE.md` 兩處 | 修正過時描述 | **owner `rc-563734cd294e`** |

**hairline 的三條限制**（Iris）：不複用 `.doc-md hr` 的 class（只沿用粗細與顏色值）；**`margin: 18px 0` 不照抄**，調完**實際截圖看一眼**再定值；不碰 `office.css:201-226` 的 baseline divider。

**四條邊界規則**（Iris 3e）：零置頂 → 不渲染 hairline 也不渲染 group wrapper；**全部置頂 → 也不渲染 hairline**（判斷條件是「兩組都非空」）；hairline 掛在**非置頂組第一張卡的上緣**；置頂組內順序照契約 2。

---

## 5. 測試策略

現行排序**零護欄**，所以全部是新增。命名照既有慣例 `<Component>.<feature>.test.tsx`。

| 測試 | 檔案 | 覆蓋 |
|---|---|---|
| comparator 四層 | `lib/rosterOrder.test.ts` | S1：每層各一組資料驗順序 |
| tie-breaker 決定性 | 同上 | S2：打亂輸入多次，輸出恆等 |
| fallback | 同上 | S5：`undefined` / `0` / 缺席三種輸入 |
| 置頂組不受 unread 攪動 | 同上 | Iris 契約 2 的短路 |
| 孤兒 pin id | 同上 | 不崩、不顯示幽靈列 |
| 分組渲染邊界 | `OfficePage.roster-order.test.tsx` | 零置頂／全置頂／混合三態的 hairline 與 group wrapper |
| 當前對話不被抽換 | `OfficePage.selected-stability.test.tsx` | §2：模擬 refetch 換掉排序首位，斷言顯示對象不變 |
| 置頂新增／取消 | `MemberDetailPanel.pin.test.tsx` | S6 |
| settings 失敗降級 | 同上 | 讀失敗 → `[]`；寫失敗 → 回退 |
| server 端 stats | Go test + `conformance/` | 新 DAL helper 的 unread 與 activity 計算 |

**控制組驗證（V8，必做）**：至少對 comparator 與「當前對話不被抽換」兩支，**刻意把實作改壞一次、確認測試真的變紅**，再改回來。理由：這兩支測的都是「某件事不發生」，而斷言「不發生」的測試最容易寫成恆真。

---

## 6. 失敗模式與還原路徑

| 失敗模式 | 偵測 | 還原 |
|---|---|---|
| spec 改了但生成物沒重生 | CI step 1g / step 4 drift gate | 重跑 `bin/gen-ocapi` + `npm run gen:api` |
| 新 SQL 算錯 unread（破壞既有紅燈） | 既有 conformance + 新 Go test | 整個 helper 可回退到原 `ListChat()` 路徑（**這是取代不是刪除，原函式仍在**） |
| P2 改動誤傷 T-661b 的收窄 | `OfficePage.jump-outsource.test.tsx`（既有） | 該測試變紅即表示碰到了不該碰的範圍 |
| 排序把 owner 慣用的人推走 | 實機 UX 驗證 | comparator 是純函式，單點回退 |
| PR #12 先合併造成衝突 | rebase 時 | 生成物**從合併後的 spec 重生**，不手工拼接；**rebase 後重跑完整 `bin/ci.sh`**（舊基底的綠燈對新基底無效力） |

**整體還原路徑**：本票**不做 migration、不新增端點、不刪除既有函式**，所以 revert 單一 commit 即可完全回到現狀，無資料殘留、無 schema 殘留。

---

## 7. 實作順序（給實作者的執行序）

1. **先驗 P2 的後果鏈**（§2 的警示框）——結果進 `verification.md`。
2. `spec/openapi.json` 改兩個 DTO → 重生兩份生成物 → 確認 CI 的 drift gate 過。
3. server：`ListMemberChatStats` + 取代 roster handler 的舊路徑 + Go test。
4. 前端 seam 一路補到 `types.ts`（F4/F5/F6）。
5. `lib/rosterOrder.ts` + 測試（此時可獨立跑綠）。
6. `OfficePage` 接上 comparator（F2）與 P2 修正（F3）。
7. 置頂入口（F8）+ 分組視覺（F9）+ i18n（F10）。
8. `frontend/CLAUDE.md` 兩處修正（F11）。
9. 完整 `bin/ci.sh`，判準 rc=0 **且**最後一行精確 `[ci] all green`。

**每一步都不得順手重構無關的碼。** 變更 manifest 逐檔記進 `change-manifest.md`，push 前逐檔講得出必要性（root `CLAUDE.md` §7）。
