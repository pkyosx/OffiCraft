# T-ed38 · Step 6 — UX／產品設計與架構師契約

基準 commit：`6158b32`（== `origin/main` == live `v0.5.45`）
狀態：**Atlas（後端持久化形狀）已定稿**；**Iris（UI 契約）複核中，§3 三項待補**。

---

## 1. 推薦方案

### 1.1 排序規則（四層，需求已定）

```
1. 手動置頂           pinned 群組 —— 組內按「置頂時間新→舊」，
                      不受 unread／最近互動攪動（Iris 契約 2）
2. unread_count > 0   有紅燈者
3. 最近互動新到舊      last_activity_at DESC
4. 既有穩定順序        role==="assistant" 優先，其餘沿用 server 姓名序
```

**兩位架構師的契約在這裡剛好咬合，無衝突**：Atlas 定「`pinned_member_ids` 陣列是 ordered set，順序就是 pinned 群組順序」；Iris 定「新置頂的在上」。→ 實作上就是**新置頂 unshift 到陣列開頭**，顯示順序直接讀陣列順序，不需要額外存置頂時間戳。

第 4 層**沿用現行 comparator 的既有行為不動**（含 `role_key` 空值被當 assistant 的 fallback）—— 那是既有行為，改它會影響 role 顯示等其他面，列為 out-of-scope。

### 1.2 為什麼是這個順序

- 「有人在等我」比「我剛跟誰談過」急迫 → 未讀在最近互動之上。
- 置頂是 owner 的顯式意圖，應該蓋過系統的自動判斷 → 置頂在最上層。
- 第 4 層保留既有序，讓「完全沒互動過的成員」仍有一個穩定、可預期的位置（不是隨機、不是永遠沉底）。

### 1.3 替代方案與代價（DoD 要求至少一個）

**替代方案 A：把未讀與最近互動合成單一分數排序**（例如 `unread ? now : last_activity`）。
- 優點：只有一層比較，實作最簡單，reorder 動盪較小。
- 代價：**語意塌陷**。一個「三天前傳訊息、至今未讀」的成員會被排到「剛剛才聊完」的成員前面還是後面，取決於分數公式的細節，而不是取決於一條可以講給人聽的規則。owner 無法從畫面反推順序為何如此。**否決理由**：需求明確要求「先處理紅燈、再回到最近交談」，那是兩個有序的判準，不是一個加權分數。

**替代方案 B：未讀組內部也按未讀數多寡排序**（未讀 5 則排在未讀 1 則之前）。
- 優點：直覺上「更急的排更前面」。
- 代價：未讀數會因為對方連續發訊而跳動，造成比時間排序更頻繁的 reorder；而且「未讀較多」不等於「較急」。**否決理由**：加劇本票最主要的風險（live reorder 誤點），換來的排序品質提升沒有證據支持。未讀組內部仍按最近互動排序。

---

## 2. 已定稿：後端持久化契約（Atlas，2026-07-28）

以下是 Atlas 的書面裁定，**適用於「若 owner 選擇 server-backed」的分支**。原文摘錄：

> 【結論】直接走「settings ordered-set + caller-relative SQL projection」，本票不加 migration。

**`pinned_member_ids`**
> 存既有 setting 表，key=`display.pinned_member_ids`，值為 JSON `string[]`；沿用 `GET/PATCH /api/settings`，不要新端點、不要新表，也不要塞進 `MemberDTO`。理由：pin 是 viewer 的顯示偏好，不是 member 本身的狀態。
> wire：`SettingsDTO.pinned_member_ids` additive-optional；回應永遠 array，缺 key=`[]`。`SettingsUpdateDTO.pinned_member_ids` optional；省略=不改，`[]`=清空。
> 陣列是 ordered set：順序就是 pinned 群組順序；空字串或重複 id 422，全部驗完才寫。已移除／未知 id 允許留存，render 時與 active staff roster 取交集並忽略孤兒，不做隱藏 cleanup write。
> 寫入語意是整組原子 replace、last-write-wins；這和既有 settings 相同。不要做 server merge，否則跨裝置同時改動時順序無法定義。
> 用純 server truth，不加 localStorage cache：登入後本來就要拿 settings + roster，舊 cache 只會讓已取消／已移除的 pin 短暫復活。settings 讀失敗就誠實降級 `[]`。跨裝置的承諾是 reload/login 後一致，不額外承諾即時同步。

