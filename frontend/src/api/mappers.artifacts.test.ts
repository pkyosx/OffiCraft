// mappers — task artifact projection (T-3dc5, reshaped by T-92). NEITHER task
// projection folds artifact rows any more: the full task and the light list
// both carry the server's `artifact_count` and nothing else, so the two agree
// by construction instead of one taking an array's length. The artifact ROW
// itself is the narrowed T-92 shape (`name` + `description` in place of the
// single `label`, and no filename/is_image/attachment_id — those are derived
// from `name`/`mime`/`url` at the one place that draws a row). An unknown kind
// still falls back to "link" — the kind whose url is an external address rather
// than a blob serve path — instead of fabricating a file/image. (Every kind is
// blob-backed since T-92; what separates them on this row is what `url` means.)

import { describe, it, expect } from "vitest";
import {
  toTask,
  toTaskListItem,
  toTaskArtifact,
  toTaskArtifactVersion,
} from "./mappers";
import type {
  WireTask,
  WireTaskListItem,
  WireTaskArtifact,
  WireTaskArtifactVersion,
} from "./wire";

// The generated wire types carry every field (server response DTO is handwritten
// always-present); these helpers build complete wire objects so the tests
// typecheck while still exercising the mapper's projection/narrowing.
function wireArtifact(over: Partial<WireTaskArtifact>): WireTaskArtifact {
  return {
    id: "ta-0",
    kind: "link",
    url: "",
    name: "",
    description: "",
    mime: "",
    created_ts: 0,
    created_by: "",
    version_count: 1,
    ...over,
  };
}

function wireVersion(
  over: Partial<WireTaskArtifactVersion>,
): WireTaskArtifactVersion {
  return {
    id: 1,
    kind: "link",
    url: "",
    name: "",
    description: "",
    filename: "",
    mime: "",
    is_image: false,
    attachment_id: "",
    created_ts: 0,
    created_by: "",
    ...over,
  };
}

function wireTask(over: Partial<WireTask>): WireTask {
  return {
    id: "t-1",
    task_no: "T-1",
    title: "",
    type_key: "",
    description: "",
    status: "in_progress",
    lock: "",
    priority: "mid",
    executor_kind: "member",
    executor_id: "",
    creator_id: "",
    reassigned_from: "",
    reassigned_from_kind: "",
    handover_note: "",
    handover_note_ts: 0,
    handover_note_by: "",
    dedupe_key: "",
    duplicate_of: "",
    waiting_reason: "",
    inputs: {},
    closed_ts: null,
    created_ts: 0,
    updated_ts: 0,
    closeout_reported: false,
    deps: [],
    steps: [],
    progress_done: 0,
    progress_total: 0,
    // T-74f8: the declared destination of the ball at close. Always-present on
    // the wire ("" = never declared), so the complete-wire-object helper has to
    // carry it or nothing here typechecks.
    handoff: "",
    handoff_note: "",
    handoff_task_id: "",
    // T-6020: who froze the task ("" = not frozen). Same always-present shape.
    frozen_by: "",
    // T-66: the task read DESCRIBES ITSELF — always "summary"/false, because
    // get_task no longer carries the step notes. Always-present on the wire,
    // so the complete-wire-object helper carries it like the rest.
    detail_level: "summary",
    notes_included: false,
    // T-92: the deliverables are said in ONE number now. The task read used to
    // carry an id+label index (and, before that, the whole rows); it carries
    // this count and nothing else, so the field the mapper reads is the same
    // one the light list has always read.
    artifact_count: 0,
    ...over,
  };
}

function wireListItem(over: Partial<WireTaskListItem>): WireTaskListItem {
  return {
    id: "t-1",
    task_no: "T-1",
    title: "",
    type_key: "",
    status: "in_progress",
    lock: "",
    priority: "mid",
    executor_kind: "member",
    executor_id: "",
    creator_id: "",
    reassigned_from: "",
    reassigned_from_kind: "",
    dedupe_key: "",
    duplicate_of: "",
    waiting_reason: "",
    closed_ts: null,
    created_ts: 0,
    updated_ts: 0,
    deps: [],
    progress_done: 0,
    progress_total: 0,
    artifact_count: 0,
    // The light list's current-node pair ("" = no plan / all done). Always
    // present on the wire, so this complete-object helper carries it.
    current_step_id: "",
    current_step_name: "",
    ...over,
  };
}

