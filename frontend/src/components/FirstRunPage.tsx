import { useEffect, useState, type FormEvent } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { isHttpStatus } from "../api/errors";
import { LogoMark } from "./icons";
import "./login.css";

/**
 * First-run setup wall — shown ONLY in real-backend mode when NO owner
 * password exists yet (AuthGate branches on GET /api/auth/status). The owner
 * pastes the one-shot claim token the server printed to its local serve log /
 * installer banner, picks a password, and POST /api/auth/set-password both
 * claims the server and logs the session in (api.setPassword persists the
 * minted owner token). HONEST error mapping: 401 → wrong claim token,
 * 409 → someone already set a password (offer login), 422/local → length or
 * mismatch — never a fake success.
 *
 * One-click first run: serve auto-opens the browser at /?code=<claim token>
 * (server browser.go), so the token field prefills from the query string and
 * focus lands on the password field — the owner only picks a password. The
 * code is scrubbed from the URL immediately (history.replaceState) so it
 * never lingers in the address bar or history.
 */
function readClaimCodeFromURL(): string {
  return (new URLSearchParams(window.location.search).get("code") ?? "").trim();
}
export function FirstRunPage({
  onSuccess,
  onGotoLogin,
}: {
  onSuccess: () => void;
  onGotoLogin: () => void;
}) {
  const { t } = useI18n();
  const [claimToken, setClaimToken] = useState(readClaimCodeFromURL);
  const [prefilled] = useState(() => claimToken !== "");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<"" | "claim" | "short" | "mismatch" | "taken">("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!prefilled) return;
    const url = new URL(window.location.href);
    if (url.searchParams.has("code")) {
      url.searchParams.delete("code");
      history.replaceState(null, "", url.pathname + url.search + url.hash);
    }
  }, [prefilled]);

  const filled = claimToken.trim() !== "" && password !== "" && confirm !== "";

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (busy || !filled) return;
    if (password.length < 8) {
      setError("short");
      return;
    }
    if (password !== confirm) {
      setError("mismatch");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await api.setPassword(password, claimToken.trim());
      onSuccess();
    } catch (err) {
      if (isHttpStatus(err, 409)) setError("taken");
      else if (isHttpStatus(err, 422)) setError("short");
      else setError("claim"); // 401 wrong claim token (or unreachable server)
      setBusy(false);
    }
  }

  const errorText = {
    "": "",
    claim: t.firstRun.errorClaim,
    short: t.firstRun.errorTooShort,
    mismatch: t.firstRun.errorMismatch,
    taken: t.firstRun.errorTaken,
  }[error];

  return (
    <div className="login">
      <form className="login__card" onSubmit={handleSubmit}>
        <span className="login__logo" aria-hidden>
          <LogoMark size={32} />
        </span>
        <h1 className="login__title">{t.firstRun.title}</h1>
        <p className="login__hint">{t.firstRun.intro}</p>

        <input
          className="login__input"
          type="text"
          value={claimToken}
          autoFocus={!prefilled}
          autoComplete="off"
          placeholder={t.firstRun.claimPlaceholder}
          aria-label={t.firstRun.claimPlaceholder}
          disabled={busy}
          onChange={(e) => {
            setClaimToken(e.target.value);
            if (error) setError("");
          }}
        />
        <p className="login__hint login__hint--field">{t.firstRun.claimHint}</p>

        <input
          className="login__input"
          type="password"
          value={password}
          autoFocus={prefilled}
          autoComplete="new-password"
          placeholder={t.firstRun.passwordPlaceholder}
          aria-label={t.firstRun.passwordPlaceholder}
          disabled={busy}
          onChange={(e) => {
            setPassword(e.target.value);
            if (error) setError("");
          }}
        />
        <input
          className="login__input"
          type="password"
          value={confirm}
          autoComplete="new-password"
          placeholder={t.firstRun.confirmPlaceholder}
          aria-label={t.firstRun.confirmPlaceholder}
          disabled={busy}
          onChange={(e) => {
            setConfirm(e.target.value);
            if (error) setError("");
          }}
        />

        {error && <div className="login__error">{errorText}</div>}
        {error === "taken" && (
          <button type="button" className="login__link" onClick={onGotoLogin}>
            {t.firstRun.gotoLogin}
          </button>
        )}

        <button
          type="submit"
          className="login__submit"
          disabled={busy || !filled}
        >
          {busy ? t.firstRun.submitting : t.firstRun.submit}
        </button>
      </form>
    </div>
  );
}
