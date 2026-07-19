// e2e_test/tests/16_chat_scrollback.spec.js
// T-bf82 · 往上捲載入更多 (scrollback): entering a long thread loads only the
// newest window (30); scrolling to the top pulls the older history page and
// PREPENDS it (viewport stays on the row being read — no jump to top edge);
// once the history is exhausted the "已到最早訊息" marker renders; and the
// history load NEVER moves the owner's read watermark (the cursor page is
// read-only server-side — only entering/scrolling-to-bottom marks read).
//
// Real server + real HTTP matter here: the cursor (`before_ts`+`before_id`)
// rides the wire and the watermark lives server-side — vitest can only mock
// both.
const { test, expect } = require('@playwright/test');
const {
  authHeaders,
  BASE,
  ownerToken,
  hireMember,
  postChatAs,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

const NAME_M = uniqueName('Scrollback M');
const TOTAL = 40; // > one 30-page window, second page short (10) → exhausted
const PAD =
  '— padding line so every row has height and the thread really overflows';

// The owner's read watermark for the peer conversation (0 when absent).
async function ownerWatermark(request, token, peerId) {
  const res = await request.get(`${BASE}/api/chat/reads?with=${peerId}`, {
    headers: authHeaders(token),
  });
  expect(res.status(), 'GET /api/chat/reads must succeed').toBe(200);
  const rows = await res.json();
  const mine = rows.find((r) => r.reader_id === 'owner');
  return mine ? mine.last_read_ts : 0;
}

test.describe('T-bf82 · scrollback — top-of-thread history paging', () => {
  test('scroll to top loads older page, keeps the watermark, ends with the history marker', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, NAME_M);

    // Seed a 40-message owner→M thread (no unread — entry lands at bottom,
    // out of the unread-divider machinery's way).
    for (let i = 1; i <= TOTAL; i++) {
      await postChatAs(request, token, M.id, `history ${i} ${PAD}`);
    }

    await bootAuthedSpa(page, token);
    const card = page.locator('.member-card', { hasText: NAME_M });
    await expect(card).toBeVisible();
    await card.click();

    // Only the newest window is loaded (server default 30) — and because
    // more history exists, NO "已到最早訊息" marker yet.
    const thread = page.locator('.chat__messages');
    await expect(thread).toBeVisible();
    const rows = thread.locator('.chat__msg');
    await expect(rows).toHaveCount(30);
    await expect(thread.locator('.chat__history-start')).toHaveCount(0);
    await expect(rows.first()).toContainText('history 11');

    // Entering the room already marked read to the newest ts (list 即讀).
    // Snapshot the settled watermark — the history load below must not move it.
    let settled = 0;
    await expect
      .poll(async () => {
        settled = await ownerWatermark(request, token, M.id);
        return settled;
      })
      .toBeGreaterThan(0);

    // ── scroll to the very top → the older page loads and PREPENDS ──
    await thread.evaluate((el) => {
      el.scrollTop = 0;
      el.dispatchEvent(new Event('scroll'));
    });
    await expect(rows, 'the older history page must prepend').toHaveCount(TOTAL);
    await expect(rows.first()).toContainText('history 1 ');

    // Scroll compensation: the viewport was NOT left pinned to the absolute
    // top — it grew upward, keeping the previously-read row in place.
    const scrollTop = await thread.evaluate((el) => el.scrollTop);
    expect(
      scrollTop,
      'prepend must preserve the reading position (scrollTop compensated)',
    ).toBeGreaterThan(0);

    // 40 < 2×30 → the short second page exhausted the history: the honest
    // "已到最早訊息" marker renders at the top of the thread.
    await expect(thread.locator('.chat__history-start')).toBeVisible();
    await expect(thread.locator('.chat__history-start')).toContainText(
      '已到最早訊息',
    );

    // ── THE watermark pin: the history page never advanced (nor fanned) the
    // owner's read watermark — it still reads exactly the settled value. ──
    expect(await ownerWatermark(request, token, M.id)).toBe(settled);
  });
});
