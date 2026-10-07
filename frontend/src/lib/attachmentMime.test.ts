import { describe, it, expect } from "vitest";
import { attachmentMimeForName, pastedImageName } from "./attachmentMime";

describe("attachmentMimeForName", () => {
  it("answers the table's mime for the suffix after the last dot, case-insensitively", () => {
    expect(attachmentMimeForName("report.pdf")).toBe("application/pdf");
    expect(attachmentMimeForName("Scan.PDF")).toBe("application/pdf");
    expect(attachmentMimeForName(" icon.svg ")).toBe("image/svg+xml");
    expect(attachmentMimeForName("fix.patch")).toBe("text/x-diff");
    expect(attachmentMimeForName("query.sql")).toBe("text/plain");
  });

  it("answers application/octet-stream with no extension or one outside the table", () => {
    for (const name of ["", "README", "bundle.zip", "report.pdf.zip", "trailing."]) {
      expect(`${name}=${attachmentMimeForName(name)}`).toBe(`${name}=application/octet-stream`);
    }
  });
});

describe("pastedImageName", () => {
  it("names a clipboard image by the table's first extension for its type, and nothing else", () => {
    expect(pastedImageName("image/png")).toBe("pasted-image.png");
    expect(pastedImageName("image/jpeg")).toBe("pasted-image.jpg");
    expect(pastedImageName("image/heic")).toBe("");
    expect(pastedImageName("")).toBe("");
  });
});
