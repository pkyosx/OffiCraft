// e2e_test/tests/03_chat_autoscroll.spec.js
// B6 · chat — the message thread must autoscroll to the newest message.
//
// BASELINE = RED (known-failure). At git 5572eab ChatArea.tsx renders the message
// list container `.chat__messages` (a bounded, overflow-y:auto scroll box) with
// NO ref / useEffect / scrollIntoView — nothing ever drives it to the bottom. So
// when a conversation has more messages than fit on one screen, the thread opens
// pinned at the TOP (scrollTop === 0) and the latest message is off-screen below.
//
// This spec seeds a real conversation of ≥15 messages (via POST /api/chat), opens
// it, and asserts the CORRECT EXPECTATION: the thread is scrolled to the bottom
// (scrollTop + clientHeight ≈ scrollHeight). Against the current buggy build that
// expectation is false (it sits at the top), the body throws, and because it is
// wrapped in `test.fail()` a throwing body is the EXPECTED outcome — the spec
// PASSES (baseline red = bug confirmed present).
//
// LAND SIGNAL: once ChatArea gains autoscroll, the body stops throwing and
// Playwright reports "expected to fail but passed". When you see that, DELETE the
// `test.fail()` marker to flip this into a permanent green regression guard.
const { test, expect } = require('@playwright/test');

const BASE = process.env.OC_E2E_BASE || 'http://127.0.0.1:8791';
const PASSWORD = process.env.OC_E2E_PASSWORD || 'joey-e2e-local-pw';
const SEED_COUNT = 18; // > one screen height, and < the default GET /api/chat limit (30)

// Log in (real API) and return the owner token.
async function ownerToken(page) {
  const login = await page.request.post(`${BASE}/api/login`, {
    data: { password: PASSWORD },
  });
  expect(login.status(), 'login must succeed').toBe(200);
  const { token } = await login.json();
  expect(token, 'login must return an owner token').toBeTruthy();
  return token;
}

// Ensure exactly one assistant member exists to chat with (the office roster
// filters kind === "assistant", and on desktop the chat pane auto-opens
// roster[0]). Reuse an existing assistant if one is already seeded so re-runs stay
// idempotent; otherwise hire "Mira".
async function ensureAssistant(page, token) {
  const auth = { Authorization: `Bearer ${token}` };
  const listRes = await page.request.get(`${BASE}/api/members`, { headers: auth });
  const members = await listRes.json();
  const existing = members.find(
    (m) => m.kind === 'assistant' && m.roster_status !== 'removed',
  );
  if (existing) return existing.id;

  const hireRes = await page.request.post(`${BASE}/api/members`, {
    headers: auth,
    data: { name: 'Mira', kind: 'assistant' },
  });
  expect(hireRes.status(), 'hiring an assistant must succeed').toBe(200);
  return (await hireRes.json()).id;
}

// Seed the conversation to at least SEED_COUNT messages (owner → member) so the
// thread overflows one screen. The server stamps sender from the JWT sub, so we
// only supply { to, body }.
async function seedChat(page, token, memberId) {
  const auth = { Authorization: `Bearer ${token}` };
  const before = await (
    await page.request.get(
      `${BASE}/api/chat?with=${memberId}&limit=100`,
      { headers: auth },
    )
  ).json();
  for (let i = before.length; i < SEED_COUNT; i++) {
    const res = await page.request.post(`${BASE}/api/chat`, {
      headers: auth,
      data: {
        to: memberId,
        body: `autoscroll baseline message number ${i + 1} — padding line to overflow one screen height so the container needs to scroll`,
      },
    });
    expect(res.status(), 'posting a chat message must succeed').toBe(200);
  }
}

test.describe('B6 · chat — thread autoscrolls to the newest message', () => {
  // RED-vs-GREEN via env. With OC_E2E_BASELINE=1 (run against the pre-fix build,
  // e.g. 5572eab) the assertion below is expected to fail, so test.fail() makes a
  // throwing body the PASS condition (baseline red confirmed). Without the env (run
  // against the fixed build, or as the permanent green regression guard) test.fail()
  // is NOT armed, so the assertion must genuinely pass (green).
  if (process.env.OC_E2E_BASELINE === '1') test.fail();

  test('opening a long conversation lands scrolled to the bottom', async ({
    page,
  }) => {
    const token = await ownerToken(page);
    const memberId = await ensureAssistant(page, token);
    await seedChat(page, token, memberId);

    // Inject the token into localStorage (key `oc_token`, see api/auth.ts) and
    // boot the SPA already-authenticated — no login UI.
    await page.goto('/');
    await page.evaluate((t) => localStorage.setItem('oc_token', t), token);
    await page.reload();

    // The office tab is the default; on desktop the chat pane auto-opens the
    // roster[0] assistant (OfficePage.tsx: `selected = ... ?? roster[0]`). Wait
    // for the seeded messages to render into `.chat__messages`.
    const thread = page.locator('.chat__messages');
    await expect(thread, 'the chat message thread must render').toBeVisible();
    await expect
      .poll(async () => thread.locator('.chat__msg').count(), {
        message: 'the seeded messages must load into the thread',
      })
      .toBeGreaterThanOrEqual(SEED_COUNT);

    // Sanity: the thread MUST actually overflow, otherwise "scrolled to bottom" is
    // trivially true and the test would be meaningless (a false green). If this
    // fails, the seed count / viewport isn't producing an overflow — fix the setup
    // rather than trusting the autoscroll assertion.
    const metrics = await thread.evaluate((el) => ({
      scrollTop: el.scrollTop,
      clientHeight: el.clientHeight,
      scrollHeight: el.scrollHeight,
    }));
    expect(
      metrics.scrollHeight,
      'thread must overflow (scrollHeight > clientHeight) for the test to be meaningful',
    ).toBeGreaterThan(metrics.clientHeight + 1);

    // CORRECT EXPECTATION — an opened conversation should be pinned to the bottom
    // so the newest message is visible: scrollTop + clientHeight ≈ scrollHeight
    // (a few px tolerance for sub-pixel rounding). At baseline there is no
    // autoscroll, so the thread sits at the TOP (scrollTop ≈ 0) and this throws —
    // which test.fail() turns into the expected red. AFTER THE FIX this passes and
    // the test "unexpectedly passes", signalling it is time to drop test.fail().
    const TOLERANCE_PX = 4;
    const distanceFromBottom =
      metrics.scrollHeight - (metrics.scrollTop + metrics.clientHeight);
    expect(
      distanceFromBottom,
      `chat should autoscroll to the bottom; sat ${distanceFromBottom}px from bottom (scrollTop=${metrics.scrollTop})`,
    ).toBeLessThanOrEqual(TOLERANCE_PX);
  });
});
