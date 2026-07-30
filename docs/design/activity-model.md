# 活動模型 — 「它現在有沒有在跑 turn」

> T-a1d7。本文件是 **activity** 這個維度的 logical spec:它與 `state-model.md` 的
> **presence** 是**兩個正交的維度**,不可互相取代、不可合併詞彙。
> 相關實作:`server/ocserverd/domain.go`(`deriveActivity`)、
> `server/ocserverd/api_activity.go`(store + ingestion + 兩個連線邊)、
> `cli/ocagent/activity.go`(Claude hook 上報)、
> `cli/ocwarden/spawn.go`(hook 安裝)、`cli/ocwarden/codex_session.go`(Codex 上報)。
> code 與本文不合時 flag owner 定奪(repo CLAUDE.md §8)。

## WHY

`online` 只回答一件事:**它現在正持著一條到 server 的 SSE 連線**(`state-model.md`
原則 1,二態、無 heartbeat、無 TTL)。它**不**回答 owner 真正想知道的那件事:
**模型現在到底有沒有在跑?** 一個閒置整晚的 agent 與一個正在跑 40 分鐘長工作的
agent,在監控頁上長得一模一樣。

所以 activity 是**新增的第二個維度**,不是把 presence 變細。
⚠️ **絕不塞進 `deriveLiveness` / presence 五態**:那是 presence 專用核心,把兩組詞彙
攪在一起,「離線」和「沒在工作」就會變成同一個字。

## 狀態(四態閉集,server 裁決)

| state | 何時 | 顯示錨點 |
|---|---|---|
| `active` | 有 turn 主張、agent **目前 online**、且 `now - since <= activityMaxTurnSecs` | `working_since` |
| `idle` | 有回報過,但當下沒有 turn 主張(或不 online) | `last_turn_completed_at`(可能沒有) |
| `unknown` | 有 turn 主張、online,但超過門檻沒收到結束訊號 | `working_since` |
| `never` | 從未收到任何 activity 上報(舊 runtime、server 剛重啟) | — |

**為什麼逾時是 `unknown` 而不是翻 `idle`**:我們並不知道它閒下來了——我們知道的是
「它說它開始了、然後就沒消息」。翻成 `idle` 是**捏造一個我們沒觀察到的事實**;
`unknown` 保留 `working_since`,owner 一眼看到「這個宣稱工作了 47 分鐘」,那正好是他
該去看一眼的時候。同源紀律:`hardware_stale` 也是**標記而非收回數值**。

**`activityMaxTurnSecs = 2700`(45 分鐘)** — owner 2026-07-28 於卡
`rc-3b18366fbe04` 拍板。這是**誠實度 vs 誤標率**的取捨:門檻太低,正常的長工作會
一直被標成「未收到結束訊號」,owner 會開始不信任這一欄;門檻太高,Claude 使用者按
ESC 之後畫面會持續說「工作中」很久,那段時間是真的在說謊。

⚠️ **`activityMaxTurnSecs` 與 `ZombieConfirmGrace`(180s)是兩個不同的量,不可互相
取代**:
- `activityMaxTurnSecs`:agent **仍 online**,但這一輪沒送結束訊號 → 多久後停止宣稱工作中。
- `ZombieConfirmGrace`:**連線層**斷了多久才算真的離開(重連時要不要保留舊主張)。
  activity **直接沿用**這個既有常數,不自己發明第二個數字——它的推導與事故背景
  (2026-07-20:STOP 撞上一個再幾秒就會自己重連的 session)正好就是「重連中 vs 真的
  沒了」,與這裡要判的是同一件事。

## 儲存:記憶體,重啟失憶是契約

依 `state-model.md` 的判準——「重啟後要不要記得?不要(重連就自然重建)→ observed →
記憶體」——**`working_since` 與 `last_turn_completed_at` 都只活在記憶體,不落 DB**。
專用的 `s.activity` store,**刻意不掛進 `s.telemetry`**:
1. telemetry 的既有契約是「只在解僱時清、**斷線不清**」,activity 需要斷線邊,
   混在一起會讓那條契約變成「看情況」;
2. telemetry 裝的是**量測樣本**,activity 裝的是**狀態主張**,清除與去重規則都不同。

**已知代價(誠實)**:server 重啟(含每次升級)後所有成員回到 `never`,直到下一次
turn 邊界。**為什麼仍選這條**:`banked_cost` 之所以 durable 是因為錢不能掉;
「上次結束多久」掉了只是顯示退化。這個決定**局部可逆**:日後要記得,補一張表 +
開機讀回即可,wire 完全不用動。

## 時間戳一律用 server 收訊時刻

reporter 可能跑在別台機器上,時鐘偏移會讓畫面出現「工作中 -3 分鐘」。reporter 的
`seq` 只用於**排序**(且只與同一 reporter 的前一個值比較),永不顯示。

## 斷線 / 重連

