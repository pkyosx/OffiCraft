import { describe, it, expect } from "vitest";
import { contentKindOf, isTextKind, textKindForName } from "./contentKind";

describe("contentKindOf", () => {
  it.each([
    ["text/markdown", "x", "markdown"],
    ["text/x-markdown", "x", "markdown"],
    ["text/plain", "design.md", "markdown"],
    ["application/octet-stream", "NOTES.MARKDOWN", "markdown"],
    ["application/json", "report.data", "json"],
    ["application/json; charset=utf-8", "report.data", "json"],
    ["application/octet-stream", "REPORT.JSON", "json"],
    ["text/plain", "config.json", "json"],
    ["text/xml", "feed", "xml"],
    ["application/xml", "feed", "xml"],
    ["application/atom+xml", "feed", "xml"],
    ["text/plain", "config.yaml", "yaml"],
    ["text/plain", "config.yml", "yaml"],
    ["text/csv", "x", "csv"],
    ["application/octet-stream", "table.csv", "csv"],
    ["text/plain", "notes.txt", "text"],
    ["text/plain", "main.py", "text"],
    ["application/octet-stream", "settings.ini", "text"],
    ["text/x-diff", "change.patch", "text"],
    ["text/plain", "untitled", "text"],
    ["text/html", "page.html", "html"],
    ["text/html; charset=utf-8", "page", "html"],
    ["application/octet-stream", "page.html", "text"],
    ["image/png", "shot.md", "image"],
    ["image/svg+xml", "logo.svg", "image"],
    ["application/pdf", "report.pdf", "pdf"],
    ["application/pdf", "report.md", "pdf"],
    ["application/zip", "report.json", "binary"],
    ["application/octet-stream", "blob.bin", "binary"],
    ["application/octet-stream", "", "binary"],
    ["", "README.md", "markdown"],
  ])("%s named %s → %s", (mime, filename, kind) => {
    expect(contentKindOf(mime, filename)).toBe(kind);
  });
});

describe("isTextKind", () => {
  it("is true for the kinds drawn from text and false for the rest", () => {
    expect(
      (["markdown", "json", "xml", "yaml", "csv", "text", "html", "image", "pdf", "binary"] as const).map(
        (k) => `${k}=${isTextKind(k)}`,
      ),
    ).toEqual([
      "markdown=true",
      "json=true",
      "xml=true",
      "yaml=true",
      "csv=true",
      "text=true",
      "html=false",
      "image=false",
      "pdf=false",
      "binary=false",
    ]);
  });
});

describe("textKindForName", () => {
  it("names the kind of a known text extension and null for anything else", () => {
    expect(textKindForName("週報.md")).toBe("markdown");
    expect(textKindForName("a.LOG")).toBe("text");
    expect(textKindForName("週報 v3")).toBeNull();
    expect(textKindForName("blob.bin")).toBeNull();
  });
});
