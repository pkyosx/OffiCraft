-- +goose Up
-- T-33 — 規則 v8 的格式：標題獨立成格，`problem` 變成 `impact`，impact 帶星等與審核旗標。
--
-- owner 2026-09-05 逐字「用這個去做吧」，對象是規則 v8（ta-091e7a9cb434）。v8 的
-- 六格是：標題／1 對象×活動／2 內容（機制→實例→動作）／3 射程邊界／4 impact／
-- 5 相關事件。第 5 格已經是 lore_event 那張表，1～3 格已經是 trigger / content /
-- retire_when，所以這一支原本只動三件事：補上標題、把第 4 格改名、給第 4 格兩個欄位。
-- ⚠️「只動三件事」這句話後來被兩道裁定推翻，說在這裡是因為讀的人會拿它去數這一支
-- 到底改了什麼：rc-9002654dd81c（2026-09-06）把 `trigger` 併進 `heading`，
-- 而 owner 2026-09-06 逐字「retire_when -> revisit_when」又改了第 3 格的名字
-- （見檔尾那一段）。今天這一支動的是五件事，第 1 與第 3 格都在裡面。
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
-- 🔴 上限是 **140 個字元（Unicode rune，中文一個字算 1）**。owner 2026-09-05 逐字
-- 「我們標題規定 140 字元好了」。⚠️ 這一行以前寫的是「沒有長度上限」，那句話從
-- 這道裁定起是假的 —— 就地改掉而不是加註，是因為兩句相反的話擺在同一份檔案裡，
-- 讀的人只會挑一句信，而且不知道自己挑了。
-- 實測（2026-09-05）：站上 27 條舊格式原文最長 79 個 rune、24 條照 v8 重寫的標題
-- 最長 130 ⇒ 今天沒有任何一條會被擋，但最長那條離上限只剩 10 個 rune。
--
-- 🔴 上限**沒有**寫成 CHECK，而且理由跟下一行「拒絕空值」是同一個，不是新的：
-- SQLite 的 CHECK 只會回一句 "CHECK constraint failed"，說不出**是哪一格**、
-- 上限多少、送來的是多少 —— 而那三件事正是寫入者要拿來把標題砍短的全部依據。
-- 量法也是理由的一半：CHECK 只能用 length()，而它數的是 SQLite 自己的字元定義，
-- 不是 utf8.RuneCountInString；兩處各數各的，就會有一種標題一邊過一邊不過。
-- ⚠️ 還有一個代價不能不說：對一張**已經有資料的**表加 CHECK，SQLite 要重建整張
-- 表，而重建會拿既有列去過那道新約束 —— 一條今天存在的超長標題會讓 migration
-- 整支失敗。這道門是拒絕**新的**寫入，不是回頭改任何一列。
-- ⇒ 擋在 DAL（loreHeadingError / loreHeadingMaxRunes），寫入路徑與提案路徑
--   （送出時的形狀檢查 ＋ 核可時 ApplyLoreProposal）都會經過它。
--
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

