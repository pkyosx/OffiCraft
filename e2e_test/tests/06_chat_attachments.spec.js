// e2e_test/tests/06_chat_attachments.spec.js
// B7 · chat attachments — full round-trip: POST base64 attachments → thread
// renders image thumbnail + download chip → the gated blob really loads via
// the `?token=` URL → a MEMBER (agent-scope) token is authorized to GET the
// blob (the ocagent download contract, previously pinned only by unit tests) →
// the preview/download Content-Disposition split holds.
//
// Fixtures are 100% API-made (lib/fixtures.js): an inline 1x1 PNG + an inline
// empty zip ride ONE message as the generic `attachments` list. The spec hires
// its OWN member (specs share one isolated server across parallel workers).
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
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

const MEMBER_NAME = uniqueName('AttRt Carrier');
const ZIP_FILENAME = 'notes.zip';

test.describe('B7 · chat attachments — send/receive round-trip', () => {
  test('multi-attachment post → UI thumbnail/chip → gated blob serve (owner AND member token)', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const member = await hireMember(request, token, MEMBER_NAME);

    // ── fixture: ONE owner→member message carrying text + a PNG + a zip ──
    const msg = await postChatAs(request, token, member.id, 'attachment round-trip', [
      { data_b64: PNG_1x1_B64, filename: 'photo.png', mime: 'image/png' },
      { data_b64: ZIP_EMPTY_B64, filename: ZIP_FILENAME, mime: 'application/zip' },
      // base64("hello attachment log") — a previewable NON-image (text/plain)
      // to pin the `inline` + CSP-sandbox branch of the disposition split.
      { data_b64: 'aGVsbG8gYXR0YWNobWVudCBsb2c=', filename: 'run.log.txt', mime: 'text/plain' },
    ]);
    expect(msg.attachments, 'the message must carry all three attachment refs').toHaveLength(3);
    const [png, zip, txt] = msg.attachments;
    expect(png.is_image, 'the PNG ref must be flagged is_image').toBe(true);
    expect(zip.is_image, 'the zip ref must NOT be flagged is_image').toBe(false);
    expect(zip.filename, 'the zip ref must keep its filename').toBe(ZIP_FILENAME);

    // ── API: owner-token blob serve + the preview/download disposition split ──
    // An IMAGE is served with NO disposition at all (contract: <img src> keeps
    // working; see handle_get_chat_attachment).
    const pngRes = await request.get(`${BASE}${png.url}`, { headers: authHeaders(token) });
    expect(pngRes.status(), 'owner GET of the image blob must 200').toBe(200);
    expect(pngRes.headers()['content-type'], 'stored mime must round-trip').toBe('image/png');
    expect(
      pngRes.headers()['content-disposition'],
      'an image is served with NO disposition (inline <img> contract)',
    ).toBeUndefined();
    // A previewable NON-image (text/*) is `inline` + CSP sandbox (no script/
    // same-origin reach — an inline html/text blob must not read localStorage).
    const txtRes = await request.get(`${BASE}${txt.url}`, { headers: authHeaders(token) });
    expect(txtRes.status(), 'owner GET of the text blob must 200').toBe(200);
    expect(
      txtRes.headers()['content-disposition'],
      'a previewable non-image is served inline (new-tab preview)',
    ).toContain('inline');
    expect(
      txtRes.headers()['content-security-policy'],
      'every non-image inline preview must carry the CSP sandbox',
    ).toContain('sandbox');
    const zipRes = await request.get(`${BASE}${zip.url}`, { headers: authHeaders(token) });
    expect(zipRes.status(), 'owner GET of the zip blob must 200').toBe(200);
    expect(zipRes.headers()['content-type']).toBe('application/zip');
    const zipDisp = zipRes.headers()['content-disposition'] || '';
    expect(
      zipDisp,
      'an opaque binary is a forced download (Content-Disposition: attachment)',
    ).toContain('attachment');
    expect(zipDisp, 'the download must carry its filename').toContain(ZIP_FILENAME);

    // ── API: the `?token=` query fallback (what authedAttachmentUrl rides —
    // a bare <img src>/<a href> cannot set an Authorization header) ──
    const qRes = await request.get(`${BASE}${png.url}?token=${encodeURIComponent(token)}`);
    expect(qRes.status(), 'the ?token= query auth fallback must 200').toBe(200);

    // ── API: MEMBER-token download authorization (the ocagent download
    // contract — the route is GATED for ANY verified principal, NOT owner-only;
    // tightening it would silently break `ocagent download`) ──
    const memberTok = await mintMemberToken(request, token, member.id, 1);
    for (const att of [png, zip]) {
      const res = await request.get(`${BASE}${att.url}`, {
        headers: authHeaders(memberTok),
      });
      expect(
        res.status(),
        `a member (agent-scope) token must be authorized to GET blob ${att.id}`,
      ).toBe(200);
    }
    // …while NO credential stays a flat 401 (gated, not public).
    const anonRes = await request.get(`${BASE}${png.url}`);
    expect(anonRes.status(), 'an unauthenticated blob GET must 401').toBe(401);

    // ── API negative: a truly empty message (no text AND no attachments) → 400 ──
    const empty = await request.post(`${BASE}/api/chat`, {
      headers: authHeaders(token),
      data: { to: member.id, body: '' },
    });
    expect(empty.status(), 'an empty message must be rejected 400').toBe(400);

    // ── browser: the thread renders the thumbnail + the download chip ──
    await bootAuthedSpa(page, token);
    await page
      .locator('.member-card', { hasText: MEMBER_NAME })
      .first()
      .click();

    const img = page.locator(`.chat__msg-image[src*="${png.id}"]`);
    await expect(img, 'the image attachment must render as an inline thumbnail').toBeVisible();
    const src = await img.getAttribute('src');
    expect(src, 'the thumbnail src must hit the gated blob route').toContain('/api/chat/attachment/');
    expect(src, 'the thumbnail src must ride the ?token= auth (authedAttachmentUrl)').toContain('token=');
    // naturalWidth > 0 ⇒ the gated blob REALLY loaded (a 401 leaves a broken img).
    await expect
      .poll(async () => img.evaluate((el) => el.naturalWidth), {
        message: 'the thumbnail must actually decode (naturalWidth > 0)',
      })
      .toBeGreaterThan(0);

    const chip = page.locator('.chat__msg-file', { hasText: ZIP_FILENAME });
    await expect(chip, 'the zip must render as a download chip with its filename').toBeVisible();
    const href = await chip.getAttribute('href');
    expect(href, 'the chip href must hit the gated blob route').toContain(`/api/chat/attachment/${zip.id}`);
    expect(href, 'the chip href must ride the ?token= auth').toContain('token=');

    // Text + attachments share ONE bubble (owner feedback: a text+attachment
    // message reads as a single message, not two blocks).
    const bubble = page.locator('.chat__msg-bubble', { hasText: 'attachment round-trip' });
    await expect(bubble.locator('.chat__msg-image')).toBeVisible();
  });
});
