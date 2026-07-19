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

// Hire a fresh roster member (kind=assistant so it surfaces on the office
// roster). Returns the full MemberDTO. Each spec hires its OWN members (specs
// run in parallel workers against the one shared isolated server — never
// mutate another spec's fixtures, and never dismiss the seed `mira`).
async function hireMember(request, token, name) {
  const res = await request.post(`${BASE}/api/members`, {
    headers: authHeaders(token),
    data: { name, kind: 'assistant' },
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

// Boot the SPA already-authenticated: inject the owner token into localStorage
// (key `oc_token`, see frontend api/auth.ts) and reload — no login UI.
async function bootAuthedSpa(page, token) {
  await page.goto('/');
  await page.evaluate((t) => localStorage.setItem('oc_token', t), token);
  await page.reload();
}

module.exports = {
  BASE,
  PASSWORD,
  uniqueName,
  PNG_1x1_B64,
  ZIP_EMPTY_B64,
  authHeaders,
  ownerToken,
  hireMember,
  mintMemberToken,
  postChatAs,
  listMembers,
  unreadCountOf,
  bootAuthedSpa,
};
