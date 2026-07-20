// ReplyCardBody — the SHARED interior of a reply card (等我回覆卡), extracted
// from RepliesPage for B3 so the two render surfaces can never drift:
//
//   • RepliesPage (§2)   — wraps these in its own article shell (identity row,
//                          跳到原訊息, 已等你 {t}).
//   • ChatReplyCard (§3) — wraps the SAME bodies in the chat-thread card.
//
// Three bodies, one per card state:
//   ReplyCardWaitingBody  — the quick-reply chips (options[0] tagged AI 建議)
//                           + the typed ReplyComposer. Answering is the only
//                           POSITIVE way out (no close/skip control exists —
//                           spec; the owner-only 標為過期 lives in the card
//                           HEAD, outside this shared interior).
//   ReplyCardAnsweredBody — the final answer tagged 你選的 (+ AI 建議 when it
//                           IS the AI pick), 查看當初選項 expand, and 重新決定
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

import { useState } from "react";
import { useI18n } from "../i18n";
import type { ChatAttachmentView, ReplyCard, ReplyCardAnswerInput } from "../api/adapter";
import { AttachmentStrip } from "./AttachmentStrip";
import {
  MarkdownPreviewOverlay,
  isMarkdownAttachment,
} from "./MarkdownPreviewOverlay";
import { ReplyComposer } from "./ReplyComposer";
import { ChevronRightIcon, EyeIcon } from "./icons";

/** Shared by both attachment strips below (question-side and answer-side): a
 * per-item 預覽 action for `.md` files that opens the SAME in-cockpit overlay
 * ChatArea and the task artifacts popover use (T-a1c4 / T-90df) — one preview
 * surface, not a third copy. Non-markdown items get no extra action, so the
 * existing download-chip behaviour is untouched. */
function useMarkdownPreviewExtra() {
  const { t } = useI18n();
  const [preview, setPreview] = useState<{ title: string; url: string } | null>(
    null,
  );
  function renderExtra(att: ChatAttachmentView) {
    if (!isMarkdownAttachment(att.mime, att.filename)) return null;
    return (
      <button
        type="button"
        className="reply-card__preview-btn"
        aria-label={t.chat.mdPreview.action}
        title={t.chat.mdPreview.action}
        onClick={() =>
          setPreview({ title: att.filename || t.chat.downloadAttachment, url: att.url })
        }
      >
        <EyeIcon size={13} />
      </button>
    );
  }
  const overlay = preview ? (
    <MarkdownPreviewOverlay
      title={preview.title}
      url={preview.url}
      onClose={() => setPreview(null)}
    />
  ) : null;
  return { renderExtra, overlay };
}

/** The quick-reply option chips. `pickable: false` renders them as a static
 * review (當初選項 before 重新決定 re-arms them); `currentIdx` marks the
 * standing answer on an answered card. options[0] always carries the AI 建議
 * tag (spec: the first option IS the AI's own recommendation). */
export function ReplyOptionChips({
  card,
  pickable,
  currentIdx = null,
  onPick,
}: {
  card: ReplyCard;
  pickable: boolean;
  currentIdx?: number | null;
  onPick?: (idx: number) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="reply-card__options">
      {card.options.map((text, idx) => {
        const isCurrent = currentIdx === idx;
        return (
          <button
            key={idx}
            type="button"
            className={
              "reply-option" +
              (idx === 0 ? " reply-option--ai" : "") +
              (isCurrent ? " reply-option--current" : "") +
              (pickable ? "" : " reply-option--static")
            }
            disabled={!pickable}
            onClick={() => onPick?.(idx)}
          >
            <span className="reply-option__num">{idx + 1}</span>
            <span className="reply-option__text">{text}</span>
            {isCurrent && (
              <span className="reply-tag reply-tag--current">
                {t.replies.currentTag}
              </span>
            )}
            {idx === 0 && (
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

/** The stored answer's attachments (served refs — token-authed like chat
 * blobs): images inline, files as download chips — the SHARED AttachmentStrip
 * under this card face's existing classes. Renders nothing when the answer
 * carries none. */
function ReplyAnswerAttachments({ card }: { card: ReplyCard }) {
  const { renderExtra, overlay } = useMarkdownPreviewExtra();
  return (
    <>
      <AttachmentStrip
        attachments={card.answer?.attachments ?? []}
        className="reply-card__answer-atts"
        itemClassName="reply-card__att"
        imageClassName="reply-card__answer-image"
        renderExtra={renderExtra}
      />
      {overlay}
    </>
  );
}

/** The QUESTION-side attachments the initiator opened the card with (T-5e8a):
 * the same strip the answer side renders, but images are CLICKABLE — the
 * shell wires `onOpenImage` into its own Lightbox (點開預覽 on every card
 * face). Renders nothing when the card carries none; shown on every status
 * (waiting / answered / expired) — the question's context never expires. */
export function ReplyCardQuestionAttachments({
  card,
  onOpenImage,
}: {
  card: ReplyCard;
  onOpenImage?: (src: string) => void;
}) {
  const { renderExtra, overlay } = useMarkdownPreviewExtra();
  return (
    <>
      <AttachmentStrip
        attachments={card.attachments}
        className="reply-card__answer-atts reply-card__question-atts"
        itemClassName="reply-card__att"
        imageClassName="reply-card__answer-image chat__msg-image--clickable"
        onOpenImage={onOpenImage}
        renderExtra={renderExtra}
      />
      {overlay}
    </>
  );
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
  return (
    <>
      <ReplyOptionChips
        card={card}
        pickable
        onPick={(i) => void onAnswer({ optionIdx: i }).catch(() => {})}
      />
      <ReplyComposer
        placeholder={t.replies.inputPlaceholder}
        onSend={(body, attachments) => onAnswer({ text: body, attachments })}
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
  const optionIdx = ans?.optionIdx ?? null;
  const isAiPick = optionIdx === 0;

  async function doReanswer(input: ReplyCardAnswerInput) {
    await onReanswer(input);
    setEditing(false);
  }

  function toggleExpanded() {
    setExpanded((v) => !v);
    // Collapsing also leaves edit mode (nothing changed — same as cancel).
    setEditing(false);
  }

  return (
    <>
      {/* The FINAL answer: 你選的 always; AI 建議 additionally when the
       * standing pick IS options[0] (spec: 若選的正是 AI 建議，另標). An
       * answer may carry an option AND typed text — both render. */}
      <div className="reply-card__answer" data-testid="final-answer">
        <span className="reply-tag reply-tag--pick">{t.replies.yourPick}</span>
        {isAiPick && (
          <span className="reply-tag reply-tag--ai">{t.replies.aiPick}</span>
        )}
        <span className="reply-card__answer-text">
          {optionIdx !== null && (
            <span className="reply-card__answer-option">
              {card.options[optionIdx]}
            </span>
          )}
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
            currentIdx={optionIdx}
            onPick={(i) => void doReanswer({ optionIdx: i }).catch(() => {})}
          />
          {editing ? (
            <>
              <ReplyComposer
                placeholder={t.replies.redecidePlaceholder}
                onSend={(body, attachments) =>
                  doReanswer({ text: body, attachments })
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
              onClick={() => setEditing(true)}
            >
              {t.replies.redecide}
            </button>
          )}
        </div>
      )}
    </>
  );
}
