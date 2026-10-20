import { useState, type FormEvent } from "react";
import { useI18n } from "../i18n";
import { login, passkeyLogin } from "../api/auth";
import { isHttpStatus, retryAfterSeconds } from "../api/errors";
import {
  classifyBrowserError,
  getPasskeyAssertion,
  localhostAlternative,
  passkeysSupported,
} from "../lib/webauthn";
import { LogoMark } from "./icons";
import "./login.css";

/** Why a `?login=` link did not log in: refused (throttledFor null) or braked. */
export type LoginLinkFailure = { throttledFor: number | null };

/**
 * Owner login wall — shown ONLY in real-backend mode when no token exists
 * (AuthGate owns that decision; mock mode never renders this). Submits the
 * deploy password — plus a TOTP code when the server says one is required — to
 * POST /api/login; on success the server-minted owner token is persisted
 * (api/auth.login) and onSuccess() lets AuthGate boot the app.
 * HONEST: a wrong credential yields a 401 → inline error, no entry, no fake token.
 *
 * 🔴 THE ERROR WORDING IS A SECURITY DECISION, NOT A COPY CHOICE. The server
 * answers ONE indistinguishable 401 for "wrong password" and "wrong code",
 * because naming the failing half would confirm a correct password to an
 * attacker who has guessed only that half. This wall therefore CANNOT know
 * which field is wrong, and says so honestly by naming both — rather than
 * guessing at one and sending the owner to re-type the field that was fine.
 *
 * `mfaRequired` arrives from the PUBLIC /api/auth/status probe, which is the
 * only way to render the right fields before anyone holds a token.
 */
