// e2e_test/tests/15_card_absolute_time.spec.js
// NOTE(joey): chat MESSAGE timestamps currently render hh:mm-only (formatTime) — this spec targets CARD absolute time (formatAbsolute on RepliesPage), matching the "卡片絕對時間" requirement. Whether chat messages should also show absolute time is a separate open question for kyle/Seth.
//
// C2 · reply-card absolute time — the 等我回覆 cards stamp their open time in an
// ABSOLUTE date+time shape ("M/D HH:mm", or "YYYY/M/D HH:mm" when the year
// differs), NOT a relative "x ago". Frontend: RepliesPage.tsx renders
//   <span data-testid="opened-at">{t.replies.openedAt(formatAbsolute(card.createdTs, now))}</span>
// (formatAbsolute in frontend/src/lib/dateFormat.ts; the zh dict wraps it as
// "開卡 <time>"). This is distinct from the ticking 已等你 duration counter
// ([data-testid="waited"]) that lives alongside it.
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
async function createReplyCardAs(request, agentToken, card) {
  const res = await request.post(`${BASE}/api/reply-cards`, {
    headers: authHeaders(agentToken),
    data: card,
  });
  expect(res.status(), 'creating a reply card must succeed').toBe(200);
  return res.json();
}

function repliesTab(page) {
  // T-0004 renamed the owner-facing concept 等我回覆 → 請示 (zh.nav.replies).
  return page.locator('.nav-tab', { hasText: '請示' });
}

// A date component (M/D or YYYY/M/D) followed later by an HH:mm clock — the
// signature of formatAbsolute's output, which a bare "HH:mm" or a relative
// "分鐘前 / ago" string can NOT satisfy.
const ABSOLUTE_RE = /\d{1,4}[/-]\d{1,2}.*\d{1,2}:\d{2}/;

test.describe('C2 · reply cards stamp open time ABSOLUTE (formatAbsolute), not relative', () => {
  test('a waiting 等我回覆 card shows its open time as an absolute date+time, not "x ago" or bare hh:mm', async ({
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
      options: ['寄出(AI 建議)', '先不要'],
    });
    expect(card.status).toBe('waiting');

    // ── boot the cockpit as owner and open the 等我回覆 page ──
    await bootAuthedSpa(page, token);
    await repliesTab(page).click();

    const waiting = page
      .getByTestId('waiting-card')
      .filter({ hasText: summary });
    await expect(waiting, 'the opened ask must list as a waiting card').toBeVisible();

    // The card's open-time stamp: absolute date+time (formatAbsolute).
    const openedAt = waiting.getByTestId('opened-at');
    await expect(openedAt, 'the card must carry an open-time stamp').toBeVisible();
    const openedText = (await openedAt.innerText()).trim();

    expect(
      openedText,
      `the card open time must be ABSOLUTE (date + HH:mm); got "${openedText}"`,
    ).toMatch(ABSOLUTE_RE);
    // …and it must NOT be a relative phrasing.
    expect(
      openedText,
      'the card open time must NOT be a relative "x ago" string',
    ).not.toMatch(/(前|ago|之前)/);

    // The absolute open-time stamp is a SEPARATE node from the ticking 已等你
    // duration counter — the relative counter still lives, but on its own node.
    const waited = waiting.getByTestId('waited');
    await expect(
      waited,
      'the ticking 已等你 relative counter lives on its own node',
    ).toContainText('已等你');
    expect(
      await waited.innerText(),
      'the 已等你 counter must NOT be an absolute date+time (that is the opened-at node)',
    ).not.toMatch(ABSOLUTE_RE);
  });
});
