// SigningKeysCard — 設定 › 系統更新與備份 · 簽章金鑰 (T-62).
//
// Its own file rather than a private function inside SettingsPage for the same
// reason ThemeSettings is: the browser-level guard beside it
// (visual-guards/signing-keys-card.ct.spec.tsx) has to mount the real card, and
// exporting one purely so a test can reach it is a backdoor that then has to be
// kept honest forever.
import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import { useSigningKeys } from "../hooks/useSigningKeys";
import { ConfirmModal } from "./ConfirmModal";
import { formatAbsolute } from "../lib/dateFormat";

/**
 * 簽章金鑰 (T-62) — how many keys exist, when each was made, which one signs,
 * and the two actions that change that.
 *
 * The card's whole job beyond listing is to make the ASYMMETRY between the two
 * buttons visible, because it is not obvious and it is not recoverable:
 *
 *  - 產生新金鑰 is safe and reversible-in-effect. It adds a key and moves the
 *    signing mark; nobody is logged out, so it needs no confirmation.
 *  - 移除 is a REVOCATION with no undo. Everything the key signed dies at once
 *    — credentials AND the file share links derived from it (owner ruling,
 *    card rc-cf9c27c07442) — with no grace period and no notice to whoever
 *    holds them. So it goes through a confirmation that spells out both, plus
 *    the thing nobody would guess: warden credentials carry no expiry, so
 *    "wait a few days and the old tokens will have lapsed" is FALSE for them.
 *    The question is whether every machine has reconnected.
 *
 * The signing key has no 移除 button at all rather than one that errors: the
 * server refuses it with a 409, but a button that exists in order to fail is a
 * worse answer than a button that is not there.
 */
export function SigningKeysCard() {
  const { t } = useI18n();
  const d = t.signingKeys;
  // The fallback is the card's own copy for a rejection that carried no server
  // reason — never an empty error line, which reads as "nothing went wrong".
  const { keys, loading, busy, error, rotate, remove } = useSigningKeys(
    d.actionFailed,
  );
  const [confirming, setConfirming] = useState<string | null>(null);
  const nowSecs = Math.floor(Date.now() / 1000);

  // Close the confirmation when the removal actually LANDS — i.e. when the key
  // is no longer in the ring the server just answered with. A refusal leaves
  // the key there, so the modal stays open carrying the server's reason, which
  // is where the user pressed and where they are looking.
  useEffect(() => {
    if (confirming !== null && !keys.some((k) => k.keyId === confirming)) {
      setConfirming(null);
    }
  }, [keys, confirming]);

  return (
    <>
      <h2 className="settings__title settings__title--doc">{d.title}</h2>
      <div className="param-card signing-keys" data-testid="set-signing-keys">
        <div className="signing-keys__hint">{d.intro}</div>

        {loading ? (
          <div className="signing-keys__loading">{d.loading}</div>
        ) : keys.length === 0 ? (
          <div className="signing-keys__loading">{d.emptyState}</div>
        ) : (
          <>
            <div className="signing-keys__count" data-testid="set-signing-keys-count">
              {d.countLabel(keys.length)}
            </div>
            <ul className="signing-keys__list">
              {keys.map((k) => (
                <li
                  className="signing-keys__row"
                  key={k.keyId}
                  data-testid={`set-signing-key-${k.keyId}`}
                  data-signing={k.isSigning ? "yes" : "no"}
                >
                  <span className="signing-keys__id">{k.keyId}</span>
                  <span
                    className={`signing-keys__badge signing-keys__badge--${
                      k.isSigning ? "signing" : "retired"
                    }`}
                  >
                    {k.isSigning ? d.signingBadge : d.retiredBadge}
                  </span>
                  <span className="signing-keys__created">
                    {/* null is "never recorded", NOT a missing value — it gets
                        words, because rendering it as a date would be a wrong
                        fact rather than an absent one. */}
                    {k.createdTs === null
                      ? d.createdUnknown
                      : `${d.createdLabel} ${formatAbsolute(k.createdTs, nowSecs)}`}
                  </span>
                  {!k.isSigning && (
                    <button
                      type="button"
                      className="btn btn--danger-ghost signing-keys__remove"
                      disabled={busy}
                      onClick={() => setConfirming(k.keyId)}
                    >
                      {d.removeButton}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          </>
        )}

        <div className="signing-keys__actions">
          <button
            type="button"
            className="btn"
            data-testid="set-signing-keys-rotate"
            disabled={busy || loading}
            onClick={rotate}
          >
            {d.rotateButton}
          </button>
          <div className="signing-keys__hint">{d.rotateHint}</div>
        </div>

        {error !== "" && (
          <div className="set-error param-error" data-testid="set-signing-keys-error">
            {error}
          </div>
        )}

        {confirming !== null && (
          <ConfirmModal
            testId="set-signing-keys-confirm"
            confirmTestId="set-signing-keys-confirm-ok"
            danger
            busy={busy}
            cancelLabel={d.removeConfirmCancel}
            confirmLabel={d.removeConfirmOk}
            body={
              <>
                <div className="signing-keys__confirm-title">
                  {d.removeConfirmTitle}
                </div>
                <div>{d.removeConfirmBody}</div>
                {/* The part nobody would guess, and the reason this modal has
                    two paragraphs instead of one. */}
                <div className="signing-keys__confirm-warn">
                  {d.removeConfirmWarden}
                </div>
              </>
            }
            onCancel={() => setConfirming(null)}
            error={error !== "" ? error : null}
            onConfirm={() => {
              // Closing here is what made `busy` and `error` dead props: the
              // modal was gone before either could mean anything, so a refusal
              // surfaced below a dialog the user had already dismissed. The
              // hook clears `error` when the next action starts, and the effect
              // below closes the modal once a removal actually lands.
              remove(confirming);
            }}
          />
        )}
      </div>
    </>
  );
}
