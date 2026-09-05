-- +goose Up
-- T-33 — 規則 v8 的格式：標題獨立成格，`problem` 變成 `impact`，impact 帶星等與審核旗標。
--
-- owner 2026-09-05 逐字「用這個去做吧」，對象是規則 v8（ta-091e7a9cb434）。v8 的
-- 六格是：標題／1 對象×活動／2 內容（機制→實例→動作）／3 射程邊界／4 impact／
-- 5 相關事件。第 5 格已經是 lore_event 那張表，1～3 格已經是 trigger / content /
-- retire_when，所以這一支只動三件事：補上標題、把第 4 格改名、給第 4 格兩個欄位。
--
-- ── 為什麼是 ALTER，而 00081 當初是直接改欄位宣告 ─────────────────────────────
--
-- 00081 的檔頭寫著它可以直接改宣告，條件是「這個分支自己引入、還沒進 main、線上
-- 零資料」。那個條件到這一支已經不成立，而且失效的時點比「合併」更早一步：這個站
-- 發版是自動的（push 到 main ＋ CI 全綠就自己發），所以 T-33 一合併，00081／00083
-- 的欄位就必然會出現在真的資料庫裡。
--
-- 🔴 而那之後再去改宣告，不會報錯，只會讓「已經升級過的站」與「全新安裝的站」從
-- 此帶著不同的 schema，兩邊都認為自己是對的。ALTER 是唯一能讓兩種站走到同一個
-- 狀態的寫法。
--
-- ── 🔴 00081 裡有一段說明從這一支開始是假的，而我不能去改它 ──────────────────
--
-- 00081 的 lore_entry 註解裡寫著：
--   「`trigger` 兼任這條條目的標題……五格裡根本沒有『名字』這一格」
--   「⚠️『第一格兼任標題、因此拿掉 label 與 40 runes 上限』是實作判斷，不是負責人
--     的裁定。它被寫在這裡而不是默默做掉，就是為了讓下一個人看得見它可以被推翻。」
--
-- **v8 推翻了它**：標題是獨立的一格，而且 v8 對標題有 trigger 沒有的要求（寫「發生
-- 了什麼」、不得是祈使句、標題裡的名詞與數字都要在第 2 格找得到）。
--
-- 🔴 那段註解沒有被就地更正，是因為 migration.lock 對每一支 migration 的**檔案內容**
-- 做 sha256。改 00081 一個註解字元 ⇒ lock 中段那一行變動 ⇒ 那正是
-- migration_lock_t75_test.go 用來抓「一支已釋出的 migration 被編輯」的訊號。
-- ⇒ **更正寫在這裡，這一支就是那段話的接續。** 讀 00081 那段的人，請讀到這裡為止。
--
-- ⚠️ 判準要講準，我第一版寫寬了，Kyle（第 44 代）收窄的：
-- **判準是「這支 migration 的那一行在不在 lock 的中段」，不是「它有沒有進過 main」。**
-- 進 main 只是其中一個充分條件 —— 你在自己的工作樹裡改一支既有 migration，lock
-- 中段當場就動了，還沒有人合併任何東西。
-- 🔴 寫寬的那一版會害人：它會讓下一個人在自己工作樹裡動了 00081、跑完
-- gen-migration-lock、看到中段變動，然後以為那是誤報。中段變動永遠不是誤報。

-- ── 標題 ────────────────────────────────────────────────────────────────────
-- 沒有長度上限，理由跟 trigger 當初一樣：v8 自己示範的標題是整句話。
-- 空字串 = 還沒寫。這一格 v8 要求必填，但跟 trigger 一樣，「拒絕空值」放在 DAL，
-- 不放在 CHECK —— SQLite 的 CHECK 訊息說不出是哪一格空了。
ALTER TABLE lore_entry ADD COLUMN heading TEXT NOT NULL DEFAULT '';

-- ── 第 4 格改名：problem → impact ───────────────────────────────────────────
-- 不是換個說法。`problem`（之前發生過什麼問題）問的是起因；v8 的 `impact` 問的是
-- 「我們原本想 ooo，結果 xxx 了」——原本要達成什麼、實際變成什麼。
-- 🔴 v8 明寫：那件壞事其實沒發生（護欄先擋下來了）就寫「沒有發生，因為…」，
-- 不要編一個沒發生的後果；真的填不出來就留白。
ALTER TABLE lore_entry RENAME COLUMN problem TO impact;

