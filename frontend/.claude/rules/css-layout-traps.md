---
paths:
  - "src/**/*.css"
  - "src/styles/**"
  - "src/components/**"
  - "visual-guards/**"
---

# CSS / 版面陷阱

## 文字與邊界

自由文字與 markdown 的基底 `.doc-md` 要用 `overflow-wrap: anywhere`，讓長 URL、sha 與無空白 token 能收縮 min-content；修在 `.doc-md` 基底（`settings.css`），它是唯一來源，別逐 surface 貼。T-4974 曾逐處貼，結果同一個病從沒貼到的頁面復發，才收斂成 T-d451 的基底修法。下一個修這類 bug 的人若不知這個理由，會重走那條路。

`anywhere` 不是 `break-word`：兩者都會斷已經溢出的行，但只有 `anywhere` 會收縮 min-content；用 `break-word` 以為等價，容器仍不肯縮，會讓修理的人誤以為 bug 已消失。沒有 markdown 繼承的自由文字欄位要自己宣告；`.doc-md` 的 `pre` 與 `table` 是明確允許的橫向 scroll 子區，修整頁面 overflow 時不可把它們的 `overflow-x: auto` 拿掉。守衛要同時量頁面與實際 scroll container，並同時確認 `pre`/`table` 仍能滾。

重驗 mutant 時，整頁 overflow 那條若先炸，測試會中止，底下的 per-surface 斷言根本沒執行；要證明後者，先暫時放寬整頁斷言再跑 mutant，不能把「整體失敗」誤讀成「後面的斷言也驗過」。

固定高度、可收縮 flex item、CJK 標籤同時出現時，用 `white-space: nowrap` 保住單行；中文的 min-content 可能只有一個字，固定高就會被折行溢出，而同一段 CSS 對拉丁字的幾何不等價。不要用 `flex:none` 代替，也不要只看 computed property：要用真瀏覽器量 line box 與實際 overflow。≤359px 的既有 header wrap 是 nowrap 之後的洩壓閥，別擴大斷點到會讓較寬手機多一行的範圍。

## 浮層與 CSS ownership

絕對定位浮層不可用以視窗左緣為座標的 vw 夾寬度。讓父容器提供 `left:0; right:0; width:auto`，再以 max-width 收上限；量浮層自己與中間 scroll container，不要只量被壓回視窗寬的 flex parent。`documentElement` 沒橫溢出不代表沒 bug，祖先的 `overflow-y:auto` 可能把溢出吸進自己的 scroll container；CT 也要重現真實祖先鏈，裸掛會多出餘裕而假綠。

使用某個 block class 的元件要自己 import 該 block 的 stylesheet；不可依賴 transitive import。最後一個間接 importer 消失時，仍使用同一 class 的另一個 dialog 會一起變成原生樣式；styleOwnership test 是必要護欄，因為 jsdom 與 tsc 都看不出 class 字串和 stylesheet 的關係。

## 篩選列的 pill 樣式今天有三份逐格相同的拷貝（T-93）

`.id-filter`（`idFilter.css`）與 `.tasks__filter`（`tasks.css`）逐屬性相同，`.replies__clear-filters` 與 `.tasks__clear-filters` 也是。這是刻意的取捨——元件自己帶著外觀，才不會依賴宿主頁的 stylesheet（同一節「用了哪份 CSS 的 class，就要自己 import 那份 CSS」）——但代價是**改其中一份不會有任何東西提醒你另外兩份還是舊的**。

⇒ 動這幾格的 padding／radius／border／字級時，三份一起看；只改一份就會讓兩頁的篩選列在同一個畫面上長得不一樣，而測試與 lint 都不會叫。

## lazy fetch

lazy prompt 的 fetch function 若由 wrapper inline 建立，不得直接放進 effect deps。用 ref 保存讀取函式，deps 只放真正的 cache key；in-flight 與 loaded key 分開，只有文字成功到手才蓋 loaded key。重繪不能取消仍有效的讀取，失敗要落 error state 並提供 retry。測試要在讀取途中用新 element 觸發 rerender、覆蓋成功、失敗重試與收合再展開。

## column flex 的 `align-items: flex-start` 會讓子元素被自己最寬的內容綁架(T-4aa0)

`flex-direction: column` 下,`align-items` 決定子元素的寬度；`flex-start` 讓它採
fit-content,下限是 min-content。子孫只要有不肯縮的 `<pre>` 或寬表格,子元素就會撐破
父容器的寬度限制。任務卡在真實祖先鏈量到 +148 @390；拿掉該宣告後 `<pre>` 回到
自己的內部捲動。

- 數字要註明真實祖先鏈：裸掛同一 bug 只有 +104，補上 `.app__main` 的 22px padding
  才是 +148；裸掛量測會低估真畫面。
- `min-width: 0` 治的是 flex item 的自動最小尺寸，不是這裡的 cross size；實測整條
  祖先鏈都無效。
- 不要從視覺上最明顯的區塊猜兇手；逐一隱藏子元素，看哪個消失才讓溢出消失，才是因果
  判定。按鈕移除後遺留的 `flex-start` 也可能成為同一顆地雷。

## 用了哪份 CSS 的 class,就要自己 import 那份 CSS(T-7526)
使用某個 block class 的元件要自己 import 該 block 的 stylesheet；不可依賴 transitive import。
styleOwnership test 防止最後一個間接 importer 消失後 dialog 變成原生樣式；jsdom 與 tsc
都看不出 class 字串和 stylesheet 的關係。

正職與外包詳情面板共用 `.mp-identity__actions` 的 column 外殼與 row buttons；更改在前、停止在後，沒在跑時只顯示喚醒。改 row/column 時手機 media query 要按新形狀重新驗跨距與均分，不能只驗「元素仍存在」；REST 仍是 `/restart`，退場的是 UI 用語，不是凍結 wire。

喚醒先開與更改相同的設定 dialog，預設保留原執行環境、模型、思考強度與已釘機器；落地順序是 model、必要時 relocate、restart。restart 不吃 machine_id。睡著的已釘機器不能 fallback 到第一台線上機器。

正職可只儲存；外包 relocate 會 kill + re-dispatch，除非 desired_state 已 offline，所以不可把兩者 UI 強行對齊。released worker 的身分文字與入口共用，依 worker.status 判定；released 不畫生命週期卡或 dead action，offline 對照仍要保留。
