import { useCallback, useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { isHttpStatus, serverMessageOf } from "../api/errors";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import type { RuntimeLoginState, RuntimeLoginView } from "../types";
import { CheckIcon, CopyIcon, ExternalLinkIcon } from "./icons";
import "./confirm-modal.css";
import "./runtime-login.css";

/** A warden that predates the login verbs never moves a login past
 * `starting`; this is how long the dialog waits before saying so. */
const RUNTIME_LOGIN_START_TIMEOUT_MS = 30_000;

// claude's answer to a wrong or expired code: the token exchange is refused.
const CODE_REJECTED = /^Login failed: Request failed with status code 40[01]\b/;

const TERMINAL: RuntimeLoginState[] = ["succeeded", "failed", "expired", "cancelled"];

function isTerminal(state: RuntimeLoginState): boolean {
  return TERMINAL.includes(state);
}

type Failure = { kind: "noResponse" } | { kind: "error"; message: string };

/** The Claude sign-in dialog. Closing it cancels a login still in flight, so
 * the machine's login process never outlives the dialog. */
export function RuntimeLoginDialog({
  machineId,
  machineName,
  loggedIn,
  onClose,
}: {
  machineId: string;
  machineName: string;
  loggedIn: boolean;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const m = t.monitor.runtimeLogin;
  const [attempt, setAttempt] = useState(0);
  const [login, setLogin] = useState<RuntimeLoginView | null>(null);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [code, setCode] = useState("");
  const [codeRefused, setCodeRefused] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [copied, setCopied] = useState(false);
  const loginRef = useRef<RuntimeLoginView | null>(null);
  loginRef.current = login;
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let alive = true;
    setLogin(null);
    setFailure(null);
    setCode("");
    setCodeRefused(false);
    api
      .startRuntimeLogin(machineId, "claude")
      .then((next) => {
        if (alive) setLogin(next);
      })
      .catch((e) => {
        if (!alive) return;
        setFailure(
          isHttpStatus(e, 409)
            ? { kind: "noResponse" }
            : { kind: "error", message: isHttpStatus(e, 422) ? m.startRefused : serverMessageOf(e) || String(e) }
        );
      });
    return () => {
      alive = false;
    };
  }, [machineId, attempt]);

  useEffect(() => {
    const unsubscribe = api.subscribeEvents((topic) => {
      const current = loginRef.current;
      if (topic !== "runtime_login" || !current) return;
      api
        .getRuntimeLogin(machineId, current.loginId)
        .then((next) => {
          if (loginRef.current?.loginId === next.loginId) setLogin(next);
        })
        .catch((e) => console.warn("RuntimeLoginDialog: refetch failed", e));
    });
    return unsubscribe;
  }, [machineId]);

  const stillStarting = failure === null && (login === null || login.state === "starting");
  useEffect(() => {
    if (!stillStarting) return;
    const timer = window.setTimeout(() => {
      const current = loginRef.current;
      if (current && !isTerminal(current.state)) {
        void api.cancelRuntimeLogin(machineId, current.loginId).catch(() => {});
      }
      setFailure({ kind: "noResponse" });
    }, RUNTIME_LOGIN_START_TIMEOUT_MS);
    return () => window.clearTimeout(timer);
  }, [stillStarting, machineId, attempt]);

  const cancelledRef = useRef(new Set<string>());
  const cancelInFlight = useCallback((): Promise<void> => {
    const current = loginRef.current;
    if (!current || isTerminal(current.state) || cancelledRef.current.has(current.loginId)) {
      return Promise.resolve();
    }
    cancelledRef.current.add(current.loginId);
    return api
      .cancelRuntimeLogin(machineId, current.loginId)
      .then(() => {})
      .catch(() => {});
  }, [machineId]);

  // Leaving the page must end the machine's login process too, not just the
  // close button.
  useEffect(() => () => void cancelInFlight(), [cancelInFlight]);

  const close = useCallback(() => {
    void cancelInFlight();
    onClose();
  }, [cancelInFlight, onClose]);
  useEscapeLayer(close, rootRef);

  // The cancel must land first: a start while the old login is still in flight
  // answers that old login, which the late cancel then ends.
  async function restart() {
    await cancelInFlight();
    setAttempt((a) => a + 1);
  }

  async function submit() {
    const current = loginRef.current;
    if (!current || submitting || code.trim() === "") return;
    setSubmitting(true);
    setCodeRefused(false);
    try {
      setLogin(await api.submitRuntimeLoginCode(machineId, current.loginId, code.trim()));
      setCode("");
    } catch (e) {
      if (isHttpStatus(e, 422)) {
        setCodeRefused(true);
      } else if (isHttpStatus(e, 409)) {
        setFailure({ kind: "error", message: m.codeConflict });
      } else {
        setFailure({ kind: "error", message: serverMessageOf(e) || String(e) });
      }
    } finally {
      setSubmitting(false);
    }
  }

  async function copyUrl(url: string) {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      setCopied(false);
    }
  }

  const restartButton = (
    <button
      type="button"
      className="confirm-modal__btn confirm-modal__btn--accent"
      data-testid="runtime-login-restart"
      onClick={() => void restart()}
    >
      {m.restart}
    </button>
  );

  let body: React.ReactNode;
  let ended = false;
  if (failure?.kind === "noResponse") {
    ended = true;
    body = (
      <div data-testid="runtime-login-no-response">
        <p className="runtime-login__line">
          {machineName}
          {m.noResponseTail}
        </p>
        <p className="runtime-login__hint">{m.noResponseCauses}</p>
      </div>
    );
  } else if (failure?.kind === "error") {
    ended = true;
    body = (
      <p className="runtime-login__line runtime-login__line--bad" data-testid="runtime-login-failed">
        {m.failedLead}
        {failure.message}
      </p>
    );
  } else if (login === null || login.state === "starting") {
    body = (
      <p className="runtime-login__line" data-testid="runtime-login-preparing">
        {m.preparingLead}
        {machineName}
        {m.preparingTail}
      </p>
    );
  } else if (login.state === "awaiting_code") {
    const url = login.authUrl ?? "";
    body = (
      <div className="runtime-login__awaiting" data-testid="runtime-login-awaiting">
        <p className="runtime-login__hint">{m.instructions}</p>
        <div className="runtime-login__url">
          <a
            href={url}
            target="_blank"
            rel="noopener noreferrer"
            className="runtime-login__link"
            data-testid="runtime-login-url"
          >
            <ExternalLinkIcon size={14} />
            <span>{m.openUrl}</span>
          </a>
          <button
            type="button"
            className="confirm-modal__btn"
            data-testid="runtime-login-copy"
            onClick={() => void copyUrl(url)}
          >
            {copied ? <CheckIcon size={14} /> : <CopyIcon size={14} />}
            <span>{copied ? m.copied : m.copyUrl}</span>
          </button>
        </div>
        <form
          className="runtime-login__code"
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <label className="runtime-login__code-label">
            <span>{m.codeLabel}</span>
            <input
              className="runtime-login__code-input"
              data-testid="runtime-login-code"
              value={code}
              autoComplete="off"
              spellCheck={false}
              onChange={(e) => setCode(e.target.value)}
            />
          </label>
          <button
            type="submit"
            className="confirm-modal__btn confirm-modal__btn--accent"
            data-testid="runtime-login-submit"
            disabled={submitting || code.trim() === ""}
          >
            {m.submit}
          </button>
        </form>
        {(codeRefused || login.reason) && (
          <p className="runtime-login__code-error" data-testid="runtime-login-code-refused">
            {m.codeIncomplete}
          </p>
        )}
      </div>
    );
  } else if (login.state === "verifying") {
    body = (
      <p className="runtime-login__line" data-testid="runtime-login-verifying">
        {m.verifying}
      </p>
    );
  } else if (login.state === "succeeded") {
    ended = true;
    const email = login.account?.email ?? "";
    const org = login.account?.orgName ?? "";
    body = (
      <p className="runtime-login__line runtime-login__line--good" data-testid="runtime-login-succeeded">
        {m.succeededLead}
        {email}
        {org !== "" && (
          <>
            {m.orgLead}
            {org}
            {m.orgTail}
          </>
        )}
      </p>
    );
  } else if (login.state === "failed") {
    ended = true;
    const reason = login.reason ?? "";
    body = (
      <div data-testid="runtime-login-failed">
        <p className="runtime-login__line runtime-login__line--bad" data-testid="runtime-login-failed-summary">
          {CODE_REJECTED.test(reason) ? m.codeRejected : m.failedHeading}
        </p>
        {reason !== "" && (
          <p className="runtime-login__cli" data-testid="runtime-login-failed-cli">
            {m.cliSaidLead}
            {reason}
          </p>
        )}
      </div>
    );
  } else {
    ended = true;
    body = (
      <p className="runtime-login__line runtime-login__line--bad" data-testid="runtime-login-failed">
        {login.state === "expired" ? m.expired : m.cancelled}
      </p>
    );
  }

  const succeeded = login?.state === "succeeded" && failure === null;
  return (
    <div
      ref={rootRef}
      className="confirm-modal"
      role="dialog"
      aria-modal="true"
      aria-label={m.title}
      data-testid="runtime-login-dialog"
    >
      <div className="confirm-modal__box">
        <div className="runtime-login__title">{m.title}</div>
        <div className="confirm-modal__body">
          {body}
          {loggedIn && !ended && (
            <p className="runtime-login__hint" data-testid="runtime-login-replace-hint">
              {m.replaceHint}
            </p>
          )}
        </div>
        <div className="confirm-modal__actions">
          {ended && !succeeded && restartButton}
          <button
            type="button"
            className="confirm-modal__btn"
            data-testid="runtime-login-close"
            onClick={close}
          >
            {m.close}
          </button>
        </div>
      </div>
    </div>
  );
}
