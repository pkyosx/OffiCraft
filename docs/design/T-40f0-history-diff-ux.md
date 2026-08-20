# T-40f0 —「初始版本」也能先比較，且差異改成 GitHub Files-changed 樣式

狀態：**已實作**（owner 2026-07-31 兩張卡上逐項拍板；本檔是碼的 context doc，根 CLAUDE.md「核心不變量／context 跟碼共存」）
前身：`docs/design/T-1f39-document-history-ux.md`（版本歷史本體的設計定案，v0.5.60 上線）

owner 用完 T-1f39 提了兩件事，各在一張卡上拍板：

- `rc-28885813e065` ①：**「初始版本」那一列要能跟現況比較**。
- `rc-b69722f81136` ①：差異畫面照 **GitHub PR 的 Files changed** 做（他先看過兩欄版草圖，明確挑了單欄當預設）。

---

## A.「初始版本」為什麼原本比不了 —— 以及補了什麼

**病因不在前端。** 版本清單最底下那一列（`onReset` 存在時才長出來的「初始版本」）點下去直接跳還原確認，
是因為 **seed 的內容從來沒有交到前端**：伺服器唯一會吐出它的時機是「重置之後」的那個回應。所以在整份清單裡，
**唯一一列使用者只能盲按的，剛好是最不可逆的那一列**（它把 owner 寫過的每一個字整份換掉）。

補的是一條**唯讀**取得方式：

| 面向 | 內容 |
| --- | --- |
| 端點 | `GET /api/document-history/{kind}/{key}/seed` |
| DTO | 新增 `DocumentSeedDTO { kind, key, content }`；`content` 用**與保留版本相同的欄位名**，所以同一個閱讀面／同一個 diff 直接服務它 |
| 授權 | `Auth: gated`、`Requires: machine` —— **與「列出保留版本」同一個讀取地板**（比較就是讀）。還原那條仍是 `agent` 地板 ＋ 各文件自己的 in-handler 寫入門檻，一個字沒動 |
| MCP | **工具 `get_document_seed`**（owner 裁定 `rc-b7d29de0eb9c`：「開放，照你 7/30 那句話一律給」）。⚠️ 這一格**推翻過一次**：本文件原本寫 `MCPExclude: true`，論證是「role 的 seed 就是 boot context 注進那個 persona 的同一份文字、全域情境的預設就是空文件 —— 對 agent 零新增資訊」。owner 依他 **2026-07-30** 那條政策否決了那個論證，而那正是隔壁 restore 那列已經引的同一條裁定（`rc-b5fd1135e2dd`）：**讀的給 agent、寫回去的不給**。切分點是**動詞**，不逐條重新表決「這個讀值不值得」——用「不夠有用」去排除，等於把那場表決又搬回來。**地板一個字沒動**（仍 `machine`，與 `list_document_history` 同級），這條路上也沒有寫入動詞可以被順帶打開 |
| 404 的位置 | **恰好等於「重置也會 404」的那一組**：自訂角色、任務手冊（兩條序列）、per-role lessons。座艙的 初始版本 那一列本來就只在有 `onReset` 時渲染，所以「比得了」與「還原得了」永遠同進同退 |
| 有 seed 的兩個文件 | `global_context` → `{text: "", tombstoned: "true"}`（**空文件就是它的預設**，不是「沒有答案」——對 diff 而言缺鍵與空字串是兩份不同的文件）；`role_definition` 的 seed 角色 → 檔案 seed 的 `definition_md` |

**wire-freeze 流程照走**（根 CLAUDE.md「驗證、CI 與出貨／wire spec-first」）：先改 `spec/openapi.json` → `bash bin/gen-ocapi` 重生 `ocapi_gen.go`
→ `npx openapi-typescript` 重生 `frontend/src/api/generated/schema.ts`；`conformance/routes_manifest.json`
（127 列）、`test_auth_matrix.py`、`test_rest_happy.py` 同批。**`spec/mcp-catalog.json` 也同批**（手維護；
新工具 `get_document_seed` 的描述子插在 `list_document_history` 之後，89 → 90 個工具），
`conformance/routes_manifest.json` 的 `mcp_tool` 欄一併從 `null` 改成工具名。
`catalog_hash` 由 route 表的**工具名集合**推導（`catalogHashOf`），所以這次會變 —— 那就是它的用途，
agent 的目錄變了就該收到重啟訊號。

### 座艙側