**`last_activity_at`**
> 同意你的語意：對 `currentActor(r)` 而言，actor↔peer 任一方向最後一則 `chat_message.ts`；不是 peer 全域活動，也不看 `chat_read.last_read_ts`。
> spec 用 additive-optional `number`（epoch seconds）；full response 無互動=0。`?fields=light` 也維持 honest-empty=0，schema description 要明寫「0 可能是無互動，light 則是未計算」。新 client 打舊 server 缺欄時同樣 fallback 0。

**取得方式**
> 不沿用 `ListChat()`，也不要在它旁邊再加第二條 activity query。新增 DAL helper，例如 `ListMemberChatStats(actor) map[peer]MemberChatStats{UnreadCount, LastActivityAt}`，一次 SQL 同時計算兩者，並替換 roster handler 現有的 `ListChat()+ListChatReads()+UnreadCounts` 路徑。
> SQL 形狀：`WHERE sender=:actor OR recipient=:actor`；peer=`CASE WHEN sender=:actor THEN recipient ELSE sender END`；`MAX(ts)` 算 activity；LEFT JOIN `(reader_id,peer_id)` 的 `chat_read`，`SUM(CASE WHEN recipient=:actor AND ts>COALESCE(last_read_ts,0) THEN 1 ELSE 0 END)` 算 unread；依 peer GROUP BY。

**live DB 實測（Atlas 親測，補上我查不到的數字）**
> `chat_message` 1,089 列；owner 相關 632 列；10 peers；DB 8.55 MB。現有索引僅 `ts`。候選單掃描 query plan 是 scan `chat_message` + 用 chat_read PK join + temp GROUP BY，實跑約 1 ms。以這個量級，本票先不加 sender/recipient index。

**migration**
> 更正一點：`00041` 不是「兩種落地順序都安全」。若本票先以 00041 上線，DB 到 41；PR #12 後來才帶入 00040，bare `goose.Up` 會因 missing/out-of-order migration 拒絕起站。此次不做 migration，正好消除順序依賴。

→ **我原本的判斷是錯的，已採納更正**（`impact-inventory.md` §2 已改）。

---

## 3. 已定稿：UI 契約（Iris，2026-07-28）

完整裁定見 `T-ed38-ui-contracts-iris.md`（Iris 附件）。以下是結論與我採納的狀態。

### 3.1 契約 1 — 我問的問題本身是錯的（Iris 重新框定）

我問的是「選中列固定策略」。Iris 實讀後指出那底下是**兩個性質不同的問題**，而我給的四個選項**全部只解到第一個**：

- **P1｜有明確選取時的視覺跳位。** `selectedId` 在 URL hash，排序怎麼變都不影響「在看誰」，只影響那一列的畫面位置。**視覺問題。**
- **P2｜無選取時，眼前的對話會被抽換。** 嚴重一級。

**P2 的機制**（Iris 親自回讀原碼驗證）：`OfficePage.tsx` 全檔只有一個 `useEffect`（:185，與此無關），**零 `useMemo`、零 `useCallback`**，`setSelectedId` 呼叫點只有 :440/:455/:498。所以 `:236-238` 的 `selected` 是**每次 render 重算的 derived const**。新排序一旦引入會動的鍵（unread、最近互動），SSE 每次 refetch 都可能換掉 `roster[0]` → **未選取的桌機使用者，眼前那間聊天室會在他完全沒操作的情況下自己換走**。

> 諷刺的地方：**最可能觸發重排的事件就是「有人發了新訊息」，而那正是 owner 最可能正在看畫面的時刻。**

**我漏了這一點。** 我的 `problem-framing.md` 只寫到「改變預設打開哪一間」，把它當成一次性的初始值問題，沒看出它會被反覆抽換。

**決策 1a｜P2 必須解**
> 契約：**排序的變動，不得改變「當前正在顯示的對話」。**
> 作法：把「空選擇時的預設對象」從每 render 重算的 fallback，改成**首次拿到非空 roster 時解析一次、之後固定**（用 ref/state 記住解析出的 id，roster 重排不重算）。
> **明確不動的**：T-661b 那條收窄原封不動；**也不寫回 URL hash**（空選擇仍是空）。
> 這個改動**在現行排序下行為完全不變**，所以它不是行為變更，是**為新排序預先拆掉的引信**。

**決策 1b｜P1 先不解，都不做**（我無異議）
Iris 逐條否決我的三個選項：(a) 釘住選中列會產生「畫面順序 ≠ 實際排序」的謊言；(b) hover 凍結是用全域手段解區域問題，且離開 hover 會有累積的大跳；(c) 動畫是成本問題 —— 此 repo **沒有動畫庫、沒有 FLIP、`prefers-reduced-motion` 全樹 0 命中**，等於要從零建一套基礎設施。

