# T-ed38 · 變更 manifest

基準 commit：`6158b32`。**每一個**新增／修改的檔案逐條列出必要性（root `CLAUDE.md` §7：
push 前逐檔講得出必要性，不把第一道審查讓給 gate）。

**沒有刪除任何檔案。沒有 migration。沒有新增端點／路由。**

`git show --name-status` 摘要（**對本分支的 commit 實跑，不是憑記憶**）：**29 個修改檔、
19 個新增檔、0 個刪除檔**，共 **48 檔**。19 個新增檔中有 **11 個是 `docs/T-ed38/*.md`**
（B1 之後才納入版控，見 §G）。

⚠️ **這一行的數字錯過兩次，兩次都是獨立 review 抓到的**，所以把更正史留著：
- 第一版寫「27 個修改檔、8 個新增檔（含 `docs/T-ed38/`）」——**與當時的 commit 不符**，
  `docs/T-ed38/` 根本不在那顆 commit 裡（round1 B1）。
- 第二版寫「28 修改／18 新增／46 檔」，仍與實況差一——round2 review 當下的實況是
  **29 修改／18 新增／47 檔**（round2 O1）。
- 目前的 **29／19／48** 與上一行差一個新增檔，差的就是 **`review-round2.md` 本身**
  （review 產出後才納入版控）。**特別寫明這一筆，免得下一個讀者把它當成第三次算錯。**

**教訓：這種摘要數字必須從 `git show --name-status` 現算，不能沿用上一輪再手調** ——
兩次錯都是「在前一版數字上加減」造成的。

⚠️ **基底已換過兩次**：本檔開頭的「基準 commit `6158b32`」是**最初**的基底；分支其後 rebase 到
`a7a0e35`，再 rebase 到 **`8a5f1fb`（＝目前 `origin/main`，也是本 commit 的 parent）**。
上面的數字以 **`8a5f1fb`** 為準。（第二版曾寫「本分支尚未跟上 `8a5f1fb`」——**那句在寫下時就已過期**，
分支當時已經 rebase 完成，round2 O1 指出此誤，已刪。）

---

## A. spec（wire SSOT — 必須先行，否則生成物 gate 必紅）

| 檔案 | 動作 | 為什麼必要 |
|---|---|---|
| `spec/openapi.json` | 修改 | 三個 additive-optional 欄位：`MemberDTO.last_activity_at`、`SettingsDTO.pinned_member_ids`、`SettingsUpdateDTO.pinned_member_ids`。`MemberDTO` 帶 `additionalProperties: false`，不改這裡就無法上 wire。三處都**不進 `required`**（root §12）。description 依 Atlas 要求寫明「`0` 在 full 是無互動、在 `?fields=light` 是未計算」。 |
| `spec/mcp-catalog.json` | 修改 | `update_settings` 工具的 inputSchema 補 `pinned_member_ids`。**不是可選的**：`TestFrozenCatalogAgreesWithOpenapiOnEveryToolsParameters` 要求 catalog 與 openapi 對每個工具的參數集合逐字相符；不補這裡 CI 直接紅（實際發生過，見 verification.md §2）。 |

## B. 生成物（**未手改，全部由工具重生**）

| 檔案 | 動作 | 為什麼必要 |
|---|---|---|
| `server/ocserverd/ocapi_gen.go` | 重生 | `bash bin/gen-ocapi`。CI step 1g 驗它能從凍結 spec byte-identical 重生。diff 僅新欄位 + gofmt 對齊。 |
| `frontend/src/api/generated/schema.ts` | 重生 | `npm run gen:api`。CI step 4 驗 FE schema drift。 |
| `frontend/src/i18n/messageKeys.generated.ts` | 重生 | `npm run gen:msgkeys`（新增三個 i18n 葉子後）。主題包 `wording` 覆寫的白名單。 |
| `server/ocserverd/message_keys_gen.go` | 重生 | 同一支 `gen:msgkeys` 同時產出的 server 端孿生，必須同批。 |

## C. server

