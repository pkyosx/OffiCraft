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

## 聊天未讀跳轉(M2 批次 19;LINE/FB 式,純 FE)
ChatArea 兩個行為,皆不動 server:
- **進房跳第一則未讀**:進對話時 snapshot `member.unreadCount`(**render 同步取**,搶在 listChat「list 即讀」清 watermark 之前——這是 race-free 的關鍵;server 清掉後 roster unreadCount 才歸 0)。第一則未讀 = thread 中 `from===peer && to===owner` 訊息的**最後 count 則之最早者**;其上渲染 `.chat__unread-divider`(「以下是未讀訊息」細線)並 `scrollIntoView({block:"start"})` 頂到視野頂;divider 整個 session 保留(如 LINE)。無未讀照舊落底。ChatArea 換 peer 不 remount → render-time guard 重置 session 追蹤;useChat 於 withId 換時**立即清空 messages**(防舊 thread 殘影 + 防未讀定位錨錯舊訊息)。
- **房內新訊息浮條**:owner 上滾(沿用既有 `nearBottomRef` 判定,80px 帶)時新進 `to===owner` 訊息 → `.chat__new-msg-chip` 灰底 pill 浮在 `.chat__body` 底部;錨點 = 浮條出現後**第一則**未看訊息(session 內以 message-id diff 追蹤,不動 server);點擊 smooth 捲到該則(`[data-msg-id]`);**捲到底才消失**(onScroll near-bottom 清除),點擊本身不清。在底部時維持原自動跟底、永不出浮條。i18n key:`chat.newMessages` / `chat.unreadBelow`(三語)。

## 全幅閱覽 = 一個 overlay、三種來源(owner 2026-07-28;T-f014 收編圖片)
`MarkdownPreviewOverlay` 是**唯一**的座艙內全幅面 —— markdown **與圖片都算**,
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
座艙裡**只剩這一個**全尺寸看圖面,所以每張圖(已存檔附件 / staged 預覽)都拿到同一組
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
單卡 refetch。**list wire 輕量化(T-3f31)**:`GET /api/reply-cards` 只回輕量摘要
(summary+決策 digest,無 body/options 全文)——http adapter 的 `listReplyCards`
逐卡 hydrate(list 拿 id 序 → per-id `GET /api/reply-cards/{id}`)還原完整
`ReplyCard[]`,adapter 契約與 pane 渲染(chips/body)不變;mock 本就出全卡,
parity 在 adapter 層。跳到原訊息 = `#office/chat/<id>/msg/<msgId>`(hashRoute `msgId`)
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
兩個真的需要全狀態的視圖靠**送空集合**表達:清除篩選、以及 `#tasks/<id>` 跳轉錨點。
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
re-run):終態目標 → 自動展開已結束;被篩選藏住 → **只清相關的那個維度**
(matches 拆成三個 per-dimension predicate);card 進 DOM → scrollIntoView +
`task-card--located` 高亮 flash(2.6s)→ **消費 anchor**(route 退回 `#tasks`,
one-shot,可重跳);未知/過期 id 誠實自癒(消費 anchor、不高亮)。

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

## 用詞清單是 windowed 的,但**一個代碼都不准少**(T-8115)
`.ts-wording-list` 有 ~870 個可覆寫代碼。它以前把**全部**一次掛上去 ⇒ 開主題編輯器就
要建 ~870 個受控 `<input>`。現在只掛捲動視窗看得到的那些(+ overscan),上下兩塊
`.ts-wording-pad` 佔掉其餘的高度。
- 🔴 **這條的紅線是「不准少」**:捲軸範圍仍涵蓋全部、每個代碼仍捲得到、搜尋仍給**全部**
  命中。**任何形式的「預設只顯示 N 筆」都已被 owner 端否決過一次**(前一版 `slice(0, 30)`
  ,連搜尋後的 125 筆結果也砍成 30);不要再回去。
