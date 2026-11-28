// 分享: every share link still in force, across files and comparisons, each
// revocable. A link's URL is never shown here — the server keeps only a hash.

import { useState } from "react";
import { useI18n } from "../i18n";
import { useShareLinks } from "../hooks/useShareLinks";
import { useActorName } from "../hooks/useActorName";
import { formatAbsolute } from "../lib/dateFormat";
import { shareLinkTargetName } from "../lib/shareLink";
import type { ShareLinkView } from "../types";
import { ConfirmModal } from "./ConfirmModal";
import { FileTextIcon, InfinityIcon, PlusMinusIcon, UnlinkIcon } from "./icons";
import "./lore.css";
import "./share-links.css";

export function ShareLinksPage() {
  const { t, msg } = useI18n();
  const d = t.shareLinks;
  const actorName = useActorName();
  const { links, loading, loadError, revoke } = useShareLinks(d.loadFailed, d.revokeFailed);
  const [target, setTarget] = useState<ShareLinkView | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const nowSecs = Math.floor(Date.now() / 1000);

  async function confirmRevoke() {
    if (!target) return;
    setBusy(true);
    const err = await revoke(target.id);
    setBusy(false);
    if (err) {
      setError(err);
      return;
    }
    setTarget(null);
    setError("");
  }

  return (
    <div className="share-page" data-testid="share-links-page">
      {loadError && (
        <div className="lore__error" data-testid="share-links-load-error">
          {loadError}
        </div>
      )}

      {!loading && links.length === 0 && !loadError && (
        <div className="lore__empty" data-testid="share-links-empty">
          {d.empty}
        </div>
      )}

      {links.length > 0 && (
        <div className="share-page__table" data-testid="share-links-list">
          <div className="share-page__row share-page__row--head" aria-hidden="true">
            <span />
            <span>{d.colName}</span>
            <span>{d.colCreatedBy}</span>
            <span>{d.colExpires}</span>
            <span />
          </div>
          {links.map((l) => {
            const name = shareLinkTargetName(l);
            return (
              <div
                key={l.id}
                className="share-page__row"
                data-testid="share-link-row"
                data-share-link-id={l.id}
                title={msg.shareLinkCreatedAt(formatAbsolute(l.createdTs, nowSecs))}
              >
                <span
                  className="share-page__kind"
                  role="img"
                  aria-label={l.kind === "attachment" ? d.kindAttachment : d.kindDiff}
                >
                  {l.kind === "attachment" ? <FileTextIcon size={15} /> : <PlusMinusIcon size={15} />}
                </span>
                <span className="share-page__name" data-testid="share-link-name">
                  {name}
                </span>
                <span className="share-page__by">{actorName(l.createdBy)}</span>
                <span className="share-page__exp" data-testid="share-link-expires">
                  {l.expiresTs === null ? (
                    <span className="share-links__inf" role="img" aria-label={d.forever}>
                      <InfinityIcon size={15} />
                    </span>
                  ) : (
                    formatAbsolute(l.expiresTs, nowSecs)
                  )}
                </span>
                <button
                  type="button"
                  className="share-act share-act--danger"
                  data-testid="share-link-revoke"
                  aria-label={`${d.revoke} ${name}`}
                  title={d.revoke}
                  onClick={() => {
                    setError("");
                    setTarget(l);
                  }}
                >
                  <UnlinkIcon size={15} />
                </button>
              </div>
            );
          })}
        </div>
      )}

      {target && (
        <ConfirmModal
          body={msg.shareLinkRevokeConfirmBody(shareLinkTargetName(target))}
          error={error || null}
          cancelLabel={d.cancel}
          confirmLabel={d.revokeConfirm}
          busy={busy}
          danger
          onCancel={() => setTarget(null)}
          onConfirm={confirmRevoke}
          testId="share-links-revoke-confirm"
          confirmTestId="share-links-revoke-confirm-btn"
        />
      )}
    </div>
  );
}
