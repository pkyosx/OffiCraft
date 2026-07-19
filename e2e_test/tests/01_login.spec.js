// e2e_test/tests/01_login.spec.js
// A · service skeleton — the simplest end-to-end slice: the isolated service is
// up (setup.sh), an owner can log in, and the token authorizes a DB-backed
// endpoint. Uses Playwright's APIRequestContext (no browser needed).
const { test, expect } = require('@playwright/test');

const BASE = process.env.OC_E2E_BASE || 'http://127.0.0.1:8791';
const PASSWORD = process.env.OC_E2E_PASSWORD || 'joey-e2e-local-pw';

test.describe('A · service skeleton — login', () => {
  test('service is up: GET /api/version returns a git_sha', async ({ request }) => {
    const res = await request.get(`${BASE}/api/version`);
    expect(res.status()).toBe(200);
    const body = await res.json();
    expect(body.git_sha).toBeTruthy();
  });

  test('POST /api/login returns an owner bearer token', async ({ request }) => {
    const res = await request.post(`${BASE}/api/login`, { data: { password: PASSWORD } });
    expect(res.status()).toBe(200);
    const body = await res.json();
    expect(body.token).toBeTruthy();
    expect(body.token_type).toBe('bearer');
    expect(body.owner_id).toBe('owner');
  });

  test('a wrong password is rejected', async ({ request }) => {
    const res = await request.post(`${BASE}/api/login`, { data: { password: 'definitely-wrong' } });
    expect(res.status()).toBeGreaterThanOrEqual(400);
  });

  test('the owner token authorizes a DB-backed endpoint (GET /api/members)', async ({ request }) => {
    const login = await request.post(`${BASE}/api/login`, { data: { password: PASSWORD } });
    const { token } = await login.json();
    const res = await request.get(`${BASE}/api/members`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(res.status()).toBe(200);
    const members = await res.json();
    expect(Array.isArray(members)).toBe(true);
  });
});
