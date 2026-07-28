# T-ed38 — 後端現況驗證 (backend baseline)

**驗證基準 commit：`6158b32ec39a4c69b3b874783e1a213f43cd265c`**
（== `origin/main` == live server `git_sha` v0.5.45，依交辦單所述；本文件**未**親自對 live `/api/version` 打過，這一點見「未確認」節。）

驗證方式：唯讀讀碼 + `git show` / `git ls-tree` 唯讀指令。**沒有修改任何產品程式碼**，唯一寫入是本檔。
本輪**沒有**執行 `go test` / `bin/ci.sh` / conformance —— 所有結論都是**靜態讀碼證據**，不是執行證據。

---

## 0. 四項斷言的判定總表

| # | 斷言 | 判定 |
|---|------|------|
| 1 | `MemberDTO` 缺 `last_activity_at` | **證實** |
| 2 | roster query 的排序 | **部分正確** — 排序在 **SQL**（`ORDER BY name COLLATE NOCASE`），**後端完全沒有**助理特別處理；「助理置頂」是**前端**做的 |
| 3 | `unread_count` = chat_read watermark 的反相計數 | **證實**，但範圍要窄化：是「**收件人 == 呼叫者**」而非硬寫死 owner |
| 4 | 既有資料表可推導每位 peer 的 last_activity_at | **證實（可推導）** — `chat_message.ts` 足以推導，**不需要新欄位**；但**現行 handler 的取得方式（全表 `ListChat()`）不適合直接沿用**，見 §4.4 |

---

## 1. 斷言一：`MemberDTO` 是否缺 `last_activity_at`

### 1.1 權威定義的位置（先確認 server/CLAUDE.md 的說法）

`server/CLAUDE.md:15` 明寫：

> **response DTO 手寫在 `wire.go`**（Pydantic 宣告序 + null-不省略;生成型別只當 request-body 詞彙——生成 struct 的 omitempty 會丟 `token: null` 這類語意鍵）

**已在碼上坐實這句話**：`api_members.go` 全部 20 個 write 點呼叫的是小寫 `memberDTO` / `newMemberDTO` / `writeMemberDTO`（`server/ocserverd/api_members.go:111,125,192,215,262,315,405,431,453,474,503,548,559,576,586,610,629,688,703`），**沒有任何一處**回傳生成型別 `MemberDTO`。生成型別 `MemberDTO`（`server/ocserverd/ocapi_gen.go:724`）在整個 server 只被 `ocapi_gen.go:1297` 自己引用（`BootstrapResultDTO.Member`）。

⇒ **上線的那一份 = `server/ocserverd/wire.go:119` 的 `memberDTO`（小寫）**。

### 1.2 現有欄位逐條（`server/ocserverd/wire.go:119-151`）

| Go 欄位 | wire key | 型別 |
|---|---|---|
| ID | `id` | string |
| MemberNo | `member_no` | string |
| Name | `name` | string |
| Kind | `kind` | string |
| RoleKey | `role_key` | string |
| RoleName | `role_name` | string |
| Runtime | `runtime` | string |
| Model | `model` | string |
| Effort | `effort` | string |
| DesiredState | `desired_state` | string |
| DesiredMachineID | `desired_machine_id` | string |
| Machine | `machine` | string |
| Presence | `presence` | string |
| RefocusSince | `refocus_since` | float64 |
| LastOp | `last_op` | string |
| LastOpOK | `last_op_ok` | \*bool |
| LastOpLog | `last_op_log` | string |
| LastOpReason | `last_op_reason` | string |
| LastOpAt | `last_op_at` | float64 |
| UnreadCount | `unread_count` | int |
| RosterStatus | `roster_status` | string |
| OwnerID | `owner_id` | string |
| SchemaVersion | `schema_version` | int |
| RelocationPending | `relocation_pending,omitempty` | \*bool (T-8655) |
| ActivationPending | `activation_pending,omitempty` | \*bool (T-ba62) |

**沒有 `last_activity_at`，也沒有任何等價的「最近互動時間」欄位。** 最接近的 `last_op_at` 是 **warden op receipt 的時間戳**（`migrations/00001_schema.sql:55` 的 `last_op_at REAL`，由 `foldCommandResult` 寫入，`api_monitoring.go:237`），語意是「這台機器最後一次執行動作」，**不是**「owner 與這位成員最後一次互動」。

### 1.3 `spec/openapi.json` 的 MemberDTO schema

`spec/openapi.json:1695-1860`（`"MemberDTO"` 起、`"title": "MemberDTO"` 在 1852）。properties 清單與上表**逐一對應**、同樣**沒有** `last_activity_at`。

🔴 **關鍵限制（對後續設計最要緊的一條）**：

```json
"required": [ "id", "name" ],
"title": "MemberDTO",
"additionalProperties": false,
```
（`spec/openapi.json:1846-1854`）

`additionalProperties: false` 表示：**只在 `wire.go` 加欄位、不改 spec，conformance 會紅**。`conformance/schema_check.py:1-12` 的 docstring 明寫它實作了 `additionalProperties`：

> Validates a decoded JSON instance against the subset of JSON Schema that ``spec/openapi.json`` actually uses: ``$ref`` …, ``required`` / ``properties`` / ``additionalProperties``, …

⇒ 加欄位**必須**先動 spec（見 §7 的可執行清單）。

### 1.4 一條已存在的「不要長大 MemberDTO」前例（設計時值得知道）

`server/ocserverd/api_monitoring.go:225-228`：

```go
// Deliberately in-place inside the existing five last_op* fields: a separate
// durable slot would grow MemberDTO, and the wire is frozen (CLAUDE.md §13).
// This follows the isStopNoopReceipt precedent — the fold already knows that not
// every receipt deserves the slot on its own terms.
```