-- ── 4b 星等 ─────────────────────────────────────────────────────────────────
-- 刻度是 owner 2026-09-05 逐字的三句：做白工 → 做完更糟糕了 → 還把其他東西也弄壞。
-- 判法只有一個問題：「弄壞了什麼？」
--   沒弄壞任何東西 = 1｜弄壞的只有你動的那個 = 2｜弄壞的包含你沒動的 = 3
-- ⚠️ 三級之間不是累加。舊版寫「還弄壞了別的」，那個「還」會被讀成「3 一定要先滿足
--    2」，owner 判定它模糊並拿掉。真正的分界只有一條：弄壞的東西在不在你動的範圍內。
-- ⚠️ 沒有但書。草稿的「修好的比弄壞的大 ⇒ 降一級」在 24 條真實條目上一條都沒觸發，
--    owner 看過四個實例後認可沒有但書的判法。要加回來請先找到一條真的需要它的條目。
--
-- 🔴 0 不是一個星等，是「還沒判」。CHECK 允許 0 是因為這一欄對既有列必須有預設值，
-- 而把既有列預設成 1（沒弄壞任何東西）等於替它們做了一次沒有人做過的判定。
-- 「還沒判」與「判為 1」必須分得開，否則 v8 的自檢就無從查起誰漏填。
ALTER TABLE lore_entry ADD COLUMN impact_stars INTEGER NOT NULL DEFAULT 0
    CHECK (impact_stars BETWEEN 0 AND 3);

-- ── 審核旗標 ────────────────────────────────────────────────────────────────
-- 🔴 這一欄的需求出處，以及它今天還沒有被裁定的部分 —— 讀之前先看這段。
--
-- 規則 v8（ta-091e7a9cb434）**整份文件裡沒有「審核」「admin」「排序」「蓋章」
-- 這幾個字**（grep 過，五個關鍵字全部 0 命中）。「審核與星等是兩欄／審核旗標只有
-- owner＋admin／排序是審核過的在前」這三句的出處是**我們自己寫給 owner 的一則
-- 訊息**（`c-8e647bf72e80`，from = ow-e27260b9ed05），不是他的裁定。它被寫進交接
-- 時標成了「v8 定案」，我照著它把「只有 owner 與 admin 能動」寫成事實 —— 那句話
-- 現在拿掉了，因為它宣稱了一個沒有人做過的決定。
--
-- 🔴 owner 真的裁過的是 `rc-ccd8ef9517fb`，逐字：「我審核以後可以給分數 沒有審核
-- 過的零分 審核過的最高可以到五分 表示這條目的權重」—— 那是**一欄**（0＝未審核，
-- 1–5＝權重），跟這裡的兩欄是**相反的結構**。
--
-- ⚠️ 兩欄仍然可能是對的，理由是兩個軸不同：他那個分數是**人審過之後給的權重**，
-- v8 4b 的星等是 **agent 提案的「弄壞了什麼」**。壓成一欄會讓「沒有人審過」跟
-- 「審過但影響輕微」變成同一個 0。**但這是推論，不是裁定** —— 沒有人問過他那兩個
-- 數字是不是同一個。卡在 `rc-37f10fec50d1`，等他回。
--   是同一個 ⇒ 一欄，這個 `reviewed` 欄要拿掉。
--   不是     ⇒ 兩欄留下，而「誰能蓋章」還要他再裁一次。
-- ⇒ **在他回覆之前這一欄原地不動**：拿掉跟留著一樣是在替他做決定。
--
-- ⚠️ 我沒有加 `reviewed_by` / `reviewed_ts`。v8 與 owner 的裁定說的是「旗標」，
-- 加上「誰審的、何時審的」是我自己想要的東西，不是被要求的 —— 射程外的不順手做。
-- **但我要照實說它的代價**：一個純旗標回答不了「這是誰蓋的章」。
-- 已查證：`lore_governance_event` 有 `actor_id` 欄，但它的五種 kind
-- （entity-approve／entity-merge／retire／revive／supersede）與五個呼叫點
-- **沒有一個**在設定這個旗標時觸發 ⇒ 誰蓋的章，站上記不下來。
-- 表本身收得下（`kind` 刻意不是 CHECK 列舉），要記只需要一個新的 kind 常數與一個
-- 呼叫點 —— 那要等上面那張卡回來、有人來寫 writer 的時候一起做。
ALTER TABLE lore_entry ADD COLUMN reviewed INTEGER NOT NULL DEFAULT 0
    CHECK (reviewed IN (0, 1));

