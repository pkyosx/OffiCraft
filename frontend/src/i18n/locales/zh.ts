import type { Effort } from "../../types";

export const zh = {
  orgName: "AI 工作室",
  user: "CEO（你）",
  common: {
    apply: "套用",
    cancel: "取消",
  },
  nav: {
    office: "辦公室",
    officeUnread: "有未讀訊息",
    replies: "請示",
    tasks: "任務",
    monitor: "監控",
    // 使用說明 — 主導覽最右的分頁(owner:「user guide 改放在 tab 中,監控的
    // 右邊」)。分頁標籤與頁面標題分開兩個 key:標籤要短,標題可以完整。
    guide: "使用說明",
    // 左上 logo = 回首頁的入口(aria-label/title)
    home: "回首頁",
  },
  // ── 使用說明(產品說明書)──
  // 從「設定 › 使用說明」升為主導覽分頁後,這三個字串不再屬於 settings 命名空間
  // (owner:「user guide 改放在 tab 中,監控的右邊,不要放在 settings 裡」)。
  guide: {
    title: "使用說明",
    loadError: "載入使用說明失敗，請稍後重試",
    empty: "還沒有說明頁",
  },
  // ── 任務頁(M3 任務卡)──
  tasks: {
    title: "任務",
    openTitle: "未結束",
    closedTitle: "已結束",
    // 空狀態 ×2(SPEC §2.3 指定文案)
    emptyNone: "目前沒有任務",
    emptyFiltered: "沒有符合篩選條件的任務",
    loadError: "載入任務失敗，請稍後重試",
    // 篩選列(任一生效顯「清除篩選」)
    clearFilters: "清除篩選",
    // 「所有人」→「所有負責人」(T-17be): 這顆篩的是 executor,但「所有人」在中文
    // 有兩讀 ——「所有的人」與「所有權人(owner)」——「所有」本身就是所有權的
    // 意思,而這個座艙裡真的有 owner 這個角色,兩讀都講得通。補上被篩的名詞就
    // 消歧義,也跟隔壁 filterExecutorNoun:「負責人」對齊。
    // 同類掃描過 en/xian,兩者都不動:en「Everyone」沒有所有權那一讀;
    // xian「眾人」的「眾」只有「多」的意思,也沒有。歧義是中文「所有」這個詞
    // 特有的,不是這三顆 key 共有的毛病 —— filterTypeAll/filterStatusAll 的
    // 「所有類型」「所有狀態」同理:後面已經接了被篩的名詞,本來就不會誤讀。
    filterExecutorAll: "所有負責人",
    filterTypeAll: "所有類型",
    filterStatusAll: "所有狀態",
    // 多選摘要用的量詞 — 選 2 項以上時顯「量詞 · N」(T-be18)
    filterExecutorNoun: "負責人",
    filterTypeNoun: "類型",
    filterStatusNoun: "狀態",
    outsource: "外包",
    unassigned: "未指派",
    adhoc: "自由代辦",
    // 卡頭 label column(T-705e):欄名等寬對齊,值以 chip 呈現。☑ #T-xxxx
    // 徽章移居徽章列(v2),不再帶欄名。
    typeLabel: "任務類型",
    assigneeLabel: "負責人",
    creatorLabel: "建立者",
    keyLabel: "識別鍵",
    // 舊任務無建立者資料 → 顯示「—」不可點
    creatorUnknown: "—",
    // 任務類型列(齒輪)點了跳該類型的設定頁
    typeSettingsLink: "開啟任務類型設定",
    // 負責人／建立者列點了開對應聊天視窗、輸入框帶 [T-xxxx] 前綴
    messageAssignee: "傳訊息給負責人",
    messageCreator: "傳訊息給建立者",
    // 前任列(T-ba04 轉派交接)：轉派後任務卡顯示「前任」給接手人交接對話
    previousAssigneeLabel: "前任",
    messagePreviousAssignee: "傳訊息給前任",
    // 外包執行者顯示「代號 · 模型 · 投入度」的投入度字樣
    effortOf: {
      low: "低投入",
      medium: "中投入",
      high: "高投入",
    } as Record<string, string>,
    // 八態(SPEC 核心名詞;文案照 spec,不用 mockup 的「等我核可/等待外部事件」)
    status: {
      not_started: "尚未執行",
      in_progress: "進行中",
      waiting_owner: "等我回覆",
      waiting_external: "等待外部",
      done: "已完成",
      terminated: "終止",
      duplicated: "重複",
    } as Record<string, string>,
    // 轉派中 LOCK 疊加徽章(T-9ca5):與 status 正交 —— 被轉派的任務保有推導狀態
    // 之餘,額外掛此標。reassigning 已不再是 status 值。
    lockReassigning: "轉派中",
    // 優先權四級(凍結 = 最低 + 暫停推進)
    priority: {
      high: "高",
      mid: "中",
      low: "低",
      frozen: "凍結",
    } as Record<string, string>,
    // 節點狀態徽章
    stepStatus: {
      pending: "待辦",
      in_progress: "進行中",
      done: "完成",
      waiting_owner: "等我回覆",
      // 等待外部(T-6f11):與任務層 status.waiting_external / 特殊徽章
      // stepWaitingExternal 同詞 —— 三處必須一致,兩層才讀成同一件事。
      // resolver(lib/stepBadge.ts)平常走特殊徽章;這條 map 項是它的後防,
      // 讓任何走到 plain status 徽章的 waiting_external 都不會漏出原始 key。
      waiting_external: "等待外部",
      // re-plan 凍結的已答卡節點(T-1aea):終態、灰階、只留問答史料
      superseded: "已取代",
    } as Record<string, string>,
    // 節點等待外部徽章(T-9ca5):步驟自身的「等待外部」,有別於 等我回覆
    stepWaitingExternal: "等待外部",
    // gate 預告(虛線)與生效(實心)同詞
    gateAnnounced: "等我回覆",
    // 綁定卡已非 waiting 時的 step 徽章(T-d64f):已答但 step 尚未被接手/已過期
    stepCardAnswered: "已回覆",
    stepCardExpired: "已過期",
    // 頭部:進度用 server 算好的 progress_done/total(SPEC §3.1);文案照
    // mockup「步驟 N/M」(owner 2026-07-13)
    progress: (done: number, total: number) => `步驟 ${done}/${total}`,
    elapsed: (t: string) => `已歷時 ${t}`,
    // 卡片預設摺疊;點整張卡切換展開(手機版重構 2026-07-17,chevron 已移
    // 除)——此兩句是卡片(role=button)的 aria-label
    expandCard: "展開工作流程",
    collapseCard: "收合工作流程",
    // 工作流程時間軸
    workflow: "工作流程",
    dod: "DoD",
    parallel: (n: number) => `同時進行 · ${n} 項並行`,
    // 過渡態(SPEC §3.1):外包未指派 → 等待指派;有執行者但零節點 →
    // 「等待 ○○ 建立 Steps」(○○ = 負責人顯示名,owner 核定 2026-07)
    waitingAssign: "等待指派",
    planningBy: (name: string) => `等待 ${name} 建立 Steps`,
    stepsLoading: "載入中…",
    stepsLoadError: "工作流程載入失敗",
    stepsRetry: "重試",
    // 等待外部:標籤而已 —— waiting_reason 本身是 agent 寫的自由文字(常帶
    // **粗體** / `反引號`),必須走 <Markdown> 渲染。所以這裡不能再是
    // `等待中 · ${reason}` 那種把內文一起吃進模板的形狀,否則前綴會被送進
    // markdown parser。分隔號 · 在 JSX 裡當字面量(同 tasks.progress 那行)。
    waitingLabel: "等待中",
    // deps:「等 T-xxxx」chip 可多筆(mockup 樣式,owner 2026-07-13)
    blockedBy: (taskNo: string) => `等 ${taskNo}`,
    // T-1d82:dep 指向的任務查不到(已刪 / 壞 id)。保留原始 id(那是僅剩的線索),
    // 但明說「查無此任務」,免得這列被讀成「連結壞了」。
    blockedByMissing: (depId: string) => `等 ${depId}(查無此任務)`,
    depJump: (taskNo: string) => `跳到 ${taskNo}`,
    // 識別鍵徽章(值為 URL 時外連)
    openKeyLink: "開啟連結",
    // 卡上訊息框(未指派時 disabled)
    messagePlaceholder: (name: string) => `傳訊息給 ${name}…`,
    send: "送出",
    messageError: "訊息送出失敗，請稍後重試",
    // owner 動作:狀態 badge 下拉(標記重複+終止,二次確認 — v5 從 ⋮ 搬過來,
    // ⋮ 本身隨後由 owner 裁示刪除);優先權改卡面 chip 就地編輯(v2)
    statusMenuLabel: "狀態操作",
    priorityLabel: "優先權",
    // 任務編號 chip 點擊複製(owner 2026-07-19 圈截圖):點 chip 把顯示的任務
    // 編號寫進剪貼簿,給一個短暫「已複製」回饋。copyTaskNo 是 chip 的 aria-label
    // (帶顯示號),taskNoCopied 是複製成功後的短暫提示文字。
    copyTaskNo: (taskNo: string) => `複製任務編號 ${taskNo}`,
    taskNoCopied: "已複製",
    // 等我回覆:跳到卡內嵌的等我回覆卡。v5 起這是狀態下拉裡的一個選項(owner 明示
    // 知情裁示:原本點 badge 一步到位跳卡,現在收進下拉變兩步)。
    statusJump: "查看等我回覆卡",
    // 等待外部:跳到那個 waiting_external 的節點(T-c514, owner 2026-07-20)。
    // 與 statusJump 同一族——兩者都是「帶我去卡住的地方」,所以並列在選單最前。
    // 這條在 T-c514 移除任務層 reason 顯示後才有必要:reason 現在只活在節點內,
    // 導航到節點就從方便變成必要。
    statusJumpExternal: "查看等待外部節點",
    terminate: "終止",
    terminateConfirmBody: (title: string) =>
      `確定要終止「${title}」嗎？任務將移入已結束區，無法恢復；後端會通知負責人做結束處理。`,
    terminateConfirm: "確認終止",
    // 標記重複(T-02c9):負責人指向原票即可收斂,免 owner 逐張終止
    markDuplicate: "標記重複",
    markDuplicateBody: (taskNo: string) =>
      `把「${taskNo}」標記為某張原票的重複?任務將移入已結束區、無法恢復。請選擇原票:`,
    markDuplicatePick: "請選擇原票",
    markDuplicateConfirm: "確認標記重複",
    duplicateOf: (taskNo: string) => `重複於 ${taskNo}`,
    duplicateJump: "跳到原票",
    actionError: "操作失敗，請稍後重試",
    // 轉派(T-160e,owner+特助限定):把任務交給另一位正職,或當場新起一位外包
    // (模型／投入度／機器同任務類型指派那套)。任務先進「轉派中」、雙方收到
    // 交接通知,由新負責人自己轉回進行中——前端不代轉。
    reassign: "轉派…",
    reassignTitle: (taskNo: string) => `轉派 ${taskNo}`,
    reassignBody:
      "任務會先進入「轉派中」,雙方都會收到交接通知;新負責人讀完交接後,自己把狀態轉回進行中。",
    reassignToMember: "轉給成員",
    reassignToOutsource: "轉外包",
    reassignPickMember: "請選擇要接手的成員",
    reassignPickMachine: "請選擇要運行的機器",
    reassignNoMembers: "沒有可轉派的成員",
    reassignNote: "交接備註(選填)",
    reassignNotePlaceholder: "想交代新負責人的事…",
    reassignConfirm: "確認轉派",
    reassignError: "轉派失敗，請稍後重試",
    // 內嵌請示卡(重用 M2 ReplyCardBody;可多張;內嵌在所屬 step 內)
    replyHeader: "請示",
    replyBadge: "等你回覆",
    replyInChat: "在聊天室回覆",
    // 審批持久標記:曾經開過卡/標過 gate 的 step,做完後仍看得出(owner
    // 2026-07-14:不消失的標記)
    gateMark: "審批",
    // 已回覆卡收合成一行摘要(可展開)
    replyAnsweredTag: "已回覆",
    expandReply: "展開回覆卡",
    collapseReply: "收合回覆卡",
    // 產物集(T-3dc5):任務卡上釘的交付物(檔案/圖片/連結)。徽章「產物 N」
    // 在彩色徽章列;點開浮層照檔案庫樣式分三籤。0 個產物時徽章不出現。
    artifacts: {
      badge: "產物",
      open: "查看產物",
      panelTitle: "產物",
      // T-49fb: the three tabs are gone (one list). What is left of the trio
      // is the image row's name fallback — an image artifact may carry neither
      // filename nor label, and its chip must never render empty.
      imageName: "圖片",
      empty: "還沒有產物",
      close: "關閉產物",
      remove: "移除產物",
      removeConfirm: "從任務卡移除這個產物?(不會刪除檔案本身)",
      downloadHint: "下載",
      openLinkHint: "開啟連結",
    },
  },
  // ── 請示頁(M2 回覆卡 B2)──
  replies: {
    waitingTitle: "請示",
    handledTitle: "近期已處理",
    handledHint: "你已回覆或標過期的事項 · 已回覆的一天內可重新決定",
    // 全部處理完的空狀態
    empty: "✓ 目前沒有待處理的請示",
    loadError: "載入請示失敗，請稍後重試",
    waited: (t: string) => `已等你 ${t}`,
    // 開卡/已回覆一律絕對時間含日期(如 7/13 09:05),不用相對或「今天」。
    openedAt: (time: string) => `開卡 ${time}`,
    answeredAt: (time: string) => `已回覆 ${time}`,
    expiredAt: (time: string) => `已過期 ${time}`,
    // 標為過期(owner 專用終態、不是回答、不可復原)——按鈕開二次確認
    expire: "標為過期",
    expireConfirm: "確認標為過期",
    expireConfirmBody: (summary: string) =>
      `要把「${summary}」標為過期嗎?此動作不可復原、也不算回答——成員會收到通知,問題還在的話他會重新開一張新卡。`,
    expireError: "標為過期失敗，請稍後重試",
    expiredTag: "已過期",
    expiredNote: "你未回答、已標為過期;若問題還在,成員會重新開卡",
    // 快捷選項第 1 個一律是 AI 的首選
    aiPick: "AI 建議",
    yourPick: "你選的",
    jumpToChat: "跳到原訊息",
    inputPlaceholder: "輸入回覆…",
    answerError: "回覆失敗，請稍後重試",
    // T-4166:409 不是暫時性失敗——這張卡已經不能再回覆了（任務已結束，或卡片
    // 已被處理）。叫使用者「稍後重試」是把他推上一條重試一百次都會失敗的路。
    answerStale: "這張卡已失效，無法回覆:它的任務已結束，或卡片已被處理。若仍列在待回覆，請用卡片上的「標為過期」收掉它。",
    // 已回覆卡:展開當初選項/重新決定
    viewOptions: "查看當初選項",
    collapseOptions: "收合選項",
    currentTag: "目前",
    redecide: "重新決定",
    redecideHint: "重新選一個，或直接打字改寫回覆",
    redecidePlaceholder: "或直接打字改寫回覆…",
    // §3.6 請示 → 任務：任務衍生的請示卡顯示精簡任務資訊（類型）＋跳轉;
    // 不露任務編號／識別鍵。純聊天請示不顯示。
    taskBadge: "任務",
    viewTask: "查看任務詳情",
  },
  office: {
    membersTitle: "辦公室成員",
    // 左欄頂部 tab(T-66a8 mockup 2026-07-18):正職／外包 文字 tab 切換,選中的
    // tab 有藍色底線。staffTitle 同時是 tab 標籤。
    staffTitle: "正職",
    // tab 下的小字計數:正職「N 人」。
    staffSub: (n: number) => `${n} 人`,
    // 側欄最下方「招攬新成員」鈕(依當前 tab 分流:正職→角色誌、外包→上限設定)。
    recruit: "招攬新成員",
    // T-3451: 列表列／聊天 header 當前任務的空狀態（沒有進行中的任務）。
    noCurrentTask: "無當前任務",
    role: {
      assistant: "特助",
    },
    // Presence 圓點的無障礙標籤(每個 lifecycle 視覺態一句)。圓點的「顏色」
    // 是給眼睛看的唯一 presence 訊號,螢幕閱讀器讀不到顏色 —— 所以同一份
    // presence 事實在這裡以文字形式提供(LifecycleDot 的 role="img"
    // aria-label)。名字旁那個「離線」文字徽章已移除(owner 2026-07-17:
    // 離線時綠點會變灰點,不必再多寫一次),這裡是離線狀態唯一的文字出口。
    presence: {
      offline: "離線",
      waking: "喚醒中",
      "online-awake": "線上",
      stopping: "停止中",
      stopped: "已停止",
    },
    // Roster avatar-button aria-label/title: the row itself opens the chat
    // (the old dedicated 聊聊 button is gone); the avatar alone opens the
    // member detail panel.
    viewProfile: "成員詳情",
    // Mobile single-page-nav back control: returns from a member's chat to the
    // roster (desktop keeps both panes, so this never shows there).
    backToMembers: "返回成員",
    // Honest load-failure notice — shown when the roster fetch REJECTED, so a
    // failed load never masquerades as an empty office (「成員 · 0」).
    loadError: "載入辦公室成員失敗，請稍後重試",
    // 跳到原訊息(#office/chat/<id>/msg/<msgId>)但該 chatId 已不在名單(成員被
    // 刪等):不再靜默落到 roster[0](Mira)——渲染這位對象的歷史對話(唯讀),
    // 標題誠實標明對象已不在。外包 worker 走下方 outsource.released* 專屬文案。
    chatUnavailableTitle: "對話對象已不在名單",
    chatUnavailableSub: "此成員已不在辦公室,以下為歷史對話(唯讀)",
    // ── 外包面板（SPEC §4：左欄成員列下方的可摺疊區塊）──
    // 每列只顯示「代號 · 任務狀態」＋任務標題（代號已隱含模型，不另寫模型;
    // 不顯示識別鍵與任務 ID）；點列＝開聊天頻道（標題「外包 · 代號」）。
    outsource: {
      title: "外包",
      // tab 下小字計數:外包「N 人」+「· 上限 M」後綴(cap 未載到則省略後綴)。
      workerSub: (n: number) => `${n} 人`,
      capSuffix: (cap: string) => ` · 上限 ${cap}`,
      // 外包身分標籤的單一來源(T-3ed8,owner 2026-07-20 裁決 完全一致):聊天
      // header/寄件者標籤、任務卡 chip、側欄外包列、監控頁 session 全走這支,
      // 「外包 · 代號」四處一致、不漂移。
      label: (codename: string) => `外包 · ${codename}`,
      // 並行上限;0 ＝ 暫停指派（要註明）
      paused: "已暫停指派",
      // 上限 popover（照 seth-member-2 mockup，owner 2026-07-13）；
      // -1 ＝ 無限（header 顯 ∞），0 ＝ 暫停指派
      capTitle: "外包上限設定",
      capHint: "設定同時可雇用的外包數量上限；設為無限則不限制。",
      capMaxLabel: "最多雇用",
      capUnlimited: "無限",
      capDecrease: "減少",
      capIncrease: "增加",
      capSave: "完成",
      capError: "沒存成，請再試一次",
      loadError: "載入外包清單失敗，請稍後重試",
      // 每列頭像＝開外包詳情面板（列身＝開聊天），照成員卡的雙擊區慣例。
      viewDetail: "外包詳情",
      // 第三行任務代號 chip 的 aria/title：點了跳 #tasks/<id> 任務頁定位。
      openTask: "開啟任務詳情",
      // 跳到原訊息時,外包 worker 已結案釋出、掉出 LIVE 名單(拿不到代號):不再
      // 落到 roster[0](Mira),改用這組誠實文案渲染其歷史對話(唯讀)。
      releasedChatTitle: "外包 · 已釋出",
      releasedChatSub: "此外包成員已結案釋出,以下為歷史對話(唯讀)",
    },
  },
  // ── 外包 worker 詳情面板（前端 only 精簡版：只放外包真有的欄位，
  // 不套用成員面板的機器綁定／model 編輯／context％／花費／refocus）──
  workerDetail: {
    back: "返回",
    codename: "代號",
    model: "模型",
    effort: "投入度",
    status: "狀態",
    // 外包生命週期狀態（assigned → active → released）；未知值原樣顯示。
    statusLabel: (s: string) =>
      (({ assigned: "已指派", active: "工作中", released: "已釋放" }) as Record<
        string,
        string
      >)[s] ?? s,
    task: "委託任務",
    delegator: "委託人",
    // 委託人＝owner 本人建的票時顯示（真實來源，非佔位）。
    delegatorOwner: "系統 Owner",
    // creator_id 為空（pre-column／排程自動建票）時的誠實 fallback，取代舊的
    // 一律假「System owner」。
    delegatorSystem: "系統排程",
    // ── T-f190：對齊成員詳情的真實資訊欄 ──────────────────────────────────
    machine: "機器",
    claudeAccount: "Claude Account",
    runtime: "運行狀況",
    context: "context",
    estimatedCost: "估計$",
    // presence（成員同一套詞彙——A案 P6）的誠實文案（不留空白假值）。
    notAssigned: "尚未分配",
    starting: "啟動中",
    offline: "離線",
    working: "工作中",
    // ── T-32e1/T-f190 生命週期操作（對齊成員詳情：換手／停止／換 model）──────
    stopped: "已停止",
    // 換手（refocus）：僅線上可觸發；送出後由外包端非同步重生，故保留「已送出」註記。
    refocus: "重新聚焦",
    refocusOfflineHint: "僅線上可重新聚焦",
    refocusing: "聚焦中…",
    refocusDone: "已送出",
    refocusError: "聚焦失敗",
    refocusSubmittedNote: "已送出重新聚焦 · 外包重生中…",
    refocusSinceLabel: (t: string) => `上次換手 ${t}`,
    // 停止／重啟（owner 明示；停止後座艙誠實顯示「已停止」，不自動救活）。
    stop: "停止",
    stopping: "停止中…",
    restart: "重新啟動",
    restarting: "啟動中…",
    stopError: "操作失敗，請稍後重試",
    // 換 model（沿用成員 model/effort 編輯器）。
    modelSave: "儲存",
    modelCancel: "取消",
    modelError: "儲存失敗，請稍後重試",
    modelNextSpawnNote: "工作中立即生效；已指派則下次啟動生效",
    // 改機器（owner-only）：picker 標題／確認、無線上機器提示。
    relocateTitle: "選擇要遷移到的機器",
    relocateConfirm: "遷移到此機器",
    noOnlineMachine: "沒有線上的機器",
    // 最近操作（沿用成員面板語意；P5b 後外包 verb 就是成員的 start／stop）。
    lastOp: "最近操作",
    lastOpStart: "啟動",
    lastOpStop: "停止",
    lastOpOk: "成功",
    lastOpFail: "失敗",
    lastOpLogLabel: "查看記錄",
    terminal: "終端 · TMUX",
    copyCommand: "複製指令",
    copied: "已複製",
    terminalHint: "在你自己的終端機貼上這行，即可接上這位外包的工作階段。",
    // 初始 PROMPT 預覽（boot-context）：外包沒存派工當下的逐字 persona，伺服器對
    // 目前的任務／手冊即時重組，故 hint 與 note 都要誠實標明「目前版本」。
    initialPromptHint: "目前版本重組",
    initialPromptNote:
      "此為依目前任務與手冊即時重組的預覽，非派工當下的逐字版本（任務／手冊事後修改過會有差異）。",
    dash: "—",
  },
  // ── Layer-4 lifecycle UI (aligned to backend's real five-state presence) ──
  // 5 visual-state names + 5 状态化 action-button labels + lifecycle surface
  // messages (T3.2). Copy only — no data contract touched.
  lifecycle: {
    action: {
      // 「生成」→「喚醒」(owner 驗收):按鈕的語意是喚醒既有成員,不是生出新的。
      spawn: "喚醒",
      cancel: "取消",
      stop: "停止",
      "force-stop": "強制停止",
    },
    message: {
      // 先關收尾:收尾中 → 壓縮中(dump)
      windDown: "收尾中…",
      dump: "壓縮中（dump）…",
      // 後起:resume-report(接下來要做什麼 + 手上事項)
      resumeReport: "接手回報 · 接下來要做什麼、手上有哪些事項",
      // degraded / 熔斷告警
      degraded: "服務降級 · 已觸發熔斷保護",
    },
  },
  login: {
    title: "登入 AI 工作室",
    passwordPlaceholder: "部署密碼",
    submit: "登入",
    submitting: "登入中…",
    error: "密碼錯誤，請再試一次",
  },
  // 首設密碼(全新安裝第一次打開座艙;啟用碼 = server 啟動訊息印出的一次性
  // claim token,證明你是這台機器的主人)。
  firstRun: {
    title: "設定管理密碼",
    intro: "第一次使用，先設定登入這個主控台的密碼。",
    claimPlaceholder: "啟用碼",
    claimHint: "啟用碼印在伺服器的啟動訊息裡，只有這台機器的主人拿得到。",
    passwordPlaceholder: "新密碼（至少 8 個字）",
    confirmPlaceholder: "再輸入一次新密碼",
    submit: "開始使用",
    submitting: "設定中…",
    errorClaim: "啟用碼不對，請再確認",
    errorTooShort: "密碼至少要 8 個字",
    errorMismatch: "兩次輸入的密碼不一樣",
    errorTaken: "密碼已經設定過了，請直接登入",
    gotoLogin: "前往登入",
  },
  // T-ba62 首次安裝自動化的結果橫幅:設完初始密碼後,server 會自己把這台機器的
  // warden 裝好、把助理叫醒。這個橫幅只在「沒有全部成功」時出現,因為成功時座艙
  // 上有一個醒著的助理本身就是訊號;失敗時它是使用者唯一看得到的「為什麼」。
  onboarding: {
    titleFailed: "自動設定沒有全部完成",
    intro: "設完密碼之後,系統會自動幫你裝好這台機器、叫醒助理。這次有一步沒過:",
    stepInstallWarden: "安裝這台機器",
    stepWakeAssistant: "喚醒助理",
    detailShow: "顯示詳細記錄",
    detailHide: "收起詳細記錄",
    dismiss: "知道了",
  },
  // ── 派送失敗告示（T-7fa1）──────────────────────────────────────────────
  // 「按了喚醒卻什麼都沒發生」的唯一出口：server 回 activation_pending 時，這
  // 段字取代原本永遠轉不完的「喚醒中…」。
  //
  // 🔴 文案的範圍必須等於那個 bool 的範圍（review r1 BLOCKER-1）。第一版寫
  // 「指令沒有送達目標機器」＋兩條「去看機器在不在線」——那是**指名了一個
  // server 沒有告訴我們的原因**。reviewer 用兩支 server probe 證明
  // activation_pending=true 也會發生在「上一次 START 還在飛」與「重試 backoff
  // 窗內」，而後者的「最近操作」已經寫了正確且相反的診斷（wake_timeout：指令
  // 有送出去、是機器上的 claude 沒登入）。一句具體但錯誤的因果，比原本的沉默
  // 更糟——它會把人推向錯的方向。所以現在只說 bool 真的知道的事，原因寫成兩
  // 條並列可能，並把人指回比這裡更準的那一行。
  dispatchAlert: {
    wakeTitle: "這次沒有送出喚醒指令",
    wakeBody:
      "這一次點擊沒有派出任何指令，成員不會因此醒來。喚醒意圖已經記下來，背景會繼續重試。",
    wakeStep1:
      "可能是目標機器（或它上面的常駐程式）沒有連上 —— 到「監控」看得到它在不在線。",
    wakeStep2:
      "也可能是前一次的指令還在重試中 —— 這個成員的「最近操作」若寫了原因，以那一行為準，它比這裡精確。",
    relocateTitle: "這次沒有送出搬移指令",
    relocateBody:
      "新機器已經指定好了，但這一次沒有派出搬移指令 —— 要收下這道指令的機器沒有連上。背景會繼續重試。",
    relocateStep1:
      "到「監控」看得到哪幾台機器不在線 —— 這道指令送不出去，就是因為要收下它的那一台沒有連上。",
    relocateStep2:
      "等那台機器連上，背景重試就會把這次搬移送出去 —— 不必重按，新指定的機器已經存下來了。",
  },
  profile: {
    title: "個人檔案",
    rename: "改名",
    renamePlaceholder: "輸入名字",
    preferences: "偏好設定",
    preferencesSub: "名稱、外觀、語言、密碼",
    logout: "登出",
    back: "偏好設定",
    theme: "主題",
    themeManageHint: "在「設定 › 主題」新增與編輯",
    themeOffice: "辦公室",
    themeImport: "匯入",
    themeExport: "匯出",
    themeEdit: "編輯",
    themeDelete: "刪除",
    themeImportTitle: "匯入主題",
    themeImportPlaceholder: "在此貼上主題 JSON…",
    themeChooseFile: "選擇 .json 檔",
    themeConfirmImport: "匯入",
    themeImportDup: "已有相同 id 的自訂主題",
    themeImportReadFailed: "讀取檔案失敗",
    themeLimitReached: "自訂主題數量已達上限",
    themeEditTitle: "編輯主題",
    themeNameLabel: "名稱",
    language: "語言",
    langZh: "中文",
    langEn: "English",
    changePassword: "修改密碼",
    changePasswordSub: "登入這個主控台用的密碼",
    currentPasswordPlaceholder: "目前密碼",
    newPasswordPlaceholder: "新密碼（至少 8 個字）",
    confirmPasswordPlaceholder: "再輸入一次新密碼",
    save: "儲存",
    saving: "儲存中…",
    pwdChanged: "密碼已更新",
    pwdErrorCurrent: "目前密碼不對",
    pwdErrorTooShort: "新密碼至少要 8 個字",
    pwdErrorMismatch: "兩次輸入的新密碼不一樣",
  },
  chat: {
    offlineTitle: (name: string) => `${name} 目前離線`,
    offlineHint: "這位成員目前不在線上，喚醒後才能開始對話。",
    // T-94c1: offline/stopped can now be messaged (queues until wake).
    offlineQueueHint: (name: string) =>
      `你仍可在下方留言，${name} 上線後就會讀到。`,
    // T-94c1 wake row (offline/stopped composer): queue notice + in-place wake.
    wakeQueueHint: (name: string) =>
      `${name} 目前離線中 — 訊息會排隊，或立即喚醒上線`,
    wakeButton: "喚醒",
    wakePending: "喚醒中…",
    emptyRange: "這個範圍還沒有訊息",
    inputPlaceholder: (name: string) => `回覆 ${name}…`,
    // M2-4 composer lock: shown IN PLACE OF the reply input while the member
    // is not online (offline / stopped / waking / stopping).
    composerOffline: (name: string) => `${name} 目前離線中`,
    // Whole-bar link variant — used when the caller wires onOpenDetail; 喚醒
    // 功能只在成員詳情面板,整條 bar 就是過去的入口。
    composerOfflineWake: (name: string) =>
      `${name} 目前離線中 — 前往成員面板喚醒`,
    me: "我",
    // 系統自動訊息的發話者標籤(T-ba04 轉派交接通知等,sender="system")
    systemSender: "系統",
    send: "送出",
    imageTooLarge: "圖片太大（上限 20 MB）",
    pastedImageAlt: "貼上的截圖",
    imageAlt: "聊天圖片",
    viewImageLabel: "檢視原圖",
    closeImageLabel: "關閉圖片",
    attachLabel: "附加檔案",
    attachTooLarge: (maxMb: number) => `檔案太大（上限 ${maxMb} MB）`,
    // 件數上限防呆:一則訊息最多帶 N 個附件(超過的不入列)。
    attachTooMany: (max: number) => `一則訊息最多附 ${max} 個檔案`,
    removeAttachmentLabel: "移除附件",
    downloadAttachment: "下載",
    read: "已讀",
    // M2 批次 19 未讀跳轉:往上滾時收到新訊息 → 視窗下方浮出的提示 chip;
    // 帶未讀進房時,第一則未讀訊息上方的細分隔線。
    newMessages: "有新訊息",
    unreadBelow: "以下為尚未閱讀的訊息",
    // T-bf82 往上捲載入更多:歷史撈完(hasMore=false)時,訊息串頂端的
    // 「已到最早訊息」標記。
    historyStart: "已到最早訊息",
    // 訊息流的 LINE 式日期分隔線(跨日處置中 pill;捲動時 sticky 浮在頂端)。
    // weekday 0=週日 … 6=週六;非今年才帶年份(LINE 慣例)。
    dateToday: "今天",
    dateYesterday: "昨天",
    dateOn: (month: number, day: number, weekday: number) =>
      `${month}月${day}日 (週${"日一二三四五六"[weekday]})`,
    dateOnYear: (year: number, month: number, day: number, weekday: number) =>
      `${year}年${month}月${day}日 (週${"日一二三四五六"[weekday]})`,
    // 兩個成員(agent)彼此對話的段落預設收合，避免洗版;點擊展開/收合。
    interAgentExpand: (count: number) => `${count} 則成員間對話 · 展開`,
    interAgentCollapse: "收合成員間對話",
    // M2-3 對話檔案/圖片庫:標題列圖示開啟的面板。M2 批次 16 起收錄該成員
    // 「全部對話」的附件(owner↔成員雙向 + 成員↔其他 agent 雙向),
    // 並分「圖片 / 檔案」兩個分頁(各自誠實空狀態)。
    tasksLink: "查看這位成員未完成的任務",
    roleSettingsLink: "開啟這個角色的定義設定",
    galleryLabel: "檔案與圖片",
    galleryTabImages: "圖片",
    galleryTabFiles: "檔案",
    galleryEmptyImages: "還沒有圖片",
    galleryEmptyFiles: "還沒有檔案",
    // M2 批次 18:上傳者篩選 chip 列(選項由實際附件的寄件者動態生成,
    // 與圖片/檔案分頁疊加生效)。
    gallerySenderFilterLabel: "依上傳者篩選",
    gallerySenderAll: "全部",
    galleryClose: "關閉檔案庫",
    galleryPreviewHint: "開新分頁預覽",
    galleryDownloadHint: "下載",
    // 檔案級永久分享連結(?sig= HMAC)— 複製到剪貼簿。
    copyShareLink: "複製分享連結",
    shareLinkCopied: "已複製連結",
    // .md 附件的座艙內預覽(T-a1c4):與下載分開的動作;overlay 內用
    // Markdown.tsx render(不是開新分頁看原始碼)。
    // T-7bc2: the chip itself is the trigger now — no separate "action" label.
    mdPreview: {
      download: "下載",
      close: "關閉預覽",
      loading: "載入預覽中…",
      error: "無法載入預覽",
    },
  },
  mp: {
    back: "返回",
    rename: "改名",
    renamePlaceholder: "輸入名字",
    wake: "喚醒",
    wakeManual: "手動喚醒",
    // 點喚醒後、server presence 尚未跟上前的即時回饋
    wakePendingNote: "喚醒中…",
    forceStopConfirmTitle: "強制停止?",
    forceStopConfirmBody: (name: string) =>
      `立即強制停止 ${name}——現在就砍掉 session、跳過正常收尾。進行中的未存工作會遺失。`,
    forceStopConfirmAction: "強制停止",
    forceStopBusy: "停止中…",
    model: "模型",
    effort: "EFFORT · 思考強度",
    effortLevel: (e: Effort) =>
      ({ low: "低", medium: "中", high: "高" })[e],
    // model/effort 可設定（M2-2）— launch intents, 變更於下次喚醒生效
    // （編輯鈕本身用全站共用的 settings.edit 樣式/文案）
    modelEffortSave: "儲存",
    modelEffortCancel: "取消",
    modelPlaceholder: "自訂模型字串（留空用預設）",
    claudeAccount: "Claude Account",
    modelEffortNextWakeNote: "變更於下次喚醒／換手生效",
    modelEffortError: "儲存失敗，請稍後重試",
    runtime: "運行狀況",
    machine: "機器",
    standby: "待命中",
    context: "context",
    refocus: "重新聚焦",
    refocusOfflineHint: "僅線上可重新聚焦",
    refocusing: "聚焦中…",
    refocusDone: "已送出",
    refocusError: "聚焦失敗",
    // persistent note after a refocus is submitted — the compaction happens on
    // the agent side asynchronously, so "已送出" (not "已完成") is the honest state.
    refocusSubmittedNote: "已送出重新聚焦 · agent 壓縮中…",
    refocusSinceLabel: (t: string) => `上次重新聚焦 ${t}`,
    // fleet remote-ops stage 1 — 最近操作 (last warden op receipt)
    lastOp: "最近操作",
    lastOpStart: "啟動",
    lastOpStop: "停止",
    lastOpOk: "成功",
    lastOpFail: "失敗",
    lastOpLogLabel: "查看記錄",
    estimatedCost: "估計$",
    terminal: "終端 · TMUX",
    copyCommand: "複製指令",
    copied: "已複製",
    terminalHint: "在你自己的終端機貼上執行，即可接上這個成員的 session。",
    initialPrompt: "初始 PROMPT",
    promptLoading: "載入中…",
    promptError: "讀取初始 PROMPT 失敗",
    lessons: "過往學習經驗",
    expandableHint: "下次喚醒／聚焦生效",
    lessonsLoading: "載入中…",
    lessonsError: "讀取學習經驗失敗",
    lessonsEmpty: "尚無學習經驗。",
    lessonsShared: "此角色的學習經驗(同一角色的成員共用)。",
    lessonsSaveError: "儲存學習經驗失敗",
    // ── 回呼端點 · WEBHOOK（M4）──
    webhook: {
      title: "回呼端點 · WEBHOOK",
      enabled: "啟用中",
      disabled: "已停用",
      add: "新增回呼",
      endpointIdLabel: "端點 ID",
      endpointIdPlaceholder: "如 pr-events，建立後不可改",
      purposeLabel: "用途說明",
      purposePlaceholder: "這個端點是做什麼用的（選填）",
      create: "建立",
      cancel: "取消",
      copy: "複製",
      copied: "已複製",
      deleteLabel: "刪除",
      deleteConfirm: "確定刪除這個回呼端點？token 會永久失效、無法復原。",
      createError: "建立回呼失敗（端點 ID 需為英數／_／-，且不可重複）",
      loadError: "讀取回呼端點失敗",
      empty: "尚未設定回呼端點",
      // ── 平台類型 / 簽章密鑰（M4 §2）──
      platformLabel: "平台類型",
      platformGeneric: "通用（僅 URL token）",
      platformSlack: "Slack",
      platformGithub: "GitHub",
      signingSecretLabel: "簽章密鑰 Signing Secret",
      signingSecretPlaceholder: "用於 HMAC 驗證的共享密鑰",
      signingSecretRequired: "Slack／GitHub 需填簽章密鑰",
      helperSlack: "Slack:填 App 的 Basic Information 頁面上的 Signing Secret。",
      helperGithub: "GitHub:填建立 webhook 時設定的 secret。",
      rotateSecret: "輪替密鑰",
      rotateSecretSave: "儲存密鑰",
      // ── 可觀測性計數（每列「事件統計」入口 → 顯示窗）──
      statsTitle: "事件統計",
      statsClose: "關閉",
      statsNever: "尚未收到請求",
      statsNeverHint:
        "這個端點還沒收到任何請求。從外部服務發一筆測試事件,就會出現在這裡。",
      statsLastReceivedLabel: "最後收到",
      statsDroppedLabel: "丟棄",
      statsAgo: (ago: string) => `${ago} 前`,
      dropReasonSigFailed: "驗簽失敗",
      dropReasonDisabled: "停用中被打",
      dropReasonMemberGone: "成員已不存在",
      requestsTitle: "最近請求",
      requestsLoading: "載入中…",
      requestsError: "讀取最近請求失敗",
      requestsEmpty: "尚無請求紀錄",
      outcomeDelivered: "已送達",
      outcomeDropped: "丟棄",
      outcomeChallenge: "驗證握手",
      outcomePing: "PING",
      requestHeaders: "HEADERS",
      requestBody: "BODY",
      requestBodyEmpty: "(空)",
      requestTruncated: "已截斷",
    },
    dash: "—",
  },
  // ── 機器選擇 / 搬移 agent（喚醒・重生・搬移時挑選線上機器）──
  machine: {
    // 無線上機器時,生成／喚醒按鈕停用的提示
    noOnlineMachine: "沒有線上的機器",
    picker: {
      label: "選擇機器",
      // 目前綁定的機器離線時,於清單中停用並標註
      offlineOption: (name: string) => `${name}（離線）`,
      spawnTitle: "選擇要運行的機器",
      spawnConfirm: "在此機器喚醒",
      relocateTitle: "選擇要遷移到的機器",
      relocateConfirm: "遷移到此機器",
    },
  },
  monitor: {
    dash: "—",
    // section titles (grey small-caps labels, per mockup)
    accountsTitle: "帳號資訊",
    machinesTitle: "機器資訊",
    sessionsTitle: "AI 會話",
    // inline-rename affordances (machine + account display_name)
    renameMachine: "機器改名",
    renameAccount: "帳號改名",
    renamePlaceholder: "輸入顯示名稱",
    renameError: "改名失敗",
    // §1 account cards
    accountsEmpty: "尚無帳號用量資料",
    estimate: "估計",
    fiveHour: "5 小時窗",
    sevenDay: "7 天窗",
    usage: "用量",
    time: "時間",
    overheated: "過熱",
    // 帳號詳情 modal(T-a9a7):該 claude 帳號背後的真實識別。email/org 來自
    // owner-only 的 account_label;任何缺值一律誠實顯示 "—",絕不猜。
    detail: {
      open: "帳號詳情",
      title: "帳號詳情",
      close: "關閉",
      accountKey: "歸戶 key",
      userId: "使用者 ID(雜湊)",
      orgUuid: "組織 UUID",
      email: "Email",
      org: "組織",
      labelRaw: "回報標籤原文",
      machines: "使用機器",
      estCost: "估計成本",
    },
    // §2 machine table headers
    machineCol: {
      machine: "機器",
      status: "狀態",
      claude: "Claude",
      account: "帳號",
      cpu: "CPU",
      ram: "RAM",
      battery: "電量",
      power: "電源",
    },
    // §3 session table headers
    sessionCol: {
      member: "成員",
      machine: "機器",
      account: "帳號",
      model: "模型",
      context: "context",
      estCost: "估計$",
    },
    // machine lifecycle: onboard (新增機器 / 上線) + teardown (拆除)
    machine: {
      actionsCol: "操作",
      copy: "複製",
      copied: "已複製",
      close: "關閉",
      // 機器列表(來源:GET /api/machines)
      machinesEmpty: "尚無機器,請先新增機器 / 上線",
      online: "線上",
      offline: "離線",
      // onboard — 虛線大按鈕點擊後,列表 inline 長出一列:填機器名,
      // Enter/確認建立、Esc/取消收回
      onboardEntry: "新增機器 / 上線",
      onboardNamePlaceholder: "機器名稱",
      onboardConfirm: "建立",
      onboardBusy: "新增中…",
      onboardError: "新增機器失敗",
      // ── 三動詞:安裝 / 解除安裝 / 刪除 (install / uninstall / delete) ──
      // 三顆按鈕標籤
      install: "安裝",
      uninstall: "解除安裝",
      deleteMachine: "刪除",
      // 離線機器沒有可解除安裝的 warden(按鈕停用時的提示)
      uninstallOfflineHint: "機器離線,沒有可解除安裝的 warden",
      // 解除安裝意圖已下、warden 尚未斷線 —— 與「安裝中…」同一套過渡態
      uninstallInProgress: "解除安裝中…",
      // install 對話框(非伺服器機器):單一畫面 —— 複製指令到該機器執行
      installTitle: "安裝機器",
      installRemoteHint:
        "複製下方指令,到那台機器上執行以安裝 warden。指令會重新產生一組 token。",
      // 複製安裝指令 (GET /boot-command,會重新產生 token)
      copyBootCmd: "複製安裝指令",
      copyBootCmdError: "取得指令失敗",
      // 於伺服器安裝的結果 (POST /bootstrap-here):僅失敗顯示(成功即消失)
      bootstrapBusy: "安裝中…",
      bootstrapError: "安裝請求失敗",
      // 伺服器有回錯誤細節時帶出(例:503 的 ocwarden binary 缺失原因)
      bootstrapErrorDetail: (detail: string) => `安裝請求失敗:${detail}`,
      bootstrapFailed: (exitCode: number) =>
        `安裝失敗(結束碼 ${exitCode}),原因如下:`,
      // T-ba62:成功也保留安裝記錄。原本成功分支把整份 log 丟掉,於是
      // 「裝好了」與「裝好了但裡面有警告」長得一模一樣。
      bootstrapSucceeded: "安裝完成,記錄如下:",
      // uninstall (POST /uninstall):驅動 uninstall RPC 給 warden(僅線上可用)
      uninstallConfirmTitle: "確認解除安裝",
      uninstallConfirmBody: (name: string) =>
        `確定要解除安裝「${name}」嗎？這會請該機器上的 warden 執行 ocwarden uninstall;成功後機器會變為離線,但記錄會保留(可再次安裝)。`,
      uninstallConfirm: "確認解除安裝",
      uninstallBusy: "處理中…",
      uninstallError: "解除安裝失敗",
      uninstallResultTitle: "解除安裝結果",
      uninstallDispatched:
        "已送出解除安裝指令 —— 待 warden 回報後,機器將變為離線。記錄已保留,可再次安裝。",
      uninstallAlreadyOffline:
        "機器已離線,視為已解除安裝 —— 未送出任何指令。記錄已保留,可再次安裝。",
      // uninstall 防呆:仍有成員「實際在線」於這台機器時,先跳警告
      // (離線但綁定在此的成員不計 —— 與 server 的 409 判準一致)
      uninstallWarnTitle: "尚有成員在這台機器上",
      uninstallWarnBody: (name: string, count: number) =>
        `「${name}」上還有 ${count} 位成員在線上。現在解除安裝會在成員仍在這台機器上時把 warden 拆除 —— 建議先將相關成員下線。仍要繼續嗎?`,
      uninstallWarnProceed: "確認繼續",
      // delete (DELETE /machines/{id}):純刪除記錄,不送任何 warden 指令
      deleteConfirmTitle: "確認刪除機器",
      deleteConfirmBody: (name: string) =>
        `確定要刪除「${name}」嗎?這只會從清單移除該機器的記錄,不會拆除機器上的 warden(那是「解除安裝」)。`,
      deleteConfirm: "確認刪除",
      deleteBusy: "刪除中…",
      deleteError: "刪除失敗",
      // ── 一鍵升級 (T-5f01,改版:併入操作鈕群,無版本欄) ──
      // 只有已安裝(warden 在線)的機器顯示;伺服器指紋比對說有新版(stale)
      // 才可按,最新/未知為 disabled(tooltip 說明原因)。按下後轉「升級中」,
      // 直到該機在之後的心跳收斂為最新才恢復。
      upgrade: "升級",
      upgrading: "升級中…",
      upgradeCurrentHint: "已是最新版",
      upgradeUnknownHint: "尚未回報版本指紋,無法判斷是否有新版",
      upgradeOfflineHint: "機器離線,無法下發升級(上線時會自動更新)",
      upgradeError: "升級指令下發失敗,請重試",
    },
  },
  settings: {
    title: "設定",
    // landing entries
    software: "軟體更新",
    roles: "角色誌",
    params: "參數調整",
    // ── 主題管理 (T-16a1 P3b): moved here from the profile dropdown ──
    themeManage: "主題",
    themeColorsSection: "顏色",
    themeColorPicker: "取色器",
    themeWordingSection: "用詞",
    themeWordingHint: "填入替代字即可覆蓋介面用詞;留空則維持原文。",
    themeWordingSearch: "搜尋用詞…",
    themeWordingOverride: "替代字",
    themeBuiltinTag: "內建",
    themeWordingTag: "用詞",
    // ── 字型 (T-16a1 P4): 從安全字型白名單挑內文／標題字型 ──
    themeFontsSection: "字型",
    themeFontsHint: "從內建的安全字型中挑選;維持預設則沿用主題原字型。",
    themeFontBody: "內文字型",
    themeFontTitle: "標題字型",
    themeFontDefault: "預設(主題字型)",
    themeDeleteConfirm: (name: string) =>
      `刪除主題「${name}」?此動作無法復原。`,
    // ── 軟體更新 (honest build-identity card) ──
    currentVersion: "目前版本",
    upToDate: "已是最新版",
    // 檢查更新(GET /api/release/check,直接問 GitHub Releases)
    checkUpdate: "檢查更新",
    checkingUpdate: "檢查中…",
    checkUnknown: "連不上 GitHub、查不到最新版本——請稍後再試",
    checkFailed: "檢查更新失敗,請重試",
    viewRelease: "查看 release",
    updateSettings: "更新設定",
    // ── 軟體更新 toggle(receive_beta / auto_update,皆預設關閉)──
    receiveBeta: "接收 Beta 版本",
    receiveBetaSub: "更新檢查也納入 GitHub 預發佈(prerelease)· 關閉 = 只看正式 release",
    autoUpdate: "自動更新",
    autoUpdateSub: "偵測到新版本時於背景自動升級並重啟 · 預設關閉",
    upgradeFailed: "升級失敗",
    upgradeRestarting:
      "升級中——新版本已安裝完成,伺服器重啟中;此頁面將自動重新載入。",
    upgradeTimeout:
      "伺服器未以新版本回應——請查看伺服器 log;舊版 binary 保留為 ocserverd.bak。",
    // only shown when update_available is true (never in M1) — no phantom version
    updateAvailable: "有可用的新版本",
    upgrade: "升級到最新版",
    catalogHash: "MCP 目錄雜湊",
    // ── 角色誌 ──
    // 全域情境 = boot context 的三塊（依組裝順序）：系統互動（唯讀 seed）→
    // 使用者自訂（owner 可編輯的追加塊）→ 啟動程序（唯讀 seed）。UI 不露檔名。
    globalSection: "全域情境（GLOBAL CONTEXT）",
    systemName: "系統互動",
    systemSub: "系統運作說明，注入給每個 agent · 唯讀",
    readOnlyBadge: "系統唯讀",
    customName: "使用者自訂",
    customSub: "追加到每個 agent 開機情境的自訂內容 · 可編輯",
    roleDefsSection: "角色定義",
    bootName: "啟動程序",
    bootSub: "工作室固定 SOP · 唯讀",
    bootBadge: "工作室 SOP",
    // seed vs owner-edited
    defaultBadge: "預設",
    // ── detail: view / edit ──
    edit: "編輯",
    doneEdit: "完成編輯",
    cancel: "取消",
    reset: "重置",
    editorPlaceholder: "以 Markdown 撰寫…",
    // doc filenames
    // Honest load-failure notice — shown when the role/global-context fetch
    // REJECTED, so a failed load never reads as "no roles defined".
    loadError: "載入角色定義失敗，請稍後重試",
    // ── 角色定義：新增／刪除（M2-2）──
    addRole: "新增角色定義",
    addRoleName: "角色名",
    renameRole: "重新命名角色",
    addRoleSubmit: "建立",
    addRoleCancel: "取消",
    addRoleError: "建立失敗，請確認角色名後再試",
    customBadge: "自訂",
    deleteRole: "刪除",
    deleteRoleConfirm: (name: string) =>
      `確定刪除角色「${name}」？該角色的成員及其對話、學習經驗將一併移除，無法復原。`,
    deleteRoleConfirmAction: "確認刪除",
    deleteRoleOnline: "有成員在線上，無法刪除",
    deleteRoleError: "刪除失敗，請稍後重試",
    // ── 參數調整(伺服器參數;原本住在頭像選單的偏好設定,owner 2026-07-12
    // 搬到設定頁,讓參數集中一處。文案沿用搬家前的 profile.* 白話命名)──
    paramsLoadError: "載入參數失敗，請稍後重試",
    paramsSaveError: "沒存成，請再試一次",
    sessionTtl: "登入有效期",
    sessionTtlSub: "登入後多久需要重新輸入密碼",
    ttl12h: "12 小時",
    ttl24h: "24 小時",
    ttl7d: "7 天",
    ttl30d: "30 天",
    handover: "自動換手門檻",
    handoverSub: "AI 同事的記憶用到這個比例，就自動交接給下一手（40–90%）",
    // ── 存檔回讀對帳（T-1c2e，rework 後住在軟體更新區：secret 只顯示
    // 已設定/未設定,絕不露明文;自動更新開關存檔後回讀對帳（寫入 → 重新
    // GET → 比對）,回饋誠實反映伺服器實際存了什麼）──
    configSecretSet: "已設定",
    configValueUnset: "未設定",
    configSaving: "存檔中…",
    configSaved: "已存檔，回讀對帳一致",
    // 失敗路徑蓋兩種情境：寫入被拒（伺服器值沒變）與 PATCH 成功但回讀
    // 對帳失敗（無從確認伺服器存了什麼）——文案不斷言伺服器狀態，只講
    // UI 的誠實事實：無法確認 + 顯示值回到伺服器最後確認的值。
    configSaveFailed: "無法確認已存檔——顯示值已還原為伺服器最後確認值，請再試一次",
    // ── 任務手冊（SPEC §5：任務類型／playbook 的定義與維護;與角色誌並列。
    // 不對使用者顯示內部檔名 — 手冊是內容，不是檔案）──
    manuals: "任務手冊",
    manualsLoadError: "載入任務手冊失敗，請稍後重試",
    manualsEmpty: "還沒有任務類型 — 從下方新增第一個",
    addManual: "新增類型",
    addManualName: "顯示名稱（例：審查 PR）",
    addManualSubmit: "建立",
    addManualCancel: "取消",
    addManualError: "建立失敗，請確認顯示名稱後再試",
    deleteManual: "刪除",
    deleteManualConfirm: (key: string) =>
      `確定刪除任務類型「${key}」？其手冊（定義、SOP、學習經驗）將一併移除，無法復原。`,
    deleteManualConfirmAction: "確認刪除",
    // 有非終態任務 → server 409;講人話
    deleteManualOpenTasks: "這個類型還有未結束的任務，先讓它們結束才能刪除",
    deleteManualError: "刪除失敗，請稍後重試",
    // 詳情頁籤
    manualTabDefinition: "任務定義",
    manualTabLearnings: "學習經驗",
    // 任務定義三題（§5.2 引導式定義表）
    manualDisplayName: "顯示名稱",
    manualDisplayNamePlaceholder: "取個好懂的名字（留空就顯示內部 ID）…",
    manualQ1: "這是什麼任務？",
    manualQ1Hint: "接案窗口讀這段，判斷進來的 trigger 該不該收成這類任務。",
    manualQ1Placeholder: "描述這類任務的用途…",
    manualQ2: "需要哪些資訊？",
    manualQ2Hint:
      "執行前一定要有的欄位。把其中一個設成 🔑識別鍵，接案窗口就用它判斷是不是同一個任務（例如同一個 PR 連結 = 同一個任務，後續訊息會併入而非開新任務）。",
    manualQ3: "該怎麼做？",
    manualQ3Hint: "執行手冊 · AI 參考它規劃 workflow",
    manualEmptyHint: "尚未填寫",
    manualFieldNamePlaceholder: "欄位名稱",
    manualFieldRequired: "必填",
    manualFieldOptional: "選填",
    manualFieldKey: "🔑 識別鍵",
    manualAddField: "新增欄位",
    manualRemoveField: "刪除欄位",
    manualNoFields: "尚未定義欄位",
    manualLearningsHint:
      "該類型累積的回饋與修正，跨任務沿用；agent 於任務結束時回寫，你也可手動增修。",
    manualSaveError: "儲存失敗，請稍後重試",
    // 負責成員設定卡（執行者在任務建立時由手冊決定;外包的模型/投入度/份數
    // 也在這裡設定，指派本身一律由伺服器執行）
    assigneeTitle: "負責成員",
    // 手冊 hub 摘要卡副標（照 mock-manual-detail mockup）
    assigneeSummarySub: "負責成員 · 同類型所有任務由他負責",
    assigneeHint:
      "這類任務由誰執行 — 指定成員，或外包（模型、投入度與份數在此設定，指派由伺服器執行）。",
    assigneeUnset: "未設定",
    assigneeKindMember: "成員",
    assigneeKindOutsource: "外包",
    // 編輯面（成員面板式，照 mock-manual-assignee-edit / seth-ui-3）
    assigneeToggleMember: "指定成員",
    assigneeToggleOutsource: "外包",
    assigneeModelLabel: "模型",
    assigneeModelPlaceholder: "模型（留空用預設）",
    assigneeEffort: "投入程度",
    assigneeMachineLabel: "機器",
    assigneeMachineAuto: "自動分配",
    assigneeMachineAutoHint: "挑最閒的一台",
    // 機器狀態字：讀現有 machines（online）＋ monitoring（agents 數）——
    // 線上且無 agent ＝ 閒置、線上有 agent ＝ 忙碌、離線 ＝ 離線（誠實映射）
    assigneeMachineIdle: "閒置",
    assigneeMachineBusy: "忙碌",
    assigneeMachineOffline: "離線",
    assigneeMachineNote: "指定的機器若當下離線，會自動改用「自動分配」。",
    assigneeCopies: "雇用數量",
    assigneeCopiesDecrease: "減少",
    assigneeCopiesIncrease: "增加",
    assigneeUnlimited: "無限",
    assigneeClear: "解除設定",
    assigneeNoMembers: "沒有可指定的成員",
    // 任務規劃段（hub 的兩張子頁入口卡）
    manualPlanningSection: "任務規劃",
    manualDefEntrySub: "這是什麼任務、需要哪些資訊、該怎麼做",
    manualLearnEntrySub: "過往任務累積的回饋與修正",
  },
};

export type Dict = typeof zh;