這是**已存在的裁定紀錄**：上一位作者為了避免長大 MemberDTO，選擇把資訊擠進既有欄位。它**不等於**「MemberDTO 不能加欄位」（root `CLAUDE.md:37` §12 明確允許 optional 加欄），但它證明「加欄位」在這個 repo 是需要被論證的動作、不是預設。

**判定：斷言一 — 證實。**

---

## 2. 斷言二：roster query 的排序

### 2.1 handler

`server/ocserverd/api_members.go:84-128`（`HandleListMembersApiMembersGet`）：

- L85 `members, err := s.dal.ListMembers()`
- L111-126 一個單純的 for 迴圈，跳過 `RosterStatusRemoved`，逐筆 append
- L127 `writeJSON(w, http.StatusOK, out)`

**Go 端零排序**：`grep -n "sort\." server/ocserverd/api_members.go server/ocserverd/api_helpers.go` **無任何命中**（exit 1）。所以 handler 原樣沿用 DAL 回傳順序。

### 2.2 DAL 的實際 SQL

`server/ocserverd/dal.go:133-135`：

```go
func (d *DAL) ListMembers() ([]Member, error) {
	rows, err := d.db.Query(`SELECT ` + memberColumns +
		` FROM member WHERE kind != 'outsource' ORDER BY name COLLATE NOCASE`)
```

⇒ **排序 = SQL 的 `ORDER BY name COLLATE NOCASE`（純字母序、大小寫不敏感）**。

同段 doc comment（`dal.go:126-132`）另外釘住兩件事：
- 回傳**任何 roster_status**（含 soft-removed），過濾由 caller 做 —— 這就是 handler L113 那個 `continue`；
- `kind='outsource'` 由 SQL 排除（A案 P7d 設計）。

### 2.3 助理（`role_key == "assistant"`）有沒有被特別處理？

**後端：沒有。**
`grep -rn "assistant\|adminRoleKey" server/ocserverd/api_members.go server/ocserverd/dal.go` 只命中三處，全是註解或 kind 閉集說明，**沒有一處**影響排序：
- `api_members.go:132` 註解（hire 的提權說明）
- `api_members.go:165` 註解（CanonicalKind 折疊）
- `dal.go:46` 註解（`Kind` 閉集 `"assistant" | "warden" | "outsource"`）

⚠️ 注意這裡有個**容易踩的名詞陷阱**：`kind == "assistant"` 是「這是個 AI 同事」（相對於 warden/outsource），而 `role_key == "assistant"` 是「這個人的角色是助理」（`authz.go` 的 `adminRoleKey`，admin_agent 判定用）。兩者不同軸。

**前端：有，而且置頂就是在這裡做的。**
`frontend/src/components/OfficePage.tsx:128-137`：

```ts
  // The office lists ONLY real AI assistants — machine-layer members (kind
    .filter((m) => m.kind === "assistant")
    // 助理(seed assistant 角色)置頂;其餘接在後面。sort 穩定 → 各組內維持
    .sort(
        (a.role === "assistant" ? 0 : 1) - (b.role === "assistant" ? 0 : 1),
```

即：FE 先 `filter(kind === "assistant")`，再用**穩定 sort** 把 `role === "assistant"` 提前，**組內順序原樣繼承後端的 name 序**。

**判定：斷言二 — 部分正確。** 「roster query 有排序」正確且是 SQL 排序；但**助理特別處理不在後端**，在 `OfficePage.tsx`。任何「後端做優先排序」的設計都必須同時處理 FE 這個既有 sort，否則會被它蓋掉。

---

## 3. 斷言三：`unread_count` 怎麼算

### 3.1 計算位置

純函數在 `server/ocserverd/domain.go:411-425`：

```go
func UnreadCounts(messages []ChatMessage, receipts []ChatRead, reader string) map[string]int {
	watermark := map[string]float64{}
	for _, r := range receipts {
		if r.ReaderID == reader {
			watermark[r.PeerID] = r.LastReadTS
		}
	}
	counts := map[string]int{}
	for _, m := range messages {
		if m.Recipient == reader && m.TS > watermark[m.Sender] {
			counts[m.Sender]++
		}
	}
	return counts
}
```

doc comment（`domain.go:404-410`）自述：「the pure inverse of the read watermark」「no receipt ⇒ watermark 0 ⇒ every addressed message counts」。**證實「chat_read watermark 的反相計數」這個描述。**

### 3.2 注入 roster 回應的路徑

`server/ocserverd/api_members.go:95-109`：

```go
	var unread map[string]int
	if !light {
		actor := currentActor(r)
		messages, err := s.dal.ListChat()
		…
		receipts, err := s.dal.ListChatReads(actor, "")
		…
		unread = UnreadCounts(messages, receipts, actor)
	}
```

然後 `api_members.go:125`：`s.newMemberDTO(m, roleName, s.observedHost(m), unread[m.ID])`，最終落到 `api_helpers.go:286` 的 `UnreadCount: unreadCount`。

### 3.3 到底算誰的訊息（斷言的窄化）

- `reader = currentActor(r)` = **verified token 的 `sub`**（`api_helpers.go:25-28`），**不是**硬寫死的 owner。座艙用 owner token 打，所以實務上 reader = `"owner"`（`wire.go:24` `const wireOwnerID = "owner"`），但 agent 自己打 `GET /api/members` 拿到的是**它自己的**未讀數。
- 計數條件 `m.Recipient == reader`：**只算「寄給呼叫者」的訊息**。呼叫者自己寄出的不算；兩個第三方之間的訊息不算（doc comment 亦明寫）。
- chat 是**嚴格 1:1、沒有廣播**：`api_chat.go:107-121` 的 `resolveChatRecipient` 只接受 `wireOwnerID` 或一個 active 的 assistant/outsource member，其餘 404。所以不存在「全員訊息」污染計數的情況。

