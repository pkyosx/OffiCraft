# T-ed38 · 獨立 review 第二輪（implementation-blind reviewer）

標的：worktree `member-roster-priority-sorting`，單一 commit `3cdad93`，父 commit = `origin/main` = `8a5f1fb`。
`git show --stat HEAD` = **47 檔**（29 修改 / 18 新增 / 0 刪除）、4558 插入 / 81 刪除。

流程紀律：**先自己做完一輪、把結論寫下來，才讀 `docs/T-ed38/review.md`**（避免被上一輪錨定）。
§5 的 B1/B2/O1–O6 對帳是在我自己的結論定案之後才寫的。

## 方法（與上一輪最大的差別）

上一輪誠實揭露「沒有自己重做任何 mutant 控制組，因為被要求唯讀」——而那正是漏掉
`does NOT freeze an EXPLICIT chatId onto the default` 恆真的原因。

這一輪我用**隔離副本**解掉那個兩難：把 `frontend/` 與 `server/ocserverd/` rsync 到 session
scratchpad（`node_modules` 用 symlink、`seeds`/`spec`/`docs` 用 symlink 補齊相對路徑），
**所有變異都只改副本**。工作樹自始至終 `git status --porcelain` 為空（review 結束再確認一次）。
沒有跑 `bash bin/ci.sh`（另一個 agent 正在用 dev port）。

實跑（全部在副本上）：

```
# baseline
npx vitest run rosterOrder.test.ts OfficePage.roster-order.test.tsx \
    OfficePage.selected-stability.test.tsx MemberDetailPanel.pin.test.tsx
  Tests  33 passed (33)
go test . -run 'TestListMemberChatStats|TestPinnedMemberIDs' -count=1   ok
npm run --silent typecheck                                             rc=0
```

**24 顆變異，23 顆被殺、1 顆為等價變異**（明細見 §4）。

---

## 判定：**REQUEST CHANGES**（1 項阻擋，成本很低；碼與測試本身這一輪我找不到缺陷）

先講清楚：**這一輪我沒有在產品碼或測試邏輯裡找到任何缺陷。**
排序、SQL、驗證、降級、邊界我都用變異實測過，護欄全部有鑑別力（包含上一輪被揭穿的那個方向）。
唯一的阻擋項是**證據紀錄與被審查的樹對不上**——正是這張票自己剛被燒過的那一類問題。

---

## 1. 阻擋項

### B1 — `verification.md §4` 的 CI 綠燈**不涵蓋本次被審查的 commit**，而 §4 自己訂的標準要求它涵蓋

`docs/T-ed38/verification.md:299-328` 記了三次 `bin/ci.sh`，最後一次標為
**「第三次（文件補完後重跑，最終狀態）」`rc=0` / `[ci] all green` / 3586 行**，並寫下規矩：

> 理由：第二次跑之後又新增了 `docs/T-ed38/change-manifest.md` 並補寫本檔，
> CI 第 7 道有 path denylist + gitleaks，**綠燈必須是對最終工作樹跑出來的**。

但那次綠燈之後，樹又動了。上一版同名 commit 是 `76ad9a4`（17:59:43，父同為 `8a5f1fb`），
而 `HEAD` = `3cdad93`（18:25:33）。兩者差 3 個檔：

```
$ git diff --stat 76ad9a4 HEAD
 docs/T-ed38/change-manifest.md                            | 20 ++++++++++
 docs/T-ed38/verification.md                               | 41 ++++++++++++++++++++
 frontend/src/components/OfficePage.selected-stability.test.tsx | 44 +++++++++++++++++++++-
```

第三個是**碼**：`clearing an EXPLICIT pick returns to the original default …` 這支新測試，
以及舊測試的改名。也就是說——**這次修補的核心產物（那支新的鑑別力測試），從未經過權威 gate。**
`verification.md` 裡沒有第四次跑的紀錄（`grep '第四次\|ci4'` 無命中）。

**為什麼是阻擋**：這不是「大概沒事」的問題，是**紀錄本身不成立**。
§2.5 花了整整一節記錄「§2.4 的 P2-B 是寫錯方向的 mutant、給了我們假的信心」，
結論是「量測隨基底作廢，重做」。同一份文件在 §4 卻讓一次**舊樹**的綠燈繼續掛著當「最終狀態」。
land 之後讀 §4 的人會相信最終樹過了 gate；事實上沒有紀錄支持。
（caller 告知「rc=0、最後一行 `[ci] all green` 已在本 commit 上確立」——若確有該次執行，
那修法就只是把它補記進 §4；但**憑 repo 內的證據，這句話目前無法被證實**，而我不能替它背書。）

**我自己把風險壓到多低（不能取代 gate，但可以量化剩下多少未知）**：

| 檢查 | 結果 |
|---|---|
| `npm run typecheck`（含新測試檔） | rc=0 |
| `OfficePage.selected-stability.test.tsx` 全檔 | 5 passed |
| 該檔對「凍過頭」變異的鑑別力 | 實測 1 紅（見 §4 A） |
| `bin/ci.sh` 是否有寫死的前端測試支數 gate | 無（grep `1368` / `EXPECTED_TESTS` 無命中） |

剩下的未知集中在 lint/token/conformance/gitleaks 那幾道我沒跑的關卡。

**修法（擇一）**：
1. 對 `3cdad93` 的樹重跑 `bash bin/ci.sh`，把 `rc` 與 `tail -n 1` 補記為 §4「第四次」；或
2. 若那次執行已經做過，把它的實際輸出補進 §4，並把「第三次…最終狀態」改成非最終。

---

