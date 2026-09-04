// e2e_test/tests/04_session_display_names.spec.js
// B2/B3 · monitoring — the session row's machine + account must resolve to the
// owner's friendly display names, not leak raw ids.
//
// BASELINE = RED (known-failure via env). At 5572eab handle_get_monitoring built
// the session DTO with machine=m.host and account=<raw tag> — a raw id straight
// through, even when the owner had set a display-name overlay. At c2b4fe2 both go
// through a single resolve_display(overlay, raw) helper (the same one the
// machines/accounts sections use), so an owner overlay surfaces as the friendly
// label on the session row.
//
// This is a pure-API check (no browser): the resolve happens server-side in the
// /api/monitoring DTO, so we drive it through APIRequestContext.
//
// RED-vs-GREEN via env: OC_E2E_BASELINE=1 (pre-fix build) → the assertion fails
// and test.fail() makes that the pass condition (red confirmed). Without the env
// (fixed build / permanent green guard) the assertion must genuinely pass.
const { test, expect } = require('@playwright/test');

const BASE = process.env.OC_E2E_BASE || 'http://127.0.0.1:8791';
const PASSWORD = process.env.OC_E2E_PASSWORD || 'joey-e2e-local-pw';

const RAW_ACCOUNT = 'acct-raw-e2e';
const MACHINE_LABEL = 'Seth MBP (e2e)';
const ACCOUNT_LABEL = 'Team Account (e2e)';

test.describe('B2/B3 · monitoring session — machine/account friendly names', () => {
  // RED-vs-GREEN via env: armed only for the baseline (pre-fix) run.
  if (process.env.OC_E2E_BASELINE === '1') test.fail();

  test('an owner display-name overlay surfaces as friendly names on the session row', async ({
    request,
  }) => {
    const login = await request.post(`${BASE}/api/login`, { data: { password: PASSWORD } });
    expect(login.status(), 'login must succeed').toBe(200);
    const { token } = await login.json();
    const auth = { Authorization: `Bearer ${token}` };

    // Use the seeded assistant (mira). `observedHost` (api_helpers.go) resolves a
    // member's raw machine from OBSERVATION ONLY: SSE machine claim →
    // telemetry.machine → "". 🔴 There is NO desired_machine_id fallback — 67a762e
    // (T-7f28) removed it on purpose, so an unobserved member reads honest-empty
    // rather than borrowing the owner's intent. `member.host` was removed at
    // 948c7d1 (observed position is not a durable field).
    //
    // mira is NOT online here, so nothing claims a machine over SSE. This spec
    // therefore has to supply the OBSERVATION itself — see the telemetry POST
    // below, which carries `machine` so observedHost's second arm resolves.
    // `desired_machine_id` is read only to KNOW which host id to label.
    const members = await (
      await request.get(`${BASE}/api/members`, { headers: auth })
    ).json();
    const member = members.find((m) => m.kind === 'staff' && m.roster_status !== 'removed');
    expect(member, 'a seed assistant must exist to own a session row').toBeTruthy();
    const host = member.machine || member.desired_machine_id;
    expect(host, 'the seed assistant must name a machine id (observed, else its pin)').toBeTruthy();

    // Report telemetry so a session row exists for this member carrying an account
    // tag. caller-identity convention (948c7d1, docs/design/caller-identity-
    // convention.md): the telemetry entry is keyed by the CALLER (verified JWT sub),
    // never a self-reported agent_id (that param was removed) — an agent can only
    // report its OWN telemetry. So to land the account tag ON mira we must report AS
    // mira: mint mira's agent-scope token and ingest with it (no agent_id in body).
    // Reporting with the owner token would key the entry to `owner`, leaving mira's
    // session row account empty.
    const mint = await request.post(`${BASE}/api/mint`, {
      headers: auth,
      data: { member_id: member.id, ttl_days: 1 },
    });
    expect(mint.status(), "mint mira's agent token must succeed").toBe(200);
    const { token: miraToken } = await mint.json();
    // `machine` rides the body on purpose: api_monitoring.go attributes the entry
    // from the token's machine_id claim FIRST and only falls back to the payload
    // for claim-less tokens — and /api/mint tokens are exactly that (no machine_id
    // claim), so this arm is the one that fires. Without it the entry carries no
    // machine, observedHost returns "" for this offline member, and the session
    // row's machine cell is honestly blank.
    //
    // `runtime` is equally load-bearing, and for a different reason: account
    // identity is runtime-specific, so applyAccountReport (account_display.go)
    // stores the key ONLY together with a provenance stamp, and telemetryAccount
    // serves it back only when that stamp matches the actor's own runtime. An
    // account reported WITHOUT a runtime is unprovable and fail-closed dropped
    // (rule 2) — which reads back as an empty account cell, not an error. Every
    // shipped reporter sends the pair; this spec must too. It reports the
    // member's OWN runtime so the stamp matches what the read path compares to.
    const tele = await request.post(`${BASE}/api/monitoring/telemetry`, {
      headers: { Authorization: `Bearer ${miraToken}` },
      data: {
        account: RAW_ACCOUNT,
        cost: 1.0,
        machine: host,
        runtime: member.runtime || 'claude',
      },
    });
    expect(tele.status(), 'telemetry ingest (as mira) must succeed').toBe(200);

    // Overlay friendly display names onto the raw host + raw account (PATCH upserts).
    const pm = await request.patch(`${BASE}/api/machines/${host}`, {
      headers: auth,
      data: { display_name: MACHINE_LABEL },
    });
    expect(pm.status(), 'machine display-name overlay must succeed').toBe(200);
    const pa = await request.patch(`${BASE}/api/accounts/${RAW_ACCOUNT}`, {
      headers: auth,
      data: { display_name: ACCOUNT_LABEL },
    });
    expect(pa.status(), 'account display-name overlay must succeed').toBe(200);

    // GET monitoring — the session row must show the FRIENDLY names.
    const mon = await (
      await request.get(`${BASE}/api/monitoring`, { headers: auth })
    ).json();
    const row = (mon.sessions || []).find(
      (s) => s.id === member.id || s.name === member.name,
    );
    expect(row, 'the session row for the member must be present').toBeTruthy();

    // CORRECT EXPECTATION — machine + account resolve to the owner's labels.
    // Baseline leaks the raw host / raw account tag here (red); c2b4fe2 resolves
    // both through resolve_display (green).
    expect(
      row.machine,
      `session machine should resolve to the friendly name, got "${row.machine}"`,
    ).toBe(MACHINE_LABEL);
    expect(
      row.account,
      `session account should resolve to the friendly name, got "${row.account}"`,
    ).toBe(ACCOUNT_LABEL);
  });
});
