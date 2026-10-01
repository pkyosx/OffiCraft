import { useCallback, useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { isHttpStatus, serverMessageOf } from "../api/errors";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import type { RuntimeLoginRuntime, RuntimeLoginState, RuntimeLoginView } from "../types";
import {
  AlertTriangleIcon,
  CheckIcon,
  ClockIcon,
  CopyIcon,
  ExternalLinkIcon,
  KeyIcon,
  ShieldAlertIcon,
  TerminalIcon,
} from "./icons";
import "./confirm-modal.css";
import "./runtime-login.css";

/** A warden that predates the login verbs never moves a login past
 * `starting`; this is how long the dialog waits before saying so. */
const RUNTIME_LOGIN_START_TIMEOUT_MS = 30_000;

// claude's answer to a wrong or expired code: the token exchange is refused.
const CODE_REJECTED = /^Login failed: Request failed with status code 40[01]\b/;

// What a warden that can only log in claude answers a codex login_start with.
const RUNTIME_UNSUPPORTED = /cannot log in runtime "codex"/;
const CODE_UNREADABLE = /^could not read the one-time code/;

// Only called before the deadline: at or past it the dialog shows the expired view.
function remaining(deadlineMs: number, nowMs: number): string {
  const secs = Math.floor((deadlineMs - nowMs) / 1000);
  return `${String(Math.floor(secs / 60)).padStart(2, "0")}:${String(secs % 60).padStart(2, "0")}`;
}

function leftPercent(deadlineMs: number, nowMs: number, totalMs: number): number {
  if (totalMs <= 0) return 0;
  return Math.min(100, Math.max(0, ((deadlineMs - nowMs) / totalMs) * 100));
}

const TERMINAL: RuntimeLoginState[] = ["succeeded", "failed", "expired", "cancelled"];

function isTerminal(state: RuntimeLoginState): boolean {
  return TERMINAL.includes(state);
}

type Failure = { kind: "noResponse" } | { kind: "error"; message: string };

/** The sign-in dialog for one runtime on one machine: claude by sign-in URL
 * plus a pasted code, codex by device code approved on OpenAI's page. Closing
 * it cancels a login still in flight, so the machine's login process never
 * outlives the dialog. */
export function RuntimeLoginDialog({
  machineId,
  machineName,
  runtime,
  loggedIn,
  onClose,
}: {
  machineId: string;
  machineName: string;
  runtime: RuntimeLoginRuntime;
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
  const [copied, setCopied] = useState<"url" | "code" | null>(null);
  const [nowMs, setNowMs] = useState(() => Date.now());
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
      .startRuntimeLogin(machineId, runtime)
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
  }, [machineId, runtime, attempt]);

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

  // expires_ts and updated_ts are both server clock, so their difference is
  // what is left; counted down on the browser clock from when it arrived, it is
  // immune to skew between the two clocks.
  const [deadline, setDeadline] = useState<{ key: string; ms: number; totalMs: number } | null>(null);
  const deadlineKey =
    login?.state === "awaiting_authorization" && login.expiresTs != null
      ? `${login.loginId}:${login.expiresTs}:${login.updatedTs}`
      : null;
  useEffect(() => {
    if (deadlineKey === null || !login || login.expiresTs == null) {
      setDeadline(null);
      return;
    }
    const left = login.expiresTs - login.updatedTs;
    const code = `${login.loginId}:${login.expiresTs}:`;
    // A newer updatedTs for the same code leaves less time but the same code:
    // the bar keeps measuring against the code's whole lifetime instead of
    // jumping back to full.
    setDeadline((prev) => ({
      key: deadlineKey,
      ms: Date.now() + left * 1000,
      totalMs: prev?.key.startsWith(code) ? prev.totalMs : left * 1000,
    }));
    // Keyed on the values, not the object: a refetch of the same login must not
    // restart the countdown.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deadlineKey]);
  const deadlineMs = deadline && deadline.key === deadlineKey ? deadline.ms : null;
  const codeRanOut = deadlineMs != null && deadlineMs <= nowMs;
  const counting = deadlineMs != null && !codeRanOut;
  useEffect(() => {
    if (!counting) return;
    setNowMs(Date.now());
    const timer = window.setInterval(() => setNowMs(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [counting]);

  async function copy(what: "url" | "code", text: string) {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(what);
      window.setTimeout(() => setCopied(null), 1600);
    } catch {
      setCopied(null);
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

  const codex = runtime === "codex";
  const replaceHint = loggedIn && (
    <p className="runtime-login__hint" data-testid="runtime-login-replace-hint">
      {m.replaceHint}
    </p>
  );
  const openStep = (url: string) => (
    <Step
      n={1}
      title={codex ? m.codexStep1Title : m.step1Title}
      desc={codex ? m.codexStep1Desc : m.step1Desc}
    >
      <div className="runtime-login__row">
        <a
          href={url}
          target="_blank"
          rel="noopener noreferrer"
          className="confirm-modal__btn confirm-modal__btn--accent runtime-login__link"
          data-testid="runtime-login-url"
        >
          <ExternalLinkIcon size={14} />
          <span>{m.openUrl}</span>
        </a>
        <button
          type="button"
          className="confirm-modal__btn"
          data-testid="runtime-login-copy"
          onClick={() => void copy("url", url)}
        >
          {copied === "url" ? <CheckIcon size={14} /> : <CopyIcon size={14} />}
          <span>{copied === "url" ? m.copied : m.copyUrl}</span>
        </button>
      </div>
    </Step>
  );

  let body: React.ReactNode;
  let ended = false;
  if (failure?.kind === "noResponse") {
    ended = true;
    body = (
      <Status tone="bad">
        <div data-testid="runtime-login-no-response">
          <p className="runtime-login__line runtime-login__line--bad">
            {machineName}
            {m.noResponseTail}
          </p>
          <p className="runtime-login__hint">{m.noResponseCauses}</p>
        </div>
      </Status>
    );
  } else if (failure?.kind === "error") {
    ended = true;
    body = (
      <Status tone="bad">
        <p className="runtime-login__line runtime-login__line--bad" data-testid="runtime-login-failed">
          {m.failedLead}
          {failure.message}
        </p>
      </Status>
    );
  } else if (login === null || login.state === "starting") {
    body = (
      <Status tone="busy">
        <p className="runtime-login__line" data-testid="runtime-login-preparing">
          {m.preparingLead}
          {machineName}
          {m.preparingTail}
        </p>
        {replaceHint}
      </Status>
    );
  } else if (login.state === "awaiting_code") {
    body = (
      <ol className="runtime-login__steps" data-testid="runtime-login-awaiting">
        {openStep(login.authUrl ?? "")}
        <Step n={2} title={m.step2Title} desc={m.step2Desc}>
          <form
            className="runtime-login__row"
            onSubmit={(e) => {
              e.preventDefault();
              void submit();
            }}
          >
            <input
              className="runtime-login__code-input"
              data-testid="runtime-login-code"
              aria-label={m.codeLabel}
              placeholder={m.codePlaceholder}
              value={code}
              autoComplete="off"
              spellCheck={false}
              onChange={(e) => setCode(e.target.value)}
            />
            <button
              type="submit"
              className="confirm-modal__btn confirm-modal__btn--accent runtime-login__submit"
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
        </Step>
        <Step n={3} pending title={m.step3Title} desc={m.step3Desc}>
          {replaceHint}
        </Step>
      </ol>
    );
  } else if (codeRanOut) {
    ended = true;
    body = (
      <Status tone="bad">
        <p className="runtime-login__line runtime-login__line--bad" data-testid="runtime-login-failed">
          {m.codexExpired}
        </p>
      </Status>
    );
  } else if (login.state === "awaiting_authorization") {
    const userCode = login.userCode ?? "";
    body = (
      <ol className="runtime-login__steps" data-testid="runtime-login-authorize">
        {openStep(login.authUrl ?? "")}
        <Step n={2} title={m.codexStep2Title} desc={m.codexStep2Desc}>
          <div className="runtime-login__chip" role="group" aria-label={m.userCodeLabel}>
            <code className="runtime-login__chip-code" data-testid="runtime-login-user-code">
              {userCode}
            </code>
            <button
              type="button"
              className="confirm-modal__btn runtime-login__chip-copy"
              data-testid="runtime-login-copy-code"
              onClick={() => void copy("code", userCode)}
            >
              {copied === "code" ? <CheckIcon size={13} /> : <CopyIcon size={13} />}
              <span>{copied === "code" ? m.copied : m.copyCode}</span>
            </button>
          </div>
          {deadline != null && deadlineMs != null && (
            <div className="runtime-login__timer">
              <span className="runtime-login__bar" aria-hidden="true">
                <span
                  className="runtime-login__bar-fill"
                  data-testid="runtime-login-remaining-bar"
                  style={{ width: `${leftPercent(deadlineMs, nowMs, deadline.totalMs)}%` }}
                />
              </span>
              <span className="runtime-login__pill" data-testid="runtime-login-remaining">
                <ClockIcon size={12} />
                {m.remainingLead}
                <b>{remaining(deadlineMs, nowMs)}</b>
              </span>
            </div>
          )}
          <p className="runtime-login__callout">
            <ShieldAlertIcon size={14} />
            <span data-testid="runtime-login-phishing">{m.codexPhishing}</span>
          </p>
        </Step>
        <Step n={3} pending title={m.codexStep3Title}>
          <p className="runtime-login__waiting" data-testid="runtime-login-waiting">
            <span className="runtime-login__spinner" aria-hidden="true" />
            <span>
              <b>{m.codexWaiting}</b>
              {m.codexWaitingTail}
            </span>
          </p>
          {replaceHint}
        </Step>
      </ol>
    );
  } else if (login.state === "verifying") {
    body = (
      <Status tone="busy">
        <p className="runtime-login__line" data-testid="runtime-login-verifying">
          {m.verifying}
        </p>
        {replaceHint}
      </Status>
    );
  } else if (login.state === "succeeded") {
    ended = true;
    const email = login.account?.email ?? "";
    // Claude shows the organization, Codex the subscription plan.
    const org = (codex ? login.account?.plan : login.account?.orgName) ?? "";
    body = (
      <Status tone="good">
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
      </Status>
    );
  } else if (login.state === "failed") {
    ended = true;
    const reason = login.reason ?? "";
    body = (
      <Status tone="bad">
        <div data-testid="runtime-login-failed">
          <p className="runtime-login__line runtime-login__line--bad" data-testid="runtime-login-failed-summary">
            {RUNTIME_UNSUPPORTED.test(reason)
              ? m.codexUnsupported
              : codex && CODE_UNREADABLE.test(reason)
                ? m.codexCodeUnreadable
              : !codex && CODE_REJECTED.test(reason)
                ? m.codeRejected
                : m.failedHeading}
          </p>
          {reason !== "" && (
            <p className="runtime-login__cli" data-testid="runtime-login-failed-cli">
              {codex ? m.codexSaidLead : m.cliSaidLead}
              {reason}
            </p>
          )}
        </div>
      </Status>
    );
  } else {
    ended = true;
    body = (
      <Status tone="bad">
        <p className="runtime-login__line runtime-login__line--bad" data-testid="runtime-login-failed">
          {login.state === "expired" ? (codex ? m.codexExpired : m.expired) : m.cancelled}
        </p>
      </Status>
    );
  }

  const succeeded = login?.state === "succeeded" && failure === null;
  const title = codex ? m.titleCodex : m.title;
  return (
    <div
      ref={rootRef}
      className="confirm-modal"
      role="dialog"
      aria-modal="true"
      aria-label={title}
      data-testid="runtime-login-dialog"
    >
      <div className="confirm-modal__box runtime-login__box">
        <div className="runtime-login__head">
          <span className="runtime-login__mark" aria-hidden="true">
            {codex ? <KeyIcon size={17} /> : <TerminalIcon size={17} />}
          </span>
          <div className="runtime-login__head-text">
            <div className="runtime-login__title">{title}</div>
            <p className="runtime-login__subtitle" data-testid="runtime-login-subtitle">
              {codex ? m.codexSubtitleLead : m.subtitleLead}
              {machineName}
              {codex ? m.codexSubtitleTail : m.subtitleTail}
            </p>
          </div>
        </div>
        <div className="confirm-modal__body">{body}</div>
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

function Step({
  n,
  title,
  desc,
  pending = false,
  children,
}: {
  n: number;
  title: string;
  desc?: string;
  pending?: boolean;
  children?: React.ReactNode;
}) {
  return (
    <li className="runtime-login__step">
      <span
        className={`runtime-login__step-num${pending ? " runtime-login__step-num--pending" : ""}`}
        aria-hidden="true"
      >
        {n}
      </span>
      <div className="runtime-login__step-main">
        <div className="runtime-login__step-title">{title}</div>
        {desc && <p className="runtime-login__step-desc">{desc}</p>}
        {children && <div className="runtime-login__step-body">{children}</div>}
      </div>
    </li>
  );
}

function Status({ tone, children }: { tone: "busy" | "good" | "bad"; children: React.ReactNode }) {
  return (
    <div className={`runtime-login__status runtime-login__status--${tone}`}>
      <span className="runtime-login__status-mark" aria-hidden="true">
        {tone === "busy" ? (
          <span className="runtime-login__spinner" />
        ) : tone === "good" ? (
          <CheckIcon size={15} />
        ) : (
          <AlertTriangleIcon size={15} />
        )}
      </span>
      <div className="runtime-login__status-text">{children}</div>
    </div>
  );
}
