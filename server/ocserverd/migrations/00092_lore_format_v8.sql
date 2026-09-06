-- +goose Up
-- T-33 — 規則 v8 的格式，收成 MVP：標題獨立成格，其餘四格拿掉。
--
-- owner 2026-09-05 逐字「用這個去做吧」，對象是規則 v8（ta-091e7a9cb434）。v8 的
-- 六格是：標題／1 對象×活動／2 內容（機制→實例→動作）／3 射程邊界／4 impact／
-- 5 相關事件。第 5 格已經是 lore_event 那張表，1～3 格已經是 trigger / content /
-- retire_when，所以這一支原本只動三件事：補上標題、把第 4 格改名、給第 4 格兩個欄位。
-- ⚠️「只動三件事」這句話後來被三道裁定推翻，說在這裡是因為讀的人會拿它去數這一支
-- 到底改了什麼：
--   1. rc-9002654dd81c（2026-09-06）把 `trigger` 併進 `heading`。
--   2. rc-9c9bf14a579f（2026-09-06）拿掉 `origin` 與「活動」那一軸。
--   3. 🔴 owner 2026-09-06 逐字「都改掉」（訊息 `c-3d3e5582c2d2`）：條目收成 MVP，
--      `impact`（原 `problem`）／`impact_stars`／`revisit_when`（原 `retire_when`）／
--      `supersedes` **四格連同程式一起拿掉**。這道裁定推翻了同一天稍早那句
--      「retire_when -> revisit_when」—— 改名的對象不存在了。
-- ⇒ 今天這一支留在條目上的格子是：`heading`／`content`／`lore_event`（第 5 格），
--   外加一個沒有被點名、因此留在原地的 `reviewed` 旗標（見它自己那一段）。
--
-- ── 為什麼是 ALTER，而 00089 當初是直接改欄位宣告 ─────────────────────────────
--
-- 00089 的檔頭寫著它可以直接改宣告，條件是「這個分支自己引入、還沒進 main、線上
-- 零資料」。那個條件到這一支已經不成立，而且失效的時點比「合併」更早一步：這個站
-- 發版是自動的（push 到 main ＋ CI 全綠就自己發），所以 T-33 一合併，00089／00091
-- 的欄位就必然會出現在真的資料庫裡。
--
-- 🔴 而那之後再去改宣告，不會報錯，只會讓「已經升級過的站」與「全新安裝的站」從
-- 此帶著不同的 schema，兩邊都認為自己是對的。ALTER 是唯一能讓兩種站走到同一個
-- 狀態的寫法。
--
-- ── 🔴 00089 裡有一段說明從這一支開始是假的，而我不能去改它 ──────────────────
--
-- 00089 的 lore_entry 註解裡寫著：
--   「`trigger` 兼任這條條目的標題……五格裡根本沒有『名字』這一格」
--   「⚠️『第一格兼任標題、因此拿掉 label 與 40 runes 上限』是實作判斷，不是負責人
--     的裁定。它被寫在這裡而不是默默做掉，就是為了讓下一個人看得見它可以被推翻。」
--
-- **v8 推翻了它**：標題是獨立的一格，而且 v8 對標題有 trigger 沒有的要求（寫「發生
-- 了什麼」、不得是祈使句、標題裡的名詞與數字都要在第 2 格找得到）。
--
-- 🔴 那段註解沒有被就地更正，是因為 migration.lock 對每一支 migration 的**檔案內容**
-- 做 sha256。改 00089 一個註解字元 ⇒ lock 中段那一行變動 ⇒ 那正是
-- migration_lock_t75_test.go 用來抓「一支已釋出的 migration 被編輯」的訊號。
-- ⇒ **更正寫在這裡，這一支就是那段話的接續。** 讀 00089 那段的人，請讀到這裡為止。
--
-- ⚠️ 判準要講準，我第一版寫寬了，Kyle（第 44 代）收窄的：
-- **判準是「這支 migration 的那一行在不在 lock 的中段」，不是「它有沒有進過 main」。**
-- 進 main 只是其中一個充分條件 —— 你在自己的工作樹裡改一支既有 migration，lock
-- 中段當場就動了，還沒有人合併任何東西。
-- 🔴 寫寬的那一版會害人：它會讓下一個人在自己工作樹裡動了 00089、跑完
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