## 2. 非阻擋觀察（依重要性）

### O1 — `change-manifest.md` 開頭的統計數字與 rebase 狀態，對本 commit 是**錯的**

`docs/T-ed38/change-manifest.md:8-15`：

- 寫 **「28 個修改檔、18 個新增檔…共 46 檔」**；實際 `git show --stat HEAD` 是 **29 / 18 / 47**。
  差的那一檔就是 root `CLAUDE.md`（§11 改寫是在那次量測之後、依 `rc-9fe87f4e0099` 才加進來的）。
- 寫 **「`origin/main` 目前已再前進到 `8a5f1fb`，本分支尚未跟上」**；
  實際 `git merge-base --is-ancestor 8a5f1fb HEAD` 成立、`git rev-list --count 8a5f1fb..HEAD` = 1。
  **分支早就跟上了**，那句話會讓讀者誤判 land 前還欠一次 rebase。
- `:95` 寫「`docs/T-ed38/*.md`（其餘 **6** 檔）」，後面點名了 **7** 個檔，而實際「其餘」是 **8** 檔
  （10 份文件扣掉單獨列出的 `verification.md` / `change-manifest.md`）；`review.md` 只被 `:103`
  的「全 10 檔」那列涵蓋，沒有單獨的必要性敘述。

**§7 的實質要求（每個檔講得出必要性）是滿足的**——我把 47 個檔逐一比對過 manifest，
包括最容易漏的 root `CLAUDE.md`（`:146` 有完整裁定溯源）與 `api_member_avatar_test.go`（上一輪 O4，已補）。
所以這不是覆蓋率缺口，是**同一份文件第二次把自己的數字寫錯**——它自己的 `:11-12` ⚠️ 正是在
道歉第一次寫錯，並承諾「數字也依實際 `git diff` 更正」。列非阻擋，但請一起修掉，
別讓「manifest 的數字不可信」變成慣例。

### O2 — 全部置頂時仍留下一個空的 `unpinned-group` `<div>`，尾端多 8px（上一輪 O2 附帶項，未處理）

`OfficePage.tsx` 的分組三元式在 `pinnedRoster.length > 0` 時**一定**渲染那層 wrapper，
即使 `unpinnedRoster` 是空的：

```tsx
{pinnedRoster.length > 0 ? (
  <div className={unpinnedRoster.length > 0 ? "…--divided" : "office__roster-group"}
       data-testid="unpinned-group">{unpinnedRows}</div>
) : ( unpinnedRows )}
```

`.office__members-list` 是 `display:flex; gap:8px`（`office.css:292-295`），空的 flex child
高度 0 但**上方那道 gap 仍在** → 全置頂時列表尾端多 8px。
`roster-order.test.tsx:134-146` 的「全部置頂」那支用 `getByTestId("unpinned-group")`，
等於**把這個空 wrapper 釘成了契約**。

零置頂那一側（Iris 3e 規則 1）這一輪修得很到位——連 wrapper 都不渲染，測試還斷言卡片是
list 的直接子節點。同一條理由（「今天不佔位的元素，就是明天有人加 `gap` 時開始佔位的元素」）
用在零置頂上，卻沒有用在全置頂上。純視覺、幾乎看不見，所以非阻擋，但**這是一個不對稱**。

### O3 — spec 的 `pinned_member_ids` description 沒寫上限，client 會撞到一個契約沒提過的 422

`spec/openapi.json` 的 `SettingsUpdateDTO.pinned_member_ids` 只寫
「空字串或重複 id 是 422」，**沒有提 `maxPinnedMemberIDs = 100` 與 `maxPinnedMemberIDLen = 64`**。
在一個 spec-first、把 spec 當 client 契約的 repo 裡，這兩個上限對外是隱形的。
（實作與測試都對，只是契約文字少了兩句。）

### O4 — mock 的 `patchServerSettings` 註解宣稱「server parity」，但只驗了一半

`frontend/src/api/mock.ts` 新增的驗證只擋空字串與重複 id，
**沒有實作那兩個上限**，註解卻寫「（server parity）」。
`frontend/CLAUDE.md` 要求 mock 與 http 行為一致。實務影響幾乎為零（沒有 UI 能一次送 100 個 pin），
但那句「server parity」目前是超額宣稱。

### O5 — server 接受前後有空白的 id 並原樣存下

`api_settings.go` 用 `strings.TrimSpace(id) == ""` 擋空白 id，但**不 trim 後再存**——
`" m-bob "` 會通過驗證並原樣寫進 settings row，然後永遠對不到任何成員（一個永久孤兒 pin）。
同一支 handler 的 `orgName` / `ownerName` / `pushContactEmail` 都是 trim 之後才存。
FE 不會送出這種 id，所以非阻擋；只是與**同一個函式裡的既有慣例**不一致。

### O6 — 置頂的寫入門檻是 `admin_agent`，也就是任何 `role_key=="assistant"` 的 agent

`PATCH /api/settings` 自 T-6020 起是 owner **或 admin agent**。
`display.pinned_member_ids` 被描述成「owner 的手動順序」，但實際上**任何助理角色的 agent
都能整組覆寫它**（whole-set replace、last-write-wins、無稽核軌跡），owner 只會看到列表順序變了。

這是**既有 settings row 的授權面，不是本票新開的洞**，而且
`api_settings_pinned_t_ed38_test.go:118-119` 與 `usePinnedMembers.ts:10-12` 兩處都誠實寫明了。
所以不阻擋——但它值得被 owner 知道，因為「pin」在 UX 上讀起來像純個人偏好。

### O7 — `usePinnedMembers` 對飛行中的請求沒有排序保證

