# frontend/ — React SPA

進入 `frontend/` 時 nested-load。repo-wide 憲章見 root `CLAUDE.md`;本檔記 FE 專屬。棧:React18 + Vite5 + TS5。

## seam 分層(單向)
`wire → mappers → types → adapter → mock → http → hooks → component`。`api/index.ts` 的 `USE_MOCK` 是**單一 swap 點**(mock ↔ 真 http)。加一個 API:順著 seam 從 wire 到 component 各補一層,別跳層在 component 直接 fetch。

## API 錯誤(統一 envelope;見 `docs/design/api-error-envelope.md`)
非 2xx 一律 reject `ApiError`(`api/errors.ts`;mock ↔ http 同一 class):`.status`/`.code`/`.serverMessage` 來自 server envelope `{"error":{"code","message"}}`;讀 status 用 `isHttpStatus(e, n)`(同檔),**別 regex error message**(message 保形 `http <status> for <METHOD> <path>` 只供 log/legacy)。

## 設計 token(暗色)
底 `#191C24` / 卡 `#242832` / 文 `#E7E8EE` / 綠(online / 成功)`#6FD6B0`。i18n 兩語 `zh` / `en`(`Locale` 是封閉聯集,`locales/` 只有這兩份;`xian` 是**佈景主題**不是語系)。mobile 斷點 `720px`。

## presence
三畫面(roster MemberCard / MonitorPage / MemberDetailPanel)顯示走**同一個共用 `PresenceBadge`**(5 態:offline / waking / online / stopping / stopped),display 一律傳 `hub.is_online`(realtime 活線)。DB `member.online` 欄是 vestigial(唯一 reader = reconcile fallback),別當 display 真相。

**presence→視覺的推導只有一份(T-59d6)**:`LifecycleDot.tsx` 的 `presenceVisual(presence)`
是**唯一**的 5 態→視覺映射,`PresenceBadge`(正職)、**兩個外包面**(rail 的
`OutsourceTaskLine`、`WorkerDetailPanel` header)與 `MemberDetailPanel`
(它不畫點,是餵 `MemberActionButtons status=`,但那也是同一個 lifecycle→visual 映射,
一樣不准自己手寫一份)全部走它 + 同一顆 `LifecycleDot`。
顏色只准來自 `--color-dot-offline/waking/online/stopping/stopped` 五個 token
(**不准在 JS 寫 inline colour literal**——`npm run lint:tokens` 只掃 CSS,一個
`style={{background:"#6b7280"}}` 會整個繞過那道 gate,而且會讓四個非 online 態塌成
同一色,違反「點的顏色是 roster 上唯一的 presence 訊號」)。型別面:worker 的
`OutsourceWorkerView.presence` 是 **`MemberLifecycle` 五態 union**(不是裸 string),
因此打錯字或漏處理新態都是 compile error,不是默默漂移。

**wire 字串的統一只有一個 seam**:`mappers.ts::toPresence`(**不是 worker 專用**——
member 與 outsource worker 共用同一套 presence 詞彙,所以共用同一個 narrower)。
wire 那頭是裸 `string`(spec 已凍結),不認得的字 → `undefined`,再由各 caller 落到
自己誠實的地板:**member** 的 `lifecycle` 不可為空 → 落 `offline`(`status` 同源同落,
兩邊不會各說各話);**worker** 保留 `undefined`(released / 從未派工本身就是資訊)。
兩者最後都由 `presenceVisual` 畫成 offline 點,**永不假綠**。⚠️ 別把這個 narrower 拿掉:
沒被統一過的字會直接掉出 `presenceVisual` 的 no-default switch,渲染成
`lifecycle-dot--undefined`——**沒有顏色、`role="img"` 卻沒有 aria-label**,對讀屏是整個
消失的元素,而且不會有任何其他測試變紅。護欄:`api/mappers.presence.test.tsx`。

## unread 計數 badge(M2-1 紅點升級;與 presence 各自獨立)
roster MemberCard 成員列**右側(flex 尾端)的紅色計數 badge**(>99 顯示 99+、count=0 完全不渲染)= server 算好的 `member.unreadCount`(MemberDTO `unread_count`,chat_read watermark 的反相計數;只算成員→owner 訊息,agent↔agent 不計;舊純紅點 boolean 已整顆換掉)——FE 純 passthrough、**不自己算**。清除即既有已讀 choke:進對話的 `listChat` auto-mark / `markChatRead`;`useMembers` 的 ROSTER_TOPICS 含 `chat` / `chat_read` 讓 badge 即時亮/滅;開著的那個對話卡片以 `selected` 壓掉 badge(對話中新訊息永不累積)。badge 在整列(聊天入口)內,點 badge = 點列 = 進聊天,無獨立 handler。mock 以同一規則 live 計算(`unreadCountOf`)、行為與 http 一致;測試用 `__injectMockChat` 注入 inbound 訊息。

**三個顏色槽,外框自 T-d593 起獨立且無下限。** 這顆紅圓圈在控制台有 **7 個 render site
但只有 3 條 CSS 規則**(`.nav-tab__badge` in chrome.css、`.office__tab-badge` 與
`.member-card__unread` in office.css;⚠️ 側欄那兩個 site 是**同一段 JSX**——
`SidebarTab` 只有一個 `className` 字面、被呼叫兩次)。三條規則吃同一組槽:
底 `--color-danger-badge`、數字 `--color-on-danger`、**外框 `--color-danger-badge-ring`**。
- 外框那一槽的**預設是 alias `var(--color-bg)`**,不是烘死的 `#191c24`。這不是隨手寫的:
  外框本來就是借用頁面底色,而**主題可以改 `--color-bg`**;烘實色會讓「改過 `--color-bg`
  的既有主題」外框停在內建深藍、與它的頁面底分家 = 把舊主題的外觀改掉。alias 也讓
  `gen-theme-tokens.mjs` 把它收進 `THEME_ALIAS_DEFAULT_TOKENS`(匯出不烘值、編輯器補一列
  空值 placeholder 顯「跟隨 <頁面底>」)。
- 🔴 **`outline` 的對比下限已經沒有了**(owner 2026-08-01 `rc-1d57d0adc87d` 選②:
  「外框完全自由,不留下限(主題調到看不見也算你的選擇)」)。`check-token-roles.mjs` 的
  `MIN_PILL_VS_PAGE` 隨這顆退場;**主題把外框設成跟填色同色是被支持的選擇,不是缺陷**,
  別再把那條 checkRatio 加回去。**留下的只有「數字 vs 填色 ≥ 4.5:1」**(owner 沒裁到它)。
  lint 印出的 ring 比值自此**只是資訊、不是保證**。
- **護欄兩層,守的不是同一件事**:`visual-guards/badge-ring-token.ct.spec.tsx`(真 Chromium)
  量 `getComputedStyle().outlineColor` 的**實際顏色** ——jsdom 不算 CSS、解析不出 `var()`,
  這半在那裡做不到;`src/components/badgeRing.test.ts`(vitest)是來源掃描,盯
  「7 個 site 都戴那 3 個 class」＋「3 條規則都吃 ring token」＋標籤/產生器三件套。
  ⚠️ **後者存在的理由是 `test:ct` 不在雲端 gate 裡**(`bin/ci-cloud.sh` 只跑 vitest),
  只放 CT 等於回歸在 GitHub 上是綠的。

## 聊天未讀跳轉(M2 批次 19;LINE/FB 式,純 FE)
ChatArea 兩個行為,皆不動 server:
- **進房跳第一則未讀**:進對話時 snapshot `member.unreadCount`(**render 同步取**,搶在 listChat「list 即讀」清 watermark 之前——這是 race-free 的關鍵;server 清掉後 roster unreadCount 才歸 0)。第一則未讀 = thread 中 `from===peer && to===owner` 訊息的**最後 count 則之最早者**;其上渲染 `.chat__unread-divider`(「以下是未讀訊息」細線)並 `scrollIntoView({block:"start"})` 頂到視野頂;divider 整個 session 保留(如 LINE)。無未讀照舊落底。ChatArea 換 peer 不 remount → render-time guard 重置 session 追蹤;useChat 於 withId 換時**立即清空 messages**(防舊 thread 殘影 + 防未讀定位錨錯舊訊息)。
- **房內新訊息浮條**:owner 上滾(沿用既有 `nearBottomRef` 判定,80px 帶)時新進 `to===owner` 訊息 → `.chat__new-msg-chip` 灰底 pill 浮在 `.chat__body` 底部;錨點 = 浮條出現後**第一則**未看訊息(session 內以 message-id diff 追蹤,不動 server);點擊 smooth 捲到該則(`[data-msg-id]`);**捲到底才消失**(onScroll near-bottom 清除),點擊本身不清。在底部時維持原自動跟底、永不出浮條。i18n key:`chat.newMessages` / `chat.unreadBelow`(三語)。

## 全幅閱覽 = 一個 overlay、三種來源(owner 2026-07-28;T-f014 收編圖片)
`MarkdownPreviewOverlay` 是**唯一**的控制台內全幅面 —— markdown **與圖片都算**,
三個入口共用:
- **`url`**:已存檔的 .md 附件(T-a1c4),overlay 自己 fetch,header 保留「下載」
  **與複製分享連結**;因此 `url` 一定**併帶必填的 `attachmentId`**(T-4fdc:分享連結
  永遠用 blob 自己的 `att-` id 去 mint,不是 serve path、也不是 `ta-` 產物 id)。
- **`source`**:呼叫端**手上已有**的文字(聊天訊息本文)。不 fetch、不進 loading 態,
  **也沒有下載鈕、沒有分享鈕**——那串位元組從來不是檔案,給一個假造的 blob url 是
  說謊,而且沒有 id 可以分享。
- **`imageSrc`**:呼叫端手上已有的**圖片 bytes**(`data:` URI)——composer 裡還沒送出的
  staged 附件(T-f014)。那是真的檔案,所以**下載誠實保留**;但還沒上傳、沒有 blob id,
  所以**沒有分享鈕**——沒有東西可以指。
  props 是 discriminated union(`url`+`attachmentId` / `source` / `imageSrc` 三者互斥),
  傳兩個是 compile error;`url` 少了 `attachmentId` 也是 compile error。

**T-f014:舊的 `Lightbox`(`chat__lightbox*`)已退役、連同樣式整塊刪除。**
控制台裡**只剩這一個**全尺寸看圖面,所以每張圖(已存檔附件 / staged 預覽)都拿到同一組
標題列:檔名、分享連結(僅已存檔)、下載、關閉、Esc/backdrop 關閉、縮放。
退役前的實況比票面描述更糟:`AttachmentStrip` 早就**不讀** `onOpenImage` 了,
於是五個 call site 一邊把 handler 傳進一個忽略它的元件、一邊掛著一個永遠打不開的第二層
overlay,而**沒有任何東西是紅的**——沒用到的 prop 與到不了的元件都完全通過型別檢查。
守衛:`bin/tests/lightbox-retired-guard.sh`(production 碼零 `<Lightbox`、
stylesheet 零 `.chat__lightbox` 規則,附正負對照與 corpus 非空檢查)。

**放大必須改變 layout,不能只改 transform(T-7e68)**:owner 回報「可以放大,
但是無法左右或上下移動」。病因是縮放住在 `<img>` 的 `transform: scale()` 裡——
**它只把像素畫大、layout box 原地不動**,於是 `.md-preview__image-wrap` 的
`overflow: auto` 永遠沒有可捲內容,放大後溢出的四邊直接被裁掉、使用者碰不到。
⚠️ **「CSS 寫著 overflow:auto」不等於「使用者捲得動」**——這正是上一版把屬性當成
行為、寫進回報裡的錯,別再從屬性推論可達性。
- 🔴 **先別試 `transform-origin`**:最省事的想法是留著 `transform: scale()`、只加
  `transform-origin: 0 0`(或再用 padding 把空間撐出來)。**實測不通**——transform
  畫出來的溢出**不進入祖先捲動容器的 scrollable region**,400% 時
  `scrollWidth - clientWidth` 仍是 **0**,一樣沒得捲、四角一樣到不了。縮放必須是
  真的 layout。這是花實驗換到的負面結論,別再推導一次。
- **正解:縮放 = 圖片自己的 `width`/`height`**(量到的 100% fit box × zoom),
  **同時把 stylesheet 的百分比上限關掉**(inline `maxWidth/maxHeight: none`)。
  那兩條 cap 是「包住它的盒子」的百分比,留著就會讓圖再放大一次:1600×400 的圖在
  「200%」下實際畫成 fit box 的 **4 倍**,readout 直接說謊。**100% 時不下 inline
  尺寸**——那正是 `measureFit` 讀 fit box 的狀態,釘死等於凍結第一次量測。
  ⚠️ 這裡的**結構重整不是必要條件**:前一代那個 stage div 的骨架本來就是對的,
  最小修法其實只有兩行(img 補 `max-width/max-height: none` + 把 cluster 移出捲動
  容器)。現行形狀少一層間接、比較好推理,但別把它講成「非重構不可」。
- **resize 要重量 fit box,而且不能只在 100% 量**:兩條 cap 都是 viewport 相對,
  視窗一變 100% 的box 就變。只在 `zoom === 1` 掛 resize listener 會讓倍率說謊——
  900×700 下的 300%(2154px)在視窗縮到 500×420 後仍畫 2154px,而當下真正的 fit
  只剩 ~394px,**實際 5.5 倍、readout 還寫 300%**。舊的 transform 版沒有這個漂移,
  所以漏掉它是**回歸**、不是遺留。`measureFit` 量之前會**先把 inline 尺寸拔掉**,
  否則量到的只是 zoom 自己、每次重算都在前一次上複利。
- **兩條到得了溢出的路,不是一條**:pointer drag 與原生捲動(捲軸/滾輪/焦點在
  wrap 上的方向鍵),兩者都驅動**同一個** `scrollLeft/scrollTop`,沒有第二份偏移
  要同步,回到 100% 也就沒有殘留。
- **縮放控制列不可以住在捲動容器裡**:scroll container 的 `position: absolute`
  子元素是對它的 **padding box** 定位的,所以會跟著內容跑——400% 捲到角落時實測
  cluster 在 x ≈ -2031(frame 是 [91, 809]),使用者失去把自己帶到那裡的 −/+。
  因此多一層 `.md-preview__image-viewport` 當共用定位父層,cluster 掛在它身上。
- **矮視窗只准有一條捲軸**:frame 的兩條 cap 是 vh,但 frame 拿不到整個 viewport
  ——overlay 的 32px inset、panel 邊框、header、body 的 18px padding 先扣掉(共
  160px)。不扣就會在矮視窗超出 body 實際能給的高度,`.md-preview__body` 於是長出
  **自己那條**捲軸躲在 frame 的後面(900×500 實測 body 溢出 20px)。⚠️ **只放寬
  `min-height` 不夠**:改成 `min(360px, 50vh)` 仍剩 10px,因為溢出的另一半是
  `max-height: 70vh`,兩條都要扣。
- **手勢歸屬按手指數拆兩半,兩半的理由相反**(owner 2026-07-31 要求手機可拉動;
  他當時摸的是還沒 land 的 v0.5.53,也就是**完全拖不動**的舊行為)。
  - **單指歸瀏覽器**:縮放變成真 layout 之後,`.md-preview__image-wrap` 本身就是個可捲
    容器,**單指原生就能拖**(附帶慣性與回彈,自己實作要重寫一遍),背後頁面由
    `overscroll-behavior: contain` 擋住。所以 `onPanPointerDown` 對
    `pointerType === "touch"` **早退是刻意的**:再跑一次我們的 drag 會把同一段位移**套用
    兩次**(手指移一格、圖移兩格)。**兩者只能有一個負責移動**——想「補上觸控支援」而把
    那個早退刪掉,就是把 double-apply 裝回去。實測(真 input-layer 觸控事件):200px 滑動
    → scrollLeft 0 → 451;把縮放改回 transform 則 0 → 0。
  - 🔴 **雙指歸我們,不再交給 UA(T-043e;owner 2026-07-31:「在手機上二指撐開,要放大
    的是圖片本身,頁面不動」)**。交給 UA 時,pinch 縮放的是 **visual viewport**,而這個
    彈窗是 `position: fixed` 貼 **layout viewport** ⇒ header、按鈕、backdrop 跟著一起被
    放大,放大的是「整個彈窗」而不是照片。**很可能這就是整份回報的唯一根因**:只用雙指的
    人根本不會去按 −/+,app 自己的 zoom 從頭到尾沒動過。
    - **正解是兩件事,缺一不可**:(a) frame 上宣告 `touch-action: pan-x pan-y`
      ——**不是** `manipulation`(那個仍把 pinch 交給 UA),讓 compositor 在 JS 跑之前就
      不把手勢送去頁面縮放;(b) 元件自己接 `touchstart`/`touchmove`(non-passive,雙指才
      `preventDefault`)把兩指距離比例換成自己的 zoom state。實測 Chromium:**(a) 單獨就
      能把 `visualViewport.scale` 壓在 1,但沒有任何東西被放大**;**(b) 單獨能放大圖片,
      但頁面照樣被縮放**。
    - **iOS Safari 另接 WebKit 專屬的 `gesturestart`/`gesturechange`** 當第二條路
      (`touch-action` 抑制 pinch 在 Safari 歷來不完整)。兩條路用 `pinching` flag 互斥
      ——iOS 兩種事件會同時發,兩條都改 zoom 就是套用兩次。
    - 🔴 **絕對不准**用 `user-scalable=no` / `maximum-scale=1` 達成:owner 已明確否決
      (全站失去放大能力是無障礙倒退)。要的是「這個元素把手勢**接管**」,不是「對整個
      document **禁用**」。
    - ⚠️ **驗不到的部分**:守衛只有 headless Chromium。**Chromium 綠不等於 iPhone 上會動**
      ——WebKit 的 driver 開不出 `gesture*` 事件,那條路完全沒有自動化覆蓋。真正的驗收在
      owner 的手機上。
- **護衛**:`visual-guards/image-zoom-pan.ct.spec.tsx`(真 Chromium,**每一條斷言
  都量座標**、沒有一條靠屬性/class 就能滿足):四角可拖到、捲軸與方向鍵各自到得了
  遠角、控制列不隨內容跑掉、寬扁圖的百分比就是真倍率、矮視窗不出雙捲軸、resize 後
  倍率不漂。故事有**兩種長寬比**——1600×1000(高度受限)與 1600×400(寬度受限),
  只守前者會整個漏掉倍率說謊那條。雙指那組(T-043e)另加四條:撐開讀數真的變大且 frame
  兩軸都有行程、捏合會縮回、pinch 完單指滑動 `scrollTop`/`scrollLeft` 真的動、
  **`visualViewport.scale` 維持 1**。
  ⚠️ 最後那條**不是恆真的**,已實測:拿掉修補後,CDP 注入的雙指真的會驅動 Chromium 自己的
  頁面 pinch-zoom(scale 量到 **3**,而 app 讀數還停在 100%)——正是回報的 bug 在這個引擎裡
  重現。寫這種「某某沒有發生」的斷言前,**先把修補拿掉量一次**,否則你只是寫了一條擋不住
  任何東西的斷言。
