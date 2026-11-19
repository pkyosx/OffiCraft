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

  it("registers every ASCII-case-insensitive occurrence in message texts only, including one split across elements, and leaves the DOM untouched", () => {
    const html =
      '<div class="chat__msg-text">Deploy 完成，再 <b>dep</b>loy</div>' +
      '<div class="chat__msg-time">deploy</div>' +
      '<div class="chat__msg-text">no match</div>' +
      '<div class="chat__msg-text">DEPLOY</div>';
    const root = thread(html);
    const cleanup = paintChatSearchHighlights(root, " deploy ");

    expect([...registry.keys()]).toEqual([CHAT_SEARCH_HIGHLIGHT]);
    expect(registry.get(CHAT_SEARCH_HIGHLIGHT)!.ranges.map((r) => r.toString())).toEqual([
      "Deploy",
      "deploy",
      "DEPLOY",
    ]);
    expect(root.innerHTML).toBe(html);

    cleanup();
    expect([...registry.keys()]).toEqual([]);
  });

  it("without the Custom Highlight API, registers nothing and does not throw", () => {
    g.CSS = {};
    const root = thread('<div class="chat__msg-text">deploy</div>');
    paintChatSearchHighlights(root, "deploy")();
    expect(registry.size).toBe(0);
  });
});
