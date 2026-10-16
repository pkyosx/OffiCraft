import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import { __resetMock, __injectMockChat } from "../api/mock";

const realFetch = globalThis.fetch;

beforeEach(() => {
  __resetMock();
  localStorage.clear();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
  globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "# 週報" })) as unknown as typeof fetch;
});
afterEach(() => {
  globalThis.fetch = realFetch;
});

describe("OfficePage — comments on a chat attachment", () => {
  it("lets the owner send once the roster has loaded with the sender on it", async () => {
    __injectMockChat({
      id: "m-doc",
      from: "mira",
      to: "owner",
      body: "週報在附件",
      ts: Date.now() / 1000 - 60,
      attachments: [
        { id: "att-gggggggggggg", url: "/api/chat/attachment/att-gggggggggggg", filename: "weekly.md", mime: "text/markdown", isImage: false },
      ],
      replyCardId: null,
    });
    localStorage.setItem(
      "oc_doc_comments:att-gggggggggggg",
      JSON.stringify([{ id: "c1", excerpt: "週報", comment: "補日期" }]),
    );
    window.location.hash = "#office/chat/mira";
    render(
      <I18nProvider>
        <OfficePage />
      </I18nProvider>,
    );

    fireEvent.click(await screen.findByRole("button", { name: "weekly.md" }));
    await screen.findByRole("heading", { name: "週報" });
    fireEvent.click(screen.getByRole("button", { name: "註解" }));
    await waitFor(() =>
      expect((screen.getByRole("button", { name: "送出給 AI" }) as HTMLButtonElement).disabled).toBe(false),
    );
    expect(screen.queryByText("對方已不在名冊上（已移除或已釋出），無法送出。")).toBeNull();
  });

  it("names the reason once the roster has loaded without the sender", async () => {
    const goneId = "m-1892d870ded7";
    __injectMockChat({
      id: "m-gone-doc",
      from: goneId,
      to: "owner",
      body: "離職前交的文件",
      ts: Date.now() / 1000 - 60,
      attachments: [
        { id: "att-hhhhhhhhhhhh", url: "/api/chat/attachment/att-hhhhhhhhhhhh", filename: "handover.md", mime: "text/markdown", isImage: false },
      ],
      replyCardId: null,
    });
    window.location.hash = `#office/chat/${goneId}`;
    render(
      <I18nProvider>
        <OfficePage />
      </I18nProvider>,
    );

    fireEvent.click(await screen.findByRole("button", { name: "handover.md" }));
    await screen.findByRole("heading", { name: "週報" });
    fireEvent.click(screen.getByRole("button", { name: "註解" }));
    await screen.findByText("對方已不在名冊上（已移除或已釋出），無法送出。");
    expect((screen.getByRole("button", { name: "送出給 AI" }) as HTMLButtonElement).disabled).toBe(true);
  });
});
