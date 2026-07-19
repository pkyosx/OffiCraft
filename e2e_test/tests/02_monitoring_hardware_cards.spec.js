// e2e_test/tests/02_monitoring_hardware_cards.spec.js
// B1 · monitoring page — the machine table (§2) must render the per-machine
// hardware columns CPU / RAM / POWER.
//
// BASELINE = RED (known-failure). At git 5572eab the cockpit regression e331de8
// deleted the CPU/RAM/POWER columns from MonitorPage.tsx's machine table: the
// <thead> now emits only three headers (Machine / Status / Actions) and each row
// only renders name/status/actions cells. The whole data pipeline is still
// intact (wire.ts cpu_pct/ram_pct/battery_pct/ac_power → mappers.ts toMonMachine
// → types.ts MonMachineView) — only the VIEW layer lost the columns.
//
// So the assertion below is written as the CORRECT EXPECTATION (the table SHOULD
// expose a CPU column, a RAM column, and a POWER column). Against the current
// buggy build that expectation is false, so the test body throws — and because
// it is wrapped in `test.fail()`, a throwing body is the EXPECTED outcome and the
// spec PASSES (baseline red = bug confirmed present).
//
// LAND SIGNAL: once the fix re-adds the columns the body will stop throwing and
// Playwright reports "expected to fail but passed" for this test. When you see
// that, the bug is fixed — DELETE the `test.fail()` marker to flip it to a
// permanent green regression guard.
const { test, expect } = require('@playwright/test');

const BASE = process.env.OC_E2E_BASE || 'http://127.0.0.1:8791';
const PASSWORD = process.env.OC_E2E_PASSWORD || 'joey-e2e-local-pw';

// Log in via the real API and inject the owner token straight into localStorage
// (key `oc_token` — the frontend's single source of truth, see api/auth.ts),
// bypassing the login UI. Then reload so the SPA boots already-authenticated.
async function bootAuthed(page) {
  const login = await page.request.post(`${BASE}/api/login`, {
    data: { password: PASSWORD },
  });
  expect(login.status(), 'login must succeed to seed the owner token').toBe(200);
  const { token } = await login.json();
  expect(token, 'login must return an owner token').toBeTruthy();

  // Must land on the origin before touching its localStorage.
  await page.goto('/');
  await page.evaluate((t) => localStorage.setItem('oc_token', t), token);
  await page.reload();
}

test.describe('B1 · monitoring — machine hardware columns (CPU / RAM / POWER)', () => {
  // RED-vs-GREEN via env. With OC_E2E_BASELINE=1 (run against the pre-fix build,
  // e.g. 5572eab) the assertion below is expected to fail, so test.fail() makes a
  // throwing body the PASS condition (baseline red confirmed). Without the env (run
  // against the fixed build, or as the permanent green regression guard) test.fail()
  // is NOT armed, so the assertion must genuinely pass (green).
  if (process.env.OC_E2E_BASELINE === '1') test.fail();

  test('the machine table (§2) exposes CPU / RAM / POWER column headers', async ({
    page,
  }) => {
    await bootAuthed(page);

    // Enter the monitoring cockpit. The app is tab-based (no URL route): the
    // "monitor" nav tab swaps <main> to <MonitorPage>. Click it by its icon+label
    // nav button (App.tsx renders t.nav.monitor inside a .nav-tab button).
    await page.getByRole('button', { name: /監控|monitor/i }).click();

    // The §2 machine table is `table.mon-table` (MonitorPage.tsx:319). Scope every
    // assertion to its header row so we test the machine table specifically and
    // never accidentally match the session table below it.
    const machineTable = page.locator('table.mon-table').first();
    await expect(machineTable, 'the §2 machine table must render').toBeVisible();
    const headerRow = machineTable.locator('thead tr');

    // CORRECT EXPECTATION — the machine table SHOULD carry a CPU, a RAM, and a
    // POWER column so each machine's live hardware is visible. "CPU"/"RAM" are the
    // same string in every locale; POWER is 電源 (zh, the default) / 靈源 (xian) /
    // Power (en), so match any of them. At baseline these headers do not exist, so
    // each of these assertions throws — which test.fail() turns into the expected
    // red. AFTER THE FIX these pass and the whole test "unexpectedly passes",
    // signalling it is time to drop test.fail().
    await expect(
      headerRow.getByText('CPU', { exact: true }),
      'machine table should have a CPU column header',
    ).toBeVisible();
    await expect(
      headerRow.getByText('RAM', { exact: true }),
      'machine table should have a RAM column header',
    ).toBeVisible();
    await expect(
      headerRow.getByText(/^(電源|靈源|Power)$/),
      'machine table should have a POWER column header',
    ).toBeVisible();
  });
});