⇒ 以座艙（owner token）而言，`unread_count` **確實只算「該成員 → owner」的訊息**。但這是「reader 恰好是 owner」的結果，不是碼上的硬條件。

### 3.4 `?fields=light` 的誠實空值

`api_helpers.go:293-314` 的 `newMemberLightDTO` **不帶** `UnreadCount`（零值 0）、`Presence`（""）、`Machine`（""）、`last_op*`。doc comment 明寫「A consumer must not read those off a light response — the value is "not computed", not "known zero"」。
釘住它的測試：`api_perf_params_test.go:180 TestMembersFullPathComputesUnread` / `:203 TestMembersLightSkipsUnreadAndHeavyFields`。

**判定：斷言三 — 證實（範圍窄化如上）。**

---

## 4. 斷言四：既有資料表能否推導「每位 peer 的 last_activity_at」

### 4.1 chat 相關表的實際 schema

`server/ocserverd/migrations/00001_schema.sql:67-96`：

```sql
CREATE TABLE chat_message (
    id        TEXT PRIMARY KEY,
    sender    TEXT NOT NULL DEFAULT '',
    recipient TEXT NOT NULL DEFAULT '',
    body      TEXT NOT NULL DEFAULT '',
    ts        REAL NOT NULL DEFAULT 0.0,
    meta      TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_chat_message_ts ON chat_message (ts);

CREATE TABLE chat_attachment (
    id       TEXT PRIMARY KEY,
    mime     TEXT NOT NULL DEFAULT 'application/octet-stream',
    data     BLOB NOT NULL,
    filename TEXT
);

CREATE TABLE chat_read (
    reader_id    TEXT NOT NULL,
    peer_id      TEXT NOT NULL,
    last_read_ts REAL NOT NULL DEFAULT 0.0,
    PRIMARY KEY (reader_id, peer_id)
);
CREATE INDEX idx_chat_read_peer ON chat_read (peer_id);
```

### 4.2 可用的時間欄位清單

| 表 | 欄位 | 語意 | 能否代表「互動」 |
|---|---|---|---|
| `chat_message` | `ts` REAL（epoch 秒，`api_chat.go:388` `TS: nowSecs()`） | 訊息寫入時刻 | ✅ **這就是互動時間的權威** |
| `chat_read` | `last_read_ts` REAL | 讀取水位 —— 值是**被讀到的那則訊息的 ts**，不是讀的當下時刻（`api_chat.go:504` `LastReadTS: newest`；`dal.go` `PutChatRead` 註明 MONOTONIC 只前進） | ⚠️ 是「訊息時間」的投影，不是「owner 什麼時候讀的」，**不能**當獨立的互動時鐘 |
| `member` | `last_op_at` REAL（00001:55） | warden op receipt 時間 | ❌ 機器動作，非人的互動 |
| `member` | `waking_since` / `stopping_since` / `stopped_since` / `refocus_since` REAL（00001:43-46） | presence 錨點 | ❌ 生命週期，非互動 |
| `member` | `created_ts` / `released_ts` / `activated_ts`（在 `dal.go:95-99` 的 `memberColumns`，來自 00024/00025） | 名冊生命週期 | ❌ |
| `task` | `created_ts` / `updated_ts` / `closed_ts`（00004:47-50） | 任務時間 | 🔶 可作為「工作互動」的另一個訊號，但不是聊天互動 |
| `task_step` | `started_ts` / `finished_ts`（00004:87-88） | | 🔶 |

**`chat_message` 沒有 `deleted` 或 soft-delete 欄；訊息是不可變的**（`dal.go:278-282` 的 `ListChatBefore` doc comment：「messages are immutable, so a cursor stays valid forever」）。

### 4.3 判定：可以推導，**不需要新增欄位**

因為 chat 嚴格 1:1（§3.3），「owner 與 peer P 的最後互動時間」就是：

```sql
SELECT peer, MAX(ts) AS last_activity_at FROM (
  SELECT recipient AS peer, ts FROM chat_message WHERE sender    = 'owner'
  UNION ALL
  SELECT sender    AS peer, ts FROM chat_message WHERE recipient = 'owner'
) GROUP BY peer;
```

（把 `'owner'` 換成 `currentActor(r)` 就是通用版，與 `unread_count` 的 reader 語意對齊。）

⇒ **斷言四：證實 — 可從現有資料推導，不必新增 durable 欄位。**

### 4.4 🔴 但「能推導」≠「可以照現在的方式做」（這是本節最重要的一句）

現行 handler 取 unread 的方式是 **`s.dal.ListChat()`——把整張 `chat_message` 表拉進記憶體**（`dal.go:257-259`，`SELECT … FROM chat_message ORDER BY ts, id`，**沒有 LIMIT**）。`api_members.go:92-94` 的註解自己說：

> unread rides the caller's chat_read watermark over the whole chat stream —
> the single most expensive part of this handler and exactly what the light
> projection exists to avoid.

也就是說 **T-cf91 這一整張票的存在理由，就是要把這條全表掃描從輕量路徑上拿掉**。如果 `last_activity_at` 用「在 Go 端再掃一次 `ListChat()` 的結果」實作，技術上零成本（同一份 slice 掃第二遍），**但它會把這個功能永久綁死在 full path 上**，且 `?fields=light` 路徑天生拿不到（honest-empty）。

