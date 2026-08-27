# T-f014 mutant 驗證紀錄

**為什麼這份要落檔**：前一次獨立審查回來時，某包宣稱的 mutant 驗證在 repo 裡找不到任何紀錄，
審查者只好自己重跑一遍。**沒落檔等於不存在。** 下面每一列都可以被獨立重放。

同源方法論見 `docs/design/worker-panel-parity-mutants.md`（T-7526）。

## 方法

1. 把要驗的檔案複製一份到 scratchpad 當備份，`shasum -a 256` 記下雜湊。
2. 施加 mutant（單一、明確、可讀的一處改動 —— 描述欄寫的就是那一處）。
3. 跑該範圍的測試／守衛，記下**哪幾條紅、以及紅的訊息說的是不是我打的那個目標**。
4. **從 scratchpad 備份 `cp` 回來**（🔴 不准 `git checkout --`），
   再 `shasum -a 256 -c` 驗還原後逐位元組相同。

全部 12 支跑完，五個被動過的檔的還原檢查都是 `OK`。

🔴 **每一列的「紅了哪條」都附了斷言訊息**，不是只寫「失敗了」。
理由：只斷言「有東西壞掉」的守衛，在因為無關原因壞掉時照樣給綠燈 —— 這個病這個 repo 犯過兩次。
M1 的第一版正是這個坑：它紅的是 `TypeError: Cannot read properties of null`，
那是「元素不見了」和「元素指錯 bytes」共用的同一種爆法。
測試改成具名的 `expect(image, "…").not.toBeNull()` 之後，紅的訊息才真的指認目標。

## 第一批：共用彈窗吃下 staged 圖片（`MarkdownPreviewOverlay.tsx`）

| # | Mutant（改了什麼） | 紅了哪條（斷言訊息） |
|---|---|---|
| M1 | `image` 判定不看 `imageSrc`（staged bytes 掉進文字分支） | `previews a staged image…` → `the staged bytes must render through the image branch: expected null not to be null`；`opens a staged composer image…` → `the shared overlay must render the staged bytes as an image` |
| M2 | 分享鈕的閘放寬成 `attachmentId \|\| imageSrc`（替還沒上傳的 bytes 造一條分享連結） | 同兩條 → `expected <button …> to be null` |
| M3 | `downloadHref` 丟掉 staged 的 `data:` URI | `previews a staged image…` → `staged bytes are a real file — the download stays: expected null not to be null` |
| M4 | 縮放群組的 label 退回寫死英文 `"image zoom controls"` | `renders an image in the shared header shell…` → `Unable to find an accessible element with the role "group" and name "縮放圖片"` |
| M5 | 放大鈕拿掉 `aria-label` | 同上 → `Unable to find an accessible element with the role "button" and name "放大"` |
| M6 | 圖片 `alt` 退回泛用字串、不用檔名 | 同上 → `expected '聊天圖片' to be 'shot.png'` |

M2 是**防過度修正的哨兵**：M3 證明「staged 要有下載」，M2 證明「staged 不准有分享」。
兩半都承重 —— 只留一半的話，把兩顆鈕一起開或一起關都能矇混過關。

## 第二批：樣式所有權（`styleOwnership.test.ts`）

這一批對應本票最大的風險：`.md-preview*` 原本坐在 office.css 中段，
而畫它的 `MarkdownPreviewOverlay.tsx` **自己沒有 import 那張表**——
它是靠 OfficePage / RepliesPage / TasksPage 的 transitive import 搭便車。
T-7526 今晚才因為同一類事故壞掉過沒被碰到的畫面，而 jsdom 不算 CSS、`tsc` 看不出
class 字串與 stylesheet 的關係 ⇒ 所有自動檢查都會是綠的。

| # | Mutant | 紅了哪條 |
|---|---|---|
| M7 | `MarkdownPreviewOverlay.tsx` 拿掉 `import "./md-preview.css"` | `every component using .md-preview__* imports ./md-preview.css` |
| M8 | 在 office.css 塞回一條 `.md-preview__panel` 規則（便車復活） | `.md-preview__* rules live in md-preview.css and nowhere else` |
| M9 | `OWNED_SHEETS` 加一個沒人畫的 block（vacuous-green 哨兵） | `every component using .nobody-uses-this__* imports ./nobody-uses-this.css`（`users.length > 0` 斷言） |

M9 是**守衛自己的守衛**：把 block 改名、或把元件刪掉，迴圈就會掃到空集合並「通過」。
`expect(users.length).toBeGreaterThan(0)` 讓「沒東西可檢查」變成紅的，而不是綠的。
M8 釘的是另一半：所有權只有在 block **只有一個家**時才成立；規則若散在兩張表，
元件自己 import 也不夠，拿掉它會只壞一半、看起來像沒壞。

## 第三批：舊看圖層退役守衛（`bin/tests/lightbox-retired-guard.sh`）

