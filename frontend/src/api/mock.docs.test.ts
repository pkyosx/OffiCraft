// Mock adapter parity for the product guide (設定 › 使用說明): listDocs returns
// slug+title rows, getDoc returns one doc's markdown, and an unknown slug 404s
// (the same envelope the real GET /api/docs/{slug} throws). The authoritative
// content is server-side (the docs/guide embed); the mock pins the shape
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
    const doc = await mockApi.getDoc("why");
    expect(doc.slug).toBe("why");
    expect(doc.markdownMd).toContain("使用說明");
  });

  // T-68f1: the fixture exists to exercise CROSS-DOC links, so the invariant
  // that makes it usable — every in-app link target resolves to another doc in
  // the same list — is pinned here rather than left to the page tests.
  it("cross-doc link targets resolve to slugs the list actually carries", async () => {
    const docs = await mockApi.listDocs();
    const slugs = new Set(docs.map((d) => d.slug));
    expect(slugs.size).toBeGreaterThan(1);
    let checked = 0;
    for (const { slug } of docs) {
      const { markdownMd } = await mockApi.getDoc(slug);
      for (const m of markdownMd.matchAll(/\]\(([^)]+\.md)\)/g)) {
        const base = m[1].slice(m[1].lastIndexOf("/") + 1).replace(/\.md$/, "");
        // `../dev/agent-env.md` is the deliberate UNSHIPPED case.
        if (base === "agent-env") continue;
        expect(slugs, `link target ${m[1]}`).toContain(base);
        checked++;
      }
    }
    // Non-vacuity: a fixture that lost its links would satisfy the loop above
    // by never entering it.
    expect(checked, "the fixture must carry in-app doc links").toBeGreaterThanOrEqual(3);
  });

  it("throws a 404 for an unknown slug", async () => {
    await expect(mockApi.getDoc("nope")).rejects.toMatchObject({
      status: 404,
    });
    await expect(mockApi.getDoc("nope")).rejects.toBeInstanceOf(ApiError);
  });
});
