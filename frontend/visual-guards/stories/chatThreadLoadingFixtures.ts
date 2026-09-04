// Fixtures for the fix12 loading guard, in their own module because Playwright
// CT rewrites a component import into a registry declaration — a plain value
// exported alongside the component collides at collect time ("Identifier … has
// already been declared", then "No tests found"). Same reason as
// `chatForwardWalkFixtures.ts`.
export const LOADING_TOTAL = 260;
export const LOADING_ANCHOR = "L3";
