// e2e_test/tests/09_member_wire_shape.spec.js
// D1 · MemberDTO wire-shape seal (API-only — runs fine under OC_E2E_SKIP_BUILD=1).
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
//     `stopping_timed_out` NEVER reappear on the wire;
//   • DELETE /api/members/{id} (soft dismiss) answers the SAME DTO shape with
//     roster_status === "removed" (lifecycle, not presence), and the dismissed
//     row drops off the roster list while GET by id 404s.
const { test, expect } = require('@playwright/test');
const {
  BASE,
  authHeaders,
  ownerToken,
  hireMember,
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

test.describe('D1 · MemberDTO wire shape — roster_status + slim-down seal', () => {
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

  test('soft dismiss answers roster_status="removed" on the same wire shape', async ({
    request,
  }) => {
    const token = await ownerToken(request);
    // Hire a throwaway member — NEVER dismiss the seed roster (specs share the
    // one isolated server).
    const hired = await hireMember(request, token, 'WireShape Probe');
    assertMemberWireShape(hired, 'freshly hired member');
    expect(hired.roster_status, 'a fresh hire starts active').toBe('active');

    const del = await request.delete(`${BASE}/api/members/${hired.id}`, {
      headers: authHeaders(token),
    });
    expect(del.status(), 'soft dismiss must succeed').toBe(200);
    const dismissed = await del.json();
    assertMemberWireShape(dismissed, 'dismissed member (DELETE response)');
    expect(
      dismissed.roster_status,
      'dismiss must flip the LIFECYCLE field to "removed"',
    ).toBe('removed');

    // The removed row drops off the roster list (audit row survives server-side)…
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