> 理由不只是成本：P1 的實際傷害是「點錯人」，而在這個 UI 裡點錯人的代價極低——點回去就好。**用最重的手段去解最輕的傷害，不划算。**

**回頭做的觸發訊號**（寫下來，不讓它變成默默的技術債）：
> 若 owner 實際回報「點錯人」或「列表在跳」，再回頭做 FLIP + `prefers-reduced-motion`。在那之前這是**已知且刻意接受的取捨，不是遺漏**。

### 3.2 契約 2 — 拖曳不做；置頂組內按「置頂時間」新→舊

> 置頂組內順序：按置頂時間，新置頂的在上。**組內不受 unread／最近互動影響。**

Iris 一開始傾向「組內沿用一般規則」，**複核後改掉了**，理由：
> **手動置頂的核心價值就是「位置可預測」。如果置頂組內還會因為 unread 跳動，就是把使用者剛剛親手固定的東西又還給自動規則——自我否定。**

附帶好處：owner 想調整置頂組內順序，用既有動作就做得到（取消置頂再重新置頂 → 跑到最上面），**不必引進拖曳這個新機制**。

### 3.3 契約 3 — 入口放成員詳情面板；置頂用「分組」表達，列內不加東西

這題撞到**兩條 owner 裁定**，Iris 親自讀原碼確認兩條都存在：

1. `MemberCard.tsx:84-86`：*"No settings gear here: owner 2026-07-17 moved the role ⚙ OFF the roster row and INTO the member detail panel's identity card — **the roster row stays a pure presence line**."*
2. `MemberCard.tsx:96-110`（Seth 2026-07-13）：flex-end 那個 slot *"now carries **ONLY** the unread signal"* —— **這條我和 Iris 的複核者原本都沒點出來**，而 Iris 本來正想把置頂圖示放那裡。

**決策 3a｜入口 → 成員詳情面板**（avatar `<button>` 已是 a11y 完備的既有入口，有 `aria-label`、有 `stopPropagation`）。
理由：同型問題給同型答案（owner 2026-07-17 已對「成員層級動作放哪」裁定過一次）；零新增互動機制（kebab／右鍵／長按在 repo 皆 0 命中）；鍵盤可及性天然滿足；不吃掉「點列 = 進聊天」。
代價：置頂變成兩步。**可接受** —— 置頂是低頻動作，用兩步換掉「新增一套互動機制 + 踩兩條 owner 裁定」划算。

**決策 3b｜視覺表達 → 分組分隔，不是列內圖示**
> 置頂組排在最上面，與其餘成員之間用一道分隔（＋一個極輕的區段標示）隔開。位置本身就表達了「這些是被置頂的」，列內不新增任何元素。

兩條裁定管的都是**列內**，加分組是**列表結構層**，不在它們的射程內。

**決策 3c｜分組形式 = single hairline，不要文字 label**（Iris 定稿）

我查先例時貼給她的第 3 條線索，反而讓她挖到**第三條 owner 裁定**，並因此推翻了帶文字標題的做法：

> `OfficePage.tsx:402-408` 註解原文："T-66a8 (owner mockup 2026-07-18): top 正職/外包 text-tab switcher … **Replaces the old two-stacked-groups rail（正職 collapse header + 外包 panel head with their own counts）**."
> **owner 在 2026-07-18 做的事，正好就是把「帶 header 的堆疊分組」從左欄拿掉、換成頂部頁籤。** 所以如果我叫你加一個「已置頂」section header，我等於把他十天前才移除的視覺結構原封不動放回去。**選項出局，不是因為不好看，是因為 owner 已經對「左欄要不要用帶標題的分組」表過態了。**

⚠️ **Iris 的通則提醒（我記下來了）**：這個元件周圍已經是**第三條**裁定（`MemberCard.tsx:84-86`、`:96-110`、`OfficePage.tsx:402-408`），三條都帶日期＋裁定者＋理由。
> **這一帶的設計是被反覆吵過的，動任何東西前先把註解整段讀完，不要 grep 關鍵字——裁定不會用你要找的字寫。**

**視覺值**：沿用 `settings.css:508` 的 hairline 值（Iris 親讀）：
```css
border: 0;
border-top: 1px solid color-mix(in srgb, var(--color-overlay) 8%, transparent);
```
該處註解說明它為什麼是這個形狀：*"The UA default is an inset 3D groove, which on a dark canvas reads as a scratch; a single hairline is the section divider the docs mean."* —— 理由是通用的（深色底上怎麼表達分隔），可跨到左欄。