-- ── 🔴 第 4 格整格拿掉：`problem` 不改名成 `impact`，直接 DROP ───────────────
--
-- owner 2026-09-06 逐字「都改掉」（訊息 `c-3d3e5582c2d2`）：傳承條目收成 MVP，
-- impact／impact_stars／revisit_when／supersedes 四格連同程式一起拿掉。
--
-- 🔴 這一支原本在這裡做的是 `RENAME COLUMN problem TO impact`，也就是把第 4 格
-- 改名。今天那一格不存在了，所以改名沒有對象 —— **留著改名再在別處 DROP，會讓
-- 這支 migration 先造出一個沒有人用得到的欄位名，讀的人得走完兩段才知道它死了。**
-- ⇒ 直接 DROP `problem`（00089 建的那一格），一步到位。
--
-- ── 為什麼 DROP COLUMN 在今天是安全的（跟下面 `origin` 那一段同一個理由）──────
--
-- `problem` 是 00081 建的，而 00081 **已經在正式站套用過**（goose 停在 83），
-- 一支已套用的 migration 不能就地改（sha256 在 migration.lock 中段）⇒ 這一格
-- 只能由這一支 ALTER 掉。
-- 🔴 DROP COLUMN 對**既有的列**不可逆：Down 段加得回欄位，加不回值。正式站
-- `lore_entry` 今天 0 列（見下面 `origin` 那一段的量測與陽性對照）⇒ 一個值都不會掉。
-- ⚠️ 試用站有資料，那不是同一顆庫；讀到這裡準備再動這一支的人，**自己重新量一次**。
--
-- ⚠️ 兩張表都要動。lore_proposal 的那一格也叫 `problem`（00091 建的，這一支從來
-- 沒有替它改過名），漏掉它不會報錯 —— 症狀是提案表上留著一格沒有任何程式讀寫的
-- 死欄位，而它看起來跟活的一模一樣。
ALTER TABLE lore_entry    DROP COLUMN problem;
ALTER TABLE lore_proposal DROP COLUMN problem;

-- ── 🔴 星等（impact_stars）這一格沒有被建立，而不是建了再刪 ──────────────────
--
-- 這一支原本在這裡對 lore_entry、在下面對 lore_proposal 各 ADD 一個
-- `impact_stars INTEGER NOT NULL DEFAULT 0 CHECK (impact_stars BETWEEN 0 AND 3)`，
-- 刻度是 owner 2026-09-05 的三句（做白工／弄壞你動的那個／弄壞你沒動的）。
-- owner 2026-09-06「都改掉」把這一格收掉了。
--
-- 🔴 這裡是**刪掉那兩行 ADD**，不是加兩行 DROP。判準是「這一格今天存不存在於任何
-- 一顆真的資料庫裡」：`impact_stars` 只由這一支引入，而這一支一次都還沒被套用過
-- （見下面 heading 那一段的兩次量測：25 顆＋31 顆 DB，goose 最高 83、`heading` 欄
-- 零命中、附陽性對照）⇒ 沒有任何一顆庫有這一欄，DROP 它會直接失敗。
-- ⚠️ 這跟 `problem` / `retire_when` / `supersedes` 不同，那三格是 00089／00091 建
-- 的、已經在真的庫裡，所以那三格走 DROP COLUMN。同一份委託裡兩種做法，判準是
-- 「這一格是誰建的」，不是「它要不要消失」。

