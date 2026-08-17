// T-1170 — the three list endpoints answer a DIRECTORY, and the text is only
// ever reachable by naming ONE thing.
//
// 🔴 WHY THIS FILE IS ABOUT THE MOCK. Every component and hook test in this
// repo runs against `api/mock.ts`. If the mock kept serving `definition_md` /
// `sop_md` / `learnings` / a revision's `content` on its list answers, every
// surface that still read a document off a list row would stay GREEN against a
// fake server the real one no longer resembles — the "generous fake" failure
// api/dtoParity.ts was written for, in its list-shaped form. So the assertions
// below are on ABSENCE first: the field is not there, and the per-item read is
// where it lives.
//
// The type system already forbids most of this (a `RoleSummaryView` has no
// `definitionMd`), but a type cannot see the object the mock actually
// constructs — `structuredClone(row)` of a store entry would carry the field at
// runtime while satisfying the type perfectly. That gap is what these
// `not.toHaveProperty` assertions close.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockTaskManual } from "./mock";
import { isHttpStatus } from "./errors";
import { runeLength } from "./docCap";

beforeEach(() => {
  __resetMock();
});

describe("mockApi.listRoles", () => {
  it("answers roster rows WITHOUT the persona body, and getRole carries it", async () => {
    await mockApi.saveRole("assistant", { definitionMd: "我是助理。" });

    const [row] = await mockApi.listRoles();
    expect(row.key).toBe("assistant");
    // The row still says how big the document is — that is what the roster
    // needs and what the size budget is drawn from.
    expect(row.sizeChars).toBe(runeLength("我是助理。"));
    expect(row).not.toHaveProperty("definitionMd");

    const doc = await mockApi.getRole("assistant");
    expect(doc.definitionMd).toBe("我是助理。");
    // …and the per-item read is a SUPERSET of the row: nothing the roster
    // showed is lost by reading the document instead.
    expect(doc.sizeChars).toBe(row.sizeChars);
    expect(doc.isSeed).toBe(row.isSeed);
    expect(doc.isDefault).toBe(row.isDefault);
  });
});

describe("mockApi.listTaskManuals", () => {
  it("answers list rows WITHOUT sop_md/learnings, and getTaskManual carries them", async () => {
    __injectMockTaskManual({
      typeKey: "tm-000000000001",
      displayName: "審查 PR",
      purpose: "審一份 PR",
      fields: [],
      sopMd: "# 步驟\n\n先讀 diff。",
      learnings: "小心 flaky 測試。",
      assignee: null,
      updatedTs: 1,
    });

    const [row] = await mockApi.listTaskManuals();
    // Everything the list page and the hub render is still on the row…
    expect(row.displayName).toBe("審查 PR");
    expect(row.purpose).toBe("審一份 PR");
    expect(row.assignee).toBeNull();
    // …and neither long document is.
    expect(row).not.toHaveProperty("sopMd");
    expect(row).not.toHaveProperty("learnings");

    const manual = await mockApi.getTaskManual("tm-000000000001");
    expect(manual.sopMd).toBe("# 步驟\n\n先讀 diff。");
    expect(manual.learnings).toBe("小心 flaky 測試。");
    expect(manual.purpose).toBe(row.purpose);
  });
});

describe("mockApi.listDocumentHistory", () => {
  it("answers directory rows — sizes and the tombstone flag, never the text", async () => {
    await mockApi.saveGlobalContext("第一版");
    await mockApi.saveGlobalContext("第二版內容比較長");

    const [row] = await mockApi.listDocumentHistory("global_context", "global");
    expect(row).not.toHaveProperty("content");
    expect(row.actorId).toBeTruthy();
    expect(row.createdTs).toBeGreaterThan(0);
    // The size map is what the un-restorable marking and the "was blank"
    // line are judged from now, so it has to be the SERVER's unit: code
    // points, not UTF-16 units.
    expect(row.sizes).toEqual({ text: runeLength("第一版") });
    expect(row.tombstoned).toBe(false);
  });

  it("reports a tombstone as a FLAG, and never as a content field", async () => {
    await mockApi.saveGlobalContext("寫過的內容");
    await mockApi.resetGlobalContext();
    await mockApi.saveGlobalContext("之後又寫的內容");

    const [row] = await mockApi.listDocumentHistory("global_context", "global");
    expect(row.tombstoned).toBe(true);
    // `tombstoned` is not a document field, so it must not appear among the
    // sizes — a caller counting content fields there would read a tombstoned
    // revision as one that has text.
    expect(row.sizes).not.toHaveProperty("tombstoned");
  });

  it("measures in CODE POINTS, so an astral character counts once", async () => {
    // The cap verdict is derived from these numbers. `String.length` would say
    // 2 per emoji and mark a revision as over-cap that the server accepts —
    // the one direction api/docCap.ts refuses to be wrong in.
    await mockApi.saveGlobalContext("🐟🐟🐟");
    await mockApi.saveGlobalContext("後來改掉了");

    const [row] = await mockApi.listDocumentHistory("global_context", "global");
    expect(row.sizes.text).toBe(3);
    expect("🐟🐟🐟".length).toBe(6); // the mistake this pins against
  });
});

describe("mockApi.getDocumentRevision", () => {
  it("serves the NAMED revision's text and 404s an id it no longer keeps", async () => {
    await mockApi.saveGlobalContext("原本的內容");
    await mockApi.saveGlobalContext("改過的內容");

    const [row] = await mockApi.listDocumentHistory("global_context", "global");
    const revision = await mockApi.getDocumentRevision(
      "global_context",
      "global",
      row.id
    );
    // The id echoes the row that named it — a reader showing one revision's
    // text under another's heading is the failure this per-revision read
    // exists to make impossible. It is the ONLY identity fact on this answer:
    // actor and timestamp live on the directory row (T-1170), and the size the
    // row reported has to describe the text this read returns.
    expect(revision.id).toBe(row.id);
    expect(revision.content.text).toBe("原本的內容");
    expect(row.sizes.text).toBe(runeLength(revision.content.text));

    // A pruned id REJECTS rather than answering an empty document: the reader
    // must be able to say "this could not be read" beside a destructive
    // button, which is a different claim from "this version was blank".
    await expect(
      mockApi.getDocumentRevision("global_context", "global", row.id + 9999)
    ).rejects.toSatisfy((e: unknown) => isHttpStatus(e, 404));
  });
});