| 檔案 | 動作 | 為什麼必要 |
|---|---|---|
| `server/ocserverd/dal.go` | 修改 | 新增 `MemberChatStats` 型別 + `ListMemberChatStats(actor)`：一次 SQL 同時折出 unread 與 last_activity（Atlas 指定形狀）。**取代**（非並存）roster handler 舊的 `ListChat()+ListChatReads()+UnreadCounts`，連帶修掉「整張無 LIMIT 的 chat 表搬進 Go」。`ListChat()` 本身**保留**（其他 5 處消費者仍在用，也是這條的回退路徑）。 |
| `server/ocserverd/api_members.go` | 修改 | roster handler 改呼叫新 helper；`?fields=light` 分支不算，`last_activity_at` 因此誠實留 0（= 未計算）。 |
| `server/ocserverd/api_helpers.go` | 修改 | `newMemberDTO` 的第 4 個參數由 `unreadCount int` 換成 `stats MemberChatStats`，並投影 `LastActivityAt`。改參數型別而非再加一個位置參數，是為了讓「這個面不計算 caller-relative 統計」只有一種寫法（零值）。 |
| `server/ocserverd/api_roles.go` | 修改 | 上述簽章變更的 1 個呼叫點（`0` → `MemberChatStats{}`）。**純機械改動**。 |
| `server/ocserverd/api_member_avatar_test.go` | 修改 | 同一個 `newMemberDTO` 簽章變更的**測試側**呼叫點（`newMemberDTO(*stored, "", "", 0)` → `MemberChatStats{}`）。**純機械跟隨簽章**，斷言一字未動；不改就編譯不過。（獨立 review O4：本列原本漏列 —— §7 要求的是 `git show --stat` 的**每一個**檔都講得出必要性，少一列就是那道人眼防線漏了一格。） |
| `server/ocserverd/wire.go` | 修改 | 手寫 response DTO（`server/CLAUDE.md` 明載這才是上線那一份）：`memberDTO.LastActivityAt`、`settingsDTO.PinnedMemberIDs`。加註解導致 gofmt 重排整個 struct 對齊區塊 —— **那段對齊變動是 gofmt 強制的，不是順手格式化**（CI step 有 gofmt gate）。 |
| `server/ocserverd/settings.go` | 修改 | 新 settings key `display.pinned_member_ids` 常數 + `authSettings` 欄位 + boot-time load（JSON 反序列化，缺／空 = nil）。 |
| `server/ocserverd/api_stub.go` | 修改 | `apiServer` 的 live in-memory snapshot 欄位（settings 一律 DB-first + 記憶體 snapshot，免每請求讀 DB）。 |
| `server/ocserverd/server.go` | 修改 | 1 行：把 boot snapshot 蓋到 apiServer 上，與其他 12 個 display.* 設定同一處。 |
| `server/ocserverd/api_settings.go` | 修改 | PATCH 驗證（空字串／重複 id → 422，**全部驗完才寫**）＋ 原子整組 replace ＋ GET 永遠回 array（never null）。 |

### server 新增測試

| 檔案 | 為什麼必要 |
|---|---|
| `server/ocserverd/dal_member_chat_stats_t_ed38_test.go` | 新 SQL 算錯 unread 會**破壞既有紅燈**（已上線數月的功能），所以測試把新 aggregate 與舊 `UnreadCounts` 在同一份 fixture 上**逐 peer 對質**，而不只是斷言新欄位。另外釘住 last_activity 的 caller-relative + 雙向語意（agent↔agent 訊息不得洩進 owner 的數字），與「從未互動 = 不在 map 裡」。 |
| `server/ocserverd/api_settings_pinned_t_ed38_test.go` | 釘住四件會靜默出錯的事：永遠是 array 不是 null、順序逐字 round-trip、整組 replace 且省略 = 不動、422 之後**什麼都沒寫**。 |

## D. frontend seam（wire → mappers → types → adapter → mock/http → hooks → component）

