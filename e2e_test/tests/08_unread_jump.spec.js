// e2e_test/tests/08_unread_jump.spec.js
// B9 · unread badge → 進房 divider 錨定 → list-即讀 歸零 → SSE 新訊息浮條
// (M2 batch 19, 31e4e96 + 1473ff1).
//
// The race this spec exists to cover (vitest can't): the FE snapshots
// `member.unreadCount` at conversation entry STRICTLY BEFORE its own listChat
// fires — because that very listChat is "list 即讀" (the server advances the
// read watermark as a side effect) and the roster refetches to 0 right after.
// Only a real server + real HTTP ordering exercises that.
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
  unreadCountOf,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

const NAME_M = uniqueName('Unread M');
const NAME_DECOY = uniqueName('Unread Decoy');
const OLD_COUNT = 14; // read context — enough to overflow one screen
const NEW_COUNT = 5; // the unread tail

const PAD =
  '— padding line so the thread overflows one screen height and the entry position is a real scroll decision';

test.describe('B9 · unread — badge, entry divider anchor, list-即讀, floating chip', () => {
  test('badge shows the server count; entering anchors at the divider; listing clears it; SSE chip on new inbound', async ({
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

    // ── fixture: OLD read context (M → owner ×14, then the owner LISTS the
    // thread = list 即讀, watermark advances) + NEW unread tail (M → owner ×5).
    for (let i = 1; i <= OLD_COUNT; i++) {
      await postChatAs(request, tokM, 'owner', `old read message ${i} ${PAD}`);
    }
    const listRes = await request.get(`${BASE}/api/chat?with=${M.id}&limit=100`, {
      headers: authHeaders(token),
    });
    expect(listRes.status(), 'owner listing the thread must succeed (marks read)').toBe(200);
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
    // was still the PREVIOUS peer's loaded thread (useChat clears one commit
    // later), latching the one-shot against stale data — switching from a
    // NON-EMPTY thread meant the divider never rendered. Fixed by gating the
    // effect on useChat's `messagesPeer`. So we now deliberately enter M's
    // room FROM a settled NON-EMPTY thread: the divider assertions below only
    // pass with the guard in place.
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
    // …and not glued to the viewport top either: at least one already-read
    // message above the divider stays visible as context (batch 19, LINE ref).
    const contextVisible = await divider.evaluate((el) => {
      const box = el.closest('.chat__messages');
      const prev = el.previousElementSibling;
      if (!box || !prev) return false;
      const boxTop = box.getBoundingClientRect().top;
      const prevRect = prev.getBoundingClientRect();
      // the read message right above the divider intersects the viewport
      return prevRect.bottom > boxTop + 1;
    });
    expect(
      contextVisible,
      'at least one already-read message must remain visible above the divider',
    ).toBe(true);

    // ── read convergence: entering the room IS reading (list 即讀) ──
    await expect
      .poll(async () => unreadCountOf(request, token, M.id), {
        message: 'the unread count must converge to 0 after the room lists the thread',
      })
      .toBe(0);
    await expect(
      card.getByTestId('unread-badge'),
      'the roster badge must be gone once read',
    ).toHaveCount(0);

    // ── floating "有新訊息" chip: owner scrolled up + new inbound via SSE ──
    await thread.evaluate((el) => {
      el.scrollTop = 0;
    });
    await postChatAs(request, tokM, 'owner', `late-breaking message ${PAD}`);
    const chip = page.locator('.chat__new-msg-chip');
    await expect(chip, 'the floating chip must appear (SSE-pushed inbound)').toBeVisible({
      timeout: 15_000,
    });
    await expect(chip).toHaveText('有新訊息');
    await chip.click();
    // Click jumps toward the anchor / bottom; reaching the bottom clears it.
    await expect(chip, 'reaching the bottom must dismiss the chip').toBeHidden({
      timeout: 10_000,
    });
  });
});
