# T-ed38 · Step 1 — live baseline 與乾淨工作基線

任務：OffiCraft 成員列表紅燈／最近互動排序與手動置頂（識別鍵 `member-roster-priority-sorting`）
執行者：O-3（`ow-5f4832973889`）
記錄時間：2026-07-28

---

## 1. 三件可查證事實

### 1.1 live ocserverd 實際執行的 commit／版本

兩個獨立來源一致：

```
$ go version -m ~/.officraft/bin/ocserverd | grep ldflags
  build -ldflags="-s -w -X main.buildSHA=6158b32 -X main.buildTime=2026-07-28T09:48:12+08:00 -X main.appVersion=v0.5.45"

$ curl -s http://127.0.0.1:7755/api/version
{"version":"v0.5.45","git_sha":"6158b32","git_time":"2026-07-28T09:48:12+08:00",
 "catalog_hash":"d85c5f472eea492e","update_available":false,"latest_version":null}
```

- **live version**：`v0.5.45`
- **live git_sha**：`6158b32`（binary 的 build stamp 與跑著的 process 自報一致）

### 1.2 目標 repository 的 HEAD 與 branch

```
$ cd /Users/ray/ai-workspace/OffiCraft
$ git rev-parse HEAD              → 6158b32ec39a4c69b3b874783e1a213f43cd265c
$ git rev-parse --abbrev-ref HEAD → main
$ git rev-parse origin/main       → 6158b32ec39a4c69b3b874783e1a213f43cd265c
```

- **本機 HEAD == origin/main == live git_sha**。三者同一顆 commit。
- ⚠️ 附註：root `CLAUDE.md` §13 寫「此 repo 的 local main 是條孤兒 old-flat-layout 歷史，與 origin/main 無共同祖先」。**實測與此不符**：目前 `local main` 與 `origin/main` 指向同一顆 commit `6158b32`。這是一份可能已過時的文件敘述，但本次工作不依賴它——我一律以 `origin/main` 為基準開分支，行為上與該條規範一致，因此**不構成需要停下開卡的權威衝突**（沒有任何改動取決於哪一邊是對的）。若後續需要修 doc，另列候選。

### 1.3 worktree 是否乾淨、有無未推送 commit

```
$ git status --porcelain   → （空）
$ git log origin/main..HEAD → （空）
$ git stash list            → （空）
```

- 主 worktree **乾淨、無未推送 commit、無 stash、無進行中的 merge／rebase／cherry-pick**。

---

## 2. 既有使用者修改：識別與不覆蓋確認

`git worktree list` 顯示這台上共有 4 個 worktree，其中 3 個不屬於我：

| worktree | branch / HEAD | 狀態 | 對本任務的意義 |
|---|---|---|---|
| `~/ai-workspace/OffiCraft` | `main` @ 6158b32 | 乾淨 | 我不在此工作，只讀 |
| `~/.officraft/agents/ow-2ff44502a778/worktrees/member-custom-avatar` | `feat/member-custom-avatar` @ 42bbf12 | **有 origin/main 沒有的 commit，進行中** | ⚠️ 高度重疊，見 §2.1 |
| `~/.officraft/agents/ow-3a5add03ef29/worktrees/member-activity-duration` | `feat/member-activity-duration` @ 62434e8 | 已是 origin/main 的祖先（已 land），該 worktree 無未提交變更 | 已合併，無衝突風險 |
| `~/ai-workspace/.worktrees/OffiCraft-pr-12-resolve` | detached @ 6158b32 | — | 與本任務無關 |

**不覆蓋確認**：我不碰上述任何一個 worktree、不 checkout 它們的 branch、不動主 worktree 的 HEAD。我的工作全部在新建的隔離 worktree 內：

```
$ git worktree add -b feat/member-roster-priority-sorting \
    ~/.officraft/agents/ow-5f4832973889/worktrees/member-roster-priority-sorting origin/main
$ git status -sb → ## feat/member-roster-priority-sorting...origin/main   （乾淨）
```

### 2.1 ⚠️ 併行工作衝突風險（已識別，需在設計階段處理）

`feat/member-custom-avatar`（另一個外包 worker，尚未合併）與本任務的預期改動面**大量重疊**：

