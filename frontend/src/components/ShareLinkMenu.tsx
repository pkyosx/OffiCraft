// The share button of a file preview or a comparison: a menu that mints a new
// link with its own expiry and lists the target's links still in force, each
// revocable. A popover beside the button on a desktop, a bottom sheet on a
// phone — the 稍後再看 menu's shell (ReplySnoozeMenu) and styles.

import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import { useIsMobile } from "../hooks/useIsMobile";
import { useOwnerDisplayName } from "../hooks/useOwnerName";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { formatAbsolute } from "../lib/dateFormat";
import { serverMessageOf } from "../api/errors";
import { api } from "../api";
import {
  listShareLinksFor,
  mintShareLink,
  type ShareTarget,
} from "../lib/shareLink";
import {
  SHARE_EXPIRY_DEFAULT,
  shareExpiryDraft,
  type ShareExpiryUnit,
} from "../lib/shareLinkExpiry";
import type { ShareLinkView } from "../types";
import { ConfirmModal } from "./ConfirmModal";
import { AlertTriangleIcon, CheckIcon, InfinityIcon, LinkIcon, UnlinkIcon } from "./icons";
import "./replies.css";
import "./share-links.css";

const EDGE = 8;
const FEEDBACK_MS = 2000;

export function ShareLinkMenu({
  target,
  targetName,
  className,
  testId,
}: {
  target: ShareTarget;
  /** What the revoke confirmation names (a filename, or the comparison). */
  targetName: string;
  /** The trigger's skin; it belongs to the surface the button sits on. */
  className: string;
  testId: string;
}) {
  const { t, msg } = useI18n();
  const d = t.shareLinks;
  const mobile = useIsMobile();
  const ownerName = useOwnerDisplayName(t.user);
  const [memberNames, setMemberNames] = useState<Map<string, string>>(new Map());
  const creatorName = (id: string) =>
    id === "owner" ? ownerName : memberNames.get(id) || id;
  const [open, setOpen] = useState(false);
  const [amount, setAmount] = useState(SHARE_EXPIRY_DEFAULT.amount);
  const [unit, setUnit] = useState<ShareExpiryUnit>(SHARE_EXPIRY_DEFAULT.unit);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<"copied" | "copyFailed" | null>(null);
  const [error, setError] = useState("");
  const [links, setLinks] = useState<ShareLinkView[] | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [revokeTarget, setRevokeTarget] = useState<ShareLinkView | null>(null);
  const [revokeBusy, setRevokeBusy] = useState(false);
  const [revokeError, setRevokeError] = useState("");
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const timer = useRef<number | undefined>(undefined);
  const titleId = useId();
  useEffect(() => () => window.clearTimeout(timer.current), []);

  const targetKey =
    target.kind === "attachment"
      ? `a:${target.attachmentId}`
      : `d:${target.params.before}\n${target.params.after}`;
  const targetRef = useRef(target);
  targetRef.current = target;
  const targetKeyRef = useRef(targetKey);
  targetKeyRef.current = targetKey;

  // The preview overlay pages between attachments under an open menu; a list
  // or a mint that lands after that belongs to the previous file.
  useEffect(() => {
    setOpen(false);
    setRevokeTarget(null);
  }, [targetKey]);

  useEffect(() => {
    if (!open) return;
    let alive = true;
    setLinks(null);
    setLoadError(false);
    listShareLinksFor(targetRef.current)
      .then((got) => {
        if (alive) setLinks([...got].reverse());
      })
      .catch((e: unknown) => {
        console.warn("ShareLinkMenu: list failed", e);
        if (alive) setLoadError(true);
      });
    api
      .listMembers({ light: true })
      .then((members) => {
        if (alive) setMemberNames(new Map(members.map((m) => [m.id, m.name])));
      })
      .catch((e: unknown) => console.warn("ShareLinkMenu: roster read failed", e));
    return () => {
      alive = false;
    };
  }, [open, targetKey]);

  function close(refocus: boolean) {
    if (revokeTarget) return;
    setOpen(false);
    setError("");
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
      window.innerWidth - EDGE - box.width,
    );
    const below = anchor.bottom + 4;
    const top =
      below + box.height <= window.innerHeight - EDGE
        ? below
        : Math.max(EDGE, anchor.top - 4 - box.height);
    setPos({ left, top });
  }, [open, mobile, links]);

  const revokeOpen = useRef(false);
  revokeOpen.current = revokeTarget !== null;
  useLayoutEffect(() => {
    if (!open || mobile) return;
    const onDown = (e: PointerEvent) => {
      if (revokeOpen.current) return;
      const node = e.target as Node;
      if (menuRef.current?.contains(node) || triggerRef.current?.contains(node)) return;
      setOpen(false);
      setError("");
      setPos(null);
    };
    document.addEventListener("pointerdown", onDown, true);
    return () => document.removeEventListener("pointerdown", onDown, true);
  }, [open, mobile]);

  const nowTs = Date.now() / 1000;
  const draft = shareExpiryDraft(amount, unit, nowTs);

  async function generate() {
    const fresh = shareExpiryDraft(amount, unit, Date.now() / 1000);
    if (!fresh.ok) return;
    const mintedFor = targetKeyRef.current;
    window.clearTimeout(timer.current);
    setBusy(true);
    setError("");
    setResult(null);
    try {
      const created = await mintShareLink(targetRef.current, fresh.expiresTs);
      let copied = true;
      try {
        await navigator.clipboard.writeText(created.url);
      } catch (e) {
        console.warn("ShareLinkMenu: clipboard write failed", e);
        copied = false;
      }
      if (targetKeyRef.current !== mintedFor) return;
      setLinks((cur) => [created.link, ...(cur ?? [])]);
      setResult(copied ? "copied" : "copyFailed");
      timer.current = window.setTimeout(() => setResult(null), FEEDBACK_MS);
    } catch (e) {
      console.warn("ShareLinkMenu: mint failed", e);
      setError(serverMessageOf(e) || d.mintFailed);
    } finally {
      setBusy(false);
    }
  }

  async function confirmRevoke() {
    if (!revokeTarget) return;
    setRevokeBusy(true);
    try {
      await api.revokeShareLink(revokeTarget.id);
      const gone = revokeTarget.id;
      setLinks((cur) => (cur ?? []).filter((l) => l.id !== gone));
      setRevokeTarget(null);
      setRevokeError("");
    } catch (e) {
      console.warn("ShareLinkMenu: revoke failed", e);
      setRevokeError(serverMessageOf(e) || d.revokeFailed);
    } finally {
      setRevokeBusy(false);
    }
  }

  const generateLabel =
    result === "copied" ? d.copied : result === "copyFailed" ? d.copyFailed : d.generate;
  const draftMessage = draft.ok
    ? ""
    : draft.reason === "tooFar"
      ? d.tooFar
      : draft.reason === "invalid"
        ? d.invalid
        : "";

  const body = (
    <>
      <form
        className="reply-snooze__custom share-expiry"
        onSubmit={(e) => {
          e.preventDefault();
          void generate();
        }}
      >
        <div className="reply-snooze__custom-row">
          <input
            type="number"
            inputMode="numeric"
            min={1}
            step={1}
            className="reply-snooze__amount"
            aria-label={d.amountLabel}
            data-testid="share-expiry-amount"
            value={unit === "forever" ? "" : amount}
            disabled={unit === "forever"}
            onChange={(e) => setAmount(e.target.value)}
          />
          <select
            className="reply-snooze__unit"
            aria-label={d.unitLabel}
            data-testid="share-expiry-unit"
            value={unit}
            onChange={(e) => setUnit(e.target.value as ShareExpiryUnit)}
          >
            <option value="hour">{d.unitHour}</option>
            <option value="day">{d.unitDay}</option>
            <option value="forever">{d.unitForever}</option>
          </select>
          <span className="share-expiry__when" data-testid="share-expiry-preview">
            {draft.ok ? (
              draft.expiresTs === null ? (
                <span className="share-links__inf" role="img" aria-label={d.forever} title={d.forever}>
                  <InfinityIcon size={15} />
                </span>
              ) : (
                <span className="reply-snooze__preview">{formatAbsolute(draft.expiresTs, nowTs)}</span>
              )
            ) : null}
          </span>
          <button
            type="submit"
            className={`share-act share-act--primary${result === "copied" ? " share-act--done" : ""}`}
            data-testid="share-generate"
            aria-label={generateLabel}
            title={generateLabel}
            disabled={busy || !draft.ok}
          >
            {result === "copied" ? (
              <CheckIcon size={15} />
            ) : result === "copyFailed" ? (
              <AlertTriangleIcon size={15} />
            ) : (
              <LinkIcon size={15} />
            )}
          </button>
        </div>
        {(draftMessage || error) && (
          <div className="reply-snooze__preview--bad" role="alert" data-testid="share-error">
            {error || draftMessage}
          </div>
        )}
      </form>
      <div className="share-links" data-testid="share-issued">
        <div className="reply-snooze__title">
          {d.issued} · {links?.length ?? 0}
        </div>
        {loadError && <div className="reply-snooze__error">{d.loadFailed}</div>}
        {(links ?? []).map((l) => (
          <div
            key={l.id}
            className="share-links__row"
            data-testid="share-issued-row"
            data-share-link-id={l.id}
            title={msg.shareLinkCreatedAt(formatAbsolute(l.createdTs, nowTs))}
          >
            <span className="share-links__exp">
              {l.expiresTs === null ? (
                <span className="share-links__inf" role="img" aria-label={d.forever}>
                  <InfinityIcon size={15} />
                </span>
              ) : (
                formatAbsolute(l.expiresTs, nowTs)
              )}
            </span>
            <span className="share-links__by">{creatorName(l.createdBy)}</span>
            <button
              type="button"
              className="share-act share-act--danger"
              data-testid="share-revoke"
              aria-label={d.revoke}
              title={d.revoke}
              onClick={() => {
                setRevokeError("");
                setRevokeTarget(l);
              }}
            >
              <UnlinkIcon size={15} />
            </button>
          </div>
        ))}
      </div>
    </>
  );

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className={className + (open ? " share-trigger--open" : "")}
        data-testid={testId}
        aria-label={d.trigger}
        title={d.trigger}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => (open ? close(false) : setOpen(true))}
      >
        <LinkIcon size={14} />
      </button>
      {open &&
        createPortal(
          mobile ? (
            <div
              className="reply-snooze__backdrop share-menu__backdrop"
              data-testid="share-menu-backdrop"
              onClick={(e) => {
                e.stopPropagation();
                if (e.target === e.currentTarget) close(false);
              }}
            >
              <div
                ref={menuRef}
                role="dialog"
                aria-modal="true"
                aria-labelledby={titleId}
                className="reply-snooze reply-snooze--sheet share-menu"
                data-testid="share-menu"
              >
                <div className="reply-snooze__head">
                  <div id={titleId} className="reply-snooze__title">
                    {d.menuTitle}
                  </div>
                  <button
                    type="button"
                    className="reply-card__jump"
                    onClick={() => close(true)}
                  >
                    {d.cancel}
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
              className="reply-snooze reply-snooze--pop share-menu"
              data-testid="share-menu"
              style={pos ? { left: pos.left, top: pos.top } : { left: -9999, top: 0 }}
            >
              <div id={titleId} className="reply-snooze__title">
                {d.menuTitle}
              </div>
              {body}
            </div>
          ),
          document.body,
        )}
      {revokeTarget &&
        createPortal(
          <div className="share-confirm-layer">
            <ConfirmModal
              body={msg.shareLinkRevokeConfirmBody(targetName)}
              error={revokeError || null}
              cancelLabel={d.cancel}
              confirmLabel={d.revokeConfirm}
              busy={revokeBusy}
              danger
              onCancel={() => setRevokeTarget(null)}
              onConfirm={() => void confirmRevoke()}
              testId="share-revoke-confirm"
              confirmTestId="share-revoke-confirm-btn"
            />
          </div>,
          document.body,
        )}
    </>
  );
}