- ⚠️ **寫這類守衛的三個陷阱**(都真的發生過):(a) **方向要選有東西可失去的那一邊**
  ——滾輪那條原本在 `scrollY === 0` 往上滾,任何實作都不可能往上動,斷言恆真;
  (b) **不要用固定按鍵次數校準捲動距離**——方向鍵每次捲多少各引擎不同,「按 16 次」
  在一個引擎叫「抵達」、在另一個叫「只走了三分之一」;(c) 🔴 **也不要改成「按到
  offset 不再變為止」**——這是本檔上一版寫的建議,**在 WebKit 上連錯兩次**:Safari
  **會吃掉聚焦後的第一次方向鍵**(實測 scrollLeft 0 → 0 → 28 → 40),停在第一次沒動
  就等於在暖身期放棄;而且 WebKit **對鍵盤捲動有動畫**,停在某次沒動也可能停在動畫
  中間、量到使用者根本沒看到的位置。**正解是直接問最終問題**:「按方向鍵(有上限)
  直到那個角落進入可視框」——與引擎無關,而且真的不會捲的實作依然過不了。
- **各引擎差異(實測,Chromium vs WebKit)**:①上面那條鍵盤差異;②**transform 畫出來
  的溢出在 WebKit 是進入捲動區的、在 Chromium 不是**——所以原始那個 bug 在 Safari 上
  只是「拖不動、但捲軸和方向鍵到得了」,在 Chrome 上才是四角全部碰不到。修完之後兩者
  行為一致。**別假設某個引擎的觀察可以外推**:mutant 實測,壞版本在 WebKit 只紅 4 條、
  在 Chromium 紅 6 條。

**樣式所有權**:`.md-preview*` 已從 office.css 抽成 `md-preview.css`,由
`MarkdownPreviewOverlay.tsx` **自己 import**。原本它是靠 OfficePage / RepliesPage /
TasksPage 的 transitive import 搭便車拿到樣式,而這個彈窗還會從任務產物彈窗、任務卡上
掛起來——正是 T-7526 那個「唯一 importer 一消失、連沒被碰到的畫面也一起壞」的形狀。
護欄:`src/components/styleOwnership.test.ts`(用了 block 就要自己 import;
且該 block 的規則只准有一個家)。

**單一換行:`source` 開 `breaks`、`url` 不開**(Seth 2026-07-28 review PR #18)。
聊天泡泡是 Enter=換行的介面(`breaks`),同一段文字被拿到全幅面讀時如果落回標準
markdown 的 soft-wrap,一則普通多行訊息就被重排成一條長句——**同一份文字在兩個面
長得不一樣**。反過來,已存檔的 .md 是文件、不是聊天行,維持標準 soft-wrap 跟其他
17 個文件面一致。這是刻意的**分家**,兩邊都有測試釘住(改一邊、另一邊會紅):
`MarkdownPreviewOverlay.test.tsx` 的 inline-breaks 與 blob-soft-wrap 兩支 +
`ChatArea.msg-fullscreen.test.tsx` 的泡泡/全幅面同形對照。

**render 一定要戴 `.doc-md`**:overlay 原本只掛自己的 `md-preview__md`,結果標題/
程式碼/表格/callout/連結全落回 UA 預設值——面板是深色主題、內容卻沒上色(owner
2026-07-28 回報)。`.doc-md` 是 17 個 render site 共用的文件皮膚,少戴一個 class
就等於自成一格,別再犯。護欄:`MarkdownPreviewOverlay.test.tsx`。

**聊天訊息的「放大閱讀」角落鈕**(`.chat__msg-expand`):只長在**對方(incoming)且
有本文**的氣泡上——自己那句是剛打的、純附件氣泡的檔案 chip 本身就是預覽入口。
hover(或鍵盤 focus)才現形,coarse pointer 恆顯低不透明度。⚠️ 位置是**氣泡讓位**
(`.chat__msg-bubble--expandable` 的 padding-right),**不是**浮在文字上:氣泡會
shrink-wrap 內容,單行訊息時浮動鈕會正好蓋掉最後一個字(實測「短訊息也有按鈕嗎？」
的「？」被吃掉)。Slack 那種浮動作法只有在全寬列上才成立。

## Esc 只給最內層:`lib/escapeLayers.ts` + `useEscapeLayer`(T-esc)
每個可關閉的面以前**各自** `window.addEventListener("keydown")`,於是**一次 Esc 被送給
全部的人**,每個人自己判斷該不該關。任務產物彈窗因此得去問「有沒有覆蓋層開著」
(`onPreviewChange` → `attachmentPreviewOpen`),而它問的時候答案**已經被拆掉了**:
DOM listener 依註冊順序觸發,覆蓋層先關掉自己、把 `false` 回報上來,彈窗的 listener
才跑。誰先誰後取決於誰先註冊,所以 `artifacts-badge.ct.spec.tsx` 那條 `.md chip` 守衛
**時綠時紅**。實測(乾淨 worktree,`origin/main` 原碼,n=15):**12 紅 3 綠**,而且
**與負載的相關性是反的** —— 紅落在 load 1.4–17.8、3 次綠都落在 load 17–18。
⚠️ **它的紅是 `mdChip.focus()` 等滿 30 秒逾時**,那個逾時**就是這個 bug**
(彈窗被連帶關掉 ⇒ chip 永遠不會回來),**不是負載造成的假紅、不准調寬逾時**。
- **機制**:全 app 只有**一個** window listener,Esc 交給**最內層**那一層。下面的層
  根本收不到,所以**沒有人需要去問上面有誰**。
- 🔴 **誰在最上層由 DOM 包含關係決定,不是註冊順序**。React 的**子元件 effect 先於
  父元件**,所以巢狀面若與宿主**同一個 commit** mount 就會**先**註冊 —— 用註冊順序排
  會**完全顛倒**(三層巢狀時 Esc 給最外層,最內層永遠收不到)。「巢狀面一定是後續互動
  才開的」**沒有任何東西在維持**,deep-link 就會踩到。註冊順序只留給**互不包含**的兩
  個面(兩個並排 dialog)當 tie-break:後開的在上。
- **用法**:`useEscapeLayer(onEscape, ref)` —— `ref` 指向這個面的根節點,巢狀關係就是
  從它讀的,**會被別的面包住的都要傳**;常駐元件裡的子視窗傳第三個參數 `active`
  (關著就不佔位)。handler 身分每次 render 變都沒關係,**層的位置不會動**。
- **層內要吞掉 Esc 就在 handler 裡判**(`ConfirmModal` 送出中即如此),別讓它漏到下面。
- **element-level 的 Esc 要 `preventDefault()`**:輸入框自己的 `onKeyDown`(`InlineEdit`
  / 角色新增列 / 手冊新增列 / 機器 onboard 列)與 layer **兩個都會跑**,不擋就會「取消
  編輯」**順便**把它所在的那個面也關掉。dispatcher 認 `e.defaultPrevented`,四個
  handler 各自 `preventDefault`。⚠️ 六個 layer **沒有一個真的做 focus trap**(只宣告了
  `aria-modal`),所以按 Tab 是回得到後面的輸入框的 —— 這條不是理論。
- 🔴 護欄的重點是 `lib/escapeLayerOwnership.test.ts`:**只有 `lib/escapeLayers.ts` 可以
  綁 window keydown**。理由是實測的 —— 把彈窗改回舊機制,**整套 1489 條 jsdom 只有這一條
  紅**,而唯一抓得到的 CT 守衛每跑只有約 1/3 機率紅 ⇒ 沒有它,單次 CI 偵測率約 33%。
- 其餘護欄:`lib/escapeLayers.test.ts`(派發規則,含三層同 commit 的顛倒案)、
  `lib/useEscapeLayer.test.tsx`(同 commit 巢狀/掛載/強制卸載/gated/重繪保位)、
  `TaskArtifactsPopover.test.tsx` 的兩層用例、`visual-guards/artifacts-badge.ct.spec.tsx`
  的 `.md chip` 那條(真瀏覽器)。

## 聊天/回覆輸入框(多行 composer)
三個多行 composer——聊天(ChatArea)、回覆卡(ReplyComposer)、TaskCard 任務訊息
框——都是 **textarea**(共用 `.chat__input`)。**送出決策統一到單一 `lib/composerKeys.ts`
的 `enterShouldSend`**(T-6bad),三處 onKeyDown 都走它、行為永不漂移:
- **桌面**(視窗 >720px):**Enter=送出、Shift+Enter=換行**(不變)。
- **手機**(視窗 ≤720px,`useIsMobile`):**Enter=換行、送出走送出鈕**——手機沒有
  實體鍵盤、shift+enter 不可行,一個裸 Enter 當送出會讓使用者打不了多行(owner
  2026-07-24 回報)。手機 Enter 由 `enterShouldSend` 回 false、handler **不**
  preventDefault,落回 textarea 原生換行。
- **IME 確認 Enter 永不送出**(native isComposing / 229 keyCode / 自家
  isComposingRef 三重 guard,收在 `enterShouldSend` 內),兩環境皆然。

高度隨草稿 auto-grow(`lib/autosize.ts`,useLayoutEffect 綁 draft——打字/送出清空/
失敗還原三路都會重算),CSS max-height(132px ≈ 5 行)封頂、超過走 textarea 自己的
overflow-y 滾動——長草稿永遠看得到全部。

## 回覆卡(等我回覆卡,M2 B2+B3)
兩個入口、一套內裡:`RepliesPage`(等我回覆頁)與 `ChatReplyCard`(聊天串內
inline 卡,訊息帶 `replyCardId` = wire `meta.reply_card_id` 時取代 bubble)都
渲染 **共用的 `ReplyCardBody.tsx`**(選項 chips/你選的/AI 建議 tag/重新決定流程)
+ 共用 `ReplyComposer`(打字/附檔/貼圖)——兩面永不漂移。同步 = reconcile-by-
refetch:兩側都訂 `reply_card` topic;聊天卡另走 `GET /api/reply-cards/{id}`
單卡 refetch。

🔴 **一次 owner 動作只准觸發一輪重抓(T-a3e4 step 8)**:`useReplyCards` 的
answer / reanswer / expire **不自己 refetch**——但**它們一定要自己對帳**:三個
寫入端點都回傳那張新鮮的卡,所以動作路徑**採用自己寫入的回應**
(`adoptWrite`,**零請求**)。
🔴 **光採用回應還不夠——in-flight 的 PRE-WRITE 快照會把卡片畫回去,所以採用之後那個
id 要被「按住」直到某個 server 快照同意。兩個 pane 各有一份保留、release 規則相反**:
waiting(`heldFromWaitingRef`)= 快照**不再列出**該 id 才放行;handled
(`adoptedHandledRef`)= 快照列出它**且 handled 戳記不比我們的舊**才放行(重新決定會
重新蓋戳,所以「有出現」不等於確認)。**一條 release 規則服務不了兩個 pane。** 觸發條件
只需要「點擊前不久有一則 delta 到過」= **串流剛剛還活著、然後掉了**(EventSource
掉線的典型形狀:掉線前最後一則事件觸發的 refetch 卡在飛行中),後果與原 blocker
**完全相同**(卡回到等待中、徽章跟著錯,而串流已斷 ⇒ 沒有更新的 refetch 會來救)。
T-e862 的 generation guard **救不了這一格**:它只在「有更新的 refetch」時丟掉舊快照,
串流斷掉時根本沒有更新的那一個。
⛔ **不准用「把那份 in-flight 快照整個丟掉」(`++waitingGenRef.current`)換綠**:那會
連同快照裡**別人剛開的新卡**一起丟掉,而串流已斷 ⇒ 那張卡可能一直不出現、**而且沒有
任何訊號** = 用一個靜默失敗換另一個靜默失敗。按 id 保留才對:快照其餘內容照常採用。
`handledCount` 因此要把「被按住的張數」加回去(那份 count 與被按住的列來自**同一個**
pre-write 快照)。**兩個方向各有一條測試**,見下方護欄。
🔴 **本檔上一版寫「delta 是唯一的 reconcile trigger」,那句對動作路徑是錯的、
而且是個 production blocker**:它把控制台的正確性押在一個**可有可無的即時事件**上
——EventSource 斷線或漏一帧時,server 已經收下答覆,等我回覆頁與導覽列徽章卻還
把那張卡畫成等待中,owner 再點一次就吃 409。**「SSE 斷線時卡片不再就地翻面」不是
已接受的交換,別再引用它**:step 8 換到的是**少一輪往返**,不是少一條 fallback。
delta 仍然是**別人**的寫入的 reconcile trigger,而它對 owner 自己的寫入**也會來**(http:server 的
`publishReplyCard`,在 row commit 之後、response flush 之前;mock:
`answerReplyCard` 裡同步呼叫的 `emitTopic`)。舊碼兩條路各抓一輪,T-e862 的
generation guard **只擋 commit、不擋請求**,所以多的那一輪是「整份下載完再丟掉」
——畫面上完全看不出來,任何「答完卡片會離開清單」的斷言在壞碼上照樣綠。
真 ocserverd 實測(25 張 waiting 卡):答一張卡 = **48 次逐卡 GET / 100,952 B**
→ 修後 **24 次 / 51,406 B**;對照組(別人開的卡、控制台沒動作)改動前後都是一輪,
那正是坐實「第二輪屬於本地動作路徑」的實驗。
⚠️ **`refresh()` 不在此列、仍無條件重抓**:它的 caller 是 409(卡已被別處處理),
那是別人的寫入,沒有自己的 delta 會來。
⚠️ 舊註解說動作路徑的 refetch 是「為了讓 mock 行為一致」——mock 長出 `emitTopic`
之後那句就不成立了,**過時的理由正是這個重複活下來的原因**。
護欄:`hooks/useReplyCards.one-round.test.tsx`(數呼叫次數,不看畫面;mutant:
把動作路徑的 refetch 加回去 → 紅 2 條、把 SSE 那條刪掉 → 紅 3 條)。

🔴 **同一條規則的第三個站點:`ChatReplyCard` 的單卡重複(T-a3e4 節點 8 後半)。**
`doAnswer` 原本在 `await api.answerReplyCard()` 之後**又自己** `refetch()`,而那張卡
還是 `waiting` ⇒ SSE effect 的 T-cdf4 guard 放行 ⇒ 同一次寫入抓兩遍。現已拿掉動作
路徑那一次。
⚠️ **但 `doReanswer` 的 refetch 保留,而且不准「順手對齊」成同一個形狀** —— 這個
不對稱是 T-cdf4 guard 逼出來的:重新決定作用在**已回覆**卡上,而那道 guard **刻意**
把終態卡的 delta 丟掉(那正是「70+ 張歷史卡不會每張都重抓」的來源),所以 SSE 路徑
**不會**觸發,動作路徑是那張卡唯一的更新來源。拿掉它 = `ReplyCardAnsweredBody` 在
`onReanswer` resolve 當下就關掉編輯模式,owner 被留在**舊答案**的畫面上。
`doAnswer` 現在**採用 `answerReplyCard` 的回應**(zero request),舊註解裡「斷線時不再
就地翻面是接受的代價」那句**已作廢**。
🔴 **但這件事是有條件的,別再寫成無條件定論(本檔上一版寫成「所以 SSE 斷線時那張卡
照樣就地翻面」,那句在只有採用、沒有讀取世代守衛的當下是**假的**——in-flight 的
pre-write `getReplyCard` 落地會把選項 chips 放回去,實測構造出來過)**。它成立要
**兩件事同時在**:(a) `doAnswer` 採用寫入回應,(b) 那次採用會讓**所有還在飛的讀取失效**
(`readGenRef`,`commitCard` 推進世代)。缺一即假。而且這句只講**這個元件的這張卡**,
等我回覆頁那兩個 pane 要靠自己的機制(`useReplyCards` 的按 id 保留)。
🔴 **同一個類別在回覆卡一共五個站點,三輪才收完,順序是:**`useReplyCards.answer/
expire`(pane+徽章)→ 採用之後的 in-flight waiting 快照 → **inline `ChatReplyCard`**、
**`TaskReplyCard`**(兩者都是單卡 `getReplyCard` 沒有世代守衛)、**handled pane**
(展開過一次之後每則 delta 都重抓)。**判準只有一句:某個 async 讀完之後寫回本地狀態,
而那次寫入可能比某個本地已採用的真相舊 ⇒ 它需要世代守衛或按 id 保留。**
`refresh()` 仍是 409 的無條件路徑(那是別人的寫入,沒有自己的 delta)。
護欄兩層、守的不是同一件事:`components/ChatReplyCard.one-round.test.tsx`
(**數呼叫**、不看畫面)+ `hooks/useReplyCards.sse-loss.test.tsx`(**看畫面**、把
`subscribeEvents` 換成 no-op)。
🔴 **理由要寫對(本檔上一版寫錯了、而且已被實測推翻)**:one-round 那條斷言是
**精確等於 1**,**0 不滿足它**——把 SSE reconcile 分支刪掉讓動作路徑變零輪,
one-round **紅 3 條**(實測)。真正的原因是**射程**:那個預算量的是「串流暢通時花
幾輪」,而修補前的碼在串流暢通時**恰好就是 1 輪**⇒ 它對「串流斷線」這個情境
**無話可說**,所以斷線那半必須另有證人。成本與正確性各一個證人,任一條都不能代替
另一條。
🔴 **「寫入回的是整張卡」這個前提現在有 server 側證人**:`adoptWrite` 的正確性完全押在
「三個寫入端點回的是全卡」,而 client 側的證據只有 jsdom + `api/mock`(它**構造上**就
回全卡,嫌不了你)。真正的對帳在
`server/ocserverd/api_replycards_writeecho_test.go`:三個動詞的**回應 body** 與那張卡
自己的 `GET /api/reply-cards/{id}` **逐位元組相同**(identity 而非欄位清單——清單會在
DTO 長新欄位的那天過期)。**語料必須有 body + options + 綁任務的卡**,否則兩個投影
長得一樣、比較等於沒比(實測:`openPlainCard` 的 fixture body 是空的,三條都會被那道
反恆真檢查擋下)。mutant:讓 `writeReplyCard` 對終態卡回**輕量列** → **3 條全紅**。
**mutant 實測(兩個方向各自被恰好一條釘住)**:把 `doAnswer` 的 refetch 加回去 →
「answering …」紅,**量到 2 次**(坐實重複真的存在);把 `doReanswer` 的拿掉 →
「re-answering STILL refetches」紅,**量到 0 次**(坐實它承重)。

✅ **逐張 hydrate 的 N+1 已經沒了(T-a3e4 節點 8 後半;owner 2026-08-02 核准
`?view=full`)**——上面那句「N+1 還在、不是這一顆修的東西」已經是**假的**,別再引用。
`GET /api/reply-cards?view=full` 一個請求回**整個 pane 的全卡**(逐位元組等於每張卡
自己的 `GET /api/reply-cards/{id}`,由 server 端測試釘住),http adapter 的
`listReplyCards` 因此只發**一個**請求。
- 🔴 **價值在往返次數,不在流量,別講錯**:真 ocserverd 實測 waiting pane
  **26 請求 / 49,970 B → 1 請求 / 44,183 B** = 少 **25 個 RTT**、位元組只少 **11.6%**。
  慢線路上延遲就是全部成本;**不要把它講成「省流量」**。所以 `http.view-full.test.ts`
  **只數請求、沒有任何位元組斷言**(那會暗示一個幾乎不存在的節省)。
  ⚠️ **上面那組是 owner 裁定當天、實作開始「之前」量的**(拿當時的 light+逐卡
  hydrate 對照一個手動組出來的 atomic 回應)。**它不是事後實測,別再引用成事後實測。**
  **改後實測(2026-08-02,真 ocserverd、隔離 port、fresh DB、母體量測當下重數
  25 waiting / 15 answered / 10 expired,只換 `api/http.ts` 這一個變因)**:
  | 情境 | 改前(light + 逐卡) | 改後(`view=full`) |
  |---|---|---|
  | 開頁:waiting pane | 26 請求 / 27,537 B | **1 請求 / 21,294 B** |
  | 展開 handled、一則 delta | 54 請求 / 58,509 B | **3 請求 / 44,195 B** |
  (請求數只算 `/api/reply-cards*`,`/count` 那一發兩邊都是 1、未計入。)
  ⇒ 第二列**少 51 個 RTT、位元組只少 24.5%** —— 結論與上面同向:**價值在往返次數**。
  ⚠️ **位元組不可與那組 49,970 / 44,183 直接相比**:舊 harness 未進 repo、
  卡片語料無法重建,兩組的母體內容不同 ⇒ 只有**請求數**是可比的。
  ✅ **「畫面內容不變」有畫面層證人了**:同一台 server、同一份 DB、同一份母體,
  兩個 arm 各渲染一次 `RepliesPage`(waiting + 展開的 handled),正規化後的 DOM
  **逐位元組相同**(121,588 B,sha 同);負對照(把 `view` 改成 `light`)DOM 掉到
  56,528 B ⇒ 這個比較有鑑別力,不是恆真。
- **①② 是同一個修改點**:waiting pane 與 近期已處理 pane 都走 `listReplyCards`
  (`useReplyCards` 呼叫三次:waiting / answered / expired)⇒ 一處改完,展開的
  等我回覆頁從每 delta 約 51 次往返收成 **3 個請求**。**收合時零成本那個 gate
  (`handledLoaded`)沒有被碰**,別動它。
- ⚠️ **`view` 只活在 http seam,不是 adapter 概念**:mock 本來就出全卡,所以 parity
  在 adapter 層不變、mock 一行沒改。**別把 `view` 提到 adapter 簽章上。**
- 🔴 **agent 面一個位元組沒變**:`view` **刻意不在** `list_reply_cards` 的 MCP
  inputSchema 裡(登記在 `server/ocserverd/spec_catalog_conformance_test.go` 的
  `deliberatelyOffMCP`,不是 `knownCatalogDrift`——那份是「該補的債」,填錯欄會招來
  下一個人「把它廣告出去」)。理由:輕量列就是 owner 裁定的 agent 契約(T-3f31),
  給 agent 一個一次拉整個 pane 全卡的把手,等於把 T-3f31 縮掉的還回去。seeds 因此
  **不需要同批改**(它教的「列表只給標題＋決策要點、全文 `get_reply_card`」對 agent
  仍然逐字為真)。conformance `test_view_is_not_advertised_to_agents` 對**線上**
  tools/list 釘這條(不是對凍結檔)。
- **回應 schema 是聯集**(light 列 | 全卡),因為同一條路由服務兩種投影;`?view=full`
  就是選第二臂的東西,所以 adapter 在那裡窄化。代價:`ocapi_gen.go` 多 88 行**沒有
  任何 caller** 的 union wrapper(該檔第一個 union 構造)——owner 在知情下選了
  「規格誠實」這一邊,對照選項是照 `?view=list` 的先例**完全不宣告**輕量形狀。

**list wire 輕量化(T-3f31)**:`GET /api/reply-cards` 的**預設**仍只回輕量摘要
(summary+決策 digest,無 body/options 全文),`?view=light` 與不帶參數逐位元組相同;
不認得的 `view` 值回 **400 並點名兩個合法值**(默默落回 light 會讓一個打錯的字**無聲**
恢復逐列 fan-out——那正是這顆在修的成本;這是刻意偏離 `?view=list` / `?fields=light`
兩個先例的靜默落回,owner 知情未反對)。跳到原訊息 = `#office/chat/<id>/msg/<msgId>`(hashRoute `msgId`)
→ ChatArea `jumpToMsgId` 定位(center scroll)+ `chat__msg--located` 高亮
flash;one-shot、消費掉 entry positioning(不與未讀 divider 打架);目標超出
載入窗(recent 30)誠實 fallback 落底。徽章(待回覆數)與聊天未讀紅點是兩個
獨立訊號:回卡不清紅點,紅點只有進對話才清。**已過期終態(T-1aa4)**:waiting
卡 head 有 owner 專用「標為過期」次要鈕(`ConfirmModal` 二次確認——終態、不可
復原、不算回答);`ReplyCardBody` 第三個內裡 `ReplyCardExpiredBody`(灰 tag +
選項靜態 review,無 chips 可點、無重新決定),三個渲染面(RepliesPage/
ChatReplyCard/TaskReplyCard)共用,collapsed stub 的 tag 分「已回覆/已過期」。
等我回覆頁第二 pane 改**「近期已處理」**(answered+expired 併列、各自 24h 窗、
handledTs 新→舊;header N = count.answered+count.expired);`useReplyCards` 出
`handled/handledCount/expire`,`api.expireReplyCard(id)`(mock 鏡像含 step/task
hold 釋放)。status union 全線(adapter/mappers join)= waiting|answered|expired。

## 任務頁 + 任務卡(M3 Phase 3)
主導航第四頁「任務」(`#tasks`);badge = 非終態任務數(`GET /api/tasks/count`,
`useTaskCount` 訂 `task` topic,接法同等我回覆 badge)。資料流 = `useTasks`
(mount fetch + SSE `task`/`outsource_worker`/`task_manual` refetch);
🔴 **清單以「使用者勾的那組狀態」向 server 要(T-a3e4,owner 拍板「不是應該以狀態
filter 嗎」)**:`useTasks(initialStatuses)` 把 TasksPage 的 `statusFilter` 送成
重複的 `?statuses=`,**執行者/類型兩軸仍在 FE**(它們不是 payload 病灶)。
舊寫法是「不帶 query 拉全量、全部在 FE 篩」,T-2b9d 加了 `?open=true` 想救,
但 T-1d82 又補了一條「只要任何未結案任務帶 dep 就把 includeClosed 打開」——
實務上恆真,所以**每一則 task SSE 都在重抓整部歷史**(實測 408,482 B vs 17,295 B)。
現在 dep 顯示資料由 server 附在列上(見下方 dep chips),那條 clause 已整個刪除。
**只有一個**視圖真的需要全狀態,靠**送空集合**表達:清除篩選——那時使用者要的就是
全部,下載全部是答案、不是缺陷,別「順手」把它也優化掉。
🔴 **`#tasks/<id>` 跳轉錨點曾是第二個,而那個是缺陷**(owner 2026-08-01 指名要修):
錨點可能指向沒被勾的狀態(預設篩選下的已結案任務就是日常),舊碼因此**整個放掉篩選、
改抓不帶條件的全量**,只為了讓那一張出現在畫面上(實測 432 kB / 706 列)。現在
`useTasks(initialStatuses, anchorTaskId)` 走 **`GET /api/tasks/{id}` 只補抓那一張**再
併進 `tasks`,清單那一問一個字都不動。三條配套是一體的,拆掉任何一條就是把病裝回去:
(a) **anchor id 是參數、不是 effect**——跳轉可能是首次 mount,晚一個 commit 就等於
mount 那一發又拉了全量;(b) **`anchorPending`**——單張補抓在自己的 request 上,所以
存在「清單到了、那一張還沒到」的幀,自癒邏輯(未知 id → 退回 `#tasks`)與兩個空狀態
文案都必須等它,否則連結會在使用者眼前把自己抹掉;它**成功與失敗都會落定**,所以補抓
失敗(500/離線)是誠實退回一般清單,不是空白也不是轉不停;(c) **合併時清單列優先**
——`TaskDTO` 沒有 `dep_tasks` 欄(那個 join 只掛在輕量列上,spec 已凍結),讓單張版蓋掉
清單列會讓被連到的卡片 dep chips 掉回「還不知道」。⚠️ 已知取捨:錨點指向**沒被勾的狀態**
時,那張卡的 dep chips 就是「還不知道」態(`depTasks === undefined`)——誠實的第三態,
不是謊,但要修得動凍結 wire,不在本批。
護欄:`hooks/useTasks.anchor.test.ts`(斷言實際送出的 `statuses` 永不為 undefined)+
`components/TasksPage.anchor-fetch.test.tsx`(已結案錨點仍顯示、in-flight 不自癒、
補抓失敗誠實落地)。
**空狀態文案的判準改讀 `GET /api/tasks/count` 的 `total`**(未篩選總數):
「目前沒有任務」是對整個工作室的主張,篩選過的清單答不出這件事——而它是一個
grouped COUNT,不是把清單重新拉寬。
分區:未結束(非終態一清單,高→中→低→凍結、同級 created 新→舊,不分狀態子組)
/已結束(可摺疊預設收合,同 RepliesPage answered-toggle)。卡(`TaskCard`)
無詳情頁、**預設摺疊**(owner 照 mockup 拍板 2026-07-13):卡頭(標題+
「type · 負責人 · 模型 · 投入度」副標,成員執行者帶「· 成員」)+優先權/狀態
徽章+kebab+chevron;#T 代號 chip+識別鍵 chip+「等 T-xxxx」dep chips(**dep 的編號/標題/狀態讀
`task.depTasks`(wire `dep_tasks`)——server 對整張 task 表 join 好的,T-a3e4;
不要改回 `allTasks.find`,那個查找就是上面那條 payload 病灶的來源。三態要分清:
有 status = 解析到、status 為空 = 查無此任務、整個欄位 undefined = 這個 server
不解 dep(還不知道,不可宣稱不存在))、進度條
「步驟 N/M · 已歷時 X」、等待外部紫 banner、訊息框**摺疊時也顯示**;chevron
展開才給 description+內嵌回覆卡+工作流程(每步名稱+狀態徽章+DoD+右上耗時);
負責人、建立者與前任負責人的身分 chip 會依 stable member id 顯示個人頭像，
無個人圖時沿用 role/theme → glyph fallback；Avatar 本身仍不畫 presence 點。
§3.6 跳轉目標自動展開:
- **進度/狀態全 passthrough**:`progress_done/total` 用 server 算好的,UI 不自算;
  狀態推進 agent 回報、owner 只有「終止」這一個直接狀態動作(ConfirmModal 二次確認)
  + 優先權調整(含凍結/解凍,同一 `/priority` knob)。
