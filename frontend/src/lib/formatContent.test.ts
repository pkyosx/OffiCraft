import { describe, it, expect } from "vitest";
import { CSV_MAX_CHARS, CSV_MAX_ROWS, formatJson, formatXml, parseCsv } from "./formatContent";

describe("formatJson", () => {
  it("indents valid JSON and hands invalid JSON back unchanged", () => {
    expect(formatJson('{"name":"Mira","items":[1,2]}')).toEqual({
      body: '{\n  "name": "Mira",\n  "items": [\n    1,\n    2\n  ]\n}',
      unparseable: false,
    });
    expect(formatJson('{"name":}')).toEqual({ body: '{"name":}', unparseable: true });
  });

  it("keeps every literal as written: big integers, 1.0, escaped quotes and punctuation inside strings, empty containers", () => {
    expect(
      formatJson('{"a":12345678901234567890,"b":1.0, "c":"x, {y}: \\"[z]\\"","d":[ ],"e":{}}'),
    ).toEqual({
      body: '{\n  "a": 12345678901234567890,\n  "b": 1.0,\n  "c": "x, {y}: \\"[z]\\"",\n  "d": [],\n  "e": {}\n}',
      unparseable: false,
    });
  });
});

describe("formatXml", () => {
  it("indents nested elements two spaces a level, keeping the declaration, attributes and leaf text", () => {
    expect(
      formatXml('<?xml version="1.0"?><feed lang="zh"><entry id="1"><title>A &amp; B</title></entry><empty></empty></feed>'),
    ).toEqual({
      body:
        '<?xml version="1.0"?>\n' +
        '<feed lang="zh">\n' +
        '  <entry id="1">\n' +
        "    <title>A &amp; B</title>\n" +
        "  </entry>\n" +
        "  <empty/>\n" +
        "</feed>",
      unparseable: false,
    });
  });

  it("keeps mixed content, comments and an inherited namespace as written", () => {
    expect(
      formatXml('<doc xmlns="urn:x"><!-- note --><p>Hello <b>you</b> there</p></doc>').body,
    ).toBe('<doc xmlns="urn:x">\n  <!-- note -->\n  <p>Hello <b>you</b> there</p>\n</doc>');
  });

  it("hands malformed XML back unchanged", () => {
    expect(formatXml("<a><b></a>")).toEqual({ body: "<a><b></a>", unparseable: true });
  });
});

describe("parseCsv", () => {
  it("splits rows and fields, honouring quotes, doubled quotes, quoted commas and quoted newlines", () => {
    expect(parseCsv('name,note\r\nMira,"says ""hi"", twice"\nKyle,"two\nlines"\n')).toEqual({
      rows: [
        ["name", "note"],
        ["Mira", 'says "hi", twice'],
        ["Kyle", "two\nlines"],
      ],
    });
  });

  it("keeps a last row with no trailing newline and ragged rows as they are", () => {
    expect(parseCsv("a,b,c\n1")).toEqual({ rows: [["a", "b", "c"], ["1"]] });
  });

  it("calls an unterminated quote unparseable and an empty file unparseable", () => {
    expect(parseCsv('a,"b\n1,2')).toEqual({ error: "unparseable" });
    expect(parseCsv("")).toEqual({ error: "unparseable" });
  });

  it("calls a file past the row or size ceiling too large", () => {
    expect(parseCsv("x\n".repeat(CSV_MAX_ROWS + 1))).toEqual({ error: "too_large" });
    expect(parseCsv("x".repeat(CSV_MAX_CHARS + 1))).toEqual({ error: "too_large" });
    expect("rows" in parseCsv("x\n".repeat(CSV_MAX_ROWS))).toBe(true);
  });
});
