# T-ed38 · 驗證紀錄

實際執行的指令與結果。**未跑或失敗的項目一律照實揭露，不含糊。**

---

## 0. 改動前基線（動任何產品碼之前）

目的：先坐實「我還沒動手時 CI 就是綠的」。沒有這個基線，之後若 CI 紅了，分不清是我弄壞的還是本來就壞的。

**執行時工作樹狀態**：僅 `docs/T-ed38/` 為新增（untracked），**零產品程式碼變動**。

```
$ cd ~/.officraft/agents/ow-5f4832973889/worktrees/member-roster-priority-sorting
$ bash bin/ci.sh
rc=0
```

判準逐項比對（root `CLAUDE.md` §13：**rc == 0 且整份輸出的最後一行**精確等於 `[ci] all green`，兩個條件都要）：

| 條件 | 結果 |
|---|---|
| `rc == 0` | ✅ rc=0 |
| `tail -n 1 \| grep -qFx '[ci] all green'` | ✅ MATCH（精確相等，非寬鬆 grep） |

輸出共 3583 行，完整 log 留在本次 session 的 scratchpad（`ci-baseline.log`）。

**基線 commit**：`6158b32ec39a4c69b3b874783e1a213f43cd265c`

---

## 0.5 P2 後果鏈驗證（tech-design §2 的警示框，實作第一件事）

**問題**：Iris 說「新排序一旦引入會動的鍵，未選取的使用者眼前那間聊天室會自己換走」是**推論、不是實測**。要先實測。

**方法**（照 tech-design §2 指定的驗法，用 vitest 而非跑起 app —— 快且可重複）：
1. **只**把 `OfficePage.tsx:135-138` 的 comparator 換成含 unread 的版本（`(a.unreadCount > 0 ? 0 : 1) - (b.unreadCount > 0 ? 0 : 1)` 疊在既有 role 層之前），**不做任何其他改動**（沒有 P2 修正、沒有新檔、沒有 spec 改動）。
2. 臨時測試 `OfficePage.p2probe.test.tsx`：mock roster 注入第二位成員 Bob（`role_key="engineer"` → 排在 Mira 之後），hash 留空（**未選取**），render `OfficePage`。
3. 斷言初始 chat header = `Mira`；接著注入一則 `bob → owner` 訊息並 fan `chat` topic（`useMembers` 的 ROSTER_TOPICS 含 `chat` → refetch）；等 roster 首位變成 Bob 後，再讀一次 chat header。

**實際執行**：

```
$ cd frontend && npx vitest run src/components/OfficePage.p2probe.test.tsx
PROBE before: "Mira"
PROBE after:  "Bob"
 FAIL  src/components/OfficePage.p2probe.test.tsx
AssertionError: expected 'Bob' to be 'Mira'
 Test Files  1 failed (1)
```

**結果：✅ 抽換確實發生 —— Iris 的推論被實測坐實。**

在 owner 完全沒有操作的情況下，只因為 Bob 發了一則訊息，桌機 chat pane 顯示的對話從 Mira 換成了 Bob。沒有任何 memo／早退攔住它（`selected` 確實是每次 render 重算的 derived const）。

→ **P2 的實作理由原文成立，照 tech-design §2 實作，不需重寫。**

**善後**：probe 檔已刪除，comparator 已還原回原狀（本節之後的實作是從乾淨基線重新開始的）。probe 期間新增的兩個 mock 測試鉤子（`__injectMockMember` / `__emitMockTopic`）**保留**——正式測試矩陣同樣需要它們。

---

## 1. 實作後驗證

以下每一項都附**實際執行的指令與輸出**。未跑或失敗的照實標示。

### 1.1 spec-first 與生成物

```
$ bash bin/gen-ocapi
[gen-ocapi] regenerated server/ocserverd/ocapi_gen.go (oapi-codegen v2.7.2)   rc=0

$ cd frontend && npm run gen:api
✨ openapi-typescript 7.13.0
🚀 ../spec/openapi.json → src/api/generated/schema.ts [83ms]                   rc=0

$ cd frontend && npm run gen:msgkeys
[gen-message-keys] wrote 793 message keys →
  frontend/src/i18n/messageKeys.generated.ts
  server/ocserverd/message_keys_gen.go                                        rc=0
```