**設計上真正的選項**（本文件不做決定，只列出碼上支撐的事實）：
1. **複用既有 full-path 的 `ListChat()` 結果**，在 Go 端同一趟迴圈算出 per-peer max ts。零新查詢、零新 migration，但延續全表掃描、light 路徑無值。
2. **新增一支 DAL 聚合查詢**（上面那段 SQL），只回 `map[peer]float64`。SQLite 端 GROUP BY，不拉 body/meta。⚠️ 現有索引只有 `idx_chat_message_ts (ts)`，**沒有 `sender` / `recipient` 索引**，這條查詢會走全表掃描（但只讀三欄、不搬 blob，代價遠低於選項 1）。要加索引 = 一個新 migration。
3. **新增 durable 欄位**（例如 `member.last_activity_at`，寫入點在 `post_chat`）。**依 §4.3 這在「能不能推導」層面是不必要的**；只有在效能證據支持時才值得，且會多一個必須維持一致的第二真相來源。

### 4.5 未確認 / 需要再驗的部分

- **正式站的 `chat_message` 實際列數與表大小** —— 未確認。沒有這個數字，就無法判定選項 1/2/3 之間的效能取捨；我沒有 live DB 存取。
- **是否有既有的 per-peer 最後訊息 helper 可以直接複用** —— 我在 `dal.go` / `domain.go` 沒找到；`ListChatBefore`（`dal.go:283`）是 keyset 分頁、不是聚合。但我沒有做**全 repo 窮舉**，只掃了 `dal.go` / `domain.go` / `api_chat.go`。
- **task 側的互動訊號（`task.updated_ts` by executor）要不要算進 last_activity_at** —— 這是產品語意問題，碼上無法回答，屬 owner 裁定面。

---

## 5. migration 現況

### 5.1 目前最高序號 & 跳號

`ls server/ocserverd/migrations/` 於基準 commit 的最後幾筆：

```
00035_normalize_auto_machine_placement.sql
00036_task_outsource_dispatched.sql
00037_task_frozen_by.sql
00039_member_last_machine_id.sql
```

⇒ **最高序號 = `00039_member_last_machine_id.sql`。確認 00038 跳號。**

**另外還有一個 00027 跳號**（交辦單沒提到）：`00026_outsource_delegation_policy.sql` 之後直接跳到 `00028_task_lock_and_step_waiting_reason.sql`。
歷史查證：`00027_outsource_intent.sql` 由初始樹 `8e573a7` 加入，於 `6c82e2a`（"sync: last train from open-company main"）**被刪除**。這是一個**已消失的舊 migration**，與 00038 的「被在飛分支佔用」是**兩種不同的跳號**，不要混為一談。

### 5.2 00038 跳號的既有裁定（**必讀，直接影響本票選號**）

`server/CLAUDE.md:83`（逐字）：

> ⚠️ **migration 編號是 `00039`,`00038` 在 `origin/main` 上確實空著——這個跳號是刻意留著的,別「順手補回去」**:`00038_member_avatar.sql` 已經被**在飛的 PR #12**(`pkyosx/OffiCraft`,`feat/member-custom-avatar`,2026-07-27 open)佔用。`migrate.go` 呼叫的是 bare `goose.Up`(無 `WithAllowMissing`),兩種失敗方向都要看清楚:**跳號**的代價是「已在版本 39 的 DB 遇到後來出現的 00038 會 migrate 失敗、server 拒絕啟動」——**很吵、擋在起站前、看得見**;**撞號**(兩個分支各有一個 00038)的代價是 goose 記到版本 38 就認為做過了,**後 merge 的那份 DDL 永遠不會被執行、沒有任何錯誤**——安靜的 schema 缺漏。撞號嚴格更糟…

「bare `goose.Up`」已在碼上坐實：`server/ocserverd/migrate.go:44-49` 的 `runMigrations` 只做 `goose.SetBaseFS` + `SetDialect("sqlite3")`（**沒有** `WithAllowMissing`）。

### 5.3 ⚠️ 這條 doc 已經對不上碼了：avatar 分支現在佔的是 **00040**，不是 00038

唯讀查證（**沒有 checkout**）：

```
$ git ls-tree --name-only feat/member-custom-avatar server/ocserverd/migrations/ | tail -5
… 00037_task_frozen_by.sql
… 00039_member_last_machine_id.sql
… 00040_member_avatar.sql
```

且該分支 head `42bbf12` 的 parent 就是本票基準 `6158b32` —— 它**已經 rebase 到基準之上、並把自己的 migration 改成 00040**（`git show --stat 6392a5e` 顯示它**原本**是 `00038_member_avatar.sql`，即 CLAUDE.md 描述的是那個舊版本）。

**對本票的直接後果：**
- `00038` 在 `origin/main` 與 avatar 分支上**都是空的**。它現在是一個**真正的空號**，但 §5.2 那條裁定的**理由**（bare `goose.Up`，撞號比跳號糟）仍然成立，**不要因為 avatar 搬走就去補 00038** —— 已經跑到版本 39/40 的 DB 遇到新出現的 00038 會**拒絕起站**。
- 本票若要新增 migration，**安全的號碼是 `00041`**（避開 avatar 分支已佔的 00040）。
- `server/CLAUDE.md:83` 那段文字**已過時**（仍寫 00038）。這屬於 root `CLAUDE.md:21` §8 的 doc↔碼 不一致情形 —— 依 §8 護欄**不自裁**，回報即可；它大機率只是 avatar 分支 rebase 時沒同步這段（rebase 責任在該分支，CLAUDE.md:83 自己也寫了「由 PR #12 改成 00040(那是它的 rebase 責任)」）。

