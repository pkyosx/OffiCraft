// The composer half of @mentions, shared by every box that can send a mention:
// the chat composer, the task card's message box and the reply card's answer
// box. `useMentionRoster` turns the roster a page already holds into the @ list
// and into the resolver that draws a mention link as a person;
// `useMentionComposer` is the picker state and keys for one textarea.

import {
  createContext,
  useCallback,
  useContext,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type KeyboardEvent,
  type RefObject,
  type SyntheticEvent,
} from "react";
import { useI18n } from "../i18n";
import type { Member } from "../types";
import type { OutsourceWorkerView } from "../api/adapter";
import {
  activeMentionQuery,
  filterMentionCandidates,
  insertMention,
  mentionCandidates,
  mentionOptionId,
  type MentionCandidate,
} from "../lib/chatMention";
import { isImeComposingKey } from "../lib/composerKeys";
import { navigateHash, type HashRoute } from "../lib/hashRoute";
import type { MentionOpener } from "./useMentionOpener";

export interface MentionRoster {
  candidates: MentionCandidate[];
  opener: MentionOpener;
}

/** The roster of the page around a reply card, for the card's answer box and
 * its answer text. Null outside a provider: no @ list, and links show as their
 * written `@name`. */
export const MentionRosterContext = createContext<MentionRoster | null>(null);

export function useMentionRosterContext(): MentionRoster | null {
  return useContext(MentionRosterContext);
}

/** `chatId` keeps the opened detail over that chat; `backTo` sends the detail's
 * 返回 back to the page the mention was clicked on. */
export function useMentionRoster(
  members: Member[],
  workers: OutsourceWorkerView[],
  at: { chatId?: string; backTo?: HashRoute["backTo"] },
): MentionRoster {
  const { t, msg } = useI18n();
  const { chatId, backTo } = at;
  const candidates = useMemo(
    () =>
      mentionCandidates(
        members,
        workers,
        msg.outsourceLabel,
        (m) => (t.office.role as Record<string, string>)[m.role] ?? (m.roleName || ""),
      ),
    [members, workers, msg, t],
  );
  const opener = useCallback<MentionOpener>(
    (target) => {
      const worker =
        target.kind === "worker" ? workers.find((w) => w.id === target.id) : undefined;
      const name =
        target.kind === "member"
          ? members.find((m) => m.id === target.id)?.name
          : worker && msg.outsourceLabel(worker.codename);
      if (name === undefined) return null;
      return {
        name,
        open: () =>
          navigateHash({
            page: "office",
            ...(chatId ? { chatId } : {}),
            ...(target.kind === "member" ? { detailId: target.id } : { workerId: target.id }),
            ...(backTo ? { backTo } : {}),
          }),
      };
    },
    [members, workers, msg, chatId, backTo],
  );
  return useMemo(() => ({ candidates, opener }), [candidates, opener]);
}

export function useMentionComposer({
  inputRef,
  value,
  setValue,
  candidates,
  composingRef,
}: {
  inputRef: RefObject<HTMLTextAreaElement | null>;
  value: string;
  setValue: (text: string) => void;
  candidates: MentionCandidate[];
  composingRef: RefObject<boolean>;
}) {
  // Null until the textarea reports a caret, so a restored draft that happens
  // to end in "@x" does not pop the list open.
  const [caret, setCaret] = useState<number | null>(null);
  // The `@` index whose list was closed (Esc or a pick); it stays closed until
  // the caret is in a different `@` token.
  const [dismissedAt, setDismissedAt] = useState<number | null>(null);
  const [active, setActive] = useState(0);
  const pendingCaretRef = useRef<number | null>(null);
  const listId = useId();

  const query = caret === null ? null : activeMentionQuery(value, caret);
  const matches =
    query && query.start !== dismissedAt ? filterMentionCandidates(candidates, query.query) : [];
  const open = matches.length > 0;
  const index = Math.min(active, matches.length - 1);

  useLayoutEffect(() => {
    const next = pendingCaretRef.current;
    if (next !== null && inputRef.current) {
      pendingCaretRef.current = null;
      inputRef.current.setSelectionRange(next, next);
    }
  }, [value, inputRef]);

  function track(text: string, at: number) {
    setCaret(at);
    const q = activeMentionQuery(text, at);
    if (q === null || q.start !== dismissedAt) setDismissedAt(null);
  }

  function pick(c: MentionCandidate) {
    if (!query) return;
    const next = insertMention(value, query, c.name);
    pendingCaretRef.current = next.caret;
    setDismissedAt(query.start);
    setCaret(next.caret);
    setValue(next.text);
    inputRef.current?.focus();
  }

  /** True when the key belonged to the open list; the caller must then do
   * nothing else with it (no send, no newline). */
  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>): boolean {
    if (!open || isImeComposingKey(e, composingRef.current ?? false)) return false;
    const n = matches.length;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setActive((index + (e.key === "ArrowDown" ? 1 : -1) + n) % n);
      return true;
    }
    // Enter picks on phones too: while the list is open it is never a send
    // and never a newline.
    if (e.key === "Enter" || (e.key === "Tab" && !e.shiftKey)) {
      e.preventDefault();
      pick(matches[index]);
      return true;
    }
    if (e.key === "Escape") {
      // Without preventDefault the window-level Esc layer would also close
      // whatever panel hosts this composer.
      e.preventDefault();
      if (query) setDismissedAt(query.start);
      return true;
    }
    return false;
  }

  return {
    pick,
    onKeyDown,
    /** Call from the textarea's onChange, after storing the new value. */
    onChange(e: ChangeEvent<HTMLTextAreaElement>) {
      setActive(0);
      track(e.target.value, e.target.selectionStart ?? e.target.value.length);
    },
    inputProps: {
      onSelect(e: SyntheticEvent<HTMLTextAreaElement>) {
        const el = e.currentTarget;
        track(el.value, el.selectionStart ?? el.value.length);
      },
      "aria-autocomplete": "list" as const,
      "aria-controls": open ? listId : undefined,
      "aria-activedescendant": open ? mentionOptionId(listId, index) : undefined,
    },
    listProps: open ? { id: listId, items: matches, active: index, onPick: pick } : null,
  };
}