**生成物一律未手改**（`ocapi_gen.go` 的 diff 只有新欄位與 gofmt 對齊；`schema.ts` +17 行）。

⚠️ **踩到一個設計文件沒預告的 gate，誠實記下**：只改 `spec/openapi.json` 不夠。
`spec/mcp-catalog.json` 的 `update_settings` 工具參數集合必須與 openapi 逐字相符，
否則 `TestFrozenCatalogAgreesWithOpenapiOnEveryToolsParameters` 直接紅：

```
DRIFT on tool "update_settings" (PATCH /api/settings): spec/openapi.json and
spec/mcp-catalog.json disagree about its parameters.
  in openapi but not in the catalog: [pinned_member_ids]
```

補上 catalog 後轉綠。

### 1.2 server（Go）

```
$ cd server/ocserverd && go build ./... && go vet ./...      rc=0（無輸出）
$ go test -run 'TestListMemberChatStats' ./...               ok  ocserverd  0.646s
$ go test -run 'TestPinnedMemberIDs' ./...                   ok  ocserverd  0.728s
$ go test ./...                                              ok  ocserverd  56.541s
```

新增測試：`dal_member_chat_stats_t_ed38_test.go`（3 支）、`api_settings_pinned_t_ed38_test.go`（2 支）。

### 1.3 frontend

```
$ cd frontend && npm run typecheck                            rc=0（無輸出）
$ npm run lint:tokens
[css-tokens] ok — no raw colour literals, no dead token refs outside the token layer.
$ npx vitest run
 Test Files  170 passed (170)
      Tests  1357 passed (1357)
```

⚠️ **一次 flake，誠實揭露**：全套件的**第一次**執行有一支紅
（`ChatReplyCard.markdown-render.test.tsx > renders card.body markdown (bold/list) as elements`）。
單獨重跑該檔 4 支全綠；其後**連跑 4 次全套件皆 1357/1357 全綠**，`bin/ci.sh` 兩次跑也都綠。
判斷：與本票改動無關的時序 flake（該檔不碰 roster/settings/排序）。**我沒有進一步查它的根因，
也沒有把它列進本票範圍** —— 這是一個「發現但未納入」的既有問題。

### 1.4 測試矩陣對照（tech-design §5）

| 測試 | 檔案 | 狀態 |
|---|---|---|
| comparator 四層（S1） | `lib/rosterOrder.test.ts` | ✅ 7 支（含第 5 層的 `L5a` name / `L5b` id 各一） |
| tie-breaker 決定性（S2） | 同上 | ✅ 1 支（9 位成員，旋轉＋反轉共 18 種排列；fixture 內含走 5a 與走 5b 各一對） |
| fallback（S5） | 同上 | ✅ 4 支 |
| 置頂組不受 unread 攪動（契約 2） | 同上 | ✅ 2 支（含對照組） |
| 孤兒 pin id | 同上 + `OfficePage.roster-order.test.tsx` | ✅ |
| 分組渲染邊界（零／全部／混合） | `OfficePage.roster-order.test.tsx` | ✅ 6 支 |
| 當前對話不被抽換（P2） | `OfficePage.selected-stability.test.tsx` | ✅ 4 支（含反向：沒凍過頭） |
| 置頂新增／取消（S6） | `MemberDetailPanel.pin.test.tsx` | ✅ 3 支 |
| settings 失敗降級 | 同上 | ✅ 2 支（讀失敗 → `[]`；寫失敗 → 回退） |
| server 端 stats | `dal_member_chat_stats_t_ed38_test.go` | ✅ 3 支（含與舊 `UnreadCounts` 逐 peer 對質） |
| server 端 settings | `api_settings_pinned_t_ed38_test.go` | ✅ 2 支 |

---

## 2. V8 控制組驗證（刻意改壞，確認測試真的變紅）

理由：這些測的都是「某件事**不**發生」，而斷言「不發生」最容易寫成恆真。
每顆 mutant 跑完即還原（原檔備份在 scratchpad）。

### 2.1 comparator（`lib/rosterOrder.ts`，16 支）

| mutant | 改法 | 結果 |
|---|---|---|
| M1 | 拿掉第 1 層「兩者都 pinned 就短路」，讓它落到 unread/最近互動 | **1 紅** — `契約 2: keeps the stored pin order even when a later pin has unread AND newer activity`。其餘 15 綠。 |
| M2 | 第 5 層 `return 0`（失去全序） | **3 紅** — `L5 breaks a full tie by id`、`S2 打亂輸入輸出恆等`、`S5 ABSENT lastActivityAt 退回舊排序` |
| M3 | 刪掉第 2 層（unread） | **2 紅** — `L2 unread beats recency`、`S5 ABSENT unreadCount` |

