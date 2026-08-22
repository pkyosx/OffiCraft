# Theme avatar pools and per-theme member selections

Status: owner-approved on 2026-08-12. Supersedes the single `member.avatar_index`
design of 2026-07-29 (`rc-e9da3501194f`).

## Why the index model was replaced

The first design gave each member ONE durable integer and resolved it against
whatever pool the active theme happened to carry, with a modulo wrap. Three
user-visible faults follow from that, and none of them can be fixed inside the
model:

- **Silent face swaps on a theme switch.** Two themes rarely hold pools of the
  same length or order, so the same index lands on a different image. The member
  changes face, and nothing tells the owner it happened.
- **Collisions.** The modulo wrap maps several members onto one image when a
  pool is shorter than the roster's index range.
- **Rebinding on a pool edit.** Removing one image shifts every later position,
  so members that had nothing to do with that image get a new face.

The replacement stores an ASSOCIATION and a STABLE IMAGE IDENTITY. A position
can not be a durable selection key; an identity can.

## Contract

- The association is `member_theme_avatar(member_id, theme_id, icon_id)` with
  the primary key `(member_id, theme_id)`. A member holds at most one choice per
  theme, and the themes never overwrite each other.
- The table is SPARSE. A member with no row has made no explicit choice.
- **First entry to a theme renders that theme's FIRST matching-pool image and
  does NOT persist it** (owner decision). The row appears only when the owner
  picks an image. This is what keeps "never chose" distinguishable from "chose
  the first image".
- `ThemeIconDTO` is one pool item: a stable `id` plus the embedded `image`. The
  id is DERIVED from the image bytes — `"icn-" + sha256(dataURI)[:6]` as hex —
  so the same image keeps the same id across an export and an import, and a
  caller-supplied id is overwritten rather than trusted.
- `ThemeBundleDTO.avatarPools` is the canonical ordered `member|outsource ->
  ThemeIconDTO[]` map, with at most 12 items in each pool. `avatars.owner` and
  `avatars.assistant` remain single images.
- **Reorder is NOT supported in this version.** Order is presentation only. The
  UI offers add, replace, remove, clear and done, and nothing else. Selections
  point at ids, so a future reorder can not move anybody's face.
- Legacy inputs stay accepted: an `avatars.member` / `avatars.outsource`
  singleton becomes a one-item pool, and a pool written as a bare array of data
  URIs is lifted to `ThemeIconDTO` items. Both gain derived ids on the write
  path, so no data migration is needed.
- Every pool item reuses the existing image boundary: PNG/JPEG/WEBP data URI,
  strict base64, matching magic bytes, decoded size at most 64 KiB and raw value
  length at most 96 KiB.

### Fallbacks, stated once

The renderer resolves in this order, and every surface uses the same order:

1. the image whose `id` equals the member's recorded `icon_id` for the ACTIVE
   theme;
2. otherwise the FIRST image of the matching pool — this covers both "never
   chose" and "the chosen image was removed";
3. otherwise the built-in glyph, which is what an empty or absent pool renders;
4. an image that fails to LOAD falls back to the built-in glyph, and inside the
   chooser to a labelled placeholder rather than a browser broken-image box.

Step 2 never resolves to "whatever sits at that position now". That rule is the
whole point of the identity.

### Removal

- Removing an image from a pool deletes the association rows that named it. The
  affected members fall back to the pool's first image; nobody else moves.
- Deleting a theme deletes its association rows. No dangling selection can
  affect another theme, and a theme later created with the SAME id starts empty
  rather than resurrecting the old choices.
- Dismissing a member, or releasing an outsource worker, deletes that actor's
  rows in every theme.
- The prune runs on the theme WRITE and DELETE faces (`PUT` and `DELETE
  /api/themes/{theme_id}`), which are the only paths that edit a theme or a
  pool. It used to run on the settings write path; upstream moved themes out of
  settings into their own resource and table (T-83ef), and the prune moved with
  them. A prune left on the settings path would silently stop covering
  anything.

### Export is asset-only

Theme export carries the theme, its pool images and their stable icon ids. It
does NOT carry `member_theme_avatar` rows, and no station export is in scope.
A recipient who imports the theme starts with the first image for every member
until that local owner picks one. This is deliberate: a selection belongs to a
roster, and the recipient's roster is a different one.

## API and events

- `MemberDTO.avatar_icon_id` and `OutsourceWorkerDTO.avatar_icon_id` are the
  read face. Both are nullable, and `null` means "no choice recorded for the
  active theme".
- Themes live in the `custom_theme` table behind their own resource (T-83ef),
  so an icon id is resolved by reading that theme's stored bundle. The
  association names the theme by id, which is what lets the two stay separate.
- Owner-only `PUT /api/members/{member_id}/theme-avatar` accepts
  `{ "theme_id": ..., "icon_id": ... }` for an active staff or outsource
  identity. Wardens are rejected. An unknown theme, or an icon that is not in
  that theme's matching pool, is a 422: a selection the pool can not resolve is
  how a member silently gets another member's face.
- The narrow response is `{member_id, theme_id, icon_id}`.
- **The route is owner-only and MCP-excluded by owner ruling** (2026-07-27,
  carried forward). A member's face is how the owner tells the fleet apart, so
  an agent or a machine token must not change it. The rationale sits on the
  handler in `api_members.go`; `routes.go` and the T-6020 governance test both
  point at it.
