// e2e_test/tests/20_chat_jump_to_origin.spec.js
// T-48 ③ · 跳到原訊息 —— 定位到一則「已經捲出載入視窗之外」的訊息。
//
// 這是這張票對使用者最直接的那一件，owner 逐字：「請示卡的跳到原訊息功能，以及
// 有訊息的通知時，我希望都可以正確定位到該訊息」。
//
// 🔴 修之前是這樣壞的：聊天室開起來只載最新 30 則,「跳到原訊息」在**已經載進
// DOM 的那些列裡** querySelector，找不到就**不出聲、直接捲到最下面** —— 跟
// 「跳對了、剛好那一則在最下面」長得一模一樣。目標只要比 30 則舊就必定跳錯。
//
// 修完是這樣：以訊息 id 開窗（GET /api/chat?end_id= 往舊、?start_id= 往新，
// 兩端都含），落在那一則上。
//
// 🔴 T-48 fix12（owner rc-e1fb80065f8f「可以直接在這票做，並且一次撈100則撈完」
// ＋ c-6a973512ed77「我是指整個訊息撈完才 render」）：往新那條**不再是一次手勢
// 一頁**。`loadAround` 從錨點一路撈到活尾巴，每通 100 則（`limit` 在 window 路徑
// 本來就吃得到，上限 `chatWindowMaxLimit = 200`），**全部撈完才 render 一次**。
// 所以這支原本那兩條斷言（「載進來的列數遠少於整條線」、「最新那一則不在 DOM」）
// 現在是**反的**，並且已經改寫成它們的對立面。
//
// ⚠️ 那個反轉曾經來回過一次。f995f507 之前有一條「走廊」會自動走完，被拿掉時
// 這裡留下一行註解說它「實測 8,000 則約兩分鐘、2.6 GB」。那個數字**沒有任何產物
// 支撐**，而且重量之後不成立：8,000 則一次載進畫面實測 0.58 秒 / 49 MB heap /
// 156k DOM 節點（work/T-48-docs/fix11-render-cost.md，真 Chromium，附原始輸出）。
// 真正要小心的是**一頁一頁 commit** 的累積成本（8,000 則 12.4 秒），而 fix12 正是
// 因此改成一次 commit。
//
// 真瀏覽器才量得到的部分：落點是不是真的在視窗裡（jsdom 沒有版面，
// scrollIntoView 只能記錄有沒有被呼叫），以及窄寬兩寬下都要成立。
const { test, expect } = require('@playwright/test');
const {
  BASE,
  ownerToken,
  hireMember,
  mintMemberToken,
  postChatAs,
  unreadCountOf,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

const TOTAL = 80; // ≫ 一頁 30，目標挑第 3 則:往舊往新都還有東西
const TARGET_INDEX = 3;
const PAD = '— 墊長一點，讓每一列都有高度，整條線真的會溢出視窗';

const WIDTHS = [
  { name: '窄 (390)', size: { width: 390, height: 780 } },
  { name: '寬 (1280)', size: { width: 1280, height: 900 } },
];

for (const w of WIDTHS) {
  test.describe(`T-48 ③ · 跳到原訊息 — ${w.name}`, () => {
    test('定位到一則載入視窗之外的訊息，而且沒有把整條歷史拉下來', async ({
      page,
    }) => {
      await page.setViewportSize(w.size);
      const request = page.request;
      const token = await ownerToken(request);
      const NAME = uniqueName('JumpOrigin M');
      const M = await hireMember(request, token, NAME);

      const ids = [];
      for (let i = 1; i <= TOTAL; i++) {
        const msg = await postChatAs(request, token, M.id, `line ${i} ${PAD}`);
        ids.push(msg.id);
      }
      const targetId = ids[TARGET_INDEX - 1];

      await bootAuthedSpa(page, token);
      // 這就是請示卡與通知用的那條路由 —— 只帶得出一個訊息 id，帶不出游標，
      // 正是舊實作沒辦法定位的原因。
      await page.evaluate(
        ([mid, cid]) => {
          window.location.hash = `#office/chat/${cid}/msg/${mid}`;
        },
        [targetId, M.id],
      );

      const thread = page.locator('.chat__messages');
      await expect(thread).toBeVisible();
      const target = thread.locator(`[data-msg-id="${targetId}"]`);

      // ① 那一則真的被撈回來了 —— 它比最新 30 則舊得多，舊實作連 DOM 裡都沒有。
      await expect(target).toBeAttached();
      await expect(target).toContainText(`line ${TARGET_INDEX} `);
      // ② 而且真的**停在畫面裡**（jsdom 量不到這一格）。
      await expect(target).toBeInViewport();
      // ③ 定位閃光落在那一列上，不是別人身上。
      await expect(target).toHaveClass(/chat__msg--located/);

      // ④ 🔴 fix12 的正題，而且這一條以前是它的反面：一路撈到了活尾巴。
      // 以前這裡斷言「最新那一則不在 DOM」（一次手勢一頁），現在必須在 ——
      // 否則讀的人得自己走回去，而那條路已經沒有了。
      await expect(
        thread.locator(`[data-msg-id="${ids[TOTAL - 1]}"]`),
        '跳到原訊息要一路撈到最新那一則',
      ).toBeAttached();
      const loaded = await thread.locator('.chat__msg').count();
      expect(loaded, '整條線都該在畫面上').toBe(TOTAL);

      // ⑤ 箭頭仍然要在：最新那一則**已經載進來了**，但它不在視窗裡 ——
      // 讀的人停在目標上。箭頭講的是「你不在最新」，不是「最新還沒載」。
      await expect(page.getByTestId('chat-jump-latest')).toBeVisible();

      // ⑥ 點箭頭 → 捲到真的最新那一則。
      await page.getByTestId('chat-jump-latest').click();
      const newest = thread.locator(`[data-msg-id="${ids[TOTAL - 1]}"]`);
      await expect(newest).toBeInViewport();
    });

  });
}

// ─────────────────────────────────────────────────────────────────────────────
// 這一票剩下的兩個「安靜地做錯事」。兩件都只在真瀏覽器 + 真 server 才算數:
// 一件要看畫面上有沒有真的出現那句話,一件要看 server 端的未讀數有沒有被動到。
// 只跑一個寬度就夠 —— 這兩件都不是版面問題。
test.describe('T-48 · 剩下的靜默失敗', () => {
  test('定位失敗時,畫面上真的講一句話 —— 不是只有 console', async ({ page }) => {
    // 🔴 接上以訊息 id 開窗之後,「那則訊息真的不存在」變成 server 的 404。
    // 前端退回底部 —— 光是這樣,跟「跳成功、剛好那則在最下面」長得一模一樣,
    // 正是這張票要拿掉的那個病。所以要在畫面上說出來。
    await page.setViewportSize({ width: 1280, height: 900 });
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, uniqueName('JumpMiss M'));
    await postChatAs(request, token, M.id, `only line ${PAD}`);

    await bootAuthedSpa(page, token);
    // 格式合法(c-<hex>)但 server 上沒有這一則 —— 空白頁與 404 的差別就在這裡。
    await page.evaluate(
      (cid) => {
        window.location.hash = `#office/chat/${cid}/msg/c-00000000000000000000000000000000`;
      },
      M.id,
    );

    const notice = page.locator('.chat__jump-miss');
    await expect(notice).toBeVisible();
    await expect(notice).toContainText('找不到那則訊息');
    // 而且是關得掉的,不是永遠賴在那裡。
    await notice.locator('button').click();
    await expect(notice).toHaveCount(0);
  });

  test('跳到舊訊息不會把中間沒看過的標成已讀,回到最新那一端才標', async ({
    page,
  }) => {
    // owner 裁定逐字:mark-read 表達的意圖是「我看過了」,不是「我跳過來過」。
    // 這裡量的是 server 端的未讀數 —— 前端旗標可以說謊,未讀數不會。
    await page.setViewportSize({ width: 1280, height: 900 });
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, uniqueName('JumpRead M'));
    const tokM = await mintMemberToken(request, token, M.id, 1);

    const ids = [];
    for (let i = 1; i <= TOTAL; i++) {
      const msg = await postChatAs(request, tokM, 'owner', `line ${i} ${PAD}`);
      ids.push(msg.id);
    }
    expect(await unreadCountOf(request, token, M.id)).toBe(TOTAL);

    await bootAuthedSpa(page, token);
    await page.evaluate(
      ([mid, cid]) => {
        window.location.hash = `#office/chat/${cid}/msg/${mid}`;
      },
      [ids[TARGET_INDEX - 1], M.id],
    );

    const thread = page.locator('.chat__messages');
    const target = thread.locator(`[data-msg-id="${ids[TARGET_INDEX - 1]}"]`);
    await expect(target).toBeInViewport();

    // ① 🔴 fix12 之後這一格更難守,不是更簡單。以前擋 mark-read 的是
    // `hasNewer`(錨點視窗),而走訪現在會把中間那一整段**全部載進來**,
    // `hasNewer` 在落地那一瞬間就是 false —— 進房那支 mark-read effect 完全不看
    // 視窗位置,守衛換成 `tailSeen`(讀的人有沒有真的到過活尾巴)才擋得住。
    // 這裡量的是 server 端的未讀數:前端旗標可以說謊,未讀數不會。
    await expect(
      thread.locator(`[data-msg-id="${ids[TOTAL - 1]}"]`),
      '前提:走訪真的把最新那一則也載進來了 —— 否則擋住 mark-read 的是舊守衛',
    ).toBeAttached();
    // 給它時間去做錯事:mark-read 是 fire-and-forget,不等它就等於沒量到。
    await page.waitForTimeout(1500);
    expect(
      await unreadCountOf(request, token, M.id),
      '跳過去不等於看過 —— 走訪載進來的那一整段不准被標成已讀',
    ).toBe(TOTAL);

    // ② 🔑 另一個方向,而且不能省:只釘①的話,「整條路壞掉、永遠不標」也會過,
    // 那本身就是另一個靜默失敗。按下回到最新 → 真的到了活的尾巴 → 才標。
    await page.getByTestId('chat-jump-latest').click();
    await expect(
      thread.locator(`[data-msg-id="${ids[TOTAL - 1]}"]`),
    ).toBeInViewport();
    await expect
      .poll(async () => unreadCountOf(request, token, M.id), {
        message: '回到最新那一端之後就要標已讀',
      })
      .toBe(0);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// 讀取失敗 ≠ 訊息不見了。這一件只有真瀏覽器算數:要真的讓那兩個開窗請求失敗
// (route.abort(),等同斷線/5xx),再看畫面上到底講了哪一句、以及那顆重試鈕按下去
// 有沒有真的再撈一次。
//
// 🔴 為什麼這是產品而不是文案潤飾:「已經被清掉了」會讓使用者**不再試**,
// 「現在讀不到」會讓他**再試一次**。訊息躺在 502 後面時說前者,就是這張票開票的
// 那個病 —— 對使用者說一句不成立的話 —— 換了一個地方重演。
test.describe('T-48 · 讀取失敗要說「現在讀不到」,而且真的給得出重試', () => {
  test('開窗請求失敗時說的是新那句、附重試鈕;按下去真的再撈一次並落在那一則', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, uniqueName('JumpUnreach M'));

    const ids = [];
    for (let i = 1; i <= TOTAL; i++) {
      const msg = await postChatAs(request, token, M.id, `line ${i} ${PAD}`);
      ids.push(msg.id);
    }
    const targetId = ids[TARGET_INDEX - 1];

    // 只打掉「開窗」那兩個請求(帶 start_id / end_id 的),一般的載入照常 ——
    // 這樣量到的才是跳轉這條路的失敗,不是整個座艙壞掉。
    const isAnchorWindow = (url) =>
      url.pathname === '/api/chat' &&
      (url.searchParams.has('start_id') || url.searchParams.has('end_id'));
    await page.route(isAnchorWindow, (route) => route.abort('failed'));

    await bootAuthedSpa(page, token);
    await page.goto(`/#office/chat/${M.id}/msg/${targetId}`);
    await page.reload();

    const notice = page.locator('.chat__jump-miss');
    await expect(notice).toBeVisible({ timeout: 15_000 });
    // ① 說的是「現在讀不到」,不是「被清掉了」。兩個方向都斷言 —— 只釘一句的話,
    //    把兩句合成一句照樣會過。
    await expect(notice).toContainText('現在讀不到那則訊息');
    await expect(notice).not.toContainText('可能已經被清掉了');
    // ② 而且真的有一條再試一次的路。
    const retry = page.getByTestId('jump-miss-retry');
    await expect(retry).toBeVisible();

    // ③ 辦公室回來了 —— 按下去要真的再撈一次,而且這次要落在那一則身上。
    //    ⚠️ 這一格是 F3 的形狀最容易復發的地方:鈕在、按得下去、什麼都沒發生。
    await page.unroute(isAnchorWindow);
    await retry.click();

    const thread = page.locator('.chat__messages');
    const target = thread.locator(`[data-msg-id="${targetId}"]`);
    await expect(target).toBeAttached({ timeout: 15_000 });
    await expect(target).toBeInViewport();
    await expect(target).toHaveClass(/chat__msg--located/);
    await expect(notice, '撈到了就不該還掛著提示').toHaveCount(0);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// owner 交辦逐字:「也要測試如果有新訊息跳進來,點選預覽畫面跳下去時,運作會正常」。
//
// 這是這一票最容易壞的接縫,因為它同時要滿足兩件相反的事:**停在錨點**(不准被
// 新訊息拉走、不准把中間那段標成看過)與**跳到最新**(點下去要真的到活的尾巴)。
//
// 🔴 fix13 —— 這段檔頭原本寫的前提**已經不成立了**,連同它底下那條斷言一起反轉。
// 它以前說的是:「錨點視窗期間 useChat 刻意不跑最新頁的載入,所以新訊息進不了那個
// thread —— 預覽列在錨點視窗下是不會出現的,讓位給箭頭」。fix12 之後 `loadAround`
// 從錨點**一路撈到活尾巴、全部撈完才 commit**,所以跳轉一落地 thread 就**已經接在
// 活尾巴上**:SSE 的新訊息當然進得來,於是出現的是**預覽列**,而**箭頭讓位**
// —— 方向恰好跟舊前提相反,但互斥規則本身(rc-72054864ff88,一次只准有一個底部
// 提示,見 lib/chatBottomAffordance)一個字都沒變,只是換成另一邊贏。
// 實測產物:work/T-48-docs/fix13-e2e-walk-to-latest.md 附了紅那一刻的 DOM 快照
// —— `during-anchor` 那一列在 thread 裡、預覽列在場,畫面不是什麼都沒說。
//
// 這支把整條脊椎走完:錨點 →(新訊息)→ 預覽列取代箭頭 → 關掉預覽列 → 箭頭回來 →
// 按箭頭回到活尾巴 → 捲上去 →(再一則新訊息)→ 預覽列 → 點它 → 落在最新那一則 →
// 未讀歸零。兩個底部提示各自的路都走過,而且每一步都問一次 server 端的未讀數。
//
// 🔴 只有真瀏覽器算數:jsdom 沒有版面,量不到「那一則真的在視窗裡」;而未讀數要
// 問 server,前端旗標可以說謊。
test.describe('T-48 · 錨點視窗中有新訊息進來,點預覽列跳下去', () => {
  test('錨點裡的新訊息由預覽列接手、箭頭讓位,兩條路都回得到活尾巴並落在最新那一則', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, uniqueName('JumpPreview M'));
    const tokM = await mintMemberToken(request, token, M.id, 1);

    const ids = [];
    for (let i = 1; i <= TOTAL; i++) {
      const msg = await postChatAs(request, tokM, 'owner', `line ${i} ${PAD}`);
      ids.push(msg.id);
    }

    // 🔴 這一支**必須從一個已經帶著 msgId 的 URL 開機**,不能像上面幾支那樣先開
    // 座艙再改 hash。實測(cold run,加了 [REQ] 追蹤):辦公室會自己選一間房,於是
    // ChatArea 先以「沒有跳轉目標」掛載一次 —— `GET /api/chat?with=M` +
    // `POST /api/chat/mark-read` 都已經發出去了,65ms 後 end_id/start_id 才進來。
    // 未讀 81 因此變成 1,而那與被測行為無關,是測試自己把房間先打開的。
    // 帶著 hash 開機就是通知/保存連結真正的那條路,也讓第一次掛載就拿到 anchor。
    await bootAuthedSpa(page, token);
    await page.goto(`/#office/chat/${M.id}/msg/${ids[TARGET_INDEX - 1]}`);
    await page.reload();

    const thread = page.locator('.chat__messages');
    const target = thread.locator(`[data-msg-id="${ids[TARGET_INDEX - 1]}"]`);
    await expect(target).toBeInViewport();

    // ① 還沒有新訊息時,停在錨點上的底部提示是**箭頭** —— 下一格量的是它讓位,
    //    所以要先確認它本來真的在場,否則「箭頭不見了」也可能是它從來沒出現過。
    await expect(
      page.getByTestId('chat-jump-latest'),
      '停在錨點、最新那一則不在視窗裡 —— 箭頭必須在場',
    ).toBeVisible();

    // ② 🔴 fix13 反轉的那一格。新訊息在讀者停在錨點時進來:
    //    · 畫面必須**留在原地**(不准被新訊息拉走)——這一半沒有變;
    //    · 而底部提示換成**預覽列**,**箭頭讓位**。fix12 之後 thread 一落地就
    //      接著活尾巴,新訊息進得來 ⇒ 有東西可以預覽 ⇒ 依互斥規則預覽列贏。
    //      這裡兩邊都斷言:只釘「預覽列在」的話,兩個提示同時貼在畫面上也會過,
    //      而那正是 lib/chatBottomAffordance 存在的理由。
    const duringBody = `during-anchor ${PAD}`;
    const during = await postChatAs(request, tokM, 'owner', duringBody);
    const duringStrip = page.getByTestId('chat-new-msg-preview');
    await expect(
      duringStrip,
      '錨點落地就接著活尾巴了 —— 新訊息必須進得來並由預覽列說出來',
    ).toBeVisible({ timeout: 15_000 });
    await expect(duringStrip).toContainText(duringBody);
    await expect(target, '新訊息不准把讀者從錨點拉走').toBeInViewport();
    await expect(
      page.getByTestId('chat-jump-latest'),
      '箭頭要讓位給預覽列 —— 一次只准有一個底部提示',
    ).toBeHidden();
    // 🔴 中間那一大段誰都沒看過 —— server 端的未讀數一則都不准少。
    // 走訪把 80 則全載進來、`hasNewer` 已經是 false,擋住 mark-read 的只剩
    // `tailSeen`。前端旗標可以說謊,未讀數不會。
    expect(
      await unreadCountOf(request, token, M.id),
      '停在錨點視窗時不該送出 mark-read',
    ).toBe(TOTAL + 1);

    // ③ 關掉預覽列 —— 箭頭要回來,而且**不准順手把人捲到底**:關掉的是提示,
    //    不是位置,未讀數也一則都不准少。
    await page.getByTestId('chat-new-msg-dismiss').click();
    await expect(duringStrip).toBeHidden();
    await expect(
      page.getByTestId('chat-jump-latest'),
      '預覽列關掉之後,箭頭要接回來 —— 否則讀者手上一條回去的路都沒有',
    ).toBeVisible();
    await expect(target, '關掉提示不等於離開錨點').toBeInViewport();
    expect(
      await unreadCountOf(request, token, M.id),
      '關掉預覽列不是「我看過了」',
    ).toBe(TOTAL + 1);

    // ④ 按箭頭回到活的尾巴 —— 錨點期間那則新訊息也在這裡出現。
    await page.getByTestId('chat-jump-latest').click();
    await expect(thread.locator(`[data-msg-id="${during.id}"]`)).toBeInViewport();
    await expect
      .poll(async () => unreadCountOf(request, token, M.id), {
        message: '回到活的尾巴之後就要標已讀',
      })
      .toBe(0);

    // ⑤ 🔑 接回活尾巴之後,普通的預覽列路徑要**完全正常** —— 這一格才是 owner
    //    問的那句話。捲上去(讓最新那一則離開視窗)、對方再開口。
    //
    // ⚠️ 這個 waitForTimeout 原本是在等 `scrollToLatest` 的 2600ms 修正窗關掉
    // (那段期間任何一次 reflow 都會把畫面再拉回底部,於是新訊息落地時 near-bottom
    // 仍為真、走的是自動跟隨而不是預覽列)。那個修正窗已經在 T-48 刪掉了
    // (owner rc-6c27f486ef9d),所以這裡不再有東西會把畫面拉回去 —— 留著只是一段
    // 讓版面靜下來再量的餘裕。⚠️ 原本這裡寫的兩個來源(圖片解碼、卡片補撈)今天
    // 都推不動版面了:縮圖有固定的 220px 框,等待中的請示卡在 commit 之前就抓完。
    // 捲的幅度也刻意只夠讓最新那一則
    // 離開視窗,不捲到頂,免得順帶把 loadOlder 也牽進來。
    await page.waitForTimeout(3000);
    await thread.evaluate((el) => {
      el.scrollTop = el.scrollHeight - el.clientHeight - 600;
    });
    await expect(
      page.getByTestId('chat-jump-latest'),
      '最新那一則離開視窗就該有箭頭 —— 沒有的話下面那半是白等的',
    ).toBeVisible({ timeout: 10_000 });
    const lateBody = `after-return ${PAD}`;
    const late = await postChatAs(request, tokM, 'owner', lateBody);
    const strip = page.getByTestId('chat-new-msg-preview');
    await expect(
      strip,
      '接回活尾巴之後,新訊息必須進得來(SSE 載入不能還被錨點擋著)',
    ).toBeVisible({ timeout: 15_000 });
    await expect(strip).toContainText(lateBody);
    await expect(
      page.getByTestId('chat-jump-latest'),
      '箭頭要讓位給預覽列',
    ).toBeHidden();

    // ⑥ 點預覽列跳下去 —— 落在**最新那一則**,提示消失,未讀再次歸零。
    await page.getByTestId('chat-new-msg-jump').click();
    await expect(thread.locator(`[data-msg-id="${late.id}"]`)).toBeInViewport();
    await expect(strip).toBeHidden({ timeout: 10_000 });
    await expect
      .poll(async () => unreadCountOf(request, token, M.id), {
        message: '點預覽列跳到最新之後要標已讀',
      })
      .toBe(0);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// T-48 · 第三輪獨立審查 R3-1 —— 「跳到一半切去別條對話」。
//
// 🔴 這是活過的 bug,不是理論風險:`load()` 的錨點閘原本靠一個**不分 peer、也不隨
// 訂閱重置**的計數擋著。A 的錨點兩個平行 GET 還在空中時點另一個人的 roster row,
// B 的第一次載入就被 A 留下的計數擋掉 —— 而且擋掉之後沒有人會再叫一次
// (load() 只由訂閱/SSE/focus 觸發)。原始量測:B 的房間 22 秒都還是 0 列,A 的錨點
// 第 8 秒就落地了也不會自己好。
//
// 只有真瀏覽器算數:切換對話是使用者手勢(點 roster row),而它同時牽動 ChatArea 的
// member prop 與 useChat 的訂閱重建 —— jsdom 那一層(ChatArea.anchor-entry.test.tsx)
// 量得到同一件事,但量不到「真的點下去」。
test.describe('T-48 · 錨點還在飛的時候切去別條對話', () => {
  test('切過去的那一間照樣載得起來,而且上一條的錨點落地也不會把它換掉', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    const request = page.request;
    const token = await ownerToken(request);
    const NAME_A = uniqueName('SwitchAnchor A');
    const NAME_B = uniqueName('SwitchAnchor B');
    const A = await hireMember(request, token, NAME_A);
    const B = await hireMember(request, token, NAME_B);

    const idsA = [];
    for (let i = 1; i <= TOTAL; i++) {
      const msg = await postChatAs(request, token, A.id, `A line ${i} ${PAD}`);
      idsA.push(msg.id);
    }
    const B_COUNT = 5;
    for (let i = 1; i <= B_COUNT; i++) {
      await postChatAs(request, token, B.id, `B line ${i} ${PAD}`);
    }

    // 把開窗那兩個請求押在空中 —— 伺服器一忙就是這個形狀,只是這裡把它拉長到
    // 肉眼與斷言都追得上。
    const HOLD_MS = 8000;
    const isAnchorWindow = (url) =>
      url.pathname === '/api/chat' &&
      (url.searchParams.has('start_id') || url.searchParams.has('end_id'));
    await page.route(isAnchorWindow, async (route) => {
      await new Promise((r) => setTimeout(r, HOLD_MS));
      await route.continue();
    });

    const anchorReqs = [];
    page.on('request', (r) => {
      const u = new URL(r.url());
      if (isAnchorWindow(u)) anchorReqs.push(u.href);
    });

    await bootAuthedSpa(page, token);
    await page.goto(`/#office/chat/${A.id}/msg/${idsA[TARGET_INDEX - 1]}`);
    await page.reload();

    const thread = page.locator('.chat__messages');
    // 前提:A 的錨點真的在空中,而且 A 的房間還是空的(錨點優先 ⇒ 不先載最新頁)。
    await expect
      .poll(() => anchorReqs.length, { message: 'A 的錨點必須先發出去' })
      .toBeGreaterThanOrEqual(2);
    await expect(
      thread.locator('.chat__msg'),
      '前提:錨點還沒落地,A 的房間是空的',
    ).toHaveCount(0);

    // 使用者手勢:點 B 的 roster row。
    await page.locator('.member-card', { hasText: NAME_B }).click();

    // 🔑 B 的房間必須在 A 的錨點落地**之前**就填滿 —— 那正是舊碼永遠到不了的
    // 那一格(舊碼量到的是 22 秒 0 列)。
    await expect(
      thread.locator('.chat__msg'),
      'B 的房間被上一條對話的錨點鎖住了 —— 這正是 R3-1',
    ).toHaveCount(B_COUNT, { timeout: HOLD_MS - 2000 });
    await expect(thread.locator('.chat__msg').last()).toContainText(
      `B line ${B_COUNT} `,
    );

    // …而且 A 的錨點落地之後,不准把 B 的房間換掉。
    await page.waitForTimeout(HOLD_MS);
    await expect(thread.locator('.chat__msg')).toHaveCount(B_COUNT);
    await expect(
      thread.locator(`[data-msg-id="${idsA[TARGET_INDEX - 1]}"]`),
      'A 的錨點視窗不准落在 B 的房間裡',
    ).toHaveCount(0);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// T-48 fix14 · 同一間房裡的**第二次**跳轉 —— e2e 層在這一輪之前是零覆蓋，而它
// 正是 fix14 修的主角。
//
// 🔴 為什麼它是自己一格，而不是上面那幾支的重複：OfficePage 的 key 是 peerId，
// 所以「房間已經開著、只換 msgId」（同一位成員的第二次跳轉、上一頁、舊連結）
// **不會 remount**。fix14 之前 `initialLoading` 答的是「這條對話第一次載入」，
// 在這條路上恆為 false ⇒ 整段走訪畫面停在**舊內容**、零指示，而那正是最慢的一格
// （走訪要一路撈到活尾巴才 commit）。
//
// 🔴 而且不能只驗轉圈。實作者自己挖到的那一格：旗標若在 commit **之後**才放下，
// 轉圈是**取代**訊息區的 ⇒ commit 落在還是轉圈的那一幀時，跳轉的 reactor 在 DOM
// 裡查不到目標列 ⇒ **沒有人捲**。畫面上會是「轉圈出現過、訊息也回來了、但人停在
// 錯的地方」—— 只斷言轉圈的測試會全綠。所以每一次跳轉都要把
// 「轉圈在場 → 訊息區被它取代 → 落在目標那一則且亮起定位閃光」整條走完。
test.describe('T-48 fix14 · 同一間房裡的第二次跳轉', () => {
  test('房間已經開著、只換 msgId —— 兩次跳轉都有轉圈，而且都真的停在目標那一則', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, uniqueName('SecondJump M'));

    const ids = [];
    for (let i = 1; i <= TOTAL; i++) {
      const msg = await postChatAs(request, token, M.id, `line ${i} ${PAD}`);
      ids.push(msg.id);
    }

    await bootAuthedSpa(page, token);
    // ① 先用**普通的方式**進房 —— 沒有 msgId，房間開著、停在活尾巴。
    //    這一步就是這支的前提：接下來每一次跳轉都是「房間已經開著」的那條路。
    await page.goto(`/#office/chat/${M.id}`);
    const thread = page.locator('.chat__messages');
    await expect(
      thread.locator(`[data-msg-id="${ids[TOTAL - 1]}"]`),
      '前提：普通進房要落在活尾巴',
    ).toBeInViewport({ timeout: 15_000 });

    // 把開窗那兩個請求押慢 —— 轉圈延遲 150ms 才出現，走訪在本機快到肉眼與斷言
    // 都追不上。押慢的是**網路**，不是被測的判準：真實世界的慢網路就是這個形狀。
    const HOLD_MS = 1200;
    const isAnchorWindow = (url) =>
      url.pathname === '/api/chat' &&
      (url.searchParams.has('start_id') || url.searchParams.has('end_id'));
    await page.route(isAnchorWindow, async (route) => {
      await new Promise((r) => setTimeout(r, HOLD_MS));
      await route.continue();
    });

    const spinner = page.locator('.chat__loading');

    // 只換 hash 的 msgId —— 這就是不會 remount 的那條路。
    const jumpTo = async (msgId) => {
      await page.evaluate(
        ([cid, mid]) => {
          window.location.hash = `#office/chat/${cid}/msg/${mid}`;
        },
        [M.id, msgId],
      );
    };
    // 一次跳轉要走完的整條脊椎：轉圈在場 → 它**取代**訊息區 → 落在目標那一則 →
    // 定位閃光在那一列上 → 轉圈收掉。
    const expectSpinnerThenLandOn = async (msgId, label) => {
      await expect(spinner, `${label}：走訪期間必須有轉圈`).toBeVisible({
        timeout: 15_000,
      });
      await expect(spinner).toContainText('正在載入對話…');
      await expect(
        thread,
        `${label}：轉圈是取代訊息區的 —— 沒取代的話下面那格量不到 fix14 的風險`,
      ).toHaveCount(0);
      const target = thread.locator(`[data-msg-id="${msgId}"]`);
      await expect(
        target,
        `${label}：🔴 轉圈出現過不算數 —— 要真的有人把畫面捲到目標那一則`,
      ).toBeInViewport({ timeout: 20_000 });
      await expect(target).toHaveClass(/chat__msg--located/);
      await expect(spinner, `${label}：撈完了轉圈就該收掉`).toHaveCount(0);
    };

    // ② 第一次跳轉（房間已經開著）。
    await jumpTo(ids[49]);
    await expectSpinnerThenLandOn(ids[49], '同房第一次跳轉');

    // ③ 🔴 第二次跳轉，同一間房、只換 msgId —— fix14 的主角。
    //    起跳時畫面上**已經有內容**（上一次跳轉的窗），這正是「問在
    //    messages.length 之後就永遠走不到轉圈那個分支」的那一格。
    await jumpTo(ids[TARGET_INDEX - 1]);
    await expectSpinnerThenLandOn(ids[TARGET_INDEX - 1], '同房第二次跳轉');
    await expect(
      thread.locator(`[data-msg-id="${ids[TARGET_INDEX - 1]}"]`),
    ).toContainText(`line ${TARGET_INDEX} `);

    // ④ 🔴 另一半，而且它是「轉圈判準變寬」之後唯一擋得住把它變成**恆亮**的東西。
    //    第二次跳轉之後整條線（80 則）都已經在手上了，再跳到**已經載進來**的一則
    //    不需要任何網路 —— 所以這一格要成立的是相反的三件事：
    //      · 一個開窗請求都不准再發（已經握在手上的東西不用再買一次）；
    //      · 一格轉圈都不准畫（ChatThreadLoading 自己的話：「A WAIT NOBODY
    //        NOTICED IS NOT A WAIT WORTH DRAWING」—— 沒有等待就畫轉圈是雜訊，
    //        而且會讓畫面閃一下）；
    //      · 但**還是要真的捲過去**，落在那一則並亮起定位閃光。
    //    ⚠️ 這一格是實測出來的，不是推的：加這支時原本這裡也寫成「要有轉圈」，
    //    紅了之後量到第三、第四次跳轉的 `/api/chat` 請求數是 0（見
    //    work/T-48-docs/fix13-e2e-walk-to-latest.md 第三輪的 [DIAG] 逐字輸出），
    //    是**測試寫錯了**，不是產品少畫了。
    const chatReqs = [];
    page.on('request', (r) => {
      const u = new URL(r.url());
      if (u.pathname === '/api/chat') chatReqs.push(u.search);
    });
    await expect(
      thread.locator(`[data-msg-id="${ids[70]}"]`),
      '前提：這一則已經在手上了 —— 否則下面量的不是「不用再撈」那一格',
    ).toBeAttached();

    await jumpTo(ids[70]);
    const landed = thread.locator(`[data-msg-id="${ids[70]}"]`);
    await expect(landed).toBeInViewport({ timeout: 15_000 });
    await expect(landed).toHaveClass(/chat__msg--located/);
    await expect(
      spinner,
      '目標已經在手上，沒有等待可言 —— 不准畫轉圈',
    ).toHaveCount(0);
    expect(
      chatReqs,
      '目標已經在手上 —— 不准再買一次',
    ).toEqual([]);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// T-48 fix14 · 走訪**還在撈**的時候切去別條對話 —— 另一格 e2e 今天沒有的。
//
// 上面 R3-1 那支量的是「帶著錨點**開機**、第一次掛載還在飛時切走」。這一支量的是
// 另一個入口，也是 fix14 新增 `cancelled` 態要守的那個：房間**已經開著**、在同一間
// 房裡發動跳轉，走訪的迴圈已經在一頁一頁打請求了，這時候讀者切去別人。
//
// 🔴 兩件事要同時成立，而且第二件是新的：
//   · 切過去的那一間要**正常載得起來**（不准被上一條的閂鎖住）；
//   · 舊走訪的結果**不准落到新畫面上** —— 它被叫停之後什麼都不准 commit。
//     沒有這一條，讀者會在 B 的房間裡看到 A 的歷史，而那是最難看懂的一種錯。
test.describe('T-48 fix14 · 同房跳轉還在撈的時候切去別條對話', () => {
  test('切過去的那一間正常載入，被叫停的走訪一列都不准落到新畫面上', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    const request = page.request;
    const token = await ownerToken(request);
    const NAME_A = uniqueName('CancelWalk A');
    const NAME_B = uniqueName('CancelWalk B');
    const A = await hireMember(request, token, NAME_A);
    const B = await hireMember(request, token, NAME_B);

    const idsA = [];
    for (let i = 1; i <= TOTAL; i++) {
      const msg = await postChatAs(request, token, A.id, `A line ${i} ${PAD}`);
      idsA.push(msg.id);
    }
    const B_COUNT = 5;
    for (let i = 1; i <= B_COUNT; i++) {
      await postChatAs(request, token, B.id, `B line ${i} ${PAD}`);
    }

    await bootAuthedSpa(page, token);
    // 前提：A 的房間用普通的方式開著。
    await page.goto(`/#office/chat/${A.id}`);
    const thread = page.locator('.chat__messages');
    await expect(
      thread.locator(`[data-msg-id="${idsA[TOTAL - 1]}"]`),
    ).toBeInViewport({ timeout: 15_000 });

    const HOLD_MS = 8000;
    const isAnchorWindow = (url) =>
      url.pathname === '/api/chat' &&
      (url.searchParams.has('start_id') || url.searchParams.has('end_id'));
    await page.route(isAnchorWindow, async (route) => {
      await new Promise((r) => setTimeout(r, HOLD_MS));
      await route.continue();
    });
    // 前提量的是**請求真的在空中**，不是轉圈在不在：轉圈是另一條線（fix14 的
    // `initialLoading`）的產物，拿它當這一支的前提，會讓「取消壞掉」跟「轉圈壞掉」
    // 紅成同一個樣子。
    const anchorReqs = [];
    page.on('request', (r) => {
      const u = new URL(r.url());
      if (isAnchorWindow(u)) anchorReqs.push(u.href);
    });

    // 在**已經開著的**房間裡發動跳轉 —— 走訪開始飛。
    await page.evaluate(
      ([cid, mid]) => {
        window.location.hash = `#office/chat/${cid}/msg/${mid}`;
      },
      [A.id, idsA[TARGET_INDEX - 1]],
    );
    const spinner = page.locator('.chat__loading');
    await expect
      .poll(() => anchorReqs.length, { message: '前提：走訪真的在飛' })
      .toBeGreaterThanOrEqual(2);

    // 使用者手勢：點 B 的 roster row。
    await page.locator('.member-card', { hasText: NAME_B }).click();

    // ① B 的房間必須在 A 的走訪落地**之前**就填滿。
    await expect(
      thread.locator('.chat__msg'),
      'B 的房間被上一條對話的走訪鎖住了',
    ).toHaveCount(B_COUNT, { timeout: HOLD_MS - 2000 });
    await expect(thread.locator('.chat__msg').last()).toContainText(
      `B line ${B_COUNT} `,
    );
    await expect(spinner, 'B 載完了轉圈不准賴在畫面上').toHaveCount(0);

    // ② 🔴 等到 A 的走訪本來會落地的時間點之後 —— 它一列都不准 commit 到 B。
    await page.waitForTimeout(HOLD_MS + 2000);
    await expect(
      thread.locator('.chat__msg'),
      '被叫停的走訪把 A 的列灌進 B 的房間了',
    ).toHaveCount(B_COUNT);
    await expect(
      thread.locator(`[data-msg-id="${idsA[TARGET_INDEX - 1]}"]`),
      'A 的錨點不准落在 B 的房間裡',
    ).toHaveCount(0);
    await expect(
      thread.locator(`[data-msg-id="${idsA[TOTAL - 1]}"]`),
      'A 的活尾巴也不准落在 B 的房間裡',
    ).toHaveCount(0);
    await expect(spinner, '切走之後不准留下一顆轉不完的轉圈').toHaveCount(0);
  });
});