🔴 **M2 第一次跑只紅 2 支，S2 沒被抓到 —— 這是控制組驗證的實際產出，不是走過場。**
原因：S2 的 fixture 裡沒有任何一對成員在 1–4 層完全同分，所以那支測試從來沒走到第 5 層，
**刪掉第 5 層它照樣綠**。已在 fixture 補一對全同分成員（`m-2` / `m-7`）並重跑 M2 → 3 紅。
這正是「斷言不發生的測試容易恆真」的具體案例。

### 2.2 P2「當前對話不被抽換」（`OfficePage.selected-stability.test.tsx`，4 支）

| mutant | 改法 | 結果 |
|---|---|---|
| P2-A | `defaultSelected` 改回每 render 重算 `roster[0]` | **2 紅** — 兩支 P2 契約測試 |
| P2-B | 凍過頭：`roster.find(selectedId) ?? defaultSelected`（丟掉 T-661b 收窄） | **4 紅** — 本檔的 `does NOT freeze an EXPLICIT chatId onto the default` **加上既有的** `OfficePage.jump-outsource.test.tsx` 3 支 |

兩個方向都有鑑別力：做不夠會紅，做過頭也會紅（而且 P2-B 會踩到既有護欄，正是 tech-design §6
預期的偵測方式）。

### 2.3 重跑：rebase 到新基底 + 第 5 層改成 name → id 之後

上面 §2.1 / §2.2 是在**舊基底**（`6158b32`）、且第 5 層還是**純 id** 時做的。分支已 rebase 到
`origin/main` = `a7a0e35`，第 5 層也依 tech-design §1.4 改成 `name.toLowerCase()` → `id` 兩段式，
**舊控制組一律不算數**。

⚠️ **本節原本寫「整組重做」，那句話不準確，已更正**（獨立 review O3 抓到）：這一輪實際只重做了
下表的 **M-5a / M-5b / M-P2** 三顆，舊基底跑過的 **M1（置頂組不被攪動）與 P2-B（凍過頭）沒有重做**，
而 M1 正是本票最需要控制組的那一支（它斷言的是「某件事**不**發生」）。**那兩顆已於 §2.4 在現行基底
補做完畢**；本節只保留當時實際做過的三顆。

執行方式：每顆 mutant 都跑**整套** `cd frontend && npx vitest run`（172 檔 / 1368 支），
不只跑相關檔——這樣才看得到有沒有波及別處。每顆跑完立刻從 scratchpad 的原檔還原，
並以 `diff` 確認位元組相同（輸出 `RESTORED` / `OFFICEPAGE_RESTORED`）。

| mutant | 改法 | 結果 |
|---|---|---|
| M-5a | 拿掉 5a（name 比較），只留 id 兜底 | **1 紅**／1367 綠 — `compareMembers — the four layers, one at a time > L5a breaks a full tie by NAME, case-insensitively` |
| M-5b | 5b（id 兜底）改成 `return 0` | **2 紅**／1366 綠 — `L5b breaks a SAME-NAME tie by id, so the order is TOTAL`、`S2: the order is deterministic under shuffling > returns an identical order for every permutation of the same roster` |
| M-P2 | `defaultSelected` 改回每 render 重算 `roster[0]`（丟掉 `defaultChatIdRef`） | **2 紅**／1366 綠 — `OfficePage — the shown conversation survives a re-sort (T-ed38 P2) > keeps the SAME chat open when a re-sort moves another member to the top`、`… > still holds after several re-sorts, and lets the owner pick freely` |

**三顆都有測試變紅，沒有任何一顆是啞的。**

🔴 **M-5b 是上一輪漏掉的那支，這次確實抓到了。** 修 fixture 之前，S2 的成員全都
`name === id`，所以 5a 一個人就決定了全部順序、5b 從來沒被執行到 —— `return 0` 也照樣綠。
現在 fixture 補了 `m-8` / `m-9`（1–4 層同分**且同名 `Twin`**），只有 id 能分開它們，
S2 才真的守住 5b。同組的 `m-2` / `m-7`（1–4 層同分、**名字不同**）守 5a。

