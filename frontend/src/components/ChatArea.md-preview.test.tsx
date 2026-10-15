// T-a1c4 / T-7bc2: a .md chat attachment's chip IS the in-cockpit 預覽
// trigger (a <button>, not the download <a>) — owner 2026-07-21 moved this
// off a separate hover-revealed 眼睛 button onto the chip itself, same
// click-target contract as an image thumbnail. A non-markdown attachment
// (pdf) stays a plain download <a>.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { ChatArea } from "./ChatArea";
import { AttachmentModalHost } from "./AttachmentModalHost";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";

let messages: ChatMessage[] = [];

// The stand-in answers PER ROOM, like the real hook: one instance of `useChat`
// belongs to one peer (T-48, R13-5), so another room's thread is empty here
// rather than being the same array under a different header.
vi.mock("../hooks/useChat", () => ({
  useChat: (withId: string) => ({
    messages: withId === "m1" ? messages : [],
    peerLastReadTs: 0,
    send: vi.fn(() => Promise.resolve()),
    markRead: vi.fn(() => Promise.resolve()),
  }),
}));

function mkMember(id = "m1", name = "Mira"): Member {
  return {
    id,
    name,
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "staff",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    terminalAttachCommand: `tmux -L officraft attach -t member-${id}`,
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

function msgWith(attachments: ChatMessage["attachments"]): ChatMessage {
  return {
    id: "msg1",
    from: "m1",
    to: "owner",
    body: "here",
    ts: 1000,
    replyCardId: null,
    attachments,
  };
}

beforeEach(() => {
  localStorage.setItem("oc_token", "jwt-1");
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(() => vi.restoreAllMocks());

describe("chat .md preview action (T-a1c4 / T-7bc2)", () => {
  it("renders every stored file chip as a popup trigger", () => {
    messages = [
      msgWith([
        { id: "a-md", url: "/api/chat/attachment/a-md", filename: "design.md", mime: "text/markdown", isImage: false },
        { id: "a-pdf", url: "/api/chat/attachment/a-pdf", filename: "report.pdf", mime: "application/pdf", isImage: false },
      ]),
    ];
    const { container } = render(
      <I18nProvider>
        <ChatArea member={mkMember()} />
      </I18nProvider>,
    );
    expect(container.querySelectorAll("button.chat__msg-file").length).toBe(2);
    expect(container.querySelectorAll("a.chat__msg-file").length).toBe(0);
  });

  it("opens the preview overlay and renders the markdown on click", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# Design\n\nthe **plan**",
    })) as unknown as typeof fetch;
    messages = [
      msgWith([
        { id: "a-md", url: "/api/chat/attachment/a-md", filename: "design.md", mime: "text/markdown", isImage: false },
      ]),
    ];
    const { container, getByRole } = render(
      <I18nProvider>
        <ChatArea member={mkMember()} />
      </I18nProvider>,
    );
    fireEvent.click(container.querySelector("button.chat__msg-file")!);
    await waitFor(() => expect(getByRole("heading", { name: "Design" })).toBeTruthy());
    // Preview and download are separate: the overlay carries its own 下載 link.
    const dl = document.body.querySelector("a.md-preview__download") as HTMLAnchorElement;
    expect(dl.getAttribute("download")).toBe("design.md");
  });

  it("an open document preview does not survive the room it was opened in", async () => {
    // 🔴 R10-1 / R11-1 — the leak the tenth review MEASURED, on the overlay that
    // actually opens it. `ChatArea` used to be reused across conversations while
    // `useChat` swapped its thread one commit later, so there was a paintable
    // frame with Bruno's header over Alice's messages, and the file chip's
    // overlay (AttachmentStrip's own state, NOT ChatArea.mdPreview) sat on top
    // of it still showing Alice's filename and Alice's content.
    //
    // 🔴 WHAT CLOSES IT NOW IS THE MOUNT (T-48, R13-5). The room is entered by
    // mounting under `key={peerId}`, so leaving it unmounts the whole subtree —
    // chip, strip and overlay. The previous fix was a render-time filter
    // (`shownMessages`) that refused to paint another room's messages; it went
    // with the frame it existed for. Driven here the way `OfficePage` drives it.
    //
    // ⚠️ THIS TEST SUPPLIES ITS OWN KEY, so it cannot see a key removed from
    // `OfficePage` — `lint-chat-area-key` is what goes red for that. What it
    // still catches is an overlay that outlives whatever opened it.
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# 機密",
    })) as unknown as typeof fetch;
    messages = [
      msgWith([
        { id: "a-md", url: "/api/chat/attachment/a-md", filename: "A的機密.md", mime: "text/markdown", isImage: false },
      ]),
    ];
    const mira = mkMember();
    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea key={mira.id} member={mira} />
      </I18nProvider>,
    );
    fireEvent.click(container.querySelector("button.chat__msg-file")!);
    await waitFor(() =>
      expect(document.body.querySelector(".md-preview")).toBeTruthy(),
    );

    // Walk into Bruno's room.
    const bruno = mkMember("m2", "Bruno");
    rerender(
      <I18nProvider>
        <ChatArea key={bruno.id} member={bruno} />
      </I18nProvider>,
    );
    expect(document.body.querySelector(".md-preview")).toBeNull();
    expect(document.body.textContent).not.toContain("A的機密.md");
  });

  it("carries a 複製分享連結 button that mints THIS attachment's share link", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# Design",
    })) as unknown as typeof fetch;
    const mint = vi
      .spyOn(api, "getChatAttachmentShareLink")
      .mockResolvedValue("/api/chat/attachment/a-md?sig=test-sig");
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    messages = [
      msgWith([
        { id: "a-md", url: "/api/chat/attachment/a-md", filename: "design.md", mime: "text/markdown", isImage: false },
      ]),
    ];
    const { container, getByRole } = render(
      <I18nProvider>
        <ChatArea member={mkMember()} />
      </I18nProvider>,
    );
    fireEvent.click(container.querySelector("button.chat__msg-file")!);
    await waitFor(() => expect(getByRole("heading", { name: "Design" })).toBeTruthy());

    const actions = document.body.querySelector(".md-preview__actions") as HTMLElement;
    const share = within(actions).getByRole("button", { name: "複製分享連結" });
    fireEvent.click(share);
    await waitFor(() => expect(mint).toHaveBeenCalledWith("a-md"));
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith(
        `${window.location.origin}/api/chat/attachment/a-md?sig=test-sig`,
      ),
    );
  });
});