---

## 6. roster 相關的既有測試（命名與風格慣例）

### 6.1 Go 測試

| 檔案 | 內容 |
|---|---|
| `server/ocserverd/api_members_relocate_test.go` | `TestRelocateMember_PlacementOnly` / `_MigratesLiveMember` / `_Rejects` / `_RejectsUnresolvableMachine` / `_AdminGated` / `_WorkerIdFallback` / `_MachineIsMandatory` |
| `server/ocserverd/api_members_activate_pending_test.go` | `TestActivateMember_UnlandedSurfacesPending` / `_LandedNoPending` / `_UnbuildableFrameSurfacesPending` / `_RejectsUnresolvableMachine` / `_AlreadyOnlineIsNotPending` |
| `server/ocserverd/api_members_relocate_pending_test.go` | `TestRelocateMember_UnlandedSurfacesPending` / `_LandedNoPending` |
| `server/ocserverd/api_members_restartself_test.go` | `TestRestartSelf…`（5 條） |
| `server/ocserverd/api_members_waking_model_test.go` | `TestReportWaking…KeepsOwnerConfiguredModel`（2 條） |
| `server/ocserverd/api_perf_params_test.go` | **最相關**：`:166 listMembers()` helper、`:180 TestMembersFullPathComputesUnread`、`:203 TestMembersLightSkipsUnreadAndHeavyFields`（`:215` 還帶一段 `// MUTANT:` 註解，示範這個 repo 慣用的 mutant 記錄） |
| `server/ocserverd/api_helpers_test.go` | `:56`/`:91` 用 `decodeBody[memberDTO](t, rec)` 讀回 DTO —— **這就是斷言 DTO 欄位的標準寫法** |
| `server/ocserverd/domain_test.go` | `UnreadCounts` 的純函數測試（4 處命中） |
| `server/ocserverd/dal_test.go` | `ListMembers` 相關（2 處命中） |

**命名慣例**：`Test<動作或主體>_<被斷言的具體性質>`，底線後那段是一個**可讀的句子片段**（`_MachineIsMandatory`、`_LightSkipsUnreadAndHeavyFields`）。新增票號專屬檔時的慣例是 `<domain>_<票號>_test.go`（例：`api_context_cap_t3351_test.go`、`worker_sticky_placement_t98f4_test.go`、`routes_t5336_webhook_authz_test.go`）。

### 6.2 conformance（Python，HTTP-only 黑箱）

- 檔案：`conformance/test_rest_happy.py`（89K）、`test_auth_matrix.py`、`test_lifecycle.py`、`test_mcp.py`、`test_sse.py`、`test_tasks.py`、`test_reply_cards.py`、`test_error_envelope.py`
- roster 路由在 `conformance/routes_manifest.json` 的 15 列（`/api/members*`），逐列帶 `auth` / `requires` / `mcp_tool`。`GET /api/members` 那列：
  ```json
  {"auth": "gated", "mcp_tool": "get_members", "method": "GET", "path": "/api/members", "requires": "machine"}
  ```
- 命名慣例：`def test_<行為敘述>(hctx: HCtx) -> None:`，全小寫底線，句子式（`test_relocate_requires_a_machine_that_resolves`、`test_settings_updater_channel_toggles_roundtrip`）。
- `test_rest_happy.py:1976 test_happy_covers_manifest` / `:1991 test_openapi_covers_manifest` 是**完整性閘**：manifest ≡ spec operations ≡ happy 覆蓋。
- `conformance/schema_check.py` 提供 `violations(instance, schema, spec)`，是**回應 body 對 spec schema 比對**的共用工具（含 `additionalProperties`）。

---

## 7. 在 `MemberDTO` 加一個 optional 欄位的完整流程（可執行清單）

依 root `CLAUDE.md:36-42`（§12 結構約定 + §13 wire 凍結）與 `server/CLAUDE.md:13,15,148`，以及 `bin/ci.sh` 的實際 gate：

> root `CLAUDE.md:37`：**DTO 向後相容**:對外 DTO 加欄一律 `optional`(不破既有 client);要破壞相容**先問 Seth**
> root `CLAUDE.md:42`：**wire 已凍結(M1)**:動 wire(HTTP OpenAPI 面或 MCP tool 面)= **先改 `spec/*.json` + owner 過目,再動碼**

| # | 步驟 | 檔案 | 手改 or 生成 |
|---|---|---|---|
| 0 | **先取得 owner 過目**（wire 凍結流程的第一步，不是形式） | — | — |
| 1 | 在 `MemberDTO.properties` 加欄位（**不要**加進 `required`；`additionalProperties: false` 使這步成為必要條件） | `spec/openapi.json`（schema 在 1695-1860） | ✋ **手改**（唯一 SSOT） |
| 2 | 重生 Go wire types | `server/ocserverd/ocapi_gen.go` | 🤖 **生成物，絕不可手改** — 跑 `bash bin/gen-ocapi`（`bin/gen-ocapi:1-31`；oapi-codegen `v2.7.2` pinned；config 在 `server/ocserverd/ocapi.yaml`） |
| 3 | 在**手寫**的 response DTO 上加對應欄位 | `server/ocserverd/wire.go:119` `memberDTO` | ✋ **手改**（這一份才是上線的，見 §1.1） |
| 4 | 在投影函數填值 | `server/ocserverd/api_helpers.go:265 newMemberDTO`（+ 決定 `:301 newMemberLightDTO` 要不要帶 —— light 的既有契約是 honest-empty） | ✋ 手改 |
| 5 | handler 端注入資料（若需要新的查詢） | `server/ocserverd/api_members.go:84` | ✋ 手改 |
| 6 | 重生 FE schema | `frontend/src/api/generated/schema.ts` | 🤖 **生成物，絕不可手改** — `cd frontend && npm run gen:api`（`frontend/package.json:10` = `openapi-typescript ../spec/openapi.json -o src/api/generated/schema.ts`） |
| 7 | FE seam 逐層接（wire→mappers→types→adapter→mock→http→hooks→component，root `CLAUDE.md:60`） | 至少 `frontend/src/api/mappers.ts:125 toMember`（現有 `unreadCount: w.unread_count ?? 0` 在 `:213`）、`mock.ts` | ✋ 手改 |
| 8 | 測試 | Go：`api_perf_params_test.go` 風格；conformance：`test_rest_happy.py` + `schema_check.py` | ✋ 手改 |
| 9 | **註**：只加欄位、不加/改路由時，`conformance/routes_manifest.json` **不需要**動（它記的是 route × auth × requires × mcp_tool，不是 DTO 欄位） | — | — |