⚠️ **一個誠實的觀察，不補測試遮掉**：M-5a 只紅 1 支，S2 **沒**紅。
原因是 S2 fixture 裡除了 `Twin` 那對之外，其餘成員的 `name` 與 `id` 同值（`mkMember` 的預設），
所以拿掉 name 層之後 id 層給出**完全相同**的順序 —— S2 驗的是「決定性」，那個性質此時仍成立，
它**本來就不該**因此變紅。5a 的行為由專門的 `L5a` 那支（ids 故意與 names 反向）守，那支紅了。
判斷：這不是護欄漏洞，是兩支測試各司其職；**不為了讓數字好看去改 S2**。

### 2.4 補做：M1 與 P2-B，以及 review 修正新增的三支測試（全部在**現行**基底）

前提：§2.3 漏掉的兩顆，加上為了修 review 的 B2 / O1 / O2 而新增或擴寫的斷言，一律要自證有鑑別力。
**舊基底那輪的結果全部作廢，下表每一列都是在現行工作樹上實跑的。**

做法（每一顆都一樣）：先把原檔備份到 scratchpad → 改壞 → 跑**整套**（前端 `npx vitest run`
172 檔 / 1368 支；server `go clean -testcache` 後 `go test`）→ 還原 → `diff` 確認**位元組相同**
（輸出 `*_RESTORED_IDENTICAL`）→ 再跑一次確認回綠。

| # | mutant（怎麼改壞的） | 結果 |
|---|---|---|
| **M1**（§2.3 漏做，本次補） | `rosterOrder.ts` 拿掉第 1 層「兩者都 pinned 就短路」`if (pinA !== undefined && pinB !== undefined) return pinA - pinB;` | **1 紅**／1367 綠 — `契約 2: a pinned group is never reshuffled > keeps the stored pin order even when a later pin has unread AND newer activity` |
| **P2-B**（§2.3 漏做，本次補） | `OfficePage.tsx` 凍過頭：`roster.find(selectedId) ?? defaultSelected`（丟掉 T-661b 收窄） | **4 紅**／1364 綠 — 本檔的 `does NOT freeze an EXPLICIT chatId onto the default (T-661b stays intact)` **加上既有的** `OfficePage.jump-outsource.test.tsx` 3 支 |
| **O1**（新斷言） | 拿掉 fallback 的 ref 重新錨定（退回只在 `""` 時寫入的舊寫法） | **1 紅**／1367 綠 — `falls back to the current first row if the remembered default is dismissed — and is STILL frozen afterwards` |
| **O2**（新斷言） | `OfficePage.tsx` 讓 unpinned wrapper **永遠**渲染（divider 條件原樣保留，精確還原修正前的形狀） | **1 紅**／1367 綠 — `零置頂: no group wrapper and no hairline — the roster looks unchanged` |
| **B2-a**（新測試） | `api_settings.go` 刪掉陣列長度上限檢查 | **紅** — `an over-cap set must be 422, got 200`（101 個 id 被收下） |
| **B2-b**（新測試） | 刪掉單一 id 長度檢查 | **紅** — 65 字元的 id 被收下 |
| **B2-c**（新測試，**反向**） | 上限改成 `>=`（連邊界值也拒） | **紅** — `exactly 100 ids must be accepted, got 422`。這顆證明該測試**兩個方向都守**：不只擋超標，也擋「上限自己把邊界誤殺」 |

**七顆全部有測試變紅，沒有任何一顆是啞的。**

⚠️ **一個過程中的誠實紀錄**：O2 那顆第一次改壞時我把外層條件直接寫成 `{true ? (`，跑出 **2 紅**
（多紅了 `an ORPHAN pin …`）。原因是修正後 divider 的「兩組都非空」條件被拆成外層 `pinned > 0`
＋內層 `unpinned > 0`，強制外層恆真等於連帶弄壞了 divider 條件 —— 那是一顆**比預期更大**的 mutant，
證不到「wrapper 本身」這件事。改成上表那顆精確還原修正前形狀的版本後是乾淨的 1 紅。
**紀錄下來是因為它示範了 mutant 本身也會寫錯，而寫錯的方向剛好會讓人高估護欄。**

### 2.5 第二次換基底後重做 M1／P2-B —— **P2-B 被揭穿是恆真的**