export function LoginPage({
  onSuccess,
  mfaRequired = false,
  refreshMfaRequired,
  loginLinkFailure = null,
  passkeyLoginAvailable = false,
}: {
  onSuccess: () => void;
  mfaRequired?: boolean;
  /** Re-read the public auth probe; resolves to whether a code is required NOW.
   * Called only when a login is refused while this wall shows no code field —
   * see the 401 branch below. */
  refreshMfaRequired?: () => Promise<boolean>;
  /** A `?login=` link that did not log in on the way here (AuthGate). */
  loginLinkFailure?: LoginLinkFailure | null;
  /** From the public auth probe: whether this host can offer passkey sign-in. */
  passkeyLoginAvailable?: boolean;
}) {
  const { t, msg } = useI18n();
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState(false);
  /** Seconds to wait, from a 429's Retry-After. null = not throttled. */
  const [throttledFor, setThrottledFor] = useState<number | null>(
    loginLinkFailure?.throttledFor ?? null,
  );
  /** Set when a refused login turned out to be a MISSING code, not a wrong
   * password — the wall was out of date and has just grown its code field. */
  const [codeNowRequired, setCodeNowRequired] = useState(false);
  const [busy, setBusy] = useState(false);
  const [passkeyBusy, setPasskeyBusy] = useState(false);
  const [passkeyOutcome, setPasskeyOutcome] = useState<"" | "cancelled" | "failed">("");
  const showPasskey = passkeyLoginAvailable && passkeysSupported();
  const showLocalhostHint =
    !passkeyLoginAvailable && window.location.hostname === "127.0.0.1";

  function clearErrors() {
    if (error) setError(false);
    if (throttledFor !== null) setThrottledFor(null);
    if (codeNowRequired) setCodeNowRequired(false);
    if (passkeyOutcome) setPasskeyOutcome("");
  }

  async function handlePasskey() {
    if (busy || passkeyBusy) return;
    setPasskeyBusy(true);
    setError(false);
    setThrottledFor(null);
    setCodeNowRequired(false);
    setPasskeyOutcome("");
    try {
      await passkeyLogin(getPasskeyAssertion);
      onSuccess();
    } catch (err) {
      if (isHttpStatus(err, 429)) setThrottledFor(retryAfterSeconds(err));
      else setPasskeyOutcome(classifyBrowserError(err) === "cancelled" ? "cancelled" : "failed");
      setPasskeyBusy(false);
    }
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (busy || !password || (mfaRequired && !code)) return;
    setBusy(true);
    setError(false);
    setThrottledFor(null);
    setCodeNowRequired(false);
    try {
      await login(password, mfaRequired ? code : undefined);
      onSuccess();
    } catch (err) {
      // 429 is the credential-attempt brake, NOT a wrong credential — telling
      // the owner "wrong password" here would send them chasing a typo that
      // does not exist.
      if (isHttpStatus(err, 429)) {
        setThrottledFor(retryAfterSeconds(err));
      } else if (!mfaRequired && refreshMfaRequired && (await refreshMfaRequired())) {
        // 🔴 THE WALL WAS OUT OF DATE, not the password. Being refused while
        // showing no code field is exactly what a stale wall looks like — the
        // factor was armed on another device, or the first-paint probe failed
        // and left the default. Re-probing turns a permanent "wrong password"
        // dead end (escapable only by reloading, which nothing on screen
        // suggests) into a code field and a sentence saying why it appeared.
        setCodeNowRequired(true);
      } else {
        setError(true);
      }
      // The password is cleared but the code is NOT: a TOTP code is short-lived,
      // so the owner's next attempt needs a fresh one anyway, and wiping the
      // field they just read off their phone is the more annoying half.
      setPassword("");
      setBusy(false);
    }
  }

  return (
    <div className="login">
      <form className="login__card" onSubmit={handleSubmit}>
        <span className="login__logo" aria-hidden>
          <LogoMark size={32} />
        </span>
        <h1 className="login__title">{t.login.title}</h1>

        <input
          className="login__input"
          type="password"
          value={password}
          autoFocus
          autoComplete="current-password"
          placeholder={t.login.passwordPlaceholder}
          disabled={busy}
          onChange={(e) => {
            setPassword(e.target.value);
            clearErrors();
          }}
        />

        {mfaRequired && (
          <>
            <input
              className="login__input"
              type="text"
              value={code}
              // one-time-code lets password managers and iOS/Android autofill
              // offer the code straight from the authenticator.
              autoComplete="one-time-code"
              inputMode="numeric"
              placeholder={t.login.codePlaceholder}
              aria-label={t.login.codePlaceholder}
              disabled={busy}
              onChange={(e) => {
                setCode(e.target.value);
                clearErrors();
              }}
            />
            <div className="login__hint">{t.login.codeHint}</div>
          </>
        )}

        {loginLinkFailure !== null && loginLinkFailure.throttledFor === null && (
          <div className="login__error">{t.login.loginLinkInvalid}</div>
        )}
        {throttledFor !== null && (
          <div className="login__error">{msg.loginThrottled(throttledFor)}</div>
        )}
        {codeNowRequired && (
          <div className="login__error">{t.login.codeNowRequired}</div>
        )}
        {error && (
          <div className="login__error">
            {mfaRequired ? t.login.errorWithCode : t.login.error}
          </div>
        )}
        {passkeyOutcome === "cancelled" && (
          <div className="login__hint">{t.login.passkeyCancelled}</div>
        )}
        {passkeyOutcome === "failed" && (
          <div className="login__error">{t.login.passkeyFailed}</div>
        )}

        <button
          type="submit"
          className="login__submit"
          disabled={busy || passkeyBusy || !password || (mfaRequired && !code)}
        >
          {busy ? t.login.submitting : t.login.submit}
        </button>

        {showPasskey && (
          <button
            type="button"
            className="login__submit login__submit--secondary"
            disabled={busy || passkeyBusy}
            onClick={() => void handlePasskey()}
          >
            {passkeyBusy ? t.login.passkeyWaiting : t.login.passkeySubmit}
          </button>
        )}
        {showLocalhostHint && (
          <div className="login__hint">{msg.passkeyIpHint(localhostAlternative())}</div>
        )}
      </form>
    </div>
  );
}
