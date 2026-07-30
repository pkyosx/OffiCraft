// lib/rosterOrder.ts — the 正職 roster's display order (T-ed38).
//
// Extracted from OfficePage as a PURE function on purpose: the old comparator
// was inlined in the component with zero test cover, and the rules below are
// exactly the kind that fail silently (a row sorts to the wrong place; nothing
// throws, nothing looks broken). A pure function is testable; an inline arrow
// inside a 600-line component is not.
//
// Four ordered layers plus a deterministic tie-break:
//
//   1. PINNED    — the owner's manual pins, in the stored array's order.
//   2. UNREAD    — anyone with a waiting message.
//   3. RECENCY   — most recently exchanged first.
//   4. ROLE      — the seed assistant role first (the PRE-EXISTING rule, kept
//                  byte-for-byte; see below).
//   5. NAME → ID — lower-cased name first, then the id, so the order is TOTAL.
//
// They are ordered by urgency of the owner's attention: an explicit pin beats
// the system's guess, "someone is waiting on me" beats "I spoke to them
// recently", and recency beats the static role order.

import type { Member } from "../types";

/** Build the pin lookup from the stored `pinned_member_ids` array.
 *
 * The array IS an ORDERED SET — its order is the pinned group's display order
 * (newest pin first, since a new pin is unshifted onto the front). So the
 * index, not any timestamp, is the sort key: the two architects' contracts
 * ("the array order is the group order" / "a new pin goes on top") meet here
 * and no separate pin-time needs storing.
 *
 * A duplicate id would collapse to its LAST index; the server rejects
 * duplicates with a 422, so this never fires against a real settings read.
 */
export function pinIndexOf(pinnedIds: readonly string[]): Map<string, number> {
  const index = new Map<string, number>();
  pinnedIds.forEach((id, i) => index.set(id, i));
  return index;
}

/**
 * The roster comparator, closed over one pin lookup.
 *
 * ⚠️ Layer 1 SHORT-CIRCUITS when BOTH members are pinned — that is a contract,
 * not an optimisation. The whole value of a manual pin is that its position is
 * predictable; letting unread or recency reshuffle WITHIN the pinned group
 * would hand the owner's just-placed order straight back to the automatic
 * rules. Pins are exempt from every layer below.
 *
 * ⚠️ Layer 4 is the PRE-EXISTING behaviour, carried over unchanged — including
 * the effect of `mappers.ts`'s `role_key || "assistant"` fallback (a member
 * with no role sorts as an assistant). Changing that would also move the avatar
 * decision in `lib/avatarKind.ts`, which reads the same field. Out of scope.
 *
 * ⚠️ Layer 5 exists because a STABLE sort is not enough. `Array#sort` has been
 * stable since ES2019, so equal keys keep their INPUT order — but the input
 * order is itself not guaranteed: `GET /api/members` orders by name with no
 * secondary key, so two same-named members may swap between queries. Leaning on
 * that SQL is leaning on something that was never promised to the frontend:
 * add paging, change the collation, or swap the index for speed, and the screen
 * order changes with NO test going red. Comparing the name HERE makes the
 * frontend's order self-contained — whatever order the server returns, the
 * result is the same — and the id underneath makes it total (ids are
 * server-minted and unique), so two same-named members never swap either.
 *
 * 🔴 Layer 5a uses `toLowerCase()` and plain `<` / `>`. NOT `localeCompare`,
 * and NOT `toLocaleLowerCase()`. `localeCompare` reads the runtime's ICU data,
 * so two browsers — or a browser and the Node that runs the tests — can order
 * the same non-ASCII names differently. That is the failure a deterministic
 * order test cannot catch: green in jsdom, a different order for the user.
 * `toLowerCase()` is locale-INDEPENDENT (the `toLocale…` twin is the
 * locale-aware one) and compares code units, identically everywhere. The price
 * is that non-ASCII names are not ordered the way a speaker of that language
 * would expect — accepted on purpose: determinism first, perfect collation
 * never. (`localeCompare` also has 0 hits in `frontend/src`; using it would be
 * introducing a new convention, not following one.)
 */
export function compareMembers(
  pinIndex: Map<string, number>,
): (a: Member, b: Member) => number {
  return (a, b) => {
    // 1 — pins. Both pinned → the stored order decides and NOTHING below runs.
    const pinA = pinIndex.get(a.id);
    const pinB = pinIndex.get(b.id);
    if (pinA !== undefined && pinB !== undefined) return pinA - pinB;
    if (pinA !== undefined) return -1;
    if (pinB !== undefined) return 1;

    // 2 — unread first. `?? 0` NOT `|| 0`: 0 is a legal value here, and the
    // two spellings must not be allowed to blur if the sentinel ever changes.
    const unreadRank = (m: Member) => ((m.unreadCount ?? 0) > 0 ? 0 : 1);
    const byUnread = unreadRank(a) - unreadRank(b);
    if (byUnread !== 0) return byUnread;

    // 3 — most recent exchange first. An absent field (an older server that
    // never sends it) reads as 0 for EVERYONE, which retires this layer
    // wholesale and falls back to the previous ordering. That is a safe
    // degradation, not a break. A genuine 0 ("never talked") sorts after
    // anyone who has — neither pinned to the bottom nor to the top.
    const activity = (m: Member) => m.lastActivityAt ?? 0;
    const byRecency = activity(b) - activity(a);
    if (byRecency !== 0) return byRecency;

    // 4 — the seed assistant role first (pre-existing).
    const roleRank = (m: Member) => (m.role === "assistant" ? 0 : 1);
    const byRole = roleRank(a) - roleRank(b);
    if (byRole !== 0) return byRole;

    // 5a — name, lower-cased, code-unit compared. See the warning above: this
    // must not become `localeCompare`.
    const nameA = a.name.toLowerCase();
    const nameB = b.name.toLowerCase();
    if (nameA < nameB) return -1;
    if (nameA > nameB) return 1;

    // 5b — same name: the id makes the order total.
    return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  };
}

/**
 * Split an ALREADY-SORTED roster into the pinned group and the rest.
 *
 * Layer 1 puts every pinned member ahead of every unpinned one, so the split is
 * a prefix — no second pass over the pin lookup for ordering, and the two
 * halves cannot disagree with the sort.
 *
 * Pin ids that match no live member (a dismissed member, a member from another
 * device) simply never appear: the intersection happens HERE, at render, and
 * nothing writes a cleaned-up list back. A pin outliving its member is normal,
 * not corruption to repair.
 */
export function splitPinned(
  sorted: readonly Member[],
  pinIndex: Map<string, number>,
): { pinned: Member[]; rest: Member[] } {
  let cut = 0;
  while (cut < sorted.length && pinIndex.has(sorted[cut].id)) cut++;
  return { pinned: sorted.slice(0, cut), rest: sorted.slice(cut) };
}