describe("chat .md attachment comments", () => {
  const md = (id: string) => ({
    id,
    url: `/api/chat/attachment/${id}`,
    filename: `${id}.md`,
    mime: "text/markdown",
    isImage: false,
  });

  async function sendSeededComment(chipName: string, members: Member[], rosterSettled?: boolean) {
    localStorage.setItem(
      `oc_doc_comments:${chipName}`,
      JSON.stringify([{ id: "c1", excerpt: "Design", comment: "ok" }]),
    );
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "# Design" })) as unknown as typeof fetch;
    const view = render(
      <I18nProvider>
        <ChatArea member={mkMember()} members={members} rosterSettled={rosterSettled} />
      </I18nProvider>,
    );
    const expand = within(view.container).queryByRole("button", { name: /展開/ });
    if (expand) fireEvent.click(expand);
    fireEvent.click(within(view.container).getByRole("button", { name: `${chipName}.md` }));
    await waitFor(() => expect(view.getByRole("heading", { name: "Design" })).toBeTruthy());
    fireEvent.click(view.getByRole("button", { name: "註解" }));
    return view;
  }

  it("addresses comments on an agent's attachment to that agent, and on the owner's own to the room's peer", async () => {
    const post = vi.spyOn(api, "postChat").mockResolvedValue(undefined);
    messages = [
      { ...msgWith([md("att-from-nova")]), id: "msg-n", from: "m2", to: "m1" },
      { ...msgWith([md("att-from-owner")]), id: "msg-o", from: "owner", to: "m1" },
    ];
    const roster = [mkMember(), mkMember("m2", "Nova")];

    const first = await sendSeededComment("att-from-nova", roster, true);
    expect(first.getByText("Nova")).toBeTruthy();
    fireEvent.click(first.getByRole("button", { name: "送出給 AI" }));
    await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
    first.unmount();

    const second = await sendSeededComment("att-from-owner", roster, true);
    fireEvent.click(second.getByRole("button", { name: "送出給 AI" }));
    await waitFor(() => expect(post).toHaveBeenCalledTimes(2));
    expect(post.mock.calls.map(([arg]) => arg.to)).toEqual(["m2", "m1"]);
  });

  it("does not call a sender departed while the roster has not loaded", async () => {
    messages = [{ ...msgWith([md("att-from-nova")]), id: "msg-n", from: "m2", to: "m1" }];
    const view = await sendSeededComment("att-from-nova", [mkMember()]);
    expect((view.getByRole("button", { name: "送出給 AI" }) as HTMLButtonElement).disabled).toBe(true);
    expect(view.queryByText("對方已不在名冊上（已移除或已釋出），無法送出。")).toBeNull();
  });

  it("refuses to send to a sender who is no longer on the roster, and says a webhook or scheduled sender is not a member instead", async () => {
    messages = [{ ...msgWith([md("att-from-nova")]), id: "msg-n", from: "m2", to: "m1" }];
    const view = await sendSeededComment("att-from-nova", [mkMember()], true);
    expect((view.getByRole("button", { name: "送出給 AI" }) as HTMLButtonElement).disabled).toBe(true);
    expect(view.getByText("對方已不在名冊上（已移除或已釋出），無法送出。")).toBeTruthy();
    expect(view.queryByText("這份文件不是成員寄出的，無法直接送出註解。")).toBeNull();
    view.unmount();

    for (const from of ["hook:ep-aaaa", "sched:sc-aaaa", "system"]) {
      messages = [{ ...msgWith([md("att-synthetic")]), id: "msg-s", from, to: "owner" }];
      const synthetic = await sendSeededComment("att-synthetic", [mkMember()], true);
      expect((synthetic.getByRole("button", { name: "送出給 AI" }) as HTMLButtonElement).disabled).toBe(true);
      expect(synthetic.getByText("這份文件不是成員寄出的，無法直接送出註解。")).toBeTruthy();
      expect(synthetic.queryByText("對方已不在名冊上（已移除或已釋出），無法送出。")).toBeNull();
      synthetic.unmount();
    }
  });
});