1. **斷線不刪主張**,只記 `offline_since`。
2. **衍生時 gate**:`activity_state` 只有在 agent **目前 online** 時才可能是
   `active` / `unknown`。這滿足「離線絕不顯示工作中」的意圖,但用的是**衍生時
   gating** 而不是刪資料——刪掉就救不回來,gate 可以。
3. **重連時才決定要不要丟掉舊主張**:`now - offline_since < ZombieConfirmGrace` →
   保留(網路抖動,同一輪 turn 撐過去了);否則丟棄。

**為什麼不是「斷線就清」**:`ocagent listen` 的 drop→auto-reconnect 是**常態**
(設計期間單一 session 內實測 2 次 `stream ended: unexpected EOF` 緊接重連,turn
全程未中斷)。斷線就清會在那一瞬間把「工作中」清掉,而 Claude 要到 turn 結束才會再
送訊號——這一輪剩下的時間就再也顯示不出來。

⚠️ **斷線刻意不寫 `last_turn_completed_at`**:我們沒有觀察到那一輪結束,只觀察到它
不見了。捏一個結束時間就是說謊。

## 訊號來源(兩個 runtime,能力不對稱)

| 情境 | Codex | Claude |
|---|---|---|
| 正常結束 | ✅ `turn/completed` | ✅ `Stop` hook |
| API 錯誤 | ✅ `turn/completed` | ✅ `StopFailure` hook(**取代** `Stop`) |
| 使用者中斷 | ✅ `thread/status/changed` → idle | 🔴 **無訊號** → 靠門檻 |
| turn 中 `/clear` | n/a | ⚠️ `SessionEnd` hook |
| crash / kill | 🔴 無訊號 → **靠 SSE 斷線** | 🔴 無訊號 → **靠 SSE 斷線** |

**收斂觀察**:「進程死掉」的情境 SSE 一定會斷,所以斷線是**唯一一個不需要新機制、就能
覆蓋全部進程級漏送**的收斂點。剩下真正需要門檻裁決的,只有「進程還活著、只是沒送
結束事件」這個窄很多的情境。

**Codex 為什麼同時吃 turn 邊界與 `thread/status/changed`**:前者是**邊界語意**(帶
turn id 與精確時刻,掉一個就永遠對不回來),後者是**狀態語意**(可重述、冪等,中斷後
會送),兩者併用等於讓狀態訊號當自我修復。同一個 (state, turn) 重述在 sidecar 端就
被丟掉,不浪費一次 HTTP。

## 去重與排序

| 規則 | 內容 |
|---|---|
| R1 | 同一 `session_id` 內,`seq` 沒有比存的大 → 丟棄(亂序保護) |
| R2 | `session_id` 改變 → 新 session,接受並丟棄舊主張、重置 seq 基準 |
| R3 | `idle` 只在 `turn_id` 與存的 active turn 相符(或任一方為空)時才清主張 |
| R4 | 同一 turn 重複宣告 → 不重新錨定 `since`(否則長 turn 會看起來剛開始) |

`last_turn_completed_at` **只在 idle 真的關掉一個主張時**才蓋。裸 idle(沒有主張可關)
不蓋——我們沒有觀察到任何一輪結束。

## 前端界線

🔴 **FE 不做任何 verdict**:`activity_state` 直接 passthrough,**不拿時間戳跟自己的
時鐘比去推「他還在不在工作」**(`frontend/CLAUDE.md` 明文既有裁定:門檻只有一個家,
第二份必然會跟第一份各說各話)。FE 只做 `now - anchor` 的**格式化**,沿用
`lib/duration.ts` 的 `formatDuration`,ticking 掛在 `MonitorPage` 頁面層(30s,
純顯示、零 fetch、零模型呼叫)。

## 範圍界線

- 落點只有**監控頁 AI 會話表**(正職列與外包列共用同一個 `ActivityCell`)。
- **不動** `PresenceBadge` / `MemberCard` / 辦公室頁:`PresenceBadge.tsx` 記載 Seth
  當初親自砍掉 last-seen 副標(「同一行三份重複表達 presence」)。activity 嚴格說是
  **正交的新維度**、不是那份被砍的重複,但擴到辦公室頁屬於**範圍擴張**,留給 owner
  決定。
- **不出 MCP 工具**(`MCPExclude`):這是 runtime 自動化的窄用途上報,出成工具反而會
  誘導 agent 手動謊報自己的活動狀態。`seeds/` 因此**不需要**同批更新(憲章 §9 的
  觸發條件是「agent 要照做的流程」;這條路徑 agent 不該碰)。

## Rollback

wire 純新增且全 optional、無 migration、無 durable 狀態 → 換回舊 binary 即完全還原。
Claude hooks 寫在 per-agent workdir 的 settings.json、每次 spawn 重寫,舊 binary 一
spawn 就沒有 hooks,沒有殘留要清。唯一不可自動還原的:已 spawn 且還活著的 agent 的
settings.json 仍帶 hooks,那些 hook 會呼叫一個舊 ocagent 不認得的子命令 → hook 失敗,
而失敗的 hook 不阻擋 turn。