- `useDocumentSeed`（新 hook）—— 只在**版本清單被打開**且該文件有 `onReset` 時取一次，不訂任何 topic
  （文件的出廠內容不會在座艙開著的期間改變）。
- `DocumentHistoryEntry` 的 `reading` 狀態改成 discriminated union（`version` / `seed`），
  seed 那一列的 `onClick` 從「開重置確認框」改成「開閱讀面」。列的**位置與樣式沒動**——入口不會更難找。
- `DocumentHistoryModal` 多兩個 optional prop：
  - `seed`：這是出廠預設而不是保留版本。**沒有時間、也沒有修改者**（沒人寫它），所以 header 直接寫「初始版本」，
    而不是渲染一行捏造的「修改者」；還原鈕改成「還原成初始版本」，確認框用重置自己的文案。
  - `seedUnavailable`：seed 那個 GET 失敗了。此時兩個 pane 都說「讀不到」，**絕不說「這個版本沒有內容」**
    （那是另一個、而且是錯的主張），而**還原照樣按得下去**——把文件退回預設不需要這個 client 手上有任何內容。
    這一條是刻意的：T-1f39 已經為「初始版本不做別人的人質」修過一次（見該檔 L2），T-40f0 新增了第二個
    可能失敗的請求，所以同一條護欄要再守一次。
- 列表層那個 `doc-history-seed-confirm` ConfirmModal **整塊刪除**（連 `resetting`/`resetBusy`/`resetError` 狀態）：
  還原與重置現在共用 modal 裡那**一個**破壞性確認路徑，不可能再各說各話。

🔴 **「看」不會觸發「還原」，是有測試釘住的主張，不是註解**：
- `server/ocserverd/api_document_seed_test.go::TestGetDocumentSeed_ReadingNeverWritesTheDocument`
  ——讀兩次之後，活文件與它的保留歷史**逐 byte 不變**。
- `frontend/src/components/SettingsPage.document-history.test.tsx::opens 初始版本 for READING, and looking at it restores nothing`
  ——開列、開 modal、切到差異 pane，全程 `resetGlobalContext` 零呼叫、文件仍是 owner 的字。

---

## B. 差異呈現：GitHub Files-changed

owner 逐項拍板，實作對應如下（碼在 `frontend/src/components/DiffView.tsx` 檔頭也逐條標了）：

| owner 的項目 | 實作 | 釘住它的測試（`DiffView.test.tsx`，除註明者外） |
| --- | --- | --- |
| ① 預設**單欄**上下對照；**兩欄對照**保留為可切換 | `mode` state（預設 `"unified"`）＋ head 上的 `.diff-view__modes` | `renders unified by default…` ＋ `switches to the two-column view and back…` |
| ② **整列上色**，辨認靠顏色不靠行首符號 | 列本身上 `--added`/`--removed` 底色，**行號格另上更深的同色**（否則 sunken 背景會在整列顏色上打兩個灰洞） | `tints the entire row — the number gutter included…`；顏色本身由真引擎守（`visual-guards/diff-view.ct.spec.tsx`，jsdom 不解 `color-mix`） |
| ③ **兩排行號**（舊／現況；只存在一邊的行另一邊留空） | `numberCell()` 兩格 | 單欄那條逐格斷言 `["2","","-","bravo"]` |
| ④ 同列只改幾個字時**把改到的字標亮** | 新模組 `frontend/src/lib/wordDiff.ts` | `marks only the characters that changed…` ＋ `wordDiff.test.ts` 15 條 |
| ⑤ 行首 `−`／`+` 那一欄**保留**（輔助） | `markerCell()`，並帶 `aria-label` | `labels the marker cell so added/removed is not carried by colour alone` |
| ⑥ **不做摺疊／展開**，整份顯示 | `DiffView` 固定送 `collapseUnchanged: false`，並移除 `@@` 分隔列與其樣式 | `renders every unchanged line, with no collapse separator anywhere` |

### 為什麼字級標亮是新模組，而不是改 `lib/lineDiff.ts`

`lineDiff` 是「**哪幾行**不同」的權威，有自己的測試檔，這次**一個字都沒動**。`wordDiff` 回答的是
呈現層在 `lineDiff` 講完之後才問的、嚴格更窄的問題：一個 `-` 行與取代它的那個 `+` 行之間，**哪些字**動了。
它不能改變有哪些列、列的順序或行號 —— 拿掉它，差異照樣畫得出來，只是少了字級的底色。

