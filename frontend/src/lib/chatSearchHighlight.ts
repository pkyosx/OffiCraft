import { asciiLower } from "./chatSearch";

export const CHAT_SEARCH_HIGHLIGHT = "oc-chat-search";

type HighlightRegistry = { set(name: string, h: unknown): void; delete(name: string): void };
type HighlightCtor = new (...ranges: Range[]) => unknown;

function highlightApi(): { registry: HighlightRegistry; Highlight: HighlightCtor } | null {
  const registry = (globalThis.CSS as { highlights?: HighlightRegistry } | undefined)?.highlights;
  const Highlight = (globalThis as { Highlight?: HighlightCtor }).Highlight;
  return registry && Highlight ? { registry, Highlight } : null;
}

/** Every occurrence of `query` inside `el`'s text, as ranges that may span
 * element boundaries (a keyword half in bold still matches). */
function rangesIn(el: Element, needle: string): Range[] {
  const nodes: { node: Text; start: number }[] = [];
  let text = "";
  const walker = el.ownerDocument.createTreeWalker(el, NodeFilter.SHOW_TEXT);
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    nodes.push({ node: n as Text, start: text.length });
    text += (n as Text).data;
  }
  const hay = asciiLower(text);
  const at = (offset: number, end: boolean) => {
    let i = nodes.length - 1;
    while (i > 0 && (end ? nodes[i].start >= offset : nodes[i].start > offset)) i -= 1;
    return { node: nodes[i].node, offset: offset - nodes[i].start };
  };
  const ranges: Range[] = [];
  for (let hit = hay.indexOf(needle); hit >= 0; hit = hay.indexOf(needle, hit + needle.length)) {
    const from = at(hit, false);
    const to = at(hit + needle.length, true);
    const range = el.ownerDocument.createRange();
    range.setStart(from.node, from.offset);
    range.setEnd(to.node, to.offset);
    ranges.push(range);
  }
  return ranges;
}

/** Paints every occurrence of `query` in the message texts under `root`
 * through the CSS Custom Highlight API — the message DOM is never touched —
 * but only in rows (`[data-msg-id]`) that `searchable` admits, so nothing is
 * marked that the search itself can never step to. Returns the cleanup.
 * Without the API it paints nothing. */
export function paintChatSearchHighlights(
  root: Element | null,
  query: string,
  searchable: (msgId: string) => boolean,
): () => void {
  const hl = highlightApi();
  const needle = asciiLower(query.trim());
  if (!hl || !root || needle === "") return () => {};
  const ranges = Array.from(root.querySelectorAll(".chat__msg-text"))
    .filter((el) => searchable(el.closest("[data-msg-id]")?.getAttribute("data-msg-id") ?? ""))
    .flatMap((el) => rangesIn(el, needle));
  hl.registry.set(CHAT_SEARCH_HIGHLIGHT, new hl.Highlight(...ranges));
  return () => hl.registry.delete(CHAT_SEARCH_HIGHLIGHT);
}
