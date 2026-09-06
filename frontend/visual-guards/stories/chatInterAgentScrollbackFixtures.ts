// Fixtures for the T-124 scrollback guard, in their own module because
// Playwright CT rewrites a component import into a registry declaration — a
// plain value exported alongside the component collides at collect time
// ("Identifier … has already been declared", then "No tests found"). Same
// reason as `chatThreadLoadingFixtures.ts`.

/** How many 30-message pages of history sit ABOVE the page the thread opens on.
 * Each has the screenshot's own composition (14 成員間 + 1 + 12 成員間 + 1 + 2
 * 成員間), so every page a scrollback pulls is again mostly collapsed. */
export const OLDER_PAGES = 3;
/** Everything in the fixture stream, the opening page included. */
export const TOTAL_MESSAGES = (OLDER_PAGES + 1) * 30;
/** The owner↔peer bubbles in the whole stream — 2 per page. They are the rows a
 * guard can count without expanding anything. */
export const NORMAL_TOTAL = (OLDER_PAGES + 1) * 2;
