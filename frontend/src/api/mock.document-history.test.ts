// The mock adapter's retained-revision path (T-7d33): the offline cockpit must
// exercise the same shape the server keeps — a write retains what it replaced,
// the list is newest-first and bounded at 3, and a restore puts the old content
// back over the live document (itself retaining what it overwrote).

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";
import { ApiError, isHttpStatus } from "./errors";
import { documentRevisions } from "../test/documentHistory";

beforeEach(() => {
  __resetMock();
});

describe("mockApi · document history", () => {
  it("has no revisions for a document nobody has edited, nor after its first write", async () => {
    expect(await documentRevisions(mockApi, "global_context", "global")).toEqual(
      []
    );

    // The product boundary: a seed/default document has no previous version, so
    // the FIRST customization replaces nothing and retains nothing. The server
    // skips the empty snapshot (dal.go); a synthesized "was default" revision
    // here would show the cockpit a version the real server never kept.
    await mockApi.saveGlobalContext("first customization");
    await mockApi.saveRole("assistant", { definitionMd: "first rewrite" });
    await mockApi.saveLessons("assistant", "general", "first learnings");

    expect(
      await documentRevisions(mockApi, "global_context", "global")
    ).toEqual([]);
    expect(
      await documentRevisions(mockApi, "role_definition", "assistant")
    ).toEqual([]);
    expect(
      await documentRevisions(mockApi, "lessons", "assistant::general")
    ).toEqual([]);
  });

  it("retains the state each write replaced, newest first", async () => {
    await mockApi.saveGlobalContext("first");
    await mockApi.saveGlobalContext("second");
    await mockApi.saveGlobalContext("third");
    // A reset is a write too — it persists a tombstoned row, which the write
    // after it retains.
    await mockApi.resetGlobalContext();
    await mockApi.saveGlobalContext("fourth");

    const versions = await documentRevisions(mockApi, 
      "global_context",
      "global"
    );
    expect(versions.map((v) => v.content.text)).toEqual(["", "third", "second"]);
    // The newest one is the doc as the reset left it — the tombstone flag is
    // why a restore of it can honestly go back to seed.
    expect(versions[0].content.tombstoned).toBe("true");
    expect(versions[1].content.tombstoned).toBe("false");
    expect(versions[0].actorId).toBeTruthy();
    expect(versions[0].createdTs).toBeGreaterThan(0);
  });

  it("keeps at most 3 revisions per document", async () => {
    for (const text of ["a", "b", "c", "d", "e"]) {
      await mockApi.saveGlobalContext(text);
    }
    const versions = await documentRevisions(mockApi, 
      "global_context",
      "global"
    );
    expect(versions.map((v) => v.content.text)).toEqual(["d", "c", "b"]);
  });

  it("scopes history per document, not per kind", async () => {
    await mockApi.saveLessons("assistant", "general", "assistant learnings");
    await mockApi.saveLessons("assistant", "general", "assistant learnings v2");
    expect(
      await documentRevisions(mockApi, "lessons", "researcher::general")
    ).toEqual([]);
    expect(
      await documentRevisions(mockApi, "lessons", "assistant::general")
    ).toHaveLength(1);
  });

  it("restore puts the old text back and retains what it overwrote", async () => {
    await mockApi.saveGlobalContext("original");
    await mockApi.saveGlobalContext("replacement");
    const [target] = await documentRevisions(mockApi, 
      "global_context",
      "global"
    );
    expect(target.content.text).toBe("original");

    const restored = await mockApi.restoreDocumentHistory(
      "global_context",
      "global",
      target.id
    );
    expect(restored.id).toBe(target.id);
    expect((await mockApi.getGlobalContext()).text).toBe("original");

    const after = await documentRevisions(mockApi, "global_context", "global");
    expect(after[0].content.text).toBe("replacement");
  });

  it("restoring a tombstoned revision puts the document back on its seed", async () => {
    await mockApi.saveRole("assistant", { definitionMd: "owner rewrite" });
    // The reset puts the role back on its seed (a tombstoned row); the write
    // after it is what retains that state as a revision.
    await mockApi.resetRole("assistant");
    await mockApi.saveRole("assistant", { definitionMd: "second rewrite" });
    const [seedVersion] = await documentRevisions(mockApi, 
      "role_definition",
      "assistant"
    );
    expect(seedVersion.content.tombstoned).toBe("true");
    // 🔴 TWO STATEMENTS, deliberately not one (T-40f0 node 11). This used to
    // assert `role.definitionMd === seedVersion.content.definition_md` after
    // the restore — one sentence that quietly claimed "what a tombstoned
    // revision STORES equals what the document folds to". On the real server
    // that is FALSE: the stored column is empty and the seed is supplied by the
    // fold. Writing it as one equality made the display-layer defect (the diff
    // announcing that restoring would wipe the document) unrepresentable in
    // any mock-built fixture.
    expect(seedVersion.content.definition_md).toBe("");

    await mockApi.restoreDocumentHistory(
      "role_definition",
      "assistant",
      seedVersion.id
    );
    const role = await mockApi.getRole("assistant");
    expect(role.isDefault).toBe(true);
    // …and the FOLD is where the seed text comes back — non-empty, and not the
    // rewrite it replaced.
    expect(role.definitionMd.length).toBeGreaterThan(0);
    expect(role.definitionMd).not.toBe("second rewrite");
    expect(role.definitionMd).toBe(
      (await mockApi.getDocumentSeed("role_definition", "assistant")).content
        .definition_md
    );
  });

  // T-1f39 split the manual's one four-field bundle into TWO independent
  // series and stopped versioning purpose/fields altogether. The restore is
  // narrow to match: exactly the field its series versions goes back, and the
  // rest of the manual is left where it stands.
  it("restores a task manual's SOP alone, leaving every other field where it is", async () => {
    const manual = await mockApi.createTaskManual("Review PR");
    await mockApi.updateTaskManual(manual.typeKey, {
      purpose: "review pull requests",
      sopMd: "## steps",
      learnings: "keep diffs small",
      fields: [{ name: "pr_url", required: true, isKey: true }],
    });
    // Later edits move all four fields; only the SOP one is retained.
    await mockApi.updateTaskManual(manual.typeKey, {
      purpose: "changed purpose",
      sopMd: "## rewritten",
      learnings: "changed learnings",
      fields: [],
    });

    const [previous] = await documentRevisions(mockApi, 
      "task_manual_sop",
      manual.typeKey
    );
    expect(previous.content).toEqual({ sop_md: "## steps" });
    await mockApi.restoreDocumentHistory(
      "task_manual_sop",
      manual.typeKey,
      previous.id
    );

    const back = await mockApi.getTaskManual(manual.typeKey);
    expect(back.sopMd).toBe("## steps");
    // The three fields this series does NOT version are untouched — before the
    // split, restoring dragged the purpose and the identifier fields back too.
    expect(back.purpose).toBe("changed purpose");
    expect(back.learnings).toBe("changed learnings");
    expect(back.fields).toEqual([]);
  });

  it("versions a manual's learnings on their own series, independent of the SOP", async () => {
    const manual = await mockApi.createTaskManual("Review PR");
    await mockApi.updateTaskManual(manual.typeKey, {
      sopMd: "## v1",
      learnings: "第一版經驗",
    });
    // THREE SOP-only writes: enough to wash a shared 3-slot series clean. The
    // learnings series must still be holding its own single revision.
    for (const sop of ["## v2", "## v3", "## v4"]) {
      await mockApi.updateTaskManual(manual.typeKey, { sopMd: sop });
    }
    await mockApi.updateTaskManual(manual.typeKey, { learnings: "第二版經驗" });

    const learnings = await documentRevisions(mockApi, 
      "task_manual_learnings",
      manual.typeKey
    );
    expect(learnings.map((v) => v.content)).toEqual([
      { learnings: "第一版經驗" },
    ]);
    const sops = await documentRevisions(mockApi, 
      "task_manual_sop",
      manual.typeKey
    );
    expect(sops.map((v) => v.content.sop_md)).toEqual([
      "## v3",
      "## v2",
      "## v1",
    ]);
  });

  it("retains nothing for an edit to the purpose or the identifier fields", async () => {
    const manual = await mockApi.createTaskManual("Review PR");
    await mockApi.updateTaskManual(manual.typeKey, {
      sopMd: "## steps",
      learnings: "keep diffs small",
    });
    await mockApi.updateTaskManual(manual.typeKey, {
      purpose: "review pull requests",
      fields: [{ name: "pr_url", required: true, isKey: true }],
      displayName: "PR 審查",
    });

    // Those three are not versioned at all (owner ruling), so neither series
    // moved — and the SOP series still holds only what a SOP write retained.
    expect(
      await documentRevisions(mockApi, "task_manual_sop", manual.typeKey)
    ).toEqual([]);
    expect(
      await documentRevisions(mockApi, "task_manual_learnings", manual.typeKey)
    ).toEqual([]);
  });

  // Deleting a document takes its retained revisions with it, in the same
  // transaction (dal.go DeleteRoleDef / DeleteLessonsOfRole / DeleteTaskManual):
  // history is readable by any authenticated caller, so a leftover revision is a
  // readable echo of a deleted document and makes the guide's 「永久移除」 false.
  // No live cockpit path reaches a stale row today — role keys and manual
  // type_keys are randomly minted, so a deleted key is never seen again — but
  // the mock is the cockpit's stand-in for the contract: one that still lists
  // history for a deleted document teaches the UI, and the next reader of this
  // file, a behaviour the server does not have.
  it("deleting a role drops its own history and the history of ALL its lessons", async () => {
    const { role } = await mockApi.createRole({ name: "臨時角色" });
    await mockApi.saveRole(role.key, { definitionMd: "改寫" });
    // TWO task types: the lessons history key is compound (`<role>::<type>`),
    // so a delete that only matched one exact key would leave the other behind.
    for (const taskType of ["general", "planning"]) {
      await mockApi.saveLessons(role.key, taskType, "第一版");
      await mockApi.saveLessons(role.key, taskType, "第二版");
      expect(
        await documentRevisions(mockApi, "lessons", `${role.key}::${taskType}`)
      ).toHaveLength(1);
    }
    expect(
      await documentRevisions(mockApi, "role_definition", role.key)
    ).toHaveLength(1);

    await mockApi.deleteRole(role.key);

    expect(
      await documentRevisions(mockApi, "role_definition", role.key)
    ).toEqual([]);
    expect(
      await documentRevisions(mockApi, "lessons", `${role.key}::general`)
    ).toEqual([]);
    expect(
      await documentRevisions(mockApi, "lessons", `${role.key}::planning`)
    ).toEqual([]);
  });

  it("deleting a task manual drops the history of BOTH its series", async () => {
    const manual = await mockApi.createTaskManual("Review PR");
    await mockApi.updateTaskManual(manual.typeKey, {
      sopMd: "第一版 SOP",
      learnings: "第一版經驗",
    });
    await mockApi.updateTaskManual(manual.typeKey, {
      sopMd: "第二版 SOP",
      learnings: "第二版經驗",
    });
    // Both series really hold something — a delete test whose fixture retained
    // nothing would pass on a delete that dropped nothing.
    expect(
      await documentRevisions(mockApi, "task_manual_sop", manual.typeKey)
    ).toHaveLength(1);
    expect(
      await documentRevisions(mockApi, "task_manual_learnings", manual.typeKey)
    ).toHaveLength(1);

    await mockApi.deleteTaskManual(manual.typeKey);

    expect(
      await documentRevisions(mockApi, "task_manual_sop", manual.typeKey)
    ).toEqual([]);
    expect(
      await documentRevisions(mockApi, "task_manual_learnings", manual.typeKey)
    ).toEqual([]);
  });

  it("rejects a revision id this document does not have", async () => {
    await mockApi.saveGlobalContext("only edit");
    await expect(
      mockApi.restoreDocumentHistory("global_context", "global", 9999)
    ).rejects.toSatisfy((e) => isHttpStatus(e, 404));
  });

  // The retired bundle: the server 400s BOTH routes and migration 00044 deleted
  // its rows, so a mock that answered 200 (or 404) would let a surface still
  // addressing the dead kind look alive in every frontend test and fail only in
  // production. The refusal must also NAME the two replacements — that is what
  // makes it actionable rather than a wall.
  it("refuses the retired task_manual kind on both routes, naming its two replacements", async () => {
    const manual = await mockApi.createTaskManual("週報");
    await mockApi.updateTaskManual(manual.typeKey, { sopMd: "第零版" });
    await mockApi.updateTaskManual(manual.typeKey, { sopMd: "第一版" });

    await expect(
      documentRevisions(mockApi, "task_manual", manual.typeKey)
    ).rejects.toSatisfy((e) => isHttpStatus(e, 400));
    await expect(
      mockApi.restoreDocumentHistory("task_manual", manual.typeKey, 1)
    ).rejects.toSatisfy(
      (e) =>
        e instanceof ApiError &&
        e.serverMessage.includes("task_manual_sop") &&
        e.serverMessage.includes("task_manual_learnings")
    );

    // Positive control: the two live series on the SAME manual still answer.
    expect(
      await documentRevisions(mockApi, "task_manual_sop", manual.typeKey)
    ).toHaveLength(1);
  });

  // ── 初始版本 (T-40f0) ─────────────────────────────────────────────────────
  // The mock has to mirror WHICH documents ship a default, or the offline
  // cockpit renders a 初始版本 row that compares fine here and 404s in
  // production (or the reverse).
  it("serves the shipped default of the two documents that have one", async () => {
    // The global block's default IS the empty document — and the field NAME has
    // to be there: to a diff, an absent key and an empty string are different
    // documents.
    expect(await mockApi.getDocumentSeed("global_context", "global")).toEqual({
      kind: "global_context",
      key: "global",
      content: { text: "", tombstoned: "true" },
    });

    // A seed role's default is its FILE seed, not whatever it says now.
    await mockApi.saveRole("assistant", { definitionMd: "owner's rewrite" });
    const seedRole = await mockApi.getDocumentSeed(
      "role_definition",
      "assistant"
    );
    expect(seedRole.content.definition_md).not.toBe("owner's rewrite");
    expect(seedRole.content.definition_md.length).toBeGreaterThan(0);
    expect(seedRole.content.tombstoned).toBe("true");
    // …and the live document was NOT put back by reading it (the whole safety
    // claim of this route).
    expect((await mockApi.getRole("assistant")).definitionMd).toBe(
      "owner's rewrite"
    );
  });

  it("404s for every document that ships no default — the same set a reset refuses", async () => {
    const { role: custom } = await mockApi.createRole({ name: "臨時角色" });
    const manual = await mockApi.createTaskManual("週報");

    for (const probe of [
      ["role_definition", custom.key],
      ["task_manual_sop", manual.typeKey],
      ["task_manual_learnings", manual.typeKey],
      ["lessons", `${custom.key}::general`],
    ] as const) {
      await expect(
        mockApi.getDocumentSeed(probe[0], probe[1])
      ).rejects.toSatisfy((e) => isHttpStatus(e, 404));
      // The equivalence itself: the reset of that same document also refuses.
      // (Only role_definition HAS a reset route; the other three have none at
      // all, which is the stronger form of the same fact.)
    }
    await expect(mockApi.resetRole(custom.key)).rejects.toSatisfy((e) =>
      isHttpStatus(e, 404)
    );

    // Positive control: the seed role's default is served, so this is an
    // equivalence and not a blanket 404.
    expect(
      (await mockApi.getDocumentSeed("role_definition", "assistant")).content
        .definition_md.length
    ).toBeGreaterThan(0);
  });
});