前提：`origin/main` 又前進 5 顆（`a7a0e35` → `8a5f1fb`），本票 rebase 到 `76ad9a4`。
**照本檔一貫的規矩，§2.4 的量測隨基底作廢，重做。** 重做的結果推翻了 §2.4 的一半結論。

| # | mutant | 結果 |
|---|---|---|
| **M1** | `rosterOrder.ts` 中和第 1 層「兩者都 pinned 就短路」（只中和組內短路，保留 pinned 先於 unpinned） | **有鑑別力** — 3 紅／14 綠（`rosterOrder.test.ts`），含目標測試 `keeps the stored pin order even when a later pin has unread AND newer activity`，錯誤為 `expected [ 'second', 'first' ] to deeply equal [ 'first', 'second' ]`。**配對對照組 `the SAME two members DO reorder once they are not pinned` 維持綠** —— 這證明 fixture 不是「本來就長那樣」，目標測試不是恆真。 |
| **P2-B** | `OfficePage.tsx` **精確的凍過頭**：`if (selectedId) { defaultChatIdRef.current = selectedId; }` | 🔴 **恆真／零鑑別力** — 全前端 **172 檔 / 1368 支全綠**，無一支偵測得到。 |

🔴 **§2.4 的 P2-B 是一顆寫錯方向的 mutant，它給了我們假的信心。**
§2.4 改的是 `roster.find(selectedId) ?? defaultSelected`（**丟掉 T-661b 收窄**）——那是另一個失效模式，
既有測試本來就守得住，所以紅了 4 支。但契約要釘的「**凍過頭**」是指
**把一個 explicit 選擇寫進 ref**，那顆從來沒被試過。這正是 §2.4 自己記下的那個教訓再次應驗：
**mutant 本身會寫錯，而寫錯的方向剛好會讓人高估護欄。**

**該 mutant 不是等價變異**（用一次性探針證明，探針已刪除、工作樹已確認乾淨）：
選預設 → 顯式切到 `#office/chat/bob` → 清回 `#office`。未變異時 header 回到 `Mira`；
變異後停在 `Bob`。**行為差異真實可觀察，只是沒有任何測試觀察它。**

**盲點的兩個成因（複合）**：
1. 舊測試用的 explicit id 指向**不存在的成員**，所以變異寫進 ref 的值在下次 render 一樣
   `roster.find` 落空、照樣退回 `roster[0]` —— **變異被測試自己挑的 fixture 遮住**。
2. 它**從不把選擇清回空**，而 ref 的值**只在 `selectedId === ""` 時被讀取**，
   那是凍過頭唯一觀察得到的時機。

**修法（已做）**：新增
`clearing an EXPLICIT pick returns to the original default — the pick is never re-anchored onto it`，
形狀是 **pick → CLEAR → assert**，且 explicit id 指向一位**真實在名冊裡**的成員。
重跑該 mutant：**1 紅**（唯一紅的就是新測試），錯誤 `AssertionError: expected 'Bob' to be 'Mira'`；
還原後該檔 5 支全綠（原 4 支 → 5 支）。

**連帶修正**：舊測試 `does NOT freeze an EXPLICIT chatId onto the default` **名不副實**（它斷言的是
T-661b 收窄，不是凍結），已改名為
`renders an unresolvable EXPLICIT chatId's own history, never the default member's room (T-661b)`，
並在碼上註明改名理由。**那個過度承諾的名字正是這個缺口能藏住的原因之一** —— 它讓每個讀測試清單的人
（包括寫 `frontend/CLAUDE.md` 那句話的我們自己）以為該方向已被保護。

**`frontend/CLAUDE.md:107`「兩個方向都釘(不抽換 / 沒有凍過頭)」這句話，在寫下當時是假的**
（該句由本 commit 新增）。補上新測試之後它才成真。**這句沒有被改小，是被補到成立。**

---

## 3. 視覺驗證（實際看過圖，不只確認檔案產生）

`npm run dev` + Playwright（mock 模式，用 dev-only `__mockSeed` stage 出 4 位成員、置頂 2 位）。
截圖存於本次 session scratchpad（`g-*.png`）。

