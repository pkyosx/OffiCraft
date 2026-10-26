// How many test workers a LOCAL run may use, for both vitest and Playwright CT.
//
// Several agents share one dev machine. Left to their defaults, vitest and
// Playwright size their pools to the core count, so a single test run takes the
// whole box and every other agent on it stalls. OC_LOCAL_TEST_WORKERS (default
// 4) is the per-run cap; the Makefile's test-go recipe reads the same variable.
//
// On CI this returns undefined and the callers leave the runner's own default
// alone — the owner ruled that cloud parallelism stays exactly as it is, so do
// not "simplify" this into an unconditional cap. `CI` is the same signal
// forbidOnly already keys on; GitHub Actions always sets it.
//
// A malformed value throws instead of falling back: a typo that silently means
// "no cap" is the exact outcome this exists to prevent.
export var DEFAULT_LOCAL_TEST_WORKERS = 4;
export function localTestWorkers(env) {
    if (env === void 0) { env = process.env; }
    if (env.CI)
        return undefined;
    var raw = env.OC_LOCAL_TEST_WORKERS;
    if (raw === undefined || raw === "")
        return DEFAULT_LOCAL_TEST_WORKERS;
    if (!/^[1-9][0-9]*$/.test(raw)) {
        throw new Error("OC_LOCAL_TEST_WORKERS must be a positive integer, got ".concat(JSON.stringify(raw)));
    }
    return Number(raw);
}
