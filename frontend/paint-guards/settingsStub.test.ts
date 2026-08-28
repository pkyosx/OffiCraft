// settingsStub.test.ts — what the stub SAYS when it cannot start.
//
// A stub that dies on a taken port is survivable; a stub that dies without
// naming the taken port is not, because the only thing the runner then prints
// is "the web server did not start", which reads exactly like the stub being
// broken. These assertions are on the wording for that reason, not on the exit
// code alone.

import { spawn, type ChildProcess } from "node:child_process";
import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

// Off cwd, not import.meta.url: under the jsdom environment import.meta.url is
// an http:// URL and fileURLToPath refuses it. Vitest runs with the frontend
// package as cwd, and the check below turns a future change to that into a
// named failure rather than a spawn of nothing.
const FRONTEND = process.cwd();
const STUB = resolve(FRONTEND, "paint-guards/settingsStub.mjs");
if (!existsSync(STUB)) throw new Error(`settingsStub.mjs not found at ${STUB}`);

const running: ChildProcess[] = [];

afterEach(() => {
  for (const child of running.splice(0)) child.kill("SIGKILL");
});

function launch(...extra: string[]) {
  const child = spawn(
    process.execPath,
    [STUB, "--dist", "dist", "--mode", "ok", "--delay", "0", ...extra],
    { cwd: FRONTEND }
  );
  running.push(child);
  let stdout = "";
  let stderr = "";
  child.stdout.on("data", (d) => (stdout += d));
  child.stderr.on("data", (d) => (stderr += d));
  /** Resolves with the port from the stub's banner once it is listening. */
  const listening = new Promise<number>((resolvePort, reject) => {
    child.stdout.on("data", () => {
      const port = /\[paint-guard stub\] :(\d+)/.exec(stdout)?.[1];
      if (port) resolvePort(Number(port));
    });
    child.on("exit", () => reject(new Error(`stub exited: ${stderr || stdout}`)));
  });
  // A case that only wants `exited` never awaits this one, and afterEach's kill
  // would then surface as an unhandled rejection. Awaiting it still throws.
  listening.catch(() => {});

  return {
    listening,
    exited: new Promise<{ code: number | null; stdout: string; stderr: string }>((resolve) =>
      child.on("exit", (code) => resolve({ code, stdout, stderr }))
    ),
  };
}

describe("settingsStub.mjs", () => {
  it("lets the OS pick the port when none is pinned", async () => {
    const port = await launch().listening;
    expect(port).toBeGreaterThan(0);
  });

  it("gives two unpinned stubs different ports instead of a collision", async () => {
    const first = await launch().listening;
    const second = await launch().listening;
    expect(second).not.toBe(first);
  });

  it("names the taken port and clears itself of blame when the bind fails", async () => {
    const port = await launch().listening;
    const { code, stderr } = await launch("--port", String(port)).exited;

    expect(code).toBe(1);
    expect(stderr).toContain(`port ${port} is ALREADY IN USE`);
    expect(stderr).toContain("This stub is NOT broken");
    expect(stderr).toContain("Drop --port");
    // The bare node crash this replaced was an unhandled 'error' event whose
    // stack pointed into this file — the shape that reads as "the stub is
    // broken". It must not come back.
    expect(stderr).not.toContain("Unhandled 'error' event");
  });
});
