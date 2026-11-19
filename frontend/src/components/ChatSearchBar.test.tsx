import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, act, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ChatSearchBar } from "./ChatSearchBar";
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

function msg(id: string, ts: number): ChatMessage {
  return { id, from: "m-1", to: "owner", body: `deploy ${id}`, ts, attachments: [], replyCardId: null };
}

function Harness({ onJump, onExit }: { onJump: (id: string) => void; onExit: () => void }) {
  const search = useChatSearch("m-1", onJump);
  return (
    <>
      <ChatSearchBar search={search} listOpen={false} onToggleList={() => {}} onExit={onExit} />
      {search.results.map((m) => (
        <button key={m.id} type="button" aria-label={`pick ${m.id}`} onClick={() => search.pick(m.id)} />
      ))}
    </>
  );
}

function renderBar() {
  const onJump = vi.fn();
  const onExit = vi.fn();
  const utils = render(
    <I18nProvider>
      <Harness onJump={onJump} onExit={onExit} />
    </I18nProvider>,
  );
  const input = screen.getByLabelText(zh.chat.searchPlaceholder) as HTMLInputElement;
  return { ...utils, onJump, onExit, input };
}

async function answer(i: number, page: ChatSearchPage) {
  await act(async () => {
    pending[i].resolve(page);
  });
}

async function search(input: HTMLInputElement, q: string) {
  fireEvent.change(input, { target: { value: q } });
  fireEvent.keyDown(input, { key: "Enter" });
}

function bar(container: HTMLElement) {
  const step = (label: string) => {
    const b = screen.queryByLabelText(label) as HTMLButtonElement | null;
    return b === null ? "absent" : b.disabled ? "disabled" : "enabled";
  };
  return {
    status: container.querySelector(".chat-search__status")?.textContent ?? null,
    older: step(zh.chat.searchPrev),
    newer: step(zh.chat.searchNext),
  };
}