| 檔案 | 動作 | 為什麼必要 |
|---|---|---|
| `frontend/src/types.ts` | 修改 | `Member.lastActivityAt?: number`。optional 是刻意的（舊 server 無此欄；先例 `Member.roleName`）。 |
| `frontend/src/api/mappers.ts` | 修改 | `toMember` 補 `lastActivityAt`（`?? 0`，不可寫 `\|\| 0`）；`toServerSettings` 補 `pinnedMemberIds`（缺席 → `[]`）。 |
| `frontend/src/api/adapter.ts` | 修改 | `ServerSettingsView.pinnedMemberIds` + `ServerSettingsPatch.pinnedMemberIds`（seam 的型別契約，mock 與 http 共用）。 |
| `frontend/src/api/http.ts` | 修改 | PATCH body 補 `pinned_member_ids`（只有顯式傳入才送，維持 partial-patch 語意）。 |
| `frontend/src/api/mock.ts` | 修改 | (1) settings 的 pinned 讀寫 + 與 server 同規則的 422 驗證（`frontend/CLAUDE.md`：mock 必須與 http 行為一致）；(2) `lastActivityOf()` live 計算，與 `unreadCountOf` 同一種誠實方式；(3) 三個 wire fixture 補 `last_activity_at`（生成型別要求）；(4) **新增兩個 test-only hook** `__injectMockMember` / `__emitMockTopic` —— seed roster 只有一位正職成員，任何「排序」測試都需要更多列，而 `__injectMockChat` 刻意不 fan topic，所以要重排就必須能顯式驅動 refetch；(5) dev-only `__mockSeed` 補 `injectMember` / `emitTopic`，讓 Playwright 截圖能 stage 出「混合置頂」畫面（那正是該 block 既有的用途，production build 會 dead-code 消除）。 |
| `frontend/src/api/mappers.presence.test.tsx` | 修改 | 1 行：wire fixture 補 `last_activity_at: 0`（生成型別把它列為 required）。**純編譯需求，斷言未動。** |

## E. frontend 行為

| 檔案 | 動作 | 為什麼必要 |
|---|---|---|
| `frontend/src/lib/rosterOrder.ts` | **新增** | comparator 抽成純函式（`compareMembers` / `pinIndexOf` / `splitPinned`）。內聯在 component 裡測不到，而現行排序**零護欄** —— 這是本票測試策略的前提。 |
| `frontend/src/hooks/usePinnedMembers.ts` | **新增** | 置頂讀寫 seam。純 server、**無 localStorage**（比照 `useOrgName`，理由更強：快取住一個別處已取消的 pin 會讓那一列短暫復活）。讀失敗 → `[]`；寫失敗 → 回退。 |
| `frontend/src/components/OfficePage.tsx` | 修改 | (1) 改呼叫 comparator；(2) **P2 修正**：空選擇的預設對象改成首次解析後存 ref（實測過的抽換，見 verification.md §0.5），範圍嚴格限制在「求值時機」——T-661b 收窄、URL hash 語意、三個 `setSelectedId` 呼叫點都未動；(3) 分組渲染（`role="group"` + hairline）＋ 共用 `renderMemberCard`；(4) 把 pin 狀態與 toggle 接到詳情面板。 |
| `frontend/src/components/MemberDetailPanel.tsx` | 修改 | 新增 optional `pinned` / `onTogglePin` 與置頂切換鈕。放這裡而非列上，是因為兩條 owner 裁定管著 roster 列（`MemberCard.tsx:84-86` / `:96-110`）。**optional 是必要的**：MonitorPage 也渲染這個面板但沒有 roster 可置頂，未傳就不顯示。 |
| `frontend/src/components/office.css` | 修改 | 兩個新 class：`.office__roster-group`（保住列表原本 8px 節奏，讓「沒有置頂時視覺完全不變」成立）與 `.office__roster-group--divided`（hairline）。**未複用 `.doc-md hr` 的 class**、**未動 `office.css:201-226` 的 baseline divider**。 |
| `frontend/src/i18n/locales/zh.ts`、`en.ts` | 修改 | 三個**靜態字串葉子** `office.pinnedGroup` / `pinMember` / `unpinMember`（不可寫成 interpolation 模板，否則主題包 `wording` 覆寫看不見它們）。 |

### frontend 新增測試

| 檔案 | 為什麼必要 |
|---|---|
| `frontend/src/lib/rosterOrder.test.ts` | 四層各一組不含糊的資料、契約 2（置頂組不被 unread/最近互動攪動）＋其對照組、S2 打亂輸入輸出恆等、S5 三種 fallback、孤兒 pin。 |
| `frontend/src/components/OfficePage.roster-order.test.tsx` | 排序真的走到畫面上，加上分組三態邊界（零／全部／混合）與孤兒 pin 的 render 行為。 |
| `frontend/src/components/OfficePage.selected-stability.test.tsx` | P2 契約，**兩個方向都釘**：不得自己抽換，且不得凍過頭（T-661b 收窄必須還在）。 |
| `frontend/src/components/MemberDetailPanel.pin.test.tsx` | 置頂／取消（含「新 pin 進最前面」）＋ settings 讀失敗降級、寫失敗回退。 |

