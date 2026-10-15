// lib/docComments.ts — the owner's comments on a delivered .md document: the
// draft kept per attachment in browser storage, the chat message they are sent
// as, and the lookup that finds an excerpt in the rendered document again.

export interface DocComment {
  id: string;
  excerpt: string;
  comment: string;
}

/** Who the comments go to, resolved by the surface that opened the document. */
export interface DocCommentRecipient {
  id: string;
  name: string;
  /** Against the live roster (members or live outsource workers); "pending"
   * until that roster has loaded, so nobody is called departed by a miss.
   * "not-member" is a server-stamped sender (system, webhook, schedule). */
  roster: "active" | "left" | "pending" | "not-member";
}

export interface DocCommentTarget {
  /** null ⇒ nobody to send to (e.g. an unassigned task). */
  recipient: DocCommentRecipient | null;
  /** Present when the document is a task artifact. */
  task?: { id: string; taskNo: string; artifactName: string };
}

const STORAGE_PREFIX = "oc_doc_comments:";

// Private mode and blocked site data make storage throw; a draft that cannot be
// kept still works for as long as the overlay stays open.
export function loadDocCommentDraft(attachmentId: string): DocComment[] {
  try {
    const raw = window.localStorage.getItem(STORAGE_PREFIX + attachmentId);
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (c): c is DocComment =>
        typeof c === "object" &&
        c !== null &&
        typeof (c as DocComment).id === "string" &&
        typeof (c as DocComment).excerpt === "string" &&
        typeof (c as DocComment).comment === "string",
    );
  } catch {
    return [];
  }
}

export function saveDocCommentDraft(attachmentId: string, comments: DocComment[]): void {
  try {
    if (comments.length === 0) {
      window.localStorage.removeItem(STORAGE_PREFIX + attachmentId);
    } else {
      window.localStorage.setItem(STORAGE_PREFIX + attachmentId, JSON.stringify(comments));
    }
  } catch {
    /* storage unavailable */
  }
}

function excerptLines(excerpt: string): string[] {
  const lines = excerpt.split(/\r?\n/).map((l) => l.trim());
  const out: string[] = [];
  for (const line of lines) {
    if (line === "" && (out.length === 0 || out[out.length - 1] === "")) continue;
    out.push(line);
  }
  while (out.length > 0 && out[out.length - 1] === "") out.pop();
  return out;
}

function escapeLinkText(text: string): string {
  return text.replace(/[\\[\]`]/g, "\\$&");
}

// So a comment line reads as the comment's own text, never as a quoted excerpt
// or another numbered item.
function escapeBlockMarker(line: string): string {
  return line.replace(
    /^(\s*)(#|>|[-*+](?=\s|$)|\d+(?=[.)](?:\s|$))|([-*_=])\3{2}|```|~~~)/,
    (m, indent: string, marker: string) => (/^\d/.test(marker) ? `${m}\\` : `${indent}\\${marker}`),
  );
}

/** The chat message body. It is read by an AI through get_chat, so its labels
 * are a fixed format rather than UI wording a theme may override. Links are
 * host-less paths so they open on whatever address the reader uses. */
export function formatDocCommentMessage(input: {
  filename: string;
  attachmentId: string;
  task?: DocCommentTarget["task"];
  comments: DocComment[];
}): string {
  const head = [
    `**文件註解**：[${escapeLinkText(input.filename)}](/api/chat/attachment/${input.attachmentId})（${input.attachmentId}）`,
  ];
  if (input.task) {
    head.push(
      `任務 [${input.task.taskNo}](/#tasks/${input.task.id}) 產物「${input.task.artifactName}」`,
    );
  }
  // Each item's body is indented under its marker: the renderer reads a list
  // item's first line inline, so the blockquote has to start on a continuation
  // line to stay a blockquote.
  const items = input.comments.map((c, i) => {
    const quote = excerptLines(c.excerpt).map((l) => (l === "" ? "   >" : `   > ${l}`));
    const commentLines = c.comment.trim().split(/\r?\n/);
    const comment = commentLines.map((l, j) =>
      j === 0 ? `   註解：${l}` : l === "" ? "" : `   ${escapeBlockMarker(l)}`,
    );
    return [`${i + 1}. 原文：`, ...quote, "", ...comment].join("\n");
  });
  return [head.join("\n"), ...items].join("\n\n");
}

/** A Range over the first occurrence of `excerpt` in `root`'s text, or null.
 * Whitespace is ignored on both sides: a selection's text carries line breaks
 * between blocks that the DOM's text nodes do not. */
export function locateExcerpt(root: Node, excerpt: string): Range | null {
  const needle = excerpt.replace(/\s+/g, "");
  if (needle === "") return null;
  const doc = root.ownerDocument ?? (root as Document);
  const walker = doc.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  const chars: string[] = [];
  const at: { node: Text; offset: number }[] = [];
  for (let n = walker.nextNode(); n !== null; n = walker.nextNode()) {
    const text = n as Text;
    const data = text.data;
    for (let i = 0; i < data.length; i++) {
      if (/\s/.test(data[i])) continue;
      chars.push(data[i]);
      at.push({ node: text, offset: i });
    }
  }
  const index = chars.join("").indexOf(needle);
  if (index < 0) return null;
  const start = at[index];
  const end = at[index + needle.length - 1];
  const range = doc.createRange();
  range.setStart(start.node, start.offset);
  range.setEnd(end.node, end.offset + 1);
  return range;
}
