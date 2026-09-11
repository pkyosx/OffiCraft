// e2e_test/tests/15_card_waited_time.spec.js
// NOTE(joey): chat MESSAGE timestamps render hh:mm-only (formatTime). The CARD absolute stamp this note was written about is gone from the 請示 card (owner 2026-09-11) and survives only on the 近期已處理 pane; whether chat messages should show absolute time is still an open question for kyle/Seth.
//
// C2 · reply-card 等待時間 — a 請示 card stamps how long it has been WAITING, as
// a relative 已等你 counter in its head, to the RIGHT of 標為過期, and it carries
// NO absolute open-time stamp at all.
//
// 🔴 THIS FILE USED TO ASSERT THE OPPOSITE, AND THE FLIP IS A RULING, NOT A
// REGRESSION. Until 2026-09-11 the card carried `[data-testid="opened-at"]` —
// `開卡 <formatAbsolute>` — beside the ticking counter (Seth 2026-07-13:
// reply-card times are absolute). owner saw it on a screenshot of this very head
// and answered「我不想知道絕對時間 相對時間已經夠了」, so the node is gone from
// RepliesPage.tsx. Do not restore it, and do not read the git history as
// evidence that it belongs there.
//
// What is still absolute is the HANDLED pane's stamp (`已回覆 <time>` /
// `已過期 <time>`) — a different pane, untouched by that ruling and out of this
// spec's scope.
const { test, expect } = require('@playwright/test');
const {
  authHeaders,
  BASE,
  ownerToken,
  hireMember,
  mintMemberToken,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

// Open a reply card AS the agent token (same path as spec 13): the initiator is
// the verified JWT sub; the server mints the id + createdTs + companion chat
// message. POST /api/reply-cards is the reply-card creation endpoint.
//
// linked_task: null is DECLARED, not defaulted-into — see the twin comment in
// spec 13. Required since T-18, and null is the honest answer here: this spec
// opens no task and plans no step, and what it pins is how a card's timestamp
// RENDERS (相對時間 vs 絕對時間), which no task binding touches.
//
// ⚠️ Spread FIRST so a caller can override it. A card opened ABOUT a task must
// pass its own linked_task {task_id, step_id} rather than inherit this null.
// 🔴 T-91: THE CREATE ANSWERS A RECEIPT, NOT THE CARD. POST /api/reply-cards
// used to hand back the whole ReplyCardDTO; it now answers
// {id, chat_message_id, created_ts, attachments} — every field the handler
// MINTED, and nothing the caller sent. So this helper creates, then READS THE
// CARD BACK, which is the same "write then re-read" the cockpit itself moved to
// in this package. Returning the receipt directly would make every downstream
// `.status` / `.options` / `.select_mode` assertion silently `undefined`.
async function createReplyCardAs(request, agentToken, card) {
  const res = await request.post(`${BASE}/api/reply-cards`, {
    headers: authHeaders(agentToken),
    data: { linked_task: null, ...card },
  });
  expect(res.status(), 'creating a reply card must succeed').toBe(200);
  const receipt = await res.json();
  expect(receipt.id, 'the create receipt must name the card it minted').toBeTruthy();
  const read = await request.get(`${BASE}/api/reply-cards/${receipt.id}`, {
    headers: authHeaders(agentToken),
  });
  expect(read.status(), 'reading the freshly opened card must succeed').toBe(200);
  return read.json();
}

function repliesTab(page) {
  // T-0004 renamed the owner-facing concept 等我回覆 → 請示 (zh.nav.replies).
  return page.locator('.nav-tab', { hasText: '請示' });
}

// A date component (M/D or YYYY/M/D) followed later by an HH:mm clock — the
// signature of formatAbsolute's output, which a bare "HH:mm" or a relative
// "分鐘前 / ago" string can NOT satisfy.
const ABSOLUTE_RE = /\d{1,4}[/-]\d{1,2}.*\d{1,2}:\d{2}/;

test.describe('C2 · reply cards stamp the WAIT as a relative counter, with no absolute open time', () => {
  test('a waiting 請示 card shows 已等你 <duration> in its head, right of 標為過期, and no absolute open time', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, uniqueName('AbsTime M'));
    const tokM = await mintMemberToken(request, token, M.id, 1);

    // ── an agent opens ONE ask ──
    const summary = uniqueName('要現在寄出這份報告嗎');
    const card = await createReplyCardAs(request, tokM, {
      kind: 'decision',
      summary,
      body: '報告已整理完畢,等你確認。',
      // T-40 option shape: objects carrying their own ai_pick. The pick sits
      // on the SECOND option on purpose — position carries no meaning, and a
      // card that put it first would keep passing against the old convention.
      options: [
        { text: '先不要', ai_pick: false },
        { text: '寄出', ai_pick: true },
      ],
    });
    expect(card.status).toBe('waiting');

    // ── boot the cockpit as owner and open the 請示 page ──
    await bootAuthedSpa(page, token);
    await repliesTab(page).click();

    const waiting = page
      .getByTestId('waiting-card')
      .filter({ hasText: summary });
    await expect(waiting, 'the opened ask must list as a waiting card').toBeVisible();

    // The card's ONE stamp: the ticking relative counter — and it is on the
    // COLLAPSED card, because that head is the collapsed row (owner 2026-09-11).
    await expect(
      waiting,
      'a card starts collapsed — 預設全部折疊',
    ).toHaveAttribute('aria-expanded', 'false');
    const waited = waiting.getByTestId('waited');
    await expect(
      waited,
      'the card must carry the relative 已等你 counter',
    ).toContainText('已等你');
    expect(
      await waited.innerText(),
      'the 已等你 counter must NOT be an absolute date+time',
    ).not.toMatch(ABSOLUTE_RE);

    // …and the absolute open-time stamp is GONE (owner 2026-09-11). Asserted as
    // absence of the NODE, not of a format: a restored stamp in a different
    // shape would still be the thing he rejected.
    await expect(
      waiting.getByTestId('opened-at'),
      'the waiting card must carry NO absolute open-time stamp',
    ).toHaveCount(0);

    // Position: in the head, AFTER 標為過期 (owner:「相對時間在標為過期右方」).
    const order = await waiting.evaluate((el) => {
      const head = el.querySelector('.reply-card__head');
      if (!head) return null;
      const kids = [...head.children];
      return {
        expire: kids.findIndex((k) => k.dataset.testid === 'expire-card'),
        waited: kids.findIndex((k) => k.dataset.testid === 'waited'),
      };
    });
    expect(order, 'the card must draw its head even while collapsed').not.toBeNull();
    expect(
      order.expire >= 0 && order.waited > order.expire,
      `已等你 must sit in the head, to the right of 標為過期 (got ${JSON.stringify(order)})`,
    ).toBe(true);
  });
});
