import { describe, it, expect } from "vitest";
import { quotedMessageThreadPeer } from "./quotedMessageThread";

describe("quotedMessageThreadPeer", () => {
  it("stays in the current room when its peer sent or received the message", () => {
    expect(quotedMessageThreadPeer("m1", { from: "m1", to: "owner", deletedTs: 0 })).toBe("m1");
    expect(quotedMessageThreadPeer("m1", { from: "owner", to: "m1" })).toBe("m1");
    expect(quotedMessageThreadPeer("m1", { from: "m2", to: "m1" })).toBe("m1");
  });

  it("goes to the sender's room when the current peer is on neither end", () => {
    expect(quotedMessageThreadPeer("m1", { from: "ow-512", to: "m3" })).toBe("ow-512");
  });

  it("goes to the recipient's room when the sender has no room of its own", () => {
    expect(quotedMessageThreadPeer("m1", { from: "owner", to: "m2" })).toBe("m2");
    expect(quotedMessageThreadPeer("m1", { from: "hook:ep-1", to: "m2" })).toBe("m2");
    expect(quotedMessageThreadPeer("m1", { from: "sched:s-1", to: "m2" })).toBe("m2");
  });

  it("has nowhere to go when neither end has a room", () => {
    expect(quotedMessageThreadPeer("m1", { from: "system", to: "owner" })).toBeNull();
  });

  it("has nowhere to go for a deleted message, even in the current room", () => {
    expect(
      quotedMessageThreadPeer("m1", { from: "m1", to: "owner", deletedTs: 1700000000 }),
    ).toBeNull();
  });
});
