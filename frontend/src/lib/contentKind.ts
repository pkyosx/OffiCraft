// What a stored file IS, for every surface that draws one: the chat / artifact
// preview overlay, the 檔案庫 page and its edit overlay, and the version
// readers. One answer, so the same file never reads as a document in one place
// and as "cannot preview" in another.
//
// ⚠️ Not the same question as `isInlineDisplayableMime` (MarkdownPreviewOverlay):
// that one mirrors the server's inline/attachment decision for the serve route
// and must stay a mirror. This one asks what the cockpit can draw itself.

export type ContentKind =
  | "markdown"
  | "json"
  | "xml"
  | "yaml"
  | "csv"
  | "text"
  | "html"
  | "image"
  | "pdf"
  | "binary";

const TEXT_KINDS: ReadonlySet<ContentKind> = new Set([
  "markdown",
  "json",
  "xml",
  "yaml",
  "csv",
  "text",
]);

/** Extensions whose bytes are text by definition. Closed on purpose: a name is
 * only a second opinion, consulted when the mime does not know, and a wrong
 * "text" here would put a binary's bytes on screen. */
const KIND_BY_EXTENSION: Record<string, ContentKind> = {
  md: "markdown",
  markdown: "markdown",
  json: "json",
  xml: "xml",
  yaml: "yaml",
  yml: "yaml",
  csv: "csv",
  txt: "text",
  log: "text",
  tsv: "text",
  diff: "text",
  patch: "text",
  sql: "text",
  go: "text",
  ts: "text",
  tsx: "text",
  js: "text",
  jsx: "text",
  py: "text",
  sh: "text",
  toml: "text",
  ini: "text",
  env: "text",
  css: "text",
  html: "text",
  htm: "text",
};

export function baseMime(mime: string): string {
  return mime.split(";")[0]!.trim().toLowerCase();
}

function extensionOf(filename: string): string {
  const name = filename.trim();
  const dot = name.lastIndexOf(".");
  return dot < 0 ? "" : name.slice(dot + 1).toLowerCase();
}

/** The text kind a NAME claims, or null when its extension is not a known text
 * one. */
export function textKindForName(filename: string): ContentKind | null {
  return KIND_BY_EXTENSION[extensionOf(filename)] ?? null;
}

/**
 * The served mime decides first; the filename's extension only speaks when the
 * mime is generic (empty, `application/octet-stream`, `text/plain` — the server
 * stores .yaml/.py/.sql as text/plain) or names no particular text format.
 *
 * HTML is only `html` by its served mime: the page is shown by loading the
 * serve route in a frame, so bytes the server sends as anything else would not
 * render there. An `.html` name under a generic mime reads as text.
 */
export function contentKindOf(mime: string, filename: string): ContentKind {
  const base = baseMime(mime);
  if (base.startsWith("image/")) return "image";
  if (base === "text/html") return "html";
  if (base === "application/pdf") return "pdf";
  if (base === "text/markdown" || base === "text/x-markdown") return "markdown";
  if (base === "application/json" || base.endsWith("+json")) return "json";
  if (base === "text/xml" || base === "application/xml" || base.endsWith("+xml")) return "xml";
  if (base === "text/csv") return "csv";
  if (base === "application/yaml" || base === "text/yaml" || base === "application/x-yaml") {
    return "yaml";
  }
  const generic = base === "" || base === "application/octet-stream";
  if (generic || base.startsWith("text/")) {
    const byName = textKindForName(filename);
    if (byName !== null) return byName;
    if (base.startsWith("text/")) return "text";
  }
  return "binary";
}

export function isTextKind(kind: ContentKind): boolean {
  return TEXT_KINDS.has(kind);
}
