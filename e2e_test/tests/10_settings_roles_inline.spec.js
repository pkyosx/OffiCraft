// e2e_test/tests/10_settings_roles_inline.spec.js
// B10 · 角色誌/監控 inline create rows + role rename/reset gating (M2-2 batch:
// 8d947d1 / 23d8063 / cee768f / 49d1930 / 3f88128).
//
//   • 角色誌「新增角色定義」: the add entry grows a single-field inline row
//     (name only); the server names the founding member itself and mints both
//     ids; Esc collapses without creating.
//   • CUSTOM role detail: the 角色名 gets the pencil InlineEdit (rename rides
//     the role PATCH choke and the roster follows); NO 重置 button (the server
//     404s a custom reset — the affordance is honestly omitted); NO internal
//     `role-….md` filename chip.
//   • SEED role detail: name LOCKED (no pencil), 重置 offered (a file seed
//     exists to restore).
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
    // Scope to the ROLE doc card (.first()) — the page also renders the shared
    // per-role LessonsCard below, which has its own edit affordance.
    const roleDocCard = page.locator('.doc-card').first();
    await roleDocCard.locator('.doc-btn--edit').click();
    await expect(
      roleDocCard.locator('.doc-card__actions .doc-btn', { hasText: '重置' }),
      'a custom role has no seed to restore — 重置 must be absent',
    ).toHaveCount(0);
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

    // ── seed role (助理): locked name (no pencil), 重置 offered ──
    await openSettings(page);
    await page.locator('.set-entry', { hasText: '角色誌' }).click();
    await page.locator('.set-entry', { hasText: '助理' }).first().click();
    await expect(page.locator('.settings__title--doc')).toBeVisible();
    await expect(
      page.locator('.settings__title--doc .inline-edit__iconbtn'),
      'a seed role name is locked — no rename pencil',
    ).toHaveCount(0);
    const seedDocCard = page.locator('.doc-card').first();
    await seedDocCard.locator('.doc-btn--edit').click();
    await expect(
      seedDocCard.locator('.doc-card__actions .doc-btn', { hasText: '重置' }),
      'a seed role has a file seed to restore — 重置 must be offered',
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
    await expect(
      page.locator('.mon-table, table').first(),
      'the machine table must show the new row under the typed name',
    ).toContainText(MACHINE_NAME);
    // API對照: the registry carries it (created via POST /api/machines).
    const machines = await (
      await request.get(`${BASE}/api/machines`, { headers: authHeaders(token) })
    ).json();
    expect(
      machines.find((m) => m.display_name === MACHINE_NAME),
      'the machine registry must carry the onboarded machine',
    ).toBeTruthy();
  });
});
