// e2e_test/tests/08_unread_jump.spec.js
// B9 · unread badge → 進房 divider 錨定 → 進房 mark-read 歸零 → SSE 新訊息浮條
// (M2 batch 19, 31e4e96 + 1473ff1).
//
// The race this spec exists to cover (vitest can't): the FE snapshots
// `member.unreadCount` at conversation entry STRICTLY BEFORE the entry read
// receipt goes out — since 8cd4fff9 the LISTING marks nothing, but ChatArea
// fires POST /api/chat/mark-read as soon as the newest page lands on a focused
// window, and the roster refetches to 0 right after. The clearer changed; the
// ordering hazard did not. Only a real server + real HTTP ordering exercises it.
//
// ⚠ ordering is load-bearing throughout: every unread_count sample happens
// BEFORE anything lists M's thread. The spec hires its OWN member (M is never
// roster[0] — the seed Mira is — so the office auto-open never touches M's
// watermark before the badge assertion).
const { test, expect } = require('@playwright/test');
const {
  authHeaders,
  BASE,
  ownerToken,
  hireMember,
  mintMemberToken,
  postChatAs,
  markChatRead,
  unreadCountOf,
  bootAuthedSpa,
  uniqueName,
  PNG_400x300_B64,
} = require('../lib/fixtures');

const NAME_M = uniqueName('Unread M');
const NAME_DECOY = uniqueName('Unread Decoy');
const OLD_COUNT = 14; // read context — enough to overflow one screen
const NEW_COUNT = 5; // the unread tail

const PAD =
  '— padding line so the thread overflows one screen height and the entry position is a real scroll decision';

