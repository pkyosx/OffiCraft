import { Fragment, useLayoutEffect, useRef, useState } from "react";
import {
  SUGGESTED_REPLIES_MAX_ENTRIES,
  SUGGESTED_REPLY_MAX_LEN,
} from "../api/suggestedReplies";
import { useI18n } from "../i18n";
import { TrashIcon } from "./icons";
import "./settings.css";

/** The rows of one editable 建議回覆 list. The caller frames it (a settings row,
 * the chat room's dialog) and saves it; each mount holds its own draft.
 *
 * The draft is `null` until the owner touches something, so a value arriving
 * from the server flows straight through; once touched, the draft owns the
 * rows until it commits.
 *
 * 🔴 THE EMPTY LIST IS A REAL VALUE, not "unset". Deleting the last row saves
 * `[]`, and that is what turns the chips off. */
export function SuggestedRepliesEditor({
  label,
  idPrefix,
  value,
  onCommit,
  onTouch,
  emptyNote,
  keepFocusOnRemove = false,
}: {
  label: string;
  idPrefix: string;
  value: readonly string[];
  /** Save this list wholesale. Called only when it differs from `value`. */
  onCommit: (next: string[]) => void;
  /** Called as soon as the owner types. */
  onTouch?: () => void;
  /** Shown when there are no rows at all. */
  emptyNote?: string;
  /** For a focus trap around this editor: removing the bottom row unmounts the
   * focused button and focus falls to <body>, where the trap's keydown no longer
   * hears Tab. Then the add button takes focus. Any other row keeps it, since the
   * next row slides into the same button. */
  keepFocusOnRemove?: boolean;
}) {
  const { t } = useI18n();
  const [draft, setDraft] = useState<string[] | null>(null);
  const rows = draft ?? [...value];
  // An Enter that CONFIRMS a CJK candidate must not be read as "this line is
  // finished, save it".
  const composingRef = useRef(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const addRef = useRef<HTMLButtonElement>(null);
  const removedRef = useRef(false);
  useLayoutEffect(() => {
    if (!removedRef.current) return;
    removedRef.current = false;
    if (!rootRef.current?.contains(document.activeElement)) addRef.current?.focus();
  });

  /** RUNES, not UTF-16 units — the same measure the server applies
   * (utf8.RuneCountInString), so a CJK sentence gets the full budget and the
   * number this page shows is the number the server counted. */
  const runeLen = (v: string) => [...v.trim()].length;
  const overCap = (v: string) => runeLen(v) > SUGGESTED_REPLY_MAX_LEN;

  function commit(next: string[]) {
    // 🔴 REFUSE, NEVER SHORTEN. owner (rc-76ab62ceb3ff): 「超過就直接拒絕存檔並
    // 告訴你為什麼,不會偷偷截斷」. That is also why the input below carries NO
    // `maxLength`: the browser enforces maxLength by CUTTING A PASTE with no
    // notice at all, and pasting is exactly how a saved reply gets into this
    // field. The over-long text stays on screen, in full, with the reason
    // printed under it, and nothing is sent.
    if (next.some(overCap)) {
      setDraft(next);
      return;
    }
    const cleaned = next.map((v) => v.trim()).filter((v) => v.length > 0);
    setDraft(null);
    if (
      cleaned.length === value.length &&
      cleaned.every((v, i) => v === value[i])
    ) {
      return;
    }
    onCommit(cleaned);
  }

  /** A structural edit (delete / reorder) is a decision, not a keystroke, so it
   * saves at once — there is no blur to wait for. */
  function apply(next: string[]) {
    setDraft(next);
    commit(next);
  }

  const full = rows.length >= SUGGESTED_REPLIES_MAX_ENTRIES;

  return (
    <div className="sugg-edit" ref={rootRef}>
      {rows.length === 0 && emptyNote && <div className="sugg-edit__note">{emptyNote}</div>}
      {rows.map((text, i) => (
        <Fragment key={`${idPrefix}-${i}`}>
        <div className="sugg-edit__row">
          <input
            id={`${idPrefix}-${i}`}
            className="param-input sugg-edit__input"
            type="text"
            // This field manages a LIST; the sentences already live in the
            // rows above it. The browser's own "you typed this before"
            // dropdown floats a stale copy over them (owner c-90914e605cdb).
            autoComplete="off"
            aria-invalid={overCap(text) || undefined}
            placeholder={t.settings.suggestedReplyPlaceholder}
            aria-label={`${label} ${i + 1}`}
            value={text}
            onChange={(e) => {
              onTouch?.();
              const next = [...rows];
              next[i] = e.target.value;
              setDraft(next);
            }}
            onBlur={(e) => {
              // Saving drops a blank row and the rest slide up under the pointer,
              // so a click already on its way to a lower row would land on the
              // next sentence: typing edits it, its trash button deletes it. The
              // blank row waits until focus leaves the list.
              const stayingInList = rootRef.current?.contains(e.relatedTarget);
              if (stayingInList && rows.some((v) => v.trim().length === 0)) return;
              commit(rows);
            }}
            onCompositionStart={() => {
              composingRef.current = true;
            }}
            onCompositionEnd={(e) => {
              composingRef.current = false;
              const next = [...rows];
              next[i] = e.currentTarget.value;
              setDraft(next);
            }}
            onKeyDown={(e) => {
              if (
                e.nativeEvent.isComposing ||
                e.keyCode === 229 ||
                composingRef.current
              ) {
                return;
              }
              if (e.key === "Enter") commit(rows);
            }}
          />
          <button
            type="button"
            className="sugg-edit__btn"
            title={t.settings.suggestedReplyMoveUp}
            aria-label={`${label} ${t.settings.suggestedReplyMoveUp} ${i + 1}`}
            disabled={i === 0}
            onClick={() => {
              const next = [...rows];
              [next[i - 1], next[i]] = [next[i], next[i - 1]];
              apply(next);
            }}
          >
            ↑
          </button>
          <button
            type="button"
            className="sugg-edit__btn"
            title={t.settings.suggestedReplyMoveDown}
            aria-label={`${label} ${t.settings.suggestedReplyMoveDown} ${i + 1}`}
            disabled={i === rows.length - 1}
            onClick={() => {
              const next = [...rows];
              [next[i], next[i + 1]] = [next[i + 1], next[i]];
              apply(next);
            }}
          >
            ↓
          </button>
          <button
            type="button"
            className="sugg-edit__btn sugg-edit__btn--remove"
            title={t.settings.suggestedReplyRemove}
            aria-label={`${label} ${t.settings.suggestedReplyRemove} ${i + 1}`}
            onClick={() => {
              removedRef.current = keepFocusOnRemove;
              apply(rows.filter((_, j) => j !== i));
            }}
          >
            <TrashIcon size={14} />
          </button>
        </div>
        {overCap(text) && (
          <div className="sugg-edit__error" role="alert">
            {t.settings.suggestedRepliesTooLong(
              runeLen(text),
              SUGGESTED_REPLY_MAX_LEN
            )}
          </div>
        )}
        </Fragment>
      ))}
      <button
        ref={addRef}
        type="button"
        className="sugg-edit__add"
        disabled={full}
        title={full ? t.settings.suggestedRepliesFull : undefined}
        onClick={() => {
          onTouch?.();
          setDraft([...rows, ""]);
        }}
      >
        {t.settings.suggestedReplyAdd}
      </button>
      {/* A disabled button says nothing about WHY. Twenty is the cap, and
          "the cap is reached" has to be readable, not inferred from a button
          that stopped responding. */}
      {full && (
        <div className="sugg-edit__note">{t.settings.suggestedRepliesFull}</div>
      )}
    </div>
  );
}