三個設計決定（每一個都有測試，因為每一個反過來做都會通過「有東西」這種弱斷言）：

1. **切成 token 而不是逐字**：中文散文逐字比會讓 LCS 在零散的「的／了」上遊走，幾乎每個字都被標亮
   —— 那是穿著精確外衣的雜訊。tokenizer 因此把 latin/數字**整段**保留、空白**整段**保留、其餘（含 CJK）
   一字一個 token（中文沒有空白，逐字就是它能有的詞粒度）。
2. **兩行毫無共同 token 時，一個字都不標**：整列的顏色已經說完「這一行被整個換掉」了；再把每個 token
   標起來，會讓「只改幾個字」與「整行換掉」長得一模一樣 —— 而分辨這兩者就是這個功能存在的全部理由。
   ⚠️ **共同的空白不算證據**（`"  "` 對上 `"  "` 在任何兩行之間都成立），所以相似度只數非空白 token。
3. **每側 400 token 上限**：token LCS 是 O(n·m)，而且**每一對變更列都要跑一次**；一行 minified blob
   或 base64 只能讓它少一層底色，不能讓 tab 卡死。超限時 lossless 退回「不標亮」。

### 兩欄對照的配對規則

一個「變更區塊」＝連續的 `removed` 列後面緊接著連續的 `added` 列（`lineDiff` 對平手偏向 removal，
所以取代一定長這個形狀）。**位置配對**：第 i 個 removed 配第 i 個 added；長度不等時多出來的那些列
**不配對**（那是純粹被刪或純粹被加的整行，內部沒有可比對的對象）。`pairChangedRows` 同時服務兩欄版面
與字級標亮，所以兩者不可能對「哪兩行是一組」有兩種說法。反向的形狀（`added` 在 `removed` 之前、
中間夾了 context 行）**必須不配對**，各有一條測試。

### 🔴 兩種「看起來空白」的狀態不得混為一談

這條在 T-1f39 就有，T-40f0 **加測試、不放寬**：

- 兩版完全相同 → `diff-view-empty`，明講「內容完全相同」。
- 差異過大、拒絕比對 → `diff-view-too-large`，明講「太長無法逐行比對」**並報出行數**。

兩條測試各自**同時斷言另一個不在場**（`says the two versions are IDENTICAL…` /
`says the comparison was REFUSED for size, never that the versions match`），
所以「拒絕」永遠不可能偽裝成「沒有差異」。

### 退場的東西

- `diff.skippedLead` / `diff.skippedTail` 兩個字典葉子與 `compose.ts` 的 `diffSkipped` composer
  —— 摺疊的分隔列沒了，它們就是死碼。`messageKeys.generated.ts` 與 `message_keys_gen.go` 由
  `npm run gen:msgkeys` 重生。
- `.diff-view__skip*` 全部 CSS 規則。
- `DiffView` 的 `options` 從 `LineDiffOptions` 窄成自有的 `DiffViewOptions`（只剩 `maxLines`）：
  留著 `collapseUnchanged` 這個 knob 等於在介面上廣告一個這個面拒絕擁有的行為。

新增字串（**兩語都補**，`zh` / `en` —— 這個 repo 的 `Locale` 是封閉的兩語聯集，不是三語）：
`diff.viewLabel` / `viewUnified` / `viewSplit` / `wholeDocNote`、`settings.historySeedUnavailable`。

---

## 沒有動到的東西（審查時可以直接跳過）

- `lib/lineDiff.ts` 與 `lib/lineDiff.test.ts`：**一個字未改**，18 條全綠。
- 還原／重置的授權門檻、確認框的破壞性、over-cap 版本的「看得到按不下去」。
- `seeds/`：**有新工具，但沒有新的 agent 流程**，所以這次不涉及手冊「獨立審查」的五點 code-hygiene 清單 —— 判準不是
  「有沒有加工具」而是「agent 要不要學新做法」。附錄 A 明文叫 agent **別背固定清單、一律以 `tools/list`
  為權威**，而 seeds 從來沒有列舉過 `list_document_history`（`get_document_seed` 與它同形、同地板、
  同一種用法），所以沒有任何一句 seed 因為這次改動變成假話。對照組是 `get_chat_attachment_share_link`：
  那次**必須**改 seeds，因為它教的是一條 agent 原本不會做的新動作（簽連結、自己前綴 origin）。
  ⚠️ **`spec/mcp-catalog.json` 不在這一節了**，它這次有改（見上）。
