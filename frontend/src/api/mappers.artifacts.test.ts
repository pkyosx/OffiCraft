// mappers — task artifact projection (T-3dc5). The full task folds the artifact
// rows + a count == length; the light list carries only the count (artifacts
// []); an unknown kind falls back to "link" (the no-blob shape) rather than
// fabricating a file/image.

import { describe, it, expect } from "vitest";
import { toTask, toTaskListItem, toTaskArtifact } from "./mappers";
import type { WireTask, WireTaskListItem, WireTaskArtifact } from "./wire";

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
});

describe("toTask / toTaskListItem artifact folding", () => {
  it("full task folds artifacts and keeps count == length", () => {
    const view = toTask(
      wireTask({
        artifacts: [
          wireArtifact({ id: "ta-1", kind: "link", url: "https://x/1" }),
          wireArtifact({ id: "ta-2", kind: "file", attachment_id: "att-1", url: "/api/chat/attachment/att-1" }),
        ],
      }),
    );
    expect(view.artifacts?.length).toBe(2);
    expect(view.artifactCount).toBe(2);
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