**兩道會抓漂移的 CI gate**（`bin/ci.sh`）：
- step 1g：`ocapi_gen.go` 必須能從凍結 spec **byte-identical** 重生；
- step 4c（`bin/ci.sh:346-350, 478-489`）：重生 `schema.ts` 並 `diff -u` 對比 committed 版本，不符即
  `[ci] FAIL — contract drift: frontend/src/api/generated/schema.ts is STALE vs spec/openapi.json.`

⇒ **`ocapi_gen.go` 與 `frontend/src/api/generated/schema.ts` 這兩個檔案是生成物，手改一定被 CI 抓到。**

---

## 8. per-owner 偏好設定的持久化先例（給 `pinned_member_ids` 若走 server-backed 用）

`server/ocserverd/settings.go` 是**現成、成熟、正在用**的先例，而且已經有**「owner 個人偏好、跨裝置同步、非 agent 讀取路徑」**這個完全對應的類別：

| 常數 | DB key | 型別 | 註解出處 |
|---|---|---|---|
| `settingOwnerName` | `owner.name` | string | `settings.go:87-92` — 「Server-backed (PATCH /api/settings) so the nickname syncs across the owner's devices… **NOT an agent read path**」 |
| `settingDisplayTheme` | `display.theme` | string | `settings.go:101-108` — 「server is the cross-device source of truth reconciled at login」+ FE 保 localStorage cache |
| `settingDisplayLanguage` | `display.language` | string | `settings.go:109-113` — 同一份 dual-layer 契約 |
| `settingDisplayWide` | `display.wide` | bool | `settings.go:114-121` — 「Stored like the updater toggles — `strconv.FormatBool` text, absent row = false」 |
| 🔴 **`settingDisplayCustomThemes`** | `display.custom_themes` | **JSON 陣列** | `settings.go:122-126` — 「a JSON array of {id,name,colors} colour bundles… Server-backed so the set syncs across devices… Absent/"" = none saved」 |

**`display.custom_themes` 就是 `pinned_member_ids` 的直接模板**：它是一個「存進 setting 表的 JSON 陣列」，讀寫路徑完整可抄：

- 表：`migrations/00002_settings.sql`（schemaless key-value）。`settings.go:25-27` 註明「The closed settings key set. The setting table is schemaless key-value; the reader here holds the schema (type, default, who writes). **Keys not listed are never read.**」
  ⇒ **加一個 owner 偏好不需要 migration**，只要在這個閉集加一個常數。
- 快照：`authSettings` struct（`settings.go:142-159`），`displayCustomThemes []ThemeBundleDTO` 在 `:158`。
- 寫入：`api_settings.go:404-422`（`json.Marshal` → `s.dal.PutSetting(settingDisplayCustomThemes, string(marshaled))` → 就地更新 in-memory 快照）。
- 讀出：`api_settings.go:471-489`（`// custom_themes always serialises as an array, never null (the wire shape).`）。
- wire：`server/ocserverd/wire.go:83-86` `CustomThemes []ThemeBundleDTO \`json:"custom_themes"\``（同樣是**手寫**在 wire.go）。
- 授權：`GET/PATCH /api/settings` 自 T-6020 起 `requires=admin_agent`（`server/CLAUDE.md:10,32`）。⚠️ **這是一個要留意的落差**：`pinned_member_ids` 若走 settings，它的寫入門檻就是 admin_agent，而 `GET /api/members` 的門檻是 `machine`。這兩者是否該一致，屬設計決策。
- 一致性驗證：`conformance/test_rest_happy.py:1634 test_settings_custom_theme_background_and_mode_round_trip` 是現成的 round-trip 測試範本。

⚠️ **一個語意警告**：settings 表是 **single-owner、全站唯一一份**（`00001_schema.sql:112-115` 對 `user_context` 寫「SINGLE-ROW table… single-owner by decree」）。所以「per-owner 偏好」在這個 repo 實際上 == 「全站偏好」。如果未來要支援多 owner，這個先例不夠用 —— 但這不是本票的問題。

---

## 9. `server/CLAUDE.md` 全文中與本任務相關的規範摘要（含行號指引）

我完整讀完了 `server/CLAUDE.md` 全 148 行。以下是**與 T-ed38 直接相關**的條目：

### 9.1 wire / DTO 相容性