`save()` 每次都直接發 PATCH，沒有 in-flight guard、沒有序號。
快速「置頂 → 取消置頂」兩下，若兩個回應**亂序抵達**，較慢那個的
`.then(s => setPinned(s.pinnedMemberIds))` 會把 UI 拉回舊值，而 server 的最終狀態是另一個
→ UI 與 server 分歧到下次 reload。要觸發得在一次 render cycle 內連點，實務機率極低。
（`prev`/`next` 顯式傳入的寫法已經擋掉了 StrictMode 重複送出——見 §4 E，我實測過。）

### O8 — StrictMode 的兩個宣稱正確，但**沒有任何測試在 StrictMode 下跑**

`usePinnedMembers.ts:69-71` 明確為 StrictMode 做了設計決定（不用 updater form，避免一次點擊送兩個 PATCH），
`main.tsx` 也確實用了 `<StrictMode>`，但四支新測試都是裸 render。
我另寫探針實測（副本上，已刪）：StrictMode 下一次點擊**確實只發 1 個 PATCH**，
且預設對話的 pick → clear → 回到 Mira 也成立。**宣稱為真，只是沒有護欄釘住它。**

---

## 3. 我查了、確認乾淨的地方

### 需求符合度（`lib/rosterOrder.ts` 逐層）

| 層 | 判定 | 依據 |
|---|---|---|
| 1 置頂（陣列序即顯示序） | ✅ | `:82-87`；兩者皆 pinned 時短路，下面每一層都不跑 |
| 2 未讀（布林，不是計數） | ✅ | `:89-93`；`rosterOrder.test.ts:81-90` 明確釘「不依未讀**數量**排」 |
| 3 最近互動（新→舊） | ✅ | `:95-102`；缺欄位 → 全體 0 → 整層退休（有測試） |
| 4 role=assistant 優先 | ✅ | `:104-107`；既有行為逐字保留 |
| 5a name（`toLowerCase()` + `<`/`>`） | ✅ | `:109-114`；**不是** `localeCompare` |
| 5b 同名再比 id → **全序** | ✅ | `:116-117`；S2 用 9 人 fixture × 18 種排列驗恆等 |

決定性的取捨我認同：`localeCompare` 依賴 runtime ICU，正好是「jsdom 綠、瀏覽器另一個順序」
這種決定性測試抓不到的失效；碼註解與 `frontend/CLAUDE.md` 都把代價寫明了。
`localeCompare` 在 `frontend/src` 確實 0 命中，所以這不是引入新慣例。

Tie-break 確實是**全序**：id 由 server 鑄造且唯一，所以任兩個不同成員必有確定順序，
不依賴 `Array#sort` 的穩定性，也不依賴 `GET /api/members` 的 `ORDER BY name`。

### 後端

- `ListMemberChatStats` 的 SQL 語意與舊 `UnreadCounts` 我逐 case 推過並**用變異實測**（§4 B）：
  `LEFT JOIN … ON cr.peer_id = m.sender` 在 outbound 列 join 不到，但 `unread` 條件本來就要求
  `m.recipient = actor`，不影響；`chat_read` 是 (reader, peer) 複合鍵 upsert，不會 fan-out。
- `dal_member_chat_stats_t_ed38_test.go` 把 agent↔agent 那則訊息（ts 99，全表最新）
  放進 fixture 並斷言它**不得**洩進 owner 任一 peer 的數字，再換 caller 為 `m-2` 反向驗一次。
  這仍然是本包最好的一支測試。
- 驗證全部在 `s.settingsMu.Lock()` **之前**完成（`api_settings.go:250-360` vs `:366+`），
  所以「422 什麼都不寫」是結構性成立的，不只是測試碰巧綠——我用「把寫入搬到驗證迴圈之前」
  的變異實測過（§4 D S6）。
- 上限值與 `maxCustomThemes`（同一張 settings row）同源同理由，測試把**兩個邊界都釘了**
  （剛好 100 / 剛好 64 必須通過，101 / 65 必須 422）。

### 相容性

- `spec/openapi.json`：`MemberDTO.required` / `SettingsDTO.required` / `SettingsUpdateDTO.required`
  都沒動，新欄位帶 `default` → **additive-optional**。舊 client 忽略多的欄位即可。
- 新 client 打舊 server：`w.last_activity_at ?? 0` / `w.pinned_member_ids ?? []`
  （`mappers.ts`），第 3 層整層退休、退回舊排序，**有測試**（`ABSENT lastActivityAt`）。
- `?fields=light`：`OfficePage` 用的是 **full**（`useMembers()` 無 opts）；
  唯一的 light 消費者是 `RepliesPage`，不排序。那個 0 的雙重語意在 spec description、
  `wire.go`、`types.ts` 三處都寫明了。
- 孤兒 pin：交集發生在 render（`splitPinned`），server **刻意不做 cleanup write**，
  純函式與 render 兩層各有測試。
- spec-first：`ocapi_gen.go` / `generated/schema.ts` / `messageKeys.generated.ts` /
  `message_keys_gen.go` 的 diff 只有新欄位與 gofmt 對齊，看不出手改痕跡；
  `bin/ci.sh:240-256`、`:346`、`:419-420` 各有 drift gate 會抓（前提是 B1 那次 gate 有跑）。

### Doc↔code 真實性（逐條查證，不是看敘述）

