// lib/docCommentFrame.ts — the messages between the document reader and an
// HTML attachment shown in its iframe: the page says it is `ready`, which
// marked place (`data-oc-anchor`) was picked and what text is selected; the
// reader says whether comment mode is on, which places already carry a comment,
// and which one to show.
// The page side is the snippet `ocagent upload --help` prints
// (cli/ocagent/doc_comments_snippet.html); the two must agree on this format.

import { useEffect, useRef } from "react";
import type { RefObject } from "react";

const OC = "doc-comments";
const VERSION = 1;
export const ANCHOR_ID_MAX = 128;
export const ANCHOR_LABEL_MAX = 200;
/** The chat-body cap: a longer excerpt could not be sent anyway. */
export const SELECTION_TEXT_MAX = 4000;

/** In the page's own viewport coordinates. */
export type FrameRect = { left: number; top: number; width: number; height: number };

export type FrameMessage =
  | { type: "ready" }
  | { type: "pick"; anchor: { id: string; label: string } }
  | { type: "select"; selection: { text: string; rect: FrameRect } | null };

function parseRect(value: unknown): FrameRect | null {
  if (typeof value !== "object" || value === null) return null;
  const { left, top, width, height } = value as Record<string, unknown>;
  const finite = (n: unknown): n is number => typeof n === "number" && Number.isFinite(n);
  if (!finite(left) || !finite(top) || !finite(width) || !finite(height)) return null;
  if (width < 0 || height < 0) return null;
  return { left, top, width, height };
}

/** The page's message, or null for anything this reader does not accept. The
 * page is arbitrary agent-written script, so nothing in `data` is trusted. */
export function parseFrameMessage(data: unknown): FrameMessage | null {
  if (typeof data !== "object" || data === null) return null;
  const m = data as Record<string, unknown>;
  if (m.oc !== OC || m.v !== VERSION) return null;
  if (m.type === "ready") return { type: "ready" };
  if (m.type === "select") {
    if (typeof m.text !== "string" || m.text.length > SELECTION_TEXT_MAX) return null;
    const text = m.text.trim();
    if (text === "") return { type: "select", selection: null };
    const rect = parseRect(m.rect);
    return rect ? { type: "select", selection: { text, rect } } : null;
  }
  if (m.type !== "pick" || typeof m.anchor !== "object" || m.anchor === null) return null;
  const { id, label } = m.anchor as Record<string, unknown>;
  if (typeof id !== "string" || id.trim() === "" || id.length > ANCHOR_ID_MAX) return null;
  if (label !== undefined && typeof label !== "string") return null;
  if (typeof label === "string" && label.length > ANCHOR_LABEL_MAX) return null;
  const shown = typeof label === "string" && label.trim() !== "" ? label : id;
  return { type: "pick", anchor: { id, label: shown } };
}

export type ReaderMessage =
  | { type: "mode"; on: boolean }
  | { type: "commented"; ids: string[] }
  | { type: "focus"; id: string };

export function postToFrame(frame: HTMLIFrameElement | null, message: ReaderMessage): void {
  // The sandboxed page has an opaque origin, which no targetOrigin but "*" can
  // name. Nothing sent here is more than the page's own anchor ids.
  frame?.contentWindow?.postMessage({ oc: OC, v: VERSION, ...message }, "*");
}

/** Calls `onMessage` for each accepted message from `frame`'s own window. */
export function useFrameMessages(
  frame: RefObject<HTMLIFrameElement | null>,
  active: boolean,
  onMessage: (message: FrameMessage) => void,
): void {
  const handler = useRef(onMessage);
  handler.current = onMessage;
  useEffect(() => {
    if (!active) return;
    const listen = (e: MessageEvent) => {
      const source = frame.current?.contentWindow;
      if (!source || e.source !== source) return;
      const message = parseFrameMessage(e.data);
      if (message) handler.current(message);
    };
    window.addEventListener("message", listen);
    return () => window.removeEventListener("message", listen);
  }, [frame, active]);
}
