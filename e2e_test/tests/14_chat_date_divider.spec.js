// e2e_test/tests/14_chat_date_divider.spec.js
// C1 · chat day divider — the LINE/Slack-style calendar-day separator that
// ChatArea renders BETWEEN messages of different local calendar days.
//
// Frontend (already implemented): ChatArea.tsx splits the thread with
// splitByDay() and renders, per day group,
//   <div className="chat__day-divider"><span className="chat__day-pill">
//     {formatDayLabel(dayTs, now, t.chat)}</span></div>
// (frontend/src/lib/dateFormat.ts). The default locale is zh → the pill reads
// 今天 / 昨天 / a dated fallback (frontend/src/i18n/locales/zh.ts).
//
// POST /api/chat can NOT backdate (the server always stamps `now`), so a
// cross-day thread is impossible to build over the wire. This spec SEEDS two
// messages spanning yesterday→today by a DIRECT SQLITE INSERT into the
// isolated DB (chat_message table: id, sender, recipient, body, ts REAL, meta)
// BEFORE booting the SPA, then asserts the render groups them under two day
// dividers with the older group above the 今天 divider.
const { test, expect } = require('@playwright/test');
const { execFileSync } = require('child_process');
const path = require('path');
const fs = require('fs');
const {
  BASE,
  ownerToken,
  hireMember,
  mintMemberToken,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

// Resolve the isolated sqlite file. oc.toml's [storage].dsn maps onto
// $REPO_ROOT/var/data/<file>.db (server/ocserverd/config.go: the DSN default
// is var/data/officraft.db; setup.sh's example uses var/data/e2e.db). We
// glob var/data/*.db so either filename resolves — the isolated suite wipes
// var/data on setup, so exactly one db is present.
function isolatedDbPath() {
  const dataDir = path.resolve(__dirname, '..', '..', 'var', 'data');
  const dbs = fs
    .readdirSync(dataDir)
    .filter((f) => f.endsWith('.db'))
    .map((f) => path.join(dataDir, f));
  expect(
    dbs.length,
    `exactly one isolated sqlite db must live under ${dataDir}`,
  ).toBeGreaterThan(0);
  return dbs[0];
}

// Direct INSERT of one chat_message row via the sqlite3 CLI (a required harness
// tool; preferred over a node driver that may not be installed). `ts` is REAL
// epoch-seconds — exactly what dayStartOf()/splitByDay() consume. id follows
// the server's c-<hex> convention so nothing downstream trips on the shape.
function seedChatMessage(dbPath, { id, sender, recipient, body, ts }) {
  const esc = (s) => String(s).replace(/'/g, "''");
  const sql =
    `INSERT INTO chat_message (id, sender, recipient, body, ts, meta) VALUES ` +
    `('${esc(id)}', '${esc(sender)}', '${esc(recipient)}', '${esc(body)}', ${ts}, '{}');`;
  execFileSync('sqlite3', [dbPath, sql], { encoding: 'utf8' });
}

// Local-midnight epoch seconds of the day containing `sec` (mirrors
// dateFormat.ts dayStartOf — LOCAL time, exactly the boundary the UI uses).
function dayStartOf(sec) {
  const d = new Date(sec * 1000);
  d.setHours(0, 0, 0, 0);
  return Math.floor(d.getTime() / 1000);
}

test.describe('C1 · chat day divider — cross-day thread renders 今天/昨天 dividers grouping messages by local calendar day', () => {
  test('a thread spanning yesterday→today (seeded by direct sqlite INSERT) renders two day dividers, older message above the 今天 divider', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, uniqueName('DayDivider M'));
    // Mint (unused for the seed, but keeps the member a real, addressable peer
    // and mirrors the other specs' member setup).
    await mintMemberToken(request, token, M.id, 1);

    // ── SEED two cross-day messages by DIRECT sqlite INSERT (POST can't
    // backdate). One at yesterday's local midnight, one at "now" (today). The
    // owner is the wire peer 'owner'; the member M is sender/recipient so the
    // thread (with=M) contains exactly these two rows.
    const nowSec = Math.floor(Date.now() / 1000);
    const yesterdaySec = dayStartOf(nowSec) - 86_400; // yesterday's local midnight
    const dbPath = isolatedDbPath();

    const suffix = uniqueName('C1'); // unique bodies → unambiguous locators
    const yesterdayBody = `昨天的訊息 ${suffix}`;
    const todayBody = `今天的訊息 ${suffix}`;
    // Guard: the two seed points must land on DIFFERENT local calendar days
    // (they do by construction — one full day apart — but pin it so a clock
    // edge never silently collapses the assertion).
    expect(
      dayStartOf(yesterdaySec),
      'the two seed messages must fall on different local calendar days',
    ).not.toBe(dayStartOf(nowSec));

    seedChatMessage(dbPath, {
      id: `c-c1yest${Date.now().toString(36)}`,
      sender: M.id,
      recipient: 'owner',
      body: yesterdayBody,
      ts: yesterdaySec,
    });
    seedChatMessage(dbPath, {
      id: `c-c1today${Date.now().toString(36)}`,
      sender: M.id,
      recipient: 'owner',
      body: todayBody,
      ts: nowSec,
    });

    // Sanity: the isolated server serves both seeded rows over its list API
    // (with=M) — proves the direct INSERT landed in the live DB the SPA reads.
    const listRes = await request.get(`${BASE}/api/chat?with=${M.id}&limit=100`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(listRes.status(), 'listing the seeded thread must succeed').toBe(200);
    const listed = await listRes.json();
    const bodies = listed.map((m) => m.body);
    expect(bodies, 'the yesterday seed must be listed').toContain(yesterdayBody);
    expect(bodies, 'the today seed must be listed').toContain(todayBody);

    // ── boot the cockpit as the owner and open M's conversation ──
    await bootAuthedSpa(page, token);
    const card = page.locator('.member-card', { hasText: M.name });
    await expect(card).toBeVisible();
    await card.click();

    const thread = page.locator('.chat__messages');
    await expect(thread).toBeVisible();
    const yMsg = thread.locator('.chat__msg', { hasText: yesterdayBody });
    const tMsg = thread.locator('.chat__msg', { hasText: todayBody });
    await expect(yMsg, 'the yesterday message must render').toBeVisible();
    await expect(tMsg, 'the today message must render').toBeVisible();

    // ── the core assertion: TWO day dividers (one per calendar day) ──
    const dividers = thread.locator('.chat__day-divider');
    await expect(
      dividers,
      'a two-day thread must render exactly two day dividers (one per local calendar day)',
    ).toHaveCount(2);
    const pills = thread.locator('.chat__day-pill');
    await expect(pills).toHaveCount(2);

    // Default locale is zh → the pills read 昨天 (older group) then 今天
    // (newer group), top-to-bottom.
    const pillTexts = await pills.allInnerTexts();
    expect(
      pillTexts.map((s) => s.trim()),
      'the two day pills must be 昨天 then 今天 (older group above newer)',
    ).toEqual(['昨天', '今天']);

    // ── grouping: the yesterday message sits UNDER the 昨天 divider and ABOVE
    // the 今天 divider; the today message sits UNDER the 今天 divider. We assert
    // vertical order in the rendered DOM via bounding boxes.
    const yesterdayPill = thread.locator('.chat__day-pill', { hasText: '昨天' });
    const todayPill = thread.locator('.chat__day-pill', { hasText: '今天' });
    await expect(yesterdayPill).toBeVisible();
    await expect(todayPill).toBeVisible();

    const topOf = async (loc) => (await loc.boundingBox()).y;
    const yPillY = await topOf(yesterdayPill);
    const yMsgY = await topOf(yMsg);
    const tPillY = await topOf(todayPill);
    const tMsgY = await topOf(tMsg);

    expect(yPillY, '昨天 divider must be above the yesterday message').toBeLessThan(
      yMsgY,
    );
    expect(
      yMsgY,
      'the yesterday message must sit ABOVE the 今天 divider (grouped under 昨天)',
    ).toBeLessThan(tPillY);
    expect(tPillY, '今天 divider must be above the today message').toBeLessThan(
      tMsgY,
    );
  });
});