-- ── 🔴 標題與第 1 格合併成一格：`trigger` 沒了，只留 `heading` ────────────────
--
-- 負責人 2026-09-06 於 `rc-9002654dd81c` 圈 [0] 逐字裁定：
--   「合併成 heading 一格（同時把搜尋改成掃 heading＋內容、待審畫面改顯示 heading）」
--
-- 理由不是「少一格比較乾淨」，是兩格的分工**已經壞掉而且在漂移**：
--   * 搜尋（dal_lore_search.go 的 loreEntryMatchesLiteral）只掃 `trigger`＋
--     `content`，`heading` 一個字都掃不到 ⇒ 使用者在列表上讀到的那一行，
--     搜尋搜不到，而「搜不到」跟「站上真的沒有這條」長得一模一樣。
--   * 待審對象畫面（api_lore_entity.go / dal_lore_entity.go）只顯示 `trigger`，
--     而搜尋結果兩格都回 ⇒ **同一條記憶在三個畫面上用三句不同的話代表自己**，
--     而審核者要做的判斷正是「這是不是我剛剛在列表上看到的那一條」。
-- 合併之後名字與檢索軸是同一格，那個落差在構造上消失，不是靠誰記得去對齊。
--
-- ── 🔴 為什麼是**就地改這一支**，不是新開 00085 ──────────────────────────────
--
-- 因為 00084 **還沒有被任何一個站台套用過**，所以「加了一欄又拿掉」這段歷史不需要
-- 存在，也沒有任何一個資料庫會因此對不上它跑過的東西。兩條獨立證據（Kyle 掃三台
-- 機器 25 顆 DB，2026-09-06，唯讀）：
--   1. 25 顆 DB 的 goose 版本最高是 **83**，沒有一顆到 84。
--   2. 對同一批 DB 直接查 `heading` 欄，**零命中** —— 這一支加的欄位一個都不存在。
-- 兩條分別回答「goose 說它跑過嗎」與「欄位真的在嗎」，所以不是同一個量法量兩次。
-- ⚠️ 這兩句話只在此刻為真。讀到這裡而準備再改這一支的人，**自己重新量一次**：
-- 一旦有任何站台套用過 84，就地改就會讓「升級過的站」與「全新安裝的站」帶著不同
-- 的 schema，而兩邊都會認為自己是對的（這正是這個檔頭上面那一大段在講的事）。
--
-- 🔴 第二次重量（2026-09-06，seth-m5 一台機器 31 顆 DB，全部 mode=ro 唯讀開啟），
-- 為了下面那一段 retire_when → revisit_when 的就地改：
--   1. goose 版本：站台庫最高仍是 **83**。
--   2. `lore_entry.heading` 欄：**31 顆全部零命中**。
--   陽性對照（沒有它，零命中不算數）：同一個查法在 9 顆 83 的庫上查得到
--   `lore_entry` 這張表、以及 `trigger` / `content` / `retire_when` 三欄各 1 命中，
--   `revisit_when` 0 命中 —— 量具會分辨，不是對什麼都回 0。
-- ⚠️ 有兩顆 DB 的 goose MAX 是 **85**，而它們**不是**反例，值得寫下來，因為下一個
--   人照著量會再撞到同一顆：它們在 `.../t79-impl/trash/.../binary-migrate/fresh.db`，
--   goose 表裡只有 {80, 85} 兩列、一張 lore 表都沒有。那個 85 是 **T-79 分支的
--   00085（交代單）**，跟這條分支的 00084 只是撞號。
-- 🔴 所以「MAX >= 84」這個量法本身會誤報 —— 它量的是號碼，不是這一支跑過沒有。
--   會把誤報擋下來的是第 2 條（欄位真的在嗎），這也正是這兩條當初被要求「不是同一個
--   量法量兩次」的用處：第一條紅了、第二條綠了的時候，去看第二條。
-- ⚠️ 這兩顆 DB 我沒有動、也沒有刪。
--
-- ── 🔴 下面那兩行 UPDATE 是**承重的**，不是保險 ────────────────────────────
--
-- 套用這一支之前，所有既有列的 `heading` 都是空字串（上面那兩支 ADD COLUMN 的
-- DEFAULT ''），這一條記憶的身分**整個活在 `trigger` 裡**。實測：試用站 63 列
-- `trigger` 全部非空、而 `heading` 欄根本還不存在。
-- ⇒ 少了那兩行 UPDATE，DROP COLUMN 之後會產生一批**沒有標題的條目**，而標題是
--    別人決定要不要把內容載進脈絡的唯一依據。
-- 🔴 而且**不會報錯**：欄位是 NOT NULL DEFAULT ''，空字串完全合法，migration 全綠、
--    測試全綠，症狀只會出現在幾個月後某個人打開列表看到一排空白的那一刻。
-- ⚠️ `WHERE heading = ''` 不是「小心一點」，它是在說：只有還沒有人寫過標題的列才
--    從 trigger 借。已經照 v8 寫好標題的列不會被 trigger 蓋掉。
UPDATE lore_entry    SET heading = trigger WHERE heading = '';
UPDATE lore_proposal SET heading = trigger WHERE heading = '';
ALTER TABLE lore_entry    DROP COLUMN trigger;
ALTER TABLE lore_proposal DROP COLUMN trigger;

-- ⚠️ 已經 checkout 過 `t-33/lore-format-v8` 這條分支、並在本機跑過 migrate 的人：
-- **你的本機 DB 要重建。** 你那顆庫的 goose 已經把 84 記成 is_applied=1，所以這一支
-- 改過的內容它不會再跑一次 —— 你會停在一顆「有 trigger 欄、heading 全空」的舊 84，
-- 而程式碼已經不認得那個形狀了。這不是一個會自己好的狀態，也不會有訊號告訴你。

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

