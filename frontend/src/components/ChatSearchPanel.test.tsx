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

function Harness({ onPick, onClose }: { onPick: (id: string) => void; onClose: () => void }) {
  const search = useChatSearch("m-1", () => {});
  return (
    <>
      <input
        aria-label="query"
        value={search.draft}
        onChange={(e) => search.setDraft(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && search.submit()}
      />
      <ChatSearchPanel
        search={search}
        labelOf={(m) => (m.from === "owner" ? zh.chat.me : "Mira")}
        onPick={(id) => {
          search.pick(id);
          onPick(id);
        }}
        onClose={onClose}
      />
    </>
  );
}

function renderPanel() {
  const onPick = vi.fn();
  const onClose = vi.fn();
  const utils = render(
    <I18nProvider>
      <Harness onPick={onPick} onClose={onClose} />
    </I18nProvider>,
  );
  const input = screen.getByLabelText("query") as HTMLInputElement;
  return { ...utils, onPick, onClose, input };
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
      matchCount: 3,
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
    await answer(0, { messages: [], matchCount: 0 });
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
      matchCount: 4,
    });

    fireEvent.change(input, { target: { value: "typed but not searched" } });
    fireEvent.click(screen.getByRole("button", { name: zh.chat.searchLoadOlder }));
    expect(pending[1].args).toEqual(["m-1", "deploy", "cur-1"]);
    await answer(1, {
      messages: [msg("c-3", "m-1", "deploy 3", NOV18_0905 + 3), msg("c-4", "m-1", "deploy 4", NOV18_0905 + 4)],
      matchCount: 4,
    });

    expect(rows(container).map((r) => r.snippet)).toEqual(["deploy 6", "deploy 5", "deploy 4", "deploy 3"]);
    expect(container.querySelector(".chat-search__older")).toBeNull();
  });

  it("clicking a result makes it the current one and leaves the panel to its host; Esc closes it", async () => {
    const { input, container, onPick, onClose } = renderPanel();
    fireEvent.change(input, { target: { value: "deploy" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await answer(0, {
      messages: [msg("c-8", "m-1", "deploy 8", NOV18_0905), msg("c-9", "m-1", "deploy 9", NOV18_0905 + 1)],
      matchCount: 2,
    });
    const current = () =>
      Array.from(container.querySelectorAll(".chat-search__item")).map((r) => r.getAttribute("aria-current"));
    expect(current()).toEqual(["true", null]);

    fireEvent.click(container.querySelectorAll(".chat-search__item")[1]);
    expect(onPick.mock.calls).toEqual([["c-8"]]);
    expect(current()).toEqual([null, "true"]);
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