**但有三條限制**：
- **不要複用 `.doc-md hr` 那個 class**（它綁在 markdown 渲染上）；只沿用粗細與顏色值，開自己的 class。
- **`margin: 18px 0` 不要照抄** —— 左欄節奏是 `.member-card` 的 `padding: 12px`，上下各 18px 會開一個異常大的洞。⚠️ Iris 標明**這條她沒看過畫面、是從數字推的**，要我調完**實際截圖看一眼**再定值。
- **不要碰 `office.css:201-226` 的 baseline divider**（它自己交代「固定基準線，不要動」）。新 hairline 用新 class。

**決策 3d｜a11y：視覺用 hairline，語意用 `role="group"`**
hairline 純視覺、螢幕閱讀器讀不到，但**不用文字 label 來補**：
- 置頂那幾張卡外包一層 `<div role="group" aria-label="已置頂">`（字串走 i18n）
- hairline 純 CSS（`border-top` 或 `::before`），**不進 a11y tree**，**也不加 `role="separator"`**（group 已提供語意，再加就是重複播報）

**決策 3e｜四條邊界規則**
1. **零個置頂** → 不渲染 hairline、不渲染 group wrapper，列表完全回到現在的樣子。
2. **全部都置頂** → **不渲染 hairline，也不渲染 unpinned group wrapper**（下組是空的，尾端掛一條線很怪；空 wrapper 同理）。判斷條件是「**兩組都非空**」，不是「置頂組非空」。

   ⚠️ **本條的後半（wrapper）是事後補上的，補的經過比結論重要。** 原文只寫了 hairline，
   實作照字面做，於是全置頂時仍渲染一個**空的** `unpinned-group` div —— 而它位在一個
   `gap: 8px` 的 flex column 裡，**尾端因此多出 8px 死白**，且測試把那個 wrapper 釘成了契約。
   由獨立 review（round2 O2）抓到，送回 Iris 裁定，她選「比照第 1 條，不渲染」。

   **她的判準不是 8px 好不好看**：「**這個 div 不代表任何東西。**『未置頂群組』在全部都置頂時
   不存在。渲染一個空容器來代表一個不存在的群組，是**讓 DOM 說謊**；`gap` 只是把謊話變得
   看得見而已。」另外她指出這比原本設想的更不該留：她當初的理由是「哪天有人在父層加 `gap`
   就開始承重」，而**這裡父層本來就有 `gap`**，所以那不是潛在風險，是已經在發生的後果。

   📌 **通則（Iris 要求記下，適用於往後所有契約撰寫）：契約若對同一個結構列了多個邊界 case，
   每一條規則都要對每一個 case 各過一遍，不能只對「當時正在想的那個 case」寫。**
   她自陳漏因：「寫第 2 條的時候腦子裡只有 hairline」。**漏掉的那一格不會長得像漏洞——
   它會長得像『那條規則不適用』**，所以讀的人不會停下來問。這與本票另一條教訓同族
   （檢查網只撈得到被寫成字的東西），差別在這次是**同一句話沒有被寫第二遍**。
3. **hairline 掛哪** → 掛在**非置頂組第一張卡的上緣**，不是置頂組最後一張的下緣（後者日後加 footer 之類的東西時會變成孤兒線）。
4. 置頂組內順序照契約 2。

---

## 4. 狀態、文案與可及性（不依賴待補契約的部分）

### 4.1 空與錯誤狀態

| 情境 | 行為 |
|---|---|
| 沒有任何成員被置頂 | 不渲染任何置頂分隔或標籤；列表就是三層排序的結果。**不顯示「尚無置頂」空狀態** —— 那是對一個沒被要求的功能做視覺宣告。 |
| 置頂了已被解僱／移出名冊的成員 | 該 id 留在 settings 陣列中，**render 時與 active roster 取交集後忽略**；不顯示幽靈列、不做隱藏 cleanup write（Atlas 契約）。 |
| settings 讀取失敗 | **誠實降級為 `[]`（無置頂）**，列表照常以三層排序渲染。不可整個列表不渲染，也不可靜默重試到天荒地老。 |
| 置頂寫入失敗 | 樂觀更新後回退到最後確認值（比照 `useOrgName` 既有作法），並讓 owner 看得出沒存成功。 |
| 成員從未互動過（`last_activity_at` 為 0 或缺席） | 落到第 4 層（既有穩定序），**不是沉底、不是置頂**。 |
| server 沒有這個欄位（舊 server） | 同上 —— 全體 fallback 0，等於整個第 3 層失效、退回現行行為。**這是安全的降級，不是壞掉。** |

