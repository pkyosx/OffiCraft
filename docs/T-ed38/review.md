# T-ed38 · 獨立 review（implementation-blind reviewer）

標的：worktree `member-roster-priority-sorting`，單一 commit `4ca8cd1`，基底 `origin/main` = `a7a0e35`。
方式：`git show 4ca8cd1` 逐檔讀完 36 個檔；讀 `docs/T-ed38/` 五份設計文件 + root/frontend/server `CLAUDE.md`；
親自執行（未修改任何檔案）：

```
$ cd frontend && npx vitest run src/lib/rosterOrder.test.ts \
    src/components/OfficePage.roster-order.test.tsx \
    src/components/OfficePage.selected-stability.test.tsx \
    src/components/MemberDetailPanel.pin.test.tsx
  Test Files  4 passed (4)   Tests  32 passed (32)

$ cd server/ocserverd && go test -run 'TestListMemberChatStats|TestPinnedMemberIDs' ./...
  ok  ocserverd  0.826s

$ cd frontend && npm run typecheck        rc=0
```

**結論：REQUEST CHANGES**（兩項，都很便宜；碼本身的品質高，問題不在邏輯）。

---

## 1. 阻擋項

### B1 — `docs/T-ed38/` 完全沒進 commit，而 commit 裡的碼引用它（§8 + §7）