| # | Mutant | 守衛 rc | 紅了哪條 |
|---|---|---|---|
| M10 | 把 `<Lightbox … />` 種回 `ChatArea.tsx` | 1 | `<Lightbox is back in production source — use frontend/src/components/MarkdownPreviewOverlay.tsx instead:` + 命中的 `path:line` |
| M11 | 把 `.chat__lightbox { … }` 種回 office.css | 1 | `.chat__lightbox styling is back — that block was deleted with the component:` + 命中的 `path:line` |
| M12 | 把 `FE` 指到不存在的 `frontendz`（vacuous-green 哨兵） | 1 | `frontend/src/components is missing — every scan below would be a vacuous pass` + `scan corpus is only 0 source file(s)` |
| M13 | 語料改回 `find` ＋ 目錄名 deny-list（T-1a7d 之前的樣子） | 1 | `the corpus reached into build output …` ＋ `the component scan reported a violation from build output — this is the false red T-1a7d fixed, come back` |
| M14 | 語料縮回 `git ls-files --cached`（拿掉 `--others --exclude-standard`） | 1 | `reach: the untracked src/components/SneakyPreview.tsx was NOT reported — the corpus has shrunk to staged work only` |
| M15 | 把整棵樹從 git 倉庫剝離（`tar` 掉 `.git`，模擬 tarball 部署） | 1 | `scan corpus is only 0 source file(s) …` ＋ 下游掃描斷言改報 `this is a vacuous pass, not a clean tree`（不再印 `ok`） |
| M16 | 把語料範圍改回整個 `frontend/`（拿掉 `SCAN_ROOTS` 收斂），對照樹裡的 `recon-out/` 誘餌就會進語料 | 1 | `the class scan reported a rule from recon-out/, which is neither committed nor ignored — this is the T-1a7d false red for the THIRD time, in the same shape wearing different clothes` |
| M17 | 從 `SCAN_ROOTS` 拿掉 `visual-guards` | 1 | `these TRACKED source files are outside every scanned root …` 並逐一列名 |
| M18 | 只改 coverage 檢查**自己的基準**一個 token（`all_tracked_sources` 的 `'*.ts' …` → `'*.tsz'`） | 1 | `the coverage check's own baseline is only 0 tracked file(s) — it is comparing against nothing …` ＋ `positive control: the coverage check did NOT name the tracked out-of-root lib/helper.ts` |

M12 是本守衛最重要的一支：grep 打錯路徑會回 0 行，而 0 行正是「通過」的長相。
守衛自己的 corpus 檢查（目錄存在 + **來源檔數** ≥ 100 + 倖存的彈窗檔還在）讓那種綠變紅。
⚠️ 是「**來源檔**」不是「受版控檔」：語料是 tracked **加上** untracked-未被 ignore 的檔（見下面 `--others` 那段），
這份文件在別處就是這樣論證的，寫成「受版控」會和它自己打架。

守衛內建的正負對照（每次執行都跑，不需人工施加 mutant）：
在 `mktemp` 出來的**假 git 倉庫**裡種一個 `<Lightbox>` 與一條 `.chat__lightbox` 規則並 `git add`，
斷言掃描回報的是**那個 path:line**（不是「有東西失敗」）；
再把兩個違規刪掉，斷言乾淨的樹**什麼都不報**。
少了負向那半，一個「見檔就報」的壞掃描也能滿足兩條正向對照。

### T-1a7d 第三輪（2026-08-27，第二次審查後）：🔴 語料改成「**正向的來源根**」

前兩版都是**指名要排除什麼**，而兩次都被繞過：

| 版本 | 排除方式 | 被什麼繞過 |
|---|---|---|
| base | `find … -name dist -prune` | `dist-paint-guard/`（`-name dist` 不匹配） |
| 第二版 | `--others --exclude-standard` | `frontend/recon-out/`（`.gitignore` **刻意**只蓋 `recon-out/*.png`） |

🔴 **第二版把 T-1a7d 要修的那個假紅一字不差帶回了宣稱修好它的那顆 commit 上**：
在 `frontend/recon-out/` 放一顆過期 bundle ⇒ `.chat__lightbox styling is back … recon-out/index-STALE.css:1`。
**「未被 ignore」本身就是一個 deny-list，只是寫在另一個檔案裡。**
（而同一次執行裡，「語料不含建置產物」那條還印 `ok` —— 因為它的 regex 用 `out` 當完整路徑元件，`recon-out/` 不匹配。**`-name dist` 蓋不到 `dist-paint-guard`，換成 `out` 蓋不到 `recon-out`；同一個形狀換了件衣服。**）

**第三版反過來講**：`SCAN_ROOTS=(src visual-guards paint-guards scripts playwright)` 加上頂層設定檔，語料仍是 `--cached --others --exclude-standard`。
`recon-out/`、`vite-out/`、以及**未來任何還沒被發明的產物目錄**，都因為**從來沒被包含**而被排除 —— 這是唯一一種不需要維護的排除。