- **成本量在哪(別重推一次)**:jsdom 那 ~2.5 秒**不是** React render(整個元件 103 次
  render 只有 579ms,其中八成是這張清單)。真正的大頭是 dom-testing-library 的
  `getByLabelText` 對每個 labelable 元素讀 `input.labels`,而 jsdom 每次都**重走整份
  document** 找 form controls ⇒ N 個 input = 每次查詢 N 次全文件走訪(profile:
  `NodeList-impl._update` 1145ms + `form-controls.query` 551ms,佔 top-2)。
  所以**要降的是 DOM 裡 `<input>` 的個數**。memo / useCallback / 抽元件動的是那 579ms
  的一小角,對上面那兩個 top-2 一點都碰不到——**別再從「太多 render」的直覺出發**,
  先 `--cpu-prof` 量一次(作法:自訂一份 `pool: "forks"` + `execArgv: ["--cpu-prof"]`
  的 vitest config)。同一支 profile 也證明了 `readDictMessage` 只有 11.4ms,不是病灶。
- 🔴 **上面那個 ~9 倍(5.13s → 0.543s)是 jsdom 專屬的數字,不准拿去講使用者的體感。**
  真瀏覽器(Chromium CT,M5 Mac,各 4 跑)量到的是:**開啟成本 ~80ms → ~44ms**(約 1.8 倍,
  一個影格半),**打字延遲反而從 6.5ms 略退到 7.4ms**(下面那個 `useLayoutEffect` 沒有
  dep array,每次 commit 都做一次 `querySelectorAll` + 讀 `offsetTop`/`clientHeight`,
  等於每個按鍵強制一次同步 layout——想再優化就從這裡下手),捲動成本不變(rAF-bound)。
  ⇒ 這顆的正當理由是**「解掉一條會逾時的測試 + 結構上 DOM 與畫面等比 + 慢機器上那 1.8 倍
  的絕對值會放大」**,不是「編輯器很慢」。**「870 個受控 input 讓打字卡」這個直覺沒有被
  證實**,別再用它當出發點。
- **overscan 不是重繪順滑度的旋鈕,是鍵盤 Tab 能不能走完全清單的關鍵**:Tab 只到得了
  已掛載的 input,瀏覽器把它捲進視野 → `onScroll` 推進視窗 → 下面又補一列。視窗邊緣
  外**永遠要有掛載的列**,把 overscan 調成 0 就是把 Tab 遍歷砍斷。
- **`.ts-wording-list` 不准加 `gap`**:spacer 是以「整數個 row pitch」算高度的,flex gap
  會在每個 spacer 兩側各多 4px 而沒有任何一列去對帳。列距寫在 `.ts-wording-row` 自己的
  padding(6+6 ≡ 舊的 4+4 + gap 4)。
- **row pitch / viewport 兩個常數只是 fallback**;真實瀏覽器由 `useLayoutEffect` 量
  (pitch 取相鄰兩列 `offsetTop` 差,所以 gap 若真的回來也算得進去)。jsdom 量到 0,
  就用常數。⚠️ **那兩個數字必須是量到的、不是推的**:`WORDING_ROW_PITCH_PX` 曾經寫
  40、註解卻推導成 36、而 Chromium 實際量到 **48**,三者互不相符。實務上無害
  (`useLayoutEffect` 在 paint 前就蓋掉它),但下一個 builder 讀到會以為那是量過的。
  現在是 48,`.ts-wording-row` = 6+6 padding + 36px content box(兩行 meta 欄 36px,
  比旁邊 34px 的 input 高)。**改 CSS 就重量一次,別重推一次。**
- **AT 看得到的是視窗、不是全集**,所以 `role="list"` + 每列 `aria-setsize`/`aria-posinset`
  用**絕對**位置,讀屏才不會把 ~870 項的清單念成 21 項。
- 🔴 **有焦點的那一列永遠掛著,即使捲出視窗**(`focusedWordingCode` / `wordingPinned`)。
  卸載一個**持有焦點**的 input,瀏覽器會把焦點交還 `<body>`——游標整個消失、繼續打字
  沒有反應。**值不會掉**(`editWording` 住在 window 之上),掉的只有焦點,這兩件事別寫混。
  修法是把那一列 `position:absolute` 放在它本來該在的 offset(上面的 spacer 已經幫它
  留好高度),所以既不擠掉任何真列、捲回來也剛好在原位、不會出現兩份。
  ⚠️ **它必須跟視窗內那些列在同一個 keyed array 裡**:React 是**逐 slot** 對帳的,
  把它另外放一個 slot(例如 spacer 後面)= 捲出視窗時整個 node 被拆掉重建,
  **焦點照樣掉**——那正是這條要修的東西,實測踩過。量測 pitch 的
  `querySelectorAll` 也因此要排除 `.ts-wording-row--pinned`(它的 `offsetTop` 答的是
  另一個問題)。