-- ── 🔴 提案也要帶得動標題與星等，否則核可會寫出一份說謊的原文 ────────────────
--
-- owner 2026-09-05 於 `rc-bbccbeb3d9e6` 逐字：「**任何修改都是提案的一環**」。
-- 卡上問的只有「標題算不算提案的版本內容」；他的答案比那一格大 —— 條目上改得動
-- 的每一格，都要能由提案主張。
--
-- 🔴 而在這兩欄之前，那不只是「少了兩格」，它會產生一份**主動說謊的原文**。
-- 實測（重現測試，配陽性對照，2026-09-05）：
--   entry.Heading after accept = "開機脈絡在兩個地方各組了一次，兩份內容不一樣"
--   journal body after accept  = "heading:\n\n\ntrigger:\n…"     ← 標題是空的
-- 成因是三行接在一起：`loreRevisionBody` 印 heading；`loreProposalEntry` 回傳的
-- Heading 是零值（因為這張表沒有那一欄）；`ApplyLoreProposal` 把提案**存下來的**
-- 那串 body 原封不動寫進 lore_revision，而 UPDATE lore_entry 只動四格。
-- ⇒ 核可之後條目上的標題還在，原文層卻宣稱這條沒有標題 —— 而原文層存在的唯一
-- 理由，就是讓 agent 在不再相信壓縮版時回去看當初寫了什麼（本票硬條件 4）。
-- ⚠️ 陽性對照是這份證據的關鍵：同一支測試在提案之前先問一次，那時候原文層
-- **有**標題 ⇒ 不是量法看不到標題，是核可那一步把它換掉了。
--
-- ── 為什麼寫在這一支，而不是補進 00083（建 lore_proposal 的那一支）─────────
--
-- 🔴 Kyle（`c-5f576ae65f3d`）裁定並指名這個陷阱：`81/82/83` 在正式庫已經
-- `is_applied=1`（唯讀查證，配陽性／陰性對照）。goose **不會重跑一支已套用的
-- migration** ⇒ 把欄位補進 00083 等於什麼都沒發生，而且**沒有任何訊號**：本機
-- 的拋棄式 DB 會正確、正式庫會缺欄位、測試全綠。
-- ⇒ 新欄位一律走這一支的 ALTER TABLE，即使那看起來比較醜。
--
-- ── 為什麼不另開 00085 ──────────────────────────────────────────────────────
-- 同一個變更的兩半。Kyle 已把 `00085` 許給 T-79，而排序約束（T-33 先、T-79 後）
-- 兩案相同、不構成判準。⚠️ 那個排序是硬約束不是偏好：T-79 承包者實測
-- 「檔在、號比當前版本小、未套用 → exit 1，且 DB 停在原處、每次啟動都撞同一個
-- 錯、不會自己好」。
-- ⚠️ 改這一支會動 migration.lock 的中段，而 Kyle 的判準是「中段變動永遠不是
-- 誤報」—— 這裡不衝突：`TestMigrationLockGrowsOnlyAtItsTail` 比的是 origin/main
-- 的 lock 是不是本樹的**前綴**，而 00084 不在 main 上。

-- 空字串 = 這份提案沒有主張標題（既有的 27 份提案會落在這一格上，它們是在標題
-- 這一格存在之前送的）。「拒絕空標題」放在 DAL 不放在 CHECK，理由同上面標題那
-- 一段；而拒絕的時機有**兩個**：送出時的形狀檢查，以及**核可時**——後者是為了
-- 那 27 份：形狀檢查沒看過它們，核可它們會把條目上的標題清成空的。
ALTER TABLE lore_proposal ADD COLUMN heading TEXT NOT NULL DEFAULT '';

