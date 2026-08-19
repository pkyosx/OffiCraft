// dtoParity.ts — WHAT A ONE-ITEM REFETCH CANNOT SERVE (T-8115 follow-up).
//
// T-8115 made an SSE delta re-read the ONE entity it named (`GET /{id}`) instead
// of re-downloading the whole list. That is only sound where the single-item
// response is a SUPERSET of the list row for every field the screen renders.
// Two of the three endpoints it used are NOT:
//
//  - `GET /api/members/{id}` — FIXED at the source (T-8115 review, team-lead
//    approved 2026-08-01). It used to hand `newMemberDTO` a literal 0 for
//    `unread_count` while `GET /api/members` computed the real number, so
//    re-reading one member ZEROED the roster badge the delta was announcing — the
//    value could only ever go DOWN through that path. THESE TWO handlers — the
//    roster list and `GET /api/members/{id}` — now call the same
//    `unreadCountsForRequest` (`server/ocserverd/api_helpers.go`), so the per-item
//    path is faithful again. No schema change was involved: MemberDTO has always
//    declared the field. Pinned server-side by
//    `api_members_unread_parity_test.go` (single vs list, on the response body).
//    ⚠️ SCOPE — "both handlers", not "every endpoint that returns a MemberDTO".
//    Verified 2026-08-01: of the six `newMemberDTO` call sites, only those two
//    pass a computed count; `writeMemberDTO` (shared by ~15 handlers),
//    `api_members.go:462`, `:565` and `api_roles.go:222` still pass a literal 0.
//    No user-visible consequence today — the cockpit never feeds those responses
//    back into the roster — but do not read the sentence above as a promise that
//    unread_count is real everywhere.
//    ⚠️ NOR is it "one shared computation" repo-wide: four inline
//    ListChat→ListChatReads→UnreadCounts copies remain (`api_outsource.go` :136,
//    :199, :348 and `api_chat.go` :873). The helper unified the MEMBER pair only.
//  - `GET /api/tasks/{id}` — `dep_tasks` IS NOT ON THE WIRE AT ALL. The frozen
//    spec declares it on `TaskListItemDTO` only, never on `TaskDTO`
//    (`spec/openapi.json`; `toTask()` in `api/mappers.ts` therefore sets no
//    `depTasks`, while `toTaskListItem()` passes it through verbatim). Absence is
//    not `[]`: the card renders "nobody resolved this dep" vs "查無此任務"
//    DIFFERENTLY on purpose (`components/TaskCard.tsx`), so a per-item refetch
//    silently degrades every dep row on that card to a bare short id.
//  - `GET /api/outsource-workers/{id}` — SAFE, and the reason is worth copying:
//    the single-item handler calls the SAME `projectWorker` with the same real
//    `unread[worker.ID]` as the list handler
//    (`server/ocserverd/api_outsource.go`). Nothing is dropped.
//
// The remaining gap cannot be closed on the client at all: `dep_tasks` is a field
// the frozen wire does not carry, so closing it is an additive spec change and is
// waiting on the owner. Until then `useTasks` re-pulls its list — the request
// count is the same either way (one GET), only the payload is bigger.
//
// 🔴 THE POINT OF THIS FILE. The regression shipped green because the hook
// tests' hand-rolled fake `getMember` / `getTask` returned the LIST row
// unchanged — the fake was more generous than the real server, so the value
// assertions were measured against a wire that does not exist. `api/mock.ts` had
// it right all along (its `getTask` explicitly drops `depTasks`). So the gaps
// live here, ONE place, and `dtoParity.test.ts` pins this table against the mock
// adapter and against the generated wire types.
//
// 🔴 WHAT ACTUALLY HOLDS THE LINE TODAY — routing the fakes through
// `projectSingleItem` is NOT it any more, for either remaining consumer.
// Measured 2026-08-01: reverting BOTH fakes in `hooks/sseFanout.test.tsx` to a
// bare `return found` (a list row answering `GET /{id}` — the original defect
// shape) leaves all 14 tests GREEN. `member` because its gap list is now empty,
// so the projection is the identity; `task` because `useTasks` no longer calls
// `getTask` at all, so the fake has no consumer. That is NOT "unguarded" — the
// three guards that DO bite, each mutant-tested:
//   1. `server/ocserverd/api_members_unread_parity_test.go` — asserts the NUMBER
//      IN THE RESPONSE BODY, single-item vs list. Put the literal `0` back and it
//      goes red; hardcode the reader as the owner and the per-caller case goes red.
//   2. this table vs `api/mock.ts` (`dtoParity.test.ts`) — stop computing unread
//      in the mock's `getMember`, or let its `getTask` keep the dep join, → red.
//   3. the COMPILE-TIME pin in `dtoParity.test.ts` (`TaskDTO` has no `dep_tasks`).
//      The dep-join half rests on this ONE guard — do not treat it as decoration.
// ── /api/themes (T-83ef) — SAFE, and the asymmetry runs the OTHER WAY ────────
// This file exists because a per-item response can be POORER than the list row.
// Themes are the opposite by design and it is worth writing down so nobody
// "fixes" it: `GET /api/themes` returns id and name ONLY, while
// `GET /api/themes/{id}` returns the whole bundle. A per-item read is therefore
// a strict superset — the direction this file worries about cannot occur.
//
// The hazard is the mirror image, so state it plainly: a LIST ROW IS NOT A
// BUNDLE. Anything that needs colours, wording, avatars, logo, nav icons or
// backgrounds must fetch the one theme; treating a row as a bundle loses every
// one of them. `ThemeListItem` is a separate type with no such fields, so this
// is a compile error rather than a silent blank theme — that type separation IS
// the guard, and collapsing the two types into one optional-field type would
// remove it while looking tidier.
//
// Why the list is thin at all: a bundle carries its images embedded, so a list
// of bundles is the several-hundred-kilobyte payload that made GET /api/settings
// unusable and that T-83ef exists to remove (owner ruling 2026-08-18).
//
// ── PATCH /api/settings: THE MOCK MUST NOT SWEEP display_theme (T-83ef) ──────
// An omitted display_theme changes nothing — on BOTH sides. Neither the server
// nor the mock may reset a now-dangling active theme here.
//
// It used to be the other way: both swept, back when this endpoint also wrote
// the bundles and so could orphan the active theme itself. It cannot any more —
// DELETE /api/themes/{id} does the reset and says so in its receipt. The server
// dropped its sweep for a stated reason ("a second opinion about a fact that
// endpoint already settled, and the two would drift", api_settings.go); the
// mock kept sweeping, so the two that drifted were the server and the mock, and
// that comment described its own situation for a while with nobody noticing.
//
// 🔴 THIS ONE CANNOT BE PINNED BY A TEST, which is exactly why it is written
// here. A dangling display_theme is unreachable through the mock's own API: the
// PATCH refuses an unknown id (422) and deleteTheme resets the active one. The
// state arises in production from a real race — the existence check in
// PATCH /api/settings sits outside the lock it then writes under, so a DELETE
// landing in between leaves settings naming a theme with no row (see the note
// on displayThemeExists in api_themes.go). A mock that healed that state would
// be kinder than the server about a condition the owner can actually be sitting
// in, and no test would ever say so.
//
// Before adding any new per-item refetch, read the gap for that endpoint AND
// those three. The fake-side protection lapses silently whenever a gap empties
// or a consumer goes away; nothing announces it.
//
// ⚠️ NOT pinned by conformance. `unread_count` appears NOWHERE in `conformance/`
// (verified 2026-08-01: zero hits) — so the repo's own behaviour-contract layer,
// the one that runs against a real ocserverd, says nothing about this field in
// either its old or its fixed form. Guard 1 is a Go unit test, which is a
// different thing. Closing that is a follow-up nobody has taken.

