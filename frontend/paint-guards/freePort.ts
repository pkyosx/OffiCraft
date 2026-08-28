// freePort.ts — where the paint guards' stub ports come from.
//
// They used to be the literals 4318 and 4319, spelled into
// playwright-paint.config.ts. Two working copies of this repo running the
// guards at the same time therefore asked the kernel for the SAME pair, and
// whichever lost the bind died — a collision that no amount of retrying could
// get past, because the next run picked the same two numbers again. Letting
// the kernel choose (bind port 0, read back what it assigned) removes the
// collision by construction instead of by convention.
//
// This does NOT make settingsStub.mjs's listen error handler redundant. The
// probe socket has to be closed again before the stub can bind the port, so
// what comes back here is a hint, not a reservation, and anyone can still hand
// the stub an explicit --port. A bind that fails anyway has to SAY it failed
// and why — that half lives in settingsStub.mjs.

import { createServer } from "node:net";

/**
 * `count` distinct ports the OS reports as unused.
 *
 * Bound the way settingsStub.mjs binds — `listen(0)` with no host, i.e. the
 * dual-stack wildcard — so a port that probes free here is free for the stub
 * too. Probing 127.0.0.1 instead would clear a port that a dual-stack listener
 * elsewhere already holds.
 *
 * Synchronous on purpose: a Playwright config is evaluated synchronously and
 * the ports have to be inside the `webServer` commands it returns. Node fills
 * in the assigned address during `listen()` itself for a hostless numeric
 * bind, which is what makes that possible; if that ever stops being true the
 * throw below says so rather than letting `undefined` reach a command line.
 *
 * Every probe is held open until the whole set is chosen, so one call can
 * never hand out the same port twice.
 */
export function allocateFreePorts(count: number): number[] {
  const probes = Array.from({ length: count }, () => createServer());
  try {
    return probes.map((probe) => {
      probe.listen(0);
      const addr = probe.address();
      if (addr === null || typeof addr === "string") {
        throw new Error(
          "allocateFreePorts: node reported no bound address synchronously after " +
            "listen(0), so the paint-guard stub ports cannot be chosen. Set " +
            "PAINT_GUARD_OK_URL and PAINT_GUARD_UNKNOWN_URL to servers you start " +
            "yourself to work around this."
        );
      }
      return addr.port;
    });
  } finally {
    for (const probe of probes) probe.close();
  }
}
