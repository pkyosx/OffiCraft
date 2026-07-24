import { describe, it, expect } from "vitest";
import { avatarKindForMember } from "./avatarKind";

describe("avatarKindForMember", () => {
  it("returns outsource for an ow- prefixed id", () => {
    expect(avatarKindForMember({ id: "ow-7" })).toBe("outsource");
  });

  it("returns outsource for kind outsource even without an ow- id", () => {
    expect(avatarKindForMember({ id: "m-1", kind: "outsource" })).toBe(
      "outsource"
    );
  });

  it("returns assistant for an assistant-role member", () => {
    expect(avatarKindForMember({ id: "m-1", role: "assistant" })).toBe(
      "assistant"
    );
  });

  it("returns member for a plain staff member", () => {
    expect(avatarKindForMember({ id: "m-1", role: "r-abc" })).toBe("member");
  });

  it("prefers outsource over assistant when both signals collide", () => {
    expect(
      avatarKindForMember({ id: "ow-3", role: "assistant" })
    ).toBe("outsource");
  });
});
