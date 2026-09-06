// SuggestedReplies — the one-click 建議回覆 row under a reply box (T-122).
//
// owner 2026-09-06: 「我想要可以在下面有幾個建議的回覆，像一般客服聊天訊息那樣」.
// Two boxes carry it and they are NOT the same component — the reply card's
// composer (ReplyComposer) and the task card's message box (the composer inside
// TaskCard) — so the row itself lives here and both mount it. One visual
// language, one behaviour, one place to change.
//
// 🔴 A CLICK FILLS THE BOX. IT DOES NOT SEND. Both boxes send outwards and a
// sent message cannot be recalled, so a mis-tap must cost a keystroke, never a
// message. The owner still presses send.
//
// 🔴 A CLICK NEVER DESTROYS WHAT IS ALREADY TYPED. `appendSuggestion` puts the
// sentence on its own line after the existing draft instead of replacing it —
// the same instinct as the chat composer's seed, which refuses to overwrite a
// draft in progress (ChatArea `draftSeed`), but resolved the other way round:
// the seed is automatic and may decline to act, this is a click and must always
// do something visible.
//
// Empty list ⇒ this renders NOTHING (not an empty row, not a heading). That is
// the state every install is in until the owner configures some, and the reply
// box has to look untouched in it.

import "./suggested-replies.css";

/** Fold a suggestion into a draft. Empty draft ⇒ the suggestion alone; a draft
 * already in progress ⇒ the suggestion on a new line after it. Exported for
 * the guards, which assert on the composition rather than on a rendered box. */
export function appendSuggestion(draft: string, suggestion: string): string {
  return draft.length === 0 ? suggestion : `${draft}\n${suggestion}`;
}

export function SuggestedReplies({
  replies,
  onPick,
  testId,
}: {
  /** The owner's configured sentences (hooks/useSuggestedReplies).
   *
   * 🔴 THE TYPE IS NOT THE GUARD. This is a SHARED component with callers this
   * module does not own, and the value ultimately comes off a settings payload
   * the frontend cannot verify. Anything that is not a list of sentences is
   * read as "the owner configured none" below — see the render. */
  replies: readonly string[];
  /** Hand the picked sentence to the box. The CALLER folds it into its own
   * draft and focuses its textarea — this component owns no draft. */
  onPick: (suggestion: string) => void;
  /** Distinguishes the two boxes on a page that shows both. */
  testId: string;
}) {
  // 🔴 EVERY BAD SHAPE ANSWERS "NO SUGGESTIONS", WHICH RENDERS NOTHING.
  //
  // `replies.length` on undefined/null, and `replies.map` on a string, both
  // THROW DURING RENDER — and a render throw unmounts the whole subtree, which
  // here is the reply box with the owner's half-typed draft in it. That is the
  // exact damage commit 7d4e5874 fixed on the CALL side (a settings read that
  // threw); this is the same damage arriving through the RETURN VALUE instead.
  // The hook guards its own read too; this guard is for every other caller,
  // because a shared component cannot assume its callers were careful.
  //
  // Drawing nothing is not a degraded mode here — it is exactly the state an
  // install with no configured sentences is in, and the owner ruled that state
  // legal.
  const list = Array.isArray(replies)
    ? replies.filter((v): v is string => typeof v === "string")
    : [];
  if (list.length === 0) return null;
  return (
    <div className="suggested-replies" data-testid={testId}>
      {list.map((text, i) => (
        <button
          key={`${i}-${text}`}
          type="button"
          className="suggested-replies__chip"
          title={text}
          onClick={() => onPick(text)}
        >
          {text}
        </button>
      ))}
    </div>
  );
}
