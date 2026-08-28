// freePort.test.ts — the ports half of the paint guards' plumbing.
//
// The bug these assertions exist against is not a wrong answer, it is a PINNED
// answer: playwright-paint.config.ts used to spell 4318 and 4319, so two working
// copies running the guards at the same time asked for the same pair and the
// loser died. "Different every run" is therefore the property under test, and it
// is asserted on the config itself — a correct allocateFreePorts() that nobody
// calls would leave the collision exactly where it was.

import { createServer, type AddressInfo } from "node:net";
import { afterEach, describe, expect, it, vi } from "vitest";
import { allocateFreePorts } from "./freePort";

const URL_VARS = ["PAINT_GUARD_OK_URL", "PAINT_GUARD_UNKNOWN_URL"] as const;

describe("allocateFreePorts", () => {
  it("hands out the requested number of distinct ports", () => {
    const ports = allocateFreePorts(3);
    expect(ports).toHaveLength(3);
    expect(new Set(ports).size).toBe(3);
  });

  it("hands out ports that can then actually be bound", async () => {
    const [port] = allocateFreePorts(1);
    const server = createServer();
    try {
      await new Promise<void>((resolve, reject) => {
        server.once("error", reject);
        server.listen(port, () => resolve());
      });
      expect((server.address() as AddressInfo).port).toBe(port);
    } finally {
      server.close();
    }
  });
});

describe("playwright-paint.config.ts", () => {
  const saved = URL_VARS.map((name) => [name, process.env[name]] as const);

  afterEach(() => {
    for (const [name, value] of saved) {
      if (value === undefined) delete process.env[name];
      else process.env[name] = value;
    }
  });

  /** Evaluate the config afresh, the way a new `playwright test` process would,
   * and report what it decided. */
  async function loadConfig() {
    for (const name of URL_VARS) delete process.env[name];
    vi.resetModules();
    const { default: config } = await import("../playwright-paint.config");
    const servers = [config.webServer ?? []].flat();
    return {
      ports: servers.map((server) => Number(new URL(server.url!).port)),
      urls: URL_VARS.map((name) => process.env[name]),
    };
  }

  it("starts one stub per scenario", async () => {
    const { ports } = await loadConfig();
    expect(ports).toHaveLength(URL_VARS.length);
  });

  it("picks a different port for every stub on every run", async () => {
    const first = await loadConfig();
    const second = await loadConfig();
    expect(new Set([...first.ports, ...second.ports]).size).toBe(
      first.ports.length + second.ports.length
    );
  });

  it("publishes each stub's URL so no spec has to know a port", async () => {
    const { ports, urls } = await loadConfig();
    expect(urls).toEqual(ports.map((port) => `http://localhost:${port}`));
  });

  it("leaves a stub alone when its URL was supplied from outside", async () => {
    for (const name of URL_VARS) delete process.env[name];
    process.env.PAINT_GUARD_OK_URL = "http://localhost:9999";
    vi.resetModules();
    const { default: config } = await import("../playwright-paint.config");
    const servers = [config.webServer ?? []].flat();
    expect(servers).toHaveLength(1);
    expect(servers[0].command).toContain("--mode unknown-theme");
    expect(process.env.PAINT_GUARD_OK_URL).toBe("http://localhost:9999");
  });
});
