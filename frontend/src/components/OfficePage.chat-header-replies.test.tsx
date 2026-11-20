// The chat header's 請示卡 jump as OfficePage wires it: the badge counts the
// waiting cards THIS peer opened, staff and outsource alike, and the jump
// lands on the 請示 page narrowed to that peer.
import { describe, it, expect, beforeEach, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import { ReplyCardsProvider } from "../hooks/useReplyCards";
import { __resetWorkerCodenameCache } from "../hooks/useWorkerCodenames";
import { resetAllSharedSnapshots } from "../lib/sharedSnapshot";
import { __injectMockOutsourceWorker, __injectMockReplyCard, __resetMock } from "../api/mock";
import type { OutsourceWorkerView, ReplyCard } from "../api/adapter";

const worker = {
  id: "ow-a",
  codename: "O-1",
  model: "opus",
  effort: "medium",
  taskId: "T-1",
  taskTypeKey: "",
  taskTitle: "",
  taskStatus: "in_progress",
  createdTs: 0,
} as OutsourceWorkerView;

let seq = 0;
function card(from: string, over: Partial<ReplyCard> = {}): ReplyCard {
  seq += 1;
  return {
    id: `rc-hdr${String(seq).padStart(9, "0")}`,
    from,
    kind: "decision",
    summary: `請示 ${seq}`,
    body: "",
    options: [],
    selectMode: "single",
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 60 * seq,
    answeredTs: null,
    chatMessageId: `msg-hdr-${seq}`,
    answer: null,
    ...over,
  };
}

function openRoom(id: string) {
  window.location.hash = `#office/chat/${id}`;
  return render(
    <I18nProvider>
      <ReplyCardsProvider>
        <OfficePage />
      </ReplyCardsProvider>
    </I18nProvider>,
  );
}
const button = () => screen.findByRole("button", { name: "這位成員還沒回的請示卡" });
const badge = () => screen.queryByTestId("chat-header-replies-badge")?.textContent ?? null;

beforeEach(() => {
  vi.restoreAllMocks();
  __resetMock();
  resetAllSharedSnapshots();
  __resetWorkerCodenameCache();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
  __injectMockOutsourceWorker(worker);
  const now = Date.now() / 1000;
  __injectMockReplyCard(card("mira"));
  __injectMockReplyCard(card("mira"));
  __injectMockReplyCard(card("mira", { snoozedUntilTs: now + 3600 }));
  __injectMockReplyCard(card("kyle"));
  __injectMockReplyCard(card("ow-a"));
});

describe("OfficePage chat header 請示卡 jump", () => {
  it.each([
    ["mira", "2"],
    ["ow-a", "1"],
  ])("the %s room's badge counts only that peer's waiting cards, snoozed ones left out", async (id, count) => {
    openRoom(id);
    await button();
    await waitFor(() => expect(badge()).toBe(count));
  });

  it("the badge follows a card that arrives while the room is open", async () => {
    openRoom("mira");
    await waitFor(() => expect(badge()).toBe("2"));
    act(() => __injectMockReplyCard(card("mira")));
    await waitFor(() => expect(badge()).toBe("3"));
    act(() => __injectMockReplyCard(card("kyle")));
    await waitFor(() => expect(badge()).toBe("3"));
  });

  it("a peer with no waiting card still has the button, with no number on it", async () => {
    __injectMockOutsourceWorker({ ...worker, id: "ow-b", codename: "O-2" });
    openRoom("ow-b");
    await button();
    await waitFor(() => expect(screen.getByTestId("chat-header-search")).toBeTruthy());
    expect(badge()).toBeNull();
  });

  it.each(["mira", "ow-a"])("clicking it in the %s room opens #replies/opener/<that peer>", async (id) => {
    openRoom(id);
    fireEvent.click(await button());
    expect(window.location.hash).toBe(`#replies/opener/${id}`);
  });
});