- root `CLAUDE.md` §11：我**實際讀了三張卡**。`rc-9fe87f4e0099` 狀態 answered、
  `option_idx = 0` =「照新格式寫，並順手把 repo 那條規定也改成新格式」→ **改寫確實被授權**。
  `rc-a202dc39521c` `option_idx = 1` =「全面覆蓋，含 OffiCraft」→ 文字裡「覆蓋本 repo 舊寫法」正確。
  `rc-22758d4c734a` `option_idx = 2` =「不放票號」→ 文字裡那句正確。
  三處卡號引用全部對得上，文字自洽。本 commit 自己也符合新格式
  （`feat(roster): …`，56 字元、小寫祈使句、無句點、無票號、`[why]`/`[how]`、`Co-Authored-By` 保留）。
  唯一的小出入：`[how]` 是 6 條 bullet，規則寫「2-5 句或條列」。
- `frontend/CLAUDE.md` 的兩處「順手修正」——`rc-563734cd294e` 狀態 answered、
  `option_idx = 0` =「這次順手改掉這兩句」→ **有裁定，不是夾帶**。
  而且那兩句的**新內容我對著碼驗過是真的**：`MemberCard.tsx:111` 確實是
  `!(selected && windowActive)`；左欄確實是頂部頁籤。
- `frontend/CLAUDE.md` 新增的「roster 排序 + 手動置頂」整節：四層、5a 不可用 `localeCompare`、
  四條邊界、wire 欄位、P2 範圍——逐條對得上實作。
- `mappers.ts:141` 的 `role_key || "assistant"` fallback 與 `avatarKind.ts` 的耦合，
  註解說「本票不碰」，diff 確認確實沒碰。

### Scope

47 個檔我全部追到來源：feature 本體、生成物、或**兩張有紀錄的 owner 裁定**
（`rc-9fe87f4e0099` → root `CLAUDE.md`；`rc-563734cd294e` → `frontend/CLAUDE.md` 兩處）。
`api_member_avatar_test.go` / `api_roles.go` 是 `newMemberDTO` 簽章變更的機械跟隨。
**沒有找到任何 scope creep。** 沒有暫存檔、debug 輸出、`.bak`、binary、機密。

---

## 4. 變異測試明細（本輪的主要新增證據）

**全部在隔離副本上執行；工作樹未被修改過。** 「殺」= 至少一支測試變紅。

### A. `OfficePage` 選取穩定性（上一輪被揭穿的那個方向）

| # | 變異 | 結果 |
|---|---|---|
| **A** | **精確的凍過頭**：`roster.find(m => m.id === (selectedId \|\| defaultChatIdRef.current))` | **殺** — 1 紅，且**唯一紅的就是新測試** `clearing an EXPLICIT pick returns to the original default …` |
| B | 完全不凍結（退回 `: roster[0]`） | 殺 — 3 紅 |
| C | fallback 時不重新錨定（`if (defaultSelected && !defaultChatIdRef.current)`） | 殺 — 1 紅（`… and is STILL frozen afterwards`） |

**A 是本輪最關鍵的一顆。** 上一輪的控制組證明舊測試對這個方向 1368 支全綠、零鑑別力；
我用同一個方向的精確變異重跑，**新測試單獨紅了**。
它不是另一個舒服的斷言——`pick → CLEAR → assert` 的形狀 + explicit id 指向**真實在名冊裡**的成員，
兩個條件缺一不可，而測試的註解把「為什麼缺一不可」寫下來了。**這一項我判定已真正修好。**

### B. `dal.ListMemberChatStats` 的 SQL（5 顆，全殺）

| # | 變異 | 結果 |
|---|---|---|
| G1 | unread 不看已讀水位（拿掉 `ts > COALESCE(...)`） | 殺（`…UnreadMatchesUnreadCounts`） |
| G2 | peer 只用 `m.sender`（outbound 列 key 錯） | 殺 |
| G3 | `WHERE … OR 1=1`（caller-blind，agent↔agent 洩漏） | 殺 |
| G4 | last activity 只算 inbound | 殺 |
| G5 | join 到 `m.recipient`（水位比錯 peer） | 殺 |

### C. `lib/rosterOrder.ts`（11 顆：9 殺、2 等價）

| # | 變異 | 結果 |
|---|---|---|
| M1 | 拿掉 id tie-break | 殺（2 紅） |
| M2 | 拿掉 name tie-break | 殺（1 紅） |
| M3b | 讓 unread + recency 在**置頂組內**重排（契約 2） | 殺（1 紅，正是 `keeps the stored pin order …`） |
| M5 | recency 反向 | 殺（5 紅） |
| M6 | 缺 `lastActivityAt` 讀成 `Infinity` | 殺（1 紅） |
| M7 | unread 層拿掉（讓 recency 先決定） | 殺（3 紅） |
| M8 | name 不 lower-case | 殺（1 紅） |
| M9 | `splitPinned` 一律回傳空 pinned | 殺（4 紅） |
| M11 | 拿掉 role 層 | 殺（6 紅） |
| M4 | `?? 0` → `\|\| 0` | **等價變異**（`number \| undefined` 下兩者行為相同）——不是測試缺口 |
| M10 | `pinIndexOf` 重複 id 取**首個** index | **未殺**：docstring 宣稱「重複會坍縮到 LAST index」，無測試；但 server 422 擋重複，實務不可達 |

### D. 分組邊界 + settings 驗證（8 顆，全殺）

| # | 變異 | 結果 |
|---|---|---|
| D1 | 只要 pinned 非空就上 hairline（丟掉「兩組都非空」） | 殺（`全部置頂: still NO hairline`） |
| D2 | 零置頂也渲染 wrapper | 殺（2 紅，含 orphan-pin 那支） |
| S1 | 上限 off-by-one（`>=`） | 殺 |
| S2 | 完全不擋陣列長度 | 殺 |
| S3 | 不擋單一 id 長度 | 殺 |
| S4 | 空白檢查用 `id == ""` 而非 `TrimSpace` | 殺 |
| S5 | 拿掉重複檢查 | 殺 |
| S6 | 在驗證迴圈**之前**就寫入（破壞 validate-then-write） | 殺（2 紅） |

