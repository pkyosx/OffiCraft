// Mock adapter parity for the product guide (設定 › 使用說明): listDocs returns
// slug+title rows, getDoc returns one doc's markdown, and an unknown slug 404s
// (the same envelope the real GET /api/docs/{slug} throws). The authoritative
// content is server-side (README + docs/guide embed); the mock pins the shape
// the 使用說明 page + Mira's get_doc consume.

import { describe, it, expect } from "vitest";
import { mockApi } from "./mock";
import { ApiError } from "./errors";

describe("mock product guide (docs)", () => {
  it("lists docs as slug + title rows", async () => {
    const docs = await mockApi.listDocs();
    expect(docs.length).toBeGreaterThan(0);
    expect(docs[0]).toHaveProperty("slug");
    expect(docs[0]).toHaveProperty("title");
  });

  it("reads one doc's markdown by slug", async () => {
    const doc = await mockApi.getDoc("readme");
    expect(doc.slug).toBe("readme");
    expect(doc.markdownMd).toContain("使用說明");
  });

  it("throws a 404 for an unknown slug", async () => {
    await expect(mockApi.getDoc("nope")).rejects.toMatchObject({
      status: 404,
    });
    await expect(mockApi.getDoc("nope")).rejects.toBeInstanceOf(ApiError);
  });
});
