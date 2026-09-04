// e2e_test/tests/12_in_conversation_divider.spec.js
// B12 · in-conversation unread divider — the divider must anchor for arrivals
// that land while the owner is ALREADY in the room.
//
// The report below is HISTORY and is quoted as written; the 「有新訊息」 chip it
// names no longer exists (T-48 replaced it with the new-message preview strip,
// `chat-new-msg-preview`, and the strip's click lands on the LATEST message
// rather than on the divider's anchor). What survives unchanged is the defect
// it describes: arrivals inside the room left no divider at all.
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
  markChatRead,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

const NAME_M = uniqueName('InConv M');
const SEED_COUNT = 12; // enough read context to overflow one screen

const PAD =
  '— padding line so the thread overflows one screen height and scrolled-up is a real scroll state';

// ⚠️ THE TITLE BELOW USED TO SAY 「chip and divider share ONE new-message
// anchor」, and BOTH halves of that are false today (T-48): there is no chip —
// it was replaced by the new-message preview strip (`chat-new-msg-preview`),
// which is mutually exclusive with the back-to-latest arrow — and the two no
// longer share an anchor. The divider still anchors at the FIRST unread; the
// strip's click lands on the LATEST message. What this spec pins is that both
// ends of the unread run are marked, not that they point at the same row.
test.describe('B12 · in-conversation arrivals — the preview strip and the divider mark the same unread run from its two ends', () => {
  test('foreground + already in the room: two new inbound → the new-message preview strip appears AND the divider anchors at the first new message', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, NAME_M);
    const tokM = await mintMemberToken(request, token, M.id, 1);

    // ── fixture: a fully-READ thread (M → owner ×12, read up to the last of
    // them → unread 0). Entering the room below must therefore render NO entry
    // divider — the divider this spec asserts can only come from the
    // in-conversation anchoring path.
    //
    // 🔴 THE READ IS REPORTED EXPLICITLY. It used to be produced by LISTING the
    // thread; commit 8cd4fff9 removed that side effect from every path, and a
    // fixture still built on the listing leaves the thread UNREAD — which draws
    // an ENTRY divider and reddens the assertion below on a claim it is not
    // about.
    let lastSeed;
    for (let i = 1; i <= SEED_COUNT; i++) {
      lastSeed = await postChatAs(request, tokM, 'owner', `seed read message ${i} ${PAD}`);
    }
    await markChatRead(request, token, M.id, lastSeed.ts);

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
    // 🔴 THE SCROLL HAS TO BE WAITED FOR, NOT MERELY ISSUED.
    // `el.scrollTop = 0` updates the property synchronously, but ChatArea keeps
    // its "is the owner near the bottom?" answer in a ref of its own
    // (`session.nearBottom`, on the per-conversation `ChatSession` — there is no
    // `nearBottomRef` any more, and it is written from a handful of sites, not
    // one). The site that answers THIS question is its onScroll handler: a
    // programmatic assignment
    // dispatches the scroll event on a later task, so code that assigns and walks
    // straight on leaves the component still holding the previous answer: the
    // next arrival is auto-followed to the bottom and no preview strip is ever armed.
    // Resolving on the element's own scroll event puts us after that dispatch —
    // scroll is one of React 18's non-delegated events, so `onScroll` is NOT a
    // root listener: react-dom attaches it straight to this element, in the
    // target phase, when ChatArea mounts. The listener below goes on the same
    // element in the same phase, only later, so registration order is what puts
    // React's handler ahead of ours.
    await thread.evaluate(
      (el) =>
        new Promise((resolve) => {
          if (el.scrollTop === 0) {
            resolve(null);
            return;
          }
          el.addEventListener('scroll', () => resolve(null), { once: true });
          el.scrollTop = 0;
        }),
    );
    await expect
      .poll(() => thread.evaluate((el) => el.scrollTop), {
        message:
          'the scroll to the top must have landed before any new message is posted',
      })
      .toBeLessThan(40);

    // ── TWO new messages land while the owner is in the room (SSE-pushed) ──
    const new1 = await postChatAs(request, tokM, 'owner', `sudden new message 1 ${PAD}`);
    await postChatAs(request, tokM, 'owner', `sudden new message 2 ${PAD}`);

    // ── THE SCROLL POSITION IS SAMPLED TWICE, ON PURPOSE ───────────────────
    //
    // These are NOT the same assertion written down twice. They sit on
    // opposite sides of the strip wait and are read at different moments.
    //
    // ① below runs BEFORE anything that can abort, so a red strip still leaves a
    // scroll position in the log. What that number can settle is narrow, and the
    // narrowness is the point: a big reading is what BOTH candidate mechanisms
    // produce — the scroll never landing, and the arrival pulling a scrolled-up
    // viewport back down — because both end with the viewport at the bottom. It
    // therefore rules nothing in and nothing out by itself. What makes the
    // reading attributable is the gate above: the scroll is now waited for and
    // asserted by name before a single new message is posted, so if that
    // mechanism is the live one the run reddens there, on its own message,
    // rather than here.
    // ② after the strip is the ORIGINAL guarantee and it is unchanged: the SSE
    // arrival must not yank a scrolled-up reader back to the bottom. ① cannot
    // stand in for it — ① samples before the frames are necessarily processed,
    // so it says nothing about what the arrival did. Moving ② up here instead
    // of adding ① would have silently deleted that guarantee.
    //
    // ① DIAGNOSTIC — where was the viewport when the arrivals were posted?
    const scrolledUp = await thread.evaluate((el) => el.scrollTop);
    expect(
      scrolledUp,
      'the owner must still be scrolled up when the arrivals land (sampled here, ' +
        'before the strip wait, so that a red strip still records a scroll position)',
    ).toBeLessThan(40);

    // The new-message preview strip appears…
    const strip = page.getByTestId('chat-new-msg-preview');
    await expect(strip, 'the new-message preview strip must appear').toBeVisible({
      timeout: 15_000,
    });

    // ② THE ORIGINAL GUARANTEE, in its original place. It has to stay AFTER the
    // strip: only once the strip is up do we know the SSE frames were received
    // and rendered, which is the one moment at which "the arrival did not yank
    // the viewport" is a claim about the arrival rather than about nothing.
    // Same expression, same message, same threshold as before this change.
    //
    // …and the viewport was NOT yanked (the owner keeps reading history; the
    // divider anchoring must never scroll on its own).
    const scrollTop = await thread.evaluate((el) => el.scrollTop);
    expect(scrollTop, 'the arrival must not yank the scrolled-up owner').toBeLessThan(40);

    // THE BUG: the divider must exist ALREADY, anchored at the FIRST of the
    // two new messages. 🔴 T-48: that is NO LONGER where the jump lands — the
    // jump goes to the LATEST message; the divider is what keeps the START of
    // the unread block marked once the reader is standing at its end.
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
      'the divider must sit immediately above the FIRST new message of the run',
    ).toBe(new1.id);

    // ── click the strip: the jump lands on the LATEST message; the divider is
    // still there (session-kept) and the strip is consumed.
    await page.getByTestId('chat-new-msg-jump').click();
    await expect(
      divider,
      'the divider must survive the jump (session-kept, LINE-style)',
    ).toBeVisible();
    await expect(strip, 'reaching the latest message must dismiss the strip').toBeHidden({
      timeout: 10_000,
    });
    await expect(
      divider,
      'the divider must survive even after the strip is dismissed',
    ).toBeVisible();
  });
});
