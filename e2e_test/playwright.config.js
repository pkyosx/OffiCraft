// e2e_test/playwright.config.js — config for the isolated officraft e2e suite.
// The service is brought up by setup.sh (NOT by playwright's webServer), so specs
// just point at OC_E2E_BASE. Browser-based render specs (B group) are added later
// and will require `npx playwright install chromium`.
const { defineConfig } = require('@playwright/test');

// The whole suite talks to ONE server process and ONE SQLite file. fullyParallel
// was already false, but that only serialises tests WITHIN a file — playwright
// still runs FILES concurrently, one worker per core, and those workers then
// fight over the same station. Measured 2026-08-05 on this suite: 7 workers → 7
// red, the same tree serialised → 4 red. Three of those reds were the harness
// eating itself, and a gate that reddens for reasons unrelated to the code under
// test gets switched off within a week. One worker, always.
const WORKERS = 1;

// Some specs are not browser tests: they need a LIVE agent process — `claude` on
// PATH, a real warden spawned — and they BURN REAL
// API QUOTA — running it costs money, every time.
//
// 🔴 THE DEFAULT IS "DO NOT RUN", AND OPTING IN COSTS MONEY.
// Specs that need a LIVE agent process declare themselves by FILENAME
// (`*.live-agent.spec.js`) and are ignored unless the caller explicitly asks for
// them with OC_E2E_LIVE_AGENT=1. Nobody has to remember to add a guard, because
// there is no guard to add — there is only an explicit request to spend.
//
// Why this shape (T-c329, owner ruled at rc-d51e755d3207 / rc-4e3ae0ec146d):
//   * DEFAULT-OFF, not opt-out. The previous flag was an EXCLUSION set only in
//     .github/workflows/ci.yml, so the cloud was protected and every laptop was
//     not: one local `run_all.sh` silently spawned a real agent and billed for
//     it. A protection that exists only on one path is a protection that the
//     other path never had.
//   * MEMBERSHIP BY FILENAME, no list in this file. A hardcoded list means every
//     future live-agent spec must remember to register itself, and the one that
//     forgets runs — and charges — by default. The predicate must be something a
//     new spec cannot omit while still being in the class.
//   * Still a FILE-level predicate, not a title regex: a regex silently widens
//     the day someone reuses those words, and what gates these specs is a
//     file-level prerequisite (a live agent), not a phrase.
//   * STRICT `=== '1'`: a typo (`true`, `yes`, `TRUE`) falls to "did not run,
//     spent nothing". Misconfiguration must fail toward not-spending.
// e2e_test/assert-specs-ran.sh asserts AFTERWARDS that the class really stayed
// out when it was not asked for — an exclusion nobody checks is an exclusion
// that quietly stops excluding.
const LIVE_AGENT = process.env.OC_E2E_LIVE_AGENT === '1';

module.exports = defineConfig({
  testDir: './tests',
  ...(LIVE_AGENT ? {} : { testIgnore: ['**/*.live-agent.spec.js'] }),
  fullyParallel: false,
  workers: WORKERS,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.OC_E2E_BASE || 'http://127.0.0.1:8791',
    extraHTTPHeaders: {},
    // Until now a red run produced ONE line of text and nothing else: no DOM,
    // no network, no scroll state. That is why the intermittent reds in
    // 12_in_conversation_divider could not be told apart — two different causes
    // (the product scrolling on its own vs. a scroll event that had not
    // dispatched yet) print the same sentence. `retain-on-failure` keeps a full
    // trace ONLY for tests that actually fail, so a green run still writes
    // nothing and the steady-state artifact cost stays zero.
    trace: 'retain-on-failure',
  },
});
