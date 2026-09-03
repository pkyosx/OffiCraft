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

/** Each stubbed read answers ITS OWN NAME. That is what makes the source
 * observable: a field-name check alone passes just as happily when `insight`
 * reads the LESSONS document, because both kinds carry a field called `text`.
 * A wrong source is the worst failure this table has — it draws a confident,
 * completely wrong comparison next to a 還原 button — so the assertion has to
 * be able to see it. */
function stubEveryLiveRead() {
  vi.spyOn(api, "getGlobalContext").mockResolvedValue({
    text: "getGlobalContext",
  } as Awaited<ReturnType<typeof api.getGlobalContext>>);
  vi.spyOn(api, "getRole").mockResolvedValue({
    definitionMd: "getRole",
  } as Awaited<ReturnType<typeof api.getRole>>);
  vi.spyOn(api, "getLessons").mockResolvedValue({
    text: "getLessons",
  } as Awaited<ReturnType<typeof api.getLessons>>);
  vi.spyOn(api, "getInsight").mockResolvedValue({
    text: "getInsight",
  } as Awaited<ReturnType<typeof api.getInsight>>);
  vi.spyOn(api, "getTaskManual").mockResolvedValue({
    sopMd: "getTaskManual",
    learnings: "getTaskManual",
  } as Awaited<ReturnType<typeof api.getTaskManual>>);
  vi.spyOn(api, "getTask").mockResolvedValue({
    description: "getTask",
    title: "getTask",
  } as Awaited<ReturnType<typeof api.getTask>>);
  vi.spyOn(api, "getBootDoc").mockResolvedValue({
    text: "getBootDoc",
  } as Awaited<ReturnType<typeof api.getBootDoc>>);
}

/** Which api method each kind MUST read. Written out by hand rather than
 * derived from the table under test — a expectation computed from the thing it
 * judges is true no matter what that thing says. */
const SOURCE_OF: Record<DocumentKind, string> = {
  global_context: "getGlobalContext",
  role_definition: "getRole",
  lessons: "getLessons",
  insight: "getInsight",
  task_manual: "",
  task_manual_sop: "getTaskManual",
  task_manual_learnings: "getTaskManual",
  task_description: "getTask",
  task_title: "getTask",
  system_interaction: "getBootDoc",
  boot_sequence: "getBootDoc",
  offboard: "getBootDoc",
  accelerated_stop: "getBootDoc",
  task_closeout: "getBootDoc",
  task_reassign_predecessor: "getBootDoc",
  task_takeover_with_predecessor: "getBootDoc",
  task_takeover_fresh: "getBootDoc",
  task_unblocked: "getBootDoc",
};

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
      const key = kind === "global_context" ? "global" : "some-key";
      const content = await readLiveDocumentContent(kind, key);
      expect(Object.keys(content).sort()).toEqual([...DOC_FIELD_ORDER[kind]].sort());
      // WHICH DOCUMENT, not just which field names. Two kinds sharing a field
      // called `text` are indistinguishable by shape alone.
      expect(Object.values(content)).toEqual(
        DOC_FIELD_ORDER[kind].map(() => SOURCE_OF[kind]),
      );
      // And it must be asked for the key it was given — a kind that ignores its
      // key silently answers about some other document entirely.
      const source = SOURCE_OF[kind] as keyof typeof api;
      if (kind !== "global_context") {
        expect(api[source]).toHaveBeenCalledWith(
          ...(SOURCE_OF[kind] === "getBootDoc" ? [kind, key] : [key]),
        );
      }
    },
  );

  it.each(RETIRED)("refuses %s, which no longer has a live document", async (kind) => {
    stubEveryLiveRead();
    await expect(readLiveDocumentContent(kind, "tm-1")).rejects.toBeInstanceOf(
      DocLiveContentUnavailable,
    );
  });
});

describe("readLiveDocumentContent, the singleton's key", () => {
  afterEach(() => vi.restoreAllMocks());

  it("refuses a global_context key that is not the document's own", async () => {
    // The other two faces (a revision id, /seed) 404 on a wrong key. Answering
    // about the real document anyway would make a wrong address look right.
    stubEveryLiveRead();
    await expect(
      readLiveDocumentContent("global_context", "not-global"),
    ).rejects.toBeInstanceOf(DocLiveContentUnavailable);
    expect(api.getGlobalContext).not.toHaveBeenCalled();
  });

  it("reads global_context under its own key", async () => {
    stubEveryLiveRead();
    expect(await readLiveDocumentContent("global_context", "global")).toEqual({
      text: "getGlobalContext",
    });
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
