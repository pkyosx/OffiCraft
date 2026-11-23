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

describe("httpApi 任務手冊建議回覆", () => {
  it("GET maps each type's list off the wire", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        lists: [
          { type_key: "builtin-role-design", replies: ["照角色手冊", "先放著"] },
          { type_key: "tm-quote", replies: ["報價單已附"] },
        ],
      }),
    );
    expect(await httpApi.getTaskManualSuggestedReplies()).toEqual({
      "builtin-role-design": ["照角色手冊", "先放著"],
      "tm-quote": ["報價單已附"],
    });
    expect(await onlyRequest()).toEqual({ path: "/api/task-manual-suggested-replies", method: "GET", body: undefined });
  });

  it("PUT sends the given list to that type and answers what the server stored", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ type_key: "tm-quote", replies: ["報價單已附", "好"] }));
    expect(await httpApi.putTaskManualSuggestedReplies("tm-quote", [" 報價單已附 ", "好"])).toEqual(["報價單已附", "好"]);
    expect(await onlyRequest()).toEqual({
      path: "/api/task-manual-suggested-replies/tm-quote",
      method: "PUT",
      body: { replies: [" 報價單已附 ", "好"] },
    });
  });
});