- ⚠️ **已知且刻意接受的四個缺口**(全部實測坐實,不是推的):
  1. **瀏覽器 Cmd+F 找不到未掛載的列**。實測:`window.find()` 對第 70% 深度那列的英文
     原文,windowed 回 `false`、全掛載回 `true`。由面板自己的搜尋框兜住——它比對
     代碼 / 英文原文 / 目前語言文字,**正好涵蓋** Cmd+F 在這份清單上找得到的全部文字。
     殘留的是操作面:Cmd+F 是跨全頁一次搜尋,現在「用詞那段要改用另一個框」。
  2. **讀屏的循序瀏覽(virtual cursor 一路往下讀)只掃得到已掛載的列**。與 1 同源。
     🔴 **這條沒有 AT 實測過**,是機制推論——別把它寫成量過的。
  3. **全選複製 / 列印整份清單**。實測:整頁可選取文字從全掛載的 **33,034 字元**掉到
     **1,735 字元**(`document.body.innerText` 32,182 → 1,729),Cmd+P 同一根因。
     **嚴重度判定(拿產品裡的證據判,不是「反正沒人這樣用」)**:主題包**有**正式的
     匯出/匯入路徑(每個主題一顆下載鈕 → `serializeBundle` 出 JSON,匯入頁吃貼上或檔案),
     而且 `wording` 是 round-trip 的欄位之一 ⇒ **「把我的用詞覆寫搬走 / 搬到另一台」
     完全由它承接,這一條不損失任何東西**。但那份 JSON 只裝**你已經覆寫的那些**
     (`saveEdit` 只留非空值),**不是** 866 條「英文原文 + 目前譯文」的對照表。
     所以真正失去的是那一個用途:**把整份清單倒出來當翻譯工作表交給譯者**——
     全選複製本來是產品裡唯一做得到這件事的手段,現在做不到,而且沒有替代路徑。
     這一條**是真的損失**,只是範圍窄;要補的話是一顆「匯出用詞對照表」的鈕,另開一張票。
  4. 🔴 **釘住的那一列,Tab 會直接掉出清單;讀屏的序號也會在它那裡跳一次。**
     **這一條不是 windowing 的固有代價,是上面那個 focus pin 自己引入的新回歸**
     (對**改動前**而言——改動前這個情境根本不存在,因為焦點會先掉光)。不要把它
     淡化成小瑕疵。根因同一個:pinned row 掛在 keyed array 的**尾巴**,DOM 順序排在
     視窗那 20 列之後,而 Tab 與讀屏的循序閱讀**都吃 DOM 順序**。
     - **實測 N1**:焦點在第 0 列 → 捲到底(第 0 列變成釘住的)→ 按 Tab →
       落在**「取消」按鈕**(`BUTTON.doc-btn`),整個掉出清單。
       對照組(焦點在視窗**內**的列)→ Tab 正確走到下一個代碼。
     - **實測 N2**:釘住時 DOM 上的 `aria-posinset` 序列是 `[847, 848, …, 865, 866, 1]`
       ——讀屏一路往下讀,會在第 866 項之後撞到第 1 項。
     - **觸發很窄**:必須「游標留在某列 **且** 把清單捲到那列看不見 **且** 這時按 Tab
       或用讀屏往下讀」。正常的 Tab 遍歷不會踩到(Tab 會把該列捲進視野,它就不是釘住的)。
     - ⛔ **真修法是把 pinned row 插回它的序位,但這一顆不做**:那會動到「同一個 keyed
       array + 尾端追加」這個**正是讓焦點活下來的**對帳假設,而「換個位置 = 修了等於
       沒修」這個地雷本檔上面已經記了一次、也真的踩過。要修請另開一張票,並且**先想清楚
       怎麼在不觸發重建的前提下換序**,不要在收尾階段順手改。