-- 刻度與上面 lore_entry.impact_stars **必須**相同：核可會把這個值寫進那一欄，
-- 兩邊合法區間不一樣的話，一份存得下的提案會在核可那一刻撞上另一張表的 CHECK，
-- 而失敗的位置離送出的人很遠。
-- ⚠️ 照實說的代價：0 既是「還沒判」，也是一份沒填這一格的提案會存下來的值 ⇒ 一份
-- 提案有可能把條目從 2 降回 0。**但那不是靜默的**：這一批同時把 impact_stars 印進
-- `loreRevisionBody` ⇒ 它進了 body、進了 sha256、也進了審核者眼前那份 diff。
-- 看得見的降級是一個主張，看不見的才是 bug。
ALTER TABLE lore_proposal ADD COLUMN impact_stars INTEGER NOT NULL DEFAULT 0
    CHECK (impact_stars BETWEEN 0 AND 3);

-- ── 🔴 拿掉「活動」這一個檢索軸：DROP TABLE lore_action ──────────────────────
--
-- owner 2026-09-05 逐字：「第一個欄位 subject 就這樣 不用爭辯了」「只有subject
-- 沒有 action因為後者太多可能性」。
--
-- 理由不是「用不到」，是**它從來不是索引**：`actions` 是開放集合（00081 自己
-- 寫著「every new kind of experience can mint a new action name」），寫的人各寫
-- 各的，而讀取端沒有任何一處拿它做收斂。一個不會收斂的軸，看起來像檢索條件、
-- 實際上是自由文字。
--
-- 🔴 這一支同時讓 00081 的三段話從此是**錯的**，不是舊的 —— 一樣不能就地改
-- （sha256 在 lock 中段），所以更正接續寫在這裡：
--   1.「TWO TABLES, NOT ONE TAG BAG … that is the T1/T2 distinction」
--      ⇒ T1/T2 分級一起拿掉了。機制是 `matched == askedAxes → T1`，只剩一個軸
--        之後 `matched < askedAxes` 永遠不成立 ⇒ T2 永遠不會出現，那個分級變成
--        一個永遠只有一個值的裝飾。
--   2.「AND THE TWO AXES ARE DIFFERENT SHAPES ON PURPOSE: … actions do NOT
--      [saturate]」⇒ 這句話是對的，而且正是拿掉它的理由。
--   3. 檔頭那段「`trust_scope` … is derived from the entry's actions at read
--      time by memoryTrustScope()」⇒ **那個推導的輸入沒有了**。memoryTrustScope
--      連同 trust_scope / trust_fell_back / unmapped_actions /
--      force_trust_analogy 一起拿掉：留著的話每一條的 trust_scope 都會是常數
--      "trust"、trust_fell_back 都會是常數 false —— 一個永遠不會分辨任何東西的
--      分類器，比沒有分類器危險。
--   ⚠️ 隨之消失的是**跨對象牆**（trust 類條目預設不跨到別的對象）。它掛在類比
--      層上，而類比層是 owner 裁掉的，所以它不是被繞過，是失去了它作用的那一層。
--      今天沒有別的東西在守這件事。
--
-- ⚠️ 破壞性且不可逆：Down 段只還原得了**表結構**，還原不了列。已寫進 lore_action
-- 的 (entry_id, action) 在 Up 之後就不存在了，goose down 會給你一張空表。射程內
-- 沒有真的資料（origin/main 上 lore 的檔案數是 0），所以代價是量得到的零 —— 但
-- 這句話只在合併前為真，讀到這裡的人請自己重新量一次。
DROP TABLE lore_action;

-- +goose Down
-- 🔴 只還原結構，不還原資料 —— 見上面那段。欄位宣告與索引與 00081 逐字相同，
-- 否則「升級過的站」與「down 過再 up 的站」會帶著不同的 schema。
CREATE TABLE lore_action (
    entry_id TEXT NOT NULL REFERENCES lore_entry(id),
    action   TEXT NOT NULL,
    PRIMARY KEY (entry_id, action)
);
CREATE INDEX idx_lore_action_action ON lore_action (action, entry_id);
ALTER TABLE lore_proposal DROP COLUMN impact_stars;
ALTER TABLE lore_proposal DROP COLUMN heading;
ALTER TABLE lore_entry DROP COLUMN reviewed;
ALTER TABLE lore_entry DROP COLUMN impact_stars;
ALTER TABLE lore_entry RENAME COLUMN impact TO problem;
ALTER TABLE lore_entry DROP COLUMN heading;