describe("chat message links to a stored attachment", () => {
  const DOC = "/api/chat/attachment/att-aaaaaaaaaaaa";
  const body = [
    `**文件註解**：[設計.md](${DOC})`,
    "",
    "1. 原文：",
    "   > 圖片：[架構圖.png](/api/chat/attachment/att-bbbbbbbbbbbb)",
    "",
    "   註解：看 [規格](https://example.com/spec)，以及 [架構圖](/api/chat/attachment/att-cccccccccccc)",
  ].join("\n");

  function readingTheCommentMessage() {
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "# 設計稿" })) as unknown as typeof fetch;
    vi.spyOn(api, "getChatAttachmentShareLink").mockResolvedValue("/api/chat/attachment/x?sig=s");
    const windowOpen = vi.spyOn(window, "open").mockReturnValue(null);
    messages = [{ ...msgWith([]), body }];
    const view = render(
      <I18nProvider>
        <AttachmentModalHost>
          <ChatArea member={mkMember()} />
        </AttachmentModalHost>
      </I18nProvider>,
    );
    const expand = within(view.container).queryByRole("button", { name: /展開/ });
    if (expand) fireEvent.click(expand);
    const link = (name: string) => within(view.container).getByRole("link", { name });
    return { view, windowOpen, link };
  }

  const overlay = () => document.body.querySelector<HTMLElement>(".md-preview[role=dialog]");

  it("previews the commented document in place on a plain click, with no token in any address", async () => {
    const before = window.location.href;
    const { view, windowOpen, link } = readingTheCommentMessage();
    const docLink = link("設計.md");
    expect(docLink.getAttribute("href")).toBe(DOC);

    expect(fireEvent.click(docLink)).toBe(false);
    await waitFor(() => expect(view.getByRole("heading", { name: "設計稿" })).toBeTruthy());
    expect(overlay()?.getAttribute("aria-label")).toBe("設計.md");
    expect(overlay()?.querySelector(".md-preview__title")?.textContent).toContain("設計.md");
    expect(globalThis.fetch).toHaveBeenCalledWith("/api/chat/attachment/att-aaaaaaaaaaaa?token=jwt-1");
    expect(windowOpen).not.toHaveBeenCalled();
    expect(window.location.href).toBe(before);
    expect(docLink.getAttribute("href")).toBe(DOC);
    expect(within(overlay()!).queryByRole("button", { name: "註解" })).toBeNull();
  });

  it("previews in place on a modified or middle click too, since a new tab would have no token", async () => {
    const { windowOpen, link } = readingTheCommentMessage();
    const clicks: [string, (a: HTMLElement) => boolean][] = [
      ["meta", (a) => fireEvent.click(a, { metaKey: true })],
      ["ctrl", (a) => fireEvent.click(a, { ctrlKey: true })],
      ["shift", (a) => fireEvent.click(a, { shiftKey: true })],
      ["middle", (a) => fireEvent(a, new MouseEvent("auxclick", { bubbles: true, cancelable: true, button: 1 }))],
    ];
    for (const [, click] of clicks) {
      expect(click(link("設計.md"))).toBe(false);
      await waitFor(() => expect(overlay()?.getAttribute("aria-label")).toBe("設計.md"));
      fireEvent.keyDown(window, { key: "Escape" });
      await waitFor(() => expect(overlay()).toBeNull());
    }
    expect(windowOpen).not.toHaveBeenCalled();
  });

  it("previews an image comment's link as that image, typed by its link text's extension", async () => {
    const { link } = readingTheCommentMessage();
    fireEvent.click(link("架構圖.png"));
    await waitFor(() => expect(overlay()?.querySelector("img")).toBeTruthy());
    expect(overlay()?.querySelector("img")?.getAttribute("src")).toBe(
      "/api/chat/attachment/att-bbbbbbbbbbbb?token=jwt-1",
    );
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("asks the server for the type when the link text has no extension, loading meanwhile, and shows the image it answers", async () => {
    const { link } = readingTheCommentMessage();
    let answer!: (r: unknown) => void;
    globalThis.fetch = vi.fn(
      () => new Promise((resolve) => (answer = resolve)),
    ) as unknown as typeof fetch;
    const flowChart = link("架構圖");

    fireEvent.click(flowChart);
    await waitFor(() => expect(overlay()?.getAttribute("aria-label")).toBe("架構圖"));
    expect(within(overlay()!).getByText("載入預覽中…")).toBeTruthy();
    expect(globalThis.fetch).toHaveBeenCalledTimes(1);
    expect(globalThis.fetch).toHaveBeenCalledWith("/api/chat/attachment/att-cccccccccccc?token=jwt-1", {
      method: "HEAD",
    });

    answer({ ok: true, headers: new Headers({ "content-type": "image/png" }) });
    await waitFor(() => expect(overlay()?.querySelector("img")).toBeTruthy());
    expect(overlay()?.querySelector("img")?.getAttribute("src")).toBe(
      "/api/chat/attachment/att-cccccccccccc?token=jwt-1",
    );
    expect(within(overlay()!).queryByText("載入預覽中…")).toBeNull();
    expect(globalThis.fetch).toHaveBeenCalledTimes(1);
    expect(flowChart.getAttribute("href")).toBe("/api/chat/attachment/att-cccccccccccc");
  });

  it("shows the not-previewable view when the type question fails", async () => {
    const { link } = readingTheCommentMessage();
    globalThis.fetch = vi.fn(async () => ({ ok: false, status: 500, headers: new Headers() })) as unknown as typeof fetch;
    vi.spyOn(console, "warn").mockImplementation(() => {});
    fireEvent.click(link("架構圖"));
    await waitFor(() => expect(within(overlay()!).getByText("此檔案無法預覽，請下載")).toBeTruthy());
    expect(overlay()?.querySelector("img")).toBeNull();
    expect(within(overlay()!).queryByText("載入預覽中…")).toBeNull();
  });

  it("leaves an external link in the same message to open its new tab", () => {
    const { link } = readingTheCommentMessage();
    const external = link("規格");
    expect(external.getAttribute("href")).toBe("https://example.com/spec");
    expect(external.getAttribute("target")).toBe("_blank");
    expect(fireEvent.click(external)).toBe(true);
    expect(overlay()).toBeNull();
  });
});
