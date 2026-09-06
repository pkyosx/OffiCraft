// e2e_test/tests/09_member_wire_shape.spec.js
// D1 · Member read-face wire-shape seal (API-only — runs fine under
// OC_E2E_SKIP_BUILD=1).
//
// WHY: the M2 wire rename `status` → `roster_status` (cb61f9f) and the wire
// slim-down that removed `online` / `waking_since` / `stopping_timed_out`
// (b8f883f — FE only consumes the derived `presence`) have NO e2e seal: the FE
// mapper carries `?? 0` / fallback defaults, so a silent server-side rollback
// of the wire shape would not turn any existing spec red. This spec pins the
// contract AT THE WIRE:
//   • every roster row carries `roster_status` (∈ {active, removed}),
//     `presence` (string), and `unread_count` (a number, ALWAYS present);
//   • the retired keys `status` / `online` / `waking_since` /
//     `stopping_timed_out` NEVER reappear on the wire.
//
// 🔴 WHERE THE PROBE LIVES, AND WHY IT MOVED (T-91). This file used to take its
// wire-shape sample off the WRITE answers — the POST /api/members hire and the
// DELETE /api/members/{id} dismiss — because those routes echoed back the whole
// roster row they had just written. Since T-91 the fifteen lifecycle writes
// answer a bounded receipt instead (hire and dismiss: `{id}` and nothing else),
// so a probe there would be worse than useless: the "present" half would fail
// outright, and the "retired keys must be absent" half would pass VACUOUSLY on
// a one-field object — a green that proves nothing about the DTO. The probe
// therefore runs against the READ faces, `GET /api/members` and
// `GET /api/members/{id}`, which are unchanged by T-91 and are the only places
// the full MemberDTO still reaches a client. That is also where the FE mapper
// actually gets its rows, so it is the honest place to seal them.
//
// The write legs stay, pinning what they NOW answer:
//   • the hire and the dismiss each answer a receipt — `id` present, and none
//     of the roster-row fields riding along uninvited;
//   • the "dismiss flips the lifecycle to removed" claim can no longer be read
//     off any response: the DELETE says only `id`, and there is NO read face
//     that serves the dismissed row — `GET /api/members/{id}` 404s afterwards
//     (the audit row survives server-side, off the wire). So the claim is
//     proved the only way that is still honest: the row drops off the roster
//     list AND the direct GET is an honest 404.
const { test, expect } = require('@playwright/test');
const {
  BASE,
  authHeaders,
  ownerToken,
  // `hireMember` is deliberately NOT imported: the fixture merges the caller's
  // own `name` into what it hands back, and this spec must see the hire's wire
  // answer verbatim. It POSTs /api/members itself.
  listMembers,
} = require('../lib/fixtures');

// The retired wire keys — their reappearance is a regression, full stop.
const RETIRED_KEYS = ['status', 'online', 'waking_since', 'stopping_timed_out'];

function assertMemberWireShape(row, label) {
  // Present-and-typed: the M2 fields every consumer relies on.
  expect(
    Object.prototype.hasOwnProperty.call(row, 'roster_status'),
    `${label}: roster_status must be on the wire`,
  ).toBe(true);
  expect(
    ['active', 'removed'],
    `${label}: roster_status must be the active/removed lifecycle`,
  ).toContain(row.roster_status);
  expect(
    typeof row.presence,
    `${label}: presence (the single presence word) must be a string`,
  ).toBe('string');
  expect(
    Object.prototype.hasOwnProperty.call(row, 'unread_count'),
    `${label}: unread_count must ALWAYS be present (int, default 0)`,
  ).toBe(true);
  expect(
    typeof row.unread_count,
    `${label}: unread_count must be a number`,
  ).toBe('number');
  // Absent: the retired keys must never come back.
  for (const key of RETIRED_KEYS) {
    expect(
      Object.prototype.hasOwnProperty.call(row, key),
      `${label}: retired wire key "${key}" must NOT reappear`,
    ).toBe(false);
  }
}