describe("ChatSearchBar", () => {
  beforeEach(() => {
    pending = [];
  });

  it("when typing without Enter, sends no search; Enter then searches the trimmed query and shows 搜尋中…", () => {
    const { input, container } = renderBar();
    expect(document.activeElement).toBe(input);
    fireEvent.change(input, { target: { value: "  deploy " } });
    fireEvent.blur(input);
    expect(pending).toEqual([]);
    expect(bar(container)).toEqual({ status: null, older: "absent", newer: "absent" });

    fireEvent.keyDown(input, { key: "Enter" });
    expect(pending.map((p) => p.args)).toEqual([["m-1", "deploy", undefined]]);
    expect(bar(container)).toEqual({ status: "搜尋中…", older: "absent", newer: "absent" });
  });

  it("when Enter confirms an IME composition or the query is only spaces, sends no search", () => {
    const { input } = renderBar();
    fireEvent.change(input, { target: { value: "部署" } });
    fireEvent.keyDown(input, { key: "Enter", isComposing: true });
    fireEvent.change(input, { target: { value: "   " } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(pending).toEqual([]);
  });

  it("after a search, jumps to the newest match as N/N; ▲ steps older and ▼ newer, each jumping, and each is disabled at its end", async () => {
    const { input, container, onJump } = renderBar();
    await search(input, "deploy");
    await answer(0, { messages: [msg("c-1", 1), msg("c-2", 2), msg("c-3", 3)], matchCount: 3 });
    expect(onJump.mock.calls).toEqual([["c-3"]]);
    expect(bar(container)).toEqual({ status: "3/3", older: "enabled", newer: "disabled" });

    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    expect(bar(container)).toEqual({ status: "2/3", older: "enabled", newer: "enabled" });
    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    expect(bar(container)).toEqual({ status: "1/3", older: "disabled", newer: "enabled" });
    fireEvent.click(screen.getByLabelText(zh.chat.searchNext));
    expect(bar(container)).toEqual({ status: "2/3", older: "enabled", newer: "enabled" });
    expect(onJump.mock.calls).toEqual([["c-3"], ["c-2"], ["c-1"], ["c-2"]]);
  });

  it("when ▲ steps past the loaded matches, fetches the next older page with the cursor first, then jumps to it", async () => {
    const { input, container, onJump } = renderBar();
    await search(input, "deploy");
    await answer(0, { messages: [msg("c-3", 3), msg("c-4", 4)], matchCount: 4, nextCursor: "cur-1" });
    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    expect(bar(container)).toEqual({ status: "3/4", older: "enabled", newer: "enabled" });

    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    expect(pending.map((p) => p.args)).toEqual([
      ["m-1", "deploy", undefined],
      ["m-1", "deploy", "cur-1"],
    ]);
    expect(bar(container)).toEqual({ status: "3/4", older: "disabled", newer: "disabled" });
    expect(onJump.mock.calls).toEqual([["c-4"], ["c-3"]]);

    await answer(1, { messages: [msg("c-1", 1), msg("c-2", 2)], matchCount: 4 });
    expect(bar(container)).toEqual({ status: "2/4", older: "enabled", newer: "enabled" });
    expect(onJump.mock.calls).toEqual([["c-4"], ["c-3"], ["c-2"]]);
  });

  it("while the older page is in flight, ▼ does nothing, and the landing page moves one step older from where ▲ was pressed", async () => {
    const { input, container, onJump } = renderBar();
    await search(input, "deploy");
    await answer(0, { messages: [msg("c-3", 3), msg("c-4", 4)], matchCount: 4, nextCursor: "cur-1" });
    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    fireEvent.click(screen.getByLabelText(zh.chat.searchNext));
    expect(onJump.mock.calls).toEqual([["c-4"], ["c-3"]]);

    await answer(1, { messages: [msg("c-1", 1), msg("c-2", 2)], matchCount: 4 });
    expect(bar(container)).toEqual({ status: "2/4", older: "enabled", newer: "enabled" });
    expect(onJump.mock.calls).toEqual([["c-4"], ["c-3"], ["c-2"]]);
  });

  it("when a result is picked while the older page is in flight, the picked match stays current after the page lands and the landing jumps nowhere", async () => {
    const { input, container, onJump } = renderBar();
    await search(input, "deploy");
    await answer(0, { messages: [msg("c-3", 3), msg("c-4", 4)], matchCount: 4, nextCursor: "cur-1" });
    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    fireEvent.click(screen.getByLabelText("pick c-4"));
    expect(onJump.mock.calls).toEqual([["c-4"], ["c-3"], ["c-4"]]);

    await answer(1, { messages: [msg("c-1", 1), msg("c-2", 2)], matchCount: 4 });
    expect(bar(container)).toEqual({ status: "4/4", older: "enabled", newer: "disabled" });
    expect(onJump.mock.calls).toEqual([["c-4"], ["c-3"], ["c-4"]]);
  });

  it("after the older page fails, ▼ brings the row back from the failed wording to i/N", async () => {
    const { input, container } = renderBar();
    await search(input, "deploy");
    await answer(0, { messages: [msg("c-3", 3), msg("c-4", 4)], matchCount: 4, nextCursor: "cur-1" });
    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    await act(async () => {
      pending[1].reject(new Error("502"));
    });
    expect(bar(container).status).toBe(zh.chat.searchFailed);

    fireEvent.click(screen.getByLabelText(zh.chat.searchNext));
    expect(bar(container)).toEqual({ status: "4/4", older: "enabled", newer: "disabled" });
    expect(container.querySelector(".chat-search__status")!.className).toBe("chat-search__status");
  });

  it("when a match arrives after the search, positions stay counted against the first answer's total", async () => {
    const { input, container } = renderBar();
    await search(input, "deploy");
    await answer(0, { messages: [msg("c-3", 3), msg("c-4", 4)], matchCount: 4, nextCursor: "cur-1" });
    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    await answer(1, { messages: [msg("c-1", 1), msg("c-2", 2)], matchCount: 5 });
    expect(bar(container).status).toBe("2/4");
    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    expect(bar(container)).toEqual({ status: "1/4", older: "disabled", newer: "enabled" });
  });

  it("when the older page fails, the row says the search failed and ▲ retries back to i/N", async () => {
    const { input, container, onJump } = renderBar();
    await search(input, "deploy");
    await answer(0, { messages: [msg("c-4", 4)], matchCount: 2, nextCursor: "cur-1" });
    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    await act(async () => {
      pending[1].reject(new Error("502"));
    });
    expect(bar(container)).toEqual({ status: zh.chat.searchFailed, older: "enabled", newer: "disabled" });
    expect(container.querySelector(".chat-search__status")!.className).toBe(
      "chat-search__status chat-search__status--error",
    );

    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    expect(pending[2].args).toEqual(["m-1", "deploy", "cur-1"]);
    await answer(2, { messages: [msg("c-1", 1)], matchCount: 2 });
    expect(bar(container)).toEqual({ status: "1/2", older: "disabled", newer: "enabled" });
    expect(onJump.mock.calls).toEqual([["c-4"], ["c-1"]]);
  });

  it("when an older page from a previous search lands after a new search, keeps the new search's results, count and current match", async () => {
    const { input, container, onJump } = renderBar();
    await search(input, "alpha");
    await answer(0, { messages: [msg("c-4", 4)], matchCount: 3, nextCursor: "cur-1" });
    fireEvent.click(screen.getByLabelText(zh.chat.searchPrev));
    await search(input, "beta");
    await answer(2, { messages: [msg("c-9", 9)], matchCount: 1 });
    await answer(1, { messages: [msg("c-1", 1), msg("c-2", 2)], matchCount: 3 });

    expect(bar(container)).toEqual({ status: "1/1", older: "disabled", newer: "disabled" });
    expect(onJump.mock.calls).toEqual([["c-4"], ["c-9"]]);
  });

  it("when nothing matches, says 找不到含「q」的訊息 where the count goes; when the search fails, says it failed", async () => {
    const { input, container } = renderBar();
    await search(input, "rollback");
    await answer(0, { messages: [], matchCount: 0 });
    expect(bar(container)).toEqual({ status: "找不到含「rollback」的訊息", older: "absent", newer: "absent" });

    await search(input, "deploy");
    await act(async () => {
      pending[1].reject(new Error("502"));
    });
    expect(bar(container)).toEqual({ status: zh.chat.searchFailed, older: "absent", newer: "absent" });
  });

  it("when an earlier search answers after a newer one, keeps the newer search's result and jumps only for it", async () => {
    const { input, container, onJump } = renderBar();
    await search(input, "alpha");
    await search(input, "beta");
    await answer(1, { messages: [msg("c-2", 2)], matchCount: 1 });
    await answer(0, { messages: [msg("c-1", 1), msg("c-9", 9)], matchCount: 2 });
    expect(bar(container).status).toBe("1/1");
    expect(onJump.mock.calls).toEqual([["c-2"]]);
  });

  it("Esc in the search row exits search mode", () => {
    const { input, onExit } = renderBar();
    fireEvent.keyDown(input, { key: "Escape" });
    expect(onExit).toHaveBeenCalledTimes(1);
  });
});