### E. `usePinnedMembers`（2 顆）

| # | 變異 | 結果 |
|---|---|---|
| P1 | 拿掉寫入失敗的 rollback（`setPinned(prev)`） | 殺（`a failed settings WRITE snaps the toggle back …`）——**這支不是恆真的**：樂觀更新讓 true→false 的轉換真的可觀察 |
| — | StrictMode 探針（另寫、已刪） | 一次點擊 **1 個 PATCH**；pick → clear → 回到 Mira 成立 |

### 「X 不會發生」型斷言的逐條判讀（本輪被特別要求的檢查）

| 斷言 | 判定 |
|---|---|
| `expect(headerName()).toBe("Mira")` 在重排之後（selected-stability ×2） | **非恆真** — 兩支都先放**正向控制**（先斷言 roster 真的重排了）。變異 B 殺得掉。 |
| `clearing an EXPLICIT pick …` | **非恆真** — 變異 A 精確殺掉（見上）。 |
| `queryByTestId("pinned-group")).toBeNull()`（零置頂 / 孤兒 pin） | **非恆真** — 變異 D2 殺掉；而且那支還額外斷言 list 的**直接子節點都是 `.member-card`**，比單看 testid 強。 |
| `container.querySelector('.--divided')).toBeNull()`（全置頂） | **非恆真** — 變異 D1 殺掉。 |
| `expect(container.querySelector('[role="separator"]')).toBeNull()` | **恆真**（碼裡從來沒有 `role="separator"`）。屬於「防未來回歸」的守衛，成本為零，我不視為問題，只是它不提供現在的證據。 |
| `a failed settings WRITE snaps the toggle back`（false → false） | **非恆真** — 變異 P1 殺掉。 |
| `a failed settings READ degrades to no pins`（全是 null / false） | **偏弱** — 我想不到一顆能被它殺、又不被別支殺的非等價變異。它證明的是「讀失敗時不崩、入口仍在」，這件事有價值，但鑑別力來自那 2 張卡片的存在斷言，不是那些 `toBeNull()`。 |
| `_, ok := stats["m-3"]; ok`（never-contacted peer 必須缺席） | **非恆真** — 變異 G3 會讓它有值。 |

---

## 5. 對上一輪 B1/B2/O1–O6 的對帳

| 上一輪 | 現況 | 我的查證 |
|---|---|---|
| **B1** `docs/T-ed38/` 沒進 commit、碼引用變懸空 | ✅ **已解** | 10 份文件全部在 commit 裡（18 個新增檔含它們）；`OfficePage.tsx` / `selected-stability.test.tsx` / `frontend/CLAUDE.md` 對 `docs/T-ed38/verification.md §0.5` 的三處引用現在**解析得到**。manifest `:103` 記了納入理由與掃描結果。 |
| **B2** `pinned_member_ids` 無長度上限 | ✅ **已解** | `maxPinnedMemberIDs = 100` / `maxPinnedMemberIDLen = 64`，與 `maxCustomThemes` 同源同理由；測試把**兩個邊界都釘了**（剛好通過 / 多一個 422）。我用 4 顆變異（S1/S2/S3 + off-by-one）實測，全殺。 |
| **O1** ref 在預設成員離開後永久退化 | ✅ **已解** | 現在**無條件**重新錨定（`if (defaultSelected) { ref.current = defaultSelected.id }`），且新增了測試的**後半段**（解僱 → fallback → 再驗一次仍然凍住）。變異 C 實測 1 紅。**修法沒有引入新問題**：pick/clear 那條路徑我用變異 A 反向確認沒有凍過頭。 |
| **O2** 零置頂仍渲染 wrapper | ✅ **已解**（主項） / ⚠️ **附帶項未解** | 零置頂現在連 wrapper 都不渲染，測試改成斷言**直接子節點**（比原本強）。但 O2 的附帶項「全置頂時空 wrapper 造成尾端 8px」**仍在**，而且被測試釘住了 → 見本輪 **O2**。 |
| **O3** `verification.md §2.3` 宣稱「整組重做」但只做了 3 顆 | ✅ **已解，而且解得比要求好** | §2.3 已改；新增 §2.4、§2.5。§2.5 更進一步**推翻了 §2.4 自己的一半結論**，白紙黑字寫下「§2.4 的 P2-B 是一顆寫錯方向的 mutant，它給了我們假的信心」，並列出盲點的兩個成因。這是我這輪看到最有價值的一段文件。 |
| **O4** manifest 漏 `api_member_avatar_test.go` | ✅ **已解** | manifest `:110` 補上；我用逐檔比對確認 47 檔全部有著落。但**同一份文件的開頭數字又錯了**（本輪 O1）。 |
| **O5** 冷啟動時預設對話在 pin 讀回前就決定 | ➖ **仍然如此，仍然不是 bug** | 與 P2 契約一致（不得抽換），`MemberDetailPanel.pin.test.tsx` 的 `openBobPanel()` 註解記了這個時序（也正是它要先 await 兩個 microtask 的原因）。 |
| **O6** `frontend/CLAUDE.md` 外包段落句子接不上 | ✅ **已解** | 改成「…`activeTab` 是純 component state、不進路由。（本段原本描述…）」換段後另起「**正職成員卡** = 名字+…」。manifest `:147` 記了「純文氣修正、內容未改」。 |