- Staff writes publish `member`; outsource writes publish `outsource_worker`.
  The partial SSE payload does NOT carry the choice — it is per theme, so a
  payload field would have to name a theme — and clients reconcile by refetch.

### Wire-breaking change, stated plainly

This release breaks the wire in three ways, with no deprecation window:

1. `avatar_url` is REMOVED from the member and outsource DTOs.
2. `PUT` and `DELETE /api/members/{member_id}/avatar` are REMOVED.
3. `avatar_icon_id` is REQUIRED on those DTOs (nullable, but always present).

**Why no deprecation window:** the product ships the Go server and the built
React application as ONE binary. There is no mixed-version rolling window in
which an old client talks to a new server, so a deprecation period would carry
cost with no reader to protect. An old STANDALONE client gets a 404 on the
retired routes, which is a visible failure rather than a false successful write.

`avatar_icon_id` is required on the RESPONSE so a reader can tell "the server
does not know about this field" from "this member has not chosen". The request
body keeps both of its fields required for the same reason: an omitted
`theme_id` must not silently mean "the active one".

Exported new bundles are backward incompatible with an older client that does
not know `avatarPools`. Legacy singleton bundles stay forward compatible
through normalization.

## Persistence and rollback

Migration `00060`:

1. Create `member_theme_avatar(member_id, theme_id, icon_id)` with the primary
   key `(member_id, theme_id)`. Nothing is back-filled.
2. Delete every `chat_attachment` row referenced by a non-empty
   `member.avatar_attachment_id`.
3. Drop `member.avatar_attachment_id`.

Down migration:

1. Restore `avatar_attachment_id TEXT NOT NULL DEFAULT ''`.
2. Drop `member_theme_avatar`.

The deleted personal image bytes are intentionally not reconstructed on
rollback. The pointer is restored empty so the old schema is valid and never
references a missing blob. This is the owner-approved irreversible cleanup of
the rejected personal-avatar model.

### The only retreat is a backup restore

Down does not undo this migration in any useful sense. It restores the column
shape, not the bytes, so every member that had a personal avatar comes back
with an empty pointer. Whoever presses merge should know that:

- **Recovering the images requires restoring the database from a backup taken
  before the upgrade.** There is no in-place repair, and no partial one.
- The scheduled backup (`~/.officraft/server/data/backups/`) plus the
  pre-migration snapshot that `bin/migrate` takes are the two candidate
  restore points. Verify one exists before upgrading a station that has
  personal avatars.
- Nothing else is destroyed. The deletion is scoped to attachments whose id
  starts with `ava-`, which only the retired personal-avatar model ever wrote.

Measured on a copy of this station's production database (25 members, 319
attachments, schema at version 46): 5 `ava-` attachments totalling 155,095
bytes were deleted, 314 attachments were untouched, and no member row was
lost. The five affected members render from the active theme afterwards.

## Cockpit

The member row is COMPACT by default: it shows the current image and a
"choose image" control, and opens the pool in a chooser on request. It does not
lay the pool out inline. The owner ran the inline shape in a trial and a pool of
any size stretched every roster row until the member list was unreadable
(2026-08-12). The chooser keeps keyboard reach, a radio group with accessible
names, and an explicit empty-pool explanation instead of disappearing.

## Deployment

The product ships the Go server and generated React application together in one
binary:

1. Update the OpenAPI and SSE specs.
2. Regenerate the Go and TypeScript wire types.
3. Apply migration, backend, mock and frontend changes in the same commit.
4. Run migration up/down/up coverage plus the full authoritative CI.
5. Deploy the single binary. No dual-read or dual-write phase is needed.

Rollback to an older binary is schema-safe after the Down migration, but old
personal images remain gone.

## Test strategy

- Migration: the association starts empty and nothing is back-filled; a second
  row for the same `(member, theme)` violates the primary key while the same
  member can choose in another theme; referenced avatar blobs are deleted; Down
  restores a valid empty pointer and drops the table; up/down/up succeeds.
- Persistence: a choice in theme A survives a switch to theme B and back, and
  neither overwrites the other; first entry renders the first image and writes
  no row; an explicit pick of that same first image DOES write one.
- Removal: removing an image clears only its own selections and moves nobody
  else; deleting a theme clears only its own; a dismissed member and a released
  worker leave nothing behind.
- Refusals: unknown theme, an icon outside the matching pool, a blank field, a
  warden target, an unknown member, and every non-owner identity.
- Identity: the id is derived from the image bytes, differs between images, and
  a caller-supplied id is overwritten.
- Bundle parity: key set, 12-item bound, per-item image validation, legacy
  singleton and legacy bare-string normalization, canonical echo, import and
  export order.
- Rendering: the chosen id wherever it sits in the pool, a removed id, no
  recorded choice, an empty pool, a kind with no pool, and a failed image load.
- UI: the row stays compact until the pool is requested; add, replace, remove
  and clear; the 12-item refusal; save and cancel; chooser success, failure and
  focus return; desktop, 768px and 390px; keyboard controls and accessible
  names. **Reorder is not implemented and is not tested — it is deferred.**
