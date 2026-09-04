// e2e_test/lib/fixtures.js — shared API fixture helpers for the isolated e2e
// suite (M2 specs 06–11). Everything here drives the REAL isolated server
// (OC_E2E_BASE, :8791) through its public API — never a mock, never prod.
//
// Conventions (mirrors tests/03 & 04):
//   • owner identity = POST /api/login with OC_E2E_PASSWORD.
//   • member (agent-scope) identity = POST /api/mint — ALWAYS with an explicit
//     ttl_days (the DTO requires it; omitting it is a 422, not a default).
//   • the server stamps chat sender from the verified JWT sub, so "post as X"
//     is purely "post with X's token" ({to, body} only — `from` is ignored).
const { expect } = require('@playwright/test');

const BASE = process.env.OC_E2E_BASE || 'http://127.0.0.1:8791';
const PASSWORD = process.env.OC_E2E_PASSWORD || 'kyle-e2e-local-pw';

// ── inline binary fixtures (no external files) ──────────────────────────────
// A valid 1x1 red PNG (67 bytes) — sniffable image/png magic.
const PNG_1x1_B64 =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
// A valid EMPTY zip (the 22-byte end-of-central-directory record) — a real,
// unzip-able archive, NOT previewable (forces the download disposition path).
const ZIP_EMPTY_B64 = 'UEsFBgAAAAAAAAAAAAAAAAAAAAAAAA==';

// A real PNG of a GIVEN SIZE, generated rather than pasted (3KB of base64 in
// this file buys nothing a `zlib` call does not).
//
// 🔴 WHY A BIG ONE EXISTS AT ALL. It has to be real bytes that take real time
// to arrive and decode: PNG_1x1_B64 lands in one segment, so a spec that wants
// to hold an image OPEN in the undecoded state has nothing to hold.
// ⚠️ It no longer reproduces a REFLOW. It used to — `.chat__msg-image` was
// width/height:auto, so an undecoded image was a zero-height row that grew to
// its real height later, and that was the whole mechanism behind 「上方晚載入
// 的內容把目標擠走」. Since T-48 gave the thumbnail a fixed 220px box the row
// is its final height before the bytes arrive, and the growth is 0px.
function pngOfSize(w, h) {
  const zlib = require('zlib');
  const chunk = (type, data) => {
    const body = Buffer.concat([Buffer.from(type, 'ascii'), data]);
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const crc = Buffer.alloc(4);
    crc.writeUInt32BE(zlib.crc32 ? zlib.crc32(body) >>> 0 : crc32(body));
    return Buffer.concat([len, body, crc]);
  };
  // Node <20.12 has no zlib.crc32 — carry the table rather than depend on it.
  function crc32(buf) {
    let c = ~0;
    for (const b of buf) {
      c ^= b;
      for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1));
    }
    return ~c >>> 0;
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 2; // truecolour
  // Non-uniform pixels on purpose: a solid colour compresses to a few hundred
  // bytes and can land in one TCP segment, which makes "still decoding" hard to
  // hold open.
  const rows = [];
  for (let y = 0; y < h; y++) {
    const row = Buffer.alloc(w * 3 + 1);
    for (let x = 0; x < w * 3; x++) row[x + 1] = (x * 7 + y * 3) % 256;
    rows.push(row);
  }
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', ihdr),
    chunk('IDAT', zlib.deflateSync(Buffer.concat(rows), { level: 9 })),
    chunk('IEND', Buffer.alloc(0)),
  ]).toString('base64');
}

/** 400x300 — drawn 293x220 inside `.chat__msg-image`'s fixed box. */
const PNG_400x300_B64 = pngOfSize(400, 300);

// A per-call unique display name. run_all always starts from a FRESH DB, but
// specs must stay idempotent under re-runs against a still-warm server (dev
// iteration) — a duplicated display name would make name-scoped UI locators
// ambiguous (strict-mode violation / clicking a stale twin's thread).
let nameSeq = 0;
function uniqueName(prefix) {
  nameSeq += 1;
  return `${prefix} ${Date.now().toString(36)}${nameSeq}`;
}

function authHeaders(token) {
  return { Authorization: `Bearer ${token}` };
}

// Log in (real API) and return the owner token. Works from both `page` and
// `request` fixtures (anything exposing .post — APIRequestContext).
async function ownerToken(request) {
  const login = await request.post(`${BASE}/api/login`, {
    data: { password: PASSWORD },
  });
  expect(login.status(), 'login must succeed').toBe(200);
  const { token } = await login.json();
  expect(token, 'login must return an owner token').toBeTruthy();
  return token;
}

// Hire a fresh roster member (kind=staff so it surfaces on the office
// roster). Returns the full MemberDTO. Each spec hires its OWN members (specs
// run in parallel workers against the one shared isolated server — never
// mutate another spec's fixtures, and never dismiss the seed `mira`).
async function hireMember(request, token, name) {
  const res = await request.post(`${BASE}/api/members`, {
    headers: authHeaders(token),
    data: { name, kind: 'staff' },
  });
  expect(res.status(), `hiring member "${name}" must succeed`).toBe(200);
  return res.json();
}

