import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { authedAttachmentUrl } from "./http";
import { TOKEN_KEY } from "./auth";

// An HTML attachment opened through this URL may script once the server sees
// only a sig, so the owner token must land as the one and only `token` value
// in the query — never after a stored "#", never behind an empty `token` key.
describe("authedAttachmentUrl", () => {
  beforeEach(() => localStorage.setItem(TOKEN_KEY, "owner-jwt"));
  afterEach(() => localStorage.removeItem(TOKEN_KEY));

  it.each([
    ["/api/chat/attachment/att-0123456789ab", "/api/chat/attachment/att-0123456789ab?token=owner-jwt"],
    ["/api/chat/attachment/att-0123456789ab?sig=s#", "/api/chat/attachment/att-0123456789ab?sig=s&token=owner-jwt"],
    ["/api/chat/attachment/att-0123456789ab?token&sig=s", "/api/chat/attachment/att-0123456789ab?sig=s&token=owner-jwt"],
    ["/api/chat/attachment/att-0123456789ab?token=&sig=s", "/api/chat/attachment/att-0123456789ab?sig=s&token=owner-jwt"],
  ])("%s → %s", (input, want) => {
    expect(authedAttachmentUrl(input)).toBe(want);
  });

  it("leaves non-server URLs alone", () => {
    expect(authedAttachmentUrl("data:image/png;base64,AA")).toBe("data:image/png;base64,AA");
    expect(authedAttachmentUrl("blob:http://x/1")).toBe("blob:http://x/1");
  });
});