### 4.2 文案

依 `frontend/CLAUDE.md` 的 i18n 規範：**寫成靜態字串葉子，不寫 interpolation 模板**（否則主題包的 `wording` 覆寫看不見它）。三語 `zh` / `en` / `xian`。

需要的葉子（暫定鍵名，實作時對齊既有命名）：
- `office.pinMember` — 置頂這位成員
- `office.unpinMember` — 取消置頂
- （若 Iris 的入口需要群組標籤）`office.pinnedGroup` — 已置頂

### 4.3 鍵盤與焦點

- 置頂／取消置頂**必須可用鍵盤完成**，且**不可吃掉「點列 = 進聊天」**這個既有行為（`frontend/CLAUDE.md`：整列本身是聊天入口，flex-end 位置目前只剩未讀 badge）。
- 動作觸發後焦點不可消失（列被 reorder 後焦點要跟著該成員，不是跟著位置）。這一點與 U1 的固定策略相關，實作時一併處理。
- 沿用既有 `LifecycleDot` / `PresenceBadge` 的無障礙模式，不自己手寫第二套。

### 4.4 Responsive

| 寬度 | 要求 |
|---|---|
| 390px | 置頂入口不得把成員名稱擠掉或造成橫向溢位。`frontend/CLAUDE.md` 已記載長 token 溢出的單一來源在 `.doc-md` 基底，但**成員名稱不是 markdown 欄位**，需自行確認不溢位。 |
| 768px | 左欄與聊天區並存時排版正常。 |
| desktop | `.office__members-list` 的 `flex:1` 自身捲動行為不變；外包區 `max-height: min(42vh, 276px)` 不受影響。 |

⚠️ 若 Iris 的 U3 選擇 popover／浮層形式：`frontend/CLAUDE.md` 明載**浮層寬度不可用 `vw` 夾**（T-49fb），必須 `left:0; right:0; width:auto` + `max-width`。

---

## 5. 本票明確不承諾的事

- **P1（有明確選取時的視覺跳位）不解，不做 reorder 動畫。** 這是**已知且刻意接受的取捨，不是遺漏**（Iris 決策 1b）。回頭做的觸發訊號：owner 實際回報「點錯人」或「列表在跳」→ 屆時做 FLIP + `prefers-reduced-motion`。
- **不承諾置頂變更在另一裝置上「不 reload 也即時同步」**。現有 settings 沒有這項即時承諾；要做需另加 settings SSE/refetch，屬另一張票。（Atlas 點名的最可能錯點。）
- 不改外包列表排序（另一套刻意設計）。
- 不改 `mappers.ts:141` 的 `role_key` 空值 fallback 本身。
- 不改 `unread_count` 的壓制條件 `selected && windowActive`。

---

## 6. 決策紀錄（gate 後補）

| 決策 | 由誰 | 卡片 id | 結論 |
|---|---|---|---|
| 持久化方向：local-only vs server-backed | owner | `rc-e925695724e3` | ✅ **選項 1：存在 server，跨裝置一致**（2026-07-28 答覆，選項無附加文字） |
| `frontend/CLAUDE.md` 兩處過時描述怎麼處理 | owner | `rc-563734cd294e` | ✅ **選項 1：本票順手改掉這兩句**（2026-07-28 答覆，選項無附加文字） |
| U1 / U2 / U3 + 分組形式 3c–3e | Iris | （不走卡，chat 裁定） | ✅ 見 §3 |
| 後端持久化形狀與查詢方式 | Atlas | （不走卡，chat 裁定） | ✅ 見 §2 |

**兩件「告知而非請求核可」的事**：卡片 `rc-e925695724e3` 的內文另外告知了兩件連帶行為變更（預設打開的聊天室會變；現行置頂組實質是「Mira ＋ 所有沒設角色的人」），並寫明「不用你決定，除非你反對」。**owner 選了選項但未就這兩點留下任何文字。**
→ 我據此推進，但誠實記錄：**這是「未表示反對」，不是「明確核可」**。若日後回頭檢視，這兩點的授權強度低於上面兩列。

**因 owner 裁定而新增的 in-scope 項目**（原本列在 out-of-scope 的 doc 修正）：
- `frontend/CLAUDE.md:40` — badge 壓制條件改為描述實碼的 `selected && windowActive`
- `frontend/CLAUDE.md` 左欄結構描述 — 改為描述 T-66a8 的頂部文字頁籤
兩處的判斷依據（Iris 親讀實碼）：**碼帶著理由、文件沒有 → 文件落後於碼，不是碼漂移**。
