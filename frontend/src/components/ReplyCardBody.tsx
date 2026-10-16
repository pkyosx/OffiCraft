// ReplyCardBody — the SHARED interior of a reply card (等我回覆卡), extracted
// from RepliesPage for B3 so the two render surfaces can never drift:
//
//   • RepliesPage (§2)   — wraps these in its own article shell (identity row,
//                          跳到原訊息, 已等你 {t}).
//   • ChatReplyCard (§3) — wraps the SAME bodies in the chat-thread card.
//
// Three bodies, one per card state:
//   ReplyCardWaitingBody  — the quick-reply chips (each tagged AI 建議 iff it
//                           carries its OWN aiPick) + the typed ReplyComposer.
//                           A MULTI card stages: ticking accumulates and ONE
//                           send button submits the ticked options AND the
//                           typed text as a single answer — and it OPENS with
//                           the ai_pick options already ticked (owner
//                           2026-08-31), which the owner un-ticks or adds to.
//                           A SINGLE card does
//                           NOT stage — one click on a chip IS the answer and
//                           sends immediately (owner, card rc-06bc715358c2),
//                           carrying whatever is already typed alongside it.
//                           Answering is the only POSITIVE way out
//                           (no close/skip control exists —
//                           spec; 標為過期 (owner/admin agent, and since T-1b88
//                           the card's own author) lives in the card
//                           HEAD, outside this shared interior).
//   ReplyCardAnsweredBody — the final answer tagged 你選的 (+ AI 建議 when any
//                           circled option carries aiPick), 查看當初選項 expand, and 重新決定
//                           at the expansion's bottom (options re-arm + the
//                           same composer; cancel keeps the original answer).
//   ReplyCardExpiredBody  — the terminal 已過期 state (T-1aa4): a grey static
//                           note + the original options as a static review; no
//                           pick, no composer, no 重新決定 — an expiry is not
//                           an answer and never reopens.
//
// Error surfacing stays with the CALLER: onAnswer/onReanswer rejecting keeps
// the composer content (ReplyComposer's contract) / the standing answer; the
// caller shows its own error notice.

import { useRef, useState } from "react";
import type { ReactNode } from "react";
import { useI18n } from "../i18n";
import type {
  ChatAttachmentInput,
  ReplyCard,
  ReplyCardAnswerInput,
} from "../api/adapter";
import {
  AttachmentLinkCommentsContext,
  useAttachmentLinkComments,
} from "../hooks/useAttachmentOpener";
import type { DocCommentRecipient } from "../lib/docComments";
import { AttachmentStrip } from "./AttachmentStrip";
import { ReplyComposer } from "./ReplyComposer";
import { ChevronRightIcon } from "./icons";

/** Add or remove `idx` from a MULTI card's staged selection. The result is kept
 * SORTED ASCENDING so the staged set is a function of WHICH chips are ticked and
 * not of the order they were ticked in.
 *
 * Multi only, by construction: a single card does not stage — its click answers
 * — so there is no single branch here to go stale.
 *
 * Un-ticking must stay reachable: "nothing circled" is the state the send button
 * refuses to send. */
function toggleSelection(selected: number[], idx: number): number[] {
  if (selected.includes(idx)) return selected.filter((i) => i !== idx);
  return [...selected, idx].sort((a, b) => a - b);
}

/** What a WAITING card starts with ticked: on a MULTI card, every option
 * carrying `ai_pick` — owner 2026-08-31: 「多選的時候，UI 應該要預設就先把我勾好
 * AI 建議的」. Nothing is sent by this; a tick on a multi card only stages, and
 * the owner un-ticks or adds to it before pressing send.
 *
 * 🔴 SINGLE cards get an EMPTY set, unconditionally. A single card answers on
 * the click, so on that kind "pre-ticked" would have to mean "pre-sent" — the
 * ticked state is not something a single card can sit in at all. The `multi`
 * test below is the whole safety of that, which is why a test pins it.
 *
 * A multi card with no ai_pick anywhere starts empty, as it always did.
 *
 * Not used by 重新決定: edit mode seeds from the answer ALREADY STANDING, so
 * re-opening a decision never quietly rewrites it into the AI's. */
