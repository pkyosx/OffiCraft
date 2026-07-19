// e2e_test/tests/13_reply_cards.spec.js
// B13 · reply cards (等我回覆卡) — the M2 SPEC full loop, black-box over the
// real server + real UI (reply-card batch B1 API + B2 等我回覆頁 + B3 聊天整合).
//
// One agent-opened ask must travel the WHOLE circuit:
//   agent opens (POST /api/reply-cards, agent token) → cockpit badge +1,
//   等我回覆 page lists it (longest-waiting first), the SAME ask renders as an
//   inline card in the chat thread → the owner answers on EITHER surface
//   (option chip on the page / typed text in the chat — a counter-question
//   included) → both surfaces converge, badge −1 live, and the agent reads the
//   answer back over the API WITH the original option wording → 重新決定
//   replaces the answer (status STAYS answered; cancel keeps it) → 跳到原訊息
//   lands in the room, located + highlighted.
//
// ⚠ HISTORY (found while writing this spec; FIXED since): the http adapter's
// subscribeEvents() used to open its OWN EventSource per subscriber — App
// badge + useMembers + useMonitoring + useChat already held 4, and EACH
// mounted ChatReplyCard added one more, so a chat thread with ≥2 inline cards
// exhausted Chromium's 6-connections-per-host pool and every further fetch
// (the answer POST included) hung forever; a second SPA tab bricked the same
// way. The frontend now pools ONE shared EventSource fanned out client-side
// to every subscriber (frontend/src/api/http.ts). So the scenarios below use
// the natural layout — cards A and B share ONE member/thread, mounting two
// inline cards together — and the dedicated 雙卡同房 leg at the bottom pins
// the pool fix directly (exactly one /api/events connection; the reply POST
// completes with two cards + every other live hook subscribed).
// Cross-surface LIVE sync is pinned by answering over the owner REST API
// while the chat surface is on screen — the server fans the exact same
// reply_card delta the 等我回覆 page (or a second window) would.
//
// Red-dot independence rides in its own test: answering from the 等我回覆
// page must NOT clear the member's chat unread badge — only entering the
// conversation does.
const { test, expect } = require('@playwright/test');
const {
  authHeaders,
  BASE,
  ownerToken,
  hireMember,
  mintMemberToken,
  unreadCountOf,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

// Open a reply card AS the agent token (the initiator is always the verified
// JWT sub — the server mints id / timestamps / the companion chat message).
async function createReplyCardAs(request, agentToken, card) {
  const res = await request.post(`${BASE}/api/reply-cards`, {
    headers: authHeaders(agentToken),
    data: card,
  });
  expect(res.status(), 'creating a reply card must succeed').toBe(200);
  return res.json();
}

// The agent's pull path after a reply_card delta: one card in full.
async function getReplyCardAs(request, token, cardId) {
  const res = await request.get(`${BASE}/api/reply-cards/${cardId}`, {
    headers: authHeaders(token),
  });
  expect(res.status(), `reading card ${cardId} must succeed`).toBe(200);
  return res.json();
}

async function waitingCount(request, token) {
  const res = await request.get(`${BASE}/api/reply-cards/count`, {
    headers: authHeaders(token),
  });
  expect(res.status(), 'GET /api/reply-cards/count must succeed').toBe(200);
  return (await res.json()).waiting;
}

// The nav badge (等我回覆 pill) renders ONLY when the waiting count > 0.
async function expectBadge(page, count) {
  const badge = page.getByTestId('replies-badge');
  if (count === 0) {
    await expect(badge, 'a zero count must render NO badge').toHaveCount(0);
  } else {
    await expect(badge, 'the nav badge must show the waiting count').toHaveText(
      String(count),
    );
  }
}

function repliesTab(page) {
  return page.locator('.nav-tab', { hasText: '等我回覆' });
}

function chatCardOf(page, card) {
  return page.locator(
    `[data-testid="chat-reply-card"][data-reply-card-id="${card.id}"]`,
  );
}

test.describe('B13 · reply cards — SPEC full loop over real UI + API', () => {
  test('open → badge/page/chat · answer via page chip + chat typing · live SSE sync · agent readback · 重新決定 · 跳到原訊息', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    // Natural layout (see the header note): cards A and B BOTH live with M1 —
    // their thread mounts two inline cards together (the shared-EventSource
    // pool makes that safe). The live-sync card C rides its own member M2 so
    // that scenario starts from a clean room.
    const M1 = await hireMember(request, token, uniqueName('ReplyCard M1'));
    const M2 = await hireMember(request, token, uniqueName('ReplyCard M2'));
    const tokM1 = await mintMemberToken(request, token, M1.id, 1);
    const tokM2 = await mintMemberToken(request, token, M2.id, 1);

    const baseWaiting = await waitingCount(request, token);

    // ── agents open TWO cards (A first → A has waited longest) ──
    const summaryA = uniqueName('要幫你寄出這封信嗎');
    const optionsA = ['寄出(AI 建議)', '先不要寄', '改寄給別人'];
    const cardA = await createReplyCardAs(request, tokM1, {
      kind: 'decision',
      summary: summaryA,
      body: '信件草稿已完成,收件人與附件都備妥。',
      options: optionsA,
    });
    // Wire contract: waiting, initiator stamped from the token, the companion
    // chat message minted, options echoed verbatim, no answer yet.
    expect(cardA.status).toBe('waiting');
    expect(cardA.from).toBe(M1.id);
    expect(cardA.chat_message_id).toBeTruthy();
    expect(cardA.options).toEqual(optionsA);
    expect(cardA.answer).toBeNull();
    expect(cardA.answered_ts).toBeNull();

    const summaryB = uniqueName('伺服器憑證已備好,請確認後我繼續');
    const cardB = await createReplyCardAs(request, tokM1, {
      kind: 'action',
      summary: summaryB,
      options: ['我做完了,請繼續'],
    });

    // ── cockpit: badge +2, 等我回覆 page lists both, longest-waiting first ──
    await bootAuthedSpa(page, token);
    await expectBadge(page, baseWaiting + 2);
    await repliesTab(page).click();

    const waitingA = page
      .getByTestId('waiting-card')
      .filter({ hasText: summaryA });
    const waitingB = page
      .getByTestId('waiting-card')
      .filter({ hasText: summaryB });
    await expect(waitingA).toBeVisible();
    await expect(waitingB).toBeVisible();
    await expect(
      waitingA.getByTestId('waited'),
      'a waiting card must show the ticking 已等你 counter',
    ).toContainText('已等你');
    // Server order = created ascending → A (older) renders ABOVE B.
    const order = await page
      .getByTestId('waiting-card')
      .evaluateAll((cards, [a, b]) => {
        const idx = (needle) =>
          cards.findIndex((c) => c.textContent.includes(needle));
        return { a: idx(a), b: idx(b) };
      }, [summaryA, summaryB]);
    expect(order.a, 'card A must be listed').toBeGreaterThanOrEqual(0);
    expect(
      order.a,
      'the longest-waiting card must sort first (A created before B)',
    ).toBeLessThan(order.b);

    // ── answer A on the 等我回覆 page (option chip, NOT the AI pick) ──
    await expect(
      waitingA.locator('.reply-option').first(),
      'options[0] must carry the AI 建議 tag',
    ).toContainText('AI 建議');
    await waitingA.locator('.reply-option').nth(1).click();
    await expect(
      waitingA,
      'an answered card must leave the 待回覆 pane',
    ).toHaveCount(0);
    const answeredA = page
      .getByTestId('answered-card')
      .filter({ hasText: summaryA });
    await expect(answeredA).toBeVisible();
    const finalA = answeredA.getByTestId('final-answer');
    await expect(finalA).toContainText('你選的');
    await expect(finalA).toContainText(optionsA[1]);
    // …badge −1 live (SSE, no reload).
    await expectBadge(page, baseWaiting + 1);

    // ── agent readback: the answer rides WITH the original option wording ──
    const readA = await getReplyCardAs(request, tokM1, cardA.id);
    expect(readA.status).toBe('answered');
    expect(readA.answer.option_idx).toBe(1);
    expect(readA.options[readA.answer.option_idx]).toBe(optionsA[1]);
    expect(readA.answered_ts).not.toBeNull();

    // ── 重新決定 on answered card A: cancel keeps, re-pick replaces ──
    await answeredA.locator('.reply-card__toggle').click(); // 查看當初選項
    await expect(
      answeredA.locator('.reply-card__past .reply-option'),
      'the original options must be reviewable in full',
    ).toHaveCount(optionsA.length);
    await answeredA.locator('.reply-card__redecide').click();
    await answeredA.locator('.reply-card__cancel').click(); // 取消 → no change
    const afterCancel = await getReplyCardAs(request, tokM1, cardA.id);
    expect(
      afterCancel.answer.option_idx,
      'cancelling 重新決定 must keep the original answer',
    ).toBe(1);

    await answeredA.locator('.reply-card__redecide').click();
    await answeredA.locator('.reply-card__past .reply-option').first().click();
    await expect(finalA).toContainText(optionsA[0]);
    await expect(
      finalA,
      'a re-pick of options[0] must carry BOTH 你選的 and AI 建議 tags',
    ).toContainText('AI 建議');
    const revisedA = await getReplyCardAs(request, tokM1, cardA.id);
    expect(revisedA.answer.option_idx, 'the agent must read the NEW decision').toBe(0);
    expect(revisedA.status, '重新決定 must never reopen the card').toBe('answered');
    expect(
      await waitingCount(request, token),
      'a revision must never re-count the badge',
    ).toBe(baseWaiting + 1);

    // ── 跳到原訊息: land in M1's room, located + highlighted ──
    await answeredA.locator('.reply-card__jump').click();
    await expect(page).toHaveURL(
      new RegExp(`#office/chat/${M1.id}/msg/${cardA.chat_message_id}$`),
    );
    const locatedMsg = page.locator(
      `.chat__msg[data-msg-id="${cardA.chat_message_id}"]`,
    );
    await expect(locatedMsg, 'the origin message must be in the thread').toBeVisible();
    await expect(
      locatedMsg,
      'the jump must locate + highlight the origin ask',
    ).toHaveClass(/chat__msg--located/);
    // The ask renders as the inline card (B3, no banner), and the two surfaces
    // agree: A was answered (and revised) on the PAGE — the chat card shows
    // the same standing answer.
    const chatCardA = chatCardOf(page, cardA);
    await expect(chatCardA).toBeVisible();
    await expect(chatCardA).toContainText(summaryA);
    await expect(chatCardA.getByTestId('final-answer')).toContainText(optionsA[0]);

    // ── card B: waiting inline in the SAME room — chatCardA (answered) and
    // chatCardB (waiting) are mounted TOGETHER, plus the badge/members/
    // monitoring/chat hooks (the layout that used to brick the pool). TYPE the
    // answer there (a counter-question IS a reply and closes the card).
    const chatCardB = chatCardOf(page, cardB);
    await expect(chatCardB).toBeVisible();
    await expect(chatCardB).toContainText(summaryB);
    await expect(chatCardB.locator('.reply-option')).toHaveCount(1);
    await expect(chatCardB.locator('.reply-option').first()).toContainText(
      'AI 建議',
    );
    const counterQuestion = '哪一台伺服器?先給我主機名稱。';
    await chatCardB.locator('.chat__input').fill(counterQuestion);
    await chatCardB.locator('.chat__input').press('Enter');
    await expect(chatCardB.getByTestId('final-answer')).toBeVisible();
    await expect(chatCardB.getByTestId('final-answer')).toContainText(
      counterQuestion,
    );
    await expectBadge(page, baseWaiting); // closed from the CHAT surface too
    const readB = await getReplyCardAs(request, tokM1, cardB.id);
    expect(readB.status).toBe('answered');
    expect(readB.answer.option_idx).toBeNull();
    expect(readB.answer.text).toBe(counterQuestion);

    // ── LIVE cross-surface sync (SSE, no navigation): with M2's room on
    // screen, a card opens mid-conversation → the inline card appears live;
    // answering it over the owner REST API (exactly what the 等我回覆 page /
    // another window fires) flips the visible chat card in place.
    await page.locator('.member-card', { hasText: M2.name }).click();
    const summaryC = uniqueName('要現在部署嗎');
    const cardC = await createReplyCardAs(request, tokM2, {
      kind: 'decision',
      summary: summaryC,
      options: ['部署(AI 建議)', '先不要'],
    });
    const chatCardC = chatCardOf(page, cardC);
    await expect(
      chatCardC,
      'a card opened mid-conversation must appear inline live (SSE)',
    ).toBeVisible({ timeout: 15_000 });
    await expectBadge(page, baseWaiting + 1); // the badge follows live too
    const answerRes = await request.post(
      `${BASE}/api/reply-cards/${cardC.id}/answer`,
      { headers: authHeaders(token), data: { option_idx: 0 } },
    );
    expect(answerRes.status(), 'the owner answer POST must succeed').toBe(200);
    await expect(
      chatCardC.getByTestId('final-answer'),
      'an answer from the other surface must flip the visible chat card live',
    ).toBeVisible({ timeout: 15_000 });
    await expect(
      chatCardC.getByTestId('final-answer'),
      'options[0] picked → both 你選的 and AI 建議 tags',
    ).toContainText('AI 建議');
    await expectBadge(page, baseWaiting);

    // ── every card of this run answered → the page's honest empty state ──
    await repliesTab(page).click();
    await expect(
      page.getByTestId('waiting-card').filter({ hasText: summaryB }),
    ).toHaveCount(0);
    await expect(
      page.getByTestId('answered-card').filter({ hasText: summaryB }),
    ).toBeVisible();
    if (baseWaiting === 0) {
      await expect(page.getByTestId('replies-empty')).toBeVisible();
    }
  });

  test('雙卡同房 — ≥2 inline cards + every other live hook share ONE SSE connection; the reply POST never hangs', async ({
    page,
  }) => {
    // Regression guard for the connection-pool bug (see the header note): two
    // waiting cards in ONE thread mount two inline ChatReplyCards on top of the
    // App badge + useMembers + useMonitoring + useChat subscribers. Under the
    // old one-EventSource-per-subscriber design that held 6 connections —
    // Chromium's per-host cap — and the answer POST hung forever. With the
    // shared pool, BOTH answers must complete, and the SPA must have opened
    // exactly ONE /api/events downlink for the whole scenario.
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, uniqueName('PoolShare M'));
    const tokM = await mintMemberToken(request, token, M.id, 1);
    const baseWaiting = await waitingCount(request, token);

    const summary1 = uniqueName('第一張:要先跑整套測試嗎');
    const summary2 = uniqueName('第二張:要順便升級依賴嗎');
    const card1 = await createReplyCardAs(request, tokM, {
      kind: 'decision',
      summary: summary1,
      options: ['跑(AI 建議)', '跳過'],
    });
    const card2 = await createReplyCardAs(request, tokM, {
      kind: 'decision',
      summary: summary2,
      options: ['升級(AI 建議)', '先不要'],
    });

    // Count every SSE downlink the SPA opens (register BEFORE boot).
    let sseOpens = 0;
    page.on('request', (req) => {
      if (new URL(req.url()).pathname === '/api/events') sseOpens += 1;
    });

    await bootAuthedSpa(page, token);
    await expectBadge(page, baseWaiting + 2);
    await page.locator('.member-card', { hasText: M.name }).click();

    const chatCard1 = chatCardOf(page, card1);
    const chatCard2 = chatCardOf(page, card2);
    await expect(chatCard1, 'card 1 must mount inline').toBeVisible();
    await expect(chatCard2, 'card 2 must mount inline ALONGSIDE card 1').toBeVisible();

    // Answer card 1 via its option chip — the POST must complete, not hang.
    await chatCard1.locator('.reply-option').nth(1).click();
    await expect(
      chatCard1.getByTestId('final-answer'),
      'the chip answer POST must complete with two cards mounted',
    ).toBeVisible({ timeout: 15_000 });

    // Answer card 2 by TYPING in its composer — the second POST too.
    const typed = '兩張卡同房也要能回。';
    await chatCard2.locator('.chat__input').fill(typed);
    await chatCard2.locator('.chat__input').press('Enter');
    await expect(
      chatCard2.getByTestId('final-answer'),
      'the typed answer POST must complete with two cards mounted',
    ).toBeVisible({ timeout: 15_000 });
    await expect(chatCard2.getByTestId('final-answer')).toContainText(typed);
    await expectBadge(page, baseWaiting);

    // Agent readback: both answers landed server-side.
    const read1 = await getReplyCardAs(request, tokM, card1.id);
    expect(read1.status).toBe('answered');
    expect(read1.answer.option_idx).toBe(1);
    const read2 = await getReplyCardAs(request, tokM, card2.id);
    expect(read2.status).toBe('answered');
    expect(read2.answer.text).toBe(typed);

    // Direct pin on the pool: ONE shared /api/events connection, ever.
    expect(
      sseOpens,
      'the whole SPA must share exactly ONE SSE downlink',
    ).toBe(1);
  });

  test('answering from the 等我回覆 page leaves the chat unread red dot untouched', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, uniqueName('RedDot M'));
    const tokM = await mintMemberToken(request, token, M.id, 1);

    const summary = uniqueName('這批舊 log 可以刪了嗎');
    await createReplyCardAs(request, tokM, {
      kind: 'decision',
      summary,
      options: ['刪(AI 建議)', '先保留'],
    });
    // The ask rides an ordinary chat message → exactly one unread for M.
    expect(await unreadCountOf(request, token, M.id)).toBe(1);

    await bootAuthedSpa(page, token);
    const memberCard = page.locator('.member-card', { hasText: M.name });
    // Precondition honesty: the office auto-opens roster[0]; if that were M,
    // its watermark would already be cleared and this test meaningless.
    await expect(memberCard).not.toHaveClass(/member-card--selected/);
    await expect(memberCard.getByTestId('unread-badge')).toHaveText('1');

    // Answer from the 等我回覆 page WITHOUT ever entering M's conversation.
    await repliesTab(page).click();
    const waiting = page.getByTestId('waiting-card').filter({ hasText: summary });
    await waiting.locator('.reply-option').first().click();
    await expect(waiting).toHaveCount(0);
    await expect(
      page.getByTestId('answered-card').filter({ hasText: summary }),
    ).toBeVisible();

    // The two signals are independent: the card is closed, the red dot stays.
    expect(
      await unreadCountOf(request, token, M.id),
      'answering must NOT advance the chat read watermark',
    ).toBe(1);
    await page.locator('.nav-tab', { hasText: '辦公室' }).click();
    await expect(
      memberCard.getByTestId('unread-badge'),
      'the unread red dot clears only by ENTERING the conversation',
    ).toHaveText('1');

    // …and entering the room is what finally clears it.
    await memberCard.click();
    await expect
      .poll(async () => unreadCountOf(request, token, M.id), {
        message: 'entering the conversation must clear the unread count',
      })
      .toBe(0);
    await expect(memberCard.getByTestId('unread-badge')).toHaveCount(0);
  });
});
