import type { Effort } from "../../types";

export const zh = {
  orgName: "AI 工作室",
  user: "CEO（你）",
  common: {
    apply: "套用",
    cancel: "取消",
  },
  // ── 逐行比對（components/DiffView）——版本紀錄的差異呈現 ──
  diff: {
    ariaLabel: "逐行比對",
    beforeLabel: "先前版本",
    afterLabel: "目前內容",
    // +/- 記號的可讀名稱：顏色以外的第二條線索。
    addedLine: "新增的行",
    removedLine: "刪除的行",
    contextLine: "未變更的行",
    noChanges: "這兩個版本的內容完全相同",
    // 顯示方式：單欄（預設，上下對照）／兩欄對照。owner 2026-07-31 明確挑了
    // 單欄當預設——中文長句在兩欄裡會被擠窄。
    viewLabel: "比對方式",
    viewUnified: "單欄",
    viewSplit: "兩欄對照",
    // 「不摺疊」要說出來：這個介面以前會把離變更較遠的未變更內容摺起來，
    // 記得舊行為的人需要被告知它現在整份顯示。
    wholeDocNote: "整份顯示（不摺疊）",
    // 標題可以點的時候用的說明。owner 2026-09-03 c-944088dceab0：「兩份應該
    // 都要是連結」——存的一直是兩個指標，但畫面上點不進去，這一句是那個入口。
    openSide: (label: string) => `單獨打開「${label}」`,
    tooLargeLead: "內容太長，無法逐行比對（",
    tooLargeTail: " 行）。",
    // 這份比較的對外連結（server 簽章，沒有帳號的人也打得開）。owner 2026-09-03
    // 定調「1. 用圖示」，所以這三句就是這個控制項的全部名字；失敗那句一定要
    // 有——沒複製成功的畫面不可以長得跟複製成功一樣。
    copyShareLink: "複製對外連結",
    shareLinkCopied: "已複製對外連結",
    shareLinkCopyFailed: "複製連結失敗",
  },
  // ── 即時連線中斷提示（components/ConnectionBanner）──
  // 這條橫幅存在的理由只有一個:連線死掉的畫面跟「沒有新消息」的畫面長得一模
  // 一樣。使用者不會知道自己正在看一份凍結的快照(owner 2026-08-21 轉來的回報:
  // 「要 refresh page 才會更新」)。所以斷線這件事一定要說出來,而且要說出後果
  //(畫面可能不是最新的),不是只說「連線中」。
  connection: {
    lostTitle: "即時更新已中斷",
    lostBody: "正在自動重新連線，畫面上的內容可能不是最新的。",
    reload: "重新整理",
    ariaLabel: "即時連線狀態",
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
  // ── 傳承 Lore(T-33)───────────────────────────────────────────────────
  // 這個分頁的文案有一半在講「這一格沒有東西可填,而且缺的是哪一條路」。那不是
  // 佔位字,是這張票存在的理由:站上今天只有六條 lore route(寫入、搜尋、讀一條、
  // 讀一版、停用、恢復),而設計稿上有一半的區塊需要「列對象目錄」「列待審對象」
  // 「核可/合併」這些不存在的路。畫一個 0 上去,讀起來會是「我們查過,沒有」——
  // 所以這些區塊一律說出缺的是哪一條路,而且一個數字都不印。
  lore: {
    pendingEmpty: "沒有等你審核的對象。",
    pendingFailed: "讀不到待審清單：",
    pendingLoading: "載入中…",
    pendingEntries: (n: number) => `底下 ${n} 條記憶`,
    pendingNoEntries: "底下還沒有記憶",
    // 🔴 「底下 0 條」有兩種成因,處置完全相反,所以這裡是兩句話而不是一句。
    // 從來沒被用過 ⇒ 打錯字的形狀;曾經有但都退役了 ⇒ 跟名字對不對無關。
    pendingNeverUsed: "底下一條都沒有過 —— 這個名字鑄出來就沒再被用過",
    pendingAllRetired: (n: number) => `底下曾經有 ${n} 條,現在全部退役了`,
    pendingAlsoRetired: (n: number) => `(另有 ${n} 條已退役)`,
    pendingMintedBy: (who: string) => `由 ${who} 鑄出`,
    pendingMintedByUnknown: "沒有記錄是誰鑄出這個名字",
    pendingEntryListLead: "底下這幾條:",
    pendingEntryStatusSuperseded: "已被取代",
    pendingEntryStatusUnderspecified: "資訊不足",
    pendingSimilarLead: "像：",
    pendingApprove: "核可",
    pendingMerge: (name: string) => `併進 ${name}`,
    // ── 合併的單一入口(owner 2026-09-05 逐字:「改成單一入口:只留一顆合併
    // 鈕,按了列出候選讓你挑,再確認」)────────────────────────────────
    // 🔴 這四句刻意都是插值函式而不是靜態字串,原因不是文法:靜態字串葉會進
    // `messageKeys.generated.ts` 與 `server/ocserverd/message_keys_gen.go`,而
    // 這一輪的界線是「只改前端、不動產生檔」。四句都真的有東西要插,所以這不是
    // 為了繞閘門把靜態字硬折成函式 —— 但下一句要是插不出東西,就該去重新產生
    // 那兩個檔,而不是硬折。
    // 入口鈕上印候選數:按下去會看到幾個,按之前就知道。
    pendingMergeStart: (n: number) => `合併…（${n} 個候選）`,
    // 挑的那一步。這裡要說清楚「誰」要被併走,因為消失的是它。
    pendingMergePickLead: (name: string) =>
      `要把「${name}」併進哪一個？挑一個候選：`,
    // 送出鈕。沒挑的時候它是死的,而且要說得出為什麼死 —— 一顆沒有理由的灰鈕
    // 會被當成壞掉。
    pendingMergePickSubmit: (picked: string) =>
      picked === "" ? "先挑一個候選" : `下一步：確認併進 ${picked}`,
    // 🔴 確認那一步的正文。這整個改動存在的理由就在第二句:後端的合併是單向
    // 的,沒有 unmerge 路徑,按錯救不回來,所以畫面上必須明寫。
    pendingMergeConfirmBody: (from: string, into: string) =>
      `要把「${from}」併進「${into}」。這個動作無法還原：合併之後沒有拆回來的路。「${from}」這個名字不會被刪掉，它會變成「${into}」的別名 —— 之後用「${from}」寫入或搜尋都會落到「${into}」身上，底下的記憶也都算到「${into}」頭上。`,
    pendingBusy: "處理中…",
    pendingActionFailed: "這一筆沒有成功：",
    reasonSameNormalized: "大小寫／符號正規化後完全相同",
    reasonEditDistance1: "只差一個字",
    reasonEditDistance2: "差兩個字",
    reasonPrefix: "一個是另一個的開頭",
    reasonSubstring: "一個包含在另一個裡面",
    listCount: (n: number) => `共 ${n} 條記憶`,
    listTruncated: (n: number) => `這一頁只載入了最新的 ${n} 條，還有更多沒顯示。`,
    listLoading: "載入中…",
    listEmpty: "還沒有任何記憶被寫進來。",
    listFailed: "讀不到記憶清單：",
    listFilterPlaceholder: "輸入關鍵字即時篩選",
    listFilterNoHit: "沒有記憶符合這個關鍵字。",
    listNoSubject: "未歸類",
    listGroupExpand: "展開",
    listGroupCollapse: "收合",
    pendingTitle: "等你審核",
    entriesTitle: "記憶",
    title: "傳承",
    entryOriginLabel: "來自",
    entryOpen: "展開這一條",
    entryClose: "收起這一條",
    entryLoading: "讀取中…",
    entryFailed: "讀不到這一條，這是伺服器回的：",

    // ── 條目詳情 ──
    // 🔴 v8 把標題拉出來成為獨立的一格，這一句以前寫著「這一格也是這條的標題」
    // —— 那是 v7 的說法，而 v8 明文推翻它。留著會讓讀的人照著做錯：他會把
    // 「發生了什麼」寫進第 1 格，而第 1 格是索引鍵，寫成事實敘述就撈不到了。
    fieldHeading: "標題 · 發生了什麼（清單上只看得到這一行）",
    fieldTrigger: "什麼時候要記起來 · 對象 × 活動，這是別人找到這條的唯一路徑",
    fieldContent: "內容 · 只有這一段會進 agent 的記憶",
    fieldRetireWhen: "什麼時候不需要了",
    // 🔴 v8 把第 4 格從 problem（之前發生過什麼問題）改成 impact，因為問的東西
    // 換了：前者問起因，這一格問後果。
    fieldImpact: "impact · 原本想達成什麼，實際變成什麼",
    fieldImpactStars: "重要性 · 弄壞了什麼",
    fieldEvents: "相關的完整資訊 · 時／事／人／地／物",
    fieldEmpty: "（空白 —— 寫的人沒有填）",
    eventsEmpty: "這一條沒有掛任何事件。",
    eventWhen: "時",
    eventActor: "人",
    eventPlace: "地",
    eventObject: "物",
    eventBlank: "（沒有記下）",
    fieldsNote:
      "每一格都把欄位名印出來，空的也印；一筆事件都沒有的時候，事件那一節照樣在並且說出來。「空著」跟「沒有這一節」必須長得不一樣。事件裡的人／地／物空著是合法的，所以那三格標的是「沒有記下」而不是被填成「未知」——「查不出是誰」跟「還沒有人去查」是兩件不同的事。",
    detailStatusLabel: "狀態",
    detailWrittenByLabel: "最新一版誰寫的",
    detailSupersedesLabel: "取代了",
    originalTitle: "當初寫下的原文（最新版）",
    originalEmpty: "這一條沒有原文 —— 它是在這個機制存在之前寫的。",
    shaLabel: "摘要",
    shaEmpty: "（這份回應沒有帶摘要，所以沒辦法驗它跟存下來的是同一份）",
    revisionsTitle: "版本時間軸",
    revisionsEmpty: "這一條沒有任何一版的紀錄。",
    revisionLabel: "第 ",
    revisionLabelTail: " 版",
    revisionShrinkLead: "被磨掉 ",
    revisionShrinkTail: " 字",
    revisionNoShrink: "沒有被磨短",
    revisionView: "看這一版原文",
    revisionHide: "收起這一版",
    revisionFailed: "讀不到這一版，這是伺服器回的：",
    revisionsNote:
      "「被磨掉幾字」這一列是這個分頁最有價值的一格：條目被磨空的時候，條數一條都不會少，任何以「還剩幾條」為準的指標都不會動。",
  },
  notifications: {
    dismiss: "關閉提示",
    title: "開啟通知",
    description: "有新訊息或需要你決定的請示時，會通知這台裝置。",
    enable: "開啟通知",
    enabled: "此裝置已開啟通知",
    disable: "關閉通知",
    unsupported: "這個瀏覽器不支援推播通知。",
    denied: "通知權限已被封鎖，請在瀏覽器設定中允許 OffiCraft 通知。",
    failed: "通知設定失敗，請稍後再試。",
    contactRequired: "請先在個人選單的「通知信箱」填入通知信箱。",
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
    // 卡頭 label column(T-705e):欄名等寬對齊,值以 chip 呈現。☑ #任務編號
    // 徽章移居徽章列(v2),不再帶欄名。
    typeLabel: "任務類型",
    assigneeLabel: "負責人",
    creatorLabel: "建立者",
    keyLabel: "識別鍵",
    // 舊任務無建立者資料 → 顯示「—」不可點
    creatorUnknown: "—",
    // 任務類型列(齒輪)點了跳該類型的設定頁
    typeSettingsLink: "開啟任務類型設定",
    // 負責人／建立者列點了開對應聊天視窗、輸入框帶 [任務編號] 前綴
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
      max: "最高投入",
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
    progressLabel: "步驟",
    elapsedLabel: "已歷時",
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
    planningByLead: "等待",
    planningByTail: "建立 Steps",
    stepsLoading: "載入中…",
    stepsLoadError: "工作流程載入失敗",
    stepsRetry: "重試",
    // 等待外部:標籤而已 —— waiting_reason 本身是 agent 寫的自由文字(常帶
    // **粗體** / `反引號`),必須走 <Markdown> 渲染。所以這裡不能再是
    // `等待中 · ${reason}` 那種把內文一起吃進模板的形狀,否則前綴會被送進
    // markdown parser。分隔號 · 在 JSX 裡當字面量(同 tasks.progress 那行)。
    waitingLabel: "等待中",
    // T-cc3e:步驟備註 —— 這一步做到哪、下一步接什麼。同 waitingLabel,這裡
    // 只是標籤;備註本身是 agent 寫的自由文字(常帶 markdown),走 <Markdown>。
    stepNoteLabel: "備註",
    // T-e5b1:備註預設收起(owner:「不然太長了」)。這兩句是每一步的展開開關。
    // 收起時的字面留著「備註」兩字是刻意的 —— 有備註的步驟與沒備註的步驟,
    // 在收起狀態下就靠這顆按鈕在不在分辨。
    stepNoteExpand: "展開備註",
    // T-66:備註全文改成「點開才抓」(owner rc-4c8065fb30a5:「座艙改成點開才抓」)。
    // 卡片上只有大小(note_size_chars),全文要打一次 get_task_step —— 所以按下去
    // 到文字出現之間有一段真實的空窗,而且它會失敗。這兩句就是那兩個狀態:
    // 沒有它們,抓失敗的 overlay 會是一片空白,讀起來像「這一步的備註是空的」,
    // 而那正是卡片上的入口已經否定過的事(入口只在有備註時才畫)。
    stepNoteLoading: "讀取備註中…",
    stepNoteFailed: "讀取備註失敗,請關閉後再試一次。",
    // deps:「等 <任務編號>」chip 可多筆(mockup 樣式,owner 2026-07-13)
    blockedByLabel: "等",
    // T-1d82:dep 指向的任務查不到(已刪 / 壞 id)。保留原始 id(那是僅剩的線索),
    // 但明說「查無此任務」,免得這列被讀成「連結壞了」。
    blockedByMissingSuffix: "(查無此任務)",
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
    // T-e5b1(owner 2026-08-15):任務 UI 的標題／敘述就地編輯面已移除,整族
    // 文案跟著走。能力未動 —— 移掉的是畫面的詞彙,不是能力。
    // (T-646a 已把兩支工具合併成 `update_task`。)
    descEmpty: "尚未填寫敘述",
    // 任務編號 chip 點擊複製(owner 2026-07-19 圈截圖):點 chip 把顯示的任務
    // 編號寫進剪貼簿,給一個短暫「已複製」回饋。copyTaskNo 是 chip 的 aria-label
    // (帶顯示號),taskNoCopied 是複製成功後的短暫提示文字。
    copyTaskNoLabel: "複製任務編號",
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
    terminateConfirmBodyLead: "確定要終止「",
    terminateConfirmBodyTail:
      "」嗎？任務將移入已結束區，無法恢復；後端會通知負責人做結束處理。",
    terminateConfirm: "確認終止",
    // 標記重複(T-02c9):負責人指向原票即可收斂,免 owner 逐張終止
    markDuplicate: "標記重複",
    markDuplicateBodyLead: "把「",
    markDuplicateBodyTail:
      "」標記為某張原票的重複?任務將移入已結束區、無法恢復。請選擇原票:",
    markDuplicatePick: "請選擇原票",
    markDuplicateConfirm: "確認標記重複",
    duplicateOfLabel: "重複於",
    duplicateJump: "跳到原票",
    actionError: "操作失敗，請稍後重試",
    // 轉派(T-160e,owner+特助限定):把任務交給另一位正職,或當場新起一位外包
    // (模型／投入度／機器同任務類型指派那套)。任務先進「轉派中」、雙方收到
    // 交接通知,由新負責人自己轉回進行中——前端不代轉。
    reassign: "轉派…",
    reassignTitleLabel: "轉派",
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
    // 卡片一律收合成一行摘要(可展開),標籤說明它現在的狀態
    replyAnsweredTag: "已回覆",
    replyWaitingTag: "待回覆",
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
      removeConfirm:
        "從任務卡移除這個產物？目前指向的檔案會保留，但這個產物若曾被取代，保留下來的每個舊版本都會連同檔案一起永久刪除。",
      loading: "載入產物中…",
      loadFailed: "產物讀取失敗,請關掉再打開試試",
      downloadHint: "下載",
      openLinkHint: "開啟連結",
      // ── T-60: a pinned deliverable can be REPLACED, keeping its id. The row
      // gets a versions entry only when there is more than one version to look
      // at; the reader behind it is read-only (there is no restore verb).
      versionsEntry: "查看版本",
      versionsCountTail: "版",
      versionsTitle: "產物版本",
      versionsClose: "關閉版本",
      versionsPaneLabel: "檢視",
      versionsPaneContent: "內容",
      versionsPaneDiff: "差異",
      versionsCurrent: "目前版本",
      versionsVersionLabel: "版本",
      versionsByLabel: "修改者",
      versionsEmpty: "沒有更早的版本",
      versionsLoading: "載入中…",
      versionsLoadError: "讀不到版本紀錄",
      versionsContentError: "讀不到這個版本的內容",
      versionsContentGone: "這個版本沒有指向任何內容",
      versionsUnnamed: "未命名",
      versionsUnpinned: "這個產物已經不在任務上了",
      versionsOpaqueLead: "這不是文字檔(",
      versionsOpaqueTail: "),只能切換前後各看一次。",
    },
  },
  // ── 請示頁(M2 回覆卡 B2)──
  replies: {
    waitingTitle: "請示",
    handledTitle: "近期已處理",
    handledHint: "已回覆或已標為過期的事項 · 已回覆的可重新決定",
    // 全部處理完的空狀態
    empty: "✓ 目前沒有待處理的請示",
    loadError: "載入請示失敗，請稍後重試",
    waitedLabel: "已等你",
    // 開卡/已回覆一律絕對時間含日期(如 7/13 09:05),不用相對或「今天」。
    openedAtLabel: "開卡",
    answeredAtLabel: "已回覆",
    expiredAtLabel: "已過期",
    // 標為過期(終態、不是回答、不可復原)——按鈕開二次確認。
    // ⚠️「owner 專用」這句在本包之前就已經是假話,不是本包造成的:T-6020(owner
    // 2026-07-26)起 admin 助理也按得動;owner 2026-08-07 於卡 rc-3ff94b116970
    // (T-1b88)又把「該卡的作者本人」加進 API 層(自己開的、還沒被回答的卡自己撤得回)。
    // 座艙這顆鈕仍然是 owner 面的入口,行為沒變。⚠️ 但**字串**改了:過期卡的說明與
    // 「近期已處理」副標原本用第一人稱寫「你標的」,而作者自撤的卡也顯示在同一處
    // ——卡上沒有「誰按的」欄位,前端分辨不了,所以措辭改成中性、不宣稱按的人是誰。
    expire: "標為過期",
    expireConfirm: "確認標為過期",
    expireConfirmBodyLead: "要把「",
    expireConfirmBodyTail:
      "」標為過期嗎?此動作不可復原、也不算回答——成員會收到通知,問題還在的話他會重新開一張新卡。",
    expireError: "標為過期失敗，請稍後重試",
    expiredTag: "已過期",
    expiredNote: "未被回答、已標為過期;若問題還在,成員會重新開卡",
    // 帶 ai_pick 的那個選項才是 AI 的建議;位置不再有任何意義。
    aiPick: "AI 建議",
    // 多選卡的暫存計數:勾了幾項在送出前就寫出來——多選卡上「什麼都沒勾」與
    // 「全都要」在畫面上只差哪幾顆亮著,所以把數字寫出來。
    // 選項上方的一行:同時說出卡的種類,以及「按下去會發生什麼」。後者是真正
    // 護著人的那半 —— 單選卡點一下就送出,而請示卡是一次性的、送出去收不回來,
    // 所以「以為是多選、點來試試看」必須在點之前就被擋住。勾/圓鈕已經說了種類,
    // 這行說的是後果。
    selectedCountLead: "已選",
    // zh does not inflect for number, so both forms are the same word — the
    // split exists for the locales that do (see interAgentExpandOne/Many).
    selectedCountTailOne: "項",
    selectedCountTailMany: "項",
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
    // §3.6 請示 → 任務：任務衍生的請示卡顯示精簡任務資訊（標題）＋跳轉;
    // 不露任務編號／識別鍵／類型。純聊天請示不顯示。
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
      // header/寄件者標籤、任務卡 chip、側欄外包列、監控頁 session 全走這支
      // (compose.ts 的 outsourceLabel,「外包 · 代號」四處一致、不漂移)——
      // T-081b 起那支直接組上面的 title,不再另存一份同字的模板。
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
      // 外包 worker 已結案釋出、掉出 LIVE 名單(拿不到代號)時的誠實身分。
      // 🔴 這兩片葉子是「已結案」這個事實的**唯一一份文案**(owner 2026-07-31:
      // 「為什麼從不同進入頁面會有不同的顯示方式?不是應該要一致嗎」)。能看到一個
      // released worker 的入口有兩個 —— 聊天室與直接開的詳情面板 —— 兩邊讀的都是
      // 這裡。所以措辭刻意寫成**與入口無關**:原本的「以下為歷史對話」對聊天室為真、
      // 對面板是假話,一旦為了面板另外複製一份字串,就正是這次要修的病。
      // ⛔ 不要因為「聊天室那句更貼切」而在任何一邊加第二份字串。
      releasedTitle: "外包 · 已釋出",
      releasedSub: "此外包成員已結案釋出,這裡是唯讀的歷史紀錄",
    },
  },
  // ── 外包 worker 詳情面板（前端 only 精簡版：只放外包真有的欄位，
  // 不套用成員面板的機器綁定／model 編輯／context％／花費／refocus）──
  workerDetail: {
    back: "返回",
    codename: "代號",
    model: "模型",
    effort: "投入度",
    // T-7526（owner 2026-07-31）：狀態欄與它的 statusOf 對照表整個退場。四個狀態
    // 字與身分卡那顆 LifecycleDot 完全重複，「已釋放」則由聊天室橫幅承擔。
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
    // T-7526：啟動中／離線／工作中／已停止 四個 presence 字隨狀態欄一起退場——
    // 它們是 LifecycleDot 的 aria-label（office.presence.*）的第二份副本。
    // ── T-32e1/T-f190 生命週期操作（對齊成員詳情：換手／停止／換 model）──────
    // 換手（refocus）：僅線上可觸發；送出後由外包端非同步重生，故保留「已送出」註記。
    refocus: "重新聚焦",
    refocusOfflineHint: "僅線上可重新聚焦",
    refocusing: "聚焦中…",
    refocusDone: "已送出",
    refocusError: "聚焦失敗",
    refocusSubmittedNote: "已送出重新聚焦 · 外包重生中…",
    refocusSinceLabel: "上次換手",
    // 停止（owner 明示；停止後不自動救活）。
    // ⚠️ 沒有 restart 這條字了（owner 2026-07-31「應該要統一」）：喚醒的字一律用
    // 正職那一份 `lifecycle.action.spawn`＝「喚醒」，兩個面板同一個葉子，主題包
    // 換詞也只換一次。REST 路徑仍是 /restart（凍結 wire），只有字退場。
    stop: "停止",
    stopping: "停止中…",
    stopError: "操作失敗，請稍後重試",
    // 換 model（沿用成員 model/effort 編輯器）。
    modelSave: "儲存",
    modelCancel: "取消",
    modelError: "儲存失敗，請稍後重試",
    modelNextSpawnNote: "工作中立即生效；已指派則下次喚醒生效",
    // 改機器（owner-only）：picker 標題／確認、無線上機器提示。
    relocateTitle: "選擇要遷移到的機器",
    relocateConfirm: "遷移到此機器",
    noOnlineMachine: "沒有線上的機器",
    // 最近操作（沿用成員面板語意；P5b 後外包 verb 就是成員的 start／stop）。
    lastOp: "最近操作",
    lastOpStart: "喚醒",
    lastOpStop: "停止",
    lastOpOk: "成功",
    lastOpFail: "失敗",
    lastOpLogLabel: "查看記錄",
    terminal: "終端 · TMUX",
    copyCommand: "複製指令",
    copied: "已複製",
    terminalHint: "在你自己的終端機貼上這行，即可接上這位外包的工作階段。",
    // 初始 PROMPT 預覽（boot-context）：外包沒存派工當下的逐字 persona，伺服器
    // 用同一套組裝即時重組，故 hint 與 note 都要誠實標明「目前版本」。
    // T-4595 起這份就是正職那份扣掉整個 persona——角色說明、判準、長期筆記
    // （外包沒有角色，這三份都跟著沒有）——裡面不含任務也不含手冊 —— 舊文案寫
    // 「依目前任務與手冊重組」已經是假的。
    initialPromptHint: "目前版本重組",
    initialPromptNote:
      "此為依目前開機說明即時重組的預覽，非派工當下的逐字版本（開機說明事後修改過會有差異）。內容就是正職那份扣掉整個 persona（角色說明、判準、長期筆記）——外包沒有角色，這三份都跟著沒有；任務與手冊不在裡面，它開機後自己去領。",
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
      // 升級路徑的中間段（owner 2026-08-21「停止 → 加速停止 → 強制停止」）：
      // 給已經開始的收尾上一段時鐘，並且把那個時刻告訴他。不是殺，所以不做二次確認。
      // 🔴 owner 2026-08-22：這三個詞是同一顆按鈕在三個階段的字，不是三顆並排的
      // 按鈕（「他應該體感上像是同一個按鈕 升級的概念」）。字沒變、鍵沒變，變的是
      // 同一格會依階段換成哪一個。
      "accelerated-stop": "加速停止",
      "force-stop": "強制停止",
    },
    // 那一格只有兩種「畫得出來但按不下」的情況，而且都要說明原因。
    reason: {
      alreadyStopping: "已經在收尾中了；等收尾上了時鐘，這顆會自己升級成加速停止",
      justAppeared: "剛剛升級成這一段；先停一下，免得連按兩下替你再升一級",
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
    // 刻意同時點出兩個欄位:server 對「密碼錯」與「驗證碼錯」回同一個 401,
    // 因為指名是哪一半錯,等於向只猜對一半的人確認密碼是對的。
    // 所以登入牆也無從得知 — 不該假裝知道。
    errorWithCode: "密碼或驗證碼錯誤，請再試一次",
    codePlaceholder: "6 位數驗證碼",
    codeHint: "來自你的驗證器 App",
    // 429 + Retry-After:同時驗證中的名額額滿,不是密碼錯,也不是嘗試次數 ——
    // 那個計數器已經不存在了。
    //
    // 用 lead/tail 靜態葉子、由 i18n/compose.ts 組裝 —— 不在 dictionary 直接放
    // interpolation function。theming-and-i18n.md 禁止後者,理由不是美觀而是
    // 「看不見」:message key 白名單只收 string leaf,所以 template function 裡
    // 的每一個字都無法被主題的 wording 覆寫、也不會出現在生成的 key 清單裡,
    // 於是 drift 閘門一路綠燈,而那句話其實永遠改不動。
    throttledLead: "目前同時處理的登入太多，請於",
    throttledTail: "秒後再試。",
    // 當一次被拒的登入其實是「少了驗證碼」而不是「密碼錯」時顯示 —— 也就是這面
    // 牆本來是過期的、剛剛才長出驗證碼欄位。必須解釋欄位為什麼突然出現，否則
    // owner 會讀成密碼錯了。
    codeNowRequired: "這台 server 現在需要驗證碼，請輸入驗證器 App 顯示的那一組。",
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
    intro: "設完密碼之後,系統會自動幫你裝好這台機器、喚醒助理。這次有一步沒過:",
    stepInstallWarden: "安裝這台機器",
    stepWakeAssistant: "喚醒助理",
    // ── 失敗原因(T-0648)────────────────────────────────────────────────
    // 這幾句取代 server 送來的 reason。server 那一份是寫給工程師看的英文,橫幅
    // 其他每一個字都是讀的人的語言,只有那一句不是——而一句 server 端組出來的
    // 字串,座艙翻不動。鍵名就是 server 的 code(closed vocabulary)。
    //
    // 🔴 這裡只翻「整句都是固定文字」的那幾個 code。server 另外四個 code
    // (installer_unrunnable / uninstall_intent / wake_not_recorded /
    // wake_undispatched)的句子裡嵌著一段 Go 的錯誤字串,那段字就是診斷本身,
    // 翻成中文會把它弄丟——所以刻意不列在這裡,由座艙原樣顯示 server 的
    // reason。少一句中文,好過少一段診斷。
    //
    // ⚠️ 譯文是寫給使用者看的:exit code、launchd label 這種東西不進來(它們
    //    在下面的「詳細記錄」裡,那一區維持原文,那是給工程師的)。
    reasons: {
      install_failed:
        "這台機器沒有安裝成功,所以助理沒有被喚醒——喚醒一台沒裝好的機器,只會留下一個沒有原因的灰色成員。下面的詳細記錄是安裝當下的完整輸出。",
      roster_missing:
        "這台伺服器自己的機器紀錄不在名冊裡,出廠設定沒有跑完。把伺服器重開再試一次。",
      assistant_missing:
        "出廠附的那位助理不在名冊裡,出廠設定沒有跑完。把伺服器重開再試一次。",
      interrupted:
        "自動設定跑到一半被打斷了(伺服器在那當下重開),所以沒有做完。請到 監控 › 機器 › 「安裝」 自己裝這台機器,再把助理叫上線。",
      faulted: "自動設定中途出錯停住了。伺服器的記錄裡有當下的細節。",
    },
    detailShow: "顯示詳細記錄",
    detailHide: "收起詳細記錄",
    dismiss: "不再顯示",
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
  //
  // T-66a2 補充：wake_timeout 不再只有一句。server 若能證明那個 frame 被 pop
  // 出來卻沒寫上 socket，receipt 會改寫成「指令從來沒送到那台機器、別去看那
  // 台機器的 claude」——在「真的沒送到」的情況下反而和這段文案一致。這裡的
  // 推理不變：這段字知道的仍然比 last_op_reason 少，還是要把人指回那一行。
  //
  // T-b3d0 後續再補一種：目標機器**回報過**自己沒有 Claude Code、而 codex 是
  // 好的時候，receipt 會改寫成「把這位成員的執行環境改成 Codex」，不再叫人去
  // 看那台機器上根本不存在的 claude。結論一樣：last_op_reason 比這段字準。
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
  // ── 主題的「身分名稱」(T-081b §6) ────────────────────────────────────────
  // 這個子樹裡的每一條,都是某個主題自己的 name —— 主題下拉選單裡的那一列、
  // 匯出檔案裡寫進去的 name、新建主題的預設名。主題包的 wording 覆寫**不准**
  // 碰它們:一旦可覆寫,匯入「精靈村」就會把內建主題也改叫精靈村,使用者從此
  // 找不到回去的那一列(owner 2026-07-27 回報)。
  //
  // 因此白名單產生器(scripts/gen-message-keys.mjs)整個跳過 themeIdentity 子樹
  // —— 規則掛在**結構**上,不是另外手維一份 key 清單:以後多一個內建主題,把它的
  // 名字放進這裡就自動不可覆寫。
  //
  // ⚠️ 場所稱呼不在這裡:導覽列的「辦公室」是 nav.office,照舊可被主題包換掉。
  themeIdentity: {
    office: "辦公室",
    newTheme: "新主題",
  },
  // ── 「內建 / 自訂」標籤 ────────────────────────────────────────────────────
  // 這裡放的不是任何主題的名字,而是「設定 › 主題」清單用來分組的標籤。所有講
  // 「內建 / 自訂」的介面共用這一份語意來源。
  //
  // 第三、四輪曾把它們設為不可覆寫,免得主題包把「自訂」改成「內建」讓分組說謊。
  // 第八輪放開 —— owner:「這是大家自己用的,自己要怎麼搞我們不用特別管,我們只要
  // 確定主題名稱不會隨著主題改變就好」。它們回到一般可覆寫用詞;上面的
  // themeIdentity 才是主題包唯一碰不到的子樹。
  themeMarkers: {
    builtinGroup: "內建",
    customGroup: "自訂",
  },
  profile: {
    title: "個人檔案",
    rename: "改名",
    renamePlaceholder: "輸入名字",
    preferences: "偏好設定",
    preferencesSub: "名稱、外觀、語言、版面、通知、密碼",
    logout: "登出",
    back: "偏好設定",
    theme: "主題",
    themeManageHint: "在「設定 › 主題」新增與編輯",
    themeAdd: "新增",
    themeImport: "匯入",
    themeExport: "匯出",
    themeEdit: "編輯",
    themeDelete: "刪除",
    themeImportTitle: "匯入主題",
    themeImportPlaceholder: "在此貼上主題 JSON…",
    themeChooseFile: "選擇 .json 檔",
    themeConfirmImport: "匯入",
    themeImportLinkLabel: "…或貼一條連結匯入",
    themeImportLinkPlaceholder: "https://…/theme.json",
    themeImportFromLink: "抓取並匯入",
    themeImportLinkWorking: "抓取中…",
    themeImportLinkFailed: "抓不到那條連結",
    themeImportLinkShareNote:
      "分享連結沒有身分、也不會過期——連得到這台站又拿到連結的人都讀得到這套主題,包含裡面的私人圖片。單一條連結收不回來;要作廢只有一個很粗的辦法:到〈設定 › 簽章金鑰〉移除當初簽它的那把金鑰,那會讓同一把金鑰簽過的所有連結一起失效。",
    themeImportDup: "已有相同 id 的自訂主題",
    themeImportReadFailed: "讀取檔案失敗",
    themeLimitReached: "自訂主題數量已達上限",
    themeImportSkippedLead: "已匯入,但有",
    themeImportSkippedMid: "個用詞代碼不認得、已略過:",
    themeImportSkippedMore: "等",
    themeEditTitle: "編輯主題",
    themeNameLabel: "名稱",
    language: "語言",
    langZh: "中文",
    langEn: "English",
    pushContactEmail: "通知信箱",
    pushContactEmailSub: "推播服務用來識別這個座艙的公開信箱；未填時不會送出通知。",
    pushContactEmailPlaceholder: "name@company.com",
    pushContactEmailError: "請填入可公開使用的信箱。",
    layout: "版面",
    layoutNarrow: "窄版",
    layoutWide: "寬版",
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
    // 共用「同時驗證中」名額額滿回的 429,不是失敗次數——那個計數器已經不存在,
    // 所以登入打錯幾次不可能觸發這個。少了這個分支會顯示成「目前密碼不對」,
    // 於是 owner 會被告知他正確的密碼是錯的。要等的是一下下,不是幾分鐘。
    pwdErrorThrottled: "目前同時處理的驗證太多 — 請稍候再試",
    // ── 第二因子(TOTP) ──
    mfa: "兩步驟驗證",
    mfaSubOff: "未開啟 — 你的密碼是唯一的鑰匙",
    // 出貨旗標。獨立一句,因為「這台 server 沒開放這個功能」跟「你還沒去設定」
    // 是兩件事;混在一起會讓人去找一個刻意不存在的按鈕。
    mfaSubUnavailable: "這台 server 未啟用此功能",
    mfaOfferIntro:
      "這台 server 尚未開放兩步驟驗證。開放之後才能設定——這只是讓選項出現，不會替任何人開啟。",
    mfaOfferOn: "為這台 server 開放兩步驟驗證",
    mfaOfferOff: "關閉這台 server 的此功能",
    // 明講,因為這正是這個旗標的安全性重點。
    mfaOfferOffHint:
      "這只會把設定入口收起來。已經開啟的第二因子仍然會在登入時被要求，也仍然可以從上面關掉。",
    mfaErrorOffer: "無法變更這個設定",
    mfaSubOn: "已開啟 — 登入時需要驗證器的驗證碼",
    mfaIntro:
      "每次登入都要再輸入一次手機驗證器 App 的驗證碼。如果這台 server 可以從外部連進來，建議開啟。",
    mfaEnrollStart: "設定兩步驟驗證",
    mfaEnrollStarting: "準備中…",
    mfaScanQrHint: "用驗證器 App 掃描，或手動輸入下面的設定金鑰。",
    mfaQrAlt: "兩步驟驗證的設定 QR code",
    mfaScanHint: "把這組金鑰加進驗證器 App，然後輸入它顯示的驗證碼來確認。",
    mfaSecretLabel: "設定金鑰",
    mfaOpenInApp: "用驗證器 App 開啟",
    mfaCodePlaceholder: "6 位數驗證碼",
    // 啟用時要重新驗密碼:裝上一個因子和移除它一樣具破壞性,被偷的 session
    // 不該做得到。
    mfaActivateHint: "請輸入密碼與驗證器顯示的驗證碼來確認。",
    mfaErrorActivate: "密碼或驗證碼錯誤",
    // 這種 401 其實是 session 死了、不是憑證錯。這些表單刻意不在 401 時彈回
    // 登入牆(憑證錯必須留在原地當 inline error),所以過期要明講。
    mfaErrorSession: "登入階段已過期，請重新登入",
    mfaActivate: "確認並開啟",
    mfaActivating: "確認中…",
    mfaActivated: "兩步驟驗證已開啟",
    mfaDisable: "關閉兩步驟驗證",
    mfaDisableHint:
      "需要密碼和目前的驗證碼。如果驗證器已經遺失，請改在這台機器上執行 `ocserverd mfa-disable`。",
    mfaDisabling: "關閉中…",
    mfaDisabled: "兩步驟驗證已關閉",
    mfaErrorDisable: "密碼或驗證碼錯誤",
    // 與「驗證碼錯」是不同的失敗:這裡是根本讀不到目前狀態,所以不知道該給
    // 什麼選項。用「驗證碼錯誤」會指涉一組 owner 根本沒送出的驗證碼。
    mfaErrorLoad: "讀不到兩步驟驗證的狀態",
    mfaRetry: "重試",
  },
  chat: {
    offlineTitleSuffix: "目前離線",
    offlineHint: "這位成員目前不在線上，喚醒後才能開始對話。",
    // T-94c1: offline/stopped can now be messaged (queues until wake).
    offlineQueueHintLead: "你仍可在下方留言，",
    offlineQueueHintTail: "上線後就會讀到。",
    // T-94c1 wake row (offline/stopped composer): queue notice + in-place wake.
    wakeQueueHintSuffix: "目前離線中 — 訊息會排隊，或立即喚醒上線",
    wakeButton: "喚醒",
    wakePending: "喚醒中…",
    emptyRange: "這個範圍還沒有訊息",
    threadLoading: "正在載入對話…",
    inputPlaceholder: (name: string) => `回覆 ${name}…`,
    // M2-4 composer lock: shown IN PLACE OF the reply input while the member
    // is not online (offline / stopped / waking / stopping).
    composerOfflineSuffix: "目前離線中",
    me: "我",
    // 系統自動訊息的發話者標籤(T-ba04 轉派交接通知等,sender="system")
    systemSender: "系統",
    send: "送出",
    imageTooLarge: "圖片太大（上限 20 MB）",
    pastedImageAlt: "貼上的截圖",
    imageAlt: "聊天圖片",
    viewImageLabel: "檢視原圖",
    attachLabel: "附加檔案",
    attachTooLarge: (maxMb: number) => `檔案太大（上限 ${maxMb} MB）`,
    // 件數上限防呆:一則訊息最多帶 N 個附件(超過的不入列)。
    attachTooMany: (max: number) => `一則訊息最多附 ${max} 個檔案`,
    removeAttachmentLabel: "移除附件",
    downloadAttachment: "下載",
    read: "已讀",
    // T-48:最新一則不在視窗內時右下角浮出的圓形箭頭;以及取代它的新訊息
    // 預覽列上的關閉鈕。(舊的「有新訊息」藥丸連同它的固定句子一起退場——
    // 預覽列直接寫出寄件者與那一行內容。)
    jumpToLatest: "回到最新訊息",
    newMsgPreviewDismiss: "關閉新訊息預覽",
    // M2 批次 19 未讀跳轉:帶未讀進房時,第一則未讀訊息上方的細分隔線。
    unreadBelow: "以下為尚未閱讀的訊息",
    // T-bf82 往上捲載入更多:歷史撈完(hasMore=false)時,訊息串頂端的
    // 「已到最早訊息」標記。
    historyStart: "已到最早訊息",
    // 🔴 T-b0bb:重抓回來的最新一頁接不上本地已有的最新一則,而往回補頁
    // 也沒能接上(超過上限或補頁請求失敗)⇒ 這條對話的中間少了幾則,
    // 少幾則、少哪幾則都不知道。這一句存在的唯一理由是:server 已經把那
    // 幾則標成已讀了,所以未讀數不會透露、畫面也不會有任何異狀 ——
    // 不說出來就等於沒發生。
    gapSuspected: "這條對話可能缺了一段訊息(沒能補齊)",
    // 🔴 T-48:跳到原訊息(或別人留著的連結)指向的那一則,server 說沒有這
    // 一則(開窗請求 404,不是空白頁)。畫面會退回底部 —— 光是這樣就正好是
    // 這張票剛拿掉的那個安靜的謊:跟跳成功長得一模一樣。所以要在畫面上說
    // 出來。
    jumpTargetMissing: "找不到那則訊息,可能已經被清掉了",
    jumpTargetInterrupted: "定位被較新的訊息打斷了,那則訊息還在",
    jumpTargetUnreachable: "現在讀不到那則訊息,連線好像卡了一下",
    jumpTargetRetry: "再試一次",
    jumpTargetMissingDismiss: "關閉這則提示",
    // 訊息流的 LINE 式日期分隔線(跨日處置中 pill;捲動時 sticky 浮在頂端)。
    // weekday 0=週日 … 6=週六;非今年才帶年份(LINE 慣例)。
    dateToday: "今天",
    dateYesterday: "昨天",
    dateOn: (month: number, day: number, weekday: number) =>
      `${month}月${day}日 (週${"日一二三四五六"[weekday]})`,
    dateOnYear: (year: number, month: number, day: number, weekday: number) =>
      `${year}年${month}月${day}日 (週${"日一二三四五六"[weekday]})`,
    // 兩個成員(agent)彼此對話的段落預設收合，避免洗版;點擊展開/收合。
    // 英文有單複數(message / messages),中文用量詞「則」沒有 —— 靜態片段表達
    // 不了那個 s,所以拆成單數／複數兩條可覆寫字串,分支留在 compose.ts;中文
    // 兩條填一樣的字。
    interAgentExpandOne: "則成員間對話 · 展開",
    interAgentExpandMany: "則成員間對話 · 展開",
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
    // M2 批次 18:上傳者篩選(選項由實際附件的寄件者動態生成,與圖片/檔案分頁
    // 疊加生效)。T-51 ② 把它從 chip 列改成下面那個勾選下拉。
    gallerySenderFilterLabel: "依上傳者篩選",
    gallerySenderAll: "全部",
    // T-51 ②:chip 列改成 Jira 式的勾選下拉(owner 逐字:「或是像 jira 一樣,
    // 你可以打開時,展開一個下拉式選單做勾選就好」)。收起時只有一行;**刻意沒有
    // 搜尋框** —— 早期版本放過一個,owner 2026-09-02 直接說「不需要有搜尋這功能」,
    // 而這個控制項本來就是為了「我怎麼會知道有誰,沒辦法打字」而生的;名單按件數
    // 排序,值得找的人已經在上面。
    gallerySenderSelected: (n: number) => `已選 ${n} 位`,
    gallerySenderClear: "清除選取",
    // 有篩選在的空狀態。刻意跟 galleryEmptyImages／galleryEmptyFiles 分開:那兩句
    // 講的是這個圖庫,這一句講的是這個篩選;篩選在的時候說前者,等於告訴使用者他的
    // 檔案不見了。
    galleryEmptyFiltered: "選取的上傳者在這個分頁沒有檔案",
    galleryClose: "關閉檔案庫",
    galleryPreviewHint: "開新分頁預覽",
    galleryDownloadHint: "下載",
    // 檔案級分享連結(?sig= HMAC)— 複製到剪貼簿。不會過期,但不是永久:
    // 它跟著簽章金鑰環走,移除當初簽它的那把金鑰就會讓它失效(T-62)。
    copyShareLink: "複製分享連結",
    shareLinkCopied: "已複製連結",
    shareLinkCopyFailed: "複製連結失敗",
    // .md 附件的座艙內預覽(T-a1c4):與下載分開的動作;overlay 內用
    // Markdown.tsx render(不是開新分頁看原始碼)。
    // T-7bc2: the chip itself is the trigger now — no separate "action" label.
    mdPreview: {
      download: "下載",
      close: "關閉預覽",
      loading: "載入預覽中…",
      error: "無法載入預覽",
      // T-59 —— 一側指向文件的比較。沒有自帶標題時兩欄的預設標題，以及
      // 「這一側是活的」那個標記：同一條連結下個月點開會顯示不一樣的差異，
      // 讀者必須從畫面上看得出來，而不是自己推。
      diffSideCurrent: "目前存檔內容",
      diffSideSeed: "初始版本",
      diffSideRevision: (id: string) => `版本 #${id}`,
      diffSideLive: (label: string, at: string) => `${label}（讀取於 ${at}，之後會不一樣）`,
      // 比較畫不出來，因為**有一側已經不在了**：附件被回收，或版本已經被
      // 修剪掉。直說，因為另一條路——只畫倖存的那一側——會把它每一行都標成
      // 刪除，那不是「少了一半」，是一個很有自信的錯答案。
      diffSideGone: "這個比較有一側已經不在了，畫不出來。",
      // 單獨看某一側時顯示，帶讀者回到比較畫面而不是關掉整個視窗——他是從
      // 比較裡面點進來的。
      diffSideBack: "回到比較",
      unavailable: "此檔案無法預覽，請下載",
      // T-36 — 同樣是「這裡畫不出來」，但當上面那顆「在新頁面顯示」在的時候，
      // 就不該再叫他去下載：他這張票要的就是不用再複製去別的地方貼。
      unavailableOpenInNewTab: "此檔案無法在這裡預覽，請用上方的「在新頁面顯示」開啟。",
      // T-36 — 用一個獨立分頁打開這個附件（走分享連結），只出現在瀏覽器會直接
      // 顯示、而不是下載的檔案上。旁邊那句話刻意講白話：說的是使用者「會看到
      // 什麼」，不是背後的機制。
      openInNewTab: "在新頁面顯示",
      newTabStaticNote: "新頁面只會照原樣顯示，上面的按鈕和輸入格不會有反應。",
      // T-51 ① — the two paging chevrons. They are the ONLY control for a
      // zoomed image or a text file, where the arrow keys stay with the pan and
      // the scroll, so the accessible name has to stand on its own.
      previous: "上一個",
      next: "下一個",
      zoomControls: "縮放圖片",
      zoomIn: "放大",
      zoomOut: "縮小",
      pan: "拖曳圖片移動，或用方向鍵捲動",
    },
    // 對方訊息角落的「放大閱讀」小按鈕：把這則訊息本文丟進同一個
    // 全幅 overlay 讀（長回覆在對話欄裡很難讀）。自己發的訊息沒有。
    expandMessage: "放大閱讀",
    // T-4e95「回覆這則」：每則訊息角落的回覆入口、輸入框上方的「正在回覆」
    // 橫幅與它的 x，以及訊息上方那條指回原訊息的引用列。
    // replyQuoteGone 是**訊息列**的落空文案，而且是固定的：被引用的那則訊息
    // 已經不在了（被清掉、或發話的成員已經移除）。它不重試、不補撈、不會在
    // 下一個事件來時自己變成別的樣子——因為 2026-08-21 起，引用內容是伺服器
    // 每次讀取都現組後隨訊息一起送來的，前端沒有「還沒撈到」這個狀態。
    replyAction: "回覆這則",
    replyingTo: (name: string) => `正在回覆 ${name}`,
    replyCancel: "取消回覆",
    // 🔴 文案跟著行為改（owner 2026-08-21 裁定）。它以前叫「跳到原訊息」，因為
    // 那時它真的會把對話捲到那一列。現在它不捲任何東西：它把那一則重新撈回來，
    // 丟進放大閱讀的同一個覆蓋層。按鈕寫「跳到」卻跳出一個對話框，就是在每一條
    // 回覆列上說一句小謊，所以字跟著機制一起換。
    replyQuoteJump: "看原訊息",
    replyQuoteGone: "這則訊息已不存在",
    // replyQuoteJump 背後那次讀取失敗了。它**不是**在說原訊息在不在——那是
    // replyQuoteGone 的工作，而且那句話長在引用列本身上。這句只說「這次沒拿
    // 到」，而且只說一次，就說在剛剛被按的那顆按鈕旁邊。
    replyQuoteOpenFailed: "拿不到這則訊息",
    // 🔴 橫幅的落空文案跟訊息列的**不是同一句**，而且不可以互換。
    // 訊息列問的是「伺服器這次讀取有沒有把被引訊息組出來」——組不出來就是真的
    // 沒了，所以那裡有資格斷定「這則訊息已不存在」。
    // 橫幅問的是完全另一件事：它只查得到**已載入視窗**裡的訊息（messageById），
    // 而 owner 往上捲載入 scrollback、瞄準一則舊訊息、切走再切回來只載最新一頁
    // 之後，那則訊息還在、照送也會成功（sent.reply_to 掛得對、讀回來的
    // reply_to_chat 內容完整），橫幅卻查不到它。拿訊息列那句斷言來畫這個狀態，
    // 就是對 owner 說一句可以被他自己證偽的假話。
    // 所以橫幅講的是**與狀態無關的實話**：正在回覆的是較早的一則訊息。
    replyingToEarlier: "正在回覆較早的一則訊息",
    // 引用列自己的無障礙名稱。本 repo 沒有 sr-only／visually-hidden utility
    // （見 MemberCard.presence-a11y.test.tsx），所以「這句是引用的，不是這個人
    // 現在說的」這件事只能靠 aria-label 帶。少了它，一則回覆在無障礙樹上會被
    // 攤平成「Mira。Mira。他說的。看原訊息。我說的」——這功能唯一要傳達的
    // 資訊，螢幕閱讀器使用者剛好聽不到。replyQuoteRole 是原訊息已不存在、
    // 沒有作者可標時用的，replyQuoteRoleWho 是標得出作者時用的。
    replyQuoteRole: "引用",
    replyQuoteRoleWho: (name: string) => `引用 ${name}`,
  },
  mp: {
    back: "返回",
    avatarUpload: "更換頭像",
    avatarRemove: "移除頭像",
    avatarBusy: "處理中…",
    avatarTypeError: "只支援 PNG、JPEG 或 WEBP",
    avatarTooLarge: "圖片不可超過 64 KiB",
    avatarSaveError: "頭像儲存失敗，請稍後重試",
    rename: "改名",
    renamePlaceholder: "輸入名字",
    wake: "喚醒",
    change: "更改",
    settingsSaveOnly: "只儲存，不喚醒",
    modelReportedTag: "最近一次開機回報",
    settingsIntentNote: "這裡設定的是「下次喚醒要用哪一個」。",
    settingsIntentNoteReported: "上面資訊卡的模型是 agent 最近一次開機時回報的，跟這裡的設定可能不同。",
    wakeManual: "手動喚醒",
    // 點喚醒後、server presence 尚未跟上前的即時回饋
    wakePendingNote: "喚醒中…",
    forceStopConfirmTitle: "強制停止?",
    forceStopConfirmBodyLead: "立即強制停止",
    forceStopConfirmBodyTail:
      "——現在就砍掉 session、跳過正常收尾。進行中的未存工作會遺失。",
    forceStopConfirmAction: "強制停止",
    forceStopBusy: "停止中…",
    model: "模型",
    agentRuntime: "AI 執行環境",
    effort: "EFFORT · 思考強度",
    effortOf: { low: "低", medium: "中", high: "高", max: "最高" } as Record<Effort, string>,
    // model/effort 可設定（M2-2）— launch intents, 變更於下次喚醒生效
    // （編輯鈕本身用全站共用的 settings.edit 樣式/文案）
    modelEffortSave: "儲存",
    modelEffortCancel: "取消",
    modelPlaceholder: "自訂模型字串（留空用預設）",
    modelMachineDefault: "使用此機器的 Codex 預設模型",
    claudeAccount: "Claude Account",
    codexAccount: "Codex Account",
    // T-b6d9: 這行原本寫「變更於下次喚醒／換手生效」，在正職成員的 model/runtime/
    // effort 改成「儲存即自動換手、收尾後以新值重生」之後就是假話了（線上成員不必
    // 等下一次喚醒，也不必有人另外按換手）。key 名字保留歷史拼法（theme wording
    // overlay 以 key 為契約），文案才是給人看的那一份。
    modelEffortNextWakeNote: "線上會自動換手後套用；離線則下次喚醒生效",
    modelEffortError: "儲存失敗，請稍後重試",
    runtime: "運行狀況",
    machine: "機器",
    machineMovingToLabel: "→ 要換到",
    // The same 「→ …」 shape the machine cell has always used, widened to the
    // other three configurable cells (T-7f28). 「換到」 reads as a place;
    // 「換成」 reads as a value — one word each, so the two never blur.
    pendingChangeLabel: "→ 要換成",
    // The wind-down line. It replaces 「上次重新聚焦」 while a window is open:
    // that phrasing reads as history, and the owner needs to know the change
    // is being APPLIED right now.
    windDownForChangeLabel: "正在收尾以套用你的改動",
    windDownDeadlineLabel: "正在收尾，已給死線",
    windDownByLabel: "最晚",
    windDownEffectSuffix: "生效",
    standby: "待命中",
    context: "context",
    compactionCount: (n: number) => `壓縮：${n}`,
    refocus: "重新聚焦",
    refocusOfflineHint: "僅線上可重新聚焦",
    refocusing: "聚焦中…",
    refocusDone: "已送出",
    refocusError: "聚焦失敗",
    // persistent note after a refocus is submitted — the compaction happens on
    // the agent side asynchronously, so "已送出" (not "已完成") is the honest state.
    refocusSubmittedNote: "已送出重新聚焦 · agent 壓縮中…",
    refocusSinceLabel: "上次重新聚焦",
    // fleet remote-ops stage 1 — 最近操作 (last warden op receipt)
    lastOp: "最近操作",
    lastOpStart: "喚醒",
    lastOpStop: "停止",
    lastOpOk: "成功",
    lastOpFail: "失敗",
    lastOpLogLabel: "查看記錄",
    estimatedCost: "估計$",
    costReset: "歸零",
    costResetHint: "把這個成員的累計估計花費歸零。按下去救不回來。",
    costResetConfirm: "確定歸零",
    costResetError: "歸零失敗，數字沒有被清掉。",
    costResetConfirmBodyLead: "這會把目前累計的 ",
    costResetConfirmBodyTail:
      " 歸零，從 0 重新開始累積。這個數字沒有留在任何其他地方，清掉就回不來了。",
    terminal: "終端 · TMUX",
    copyCommand: "複製指令",
    copied: "已複製",
    terminalHint: "在你自己的終端機貼上執行，即可接上這個成員的 session。",
    initialPrompt: "初始 PROMPT",
    promptLoading: "載入中…",
    promptError: "讀取初始 PROMPT 失敗",
    promptRetry: "重試",
    lessons: "過往學習經驗",
    expandableHint: "下次喚醒／聚焦生效",
    lessonsLoading: "載入中…",
    lessonsError: "讀取學習經驗失敗",
    lessonsEmpty: "尚無學習經驗。",
    lessonsShared: "此角色的學習經驗(同一角色的成員共用)。",
    lessonsSaveError: "儲存學習經驗失敗",
    // ── 判準 Insight（T-3809）——角色誌的第三塊。刻意不寫成學習經驗的變體:
    // 本票存在的理由就是「這個角色怎麼權衡」與「上次發生了什麼」不是同一份文件。──
    insight: "判準(Insight)",
    insightLoading: "載入中…",
    insightError: "讀取判準失敗",
    insightEmpty:
      "這個角色還沒有 Insight。還沒有人把判準搬進來——這一塊上線時所有角色都是空的。",
    insightShared:
      "Insight 目前不是私有的,只是分開的——任何已認證身分都讀得到;只有這個角色自己的 agent 與 admin 寫得動。",
    insightSaveError: "儲存判準失敗",
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
    // ── 定期訊息（T-f059，webhook 的孿生兄弟:觸發者換成時鐘）──
    schedmsg: {
      title: "定期訊息 · SCHEDULE",
      enabled: "啟用中",
      disabled: "已停用",
      add: "新增定期訊息",
      empty: "尚未設定定期訊息",
      loadError: "讀取定期訊息失敗",
      createError: "建立定期訊息失敗(請檢查內容、時刻與時區)",
      updateError: "儲存變更失敗(請檢查內容、時刻與時區),這條排程沒有被改動。",
      unlabeled: "(未命名)",
      create: "建立",
      save: "儲存",
      cancel: "取消",
      editLabel: "編輯",
      deleteLabel: "刪除",
      deleteConfirm: "確定刪除這條定期訊息?刪掉之後不會再送出,也無法復原。",
      labelLabel: "名稱",
      labelPlaceholder: "給人看的名字,例如「每日巡檢」(選填)",
      bodyLabel: "訊息內容",
      bodyPlaceholder: "到時間要送出去的訊息,會原封不動送給這位成員",
      // 列上只顯示前幾行,長內容用這兩個字切換。兩邊講的都是「這一列現在顯示多少」,
      // 存下來的訊息不受影響。
      bodyExpand: "顯示完整內容",
      bodyCollapse: "收合",
      cadenceLabel: "頻率",
      cadenceDaily: "每天",
      cadenceWeekly: "每週",
      cadenceMonthly: "每月",
      cadenceCustom: "自訂",
      // ── 自訂頻率(T-49e7)。四組是「幾月 × 幾號 × 幾點 × 幾分」的交集,每一組
      // 都要逐項列出來:空集合會被伺服器擋下(422),因為「每次都送」與「永遠
      // 不送」不可以只差一個鍵。
      // 🔴 四個標籤是 owner 於卡上逐字選定的(第二輪):幾月 / 幾號 / 幾點 / 幾分
      // ——「月份／日期／小時／分鐘」那組是被否掉的建議,不要「順手」改回去。 ──
      customMonthsLabel: "幾月",
      customDaysLabel: "幾號",
      customHoursLabel: "幾點",
      customMinutesLabel: "幾分",
      customSelectAll: "全選",
      customClear: "清除",
      customEmptyHint:
        "四組都要至少選一項,否則這條排程沒有任何送出時間,伺服器也會拒絕。",
      customNone: "尚未選擇",
      // 摘要片語:四組各自成句(標題就在它上面),列摘要再把四句用「 · 」接起來。
      customEveryMonth: "每個月",
      customMonthsLead: "每年 ",
      customMonthsTail: " 月",
      customDaysLead: "每月 ",
      customDaysTail: " 號",
      customEveryHour: "每小時",
      customHoursLead: "第 ",
      customHoursTail: " 點",
      customEveryMinute: "每分鐘",
      customMinutesLead: "第 ",
      customMinutesTail: " 分",
      // 等間隔的分鐘收合成這一句(只勾 0、20、40 ⇒「每 20 分鐘」)。
      customStepLead: "每 ",
      customStepTail: " 分鐘",
      // 零散選擇只列前幾個,其餘用這一句帶過(「第 0、7、13、22 分等,另 2 個」)。
      // N 是「沒列出來的那幾個」的數量(owner 裁定)。⚠️ 不要縮回「等 N 個」:
      // 中文「等 N 個」的慣例是**總數**(「北京、上海等 3 個城市」共 3 個),
      // 所以那個寫法會把「列了 4 個、還有 2 個」讀成一句自相矛盾的話,而英文的
      // 「and 2 more」毫無歧義——同一個 N,兩種語言兩種意思。「另」把它釘成餘數。
      customMoreLead: "等,另 ",
      customMoreTail: " 個",
      // 列摘要用的組合詞:語序在各語言不同,所以用內插而不是把碎片在元件裡黏起來。
      weeklyOn: (weekday: string) => `每${weekday}`,
      monthlyOn: (day: number) => `每月 ${day} 號`,
      dayOfWeekLabel: "星期幾",
      weekdaySun: "週日",
      weekdayMon: "週一",
      weekdayTue: "週二",
      weekdayWed: "週三",
      weekdayThu: "週四",
      weekdayFri: "週五",
      weekdaySat: "週六",
      dayOfMonthLabel: "幾號",
      // 🔴 owner 2026-08-10 卡 rc-aeef15360ab5 裁定開放 1-31,代價是沒有那一天的
      // 月份整個跳過。這行提示讓那個代價在「設定當下」就看得見,而不是讓人從
      // 「怎麼二月都沒動靜」去反推。
      dayOfMonthSkipHint:
        "沒有這一天的月份會整個跳過,不會改送月底。選 31 號的話,二月永遠不會送。",
      hourLabel: "時",
      minuteLabel: "分",
      timezoneLabel: "時區",
      timezonePlaceholder: "IANA 時區名,例如 Asia/Taipei",
      lastFiredLabel: "上次送出",
      lastFiredNever: "尚未送出",
    },
    // ── RESUME SUMMARY（T-8b0d，同 resume_summary 的喚醒快照，供 owner 查看）──
    resumeSummary: {
      title: "RESUME SUMMARY",
      loading: "載入中…",
      error: "讀取喚醒快照失敗",
      retry: "重試",
      chatCount: "近期訊息",
      // 🔴 這一格量的是「聊天那整塊要花多少 context」,不是內文長度。
      // 這裡刻意不逐項列舉它加了哪些東西——server 端 resumeSnapshotParts 是
      // 唯一說得準的地方,而過去每一份自己列一遍的散文都列漏了(切點 hint
      // 那一塊固定幾百字的文字,三份全漏)。標籤寫成「訊息字數」不算說錯,但看畫面
      // 的人(owner)正拿這些數字調預算,會把它讀成內文長度而低估真實成本。
      chatChars: "聊天區塊字數",
      tasksReturned: "回傳任務",
      tasksOpenTotal: "進行中任務總數",
      tasksDetailChars: "任務細節字數",
      cardsWaiting: "等待回覆卡",
      cardsAnsweredRecent: "近期已回覆卡",
      stepsOnAnsweredCard: "已回答卡上的步驟",
      answeredCardStepChars: "已回答卡步驟字數",
      chatSection: "近期聊天",
      chatEmpty: "尚無聊天訊息",
      tasksSection: "進行中任務",
      tasksEmpty: "尚無進行中任務",
      // 這是 server 給的指標，不是完成標記；owner 的回覆可能要求改做。
      answeredCardSteps: "卡在已回答卡上的步驟（請先讀卡，不代表已完成）",
      // T-91 — 開機快照任務列新增的反向相依標籤。轉派中那一格刻意不另開 key：
      // 座艙已經有 tasks.lockReassigning，這裡直接沿用，不要造一個會跟它走岔的同義詞。
      blockingLabel: "正在等這張的票：",
      // 抬頭:這份快照是什麼時候拍的。它是把底下每一個 ts_display 讀成
      // 「多久以前」的唯一錨點——讀的人(不論是 agent 還是 owner)沒有
      // 一個可信的時鐘可以拿來對。
      generatedAtLabel: "本份產生於",
      // 🔴 折疊 ≠ 截斷,兩者的文案不共用任何一個詞。
      // 這一組講的是「這則在這裡,只是被折起來了,東西還在」。
      //
      // 🔴 每則後面那個只是**記號**,不是句子。它本來是
      // 「此則已折起 46 字元(內容仍在,用 get_chat 重讀這則)」,而且**每一則
      // 都重複一次**——幾百則乘起來,那句樣板比它省下來的還多,owner 2026-08-13
      // 當場指出。慣例改成在聊天區塊抬頭只講一次(`bodyOmittedNote`),每則就
      // 只剩數字。
      bodyOmittedMark: "折起",
      // 只在聊天區塊講一次,任何一則都不必再重複。
      bodyOmittedNote: "折起 = 此則在此,僅縮短,全文仍存 server(用 get_chat 重讀全文)",
      // 這一組講的是「更早的往來可能根本沒有被帶進來」——要去撈才知道。
      // 🔴 措辭是「可能」而不是「一定」:server 端只要那條線被截在切點上就
      // 亮這個標記,而它不會再往切點外看一眼,所以就算其實沒有更舊的往來,
      // 標記一樣會亮(見 api_chat.go 的 resumeChatCutHint)。
      // 🔴 這是區塊的「名字」,不是它的內容。原本這裡複述了 hint 的第一句,
      // 兩句上下相連、意思一樣,讀者被同一件事講兩遍。不能改的是 hint 那一句:
      // agent 拿到的只有那串文字、讀不到這個 label,所以 hint 必須自足;
      // 而這個 label 是純畫面的用詞,改它不影響 agent 那一側。
      chatCutLabel: "這條線往前被切斷了:",
      // 就地顯示的回覆卡:owner 選了哪個、打了什麼、什麼時候回的。
      cardOptionsLabel: "提供的選項",
      cardAiPickTag: "AI 建議",
      cardPickedTag: "已選",
      cardAnswerTextLabel: "補充文字",
      cardAnsweredAtLabel: "回覆於",
      cardUnanswered: "尚未回覆",
      cardAttachmentsLabel: "附件",
      replyCardStatusLabel: "回覆卡狀態",
      // studio floor 兩段:名冊與機器。
      rosterSection: "名冊",
      rosterEmpty: "這份快照沒有名冊區段",
      rosterDutyLabel: "職責",
      rosterCurrentTaskLabel: "手上的任務",
      machinesSection: "機器",
      machinesEmpty: "這份快照沒有機器區段",
      machinesYouAreOnLabel: "這位站在",
      machinesYouAreOnNone: "尚無機器綁定",
      machineOnline: "線上",
      machineOffline: "離線",
      rosterChars: "名冊字數",
      machinesChars: "機器字數",
      // 長區段可收合,但預設看得到有哪些區段。
      collapse: "收合",
      expand: "展開",
    },
    dash: "—",
  },
  // ── 機器選擇 / 搬移 agent（喚醒・重生・搬移時挑選線上機器）──
  machine: {
    // 無線上機器時,生成／喚醒按鈕停用的提示
    noOnlineMachine: "沒有線上的機器",
    // 改機器按下之後的即時回饋:搬移是非同步的(API 200 只代表指定寫下了,真的
    // 移到新機器是之後才由 SSE 帶回來),所以按鈕會一直停在「更換中…」直到落地
    // 或逾時——不然按鈕與狀態都不變,會讓人以為沒按到。
    relocating: "更換中…",
    // 🔴 逾時文案只講我們觀測到的事實,不指因果:等了 30 秒沒收到落地回報,不等於
    // 「沒搬成功」(可能搬了但我們看不到)。斷言原因會把人推向錯的方向——同 T-7fa1
    // BLOCKER-1 的教訓。
    relocateTimeout: "還沒收到完成回報,可以再按一次重試。",
    relocateFailed: "這次沒有送出更換,可以再按一次重試。",
    // 同機更換(沒有可觀測的落地可等)按下後的一次性回饋:只講「已送出」這個已經
    // 發生的事實,不承諾換過去了、不起 30 秒計時。
    relocateSent: "已送出更換",
    picker: {
      label: "選擇機器",
      // 目前綁定的機器離線時,於清單中停用並標註
      offlineOptionSuffix: "（離線）",
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
    // 帳號歸零 (T-53, owner ruling rc-5c5d7c7c6dcd) — the ACCOUNT's own figure,
    // cleared without touching any member's.
    costReset: "歸零",
    costResetHint: "把這個帳號的累計花費歸零。不會動到底下任何成員的數字。按下去救不回來。",
    costResetConfirm: "確定歸零",
    costResetError: "歸零失敗，數字沒有被清掉。",
    costResetConfirmBodyLead: "這會把這個帳號累計的 ",
    costResetConfirmBodyTail:
      " 歸零，從 0 重新開始累積。底下成員各自的數字不會被動到。這個數字沒有留在任何其他地方，清掉就回不來了。",
    estimate: "估計",
    fiveHour: "5 小時窗",
    sevenDay: "7 天窗",
    usage: "用量",
    time: "時間",
    overheated: "過熱",
    // T-3b90:用量百分比是「上次回報時的快照」,時間百分比卻是即時算的。
    // 沒有這一句,兩者看起來像同時量的,一個三天前的數字會被當成現在。
    // 拆成 lead/tail 兩片,兩語都是「前後各一個空格」的 LABEL 形狀
    // (compose.ts 組裝)。分片是為了讓這兩個字能被主題包的 wording 覆寫——
    // 寫成字串模板的話,白名單收不到它,那兩個字就永遠換不掉(T-081b)。
    measuredAgoLead: "量於",
    measuredAgoTail: "前",
    // 帳號詳情 modal(T-a9a7):該 claude 帳號背後的真實識別。email/org 來自
    // owner-only 的 account_label;任何缺值一律誠實顯示 "—",絕不猜。
    detail: {
      open: "帳號詳情",
      title: "帳號詳情",
      close: "關閉",
      accountKey: "歸戶 key",
      accountIdentifier: "帳號識別碼",
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
      codex: "Codex",
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
      reinstall: "重新安裝",
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
      // 伺服器有回錯誤細節時,compose.ts 的 bootstrapErrorDetail 直接把細節接在
      // 上面那句 bootstrapError 後面 —— 同一句話不再有第二個 key。
      bootstrapFailedLead: "安裝失敗(結束碼 ",
      bootstrapFailedTail: "),原因如下:",
      // T-ba62:成功也保留安裝記錄。原本成功分支把整份 log 丟掉,於是
      // 「裝好了」與「裝好了但裡面有警告」長得一模一樣。
      bootstrapSucceeded: "安裝完成,記錄如下:",
      // 覆蓋現役 warden 的確認(伺服器自己那台、且「在線上」時才跳)。
      // 這段話刻意不與遠端機器的安裝共用:那邊只是顯示一段可複製的指令,
      // 這邊是覆蓋掉一個正在服役的 warden。
      bootstrapConfirmTitle: "確認在伺服器上重新安裝",
      bootstrapConfirmBodyLead: "「",
      bootstrapConfirmBodyTail:
        "」目前在線上,已經有一個正在服役的 warden。再安裝一次會直接覆蓋它:這台機器上的成員會全部斷線,而且此動作不可逆 —— 被覆蓋掉的 warden 無法還原,只能重新安裝並讓成員重新上線。",
      bootstrapConfirm: "覆蓋並重新安裝",
      // uninstall (POST /uninstall):驅動 uninstall RPC 給 warden(僅線上可用)
      uninstallConfirmTitle: "確認解除安裝",
      uninstallConfirmBodyLead: "確定要解除安裝「",
      uninstallConfirmBodyTail:
        "」嗎？這會請該機器上的 warden 執行 ocwarden uninstall;成功後機器會變為離線,但記錄會保留(可再次安裝)。",
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
      // 兩個參數、數字卡在句中(英文 member(s) 偽複數、中文量詞「位」),所以
      // 每語言拆三段,鍵名帶 1/2/3 標明串接順序:1 + 機器名 + 2 + 人數 + 3。
      uninstallWarnBody1: "「",
      uninstallWarnBody2: "」上還有 ",
      uninstallWarnBody3:
        " 位成員在線上。現在解除安裝會在成員仍在這台機器上時把 warden 拆除 —— 建議先將相關成員下線。仍要繼續嗎?",
      uninstallWarnProceed: "確認繼續",
      // delete (DELETE /machines/{id}):不送任何 warden 指令,但 T-9cf8 之後
      // 它已經不是舊文案講的「只是動一筆記錄」:名冊是憑證的權威,把機器移出
      // 名冊,它的 token 下一個請求就失效,還掛在這台機器上的 agent 一併失效。
      // 舊文案(「這只會從清單移除該機器的記錄」)現在等於用一段不正確的描述
      // 取得同意 —— 這個 repo 已經記過一次同樣的缺陷(安裝的閘承諾不會中斷
      // 服務,實際會)。用錯誤描述換來的同意不算同意,所以文案要講出真正的代價。
      deleteConfirmTitle: "確認刪除機器",
      deleteConfirmBodyLead: "確定要刪除「",
      deleteConfirmBodyTail:
        "」嗎?該機器的憑證會立刻失效:機器無法再回報,還指派在這台機器上的 agent 也會一起失去存取權。機器上的 warden 不會被拆除(那是「解除安裝」),而且這個動作無法復原 —— 要恢復只能重新安裝。",
      deleteConfirm: "確認刪除",
      deleteBusy: "刪除中…",
      deleteError: "刪除失敗",
      // runtime 能力(T-90be ⑤ + T-b36a):一定連時效一起顯示。過期的值 server
      // 刻意留在 wire 上(那是 worker 卡在 machine_unavailable 唯一的解釋),
      // 所以畫面的責任是「照顯示、但不冒充現況」。
      runtimeStale: "過期",
      runtimeStaleHint: "距離上次探測已久,該機之後沒有再回報,這個能力狀態可能已經不成立",
      runtimeUnknown: "從未探測(舊版 warden,或還沒有心跳)",
      // 各 runtime 自己的版本欄(T-674d)。原本 Runtime 欄的 ✓/✗ 摘要拿掉了,
      // Claude 與 Codex 各自印出探測到的版本。但原本 ✗ 講的事情還是要講得出來
      // ——那是 placement 拒絕這台機器的原因——所以「未安裝」「未登入」是格子裡
      // 的字,不是一個默默消失的版本號。
      runtimeNotInstalled: "未安裝",
      runtimeNotInstalledHint: "warden 在這台機器上找不到這個 runtime 的執行檔,無法在此喚醒",
      runtimeNoVersion: "已安裝",
      runtimeNoVersionHint: "執行檔存在,但版本探測沒有回傳結果",
      runtimeLoggedOut: "未登入",
      runtimeLoggedOutHint: "已安裝,但登入探測回報未登入,placement 不會把這個 runtime 派到這台機器",
      // 硬體樣本時效(T-b36a):過期的數值 server 會收回,於是 CPU/RAM/電源
      // 落回 dash——跟「從來沒回報過硬體」是同一個 dash。這兩個標籤就是把兩
      // 個世界分開的東西,要行動的只有後者(這台失聯了,不是它從沒說過話)。
      hardwareStale: "過期",
      hardwareStaleHint: "距離上次量測已久且之後沒有再回報,數值已收回,不以現況呈現",
      // 型別錯的硬體值(T-aad2):這格空白的第三種理由,也是原本跟「從來沒
      // 量到」長得一模一樣的那一種——探測其實有回報,只是值的型別 server 讀
      // 不懂(數字欄位送了字串)。刻意跟「過期」分開:過期是沒人去量,這個是
      // 回報端本身壞了,要查的東西不同。
      hardwareBad: "值異常",
      hardwareBadHint: "這台回報了型別不對的值,無法呈現——探測有跑,但讀數不可用。請檢查該機 warden 版本。",
      // 切換狀態。四種狀態裡只有一種會說話——已證實的失敗;其餘三種(已量到確認
      // 生效 / 量了但判斷不出來 / 從來沒量過)一律完全不顯示。
      //
      // 🔴 owner 2026-08-04 於 rc-aaa0e7967f8a 選①,把原本的三句長話全部拿掉,
      // 只留這一個短標記。他的原話:「這三句都太長了,而且看到的人能做什麼嗎
      // 他們看得懂發生什麼事情嗎?」三個抱怨都成立:
      //   1. 太長 —— 每一句都是一整行敘述,佔滿機器那一列。
      //   2. 不能行動 —— 舊註解自己寫著「沒有人能據以行動的警告,不是警告」,
      //      卻在三行之後寫「這三句都不叫任何人去重啟什麼」。**它自己違反自己**,
      //      而三句話沒有一句告訴讀者要做什麼。
      //   3. 看不懂 —— 舊文案已避開 anchor / legacy 這類術語,但「改變了執行
      //      agent 的方式」本身仍是內部概念,看的人不知道那是什麼、也不知道嚴不嚴重。
      //
      // ⇒ **短標記不假裝在解釋,它只說「這裡不對勁」。** 說不清楚哪裡不對勁是
      // 刻意的取捨:看到的人要來問,而那比一句「每個字都看得懂、卻不知道要幹嘛」
      // 的長句好。
      //
      // ⚠️ 原本那三句是為了修一個真實事故加的:在它們之前,三種狀態共用一片空白,
      // 於是一台其實沒生效的機器看起來很健康三個小時。**那個事故仍然被擋住**——
      // 已證實的失敗現在有一個面孔,只是那個面孔很短。真正退回沉默的只有兩種
      // 「沒有答案」的狀態,而它們本來就沒有東西可說(讀完不能做任何事)。
      cutoverNotInEffect: "未生效",
    },
  },
  // ── 備份健康(T-da06)——排程備份還有沒有在產生還原點 ──
  // 兩個面共用這一組字:topbar 常駐指示燈與監控頁的備份卡。**主要句子一律由
  // `code` 推導**(下面的 reason*),伺服器的 `detail` 只當次要診斷字串顯示——
  // 它是英文、給工程師看的,不是使用者面的那句話。
  // 簽章金鑰輪替 (T-62)
  signingKeys: {
    title: "簽章金鑰",
    intro:
      "伺服器用簽章金鑰簽發登入憑證。可以同時存在多把：只有一把在簽，其餘的仍然驗得過 —— 這是換金鑰的過渡期。",
    loading: "讀取中…",
    signingBadge: "正在簽",
    retiredBadge: "只驗不簽",
    createdLabel: "產生於",
    createdUnknown: "此站啟用以來（時間未記錄）",
    countLabel: (n: number) => `目前有 ${n} 把金鑰`,
    rotateButton: "產生新金鑰",
    rotateHint:
      "產生一把新的並讓它接手簽章。既有的登入不會被踢掉：舊金鑰留著繼續驗，只是不再簽新的。立刻生效，不必重啟。",
    removeButton: "移除",
    // 🔴 這兩句是這張卡最重要的文字。移除沒有復原，而它的射程比人直覺的大。
    removeConfirmTitle: "移除這把金鑰？",
    removeConfirmBody:
      "這把金鑰簽過的東西會當場全部失效，沒有緩衝期，也不會通知任何人：用它簽的登入憑證會被拒絕，用它產生的分享連結（檔案的、比較的）也會一起壞掉。",
    removeConfirmWarden:
      "⚠️ 機器（warden）的憑證沒有到期時間，不會自己過期。要判斷現在能不能移除，看的是「每一台機器都已經換到新金鑰了嗎」，不是「等了幾天」，也不是「都重新連過了」——重新連上、但手上還是舊金鑰簽的憑證，一樣會在你按下去的當下失聯。離線的機器要等它自己上線才換得掉。",
    removeConfirmCancel: "取消",
    removeConfirmOk: "確定移除",
    actionFailed: "這個動作沒有成功，伺服器沒有說明原因。",
    emptyState: "讀不到金鑰。",
  },
  backupHealth: {
    title: "備份健康",
    // 三個 status 的短標。unknown 不是比較安靜的 healthy:它是「判斷不出來」,
    // 而這整顆票的重點就是「沒有還原點」不可以長得像「有還原點」。
    statusHealthy: "備份正常",
    statusUnhealthy: "備份異常",
    statusUnknown: "無法判斷",
    // 為什麼是紅的 —— 由 code 推導的那一句(不是 server 的 detail)。
    reasonNeverRan: "排程備份從來沒有成功產生過還原點。",
    reasonStale: "最新的排程備份已經超過保鮮期，備份可能已經停掉了。",
    reasonFailed: "最近一次排程備份失敗或被略過，沒有產生新的還原點。",
    // unknown 的兩種來源分開講:伺服器說它還沒評估 vs 座艙根本問不到伺服器。
    reasonUnknown: "監看器還沒評估過，或讀不到自己的狀態，所以現在無法判斷有沒有還原點。",
    reasonUnavailable: "讀不到備份狀態（問不到伺服器），所以現在無法判斷有沒有還原點。",
    // 事實列
    newestLabel: "最新排程備份",
    newestNever: "從來沒有",
    sinceLabel: "異常已持續",
    staleAfterLabel: "保鮮期",
    detailLabel: "伺服器診斷",
    // 時間後綴(接在 3d 4h 這種長度後面)
    ago: "前",
    loading: "讀取中…",
  },
  settings: {
    title: "設定",
    // landing entries
    software: "系統更新與備份",
    // 全域情境 (T-a241) — 事件程序文件那一區從「角色誌」抽出來，成為〈設定〉底下
    // 自己一塊，排在「系統更新與備份」與「角色誌」之間。角色誌只剩角色定義。
    globalContext: "全域情境",
    roles: "角色誌",
    params: "參數調整",
    // ── 主題管理 (T-16a1 P3b): moved here from the profile dropdown ──
    themeManage: "主題",
    themeColorsSection: "顏色",
    themeColorOpacity: "不透明度",
    themeColorFollows: "跟隨",
    themeColorPicker: "取色器",
    themeWordingSection: "用詞",
    themeWordingHint: "填入替代字即可覆蓋介面用詞;留空則維持原文。",
    themeWordingSearch: "搜尋用詞…",
    themeWordingOverride: "替代字",
    themeWordingTag: "用詞",
    // ── 字型 (T-16a1 P4): 從安全字型白名單挑內文／標題字型 ──
    themeFontsSection: "字型",
    themeFontsHint: "從內建的安全字型中挑選;維持預設則沿用主題原字型。",
    themeFontBody: "內文字型",
    themeFontTitle: "標題字型",
    themeFontDefault: "預設(主題字型)",
    // ── 頭像 (T-16a1 P5):依成員類型上傳頭像圖 ──
    themeAvatarsSection: "頭像",
    themeAvatarsHint:
      "可依成員類型各上傳一張頭像(PNG / JPEG / WEBP,上限 64 KB);留空則沿用內建頭像。",
    themeAvatarMember: "正職頭像",
    themeAvatarOutsource: "外包頭像",
    themeAvatarOwner: "CEO 頭像",
    themeAvatarAssistant: "助理頭像",
    themeAvatarChoose: "選擇圖片",
    themeAvatarClear: "清除",
    themeAvatarInvalid: "圖片無效——僅接受 64 KB 以內的 PNG / JPEG / WEBP 檔。",
    // ── 工作室 logo + 導覽圖示 (T-ea81) ──
    themeLogoSection: "工作室 Logo",
    themeLogoHint:
      "上傳頂欄 Logo(PNG / JPEG / WEBP,上限 64 KB);留空則沿用內建圖示。",
    themeLogo: "Logo",
    themeNavIconsSection: "導覽圖示",
    themeNavIconsHint:
      "可為每個導覽頁籤各上傳一張圖示(PNG / JPEG / WEBP,上限 64 KB);留空則沿用內建圖示。",
    themeNavOffice: "辦公室圖示",
    themeNavReplies: "請示圖示",
    themeNavTasks: "任務圖示",
    themeNavMonitor: "監控圖示",
    themeNavGuide: "使用說明圖示",
    // ── 外框背景圖 (T-081b) ──
    themeCanvasBgSection: "外框背景",
    themeCanvasBgHint:
      "上傳疊在底色之上的圖(PNG / JPEG / WEBP,上限 512 KB),留空則只有純底色。平鋪與貼邊只畫在內容欄兩側的外框,因此在手機、窄視窗與寬版版面(外框寬度為 0)都看不到;滿版則畫滿整個視窗。",
    // 背景圖有自己的上限(512 KB),所以不能沿用共用的 themeAvatarInvalid
    // ——那句寫著 64 KB,對背景圖是假的(T-72da)。
    themeCanvasBgInvalid: "圖片無效——僅接受 512 KB 以內的 PNG / JPEG / WEBP 檔。",
    themeCanvasBg: "外框底圖",
    themeCanvasBgMode: "鋪法",
    themeCanvasBgModeTile: "平鋪 — 重複鋪滿整個外框",
    themeCanvasBgModeSides: "貼邊 — 左右各貼一張",
    themeCanvasBgModeCover: "滿版 — 一張圖填滿整個視窗",
    themeCanvasBgModeHint:
      "貼邊適合本身站得住的圖(例如左右各一棵樹);不做鏡像,要左右對望請直接畫成左右對稱的圖。",
    themeCanvasBgModeCoverHint:
      "滿版只有在這個主題同時把頂列／頁籤列／內容區設成半透明色(#RRGGBBAA 或 rgba)時才看得到。那三層底下坐著文字,圖與文字的對比要由主題自己負責。",
    themeDeleteConfirmLead: "刪除主題「",
    themeDeleteConfirmTail: "」?此動作無法復原。",
    // ── 系統更新與備份 (honest build-identity card) ──
    currentVersion: "目前版本",
    upToDate: "已是最新版",
    // 檢查更新(GET /api/release/check,直接問 GitHub Releases)
    checkUpdate: "檢查更新",
    checkingUpdate: "檢查中…",
    checkUnknown: "連不上 GitHub、查不到最新版本——請稍後再試",
    checkFailed: "檢查更新失敗,請重試",
    viewRelease: "查看 release",
    updateSettings: "更新設定",
    // ── 系統更新與備份 toggle(receive_beta / auto_update,皆預設關閉)──
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
    // 十份文件依 owner 的讀法分三組（定稿 2026-08-24）：上線 → 下線 → 任務事件。
    // 「只顯示不給改」那一組不見了——分組是版面上的分群，不是「能不能改」的
    // 真相來源（那是 server 的答案）。上線這組依 boot context 的組裝順序排：
    // 系統互動 → 使用者自訂 → 啟動步驟（啟動步驟那一列開的是 claude / codex
    // 兩份文件的索引）。
    // UI 不露檔名。
    globalSection: "上線（BOOT）",
    systemName: "系統互動",
    systemSub: "系統運作說明，注入給每個 agent · 可編輯",
    customName: "使用者自訂",
    customSub: "追加到每個 agent 開機情境的自訂內容 · 可編輯",
    roleDefsSection: "角色定義",
    // 兩份**不同**的文件，分別開自己的頁：它們的第 3 步語意相反，
    // 所以清單上不併成一列、詳情頁也不並排。
    // The ONE list row. The runtime-specific names below still title the PAGE
    // and its history list — the row no longer names a runtime because you
    // pick one after you are inside.
    bootName: "啟動步驟",
    bootSub: "AI 開機時照著做的步驟 · 兩種執行環境各一份 · 可編輯",
    bootRuntimeClaude: "一般",
    bootRuntimeCodex: "Codex",
    bootClaudeName: "啟動步驟（Claude Code）",
    bootClaudeSub: "Claude Code 執行環境的開機 SOP · 可編輯",
    bootCodexName: "啟動步驟（Codex CLI）",
    bootCodexSub: "Codex App Server 執行環境的開機 SOP · 可編輯",
    // 〈停止〉（T-c9c0）——不進開機情境，是 server 要收掉這個 session 時
    // 夾帶給 agent 的收尾指示，所以在清單上自成一列，排在啟動步驟之後。
    offboardName: "停止",
    offboardSub: "server 要收掉這個 session 時夾帶給 agent 的收尾指示 · 可編輯",
    // ── T-3201：其餘六份生命週期文件 ──
    // 分組標題。上線那組沿用既有的 globalSection；下線、任務是另外兩組。
    stopSection: "下線（STOP）",
    taskEventSection: "任務事件（TASK）",
    acceleratedStopName: "加速停止",
    acceleratedStopSub: "被要求提前收工時給 agent 的指示 · 有截止時間 · 可編輯",
    taskCloseoutName: "任務結案",
    taskCloseoutSub: "任務被判定結束時給 agent 的收尾指示 · 可編輯",
    taskReassignPredecessorName: "任務轉派 · 給前任",
    taskReassignPredecessorSub: "手上的任務被轉給別人時給 agent 的交接指示 · 可編輯",
    taskTakeoverWithPredecessorName: "任務轉派 · 給接手人",
    taskTakeoverWithPredecessorSub: "接手別人做過的任務時給 agent 的指示 · 可編輯",
    taskTakeoverFreshName: "新任務",
    taskTakeoverFreshSub: "第一次被指派這個任務時給 agent 的指示 · 可編輯",
    taskUnblockedName: "擋著你手上任務的票解開了",
    taskUnblockedSub: "依賴的任務放行時給 agent 的通知 · 可編輯",
    // 唯讀文件的說明：說「這份是什麼」，不說「你沒有權限」——沒有任何人可以改，
    // 講權限會讓人去找一個根本不存在的角色來授權。
    bootDocReadOnlyNote:
      "這份文件顯示在這裡，是為了讓你看得到 agent 到底被告知了什麼；它不給任何人編輯，也沒有出廠版以外的版本。",
    bootDocSaveConfirmAcceleratedStop:
      "要儲存這份加速停止程序嗎？之後每一個被要求提前收工的 agent 都會讀到這份內容，而且是在只剩下一小段時間的情況下讀——寫得完才算數。",
    bootDocSaveConfirmTaskEvent:
      "要儲存這份任務事件程序嗎？之後每一次這個事件發生，被通知的 agent 都會讀到這份內容。",
    // ── 開機情境區塊：可編輯面（T-791e）──
    bootDocNoteHistoryLead: "版本紀錄只保留最近 ",
    bootDocNoteHistoryTail:
      " 版，而且是以「存檔次數」計、不是以時間計——連按幾次小修就會把較舊的版本沖掉。「還原出廠版」不受影響，永遠在。",
    bootDocSaveConfirmBoot:
      "要儲存這份啟動步驟嗎？啟動步驟改壞會讓之後開機的 agent 掛不上 SSE、因此永遠不會上線，而且不會有任何錯誤訊息——到時候也沒有人在線上可以救。存檔前請確認你看過預覽；真的出事就按「還原出廠版」。",
    bootDocSaveConfirmSystem:
      "要儲存這份系統互動說明嗎？之後開機的每一個 agent 都會讀到這份內容。",
    bootDocSaveConfirmOffboard:
      "要儲存這份〈停止〉嗎？之後每一個被收掉的 session 都會讀到這份內容，而且讀到的時候沒有人在線上可以問——而且這條路上沒有任何時鐘：一般停止、Refocus、改機器或換 model、token 快到期、context 的第一段門檻，全都是送出去之後等它自己回報。會倒數的那一種讀的是另一份〈加速停止〉，不是這份。所以這份要在「沒有人替它計時」的前提下寫得完才算數。",
    bootDocSaveConfirmAction: "確認儲存",
    // 堆疊呈現的文件，點標題才展開（T-6278）。兩份啟動步驟都預設收疊，讓一個
    // 畫面看得到兩份；標籤寫的是「按下去會怎樣」，不是目前狀態。
    docExpand: "展開這份文件",
    docCollapse: "收合這份文件",
    historyBootSystemTitle: "系統互動的版本紀錄",
    historyBootClaudeTitle: "啟動步驟（Claude Code）的版本紀錄",
    historyBootCodexTitle: "啟動步驟（Codex CLI）的版本紀錄",
    historyBootOffboardTitle: "停止的版本紀錄",
    historyAcceleratedStopTitle: "加速停止的版本紀錄",
    historyTaskCloseoutTitle: "任務結案的版本紀錄",
    historyTaskReassignPredecessorTitle: "任務轉派 · 給前任的版本紀錄",
    historyTaskTakeoverWithPredecessorTitle: "任務轉派 · 給接手人的版本紀錄",
    // T-6f44：這兩份**不再是唯讀的**（owner 的決定 2），所以它們跟其他八份
    // 一樣真的會有版本紀錄。上一版這裡寫著「唯讀、永遠不會有第二個版本」，
    // 那句話跟著決定 2 一起過期了。
    historyTaskTakeoverFreshTitle: "新任務的版本紀錄",
    historyTaskUnblockedTitle: "擋著你手上任務的票解開了的版本紀錄",
    // seed vs owner-edited
    defaultBadge: "預設",
    // ── detail: view / edit ──
    edit: "編輯",
    doneEdit: "完成編輯",
    cancel: "取消",
    reset: "重置",
    editorPlaceholder: "以 Markdown 撰寫…",
    // 「儲存＝整份取代」的提示（T-c33e）。開機情境那三塊的編輯器從逐段改成
    // 單一編輯框，原本由段落列隱含說出的事就必須明講：按下去送出的是整份文件。
    // T-3201 起，「整份」指的是可編輯的那一半：唯讀區不在編輯框裡，也沒有任何
    // 方式可以送出，所以能被這一次儲存蓋掉的只有下半。
    docReplaceNote:
      "儲存會用編輯框裡的內容「整份取代」這份文件可編輯的那一半——沒有逐段合併，沒被貼進來的段落就不會留下；上方的唯讀區不受影響，也改不動。",
    // 唯讀區那一塊的標籤（T-3201）。owner 的裁定是他必須看得見改不動的那一半
    //（「以前 global context 是固定內容 我們也是會顯示 只是不給改」），所以它
    // 被畫出來但沒有編輯框；標籤要說清楚它為什麼不是壞掉的輸入框。
    docReadOnlyHead: "唯讀區（程式產生，改不動）",
    // 存檔失敗但伺服器沒有給任何可引用的理由時的墊底文案（T-c33e）。
    docActionFailed: "動作失敗，請稍後重試",
    // 超過字數上限的紅字，兩個數字都要在螢幕上（T-791e，T-c33e 起共用）。
    docOverCapLead: "現在 ",
    docOverCapMid: " 字，超過上限 ",
    docOverCapTail: " 字，請先刪掉一些再儲存。",
    // ── 版本紀錄（T-7d33）——每份可編輯長文件保留最近 3 次修改，可還原 ──
    historyTitle: "版本紀錄",
    historySub: "系統保留最近 3 次修改；還原會覆蓋目前內容。",
    // 只在真的刪得掉的文件（任務手冊、自訂角色）下面出現——說明範圍，不是警告。
    historyDeleteNote:
      "版本紀錄只涵蓋這份文件的編輯；整份刪除不會留下紀錄，也無法從這裡還原。",
    historyLoading: "載入版本紀錄中…",
    historyError: "載入版本紀錄失敗，請稍後重試",
    historyEmpty: "還沒有保留任何版本",
    // 「真的寫了空字串」與「當時跟著出廠預設走」是兩件事，混成一句會讓後者看起來
    // 像一份被清空的文件——而還原它其實是把文件放回預設內容，不是清空（T-40f0
    // 節點 11，owner 2026-08-05 截圖）。
    historyNoContent: "（當時是空白內容）",
    historyDefaultContent: "（當時採用出廠預設內容）",
    historyByLabel: "修改者",
    historyDefaultBadge: "當時為預設內容",
    historyRestore: "還原這個版本",
    historyRestoreConfirmLead: "確定還原「",
    historyRestoreConfirmTail: "」這個版本？目前的內容會被覆蓋，但會存成新的版本紀錄。",
    historyRestoreConfirmAction: "確認還原",
    historyRestoreError: "還原失敗，請稍後重試",
    // ── 初始版本（T-1f39，owner 2026-07-31）——重置鈕退場後，清單最後一項就是
    //    這份文件出廠時的內容，也是唯一的重置入口，因此走同一個破壞性確認框。
    //    T-40f0（owner rc-28885813e065 ①）起這一列跟別的版本完全一樣：點下去先
    //    看得到內容與差異，還原仍在同一個破壞性確認框後面。
    historySeedTitle: "初始版本",
    historySeedNote: "這份文件最初附帶的內容。",
    historySeedRestore: "還原成初始版本",
    historySeedConfirm: "確定還原成初始版本？目前的內容會被覆蓋。",
    // 初始版本的內容讀不到時的誠實說法：不能講成「這個版本是空白的」（那是另
    // 一個、而且是錯的主張），但還原本身不需要這份內容，所以照樣按得下去。
    historySeedUnavailable:
      "初始版本的內容目前讀不到，暫時無法顯示或比較；還原成初始版本仍然可以執行。",
    // 讀完一個版本退回清單——關閉是離開版本紀錄，這是回上一層。
    historyBack: "返回版本列表",
    // 超過長度上限、伺服器一定會拒絕的版本：照樣列出來，但標成不可還原。
    historyBlockedBadge: "無法還原",
    historyBlockedReasonLead: "「",
    historyBlockedReasonMid: "」超過 ",
    historyBlockedReasonTail: " 字上限，且不比目前的內容短——伺服器會拒絕這次還原。",
    // ── 版本 modal（T-1f39）——點一列打開；預設看內容，右上角切到逐行差異 ──
    historyOpen: "檢視這個版本",
    historyPaneLabel: "顯示方式",
    historyPaneContent: "版本內容",
    historyPaneDiff: "與目前的差異",
    // 講明白比的是「伺服器上存著的」，不是編輯框裡還沒存的草稿。
    historyDiffNote: "與伺服器上目前存著的內容比較；編輯框裡尚未儲存的修改不算在內。",
    historyDiffPending: "目前的內容還沒載入完成，暫時無法比較。",
    historyVersionLabelLead: "此版本（",
    historyVersionLabelTail: "）",
    // 修改者：名字後面永遠附代號——名字會被改、代號不會，而還原是照代號認人的。
    historyActorLead: "（",
    historyActorTail: "）",
    historyCurrentLabel: "目前存檔內容",
    historyModalEmpty: "這個版本沒有任何內容。",
    // 這一句只有在「出廠預設本身就是空的」時才會出現（全域情境的預設就是空文件）；
    // 預設有內容的文件會直接把那份內容畫出來。
    historyModalDefaultContent: "這個版本當時採用出廠預設內容。",
    // 上面那一句的「讀不到」版本,而且**不共用** historySeedUnavailable —— 那句
    // 逐字講的是「初始版本」,印在一個有代號、有時間、有作者的留存版本上就是講錯
    // 版本身分,而它就站在還原鈕旁邊。
    historyDefaultUnreadable:
      "這個版本當時採用出廠預設內容,但預設內容目前讀不到,暫時無法顯示或比較;還原這個版本仍然可以執行。",
    historyClose: "關閉",
    // 一頁上同時放著兩份可編輯長文時（角色誌＝角色定義＋學習經驗），標題只寫
    // 「版本紀錄」看不出管的是哪一份——卡片得自己講清楚。owner 2026-07-31 實際
    // 在畫面上踩到這件事。
    historyRoleDefTitle: "角色定義的版本紀錄",
    historyLessonsTitle: "學習經驗的版本紀錄",
    historyInsightTitle: "判準(Insight)的版本紀錄",
    historyGlobalTitle: "全域情境的版本紀錄",
    historyManualLearningsTitle: "學習經驗的版本紀錄",
    // 任務定義頁：purpose／識別鍵已經不再留版本，卡片得說清楚它只代表 SOP。
    historySopTitle: "SOP 版本紀錄",
    historySopSub:
      "只有 SOP 會保留版本；用途與識別鍵的修改不留版本紀錄。系統保留最近 3 次修改；還原只會覆蓋 SOP。",
    historyField: {
      text: "內容",
      name: "名稱",
      definition_md: "角色定義",
      purpose: "用途",
      fields: "欄位",
      sop_md: "SOP",
      learnings: "學習經驗",
    },
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
    deleteRoleConfirmLead: "確定刪除角色「",
    deleteRoleConfirmTail:
      "」？該角色的成員及其對話、學習經驗將一併移除，無法復原。",
    deleteRoleConfirmAction: "確認刪除",
    deleteRoleOnline: "有成員在線上，無法刪除",
    deleteRoleError: "刪除失敗，請稍後重試",
    // ── 參數調整(伺服器參數;原本住在頭像選單的偏好設定,owner 2026-07-12
    // 搬到設定頁,讓參數集中一處。文案沿用搬家前的 profile.* 白話命名)──
    paramsLoadError: "載入參數失敗，請稍後重試",
    paramsSaveError: "沒存成，請再試一次",
    sessionTtl: "登入有效期",
    sessionTtlSub: "登入後多久需要重新輸入密碼",
    agentTokenTtl: "Agent token 有效期",
    agentTokenTtlSub: "新啟動的成員與外包工作者多久需要換新 token",
    ttl12h: "12 小時",
    ttl24h: "24 小時",
    ttl7d: "7 天",
    ttl30d: "30 天",
    notice: "Claude 第一次通知",
    noticeSub: "記憶用到這個比例，就把〈停止〉送給它，請它收乾淨後自己換手（要比下面的最後通牒小）",
    handover: "Claude 最後通牒",
    handoverSub: "到這個比例送最後通牒並自動換手，之後依「加速停止秒數」強制回收（40–90%）",
    codexNotice: "Codex 第一次通知",
    codexNoticeSub: "第幾輪 context compaction 後把〈停止〉送給它（要比下面的回合數小）",
    codexHandover: "Codex 最後通牒回合",
    codexHandoverSub: "完成這麼多次 context compaction 後自動重新聚焦；不依 context 百分比判斷",
    monitoringRefresh: "監控刷新間隔",
    monitoringRefreshSub: "收到連續事件時，最多每隔幾秒刷新一次（1–60）",
    seconds: "秒",
    acceleratedGrace: "加速停止秒數",
    acceleratedGraceSub:
      "按下加速停止之後，成員還有多少秒可以收尾；記憶第二段門檻自動換手也走同一個時鐘。這個時刻會原文告訴成員（10–3600）",
    rounds: "次",
    // T-ae38 起(T-30f1 又拆過一次):上限不再是一個。這些文件被刪掉的成本差很多
    // ——角色定義是常設說明、學習經驗是逐次累積的環境問答——所以不再共用同一把尺。
    docCapDuty: "角色定義字數上限",
    docCapDutySub:
      "一個角色的角色定義的字數上限。下限就是這一段自己的出廠預設（比其餘每一段都小），上限 100000，所以只能調高——調低會讓現在合法的文件變成只能越寫越短。",
    docCapInsight: "Insight 字數上限",
    docCapInsightSub:
      "一個角色的 Insight 的字數上限。下限就是出廠預設，上限 100000，所以只能調高。",
    docCapLearning: "學習經驗字數上限",
    docCapLearningSub:
      "一個角色的學習經驗的字數上限。下限就是出廠預設，上限 100000，所以只能調高。",
    docCapManualSop: "任務手冊 SOP 字數上限",
    docCapManualSopSub:
      "任務手冊的 SOP（做法藍圖）的字數上限。與下面那格各自獨立——SOP 是改寫收斂的藍圖，學習經驗是持續累積的紀錄，一個數字只能對其中一份是對的。下限就是出廠預設，上限 100000，所以只能調高。",
    docCapManualLearnings: "任務手冊學習經驗字數上限",
    docCapManualLearningsSub:
      "任務手冊的學習經驗的字數上限，與上面的 SOP 那格各自獨立。下限就是出廠預設，上限 100000，所以只能調高。",
    // T-c9b4:喚醒快照的聊天區塊預算。刻意不跟上面那幾格共用一段說明——那幾格
    // 的下限就是出廠預設(只能調高),這一格兩個方向都能調。
    // T-8:備份保留份數 N。說明文字必須把整數本身講不出來的兩件事講明——
    // 「是份數不是天數」與「是每一池不是每個資料夾」——因為需要知道的是轉這個
    // 旋鈕的人。
    backupRetain: "備份保留份數",
    backupRetainSub:
      "資料庫備份要保留幾份。超過這個數字的，會在下一次備份時直接從磁碟上刪掉——不是移到別的資料夾，刪掉就救不回來。有兩件事這個數字並不代表。它算的是「份數」，不是「天數」：它數的是檔案，所以能回溯多久完全看那幾天實際備份了幾次——忙的那幾天可能不到三天就用完，閒的時候可以撐超過一週。它也是「每一池」而不是「每個資料夾」：日常備份（定時＋手動）與升級前備份各自有各自的額度，所以這裡填 5，磁碟上最多會有十份，不是五份。範圍 1～20；上限是磁碟預算——佔用空間大約是這個數字的兩倍再乘上一份備份的大小。",
    backupRetainUnit: "份／池",
    chatBudget: "喚醒聊天字數預算",
    chatBudgetSub:
      "喚醒快照(resume_summary)裡聊天區塊的字數預算,含訊息、摺疊卡片、快照表頭與截斷提示;peek 回報的大小算的是同一個數字。範圍 1000~13000,可調高也可調低——聊天區塊每次都是重新裝箱的,調低只是下次帶回比較少則,被留下的部分照樣由「更早的訊息已省略」交代。",
    docUsage: "已用字數",
    chars: "字",
    // ── 存檔回讀對帳（T-1c2e，rework 後住在系統更新與備份區：secret 只顯示
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
    deleteManualConfirmLead: "確定刪除任務類型「",
    deleteManualConfirmTail:
      "」？其手冊（定義、SOP、學習經驗）將一併移除，無法復原。",
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
    // 三塊各自編輯（owner 2026-07-31 選定 P1）：每一塊的標頭各有一顆 編輯，
    // 三顆長得一樣，所以無障礙名稱要把區塊名帶進去。
    manualEditSectionLead: "編輯「",
    manualEditSectionTail: "」",
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
    // 機器狀態字：讀現有 machines（online）＋ monitoring（agents 數）——
    // 線上且無 agent ＝ 閒置、線上有 agent ＝ 忙碌、離線 ＝ 離線（誠實映射）
    assigneeMachineIdle: "閒置",
    assigneeMachineBusy: "忙碌",
    assigneeMachineOffline: "離線",
    assigneeMachineUnset: "未選機器",
    assigneeMachineNote:
      "這個類型的外包只會在你選的機器上喚醒。沒選機器、或該機器離線時，一律不喚醒，原因會顯示在該外包上。",
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
