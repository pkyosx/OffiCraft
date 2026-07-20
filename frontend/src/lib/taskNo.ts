// Task display number ("T-XXXX") derived on the frontend — a MIRROR of
// `TaskNo` in server/ocserverd/domain.go (kyle ruling H3: display-only, the
// first four hex chars after the "t-" prefix; collisions are possible and it
// is never a lookup key).
//
// WHY a mirror exists at all: the dep row on a task card normally prints the
// server-supplied `dep.task_no`. When a dep cannot be resolved against the
// frontend's task list, the fallback branches had no `task_no` to print and
// fell back to the RAW id (`t-1d8292a2f8db`) — visibly longer than the short
// number on the card itself. Deriving the number here makes the fallback
// print the same `T-1d82` as every other surface.
//
// WHY the comment matters: this is a rule COPIED ACROSS the wire boundary.
// If someone changes TaskNo on the server, nothing notifies this file — this
// comment and the shared test cases (see taskNo.test.ts) are the only trail
// back to the original.
//
// This is a SYMPTOM-level fix. `task_no` is a pure projection of the id and
// needs no lookup, so the server has no real reason to omit it when a dep
// fails to resolve — returning it unconditionally is the actual cure. That
// touches the wire spec, so it is out of scope here; Kyle has recorded it and
// will handle it separately. When that lands, this helper can RETIRE — please
// delete it rather than treating it as permanent design to build on.

/**
 * Derive the display number ("T-XXXX") from a task id ("t-<hex12>").
 *
 * Mirrors the Go original's two easy-to-miss edges: the prefix is TRIMMED (an
 * id without "t-" is passed through whole, not blindly sliced by two), and a
 * short id is NOT truncated. Go needs an explicit `len(hex) > 4` guard before
 * `hex[:4]` because Go slicing panics past the end; JS `slice` clamps
 * instead, so the guard is inherent here — same behavior, one less branch.
 *
 * Equivalence is claimed for the ids the server actually mints ("t-" + hex),
 * not for arbitrary strings: Go's `hex[:4]` counts BYTES, this counts UTF-16
 * code units, so a non-ASCII id would diverge. Ids are hex, so the two never
 * meet that case — but "line for line", which an earlier draft of this
 * comment claimed, was stronger than what was checked.
 */
export function deriveTaskNo(taskId: string): string {
  const hex = taskId.startsWith("t-") ? taskId.slice(2) : taskId;
  return "T-" + hex.slice(0, 4);
}