-- ── 審核旗標 ────────────────────────────────────────────────────────────────
-- 🔴 這一欄的需求出處，以及 owner 對它的裁定 —— 讀之前先看這段。
--
-- 規則 v8（ta-091e7a9cb434）**整份文件裡沒有「審核」「admin」「排序」「蓋章」
-- 這幾個字**（grep 過，五個關鍵字全部 0 命中）。「審核與星等是兩欄／審核旗標只有
-- owner＋admin／排序是審核過的在前」這三句的出處是**我們自己寫給 owner 的一則
-- 訊息**（`c-8e647bf72e80`，from = ow-e27260b9ed05），不是他的裁定。它被寫進交接
-- 時標成了「v8 定案」，我照著它把「只有 owner 與 admin 能動」寫成事實 —— 那句話
-- 現在拿掉了，因為它宣稱了一個沒有人做過的決定。
--
-- ✅ 已裁定：`rc-37f10fec50d1`，owner 2026-09-05 20:53 自由文字（沒圈選項），逐字：
--    「Same number but 1~3 is our final decision with a flag indicating if human
--     confirmation is done」
-- ⇒ 星等與他八月底講的分數是**同一個數字**，刻度定為 **1～3**（他更早在
--   `rc-ccd8ef9517fb` 講的「零分到五分」以此為準被取代），另外**再加一個旗標**表示
--   有沒有人確認過 ⇒ **這裡的兩欄結構就是他裁的結構，不要改。**
-- ⇒ 0 仍然不是一個星等，是「還沒判」（見上面 4b）。壓成一欄會讓「沒有人審過」跟
--   「審過但影響輕微」變成同一個 0，而他要的是分得開。
--
-- 🔴 他這次**沒有**回答的：**誰按得動這個旗標**。原本寫「只有 owner 與 admin」的
-- 那句話是我們自己寫的，至今沒有人裁過 ⇒ 要開放寫入路徑之前必須先問他一次。
--
-- ⚠️ 我沒有加 `reviewed_by` / `reviewed_ts`。v8 與 owner 的裁定說的是「旗標」，
-- 加上「誰審的、何時審的」是我自己想要的東西，不是被要求的 —— 射程外的不順手做。
-- **但我要照實說它的代價**：一個純旗標回答不了「這是誰蓋的章」。
-- 已查證：`lore_governance_event` 有 `actor_id` 欄，但它的五種 kind
-- （entity-approve／entity-merge／retire／revive／supersede）與五個呼叫點
-- **沒有一個**在設定這個旗標時觸發 ⇒ 誰蓋的章，站上記不下來。
-- 表本身收得下（`kind` 刻意不是 CHECK 列舉），要記只需要一個新的 kind 常數與一個
-- 呼叫點 —— 那要等「誰按得動」那一題有答案、有人來寫 writer 的時候一起做。
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
-- ── 為什麼寫在這一支，而不是補進 00091（建 lore_proposal 的那一支）─────────
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
-- 的 lock 是不是本樹的**前綴**，而 00092 不在 main 上。

-- 空字串 = 這份提案沒有主張標題（既有的 27 份提案會落在這一格上，它們是在標題
-- 這一格存在之前送的）。「拒絕空標題」放在 DAL 不放在 CHECK，理由同上面標題那
-- 一段；而拒絕的時機有**兩個**：送出時的形狀檢查，以及**核可時**——後者是為了
-- 那 27 份：形狀檢查沒看過它們，核可它們會把條目上的標題清成空的。
ALTER TABLE lore_proposal ADD COLUMN heading TEXT NOT NULL DEFAULT '';

-- 🔴 提案表上的 `impact_stars` 同樣**沒有被建立**（原本這裡有一行 ADD COLUMN，
-- 刻度與 lore_entry 的那一格必須相同）。理由見上面星等那一段：這一格只由這一支
-- 引入，而這一支還沒被任何站台套用過 ⇒ 刪掉 ADD，不是加一行 DROP。
-- ⚠️ 提案表因此只帶得動 `heading` 與 `content` 兩格，跟條目上剩下的可寫格數一致；
-- 兩邊格數對不上正是上面那段「核可會寫出一份說謊的原文」講的病。

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
-- 理由不是「用不到」，是**它從來不是索引**：`actions` 是開放集合（00089 自己
-- 寫著「every new kind of experience can mint a new action name」），寫的人各寫
-- 各的，而讀取端沒有任何一處拿它做收斂。一個不會收斂的軸，看起來像檢索條件、
-- 實際上是自由文字。
--
-- 🔴 這一支同時讓 00089 的三段話從此是**錯的**，不是舊的 —— 一樣不能就地改
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

