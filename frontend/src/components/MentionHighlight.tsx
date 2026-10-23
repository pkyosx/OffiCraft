import { useEffect, useLayoutEffect, useRef, type RefObject } from "react";
import type { MentionRange } from "../lib/mentionLinks";
import "./chat-mention.css";

// Everything that decides where a glyph lands inside the textarea's box.
const MIRRORED_STYLE = [
  "fontFamily",
  "fontSize",
  "fontWeight",
  "fontStyle",
  "fontVariant",
  "fontStretch",
  "fontFeatureSettings",
  "fontKerning",
  "letterSpacing",
  "wordSpacing",
  "lineHeight",
  "textTransform",
  "textIndent",
  "textAlign",
  "textRendering",
  "tabSize",
  "direction",
  "whiteSpace",
  "overflowWrap",
  "wordBreak",
  "boxSizing",
  "paddingTop",
  "paddingRight",
  "paddingBottom",
  "paddingLeft",
  "borderTopWidth",
  "borderLeftWidth",
  "borderBottomWidth",
] as const;

function mirror(input: HTMLTextAreaElement, layer: HTMLDivElement): void {
  const cs = getComputedStyle(input);
  for (const prop of MIRRORED_STYLE) layer.style[prop] = cs[prop];
  // A vertical scrollbar narrows the textarea's text column; widening the
  // right border by the same amount keeps every line wrapping at the same word.
  const left = parseFloat(cs.borderLeftWidth) || 0;
  const right = parseFloat(cs.borderRightWidth) || 0;
  const scrollbar = Math.max(0, input.offsetWidth - input.clientWidth - left - right);
  layer.style.borderRightWidth = `${right + scrollbar}px`;
  layer.style.top = `${input.offsetTop}px`;
  layer.style.left = `${input.offsetLeft}px`;
  layer.style.width = `${input.offsetWidth}px`;
  layer.style.height = `${input.offsetHeight}px`;
  layer.scrollTop = input.scrollTop;
}

/** Marks the mentions of a composer textarea: a transparent copy of its text
 * laid over it, where only the `@name` runs carry a tint. Render it as the
 * textarea's next sibling inside a positioned container. */
export function MentionHighlight({
  inputRef,
  text,
  ranges,
}: {
  inputRef: RefObject<HTMLTextAreaElement | null>;
  text: string;
  ranges: MentionRange[];
}) {
  const layerRef = useRef<HTMLDivElement>(null);
  const shown = ranges.length > 0;

  useLayoutEffect(() => {
    const input = inputRef.current;
    if (!input || !layerRef.current) return;
    mirror(input, layerRef.current);
    // The composer sizes the textarea in its own layout effect, which runs
    // after this one; re-read once that has landed, before the frame paints.
    const frame = requestAnimationFrame(() => {
      if (layerRef.current) mirror(input, layerRef.current);
    });
    return () => cancelAnimationFrame(frame);
  });

  useEffect(() => {
    const input = inputRef.current;
    if (!shown || !input) return;
    const sync = () => {
      if (layerRef.current) mirror(input, layerRef.current);
    };
    input.addEventListener("scroll", sync);
    const ro = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(sync);
    ro?.observe(input);
    return () => {
      input.removeEventListener("scroll", sync);
      ro?.disconnect();
    };
  }, [inputRef, shown]);

  if (!shown) return null;
  const pieces: Array<string | JSX.Element> = [];
  let last = 0;
  for (const r of ranges) {
    pieces.push(text.slice(last, r.start));
    pieces.push(
      <mark key={r.start} className="mention-highlight__mark">
        {text.slice(r.start, r.end)}
      </mark>,
    );
    last = r.end;
  }
  // The trailing space keeps a final empty line as tall as the textarea's, so
  // both scroll to the same bottom.
  pieces.push(`${text.slice(last)} `);
  return (
    <div ref={layerRef} className="mention-highlight" aria-hidden="true" data-testid="mention-highlight">
      {pieces}
    </div>
  );
}
