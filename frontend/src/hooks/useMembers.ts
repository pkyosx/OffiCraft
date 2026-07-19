// hooks/useMembers.ts — load the roster through the api client + keep it fresh.
//
// Reconcile-by-refetch (contract B): on a "member" SSE topic (the server's
// roster/presence delta — see service/repository.py _publish("member")) we
// REFETCH the roster rather than merging any event payload. In M1 the mock's
// subscribeEvents is a no-op, so refetch is driven by explicit action callbacks
// (activate/patch/refocus) — but the wiring is identical for the real backend.

import { useCallback, useEffect, useState } from "react";
import type { Member } from "../types";
import { api } from "../api";

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

export function useMembers(opts?: { light?: boolean }): UseMembers {
  const light = opts?.light ?? false;
  const topics = light ? ROSTER_TOPICS_LIGHT : ROSTER_TOPICS;
  const [members, setMembers] = useState<Member[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    const next = await api.listMembers(light ? { light: true } : undefined);
    setMembers(next);
  }, [light]);

  useEffect(() => {
    let alive = true;

    // Initial load. On rejection surface an honest error flag instead of
    // swallowing it into an empty roster. (Do NOT clearToken here — a 401 is
    // already handled at the http layer, which bounces to login.)
    api
      .listMembers(light ? { light: true } : undefined)
      .then((next) => {
        if (alive) {
          setMembers(next);
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

    // SSE: reconcile the roster by refetching on the relevant topics. The light
    // set omits chat/chat_read (T-cf91) so a chat line never re-pulls here.
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topics.has(topic)) {
        api
          .listMembers(light ? { light: true } : undefined)
          .then((next) => {
            if (alive) {
              setMembers(next);
              setError(false);
            }
          })
          .catch((e) => console.warn("useMembers: SSE refetch failed", e));
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [light, topics]);

  return { members, loading, error, refetch };
}