**「修法是否引入新問題」的專項檢查**：三個修法（re-anchor / 零置頂不渲染 wrapper / 新測試）
我各用變異反向試過，沒有一個造成新的失效；唯一的副作用是 O2 的不對稱（全置頂那側沒跟著修）。

---

## 6. 我沒查 / 查不動的地方（誠實揭露）

1. **沒有跑 `bash bin/ci.sh`** —— 依指示（另一 agent 佔用 dev port）。這正是 B1 無法由我代為關閉的原因：
   我能把風險縮小（typecheck rc=0、目標測試綠、無寫死支數 gate），**但我不能宣稱 gate 過了**。
2. **沒有跑完整的前端 1368 支與完整的 Go 測試** —— 只跑了本票相關的 4 個前端測試檔（33 支）
   與 2 支 Go 測試，以及變異回合。所以我的變異結論是「至少一支相關測試會紅」，
   不是「全套只紅這幾支」。
3. **沒有跑起 app、沒有截圖** —— S8（390/768/desktop 無溢位）、S9（實機鍵盤焦點）、
   hairline 的實際視覺、以及 O2 那 8px 的實際觀感，我完全採信 `verification.md §3`，
   而那些圖在 session scratchpad、不在 repo 裡，我看不到。**O2 我是從 CSS 推的，沒有量過像素。**
4. **沒有驗跨裝置**（兩個瀏覽器、真實 server）與 SSE 實際行為 —— 與實作者同樣的限制。
5. **沒有審 SQL 的效能**（`ListMemberChatStats` 有沒有可用索引、chat 表大時的計畫）。
   它取代的舊路徑會把整張無 LIMIT 的表搬進 Go，所以「更好」我有信心；
   「夠好」我沒有量過 `EXPLAIN QUERY PLAN`。
6. **沒有查 `ChatReplyCard.markdown-render` 那支 flake 的根因**（實作者已誠實列為「發現但未納入」）。
7. **沒有評估與其他在飛的分支的衝突面**（reflog 顯示同時間還有 3 顆同批 feature commit
   碰 `wire.go` / `OfficePage.tsx` 附近）。
8. **`bin/ci.sh` 我只 grep 了幾個關鍵字**（測試支數 gate、drift gate、lint 步驟），沒有逐行讀完
   —— 所以 §1 那張「剩下多少未知」的表是下限，不是完整清單。

---
---

# 附錄：re-verification（commit `3761bd2`）

第二輪 review 之後，修正以 amend 併回同一顆 commit。
`HEAD` = `3761bd2`，parent 仍為 `8a5f1fb`，仍是**單一 commit**，工作樹乾淨。
`git show --stat HEAD` = **48 檔**（29 修改 / 19 新增 / 0 刪除）。

`git diff 3cdad93 HEAD` = 8 檔：`OfficePage.tsx`、`OfficePage.roster-order.test.tsx`、
`api_settings.go`、`api_settings_pinned_t_ed38_test.go`、
`change-manifest.md`、`ux-design.md`、`verification.md`、`review-round2.md`（新增）。

## 判定：**APPROVE**（附一項 land 前必須修掉的非阻擋項，見 A3）

B1 我判定**已解除**。四項修正我逐一反向測試，**沒有一項引入新缺陷**；
唯一的新問題是文件標籤過期（A3），不是碼。

---

## A1. B1 是否解除 —— 我接受遞迴論證，理由如下（不是照單全收）

被要求質疑的正是這裡，所以我把它拆開驗。

**遞迴是真的嗎？** 是的，**在本 repo 自己的誠實規則下**成立。形式上有三條出路：

| 出路 | 可行？ |
|---|---|
| (a) 把權威紀錄放在 commit 之外（PR `## Checks` ／任務紀錄） | ✅ 可行，且是業界常態——CI 系統本來就以 SHA 為 key 存結果，不把結果寫回樹裡 |
| (b) 先寫下宣稱、事後跑 CI 驗證它為真 | ❌ 形式上可行，但**等於先斷言一個尚未觀測的結果**，正是 root §3／本 repo 一貫禁止的事。用它來閃避遞迴，代價是打破更根本的規矩 |
| (c) 乾脆不要求 commit 內留紀錄 | 等同 (a) |

也就是說遞迴**不是邏輯上絕對不可解**（(b) 在純邏輯上解得掉），
但 (b) 被本 repo 的誠實規則擋死，所以**在這套規則內，遞迴是實質不可解的**。
新 §4 選了 (a)，這是剩下的唯一正解。**論證正確，不是便宜行事。**

**是不是偷換掉標準？** 不是。我逐字比對過：
判準原文（`rc == 0` **且** `tail -n 1 | grep -qFx '[ci] all green'`，寬鬆 grep 不算）
**一字未改地保留**在新 §4 裡。搬動的是**紀錄的位置**，不是**門檻本身**。
而且新 §4 把五次執行**各自標明涵蓋哪個 tree／commit**，並主動寫下
「原本一邊立標準、一邊記錄上一顆 commit 的結果，自己的證據撐不起自己的宣稱」。
**這比修掉問題更有價值——它把失效模式留在紙上。**

**殘留的洞（我要講清楚，不含糊過去）**：
- 新 §4 **沒有**宣稱 `3761bd2` 是綠的。它明說自己記錄不了。**所以它沒有捏造任何東西**，
  這是它能被接受的關鍵。
- 但代價是：**land 之後，光讀 repo 無法證實最終 commit 過了 gate**，必須信任 PR。
  這是自足性的實質下降，只是相對於一個**不可能達成的理想**而言的下降。
