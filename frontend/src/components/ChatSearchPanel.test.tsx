import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, act, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ChatSearchPanel } from "./ChatSearchPanel";
import { useChatSearch } from "../hooks/useChatSearch";
import type { ChatMessage, ChatSearchPage } from "../api/adapter";

type Pending = {
  args: [string, string, string | undefined];
  resolve: (p: ChatSearchPage) => void;
  reject: (e: unknown) => void;
};
let pending: Pending[] = [];

vi.mock("../api", () => ({
  api: {
    searchChat: (withId: string, q: string, cursor?: string) =>
      new Promise<ChatSearchPage>((resolve, reject) => {
        pending.push({ args: [withId, q, cursor], resolve, reject });
      }),
  },
}));

const NOV18_0905 = 1794963900;
const NOV17_2140 = 1794922800;
const DEC31_2025_2310 = 1767193800;
const NOW = 1795060800;

function msg(id: string, from: string, body: string, ts: number): ChatMessage {
  return { id, from, to: from === "owner" ? "m-1" : "owner", body, ts, attachments: [], replyCardId: null };
}

function Harness({ onJump, onClose }: { onJump: (id: string) => void; onClose: () => void }) {
  const search = useChatSearch("m-1");
  return (
    <ChatSearchPanel
      search={search}
      labelOf={(m) => (m.from === "owner" ? zh.chat.me : "Mira")}
      onJump={onJump}
      onClose={onClose}
    />
  );
}

function renderPanel() {
  const onJump = vi.fn();
  const onClose = vi.fn();
  const utils = render(
    <I18nProvider>
      <Harness onJump={onJump} onClose={onClose} />
    </I18nProvider>,
  );
  const input = screen.getByPlaceholderText(zh.chat.searchPlaceholder) as HTMLInputElement;
  return { ...utils, onJump, onClose, input };
}

async function answer(i: number, page: ChatSearchPage) {
  await act(async () => {
    pending[i].resolve(page);
  });
}

function rows(container: HTMLElement) {
  return Array.from(container.querySelectorAll(".chat-search__item")).map((r) => ({
    who: r.querySelector(".chat-search__who")!.textContent,
    time: r.querySelector(".chat-search__time")!.textContent,
    snippet: r.querySelector(".chat-search__snippet")!.textContent,
    marks: Array.from(r.querySelectorAll("mark")).map((m) => m.textContent),
  }));
}

function stateText(container: HTMLElement) {
  return Array.from(container.querySelectorAll(".chat-search__state")).map((n) => n.textContent);
}

