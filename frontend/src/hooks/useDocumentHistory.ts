// hooks/useDocumentHistory.ts — the retained revisions of ONE editable
// long-form document (T-7d33).
//
// Mirrors useGlobalContext / useLessons: mount-fetch + reconcile-by-refetch on
// the document's OWN SSE topic (a restore republishes exactly that topic, so
// the list and the visible doc reconcile off the same signal). `restore` is
// deliberately NOT self-healing: it re-reads the list itself, and leaves
// refreshing the VISIBLE document to the caller that owns it — this hook does
// not know which doc hook is on screen.
//
// T-1170: this hook reads the DIRECTORY. What it used to hand back — every
// retained revision's full text, three documents downloaded so that at most one
// of them could be read — is now split in two: this list (identity, actor,
// time, tombstone flag, per-field sizes) and `useDocumentRevision` below, which
// fetches ONE named revision when the reader opens it.
//
// T-1f39 (owner 2026-07-31 「有點選的時候再打 API 就可以」): the history is no
// longer on screen by default, it is behind a button in the editor's own
// toolbar. `enabled` is what makes that literal — while it is false NOTHING is
// requested and NOTHING is subscribed, so merely opening an editor costs no
// history call. Flipping it to true runs exactly the mount path that was there
// before, which is why this is one hook with a switch rather than two.

import { useCallback, useEffect, useState } from "react";
import type { DocumentHistoryEntryView, DocumentKind } from "../types";
import { api } from "../api";

/** The SSE topic each document kind fans on a write. One home for the mapping,
 * so this list can never end up watching a topic the doc does not publish.
 *
 * NOTE role_definition → "role_def": that is the topic the doc's own writes
 * publish (api_roles.go), the one useRoles listens on, and — since the restore
 * path was fixed on this branch — the one a RESTORE publishes too
 * (publishDocumentHistoryRestore). It used to publish "role", which is outside
 * the closed 13-topic set (hub.go sseTopics) and was dropped at the publish
 * seam, so a restore fanned nothing at all; watching the document's own topic
 * was right then and is what keeps this list reconciling now. */
const TOPIC_OF: Record<DocumentKind, string> = {
  global_context: "global_context",
  role_definition: "role_def",
  lessons: "lessons",
  insight: "insight",
  // All three manual kinds fan the manual's own topic: the document they
  // version IS the manual, whichever slice of it a revision holds.
  task_manual: "task_manual",
  task_manual_sop: "task_manual",
  task_manual_learnings: "task_manual",
  // T-e271: a description belongs to a TASK, so its writes and its restores
  // both fan the `task` topic (publishDocumentHistoryRestore's own branch calls
  // publishTask) — the same topic useTasks reconciles on.
  task_description: "task",
  // T-2ebe: a title belongs to the same TASK, so it fans the same topic.
  task_title: "task",
  // T-791e: both boot-context blocks ride the EXISTING `global_context` topic
  // rather than a topic named after themselves. The SSE vocabulary is a CLOSED
  // set declared in spec/sse.md §3.1 (SSE_RESYNC_TOPICS, pinned against that
  // file by api/sseResyncTopics.test.ts), and the server drops anything outside
  // it at the publish seam — a `boot_sequence` topic would fan NOTHING and the
  // failure would be perfectly silent: the write lands, and every other open
  // surface simply never hears. These blocks are parts of the assembled boot
  // context, so the topic is honest as well as available; the cost is that a
  // write to one block makes the other two re-read, which is a wasted request,
  // not a wrong screen.
  system_interaction: "global_context",
  boot_sequence: "global_context",
  offboard: "global_context",
  // T-3201: the six lifecycle documents ride the same existing topic, for the
  // same reason — the SSE vocabulary is closed, so a topic named after one of
  // them would fan NOTHING and the failure would be perfectly silent.
  accelerated_stop: "global_context",
  task_closeout: "global_context",
  task_reassign_predecessor: "global_context",
  task_takeover_with_predecessor: "global_context",
  task_takeover_fresh: "global_context",
  task_unblocked: "global_context",
};

