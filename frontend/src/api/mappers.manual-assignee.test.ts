// 🔴 THIS FILE EXISTS BECAUSE A RENAME BROKE THIS MAPPER IN COMPLETE SILENCE.
//
// `toManualAssignee` is the seam where the WIRE spelling of a task manual's
// assignee becomes the VIEW spelling. The rename this file is named after
// retired the wire value `member` in favour of `staff`. kind-vocab-guard:legacy
// T-101 changed the object this function RETURNS and left the value it
// COMPARES AGAINST untouched:
//
//     if (a["kind"] === "member" ...) return { kind: "staff", ... }
//                       ^^^^^^^^                       ^^^^^
//                       wire, stale                    view, renamed
//
// The server had already started sending "staff", so the branch stopped
// matching, the function fell through to `null`, and every task manual with a
// staff assignee rendered as 未設定 ("unset") in the cockpit.
//
// NOTHING CAUGHT IT, and each miss has a different cause — this is the part
// worth reading before you delete a case below:
//
//   * `tsc --noEmit` could not: the generated schema types the wire field as a
//     bare `string` (spec/openapi.json declares no enum), so comparing it to a
//     retired literal is well-typed.
//   * The mock could not: `mock.ts` stores assignees already in the VIEW
//     shape, so the mock-backed tests never execute this function at all.
//   * No test could: this function had no direct test. That is the hole this
//     file fills.
//   * The failure could not announce itself: an unrecognised assignee shape is
//     a LEGAL input here (it maps to null = unset, deliberately, so a
//     malformed row can never fabricate an assignee). "Unrecognised" and
//     "genuinely unset" therefore render identically. There is no error path
//     to notice.
//
// So the test that matters is not "does a staff assignee map correctly" — it
// is "is the WIRE value observable from a test at all". Every case below feeds
// the wire shape the SERVER actually emits, never the view shape.

import { describe, it, expect } from "vitest";
import { toManualAssignee } from "./mappers";

/** The exact payload a live server emits for a staff-assigned manual — read
 * back from `GET /api/task-manuals/<key>`, not hand-shaped from the view. */
const wireStaff = { kind: "staff", member_id: "m-exec" };

describe("mappers · toManualAssignee reads the WIRE vocabulary", () => {
  it("maps the wire's staff assignee to the staff view (the T-101 regression)", () => {
    expect(toManualAssignee(wireStaff)).toEqual({
      kind: "staff",
      memberId: "m-exec",
    });
  });

  it("does NOT recognise the retired 'member' spelling", () => {
    // The rename is a rename, not an alias: accepting both would let a stale
    // writer keep working and hide the very drift T-101 exists to remove.
    // The retired spelling is the subject under test here, not a stale copy.
    const retiredWireKind = "member"; // kind-vocab-guard:legacy
    expect(
      toManualAssignee({ kind: retiredWireKind, member_id: "m-exec" }),
    ).toBeNull();
  });

  it("maps an outsource assignee, which the rename never touched", () => {
    // The control case. During T-101 the batch replace was bound to the
    // retired `member` token. kind-vocab-guard:legacy
    // The outsource branch was therefore left alone — pinning it here is what
    // tells a future reader that a red staff case is a rename bug and not a
    // broken mapper.
    expect(
      toManualAssignee({ kind: "outsource", model: "opus", effort: "high" }),
    ).toEqual({
      kind: "outsource",
      runtime: "claude",
      model: "opus",
      effort: "high",
      copies: 1,
      machine: "",
    });
  });

  it("maps unset, undefined and unrecognised shapes to null — all three", () => {
    // These three share ONE rendering (未設定), which is why the regression was
    // invisible. Pinning them together states that the shared output is
    // intended, so a reader who sees "unset" in the UI cannot conclude the
    // mapper agreed with the server.
    expect(toManualAssignee({})).toBeNull();
    expect(toManualAssignee(undefined)).toBeNull();
    expect(toManualAssignee({ kind: "zzz", member_id: "m-exec" })).toBeNull();
  });

  it("refuses a staff assignee whose member_id is missing or not a string", () => {
    expect(toManualAssignee({ kind: "staff" })).toBeNull();
    expect(toManualAssignee({ kind: "staff", member_id: 42 })).toBeNull();
  });
});
