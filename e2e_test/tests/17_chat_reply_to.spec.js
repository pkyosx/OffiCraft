// e2e_test/tests/17_chat_reply_to.spec.js
// T-4e95 · 回覆這則 — the whole spine in ONE real browser against ONE real server:
//   點回覆 → 橫幅指名對象 → 送出 → 對方那一列出現引用列 → 點引用列把原訊息撈回來
//   在放大閱讀的覆蓋層裡看全文（owner 2026-08-21 改設計：不再捲動）。
//
// WHY THIS EXISTS AND WHAT ONLY IT CAN SEE
// Every jsdom test in this feature stops at a seam. ChatArea's tests mock
// `useChat`, so the hook's third argument is invisible to them; useChat's tests
// mock `api`, so the wire body is invisible to them; http.mutations mocks
// `fetch`, so the SERVER is invisible to it; the Go conformance suite drives
// HTTP from Python, so the BROWSER is invisible to it. Nothing in the tree joins
// them. r16 measured the consequence: deleting `replyTo` from useChat's postChat
// call, and separately forcing `reply_to: ""` in http.ts, each left the whole
// 2258-test frontend suite green while 「回覆這則」 was dead in the real app.
const { test, expect } = require('@playwright/test');
const {
  BASE,
  authHeaders,
  ownerToken,
  hireMember,
  mintMemberToken,
  postChatAs,
  markChatRead,
  blockWebFonts,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

const NAME_M = uniqueName('Reply M');
const TARGET = 'the sentence that gets quoted back';
const ANSWER = 'answering that one';
// ⚠️ NOTHING IN THIS FILE ASSERTS THAT THE THREAD STAYS PUT. The old
// `not.toBeInViewport()` / `toBeInViewport()` pair was deleted along with the
// scrolling and nothing was written to replace it, so re-introducing a scroll
// would not turn this spec red.
//
// It stays comfortably INSIDE the client's page size (useChat loads
// CHAT_PAGE_SIZE = 30 and only grows backwards when the owner scrolls up, which
// this test never does): 1 target + 24 filler + 1 reply = 26 rows, so the target
// is LOADED — which is what the first test below needs, since it hovers the
// target row and presses 回覆這則.
//
// The SECOND test needs the opposite and says so with its own number.
const FILLER = 24;
// 🔴 FAR MORE THAN THE PAGE SIZE, ON PURPOSE. Until 2026-08-21 this spec had a
// comment warning that pushing the filler past 28 would drop the target out of
// the loaded window and break the quote assertion — because the client resolved
// the quote by looking the id up in what it had loaded, and fell back to a
// second HTTP read that could fail. The owner replaced that design: the server
// now ships the quoted message WITH every reply, on every read.
//
// So the case the old spec was carefully avoiding is now the case worth testing,
// and it is also the COMMON one — the owner's replies almost always reach far
// back. 200 rows guarantees the target is nowhere near the window.
const FAR_FILLER = 200;

// 🔴 THE OWNER'S DISPLAY NAME IS A FIXTURE HERE, NOT A CONSTANT. Since T-4e95 a
// quote names 「寄件者 → 收件者」, and when the owner is the recipient the name
// printed is the one HE set (`/api/settings owner_name`), falling back to the
// theme's default word for the human. Writing 「CEO（你）」 into an expectation
// would pin the FALLBACK — a string that lives in `src/i18n/locales/zh.ts` and
// belongs to the theme, not to this feature — and would go red the day anyone
// renames a theme's word for the human, for a reason that has nothing to do with
// reply-to. So the test sets the nickname it is going to assert, which also
// exercises the other half of T-4e95 (the cockpit calls the owner by the name he
// set, not by the theme's default) in the same string.
const OWNER_NICK = uniqueName('Owner');

test.describe('T-4e95 · reply-to — banner, wire, quote row, jump', () => {
  // The nickname is server state on a station every spec in this suite shares.
  // Put it back the way it was found — an untouched studio has no owner_name,
  // and a later spec that renders the owner must not inherit this one's fixture.
  // Unconditional and in an `afterEach`, so a spec that fails half-way still
  // cleans up.
  test.afterEach(async ({ request }) => {
    const token = await ownerToken(request);
    await request.patch(`${BASE}/api/settings`, {
      headers: authHeaders(token),
      data: { owner_name: '' },
    });
  });

  test('reply to a message: the banner names the sender, the send carries the link, the reply shows a quote row, and it opens the original in full', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, NAME_M);
    const tokM = await mintMemberToken(request, token, M.id, 1);
    // Set BEFORE the SPA boots: <App> resolves the nickname once on mount and
    // hands it down, so a value written after the boot would not be the one the
    // banner is drawn with.
    const nickRes = await request.patch(`${BASE}/api/settings`, {
      headers: authHeaders(token),
      data: { owner_name: OWNER_NICK },
    });
    expect(nickRes.status(), await nickRes.text()).toBe(200);
    expect(
      (await nickRes.json()).owner_name,
      'the server must have stored the nickname this test is about to assert',
    ).toBe(OWNER_NICK);

    // The message that will be replied TO comes from the member, so the banner
    // has a name to print that is NOT the owner's — a banner that printed the
    // wrong one of the two people is the r14 bug, and only a real name catches it.
    const targetMsg = await postChatAs(request, tokM, 'owner', TARGET);
    for (let i = 1; i <= FILLER; i++) {
      await postChatAs(request, tokM, 'owner', `filler ${i}`);
    }

    await bootAuthedSpa(page, token);
    await page.locator('.member-card', { hasText: NAME_M }).click();
    const thread = page.locator('.chat__messages');
    // 🔴 BY ID, NOT BY TEXT. `hasText: TARGET` matches TWO rows once the reply
    // exists — the original, and the reply itself, whose quote row carries the
    // original's words INSIDE its own `.chat__msg`. `.first()` happened to land
    // on the original (a reply's ts is necessarily later than what it quotes and
    // the thread is ordered by ts), but that is a property of the data, not
    // something this locator says, and an earlier comment here claimed the count
    // "can only ever be 0 or 1", which was simply false. The id is unambiguous
    // and it is what the second test already uses.
    const target = thread.locator(`[data-msg-id="${targetMsg.id}"]`);
    await expect(target).toHaveCount(1);
    await expect(target).toBeVisible();

    // ── 點回覆 (the entry is hover-revealed but always occupies layout)
    await target.scrollIntoViewIfNeeded();
    await target.hover();
    await target.getByRole('button', { name: '回覆這則' }).click();

    // ── 橫幅 names the real sender, not a coin flip between the two people —
    // AND says who he said it to. Both halves, one equality: 「寄件者 →
    // 收件者」 is the shape T-4e95 settled on and a partial match (toContainText)
    // would pass on a banner that had lost the arrow, lost the addressee, or put
    // the two people the wrong way round. Every character on the right-hand side
    // is built from this test's own fixtures — the member's unique name and the
    // nickname it wrote into `/api/settings` above — so nothing here is a copy
    // of a product string.
    const banner = page.getByTestId('chat-reply-banner');
    await expect(banner).toBeVisible();
    await expect(banner.locator('.chat__reply-banner__who')).toHaveText(
      `正在回覆 ${NAME_M} → ${OWNER_NICK}`,
    );
    await expect(banner.locator('.chat__reply-banner__body')).toContainText(
      TARGET.slice(0, 12),
    );

    // ── 送出
    const composer = page.locator('.chat__composer-row textarea');
    await composer.fill(ANSWER);
    await composer.press('Enter');
    await expect(banner).toHaveCount(0);

    // ── THE WIRE ACTUALLY CARRIED THE LINK. Read it back from the SERVER, not
    // from the DOM: this is the assertion that dies when `replyTo` is dropped
    // anywhere between the composer and the POST body.
    //
    // 🔴 THIS READ MUST POLL — it used to be a single zero-retry GET, and that
    // was a textbook write-then-read race. The only gate between the composer
    // and this line is `await expect(banner).toHaveCount(0)`, and the banner
    // going away proves NOTHING about the POST: ChatArea's `submit()` runs
    // `setReplyToId(null)` BEFORE it awaits `send(...)` — a deliberate
    // optimistic reset whose own comment says so. So when the banner hits 0 the
    // POST is still in flight. CI went red here twice (latest 2026-08-25). The
    // product is fine; the test was reading too early. Poll until the write has
    // landed, then assert on the settled rows.
    let rows = [];
    await expect
      .poll(
        async () => {
          const res = await request.get(`${BASE}/api/chat?with=${M.id}&limit=100`, {
            headers: authHeaders(token),
          });
          rows = (await res.json()).messages;
          return rows.some((m) => m.body === ANSWER);
        },
        { message: 'the reply must have been stored', timeout: 15_000 },
      )
      .toBe(true);
    const original = rows.find((m) => m.body === TARGET);
    const reply = rows.find((m) => m.body === ANSWER);
    expect(original, 'the quoted message must exist').toBeTruthy();
    expect(reply, 'the reply must have been stored').toBeTruthy();
    expect(
      reply.reply_to,
      'the stored reply must point at the message the composer was aimed at',
    ).toBe(original.id);

    // ── 引用列 on the reply's own row, carrying the quoted text.
    const replyRow = thread.locator(`[data-msg-id="${reply.id}"]`);
    const quote = replyRow.getByTestId('msg-quote');
    await expect(quote).toBeVisible();
    await expect(quote).toContainText(TARGET.slice(0, 12));
    // …and NOT the miss sentence. 🔴 THE STRING HERE MUST BE ONE THE PRODUCT CAN
    // ACTUALLY PRINT ON THIS ROW, or this line is a tautology defended by a
    // comment. It was 「較早的一則訊息」 for one round, after the row's copy had
    // already been changed away from it — the phrase existed nowhere else in the
    // repo, so the assertion could never go red. The quote ROW's miss sentence is
    // `chat.replyQuoteGone`; 「較早的一則訊息」 now belongs to the composer BANNER
    // (`chat.replyingToEarlier`), which is a different element and a different
    // claim. Same string as the second test below, on purpose.
    await expect(quote).not.toContainText('這則訊息已不存在');

    // ── 看原訊息
    //
    // 🔴 THIS USED TO BE A SCROLL, AND IT IS NOT ONE ANY MORE (owner ruling
    // 2026-08-21: 「全部統一就撈那一則顯示出來就好」). The control reads that one
    // message back from the server and opens it in the shared full-view overlay.
    const jump = quote.getByTestId('msg-quote-jump');
    await jump.focus();
    await jump.click();

    const overlay = page.locator('.md-preview');
    await expect(overlay).toBeVisible();
    await expect(overlay.locator('.md-preview__title')).toContainText(NAME_M);
    await expect(overlay).toContainText(TARGET);

    // 🔴 FOCUS FOLLOWS THE DIALOG, BOTH WAYS. `aria-modal` is a promise, not a
    // behaviour: before this shipped, opening the overlay left focus on the
    // button OUTSIDE the portal, so a keyboard user who pressed 看原訊息 got a
    // dialog they were not in and a Tab that walked the page behind it. Measured
    // in this browser, not asserted from the source.
    expect(
      await page.evaluate(() =>
        Boolean(document.activeElement?.closest('.md-preview')),
      ),
      'focus must land inside the overlay',
    ).toBe(true);

    // Esc closes it and focus comes back to the control that opened it — not to
    // <body>, which would restart the next Tab at the top of the cockpit.
    await page.keyboard.press('Escape');
    await expect(overlay).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.activeElement?.getAttribute('data-testid') ?? null,
      ),
      'closing must hand focus back to the button that opened it',
    ).toBe('msg-quote-jump');
  });

  // ── the case the old spec deliberately avoided ────────────────────────────
  //
  // 🔴 THIS IS THE ONE THAT WOULD HAVE BEEN RED BEFORE 2026-08-21 FOR A REASON
  // THAT WAS NOT A BUG, and is the reason the design changed. The quoted message
  // is 200 rows above the loaded window, so under the old shape the browser had
  // to go and fetch it: a request that could fail, a placeholder drawn while it
  // had failed, and a repair on the next inbound event. Now the quote arrives
  // attached to the reply and there is nothing to fetch.
  //
  // Adapted from the R20-B review probe `90_t4e95_r20b.spec.js`, which drove
  // exactly this setup to demonstrate the blip→lie→event→heal cycle. That cycle
  // does not exist any more, so the probe is not ported: what is kept is its
  // SETUP (push the target far out of the window in a real browser against a
  // real server) and its INSTRUMENT (route interception counting requests),
  // pointed at the opposite claim — the row is correct AND the browser asked for
  // nothing.
  test('a quote whose original is far outside the loaded window still renders — and costs no extra request', async ({
    page,
  }) => {
    // 200 sequential seed posts against a real server, then a full SPA boot.
    // The default 30s budget is not enough and a timeout here would read as a
    // product failure rather than as a slow fixture.
    test.setTimeout(180000);
    const request = page.request;
    const token = await ownerToken(request);
    const NAME_FAR = uniqueName('Reply Far');
    const M = await hireMember(request, token, NAME_FAR);
    const tokM = await mintMemberToken(request, token, M.id, 1);

    const FAR_TARGET = 'the sentence 200 rows above the window';
    const original = await postChatAs(request, tokM, 'owner', FAR_TARGET);
    for (let i = 1; i <= FAR_FILLER; i++) {
      await postChatAs(request, tokM, 'owner', `far filler ${i}`);
    }
    // Posted through the API with reply_to — the composer half of the spine is
    // covered by the test above; this one is about what a READ carries.
    const FAR_ANSWER = 'answering the one far above';
    const posted = await request.post(`${BASE}/api/chat`, {
      headers: authHeaders(tokM),
      data: { to: 'owner', body: FAR_ANSWER, reply_to: original.id },
    });
    expect(posted.status(), await posted.text()).toBe(200);
    // ⚠️ Read up to the answer first: an unread room opens AT its first unread
    // message, which here is the original itself — and then it is loaded.
    await markChatRead(request, token, M.id, (await posted.json()).ts);

    // 🔴 THE INSTRUMENT. Every by-ids read is counted BEFORE the SPA boots, so a
    // read fired during the first paint cannot slip past. Nothing is blocked —
    // blocking would prove the row renders without the answer, which is a weaker
    // claim than proving nothing asked.
    //
    // ⚠️ COUNTED IN THE PAGE, NOT IN `page.route`. This used to intercept with a
    // route handler that did `byIdsCalls += 1; route.continue()`, which is
    // adequate for asserting ZERO and is NOT adequate for asserting ONE:
    // measured on this harness, a single click was counted twice on some runs
    // and once on others, because the continued request can re-enter the
    // handler. A guard whose whole job is "exactly one" cannot be built on a
    // counter that sometimes says two — it would have been dismissed as flake
    // and deleted. Wrapping `fetch` in an init script counts the calls the
    // application actually makes, once each, and runs before any page script on
    // every navigation.
    await page.addInitScript(() => {
      window.__ocByIds = 0;
      const orig = window.fetch;
      window.fetch = function (input, ...rest) {
        try {
          const raw = typeof input === 'string' ? input : input.url;
          const u = new URL(raw, location.href);
          if (
            u.pathname === '/api/chat' &&
            u.searchParams.getAll('ids').length > 0
          ) {
            window.__ocByIds += 1;
          }
        } catch {
          /* a request whose url will not parse is not a by-ids read */
        }
        return orig.call(this, input, ...rest);
      };
    });
    const byIds = () => page.evaluate(() => window.__ocByIds);

    await bootAuthedSpa(page, token);
    await page.locator('.member-card', { hasText: NAME_FAR }).click();
    const thread = page.locator('.chat__messages');
    const replyRow = thread.locator('.chat__msg', { hasText: FAR_ANSWER }).first();
    const quote = replyRow.getByTestId('msg-quote');
    await expect(quote).toBeVisible();

    // PRECONDITION, asserted rather than assumed: the quoted row really is NOT
    // in the loaded window.
    //
    // 🔴 THE OLD WITNESS FOR THIS WAS THE ABSENCE OF THE JUMP CONTROL, AND IT IS
    // GONE. The control used to be offered only for a target the client already
    // held, so `toHaveCount(0)` doubled as proof the target was out of the
    // window. Since 2026-08-21 it is offered for EVERY reply — it reads the one
    // message back rather than scrolling to it — so its presence says nothing
    // about the window and its absence would now be a defect. Asserted the other
    // way round below, and the window fact is carried by the id check that
    // follows.
    await expect(replyRow.getByTestId('msg-quote-jump')).toHaveCount(1);
    // …and by id, which is the unambiguous half. NOT `hasText: FAR_TARGET`:
    // measured, that matches ONE element — the reply itself, because the quote
    // row now carries the original's words INSIDE the reply's own `.chat__msg`.
    // That is the feature working, and it made the text-based precondition read
    // as "the original is on screen" and fail. Ask for the row's identity.
    await expect(
      thread.locator(`[data-msg-id="${original.id}"]`),
    ).toHaveCount(0);

    // ① THE ROW IS CORRECT ANYWAY — the whole point of the redesign.
    await expect(quote).toContainText(FAR_TARGET.slice(0, 20));
    await expect(quote).not.toContainText('這則訊息已不存在');

    // ② …AND THE BROWSER ASKED FOR NOTHING. Sit through a real event too: a new
    // message arrives, the thread refetches and repaints, and the count still
    // does not move. The deleted design's debt collector fired exactly here.
    await postChatAs(request, tokM, 'owner', 'a new sentence that wakes the stream');
    await expect(thread).toContainText('a new sentence that wakes the stream', {
      timeout: 20000,
    });
    await expect(quote).toContainText(FAR_TARGET.slice(0, 20));
    expect(
      await byIds(),
      'the quote must arrive with the reply — nothing may fetch it in the background',
    ).toBe(0);

    // ── ③ 🔴 ONE CLICK, ONE REQUEST — IN A REAL BROWSER ────────────────────
    //
    // This is the same promise `ChatArea.quote-no-fetch.test.tsx` pins in jsdom,
    // asserted here where the request is a real one over the network. It is the
    // guard that stands between the redesigned control and the background
    // refetcher that was deleted: that machine kept a list of ids it still owed,
    // retried them, and repaired earlier answers when events arrived. Every one
    // of those behaviours shows up here as a second increment.
    await replyRow.getByTestId('msg-quote-jump').click();
    await expect(page.locator('.md-preview')).toBeVisible();
    await expect(page.locator('.md-preview')).toContainText(FAR_TARGET);
    expect(await byIds(), 'one click must cost exactly one by-id read').toBe(1);

    // …and it stays at one through a real repaint. Another message arrives, the
    // thread refetches, the overlay is still up — nothing may ask again.
    await postChatAs(request, tokM, 'owner', 'and one more while the overlay is open');
    await expect(thread).toContainText('and one more while the overlay is open', {
      timeout: 20000,
    });
    expect(
      await byIds(),
      'a repaint after the click must not ask again — that was the deleted collector',
    ).toBe(1);
  });

});