```
$ git diff --stat origin/main...feat/member-custom-avatar
 server/ocserverd/api_members.go     | 125 ++++++++
 server/ocserverd/dal.go             | 163 +++++++++-
 server/ocserverd/wire.go            |   3 +
 server/ocserverd/routes.go          |  23 ++
 server/ocserverd/ocapi_gen.go       | 129 +++++++-
 server/ocserverd/migrations/00040_member_avatar.sql | 16 +
 spec/openapi.json                   | 218 +++++++++++++
 frontend/src/types.ts               |   3 +
 …（共 61 檔，+2604 / −131）
```

重疊點：`MemberDTO`（`wire.go`）、roster query（`dal.go` / `api_members.go`）、`spec/openapi.json`、生成物 `ocapi_gen.go`、**migration 序號**。

- **migration 序號**：目前 `origin/main` 最高為 `00039_member_last_machine_id.sql`（`00038` 跳號）。avatar 分支已佔用 `00040`。若本任務需要 migration，必須避開已被佔用的序號（手冊 learnings 已記過此坑：goose 序號低於 DB 現有版本會被 `goose.Up` 靜默跳過）。
- 這一點會在「盤點技術影響面」與「技術設計」兩步各留一條具體對策，不在此拍板。

---

## 3. 已讀的 repository instructions 與對本任務的約束

| 文件 | 讀取範圍 | 本任務要遵守的重點 |
|---|---|---|
| root `CLAUDE.md` | 全文（66 行） | §7 push 前逐檔審 manifest；§8 改碼與 context doc 同一 commit；§11 commit 格式 `[why]`／`[how]` + 結尾 `Co-Authored-By: <實際模型名> <noreply@anthropic.com>`；§12 **對外 DTO 加欄一律 optional**、路由 table-driven；§13 **wire 凍結 → 改 wire 一律 spec 先行再 `bin/gen-ocapi` 重生**、CI 綠 = rc==0 **且**輸出最後一行精確為 `[ci] all green`、land 前 working tree == commit |
| `frontend/CLAUDE.md` | 全文（373 行） | seam 分層 `wire → mappers → types → adapter → mock → http → hooks → component`（不可跳層）；unread badge = server 算好的 `MemberDTO.unread_count`，**FE 純 passthrough 不自算**；`useMembers` 的 ROSTER_TOPICS 含 `chat`／`chat_read`；mobile 斷點 720px；長 token 溢出的單一來源在 `.doc-md`；浮層寬度不可用 `vw` 夾 |
| `server/CLAUDE.md` | 第 1–63 行親讀（全檔 149 行） | wire DTO 手寫在 `wire.go`（生成型別只當 request-body 詞彙）；route 表 122 rows 鏡自 `conformance/routes_manifest.json`；RBAC 單一 resolver + 路由表宣告；授權寫在路由表外必須列進 `authz_surface_gate_test.go` 清單；migrations 走 goose embedded FS |

**揭露**：`server/CLAUDE.md` 第 64–149 行未由我親讀（該檔單頁即超過 context 讀取上限）。這一段交由「後端現況驗證」節點的 sub-agent 完整讀過並回報與本任務相關的規範，結果併入 `backend-baseline.md`。在那份回報進來之前，本任務不對後端做任何實作決定。

---

## 4. 尚未驗證（留給後續節點）

以下四項是任務輸入標記為「待驗證」的敘述，**本節點不宣告任何一項為事實**，全數留給第 2／3 節點以 commit-SHA 原始碼證明或否證：

1. 成員列表目前僅助理角色置頂、其餘維持後端姓名排序
2. `unread_count` 紅燈的資料來源與呈現方式
3. `chat`／`chat_read` 之後列表以何種機制更新（refetch 或 SSE 事件）
4. `MemberDTO` 缺 `last_activity_at`

（第 4 項有一則初步線索：`grep -rn "last_activity|lastActivity|activity_at|activityAt"` 在 `server/`、`spec/`、`frontend/src/` 全無命中——但這只是文字面否證，仍待後端節點從 `wire.go` 的 `MemberDTO` 定義正面確認。）
