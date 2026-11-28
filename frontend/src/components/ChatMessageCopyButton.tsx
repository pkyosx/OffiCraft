import { useCallback, useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { copyPendingText, copyText } from "../lib/clipboard";
import { unlinkMentions } from "../lib/mentionLinks";
import { AlertTriangleIcon, CheckIcon, CopyIcon } from "./icons";
import "./office.css";
import "./card-head.css";

const FEEDBACK_MS = 1600;

export type CopyState = "idle" | "copied" | "failed";

/** What lands on the clipboard: the text itself, or a read that yields it. */
export type CopySource = string | (() => Promise<string>);

/** A reply card's question as copied: the summary, a blank line, the body,
 * read when asked for (a row only carries the summary). */
export function replyCardQuestionText(cardId: string, fallbackSummary: string) {
  return api.getReplyCard(cardId).then((card) =>
    [card.summary || fallbackSummary, card.body]
      .filter((part) => part)
      .map((part) => unlinkMentions(part))
      .join("\n\n"),
  );
}

/** The thread's copy result. One at a time: a second copy replaces the first
 * message's result. */
export function useMessageCopy() {
  const [result, setResult] = useState<{ id: string; state: CopyState } | null>(
    null,
  );
  const timer = useRef<number | null>(null);
  useEffect(
    () => () => {
      if (timer.current != null) window.clearTimeout(timer.current);
    },
    [],
  );

  const copy = useCallback(async (id: string, source: CopySource) => {
    const ok = await (typeof source === "string"
      ? copyText(source)
      : copyPendingText(source()));
    setResult({ id, state: ok ? "copied" : "failed" });
    if (timer.current != null) window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setResult(null), FEEDBACK_MS);
  }, []);

  const clear = useCallback(() => {
    if (timer.current != null) window.clearTimeout(timer.current);
    setResult(null);
  }, []);

  const stateOf = (id: string): CopyState =>
    result?.id === id ? result.state : "idle";

  return { copy, clear, stateOf };
}

export function useCopyLabel(state: CopyState): string {
  const { t } = useI18n();
  return state === "copied"
    ? t.chat.messageCopied
    : state === "failed"
      ? t.chat.messageCopyFailed
      : t.chat.copyMessage;
}

export function ChatMessageCopyButton({
  state,
  onCopy,
  idleLabel,
  onCard = false,
}: {
  state: CopyState;
  onCopy: () => void;
  /** Names what this button copies, in place of 複製訊息. */
  idleLabel?: string;
  /** On a card's first line it is one of the card's icon buttons. */
  onCard?: boolean;
}) {
  const copyLabel = useCopyLabel(state);
  const label = state === "idle" && idleLabel ? idleLabel : copyLabel;
  const iconSize = onCard ? 14 : 13;
  return (
    <button
      type="button"
      className={onCard ? "card-head__icon-btn" : "chat__msg-copy"}
      aria-label={label}
      title={label}
      onClick={onCopy}
    >
      {state === "copied" ? (
        <CheckIcon size={iconSize} />
      ) : state === "failed" ? (
        <AlertTriangleIcon size={iconSize} />
      ) : (
        <CopyIcon size={iconSize} />
      )}
    </button>
  );
}

/** The result. Always mounted and only its contents change: a live region
 * that arrives already holding its words is not announced by most screen
 * readers. A success is already the copy button's check, so its words here are
 * for a screen reader only; a failure is shown. */
export function ChatMessageCopyStatus({ state }: { state: CopyState }) {
  const label = useCopyLabel(state);
  return (
    <span
      className={`chat__msg-copy-status${state === "failed" ? " chat__msg-copy-status--failed" : ""}`}
      role="status"
    >
      {state === "copied" ? <span className="chat__sr-text">{label}</span> : state === "failed" ? label : ""}
    </span>
  );
}

export function CopiedMark({ label, size }: { label: string; size: number }) {
  return (
    <>
      <CheckIcon size={size} aria-hidden="true" />
      <span className="chat__sr-text">{label}</span>
    </>
  );
}