- 因此 **B1 的解除是有條件的**：條件是 PR 內文 `## Checks` 段**確實**載明對 `3761bd2`
  的執行結果（`rc=0` ＋ 最後一行精確 `[ci] all green`）。
  **若 PR 未開、或 `## Checks` 只寫「CI 綠」這種寬鬆說法，B1 就以新形式復活。**
- coordinator 告知該次執行已完成（`3761bd2`、樹乾淨、`rc=0`、精確 `[ci] all green`）。
  **我沒有、也依指示不能自己重跑 `bin/ci.sh`，所以我不為那個結果背書**——我背書的是
  §4 現在的**結構是誠實的**。

**一個必須點名的事實**：§4 記錄的五次執行中，**沒有一次涵蓋現在的碼**。
第五次涵蓋 `3cdad93`，而 `3761bd2` 之後又改了 `OfficePage.tsx` 與 `api_settings.go`。
所以那個 commit 之外的執行**不是形式手續，是唯一涵蓋現行碼的權威證據**，
不能被當成補登。我能替它縮小的未知列在 A4。

## A2. 四項修正的反向測試 —— 各自的新變異，全部被殺

沿用第二輪的作法：把 `frontend/` 與 `server/ocserverd/` rsync 到 scratchpad 隔離副本，
**只改副本**。工作樹全程未被修改（結束前再確認一次）。

副本 baseline：前端 4 檔 **33 支全綠**；`go test -run 'TestPinnedMemberIDs|TestListMemberChatStats'` **ok**。

### O2 修正（`OfficePage.tsx` 的渲染條件）—— 三個邊界各一顆，全殺

新條件是 `pinnedRoster.length > 0 && unpinnedRoster.length > 0`。三個邊界都要守：

| # | 變異 | 結果 |
|---|---|---|
| R1 | 還原舊條件 `pinnedRoster.length > 0 ?`（空 wrapper 回來） | **殺** — 1 紅，正是改寫後的 `全部置頂: no hairline AND no unpinned wrapper` |
| R2 | 只留 `unpinnedRoster.length > 0`（零置頂變成有 divided wrapper） | **殺** — 2 紅（零置頂 + 孤兒 pin） |
| R3 | 永不包 wrapper（混合時失去 hairline） | **殺** — 1 紅（混合） |

**三個邊界都有獨立的紅**，而不是一顆變異掃到全部——這代表條件的**兩個 operand 各自被守著**。
改寫後的測試也比原版強：除了 `queryByTestId(...)` 為 null，還斷言
`[...list.children]` **恰等於 `[置頂組]`**，所以「wrapper 只是掉了 testid」也擋得住
（與零置頂那側的 parentage 斷言對稱）。**Iris 說原測試「守的是錯的東西」，這句話是對的，而且已經修對。**

### O5 修正（settings 的 trim 路徑）—— 兩半各一顆，全殺

| # | 變異 | 結果 |
|---|---|---|
| T1 | 驗 trim 後、**存 raw**（`append(..., raw)`） | **殺** — `TestPinnedMemberIDsAreStoredTrimmed` |
| T2 | 去重用 **raw** 當 key（padded 雙胞胎溜過） | **殺** — 同一支 |
| T3 | 完全不 trim（還原舊形狀） | **殺** — 同一支 |

**「儲存」與「去重」兩半確實各自被守著**，不是一句籠統的斷言。

### 「這次的修正有沒有改壞既有的？」

前述所有變異還原後，副本 baseline **33 支全綠**、Go 全綠。
另外對現行碼跑：`gofmt -l` **無輸出**、`go vet` **無輸出**、`go build` **成功**、
`npm run typecheck` **rc=0**、新增的 `TestPinnedMemberIDsAreStoredTrimmed` **PASS**。
（第一次 CI 就是掛在 gofmt，所以 `api_settings.go` 改過之後這一項我特地驗了。）

### 特別檢查：coordinator 點名的「去重改用 trim 值會改變哪些請求被拒」

我把行為差分逐格推過：

| 請求 | 舊行為 | 新行為 | 判定 |
|---|---|---|---|
| `[" m-bob "]` | 200，存 `" m-bob "`（永久孤兒） | 200，存 `"m-bob"` | **修好了** |
| `["m-bob", " m-bob "]` | 200，兩筆都入庫，第二筆不可達 | **422**，什麼都不寫 | **更正確**（本來就是同一個 id） |
| 64 字元 id ＋前後空白（raw 66） | **422**（長度以 raw 計） | 200（長度以 trim 後計，存 64） | **放寬了，但無害**：入庫值仍 ≤ 64，上限的目的（bound 那一列 JSON）沒破 |
| 陣列長度上限 | 以 raw 陣列長度計 | 不變（trim 不影響筆數） | 無變化 |

**沒有找到會讓合法請求被誤拒的方向。**

順帶兩點，都**不是本次修正造成的**，只是我在推這條路徑時撞到，記下不隱藏：
1. `decodeJSONBody` 是 `io.ReadAll(r.Body)`，**這條路由沒有 `MaxBytesReader`**（全 repo 只有
   `api_chat.go:360` 提到這件事）。所以「超大 body」的暫態記憶體本來就無上限，
   與 trim 無關（改前改後都得先把 body 讀完）。**既有面，非回歸。**
2. server **沒有**對「已存但以現行規則會驗不過」的集合做自癒。若真有這種資料，
   FE 下一次 pin/unpin 會把整組原樣送回 → 422 → rollback → **置頂功能永久失效**。
   **本票不可達**（這個欄位就是本 commit 引入的，不存在歷史資料），但形狀值得記著。

