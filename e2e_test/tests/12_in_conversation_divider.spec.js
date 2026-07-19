// e2e_test/tests/12_in_conversation_divider.spec.js
// B12 · in-conversation unread divider — chip/divider ONE anchor (owner bug).
//
// Owner report (post-08fdd20): staying IN the conversation (window foreground,
// thread open), two new messages land → the floating "有新訊息" chip appears →
// clicking it jumps down… but NO "以下是未讀訊息" divider. Expected (LINE
// semantics): the divider anchors at the START of the new messages — the SAME
// anchor the chip jumps to.
//
// Root cause: the divider (`firstUnreadId`) was only ever set by the one-shot
// ENTRY positioning (snapshot of member.unreadCount at conversation entry);
// messages arriving while ALREADY in the conversation had no divider-anchoring
// path at all — only the chip's client-side id-diff saw them. This spec pins
// the aligned behavior end-to-end over the real server + SSE.
const { test, expect } = require('@playwright/test');
const {
  authHeaders,
  BASE,
  ownerToken,
  hireMember,
  mintMemberToken,
  postChatAs,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

const NAME_M = uniqueName('InConv M');
const SEED_COUNT = 12; // enough read context to overflow one screen

const PAD =
  '— padding line so the thread overflows one screen height and scrolled-up is a real scroll state';

test.describe('B12 · in-conversation arrivals — chip and divider share ONE new-message anchor', () => {
  test('foreground + already in the room: two new inbound → chip appears AND the divider anchors at the first new message', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, NAME_M);
    const tokM = await mintMemberToken(request, token, M.id, 1);

    // ── fixture: a fully-READ thread (M → owner ×12, then the owner lists it
    // = list 即讀 → watermark advances → unread 0). Entering the room below
    // must therefore render NO entry divider — the divider this spec asserts
    // can only come from the in-conversation anchoring path.
    for (let i = 1; i <= SEED_COUNT; i++) {
      await postChatAs(request, tokM, 'owner', `seed read message ${i} ${PAD}`);
    }
    const listRes = await request.get(
      `${BASE}/api/chat?with=${M.id}&limit=100`,
      { headers: authHeaders(token) },
    );
    expect(listRes.status(), 'owner listing the thread must succeed (marks read)').toBe(200);

    // ── browser: enter M's room with ZERO unread ──
    await bootAuthedSpa(page, token);
    const card = page.locator('.member-card', { hasText: NAME_M });
    await expect(card).toBeVisible();
    await card.click();
    const thread = page.locator('.chat__messages');
    await expect(thread).toBeVisible();
    await expect(
      thread.locator('.chat__msg'),
      'the seeded thread must be loaded',
    ).toHaveCount(SEED_COUNT);
    await expect(
      thread.locator('.chat__unread-divider'),
      'entering a fully-read room must render NO divider',
    ).toHaveCount(0);

    // The owner stays in the room but scrolls UP to read history.
    const overflow = await thread.evaluate(
      (el) => el.scrollHeight > el.clientHeight + 1,
    );
    expect(overflow, 'the thread must overflow one screen (real scroll state)').toBe(true);
    await thread.evaluate((el) => {
      el.scrollTop = 0;
    });

    // ── TWO new messages land while the owner is in the room (SSE-pushed) ──
    const new1 = await postChatAs(request, tokM, 'owner', `sudden new message 1 ${PAD}`);
    await postChatAs(request, tokM, 'owner', `sudden new message 2 ${PAD}`);

    // The floating chip appears…
    const chip = page.locator('.chat__new-msg-chip');
    await expect(chip, 'the "有新訊息" chip must appear').toBeVisible({
      timeout: 15_000,
    });

    // …and the viewport was NOT yanked (the owner keeps reading history; the
    // divider anchoring must never scroll on its own).
    const scrollTop = await thread.evaluate((el) => el.scrollTop);
    expect(scrollTop, 'the arrival must not yank the scrolled-up owner').toBeLessThan(40);

    // THE BUG: the divider must exist ALREADY, anchored at the FIRST of the
    // two new messages — the exact anchor the chip jumps to.
    const divider = thread.locator('.chat__unread-divider');
    await expect(
      divider,
      'the unread divider must anchor for in-conversation arrivals (owner bug: chip without divider)',
    ).toBeVisible();
    const anchorId = await divider.evaluate(
      (el) => el.nextElementSibling?.getAttribute('data-msg-id') ?? '',
    );
    expect(
      anchorId,
      'the divider must sit immediately above the FIRST new message (chip/divider ONE anchor)',
    ).toBe(new1.id);

    // ── click the chip: jump lands at the divider's anchor; the divider is
    // still there (session-kept) and reaching the bottom dismisses the chip.
    await chip.click();
    await expect(
      divider,
      'the divider must survive the jump (session-kept, LINE-style)',
    ).toBeVisible();
    await expect(chip, 'reaching the bottom must dismiss the chip').toBeHidden({
      timeout: 10_000,
    });
    await expect(
      divider,
      'the divider must survive even after the chip is dismissed',
    ).toBeVisible();
  });
});
