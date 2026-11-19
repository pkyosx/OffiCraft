// Formatting for the text kinds ContentView draws. JSON and XML use browser
// built-ins (JSON, DOMParser); the YAML parser is loaded on the first YAML file
// a page opens, so no other page downloads it.
//
// Every formatter hands the raw text back when it cannot do its job: the reader
// came to see the bytes, and a broken file is still readable as it was stored.

export type Formatted = { body: string; unparseable: boolean };

/** Re-indents valid JSON by moving whitespace only. Every literal stays as
 * written: a parse→stringify round trip would round integers past 2^53, turn
 * `1.0` into `1` and merge duplicate keys, showing a file that is not the one
 * stored. */
export function formatJson(text: string): Formatted {
  try {
    JSON.parse(text);
  } catch {
    return { body: text, unparseable: true };
  }
  let out = "";
  let depth = 0;
  const newline = () => `\n${"  ".repeat(depth)}`;
  let i = 0;
  while (i < text.length) {
    const ch = text[i]!;
    if (ch === '"') {
      let j = i + 1;
      while (j < text.length && text[j] !== '"') j += text[j] === "\\" ? 2 : 1;
      out += text.slice(i, j + 1);
      i = j + 1;
      continue;
    }
    if (ch === " " || ch === "\t" || ch === "\n" || ch === "\r") {
      i += 1;
      continue;
    }
    if (ch === "{" || ch === "[") {
      let j = i + 1;
      while (/\s/.test(text[j]!)) j += 1;
      if (text[j] === (ch === "{" ? "}" : "]")) {
        out += ch + text[j];
        i = j + 1;
        continue;
      }
      depth += 1;
      out += ch + newline();
    } else if (ch === "}" || ch === "]") {
      depth -= 1;
      out += newline() + ch;
    } else if (ch === ",") {
      out += "," + newline();
    } else if (ch === ":") {
      out += ": ";
    } else {
      out += ch;
    }
    i += 1;
  }
  return { body: out, unparseable: false };
}

/** Whether the text is well-formed YAML, every document of a `---` stream
 * included. Syntax only: a custom tag such as CloudFormation's `!Ref` is not
 * an error, because loading it would need a schema the viewer does not have. */
export async function yamlParses(text: string): Promise<boolean> {
  const { parseEvents } = await import("js-yaml");
  try {
    parseEvents(text, {});
    return true;
  } catch {
    return false;
  }
}

const XML_DECLARATION = /^\s*(<\?xml[^>]*\?>)/;

function escapeXmlText(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function escapeXmlAttr(value: string): string {
  return escapeXmlText(value).replace(/"/g, "&quot;");
}

function openTag(el: Element): string {
  let tag = `<${el.tagName}`;
  for (const attr of Array.from(el.attributes)) {
    tag += ` ${attr.name}="${escapeXmlAttr(attr.value)}"`;
  }
  return tag;
}

function isBlank(node: Node): boolean {
  return node.nodeType === Node.TEXT_NODE && (node.nodeValue ?? "").trim() === "";
}

/** One node exactly as written, without re-indenting. Built by hand rather
 * than with XMLSerializer, which re-declares an inherited namespace on every
 * element it is handed on its own. */
function inlineXml(node: Node): string {
  switch (node.nodeType) {
    case Node.ELEMENT_NODE: {
      const el = node as Element;
      if (el.childNodes.length === 0) return `${openTag(el)}/>`;
      return `${openTag(el)}>${Array.from(el.childNodes).map(inlineXml).join("")}</${el.tagName}>`;
    }
    case Node.TEXT_NODE:
      return escapeXmlText(node.nodeValue ?? "");
    case Node.CDATA_SECTION_NODE:
      return `<![CDATA[${node.nodeValue ?? ""}]]>`;
    case Node.COMMENT_NODE:
      return `<!--${node.nodeValue ?? ""}-->`;
    case Node.PROCESSING_INSTRUCTION_NODE: {
      const pi = node as ProcessingInstruction;
      return `<?${pi.target}${pi.data ? ` ${pi.data}` : ""}?>`;
    }
    default:
      return new XMLSerializer().serializeToString(node);
  }
}

function formatXmlNode(node: Node, depth: number, out: string[]) {
  const pad = "  ".repeat(depth);
  if (node.nodeType !== Node.ELEMENT_NODE) {
    out.push(`${pad}${node.nodeType === Node.TEXT_NODE ? escapeXmlText((node.nodeValue ?? "").trim()) : inlineXml(node)}`);
    return;
  }
  const el = node as Element;
  const children = Array.from(el.childNodes).filter((c) => !isBlank(c));
  // Text beside (or instead of) child elements stays on one line as written:
  // in mixed content whitespace can be meaningful, and re-indenting it would
  // change the document being shown.
  const hasText = children.some(
    (c) => c.nodeType === Node.TEXT_NODE || c.nodeType === Node.CDATA_SECTION_NODE,
  );
  if (children.length === 0 || hasText) {
    out.push(`${pad}${children.length === 0 ? `${openTag(el)}/>` : inlineXml(el)}`);
    return;
  }
  out.push(`${pad}${openTag(el)}>`);
  for (const child of children) formatXmlNode(child, depth + 1, out);
  out.push(`${pad}</${el.tagName}>`);
}

export function formatXml(text: string): Formatted {
  const parsed = new DOMParser().parseFromString(text, "application/xml");
  if (parsed.getElementsByTagName("parsererror").length > 0 || !parsed.documentElement) {
    return { body: text, unparseable: true };
  }
  const out: string[] = [];
  const declaration = XML_DECLARATION.exec(text);
  if (declaration) out.push(declaration[1]!);
  for (const node of Array.from(parsed.childNodes)) {
    if (isBlank(node)) continue;
    formatXmlNode(node, 0, out);
  }
  return { body: out.join("\n"), unparseable: false };
}

/** Past these a table is slower to read than the raw text, and slow to draw. */
export const CSV_MAX_CHARS = 512 * 1024;
export const CSV_MAX_ROWS = 2000;

export type CsvTable =
  | { rows: string[][] }
  | { error: "unparseable" | "too_large" };

/** RFC 4180: comma-separated, `"` quotes a field, `""` is a quote inside one,
 * and a quoted field may span lines. An unterminated quote is unparseable. */
export function parseCsv(text: string): CsvTable {
  if (text.length > CSV_MAX_CHARS) return { error: "too_large" };
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  let i = 0;
  while (i < text.length) {
    const ch = text[i]!;
    if (quoted) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i += 2;
          continue;
        }
        quoted = false;
        i += 1;
        continue;
      }
      field += ch;
      i += 1;
      continue;
    }
    if (ch === '"' && field === "") {
      quoted = true;
      i += 1;
      continue;
    }
    if (ch === ",") {
      row.push(field);
      field = "";
      i += 1;
      continue;
    }
    if (ch === "\n" || ch === "\r") {
      row.push(field);
      rows.push(row);
      if (rows.length > CSV_MAX_ROWS) return { error: "too_large" };
      row = [];
      field = "";
      i += ch === "\r" && text[i + 1] === "\n" ? 2 : 1;
      continue;
    }
    field += ch;
    i += 1;
  }
  if (quoted) return { error: "unparseable" };
  if (field !== "" || row.length > 0) {
    row.push(field);
    rows.push(row);
  }
  if (rows.length > CSV_MAX_ROWS) return { error: "too_large" };
  if (rows.length === 0) return { error: "unparseable" };
  return { rows };
}