- 護欄兩層,但**兩層守的不是同一件事**,別把它們講成一組可互換的三條:
  - `ThemeSettings.test.tsx`「wording list is browsable in full」**四**條:
    (a) 捲完全部 866 個都到得了、(b) 搜尋結果一個不少——**這兩條才是撐住 windowing 的**,
    mutant `slice(0,30)`(v1 那一刀)在 1.4 秒內就被它們打紅;
    (c) **有焦點的列捲走不准掉焦點**(mutant:把 `wordingPinned` 關掉 → 紅);
    (d) **打字不准把該列移走**——⚠️ **這條對本次 diff 零鑑別力**:基底的列序本來就是
    `MESSAGE_KEYS` 順序、沒有任何重排邏輯,所以它在改動前就成立。它守的是「別再回去做
    v1 那個把已覆寫列提到第 0 位的排序」加上一句 `scrollTop` 沒被 `setWordingAt` 誤重置。
    **有用,但不要把它算進「撐住這次改動」的那一組。**
  - `visual-guards/wording-list-window.ct.spec.tsx`(真 Chromium)四條。
    🔴 **jsdom 那層對幾何是全盲的**:`offsetHeight`/`clientHeight` 恆為 0,pitch 算錯、
    spacer 對不上、視窗與捲動位置漂掉,在 jsdom 全部照樣綠。CT 那條量的就是「捲到某個
    offset 時,視窗頂端那一列**正好**是該 offset 指的那一列」。
    ⚠️ **而 CT 不在 PR 的雲端 check 裡**(`bin/ci-cloud.sh` 只跑 vitest,`test:ct` 只在
    `bin/ci.sh:482` 的本機 land gate),所以純幾何的回歸在 GitHub 上是綠的,只有本機
    land gate 擋得住。這是 repo 既有結構、不是這顆引入的,但這顆把幾條關鍵不變量放在了
    那一側。
  - **mutant 實測(四種,都對現在這份檔重跑過,不是沿用舊紀錄)**:

    | mutant | jsdom | CT |
    |---|---|---|
    | 拿掉 windowing(全掛載) | 🔴 1 條 | 🔴 4 條(其中 Tab 那條是假紅,見 CT 檔頭) |
    | **窗開成固定 400 列**(仍 window、但效能砍半) | 🟢 **22/22 全綠** | 🔴 1 條(`mounted < total/4`) |
    | `slice(0, 30)`(v1 那一刀) | 🔴 3 條(含兩條可達性) | 🔴 |
    | 拿掉兩塊 spacer | 🟢 全綠 | 🔴 2 條(含 Tab #26 → `null`) |
    | 關掉 focus pin | 🔴 1 條 | 🔴 1 條(`activeElement` 量到 `BODY`) |

    ⚠️ **「拿掉 windowing」在 jsdom 那個紅是新的,但它的射程比聽起來窄,兩邊都別講錯**:
    這一版之前它在 jsdom 是 21/21 全綠,唯一擋得住的是本機才跑的 CT;現在焦點那條測試的
    **前提檢查**(`not.toContain(MESSAGE_KEYS[1])`,「視窗真的移開了」)會在全掛載時失敗,
    所以**「整個拆掉 windowing」這一種**在雲端 CI 也擋得住了。
    🔴 **但它擋的只有「全掛載」這個極端**:那條前提問的是「捲到 300 列之後,第 1 列還在不在」,
    所以任何**仍然 window、只是窗開太大**的中間態——例如固定掛 400 列——**在 jsdom 照樣全綠**,
    效能回到一半也沒人會紅。**別讓下一個人以為 jsdom 擋得住所有 windowing 回歸。**
    真正對「窗開多大」有嘴的仍然只有 CT 的 `mounted < total / 4`。
    **拿掉 spacer 也仍然只有 CT 抓得到。**

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
- **圖片驗證原封不動重用頭像那道閘**:TS `isValidAvatarValue` / Go `validAvatarValue`
  ——同一份 mime 白名單(PNG/JPEG/WEBP,SVG 永遠拒)、同一組 magic byte、同一個
  **64 KiB 解碼上限**。256×256 的可平鋪紋理綽綽有餘;想塞 4K 桌布正是這個 cap 要擋的。
  **不准為背景另開一套規則、不准調高 cap**;TS/Go 兩側是 twin,任一側加規則就得同步。
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