## F. 文件

| 檔案 | 動作 | 為什麼必要 |
|---|---|---|
| `frontend/CLAUDE.md` | 修改 | (1)(2) owner `rc-563734cd294e` 核可的兩處過時描述修正：unread badge 壓制條件補上 `windowActive`；左欄結構改為描述 T-66a8 的頂部文字頁籤。(3) 新增「roster 排序 + 手動置頂」一節 —— root `CLAUDE.md` §8 要求改碼與更新其 context doc 在同一個 commit。 |
| `docs/T-ed38/verification.md` | 修改 | P2 後果鏈驗證、控制組驗證、CI 實際結果。 |
| `docs/T-ed38/change-manifest.md` | **新增** | 本檔。 |
| `docs/T-ed38/*.md`（其餘 **7** 檔） | 新增（先前步驟產出） | `problem-framing` / `baseline` / `frontend-baseline` / `backend-baseline` / `impact-inventory` / `ux-design` / `tech-design` —— 設計階段的既有產物。（⚠️ 本列原寫「其餘 6 檔」卻列出 7 個，round2 O1 抓到；**數字與清單長度不一致就是沒有現數過**，已改為 7 並把 7 個檔名逐一寫出，讓下次可以直接對。`ux-design` 本輪有改，見末節。） |

## G. 獨立 review（`review.md`）之後的修正

`review.md` 判 REQUEST CHANGES（B1/B2 阻擋 + O1–O4 觀察）。以下是為此新增／再動的檔，逐條理由：

| 檔案 | 動作 | 為什麼必要 |
|---|---|---|
| `docs/T-ed38/`（全 10 檔） | **納入版控**（B1） | commit 裡有三處碼／文件引用 `docs/T-ed38/verification.md §0.5`（`OfficePage.tsx`、`OfficePage.selected-stability.test.tsx`、`frontend/CLAUDE.md`），而該路徑原本完全不在 commit 裡 → land 後變**懸空引用**，且 P2 的唯一實測證據就在那份文件中。先例：`docs/T-081b-evidence/`（隨碼 commit，已確認存在）。納入前掃過全部 10 個 `.md`：無憑證／token／私訊原文，全為 text/plain，最長的 hex 字串是 git SHA。 |
| `server/ocserverd/api_settings.go` | 修改（B2） | `pinned_member_ids` 原本只驗空字串與重複 id，**不驗陣列長度、也不驗單一 id 長度**。新增 `maxPinnedMemberIDs = 100` / `maxPinnedMemberIDLen = 64`，**沿用隔壁 `theme_bundle.go:37-39` 的 `maxCustomThemes = 100`**（同一張 settings 表、同一個「一個 JSON row，無界陣列是唯一撐爆它的方式」理由，連註解風格一起沿用）。錯誤語意與該檔既有驗證失敗路徑一致（422 + `fmt.Sprintf` 帶出常數，訊息不會與常數漂開）。寫入門檻是 `admin_agent`＝owner **與任何 assistant agent**，不是只有人類。 |
| `server/ocserverd/api_settings_pinned_t_ed38_test.go` | 修改（B2） | 新增 `TestPinnedMemberIDsBoundTheArrayAndEachID`：超過上限被拒、**剛好在上限可通過**、單一 id 超長被拒、id 長度邊界可通過，且每次拒絕後回頭 GET 確認**什麼都沒寫**。三顆 mutant 實測有鑑別力（verification.md §2.4）。 |
| `frontend/src/components/OfficePage.tsx` | 修改（O1 + O2） | **O1**：`defaultChatIdRef` 原本只在還是 `""` 時寫入，記住的預設成員被解僱後 `find` 永遠 miss、`defaultSelected` 退回每 render 重算 `roster[0]` —— P2 要修掉的行為在該 session 復活。改成 fallback 命中時把 ref 一起重新錨定。**O2**：零置頂時不再渲染 group wrapper（Iris 契約 3e#1 字面；她的裁定與理由見下）。列的渲染邏輯**沒有被複製**——`unpinnedRows` 只 map 一次，條件式的只有那層 `div`。 |
| `frontend/src/components/OfficePage.selected-stability.test.tsx` | 修改（O1） | 原本第 4 支只斷言「會 fallback 到 Bob」，**沒有斷言 fallback 之後又穩定下來**，所以護欄抓不到 O1。擴寫成「解僱 → fallback → 再來一次重排 → 對話仍不得被抽換」，並保留正向控制（先斷言 roster 真的重排了）。 |
| `frontend/src/components/OfficePage.roster-order.test.tsx` | 修改（O2） | 零置頂那支原本測的是 `pinned-group` 不存在與 `--divided` 不存在，**不是契約原文**。補上 `unpinned-group` 不存在，並斷言成員卡是 `.office__members-list` 的**直接子節點**——後者才擋得住「wrapper 只是掉了 testid」。 |
| `docs/T-ed38/verification.md` | 修改（O3） | §2.3 原稱「舊控制組一律不算數，整組重做」，實際只重做 3 顆、**M1 與 P2-B 沒重做**。已更正該句，並新增 §2.4 記錄在**現行基底**補做的 M1 / P2-B 與本輪新增測試的鑑別力驗證（含實際指令、紅的支數與測試名）。另更正 §3 一格把「看不到 wrapper」寫成「無 wrapper」的不準確描述。 |
| `docs/T-ed38/change-manifest.md` | 修改（O4 + 本節） | 補上漏列的 `api_member_avatar_test.go`，並新增本節。 |

