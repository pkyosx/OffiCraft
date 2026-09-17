import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TOKEN_KEY } from "./auth";
import type { Member } from "../types";

async function loadGate() {
  vi.resetModules();
  return await import("./index");
}

beforeEach(() => {
  localStorage.clear();
  vi.stubEnv("VITE_OC_TOKEN", "");
});

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
  localStorage.clear();
});

function jwt(claims: Record<string, unknown>): string {
  const b64url = (v: unknown) =>
    btoa(String.fromCharCode(...new TextEncoder().encode(JSON.stringify(v))))
      .replace(/\+/g, "-")
      .replace(/\//g, "_")
      .replace(/=+$/, "");
  return `${b64url({ alg: "HS256", typ: "JWT" })}.${b64url(claims)}.sig`;
}

function member(id: string, role: string, kind = "staff"): Member {
  return { id, role, kind } as Member;
}

const ROSTER = [
  member("mira", "assistant"),
  member("m-kyle", "officraft-developer"),
  member("w-host", "assistant", "warden"),
];

describe("viewerMaySetLoreScope", () => {
  it.each([
    { viewer: "no token", token: null, offered: false },
    {
      viewer: "an owner token",
      token: jwt({ scope: "owner", sub: "owner" }),
      offered: true,
    },
    {
      viewer: "an owner token whose payload encodes to base64url '-' and '_'",
      token: jwt({
        scope: "owner",
        sub: "owner",
        iat: 1789600000,
        exp: 1789686400,
        label: "測試???~~~",
      }),
      offered: true,
    },
    {
      viewer: "the assistant's member token",
      token: jwt({ scope: "agent", sub: "mira" }),
      offered: true,
    },
    {
      viewer: "a plain member's token",
      token: jwt({ scope: "agent", sub: "m-kyle" }),
      offered: false,
    },
    {
      viewer: "a warden carrying the assistant role key",
      token: jwt({ scope: "agent", sub: "w-host" }),
      offered: false,
    },
    {
      viewer: "a member token whose sub is not on the roster",
      token: jwt({ scope: "agent", sub: "m-gone" }),
      offered: false,
    },
    {
      viewer: "a token with no sub",
      token: jwt({ scope: "agent" }),
      offered: false,
    },
    { viewer: "a token that is not a JWT", token: "owner-jwt", offered: false },
    {
      viewer: "a token whose payload is not JSON",
      token: `a.${btoa("not-json")}.c`,
      offered: false,
    },
  ])(
    "real mode with $viewer offers the scope menu: $offered",
    async ({ token, offered }) => {
      vi.stubEnv("VITE_USE_MOCK", "false");
      if (token !== null) localStorage.setItem(TOKEN_KEY, token);
      const { viewerMaySetLoreScope, USE_MOCK } = await loadGate();
      expect(USE_MOCK).toBe(false);

      expect(viewerMaySetLoreScope(ROSTER)).toBe(offered);
    },
  );

  it("real mode with the assistant's token before the roster loads does not offer the scope menu", async () => {
    vi.stubEnv("VITE_USE_MOCK", "false");
    localStorage.setItem(TOKEN_KEY, jwt({ scope: "agent", sub: "mira" }));
    const { viewerMaySetLoreScope } = await loadGate();

    expect(viewerMaySetLoreScope([])).toBe(false);
    expect(viewerMaySetLoreScope(ROSTER)).toBe(true);
  });

  it("mock mode offers the scope menu without a token", async () => {
    vi.stubEnv("VITE_USE_MOCK", "true");
    const { viewerMaySetLoreScope, USE_MOCK } = await loadGate();
    expect(USE_MOCK).toBe(true);
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();

    expect(viewerMaySetLoreScope([])).toBe(true);
  });
});
