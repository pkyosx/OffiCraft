import { useCallback, useEffect, useState, type FormEvent } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { isHttpStatus, retryAfterSeconds, serverMessageOf } from "../api/errors";
import type { PasskeyView } from "../types";
import { formatAbsolute } from "../lib/dateFormat";
import {
  classifyBrowserError,
  createPasskeyCredential,
  isIpHost,
  localhostAlternative,
  passkeysSupported,
} from "../lib/webauthn";
import { ConfirmModal } from "./ConfirmModal";
import "./profile-dropdown.css";

/** 頭像選單 › 帳號安全 › 通行密鑰: list, add, delete the owner's passkeys. */
export function PasskeysPanel() {
  const { t, msg } = useI18n();
  const p = t.profile;
  const [list, setList] = useState<PasskeyView[] | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [pwd, setPwd] = useState("");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [added, setAdded] = useState(false);
  const [error, setError] = useState("");
  const [confirming, setConfirming] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const onIp = isIpHost(window.location.hostname);
  const supported = passkeysSupported();

  const load = useCallback(async () => {
    setLoadError(false);
    try {
      setList(await api.listPasskeys());
    } catch {
      setList(null);
      setLoadError(true);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  function serverText(err: unknown, fallback: string): string {
    if (isHttpStatus(err, 429)) return msg.loginThrottled(retryAfterSeconds(err));
    const server = serverMessageOf(err);
    return server ? `${p.passkeyErrorLead}${server}` : fallback;
  }

  async function handleAdd(e: FormEvent) {
    e.preventDefault();
    if (busy || !pwd || !name.trim()) return;
    setBusy(true);
    setError("");
    setAdded(false);
    try {
      let ceremony;
      try {
        ceremony = await api.beginPasskeyRegistration(pwd);
      } catch (err) {
        setError(isHttpStatus(err, 401) ? p.pwdErrorCurrent : serverText(err, p.passkeyBrowserFailed));
        return;
      }
      let credential;
      try {
        credential = await createPasskeyCredential(ceremony.options);
      } catch (err) {
        const outcome = classifyBrowserError(err);
        setError(
          outcome === "cancelled"
            ? p.passkeyCancelled
            : outcome === "duplicate"
              ? p.passkeyDuplicate
              : outcome === "noToJSON"
                ? p.passkeyNoToJSON
                : p.passkeyBrowserFailed,
        );
        return;
      }
      try {
        await api.finishPasskeyRegistration(ceremony.ceremonyId, credential, name.trim());
      } catch (err) {
        setError(serverText(err, p.passkeyBrowserFailed));
        return;
      }
      setPwd("");
      setName("");
      setAdded(true);
      await load();
    } finally {
      setBusy(false);
    }
  }

  async function handleDelete(id: string) {
    setDeleting(true);
    setDeleteError("");
    try {
      await api.deletePasskey(id);
      setConfirming(null);
      await load();
    } catch (err) {
      setDeleteError(serverText(err, p.passkeyDeleteFailed));
    } finally {
      setDeleting(false);
    }
  }

  const nowSecs = Math.floor(Date.now() / 1000);

  return (
    <>
      {window.location.hostname === "127.0.0.1" && (
        <div className="profile-dd__form">
          <div className="profile-dd__hint">{msg.passkeyIpHint(localhostAlternative())}</div>
        </div>
      )}

      {loadError && (
        <div className="profile-dd__form">
          <div className="profile-dd__error">{p.passkeysErrorLoad}</div>
          <button type="button" className="profile-dd__submit" onClick={() => void load()}>
            {p.mfaRetry}
          </button>
        </div>
      )}
      {!loadError && list === null && <div className="profile-dd__hint profile-dd__pk-pad">{p.passkeysLoading}</div>}
      {list !== null && list.length === 0 && (
        <div className="profile-dd__hint profile-dd__pk-pad">{p.passkeysEmpty}</div>
      )}
      {list !== null && list.length > 0 && (
        <ul className="profile-dd__pk-list">
          {list.map((k) => (
            <li className="profile-dd__pk" key={k.id} data-testid={`passkey-${k.id}`}>
              <span className="profile-dd__pk-name">{k.name}</span>
              <span className="profile-dd__pk-meta">{`${p.passkeyRpLabel} ${k.rpId}`}</span>
              <span className="profile-dd__pk-meta">{`${p.passkeyCreatedLabel} ${formatAbsolute(k.createdTs, nowSecs)}`}</span>
              <span className="profile-dd__pk-meta">
                {`${p.passkeyLastUsedLabel} ${k.lastUsedTs === null ? p.passkeyNeverUsed : formatAbsolute(k.lastUsedTs, nowSecs)}`}
              </span>
              {!k.usableHere && <span className="profile-dd__pk-flag">{p.passkeyNotUsableHere}</span>}
              {k.cloneWarningTs !== null && (
                <span className="profile-dd__pk-flag profile-dd__pk-flag--danger">{p.passkeyCloneWarning}</span>
              )}
              <button
                type="button"
                className="profile-dd__theme-mini profile-dd__theme-mini--danger profile-dd__pk-delete"
                onClick={() => {
                  setDeleteError("");
                  setConfirming(k.id);
                }}
              >
                {p.passkeyDelete}
              </button>
            </li>
          ))}
        </ul>
      )}

      {!onIp && supported && (
        <form className="profile-dd__form" onSubmit={handleAdd}>
          <div className="profile-dd__field-label">{p.passkeyAddTitle}</div>
          <input
            className="profile-dd__input"
            type="password"
            autoComplete="current-password"
            placeholder={p.currentPasswordPlaceholder}
            aria-label={p.currentPasswordPlaceholder}
            value={pwd}
            disabled={busy}
            onChange={(e) => {
              setPwd(e.target.value);
              setError("");
            }}
          />
          <input
            className="profile-dd__input"
            type="text"
            maxLength={128}
            placeholder={p.passkeyNamePlaceholder}
            aria-label={p.passkeyNamePlaceholder}
            value={name}
            disabled={busy}
            onChange={(e) => {
              setName(e.target.value);
              setError("");
            }}
          />
          {error && <div className="profile-dd__error">{error}</div>}
          {added && <div className="profile-dd__success">{p.passkeyAdded}</div>}
          <button
            type="submit"
            className="profile-dd__submit"
            disabled={busy || !pwd || !name.trim()}
          >
            {busy ? p.passkeyAdding : p.passkeyAdd}
          </button>
        </form>
      )}
      {!onIp && !supported && <div className="profile-dd__hint profile-dd__pk-pad">{p.passkeyUnsupported}</div>}

      {confirming !== null && (
        <ConfirmModal
          testId="passkey-delete-confirm"
          danger
          busy={deleting}
          cancelLabel={p.passkeyDeleteCancel}
          confirmLabel={p.passkeyDelete}
          error={deleteError || null}
          body={
            <>
              <div className="profile-dd__pk-name">{p.passkeyDeleteConfirmTitle}</div>
              <div>{p.passkeyDeleteConfirmBody}</div>
            </>
          }
          onCancel={() => setConfirming(null)}
          onConfirm={() => void handleDelete(confirming)}
        />
      )}
    </>
  );
}