**O2 的授權來源（不是我自行判斷）**：Iris 裁定照字面拿掉 wrapper，理由是「零置頂是每一個從未使用
這功能的人的預設狀態，在那個狀態下 DOM 應該跟功能上線前一模一樣」、「一個今天不影響任何東西的空
wrapper，正是哪天有人在父層加上 gap 就開始承重的元素——而那時沒有任何測試會變紅」。她同時給了會
讓她改判的條件：**若拿掉 wrapper 會逼出兩份重複的列渲染邏輯，她寧可留 wrapper**。
→ **該條件未觸發**：`renderMemberCard` 本來就已抽成共用函式，本次只是把 `unpinnedRoster.map(...)`
存成 `unpinnedRows` 求值一次，再決定要不要包一層 `div`。**沒有第二份列表渲染。**

---

## 明確**沒有**動的東西（設計要求的邊界）

- 無 migration（刻意；避開與 PR #12 的 `00040` out-of-order 起站失敗）。
- 無新端點、無新路由列 → 不觸發 `authz_surface_gate_test.go` 的清單維護。
- `mappers.ts:141` 的 `role_key || "assistant"` fallback。
- `unread_count` 的壓制條件 `selected && windowActive`（**碼**未動，只修了描述它的文件）。
- `office.css:201-226` 的 baseline divider。
- `OfficePage.tsx` 的 T-661b 收窄（並有測試釘住它沒被動到）。
- 外包列表排序、`ListChat()` 本身、其他 5 處 `UnreadCounts` 消費者。
- **`spec/openapi.json` 沒有為 B2 的上限加 `maxItems` / `maxLength`** —— 刻意照最近的先例辦：
  隔壁的 `custom_themes` 也是 `maxCustomThemes = 100` 只存在於 Go、**spec 上沒有宣告**
  （已用 python 直接讀 JSON 確認，不是看 diff 猜的）。因此本次是純 server 端驗證，**wire 未動、
  生成物未重生**，不觸發 wire-freeze 流程。要不要把兩個上限都補進 spec 是另一個題目（需 owner 過目）。
- **`frontend/src/api/mock.ts` 沒有鏡像 B2 的長度上限** —— 同一個先例：mock 也沒有鏡像
  `maxCustomThemes`。mock 目前鏡像的是空字串／重複 id 兩條（那兩條的 422 是 UI 走得到的）。
  誠實記下這是**已知的 mock↔http 不完全對等**，與既有 `custom_themes` 的狀態一致，不是本次新造的落差。

---

## 第二次 rebase（`a7a0e35` → `8a5f1fb`）之後再動的檔

基底在本票進行期間**第二次**前進。以下是這一輪新增／再動的檔，逐條理由：

