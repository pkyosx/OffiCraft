// Mock↔server parity for createRole (POST /api/roles). Two contracts the mock
// had drifted on, both pinned here so a future drift goes RED:
//
//   1. member_no derivation — the server mints the founding member's display
//      badge by DERIVING it from the member id (ocserverd/api_helpers.go:198 →
//      domain.go:160 MemberNo): SHA-256 the id, first 8 bytes big-endian as a
//      uint64, three uppercase letters (n%26, n/=26) then three digits (n%1000).
//      The mock used to hard-code "MB-NEW001" for every role — so two roles
//      collided on the same badge, unlike the server.
//   2. effort 422 message — the server appends the offending value:
//      `effort must be one of [high low medium]; got '<value>'`
//      (ocserverd/api_roles.go:128-129). The mock used a different, value-less
//      string.
//
// The derivation golden vectors below were produced by RUNNING the real Go
// domain.MemberNo, so this is a cross-implementation lock, not a self-check.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, deriveMemberNo } from "./mock";
import { ApiError } from "./errors";

describe("mock createRole — member_no derivation parity", () => {
  beforeEach(() => __resetMock());

  it("matches Go domain.MemberNo byte-for-byte (golden vectors)", async () => {
    // id => value emitted by the actual server (server/ocserverd/domain.go).
    const golden: Record<string, string> = {
      "m-abc": "MB-TPP251",
      "m-000000000000": "MB-FOH139",
      "m-deadbeefcafe": "MB-HJJ147",
      "r-1": "MB-WDH581",
    };
    for (const [id, expected] of Object.entries(golden)) {
      expect(await deriveMemberNo(id)).toBe(expected);
    }
  });

  it("createRole derives member_no from the member id (never a constant)", async () => {
    const a = await mockApi.createRole({ name: "Role A" });
    const b = await mockApi.createRole({ name: "Role B" });

    // Shape is the server's MB-XXX### badge.
    expect(a.member.memberId).toMatch(/^MB-[A-Z]{3}\d{3}$/);
    // The badge is the derivation of THIS member's raw id — the parity contract.
    expect(a.member.memberId).toBe(await deriveMemberNo(a.member.id));
    expect(b.member.memberId).toBe(await deriveMemberNo(b.member.id));
    // Distinct ids ⇒ distinct badges: guards against the old hard-coded constant.
    expect(a.member.memberId).not.toBe(b.member.memberId);
    expect(a.member.memberId).not.toBe("MB-NEW001");
  });
});

describe("mock createRole — effort 422 parity", () => {
  beforeEach(() => __resetMock());

  it("rejects an unknown effort with the server's exact message + status", async () => {
    let err: unknown;
    try {
      await mockApi.createRole({ name: "Bad Effort", effort: "extreme" });
    } catch (e) {
      err = e;
    }
    expect(err).toBeInstanceOf(ApiError);
    const api = err as ApiError;
    expect(api.status).toBe(422);
    // Byte-for-byte the Go writeError message (ocserverd/api_roles.go:128-129),
    // including the offending value.
    expect(api.serverMessage).toBe(
      "effort must be one of [high low medium]; got 'extreme'"
    );
  });

  it("accepts the closed low/medium/high vocabulary", async () => {
    for (const effort of ["low", "medium", "high"] as const) {
      const r = await mockApi.createRole({ name: `Role ${effort}`, effort });
      expect(r.member.effort).toBe(effort);
    }
  });
});