function initialSelection(card: ReplyCard): number[] {
  if (card.selectMode !== "multi") return [];
  return card.options.reduce<number[]>(
    (acc, opt, idx) => (opt.aiPick ? [...acc, idx] : acc),
    [],
  );
}

/** Build the answer body a card face submits: the ticked options AND the typed
 * text/attachments as ONE answer (the two used to be two separate POSTs, which
 * a multi-select answer cannot express). `optionIdxs` is OMITTED when nothing
 * is ticked — an empty list is a 400 server-side, deliberately, and sending it
 * would turn "I typed a reply and circled nothing" into an error. */
function answerInput(
  selected: number[],
  text: string,
  attachments: ChatAttachmentInput[],
): ReplyCardAnswerInput {
  return {
    ...(selected.length > 0 ? { optionIdxs: selected } : {}),
    text,
    attachments,
  };
}

/** The quick-reply option chips. `pickable: false` renders them as a static
 * review (當初選項 before 重新決定 re-arms them); `currentIdxs` marks the
 * STANDING answer on an answered card (the 「目前」 tag), `selectedIdxs` marks
 * what is STAGED but not yet sent.
 *
 * 🔴 The AI 建議 tag reads each option's OWN `aiPick`. It is NOT `idx === 0`:
 * position stopped carrying that meaning, so a card whose recommendation is the
 * third option tags the third option. */
