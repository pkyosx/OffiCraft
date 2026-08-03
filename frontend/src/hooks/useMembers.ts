// hooks/useMembers.ts — load the roster through the api client + keep it fresh.
//
// Reconcile-by-refetch (contract B): on a "member" SSE topic (the server's
// roster/presence delta — see service/repository.py _publish("member")) we
// REFETCH the roster rather than merging any event payload. In M1 the mock's
// subscribeEvents is a no-op, so refetch is driven by explicit action callbacks
// (activate/patch/refocus) — but the wiring is identical for the real backend.
//
// T-8115: a "chat" / "chat_read" delta that moves EXACTLY ONE card's unread badge
// re-reads that one member (`GET /api/members/{id}`) instead of the company, and
// re-reads nothing at all when it names a peer that is not on the roster — a chat
// line can neither add, remove nor rename a member. Naming TWO OR MORE held
// members re-pulls the list, because per-item costs one full ListChat() scan PER
// REQUEST server-side while the list costs exactly one however many were named —
// the crossover is derived, see the note at the call site. Still no payload
// merging: the delta only says WHICH card changed, the server says what it holds.
//
// 🔴 That per-item read is only sound because the single-member endpoint was FIXED
// to compute `unread_count` the same way the list does (review of the first
// T-8115 cut: it used to hand back a literal 0, so this very path drove the badge
// to 0 exactly when a new message should have raised it). If you are about to add
// a per-item refetch anywhere else, check `api/dtoParity.ts` FIRST — `useTasks`
// still cannot have one.

import { useCallback, useEffect, useRef, useState } from "react";
import type { Member } from "../types";
import { api } from "../api";
import { createDeltaSink, narrowToHeld } from "../lib/deltaSink";
import { burstMovesNoOwnerUnread } from "../lib/ownerUnread";

interface UseMembers {
  members: Member[];
  loading: boolean;
  /** True when the mount fetch REJECTED — so the UI can tell an honest empty
   * roster apart from a failed load (never render a failure as "members · 0").
   * A 401 is handled globally (api/http.ts → login bounce); this guards the
   * non-401 case (500 / network). */
  error: boolean;
  refetch: () => Promise<void>;
}

// Topics that mutate the roster/presence view → trigger a refetch. The server
// fans a single "member" topic for roster + presence deltas (NOT "members" /
// "presence" — those never arrive; matching them left the UI stale on wake).
// "chat" / "chat_read" also mutate the roster view since MemberDTO.unread_count
// (the M2-1 count badge) derives from the chat stream + read watermark: a new
// inbound message bumps a card's badge, an advancing watermark clears it.
// "role_def" rides along so a CUSTOM role rename (role_def delta) re-resolves
// every member card's role display name (single truth: the role doc's name).
const ROSTER_TOPICS = new Set(["member", "chat", "chat_read", "role_def"]);

// The LIGHT topic set (T-cf91): identity only (name + role), so chat / chat_read
// are DELIBERATELY excluded — a light roster carries no unread badge (the server
// returns unread_count honest-empty), and a chat line changes no name or role.
// A light consumer (請示卡頁) therefore never re-pulls the roster when anyone in
// the company speaks; only a genuine roster or role change refetches.
const ROSTER_TOPICS_LIGHT = new Set(["member", "role_def"]);

// The topics whose ONLY effect on this view is one card's unread badge
// (MemberDTO.unread_count). A chat line and an advancing read watermark cannot
// add, remove, rename or re-order a member — the roster is served ordered by
// name (dal.go) — so when such a delta NAMES a member we already hold, the
// honest refetch is that member, not the company. Any other topic ("member",
// "role_def") can change list membership or every row at once, and stays a full
// re-pull.
const BADGE_ONLY_TOPICS = new Set(["chat", "chat_read"]);

// The predicate ("could this delta move an unread number the OWNER sees?") is
// shared with useChatUnread and useOutsourceWorkers — one invariant, one home:
// `lib/ownerUnread.ts`. T-b17f tightened it from "owner at EITHER end" to
// `to === owner` / `reader === owner`; that file carries the derivation and the
// warning about the two topics' different field names.

