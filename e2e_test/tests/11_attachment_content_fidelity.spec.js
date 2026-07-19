// e2e_test/tests/11_attachment_content_fidelity.spec.js
// B11 · owner⇄agent content fidelity (owner-designed input) — the bytes/words
// that go IN must be the bytes/words that come OUT, both directions:
//
//   ① owner → agent (binary): the owner posts a PNG with a KNOWN digit drawn
//      into it (self-generated in-test — no external fixture file); the agent
//      (a minted member-scope token, the ocagent download identity) fetches the
//      gated blob back and the sha256 of the downloaded bytes must equal the
//      sha256 of the uploaded bytes — bit-for-bit fidelity through base64
//      decode → blob store → gated serve.
//
//   ② agent → owner (rendered text): the agent posts a chat message whose body
//      hides a KNOWN sentinel string; Playwright, AS THE OWNER, opens the
//      cockpit and asserts the sentinel is REALLY RENDERED on screen (not just
//      present on the wire).
//
//   ③ owner → agent → owner (document transform): the owner uploads a PDF with
//      KNOWN text through the streaming upload seam (POST /api/chat/attachments
//      raw bytes — what `ocagent upload` speaks) and sends its light {id} ref
//      via post_chat; the agent sees the ref in the thread, downloads the PDF,
//      extracts the text, uploads a plain-text file carrying ONLY those words
//      and replies with its {id} ref; the owner downloads the reply and the
//      words must survive the whole trip verbatim (whitespace-normalized).
const { test, expect } = require('@playwright/test');
const crypto = require('crypto');
const zlib = require('zlib');
const {
  BASE,
  authHeaders,
  ownerToken,
  hireMember,
  mintMemberToken,
  postChatAs,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

const MEMBER_NAME = uniqueName('Fidelity Agent');
const SENTINEL = 'FIDELITY-7X9Z-SENTINEL';

// ── minimal pure-JS PNG encoder (signature + IHDR + IDAT + IEND, CRC32) ─────
// Enough to place REAL, owner-designed pixel content into the test without any
// canvas/native dependency. Deterministic bytes → a stable sha256.
function crc32(buf) {
  let c;
  const table = [];
  for (let n = 0; n < 256; n++) {
    c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    table[n] = c >>> 0;
  }
  let crc = 0xffffffff;
  for (const b of buf) crc = table[(crc ^ b) & 0xff] ^ (crc >>> 8);
  return (crc ^ 0xffffffff) >>> 0;
}

function pngChunk(type, data) {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type, 'ascii'), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body));
  return Buffer.concat([len, body, crc]);
}

// 5x7 bitmap of the digit "7" — the known content drawn into the image.
const DIGIT_7 = [
  '#####',
  '....#',
  '...#.',
  '..#..',
  '.#...',
  '.#...',
  '.#...',
];

// Render the 5x7 glyph at `scale` into a truecolor RGB PNG: black digit on a
// white background.
function makeDigit7Png(scale = 8) {
  const w = 5 * scale;
  const h = 7 * scale;
  const rows = [];
  for (let y = 0; y < h; y++) {
    const row = Buffer.alloc(1 + w * 3); // leading filter byte 0 (None)
    for (let x = 0; x < w; x++) {
      const on = DIGIT_7[Math.floor(y / scale)][Math.floor(x / scale)] === '#';
      const v = on ? 0 : 255;
      row[1 + x * 3] = v;
      row[1 + x * 3 + 1] = v;
      row[1 + x * 3 + 2] = v;
    }
    rows.push(row);
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 2; // color type: truecolor RGB
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    pngChunk('IHDR', ihdr),
    pngChunk('IDAT', zlib.deflateSync(Buffer.concat(rows))),
    pngChunk('IEND', Buffer.alloc(0)),
  ]);
}

const sha256 = (buf) => crypto.createHash('sha256').update(buf).digest('hex');

