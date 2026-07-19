// e2e_test/playwright.config.js — config for the isolated officraft e2e suite.
// The service is brought up by setup.sh (NOT by playwright's webServer), so specs
// just point at OC_E2E_BASE. Browser-based render specs (B group) are added later
// and will require `npx playwright install chromium`.
const { defineConfig } = require('@playwright/test');

module.exports = defineConfig({
  testDir: './tests',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.OC_E2E_BASE || 'http://127.0.0.1:8791',
    extraHTTPHeaders: {},
  },
});