| 檔案 | 動作 | 為什麼必要 |
|---|---|---|
| `CLAUDE.md`（repo root） | 修改 | §11 commit 訊息格式改寫為 `<type>(<scope>): <subject>` ＋ `[why]`／`[how]` body。**來源是 owner 2026-07-28 卡 `rc-9fe87f4e0099` 的明確裁定**：任務手冊的新格式（`rc-a202dc39521c`）與本 repo §11 的舊格式互相矛盾，owner 選了「照新格式寫，**並順手把 repo 那條規定也改成新格式**」。⚠️ 這是**兩份權威打架**的案例，沒有自行裁定，開卡問過。改前已實查 `.githooks/` 與 `bin/` **無任何 commit-msg 格式 gate**，所以改文件不會讓 commit 被擋。repo 原有的「結尾署真實模型名」要求**不衝突，原樣保留**。 |
| `frontend/CLAUDE.md` | 修改（O6） | 外包面板那段的括號拖過長，`成員卡=名字+…` 懸在一個很長的括號之後，讀起來像在描述**外包卡**（實際描述的是正職成員卡）。把括號收在頁籤說明之後結句，另起「**正職成員卡** = …」。**純文氣修正，內容未改**——內容本身是 owner `rc-563734cd294e` 已核可的修正。 |
| `frontend/src/components/OfficePage.selected-stability.test.tsx` | 修改（控制組 §2.5） | (1) **新增** `clearing an EXPLICIT pick returns to the original default …`：pick → **CLEAR** → assert，且 explicit id 指向**真實在名冊裡**的成員。這是唯一能觀察到「凍過頭」的形狀，補之前該方向**零測試保護**（全 1368 支測試無一偵測得到，見 verification.md §2.5）。(2) **改名** `does NOT freeze an EXPLICIT chatId onto the default` → `renders an unresolvable EXPLICIT chatId's own history, never the default member's room (T-661b)`——舊名描述凍結、實際斷言的是 T-661b 收窄，**那個過度承諾的名字正是缺口能藏住的原因之一**。斷言內容一字未動，只改名並註明理由。 |
| `docs/T-ed38/verification.md` | 修改 | 新增 §2.5：記錄第二次換基底後重做 M1／P2-B 的真實結果，包含 **§2.4 的 P2-B 是一顆寫錯方向的 mutant、給了假信心**這件事，以及缺口的兩個成因、非等價變異的探針證明、修法與修後的紅／綠證據。 |
| `docs/T-ed38/change-manifest.md` | 修改 | 本節。 |

**這一輪沒有動任何產品邏輯。** 唯一的碼變更是**新增一支測試**與**一次測試改名**；`rosterOrder.ts`、
`OfficePage.tsx` 的產品路徑、以及 server 端一律未動（控制組的變異全部還原，工作樹已驗證乾淨）。

**誠實揭露**：`frontend/CLAUDE.md:107`「護欄兩個方向都釘(不抽換 / 沒有凍過頭)」這句話由本 commit
新增，而**在寫下當時它是假的**——凍過頭那個方向當時沒有任何測試守。它**不是被改小，是被補到成立**。

---

## 獨立 review round2 之後再動的檔

`review-round2.md` 判 **REQUEST CHANGES（1 項阻擋）**＋8 項非阻擋觀察。逐條處置：