- **gate 狀態**:`is_gate` + `reply_card_id==""` = 虛線「等我回覆」預告;非空 = 生效
  → 內嵌 `TaskReplyCard`(可多張),內裡**絕對重用** M2 `ReplyCardBody.tsx`
  (單卡 refetch + `reply_card` topic,同 ChatReplyCard 模式——回覆同步反映到
  等我回覆頁)。**H4 配套**:gate step 仍 `waiting_owner` 而綁卡已 answered →
  step 徽章顯「已回覆 · 等待接手」(子卡經 `onCard` 回報卡態給 TaskCard)。
  step 徽章單一判斷源 = `lib/stepBadge.ts`(T-d64f);**superseded(T-1aea)**:
  re-plan 凍結的已答卡節點 → 「已取代」徽章 + `task-step--superseded` 灰階,
  問答內容仍由內嵌卡承載;gate 預告分支對終態(done/superseded)不再虛線預告。
  superseded 不算 `progress_done/total`(server 除名)→ 「全 superseded」任務誠實
  報 0/0 但 steps 非空:TaskCard 的 hydrate loading gate 不再要求 progressTotal>0
  (未指派例外,等待指派可從輕量摘要直接推導),避免落「等待建立 Steps」謊態。
- **外包顯示誠實線**:TaskCard 的「外包 代號 · 模型 · 投入度」只從 LIVE
  `GET /api/outsource-workers` 解析;worker 已 release(結案)→ 誠實退回裸「外包」,
  永不捏代號。⚠️ **這條講的是任務卡上的 chip,它描述的是「這張任務要用什麼開外包」
  ——launch intent,刻意留設定值**;監控台那張表的外包列走的是**另一條**(自報值,
  見下方「effort」節),兩者別互抄。未指派(kind=outsource, executor_id="")→「未指派」+ 訊息框 disabled
  (server 會 409)。過渡態:未指派→「等待指派」、有執行者零節點→「規劃中」。
- 訊息框 → `POST /api/tasks/{id}/message`(server 幫掛 task context meta 成普通聊天
  訊息)。已歷時自 created_ts ticking(`lib/duration.ts` 的 `formatDuration`,與
  RepliesPage 已等你共用)、終態凍結在 closed_ts。狀態文案照 spec 六態
  (尚未執行/進行中/等我回覆/等待外部/已完成/終止),不用 mockup 的變體。

