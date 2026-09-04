// e2e_test/tests/20_diff_url.spec.js
// T-59 · a comparison is a URL — the round's two acceptance sentences, each of
// which spans a boundary no unit suite can span:
//
//   1. clicking a comparison link INSIDE the studio opens it as a modal in
//      place. The reader stays where they were: same url, same page behind the
//      backdrop.
//   2. opening one from OUTSIDE shows a standalone page. That path needs three
//      separate things to be true at once — the real server serves the SPA
//      shell for the path `/diff`, the signed read is answered with NO session,
//      and the page mounts ahead of the auth wall — and each of the three is
//      pinned in a different language's tests. Only a browser against the real
//      server asks all three in one question.
//
// Fixtures are 100% API-made (lib/fixtures.js): two text attachments give two
// addresses that always resolve, so the compare screen has real content on both
// sides. The spec hires its OWN member (specs share one isolated server).
const { test, expect } = require('@playwright/test');
const {
  BASE,
  authHeaders,
  ownerToken,
  hireMember,
  postChatAs,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

const MEMBER_NAME = uniqueName('Diff Reader');
// base64 of two short texts that differ on one line.
const BEFORE_B64 = Buffer.from('alpha\nshared line\n').toString('base64');
const AFTER_B64 = Buffer.from('beta\nshared line\n').toString('base64');

test.describe('T-59 · a comparison is a URL', () => {
  test('a link opens in place inside the studio, and stands alone with a signature outside it', async ({
    page,
    browser,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const member = await hireMember(request, token, MEMBER_NAME);

    // ── fixture: two stored blobs = two addresses that resolve ──
    const carrier = await postChatAs(request, token, member.id, 'the two sides', [
      { data_b64: BEFORE_B64, filename: 'before.txt', mime: 'text/plain' },
      { data_b64: AFTER_B64, filename: 'after.txt', mime: 'text/plain' },
    ]);
    expect(carrier.attachments, 'the carrier message must hold both sides').toHaveLength(2);
    const [before, after] = carrier.attachments.map((a) => a.id);

    // The INTERNAL flavour: a pure function of the two addresses, no mint. It
    // must be an absolute same-origin url — the markdown renderer's scheme
    // allowlist only ever reaches http/https links.
    const internal = `${BASE}/diff?before=${before}&after=${after}`;
    await postChatAs(request, token, member.id, `看這個 [兩份比較](${internal}) 就知道`);

    // ── 1. inside the studio: the click opens a modal, it does not navigate ──
    await bootAuthedSpa(page, token);
    await page.locator('.member-card', { hasText: MEMBER_NAME }).first().click();

    const link = page.locator('a[data-diff-link]', { hasText: '兩份比較' });
    await expect(link, 'a compare url in a message must render as an interceptable link').toBeVisible();
    expect(
      await link.getAttribute('href'),
      'it stays a real anchor, so copy-link and ⌘-click still work',
    ).toBe(internal);

    const urlBeforeClick = page.url();
    await link.click();
    const screen = page.locator('[data-testid="diff-screen"]');
    await expect(screen, 'the click must draw the comparison').toBeVisible();
    expect(page.url(), 'IN PLACE means the reader was not navigated anywhere').toBe(
      urlBeforeClick,
    );
    // Both sides really resolved — a gone side draws a status line instead.
    await expect(
      page.locator('[data-testid="diff-screen-side-gone"]'),
      'neither side may read as gone',
    ).toHaveCount(0);
    // Closing puts the reader back on the message they were reading.
    await page.locator('.md-preview__close').click();
    await expect(screen, 'closing the modal must retire the comparison').toHaveCount(0);
    await expect(link, 'the message the reader was on is still on screen').toBeVisible();

    // ── 2. outside the studio: the SIGNED url is a standalone page ──
    const minted = await request.get(
      `${BASE}/api/diff/share-link?before=${before}&after=${after}`,
      { headers: authHeaders(token) },
    );
    expect(minted.status(), 'minting the external link must succeed').toBe(200);
    const signedPath = (await minted.json()).url;
    expect(signedPath, 'the mint answers a server-relative /diff path').toMatch(/^\/diff\?/);
    expect(signedPath, '…carrying the signature that opens it with no login').toContain('sig=');

    // A FRESH context: no token in localStorage, no session of any kind — the
    // reader this link exists for.
    const stranger = await browser.newContext();
    try {
      const strangerPage = await stranger.newPage();
      await strangerPage.goto(`${BASE}${signedPath}`);

      await expect(
        strangerPage.locator('[data-testid="diff-screen"]'),
        'a signed link must draw the comparison for a reader with no session',
      ).toBeVisible();
      // The auth wall never appeared, and neither did the studio.
      await expect(
        strangerPage.locator('input[type="password"]'),
        'a signature is a credential — it must not be met with a login form',
      ).toHaveCount(0);
      // The one control the page carries is drawn only where a session is
      // certain, and this reader has none.
      await expect(
        strangerPage.locator('.diff-page__share'),
        'a reader with no account must not be offered a mint they cannot make',
      ).toHaveCount(0);
    } finally {
      await stranger.close();
    }
  });
});