/** The fields a single-item GET does NOT carry, per list-bearing endpoint. */
export const PER_ITEM_DTO_GAPS = {
  /** `GET /api/members/{id}`: same computation as the list — nothing dropped. */
  member: [] as string[],
  /** `GET /api/tasks/{id}`: dep_tasks is not a field of TaskDTO. */
  task: ["depTasks"],
  /** `GET /api/outsource-workers/{id}`: same projection as the list. */
  outsourceWorker: [] as string[],
} as const;

export type PerItemKind = keyof typeof PER_ITEM_DTO_GAPS;

/** True when a one-item refetch can stand in for a list re-pull on this
 * endpoint — i.e. nothing the list row carries is lost. */
export function perItemRefetchIsFaithful(kind: PerItemKind): boolean {
  return PER_ITEM_DTO_GAPS[kind].length === 0;
}

/**
 * Project a LIST row down to what the SINGLE-ITEM endpoint would really return.
 *
 * Test support, deliberately shipped next to the table it reads: a fake that
 * answers `GET /{id}` out of list data is exactly the mistake this file exists
 * to stop, so the fakes go through here instead of hand-copying the row. The
 * gapped fields are dropped the way the wire drops them — `depTasks` to
 * `undefined` (the field is absent from TaskDTO).
 *
 * ⚠️ It is currently a no-op for BOTH kinds the hook tests use (`member`'s gap
 * list is empty, and nothing calls `getTask` any more), so routing a fake
 * through it buys no protection today — see the header for the three guards
 * that do. Keep using it anyway: it is what makes a fake correct AGAIN the
 * moment a gap reappears, and hand-copying the row is how this went wrong once.
 *
 * The `unreadCount` branch below is dormant for the same reason — kept because
 * MemberDTO declares the field with `default: 0`, so 0 (not absence) stays the
 * honest stand-in if a member-shaped gap is ever added back.
 */
export function projectSingleItem<T extends Record<string, unknown>>(
  kind: PerItemKind,
  listRow: T
): T {
  const out = { ...listRow } as Record<string, unknown>;
  for (const field of PER_ITEM_DTO_GAPS[kind]) {
    // MemberDTO declares unread_count with `default: 0`, so the honest stand-in
    // for "not computed" is 0, not absence. Everything else in the table is a
    // field the wire does not carry at all.
    out[field] = field === "unreadCount" ? 0 : undefined;
  }
  return out as T;
}
