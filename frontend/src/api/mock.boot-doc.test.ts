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
import {
  mockApi,
  __resetMock,
  __injectMockTask,
  __injectMockOutsourceWorker,
  __setBootDocReadOnly,
} from "./mock";
import { ApiError } from "./errors";
import type { BootDocKind } from "../types";
import { BOOT_DOC_HISTORY_KEPT, BOOT_DOC_CAP_CHARS_DEFAULTS } from "./docCap";
import {
  SEED_SYSTEM_INTERACTION_MD,
  SEED_BOOT_SEQUENCE_MD,
  SEED_BOOT_SEQUENCE_CODEX_MD,
} from "./seeds";
import { documentRevisions } from "../test/documentHistory";
import { docJoinHeadBody, docSplitHeadBody } from "./docSplit";

/** What the mock STORES when `body` is saved into a document whose seed carries
 * a read-only head (T-3201): the shipped head, then the body. The wire carries
 * only the body — the head is the server's to put back — so every expectation
 * about stored text or a retained revision has to say so out loud. */
function storedFor(seed: string, body: string): string {
  const { head, split } = docSplitHeadBody(seed.trim());
  return split ? docJoinHeadBody(head, body) : body;
}

/** The editable half of a seed — what a caller would send back unchanged. */
function bodyOf(seed: string): string {
  const { body, split } = docSplitHeadBody(seed.trim());
  return split ? body : seed.trim();
}

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

  it("refuses every write to a read-only document with 405, and still serves its text", async () => {
    // 🔴 NO SHIPPED DOCUMENT IS READ-ONLY ANY MORE (T-6f44, owner's decision 2:
    // 〈新任務〉 and 〈擋著你手上任務的票解開了〉 became editable like the other
    // eight), so this drives the refusal with a SYNTHETIC one. The rule it
    // guards is unchanged and still reachable the day a document ships
    // read-only: reading works — that is the whole reason these are documents
    // rather than string literals — and BOTH write faces refuse. 405, not 403:
    // no principal may edit them, so an authz answer would send an owner
    // looking for a role to grant.
    //
    // That decision 2 actually took effect is pinned separately, by
    // mock.boot-doc-registry.test.ts against the shared registry table — this
    // test supplies its own list and so cannot notice.
    __setBootDocReadOnly(["task_takeover_fresh/global", "task_unblocked/global"]);
    for (const kind of ["task_takeover_fresh", "task_unblocked"] as const) {
      const doc = await mockApi.getBootDoc(kind, "global");
      expect(doc.readOnly).toBe(true);
      expect(doc.text).not.toBe("");

      await expect(
        mockApi.saveBootDoc(kind, "global", "owner 想改的內容")
      ).rejects.toMatchObject({ status: 405 });
      await expect(mockApi.resetBootDoc(kind, "global")).rejects.toMatchObject({
        status: 405,
      });

      // …and the refusal wrote NOTHING: the document is still the shipped one.
      expect(await mockApi.getBootDoc(kind, "global")).toMatchObject({
        text: doc.text,
        isDefault: true,
      });
    }
  });

  it("lets the editable documents through the same faces the read-only ones bounce off", async () => {
    // The paired control: without it, a mock that refused EVERY write would
    // pass the assertions above while making the whole surface read-only.
    for (const kind of [
      "accelerated_stop",
      "task_closeout",
      "task_reassign_predecessor",
      "task_takeover_with_predecessor",
    ] as const) {
      expect((await mockApi.getBootDoc(kind, "global")).readOnly).toBe(false);
      // T-91: both writes answer a receipt, so the document is READ BACK.
      await mockApi.saveBootDoc(kind, "global", `${kind} 改過
`);
      const saved = await mockApi.getBootDoc(kind, "global");
      expect(saved).toMatchObject({ isDefault: false });
      expect(saved.text).toContain("改過");
      await mockApi.resetBootDoc(kind, "global");
      expect(await mockApi.getBootDoc(kind, "global")).toMatchObject({
        isDefault: true,
      });
    }
  });

  it("404s a kind nobody registered, the same way an unknown key is a 404", async () => {
    // Not a 400: the pair addresses A DOCUMENT, and a document this server does
    // not have is not found.
    await expect(
      mockApi.getBootDoc("task_started" as BootDocKind, "global")
    ).rejects.toMatchObject({ status: 404 });
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
    // T-91: the reset answers a receipt, so the restored document is READ BACK.
    await mockApi.resetBootDoc("boot_sequence", "claude");
    const back = await mockApi.getBootDoc("boot_sequence", "claude");
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
    expect(kept[0].content.text).toBe(
      storedFor(SEED_BOOT_SEQUENCE_MD, "壞掉的內容")
    );
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

  it("refuses a save that empties a document, and retains no version for it", async () => {
    // The real server has refused this since T-2d99; the mock did not, so demo
    // mode would blank a boot document the server would have kept. An empty
    // boot sequence is not a small document, it is an agent with no
    // instructions.
    const before = await mockApi.getBootDoc("boot_sequence", "claude");
    for (const wipe of ["", "   ", "\n\t "]) {
      await expect(
        mockApi.saveBootDoc("boot_sequence", "claude", wipe)
      ).rejects.toMatchObject({ status: 400 });
    }
    expect(await mockApi.getBootDoc("boot_sequence", "claude")).toMatchObject({
      text: before.text,
      isDefault: true,
    });
    expect(await documentRevisions(mockApi, "boot_sequence", "claude")).toHaveLength(0);
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
      bodyOf(SEED_BOOT_SEQUENCE_CODEX_MD)
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
      text: storedFor(SEED_BOOT_SEQUENCE_MD, "第一版"),
      body: "第一版",
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

// ── 開機脈絡預覽（T-30e4） ──────────────────────────────────────────────
//
// The preview is the ONLY consumer of these documents that a person reads with
// their own eyes, and until T-30e4 it was the one place that did not fold: it
// read the seed constants straight, so the owner's edit was invisible in the
// very screen built to show what an agent will read.
//
// The two rules pinned here are asymmetric on purpose:
//
//   * 系統互動 and 啟動步驟 must FOLD (overlay wins) — that is the fix;
//   * the preview must take the CLAUDE boot sequence and must NOT grow a
//     runtime parameter — the real request carries no member (http.ts sends
//     `{role}` only, deliberately, so the server mints no token), so the real
//     server also resolves an empty runtime and hands back the claude document.
//     A runtime-aware mock would be a mock that disagrees with production.
describe("mockApi · 開機脈絡預覽", () => {
  it("shows the owner's edit rather than the factory text", async () => {
    const before = (await mockApi.getBootstrap("assistant")).context;
    expect(before).toContain(SEED_SYSTEM_INTERACTION_MD.trim());
    expect(before).toContain(SEED_BOOT_SEQUENCE_MD.trim());

    await mockApi.saveBootDoc(
      "system_interaction",
      "global",
      "系統互動：owner 改過的版本\n"
    );
    await mockApi.saveBootDoc(
      "boot_sequence",
      "claude",
      "啟動步驟：owner 改過的版本\n"
    );

    const after = (await mockApi.getBootstrap("assistant")).context;
    expect(after).toContain("系統互動：owner 改過的版本");
    expect(after).toContain("啟動步驟：owner 改過的版本");
    // Whole-document comparison, not a keyword probe: an overlay REPLACES the
    // document, so not one byte of the seed may survive.
    expect(after).not.toContain(SEED_SYSTEM_INTERACTION_MD.trim());
    expect(after).not.toContain(SEED_BOOT_SEQUENCE_MD.trim());
  });

  // 🔴 READ THIS BEFORE "FIXING" THE ASSERTION BELOW. It pins a limitation, not
  // a desirable behaviour: a codex member's panel shows the CLAUDE 啟動步驟,
  // whose step 3 says the opposite of what that member is really booted with.
  // The mock is right to copy it — /api/bootstrap genuinely cannot resolve a
  // runtime, because the request names no member. The day that endpoint learns
  // who the preview is for, THIS ASSERTION IS THE ONE THAT MUST CHANGE FIRST;
  // it is not a guard you are breaking, it is the guard telling you the server
  // contract moved.
  it("takes the claude boot sequence, never codex — matching a request that names no member", async () => {
    await mockApi.saveBootDoc("boot_sequence", "codex", "codex 版\n");
    await mockApi.saveBootDoc("boot_sequence", "claude", "claude 版\n");
    const ctx = (await mockApi.getBootstrap("assistant")).context;
    expect(ctx).toContain("claude 版");
    expect(ctx).not.toContain("codex 版");
  });
  it("folds the OUTSOURCE preview too — the same defect was written twice", async () => {
    // getBootstrap and getWorkerBootContext assembled the same blocks from the
    // same constants side by side. Fixing only the one the ticket named would
    // have left an identical un-folded preview one panel over, so this pins the
    // twin. It also pins the ONE real difference: a spawn names its worker, so
    // this path does resolve runtime — codex gets the codex document.
    __injectMockTask({
      id: "t-30e4",
      taskNo: "T-30e4",
      title: "外包任務",
      typeKey: "",
      description: "",
      status: "in_progress",
      priority: "mid",
      executorKind: "outsource",
      executorId: "ow-30e4",
      creatorId: "",
      dedupeKey: "",
      deps: [],
      waitingReason: "",
      duplicateOf: "",
      createdTs: 0,
      updatedTs: 0,
      closedTs: null,
      progressDone: 0,
      progressTotal: 0,
      steps: [],
    });
    __injectMockOutsourceWorker({
      id: "ow-30e4",
      codename: "O-1",
      model: "Opus",
      effort: "high",
      runtime: "codex",
      taskId: "t-30e4",
    });

    await mockApi.saveBootDoc(
      "system_interaction",
      "global",
      "系統互動：owner 改過的版本\n"
    );
    await mockApi.saveBootDoc("boot_sequence", "codex", "codex 版\n");
    await mockApi.saveBootDoc("boot_sequence", "claude", "claude 版\n");

    const ctx = await mockApi.getWorkerBootContext("ow-30e4");
    expect(ctx).toContain("系統互動：owner 改過的版本");
    expect(ctx).not.toContain(SEED_SYSTEM_INTERACTION_MD.trim());
    // Runtime-resolved, and folded — both halves, or the assertion passes on a
    // mock that folds the WRONG document.
    expect(ctx).toContain("codex 版");
    expect(ctx).not.toContain("claude 版");
    expect(ctx).not.toContain(SEED_BOOT_SEQUENCE_CODEX_MD.trim());
  });

  it("carries exactly ONE lessons title, even when the document already starts with it", async () => {
    // A generation that treats its boot segment as the document base and writes
    // it back turns the injected title into document content. Without the
    // idempotent strip the preview then shows one more title per generation —
    // drift the server does not have, in the screen built to show what the
    // server sends.
    const title = "# Lessons (assistant)";
    await mockApi.saveLessons(
      "assistant",
      `${title}\n\n${title}\n\n學到的東西\n`
    );
    const ctx = (await mockApi.getBootstrap("assistant")).context;
    expect(ctx).toContain("學到的東西");
    expect(ctx.split(title).length - 1).toBe(1);
  });

  it("strips the PRE-T-2 lessons title too, so a document poisoned before the axis was dropped self-heals", async () => {
    // T-2 removed the task_type axis, and with it the "/ general" half of this
    // title. A document poisoned BEFORE that change carries the old wording, so
    // a strip that only knew the new title would leave it wedged at the top of
    // the document with nothing able to reach it. The server strips both
    // (assets.go); this is the mock held to the same rule.
    const legacy = "# Lessons (assistant / general)";
    await mockApi.saveLessons("assistant", `${legacy}\n\n學到的東西\n`);
    const ctx = (await mockApi.getBootstrap("assistant")).context;
    expect(ctx).toContain("學到的東西");
    expect(ctx).not.toContain(legacy);
    expect(ctx.split("# Lessons (assistant)").length - 1).toBe(1);
  });
});