-- ── 🔴 拿掉 `origin`，連同它撐起的三個人工優先權機制 ────────────────────────
--
-- 負責人 2026-09-06 於 `rc-9c9bf14a579f` 圈 A 逐字：
--   「A｜對，一起拿掉。我知道我說過的話從此跟其他條目一起搶額度」
--
-- 🔴 這不是刪一個欄位，是刪一條規則。`origin`（`human:Seth` / `agent:Kyle`）的
-- 全部功能就是「人說的話優先」，而它撐著三個機制，三個都跟著走：
--   1. lore_fold.go `loreSubjectsWithinCaps` —— 掛著 `human:` 條目的對象**永不被
--      截斷**（硬保留，不是加權）。
--   2. dal_lore_search.go `sortLoreHits` —— `human:` 條目在同層裡排前面。
--   3. dal_lore_search.go `loreHitsWithinLimit` —— `human:` 條目**不受 limit 管**。
-- 🔴 留著欄位而只停用規則，或留著規則而讓它讀一個永遠不存在的值，都會產生一個
-- **恆假的守衛**：函式照跑、分支永遠進不去，讀的人看到的是一道還在的門。所以三支
-- 釘著它們的測試也一起刪掉，而不是改成斷言新行為 —— 那三支測的是這條規則本身。
--
-- ⚠️ 照實說代價，因為沒有任何東西接手：**負責人自己說過的話，從今天起跟其他條目
-- 一樣會被截斷、會被排在後面、會被 limit 砍掉**，而截斷是靜默的（掉了的對象只剩一
-- 個 omitted 數字）。他知道，這正是他圈 A 的那句話。
--
-- ── 為什麼是 DROP COLUMN，而且為什麼今天做不會掉資料 ────────────────────────
--
-- `origin` 是 00081 建的，而 00081 **已經在正式站套用過**（goose 停在 83）。
-- 一支已套用的 migration 不能就地改（sha256 在 migration.lock 中段），所以這一格
-- 只能由這一支 ALTER 掉。
-- 🔴 DROP COLUMN 對**既有的列**不可逆：Down 段加得回欄位，加不回值。
-- 量過（負責人第一手，正式站 DB 唯讀，2026-09-06）：正式站 `lore_entry` **0 列**、
-- `entity` 0 列、`lore_recall_log` 0 列 ⇒ 今天一個值都不會掉。
-- 陽性對照（沒有它，零列不算數）：同一顆 DB `member` 390 列、`chat_message`
-- 57,419 列 ⇒ 量具會數得到東西，不是對什麼都回 0。
-- ⚠️ 這句話只在此刻、只對**正式站**為真。試用站有資料，那不是同一顆庫；而讀到這
-- 裡準備再動這一支的人，**自己重新量一次**，不要沿用上面這兩個數字。
--
-- ⚠️ 只有 lore_entry 有這一欄，所以這裡只動一張表，不是漏了一張。查過兩處：
--   * lore_proposal（00091 建的）—— 整支 00091 對 `origin` **零命中**，從來沒有
--     這一格；提案帶不動它，所以核可路徑也不需要跟著改。
--   * lore_meta（00089:230）—— 檔頭那段「🔴 NO `origin` COLUMN HERE」指的是
--     **這張 L2 表**，不是 lore_proposal。它說的是「兩層各存一份會變成同一件事
--     的兩個真相」，所以 origin 當初只留在 L1。今天 L1 那一份也走了。
--
-- ── 🔴 這一支讓 00089 的三段話從此是**假的**，不是舊的 ──────────────────────
--
-- 一樣不能就地改（sha256 在 migration.lock 中段），所以更正接續寫在這裡。讀 00089
-- 那三段的人，請讀到這裡為止：
--   1. 檔頭：「🔴 `origin` LIVES ON L1, NOT ON L2 … the ranking rule makes a human
--      origin a hard axis: those entries sort ahead within their tier and survive
--      the count cap.」
--      ⇒ **那條 ranking rule 沒有了**，連同它要 SELECT 的那一欄。L1／L2 的分界本身
--        仍然成立（L2 的定義就是組裝器看不到它），只是今天沒有任何欄位靠它撐排序。
--   2. entity_type 那一段：「Subjects and `origin` are the same shape … validated
--      against the same rows, read at run time (loreOriginError in dal_lore.go)」
--      ⇒ **`loreOriginError` 這個函式不存在了。** 那一段話的**主張**仍然為真而且
--        仍然重要——型別前綴的值域只有 entity_type 這一份、在執行期讀表、Go 裡不准
--        再抄一份——只是今天執行它的是 subject 與事件 人／地／物 的檢查
--        （dal_lore_write.go 的 loreSubjectTypeAndName／dal_lore.go 的
--        loreEventError），不再是 origin。
--      ⚠️ 那條「一份清單」的性質在測試上原本只由 origin 那一支釘著
--        （TestLoreOriginTypesComeFromTheEntityTypeTable：核准一個新型別、要求寫入
--        開始通過）。它**被移植**成 TestLoreEventKeyTypesComeFromTheEntityTypeTable，
--        不是跟著欄位一起刪掉——其餘的前綴測試只檢查「未核准會被拒」，那種測試對著
--        一份 Go 硬編清單也會過，擋不住這件事。
--   3. lore_entry 的欄位註解：「🔴 `origin` IS A SUBJECT KEY, NOT AN ENUM …
--      the value set is therefore OPEN …」⇒ 那一欄沒有了，整段沒有對象。
ALTER TABLE lore_entry DROP COLUMN origin;