// The T-91 receipt shape: `id` and nothing from the roster row. Asserting the
// roster fields are ABSENT here is not a duplicate of RETIRED_KEYS above — it
// is what keeps a future "just echo the row again, it's convenient" from
// sliding back in unnoticed.
const ROSTER_ROW_KEYS = ['roster_status', 'presence', 'unread_count', 'name'];

function assertLifecycleReceipt(body, label) {
  expect(typeof body.id, `${label}: the receipt must name the agent`).toBe(
    'string',
  );
  expect(body.id, `${label}: the receipt id must not be empty`).not.toBe('');
  for (const key of ROSTER_ROW_KEYS) {
    expect(
      Object.prototype.hasOwnProperty.call(body, key),
      `${label}: roster-row key "${key}" must NOT ride along on a receipt (T-91)`,
    ).toBe(false);
  }
}

test.describe('D1 · Member read-face wire shape — roster_status + slim-down seal', () => {
  test('every roster row carries the M2 shape and none of the retired keys', async ({
    request,
  }) => {
    const token = await ownerToken(request);
    const members = await listMembers(request, token);
    expect(
      members.length,
      'the seeded roster must not be empty (Mira ships out of the box)',
    ).toBeGreaterThan(0);
    for (const row of members) {
      assertMemberWireShape(row, `member ${row.id} (${row.name})`);
      // The list endpoint omits soft-removed rows entirely.
      expect(
        row.roster_status,
        `listed member ${row.id} must be active (removed rows are omitted)`,
      ).toBe('active');
    }
  });

  test('hire and dismiss answer T-91 receipts; the read face carries the DTO', async ({
    request,
  }) => {
    const token = await ownerToken(request);
    // Hire a throwaway member — NEVER dismiss the seed roster (specs share the
    // one isolated server).
    //
    // NOTE the fixture merges the caller's own `name` into what it returns, so
    // hit the raw route here: this leg must see the receipt EXACTLY as the wire
    // sends it.
    const hireRes = await request.post(`${BASE}/api/members`, {
      headers: authHeaders(token),
      data: { name: 'WireShape Probe', kind: 'staff' },
    });
    expect(hireRes.status(), 'hiring must succeed').toBe(200);
    const hired = await hireRes.json();
    assertLifecycleReceipt(hired, 'hire receipt (POST /api/members)');

    // The full DTO — the thing this spec exists to seal — comes from the READ
    // face, which T-91 left alone.
    const read = await request.get(`${BASE}/api/members/${hired.id}`, {
      headers: authHeaders(token),
    });
    expect(read.status(), 'GET of a fresh hire must succeed').toBe(200);
    const row = await read.json();
    assertMemberWireShape(row, 'freshly hired member (GET by id)');
    expect(row.roster_status, 'a fresh hire starts active').toBe('active');

    const del = await request.delete(`${BASE}/api/members/${hired.id}`, {
      headers: authHeaders(token),
    });
    expect(del.status(), 'soft dismiss must succeed').toBe(200);
    const dismissed = await del.json();
    assertLifecycleReceipt(dismissed, 'dismiss receipt (DELETE response)');
    expect(
      dismissed.id,
      'the dismiss receipt must name the member it dismissed',
    ).toBe(hired.id);

    // roster_status === "removed" is NOT readable anywhere any more (see the
    // header): the receipt does not carry it and no read face serves the
    // dismissed row. The two observable consequences of the flip are asserted
    // instead — the removed row drops off the roster list (audit row survives
    // server-side)…
    const after = await listMembers(request, token);
    expect(
      after.find((m) => m.id === hired.id),
      'a dismissed member must not be listed',
    ).toBeUndefined();
    // …and a direct read is an honest 404.
    const get = await request.get(`${BASE}/api/members/${hired.id}`, {
      headers: authHeaders(token),
    });
    expect(get.status(), 'GET of a removed member must 404').toBe(404);
  });
});
