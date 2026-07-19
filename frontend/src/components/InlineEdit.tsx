import { useRef, useState } from "react";
import { useI18n } from "../i18n";
import { CheckIcon, CloseIcon, PencilIcon } from "./icons";
import "./inline-edit.css";

interface InlineEditProps {
  /** Current committed value (always shown fresh in the display state). */
  value: string;
  /** Called only for a real, non-empty change (trimmed !== value). */
  onCommit: (next: string) => void;
  placeholder?: string;
  /** aria-label / tooltip for the pencil (edit) affordance + input. */
  ariaLabel?: string;
  /** Extra class on the wrapper. */
  className?: string;
  /** Class for the read-only value span (e.g. topbar__org / profile-dd__name). */
  displayClassName?: string;
}

/**
 * Reusable inline text editor: click the pencil to edit, then ✓ / Enter to
 * apply or ✗ / Escape to cancel.
 *
 * Race safety — "✗ always cancels, ✓ always applies, blur never swallows a
 * click": the ✓/✗ buttons use `onMouseDown` (which fires BEFORE the input's
 * blur) with `preventDefault()` to keep focus on the input, and the input has
 * NO onBlur handler at all — so a blur can never auto-commit and eat the
 * pending click. Clicking outside simply leaves the editor open (no data loss).
 */
export function InlineEdit({
  value,
  onCommit,
  placeholder,
  ariaLabel,
  className,
  displayClassName,
}: InlineEditProps) {
  const { t } = useI18n();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  // IME composition guard — an Enter that confirms a CJK candidate must NOT
  // commit the edit. Ref (not state) so the keydown handler reads the live value
  // with no stale-closure lag.
  const isComposingRef = useRef(false);

  function startEdit() {
    setDraft(value);
    setEditing(true);
  }

  function commit() {
    const next = draft.trim();
    // Empty or unchanged → treat as cancel (no onCommit, restore + close).
    if (!next || next === value) {
      cancel();
      return;
    }
    onCommit(next);
    setEditing(false);
  }

  function cancel() {
    setDraft(value);
    setEditing(false);
  }

  if (!editing) {
    return (
      <span className={`inline-edit${className ? ` ${className}` : ""}`}>
        <span className={displayClassName}>{value}</span>
        <button
          type="button"
          className="inline-edit__iconbtn"
          aria-label={ariaLabel}
          title={ariaLabel}
          onClick={startEdit}
        >
          <PencilIcon size={14} />
        </button>
      </span>
    );
  }

  return (
    <span
      className={`inline-edit inline-edit--editing${
        className ? ` ${className}` : ""
      }`}
    >
      <input
        className="inline-edit__input"
        value={draft}
        autoFocus
        placeholder={placeholder}
        aria-label={ariaLabel}
        onChange={(e) => setDraft(e.target.value)}
        onCompositionStart={() => {
          isComposingRef.current = true;
        }}
        onCompositionEnd={(e) => {
          isComposingRef.current = false;
          setDraft(e.currentTarget.value);
        }}
        onKeyDown={(e) => {
          // Composition gate FIRST: an Enter confirming an IME candidate must
          // never commit the edit (nor Escape-cancel mid-compose).
          if (
            e.nativeEvent.isComposing ||
            e.keyCode === 229 ||
            isComposingRef.current
          ) {
            return;
          }
          if (e.key === "Enter") commit();
          if (e.key === "Escape") cancel();
        }}
      />
      <button
        type="button"
        className="inline-edit__iconbtn inline-edit__iconbtn--apply"
        aria-label={t.common.apply}
        title={t.common.apply}
        onMouseDown={(e) => {
          e.preventDefault();
          commit();
        }}
      >
        <CheckIcon size={14} />
      </button>
      <button
        type="button"
        className="inline-edit__iconbtn inline-edit__iconbtn--cancel"
        aria-label={t.common.cancel}
        title={t.common.cancel}
        onMouseDown={(e) => {
          e.preventDefault();
          cancel();
        }}
      >
        <CloseIcon size={14} />
      </button>
    </span>
  );
}