// ── minimal deterministic PDF builder (uncompressed text stream) ─────────────
// A hand-readable single-page PDF: catalog → pages → page → an UNCOMPRESSED
// content stream drawing `text` with one `Tj` operator, Helvetica. No filters,
// so the text is greppable in the raw bytes and extraction below needs no PDF
// library. The builder computes the xref byte offsets, so the output is a
// fully valid PDF (opens in any viewer) yet only a few hundred bytes.
function makeTextPdf(text) {
  const esc = text.replace(/\\/g, '\\\\').replace(/\(/g, '\\(').replace(/\)/g, '\\)');
  const content = `BT /F1 24 Tf 72 720 Td (${esc}) Tj ET`;
  const objects = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>',
    `<< /Length ${content.length} >>\nstream\n${content}\nendstream`,
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
  ];
  let pdf = '%PDF-1.4\n';
  const offsets = [];
  objects.forEach((body, i) => {
    offsets.push(pdf.length);
    pdf += `${i + 1} 0 obj\n${body}\nendobj\n`;
  });
  const xref = pdf.length;
  pdf += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
  for (const off of offsets) pdf += `${String(off).padStart(10, '0')} 00000 n \n`;
  pdf += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(pdf, 'latin1');
}

// Extract the text a PDF's content stream draws: every (...) Tj show-text
// operator, PDF string escapes unfolded. Works on any PDF whose content
// streams are uncompressed (like makeTextPdf's output) — no PDF library.
function extractPdfText(buf) {
  const raw = buf.toString('latin1');
  const parts = [];
  for (const m of raw.matchAll(/\(((?:\\.|[^\\()])*)\)\s*Tj/g)) {
    parts.push(m[1].replace(/\\([\\()])/g, '$1'));
  }
  return parts.join(' ');
}

const normWs = (s) => s.replace(/\s+/g, ' ').trim();

