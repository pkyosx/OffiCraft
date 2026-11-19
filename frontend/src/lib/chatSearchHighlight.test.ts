import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { CHAT_SEARCH_HIGHLIGHT, paintChatSearchHighlights } from "./chatSearchHighlight";

class FakeHighlight {
  ranges: Range[];
  constructor(...ranges: Range[]) {
    this.ranges = ranges;
  }
}

const registry = new Map<string, FakeHighlight>();
const g = globalThis as Record<string, unknown>;
const saved = { CSS: g.CSS, Highlight: g.Highlight };

function thread(html: string): HTMLElement {
  const root = document.createElement("div");
  root.innerHTML = html;
  document.body.appendChild(root);
  return root;
}

describe("paintChatSearchHighlights", () => {
  beforeEach(() => {
    registry.clear();
    g.CSS = { highlights: registry };
    g.Highlight = FakeHighlight;
  });
  afterEach(() => {
    g.CSS = saved.CSS;
    g.Highlight = saved.Highlight;
    document.body.innerHTML = "";
  });

  it("registers every ASCII-case-insensitive occurrence in the message texts — and a reply card's question body and free-text answer, not its option labels or buttons — of searchable rows only, including one split across elements, and leaves the DOM untouched", () => {
    const html =
      '<div data-msg-id="c-1"><div class="chat__msg-text">Deploy 完成，再 <b>dep</b>loy</div>' +
      '<div class="chat__msg-time">deploy</div></div>' +
      '<div data-msg-id="c-2"><div class="chat__msg-text">no match</div></div>' +
      '<div data-msg-id="c-3"><div class="chat__msg-text">deploy between two members</div></div>' +
      '<div data-msg-id="c-4"><div class="chat__msg-text">DEPLOY</div></div>' +
      '<div data-msg-id="c-5"><article class="reply-card">' +
      '<div class="reply-card__summary">要寄出嗎?</div>' +
      '<div class="reply-card__body">先 deploy 到 staging</div>' +
      '<button class="reply-card__toggle">deploy</button></article></div>' +
      '<div data-msg-id="c-6"><article class="reply-card">' +
      '<span class="reply-card__collapsed-summary">要寄出嗎?</span>' +
      '<span class="reply-card__answer-text"><span class="reply-card__answer-option">Deploy now</span>' +
      '<span class="reply-card__answer-free">明天再 Deploy</span></span></article></div>';
    const root = thread(html);
    const cleanup = paintChatSearchHighlights(root, " deploy ", (id) => id !== "c-3");

    expect([...registry.keys()]).toEqual([CHAT_SEARCH_HIGHLIGHT]);
    expect(
      registry.get(CHAT_SEARCH_HIGHLIGHT)!.ranges.map((r) => [
        r.startContainer.parentElement!.closest("[data-msg-id]")!.getAttribute("data-msg-id"),
        r.toString(),
      ]),
    ).toEqual([
      ["c-1", "Deploy"],
      ["c-1", "deploy"],
      ["c-4", "DEPLOY"],
      ["c-5", "deploy"],
      ["c-6", "Deploy"],
    ]);
    expect(
      registry.get(CHAT_SEARCH_HIGHLIGHT)!.ranges.slice(-2).map((r) => r.startContainer.parentElement!.className),
    ).toEqual(["reply-card__body", "reply-card__answer-free"]);
    expect(root.innerHTML).toBe(html);

    cleanup();
    expect([...registry.keys()]).toEqual([]);
  });

  it("without the Custom Highlight API, registers nothing and does not throw", () => {
    g.CSS = {};
    const root = thread('<div data-msg-id="c-1"><div class="chat__msg-text">deploy</div></div>');
    paintChatSearchHighlights(root, "deploy", () => true)();
    expect(registry.size).toBe(0);
  });
});
