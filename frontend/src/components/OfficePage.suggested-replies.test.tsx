import { describe, it, expect, beforeEach, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import { ReplyCardsProvider } from "../hooks/useReplyCards";
import { __resetWorkerCodenameCache } from "../hooks/useWorkerCodenames";
import { resetAllSharedSnapshots } from "../lib/sharedSnapshot";
import { api } from "../api";
import { __injectMockChat, __injectMockOutsourceWorker, __injectMockTask, __resetMock, mockApi } from "../api/mock";
import type { OutsourceWorkerView, TaskView } from "../api/adapter";
import { WithSuggestedReplies } from "../test/suggestedReplies";

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
      <WithSuggestedReplies>
        <ReplyCardsProvider><OfficePage /></ReplyCardsProvider>
      </WithSuggestedReplies>
    </I18nProvider>,
  );
}

const TYPED_TASK = {
  taskNo: "T-9001",
  title: "有類型的",
  typeKey: "builtin-role-design",
  description: "",
  status: "in_progress",
  priority: "mid",
  executorKind: "outsource",
  executorId: "",
  creatorId: "",
  dedupeKey: "",
  deps: [],
  waitingReason: "",
  duplicateOf: "",
  createdTs: Date.now() / 1000 - 3600,
  updatedTs: Date.now() / 1000 - 60,
  closedTs: null,
  progressDone: 0,
  progressTotal: 0,
  steps: [],
} satisfies Omit<TaskView, "id">;

const chips = () =>
  Array.from(screen.getByTestId("chat-suggested-replies").querySelectorAll("button")).map((b) => b.textContent);

const SAVE_ERROR = "建議回覆沒有存成功，畫面已換回伺服器上的內容。";
const roomError = () => screen.getByTestId("chat-suggested-replies-error").textContent;
const go = (id: string) =>
  act(async () => {
    window.location.hash = `#office/chat/${id}`;
    window.dispatchEvent(new HashChangeEvent("hashchange"));
  });
const dialogInput = () => screen.getByTestId("chat-suggested-replies-dialog").querySelector("input") as HTMLInputElement;

beforeEach(() => {
  vi.restoreAllMocks();
  __resetMock();
  resetAllSharedSnapshots();
  __resetWorkerCodenameCache();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
  __injectMockOutsourceWorker(worker);
});

