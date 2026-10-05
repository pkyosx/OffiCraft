// freePort.test.ts — the ports half of the paint guards' plumbing.
//
// The bug these assertions exist against is not a wrong answer, it is a PINNED
// answer: playwright-paint.config.ts used to spell 4318 and 4319, so two working
// copies running the guards at the same time asked for the same pair and the
// loser died. "Different every run" is therefore the property under test, and it
// is asserted on the config itself — a correct allocateFreePorts() that nobody
// calls would leave the collision exactly where it was.

import { createServer, type AddressInfo, type Server } from "node:net";
import { afterEach, describe, expect, it, vi } from "vitest";
import { allocateFreePorts } from "./freePort";

const URL_VARS = [
  "PAINT_GUARD_OK_URL",
  "PAINT_GUARD_UNKNOWN_URL",
  "PAINT_GUARD_BUILTIN_URL",
] as const;

/** Every listen()/close() any net server in this file makes, in order. */
const probeCalls = vi.hoisted(
  () => [] as { call: "listen" | "close"; port: number | null }[]
);

// The "all probes open at once" invariant is only observable WHILE
// allocateFreePorts runs — by the time it returns, every probe is shut. So the
// sockets it creates are wrapped here and asked to say when they opened and
// when they closed.
vi.mock("node:net", async (importOriginal) => {
  const actual = await importOriginal<typeof import("node:net")>();
  const portOf = (server: Server) => {
    const addr = server.address();
    return addr !== null && typeof addr !== "string" ? addr.port : null;
  };
  const wrapped = (...args: never[]) => {
    const server = actual.createServer(...args);
    const listen = server.listen.bind(server) as (...a: never[]) => Server;
    const close = server.close.bind(server) as (...a: never[]) => Server;
    server.listen = ((...a: never[]) => {
      const result = listen(...a);
      probeCalls.push({ call: "listen", port: portOf(server) });
      return result;
    }) as typeof server.listen;
    server.close = ((...a: never[]) => {
      probeCalls.push({ call: "close", port: portOf(server) });
      return close(...a);
    }) as typeof server.close;
    return server;
  };
  return { ...actual, default: { ...actual, createServer: wrapped }, createServer: wrapped };
});

describe("allocateFreePorts", () => {
  it("hands out the requested number of distinct ports", () => {
    const ports = allocateFreePorts(3);
    expect(ports).toHaveLength(3);
    expect(new Set(ports).size).toBe(3);
  });

  it("keeps every probe listening until the whole set is chosen", () => {
    probeCalls.length = 0;
    const ports = allocateFreePorts(3);
    const firstClose = probeCalls.findIndex((entry) => entry.call === "close");
    const stillOpenAtFirstClose = probeCalls
      .slice(0, firstClose === -1 ? probeCalls.length : firstClose)
      .filter((entry) => entry.call === "listen").length;
    const trace = probeCalls
      .map((entry) => `${entry.call}(${entry.port})`)
      .join(" ");
    expect(
      stillOpenAtFirstClose,
      `a probe was released before all 3 ports were chosen, so a later probe can ` +
        `be handed a port this same call already gave out — chose ${ports.join(", ")}, ` +
        `probe socket calls were ${trace}`
    ).toBe(3);
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
    expect(
      new Set([...first.ports, ...second.ports]).size,
      `two evaluations of the config shared a port — first run got ` +
        `${first.ports.join(", ")}, second run got ${second.ports.join(", ")}`
    ).toBe(first.ports.length + second.ports.length);
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
    expect(servers).toHaveLength(URL_VARS.length - 1);
    expect(servers.some((s) => s.command.includes("--mode ok"))).toBe(false);
    expect(servers.some((s) => s.command.includes("--mode unknown-theme"))).toBe(true);
    expect(process.env.PAINT_GUARD_OK_URL).toBe("http://localhost:9999");
  });
});