⚠️ **allow-list 會不會悄悄過期？** 會，所以配一條 **coverage 斷言**：
**每一個 tracked 的來源檔都必須落在某個 root 底下**。tracked ＝ 有人 commit 過 ⇒ 落在外面的只可能是「新的來源目錄沒加進來」（紅會把它逐一列名）或「不該被 commit 的東西」。
**產物永遠不會是 tracked，所以永遠踩不到這條。** ⇒ **這份清單由「紅」維護，不是由記性維護。**
🔴 **下次再有產物漏進來，正解仍然不是往 regex 補名字 —— 那會是第三次犯同一個錯。**

### ⚠️ 跑這支守衛要用 `/bin/bash`，不是 PATH 上的 `bash`

`bin-guards` 在 CI 是用 macOS 內建的 **`/bin/bash` 3.2.57** 跑的；開發機 PATH 上通常是 Homebrew bash 5。
兩者在 `set -u` 下對**空陣列展開**的行為不同 —— **3.2 會報 `unbound variable`，4+ 給空清單**。
本守衛因此在本機 17 ok / CI `bin-guards` 同一支 `extra[@]: unbound variable` 炸六次。
可攜寫法是 `${a[@]+"${a[@]}"}`。**改完這個檔案，請用 `/bin/bash bin/tests/lightbox-retired-guard.sh` 再驗一次。**

### T-1a7d（2026-08-27）：語料從 `find` 換成 `git ls-files`

原本的掃法用 `find` 走整棵樹，再用**目錄名**排除 `node_modules`／`dist`／`.git`。
那是 deny-list，而 deny-list 對「這是不是人寫的檔」是錯的形狀：
🔴 `-name dist` **不匹配 `dist-paint-guard`**，`playwright/.cache` 從頭就不在清單上。
量過（2026-08-27，一個已建置的工作副本的 `frontend/`）：舊掃法讀 497 檔，`git ls-files` 讀 480，
多出來的 17 個全是 Vite 產出的 bundle。bundle 裡有 source 當時的內容，
所以一條幾個月前就從 `office.css` 退休的規則，仍會活在某人筆電上的舊 bundle 裡 —— 守衛就報「它回來了」。
**這是結構上只發生在本機的假紅**：CI 每個 job 都是全新 checkout，`bin-guards` 那格從不跑 Vite。
正因為只在有人站在機器前的時候浪費時間，它特別能訓練大家「紅了就重跑」。

改法不是再往 deny-list 加名字，而是把語料換成 **git 認為有人要負責的檔**
（`git ls-files --cached --others --exclude-standard`）。

⚠️ **`--others --exclude-standard` 那半是審查時補的，不是裝飾。** 第一版只用 `--cached`，
量過的後果是：一個**已經寫在磁碟上、還沒 `git add`** 的新檔案會完全看不見 ——
而那正是任何人在編輯時，工作副本絕大多數時間所處的狀態。
實測：新建一個未 tracked 的 `src/components/SneakyPreview.tsx`（內含 `<Lightbox>`），
舊的 `find` 掃法抓得到、只用 `--cached` 的版本**抓不到**、補上旗標後又抓得到。
把產物排除掉的是 **gitignore**（`frontend/.gitignore` 已經蓋住 `playwright/.cache` 與
`dist-paint-guard/`），不是「有沒有被 commit」—— 兩者在已建置的工作副本上都回 480 檔。

另外加了四條斷言：
1. 對**真實**樹斷言語料裡不含 `dist*/`、`node_modules/`、`.cache/`、`build/`、`coverage/`、`out/`；
2. 對照樹裡放兩顆**未受版控的誘餌**（與受版控的植入位元組相同，一個在 `dist-paint-guard/`、
   一個在 `playwright/.cache/assets/`）—— **必須抓不到**，而 `src/` 那兩個**必須抓到**；
3. 前置檢查「誘餌真的存在、真的含有違規、而且真的被 gitignore 蓋住」——
   否則「沒抓到」可能只是檔案不在，是空對空；
4. **反向的 reach 斷言**：對照樹裡再放一個**未 tracked 且未被 ignore** 的 `SneakyPreview.tsx`，
   **必須抓得到**。少了這一條，第 2 條可以被一個「悄悄縮到只看 staged 檔」的語料滿足。

另外，語料下限一旦失守，**下游那兩條掃描斷言不再印 `ok`** —— 它們在空語料上「什麼都沒找到」
本來就是空洞通過的字面定義。rc 早就是 1，但那兩行輸出在說謊，而說謊的 `ok` 正是下一個人學會信錯東西的方式。

## 這份紀錄涵蓋不到的

- **CSS 抽檔有沒有改到畫面**：靠 162 條 Playwright CT visual guard 在真瀏覽器裡跑過
  （含 `md-preview.ct.spec`、`t-c645-attachment-preview`（縮放 transform、
  `md-preview__action-label` 在窄寬度的 media query）、`chat-md-preview`、
  `reply-card-md-preview`、`artifacts-badge`），不是靠 jsdom。
- **staged 圖片在真瀏覽器裡長什麼樣**：沒有對應的 CT 故事，只有 jsdom 測試。