-- ── 🔴 拿掉 `supersedes`（00089 建的那一格）─────────────────────────────────
--
-- owner 2026-09-06「都改掉」（`c-3d3e5582c2d2`）點名的四格之一。
-- 它存的是「這一條取代了哪一條」的條目 id，讀取端把它渲染成條目卡上的一行。
--
-- 🔴 DROP COLUMN 的安全性理由與上面 `origin` / `problem` 同一份量測：正式站
-- `lore_entry` 0 列（附陽性對照 member 390 列／chat_message 57,419 列），所以今天
-- 一個值都不會掉；而這一格是 00081 建的、00081 已套用，就地改它不合法 ⇒ 只能 ALTER。
--
-- ⚠️ 只有 lore_entry 有這一欄。lore_proposal（00091）對 `supersedes` 零命中，
-- 提案從來帶不動它，所以核可路徑不需要跟著改（陽性對照：同一個查法在 00091 上找得到
-- `retire_when` 與 `problem` 各 1 命中）。
--
-- ⚠️ 照實說**沒有跟著走的東西**，因為 owner 沒有點名它們，我不會順手清掉：
--   * `lore_entry.status` 的 CHECK 仍然收 `'superseded'`。今天沒有任何欄位記得下
--     「被誰取代」，所以那個狀態值從此只說得出「它退場了」，說不出接班的是誰。
--   * `lore_governance_event` 的 `kind` 仍然收 `'supersede'`（那一欄刻意不是 CHECK
--     列舉），治理事件層還記得動作，只是條目層不再指得回去。
ALTER TABLE lore_entry DROP COLUMN supersedes;

-- ── 🔴 第 3 格整格拿掉：`retire_when` 不改名成 `revisit_when`，直接 DROP ──────
--
-- 這一段原本做的是改名。owner 2026-09-06 逐字「retire_when -> revisit_when」
-- （訊息 `c-8fa8e792218d`）給了新名字，而**同一天**他又逐字「都改掉」
-- （`c-3d3e5582c2d2`）把這一格連同其他三格收進 MVP 之外 ⇒ 後者是較晚的裁定，
-- 這一格今天不存在，改名沒有對象。
--
-- 🔴 所以這裡是 DROP 而不是 RENAME：留著改名再在別處刪，會讓這支 migration 先造
-- 出一個 `revisit_when` 欄位名，而站上沒有任何一行程式讀得到它。
--
-- ⚠️ 照實說代價（跟改名那一版要說的是同一件事，只是更重）：`retire_when` 問的是
-- 「什麼時候它會是錯的」，`revisit_when` 本來要換成「什麼情況出現時回頭重判一次」。
-- 兩個問題今天都沒有欄位在問 ⇒ **一條條目會一直被撈出來給人，而沒有任何人知道該在
-- 什麼時候回頭看它一眼，包括它早就不成立之後。** 這是 MVP 買到的東西的價錢。
--
-- ⚠️ 這一支同時讓 00089 與 00091 各一段話從此是**錯的**，不是舊的 —— 一樣不能就地
-- 改（sha256 在 migration.lock 的中段），所以更正接續寫在這裡：
--   1. 00089 的 lore_entry 欄位註解：
--      「🔴 `retire_when` 是自由文字，不是封閉值域，而且刻意沒有 CHECK。『什麼時候
--        不需要了』可能是『等 X 上線』『等某人回答』『這個 repo 不再用 goose』」
--      以及那一行行末的「-- 什麼時候不需要了（選填，自由文字）」
--      ⇒ 欄位名與問題都換了。**「自由文字、沒有 CHECK、任何列舉都會逼人挑一個最接近
--        的錯答案」這個理由完全沒有變，而且更站得住腳**：重判的觸發條件比退役的時點
--        還要開放。變的只有那一格在問什麼。上面舉的三個例子仍然是合法的值，只是現在
--        讀成「這三件事任何一件發生時回頭重判」，而不是「發生了就丟掉」。
--   2. 00091 的 lore_proposal 欄位註解：
--      「舊的 label / symptoms / short / falsify / instance / residual_risk 六格
--        換成 trigger / content / retire_when / problem 四格 + lore_event 一張表」
--      ⇒ 那四格今天**只剩一格**：`trigger` 併進 `heading`（rc-9002654dd81c），
--        `retire_when` 與 `problem` 兩格整個沒有了（owner 2026-09-06「都改掉」），
--        剩下的是 `content`。那句話記的是一段歷史沿革，它描述的**過去**沒有錯，
--        但拿它去對照今天的 schema 會四格對不上一格。
--
-- 🔴 兩張表都要改。只改 lore_entry 不會報錯 —— lore_proposal 的欄位是照著
-- lore_entry 的形狀寫的，但沒有任何 FK 或約束把兩邊綁在一起，所以漏掉一張的症狀
-- 是「提案送得出去、核可時寫不進去」，而那要等到有人真的按下核可才會出現。
ALTER TABLE lore_entry    DROP COLUMN retire_when;
ALTER TABLE lore_proposal DROP COLUMN retire_when;

