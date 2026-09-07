# 傳承（lore）實作規格 — T-33

**這份是實作的唯一規格。** owner 於 2026-09-07 `rc-8ad66d9c91f6` 圈 [0] 核可。
設計背景與取捨在任務產物 `ta-4e133e49a6be`；草圖 https://claude.ai/code/artifact/506201df-5feb-4aff-ba8f-42bc21bf84f1

---

## 0. 紅線（違反要當阻擋項回報，不要自己放行）

- **不准在本機跑整包測試**（`bash bin/ci.sh` 之類）。只跑改動範圍的點名測試：`cd server/ocserverd && go test -run '<Name>' ./...`；前端 `npx vitest run <檔>`。
- **這台機器是 seth-m5，正式站主機**：不准跑端到端、不准起測試站。
- **不自己合併、不自己上站、不自己調 cap 旋鈕。**
- **檔名不要帶 `_token`**（`.gitignore` 會靜默吞掉，`git status` 也看不到）。
- **產生檔只能用產生器**：`ocapi_gen.go` ← `bin/gen-ocapi`；`schema.ts` ← `npm run gen:api`；`mcp-catalog.json` ← `python3 bin/gen-mcp-catalog`（🔴 是 python，用 bash 會語法錯）；`migration.lock` ← `bin/gen-migration-lock`，**不准手改**。
- **`server/ocserverd` 是獨立 go module** ⇒ `cd server/ocserverd && go test ./...`。新工作樹首跑前先 `bash bin/build-seedsdist`、`bash bin/build-docsdist`，否則會有假紅。
- **退出碼一律落檔再讀**（`cmd > out.txt 2>&1; echo $? > rc.txt`）。`cmd; echo "rc=$?"` 回報的是 `echo` 的退出碼。
- **零命中的預設解讀是「查法寫錯了」**，每次配陽性對照。
- `wc -m` 在 `LC_CTYPE=C` 下數位元組 ⇒ 先 `export LC_ALL=en_US.UTF-8`。

---

## 1. 資料模型

**Migration 版號 `00093`。** 🔴 遠端分支已佔到 00092（掃過全部 454 個 `refs/remotes/origin/*`，配過陽性對照）；用「主線最大 +1」＝00089 會撞號。

表 `lore_entries`：

| 欄位 | 型別 | 說明 |
|---|---|---|
| `id` | TEXT PK | 對外顯示成 `L-<n>`；`n` 全域遞增 |
| `seq` | INTEGER | 全域遞增，供 `L-<n>` 顯示與穩定排序 |
| `scope_kind` | TEXT | `role` 或 `manual` |
| `scope_key` | TEXT | `role` ⇒ role_key；`manual` ⇒ task manual 的 `type_key` |
| `title` | TEXT | ≤ 標題上限（預設 80 字） |
| `body` | TEXT | ≤ 內容上限（預設 500 字） |
| `author_id` | TEXT | 寫入者的成員 id，**寫入當下釘死，之後不隨名冊變動** |
| `source_task_id` | TEXT | 可空，寫入時所在的任務，純紀錄 |
| `state` | TEXT | **`active` / `pinned` / `retired` 三選一互斥** |
| `retire_reason` | TEXT | 可空，只在 `retired` 時有意義 |
| `effective_ts` | REAL | 生效期；建立時＝現在 |
| `created_ts` | REAL | **建立時間，永不變動** —— 這是「最初的生效期」，讓「提到最新」可逆 |
| `updated_ts` | REAL | 狀態或理由變動時更新 |

🔴 **條目沒有編輯路徑。** 不提供改 `title` / `body` 的 API。可變的只有 `state`、`retire_reason`、`effective_ts`。
🔴 **`created_ts` 一旦寫入永不變動**，`effective_ts` 才是「提到最新」會蓋掉的那一個。

索引：`(scope_kind, scope_key, state, effective_ts DESC)`。

---

## 2. 挑條目的邏輯（🔴 只能有一份）

一個函式，兩個出口都呼叫它。**不准在兩個地方各寫一遍。**

輸入：`scope_kind`、`scope_key`、字數上限。
步驟：
1. 取該 scope 底下 **`state != 'retired'`** 的條目。
2. 排序：**`pinned` 在前，其餘依 `effective_ts` 由新到舊**；同群內也依 `effective_ts` 由新到舊。
3. 依序累加「標題＋內容」的**字數**（Unicode 字元，不是位元組），**裝不下的整筆不載入**（不截斷），並**繼續往下試**還是**就此停住**——🔴 **就此停住**：後面的一定更舊，繼續試會讓載入集合跳號，畫面上那條線也就畫不出來。
4. 回傳被選中的條目，以及**第一個落選的位置**（畫界線要用）。

---

## 3. 兩個出口

