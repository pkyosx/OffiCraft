// mappers — task artifact projection (T-3dc5). The full task folds the artifact
// rows + a count == length; the light list carries only the count (artifacts
// []); an unknown kind falls back to "link" (the no-blob shape) rather than
// fabricating a file/image.

import { describe, it, expect } from "vitest";
import {
  toTask,
  toTaskListItem,
  toTaskArtifact,
  toTaskArtifactRef,
  toTaskArtifactVersion,
} from "./mappers";
import type {
  WireTask,
  WireTaskListItem,
  WireTaskArtifact,
  WireTaskArtifactRef,
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
    label: "",
    filename: "",
    mime: "",
    is_image: false,
    attachment_id: "",
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
    label: "",
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
    // T-66: and the ARTIFACT rows describe themselves too — always "index",
    // because the task read carries id + label per deliverable and nothing else
    // (owner c-cd063427fb2f). Always-present on the wire, same as the pair above.
    artifacts_detail_level: "index",
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
      toTaskArtifact(wireArtifact({ id: "ta-1", kind: "link", url: "https://x/pr/1", label: "PR #1" })),
    ).toMatchObject({
      id: "ta-1",
      kind: "link",
      url: "https://x/pr/1",
      label: "PR #1",
      isImage: false,
      attachmentId: "",
    });
  });

  it("carries file/image blob metadata", () => {
    expect(
      toTaskArtifact(
        wireArtifact({
          id: "ta-2",
          kind: "image",
          url: "/api/chat/attachment/att-9",
          attachment_id: "att-9",
          mime: "image/png",
          filename: "shot.png",
          is_image: true,
        }),
      ),
    ).toMatchObject({
      kind: "image",
      isImage: true,
      mime: "image/png",
      filename: "shot.png",
      attachmentId: "att-9",
    });
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
          label: "",
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
      label: "",
      filename: "v1.pdf",
      mime: "application/pdf",
      isImage: false,
      createdTs: 1700,
      createdBy: "mira",
    });
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

describe("toTaskArtifactRef (T-66 index row)", () => {
  it("passes id + label through and reads a missing label as empty", () => {
    const w: WireTaskArtifactRef = { id: "ta-7", label: "設計稿" };
    expect(toTaskArtifactRef(w)).toEqual({ id: "ta-7", label: "設計稿" });
    // A deliverable pinned without a display name reads as "" — the mapper does
    // NOT invent one (it has no filename and no url to invent from). Deciding
    // what a nameless artifact LOOKS like is the renderer's job, on the full row
    // it fetched through listTaskArtifacts.
    expect(toTaskArtifactRef({ id: "ta-8", label: "" })).toEqual({ id: "ta-8", label: "" });
  });
});

describe("toTask / toTaskListItem artifact folding", () => {
  it("full task folds artifact INDEX rows and keeps count == length", () => {
    const view = toTask(
      wireTask({
        artifacts: [
          { id: "ta-1", label: "PR #1" },
          { id: "ta-2", label: "" },
        ],
      }),
    );
    expect(view.artifacts?.length).toBe(2);
    expect(view.artifactCount).toBe(2);
    // T-66: id + label, and NOTHING a renderer could draw a row from. The
    // wire type no longer declares the rest, so this asserts the VIEW model
    // stayed just as narrow — a mapper that widened it back (defaulting a
    // url/kind in) would put the fat shape back into the cockpit's hands and
    // let a component believe it need not fetch.
    expect(Object.keys(view.artifacts![0]).sort()).toEqual(["id", "label"]);
    expect(view.artifacts![0]).toEqual({ id: "ta-1", label: "PR #1" });
  });

  it("full task with no artifacts reports [] and count 0", () => {
    const view = toTask(wireTask({}));
    expect(view.artifacts).toEqual([]);
    expect(view.artifactCount).toBe(0);
  });

  it("light list item carries the server count with empty artifacts", () => {
    const view = toTaskListItem(wireListItem({ artifact_count: 3 }));
    expect(view.artifacts).toEqual([]);
    expect(view.artifactCount).toBe(3);
  });

  it("light list item with a 0 count keeps the badge hidden", () => {
    expect(toTaskListItem(wireListItem({ artifact_count: 0 })).artifactCount).toBe(0);
  });
});