-- +goose Down
-- 🔴 逐項反面，順序是 Up 的逆序。
-- 🔴 這一段還原得了的只有**結構**，還原不了值：Up 段對 `problem` / `retire_when` /
-- `supersedes` / `origin` 做的是 DROP COLUMN，down 之後那四格都是空字串。欄位宣告
-- 與 00089／00091 逐字相同（TEXT NOT NULL DEFAULT ''），否則「升級過的站」與
-- 「down 過再 up 的站」會帶著不同的 schema，而兩邊都會認為自己是對的。
ALTER TABLE lore_proposal ADD COLUMN retire_when TEXT NOT NULL DEFAULT '';
ALTER TABLE lore_entry    ADD COLUMN retire_when TEXT NOT NULL DEFAULT '';

ALTER TABLE lore_entry ADD COLUMN supersedes TEXT NOT NULL DEFAULT '';

-- 🔴 只還原結構，還原不了值 —— 見 Up 段那一大段。欄位宣告與 00089 逐字相同
-- （TEXT NOT NULL DEFAULT ''），否則「升級過的站」與「down 過再 up 的站」會帶著
-- 不同的 schema。down 之後每一列的 origin 都是空字串，而空字串正是 00089 那道
-- 驗證（現已一併移除）當初會拒絕的值。
ALTER TABLE lore_entry ADD COLUMN origin TEXT NOT NULL DEFAULT '';

-- 🔴 只還原結構，不還原資料 —— 見上面那段。欄位宣告與索引與 00089 逐字相同，
-- 否則「升級過的站」與「down 過再 up 的站」會帶著不同的 schema。
CREATE TABLE lore_action (
    entry_id TEXT NOT NULL REFERENCES lore_entry(id),
    action   TEXT NOT NULL,
    PRIMARY KEY (entry_id, action)
);
CREATE INDEX idx_lore_action_action ON lore_action (action, entry_id);
-- 🔴 `trigger` 兩張表都要**先加回來、再把 heading 抄回去**，順序不能顛倒，而且這
-- 三步是 Up 那三步的逐項反面。欄位宣告與 00089／00091 逐字相同（TEXT NOT NULL
-- DEFAULT ''），否則「down 過再 up 的站」會帶著跟「一路升上來的站」不同的 schema。
-- ⚠️ 這裡還原得了的只有「一格變兩格」這個形狀，還原不了**兩格說的是兩句話**：
-- Up 把 trigger 併進 heading 是不可逆的，down 之後兩格會是同一串字。合併之前
-- heading 與 trigger 各自不同的那些列，down 救不回它們的差別。
ALTER TABLE lore_entry    ADD COLUMN trigger TEXT NOT NULL DEFAULT '';
ALTER TABLE lore_proposal ADD COLUMN trigger TEXT NOT NULL DEFAULT '';
UPDATE lore_entry    SET trigger = heading;
UPDATE lore_proposal SET trigger = heading;
-- 🔴 `impact_stars` 在 Up 段裡是**沒有被建立**，不是被刪掉（見 Up 段星等那兩段），
-- 所以這裡沒有它的反面。多寫一行 DROP COLUMN impact_stars 會讓 down 整支失敗。
ALTER TABLE lore_proposal ADD COLUMN problem TEXT NOT NULL DEFAULT '';
ALTER TABLE lore_proposal DROP COLUMN heading;
-- ⚠️ `reviewed` 留在 Up 段裡（owner 沒有點名它），所以它的反面也留著。
-- 它今天是一個**沒有東西可以蓋章的旗標**：它存在的唯一理由是替 impact_stars 蓋章，
-- 而星等那一格已經不存在了。留著是因為沒有人裁過要拿掉它，不是因為它還有用。
ALTER TABLE lore_entry DROP COLUMN reviewed;
ALTER TABLE lore_entry ADD COLUMN problem TEXT NOT NULL DEFAULT '';
ALTER TABLE lore_entry DROP COLUMN heading;