describe("ChatSearchPanel", () => {
  beforeEach(() => {
    pending = [];
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(NOW * 1000);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("when typing without Enter, sends no search; Enter then searches the trimmed query and shows 搜尋中…", () => {
    const { input, container } = renderPanel();
    expect(document.activeElement).toBe(input);
    fireEvent.change(input, { target: { value: "  deploy " } });
    fireEvent.blur(input);
    expect(pending).toEqual([]);

    fireEvent.keyDown(input, { key: "Enter" });
    expect(pending.map((p) => p.args)).toEqual([["m-1", "deploy", undefined]]);
    expect(stateText(container)).toEqual(["搜尋中…"]);
  });

  it("when Enter confirms an IME composition, sends no search", () => {
    const { input } = renderPanel();
    fireEvent.change(input, { target: { value: "部署" } });
    fireEvent.keyDown(input, { key: "Enter", isComposing: true });
    expect(pending).toEqual([]);
  });

  it("when the query is only spaces, neither Enter nor 搜尋 sends a search; with a query, 搜尋 sends one", () => {
    const { input } = renderPanel();
    fireEvent.change(input, { target: { value: "   " } });
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.click(screen.getByRole("button", { name: zh.chat.searchSubmit }));
    expect(pending).toEqual([]);

    fireEvent.change(input, { target: { value: "Deploy" } });
    fireEvent.click(screen.getByRole("button", { name: zh.chat.searchSubmit }));
    expect(pending.map((p) => p.args)).toEqual([["m-1", "Deploy", undefined]]);
  });

  it("when results arrive, lists them newest first with sender, time and a snippet marking every match in any ASCII case", async () => {
    const { input, container } = renderPanel();
    fireEvent.change(input, { target: { value: "deploy" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await answer(0, {
      messages: [
        msg("c-1", "owner", "DEPLOY 前先跑測試", DEC31_2025_2310),
        msg("c-2", "m-1", "x".repeat(50) + "我 deploy 了，Deploy 結果正常" + "y".repeat(50), NOV17_2140),
        msg("c-3", "m-1", "deploy 完成", NOV18_0905),
      ],
    });

    expect(container.querySelector(".chat-search__hint")!.textContent).toBe("從最新的開始列出");
    expect(rows(container)).toEqual([
      { who: "Mira", time: "11/18 09:05", snippet: "deploy 完成", marks: ["deploy"] },
      {
        who: "Mira",
        time: "11/17 21:40",
        snippet: "…" + "x".repeat(38) + "我 deploy 了，Deploy 結果正常" + "y".repeat(26) + "…",
        marks: ["deploy", "Deploy"],
      },
      { who: "我", time: "2025/12/31 23:10", snippet: "DEPLOY 前先跑測試", marks: ["DEPLOY"] },
    ]);
    expect(container.querySelector(".chat-search__older")).toBeNull();
  });

  it("when the server answers no match, says 找不到含「q」的訊息", async () => {
    const { input, container } = renderPanel();
    fireEvent.change(input, { target: { value: "rollback" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await answer(0, { messages: [] });
    expect(stateText(container)).toEqual(["找不到含「rollback」的訊息"]);
    expect(container.querySelector(".chat-search__item")).toBeNull();
  });

  it("when the search fails, says it failed instead of saying nothing matched", async () => {
    const { input, container } = renderPanel();
    fireEvent.change(input, { target: { value: "deploy" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await act(async () => {
      pending[0].reject(new Error("502"));
    });
    expect(stateText(container)).toEqual([zh.chat.searchFailed]);
  });

  it("with a next cursor, 載入更早的結果 fetches the older page with the same query and appends it newest first", async () => {
    const { input, container } = renderPanel();
    fireEvent.change(input, { target: { value: "deploy" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await answer(0, {
      messages: [msg("c-5", "m-1", "deploy 5", NOV18_0905 + 5), msg("c-6", "m-1", "deploy 6", NOV18_0905 + 6)],
      nextCursor: "cur-1",
    });

    fireEvent.change(input, { target: { value: "typed but not searched" } });
    fireEvent.click(screen.getByRole("button", { name: zh.chat.searchLoadOlder }));
    expect(pending[1].args).toEqual(["m-1", "deploy", "cur-1"]);
    await answer(1, {
      messages: [msg("c-3", "m-1", "deploy 3", NOV18_0905 + 3), msg("c-4", "m-1", "deploy 4", NOV18_0905 + 4)],
    });

    expect(rows(container).map((r) => r.snippet)).toEqual(["deploy 6", "deploy 5", "deploy 4", "deploy 3"]);
    expect(container.querySelector(".chat-search__older")).toBeNull();
  });

  it("when an earlier search answers after a newer one, keeps the newer search's results", async () => {
    const { input, container } = renderPanel();
    fireEvent.change(input, { target: { value: "alpha" } });
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.change(input, { target: { value: "beta" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await answer(1, { messages: [msg("c-2", "m-1", "beta here", NOV18_0905)] });
    await answer(0, { messages: [msg("c-1", "m-1", "alpha here", NOV17_2140)] });

    expect(rows(container).map((r) => r.snippet)).toEqual(["beta here"]);
  });

  it("clicking a result asks to jump to that message and leaves the panel open; Esc closes it", async () => {
    const { input, container, onJump, onClose } = renderPanel();
    fireEvent.change(input, { target: { value: "deploy" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await answer(0, { messages: [msg("c-9", "m-1", "deploy", NOV18_0905)] });

    fireEvent.click(container.querySelector(".chat-search__item")!);
    expect(onJump.mock.calls).toEqual([["c-9"]]);
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
