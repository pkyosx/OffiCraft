import { fileURLToPath } from "node:url";

// The test runners (vitest and Playwright CT) swap the shipped seasonal schedule
// for an empty one. Nearly every suite renders I18nProvider against the real
// clock, so with the real schedule a run inside a seasonal window would change
// wording, avatars and colours under hundreds of unrelated tests. Seasonal
// behaviour is tested with explicit windows instead.
export const NO_SEASONAL_SCHEDULE_ALIAS = {
  find: /^(\.\.\/)+themes\/seasonal-schedule\.json$/,
  replacement: fileURLToPath(new URL("./src/test/seasonal-schedule.none.json", import.meta.url)),
};