test.describe('B9 · unread — badge, entry divider anchor, 進房 mark-read, floating chip', () => {
  test('badge shows the server count; entering anchors at the divider; entering clears it; SSE chip on new inbound', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, NAME_M);
    // A second member with a NON-EMPTY thread: the entry into M's room below
    // deliberately happens FROM this thread — the stale-switch regression
    // (see the note at the hop).
    const decoy = await hireMember(request, token, NAME_DECOY);
    const tokM = await mintMemberToken(request, token, M.id, 1);
    // Seed the decoy's thread (owner → decoy; posting as the owner never
    // touches M's watermark).
    await postChatAs(request, token, decoy.id, `hello decoy ${PAD}`);

    // ── fixture: OLD read context (M → owner ×14, read up to the last of
    // them) + NEW unread tail (M → owner ×5).
    //
    // 🔴 THE READ IS REPORTED EXPLICITLY. It used to be produced by LISTING the
    // thread — `GET /api/chat?with=` advanced the watermark as a side effect —
    // and commit 8cd4fff9 removed that write from every path. A fixture still
    // built on the listing quietly leaves all 19 messages unread, and this
    // spec then fails 60 lines later on a count it never talks about.
    let lastOld;
    for (let i = 1; i <= OLD_COUNT; i++) {
      lastOld = await postChatAs(request, tokM, 'owner', `old read message ${i} ${PAD}`);
    }
    await markChatRead(request, token, M.id, lastOld.ts);
    const newMsgs = [];
    for (let i = 1; i <= NEW_COUNT; i++) {
      newMsgs.push(
        await postChatAs(request, tokM, 'owner', `NEW unread message ${i} ${PAD}`),
      );
    }
    const firstUnread = newMsgs[0];

    // ── API contract: unread_count == 5, sampled BEFORE any further list ──
    expect(
      await unreadCountOf(request, token, M.id),
      'the owner-perspective unread count must be exactly the new tail',
    ).toBe(NEW_COUNT);

    // ── browser: roster badge BEFORE entering the conversation ──
    await bootAuthedSpa(page, token);
    const card = page.locator('.member-card', { hasText: NAME_M });
    await expect(card).toBeVisible();
    // Precondition honesty: the office auto-opens roster[0]; if that were M,
    // its watermark would already be cleared and the badge trivially gone.
    await expect(
      card,
      'M must NOT be the auto-opened roster[0] (else the badge assertion is meaningless)',
    ).not.toHaveClass(/member-card--selected/);
    await expect(
      card.getByTestId('unread-badge'),
      'the roster badge must show the server-computed count',
    ).toHaveText(String(NEW_COUNT));

    // STALE-SWITCH REGRESSION (the old decoy workaround, inverted): ChatArea's
    // entry-positioning effect used to fire on a peer SWITCH while `messages`
    // was still the PREVIOUS peer's loaded thread (useChat cleared it one
    // commit later), latching the one-shot against stale data — switching from
    // a NON-EMPTY thread meant the divider never rendered. That frame is gone
    // since T-48 R13-5: OfficePage mounts ChatArea under `key={peerId}`, so
    // entering a room builds a fresh component whose one-shot has not been
    // spent. We still deliberately enter M's room FROM a settled NON-EMPTY
    // thread, because that is the path the defect lived on.
    await page.locator('.member-card', { hasText: NAME_DECOY }).click();
    await expect(
      page.locator('.chat__messages .chat__msg'),
      "the decoy's thread must be NON-empty (the stale-switch precondition)",
    ).not.toHaveCount(0);

    // ── enter the conversation: divider anchoring ──
    await card.click();
    const thread = page.locator('.chat__messages');
    await expect(thread).toBeVisible();
    const divider = thread.locator('.chat__unread-divider');
    await expect(divider, 'the unread divider must render').toBeVisible();
    await expect(divider).toContainText('以下為尚未閱讀的訊息');

    // The divider sits immediately ABOVE the FIRST unread message (the 5th-
    // from-last peer message — id known from the API fixture).
    const anchorId = await divider.evaluate(
      (el) => el.nextElementSibling?.getAttribute('data-msg-id') ?? '',
    );
    expect(
      anchorId,
      'the divider must anchor exactly at the first unread message',
    ).toBe(firstUnread.id);

    // Entry position: NOT at the bottom (the unread tail continues below)…
    const metrics = await thread.evaluate((el) => ({
      scrollTop: el.scrollTop,
      clientHeight: el.clientHeight,
      scrollHeight: el.scrollHeight,
    }));
    expect(
      metrics.scrollHeight,
      'the thread must overflow for entry positioning to be meaningful',
    ).toBeGreaterThan(metrics.clientHeight + 1);
    expect(
      metrics.scrollHeight - (metrics.scrollTop + metrics.clientHeight),
      'entry must land at the divider, not the bottom',
    ).toBeGreaterThan(4);
    // …and the DIVIDER ITSELF is what sits flush with the top of the thread
    // viewport. ChatArea does `divider?.scrollIntoView({ block: "start" })` on
    // purpose: leaving read context above it can push the first unread row out
    // of a compact viewport, which is the opposite of what entry positioning is
    // for. The earlier shape of this spec demanded a visible already-read
    // message above the divider (batch 19, LINE ref); owner ruled on 2026-08-05
    // at rc-8687b78cdbbb, option ①: the current screen is the contract — the
    // divider pins to the top — so the assertion is inverted here rather than
    // the product being changed back.
    const dividerOffset = await divider.evaluate((el) => {
      const box = el.closest('.chat__messages');
      if (!box) return null;
      return el.getBoundingClientRect().top - box.getBoundingClientRect().top;
    });
    expect(dividerOffset, 'the divider must be measurable inside the thread box').not.toBeNull();
    expect(
      Math.abs(dividerOffset),
      `the divider's top must sit flush with the thread's top, got ${dividerOffset}px off`,
    ).toBeLessThanOrEqual(2);

    // ── read convergence: entering the room IS reading. It is the COCKPIT
    // that reports it now (ChatArea's entry read receipt → POST
    // /api/chat/mark-read), not the listing — see the fixture note above. ──
    await expect
      .poll(async () => unreadCountOf(request, token, M.id), {
        message: 'the unread count must converge to 0 after the room lists the thread',
      })
      .toBe(0);
    await expect(
      card.getByTestId('unread-badge'),
      'the roster badge must be gone once read',
    ).toHaveCount(0);

    // ── T-48 新訊息預覽列: owner scrolled up + new inbound via SSE. The
    // 「有新訊息」 pill this replaces said one fixed sentence; the strip names
    // the sender and quotes the line, and clicking it lands on the LATEST
    // message rather than the first unseen one.
    await thread.evaluate((el) => {
      el.scrollTop = 0;
    });
    // ── T-48 ①: 捲上去，什麼都還沒發生 —— 圓形箭頭就該在了。owner 的條件是
    // 「最新那一則不在視窗內」（rc-72054864ff88），不是「有新訊息」。退場的
    // 「有新訊息」藥丸用的是後者，所以一個往回讀歷史的人在別人開口之前沒有任何
    // 路可以回到底部。這一條在真 server、真瀏覽器上釘住那個差別。
    await expect(
      page.getByTestId('chat-jump-latest'),
      'scrolling up alone must raise the arrow — no arrival required',
    ).toBeVisible({ timeout: 10_000 });
    await expect(
      page.getByTestId('chat-new-msg-preview'),
      'nothing arrived, so there is nothing to preview',
    ).toBeHidden();

    // 🔴 窄視窗,而且這不是「順便也測一下手機」:這是位移最大的那個寬度。
    //
    // 這一段量的是「上方的內容晚長高之後,最新那一列還在不在視窗裡」,而同一份
    // 燃料在窄視窗上推得更遠 —— 本輪 mutant 實測(請示卡晚填):390 寬 gap
    // 216.28px、卡片 51→325px;1280 寬 gap 195.77px、卡片 49→303px。
    //
    // ⚠️ 更正一句本檔自己寫過的話。這裡原本說「1280 上瀏覽器的 scroll anchoring
    // 會補 scrollTop、箭頭因此回來,390 上不會」。本輪在真瀏覽器上兩個寬度都量了,
    // 兩個寬度的箭頭**都**回來了(arrowBack=true,st 1425→1471 / 2436→2482)。
    // 那句話是這條護欄下面那組斷言曾經寫成選言的理由,而它是錯的 —— 見那裡。
    await page.setViewportSize({ width: 390, height: 844 });

    // ── 🔴 G-1 護欄的燃料:**兩種**晚長高的內容,都落在最新那一列的上方。
    //
    // 這個 fixture 在 T-48 之前全是純文字,所以下面那組斷言(最新那一列貼齊底部)
    // 是**碰巧**綠的 —— 沒有任何東西會在落地之後改變版面,所以它守不住任何事。
    // 兩個落點修正迴圈刪掉之後(owner rc-6c27f486ef9d 「拿掉。圖片／卡片展開把
    // 目標擠走我接受」),「上方晚載入的內容把最新那一列推到摺線下」變成一條真的
    // 會發生的路。owner 點名的兩種內容,今天都已經**在源頭關掉**了:
    //
    //   ① 圖片 —— `.chat__msg-image` 有固定的 220px 框(commit aea7182),
    //      那一列在 bytes 到之前就是最終高度。
    //   ② 請示卡 —— 一律**收合成一列**(T-48,owner 2026-09-04 `c-6f054c1cb481`),
    //      那一列要顯示的東西訊息本身就帶著,所以它**一次 GET 都不發**,也就沒有
    //      任何東西可以晚到。這比原本的做法(先 await 撈回來再上畫面)更強:不是
    //      「等到了」,是「沒有東西要等」。
    //
    // 🔴 **所以這個 fixture 今天沒有燃料了,這件事必須講在前面。** 兩種被 owner
    // 點名的位移來源都在源頭關掉之後,護欄 ②(落點待得住)在這個 fixture 上是
    // 一條**沒有燃料的斷言** —— 它仍然會在有人把任一個源頭修法拿掉時變紅(兩顆
    // mutant 都驗過),但它不再由「這一輪真的有東西在動」撐著。要補的是另一種
    // 今天還會晚到的內容(markdown 重排實測 0px、卡片與圖片都關了),還沒有人
    // 找到,列為已知缺口。
    //
    // 兩種內容都被**扣住到落地之後**才放行:圖片扣在
    // `/api/chat/attachment/**`,請示卡扣在 `/api/reply-cards/rc-*` —— 後者今天
    // 是一個**陰性對照**:那條路今天一次都不會被走到,所以那一列的高度不會變。
    // 視窗**下方**長高對讀者是 0px 位移(實測),推不動任何東西,所以兩者都在上方。
    let releaseImages;

    const imagesHeld = new Promise((r) => {
      releaseImages = r;
    });
    await page.route('**/api/chat/attachment/**', async (route) => {
      await imagesHeld;
      await route.continue();
    });
    for (let i = 1; i <= 3; i++) {
      await postChatAs(request, tokM, 'owner', `image above the target ${i}`, [
        { data_b64: PNG_400x300_B64, filename: `above-${i}.png`, mime: 'image/png' },
      ]);
    }

    // 請示卡的 GET 被延遲 CARD_FETCH_DELAY_MS —— 這條路今天**一次都不該被走到**。
    //
    //   · 今天的碼:卡片收合 ⇒ 那一列從落地到靜止,高度一格都不動。
    //   · 把「一律收合」拿掉的 mutant:訊息立刻上畫面、卡片先畫成 49px 的空殼,
    //     GET 在 600ms 後才回來 —— 也就是**跳轉之後**才長高。歷史實測 strip
    //     199ms、按下 221ms、卡片 49→303px、最新那一列被推走 195.77px。
    //
    // ⚠️ 那個 221ms 是**一次本機量測**(獨立審查 F-H)。CI 上一台慢的 runner 把
    // 「落地 → 按下」拖過 600ms,延遲就會在按下之前先回來 ⇒ **mutant 示範**會
    // 假綠。它削弱的是示範,不是出貨的護欄:列高那一格不看這個
    // 延遲的死線,它問的是那一列的高度有沒有變過。
    // 只攔 `rc-*`:`/api/reply-cards/count`(等我回覆徽章的輪詢)不在其中。
    const CARD_FETCH_DELAY_MS = 600;
    await page.route('**/api/reply-cards/rc-*', async (route) => {
      await new Promise((r) => setTimeout(r, CARD_FETCH_DELAY_MS));
      await route.continue();
    });
    // 一張 waiting 卡 —— 今天它是**陰性對照**,不是燃料:每一種狀態的卡都收合成
    // 一列、都不發 getReplyCard。挑 waiting 是因為它是最後一種還會長高的,拿它
    // 當對照才問得到「連它都不動了嗎」。
    // 伺服器會自己把一則同串的伴生訊息貼進來,所以它就落在最新那一列的上方。
    const cardRes = await request.post(`${BASE}/api/reply-cards`, {
      headers: authHeaders(tokM),
      data: {
        linked_task: null,
        kind: 'decision',
        summary: '要不要把這批貨改走空運',
        body: '海運艙位滿了,改空運的話成本會多兩成,但趕得上客戶的檔期。',
        options: [
          { text: '維持海運', ai_pick: false },
          { text: '改走空運', ai_pick: true },
          { text: '先問客戶', ai_pick: false },
        ],
      },
    });
    expect(cardRes.status(), 'seeding the waiting reply card must succeed').toBe(200);
    expect(
      (await cardRes.json()).status,
      'the control must really be a WAITING card — the last kind that used to grow',
    ).toBe('waiting');

    const lateBody = `late-breaking message ${PAD}`;
    await postChatAs(request, tokM, 'owner', lateBody);
    const strip = page.getByTestId('chat-new-msg-preview');
    await expect(strip, 'the preview strip must appear (SSE-pushed inbound)').toBeVisible({
      timeout: 15_000,
    });
    await expect(strip).toContainText(lateBody);
    // Mutually exclusive with the round jump-to-latest arrow.
    await expect(
      page.getByTestId('chat-jump-latest'),
      'the arrow must give way to the strip',
    ).toBeHidden();
    await page.getByTestId('chat-new-msg-jump').click();
    // 落地當下就取樣,不等下面那些輪詢 —— 這一格問的是「這一列被畫出來的時候,
    // 卡片已經是最終高度了嗎」,晚一點問就會問到晚填之後的高度。
    // (圖片這時還被扣著,一個 byte 都沒放行 —— 所以這一格的縮圖高度就是
    // 「還不知道圖長什麼樣子」時的高度。)
    const landed = await thread.evaluate((el) => {
      const c = el.querySelector('[data-testid="chat-reply-card"]');
      return {
        cardHeight: c ? Math.round(c.getBoundingClientRect().height) : null,
        imageHeights: [...el.querySelectorAll('img.chat__msg-image')].map((i) =>
          Math.round(i.getBoundingClientRect().height),
        ),
      };
    });
    // The jump lands on the latest message ⇒ the strip is consumed and the
    // arrow stays away (nothing is off screen any more).
    await expect(strip, 'reaching the latest message must dismiss the strip').toBeHidden({
      timeout: 10_000,
    });
    await expect(page.getByTestId('chat-jump-latest')).toBeHidden();

    // 🔴 而且要**待得住**,而「待得住」在有東西還在載入的時候不等於「不會動」。
    //
    // 上面那兩行是輪詢的:只要在某一格取樣到「不在」就 PASS,所以在一個「箭頭消失
    // 10–40ms 又長回來」的產品上,它有時候會綠 —— 實測 30 次跑紅 16 次。所以這裡
    // 讓版面完全靜止之後再問一次。
    //
    // 🔴 這裡曾經寫成一條選言 ——「gap <= 1 **或**箭頭在畫面上」—— 而本輪用兩個
    // mutant 在真瀏覽器上證明那條選言**兩個 regression 都攔不到**:
    //
    //   · 拿掉 `.chat__msg-image` 的 `height: 220px`,跑**當時那個只有圖片的
    //     fixture**:圖片把最新那一列推走 178.17px,箭頭回來了 → 選言綠。
    //   · 讓待回覆卡回到「掛載就展開」(T-48 之前的樣子),跑**現在這個
    //     fixture**:請示卡 51→325px 晚長高、把最新那一列推走 215.78px
    //     (1280 寬:49→303、195.77px),
    //     箭頭同樣回來了 → 選言綠。
    //
    // 原因是機械性的:上方的內容長高時瀏覽器的 scroll anchoring 會補 `scrollTop`,
    // 一補就發 scroll 事件,而 `onMessagesScroll` 是 `latestInView` 的寫入點之一。
    // 所以第二個選言幾乎必然成立 —— 它是一個永遠開著的出口,而一條永遠有出口的
    // 斷言不是護欄。(本檔上面原本寫「390 上箭頭不會回來」,那句話是選言當初的
    // 理由,而它被這一輪的實測推翻了。)
    //
    // ⇒ 選言拿掉,改成直說**今天真正的合約**。owner 在 rc-6c27f486ef9d 簽的是
    // 「拿掉那兩個落點修正迴圈,位移我接受」,而他點名的兩個位移來源後來都在
    // **源頭**關掉了(固定 220px 框 + 請示卡一律收合、不發請求)—— 所以這兩種的位移
    // 今天是 0,而那正是可以被斷言、也會在修正被拿掉時變紅的東西。
    //
    // 「介面不可以無聲說謊」那一半沒有不見:它在本測試上面那條自己被測 ——
    // 捲上去、什麼都還沒到,箭頭就必須在(`scrolling up alone must raise the
    // arrow`)。選言把兩件事綁在一起,拆開之後兩件事各自都有牙齒。
    releaseImages();
    await page.waitForTimeout(3000);
    const settled = await thread.evaluate((el) => {
      const rows = el.querySelectorAll('[data-msg-id]');
      const r = rows[rows.length - 1].getBoundingClientRect();
      const imgs = [...el.querySelectorAll('img.chat__msg-image')];
      const c = el.querySelector('[data-testid="chat-reply-card"]');
      return {
        distance: Math.round(el.scrollHeight - el.scrollTop - el.clientHeight),
        lastRowBottomGap: Number((r.bottom - el.getBoundingClientRect().bottom).toFixed(2)),
        imagesDecoded: imgs.filter((i) => i.naturalHeight > 0).length,
        imageHeights: imgs.map((i) => Math.round(i.getBoundingClientRect().height)),
        cardHeight: c ? Math.round(c.getBoundingClientRect().height) : null,
        cardCollapsed: !!c?.classList.contains('reply-card--collapsed'),
      };
    });
    // 前提誠實 ①:圖片真的解碼了。沒有這一行,下面整段可能只是「圖沒載到,所以
    // 沒有任何東西動過」的空綠。
    expect(
      settled.imagesDecoded,
      `三張圖必須真的解碼完成,否則這條護欄什麼都沒測到(量到 ${JSON.stringify(settled)})`,
    ).toBe(3);
    // 護欄 ⓪:縮圖的高度不由圖片決定 —— bytes 放行前後一模一樣。
    //
    // 🔴 這條是**直接**問固定框在不在,而不是繞道去問位移,因為繞道那條路實測是
    // 通不了的:把 `.chat__msg-image` 的 `height: 220px` 拿掉、其他不動,這個
    // fixture 量到縮圖 [2,2,2]→[120,120,120] 卻 `lastRowBottomGap` −11.14、
    // `distance` 0 —— 版面整個跟著黏在底部,位移是 0,下面護欄 ② 照樣綠(跑三次
    // 都一樣)。圖片在這個落點上今天**不是燃料**;燃料是那張請示卡。所以固定框
    // 這件事在這裡自己被問一次,問的是它本來的定義:那一列在 bytes 到之前就是
    // 最終高度。
    expect(
      landed.imageHeights,
      `縮圖在 bytes 放行之前就必須是最終高度(固定 220px 框,office.css ` +
        `\`.chat__msg-image\`)—— 扣住時量到 ${JSON.stringify(landed.imageHeights)}、` +
        `放行後 ${JSON.stringify(settled.imageHeights)},不一樣就表示高度又回去讓圖片決定了`,
    ).toEqual(settled.imageHeights);
    expect(
      landed.imageHeights.length,
      'the three held thumbnails must be on screen at landing',
    ).toBe(3);
    // ⚠️ 這裡曾經斷言「那通 GET 一次都沒發出去」,已拿掉(owner `c-a86165fb9f0d`
    // 點名)。它違反的是〈測試撰寫規則〉**第 4 條**而不是第 3 條:寫在它旁邊的
    // 理由是「以後就算有人改成照樣去撈、再吸收位移也會被擋下來」——守的是「以後
    // 的人不准怎麼做」,不是「這個功能必須做到什麼」。
    //
    // 🔴 **少守了什麼(第 4 條最後一句要求寫明)**:今天沒有任何東西釘住「收合的
    // 卡一通請求都不發」。有人改成一次把所有卡都撈回來、再用別的方法把位移吸收
    // 掉的話,這裡兩條照樣綠 —— 畫面是對的,流量不是。代價是長對話上每張卡一通
    // GET,不是看得見的缺陷,所以它是一句註記而不是一條斷言。
    //
    // 陰性對照:那一列從落地到靜止,高度一格都沒動。
    expect(
      landed.cardHeight,
      `請示卡那一列在落地當下(${landed.cardHeight}px)與版面靜止後` +
        `(${settled.cardHeight}px)必須一模一樣 —— 不一樣就表示有東西晚到並把` +
        `下面的內容推走了`,
    ).toBe(settled.cardHeight);
    // …而且它真的是收合的那一列,不是「卡片根本沒出現」的空綠。
    expect(
      settled.cardCollapsed,
      `畫面上必須真的有一列收合的請示卡(量到 ${JSON.stringify(settled)})`,
    ).toBe(true);
    // 護欄 ②:所以落點待得住。無條件,沒有箭頭那個出口。
    //
    // ⚠️ 而它是**單邊的**,這件事要寫下來(獨立審查 F-H)。`lastRowBottomGap` 是
    // 「最新那一列的下緣 − 視窗下緣」,所以它只擋一個方向:那一列被推到**摺線
    // 底下**。被留在摺線**上方**任意遠 —— gap 是個很負的數 —— 照樣 <= 1,照樣綠。
    // 補上另一邊的是護欄 ③(人不在最新訊息上時,箭頭就會在)。兩條各自有洞,
    // 合起來才是完整的:②(下方)＋ ③(上方)。拆掉任何一條,另一條都補不上。
    expect(
      settled.lastRowBottomGap,
      `版面靜止之後,最新那一列必須還完整在視窗裡 —— 上方的圖片與請示卡在源頭` +
        `都已經是最終高度,推不動它(量到 ${JSON.stringify(settled)})`,
    ).toBeLessThanOrEqual(1);
    // 護欄 ③:人在最新訊息上,所以那顆「回到最新」箭頭在這一刻是**假話**,不可以在。
    //
    // ⚠️ 這是一條**輪詢**斷言,而本檔上面才剛警告過輪詢(「只要在某一格取樣到
    // 『不在』就 PASS」,箭頭閃 10–40ms 的產品 30 次跑綠 14 次)。方向相反 —— 上面
    // 怕的是「不在」被取樣到,這裡怕的是「在」被錯過 —— 但形狀一樣,所以理由必須
    // 寫在旁邊而不是靠讀者自己推:上面那個 3 秒的靜止(`waitForTimeout` +
    // `settled` 取樣)已經先跑完了,版面到這裡不再變動,所以這條輪詢問的是一個
    // **靜止**的狀態,不是一個會閃的狀態。誰把那段靜止拿掉,這條就退化成上面警告
    // 的那個形狀。
    await expect(
      page.getByTestId('chat-jump-latest'),
      'the reader is on the latest message — the arrow would be a lie',
    ).toBeHidden();
  });
});