`git status --porcelain` → `?? docs/T-ed38/`。`git show 4ca8cd1 --stat` 的 36 個檔裡**沒有任何 docs/**。

但這三個**已 commit** 的檔案引用了那個路徑：

- `frontend/src/components/OfficePage.tsx:260` — `// zero user input (docs/T-ed38/verification.md §0.5).`
- `frontend/src/components/OfficePage.selected-stability.test.tsx:4` — 同一個引用
- `frontend/CLAUDE.md:104` — `見 \`docs/T-ed38/verification.md\` §0.5`

**為什麼是問題**：P2 這個改動的**唯一實測證據**就在那份文件裡，碼註解把讀者指過去，而 land 之後
`origin/main` 上那個路徑不存在。這正是 root §8 要防的情況（後人在缺少意圖的碼上疊床架屋），
而且 §7 的 manifest 本體（`change-manifest.md`）自己也不在 commit 裡 —— 它宣稱「8 個新增檔（含
`docs/T-ed38/`）」，與實際 commit 不符。repo 有明確先例：`docs/T-081b-evidence/`、
`docs/design/member-custom-avatar.md` 都是隨碼 commit 的。

**修法**：`git add docs/T-ed38 && git commit --amend`（或第二顆 commit）。

### B2 — `pinned_member_ids` 沒有任何長度上限，與同一支 handler 的既有先例相反

`server/ocserverd/api_settings.go:299-322` 只驗「空字串」與「重複 id」，**不驗陣列長度、也不驗單一 id 長度**。

隔壁 `server/ocserverd/theme_bundle.go:37-39` 對同一張 settings 表寫下了理由：

```go
// maxCustomThemes bounds how many bundles the owner may keep — the setting
// is one JSON row, so an unbounded array is the only way to bloat it.
maxCustomThemes = 100
```

`display.pinned_member_ids` 是**同一種東西**（一個 JSON row、boot 時 `loadAuthSettings` 反序列化進
記憶體 snapshot、每次 `GET /api/settings` 回傳）。寫入門檻是 `admin_agent`，也就是 owner **與任何
`role_key=="assistant"` 的 agent** —— 不是只有人類。

**為什麼是問題**：這不是假想情境的防禦性程式碼，是**同一個檔案裡已經寫下理由的既有模式沒有沿用**
（`org_name`/`owner_name` 80 runes、`custom_themes` 100 筆、`maxColorValueLen` 64）。加一個
`maxPinnedMemberIDs` 常數即可，與現有驗證同一個迴圈。

---

## 2. 非阻擋觀察（依重要性）

### O1 — P2 的 ref 在「被記住的預設成員離開名冊後」永久退化回不穩定的 `roster[0]`

`OfficePage.tsx:268-276`：

```ts
const defaultChatIdRef = useRef<string>("");
if (defaultChatIdRef.current === "" && roster.length > 0) {
  defaultChatIdRef.current = roster[0].id;
}
const defaultSelected =
  roster.find((m) => m.id === defaultChatIdRef.current) ?? roster[0];
```

ref 只在**還是空字串**時寫入。一旦記住的成員被解僱，`find` 之後永遠 miss，`defaultSelected` 就
**每 render 重算 `roster[0]`** —— 也就是 P2 要修掉的那個行為，在該 session 剩下的時間裡回來了。
`selected-stability.test.tsx` 的第 4 支只斷言「會 fallback 到 Bob」，沒有斷言「fallback 之後又穩定
下來」，所以護欄抓不到。

修法一行：fallback 命中時把 ref 一起更新（`defaultChatIdRef.current = roster[0].id`）。
影響面窄（要 owner 在未選取狀態下解僱當前預設對象），所以列非阻擋，但這是**契約本身的一個洞**，
不是邊角美觀問題。

### O2 — 零置頂時仍然渲染了一層 wrapper `<div>`（Iris 契約 3e 第 1 條的字面）

契約寫「零個置頂 → 不渲染 hairline、**不渲染 group wrapper**」。實作永遠渲染
`<div data-testid="unpinned-group" className="office__roster-group">`。
`change-manifest.md` 明白承認並給了理由（`.office__roster-group` 複製 8px 節奏使視覺不變），
a11y 樹也不受影響（沒有 role/aria）。判定：**不是缺陷，是有記錄的偏離**，但 `OfficePage.roster-order.test.tsx`
的「零置頂」那支測的是 `pinned-group` 不存在與 `--divided` 不存在，**不是**契約原文，值得在文件裡把
契約文字改成實際落地的形狀，免得下一個人拿原文來對。

附帶：全部置頂時那個空的 `unpinned-group` 仍是 flex item，`.office__members-list` 的 `gap: 8px`
會在列表尾端多出 8px。純視覺、幾乎看不見。

### O3 — `verification.md §2.3` 宣稱「舊控制組一律不算數，整組重做」，實際只重做了 3 顆中的 3 顆，另外 2 顆沒重做

§2.1/§2.2 在舊基底 `6158b32` 跑了 5 顆 mutant（M1 拿掉置頂短路、M2、M3 拿掉 unread 層、P2-A、P2-B）。
§2.3 rebase 後寫「舊控制組一律不算數，整組重做」，但表格只有 **M-5a / M-5b / M-P2**。
**M1（置頂組不被攪動）與 P2-B（凍過頭）在新基底上沒有重跑過** —— 而 M1 正好是本票最需要控制組的那一支
（「某件事不發生」）。

我用靜態方式補了信心，結論是這兩支**應該仍有鑑別力**，但這是我的推論不是實測：

- 契約 2 那組在 `rosterOrder.test.ts` 有**成對的對照組**：`keeps the stored pin order even when a
  later pin has unread AND newer activity` 與 `the SAME two members DO reorder once they are not
  pinned`。第二支證明 fixture 不是「本來就長那樣」，所以第一支不是恆真。
- P2-B（凍過頭）的偵測仍靠既有 `OfficePage.jump-outsource.test.tsx` + 本檔的
  `does NOT freeze an EXPLICIT chatId onto the default`，兩者都在。

另一方面，§2.3 對 **M-5a 只紅 1 支、S2 沒紅** 的自我揭露（並說明為什麼**不該**紅、且拒絕為了數字好看
去改 fixture）我逐條對照 S2 的 fixture 驗證過：`mkMember` 預設 `name = id`，除了 `m-8`/`m-9`
（同名 `Twin`）之外 name 序與 id 序一致，所以拿掉 5a 後 S2 的輸出確實不變。**那段紀錄是可信的、
而且是難得的誠實揭露。** 建議只是把 §2.3 的「整組重做」改成實際做了哪幾顆。

### O4 — `change-manifest.md` 漏了一個檔（§7 逐檔必要性）

`server/ocserverd/api_member_avatar_test.go`（`newMemberDTO(*stored, "", "", 0)` →
`MemberChatStats{}`，純機械跟隨簽章）在 manifest 裡完全沒出現。改動本身沒問題，但 §7 要求的是
**`git show --stat` 的每一個檔**都講得出必要性，manifest 少一列就是那道人眼防線漏了一格。

### O5 — 冷啟動時「預設打開的對話」是在 pin 還沒讀回來之前決定的

`usePinnedMembers` 的 settings 讀取是非同步的，roster 通常先到。`defaultChatIdRef` 因此凍在
**未套用置頂**的排序首位上。結果是重整後聊天窗開的可能不是最上面那張置頂卡。
這與 P2 契約（不得抽換）**一致**，也接近改動前的行為（Mira），所以不是 bug；
`MemberDetailPanel.pin.test.tsx` 的 `openBobPanel()` 註解也記錄了這個時序。只是值得知道。

### O6 — `frontend/CLAUDE.md` 外包段落改寫後句子接不上

原句「左欄照 mockup 分兩組 —— 正職 header=…，成員卡=名字+…」把中段換成 T-66a8 的頁籤描述之後，
留下的「)，成員卡=名字+離線徽章+PresenceBadge+未讀數」懸在一個很長的括號之後，讀起來像在描述外包卡。
內容是對的（owner `rc-563734cd294e` 核可修這段），只是文氣要順一下。

---

## 3. 我查了、沒發現問題的地方

**需求符合度（S1–S10）**

| # | 判定 | 證據 |
|---|---|---|
| S1 四層優先級 | ✅ | `rosterOrder.ts:100-118` 逐層短路；`rosterOrder.test.ts` 每層各一支、且 fixture 刻意讓其他層反向（L4/L5a 用反向 id 證明該層真的跑了） |
| S2 決定性 | ✅ | 9 人 fixture 旋轉 9 × (正+反) = 18 種排列輸出恆等；fixture 含走 5a（`m-2`/`m-7`）與走 5b（`m-8`/`m-9` 同名 `Twin`）各一對 —— 這是上一輪控制組抓出來後補的，補得對 |
| S3 不新增輪詢 | ✅ | `usePinnedMembers` 只有一個 mount-time `useEffect`；roster 更新仍走 `ROSTER_TOPICS` refetch；全 diff 無 `setInterval`／遞迴 `setTimeout` |
| S4 選中列穩定 | ✅（除 O1） | `selected-stability.test.tsx` 4 支，含「正向控制」（先斷言 roster 真的重排了，再斷言 header 沒變）——這支測試自己就防住了「什麼都沒發生所以綠」 |
| S5 fallback | ✅ | `undefined` / 真 `0` / 缺 `unreadCount` / 空 pin 陣列四支；`?? 0` 沒有寫成 `\|\| 0`（`mappers.ts:222`、`rosterOrder.ts:100`） |
| S6 置頂可新增/取消/持久 | ✅ | `MemberDetailPanel.pin.test.tsx` 走**真實 OfficePage**（不是只 render 面板），並回頭 `api.getServerSettings()` 確認真的寫到 server |
| S7 組內順序 | ✅ | 陣列順序即顯示順序、新 pin `unshift`（`usePinnedMembers.ts` + `L1` 測試 + `a NEW pin goes to the FRONT`） |
| S8 responsive | ⚠️ 未經我驗證（見 §4） | |
| S9 鍵盤 | ✅ 靜態 | 原生 `<button>` + `aria-pressed`，切換後元素不卸載 → 焦點不失；入口沿用既有 avatar `<button>` |
| S10 `bin/ci.sh` | ⚠️ 未經我重跑（見 §4） | |

**UX intent**：置頂組短路（`rosterOrder.ts:96-99`）確實使 unread/recency 完全不進入置頂組內；
P2 只改求值時機、T-661b 收窄與三個 `setSelectedId` 呼叫點逐字未動（我比對過 diff）；
分組是 hairline + `role="group"`、**沒有** section header、**沒有** `role="separator"`（測試也釘了）；
置頂入口在 `MemberDetailPanel`，roster 列內零新增元素 —— 三條 owner 裁定都沒踩到。

**相容性（§12）**：`spec/openapi.json` 的 `MemberDTO.required` 仍是 `["id","name"]`、
`SettingsDTO.required` 仍是 `["token_ttl","handover_pct"]`、`SettingsUpdateDTO.required` 仍是 `[]`
（我用 python 直接讀 JSON 確認，不是看 diff 猜的）。生成的 `schema.ts` 把新欄位標成 required 是
openapi-typescript 對「有 `default`」的既有行為（`kind`、`unread_count` 同樣沒有 `?`），不是 spec 動了
`required`。舊 client 打新 server：多一個欄位、忽略即可。新 client 打舊 server：`?? 0` / `?? []`，
第 3 層整層失效退回舊排序 —— 這條有測試（`ABSENT lastActivityAt`）。

**邊界**：零置頂／全置頂／混合三態有測試；孤兒 pin 有兩處測試（純函式 + render）且 server 刻意不做
cleanup write（`api_settings.go:299-305` 有寫理由）；settings 讀失敗 → `[]` 且入口仍可用、寫失敗 →
回退，兩支測試都在；`lastActivityAt` 缺席／為 0 分開測；同名成員由 5b 兜底且 S2 fixture 真的含同名對。

**安全**：`ListMemberChatStats(currentActor(r))` 兩個數字都是 caller-relative，
`dal_member_chat_stats_t_ed38_test.go` 特地放了一則 `m-1 → m-2` 的 agent↔agent 訊息（ts 99，全表最新）
並斷言它**不得**洩進 owner 的任一 peer 數字，同時換 caller 為 `m-2` 再驗一次 —— 這支測試我認為是本包
最好的一支。無新端點、無新路由、`GET /api/members` 地板未變、`/api/settings` 仍 `admin_agent`。
SQL 的 `LEFT JOIN chat_read ON reader_id = :actor AND peer_id = m.sender` 我逐 case 推過：
owner 自己送出的列 join 不到（`peer_id = actor`）但 `unread` 條件本來就要求 `recipient = actor`，不影響；
`chat_read` 是 (reader, peer) 複合鍵 upsert，不會 fan-out 成多列。unread 語意與舊 `UnreadCounts`
由測試**逐 peer 對質**（雙向都比，避免「新的多算了 owner 自己的送出」這種只單向比會漏掉的錯）。

**§7 manifest**：36 個檔我逐檔看過，除了 O4 那一個，其餘都在 manifest 上且理由成立。
沒有暫存檔、debug 輸出、binary、`.bak`、機密。生成物（`ocapi_gen.go` / `schema.ts` /
`messageKeys.generated.ts` / `message_keys_gen.go`）的 diff 只有新欄位與 gofmt 對齊，看不出手改痕跡。
`wire.go` 那段整片 struct tag 重排確實是 gofmt 對齊造成的，不是順手格式化。
`spec/mcp-catalog.json` 的補丁是被 `TestFrozenCatalogAgreesWithOpenapiOnEveryToolsParameters` 逼出來的，
verification.md 有原始錯誤訊息，可信。

**§11 commit 格式**：`[why]` / `[how]` 齊全、無 emoji、結尾
`Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`、author `Ray Lu <lu.yingray@gmail.com>`。✅

**§8 碼↔doc**：`frontend/CLAUDE.md` 與碼同批更新，新增的「roster 排序 + 手動置頂」一節內容與實作逐條
對得上（我對照過四層、5a 不可用 `localeCompare`、四條邊界、P2 範圍）。兩處 owner 核可的過時描述修正
（`rc-563734cd294e`）都落在核可範圍內，沒有夾帶。**唯一的問題是 B1 的懸空引用。**

---

## 4. 我沒查 / 查不動的地方（誠實揭露）

1. **沒有重跑 `bash bin/ci.sh`**（743 支 conformance + 全套 Go + 前端）。我只跑了新增的 4 支前端測試檔、
   2 支 Go 測試與 `npm run typecheck`。CI 綠只採信 `verification.md §4` 的紀錄。
2. **沒有自己重做任何 mutant 控制組** —— 本次 review 被要求唯讀，改壞再改回會動到檔案。
   §2 的鑑別力我是用**讀測試碼 + 對照 fixture** 判定的（見 O3），不是實測。
3. **沒有跑起 app、沒有截圖**。S8（390/768/desktop 無溢位）、S9（實機鍵盤）、hairline 的實際視覺
   完全採信 `verification.md §3`，而那些截圖存在 session scratchpad、不在 repo 裡，我看不到。
4. **沒有驗跨裝置**（兩個瀏覽器）與**真實 server** 的手動點測 —— 與實作者同樣的限制。
5. **沒有查 `ChatReplyCard.markdown-render` 那支 flake 的根因**（實作者已誠實列為「發現但未納入」）。
6. **沒有評估 PR #12 合併後的衝突面**（`wire.go` / `dal.go` / `api_members.go` / spec 重疊），
   只確認本票不做 migration 因此沒有 goose 順序依賴。
7. **`?fields=light` 的實際消費者**我沒有全 repo 追 —— 只確認 `newMemberLightDTO` 不填
   `LastActivityAt`（誠實留 0），且 spec description 與兩邊碼註解都寫明了那個 0 的雙重語意。