| 畫面 | 觀察 |
|---|---|
| desktop 1440 混合置頂 | 置頂組 = Bob Chen / Cara Lin（**Bob 後置頂 → 在上**，符合契約 2 的「新置頂在上」）；hairline 落在 Mira 那張卡的上緣，線細、灰度低，與卡片節奏相稱。**`margin: 18px 0` 沒有照抄** —— 實測用 `padding-top: 8px` 搭配列表原本的 8px gap（線上下各 8px），視覺上是一道呼吸，不是一個洞。 |
| 768 / 390 混合置頂 | 版面正常；`documentElement.scrollWidth - clientWidth` 三個寬度**皆為 0**（無橫向溢位）。 |
| 零置頂（三個寬度） | 無 group wrapper、無 hairline，與改動前一致。 |
<!-- 誠實更正（review O2 之後）：上面這格當初寫「無 group wrapper」時**是不準確的** ——
     截圖那一輪的實作在零置頂時仍然渲染了一層 `.office__roster-group`，只是它視覺上不可見，
     所以看圖看不出來。我把一個「看不到」寫成了「不存在」。O2 修掉 wrapper 之後這句話才成立，
     而現在守住它的是 DOM 斷言（`OfficePage.roster-order.test.tsx` 的零置頂那支直接斷言成員卡是
     `.office__members-list` 的**直接子節點**），不是截圖。截圖證明得了的東西有邊界，這是一例。 -->
| 成員詳情面板 | 置頂鈕在身分卡動作區，點擊後文字由「置頂」變「取消置頂」（`aria-pressed` 由 false → true）。 |

**同時順帶看到 P2 的效果**：置頂 Bob / Cara 之後左欄首位換人，右側聊天視窗**仍停在 Mira**。

⚠️ **未做**：`prefers-reduced-motion` / reorder 動畫 —— 設計明確不做（Iris 決策 1b，P1 不解）。
⚠️ **未做**：真實 server（非 mock）的手動點測。純 FE 的驗證紀律照 `frontend/CLAUDE.md`
（headless build → preview → Playwright，CI 綠即 land），server 端行為由 Go test + conformance 承擔。

### 3.1 UX／responsive／a11y 驗收（**執行於 commit `3cdad93`**）

⚠️ **標題必須寫死執行當下的 commit，不可寫「最終基底」。** 本節初版寫的是
「在最終基底（`3cdad93`）」，而 `3cdad93` **在寫下後就不再是最終**（O2／O5 修正之後
HEAD 成為 `3761bd2`）。獨立 review 指出這是**同一類錯誤在本票的第三次出現**
（round1 的 manifest 數字、round2 的 §4、以及本節）。**通則：凡是會隨時間失效的形容詞
——「最終」「目前」「最新」——一律換成當下那顆 SHA。**

**本節之後的改動是否影響這裡的結論？逐項對過，結論是不影響，但有一格是空白的：**
- O2 只改變**「全部置頂」**這一格的渲染（不再輸出空 wrapper）；零置頂與混合置頂兩條路徑
  **位元組完全相同**。
- 而本節 stage 的是 4 位成員、最多置頂 2 位，所以**每一張截圖都落在零置頂或混合置頂**，
  沒有一張是全置頂。因此下表第 1–7 項**全部仍然成立**。
- 🔴 **但要誠實說清楚：「全部置頂」的畫面本身沒有被截圖驗證過**（它不在下表的驗收清單裡，
  該清單第 6 項驗的是零置頂）。它目前**只由 component 測試涵蓋**——而那支測試的斷言強度是
  「列表子節點恰為 `[置頂組]`」，連「wrapper 只是掉了 testid」都擋得住。
  **這一格記為「已由測試涵蓋、未經視覺驗證」，不寫成已驗證。**

§3 那輪是在**舊基底**跑的，依本檔一貫的規矩不能沿用，故整套重做。
做法：`npm run dev`（mock 模式）＋ Playwright/Chromium，用 dev-only `window.__mockSeed`
stage 出 Mira／Bob／Cara／Dan 四位。**23 張截圖逐張開圖看過**，不是只確認檔案產生。

⚠️ **重做時發現一個會讓人白忙的細節，記給後人**：mock 的 `unread_count` 與 `last_activity_at`
是**從 `chatLog` 即時計算**的（`mock.ts` 的 `unreadCountOf` / `lastActivityOf`），
所以直接在 member 物件上注入這兩個欄位會被**靜默忽略** —— 必須用真的聊天列去 stage。
**「我設了值但它沒生效，而且沒有任何錯誤」正是最難察覺的那種假驗證。**

