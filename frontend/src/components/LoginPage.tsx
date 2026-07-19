import { useState, type FormEvent } from "react";
import { useI18n } from "../i18n";
import { login } from "../api/auth";
import { LogoMark } from "./icons";
import "./login.css";

/**
 * Owner login wall — shown ONLY in real-backend mode when no token exists
 * (AuthGate owns that decision; mock mode never renders this). Submits the
 * deploy password to POST /api/login; on success the server-minted owner token
 * is persisted (api/auth.login) and onSuccess() lets AuthGate boot the app.
 * HONEST: a wrong password yields a 401 → inline error, no entry, no fake token.
 */
export function LoginPage({ onSuccess }: { onSuccess: () => void }) {
  const { t } = useI18n();
  const [password, setPassword] = useState("");
  const [error, setError] = useState(false);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (busy || !password) return;
    setBusy(true);
    setError(false);
    try {
      await login(password);
      onSuccess();
    } catch {
      // Wrong/empty password (401) — surface the error, keep the user here.
      setError(true);
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
            if (error) setError(false);
          }}
        />

        {error && <div className="login__error">{t.login.error}</div>}

        <button
          type="submit"
          className="login__submit"
          disabled={busy || !password}
        >
          {busy ? t.login.submitting : t.login.submit}
        </button>
      </form>
    </div>
  );
}