## 外包面板 + 外包聊天(M3 Phase 4,SPEC §4;列形 2026-07-14 owner 截圖回報重裁)
辦公室左欄的第二組(`OutsourcePanel`;左欄照 mockup 分「正職/外包」兩組——
正職 header=標籤+計數+摺疊 chevron(OfficePage `staffOpen`),成員卡=名字+
離線徽章+PresenceBadge+未讀數(**聊聊鈕已移除**——Seth 2026-07-13 拍板、蓋過
mockup 與同日「恢復聊聊鈕」舊裁定:該 flex-end 位置只剩未讀 badge,有未讀才
顯示;整列本身仍是聊天入口,行為不變)。**外包列也有未讀 badge**(owner
2026-07-14 截圖回報,蓋過舊「外包無未讀資料源」誠實線):wire
OutsourceWorkerDTO 新增 optional `unread_count`(server 用與 member roster
同一個 UnreadCounts watermark 反相計數注入,spec 已凍結入 openapi.json),
FE 純 passthrough、渲染同 member-card 的紅 pill(>99 顯 99+、count=0 不渲染、
selected+windowActive 壓掉),mock 以 `unreadCountOf` 同規則 live 計算。
資料 = `useOutsourceWorkers`:**只有** `GET /api/outsource-workers` + settings,
訂 `outsource_worker`/`task`/`chat`/`chat_read` topic refetch(四個 topic 同一條
路徑)。**T-a3e4 之前它還會拉 `GET /api/tasks`(不帶 query = 整部歷史)與
`/api/task-manuals`,只為了 join 排序鍵與兩個 label**;T-ec2c 那個「chat delta 只
重抓 workers」的雙路徑就是為了繞過那次下載。現在 `task_no`/`task_created_ts`/
`task_type_key`/`task_type_name` 由 server 附在 worker DTO 上,join 與雙路徑一起
拿掉了——**別再把 task list 的 fetch 加回這個 hook**。**列形(owner
2026-07-14 截圖回報,對齊正職成員卡三行、蓋過 2026-07-13「代號·狀態+識別鍵
chip」舊裁定)**:第一行 **代號 (O-7 式)**(外包唯一的名字);第二行 **接到的
task type + presence 點**(外包沒有角色名,綁定任務的 typeKey 就是它的角色行;
typeKey 空 = 自由代辦字樣);行首那顆點是 **worker 真實 presence**(共用
`LifecycleDot` + `presenceVisual`,五態五色)——**舊的「live worker 恆 online」不變量已於
2026-07-26 由 owner 廢除**(owner 截圖回報:server presence=offline、task=not_started、
無機器的 X-46 被畫成綠點 = 錯的。「在列上」只代表任務未終態,不代表 session 起來了);第三行 **任務代號 (T-xxxx) chip,可點 → `#tasks/<taskId>`
任務頁定位**(同回覆卡「查看任務詳情」的 locate-anchor 路由)。**不顯模型名、
不顯任務標題、不顯識別鍵、不顯狀態字**(狀態看任務頁);排序 = **綁定任務的
created_ts 新→舊**(join 不到才 fallback worker 自己的 mint stamp);任務終態
→ worker 從 wire list 掉出 → 列消失(誠實,不快取)。**左欄空間分配**(owner
2026-07-14:外包區至少同時可見 2-3 列):`.office__members-list` `flex:1` 自身
捲動、`.outsource-panel` `flex:none`、其 list `max-height: min(42vh, 276px)`
內部捲動——正職永遠佔較大比例、外包不再被擠到剩一列。標題列帶「N / 上限」
(-1 顯 ∞)+ 齒輪 → **外包上限設定 popover**(標題+說明+「最多雇用」−/＋
stepper+無限鈕+完成,照 seth-member-2):上限 = `settings.outsource_max_parallel`
(PATCH /api/settings,**-1..20;-1 = 無限、0 = 暫停指派**,面板明示「已暫停
指派」;settings 沒載到 → 誠實只顯 N,不捏上限)。**點列 = 開聊天頻道**:worker 的 `ow-` id 直接
走 `#office/chat/<id>` 同一個 chatId 槽(OfficePage 先查 workers 再 fallback
roster,released 自癒回預設成員聊天);ChatArea 完整重用,以 synthetic Member
+ `headerSub` prop **替換** PresenceBadge——**理由不是「worker 沒有 presence 可顯示」**
(那句已隨上面的不變量一起廢除:worker 有真的 wire `presence`),而是版面裁定
**presence 只在 rail 那一個地方顯示**,chat header 不長第二個 presence 來源,改顯任務行;
worker 詳情 header 則走與 rail 同一顆 `LifecycleDot`;
標題「外包 · 代號」;無詳情面板(不傳 onOpenDetail)、無 unread 計數。

## 設定 › 任務手冊(M3 Phase 4,SPEC §5)
設定 landing 新增「任務手冊」與角色誌並列(`TaskManualsPage.tsx` 的
List/Detail,資料 = `useTaskManuals`:`/api/task-manuals` CRUD,訂 `task_manual`
topic;**手冊編輯 = POST /{type_key} 部分更新**,wire null=不動、assignee
`{}`=解除)。列表 = 類型列(**只顯 type_key**,照 mockup;owner 2026-07-13),**出廠全空**;
新增 = inline row 填 type_key 建**空白手冊**(重複 → 409「這個類型已存在」);
刪除 = 確認 modal,**有非終態任務 → 409** 顯「先讓它們結束才能刪除」。詳情 =
**hub 式層級**(owner 2026-07-13 照 mockup 重裁,取代舊單頁 tabs):breadcrumb
「設定›任務手冊›type」+ 大標題 + **負責成員摘要卡**(icon+「負責成員 · 同類型
所有任務由他負責」+一行設定摘要+編輯)→「任務規劃」段兩張**子頁入口卡**
(任務定義/學習經驗)→ 各自子頁,子頁頂 pill 頁籤可互切;**不顯內部檔名**
(舊裁定仍有效,mockup 的 review-pr.md chip 刻意不做)。任務定義 = 三題引導
(Q1 用途文字/Q2 欄位清單:名稱+必填切換+識別鍵標記**可複合**、可增刪(空名列
commit 時丟棄)/Q3 SOP markdown),編輯模式比照角色誌(編輯/取消/完成;
**無重置**——手冊無 seed,同 custom role 先例);學習經驗可編輯(agent 結案
回寫面);**負責成員編輯 = 成員面板式**(照 seth-ui-3):指定成員/外包全寬
segmented(成員 pick row 右側顯示該成員的**角色 label**,解析順序同
PresenceBadge:i18n seed key → server roleName → raw key;無角色資料誠實省略
——owner 2026-07-13,選人時看得出誰是什麼角色)、模型 = 成員面板同源 MODEL_QUICK_PICKS chips+自由輸入、投入程度 =
低/中/高 segmented、**機器段**(**純機器清單、無「自動分配」列**,狀態字 =
machines.online × monitoring agents 誠實映射:閒置/忙碌/離線;說明「沒選機器或
該機器離線一律不啟動,原因顯示在該外包上」——**離線自動 fallback 的承諾已廢**)、
**雇用數量 = −/＋ stepper+無限鈕**(wire `copies:0` = 無限、`machine:<machine id>`
——必須解析到真機器,`"auto"` 已廢、送了 400;**沒選機器就整個 key 省略、不送 `""`**
——wire 只認非空 id,spec TaskManualDTO)、解除設定 = wire `{}`
→ assignee patch(指派本身一律 server 執行,卡上只設定)。

## 請示 ↔ 任務跳轉(M3 Phase 4,SPEC §3.6)
`ReplyCard.task`(wire `ReplyCardDTO.task` = TaskRefDTO,mapper 恆置
null-when-absent;view 欄位 OPTIONAL 保測試 fixture,先例 Member.roleName)。
任務衍生的請示卡(task 非 null)在 RepliesPage 與 ChatReplyCard 都顯**精簡任務
資訊 row**:類型 badge(typeKey;"" → 自由代辦)+「查看任務詳情」——**不露任務
編號/識別鍵**(裁定);點 → `#tasks/<taskId>`(hashRoute 新 `taskId` 段)。純聊
天請示無此 row。TasksPage 端 = **settle loop**(每個 effect pass 修一件事再
re-run):終態目標 → 自動展開已結束;**錨點直接壓過三個篩選軸**(`matches` 對
`taskIdFilter` 短路成 `task.id === taskIdFilter`,不是逐維度去清)、那一張則由
`useTasks` 的 `anchorTaskId` 單張補抓進來(見上方任務頁節);card 進 DOM →
scrollIntoView + `task-card--located` 高亮 flash(2.6s)→ **消費 anchor**(route
退回 `#tasks`,one-shot,可重跳);未知/過期 id 誠實自癒(消費 anchor、不高亮
——但**必須等 `anchorPending` 落定**,不然「還沒載到」會被當成「不存在」)。

## 一則通知 = 一次「只抓它碰到的那一項」(T-8115)

reconcile-by-refetch 的規則沒變(**永遠不 merge payload**),但「refetch 什麼」與
「一則通知算幾次」重新定義過。三個機制,全在 client,**wire 一個位元都沒動**:

- **`SseDelta`(`api/adapter.ts`)= payload 的 identity-only 投影**。spec/sse.md §2.2
  的 payload 一直都在,禁止的是**merge**(它欠所有 server-derived DTO 欄位)。但
  「這則寫入碰到哪一個 entity」是**識別**、不是值,拿它去 `GET /{id}` 重讀一項,值仍然
  完全由 server 給。⇒ `api/http.ts` 的 `toSseDelta` **只留** `id`/`from`/`to`/`reader`/`peer`
  五個欄位,`status`/`priority`/`last_read_ts`/`codename` 這些**在 seam 就被丟掉**,
  下游 hook **拿不到**、因此不可能不小心 merge——「不准 merge」變成型別性質,不是要記得的規矩。
  護欄:`api/http.sse-delta.test.ts`(每條都斷言那個值**不在**投影裡)。
- 🔴 **names 為空 = 「什麼都可能漏了」,一律全量重抓**。resync 名不指任何一項(串流沒有
  replay,漏了什麼本質上不可知),mock 更是連第二個參數都不傳。**空名字絕不可讀成
  「沒事發生」**,那會把 mock 控制台與每次重連後的自癒一起凍住。
- **`lib/deltaSink.ts`:一「陣」delta 只做一次決定**。`resyncAll` 把 13 個 topic
  **同步**扇給每個訂閱者,所以聽 4 個 topic 的 hook 以前一次 resync 跑 4 次同樣的重抓
  (實測:一次 resync 21 個請求,12 個是重複)。**coalesce 只能發生在「決定要不要重抓」
  那一層**——傳輸層不知道某個訂閱者對哪些 topic 有反應,也就不知道那 13 次會塌成 1 次。
  它靠的是「那個扇出是同步的」:累積到下一個 microtask 剛好抓到整陣。⚠️ **這不是 debounce**,
  跨 tick 不合併(那等於刻意讓畫面慢);`deltaSink.test.ts` 有一條專門釘這件事。
- **`narrowToHeld` 是三態,而中間那態是關鍵**:`null`=名不指任何一項⇒全量;**非空陣列**=
  它指到我手上的項⇒逐項重讀;**空陣列**=它指了別人、一個都不是我的。第三態**不等於**
  第一態:對「不可能改變我這份清單成員資格」的 topic(chat/chat_read 對 roster 與外包 rail)
  它就是**真的沒事做**;對可能新增列的 topic(task)則必須當全量辦——新的一列只有清單看得到。
  把兩者合成一個 falsy 判斷,就會丟掉其中一半的修補。
- 🔴 **逐項重讀只有在「單筆回應是清單列的超集」時才成立,而三個端點只有一個是**
  (見 `api/dtoParity.ts` — 這是那份表存在的全部理由):
  - `useOutsourceWorkers` — **可以**逐項:chat/chat_read → `GET /api/outsource-workers/{id}`。
    🔴 **但先過 `burstMovesNoOwnerUnread`(T-b17f)**:`api_outsource.go` :136/:199/:358
    三處都是 `UnreadCounts(messages, receipts, currentActor(r))`、reader 一樣是 owner,
    而且**各自跑一次 `ListChat()` 全表掃描**。所以 `m-other → ow-1` 那則雖然**指名了
    rail 上的 worker**,那一列的 badge 卻**不可能動**(recipient 是 ow-1、不是 owner)
    ⇒ 0 個請求。⚠️ **`ow-1 → owner` 是完全相反的一格,它的重抓是正當的**——述詞問的是
    `to`,不是「有沒有指名一個 worker」,所以分得出來;那條的對照測試(`ow-1 → owner`
    仍 `getOutsourceWorker === 1` 且 `getChatUnreadCount === 1`)是這件事的守衛,把
    `chat` 那條述詞改成恆 `false` 就會打紅它。
    server 的單筆 handler 呼叫**同一支** `projectWorker`、帶**同一個真的** `unread[worker.ID]`
    ⇒ 一個欄位都沒少。排序鍵是**綁定任務的 created_ts**,聊天碰不到 ⇒ 不准重排(否則一則
    訊息會讓列跳位)。`outsource_worker`(派工/釋放=成員資格)與 `task` 照舊全量。
  - `useMembers` — **可以**逐項:chat/chat_read 指到手上的成員 → `GET /api/members/{id}`
    **原位換掉**(roster 由 server 按 name 排序,這兩個 topic 碰不到 name ⇒ 不重排)。
    🔴 **但這條路一度是錯的,而且錯得很安靜**:那支 handler 原本把 **literal 0** 交給
    `newMemberDTO`(清單那支才跑 `UnreadCounts`),所以逐項重讀會把 delta 正在宣告的紅點
    **歸零** —— 方向只有一邊,**badge 只降不升**。修在源頭:兩支 handler 現在都走
    **同一支** `unreadCountsForRequest`(`server/ocserverd/api_helpers.go`),
    **沒有動 schema**(`MemberDTO` 一直宣告著 `unread_count`)。server 側護欄
    `api_members_unread_parity_test.go`(單筆 vs 清單、斷言**回應 body 裡的數值**;
    把那行改回 `0` 就紅),client 側 `api/dtoParity.test.ts` 斷言兩者**相等**。
    ⚠️ **「兩支 handler 共用」的範圍就是那兩支,不是「每個回 MemberDTO 的端點」**
    (2026-08-01 實查):六個 `newMemberDTO` 呼叫點裡只有清單與單筆帶真的數字,
    `writeMemberDTO`(約 15 個 handler 共用)、`api_members.go:462`/`:565`、
    `api_roles.go:222` **仍然傳 literal 0**。今天沒有使用者可見後果(控制台不把那些
    回應塞回 roster),但別把上面那句讀成「到處都是真的」。同理它**不是** repo 尺度的
    「一支共用計算」——`api_outsource.go` :136/:199/:348 與 `api_chat.go` :873 還有
    **四份** inline 複製。
    🔴 **這條行為沒有被 conformance 釘住**:`unread_count` 在 `conformance/` 裡
    **零命中**(2026-08-01 實查),所以 repo 自己指定的行為契約層對這個欄位新舊行為
    都沒有任何主張;上面那道是 Go 單元測試,不是同一件事。
    **指到的不是我手上的人**(外包 / 已釋放的 peer)則**什麼都不做**。
    `member`/`role_def` 照舊全量。
    🔴 **逐項只給「恰好指到一個」;指到兩個以上一律重抓清單,而交叉點正好在 2、
    是推導出來的不是可調旋鈕**(owner 2026-08-01 裁定;數字為實測,server 側用
    database/sql driver seam 數 `chat_message` 讀取):
    | k(被指名且在手上的成員數) | 逐項 | 清單 |
    |---|---|---|
    | 1 | 1 GET + **1** 次全表掃描 | 1 GET + 1 次 |
    | 3 | 3 GET + **3** 次 | 1 GET + 1 次 |
    | 8 | 8 GET + **8** 次 | 1 GET + 1 次 |
    ⇒ k=1 成本打平、payload 較小 ⇒ 逐項贏;k≥2 逐項線性放大而清單**恆為 1+1** ⇒ 清單贏。
    因此**任何 k 都不比改動前差**。根因是 `unreadCountsForRequest` **每個請求各跑一次
    `ListChat()` 全表掃描**,所以逐項的成本是 k 倍、不是 k 個小請求。
    🔴 **k≥2 不是邊角情況,而且不需要 burst coalescing**:一則 chat delta 帶
    `{id, from, to}`(`api_chat.go`),`toSseDelta` 五個欄位全收,而 hub 對
    owner/dashboard 連線(`MemberID==""`)是**全量投遞**(`hub.go`)⇒ 控制台收得到
    agent↔agent 的訊息,**單一 SSE frame 就同時指名兩個名冊成員**。agent 互相講話
    在這個產品裡是常態。(反面:分開三個 tick 的三則 delta 是三個 k=1 的 burst,
    改動前也是 3 次清單 GET ⇒ **那條路沒有回歸**,放大只發生在單一 burst 內。)
    哨兵 `sseFanout.test.tsx` 的「ONE agent-to-agent line …」——**用真的會發生的形狀當
    fixture,不是人造的多成員 burst**。
    ⚠️ **這條哨兵的契約已經被下面那顆推翻過一次**:它原本叫「… names TWO members —
    that re-pulls the LIST, not two reads」並斷言 `listMembers === 1`;現在那則 delta
    **一個請求都不該發**,測試改名為「… moves no badge here — so it costs the roster
    ZERO requests」、斷言 `listMembers === 0`。**舊的 mutant 紀錄(「拿掉 `k>1 →
    full()` → 整套恰好這 1 條紅」)因此已失效**,新的三顆記在下面那張表。

    🔴 **但上面那句「agent 互相講話在這個產品裡是常態」只講對了一半,而漏掉的那一半
    會讓下一個人以為這條路已經解完了——它是常態的「浪費」,不是常態的「需求」。**
    `UnreadCounts`(`server/ocserverd/domain.go:411-425`)只數
    `m.Recipient == reader` 的訊息(它自己的 doc comment 就寫明「Messages between two
    other participants never count」),而控制台這份 roster 的 reader 是 **owner**;
    owner **不是名冊列**(single-owner schema,沒有 owner 的 member row),所以
    `heldRef` 永遠不含它。把這兩件事接上 `narrowToHeld` 就得到:

    | 真實形狀(`chat`:ids = {msgId, from, to}) | 落在名冊上的 k | 對這份 roster 有事做嗎 |
    |---|---|---|
    | member → owner | 1 | **有**(badge +1) |
    | owner → member | 1 | **沒有**(recipient 是那個成員,不是 owner) |
    | member ↔ member | **2** | **沒有** |
    | member ↔ `ow-` worker | 1 | **沒有**(worker 不在名冊上,且 recipient≠owner) |

    | 真實形狀(`chat_read`:ids = {reader, peer}) | k | 有事做嗎 |
    |---|---|---|
    | reader = owner | 1 | **有**(badge 清掉) |
    | reader = member / peer = member | **2** | **沒有** |

    ⇒ **`k ≥ 2` ⟹ 兩端都不是 owner ⟹ 這則 delta 動不了這份 roster 上的任何一個
    badge ⟹ 語意上是 no-op。** 而且「會動 badge」⟹「有一端是 owner」⟹ `k ≤ 1`,
    所以**真的有事做的情況恆為 k=1**。
    ⚠️ **反過來不成立,別把它讀成雙條件**:`k = 1` **不**蘊含「有事做」——上表第 2、4 列
    都是 k=1 而且什麼都不該做。
    (這一點與獨立驗證回報的表略有出入,以本表為準:那份表把「owner ↔ member」整列記成
    「會動」,但只有 member→owner 那個方向會。)

    🔴 **所以正解是 0 個請求,而四個實例已經全部收完**(owner 2026-08-02 裁「順手做掉」
    → 補裁「一起做完」;`useMembers` 隨 T-8115 進主幹,其餘三處 T-b17f)。述詞現在只有
    **一個家**:`lib/ownerUnread.ts` 的 `couldMoveOwnerUnread` / `burstMovesNoOwnerUnread`,
    由 `useMembers` / `useChatUnread` / `useOutsourceWorkers` **三個 hook 共用**——同一條
    不變量抄三份必然會各自漂移。整陣 delta 沒有一則能動 owner 的未讀數 ⇒ **直接 return,
    一個請求都不發**(不是清單、也不是逐項)。判斷所需的資訊本來就在 delta 上
    (`toSseDelta` 留著 `from`/`to`/`reader`/`peer`),不必問 server。
    ⚠️ **兩個 topic 的述詞欄位不同**:`chat` 是 `from`/`to`、`chat_read` 是
    `reader`/`peer`。只檢查一對,會對另一個 topic 的**每一則** delta 都答「沒有 owner」
    ⇒ 把真的有事做的那些也跳過。**mutant 實測(2026-08-02)**:把 `chat_read` 那條
    改成恆 `false` → `sseFanout.test.tsx` **恰好紅 2 條**(都在 read-echo 那組);把
    `chat` 那條改成恆 `false` → **紅 5 條**(含外包 rail 的 `ow-1 → owner` 對照)。

    🔴 **述詞是 T-b17f 收到最緊的那一版:`chat` 只認 `to === owner`、`chat_read` 只認
    `reader === owner`。** 舊的寬鬆版(「owner 在任一端」)另外浪費兩種 k=1 的逐項 GET,
    而兩者都不是對稱的雜訊、各有各的理由:
    - `chat` 的 `from === owner`——**owner 自己送出**的訊息,recipient 是對方,
      owner 對那張卡的計數不動(`UnreadCounts` 的 doc comment 自己就寫了
      「neither do the reader's own sends」)。
    - `chat_read` 的 `peer === owner`——**別人讀了 owner 的訊息**,推進的是**他的**
      watermark,而 watermark 是 per-reader 的。
    ⚠️ 上表第 4 列(member ↔ `ow-` worker)**在寬鬆版就已經是 0** 了(兩端都不是 owner),
    別把它算進收緊的收益——收緊真正省到的是上面那兩種。

    **實測(同一份 harness 母體、改前改後當下各量一次;40 列名冊 + 8 個 worker)**:

    | 形狀 | 改前(請求/位元組) | 改後 | 省在哪個 hook |
    |---|---|---|---|
    | a2a `chat`(m→m) | 1 / 1 B | **0 / 0** | ① `useChatUnread` |
    | a2a `chat_read`(reader/peer 皆 member) | 1 / 1 B | **0 / 0** | ① |
    | `owner → member` chat | 2 / 202 B | **0 / 0** | ② 收緊 + ① |
    | `chat_read` reader=member, peer=owner | 2 / 202 B | **0 / 0** | ② 收緊 + ① |
    | `member → ow-worker` chat | 2 / 145 B | **0 / 0** | ③ `useOutsourceWorkers` + ① |

    對照組**逐位元組不動**:`member → owner` 2 / 202 B、`chat_read` reader=owner
    2 / 200 B、`ow-1 → owner` 2 / 145 B、混合陣 4 / 8,212 B(含 1 次清單)、
    resync 9 / 11,083 B。
    ⚠️ **這些位元組是 harness fixture 尺度,不是正式站尺度**,而且**位元組不是這顆的
    價值所在**:`getChatUnreadCount` 的回應只有 1 個位元組(一個數字),但它在 server
    側是一次 `ListChat()` **全表掃描** + 一次名冊 + 一次 worker 清單。**價值在往返次數
    與 server 端的掃描,不在下載量。**
    🔴 **這四個實例已經全部收完,但「控制台 0 請求」只對這些形狀成立。** 別把它讀成
    「任何 delta 都不再打 API」——真的會動 badge 的照舊全打(見上面的對照組)。

    🔴 **`k > 1 → full()` 沒有刪 —— 而且它是混合陣的熱路徑,不是 fail-safe。**
    ⚠️ **本檔上一版把它寫成「生產上不可達的 fail-safe」,那是錯的,而且錯的方向會害人
    刪掉活碼。** 那個推理(「還走到那裡的每一陣都有一端是 owner ⇒ k ≤ 1」)**對「一則
    delta」成立,對「一陣」不成立**:`narrowToHeld` 讀的是 `batch.ids`,**整陣的聯集**
    (`lib/deltaSink.ts`)。**混合陣**(一則 agent↔agent + 一則給 owner,落在同一個
    microtask)就指到三張手上的卡 ⇒ **k = 3,就在今天**。
    **它的守衛是那條混合陣 CONTROL 測試**:刪掉該分支 → 它紅(`expected undefined to
    be 1`),而那一陣裡真的有一則給 owner 的訊息、roster 卻完全沒重抓。混合陣不是測試
    產物 —— `deltaSink.ts` 自己的檔頭就寫著它**刻意**合併同一個 tick 的**真** delta。
    (**沒有量過**的是 wire 實際多常把兩個 chat frame 送進同一個 microtask,別宣稱頻率。)
    🔴 **這個坑會反覆出現,記住它**:`sseFanout.test.tsx` 在它的 k 測試正上方花了 35 行
    講的就是**一陣 ≠ 一則**,而我們**隔一顆 commit 就在 `useMembers.ts` 裡踩了進去**。
    每次推理 k,先問手上拿的是哪一個:**per-delta** 的述詞(`couldMoveOwnerUnread`)
    還是 **per-burst** 的聯集(`touched`)。`lib/ownerUnread.ts` 的檔頭把這一條再寫了
    一次,因為那支檔案就是最容易被誤用成 per-burst 過濾器的地方。

    **跳過是整陣判斷、不是逐則過濾**:混合陣仍帶**全部** ids 走下面的分支,所以真的有
    事做的那一半永遠不會被吃掉;代價是 k 可能被撐大、混合陣走清單而非逐項——**永遠
    正確,偶爾不是最省**,而那是安全的方向。

    ⚠️ **兩件已知的誠實性瑕疵,刻意不修**:
    1. **哨兵的 fixture 讓 agent↔agent 之後 badge 變 6 / 2,那是真 server 產不出來的狀態**
       ——`UnreadCounts(reader=owner)` 對 `m-other → m-third` 這則訊息兩邊都不加。
       以這個檔案自己的教義(**假 api 不得比真 server 慷慨**,見下一節)來說這是瑕疵。
       **它現在反而是那條測試值斷言的鑑別力來源**:跳過失效時清單會被拉,那組真 server
       產不出來的值就會被採用、值斷言跟著紅。但**別把它讀成「badge 應該長這樣」**。
    2. **那條 `PREMISE` 斷言(`delta.ids ∩ held`)是文件,不是守衛。** 獨立驗證實測它
       對兩顆 mutant 都零鑑別力;而**結構上的理由比實測更強**:`delta` 是測試裡的區域
       字面值、`held` 來自 mount,**兩者都不依賴那個 k 分支**,所以不論 hook 怎麼改它
       都不可能紅。**不要把它算進覆蓋。**

    🔴 **判準是請求數,不是 badge 值** —— 「badge 沒變」在改動前後**都**成立(那則 delta
    本來就改不了任何值),拿它當判準會寫出一條恆真的斷言。所有這些測試的判準都是
    `h.counts`;值斷言只當佐證,而且只有在 fixture 報出**真 server 產不出來的值**時
    才有鑑別力。**mutant 實測(2026-08-02,T-b17f 全部重跑過一遍,不是沿用舊紀錄;
    每顆還原都用 scratchpad 備份 + shasum 對帳、未用 `git checkout --`)**:

    | mutant | `sseFanout.test.tsx`(18 條) |
    |---|---|
    | 拿掉 `useChatUnread` 的 `burstMovesNoOwnerUnread` 跳過 | 🔴 **5 條**(a2a、T-b17f ①②②③;皆 `expected 1 to be +0`) |
    | 述詞放寬回「owner 在任一端」(`\|\| from` / `\|\| peer`) | 🔴 **2 條**(T-b17f ② 兩條) |
    | 拿掉 `useOutsourceWorkers` 的跳過 | 🔴 **1 條**(T-b17f ③) |
    | `chat_read` 那條述詞恆 `false`(過度跳過的方向) | 🔴 **2 條**(read-echo 那組) |
    | `chat` 那條述詞恆 `false`(過度跳過的方向) | 🔴 **5 條**(含外包 rail 的 `ow-1 → owner` 對照) |
    | 刪掉 `k > 1 → full()`(混合陣的熱路徑) | 🔴 **1 條**(混合陣 CONTROL,`expected undefined to be 1`) |

    ⚠️ 前三顆各自有**一條專屬**的測試(T-b17f ①/②/③),但它們的斷言不是互斥的——
    ② 與 ③ 那幾條同時斷言 `getChatUnreadCount === 0`,所以①的 mutant 也會打紅它們。
    **真正一對一的是每顆 mutant 的最小殺傷集**:①→ ① 那條、②→ ② 那兩條、③→ ③ 那條。
  - `useTasks` — **不可以**逐項,走清單。`GET /api/tasks/{id}` **整個 wire 上沒有
    `dep_tasks`**(凍結 spec 只把那個 server-side dep join 放在 `TaskListItemDTO`;
    `toTask()` 因此不設 `depTasks`,`toTaskListItem()` 逐字帶過)。而 `TaskCard` 把
    「沒有人解析這個 dep」與「查無此任務」畫成**兩種不同的東西** ⇒ 用單筆換掉一列會讓那張
    卡的每一條 dep 退化成裸短編號(T-a3e4 的「已結案的 dep 仍講得出標題」直接消失)。
    🔴 **同一個回歸在 render 層有第二條路,而且它承重、反直覺**:展開的任務卡手上
    **同時有兩個 TaskView**(`TaskCard.tsx` 的 `const view = hasDetail ? detail : task`)
    ——`task` 是清單列(**有** `depTasks`)、`view` hydrate 後是 `GET /api/tasks/{id}`
    (**沒有**)。dep 那段刻意讀 `task`,而它周圍的欄位(artifacts、steps、description)
    全讀 `view`。**把那一行改成 `view.depTasks` 就等於把回歸② 從 hook 層搬到 render 層,
    使用者看到的東西一模一樣**。2026-08-01 實測:改動前整套 1675 條**全綠**——全部
    dep 測試都傳 `NOOP` 當 `onHydrate`,所以 `hasDetail` 永遠是 false、`view === task`,
    **一條沒展開卡片的 dep 測試對這類 bug 完全是盲的**。哨兵
    `TaskCard.dep-after-hydrate.test.tsx`(展開 + 用 `projectSingleItem("task", row)`
    當 hydrate 回傳值,斷言 dep 仍講得出標題與狀態;同一顆 mutant 現在恰好紅這 2 條)。
  - `useChatUnread` — 一個總數,沒有「只抓一項」的版本;吃 coalescing **與**
    `burstMovesNoOwnerUnread`(T-b17f)。它是 Σ `UnreadCounts(…, owner)` over 活著的
    成員 ∪ 外包(`api_chat.go:873`),所以「不是寄給 owner 的 chat」「reader 不是 owner
    的 chat_read」動不了它一個單位。**但跳過只在整陣的 topic 全是 chat/chat_read 時才
    成立**:`member` / `outsource_worker` 改的是**活著的那個集合本身**(移除一個成員會
    讓他殘留的未讀退出這個和),那兩個 topic 照舊無條件重抓。
  ⚠️ **剩下的那個缺口補不在 client**:`dep_tasks` 是凍結 wire 沒有的欄位,要它就得
  **動 spec**(additive-optional;root §12 DTO 條:加欄要先問 owner),**還在等裁定**。
  在那之前**不要「順手」把 `narrowToHeld` 接回 `useTasks`** —— 那個編譯期 pin
  (`TaskDTO` 沒有 `dep_tasks`)就是為了讓「以為加好了」立刻變成 tsc 紅。
  ⚠️ **members 那格的教訓要留著**:單筆端點「有宣告這個欄位」不等於「它會算」。
  加任何一條逐項路徑之前,先讀 `api/dtoParity.ts`,並且**去看那支 Go handler 真的填了什麼**。
- 🔴 **自激路徑:讀取本身是一次寫入。** `GET /api/chat?with=`(列表即讀)會推進
  watermark,server 於是**把 `chat_read` 扇回同一個 client**。所以在**別人的**對話有
  delta 時去 `load()` 開著的那個 thread,不只是白抓一次——它**無中生有製造第二輪事件**,
  而且公司裡任何一條聊天訊息都會來一次。⇒ `useChat` 的 `chat` 分支**先看 delta 指的
  from/to 是不是這個 peer**;`chat_read` 分支**只認 `reader === peer`**(`peerLastReadTs`
  只可能被 peer 的 watermark 推動,自己那份 echo 永遠不會改到它)。名字空的照舊無條件重抓。
  ⚠️ 它**本來就會停**(那一輪裡沒有人再打一次列表即讀,而 `PutChatRead` 只在真的前進時才扇
  ——`dal.go` + `server_test.go`),所以這不是無窮迴圈,是**每則訊息固定多一輪**的放大。
- **實測(改前 → 改後,六個 hook 同掛的控制台,單一 delta 造成的請求數,不含 mount)**:
  別的對話一則聊天 **5 → 2**;自己讀取的 echo **4 → 2**;一次 resync **21 → 9**。
  ⚠️ **這幾個數字量的是「請求次數」,而逐項 vs 清單在次數上是一樣的(都是一個 GET)**
  ——所以上面那些數字**不因為 members/tasks 改走清單而變**,變的是那一個 GET 的 payload
  大小(以及 task delta 那格會多一次 `listOutsourceWorkers`,因為 useTasks 的全量路徑
  順便重抓 worker roster——那是改動前就有的成本)。**別把「請求變少」讀成「payload 變小」。**
  `/api/settings` 在**改前改後都是 0**
  ——ad74682 的共享快取之後,任何 delta 都不會再碰它(見下一節),票面上「外包/任務/聊天
  事件在重拉 626 kB 設定」這句**在 main 上已經不成立**,實測坐實、不是推論。
- 護欄:`hooks/sseFanout.test.tsx`(六 hook 同掛的成本 + **值**雙斷言)、
  `lib/deltaSink.test.ts`、`api/http.sse-delta.test.ts`、**`api/dtoParity.test.ts`**。
  🔴 **每條成本斷言都配一條值斷言**:「請求變少」對一個乾脆不更新的 hook 也成立,
  所以那份測試同時釘住 delta 真的指到的那一列上、**server 說的那個值**。
  🔴 **而值斷言只有在假 api 不比真 server 慷慨時才算數**——見下一條。
- 🔴 **值斷言只有在假 api 不比真 server 慷慨時才算數——這條是這批修補的真正教訓。**
  第一版的兩個回歸(roster badge 歸零、任務卡 dep 退化成裸編號)**通過了 tsc、1670 條
  jsdom、CT 與 frame 探針**,因為 `sseFanout.test.tsx` 的手寫假 api 拿**清單列**回答
  `GET /{id}`:那個 wire 不存在,於是值斷言量的是一台不存在的 server。反過來
  `api/mock.ts` 一直是對的(它的 `getTask` 早就寫死 `depTasks: undefined` 並註明理由)
  ——**繞過共用 mock 自己手寫假貨,就是繞過那份已經校準好的知識。**
  ⇒ 現在單筆端點的落差集中在 `api/dtoParity.ts` **一份表**,`projectSingleItem()` 供
  測試建假貨用,hook 測試的三個單筆 getter 全部走它。
  🔴 **但「三個 getter 都走 projectSingleItem ⇒ 構造上不可能比 wire 慷慨」這句話,對
  member 與 task 兩格目前是空的——別把它當成現行防線**(2026-08-01 實測:把
  `sseFanout.test.tsx` 那兩個 fake 都改回裸 `return found`,也就是拿清單列回答
  `GET /{id}`、正是原始回歸的成因形狀,`dtoParity` + `sseFanout` **14 條全綠**)。
  兩格惰性的理由不同,而且都是結構性的:
  - **member**:`PER_ITEM_DTO_GAPS.member` 已經清空(server 修好了),所以
    `projectSingleItem("member", …)` 就是 **identity**,改不改沒有差別。
  - **task**:`useTasks` **根本不再呼叫 `getTask`**(逐項路徑已拿掉),所以那個 fake
    再慷慨也**沒有消費者**。
  ⚠️ **這不等於「這裡沒有守衛」**——守衛在,只是不在 fake 那一層。真正擋得住的是**三道**,
  每一道都實測過 mutant(還原用備份,未用 `git checkout --`):
  1. **`server/ocserverd/api_members_unread_parity_test.go`**(Go,斷言 **response body
     裡的數值**、不是「有沒有呼叫某支函式」)——把單筆 handler 改回 literal `0` 就紅
     (`served unread_count 0, want 2` + `single-item (0) and list (2) disagree`);
     把 `unreadCountsForRequest` 的 reader 寫死成 owner,per-caller 那條紅。
  2. **`api/dtoParity.test.ts` 對 `api/mock.ts` 的 parity**——把 mock 的 `getMember`
     改回不算 unread(mock 比 server **小氣**,同一類謊話的反方向)就紅;
     讓 `getTask` 留著 dep join 也紅。
  3. **`api/dtoParity.test.ts` 的編譯期 pin**(`TaskDTO` 沒有 `dep_tasks`、
     `TaskListItemDTO` 有)——把它改成「`TaskDTO` 有」**tsc 直接紅**。②(dep join)
     那半目前**只**靠這一道,所以別把它當可有可無的裝飾。
  ⇒ **要加任何一條新的逐項路徑,先把對應那格的 `PER_ITEM_DTO_GAPS` 與上面三道一起看**:
  fake 那層的保護會隨著 gap 清空 / 消費者消失而自動失效,**它不會有人通知你**。
  ⚠️ **三道都抓不到的方向**:server **自己**改了(例如哪天單筆又不算 unread、或反過來
  `TaskDTO` 真的長出 `dep_tasks`)。第 1 道是 Go 側,對 server 的**這一個**欄位守得住;
  但「表上還有哪些 gap 已經過期」整體而言只有跑真 ocserverd 的 conformance 級斷言
  (單筆 vs 清單對帳)才守得住,**那是還沒做的事,別把這份 guard 當成它。**

## /api/settings 只讀一份;`onboarding: null` 是終態(T-8115)

`GET /api/settings` 在正式站是 **639,270 bytes**(gzip 後 373 kB;`custom_themes`
一欄佔 626,721 = 98%,其餘 15 欄合計約 2.5 kB)。它同時是**六個**互不相識的
mount-fetch 消費者的來源,所以那份 payload 一次控制台載入被下載六遍。

- **唯一入口 `hooks/sharedServerSettings.ts`**(核心在 `lib/sharedSnapshot.ts`):
  合併(single-flight)+ 快取 + 世代守衛。**mount-fetch 一律走 `loadServerSettings()`,
  不要在新的地方直接叫 `api.getServerSettings()`** —— 那正是這張票在收的東西。
  現有六個消費者:`useOrgName` / `useOwnerName` / `useServerSettings`
  (`SettingsPage`、`MonitorPage`、`DocumentHistoryEntry` 三處 mount)/
  `useOutsourceWorkers` / `i18n` 的登入 reconcile / `OnboardingBanner` 首讀 /
  `PushNotifications`、`ProfileDropdown` 的 mount 路徑。
- 🔴 **快取什麼時候失效,只有三個答案**:(a) **本分頁自己存檔成功** →
  每個 `patchServerSettings` 的 echo 都要 `adoptServerSettings(echo)`(新增 PATCH
  呼叫點時**一起加**,漏掉就是畫面停在存檔前的值);(b) **身分改變**(登入 /
  auth-expired 事件)自動 invalidate;(c) `refreshServerSettings()` —— 給
  **不准讀記憶**的兩個呼叫點:onboarding 輪詢(它就是要看值變)與 設定 的
  存檔測連通 read-back(它就是要證明 server 同意)。**沒有 TTL。**
- ⚠️ **已知且 owner 已知情的邊界**:settings 改變時 server **不發任何即時通知**,
  所以另一個分頁 / 另一台裝置的存檔在這裡看不到,直到重新載入。**不要假裝解決了**;
  要真解決得先加 SSE topic(動凍結 wire)。
- **世代守衛不是裝飾**:存檔前發出、存檔後才回來的那個 GET 會把剛存的值蓋回去。
  request 記住自己出發時的 generation,`adopt`/`invalidate` 把 generation 推進,
  過期的回應只回給自己的 caller、不寫快取。
- **測試面**:`src/test/setup.ts` 在每個 test 之間 `resetAllSharedSnapshots()`
  ——module-level 快取會把上一條 test 的 fixture 餵給下一條。它從 `lib/` 匯入
  (**不是**從 hooks 那支),因為 setup 檔跑在測試檔自己的 `vi.mock("../api")`
  註冊**之前**,從 hooks 匯入會把 api 層先拉進 registry。

**`onboarding: null` 是終態——而這是與 server 的成對契約(T-8115)。** DTO 明訂 null 是
「onboarding never ran」的**正常值**(舊安裝、或建庫時就有密碼),正式站正是 null;
舊碼的 `isTerminal(null) === false` 讓每次開控制台輪滿 3 分鐘 = **61 次** × 373 kB
(這個數字是 mutant 讓測試自己印出來的,不是推的)。
- **憑什麼敢把 null 當終態**:`kickFirstRunOnboardingWith`(`server/ocserverd/onboarding.go`)
  在**開 goroutine 之前**就把 `running` 報告寫進 DB,所以那一列在
  `POST /api/auth/set-password` 的 handler **return 之前**就存在,而該回應是 return 才
  flush ⇒ **拿到那個 200 的 client 之後讀 settings 一定看得到報告**。它其餘四條 early
  return 代表 onboarding 根本不會跑、而且不重試 ⇒ **client 看得到的 null 只有一種**。
- 🔴 **這是成對的,改一邊要看另一邊**。server 那半的護欄是
  `server/ocserverd/onboarding_contract_test.go`(`TestOnboardingClaimIsPersistedBeforeKickReturns`
  + `TestSetPasswordLeavesNoNullOnboardingWindow`);FE 這半是
  `OnboardingBanner.null-poll.test.tsx`。把認領搬進 goroutine,server 那條會紅——
  那正是它存在的理由。
- 🔴 **不准改成只抓一次**:首次安裝的失敗結果在 t≈30s(`wardenOnlineWait`)才落地,
  那正是這個橫幅唯一存在的理由。首讀讀到的是 `running`(見上),而 `running` 是非終態,
  輪詢照舊跑到 180 s 天花板。
- **讀取失敗 ≠ null 報告**:catch 分支必須繼續輪——首啟開機期的短暫失敗正是它想有用的時候。
  三條測試各釘一件事(一次讀 / running→failed / 讀失敗仍續輪),三個 mutant 各紅一條。

## 首設密碼 + 伺服器設定(B3)
- **AuthGate 四態牆**(real mode only):有 token → App;無 token → 打 PUBLIC `GET /api/auth/status` 一次 → 未設密碼 = `FirstRunPage`(啟用碼 + 設密碼,POST set-password 成功即存 token 直接進 App;啟用碼從 `?code=` query 預填——server 首跑自動開的就是這條 URL,預填時 autoFocus 落密碼欄、code 讀到即 history.replaceState 從網址列抹掉)、已設 = `LoginPage`。mock mode 永不出牆(照舊直接進辦公室)。
- **ProfileDropdown 三 view**:main → preferences(主題/語言 + **伺服器設定**:登入有效期下拉 12h/24h/7d/30d、自動換手門檻 40–90%,經 api seam `getServerSettings`/`patchServerSettings` 即時生效)→ password(改密碼)。設定載入失敗 = 誠實不渲染該區塊。
- **⚠️ 密碼端點不走 openapi-fetch client**:client middleware 把任何 401 變成 clear-token + `oc-auth-expired`(登出彈跳)——打錯「目前密碼」/claim token 必須是 inline 表單錯誤,所以 `setPassword`/`changePassword` 走 http.ts 的 `credentialPost` 裸 fetch(丟同款 `ApiError`),成功後 `setToken` 換上 server 新發的 token(change-password 會撤銷所有舊 owner session)。settings GET/PATCH 照常走 typed client。

## effort / model:自報值 vs 設定值(兩個來源別混;T-e12c 之後界線更硬)

owner 2026-07-31:「成員面板以及監控台,一定要顯示回報回來的狀態,不能顯示設定值」。

- **自報值(狀態)**:`MonitoringSessionDTO.model` / `.effort` → `session.model` / `session.effort`。
  鏈路 = Claude Code statusLine payload 的 **`model.id`** / **`effort.level`** →
  `ocagent context-report` → `POST /api/monitoring/telemetry` → server 的 telemetry
  entry(key = token sub)→ monitoring session 列。兩者取的都是 **live** 值(跟得上中途
  `/effort` 與換模型),**不是** `OC_EFFORT` / `OC_MODEL` 那個啟動意圖,**而且沒有 fallback**。
  honest-empty `""` → UI 顯示「—」。
  - `model` 取 **`model.id`** 而**不是** `display_name`(狀態列上畫的那個):id 是 boot seed
    已經教成員回報的詞彙,也是**唯一**帶 `[1m]` 1M-context 標記的那個——`display_name` 對
    1M 與標準版都寫「Opus 4.5」,送它等於把兩種 session 併成同一個字串。
  - 🔴 **`model` / `runtime` / `effort` 三者都有持久層(T-7f28 起對稱)**:server 除了寫
    telemetry entry,還會在值改變時落進 roster row 的 `actual_model` / `actual_runtime` /
    `actual_effort`(`stampReportedLaunchFacts`)。telemetry 是 in-memory,只靠它的話
    server 每次 re-exec 就把全 fleet 清空。**所以正職與外包的三欄都是「上一次回報的值」,
    活得比 session 久**,而且**都不退回設定值**——退回設定值會讓「改了還沒生效」與
    「已經生效」長得一模一樣,那正是 T-7f28 要修的東西。
  - **codex runtime 由 sidecar 送**(`cli/ocwarden/codex_session.go`),不是 statusLine ——
    那條 runtime 沒有 Claude Code 的狀態列。
- **`GET /api/monitoring` 的 sessions 現在同時含正職與外包**(T-e12c);外包列靠 **`ow-` id
  前綴**辨識(server 沒有、也不該新增 kind 欄位——凍結 wire)。`MonitorPage` 的外包列因此
  用 `findSessionFor(worker.id, sessions)` 取 model/effort/context/cost/**machine/account**,
  **join 不到就一律誠實留白**,絕不退回 `GET /api/outsource-workers` 的設定值。⚠️ machine/account
  那兩欄 owner 2026-07-31 在卡上**明知代價仍選擇這樣**(`rc-4a83a5723896` ①):worker 剛被派出去、
  還沒連上的那段,機器欄就是**空白**——那不是 bug,**不要「修」成退回 worker DTO**,那份 machine
  是 in-memory 的 dispatch target(意圖),不是觀測到的落點。member 那條 lane 同時用
  `ow-` 前綴排除,否則同一個 session 會畫兩列。
- **設定值(啟動意圖)**:roster 的 `member.model` / `member.effort`、外包 DTO 的
  `worker.model` / `worker.effort`。它只活在**兩個地方**:model/effort **編輯器**(seed 與
  存回都是它),以及描述「這張任務要用什麼開外包」的 TaskCard chip 與任務手冊預設值。
  🔴 **`AgentDetailPanel` 的 `configuredModel`/`configuredEffort` 是 required、且刻意不對
  readout 做 fallback**:readout 是遙測(或 `""`),讓它當退路 = 一次儲存就把自報值寫回
  owner 的設定,未回報時甚至寫進空值而被 closed vocabulary 422。
- **兩個詳情面板資訊卡的 模型/投入度 都是自報值,且都唯讀**:成員走
  `actualModel`/`actualEffort`(awake 才顯示,T-927a),外包走 `session?.model`/`.effort`
  (`findSessionFor(worker.id, sessions)`,與監控台同一條 join,T-7526 之後 OfficePage 也
  在開外包面板時才拉 monitoring)。設定值只出現在各自的 設定／更改 對話框,那裡 seed 自
  member/worker DTO、存回也是它。⚠️ 兩個面板現在都**不傳** `onSaveModelEffort`,所以
  `AgentDetailPanel` 裡那顆 in-place 編輯器沒有 production caller——要讓它復活前先想清楚
  T-7526 拆掉它的理由(同一個畫面出現兩個改同一設定的地方)。
- **缺值守衛(T-e12c)**:「還沒回報任何東西」與「正在回報別的東西、卻獨缺 effort」以前
  長得一模一樣(都是空白),故障因此偽裝成設計躺了很久。`isReportingTelemetry(session)`
  (online ∧ 至少有一個純遙測值:context% / cost / account)為真而 effort 為空時,
  `EffortBadge` 改渲染 `mon-stale` 那顆「這個空白有原因」的 chip(既有視覺語彙,不是警告色
  ——沒有東西壞掉,只是欠一個值);什麼都沒回報則維持乾淨空白。

## 監控 › 機器表:硬體與 Runtime 的時效(T-90be ⑤ + T-b36a)
`MonitorPage` 機器表有兩組會過期的 telemetry 欄,**兩組都必須連時效一起顯示**,理由是
telemetry 只在成員被解僱時清、**斷線不清**,所以資料會比回報它的機器活得久。
- **新鮮度的裁決在 server**(`hardware_stale` / `runtime_capabilities_stale` 兩個 wire
  bool)。FE **不准**拿 `hardwareTs` / `runtimeCapabilitiesTs` 跟自己的時鐘比去重推那個
  90s 窗——門檻只有一個家(server 的 `telemetryFreshSecs`),第二份必然會跟第一份各說各話。
  戳記欄留在 view model 是給人看的時間點,不是給 UI 算 verdict 的輸入。
- **CPU / RAM / 電源**:過期時 server 已把數值收回,所以格子落回 dash——但那跟「這台從來
  沒回報過硬體」是**同一個 dash**。`hardwareStale === true` 時三格各掛一枚 `mon-stale`
  標記(`data-testid="mon-hardware-stale"`)講清楚 dash 的原因。判斷式只准是 `=== true`:
  `false` 是活樣本裡誠實的缺值、`null` 是從沒量過,兩者都不准被標成過期。
- **Claude / Codex 兩欄各印版本(T-674d,取代舊的單一 Runtimes ✓/✗ 欄)**:共用
  `RuntimeVersionCell`,兩欄讀的是**同一份** `runtimeCapabilities`,**沒有新採集、沒有
  合成版本號**。舊的 ✗ 不准被「空格子」吃掉——空格子讀起來是「不知道」,那是另一個
  (而且錯的)主張;所以 `installed:false` → 「未安裝」、`loggedIn:false` → 版本號旁掛
  「未登入」chip(`mon-bad`)。四個誠實輸出:從未回報 → dash(title 從未探測)/
  `installed:false` → 未安裝 / 有版本 → 原樣印 / 已安裝但版本探測沒回話 → 「已安裝」。
  **Claude 多一條 registry fallback**:沒有 capability entry 時落回 `MachineView.claudeVersion`
  (registry 自 T-97ee/T-7c5b 就有的欄),讓舊 warden 的 Claude 欄語意不變;**Codex 沒有
  對應的 registry 欄,唯一來源就是 capability map**,缺就是誠實的 dash,不准借 claude 的值。
  時效紀律照舊:capability 來源的值在 `stale !== false` 時掛 `mon-stale`、**值不收回**
  ——「codex 三小時前沒登入」是 worker 卡在 `machine_unavailable` 唯一的解釋;registry
  fallback 不是 telemetry,不掛時效標記。
- **機器 + 狀態是同一欄(T-674d)**:兩欄拆開時,名字格窄到每一列的 machine-id chip 都
  被擠到第二行。合併後 name / id chip / online badge 同一個 `<td>`;`.mon-machine-name`
  只在 **desktop(min-width:721px)** `flex-wrap: nowrap`——≤720px 的卡片模式**不能**
  nowrap,那個模式刻意拿掉了 `.mon-table-wrap` 的 `overflow-x: auto`(見長 token 那節),
  沒有捲軸吸收的長 machine id 會把整頁推歪。
- 護欄:`MonitorPage.hardware-freshness.test.tsx`(過期標記 / 從未回報不標 / 真 0 與真
  false 仍正確顯示)、`MonitorPage.runtime-capabilities.test.tsx`(兩欄版本 / 未安裝 /
  未登入 / 過期 / registry fallback)、`MonitorPage.machine-id.test.tsx`(合併欄的結構
  不變量)。

## 長 token 溢出:單一來源在 `.doc-md` 基底(T-d451)
owner/agent 自由文字會帶**不可斷的長 token**(長 URL、40-hex sha、無空白長字)。
沒有斷點時它把容器 min-content 撐到 token 全寬,容器不肯縮、撐破手機視窗,**整頁**
就能左右滑。**修在 `.doc-md` 基底(`settings.css`)的 `overflow-wrap: anywhere`**,
17 處 render site 與**未來新增的**一起繼承——這是唯一來源,**別再逐 surface 貼**
(T-4974 就是逐處貼,結果同一個病從沒貼到的頁面復發,才有 T-d451)。
- `anywhere` 不是 `break-word`:兩者都斷已溢出的行,但**只有 `anywhere` 收縮
  min-content**,那才是容器肯縮回視窗的原因(flex/grid 宿主尤其吃這點)。
- **不渲染 markdown 的自由文字欄位收不到這個繼承**,要自己宣告(現有:
  `replies.css` 的 `.reply-option__text` / `.reply-card__answer-text`、
  `monitor.css` 手機卡片模式的 `.mon-table td`)。加新的純文字欄位時記得。
- **橫向滾動只允許出現在明確的可滾動子區**:`.doc-md pre`(`white-space: pre`
  使 `overflow-wrap` 對它無效,實測仍正常橫捲)與 `.doc-md table`。修這類問題時
  **不可**為了消滅整頁橫滑而拿掉它們的 `overflow-x: auto`。
- 護欄:`visual-guards/docmd-longtoken-wrap.ct.spec.tsx`(文件面)、
  `monitor-table-longtoken.ct.spec.tsx`(監控表格)、
  `taskcard-longtoken-wrap.ct.spec.tsx`(任務卡)。都是**雙向**契約:整頁不許滑
  **且** pre/table 仍要能滑——單向斷言會讓「修過頭」靜靜通過。
- ⚠️ 重驗 mutant 時當心**斷言互相掩護**:整頁那條先炸會中止測試,底下 per-surface
  斷言根本沒跑。要證明後者,先暫時放寬整頁斷言再跑 mutant。

## 固定高 ＋ 可壓縮 ＋ CJK 標籤 = 必須宣告 `white-space: nowrap`(T-5e79)

owner 截圖回報:判準卡的「預設」徽章,兩個中文字被折成上下兩行、**撐破**那顆膠囊;
同一張圖右側的「編輯」按鈕一樣。三個條件同時成立就會發生,**缺一不會**:

1. 元素有**固定高**(`.set-badge` 是 `height: 19px`、`.doc-btn` 是 `30px`),
2. 它是**可收縮的 flex item**(預設 `min-width: auto`,但沒有東西阻止斷行),
3. 標籤是 **CJK**。

機制在第 3 點:**中文沒有 `white-space` 宣告時,min-content 只有一個字的寬度**
——盒子可以被壓到比標籤還窄,文字於是折行,而固定高跟不上,就溢出。拉丁字的
min-content 是整個單字,所以**同一段 CSS 對 en 幾乎沒有作用**(見下)。

- **修法是宣告 `white-space: nowrap`,不是 `flex: none`。** 兩者在這裡各自都足夠
  (mutant 實測:只拿掉任一個,護欄都還是綠),所以同時加就是一條**沒有任何東西守得住
  的冗餘宣告**。留 `nowrap`:語意最直接,而且在非 flex 宿主也成立。
- ⚠️ **`.doc-btn` 只加 `white-space`、刻意不加 `flex: none`** —— 它有約 40 個 call site,
  收縮能力在別處是有用的。
- 🔴 **這類缺陷的護欄一定要真瀏覽器,jsdom 構造上零鑑別力** —— 它不套版面,
  `insight-default-badge` 在不在 DOM 兩種情況一模一樣。
- 🔴 **斷言要落在使用者看得到的幾何,不要斷言 CSS 屬性字串**:`getComputedStyle` 讀回
  `nowrap` 會被「屬性設了但被 out-cascade」與「不折行了但仍撐破盒子」兩種情況滿足。
  現行護欄用 `Range.getClientRects()` **數 line box**(必須剛好 1)、要求每個 line box
  留在元素自己的 border box 內,再加三層容器的橫向溢出 ≤1px(擋「用溢出換不折行」)。
- **護欄**:`visual-guards/set-badge-nowrap.ct.spec.tsx`(四個寬度 320/375/390/1040;
  1040 是控制組,對每顆 mutant 都綠、**不計入覆蓋**)。**mutant 與「斷言互相掩護」的
  完整紀錄寫在該檔檔頭**,不在這裡——它是那份量測的家。
- ⚠️ **`.mp-lessons__head` 的 `@media (max-width:359px){flex-wrap:wrap}` 是上面兩條
  nowrap 造成的新溢出的洩壓閥**,不是原缺陷的一部分:不再可壓縮之後,zh 在 320px
  會溢出 23px。斷點**刻意不用站上的 720px**——開了 wrap 之後標題會以 max-content
  參與斷行,375/390 那些**本來排得下**的一列會被推成兩行(列高 42→61px 實測),
  而 owner 就是在那些寬度看控制台。
- 🔴 **已知缺口:上面每一個數字都是 zh 量的,en 不一樣。** 英文在 **360–380px 仍會
  溢出**(實測 headOver 22 @360、7 @375、0 @390),因為 `nowrap` 對拉丁 min-content
  沒有作用。**那是既有缺陷、不是這包造成的**——pre-fix 樹量到**逐位元組相同**的 en
  數字,而 <360 那段本包反而把 en 修好了(320: headOver 62→0)。把閥門放寬到涵蓋 en
  會把 wrap 推進 360–390,正是那個斷點存在的理由要避開的範圍 ⇒ **是取捨,不是遺漏。**
  理由與量測寫在 `member-detail.css` 那段 KNOWN GAP。

## 浮層寬度不可用 `vw` 夾(T-49fb)
`100vw` 從**視窗左緣**起算。一個 `position: absolute` 的浮層若不是從視窗左緣長出來
(幾乎都不是——它從卡片內緣起算),`width: min(Xpx, calc(100vw - g))` 就是**錯的座標
系**:它夾住了寬度,卻沒有把浮層自己的左偏移算進去,右緣照樣可以出界。T-2ca0 就是
這樣留下 375px 溢出 +2px 的尾巴。
- 正確作法:讓**兩個橫向邊界都由容器給**——`left: 0; right: 0; width: auto`,再用
  `max-width` 收上限。可用寬 = 容器寬,右緣**構造上**等於容器右內緣;容器在視窗內,
  浮層就一定在視窗內,與視窗寬無關。over-constrained 時 LTR 忽略 `right`,靠左展開
  的行為不變。
- 量測紀律:**量會溢出的元素自己**,別量它的 flex 父容器 rect(父容器 rect 常被壓回
  視窗寬,看起來沒事)。逐層比 `scrollWidth - clientWidth`,溢出停在哪一層,兇手就在
  那一層裡面。
- ⚠️ **`documentElement` 沒溢出不代表沒 bug**:任何祖先只要有 `overflow-y: auto`
  (CSS 規定 `overflow-x` 跟著變 `auto`),就會把溢出**吸進自己的橫向捲軸**。任務頁的
  `.tasks` 正是如此——owner 看到的「整頁左右滑」其實是 `.tasks` 在滑。斷言要同時涵蓋
  `documentElement` 與那個 scroll container。
- ⚠️ CT 護欄**必須重現真實祖先鏈**(`.app__main` 的 22px padding 等)。裸掛一張卡片
  會多出 ~22px 餘裕,溢出就消失——`artifacts-badge.ct.spec.tsx` 舊的 390px 斷言就是
  這樣一路綠著,卻沒攔到 owner 手機上的 bug。見
  `stories/TaskArtifactsOverflowStory.tsx`。

## 用了哪份 CSS 的 class,就要自己 import 那份 CSS(T-7526)
`machine-picker.css` 全 repo 只有 `MachinePicker.tsx` 一個 importer,而它只透過
`WorkerDetailPanel → useRelocateMachine → MachinePicker` 進入 module graph。**兩個詳情面板
都用 `.machine-picker*` 畫自己的設定 dialog,卻都沒有 import 那份 CSS** —— 一直在搭那條
transitive import 的便車。外包面板不再驅動那個 hook 的那一刻,最後一個 production importer
跟著消失,**兩邊的 dialog 全部變成無樣式**:沒有置中的框、沒有暗底,機器欄變成瀏覽器原生
`<select>`。**連沒被那次改動碰到的正職面板也一起壞了。**
🔴 **沒有任何自動檢查抓得到**:jsdom 不算 CSS,所以整套 vitest 全綠;`tsc` 看不出 class
字串和 stylesheet 的關係;唯一render 過 machine picker 的 CT guard 又在同一張票裡退場。
**它是靠人看截圖發現的。**
⇒ 護欄 `src/components/styleOwnership.test.ts`:某個元件的 markup 用到 `<block>__*`,
就必須自己 `import "./<block>.css"`。**樣式的所有權跟著 class 名字走,不跟著 transitive
import 的偶然走。**

## lazy fetch:別把 inline arrow 放進 effect deps(T-7526)
`AgentDetailPanel` 的初始 PROMPT 卡曾經**永遠停在「載入中…」**,而且關掉重開救不回來。
兩個成因缺一不可,修的時候也必須兩個都修:
- **不穩定的 deps**:`vm.prompt.fetch` 在兩個 wrapper 都是**每次 render 重建的 inline
  arrow**(正職是 `async () => (await api.getBootstrap(member.role)).context`,外包是
  OfficePage 重建的 `onFetchBootContext`)。它一進 deps,**任何一次重繪**(一個 SSE
  delta 就夠)就把 effect 拆掉,cleanup 的 `alive = false` 讓 `.then` 與 `.catch`
  **兩條都寫不了 state**。⇒ 讀取函式走 **ref**,deps 只留真正該重讀的東西(換一個
  agent = `cacheKey`);重繪不是重讀的理由,也不是取消的理由。
- **在「開始讀」時就蓋已載入章**:`loadedKeyRef` 原本在 fetch **啟動**時就寫,所以
  effect 重跑一律早退——收合再展開也早退。⇒ **只在文字真的到手時蓋章**,in-flight 另
  用一個 ref 擋重複發射;過期與否**比對 key**,不用會被重繪翻掉的 `alive` flag。
- **失敗要說失敗**:`.catch` 必須落到錯誤態 + 一顆重試鈕(`*-prompt-error` /
  `*-prompt-retry`),停在「載入中…」會被讀成「還在跑」而且無處可按。
⚠️ **測試要「讀到一半觸發重繪」才看得到這個病**:render 一次就斷言的測試對它完全是盲的
(它就是這樣上線這麼久沒被抓到)。而 `rerender` **必須傳一個新的 element**——傳同一個
element 物件 React 會 bail out、根本不重繪,測試會對著沒修的碼變綠。
護欄:`MemberDetailPanel.initial-prompt.test.tsx` + `WorkerDetailPanel.test.tsx`
的 initial-prompt 段。**同一段程式,但兩個 wrapper 各自證明三件事**(重繪中、失敗重試、
收合再展開)——它們的 `vm.prompt.fetch` 是兩條不同的 arrow(正職在 vm 物件字面量裡、
外包經 OfficePage 的 prop),只證一邊等於沒證另一邊那條線。
mutant 紀錄:`docs/design/worker-panel-parity-mutants.md`。

## 兩個詳情面板的動作列 = 一個形狀(T-7526, owner 2026-07-31)
身分卡右上角**永遠是一列**:`.mp-identity__actions`(column,只裝 `DispatchAlert`)
裡面包一個 `.mp-identity__buttons`(row) —— **更改 ＋ 停止** 並排,沒在跑的時候整列收成
一顆 **喚醒**。正職外包同一份 CSS、同一個順序(更改在前)。
- ⛔ **改這一列的 flex 方向時,≤720px 的 media query 要一起想**。舊規則是為了撐開一個
  **column**;原封不動套在 row 上,`justify-content: flex-end` 會讓兩顆鍵擠在右邊界
  ——「東西還在、但擺錯了」,而且**每一條「同一列/沒溢出」的斷言都還是綠的**。
  護欄因此量的是**跨距與均分**,不是存在性:`visual-guards/identity-actions-row.ct.spec.tsx`。
- **喚醒 = 先開設定再送,兩邊都是**。外包的喚醒以前是 `POST …/restart` 直接派工、什麼都不問;
  現在開的是與 更改 **同一份** dialog,四格(執行環境/模型/投入度/機器)預設成**它原本的值**、
  **且都可以改**。落地順序 `/model` → `/relocate`(機器有改才打) → `/restart`,**全是既有端點**。
  ⚠️ `/restart` **不吃 machine_id**,所以釘選只能由 relocate 寫 —— 這是外包與正職(它的
  `activate(machineId)` 自己帶機器)唯一的形狀差異,別把兩邊的順序抄來抄去。
- 🔴 **釘住的機器只是「睡著」時,seed 一律逐字保留那一台**,不准 fallback「第一台線上的機器」。
  否則 `machineChanged` 對一個停在睡著機器上的 agent **恆為 true**,開設定只想改模型的人會被
  默默搬走。兩個面板的 `openSettings` 都有這條,**改一邊要改兩邊**。
  ⚠️ 測這一條時 fixture **必須有一台線上機器**,否則那個 mutant 無處可去、測試在壞碼上照樣綠。
- **外包沒有「只儲存,不喚醒」,而且這是刻意不對齊**:正職的「只儲存」是 PATCH ＋
  placement-only relocate,都不啟動;外包的 relocate **會 kill + re-dispatch**(除非
  `desired_state` 已是 offline),所以那顆鍵對外包會是假話。要有它得新增 pin-only 端點＝動凍結 wire。
- **外包沒有「狀態」卡**(owner:「外包為什麼需要工作狀態這個UI介面」):五個狀態字裡四個是
  `LifecycleDot` 的複述,「已釋放」由聊天室橫幅承擔。**但離線原因(`worker-detail-stuck-reason`)
  留著**,搬到那顆點下面 —— 它不是狀態字的複述,而且 `最近操作` 卡是 `hasLastOp` gated 的,
  「一次都沒派出去」正好是它不渲染的情況。
- **「重啟」這個字已退場**,兩邊一律 `t.lifecycle.action.spawn`＝「喚醒」。
  ⛔ **REST 路徑仍是 `/restart`**(凍結 wire),`api.restartWorker` 也不改名 —— 退場的是字,不是契約。
- 🔴 **「已結案(released)」由身分那一層講,兩個入口同一句話**(owner 2026-07-31:
  「為什麼從不同進入頁面會有不同的顯示方式?不是應該要一致嗎」)。released worker 被 server
  從 LIVE 名單濾掉,所以只剩**兩個**入口看得到它:**聊天室**與**直接開的詳情面板**。
  - 文案只有一個家:`office.outsource.releasedTitle` / `releasedSub`。
    ⛔ **不准為某一邊加第二份字串** —— 舊名字是 `releasedChatSub`,那個 `Chat` 就是病灶。
    措辭必須**與入口無關**(「以下為歷史對話」對面板是假話);**沒有測試會擋措辭,只擋副本**。
  - 判 released 一律看 `worker.status === "released"`。
    ⛔ **不要動 `presenceVisual` 的五態 no-default switch**:`presence` 對 released 與
    對「從沒派工過」**都是 `undefined`**,那顆點分不出來,而拓寬它會波及正職 roster。
  - released 面板**不畫共用卡片、不留任何生命週期按鍵**:server 對 released worker 的
    `/stop` `/restart` `/model` `/relocate` `/refocus` **一律 404**,留著就是 dead affordance;
    八張全是 dash 的卡也只是把那句話埋了。
  - 測這一條要有**真的 released fixture** ＋ **一條 offline(非 released)對照**,
    否則只證明了「有字」,沒證明「分得出來」。
mutant 紀錄:`docs/design/worker-panel-parity-mutants.md` 第五、六批。

## verify(root §13)
純 FE UI 改動:headless build → `preview:4173` → Playwright,CI 綠即 land、**不上 prod 驗**。公開 URL https://officraft.hardcoretech.link/。`Monitor.tsx` 的 mock 部分無 telemetry backend(純前端 mock)。

## i18n 帶參數文案 = 可覆寫片段 + `compose.ts`(T-081b)
字典葉子**不再寫 interpolation 函式**。白名單產生器只收字串葉子,所以任何寫成
`` (name) => `終止「${name}」嗎?` `` 的模板,裡面的字對主題包的 `wording` 覆寫是隱形的
——「終止」按鈕換得掉、確認框內文換不掉(owner 2026-07-27 回報的正是這個)。
- **寫法**:字拆成靜態葉子(`terminateConfirmBodyLead` / `…Tail`、`progressLabel`、
  `blockedByLabel` …),組裝收在 `i18n/compose.ts` 的 `makeMessages(t, language)`;
  元件寫 `const { t, msg } = useI18n()` 後叫 `msg.taskTerminateConfirmBody(title)`。
  `msg` 與 `t` 同一個 memo 來源,主題換詞立刻反映。
- **兩種接法**:兩語言空格一致 → `label + " " + 參數`(值裡不留看不見的空白);參數卡在
  句中、中英標點/引號不同 → lead/tail **純串接**,標點寫進片段,兩語言各填各的、不強求對齊。
  只有空格差異用 `sp`(zh 無空格)吸收。多參數(`uninstallWarnBody1/2/3`)在鍵名標順序。
- **查表不要寫成模板**:狀態→顯示字用靜態物件葉子(`mp.effortOf`、`office.presence`,
  同 `tasks.status` / `tasks.priority` 的寫法),成員才會逐條可覆寫。
  (曾經的 `workerDetail.statusOf` 是同族的第三個例子,已隨外包狀態卡一起退場 — T-7526。)
- **護欄**:`i18n/compose.test.ts` 把每一句在 zh/en 的**逐字輸出**釘死——拆片段不准改到
  螢幕上的一個字元;新增一支 composer 沒進表會被 coverage 那條擋下。

## 主題的身分名稱不可被主題包覆寫(T-081b §6)
`themeIdentity.*` 子樹放的是**某個主題自己的 name**(主題下拉的那一列、匯出檔寫進去的
`name`、新建主題的預設名)。`gen-message-keys.mjs` **整個跳過這個子樹**——規則掛在結構上,
不是另外手維一份 key 清單。以後多一個內建主題,名字放進去就自動不可覆寫。
導覽列的「辦公室」是 `nav.office`(場所稱呼),**照舊可換**。護欄:
`i18n/messageKeys.theme-identity.test.ts`。

## 匯入主題包:不認得的用詞代碼 = 警告,不是錯誤(T-081b)
`wording` 覆寫裡不在白名單的 message code **一律丟棄、匯入照樣成功**(owner
2026-07-27:「已匯入的主題包還是要能夠運作,只是不認得的會失效」),但**不准無聲無息**。
- 丟棄的代碼經 `validateWording(wording, where, skipped)` / `validateThemeBundle(…, skipped)`
  的 **out-param 警告通道**回報(跨語言同一代碼只回報一次),`parseImportedBundle`
  再以 `skippedWording: string[]` 交給 UI。**它永遠不是回傳值(錯誤)** —— 真正不合法的
  包(顏色注入、保留 id、非法 token)照舊由回傳值擋下、留在匯入頁顯 `.set-error`。
- UI 面:匯入成功後落回主題列表,列表頂端出 `.set-warn` 黃框
  (`data-testid="theme-import-skipped"`),文案 = `msg.themeImportSkipped(count, sample)`。
  **比例原則**:只點名前 `IMPORT_SKIPPED_SAMPLE`(3)個代碼,其餘由 count 承載、尾巴接
  「等」/「…」,30 個略過也只有一行。
- 已存在的包(匯入時間早於白名單縮減)**不重驗、不清洗**:`applyWording` 的 `setPath`
  只覆寫既有 string leaf,不認得的路徑自然無作用。護欄:`i18n/index.test.tsx`
  「keeps applying an already-stored pack whose overlay holds an unrecognised code」。
- server 端(`wording_bundle.go`)維持同樣的丟棄語意,但**沒有面向使用者的**警告通道:
  PATCH 的回應形狀是凍結 wire(§13),加欄要先過 spec;而且 FE 在送出前已把不認得的
  代碼濾掉,server 這側的丟棄對使用者不可見。要讓 server 也**回應**回報,是另一張票——
  但**對 operator 一定要留痕**:每次丟棄寫一行 server log(bundle 位置+代碼)。
  server 讀取既有 DB 列時也跑同一支裁剪(只裁剪、不拒收),否則舊列會一路被 GET 回顯。
- **spec 是 wire 的 SSOT**:這條「不認得 = 丟棄 + 200 + 裁剪過的 echo」寫在
  `spec/openapi.json` 的 `ThemeBundleDTO.wording` / `SettingsUpdateDTO.custom_themes`,
  行為由 `conformance/test_rest_happy.py` 釘。改這個語意 = 先改 spec 再改碼。

## 主題編輯器不准把作者打的字 trim 掉(T-081b)
`wording` 覆寫的值**逐字存**,只有「是不是空的」那個判斷才 trim。
T-081b 開放的葉子有好幾條是**句子片段**,邊界空白是有意義的
(`monitor.machine.uninstallWarnBody2` = `"」上還有 "`、`…Body3` 開頭是空白),
存之前 trim 會讓產品自己的編輯器產出「上還有3位成員」——編輯器弄壞它剛開放的字串。
護欄:`ThemeSettings.test.tsx`「keeps the boundary spaces of a sentence-fragment override」。

## 用詞清單**整份 866 列都在 DOM 裡**,不做虛擬捲動(T-8115 已由 owner 撤回)

`.ts-wording-list` 有 866 個可覆寫代碼,**全部一次掛上**。這是決定,不是還沒優化。

- 🔴 **owner 2026-08-02 親自裁定撤回虛擬捲動**:「這設定根本不常進去 只要不是秒等級
  根本沒差 而且通常都是直接匯入」。判準是**不是秒等級就沒差**,加上主題通常直接匯入、
  不是手改用詞。**代價是在知情下付的,不是沒量過**:真 Chromium A/B(只改渲染那一行)
  開主題編輯器 **7.6ms → 64ms**,模擬慢機器(4x 節流)**34ms → 165ms**;DOM 從
  20 列 / 240 個 input 變成 **866 列 / 1086 個 input**。
  ⚠️ **觸及條件比「要改用詞才付」寬**:用詞清單與顏色、字體同在一張表單、**無條件渲染**,
  所以只想改一個顏色的人也付全額。這一點也在裁定時攤開過。
- 🔴 **不准再引入虛擬捲動、視窗化、overscan、或任何「只渲染 N 列」的上限**,除非有新的
  owner 裁定。理由不是效能潔癖的反面,是**三個能力靠「每一列都在文件裡」**,其中兩個是
  瀏覽器自己的功能、我們沒有、也沒有別的方式重新提供:
  1. **鍵盤 Tab 與讀屏的循序順序**。虛擬捲動**必須**把持有焦點的那一列留著(卸載持有
     焦點的 input,瀏覽器把焦點交還 `<body>`、游標整個消失),而那個「釘住的列」被
     render 在視窗**之後** ⇒ 從它按 Tab **直接掉出清單落到「取消」鈕**,`aria-posinset`
     的循序也讀成 `…865, 866, 1`(兩者皆實測)。全掛載之後沒有釘住的列、也沒有重排:
     Tab 走到下一個代碼、序列 1..866 單調遞增。
  2. **瀏覽器自己的「查找」(Cmd+F)**。實測:對第 70% 深度那列的**英文原文**,
     全掛載 `window.find()` 回 `true`、虛擬捲動回 `false`。
     ⚠️ **關鍵字一定要用英文原文,不能用 message code**——列上**不渲染 code**,拿 code
     去搜兩邊都是 false,那樣的探針零鑑別力(這正是先前一份獨立探針失敗的原因)。
     ⚠️ 而**「面板自己的搜尋框已經取代 Cmd+F」這句話是錯的**,本檔舊版寫過:那個搜尋框
     在虛擬捲動之前就存在,**不是為這個損失做的補償**。
  3. **整頁全選複製 / 列印**。實測可讀文字 **1,736 → 32,189 字元**
     (`document.body.innerText`;`Range.toString()` 是 1,491 → 30,268)。這條回來之後,
     「把整份清單倒出來當翻譯工作表交給譯者」又做得到了 ⇒ **不需要另做「匯出用詞對照表」
     那顆鈕**(那是虛擬捲動時期的補償方案,已無標的)。
- **仍然是捲動盒**:`.ts-wording-list` 維持 `max-height: 340px; overflow-y: auto`。
  「全部在 DOM 裡」講的是**文件**,不是**畫面**——沒有這個 cap,面板會長到整組代碼的高度、
  把「取消」推出畫面。
- **列距寫在 `.ts-wording-row` 自己的 padding(6+6),不用 flex gap**。這原本是虛擬捲動的
  約束(spacer 以整數 row pitch 算高);虛擬捲動走了,但**列與列之間**逐像素等價
  ——6+6 = 12px,與它取代的「4+4 padding + 4px gap」也是 4+4+4 = 12px。
  ⚠️ **只有「列間」等價,不要寫成整體逐像素等價**:清單的**首列上緣與末列下緣**各差 2px
  (padding 貢獻 6、gap 在邊緣貢獻 0)。結論不變——**改回去只會讓每一列都動、換不到任何
  東西**——但那個 2px 邊緣差是真的。
- **`aria-setsize` / `aria-posinset` 仍然明寫**,不交給 AT 自己數:它們是「第 431 項,
  共 866 項」為真的來源,而且**搜尋過後**要跟著當前結果集的序號走。
- ⚠️ **`resetWordingScroll()` 只剩一件事**:換搜尋字時把真實元素的 `scrollTop` 歸零
  (舊的偏移指向一個不存在的清單)。它**不再有** window state 要重設。

### 還剩下的、以及被撤回的成本紀錄

- 🔴 **那個逾時的根因查完了,修法是「把查詢縮小」,不是「把門檻放大」(T-e2e9,owner
  裁定 `rc-cf2a2982f31d` 選①)。`EDIT_VIEW_TIMEOUT_MS = 20_000` 已整個刪除**,12 條
  `it()` 全部回到 vitest 的 5000ms 預設。
  **成本是查詢,不是 render,而且差三個數量級。同一個 `<select>`、同一次跑、866 列都掛著**:
  | 取法 | 耗時 |
  |---|---|
  | `document.getElementById("ts-canvas-bg-mode")` | **0 ms** |
  | `container.querySelectorAll("input")`(全部 866 個,走一次) | 119 ms |
  | `within(<它所在那一列>).getByLabelText(…)` | **189 ms** |
  | `utils.getByLabelText(…)`(整個 container) | **16,813 ms** |
  兩次 label 查詢 = 29.5 秒 = 該條測試全程的 **82%**;真正 render 866 個 input 只佔 **9%**。
  機制:dom-testing-library 的 label 查詢對每個 labelable 元素讀 `input.labels`,
  jsdom 每次都重走整份 document ⇒ **O(N²)**。
  🔴 **放大門檻被試過而且不夠**:5s → 20s 之後,在 `333045e` 上單檔跑 5 次仍 **3 次紅在
  `Test timed out in 20000ms`**(seth-m5,load 81–252)。**別再往上加。**
  ⚠️ **`it()` 報的 duration 含 hooks,`testTimeout` 只綁 body**(實測:`beforeEach` 睡
  3000 + body 睡 3000 ⇒ 報 6,005ms 而且過;body 單獨睡 5500 ⇒ 逾時)。所以報出來的數字
  是 body 的**上界**,別拿它直接減 5000 講餘裕。
  ⚠️ 另一個會誤導的觀察:**耗時 38.9s / 42.6s 的跑反而通過、24.9s 的跑失敗**——逾時只在
  `await` 邊界檢查,同步查詢不會被打斷,所以過不過取決於檢查落在哪個 await,與總耗時無關。
  **這就是它看起來隨機的機制。**
  ⇒ **不要新增「在 866 列都掛著時對整個 container 跑 `getByLabelText` / `getByRole`」的
  查詢**。現行寫法一律**先縮到一個小容器**(`ThemeSettings.test.tsx` 檔頭的 `colourRow` /
  `canvasBgSlots` / `formActions` 三個 helper,以及 `within(row).getByRole("textbox")`)
  或直接 `querySelector('[data-wording-code=…]')`。照直覺改成整頁查詢就會把逾時招回來。
  🔴 **縮小 scope,不要改用 id 定位。** `*ByLabelText` 除了找到元素,**本身就在證明
  input 與它的 label 真的綁在一起**(讀屏軟體念得出欄位名靠的就是這個)。換成
  `getElementById` 會快到 0 ms,但 label 綁錯或掉了測試照樣全綠——而 owner 撤回虛擬捲動
  換回來的三個能力裡有兩個就是無障礙。縮 scope 不必付這個代價:每一處仍是 label 查詢。
  唯一的例外是 `.ts-wording-search`(它的容器就是那 866 列,沒有更小的 scope),那一處
  改成**斷言** `aria-label`,綁定仍有證人。
  ⚠️ **這是測試環境成本,不是使用者成本**,別拿這些數字當「拿掉虛擬捲動讓產品變慢」的證據
  (真瀏覽器同一份 866 列是幾十毫秒等級)。
- 🔴 **舊文那個「~9 倍(5.13s → 0.543s)」是 jsdom 專屬的數字,永遠不准拿去講使用者體感**
  ——這條規則跟虛擬捲動一起留著,因為它是關於怎麼引用數字,不是關於實作。
- ⛔ **已作廢、不要再引用的舊記載**:本檔上一版把「釘住的列 Tab 會掉出清單、讀屏序號跳回
  第 1 項」記成**「已知、刻意接受的缺口 4,要另開票」**;同一份記載也把 Cmd+F 與整頁複製
  列印記成刻意接受的缺口 1–3。**那個立場已經不存在了,三個缺口都隨虛擬捲動一起消失。**
  留這句是因為錯的事實會長腳:看到任何地方還寫著「這個缺口存在」或「我們決定不修」,
  那是舊文,直接改掉。

### 護欄兩層,各答一半

- **`src/components/ThemeSettings.test.tsx`「wording list is browsable in full」四條**
  (jsdom 看得到的那半:文件裡有什麼、DOM 順序):
  (a) 866 個代碼**全部在文件裡**(不再有捲動走訪——沒有視窗要推進,走訪只是把同一組量 N 次);
  (b) 搜尋結果一個不少(v1 那個 `slice(0, 30)` 的殺傷點);
  (c) 打字不准把該列移走(守「別再回去做把已覆寫列提到第 0 位的排序」+ `scrollTop` 沒被
  `setWordingAt` 誤重置);
  (d) **捲走之後整組還在、`aria-posinset` 單調遞增、焦點還在同一個元素、零 pinned / 零 pad**。
- **`visual-guards/wording-list-full.ct.spec.tsx`(真 Chromium)四條**
  ——⚠️ 檔名從 `wording-list-window.ct.spec.tsx` 改過來了,因為已經沒有 window。
  它守的是 **jsdom 做不到的那半**:jsdom 按不出真的 Tab(焦點不會自己動)、也**完全沒有
  「查找」**。所以「焦點實際落在哪個元素」與「瀏覽器找不找得到」只有這一層答得出來。
- ⚠️ **CT 不在雲端 gate 裡**,所以上面那條 (d) **刻意**放在 jsdom——只放 CT 等於這個回歸在
  GitHub 上是綠的。查證過的源頭(2026-08-02,不是沿用舊文):`.github/workflows/ci.yml:86`
  的唯一 job 跑 `bash bin/ci-cloud.sh`;那支腳本裡 **`test:ct` 命中 0 次**、只有
  `vitest run`,而 `test:ct` 只出現在 `bin/ci.sh:452`。

**mutant 實測(把虛擬捲動整份放回去;還原用 scratchpad 備份 + shasum 對帳,未用
`git checkout --`,還原前先把 CT build cache 移走)**:

| mutant | jsdom(22 條) | CT(4 條) |
|---|---|---|
| 虛擬捲動整份放回 | 🔴 **3 條**((a) 少 846 個、(b) 搜尋少 118 個、(d) `expected 21 to be 866`) | 🔴 **3 條可靠 + 1 條間歇**(可靠:866→20 列;Tab 兩次都量到 `BUTTON` + `[865, 866, 1]` + 只有 21 項;`window.find` 回 `false`。間歇:見下) |

🔴 **四條 CT 裡有一條是「不可靠、負載相依」的偵測器,不要把它算進覆蓋**:
「Tab walks from row to row, well past the first screenful」——**單獨跑通常綠、並行負載下
真的會紅**(`Tab #37 left the 用詞 list…`,量到 `BUTTON`)。獨立複審單獨跑 5 次有 1 次紅;
⚠️ **把它讀成「間歇」,不要讀成 20% 之類的比率——n=5 撐不起一個數字**(信賴區間寬到沒有
決策價值)。機制:overscan 本來就是為了讓循序走訪能動(每次 focus 把下一列捲進視野、
視窗跟著前進),但**機器較忙時視窗跟不上焦點驅動的捲動**。
⚠️ **在 HEAD 上它是確定性綠**(單獨 5 跑 + 整份 spec 3 跑),所以那個紅是 mutant 專屬的,
**不是這條分支引入的 flake**。
**真正擋住「虛擬捲動回來」的是另外三條。** 它留著的正當理由是**對硬上限確定性有效**:
獨立複審自己建了 v1 那顆 `slice(0, 30)`,它 3 跑 3 紅在 `Tab #30`(正好是上限邊界),
同顆 mutant 下 jsdom 也紅 3 條。
🔴 **本檔上一版把它寫成「零鑑別力/照樣綠」,那是錯的,而且錯的方向是低估自己的測試**——
那段是「實測事實紀錄」,寫錯會讓下一個人把一個**真的間歇紅**當成「本來就這樣」,或反過來
去追一個不存在的 bug。

⚠️ **一個量測陷阱**(實測踩到):`el.scrollTop = el.scrollHeight` 之後**立刻**用
`page.evaluate` 讀 DOM,讀到的是 **React 還沒重繪的舊樹**(當時量到「pinned = 0、Tab 正確」
的假象)。要嘛用會自動重試的 `expect(locator)`,要嘛先等一個能證明重繪發生的斷言。
一次性探針特別容易寫出這種假綠。

## 匯出不烘 alias 預設值(T-081b)
theme.css 裡定義值是裸 `var(--other)` 的 token(分區三槽)是「跟隨」不是顏色。
`getComputedStyle` 會把它解析掉,所以匯出/播種前必須先跳過**還坐在 alias 上**的槽,
否則每個新主題的分區都被釘死在內建色。名單由 `gen-theme-tokens.mjs` 從 theme.css 推導
(`THEME_ALIAS_DEFAULT_TOKENS`),不是手寫三個名字。護欄:`lib/themeExport.test.ts`。

## 最外層畫布可吃背景圖(T-081b,rc-1e78b3b19082 選項 2)
主題包新增 optional `backgrounds: { canvas: "data:image/…;base64,…" }`(spec 已凍結入
`openapi.json` 的 ThemeBundleDTO)。**只有最外層畫布**(內容欄兩側那塊)有這一槽。
- **為何是 zone map 而不是像 `logo` 的裸字串**:key 就是「哪一區」,把「只有畫布能吃圖」
  這條規則寫進結構——`backgrounds.topbar` 會被具名擋下(422 / `only canvas`),不是
  靠註解約定。頂列/頁籤列/內容區底下都坐著文字,**文字壓在花紋上沒有可讀性保證**,
  所以不開放,不要「順手」加進 key set。
- **圖片驗證的「安全」那半原封不動重用頭像那道閘**:同一份 mime 白名單
  (PNG/JPEG/WEBP,SVG 永遠拒)、同一組 magic byte、同一套嚴格 base64 字母集。
  **這半永遠不准為背景放寬**,它跟大小完全正交。
- 🔴 **但「多大」那半已經分家了(T-72da,owner 2026-08-03)。本檔上一版寫著
  「不准為背景另開一套規則、不准調高 cap」——那條裁定已經被 owner 自己推翻,
  不要照著它把兩個數字統一回去。**
  當時那句的前提是**背景與頭像共用同一道 gate**,所以「調高背景」必然等於「調高頭像」。
  這個前提已經不成立:閘的兩個 size cap 現在是**參數**
  (Go `validImageValue(v, maxDecoded, maxValueLen)` / TS `isValidImageValue`),
  兩個 purpose 各有一組 thin wrapper。
  - **頭像 / logo / 導覽圖示 = 64 KiB**(`maxAvatarBytes` / `MAX_AVATAR_BYTES`),
    字串長度 96 KiB。**一個字都沒動**,30–40px 的小圓圖不需要更多。
  - **背景圖 = 512 KiB**(`maxBackgroundBytes` / `MAX_BACKGROUND_BYTES`),
    字串長度 704 KiB。理由:它是**鋪滿整個視窗**的圖,owner 的實際背景貼在 64 KiB
    上限、他連講三次「太糊」。
  - 🔴 **字串長度那一層一定要跟著動**:它跑在 base64 解碼**之前**,留在 96 KiB 的話
    512 KiB 的圖會在那裡就被 `data URI is too long` 擋掉,解碼那層永遠執行不到。
    **只放寬解碼那層 = 完全沒有效果**,這是這件事最容易做半套的地方。
  - **TS/Go 兩側仍是 twin,而且不再只靠註解**:`bin/tests/fixtures/image-cap-cases.tsv`
    是唯一的真相表,兩側各自對它斷言
    (`server/ocserverd/image_cap_mirror_test.go` / `frontend/src/lib/imageCap.test.ts`),
    任一側漂掉都會紅**而且訊息點得出是哪一側**。照 `doc-cap-cases.tsv` 的先例做。
  - **控制台那道 UI 閘也要跟著分流**:`ThemeSettings.tsx` 的 `readValidatedImage`
    是四個 picker 共用的,背景那個 picker 要傳 `isValidBackgroundValue`,
    否則會出現「後端說可以、控制台說圖片無效」。
- **色與圖並存**:`global.css` 的 body 改成 `background-color: var(--color-bg)` +
  `background-image: var(--canvas-bg-image, none)` + `background-repeat: repeat`。
  沒有圖 = 完全等同從前的純色;舊主題包(沒有這個欄位)一個像素都不會變。
- **CSS 變數刻意不叫 `--color-*`**:它的值是 `url("data:…")` 不是顏色,掛進顏色契約
  會被 `themeExport.ts` 的 `isValidColorValue` 濾掉、也過不了 bundle 的顏色文法。
  因此圖片走**自己的 bundle 欄位**(與 avatars/logo 同路)round-trip,
  套用則在 `i18n/index.tsx` 的 apply effect 以 `setProperty` 推 `--canvas-bg-image`,
  並登記進 `appliedTokensRef` 讓換主題時清得掉。護欄:`lib/themeExport.test.ts`
  「round-trips a canvas background image through serialize → import」+
  「no colour token holds a url()」、`i18n/index.test.tsx` 的 apply 用例。
- **手機不受影響**(owner 特別交代):視窗 ≤1136px 時 gutter 歸零,三個分區的不透明
  底色蓋滿整幅,圖看不見也不影響版面;background 不參與 layout,所以不可能產生橫向捲動。
  實測 narrow 1440/1040/900/720/480/375 與 wide 1440/1280/1040 皆 h-scroll = 0。

## 主題快取的三道守衛:三個宿主,零個新 CI 關卡(T-1500)

pre-React 上色(`src/paint/prePaint.ts` 由 `vite.config.ts` 的 `inlinePrePaint()`
編成 IIFE inline 進 `index.html`)有三個**互不重疊**的性質要守。它們刻意**分住三處**
——設計原本要開一個新的 `4b4` 一次跑完,但那樣「一個關卡被砍掉、三道守衛同時消失」:
1. **記錄驗證** → `src/lib/themePaint.test.ts`(jsdom,既有 4b)。
2. **產物形狀** → `src/lib/paintArtifact.test.ts`(jsdom + 一次 `vite build`,既有 4b,
   **不需要瀏覽器**)。
3. **真實載入的每一幀** → `paint-guards/*.paint.spec.ts`(真 Chromium,
   `playwright-paint.config.ts`,由 `npm run test:ct` 串在既有 4c 之後)。

`MALICIOUS_PAINT_CASES` / `VALID_RICH_BUNDLE` 的**權威定義**在 `src/lib/paintFixtures.ts`,
jsdom 與瀏覽器兩層共用 ⇒ 加一個 payload 兩層同時守得到。
⚠️ 但**不是「全世界只有一份」**:`src/lib/paintFixtures.theme.json` 是給 stub 伺服器吃的
twin(它是 JSON、不能 import TS)。那份 twin 由 `themePaint.test.ts` 的
「matches the JSON copy the stub server serves」做 deep-compare 守著,所以漂了會紅
——但別把它講成不存在,下一個人會照著「只有一份」的字面去改其中一邊。

🔴 **兩層各擋哪顆 mutant,別記反(獨立覆核實測)**:
- **挖掉 `readValidatedPaint` 本身** → `themePaint.test.ts` 紅 6 + `paintCache.test.tsx` 紅 4。
- **驗證器留著,只讓 inline script 繞過它** → **jsdom 三個檔 40/40 全綠、tsc 乾淨**,
  只有 `payloadInjection.paint.spec.ts` 紅 5(6 個 payload 中的 5 個;第 6 個是 CSSOM
  擋的、fixture 自己標了不算覆蓋)。
⇒ **jsdom 那層擋不住 inline 繞過。** 這句話的用途是擋掉「jsdom 已經守住了,4c 可以砍」
這個推論——那正是想省成本時最容易講出口的一句話。

### 🔴 frame 量測一律在「登入態 + 伺服器認得該主題」下做,而且要**斷言**它成立
`reconcileFromServer()` 在 `i18n/index.tsx` 是 `if (hasToken())` 閘住的。**沒有 token
⇒ reconcile 永不執行 ⇒ `themesLoaded` 永遠 false ⇒ 只有「保留快取」那一條分支被跑到。**
實測同一個 build:沒種 token 讀 `BAD_FRAMES=0`,種了 token 讀 **231/233/249**
(伺服器不認得該主題 ⇒ `writePaint(active, [])` 把記錄**刪掉**)。
- ⇒ `zeroFlash.paint.spec.ts` 每條測試都**在頻帶內證明前提**:`/api/settings` 真的回過
  200、body 真的帶著這個主題、而且**播下去的記錄用的是不同的 `name`,跑完必須變成伺服器
  那個 name**——只有 reconcile 真的跑完才會成立。前提不成立就以 setup error 紅掉,
  不准空跑變綠。**meta-mutant 驗過**:把種 token 那行拿掉 ⇒ 紅在
  「GET /api/settings never answered 200」。
- ⇒ 量測用的 build 是 **`VITE_USE_MOCK=false`**(`bin/build` 出貨的那個)。預設 build 帶
  的是 in-memory mock,`custom_themes` 恆為 `[]` 且 0 ms 回話——**節流對它完全無效**,
  而這張票要修的閃爍窗**就是**等 `/api/settings` 的那段。伺服器由
  `paint-guards/settingsStub.mjs` 扮,`--delay 400` 不是填充,是那個窗本身。
- ⚠️ mock **無法**用來測 happy path:`mockServerSettings` 是 module-level、每次重整就重置,
  所以「重整後伺服器仍記得這個主題」在 mock 下不可能發生。

### 🔴 正向斷言:要驗「該套上的真的套上了」,不是「不含某個值」
只驗「某個禁字沒出現」的套件,會被**「applier 靜默不再套用 fonts 與 canvas」**整個繞過
——實測那顆 mutant 通過 tsc、build、產物 A–E、`paintCache.test.tsx` 的決策測試,以及一套 6 case 的
absence-only 瀏覽器探針,**6/6 全綠**。所以 `VALID_RICH_BUNDLE` 一定帶
colours **＋ fonts ＋ canvas 圖 ＋ canvas mode**,`EXPECT_APPLIED` / `EXPECT_APPLIED_VALUES`
逐條斷言它們真的到 DOM。
- **而且要歸因給 inline script**:`frameCarryingBeforeMount()` 要求那些值出現在
  **React 還沒 mount** 的幀上。裸的 `frameCarrying` 分不出 inline script 與 React 自己那條
  `!themesLoaded` fallback(它也呼叫 `readValidatedPaint()`)——實測 inline plugin 整個拿掉,
  裸版正向控制**照樣綠**。
- ⚠️ 這條需要**節流**才量得到:未節流時 React 早於 sampler 的第一個 rAF 就掛載完
  (實測第一筆取樣在 24.4 ms、`mounted` 已是 true),pre-mount 幀數為 **0**、斷言無從成立。

### 🔴 探針必須有 exit code,而且「取樣數」要有下限不是 `> 0`
- 前一代 frame 探針**只 `console.log` 數字、零個 `process.exit`**:實測 `BAD_FRAMES=1`
  而 shell exit code **0** ⇒ 接進 `set -e` 的 CI 永遠綠。現在寫成 Playwright spec,
  exit code 由 runner 保證。
- `SAMPLES > 0` **擋不住**上一輪真正燒到的那個失效:把逐幀改成「載入後單次讀」,
  `SAMPLES=1` 仍 `> 0`,而同一顆 mutant H 從 4 紅變 **6/6 假綠**。所以門檻是
  `MIN_SAMPLES`(80;健康的 3 秒窗是 200–260 幀)。**meta-mutant 驗過**:把 rAF 的
  re-arm 拿掉 ⇒ 11 條全紅、訊息是「only 1 frames sampled」。
- `({}).polluted === undefined` 是**恆真**的(applier 只呼叫 `setProperty`,沒有任何以
  payload 為鍵的賦值 sink;連驗證全拔的 mutant H 六個 case 都是 false)。它留著只為了
  「哪天這件事不再成立時被看到」,**不計入覆蓋**,註解已寫明。

### 注入案例要跑在「伺服器不認得任何主題」那台(:4319)
對著 happy-path 那台跑會有三條假紅:那台會回真主題,React 於是**合法**套上
`--canvas-bg-image` 與 `--color-bg: #010203`(實測 ~2038 ms),而那正是 `svg-canvas-bg` /
`illegal-canvasMode` / wording 三個 case 的禁字 ⇒ 斷言分不出「pre-paint 洩漏」與
「伺服器給了真主題」。`custom_themes: []` 那台上,自訂屬性的唯一寫入者就只有 pre-paint,
每次出現都可歸因。

### 儲存鍵只有一份:斷言在**原始碼**上,不只在產物上
產物斷言「找模組裡 `LS_THEME` 的值」只抓得到**兩步**漂移(先改回寫死字面量、再改常數值)。
**單步**——`prePaint.ts` 改回 `localStorage.getItem("oc.theme")`、乾淨移除 import、常數不動
——實測 tsc / build / 產物斷言**全綠**。所以 `paintArtifact.test.ts` 另外直接掃
`prePaint.ts` 與 `i18n/index.tsx` 的原始碼,禁止出現 `"oc.theme"` / `"oc.themePaint"`
字面量,在還來得及 review 的那一步就紅。
**探針自己也不准寫死鍵**:`frameProbe.ts` 的 `seedSession()` / `readStoredPaint()` 從
`api/auth` 的 `TOKEN_KEY` 與 `lib/themePaint` 的 `LS_THEME` / `LS_THEME_PAINT` 取值,
再當參數送進 `page.evaluate`。自帶一份 `"oc.theme"` 的探針在改名後**照樣綠**
——它只是在種一個沒人讀的鍵,然後斷言它從沒種進去的主題沒有出現。