Stage 出的狀態：Dan＝2 未讀但活動最舊（-7200s）、Bob＝無未讀且最新（-60s）、
Cara＝無未讀（-1800s）、Mira＝從未互動（0）。

| # | 驗收項 | 結果 | 證據 |
|---|---|---|---|
| 1 | 未讀（紅燈）優先於無未讀 | ✅ 通過 | Dan 帶紅色 `2` 排在最上，**儘管他的活動時間最舊** —— 這正是該層真的生效的證據 |
| 2 | 無未讀者之間依最近互動新到舊 | ✅ 通過 | Bob(-60s) → Cara(-1800s) → Mira(從未)，三種寬度皆同 |
| 3 | 置頂新增**與**取消 | ✅ 通過 | 加：置頂組＝[Bob, Cara]，新置頂在上，`aria-pressed` false→true、標籤 置頂→取消置頂；取消：Bob 回到他的最近互動位置，Cara 仍置頂；全部取消後回到第 1／2 項的順序 |
| 4 | 選中列不因 live reorder 被抽換 | ✅ 通過 | 顯式選取：Cara 發訊息跳到頂端並帶紅燈，**聊天窗仍停在 Bob**、hash 仍 `#office/chat/bob`，且 `member-card--selected` 隨 Bob 一起移動（DOM 驗證）。空選擇：連續兩次重排，畫面仍停在 Mira |
| 5 | 390 / 768 / 1440 三寬度 | ✅ 通過 | 每個狀態、每個寬度皆 `scrollWidth - clientWidth === 0`（`documentElement` 與 `body` 都驗）；置頂鈕完全在視窗內，且其中心點 `elementFromPoint` 回傳按鈕本身（無遮擋） |
| 6 | 零置頂＝與上線前一致 | ✅ 通過 | 無 hairline、無 group wrapper、`.office__roster-group` 數量為 0，且每張成員卡都是 `.office__members-list` 的**直接子節點** |
| 7 | 鍵盤可操作 + 焦點回饋 + 無 focus trap | ✅ 通過 | 實際按鍵：Tab 14 次到 avatar 按鈕、Enter 開面板、面板內 4 次 Tab 到置頂鈕；`:focus-visible` 有 `outline`，截圖可見亮環；Enter 與 Space 皆可切換（是真正的 `<button>`）；再往後 5 次 Tab 焦點離開面板到 `BODY`，Shift+Tab 可原路走回 → **無 trap** |

**同時記下兩點視覺觀察（非缺陷，但不隱藏）**：
1. 390px 下動作區按鈕全寬堆疊時，「喚醒」置中對齊、「置頂」靠左對齊 —— 純外觀不一致。
2. 頂部導覽頁籤列在 390px 會橫向裁切（「使用說明」被切）。**屬既有 app chrome、有自己的捲動容器、
   頁面層級溢位仍為 0**，與本票無關，僅記錄。

**未驗證（明確列出，不得被讀成已驗證）**：
- **真實 server（非 mock）的置頂持久化** —— 此處全走 mock adapter；server 側僅由 Go 測試涵蓋。
- **跨裝置／reload 後的 pin 一致性** —— mock 在 reload 時重置模組狀態，無法在此觀察。
- **螢幕閱讀器實際朗讀** `role="group"` + `aria-label` —— 只驗證了 DOM 屬性存在，**沒有跑真的輔助技術**，
  所以「聽起來對不對」是未知。
- **真實觸控裝置** —— 全部輸入都是 Chromium 合成的滑鼠／鍵盤事件。

---

## 4. 權威 gate：`bash bin/ci.sh`

**第一次**：`rc=1`，最後一行 `[ci] fix with: gofmt -w server/ocserverd`
（wire.go 加註解後 struct tag 對齊需要重排）。已跑 `gofmt -w server/ocserverd`。

**第二次（最終）**：

```
$ cd ~/.officraft/agents/ow-5f4832973889/worktrees/member-roster-priority-sorting
$ bash bin/ci.sh
rc=0
$ tail -n 1 ci.log | grep -qFx '[ci] all green'   →  MATCH
```

| 判準（root `CLAUDE.md` §13：兩個條件都要） | 結果 |
|---|---|
| `rc == 0` | ✅ rc=0 |
| `tail -n 1 \| grep -qFx '[ci] all green'` | ✅ 精確相等（非寬鬆 grep） |

