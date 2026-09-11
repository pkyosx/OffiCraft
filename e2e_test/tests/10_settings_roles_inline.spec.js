// ─────────────────────────────────────────────────────────────────────────────
// 🔴 T-10 · HOW TO TELL WHETHER A GATE IS DRAWN IN THE RIGHT PLACE
//
// Learned expensively across this file and its sibling (the other of
// MonitorPage.mutation-reconcile.test.tsx / e2e_test/tests/
// 10_settings_roles_inline.spec.js): three separate gates were written,
// reviewed and shipped while guarding something narrower than what they said
// they guarded.
//
// THE RULE. A gate's assertion message IS its specification. If a test double
// exists that makes the message FALSE while the assertion still PASSES, the
// gate is drawn in the wrong place — however sensible the expression it
// evaluates happens to look.
//
// HOW TO APPLY IT. Write the property as one sentence (the message usually is
// that sentence). Then list EVERY line that sentence depends on and build a
// double for each. The expression the gate itself reads is only ONE of those
// lines. The three worked examples, all from this ticket:
//   • "the fake must have captured subscribers" → `.length > 0` passed happily
//     on a store-last-only fake, which was precisely the bug it was added to
//     catch. Guarded "not zero"; claimed "the right one".
//   • "must FAN OUT to EVERY subscriber" → `.length > 1` still passed when the
//     emit one line below delivered to `handlers[0]` only. The sentence
//     depended on the emit; the enumeration had covered only the length.
//   • "No later poll, at any speed, can satisfy this" → true of polls, false of
//     the frame-triggered fourth request, which is neither a poll nor later.
//
// SCOPE: assertion messages AND comments, equally. The third example was a
// comment, and a confidently wrong comment is worse than none — the next
// maintainer acts on it. Anything a maintainer will act on is in scope.
//
// 🔴 THE LIMITATION, UNVARNISHED. This rule has caught three cases here, and in
// two of them it only fired because a SECOND person applied it. The author of
// these tests had already adopted the rule, applied it to his own gate, and
// still shipped the fan-out hole — he enumerated doubles for the expression the
// gate read and not for the line directly beneath it. So it is an effective
// REVIEW check and is NOT reliable as an author self-check. If the only person
// who has ever run doubles against a gate is the person who wrote it, that gate
// has not actually been checked yet.
// ─────────────────────────────────────────────────────────────────────────────
// e2e_test/tests/10_settings_roles_inline.spec.js
// B10 · 角色誌/監控 inline create rows + role rename/reset gating (M2-2 batch:
// 8d947d1 / 23d8063 / cee768f / 49d1930 / 3f88128).
//
//   • 角色誌「新增角色定義」: the add entry grows a single-field inline row
//     (name only); the server names the founding member itself and mints both
//     ids; Esc collapses without creating.
//   • CUSTOM role detail: the 角色名 gets the pencil InlineEdit (rename rides
//     the role PATCH choke and the roster follows); the 版本紀錄 list carries NO
//     初始版本 row (the server 404s a custom reset — the affordance is honestly
//     omitted); NO internal `role-….md` filename chip.
//   • SEED role detail: name LOCKED (no pencil), the 版本紀錄 list DOES carry
//     初始版本 (a file seed exists to restore).
//
// The reset affordance moved at d0c2ea3 (T-1f39): the doc card's standalone
// 重置 became 版本紀錄, and the reset is that list's last row.
//   • 監控「新增機器 / 上線」: the same inline pattern — the machine row is
//     created under the typed name (no real warden needed; the onboard row
//     surfaces immediately), Esc collapses without creating.
const { test, expect } = require('@playwright/test');
const {
  BASE,
  authHeaders,
  ownerToken,
  listMembers,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

// English names on purpose: the inline rows guard IME composition (keyCode 229)
// — a CJK Enter would be swallowed as a candidate-confirm, not a submit.
const ROLE_NAME = uniqueName('Test Officer');
const ROLE_RENAMED = uniqueName('Chief Tester');
const MACHINE_NAME = uniqueName('e2e-box');

async function openSettings(page) {
  await page.locator('button[aria-label="settings"]').click();
  await expect(page.locator('.settings__title')).toBeVisible();
}

test.describe('B10 · settings roles + monitor — inline create rows & gating', () => {
  test('角色誌: inline create → server-named founding member; rename custom / lock seed; reset seed-only', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    await bootAuthedSpa(page, token);
    await openSettings(page);
    await page.locator('.set-entry', { hasText: '角色誌' }).click();

    // ── negative first: Esc collapses the inline row without creating ──
    await page.locator('.add-entry').click();
    const row = page.getByTestId('role-create-row');
    await expect(row, 'the add entry must grow the inline row').toBeVisible();
    await page.getByTestId('role-create-name').fill('ShouldNeverExist');
    await page.getByTestId('role-create-name').press('Escape');
    await expect(row, 'Esc must collapse the row').toHaveCount(0);
    const rolesAfterEsc = await (
      await request.get(`${BASE}/api/roles`, { headers: authHeaders(token) })
    ).json();
    expect(
      rolesAfterEsc.find((r) => r.name === 'ShouldNeverExist'),
      'Esc must not create anything',
    ).toBeUndefined();

    // ── inline create: name only, Enter submits ──
    await page.locator('.add-entry').click();
    await page.getByTestId('role-create-name').fill(ROLE_NAME);
    await page.getByTestId('role-create-name').press('Enter');
    const roleEntry = page.locator('.set-entry', { hasText: ROLE_NAME });
    await expect(roleEntry, 'the new custom role must surface in the list').toBeVisible();

    // API對照: the server minted the role AND its ONE founding member, naming
    // the member ITSELF from the name pool (never the role name, never blank).
    const roles = await (
      await request.get(`${BASE}/api/roles`, { headers: authHeaders(token) })
    ).json();
    const role = roles.find((r) => r.name === ROLE_NAME);
    expect(role, 'the custom role must exist server-side').toBeTruthy();
    expect(role.key, 'the server mints the role key').toMatch(/^r-[0-9a-f]{12}$/);
    const founding = (await listMembers(request, token)).find(
      (m) => m.role_key === role.key,
    );
    expect(founding, 'the founding member must exist').toBeTruthy();
    expect(founding.name, 'the server names the member itself').not.toBe('');
    expect(founding.name, 'the member name is not the role name').not.toBe(ROLE_NAME);
    expect(founding.role_name, "the member's role_name follows the role").toBe(ROLE_NAME);

    // ── custom role detail: pencil rename, no reset, no internal filename ──
    await roleEntry.click();
    const title = page.locator('.settings__title--doc');
    await expect(title).toContainText(ROLE_NAME);
    // no internal `role-….md` filename chip anywhere on the page
    await expect(
      page.locator('.doc-card__file'),
      'the internal role file name must never render',
    ).not.toContainText(/role-.*\.md/);
    // enter doc edit mode: a CUSTOM role offers 儲存/取消 but NO 重置.
    // Scope to the ROLE doc card. The Insight card renders as DocCard's `extra`,
    // a SIBLING of .doc-card, so its own edit affordance is outside this subtree
    // already. (It used to be the per-role LessonsCard, removed in T-186.)
    const roleDocCard = page.locator('.doc-card').first();
    await roleDocCard.locator('.doc-btn--edit').click();
    // d0c2ea3 (T-1f39) removed the standalone 重置 button from the doc card and
    // put 版本紀錄 in its place; the reset became the version list's LAST row,
    // 初始版本 (`doc-history-seed`), rendered only where `onReset` is wired.
    // ⚠ So asserting a MISSING 重置 button here is now vacuously green — that
    // node no longer exists for ANY document. The seed-only gating is asserted
    // where it actually lives: open the list and demand the 初始版本 row be
    // absent for a custom role (SettingsPage passes onReset only when isSeed).
    await roleDocCard.getByTestId('doc-history-entry-role_definition').click();
    const customHistory = roleDocCard.getByTestId('doc-history-list');
    await expect(customHistory, 'the 版本紀錄 list must open').toBeVisible();
    await expect(
      customHistory.getByTestId('doc-history-seed'),
      'a custom role has no file seed to restore — the 初始版本 row must be absent',
    ).toHaveCount(0);
    await customHistory.getByTestId('doc-history-list-close').click();
    await expect(customHistory).toHaveCount(0);
    await roleDocCard.locator('.doc-card__actions .doc-btn', { hasText: '取消' }).click();
    // rename via the title pencil InlineEdit
    await title.locator('.inline-edit__iconbtn').click();
    const nameInput = title.locator('.inline-edit__input');
    await nameInput.fill(ROLE_RENAMED);
    await nameInput.press('Enter');
    await expect(title, 'the title must show the committed rename').toContainText(ROLE_RENAMED);
    // roster follows (single truth role.name): the founding member's card in
    // the office roster reads the NEW role name.
    // T-8f6e removed the ‹返回 back row — navigate up via the shared breadcrumb
    // (nav.crumbs; the 角色誌 parent segment jumps back to the roles list).
    await page.locator('nav.crumbs .crumbs__seg', { hasText: '角色誌' }).click();
    await expect(
      page.locator('.set-entry', { hasText: ROLE_RENAMED }),
      'the roles list must show the renamed role',
    ).toBeVisible();
    await page.locator('.nav-tab', { hasText: '辦公室' }).click();
    await expect(
      page
        .locator('.member-card', { hasText: founding.name })
        .locator('.member-card__presence'),
      "the roster member card's role label must follow the rename",
    ).toContainText(ROLE_RENAMED);

    // ── seed role (特助): locked name (no pencil), 重置 offered ──
    // Matched on the role's own LABEL. It used to be '助理', which came from the
    // persona's first body line — the roster row printed a one-line preview of
    // `definition_md`. T-1170 removed both the field from the list answer and
    // the preview from the row (owner-approved, card rc-a86771f4476f), so that
    // text is no longer on this page at all.
    await openSettings(page);
    await page.locator('.set-entry', { hasText: '角色誌' }).click();
    await page.locator('.set-entry', { hasText: '特助' }).first().click();
    await expect(page.locator('.settings__title--doc')).toBeVisible();
    await expect(
      page.locator('.settings__title--doc .inline-edit__iconbtn'),
      'a seed role name is locked — no rename pencil',
    ).toHaveCount(0);
    const seedDocCard = page.locator('.doc-card').first();
    await seedDocCard.locator('.doc-btn--edit').click();
    await seedDocCard.getByTestId('doc-history-entry-role_definition').click();
    const seedHistory = seedDocCard.getByTestId('doc-history-list');
    await expect(seedHistory, 'the 版本紀錄 list must open').toBeVisible();
    await expect(
      seedHistory.getByTestId('doc-history-seed'),
      'a seed role has a file seed to restore — the 初始版本 row must be offered',
    ).toBeVisible();
  });

  test('監控: 新增機器/上線 inline row creates the named machine; Esc collapses', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    await bootAuthedSpa(page, token);
    await page.locator('.nav-tab', { hasText: '監控' }).click();

    // negative: Esc collapses without creating
    await page.locator('#mon-onboard-entry').click();
    const row = page.getByTestId('mon-onboard-row');
    await expect(row).toBeVisible();
    await row.locator('input').fill('ghost-machine');
    await row.locator('input').press('Escape');
    await expect(row, 'Esc must collapse the onboard row').toHaveCount(0);
    const machinesAfterEsc = await (
      await request.get(`${BASE}/api/machines`, { headers: authHeaders(token) })
    ).json();
    expect(
      machinesAfterEsc.find((m) => m.display_name === 'ghost-machine'),
      'Esc must not onboard anything',
    ).toBeUndefined();

    // create: type the name, Enter → the machine row surfaces under that name
    // (no real warden needed — the registry row exists immediately, offline).
    await page.locator('#mon-onboard-entry').click();
    await row.locator('input').fill(MACHINE_NAME);
    await row.locator('input').press('Enter');
    await expect(row, 'a successful create collapses the row').toHaveCount(0, {
      timeout: 10_000,
    });

    // ── T-10: both assertions below are anchored so the 5s trailing poll can
    // NOT be what satisfies them. Without that, this step is a luck detector.
    //
    // MonitorPage's onboard awaits `refetchMachines()` and only THEN collapses
    // the row, so under a correct hook the new row is already committed when
    // the collapse above passes — required latency is zero. When the hook
    // instead discards that refetch (the T-10 defect), the row arrives only on
    // the trailing poll, and both assertions must fail rather than wait it out.
    //
    // Measured on this suite's own flow (fresh station, /api/settings reports
    // monitoring_refresh_seconds=5): the row collapses ~0.25s after mount and
    // the trailing poll lands ~5.1s after mount — a gap of 4907/4950/5019ms
    // over three runs. Playwright's default expect timeout is 5000ms (this
    // config sets no `expect.timeout`), so the old bare `toContainText` was
    // racing that poll to within ~50ms and usually LOSING the race in the
    // defect's favour — i.e. going green on a broken hook. The CI trace on
    // run 33033163627 is the same race landing 4ms the other way.
    //
    // (1) The causal anchor: read the table at the moment of collapse. No later
    //     POLL, at any speed, can retroactively satisfy this — but see the
    //     KNOWN GAP below, because a poll is not the only other supplier.
    //
    // 🔴 KNOWN GAP, NAMED RATHER THAN PAPERED OVER. Neither assertion here has
    //    a request-count gate, and there is a fourth request that is neither a
    //    poll nor later. `useMachines`' schedule() computes
    //    `delay = max(0, refreshSeconds*1000 - (now - lastStarted))`, and the
    //    timer callback's `inFlight` guard is set only by the effect's own
    //    refresh, never by a manual `refetch()`. So once >= refreshSeconds has
    //    elapsed since the last effect refresh, the member frame fires a real
    //    GET IMMEDIATELY, alongside the in-flight refetch, and that GET's own
    //    answer already contains the new row.
    //
    //    Measured 2026-08-27 in this browser, with a 6s idle before onboarding
    //    and only the reconciling GET held open: reconciling GET [6187, 7689]
    //    (still in flight), frame at 6324, extra GET [6324, 6326], row on
    //    screen at 6352 — and the inline row did not collapse until 7964. The
    //    row beat the collapse by 1.6 SECONDS. So against this fourth request
    //    the "read the table at collapse" anchor is worth exactly ZERO, not
    //    "a narrow margin": on a broken hook both assertions here would pass
    //    DETERMINISTICALLY, not occasionally.
    //
    //    Why it is left as a gap rather than fixed here: this test's job is the
    //    inline-row UX (Esc collapses, Enter creates, the registry agrees), and
    //    bolting the full request-accounting apparatus onto it would bury that.
    //    The flow above idles ~250ms before onboarding — roughly 20x under the
    //    5s threshold — so the fourth request is unreachable on this path today.
    //    That is a property of the current step ordering, not a guarantee: put a
    //    >5s wait anywhere above and this silently stops holding.
    //    The DETERMINISTIC T-10 guard is the forced-overlap test at the bottom
    //    of this file, which does carry the request-count gate (gate (0)).
    const tableAtCollapse = await page
      .locator('.mon-table, table')
      .first()
      .innerText();
    expect(
      tableAtCollapse,
      'the new row must already be in the table when the inline row collapses — ' +
        'arriving later means the create refetch was discarded and only the 5s poll repaired it',
    ).toContain(MACHINE_NAME);

    // (2) The retrying assertion, kept for a genuinely slow render, but with an
    //     explicit budget FAR below the ~4.9s poll gap. 2s is ~2900ms of
    //     clearance under the poll while still granting 2s to a DOM check that
    //     is already satisfied, so it cannot turn into a new false RED — the
    //     failure mode this whole ticket must not create.
    await expect(
      page.locator('.mon-table, table').first(),
      'the machine table must show the new row under the typed name',
    ).toContainText(MACHINE_NAME, { timeout: 2_000 });
    // API對照: the registry carries it (created via POST /api/machines).
    const machines = await (
      await request.get(`${BASE}/api/machines`, { headers: authHeaders(token) })
    ).json();
    expect(
      machines.find((m) => m.display_name === MACHINE_NAME),
      'the machine registry must carry the onboarded machine',
    ).toBeTruthy();
  });

  // ── T-10 deterministic regression guard — RETIRED 2026-08-27 ────────────
  // A forced-overlap case used to live here. It held the reconciling GET open
  // 400ms so the onboard's own `member` frame would land INSIDE it, turning the
  // ~1%-of-runs T-10 race into a certainty, and then asserted the row was on
  // screen at the instant the inline row collapsed.
  //
  // It was removed on the owner's ruling, and the reason is worth keeping:
  //
  //   > 「這個問題不是很重要 晚一點到又怎麼了」
  //   > 「regression 的價值在於被保護的對象的重要性」
  //
  // A race cannot be commanded, only observed — so the case carried an
  // anti-vacuity gate that reddened when the staging did not happen ("this run
  // proves nothing"). That gate was correct and it fired on main the very first
  // time (run 33050066316 on 013de9c3: the frame landed at t=337.4 while the
  // reconciling GET ran [360, 774] — it beat the window by 23ms because the POST
  // itself outran the frame's delivery). Nothing was wrong with the product.
  // But an intermittent red nobody can act on is exactly the thing that teaches
  // everyone to re-run first, which is the defect class this repo is actively
  // trying to remove — so a guard over a five-second cosmetic delay is not worth
  // paying for it.
  //
  // WHAT STILL GUARDS T-10 (do not read this as "unguarded"):
  //   • the unit layer — useMachines.test.ts / useMonitoring.enabled.test.ts /
  //     useMonitoring.sse-invalidation.test.ts. Four NAMED assertions, driven
  //     with refreshSeconds pushed to an hour and no timer ever advanced, so the
  //     only thing that can put the row on screen is the in-flight answer
  //     itself. Deterministic, no clock, no flake. Re-introducing the defect
  //     reddens all four (measured).
  //   • the case ABOVE — it asserts the row is present at the moment the inline
  //     row collapses, which the 5s trailing poll cannot satisfy. It is a
  //     CORRECT detector but only samples the race when it flips naturally
  //     (~1%), so treat it as a bonus, not as the guard.
  //
  // If the cost of this bug ever rises (e.g. onboard stops being the only path
  // through it), the removed case is recoverable from git history on this file.
});
