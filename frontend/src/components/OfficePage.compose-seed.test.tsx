import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import { ReplyCardsProvider } from "../hooks/useReplyCards";
import { __resetWorkerCodenameCache } from "../hooks/useWorkerCodenames";
import { resetAllSharedSnapshots } from "../lib/sharedSnapshot";
import { __injectMockOutsourceWorker, __resetMock } from "../api/mock";
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

function openCompose(no: string) {
  window.location.hash = `#office/chat/ow-a/compose/${no}`;
  const { container } = render(
    <I18nProvider>
      <ReplyCardsProvider>
        <OfficePage />
      </ReplyCardsProvider>
    </I18nProvider>,
  );
  return () => (container.querySelector(".chat__input") as HTMLTextAreaElement | null)?.value;
}

beforeEach(() => {
  vi.restoreAllMocks();
  __resetMock();
  resetAllSharedSnapshots();
  __resetWorkerCodenameCache();
  Element.prototype.scrollIntoView = vi.fn();
  __injectMockOutsourceWorker(worker);
});

describe("OfficePage compose seed", () => {
  it("opens a chat linked from a 傳承 card with [LoreID=…]", async () => {
    const lore = openCompose("L-7");
    await waitFor(() => expect(lore()).toBe("[LoreID=L-7] "));
  });

  it("keeps a task number bare", async () => {
    const task = openCompose("T-9");
    await waitFor(() => expect(task()).toBe("[T-9] "));
  });
});