-- ── 第 3 格改名：retire_when → revisit_when ─────────────────────────────────
-- owner 2026-09-06 逐字「retire_when -> revisit_when」（訊息 c-8fa8e792218d）。
-- 他只說了那六個字，所以下面這段語意是我寫的，不是他裁的 —— 可以被推翻。
--
-- 🔴 不是換個好聽的說法，是換一個問題。`retire_when` 問的是「什麼時候它會是錯的」，
-- 而那個問題幾乎沒有人答得出來：一條記憶失效的那一刻沒有訊號，等到有人發現它錯了，
-- 它已經被拿去用過很多次。`revisit_when` 問的是「什麼情況出現的時候，要把這一條拿
-- 出來**重新判一次**」—— 條件成立**不代表這一條已經失效**，只代表在下一次相信它
-- 之前得先看一眼。
-- ⚠️ 差別在誰承擔舉證：退役語意要寫的人預言終點，重判語意只要他指出一個看得見的
-- 觸發條件。後者答得出來，前者答不出來，而答不出來的那一格會被留白。
-- 🔴 留白的代價不是少一格資訊：這一條會一直被撈出來給人，而沒有任何人知道該在什麼
-- 時候回頭看它一眼 —— 包括它早就不成立之後。
--
-- ⚠️ 這一支同時讓 00081 與 00083 各一段話從此是**錯的**，不是舊的 —— 一樣不能就地
-- 改（sha256 在 migration.lock 的中段），所以更正接續寫在這裡：
--   1. 00081 的 lore_entry 欄位註解：
--      「🔴 `retire_when` 是自由文字，不是封閉值域，而且刻意沒有 CHECK。『什麼時候
--        不需要了』可能是『等 X 上線』『等某人回答』『這個 repo 不再用 goose』」
--      以及那一行行末的「-- 什麼時候不需要了（選填，自由文字）」
--      ⇒ 欄位名與問題都換了。**「自由文字、沒有 CHECK、任何列舉都會逼人挑一個最接近
--        的錯答案」這個理由完全沒有變，而且更站得住腳**：重判的觸發條件比退役的時點
--        還要開放。變的只有那一格在問什麼。上面舉的三個例子仍然是合法的值，只是現在
--        讀成「這三件事任何一件發生時回頭重判」，而不是「發生了就丟掉」。
--   2. 00083 的 lore_proposal 欄位註解：
--      「舊的 label / symptoms / short / falsify / instance / residual_risk 六格
--        換成 trigger / content / retire_when / problem 四格 + lore_event 一張表」
--      ⇒ 那四格今天一格都不叫那個名字了：`trigger` 併進 `heading`（rc-9002654dd81c）、
--        `problem` 改名 `impact`（本支上面那段）、`retire_when` 改名 `revisit_when`
--        （這一段）。那句話記的是一段歷史沿革，它描述的**過去**沒有錯，但拿它去對照
--        今天的 schema 會四格對不上三格。
--
-- 🔴 兩張表都要改。只改 lore_entry 不會報錯 —— lore_proposal 的欄位是照著
-- lore_entry 的形狀寫的，但沒有任何 FK 或約束把兩邊綁在一起，所以漏掉一張的症狀
-- 是「提案送得出去、核可時寫不進去」，而那要等到有人真的按下核可才會出現。
ALTER TABLE lore_entry    RENAME COLUMN retire_when TO revisit_when;
ALTER TABLE lore_proposal RENAME COLUMN retire_when TO revisit_when;

-- +goose Down
-- 🔴 逐項反面，順序是 Up 的逆序。
ALTER TABLE lore_proposal RENAME COLUMN revisit_when TO retire_when;
ALTER TABLE lore_entry    RENAME COLUMN revisit_when TO retire_when;

-- 🔴 只還原結構，不還原資料 —— 見上面那段。欄位宣告與索引與 00081 逐字相同，
-- 否則「升級過的站」與「down 過再 up 的站」會帶著不同的 schema。
CREATE TABLE lore_action (
    entry_id TEXT NOT NULL REFERENCES lore_entry(id),
    action   TEXT NOT NULL,
    PRIMARY KEY (entry_id, action)
);
CREATE INDEX idx_lore_action_action ON lore_action (action, entry_id);
-- 🔴 `trigger` 兩張表都要**先加回來、再把 heading 抄回去**，順序不能顛倒，而且這
-- 三步是 Up 那三步的逐項反面。欄位宣告與 00081／00083 逐字相同（TEXT NOT NULL
-- DEFAULT ''），否則「down 過再 up 的站」會帶著跟「一路升上來的站」不同的 schema。
-- ⚠️ 這裡還原得了的只有「一格變兩格」這個形狀，還原不了**兩格說的是兩句話**：
-- Up 把 trigger 併進 heading 是不可逆的，down 之後兩格會是同一串字。合併之前
-- heading 與 trigger 各自不同的那些列，down 救不回它們的差別。
ALTER TABLE lore_entry    ADD COLUMN trigger TEXT NOT NULL DEFAULT '';
ALTER TABLE lore_proposal ADD COLUMN trigger TEXT NOT NULL DEFAULT '';
UPDATE lore_entry    SET trigger = heading;
UPDATE lore_proposal SET trigger = heading;
ALTER TABLE lore_proposal DROP COLUMN impact_stars;
ALTER TABLE lore_proposal DROP COLUMN heading;
ALTER TABLE lore_entry DROP COLUMN reviewed;
ALTER TABLE lore_entry DROP COLUMN impact_stars;
ALTER TABLE lore_entry RENAME COLUMN impact TO problem;
ALTER TABLE lore_entry DROP COLUMN heading;
