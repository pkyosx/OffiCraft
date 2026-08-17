// Mock↔server parity for createRole (POST /api/roles): the mock must reject an
// unknown effort with the SAME message and status the server does — the server
// appends the offending value (`effort must be one of [high low max medium];
// got '<value>'`, ocserverd/api_roles.go:128-129), and the mock used to answer
// a different, value-less string.
//
// This file also used to pin a second contract: that the mock DERIVED each
// founding member's `MB-XXX###` badge from the member id instead of returning a
// constant. T-5dab retired that projection everywhere (owner rc-ae0ba9565c99 ①)
// — the identity badge renders the member's real id and the wire field is gone —
// so there is no derivation left to keep in parity.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";
import { ApiError } from "./errors";

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
      "effort must be one of [high low max medium]; got 'extreme'"
    );
  });

  it("accepts the closed low/medium/high/max effort vocabulary", async () => {
    for (const effort of ["low", "medium", "high", "max"] as const) {
      const r = await mockApi.createRole({ name: `Role ${effort}`, effort });
      expect(r.member.effort).toBe(effort);
    }
  });
});