export function ReplyOptionChips({
  card,
  pickable,
  selectedIdxs = [],
  currentIdxs = [],
  onToggle,
}: {
  card: ReplyCard;
  pickable: boolean;
  selectedIdxs?: number[];
  currentIdxs?: number[];
  onToggle?: (idx: number) => void;
}) {
  const { t } = useI18n();
  const multi = card.selectMode === "multi";
  return (
    <div className="reply-card__options">
      {card.options.map((opt, idx) => {
        const isCurrent = currentIdxs.includes(idx);
        const isSelected = selectedIdxs.includes(idx);
        // A pickable mark shows what is TICKED; a review mark shows what was
        // CIRCLED. Both are "this one", read at the right moment.
        const markOn = pickable ? isSelected : isCurrent;
        return (
          <button
            key={idx}
            type="button"
            className={
              "reply-option" +
              (opt.aiPick ? " reply-option--ai" : "") +
              (isSelected ? " reply-option--selected" : "") +
              (isCurrent ? " reply-option--current" : "") +
              (pickable ? "" : " reply-option--static")
            }
            data-testid="reply-option"
            data-option-idx={idx}
            data-selected={isSelected ? "true" : "false"}
            // MULTI chips are checkboxes: ticking accumulates and the state
            // persists on screen until the send, so `aria-checked` has
            // something true to report. NEVER `aria-pressed` alongside — a
            // toggle-button role and a checked role are two different promises
            // about one widget.
            //
            // SINGLE chips are plain buttons. They used to be `role="radio"`
            // inside a `radiogroup`, and that was a promise the card cannot
            // keep: a single card ANSWERS on the click, so there is no
            // "checked and not yet submitted" state for a radio to sit in —
            // `aria-checked` would have read "false" for the whole life of the
            // chip. A button that does something when pressed is what it is.
            role={pickable && multi ? "checkbox" : undefined}
            aria-checked={pickable && multi ? isSelected : undefined}
            disabled={!pickable}
            onClick={() => onToggle?.(idx)}
          >
            {multi ? (
              <span
                className={
                  "reply-option__mark reply-option__mark--check" +
                  (markOn ? " reply-option__mark--on" : "") +
                  (pickable ? "" : " reply-option__mark--static")
                }
                data-testid="reply-option-mark"
                aria-hidden="true"
              />
            ) : (
              // 1/2/3/4 — owner 2026-08-31: 「單選可以跟之前一樣顯示 1, 2, 3, 4
              // 就好嘛?」. The radio that briefly sat here was decoration: a
              // single card sends on the click, so its "selected" ring was a
              // state nobody could ever see it in.
              <span className="reply-option__num">{idx + 1}</span>
            )}
            <span className="reply-option__text">{opt.text}</span>
            {isCurrent && (
              <span className="reply-tag reply-tag--current">
                {t.replies.currentTag}
              </span>
            )}
            {opt.aiPick && (
              <span className="reply-tag reply-tag--ai">
                {t.replies.aiPick}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}

/** How many options are ticked, on a MULTI card only. On a single-select card
 * the chips already say it (exactly one can be lit), but on a multi card
 * "nothing ticked" and "everything ticked" differ only by which chips are lit —
 * so the count is written out. */
function ReplySelectionCount({
  card,
  selected,
}: {
  card: ReplyCard;
  selected: number[];
}) {
  const { msg } = useI18n();
  if (card.selectMode !== "multi") return null;
  return (
    <div className="reply-card__selcount" data-testid="reply-selected-count">
      {msg.replySelectedCount(selected.length)}
    </div>
  );
}

/** The stored answer's attachments (served refs — token-authed like chat
 * blobs): images inline, files as download chips — the SHARED AttachmentStrip
 * under this card face's existing classes. Renders nothing when the answer
 * carries none. */
function ReplyAnswerAttachments({ card }: { card: ReplyCard }) {
  const docComments = useAttachmentLinkComments();
  return (
    <AttachmentStrip
      attachments={card.answer?.attachments ?? []}
      docCommentsFor={docComments === null ? undefined : () => docComments}
      className="reply-card__answer-atts"
      imageClassName="reply-card__answer-image"
    />
  );
}

/** A document linked from the card, or attached to its question or answer, is
 * commented back to the card's author; read-only while the card is unread or
 * when `recipientFor` names nobody. */
export function ReplyCardLinkComments({
  from,
  recipientFor,
  children,
}: {
  from: string | undefined;
  recipientFor: ((id: string) => DocCommentRecipient | null) | undefined;
  children: ReactNode;
}) {
  const recipient = from === undefined ? null : (recipientFor?.(from) ?? null);
  return (
    <AttachmentLinkCommentsContext.Provider value={recipient === null ? null : { recipient }}>
      {children}
    </AttachmentLinkCommentsContext.Provider>
  );
}

/** The QUESTION-side attachments the initiator opened the card with (T-5e8a):
 * the same strip the answer side renders, but images are CLICKABLE — the strip
 * opens them in the shared preview overlay (點開預覽 on every card face).
 * Renders nothing when the card carries none; shown on every status
 * (waiting / answered / expired) — the question's context never expires. */
export function ReplyCardQuestionAttachments({
  card,
}: {
  card: ReplyCard;
}) {
  const docComments = useAttachmentLinkComments();
  return (
    <AttachmentStrip
      attachments={card.attachments}
      docCommentsFor={docComments === null ? undefined : () => docComments}
      className="reply-card__answer-atts reply-card__question-atts"
      imageClassName="reply-card__answer-image chat__msg-image--clickable"
    />
  );
}

/** §3.6 請示 → 任務: the 精簡任務資訊 row a TASK-derived ask wears — the task's
 * own TITLE and the 查看任務詳情 jump. A pure chat ask (task absent/null)
 * renders NOTHING: an empty slot reads as "this ask has no task", which is a
 * different claim from "it has one and we would not say which".
 *
 * The TYPE chip (label +「tm-05f7c776d6ff」-shaped typeKey) is GONE — owner
 * 2026-08-14, T-ee17 acceptance: an internal code on screen answers nothing the
 * title does not already answer better. `task.typeKey` stays on the wire; it is
 * simply no longer shown here.
 *
 * STILL never the task number/識別鍵 — that adjudication (請示卡不露任務編號)
 * is untouched: a title is not a number, and this change does not reopen it.
 *
 * ONE HOME for both surfaces (RepliesPage 的 Ask 頁 and the inline
 * ChatReplyCard): the row was copied into both files, so the title would have
 * had to be written twice — and the two copies would then drift apart the way
 * only duplicated facts do. The jump is a callback so this stays presentational
 * (each surface owns its own router). */
export function ReplyCardTaskRef({
  task,
  onJump,
}: {
  task: NonNullable<ReplyCard["task"]>;
  onJump: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="reply-card__task" data-testid="reply-task-ref">
      {task.title && (
        <span className="reply-card__task-title" title={task.title}>
          {task.title}
        </span>
      )}
      <button
        type="button"
        className="reply-card__task-jump"
        data-testid="reply-task-jump"
        onClick={onJump}
      >
        {t.replies.viewTask}
        <ChevronRightIcon size={12} />
      </button>
    </div>
  );
}

/** The chip-click behaviour BOTH pickable faces share, which is not the same
 * behaviour on the two card kinds:
 *
 *   • multi  — a click STAGES. Nothing leaves the browser until the send.
 *   • single — a click IS the answer and fires it on the spot (owner,
 *     rc-06bc715358c2: 「單選卡點一下就送出，多選卡才要按送出」).
 *
 * The single click sends through the COMPOSER (`sendNowRef`) rather than
 * calling the answer callback directly, so the text the owner has already
 * typed rides along instead of being thrown away — 「點了就送」 must not be
 * paid for with the words he typed.
 *
 * The clicked index travels in a ref, not in `selected`: a state write is not
 * readable until the next render, which is one turn too late for the send this
 * very click fires. `takeClicked()` reads it once and disarms it, so the next
 * send (a typed answer, say) falls back to the staged set. */
function useChipPick(
  card: ReplyCard,
  setSelected: React.Dispatch<React.SetStateAction<number[]>>,
) {
  const sendNowRef = useRef<(() => void) | null>(null);
  const clicked = useRef<number[] | null>(null);
  return {
    sendNowRef,
    takeClicked: () => {
      const v = clicked.current;
      clicked.current = null;
      return v;
    },
    pick: (i: number) => {
      if (card.selectMode === "multi") {
        setSelected((prev) => toggleSelection(prev, i));
        return;
      }
      clicked.current = [i];
      sendNowRef.current?.();
    },
  };
}

/** A WAITING card's interior: pickable chips + the typed composer. `onAnswer`
 * rejecting must be surfaced by the caller (the chips can simply be clicked
 * again; the composer keeps its content). */
export function ReplyCardWaitingBody({
  card,
  onAnswer,
}: {
  card: ReplyCard;
  onAnswer: (input: ReplyCardAnswerInput) => Promise<void>;
}) {
  const { t } = useI18n();
  const [selected, setSelected] = useState<number[]>(() =>
    initialSelection(card),
  );
  const { pick, sendNowRef, takeClicked } = useChipPick(card, setSelected);
  return (
    <>
      <ReplyOptionChips
        card={card}
        pickable
        selectedIdxs={selected}
        onToggle={pick}
      />
      <ReplySelectionCount card={card} selected={selected} />
      <ReplyComposer
        placeholder={t.replies.inputPlaceholder}
        hasSelection={selected.length > 0}
        sendNowRef={sendNowRef}
        onSend={async (body, attachments) => {
          await onAnswer(answerInput(takeClicked() ?? selected, body, attachments));
          setSelected([]);
        }}
      />
    </>
  );
}

/** An EXPIRED card's interior (T-1aa4): a grey terminal note + the original
 * options as a static, unpickable review. No composer, no 重新決定 — the
 * expiry is final; the agent reopens a fresh card if the question still
 * matters. */
export function ReplyCardExpiredBody({ card }: { card: ReplyCard }) {
  const { t } = useI18n();
  return (
    <>
      <div className="reply-card__answer" data-testid="expired-note">
        <span className="reply-tag reply-tag--expired">
          {t.replies.expiredTag}
        </span>
        <span className="reply-card__expired-note">
          {t.replies.expiredNote}
        </span>
      </div>
      <ReplyOptionChips card={card} pickable={false} />
    </>
  );
}

/** An ANSWERED card's interior: the final answer row (你選的 / AI 建議), its
 * attachments, the 查看當初選項 expansion and the 重新決定 edit mode. The
 * expand/edit state lives HERE (per card instance); a successful `onReanswer`
 * exits edit mode, a rejection stays in it (the caller surfaces the error). */
export function ReplyCardAnsweredBody({
  card,
  onReanswer,
}: {
  card: ReplyCard;
  onReanswer: (input: ReplyCardAnswerInput) => Promise<void>;
}) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(false);
  const [editing, setEditing] = useState(false);

  const ans = card.answer;
  const optionIdxs = ans?.optionIdxs ?? [];
  // AI 建議 on the final row is a property of the OPTIONS that were circled,
  // read off each one's own aiPick — never off index 0.
  const isAiPick = optionIdxs.some((i) => card.options[i]?.aiPick);
  // What 重新決定 has staged. Seeded from the standing answer when edit mode
  // opens, so "change one of my three picks" does not start from nothing.
  const [selected, setSelected] = useState<number[]>([]);
  const { pick, sendNowRef, takeClicked } = useChipPick(card, setSelected);

  async function doReanswer(input: ReplyCardAnswerInput) {
    await onReanswer(input);
    setEditing(false);
  }

  function startEditing() {
    // Seed edit mode from the standing answer so 「改掉三個裡的一個」 does not
    // start from nothing — MULTI only. A single card stages nothing (its click
    // sends), and a seeded set there would arm the send button to re-submit the
    // answer that is already standing.
    setSelected(card.selectMode === "multi" ? optionIdxs : []);
    setEditing(true);
  }

  function toggleExpanded() {
    setExpanded((v) => !v);
    // Collapsing also leaves edit mode (nothing changed — same as cancel).
    setEditing(false);
  }

  return (
    <>
      {/* The FINAL answer: 你選的 always; AI 建議 additionally when one of the
       * circled options carries aiPick (spec: 若選的正是 AI 建議，另標). An
       * answer may carry SEVERAL options AND typed text — all of them render;
       * showing only the first would silently drop the rest of the decision. */}
      <div className="reply-card__answer" data-testid="final-answer">
        <span className="reply-tag reply-tag--pick">{t.replies.yourPick}</span>
        {isAiPick && (
          <span className="reply-tag reply-tag--ai">{t.replies.aiPick}</span>
        )}
        <span className="reply-card__answer-text">
          {optionIdxs.map((i) => (
            <span
              className="reply-card__answer-option"
              data-testid="reply-answer-option"
              key={i}
            >
              {card.options[i]?.text ?? ""}
            </span>
          ))}
          {ans?.text && (
            <span className="reply-card__answer-free">{ans.text}</span>
          )}
        </span>
      </div>
      <ReplyAnswerAttachments card={card} />

      <button
        type="button"
        className="reply-card__toggle"
        aria-expanded={expanded}
        onClick={toggleExpanded}
      >
        <ChevronRightIcon
          size={13}
          className={`reply-card__caret${
            expanded ? " reply-card__caret--open" : ""
          }`}
        />
        <span>
          {expanded ? t.replies.collapseOptions : t.replies.viewOptions}
        </span>
      </button>

      {expanded && (
        <div className="reply-card__past">
          {editing && (
            <div className="reply-card__hint">{t.replies.redecideHint}</div>
          )}
          <ReplyOptionChips
            card={card}
            pickable={editing}
            selectedIdxs={editing ? selected : []}
            currentIdxs={optionIdxs}
            onToggle={pick}
          />
          {editing && <ReplySelectionCount card={card} selected={selected} />}
          {editing ? (
            <>
              <ReplyComposer
                placeholder={t.replies.redecidePlaceholder}
                hasSelection={selected.length > 0}
                sendNowRef={sendNowRef}
                onSend={(body, attachments) =>
                  doReanswer(
                    answerInput(takeClicked() ?? selected, body, attachments),
                  )
                }
              />
              <button
                type="button"
                className="reply-card__cancel"
                onClick={() => setEditing(false)}
              >
                {t.common.cancel}
              </button>
            </>
          ) : (
            // 重新決定 lives at the BOTTOM of the expanded 當初選項 block
            // (spec §3) — entering edit mode re-opens the options + composer;
            // cancel above keeps the original answer untouched.
            <button
              type="button"
              className="reply-card__redecide"
              onClick={startEditing}
            >
              {t.replies.redecide}
            </button>
          )}
        </div>
      )}
    </>
  );
}
