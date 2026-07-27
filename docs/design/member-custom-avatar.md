# Per-member custom avatar

Status: implemented design
Decision date: 2026-07-27
Owner gate: only the owner may upload, replace, or remove a member avatar.

## Product behavior

Every staff member and live outsource worker may have one personal image bound
to its stable member id. Display resolution is deliberately uniform across the
roster, chat, member details, monitor, reply cards, and task identity chips:

1. personal image;
2. the existing role/theme image;
3. the built-in glyph.

An image load failure continues down the same chain. Avatar images never draw a
presence dot; `PresenceBadge` / `LifecycleDot` remain the only presence signal.
The editor lives in the staff or outsource detail panel and remains usable at
the existing 390 px, 768 px, and desktop layouts.

## Contract and authorization

- `PUT /api/members/{member_id}/avatar` accepts a raw image body. Optional
  `filename` and `mime` query parameters preserve source metadata.
- `DELETE /api/members/{member_id}/avatar` is idempotent.
- Both routes require the owner principal and are excluded from MCP exposure.
- `MemberDTO.avatar_url` and `OutsourceWorkerDTO.avatar_url` are optional, so
  old records and clients preserve the existing fallback behavior.
- Uploads accept PNG, JPEG, or WebP up to 64 KiB. The server checks magic bytes
  and rejects an SVG, arbitrary data, or a declared MIME mismatch with `422`;
  an oversized body receives `413`.
- A successful mutation returns a narrow `MemberAvatarDTO`.

The browser-side checks exist for fast feedback, but server validation is the
security boundary. Route authorization, rather than a hidden UI control, is the
permission boundary.

## Persistence and lifecycle

`member.avatar_attachment_id` is the single ownership pointer. Image bytes
reuse `chat_attachment`, with a dedicated fresh `ava-...` id on every upload.
The new id also cache-busts browsers after replacement.

Replacement is one database transaction: insert the new blob, point the member
at it, delete the previous blob, then commit. Removal clears the pointer and
deletes the owned blob in one transaction. Hard member deletion and migration
rollback also remove the owned blob so no orphan remains. Display names are
never storage keys. The `ava-` namespace is single-owner: chat/reply/task
attachment refs and task artifacts reject it, because those records may outlive
an avatar replacement. General member upserts also leave the avatar pointer
untouched; only the dedicated avatar mutators may replace or clear it, so a
stale lifecycle snapshot cannot erase a newer avatar.

The route publishes `member` SSE after a staff mutation and
`outsource_worker` SSE after an outsource mutation. Existing consumers refetch
their lightweight DTOs; image bytes never enter SSE.

## Compatibility, rollback, and verification

The migration adds a non-null column with an empty default, making existing
rows resolve to the old theme/glyph fallback without a backfill or deployment
ordering constraint. Rollback deletes only blobs referenced by the avatar
column before dropping it.

Regression coverage pins upload, persistence, replacement cache busting, old
blob cleanup, idempotent removal, hard-delete cleanup, validation failures,
unsupported member kinds, owner-only/MCP-excluded routing, DTO compatibility,
frontend fallback behavior, and editor feedback. Generated OpenAPI clients are
regenerated from `spec/openapi.json`; generated files are not hand edited.