describe("toTaskArtifact", () => {
  it("passes a link artifact through honestly", () => {
    expect(
      toTaskArtifact(
        wireArtifact({
          id: "ta-1",
          kind: "link",
          url: "https://x/pr/1",
          name: "PR #1",
          description: "第一版設計稿的 PR",
        }),
      ),
    ).toEqual({
      id: "ta-1",
      kind: "link",
      url: "https://x/pr/1",
      name: "PR #1",
      description: "第一版設計稿的 PR",
      mime: "",
      createdTs: 0,
      createdBy: "",
      versionCount: 1,
    });
  });

  it("carries a file/image row's url + mime, the only blob facts left on it", () => {
    // T-92 took filename / is_image / attachment_id off this row: the first is
    // folded into the server-derived `name`, and the other two are one fact
    // each already said by `mime` and by `url`. What the mapper must still hand
    // through untouched is the pair a renderer cannot derive from anything
    // else — where the bytes are, and what they are.
    expect(
      toTaskArtifact(
        wireArtifact({
          id: "ta-2",
          kind: "image",
          url: "/api/chat/attachment/att-9",
          mime: "image/png",
          name: "shot.png",
        }),
      ),
    ).toMatchObject({
      kind: "image",
      url: "/api/chat/attachment/att-9",
      mime: "image/png",
      name: "shot.png",
    });
  });

  it("keeps the row NARROW — no filename/is_image/attachment_id sneaks back", () => {
    // The T-92 narrowing asserted as a shape, not just field by field: a mapper
    // that re-derived the three removed fields here would put them back in the
    // cockpit's hands and let a component believe they arrive on the wire.
    expect(Object.keys(toTaskArtifact(wireArtifact({}))).sort()).toEqual([
      "createdBy",
      "createdTs",
      "description",
      "id",
      "kind",
      "mime",
      "name",
      "url",
      "versionCount",
    ]);
  });

  it("falls back an unknown kind to link (the no-blob shape)", () => {
    expect(toTaskArtifact(wireArtifact({ id: "ta-3", kind: "video" })).kind).toBe("link");
  });

  it("carries version_count so the row can offer a versions entry", () => {
    expect(toTaskArtifact(wireArtifact({ version_count: 4 })).versionCount).toBe(4);
  });

  it("reads an absent version_count as 0, not as one version", () => {
    const { version_count: _dropped, ...withoutTheField } = wireArtifact({});
    expect(
      toTaskArtifact(withoutTheField as WireTaskArtifact).versionCount,
    ).toBe(0);
  });

  it("reads an absent name/description as empty — it does NOT invent one", () => {
    // A current server always sends a non-empty `name` (it derives one from the
    // blob's filename, the link target, or the id). The `?? ""` here is for an
    // OLDER server or a hand-built fixture, and what it must not do is
    // fabricate: deciding what a nameless artifact LOOKS like is the renderer's
    // job (TaskArtifactsPopover's own `artifactDisplayName` fallback), on the
    // full row it fetched through listTaskArtifacts.
    const { name: _n, description: _d, ...withoutTheFields } = wireArtifact({});
    const view = toTaskArtifact(withoutTheFields as WireTaskArtifact);
    expect([view.name, view.description]).toEqual(["", ""]);
  });
});