**第三次（文件補完後重跑）**：`rc=0`、`tail -n 1 | grep -qFx '[ci] all green'` → MATCH、3586 行。
理由：第二次跑之後又新增了 `docs/T-ed38/change-manifest.md` 並補寫本檔，
CI 第 7 道有 path denylist + gitleaks，**綠燈必須是對最終工作樹跑出來的**。

**第四次（rebase 到 `8a5f1fb` 之後，commit `76ad9a4`）**：`rc=0`、`tail -n 1` 精確 `[ci] all green`。
基底換過就重跑，舊基底的綠燈不算數。

**第五次（補上「凍過頭」新測試之後）**：對**工作樹**與 **amend 後的 commit** 各跑一次，兩次皆
`rc=0` ＋ 最後一行精確 `[ci] all green`（依 root §13「驗的是 commit 不是 tree」）。

### ⚠️ 這份文件無法記錄「最終 commit」自己的 CI 結果 —— 這是遞迴，不是疏漏

本檔**在 commit 內**。所以把「commit X 的 CI 結果」寫進本檔，這個寫入動作就會產生
commit X'（≠ X）；再為 X' 重跑並記錄，又產生 X''。**追不上，永遠差一次。**

因此本節的規則是：

- **上表每一次都明確標示它涵蓋的是哪個工作樹／哪個 commit**，不含糊帶過。
- **最終 land 的那顆 commit，其權威 CI 執行結果記錄在 commit 之外**——PR 內文的
  `## Checks` 段與任務紀錄。那兩處不是 commit 的一部分，所以不觸發遞迴。
- 判準永遠是同一條（root `CLAUDE.md` §13）：**`rc == 0` 且 `tail -n 1 | grep -qFx '[ci] all green'`，
  兩個條件都要**，寬鬆 grep 不算。

**這一節是被獨立 review 擋下來才寫成這樣的**（review-round2 的 B1）：原本 §4 一邊立下
「綠燈必須是對最終狀態跑出來的」這條標準，一邊記錄的卻是上一顆 commit 的結果，
**自己的證據撐不起自己的宣稱**。reviewer 拒絕背書是對的。

完整 log 在本次 session scratchpad（`ci2.log` / `ci3.log` / `ci-final.log`）。


---

## 5. 尚未驗證 / 已知限制（誠實列）

1. **跨裝置一致性未實測**：settings 是 server-backed，但「另一台瀏覽器 reload 後看到同樣的置頂」
   只由 server 測試（PATCH 後 GET 回同一組）間接支持，沒有真的開兩個瀏覽器驗。
2. **無 settings SSE topic** → 別的裝置改置頂**不會即時反映**。這是設計明講不承諾的（ux-design §5），
   不是遺漏。
3. **`ChatReplyCard.markdown-render` flake**（見 §1.3）根因未查。
4. ~~**排序的可觀察副作用，設計文件內部有一處張力**~~ —— **已解決**。原本 tech-design §1.3
   （靠穩定 sort 繼承 server 姓名序）與 §1.4（第 5 層純 `id` 全序）互相矛盾，實作照 §1.4 做並
   標記出來。O-3 拿去跟 Iris 確認後改寫了 §1.4：第 5 層改成 **`name.toLowerCase()` → 同名再比
   `id`** 的兩段式，前端排序因此**自足**（server 回什麼順序都不影響結果），不再暗中依賴
   `ORDER BY name COLLATE NOCASE` 這個從未寫進契約的假設。碼、`rosterOrder.ts` 註解與
   `frontend/CLAUDE.md` 均已同步。
   ⚠️ **仍是刻意接受的代價**：5a 用 `toLowerCase()` + `<` / `>`（**不可用 `localeCompare`**，
   它依賴 runtime ICU 資料，會讓非 ASCII 名字在 jsdom 綠、真實瀏覽器另一個順序），
   所以**非 ASCII 名字的順序不保證符合該語言直覺** —— 首要目標是決定性，不是完美 locale 排序。
5. **`?fields=light` 的 `last_activity_at` = 0（未計算）與「從未互動」在 wire 上不可分辨**。
   已寫進 spec description 與兩邊的碼註解，但這是**契約層面接受的模糊**，不是被解掉的問題。
