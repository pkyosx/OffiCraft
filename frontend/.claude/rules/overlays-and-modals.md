---
paths:
  - "src/components/MarkdownPreviewOverlay*"
  - "src/components/AttachmentStrip*"
  - "src/components/ComposerAttachmentPreview*"
  - "src/components/md-preview.css"
  - "src/components/ConfirmModal*"
  - "src/components/*Modal*"
  - "src/components/*Popover*"
  - "src/components/DocCard*"
  - "src/components/BootDocPage*"
  - "src/components/SettingsPage*"
  - "src/components/DiffView*"
  - "src/components/diff-view.css"
  - "src/components/DocumentHistory*"
  - "src/lib/escapeLayers.ts"
  - "src/lib/useEscapeLayer.ts"
  - "src/lib/escapeLayerOwnership.test.ts"
  - "src/lib/lineDiff.ts"
  - "src/lib/wordDiff.ts"
  - "src/lib/shareLink.ts"
  - "visual-guards/image-zoom-pan.ct.spec.tsx"
---

# 全幅閱覽 overlay、Esc 分層、DocCard、差異呈現

## 全幅閱覽

MarkdownPreviewOverlay 是唯一的全幅面，props 是互斥的 url+attachmentId、source 或 imageSrc。url 自己 fetch，保留下載與以 att- blob id mint 的分享連結；source 是聊天本文，不 fetch、不可下載或分享；imageSrc 是尚未上傳的圖片 bytes，只能下載。

圖片縮放必須改變 layout：用圖片 width/height 乘 fit box 與 zoom，不能只用 transform；量測 fit 前要移除 inline 尺寸，resize 在所有倍率重算，並解除 stylesheet 百分比 cap。pointer drag 與原生捲動共用 scrollLeft/scrollTop。控制列要在 scroll container 外，矮視窗的兩條高度 cap 都要扣除 overlay chrome。

單指 touch 交給圖片自己的原生捲動，避免 double-apply；雙指由 overlay 接管，frame 用 touch-action: pan-x pan-y，並以 non-passive touch handler 改 zoom。iOS 的 gesture 事件與 touch 事件用互斥旗標；不可用 user-scalable=no 或 maximum-scale=1 禁掉整頁縮放。

overlay portal 到 document.body，避免宿主 stacking context 困住它；需要 click-outside 的 caller 以 .md-preview 選擇器辨識，不再靠 ancestor contains。render 與 CT 查詢從 document.body/page 找 overlay。圖片、文件都要戴共用 .doc-md；source 開 breaks，url 不開。圖片展開鈕只在有本文的 incoming bubble，位置由 bubble padding 讓位。

## Esc 分層

只有 lib/escapeLayers.ts 綁 window keydown。useEscapeLayer(onEscape, ref, active) 以 DOM 包含關係找最內層；互不包含的面以後註冊者為 tie-break。被包住的面一定傳自己的 root ref，關閉中的常駐面以 active=false 退出。

層內要吞 Esc 就在 handler 判斷；input 自己的 Escape handler 也要 preventDefault，否則取消編輯會再關掉宿主面。這套 layer 不是 focus trap；不要在每個 modal 另綁 window listener。

## DocCard

DocCard 是設定頁可編輯長文件的共用外殼：標題、字數、版本入口、超上限阻擋、儲存確認與錯誤列；body 由 renderBody 提供。BootDocPage 使用它但保持唯讀，不能長回自己的 editor。新能力一律 optional，不傳就保留既有 caller 行為；不要加回已退場的 above、factoryReset、boot-doc.css 或 reset 分支。還原只在編輯模式的版本紀錄初始版本列提供。

文件是整份取代；編輯中字數讀 draft，超上限在送出前擋下，server 失敗顯示原話。沒有 usage 的文件不受 cap 影響。樣式由 settings.css 擁有；Insight、Lessons 與任務手冊尚未遷移，不要順手改。

## 差異呈現

lineDiff 只負責行結構，wordDiff 只負責相鄰取代列的 token，DiffView 只負責畫面。DiffView 永遠 render 完整 result.rows，不讀 hunks、不畫 @@；options 只暴露它真正擁有的 maxLines。相同版本與過大版本要是兩種不同空態，過大態要報行數。

token 標亮只在兩側有共同非空白 token 時做，每側有上限；顏色由真瀏覽器守衛驗，jsdom 只驗 tint 的結構位置。