- **`server/CLAUDE.md:15`** — 🔴 **最關鍵的一條**：「**response DTO 手寫在 `wire.go`**(Pydantic 宣告序 + null-不省略;**生成型別只當 request-body 詞彙**——生成 struct 的 omitempty 會丟 `token: null` 這類語意鍵)」。已在 §1.1 坐實。
- **`server/CLAUDE.md:13`** — `ocapi_gen.go` 是 committed 生成物，「**改 wire 一律 spec 先行,然後 `bash bin/gen-ocapi` 重生**(idempotent;生成物 committed 即 drift-diffable)」。
- **`server/CLAUDE.md:14`** — route 表 122 rows，`auth/requires/mcp` 逐行鏡自凍結快照 `conformance/routes_manifest.json`；`server_test.go` 的 spec-surface 測試鎖「表 ≡ spec operations」。
- **`server/CLAUDE.md:148`** — 「新端點 = RouteSpec 表加一行(`routes.go`),不散寫 mount;boot assertion 缺 requires 即拒起。」
- **`server/CLAUDE.md:38`**（T-5336）— 一個**完整的 spec-first 示範**：「生成物照 wire-freeze 流程走:改 `spec/openapi.json` → `bash bin/gen-ocapi` 重生 `ocapi_gen.go` → `npx openapi-typescript` 重生 FE schema,**三份逐字一致**。」§7 的清單就是照這句寫的。
- **`server/CLAUDE.md:88`**（T-98f4）— 另一個示範，包含 spec `required` 的變更會如何改變生成型別（`MachineId` 由 `*string` 變 `string`）。
- **`server/CLAUDE.md:35`** — 一條**輕量 vs 詳細 DTO 的既有裁定**：「⚠️ `frozen_by` **只在詳細 DTO 上,輕量列表(`GET /api/tasks`)不帶**——與既有輕量化設計一致,但 FE 想在列表顯示「誰凍的」時要先擴欄。」→ 這個 repo 有**明確的「列表輕量化」文化**，`?fields=light` 與此同源。

### 9.2 roster / members 的既有裁定

- **`server/CLAUDE.md:15`** — `api_members.go`(roster + lifecycle + self-report) 的職責邊界。
- **`server/CLAUDE.md:39`**（T-5336 裁定 2）— 🔴 **`PATCH /api/members/{member_id}`(update_member)維持 `principalMachine` 地板——刻意,不是遺漏。前提是成員之間互相信任**…「要抬需要新的 owner 裁定,不是整理型 commit。」
- **`server/CLAUDE.md:40`**（T-5336 裁定 3）— 🔴 **「讀取面權限地板這一輪一行不改。」owner 明確否決了把讀取面抬高的提議。** ⇒ `GET /api/members` 停在 `machine` 是有裁定的，不要順手改。
- **`server/CLAUDE.md:30`**（T-b89d）— **「有哪幾台」由名冊決定、不由 telemetry 決定**：monitoring 的 machines 列集合 = kind=warden ∧ roster=active，與 `GET /api/machines` / `resolveMachine` 同一判準。同源原則：**列表的成員集合來自名冊，不來自派生訊號**。這對「用 chat 活動排序」有直接啟示 —— **排序**可以吃派生訊號，但**集合**不該。
- **`server/CLAUDE.md:71`** — `domain.go` 是「純函數、無 http/SQL」層，`unread 反相` 明確列在裡面。⇒ 新的 last_activity 派生若是純函數，**應該放 `domain.go`**，與 `UnreadCounts` 並列。

### 9.3 migration 規範

- **`server/CLAUDE.md:63`** — migrations 的內容地圖（00001 9 表 single-owner 化、00002 settings、…），以及「migrate 目前 sqlite-only」。
- **`server/CLAUDE.md:83`** — 🔴 **migration 編號跳號規範全文**（見 §5.2 逐字引用）：bare `goose.Up`、跳號 vs 撞號的失敗方向、**撞號嚴格更糟**。⚠️ 這段的 00038 已對不上碼（見 §5.3）。
- **`server/CLAUDE.md:19`** — 「**佇列持久化(T-66a2 L3)**:`update` 是唯一會落庫的動詞」+ `migrations/00034` —— 一個「什麼該落庫、什麼刻意不落庫」的推理範例（「reconcile 一個 cadence 內從 presence 重推」= 能重推導的就不落庫）。⚠️ **這條推理直接適用於 last_activity_at**：它能從 `chat_message` 重推導 ⇒ 依這個 repo 的既有推理習慣，**預設不落庫**。
- **`server/CLAUDE.md:35`** — 新增 durable 欄位的完整範例（`task.frozen_by` + `00037` + `TaskDTO.frozen_by`），可當「真的要加欄位時」的模板。

### 9.4 授權面（新增欄位或端點時的要求）

- **`server/CLAUDE.md:32`**（T-6020）— 19 條營運端點降到 `admin_agent` + 改成 MCP 工具；**5 條刻意保留 owner-only，理由逐列寫在 `routes.go`**；有一條「任何**新的** owner-only 列都要有自己的裁定」的完整性檢查（`routes_t6020_governance_test.go` + `conformance/test_mcp.py` 的 `test_t6020_*`）。
- **`server/CLAUDE.md:42-47`**（T-5336 結構性防復發）— 🔴 **「凡授權寫在路由表之外,就會週期性地漏掉下一次治理變更」**。兩道 AST 掃描閘：
  - `TestAuthzOutsideTheRouteTableIsEnumerated` — 掃到的 36 條授權判斷都必須列在 `authzOutsideRouteTable` 並**寫明理由（≥40 字元）**；掃到沒列 → 紅，**列了但掃不到（stale）也 → 紅**。
  - `TestMachineFloorWriteRoutesAreEachARuling` — 每一條 `POST/PATCH/PUT/DELETE` 且 `Requires=principalMachine` 的路由都要在 `machineFloorWriteRulings` 有**追溯得到裁定**的紀錄（含票號/owner 日期/憲章節次，`Why` ≥40 字元）。
  ⇒ **本票若新增任何寫入端點（例如 server-backed `pinned_member_ids` 的 PUT），落在 machine 地板就必須寫一筆 ruling；任何 handler 內部的身分判斷就必須進 inventory。**
