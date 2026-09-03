import { describe, it, expect, vi, afterEach } from "vitest";
import { api } from "../api";
import { DOC_FIELD_ORDER } from "./docHistoryFields";
import type { DocumentKind } from "../types";
import {
  DocLiveContentUnavailable,
  isDocumentKind,
  readLiveDocumentContent,
} from "./docLiveContent";

// The live reader and the field-order table are two statements of ONE fact:
// which field names a revision of this kind carries. A compare attachment
// naming `field` picks the same slice on all three faces (a retained revision,
// the seed, the live document), so a name that drifts on one of them does not
// error anywhere — the compare screen simply renders 「沒有差異」 against every
// version, which is the worst possible way to be wrong. This pins them
// together, and it is total over DocumentKind so a new kind cannot slip in
// unpinned.

const SENTINEL = "…";

/** Every live read the table can make, answering the sentinel in each view's
 * own field names. Nothing here asserts — the assertion is what comes BACK. */
function stubEveryLiveRead() {
  vi.spyOn(api, "getGlobalContext").mockResolvedValue({
    text: SENTINEL,
  } as Awaited<ReturnType<typeof api.getGlobalContext>>);
  vi.spyOn(api, "getRole").mockResolvedValue({
    definitionMd: SENTINEL,
  } as Awaited<ReturnType<typeof api.getRole>>);
  vi.spyOn(api, "getLessons").mockResolvedValue({
    text: SENTINEL,
  } as Awaited<ReturnType<typeof api.getLessons>>);
  vi.spyOn(api, "getInsight").mockResolvedValue({
    text: SENTINEL,
  } as Awaited<ReturnType<typeof api.getInsight>>);
  vi.spyOn(api, "getTaskManual").mockResolvedValue({
    sopMd: SENTINEL,
    learnings: SENTINEL,
  } as Awaited<ReturnType<typeof api.getTaskManual>>);
  vi.spyOn(api, "getTask").mockResolvedValue({
    description: SENTINEL,
    title: SENTINEL,
  } as Awaited<ReturnType<typeof api.getTask>>);
  vi.spyOn(api, "getBootDoc").mockResolvedValue({
    text: SENTINEL,
  } as Awaited<ReturnType<typeof api.getBootDoc>>);
}

/** The one kind with no live content to read: the whole-manual kind was split
 * into its two single-field siblings and every document-history face answers
 * 400 for it. It is listed here rather than skipped silently, so that a kind
 * that stops working in future has to be added on purpose. */
const RETIRED: readonly DocumentKind[] = ["task_manual"];

describe("readLiveDocumentContent", () => {
  afterEach(() => vi.restoreAllMocks());

  const kinds = Object.keys(DOC_FIELD_ORDER) as DocumentKind[];

  it("covers every document kind", () => {
    // Guards the loops below against being vacuous: a table that lost its rows
    // would make every per-kind assertion pass by never running.
    expect(kinds.length).toBeGreaterThan(10);
    expect(kinds.every(isDocumentKind)).toBe(true);
  });

  it.each(kinds.filter((k) => !RETIRED.includes(k)))(
    "reads %s in the field names a revision of it carries",
    async (kind) => {
      stubEveryLiveRead();
      const content = await readLiveDocumentContent(kind, "some-key");
      expect(Object.keys(content).sort()).toEqual([...DOC_FIELD_ORDER[kind]].sort());
      // Every field carries the document's own text, not an empty string a
      // caller would have to distinguish from a genuinely empty document.
      expect(Object.values(content)).toEqual(
        DOC_FIELD_ORDER[kind].map(() => SENTINEL),
      );
    },
  );

  it.each(RETIRED)("refuses %s, which no longer has a live document", async (kind) => {
    stubEveryLiveRead();
    await expect(readLiveDocumentContent(kind, "tm-1")).rejects.toBeInstanceOf(
      DocLiveContentUnavailable,
    );
  });
});

describe("isDocumentKind", () => {
  it("rejects a kind this build does not know", () => {
    // The wire carries `kind` as a free string on purpose, so this is the only
    // place an unknown one is caught.
    expect(isDocumentKind("some_future_kind")).toBe(false);
    expect(isDocumentKind("")).toBe(false);
    // Inherited object properties are not document kinds.
    expect(isDocumentKind("toString")).toBe(false);
  });
});