### O1（manifest 數字）

我用 `git show --name-status` 現算並比對：**29 修改／19 新增／48 檔** —— **與 HEAD 相符**。
「本分支尚未跟上 `8a5f1fb`」已刪；`git merge-base --is-ancestor 8a5f1fb HEAD` 成立。
「其餘 6 檔」已改為 **7** 並逐一具名（我數過，是 7）。
47→48 的差額是 `review-round2.md` 本身，manifest **主動寫明**以免被誤讀成第三次算錯——
這一手是對的。**訂正史被保留而不是被覆蓋，這比數字對了更有價值。**

### 未修的四項（O3/O4、O6、O7、O8）

理由我逐條查證，**不是搪塞**：

- **O3/O4 的「既有先例」我實查過**：`custom_themes` 的 `maxCustomThemes = 100` 確實
  **只存在於 Go**，`spec/openapi.json` 與 `mock.ts` 皆未宣告（我直接讀 JSON 與 mock 確認）。
  所以「本票不單方面改變這個慣例」是**準確的陳述**，不是託詞。同意留給 owner 決定。
- **O6** 送 owner 而非自行收緊治理門檻 —— **正確**。改動 `admin_agent` 門檻會影響
  整張 settings 表的所有欄位，遠超本票範圍。
- **O7 / O8** 記為已知限制，措辭與我的觀察一致，沒有把「低機率」寫成「不存在」。

## A3. 唯一的新問題（非阻擋，但 land 前該修）—— `verification.md` §3.1

修正確實引入了一個新問題，只是在文件層：

1. **標題的 commit 已過期**。`### 3.1 在最終基底（commit 3cdad93）重做的 UX／responsive／a11y 驗收`
   —— `3cdad93` **已不是最終 commit**，而 `3761bd2` 之後又改了 `OfficePage.tsx` 的**渲染條件**。
   本檔自己反覆立的規矩就是「基底／狀態換過，舊量測不算數」；§4 剛因為同一個形狀被擋下。
   **這是同一類錯誤的第三次**（round1 manifest 數字、round2 §4、現在 §3.1）。
2. **章節位置錯了**。`### 3.1` 實際落在第 348 行，**在 `## 4`（299 行）之下**，
   而 `## 3` 在 274 行。找視覺證據的人會在 §3 底下找不到它。

**但實質結論不受影響，我實際推過而不是假設**：§3.1 stage 的是 Mira／Bob／Cara／Dan 四人、
最多置頂 2 人，所以每一張截圖的狀態不是**零置頂**就是**混合**——
而 O2 修正**只改了「全部置頂」那一格的 DOM**。零置頂與混合兩條路徑**逐字未動**。
因此第 1–7 項的證據**全部仍然成立**。

順帶：**「全部置頂」這一格在改前改後都沒有視覺驗證**（它不在 stage 出的狀態裡）。
那是一個既有缺口，不是這次改出來的回歸——但既然契約 3e 第 2 條剛被改寫，值得補一張。

**修法（擇一，都很便宜）**：
- 把標題改成它實際涵蓋的 commit，並加一句「其後 `3761bd2` 只改了『全部置頂』那一格的渲染，
  本節 stage 的狀態不含該格，故結論不受影響」；或
- 依本檔一貫規矩，對 `3761bd2` 重跑（至少第 6 項與新增一格全置頂）。

兩種都可以，**但不能就這樣 land**——否則本票就在同一份文件裡，第三次留下一個
「宣稱涵蓋最終狀態、實際不涵蓋」的段落，而這正是它兩次被擋下的原因。

## A4. 我這一輪能替 CI 縮小多少未知（不能取代 gate）

| 檢查（對 `3761bd2` 的碼） | 結果 |
|---|---|
| `gofmt -l server/ocserverd` | 無輸出 |
| `go vet ./...` | 無輸出 |
| `go build ./...` | 成功 |
| `go test -run 'TestPinnedMemberIDs\|TestListMemberChatStats'` | 7 支全 PASS（含新增的 trim 測試） |
| `npm run typecheck` | rc=0 |
| 前端 4 個相關測試檔 | 33 支全綠 |
| 本輪新變異 6 顆（R1–R3 / T1–T3） | 全部被殺 |

**仍未跑**：`bash bin/ci.sh` 全套（依指示）、conformance、gitleaks／path denylist、
`lint:tokens` / `lint:token-roles`、生成物 drift gate、以及前端完整 1368 支。
**所以我依然不對「CI 綠」背書**，只對上表背書。

## A5. 這一輪沒查的地方

1. **沒有跑 `bin/ci.sh`**（同前輪，dev port 被占用）。A1 的殘留洞與 A4 的未跑項因此仍在。
2. **沒有重看 §3.1 的 23 張截圖** —— 它們在 session scratchpad，不在 repo。A3 對「結論不受影響」
   的判斷是我從**改動範圍**推出來的（零置頂／混合路徑逐字未動），**不是重看圖得出的**。
3. **沒有重跑第二輪那 24 顆變異** —— 只重跑了與本次改動相關的路徑，加上 6 顆新變異。
   `rosterOrder.ts` 與 P2 的 ref 邏輯**本輪逐字未動**（`git diff` 確認），所以前輪結論沿用。
4. **沒有驗 PR 是否存在、`## Checks` 是否已載明** —— B1 的解除條件懸在那裡，
   我只能指出條件，無法確認它已滿足。
5. **沒有量 §3.1 那 8px 的實際像素** —— 我對 O2 的判斷全程從 CSS 與 DOM 推導。
