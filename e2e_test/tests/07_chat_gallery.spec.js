// e2e_test/tests/07_chat_gallery.spec.js
// B8 · chat gallery (M2-3 + batch 16/18) — the flattened member gallery:
//   • API: `GET /api/chat/attachments?with=<A>` is MEMBER-PERSPECTIVE scoped
//     (owner↔A both directions + A's inter-agent threads), never leaks an
//     unrelated conversation, resolves `from_name` server-side (owner = honest
//     ""), orders newest→oldest, and 422s on a blank `with`.
//   • API: the endpoint is READ-ONLY — it must NOT advance the read watermark
//     (contrast: GET /api/chat?with= IS "list 即讀"). ⚠ the unread_count sample
//     MUST be taken BEFORE anything lists A's thread — order is the assertion.
//   • browser: gallery panel opens from the chat header, 圖片/檔案 tabs split
//     by is_image, sender chips (全部/我/per-sender) stack with the tabs, and
//     an over-filtered view shows the honest empty state.
const { test, expect } = require('@playwright/test');
const {
  BASE,
  PNG_1x1_B64,
  ZIP_EMPTY_B64,
  authHeaders,
  ownerToken,
  hireMember,
  mintMemberToken,
  postChatAs,
  unreadCountOf,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

const NAME_A = uniqueName('Gal Target');
const NAME_B = uniqueName('Gal Peer');
const NAME_C = uniqueName('Gal Outsider');

test.describe('B8 · chat gallery — scope, sender labels, tabs + chips', () => {
  test('gallery API scope/order/from_name/422 + READ-ONLY, then tabs & sender chips in the UI', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const auth = authHeaders(token);
    const A = await hireMember(request, token, NAME_A);
    const B = await hireMember(request, token, NAME_B);
    const C = await hireMember(request, token, NAME_C);
    const tokA = await mintMemberToken(request, token, A.id, 1);
    const tokB = await mintMemberToken(request, token, B.id, 1);

    // ── fixture (posted oldest→newest; the gallery must answer newest→oldest):
    //   1. owner → A : image           (owner↔member, owner side)
    //   2. A → owner : zip (as A)      (owner↔member, member side; +1 unread for owner)
    //   3. B → A     : image (as B)    (inter-agent — A's perspective includes it)
    //   4. owner → C : image           (unrelated conversation — must NOT leak)
    await postChatAs(request, token, A.id, 'a pic for you', [
      { data_b64: PNG_1x1_B64, filename: 'a-pic.png', mime: 'image/png' },
    ]);
    await postChatAs(request, tokA, 'owner', 'my notes back', [
      { data_b64: ZIP_EMPTY_B64, filename: 'a-notes.zip', mime: 'application/zip' },
    ]);
    await postChatAs(request, tokB, A.id, 'inter-agent pic', [
      { data_b64: PNG_1x1_B64, filename: 'b-pic.png', mime: 'image/png' },
    ]);
    await postChatAs(request, token, C.id, 'unrelated pic', [
      { data_b64: PNG_1x1_B64, filename: 'c-pic.png', mime: 'image/png' },
    ]);

    // ⚠ READ-ONLY watermark sample — taken BEFORE anything lists A's thread
    // (GET /api/chat?with=A auto-marks read; order IS the assertion here).
    const unreadBefore = await unreadCountOf(request, token, A.id);
    expect(
      unreadBefore,
      "A's zip message must be unread for the owner before any thread list",
    ).toBe(1);

    // ── the gallery query (owner token, member-perspective scope) ──
    const galRes = await request.get(`${BASE}/api/chat/attachments?with=${A.id}`, {
      headers: auth,
    });
    expect(galRes.status(), 'the gallery query must succeed').toBe(200);
    const rows = await galRes.json();
    expect(
      rows.map((r) => r.filename),
      "exactly A's 3 attachments, newest→oldest — and never the unrelated owner↔C one",
    ).toEqual(['b-pic.png', 'a-notes.zip', 'a-pic.png']);
    const [rowB, rowA, rowOwner] = rows;
    expect(rowB.from, 'the inter-agent row carries the verified sender id').toBe(B.id);
    expect(rowB.from_name, "the sender's display name resolves server-side").toBe(NAME_B);
    expect(rowB.to, 'the inter-agent row addressee is A').toBe(A.id);
    expect(rowA.from).toBe(A.id);
    expect(rowA.from_name).toBe(NAME_A);
    expect(rowOwner.from).toBe('owner');
    expect(
      rowOwner.from_name,
      'the owner row keeps from_name honest-empty (the FE renders its own 「我」)',
    ).toBe('');
    expect(rowB.is_image).toBe(true);
    expect(rowA.is_image, 'the zip row must not be flagged is_image').toBe(false);

    // Blank `with` → 422 (the gallery is always per-member).
    const blank = await request.get(`${BASE}/api/chat/attachments?with=`, { headers: auth });
    expect(blank.status(), 'a blank ?with= must be a 422').toBe(422);
    const missing = await request.get(`${BASE}/api/chat/attachments`, { headers: auth });
    expect(missing.status(), 'a missing ?with= must be a 422').toBe(422);

    // READ-ONLY seal: the gallery listing above must NOT have advanced the
    // owner's read watermark…
    const unreadAfterGallery = await unreadCountOf(request, token, A.id);
    expect(
      unreadAfterGallery,
      'opening the gallery must not mark the thread read (READ-ONLY endpoint)',
    ).toBe(unreadBefore);
    // …while the thread list IS "list 即讀" (the contrast that proves the
    // sample order above was meaningful, not a trivial 0 == 0).
    const list = await request.get(`${BASE}/api/chat?with=${A.id}`, { headers: auth });
    expect(list.status()).toBe(200);
    expect(
      await unreadCountOf(request, token, A.id),
      'listing the thread (GET /api/chat?with=) must clear the unread count',
    ).toBe(0);

    // ── browser: tabs + sender chips (stacking) + honest empty state ──
    await bootAuthedSpa(page, token);
    await page.locator('.member-card', { hasText: NAME_A }).click();
    await page.locator('.chat__gallery-toggle').click();
    const panel = page.locator('.chat__gallery');
    await expect(panel, 'the gallery panel must open').toBeVisible();

    // 圖片 tab (default): the two image rows — owner's + B's.
    const items = panel.locator('.chat__gallery-item');
    await expect(items, 'the images tab shows exactly the 2 image rows').toHaveCount(2);
    const bRow = items.filter({ hasText: 'b-pic.png' });
    await expect(bRow.locator('.chat__gallery-thumb')).toBeVisible();
    await expect(
      bRow.locator('.chat__gallery-sub'),
      "the inter-agent row is labelled with B's display NAME (not a raw id)",
    ).toContainText(NAME_B);
    await expect(
      bRow.locator('.chat__gallery-sub'),
      'the raw internal member id must not render as the sender label',
    ).not.toContainText(B.id);

    // Sender chips: 全部 + one per actual sender (row order: B, A, 我).
    const chips = panel.locator('.chat__gallery-sender-chip');
    await expect(chips, '全部 + 3 senders').toHaveCount(4);
    await expect(chips.nth(0)).toHaveText('全部');
    for (const label of [NAME_B, NAME_A, '我']) {
      await expect(
        chips.filter({ hasText: label }),
        `the chip row must offer sender "${label}"`,
      ).toHaveCount(1);
    }

    // Chip filter stacks with the tab: B chip + 圖片 tab → only B's image.
    await chips.filter({ hasText: NAME_B }).click();
    await expect(items, "B's chip narrows the images tab to B's single image").toHaveCount(1);
    await expect(items.first()).toContainText('b-pic.png');

    // 檔案 tab + B chip → B sent no files → the honest empty state.
    await panel.locator('.chat__gallery-tab', { hasText: '檔案' }).click();
    await expect(
      panel.locator('.chat__gallery-empty'),
      'an over-filtered view shows the honest empty state, never a fake row',
    ).toBeVisible();

    // 檔案 tab + 全部 → the zip row.
    await chips.filter({ hasText: '全部' }).click();
    await expect(items, 'the files tab (unfiltered) shows the single zip').toHaveCount(1);
    await expect(items.first().locator('.chat__gallery-name')).toHaveText('a-notes.zip');
  });
});