describe("OfficePage chat 建議回覆", () => {
  it("a room with an outsource worker on a typed task shows that type's sentences, then the worker's, then the all-member ones, each sentence once; the dialog holds the worker's own only", async () => {
    __injectMockTask({ ...TYPED_TASK, id: "T-typed", executorId: "ow-typed" });
    __injectMockOutsourceWorker({ ...worker, id: "ow-typed", codename: "O-2", taskId: "T-typed" });
    await mockApi.putTaskManualSuggestedReplies("builtin-role-design", ["照角色手冊", "繼續"]);
    await mockApi.putChatSuggestedReplies("ow-typed", ["專屬", "照角色手冊"]);
    await mockApi.patchServerSettings({ suggestedRepliesChat: ["繼續", "共用"] });
    openRoom("ow-typed");
    await waitFor(() => expect(chips()).toEqual(["照角色手冊", "繼續", "專屬", "共用"]));

    fireEvent.click(screen.getByTestId("chat-header-suggested-replies"));
    const inputs = screen.getByTestId("chat-suggested-replies-dialog").querySelectorAll("input");
    expect([...inputs].map((i) => (i as HTMLInputElement).value)).toEqual(["專屬", "照角色手冊"]);
  });

  for (const [label, id] of [
    ["staff member mira, though she holds a typed task", "mira"],
    ["outsource worker on an ad-hoc task", "ow-a"],
  ] as const) {
    it(`the room with ${label} shows its own sentences, then the all-member ones`, async () => {
      __injectMockTask({ ...TYPED_TASK, id: "T-mira", executorKind: "staff", executorId: "mira" });
      __injectMockTask({ ...TYPED_TASK, id: "T-1", typeKey: "", executorId: "ow-a" });
      await mockApi.putTaskManualSuggestedReplies("builtin-role-design", ["照角色手冊"]);
      await mockApi.putChatSuggestedReplies(id, ["專屬", "繼續"]);
      await mockApi.patchServerSettings({ suggestedRepliesChat: ["繼續", "共用"] });
      openRoom(id);
      await waitFor(() => expect(chips()).toEqual(["專屬", "繼續", "共用"]));
      await act(async () => {});
      expect(chips()).toEqual(["專屬", "繼續", "共用"]);
    });
  }

  for (const id of ["mira", "ow-a"]) {
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

  it("until the first read answers the room offers no editor, then the dialog holds what the server has", async () => {
    await mockApi.putChatSuggestedReplies("mira", ["繼續"]);
    const held = await mockApi.getChatSuggestedReplies();
    let answer!: (v: Record<string, string[]>) => void;
    vi.spyOn(api, "getChatSuggestedReplies").mockReturnValueOnce(new Promise((res) => (answer = res)));
    openRoom("mira");
    await screen.findByTestId("chat-header-presence");
    expect(screen.queryByTestId("chat-header-suggested-replies")).toBeNull();

    await act(async () => answer(held));
    fireEvent.click(await screen.findByTestId("chat-header-suggested-replies"));
    expect(dialogInput().value).toBe("繼續");
  });

  it("a failed first read is told in the room, and the header button reads again and then edits", async () => {
    await mockApi.putChatSuggestedReplies("mira", ["繼續"]);
    const read = vi.spyOn(api, "getChatSuggestedReplies");
    read.mockRejectedValueOnce(new Error("down"));
    openRoom("mira");
    await waitFor(() => expect(roomError()).toBe("建議回覆載入失敗，再按一次標頭的建議回覆按鈕會重新載入"));

    fireEvent.click(screen.getByTestId("chat-header-suggested-replies"));
    await waitFor(() => expect(chips()).toEqual(["繼續"]));
    expect(read).toHaveBeenCalledTimes(2);
    expect(screen.queryByTestId("chat-suggested-replies-error")).toBeNull();

    fireEvent.click(screen.getByTestId("chat-header-suggested-replies"));
    expect(dialogInput().value).toBe("繼續");
  });

  it("a save that fails after Esc is told in that member's room only, the sentence goes, and that member's next save clears it", async () => {
    await mockApi.putChatSuggestedReplies("mira", ["繼續"]);
    const put = vi.spyOn(api, "putChatSuggestedReplies");
    put.mockRejectedValueOnce(new Error("no"));
    openRoom("mira");
    fireEvent.click(await screen.findByTestId("chat-header-suggested-replies"));
    dialogInput().focus();
    fireEvent.change(dialogInput(), { target: { value: "沒存到" } });
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByTestId("chat-suggested-replies-dialog")).toBeNull();

    await waitFor(() => expect(roomError()).toBe(SAVE_ERROR));
    await waitFor(() => expect(chips()).toEqual(["繼續"]));

    await go("ow-a");
    await waitFor(() => expect(document.querySelector(".chat__header-name")?.textContent).toBe("外包 · O-1"));
    fireEvent.click(await screen.findByTestId("chat-header-suggested-replies"));
    const dialog = screen.getByTestId("chat-suggested-replies-dialog");
    expect(dialog.querySelector("h2")?.textContent).toBe("建議回覆 · 外包 · O-1");
    expect(screen.queryAllByRole("alert").map((a) => a.textContent)).toEqual([]);
    fireEvent.click(screen.getByRole("button", { name: "關閉" }));

    await go("mira");
    await waitFor(() => expect(roomError()).toBe(SAVE_ERROR));

    fireEvent.click(screen.getByTestId("chat-header-suggested-replies"));
    expect(screen.getByTestId("chat-suggested-replies-dialog").contains(screen.getByRole("alert"))).toBe(true);
    fireEvent.change(dialogInput(), { target: { value: "這次存到" } });
    fireEvent.blur(dialogInput());
    await waitFor(() => expect(chips()).toEqual(["這次存到"]));
    expect(screen.queryAllByRole("alert").map((a) => a.textContent)).toEqual([]);
    expect(await mockApi.getChatSuggestedReplies()).toEqual({ mira: ["這次存到"] });
  });

  it("the close button on a failed save's alert clears it, and the list stays what the server holds", async () => {
    await mockApi.putChatSuggestedReplies("mira", ["繼續"]);
    vi.spyOn(api, "putChatSuggestedReplies").mockRejectedValueOnce(new Error("no"));
    openRoom("mira");
    fireEvent.click(await screen.findByTestId("chat-header-suggested-replies"));
    dialogInput().focus();
    fireEvent.change(dialogInput(), { target: { value: "沒存到" } });
    fireEvent.keyDown(window, { key: "Escape" });
    await waitFor(() => expect(roomError()).toBe(SAVE_ERROR));

    fireEvent.click(screen.getByRole("button", { name: "關閉" }));
    expect(screen.queryAllByRole("alert").map((a) => a.textContent)).toEqual([]);
    await waitFor(() => expect(chips()).toEqual(["繼續"]));
  });

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