// Owner-gated mint of a member's agent-scope token. ttl_days is REQUIRED by
// the DTO — always send it explicitly (default 1 day here; capped server-side).
async function mintMemberToken(request, ownerTok, memberId, ttlDays = 1) {
  const res = await request.post(`${BASE}/api/mint`, {
    headers: authHeaders(ownerTok),
    data: { member_id: memberId, ttl_days: ttlDays },
  });
  expect(res.status(), `minting a token for ${memberId} must succeed`).toBe(200);
  const { token } = await res.json();
  expect(token, 'mint must return an agent token').toBeTruthy();
  return token;
}

// Post one chat message AS the token's identity (server stamps the sender from
// the verified JWT sub). `attachments` is the generic list of
// {data_b64, filename?, mime?}. Returns the created ChatMessageDTO.
async function postChatAs(request, token, to, body, attachments) {
  const res = await request.post(`${BASE}/api/chat`, {
    headers: authHeaders(token),
    data: {
      to,
      body,
      ...(attachments && attachments.length > 0 ? { attachments } : {}),
    },
  });
  expect(res.status(), `posting chat to ${to} must succeed`).toBe(200);
  return res.json();
}

// Mark a conversation read up to `lastReadTs`, as the token's identity.
//
// 🔴 THIS REPLACED "list 即讀" IN THE FIXTURES. `GET /api/chat?with=` used to
// advance the reader's watermark as a side effect, and every spec that needed a
// READ thread produced one by listing it. Commit 8cd4fff9
// (「GET /api/chat 不再寫已讀水位」) removed that write from every path — a
// listing is now a pure read — so a fixture built on it silently produces an
// UNREAD thread and the spec fails somewhere far away, on an unread count it
// never mentions. Reading is now reported explicitly, by the same endpoint the
// cockpit itself calls (useChat's markRead → POST /api/chat/mark-read).
async function markChatRead(request, token, peer, lastReadTs) {
  const res = await request.post(`${BASE}/api/chat/mark-read`, {
    headers: authHeaders(token),
    data: { peer, last_read_ts: lastReadTs },
  });
  expect(res.status(), `marking ${peer} read must succeed`).toBe(200);
  return res.json();
}

// GET /api/members as the token's identity; returns the MemberDTO rows.
async function listMembers(request, token) {
  const res = await request.get(`${BASE}/api/members`, {
    headers: authHeaders(token),
  });
  expect(res.status(), 'GET /api/members must succeed').toBe(200);
  return res.json();
}

// The caller-perspective unread count for ONE member (0 when absent).
async function unreadCountOf(request, token, memberId) {
  const members = await listMembers(request, token);
  const row = members.find((m) => m.id === memberId);
  expect(row, `member ${memberId} must be on the roster`).toBeTruthy();
  return row.unread_count;
}

// ── the suite's ONE cross-origin dependency, closed off ─────────────────────
// frontend/index.html loads Schibsted Grotesk + the Noto TC families from
// Google Fonts. That is the only request in an SPA boot that leaves this
// machine, and it is enough to redden an arbitrary spec: `page.goto` /
// `page.reload` default to waitUntil:"load", playwright.config.js sets no
// navigationTimeout, so a font subresource that never completes holds `load`
// open until the 30s TEST timeout expires. Measured on CI run 33776940866
// attempt 1 — every first-party asset done in ~2.4ms, the Google stylesheet
// alone 3668ms, no fonts.gstatic.com entry at all, and
// 19_user_operation_contracts:167 dead at exactly 30.1s inside bootAuthedSpa.
// The re-run passed that spec and reddened a different one: the runner's
// egress, not any spec's code.
//
// ABORTING THESE TWO HOSTS IS SAFE BECAUSE THEY DECIDE GLYPH SHAPES AND
// NOTHING ELSE — every face here has a fallback in the stack, so the studio
// still lays out, still wraps and still scrolls; what changes is which
// typeface draws the characters. The layout-sensitive specs here measure CJK
// padding characters, which are em-wide in BOTH stacks, so the wrap points are
// identical. (Measured on the real specs by O-203, not re-verified here — the
// earlier wording, "no assertion in this suite asks", claimed more than anyone
// had checked: not asking ABOUT a font is not the same as not DEPENDING on one.)
//
// 🔴 EXACTLY THESE TWO HOSTS, NOT "cross-origin". A blanket block would starve
// a future spec that legitimately needs an outside request, and it would do so
// silently — an aborted request looks like a product that never asked.
const WEBFONT_ORIGINS = [
  'https://fonts.googleapis.com/**',
  'https://fonts.gstatic.com/**',
];

// Call BEFORE the first navigation on `page` — a route added afterwards does
// not retroactively cancel a request already in flight.
async function blockWebFonts(page) {
  for (const pattern of WEBFONT_ORIGINS) {
    await page.route(pattern, (route) => route.abort());
  }
}

// Boot the SPA already-authenticated: inject the owner token into localStorage
// (key `oc_token`, see frontend api/auth.ts) and reload — no login UI.
async function bootAuthedSpa(page, token) {
  await blockWebFonts(page);
  await page.goto('/');
  await page.evaluate((t) => localStorage.setItem('oc_token', t), token);
  await page.reload();
}

module.exports = {
  BASE,
  PASSWORD,
  uniqueName,
  PNG_1x1_B64,
  PNG_400x300_B64,
  pngOfSize,
  ZIP_EMPTY_B64,
  authHeaders,
  ownerToken,
  hireMember,
  mintMemberToken,
  postChatAs,
  markChatRead,
  listMembers,
  unreadCountOf,
  blockWebFonts,
  bootAuthedSpa,
};
