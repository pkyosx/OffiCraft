// 稍後再看 on a waiting card: four presets, then a custom span with a live
// preview of when the card comes back. A popover beside the button on a
// desktop, a bottom sheet on a phone. The menu is portaled, so its clicks still
// bubble to the card through the React tree; `role="dialog"` is what the card's
// toggle filter skips.

import { useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import { useIsMobile } from "../hooks/useIsMobile";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import {
  SNOOZE_PRESETS,
  type SnoozeUnit,
  snoozeDraft,
  snoozeUntil,
} from "../lib/replySnooze";
import "./replies.css";

const EDGE = 8;

/** What a failed snooze means to the menu: `stale` closes it (the page says
 * why and refreshes), anything else keeps it open with an error line. */
export type SnoozeOutcome = "ok" | "stale" | "failed";

export function ReplySnoozeMenu({
  cardId,
  onSnooze,
}: {
  cardId: string;
  onSnooze: (untilTs: number) => Promise<SnoozeOutcome>;
}) {
  const { t, msg } = useI18n();
  const mobile = useIsMobile();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [amount, setAmount] = useState("");
  const [unit, setUnit] = useState<SnoozeUnit>("hour");
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const titleId = useId();

  function close(refocus: boolean) {
    setOpen(false);
    setFailed(false);
    setPos(null);
    if (refocus) triggerRef.current?.focus();
  }

  useEscapeLayer(() => close(true), menuRef, open);

  useLayoutEffect(() => {
    if (!open || mobile) return;
    const anchor = triggerRef.current?.getBoundingClientRect();
    const box = menuRef.current?.getBoundingClientRect();
    if (!anchor || !box) return;
    const left = Math.min(
      Math.max(EDGE, anchor.right - box.width),
      window.innerWidth - EDGE - box.width
    );
    const below = anchor.bottom + 4;
    const top =
      below + box.height <= window.innerHeight - EDGE
        ? below
        : Math.max(EDGE, anchor.top - 4 - box.height);
    setPos({ left, top });
  }, [open, mobile]);

  useLayoutEffect(() => {
    // On a phone the backdrop covers everything and closes the sheet itself.
    if (!open || mobile) return;
    const onDown = (e: PointerEvent) => {
      const target = e.target as Node;
      if (menuRef.current?.contains(target) || triggerRef.current?.contains(target)) return;
      close(false);
    };
    document.addEventListener("pointerdown", onDown, true);
    return () => document.removeEventListener("pointerdown", onDown, true);
  }, [open, mobile]);

  async function pick(untilTs: number) {
    setBusy(true);
    setFailed(false);
    try {
      const outcome = await onSnooze(untilTs);
      if (outcome === "failed") setFailed(true);
      else close(false);
    } finally {
      setBusy(false);
    }
  }

  const nowTs = Date.now() / 1000;
  const draft = snoozeDraft(amount, unit, nowTs);
  const unitWord = (u: SnoozeUnit) =>
    u === "hour" ? t.replies.snoozeHourMany : t.replies.snoozeDayMany;

  const body = (
    <>
      <div className="reply-snooze__presets">
        {SNOOZE_PRESETS.map((p) => (
          <button
            key={`${p.amount}${p.unit}`}
            type="button"
            className="reply-snooze__preset"
            data-testid={`snooze-preset-${p.amount}${p.unit[0]}`}
            disabled={busy}
            onClick={() => void pick(snoozeUntil(p.amount, p.unit, Date.now() / 1000))}
          >
            <span>{msg.replySnoozeIn(p.amount, p.unit)}</span>
            <span className="reply-snooze__when">
              {msg.taskDueDateTime(snoozeUntil(p.amount, p.unit, nowTs))}
            </span>
          </button>
        ))}
      </div>
      <form
        className="reply-snooze__custom"
        onSubmit={(e) => {
          e.preventDefault();
          if (draft.ok) {
            const fresh = snoozeDraft(amount, unit, Date.now() / 1000);
            if (fresh.ok) void pick(fresh.untilTs);
          }
        }}
      >
        <div className="reply-snooze__custom-row">
          {t.replies.snoozeInLead && <span>{t.replies.snoozeInLead}</span>}
          <input
            type="number"
            inputMode="numeric"
            min={1}
            step={1}
            className="reply-snooze__amount"
            aria-label={t.replies.snoozeAmountLabel}
            data-testid="snooze-amount"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
          />
          <select
            className="reply-snooze__unit"
            aria-label={t.replies.snoozeUnitLabel}
            data-testid="snooze-unit"
            value={unit}
            onChange={(e) => setUnit(e.target.value as SnoozeUnit)}
          >
            <option value="hour">{unitWord("hour")}</option>
            <option value="day">{unitWord("day")}</option>
          </select>
          <button
            type="submit"
            className="reply-snooze__apply"
            data-testid="snooze-custom-apply"
            disabled={busy || !draft.ok}
          >
            {t.replies.snoozeApply}
          </button>
        </div>
        <div
          className={`reply-snooze__preview${
            !draft.ok && draft.reason !== "empty" ? " reply-snooze__preview--bad" : ""
          }`}
          data-testid="snooze-preview"
          aria-live="polite"
        >
          {draft.ok
            ? msg.replySnoozeBack(draft.untilTs)
            : draft.reason === "tooFar"
              ? t.replies.snoozeTooFar
              : draft.reason === "invalid"
                ? t.replies.snoozeInvalid
                : " "}
        </div>
      </form>
      {failed && (
        <div className="reply-snooze__error" role="alert" data-testid="snooze-error">
          {t.replies.snoozeError}
        </div>
      )}
    </>
  );

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className="reply-card__jump"
        data-testid="snooze-card"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => (open ? close(false) : setOpen(true))}
      >
        {t.replies.snooze}
      </button>
      {open &&
        createPortal(
          mobile ? (
            <div
              className="reply-snooze__backdrop"
              data-testid="snooze-backdrop"
              onClick={(e) => {
                // The tap must not reach the card underneath through the React tree.
                e.stopPropagation();
                if (e.target === e.currentTarget) close(false);
              }}
            >
              <div
                ref={menuRef}
                role="dialog"
                aria-modal="true"
                aria-labelledby={titleId}
                className="reply-snooze reply-snooze--sheet"
                data-testid={`snooze-menu-${cardId}`}
              >
                <div className="reply-snooze__head">
                  <div id={titleId} className="reply-snooze__title">
                    {t.replies.snooze}
                  </div>
                  <button
                    type="button"
                    className="reply-card__jump"
                    data-testid="snooze-close"
                    onClick={() => close(true)}
                  >
                    {t.common.cancel}
                  </button>
                </div>
                {body}
              </div>
            </div>
          ) : (
            <div
              ref={menuRef}
              role="dialog"
              aria-labelledby={titleId}
              className="reply-snooze reply-snooze--pop"
              data-testid={`snooze-menu-${cardId}`}
              style={pos ? { left: pos.left, top: pos.top } : { left: -9999, top: 0 }}
            >
              <div id={titleId} className="reply-snooze__title">
                {t.replies.snooze}
              </div>
              {body}
            </div>
          ),
          document.body
        )}
    </>
  );
}