export function useMembers(opts?: { light?: boolean }): UseMembers {
  const light = opts?.light ?? false;
  const topics = light ? ROSTER_TOPICS_LIGHT : ROSTER_TOPICS;
  const [members, setMembers] = useState<Member[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  // Which ids the roster currently holds, readable from an SSE callback (a
  // state read there would be a stale closure). Only membership is mirrored —
  // the VALUES always come from the server.
  const heldRef = useRef<Set<string>>(new Set());

  // Adopt a whole roster: state and the id mirror move together, so the mirror
  // can never disagree with what is rendered.
  const adopt = useCallback((next: Member[]) => {
    heldRef.current = new Set(next.map((m) => m.id));
    setMembers(next);
  }, []);

  const refetch = useCallback(async () => {
    const next = await api.listMembers(light ? { light: true } : undefined);
    adopt(next);
  }, [light, adopt]);

  useEffect(() => {
    let alive = true;

    const full = () =>
      api
        .listMembers(light ? { light: true } : undefined)
        .then((next) => {
          if (alive) {
            adopt(next);
            setError(false);
          }
        })
        .catch((e) => console.warn("useMembers: SSE refetch failed", e));

    // Replace the named card, IN PLACE. The server stays the source of every
    // value (one GET /api/members/{id}, unread_count included — see the header);
    // the position is kept because the roster's order is by name and these topics
    // cannot change a name. A rejection falls back to the full re-pull rather
    // than leaving one card stale behind a badge that already moved.
    // NOTE: takes an array but the caller only ever passes ONE id — see the
    // crossover note at the call site. The array shape is kept so the fan-out
    // stays visible if that rule is ever revisited with numbers behind it.
    const patchOne = (ids: string[]) =>
      Promise.all(ids.map((id) => api.getMember(id)))
        .then((fresh) => {
          if (!alive) return;
          setMembers((prev) =>
            prev.map((m) => fresh.find((f) => f.id === m.id) ?? m)
          );
          setError(false);
        })
        .catch((e) => {
          console.warn("useMembers: member refetch failed", e);
          return full();
        });

    // Initial load. On rejection surface an honest error flag instead of
    // swallowing it into an empty roster. (Do NOT clearToken here — a 401 is
    // already handled at the http layer, which bounces to login.)
    api
      .listMembers(light ? { light: true } : undefined)
      .then((next) => {
        if (alive) {
          adopt(next);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useMembers: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    // SSE: reconcile the roster on the relevant topics — ONE decision per burst
    // of deltas (a resync fans 13 topics at once, of which this hook listens to
    // four). The light set omits chat/chat_read (T-cf91) so a chat line never
    // re-pulls here at all.
    const unsubscribe = api.subscribeEvents(
      createDeltaSink((batch) => {
        const mine = [...batch.topics].filter((t) => topics.has(t));
        if (mine.length === 0) return;
        const badgeOnly = mine.every((t) => BADGE_ONLY_TOPICS.has(t));
        const touched = badgeOnly
          ? narrowToHeld(batch, (id) => heldRef.current.has(id))
          : null;
        if (touched === null) {
          void full();
          return;
        }
        // 🔴 NOTHING in this burst is addressed to the owner (chat) or read BY
        // the owner (chat_read) ⇒ it cannot move a single badge on this roster
        // ⇒ do not fetch AT ALL (not the list, not the item). See
        // `lib/ownerUnread.ts`. This is the agent↔agent case, which is ordinary
        // traffic here: before this line it cost one full `GET /api/members` per
        // message, for a screen that never changed.
        //
        // T-b17f tightened the predicate, so this now also swallows two shapes
        // that used to cost one wasted `GET /api/members/{id}` each:
        // `owner → member` (the recipient is the member, not the owner) and
        // `chat_read` with `peer === owner` (a member read OUR messages, which
        // advances THEIR watermark — watermarks are per-reader).
        //
        // Whole-burst, not per-delta: a mixed burst (one agent↔agent line AND
        // one line to the owner) still goes through the branches below with ALL
        // its ids, so a real change is never dropped. That can widen k and send
        // a mixed burst down the list path instead of the per-item one — always
        // CORRECT, occasionally not the cheapest, and the safe direction.
        if (burstMovesNoOwnerUnread(batch, mine)) return;
        // Named somebody, none of them ours: a chat line CANNOT add, remove or
        // rename a member (that is the "member" topic), so a conversation with
        // an outsource worker / a released peer changes nothing this roster
        // renders. Re-pulling the company for it was the old behaviour.
        //
        // 🔴 EXACTLY ONE named card takes the per-item path; TWO OR MORE re-pull
        // the list. The crossover is at 2 and is DERIVED, not a tunable knob
        // (measured 2026-08-01, server-side statement counts):
        //   k=1  — per-item 1 GET + 1 ListChat() full scan; list 1 + 1. A TIE on
        //          cost, and the per-item payload is smaller ⇒ per-item wins.
        //   k>=2 — per-item k GETs + k full scans (`unreadCountsForRequest` runs
        //          ListChat() once PER REQUEST); the list is ALWAYS 1 + 1 ⇒ the
        //          list wins, and the gap widens linearly with k.
        // So no k is worse than the pre-T-8115 behaviour: k=1 gains the smaller
        // payload, k>=2 costs exactly what it always did. Raising this threshold
        // re-introduces a k-times amplification of a full-table scan on a ticket
        // whose whole point was to stop the cockpit re-reading too much.
        //
        // 🔴 WHY THIS BRANCH IS STILL LIVE — and it is NOT what you might guess.
        //
        // With the skip above, a SINGLE agent↔agent delta never reaches here, so
        // it is tempting to conclude "every burst that gets here has the owner at
        // one end ⇒ k ≤ 1 ⇒ this branch is dead". **That is wrong, and it was
        // written here once.** It confuses ONE DELTA with ONE BURST:
        // `narrowToHeld` reads `batch.ids`, the UNION over the whole burst
        // (`lib/deltaSink.ts`), and a MIXED burst — one agent↔agent line plus one
        // line to the owner, landing in the same microtask — names three held
        // members. k = 3, right here, today.
        //
        // ⇒ **This branch is the hot path for mixed bursts, not a fail-safe.**
        // Its guard is the CONTROL test in `sseFanout.test.tsx` ("a burst that
        // mixes an agent↔agent line WITH a line to the owner still refetches");
        // deleting this branch turns that test red with `expected undefined to
        // be 1` — the roster simply never re-pulls, while that burst really did
        // carry a message to the owner. Mixed bursts are not a test artefact:
        // `deltaSink.ts` says in its own header that it DELIBERATELY coalesces
        // bursts of REAL deltas landing in the same tick.
        // (What has NOT been measured is how often the wire actually delivers
        // two chat frames into one microtask — so do not claim a frequency.)
        //
        // 🔴 THE TRAP THAT PRODUCED THAT WRONG CLAIM WILL COME BACK. It is the
        // same one `sseFanout.test.tsx` spends 35 lines on right above its k
        // test: **一陣 ≠ 一則 — a burst is not a delta.** We documented it and
        // then walked into it one commit later, in this very file. Anytime you
        // reason about k, first ask which of the two you are holding: the
        // per-delta predicate (`couldMoveAnOwnerBadge`, above) or the per-BURST
        // union (`touched`, here).
        //
        // Independently of all that, the cost argument above is why the
        // threshold is 2 and not a knob: k per-item reads cost k full
        // `ListChat()` scans server-side (`unreadCountsForRequest`), the list
        // costs 1 + 1 for any k.
        if (touched.length === 1) void patchOne(touched);
        else if (touched.length > 1) void full();
      })
    );

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [light, topics, adopt]);

  return { members, loading, error, refetch };
}
