# Theme avatar pools and persistent member indices

Status: owner-approved on 2026-07-29 (`rc-e9da3501194f`)

## Contract

- `member.avatar_index` is a durable non-negative integer. Existing rows migrate
  to `0`.
- A new staff member or outsource worker receives a uniformly random index in
  `[0, activePoolLength)` when the active custom theme has a matching non-empty
  pool. Otherwise it receives `0`.
- A renderer resolves a non-empty pool with
  `pool[avatar_index % pool.length]`. An empty or absent pool resolves to the
  built-in glyph. Theme changes and pool edits never mutate member rows.
- `ThemeBundleDTO.avatarPools` is the canonical ordered
  `member|outsource -> image[]` map, with at most 12 images in each pool.
  `avatars.owner` and `avatars.assistant` remain single images.
- Legacy `avatars.member` and `avatars.outsource` strings are accepted on stored
  or imported bundles, moved to singleton `avatarPools` entries, removed from
  `avatars`, and never emitted again.
- Every pool item reuses the existing image boundary: PNG/JPEG/WEBP data URI,
  strict base64, matching magic bytes, decoded size at most 64 KiB and raw value
  length at most 96 KiB.

## API and events

- `MemberDTO.avatar_index` and `OutsourceWorkerDTO.avatar_index` are the read
  face.
- Owner-only `PATCH /api/members/{member_id}/avatar-index` accepts
  `{ "avatar_index": <non-negative integer> }` for an active staff or outsource
  identity. Wardens are rejected.
- The narrow response is `{member_id, avatar_index}`.
- Staff updates publish `member`; outsource updates publish
  `outsource_worker`. Both partial SSE payloads include `avatar_index`; clients
  continue to reconcile by refetch.
- The former personal-avatar PUT/DELETE route and `avatar_url` fields are
  removed. Server and bundled frontend deploy as one binary, so there is no
  mixed-version rolling window. An old standalone client receives 404 on the
  retired route instead of a false successful write.

## Persistence and rollback

Migration `00049`:

1. Add `avatar_index INTEGER NOT NULL DEFAULT 0 CHECK (avatar_index >= 0)`.
2. Delete every `chat_attachment` row referenced by a non-empty
   `member.avatar_attachment_id`.
3. Drop `member.avatar_attachment_id`.

Down migration:

1. Restore `avatar_attachment_id TEXT NOT NULL DEFAULT ''`.
2. Drop `avatar_index`.

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

## Deployment

The product ships the Go server and generated React application together in one
binary:

1. Update OpenAPI and SSE specs.
2. Regenerate Go and TypeScript wire types.
3. Apply migration, backend, mock, and frontend changes in the same commit.
4. Run migration up/down/up coverage plus the full authoritative CI.
5. Deploy the single binary. No dual-read or dual-write phase is needed.

Rollback to an older binary is schema-safe after the Down migration, but old
personal images remain gone. Exported new theme bundles are backward
incompatible with an older client that does not know `avatarPools`; legacy
singleton bundles remain forward-compatible through normalization.

## Test strategy

- Migration: existing staff/outsource rows become index `0`; referenced avatar
  blobs are deleted; Down restores a valid empty pointer; up/down/up succeeds.
- Creation: empty pool -> `0`; one-item pool -> `0`; multi-item pool always
  yields a value in range for staff and outsource.
- Update: staff/outsource success and correct SSE payload/topic; negative,
  missing, warden, removed, unknown and unauthorized cases.
- Bundle parity: key set, 12-item bound, per-item image validation, legacy
  normalization, canonical echo, import/export order.
- Rendering: normal, out-of-range modulo, empty pool, failed image and theme
  switch on every shared avatar surface.
- UI: add/replace/remove/reorder, 12-item refusal, save/cancel, index editor
  success/failure/focus, desktop/768px/390px, keyboard controls and accessible
  names.