| 檔案 | 動作 | 為什麼必要 |
|---|---|---|
| `docs/T-ed38/review-round2.md` | 新增 | 第二輪獨立 review 的產出（reviewer 為未參與實作的第三個 actor）。與 round1 的 `review.md` **並存不覆蓋**——兩輪的判斷過程本身就是交付證據的一部分。reviewer 在 scratchpad 的隔離副本跑了 **24 顆變異：23 顆被殺、1 顆等價**，工作樹全程未被改動。 |
| `docs/T-ed38/verification.md` | 修改（**阻擋 B1**） | §4 一邊立下「綠燈必須是對最終狀態跑出來的」，一邊記錄的卻是**上一顆 commit**（`76ad9a4`）的結果 —— **自己的證據撐不起自己的宣稱**，reviewer 拒絕背書是對的。已補記第四、五次執行，並新增一節說明**這份文件在本質上記錄不了「最終 commit 自己的 CI 結果」**：本檔在 commit 內，寫入結果就會產生新 commit，追不上。解法是把最終 land commit 的權威執行結果放在 **commit 之外**（PR `## Checks` 段＋任務紀錄）。另補 §3.1 的 UX 逐項驗收結果。 |
| `frontend/src/components/OfficePage.tsx` | 修改（O2 addendum） | 全置頂時仍渲染**空的** `unpinned-group` div，位在 `gap: 8px` 的 flex column 中 → 尾端 8px 死白。條件改為 `pinned > 0 && unpinned > 0`。**Iris 裁定（不是我自行判斷）**：「這個 div 不代表任何東西……渲染一個空容器來代表一個不存在的群組，是讓 DOM 說謊；`gap` 只是把謊話變得看得見。」列仍只渲染一次（`unpinnedRows`），沒有第二份列表渲染邏輯。 |
| `frontend/src/components/OfficePage.roster-order.test.tsx` | 修改（O2 addendum） | 原測試把那個空 wrapper **釘成契約**——Iris 明言「它現在守的是錯的東西」。改成與零置頂側對稱：斷言 `unpinned-group` 不存在，**且**列表子節點恰為 `[置頂組]`（後者才擋得住「wrapper 只是掉了 testid」）。檔頭規則 2 一併更新，否則會與它正下方的測試自相矛盾。 |
| `server/ocserverd/api_settings.go` | 修改（O5） | 驗證用 `strings.TrimSpace(id) == ""` 判空，**卻儲存未 trim 的原字串** —— `" m-bob "` 會被收下並永久存成一個**永遠對不到任何成員的孤兒 pin**。這是我們自己新寫的驗證裡的內部不一致，不是既有行為。改為比照同一支 handler 的 `orgName` / `ownerName`：先 trim，再驗證與儲存；**去重也改用 trim 後的值**，否則 `"m-bob"` 與 `" m-bob "` 會雙雙入庫。 |
| `server/ocserverd/api_settings_pinned_t_ed38_test.go` | 修改（O5） | 新增 `TestPinnedMemberIDsAreStoredTrimmed`：padded id 存入後為 trim 值（echo 與後續 GET 兩處都驗），且 padded 重複值回 422 且**什麼都不寫**。 |
| `docs/T-ed38/ux-design.md` | 修改（O2 addendum） | 契約 3e 第 2 條補上 wrapper，並記下 Iris 要求的通則。 |
| `docs/T-ed38/change-manifest.md` | 修改（O1 + 本節） | 修正 headline 數字（29 修改／19 新增／48 檔）、刪除「本分支尚未跟上 `8a5f1fb`」這句寫下時就已過期的話、把「其餘 6 檔」更正為 7 並逐一具名。 |

**四項未修改的觀察，連同理由一起留下（不是漏掉）**：

- **O3 / O4（`spec/openapi.json` 與 mock 未宣告 100 / 64 上限）** —— 維持既有先例：同一張 settings 表的
  `custom_themes` 的 `maxCustomThemes = 100` 也只存在於 Go、spec 與 mock 皆未宣告（已直接讀 JSON 確認）。
  **本票不單方面改變這個慣例**；要不要把上限一併寫進 spec 是另一個題目，需 owner 過目。
- **O6（`pinned_member_ids` 寫入門檻是 `admin_agent`，故任何 `role_key=="assistant"` 的 agent 都能覆寫
  owner 的手動順序，且無稽核軌跡）** —— 這是**既有的權限面**（settings 端點的治理門檻，T-6020），
  不是本票新造的洞；本票只是新增了一個落在該門檻下的欄位。**已明確回報 owner 知悉**，不在本票擅自收緊
  ——改動治理門檻屬跨功能決定。
- **O7（`usePinnedMembers` 無 in-flight 定序，快速連點兩下若回應亂序可能讓 UI 與 server 分歧）** ——
  真實但屬設計層取捨；Atlas 定的寫入語意本來就是「整組原子 replace、last-write-wins」，
  且承諾只到「reload/login 後一致」。記為已知限制。
- **O8（StrictMode 下的行為經探測正確——一次點擊恰好一個 PATCH——但沒有跑在 StrictMode 下的committed 測試）**
  —— 記為已知的測試覆蓋缺口。