**角色傳承**（`scope_kind='role'`）
接在正職開機檔的「長期筆記」區塊後面。**只有正職**，外包沒有角色，開機檔本來就不載長期筆記。
🔴 不要動外包那條開機路徑，也不要去抽正職與外包共用開頭那段既有的重複 —— **那段跟傳承無關，屬於 T-14 的範圍，不在本次授權內。**

**任務傳承**（`scope_kind='manual'`）
接在 `GET /api/task-manuals/{type_key}` 回應裡 learnings 的後面。**不進任何人的開機檔**，正職與外包一視同仁。

上限各自獨立，**不相加**。

---

## 4. 設定：新增四列

加在既有的參數調整清單上（`SettingsPage.tsx` 的 `DOC_CAP_FIELDS` / `DOC_CAP_ORDER` 是表驅動的，加旋鈕＝加表項＋一對 i18n key，**不是新頁面**）：

| 列 | 預設 |
|---|---|
| 角色傳承字數上限 | 10000 |
| 任務傳承字數上限 | 10000 |
| 傳承標題字數上限 | 80 |
| 傳承內容字數上限 | 500 |

⚠️ 既有的 doc-cap 旋鈕有「只能調高」的規則。**傳承這四列不套那條規則**（那條規則存在是為了保護會被重寫的文件，而條目不能編輯，調低只影響新寫入、卡不住任何舊的）。owner 今天自己就把標題從 140 調到 80、內容從 1000 調到 500。

---

## 5. MCP 工具

寫入只走 MCP，座艙不做撰寫表單。

- **寫一筆**：`title`、`body`、可空的 `task_id`。
  - `task_id` 空 ⇒ `scope_kind='role'`，`scope_key` ＝呼叫者的 role_key。**呼叫者沒有角色（外包）⇒ 400**，訊息要說明沒有可寫的位置。
  - `task_id` 有 ⇒ 取該任務的 `type_key`。**`type_key` 是空的（臨時任務）⇒ 400**，訊息要說「這張任務沒有類型，沒有可寫的位置」。
    🔴 **不准默默歸到角色傳承。** 那會讓每個人開機都付那筆字數，而真正需要它的任務永遠拿不到，且不報錯。開機說明本來就寫著臨時任務沒有該寫的位置。
  - 超過標題或內容上限 ⇒ 400，**一個字都不寫**。
- **改狀態**：`entry_id` ＋ 目標狀態（`active` / `pinned` / `retired`），可帶 `retire_reason`。
- **提到最新**：`entry_id` ⇒ `effective_ts` ← 現在。`created_ts` 不動。
- **讀清單**：依 scope 篩選、分頁。

---

## 6. 前端清單頁

自己的分頁。**整套照任務卡的設計語言**（去讀 `TaskCard.tsx` / `tasks.css` 實際的樣子，不要照轉述）：

- 整列可點展開／收合，內容預設一行截斷
- 右上角一顆 chevron 指示器（inline SVG，無邊框無背景，展開是換一顆圖不是旋轉）
- **一顆狀態徽章**，點了在徽章左緣下方開小選單選 `失效 / 生效 / 置頂`，當前值用強調色＋700
- 「撰寫人」自成一列：標籤在左、頭像＋名字膠囊在右，**傳訊息圖示在膠囊裡**（點了跳聊天頁）；**撰寫人已不在名冊上就不顯示那顆圖示**
- **不做轉派**
- 生效期與「提到最新」同一行，生效期靠左、按鈕靠右
- **失效理由沒填就整列不顯示**
- 三群：置頂 → 生效中 → 已失效。**排序固定不給改**，篩選可以
- **界線**：篩選收斂到單一主詞時才畫一條具名上限線，線以下的條目**內容**轉灰（狀態徽章與按鈕維持正常色）。沒篩選就不畫線也不轉灰。**不要**統計數字、進度條、比例數字
- 任務下拉要有明確的 `無 · 角色傳承` 這一項；**條目列上不標它**
- 捲到底載 30 筆，**分頁要在伺服器端帶篩選一起走**，否則計數與界線會錯、且「捲到底沒有了」跟「真的沒有了」長得一樣

---

## 7. 測試的判準

🔴 **綠燈沒有證據力**：判準是「把修補拿掉，對應的測試真的會紅」。
- 種 mutant 至少 4 顆，一顆只改一處，每顆前 `go clean -testcache`，還原用自己備份的檔案（**不要 `git checkout --`**，那是整檔粒度，會靜默清掉同檔其他未提交改動）。
- 🔴 特別驗「挑條目真的挑對了」：排序、跳過失效、裝到上限就停。**這是最容易做出假綠的一格。**
- 恆真斷言檢查：新斷言套回改動前的碼上跑，必須真的失敗。
