import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

const fetchMock = vi.fn<(req: Request) => Promise<Response>>();

async function onlyRequest() {
  expect(fetchMock).toHaveBeenCalledTimes(1);
  const req = fetchMock.mock.calls[0][0];
  const text = await req.clone().text();
  return { path: new URL(req.url).pathname, method: req.method, body: text ? JSON.parse(text) : undefined };
}

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("httpApi chat 建議回覆", () => {
  it("GET maps each member's list off the wire", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        lists: [
          { member_id: "mira", replies: ["繼續", "先放著"] },
          { member_id: "ow-a", replies: ["好"] },
        ],
      }),
    );
    expect(await httpApi.getChatSuggestedReplies()).toEqual({ mira: ["繼續", "先放著"], "ow-a": ["好"] });
    expect(await onlyRequest()).toEqual({ path: "/api/chat/suggested-replies", method: "GET", body: undefined });
  });

  it("PUT sends the given list to that member and answers what the server stored", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ replies: ["繼續", "好"] }));
    expect(await httpApi.putChatSuggestedReplies("ow-a", [" 繼續 ", "好"])).toEqual(["繼續", "好"]);
    expect(await onlyRequest()).toEqual({
      path: "/api/chat/suggested-replies/ow-a",
      method: "PUT",
      body: { replies: [" 繼續 ", "好"] },
    });
  });

  it("the settings PATCH carries the all-member list and nothing else", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({ owner_token_ttl: 86400, agent_token_ttl: 604800, suggested_replies_chat: ["繼續"] }),
    );
    const settings = await httpApi.patchServerSettings({ suggestedRepliesChat: ["繼續"] });
    expect(await onlyRequest()).toEqual({
      path: "/api/settings",
      method: "PATCH",
      body: { suggested_replies_chat: ["繼續"] },
    });
    expect(settings.suggestedRepliesChat).toEqual(["繼續"]);
  });
});
