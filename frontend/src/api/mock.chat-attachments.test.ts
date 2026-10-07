import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";

describe("mock postChat attachments", () => {
  beforeEach(() => __resetMock());

  it("types each attachment by its filename's extension, never by the data-URI", async () => {
    const png = "data:image/png;base64,iVBORw0KGgo=";
    await mockApi.postChat({
      to: "mira",
      body: "files",
      attachments: [
        { dataB64: png, filename: "shot.PNG" },
        { dataB64: png, filename: "notes.txt" },
        { dataB64: "data:text/plain;base64,aGk=", filename: "report.pdf" },
        { dataB64: png },
      ],
    });
    const thread = await mockApi.listChat("mira");
    const sent = thread[thread.length - 1];
    expect(sent.attachments.map((a) => [a.filename, a.mime, a.isImage])).toEqual([
      ["shot.PNG", "image/png", true],
      ["notes.txt", "text/plain", false],
      ["report.pdf", "application/pdf", false],
      ["", "application/octet-stream", false],
    ]);
  });
});
