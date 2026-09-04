// e2e_test/tests/07_chat_gallery.spec.js
// B8 · chat gallery (M2-3 + batch 16/18) — the flattened member gallery:
//   • API: `GET /api/chat/attachments?with=<A>` is MEMBER-PERSPECTIVE scoped
//     (owner↔A both directions + A's inter-agent threads), never leaks an
//     unrelated conversation, resolves `from_name` server-side (owner = honest
//     ""), orders newest→oldest, and 422s on a blank `with`.
//   • API: the endpoint is READ-ONLY — it must NOT advance the read watermark
//     (contrast: POST /api/chat/mark-read, the ONE door that does; since
//     8cd4fff9 GET /api/chat?with= is a pure read too). ⚠ the unread_count
//     sample MUST still be taken BEFORE anything reports A's thread read —
//     order is the assertion.
//   • browser: gallery panel opens from the chat header, 圖片/檔案 tabs split
//     by is_image, the uploader filter (全部 / a checkbox per sender, T-51 ②)
//     stacks with the tabs. (The over-filtered empty state is NOT exercised
//     here — see the note further down and the unit test it points at.)
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
  markChatRead,
  unreadCountOf,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

const NAME_A = uniqueName('Gal Target');
const NAME_B = uniqueName('Gal Peer');
const NAME_C = uniqueName('Gal Outsider');

test.describe('B8 · chat gallery — scope, sender labels, tabs + uploader filter', () => {
  test('gallery API scope/order/from_name/422 + READ-ONLY, then tabs & the uploader filter in the UI', async ({
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

    // ⚠ READ-ONLY watermark sample — taken BEFORE anything reports A's thread
    // read (the markChatRead below does; order IS the assertion here).
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
    // …and the contrast that proves the sample above was meaningful rather than
    // a trivial 0 == 0: reporting the read explicitly DOES clear it.
    //
    // 🔴 THE CONTRAST USED TO BE THE THREAD LISTING ITSELF ("list 即讀"). Commit
    // 8cd4fff9 removed that write from every path — `GET /api/chat?with=` is a
    // pure read now, exactly like the gallery — so the listing can no longer
    // play the "this one DOES mark read" half of the comparison. The endpoint
    // the cockpit actually reports reading with can.
    const list = await request.get(`${BASE}/api/chat?with=${A.id}`, { headers: auth });
    expect(list.status()).toBe(200);
    expect(
      await unreadCountOf(request, token, A.id),
      'listing the thread must NOT clear the unread count either (8cd4fff9)',
    ).toBe(unreadBefore);
    const newest = (await list.json()).messages.at(-1);
    await markChatRead(request, token, A.id, newest.ts);
    expect(
      await unreadCountOf(request, token, A.id),
      'reporting the read must clear the unread count',
    ).toBe(0);

    // ── browser: tabs + the uploader filter (stacking) + honest empty state ──
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

    // ⚠️ WHERE ONE ASSERTION WENT (T-51 ②). This block used to end by ticking B
    // on the 檔案 tab and asserting the honest empty state, because B had sent
    // no files. That exact combination is gone: the options are cut from the
    // current tab, so an uploader you can tick right now has at least one row
    // there.
    //
    // 🔴 THAT IS NOT THE SAME AS "unreachable", and an earlier version of this
    // note said so — wrongly, for three days. A tick outlives the rows it was
    // made on: a refetch can take away a ticked uploader's images while their
    // files remain, and the prune only drops uploaders absent from EVERY row.
    // So the over-filtered view IS reachable, the panel carries a third
    // sentence for it (`galleryEmptyFiltered`), and both cases are asserted at
    // unit level in `frontend/src/components/ChatGalleryPanel.test.tsx` — "shows
    // per-tab honest empty states once loaded" for the empty gallery, and "says
    // the FILTER is empty, not the gallery" for this one. Neither is exercised
    // here; that is a coverage choice, not a claim about what can happen.
    //
    // Uploader filter (T-51 ②): ONE line when closed, whatever the number of
    // uploaders. The wrapping chip row it replaced grew a line per uploader.
    const filter = panel.locator('.chat__gallery-senders');
    const toggle = filter.locator('.chat__gallery-sender-toggle');
    await expect(toggle, 'the closed filter is a single control').toHaveCount(1);
    await expect(toggle, 'nothing ticked reads as 全部').toHaveText('全部');
    await expect(
      filter.locator('.chat__gallery-sender-menu'),
      'the list is behind the toggle until it is opened',
    ).toHaveCount(0);

    // 🔴 THE OPTIONS FOLLOW THE TAB. They used to be built from every row in
    // both tabs while the list applied the tab, so 圖片 offered uploaders who
    // had only ever sent non-images and ticking one answered with an empty
    // gallery. On 圖片 the options are exactly the senders who have an image:
    // B and 我 — A sent only the zip and must NOT be offered here.
    await toggle.click();
    const options = filter.locator('.chat__gallery-sender-option');
    await expect(options, 'the images tab offers only uploaders who have an image').toHaveCount(2);
    for (const label of [NAME_B, '我']) {
      await expect(
        options.filter({ hasText: label }),
        `the dropdown must offer uploader "${label}" on the images tab`,
      ).toHaveCount(1);
    }
    await expect(
      options.filter({ hasText: NAME_A }),
      'an uploader with no image is not offered on the images tab',
    ).toHaveCount(0);

    // Ticking stacks with the tab, and the closed control says how many.
    await options.filter({ hasText: NAME_B }).locator('input').check();
    await expect(items, "B's tick narrows the images tab to B's single image").toHaveCount(1);
    await expect(items.first()).toContainText('b-pic.png');
    await expect(toggle, 'the closed control names the count, not the people').toHaveText('已選 1 位');

    // Multi-select: two ticks widen the result rather than replacing it.
    await options.filter({ hasText: '我' }).locator('input').check();
    await expect(items, 'two ticked uploaders show both their images').toHaveCount(2);
    await expect(toggle).toHaveText('已選 2 位');

    // Clearing returns to 全部.
    await filter.locator('.chat__gallery-sender-clear').click();
    await expect(toggle).toHaveText('全部');
    await expect(items, 'clearing the ticks shows every image again').toHaveCount(2);

    // 檔案 tab: the options are re-cut to the uploaders who have a file.
    await panel.locator('.chat__gallery-tab', { hasText: '檔案' }).click();
    await expect(items, 'the files tab shows the single zip').toHaveCount(1);
    await expect(items.first().locator('.chat__gallery-name')).toHaveText('a-notes.zip');
    await toggle.click();
    await expect(
      options.filter({ hasText: NAME_A }),
      'the files tab offers the uploader who sent the zip',
    ).toHaveCount(1);
    await expect(
      options.filter({ hasText: NAME_B }),
      'an uploader with no file is not offered on the files tab',
    ).toHaveCount(0);
  });
});