interface UseDocumentHistory {
  /** Retained revisions as DIRECTORY rows, newest first (server-ordered, at
   * most 3). T-1170: no `content` — the list is a picker, and the text of the
   * one revision the reader picks comes from `useDocumentRevision`. */
  versions: DocumentHistoryEntryView[];
  loading: boolean;
  /** True when the load REJECTED — an honest "could not load" is not the same
   * screen as "this doc has never been edited". */
  error: boolean;
  refetch: () => Promise<void>;
  /** Restore one revision over the live doc, then re-read the list. Rejects
   * ONLY when the restore itself failed, so the caller can surface it — a
   * failed re-read afterwards leaves `error` set and resolves. */
  restore: (id: number) => Promise<void>;
}

export function useDocumentHistory(
  kind: DocumentKind,
  key: string,
  options: {
    /** Load (and stay subscribed) only while true. Defaults to true, so a
     * caller that wants the old mount-fetch says nothing. */
    enabled?: boolean;
  } = {}
): UseDocumentHistory {
  const enabled = options.enabled ?? true;
  const [versions, setVersions] = useState<DocumentHistoryEntryView[]>([]);
  const [loading, setLoading] = useState(enabled);
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    setVersions(await api.listDocumentHistory(kind, key));
  }, [kind, key]);

  const restore = useCallback(
    async (id: number) => {
      await api.restoreDocumentHistory(kind, key, id);
      // The document has ALREADY been overwritten by the line above. Re-reading
      // the list is a separate, non-destructive step, so its failure is a
      // separate condition: folding it into this promise reported a restore
      // that SUCCEEDED as 還原失敗, left the doc showing pre-restore content,
      // and the natural retry restored a second time — burning one of the three
      // retained slots for nothing.
      try {
        setVersions(await api.listDocumentHistory(kind, key));
        setError(false);
      } catch (e) {
        console.warn("useDocumentHistory: refresh after restore failed", e);
        setError(true);
      }
    },
    [kind, key]
  );

  useEffect(() => {
    if (!enabled) return;
    let alive = true;
    setLoading(true);

    const load = (onFail: (e: unknown) => void) =>
      api
        .listDocumentHistory(kind, key)
        .then((next) => {
          if (alive) {
            setVersions(next);
            setError(false);
          }
        })
        .catch(onFail);

    load((e) => {
      console.warn("useDocumentHistory: initial load failed", e);
      if (alive) setError(true);
    }).finally(() => {
      if (alive) setLoading(false);
    });

    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic.includes(TOPIC_OF[kind])) {
        void load((e) =>
          console.warn("useDocumentHistory: SSE refetch failed", e)
        );
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [kind, key, enabled]);

  return { versions, loading, error, refetch, restore };
}

interface UseDocumentRevision {
  /** The named revision's field→value snapshot, or `undefined` when there is
   * nothing on screen, while it loads, or when the read failed. The reader
   * distinguishes those with `loading`/`error`: an undefined content is NEVER
   * rendered as "this version is empty", which is a different and false claim
   * to make next to a destructive 還原 button. */
  content?: Record<string, string>;
  loading: boolean;
  error: boolean;
}

/**
 * ONE named retained revision's content (T-1170).
 *
 * `id` is `null` while no revision is open — nothing is requested, which is the
 * same 「點了才打 API」 rule the list itself follows, one level deeper: opening
 * the list costs the directory, opening a row costs that row.
 *
 * Deliberately NOT cached across ids: stepping back to the list and into
 * another revision must show that revision, and a revision that was restored in
 * between is a different document under the same id nowhere — but the live doc
 * it is compared against HAS changed, so re-reading is also the cheap way to
 * stay honest. Three documents, at most one open, no cache to invalidate.
 */
export function useDocumentRevision(
  kind: DocumentKind,
  key: string,
  id: number | null
): UseDocumentRevision {
  const [content, setContent] = useState<Record<string, string> | undefined>(
    undefined
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);

  useEffect(() => {
    if (id === null) {
      setContent(undefined);
      setLoading(false);
      setError(false);
      return;
    }
    let alive = true;
    // Clear FIRST: without this, opening a second revision renders the first
    // one's text under the second one's timestamp — the stale-cache failure,
    // and on this surface it sits next to a button that overwrites the live
    // document with what the reader believes it is looking at.
    setContent(undefined);
    setLoading(true);
    setError(false);

    api
      .getDocumentRevision(kind, key, id)
      .then((revision) => {
        if (alive) setContent(revision.content);
      })
      .catch((e) => {
        console.warn("useDocumentRevision: load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    return () => {
      alive = false;
    };
  }, [kind, key, id]);

  return { content, loading, error };
}
