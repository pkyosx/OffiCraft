// api/mock.boot-doc.test.ts — the three boot-context document streams in the
// mock adapter (T-791e).
//
// The mock is the adapter every frontend test runs against, so the rules pinned
// here are the ones a UI test would otherwise be measuring against a server
// that does not exist. Three of them are rulings rather than mechanics and are
// the reason this file exists at all:
//
//   * TEN retained revisions, not the three every other document keeps;
//   * counted in WRITES — a save that changes nothing must not burn a slot;
//   * the three streams are INDEPENDENT, and in particular claude and codex are
//     different documents that share no storage.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";
import { ApiError } from "./errors";
import { BOOT_DOC_HISTORY_KEPT, BOOT_DOC_CAP_CHARS_DEFAULTS } from "./docCap";
import {
  SEED_SYSTEM_INTERACTION_MD,
  SEED_BOOT_SEQUENCE_MD,
  SEED_BOOT_SEQUENCE_CODEX_MD,
} from "./seeds";
import { documentRevisions } from "../test/documentHistory";

beforeEach(() => {
  __resetMock();
});

describe("mockApi · boot-context blocks", () => {
  it("folds each of the three streams onto its OWN seed", async () => {
    const cases = [
      ["system_interaction", "global", SEED_SYSTEM_INTERACTION_MD],
      ["boot_sequence", "claude", SEED_BOOT_SEQUENCE_MD],
      ["boot_sequence", "codex", SEED_BOOT_SEQUENCE_CODEX_MD],
    ] as const;
    for (const [kind, key, seed] of cases) {
      const doc = await mockApi.getBootDoc(kind, key);
      expect(doc).toMatchObject({
        kind,
        key,
        text: seed.trim(),
        isDefault: true,
        hasSeed: true,
        capChars: BOOT_DOC_CAP_CHARS_DEFAULTS[kind],
      });
      expect(doc.sizeChars).toBe([...seed.trim()].length);
    }
  });

  it("404s a key that names no document instead of inventing a fourth stream", async () => {
    // A save on an unrecognised runtime key would otherwise create a document
    // the server has no route for — a page that looks like it is working and is
    // writing into nothing.
    await expect(
      mockApi.getBootDoc("boot_sequence", "gemini")
    ).rejects.toBeInstanceOf(ApiError);
    await expect(
      mockApi.saveBootDoc("boot_sequence", "gemini", "x")
    ).rejects.toMatchObject({ status: 404 });
    await expect(
      mockApi.getBootDoc("system_interaction", "claude")
    ).rejects.toMatchObject({ status: 404 });
  });

  it("keeps claude and codex completely apart", async () => {
    await mockApi.saveBootDoc("boot_sequence", "claude", "只有 claude");
    expect(await mockApi.getBootDoc("boot_sequence", "codex")).toMatchObject({
      text: SEED_BOOT_SEQUENCE_CODEX_MD.trim(),
      isDefault: true,
    });
    // …and their histories do not bleed either.
    await mockApi.saveBootDoc("boot_sequence", "claude", "claude 第二版");
    expect(
      await documentRevisions(mockApi, "boot_sequence", "codex")
    ).toEqual([]);
    expect(
      (await documentRevisions(mockApi, "boot_sequence", "claude")).length
    ).toBe(1);
  });

  it("retains TEN revisions, counted in saves", async () => {
    // The first write to a never-customised document replaces nothing and
    // retains nothing (server parity), so N writes leave N-1 revisions.
    for (let i = 0; i < BOOT_DOC_HISTORY_KEPT + 5; i++) {
      await mockApi.saveBootDoc("boot_sequence", "claude", `版本 ${i}\n`);
    }
    const kept = await documentRevisions(mockApi, "boot_sequence", "claude");
    expect(kept.length).toBe(BOOT_DOC_HISTORY_KEPT);
    // Newest first, and the oldest ones really were pushed out — which is the
    // half of the ruling the cockpit has to warn about.
    expect(kept[0].content.text).toContain(`版本 ${BOOT_DOC_HISTORY_KEPT + 3}`);
    expect(
      kept.some((v) => (v.content.text ?? "").includes("版本 0"))
    ).toBe(false);
  });

  it("keeps only three for every OTHER document, so the ten is this kind's own", async () => {
    // The control. Without it, a mock that quietly raised the cap for
    // everything would satisfy the assertion above.
    for (let i = 0; i < 8; i++) {
      await mockApi.saveGlobalContext(`版本 ${i}`);
    }
    expect(
      (await documentRevisions(mockApi, "global_context", "global")).length
    ).toBe(3);
  });

  it("restores the factory version without writing the seed text back as an edit", async () => {
    await mockApi.saveBootDoc("boot_sequence", "claude", "壞掉的內容");
    const back = await mockApi.resetBootDoc("boot_sequence", "claude");
    // 🔴 `isDefault` true is the load-bearing half. A "reset" implemented as a
    // replace carrying the seed text renders identically and leaves the
    // document owner-edited forever — and on a server whose seed file later
    // changes, pins it to the OLD text.
    expect(back).toMatchObject({
      text: SEED_BOOT_SEQUENCE_MD.trim(),
      isDefault: true,
    });
    // The discarded content survives as a revision — a destructive write with
    // no way back would be the one write the history does not cover.
    const kept = await documentRevisions(mockApi, "boot_sequence", "claude");
    expect(kept[0].content.text).toBe("壞掉的內容");
  });

  it("refuses an over-cap save that is not getting shorter, and allows one that is", async () => {
    const cap = BOOT_DOC_CAP_CHARS_DEFAULTS.boot_sequence;
    await expect(
      mockApi.saveBootDoc("boot_sequence", "claude", "超".repeat(cap + 1))
    ).rejects.toMatchObject({ status: 400 });
    expect(await mockApi.getBootDoc("boot_sequence", "claude")).toMatchObject({
      isDefault: true,
    });

    // Equal length over the cap is refused too — the boundary case, and the one
    // an "over the limit means longer than before" reading gets wrong.
    await expect(
      mockApi.saveBootDoc("boot_sequence", "claude", "y".repeat(cap + 1))
    ).rejects.toMatchObject({ status: 400 });
    // (The converging-downward escape hatch — an already over-cap document may
    // still be edited, as long as it is getting shorter — is the predicate's
    // own rule and is pinned against the shared fixture both sides read,
    // bin/tests/fixtures/doc-cap-cases.tsv via api/docCap.test.ts. There is no
    // cockpit path that can put a document over the line, so there is nothing
    // to reproduce here.)
  });

  it("retains no version when a save changes nothing", async () => {
    // Ten slots stop meaning much if a no-op save spends one. Owner ruling, and
    // it matters precisely on the surface where someone pastes, looks, and
    // pastes again.
    await mockApi.saveBootDoc("boot_sequence", "claude", "第一版");
    await mockApi.saveBootDoc("boot_sequence", "claude", "第二版");
    const before = await documentRevisions(mockApi, "boot_sequence", "claude");
    await mockApi.saveBootDoc("boot_sequence", "claude", "第二版");
    expect(await documentRevisions(mockApi, "boot_sequence", "claude")).toEqual(
      before
    );

    // …including the case that would otherwise be a silent lie: re-saving the
    // factory text unchanged must not flip the document to owner-edited.
    await mockApi.resetBootDoc("boot_sequence", "codex");
    await mockApi.saveBootDoc(
      "boot_sequence",
      "codex",
      SEED_BOOT_SEQUENCE_CODEX_MD.trim()
    );
    expect(await mockApi.getBootDoc("boot_sequence", "codex")).toMatchObject({
      isDefault: true,
    });
  });

  it("serves the shipped default through the document-seed route", async () => {
    // What makes 初始版本 readable and diffable before anyone goes back to it.
    expect(
      await mockApi.getDocumentSeed("boot_sequence", "codex")
    ).toMatchObject({
      kind: "boot_sequence",
      key: "codex",
      content: { text: SEED_BOOT_SEQUENCE_CODEX_MD.trim(), tombstoned: "true" },
    });
    await expect(
      mockApi.getDocumentSeed("boot_sequence", "gemini")
    ).rejects.toMatchObject({ status: 404 });
  });

  it("restoring a retained revision puts the block back, tombstone included", async () => {
    await mockApi.saveBootDoc("boot_sequence", "claude", "第一版");
    await mockApi.saveBootDoc("boot_sequence", "claude", "第二版");
    const [older] = await documentRevisions(mockApi, "boot_sequence", "claude");
    await mockApi.restoreDocumentHistory("boot_sequence", "claude", older.id);
    expect(await mockApi.getBootDoc("boot_sequence", "claude")).toMatchObject({
      text: "第一版",
      isDefault: false,
    });

    // And restoring the tombstoned 初始版本 row puts it back ON the seed rather
    // than writing the seed text in as an edit.
    const seedRow = (
      await documentRevisions(mockApi, "boot_sequence", "claude")
    ).find((v) => v.content.tombstoned === "true");
    expect(seedRow).toBeUndefined();
    await mockApi.resetBootDoc("boot_sequence", "claude");
    const afterReset = await documentRevisions(mockApi, 
      "boot_sequence",
      "claude"
    );
    expect(afterReset[0].content.tombstoned).toBe("false");
  });
});