test.describe('B11 · attachment/content fidelity — owner⇄agent round-trip', () => {
  test('① owner-uploaded PNG downloads bit-identical under the member token; ② agent text renders verbatim in the owner cockpit', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const member = await hireMember(request, token, MEMBER_NAME);
    const memberTok = await mintMemberToken(request, token, member.id, 1);

    // ── ① owner → agent: binary fidelity ──
    const png = makeDigit7Png();
    const uploadedSha = sha256(png);
    const msg = await postChatAs(request, token, member.id, 'digit for you', [
      { data_b64: png.toString('base64'), filename: 'digit-7.png', mime: 'image/png' },
    ]);
    const att = msg.attachments[0];
    expect(att.is_image).toBe(true);

    // the AGENT (member-scope token — the ocagent download identity) fetches it back
    const dl = await request.get(`${BASE}${att.url}`, {
      headers: authHeaders(memberTok),
    });
    expect(dl.status(), 'the member token must be able to download the blob').toBe(200);
    expect(dl.headers()['content-type']).toBe('image/png');
    const downloaded = await dl.body();
    expect(downloaded.length, 'no truncation').toBe(png.length);
    expect(
      sha256(downloaded),
      'sha256(downloaded) must equal sha256(uploaded) — bit-for-bit fidelity',
    ).toBe(uploadedSha);

    // ── ② agent → owner: rendered-text fidelity ──
    await postChatAs(
      request,
      memberTok,
      'owner',
      `status report: everything nominal ${SENTINEL} end of report`,
    );
    await bootAuthedSpa(page, token);
    await page.locator('.member-card', { hasText: MEMBER_NAME }).click();
    const bubble = page.locator('.chat__msg-text', { hasText: SENTINEL });
    await expect(
      bubble,
      'the sentinel string must REALLY render in the owner cockpit',
    ).toBeVisible();
    // and it renders on an INCOMING row (the agent is the sender, not the owner)
    await expect(
      page.locator('.chat__msg', { hasText: SENTINEL }).first(),
      'the sentinel message must be an incoming (non-owner) bubble',
    ).not.toHaveClass(/chat__msg--me/);
  });

  test('③ owner-sent PDF text returns verbatim as an agent-produced plain-text attachment (upload seam + light {id} refs both ways)', async ({
    request,
  }) => {
    const PDF_TEXT = 'OFFICRAFT E2E';
    const token = await ownerToken(request);
    const member = await hireMember(request, token, uniqueName('Pdf Clerk'));
    const memberTok = await mintMemberToken(request, token, member.id, 1);

    // ── owner: stream the PDF bytes through the upload seam (the `ocagent
    // upload` wire contract: raw body + ?filename=&mime=), get the light ref ──
    const pdf = makeTextPdf(PDF_TEXT);
    const up = await request.post(
      `${BASE}/api/chat/attachments?filename=brief.pdf&mime=application/pdf`,
      { headers: authHeaders(token), data: pdf },
    );
    expect(up.status(), 'the raw-bytes PDF upload must mint a blob').toBe(200);
    const ref = await up.json();
    expect(ref.id, 'the upload must answer a light ref with an id').toBeTruthy();
    expect(ref.mime).toBe('application/pdf');
    expect(ref.filename).toBe('brief.pdf');

    // ── owner → agent: post_chat carries ONLY the {id} ref, never the bytes ──
    const sent = await postChatAs(request, token, member.id, 'please transcribe this PDF', [
      { id: ref.id },
    ]);
    expect(sent.attachments).toHaveLength(1);
    expect(sent.attachments[0].id, 'the stored blob is authoritative for the ref').toBe(ref.id);
    expect(sent.attachments[0].filename).toBe('brief.pdf');

    // ── agent: sees the light ref in its thread, downloads the PDF ──
    const listRes = await request.get(`${BASE}/api/chat?with=owner&limit=100`, {
      headers: authHeaders(memberTok),
    });
    expect(listRes.status()).toBe(200);
    const seen = (await listRes.json()).find((m) => m.id === sent.id);
    expect(seen, 'the agent must see the owner message in its thread').toBeTruthy();
    expect(seen.attachments[0].id).toBe(ref.id);
    const dl = await request.get(`${BASE}${seen.attachments[0].url}`, {
      headers: authHeaders(memberTok),
    });
    expect(dl.status(), 'the agent token must download the PDF blob').toBe(200);
    expect(dl.headers()['content-type']).toBe('application/pdf');
    const gotPdf = await dl.body();
    expect(sha256(gotPdf), 'the PDF must arrive bit-identical').toBe(sha256(pdf));

    // ── agent: transform PDF → plain text, upload, reply with the {id} ref ──
    const extracted = extractPdfText(gotPdf);
    expect(normWs(extracted), 'the extraction must yield exactly the known words').toBe(PDF_TEXT);
    const txtUp = await request.post(
      `${BASE}/api/chat/attachments?filename=brief.txt&mime=text/plain`,
      { headers: authHeaders(memberTok), data: Buffer.from(extracted, 'utf8') },
    );
    expect(txtUp.status(), 'the agent token must be able to upload the txt').toBe(200);
    const txtRef = await txtUp.json();
    const reply = await postChatAs(request, memberTok, 'owner', 'transcription attached', [
      { id: txtRef.id },
    ]);
    expect(reply.attachments[0].filename).toBe('brief.txt');

    // ── owner: download the reply txt — the words must survive the whole trip ──
    const back = await request.get(
      `${BASE}/api/chat/attachment/${reply.attachments[0].id}`,
      { headers: authHeaders(token) },
    );
    expect(back.status(), 'the owner must download the agent-produced txt').toBe(200);
    expect(back.headers()['content-type']).toContain('text/plain');
    expect(
      normWs((await back.body()).toString('utf8')),
      'the txt content must be exactly the words the PDF carried',
    ).toBe(PDF_TEXT);
  });
});