describe("toTaskArtifactVersion", () => {
  it("passes a retained version through honestly", () => {
    expect(
      toTaskArtifactVersion(
        wireVersion({
          id: 7,
          kind: "file",
          url: "/api/chat/attachment/att-old",
          attachment_id: "att-old",
          name: "",
          description: "",
          filename: "v1.pdf",
          mime: "application/pdf",
          is_image: false,
          created_ts: 1700,
          created_by: "mira",
        }),
      ),
    ).toEqual({
      id: 7,
      kind: "file",
      url: "/api/chat/attachment/att-old",
      attachmentId: "att-old",
      name: "",
      description: "",
      filename: "v1.pdf",
      mime: "application/pdf",
      isImage: false,
      createdTs: 1700,
      createdBy: "mira",
    });
  });

  it("keeps the version journal WIDER than the live row (T-92 narrowed only the live one)", () => {
    // The version row was deliberately NOT narrowed: it is a cockpit-only read
    // of a bounded handful of rows rather than a cost paid on every ticket
    // read. So `filename`/`isImage`/`attachmentId` are still here — and the
    // versions modal's `v.filename || v.name` chain depends on it.
    const v = toTaskArtifactVersion(
      wireVersion({ filename: "v1.pdf", attachment_id: "att-old", is_image: false }),
    );
    expect([v.filename, v.attachmentId, v.isImage]).toEqual(["v1.pdf", "att-old", false]);
  });

  it("splits the old single label into this version's OWN name + description", () => {
    // T-92 split `label` on the history table too, so a version carries the two
    // columns as they were written. Unlike the live artifact's, this `name` is
    // NOT derived and can be empty — which is exactly why the row still has a
    // `filename` to fall back to.
    const v = toTaskArtifactVersion(
      wireVersion({ name: "設計稿", description: "評審前的版本" }),
    );
    expect([v.name, v.description]).toEqual(["設計稿", "評審前的版本"]);
  });

  it("reads a missing filename as empty rather than undefined", () => {
    const { filename: _dropped, ...withoutTheField } = wireVersion({});
    expect(
      toTaskArtifactVersion(withoutTheField as WireTaskArtifactVersion).filename,
    ).toBe("");
  });

  it("falls back an unknown kind to link (the no-blob shape)", () => {
    expect(toTaskArtifactVersion(wireVersion({ kind: "video" })).kind).toBe("link");
  });

  it("reads an image version's is_image rather than deriving it", () => {
    const v = toTaskArtifactVersion(
      wireVersion({ kind: "image", mime: "image/png", is_image: true }),
    );
    expect([v.mime, v.isImage]).toEqual(["image/png", true]);
  });
});

describe("toTask / toTaskListItem artifact folding", () => {
  it("full task carries the SERVER's count and no artifact rows at all", () => {
    const view = toTask(wireTask({ artifact_count: 2 }));
    expect(view.artifactCount).toBe(2);
    // T-92 (owner rc-15016959ad4d:「只有 ID 好像也沒用」): the task read carries a
    // count and NOTHING a renderer could draw a row from — not even ids. T-66
    // had already cut the rows to id + label; the ids went too, because a
    // caller holding one is about to act on that artifact and needs the whole
    // row anyway. A mapper that folded an array back in here would put the fat
    // shape into the cockpit's hands and let a component believe it need not
    // fetch through `listTaskArtifacts`.
    expect(Object.keys(view)).not.toContain("artifacts");
  });

  it("full task with nothing pinned reports count 0", () => {
    const view = toTask(wireTask({}));
    expect(view.artifactCount).toBe(0);
    expect(Object.keys(view)).not.toContain("artifacts");
  });

  it("full task reads the count off artifact_count, never off an array's length", () => {
    // 🔴 THE REGRESSION CASE. This used to be `(w.artifacts ?? []).length`, and
    // against a payload that stopped carrying `artifacts` that reads 0 — the
    // badge would vanish from every hydrated card while the light row beside it
    // still said 「產物 N」. The count is the wire's own number or nothing.
    expect(toTask(wireTask({ artifact_count: 7 })).artifactCount).toBe(7);
  });

  it("light list item carries the server count", () => {
    expect(toTaskListItem(wireListItem({ artifact_count: 3 })).artifactCount).toBe(3);
  });

  it("light list item with a 0 count keeps the badge hidden", () => {
    expect(toTaskListItem(wireListItem({ artifact_count: 0 })).artifactCount).toBe(0);
  });

  it("both projections read the SAME field, so the two rows cannot disagree", () => {
    // The whole point of T-92 giving the full task a count: the collapsed card
    // and the hydrated one now say the same number by construction.
    expect(toTask(wireTask({ artifact_count: 5 })).artifactCount).toBe(
      toTaskListItem(wireListItem({ artifact_count: 5 })).artifactCount,
    );
  });
});
