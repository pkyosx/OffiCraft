import { describe, it, expect, beforeEach, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import { __resetWorkerCodenameCache } from "../hooks/useWorkerCodenames";
import { resetAllSharedSnapshots } from "../lib/sharedSnapshot";
import { api } from "../api";
import { __injectMockChat, __injectMockOutsourceWorker, __resetMock, mockApi } from "../api/mock";
import type { OutsourceWorkerView } from "../api/adapter";

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

function openRoom(id: string) {
  window.location.hash = `#office/chat/${id}`;
  return render(
    <I18nProvider>
      <OfficePage />
    </I18nProvider>,
  );
}
const chips = () =>
  Array.from(screen.getByTestId("chat-suggested-replies").querySelectorAll("button")).map((b) => b.textContent);

beforeEach(() => {
  __resetMock();
  resetAllSharedSnapshots();
  __resetWorkerCodenameCache();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
  __injectMockOutsourceWorker(worker);
});

describe("OfficePage chat 建議回覆", () => {
  for (const id of ["mira", "ow-a"]) {
    it(`the ${id} room shows its own sentences, then the all-member ones`, async () => {
      await mockApi.putChatSuggestedReplies(id, ["專屬", "繼續"]);
      await mockApi.patchServerSettings({ suggestedRepliesChat: ["繼續", "共用"] });
      openRoom(id);
      await waitFor(() => expect(chips()).toEqual(["專屬", "繼續", "共用"]));
    });

    it(`the dialog in the ${id} room saves to that member, and another window's edit shows without a reload`, async () => {
      openRoom(id);
      fireEvent.click(await screen.findByTestId("chat-header-suggested-replies"));
      fireEvent.click(screen.getByRole("button", { name: "新增一句" }));
      const input = screen.getByTestId("chat-suggested-replies-dialog").querySelector("input") as HTMLInputElement;
      fireEvent.change(input, { target: { value: "新的一句" } });
      fireEvent.blur(input);
      await waitFor(async () => expect(await api.getChatSuggestedReplies()).toEqual({ [id]: ["新的一句"] }));
      await waitFor(() => expect(chips()).toEqual(["新的一句"]));

      await act(async () => {
        await mockApi.putChatSuggestedReplies(id, ["別的視窗改的"]);
      });
      await waitFor(() => expect(chips()).toEqual(["別的視窗改的"]));
    });
  }

  it("a room whose peer left the roster offers neither chips nor the editor", async () => {
    await mockApi.patchServerSettings({ suggestedRepliesChat: ["共用"] });
    __injectMockChat({
      id: "c-1",
      from: "ow-gone",
      to: "owner",
      body: "hi",
      ts: Date.now() / 1000 - 120,
      attachments: [],
      replyCardId: null,
    });
    openRoom("ow-gone");
    await screen.findByTestId("released-chat-sub");
    await act(async () => {});
    expect(screen.queryByTestId("chat-header-suggested-replies")).toBeNull();
    expect(screen.queryByTestId("chat-suggested-replies")).toBeNull();
  });
});