- **`server/CLAUDE.md:47`** — ⚠️ `callerContextTypes` 是字串比對的型別名，打錯只會**靜默縮小掃描範圍**；`TestCallerContextTypesAllExist` 是補丁。
- **`server/CLAUDE.md:43`** — 誠實提醒：這兩道閘**擋不住「一句話的敷衍」**，它們的價值是把「靜默新增」變成「可見新增」，真正擋下壞條目的是**讀 diff 的人**。

### 9.5 chat / unread / 讀取水位的既有裁定

- **`server/CLAUDE.md:117-127`**（T-e2b2 / T-62a8）— 聊天附件的原子寫入：三個原子 DAL 入口（`PutChatWithAttachments` / `PutReplyCardWithChat` / `PutReplyCardWithAttachments`），**帶附件的路徑不要直接呼叫 `PutChat`**。
- **`server/CLAUDE.md:125-126`** — 🔴 **「同一份四欄清單現在有兩個家,兩邊都要改」**：測試側 `referencedAttachmentIDs`（寫入面）與生產側 `dal.go collectSurvivingBlobRefs`（刪除面）。`DeleteChatInvolving` 是**全域唯一**刪 `chat_attachment` 的敘述。
- ⚠️ **`server/CLAUDE.md` 全文中我沒有找到任何關於「roster 排序」「unread_count 演算法」「chat 讀取水位語意」本身的裁定** —— 這幾件事的權威只在碼與 doc comment 裡（`domain.go:404-410`、`dal.go:126-132`、`api_members.go:65-83`）。這是**觀察，不是推論**：我對 148 行全文都讀過。

### 9.6 其他值得知道的橫向規範

- **`server/CLAUDE.md:146`** — 改 Go → fresh build 驗證、**不 commit binary**；需要單檔 boot 的 build 前先 `bash bin/build-seedsdist && bash bin/build-bindist`。
- **`server/CLAUDE.md:147`** — server 模組允許第三方依賴（toml / goose / modernc sqlite），但「新增依賴前先想能不能 stdlib」。
- **`server/CLAUDE.md:29`** — ⚠️ **telemetry 的 `additionalProperties: false` 是被刻意避開的**（「關掉開放性 = 舊/新 warden 多送一個沒聽過的巢狀鍵就整份 422,那正是 a7fa594 全 fleet telemetry 靜默歸零的事故形狀」）。但 **MemberDTO 用的是 `additionalProperties: false`** —— 兩者方向相反是**刻意的**（一個是 ingest 的寬容，一個是 response 的嚴格），別把 telemetry 的理由套到 MemberDTO 上。
- **root `CLAUDE.md:21` §8 + `:23` §9** — doc↔碼 不一致時**不自裁**哪個對（§5.3 的 CLAUDE.md:83 過時就是照這條處理：回報，不改）。

---

## 10. 未確認 / 明確沒做的事

1. **沒有執行任何測試或 build** —— 本文件全部結論都是靜態讀碼。`go test` / `bash bin/ci.sh` / `conformance/run.sh` 一次都沒跑。
2. **沒有對 live server 打過任何請求** —— 「基準 commit == live `git_sha` v0.5.45」是採信交辦單，我沒有親自 `curl /api/version` 坐實。
3. **正式站 `chat_message` 的實際資料量未知** —— 直接影響 §4.4 三個選項的取捨。
4. **FE 側只做了針對性 grep**（`OfficePage.tsx` 的 sort、`mappers.ts` 的 `toMember`），**沒有**完整盤點 FE 的 roster 渲染鏈。若設計走「後端排序」，FE 那個既有 stable sort 會蓋掉後端順序 —— 這一點我看到了，但**沒有**追完整條 hook→component 資料流。
5. **`seeds/` 完全沒讀** —— 若本票會改動 agent 互動面（新 MCP 工具 / 新流程），root `CLAUDE.md:23` §9c 要求 `seeds/` 同批更新。本文件不涵蓋這一面。
6. **`feat/member-custom-avatar` 只做了 `git show --stat` / `git ls-tree` 兩個唯讀查詢** —— 沒有 checkout、沒有讀它的 diff 內容。它對 `MemberDTO` 加了什麼欄位（很可能有 avatar 相關欄位）**我沒有查**，這對本票的 spec 衝突評估可能重要。
7. **`00027_outsource_intent.sql` 被刪除的原因未確認** —— 只查到刪除發生在 `6c82e2a`，沒有讀該 commit 的完整脈絡。

---

## 11. 發現但不在本次範圍的問題

1. 🔴 **`server/CLAUDE.md:83` 已過時**：仍寫 `00038_member_avatar.sql` 被 PR #12 佔用，但 `feat/member-custom-avatar` 已 rebase 到 `6158b32` 之上並改用 `00040_member_avatar.sql`（§5.3 有唯讀證據）。依 root `CLAUDE.md:21` §8 護欄，**我不自行修改**；建議由 avatar 分支在 rebase 責任內同批更新（該段文字自己也是這麼寫的）。
2. **`00027` 跳號沒有任何 doc 記載** —— `00026` → `00028` 之間的空號來自 `00027_outsource_intent.sql` 被刪除，`server/CLAUDE.md:83` 只記了 00038。下一個做 migration 的人會困惑。建議在該段補一行，但同樣不在本票範圍。
