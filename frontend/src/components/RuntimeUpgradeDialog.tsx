import { useCallback, useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { isHttpStatus, serverMessageOf } from "../api/errors";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import type { RuntimeUpgradeView } from "../types";
import { AlertTriangleIcon, CheckIcon, TerminalIcon } from "./icons";
import "./confirm-modal.css";
import "./runtime-login.css";

/** A warden that predates the upgrade verb never moves an upgrade past
 * `starting`; this is how long the dialog waits before saying so. */
const RUNTIME_UPGRADE_START_TIMEOUT_MS = 30_000;

type Failure = { kind: "offline" } | { kind: "noResponse" } | { kind: "error"; message: string };

/** Runs `claude update` on one machine through its warden and shows the
 * version before and after. There is no cancel: closing the dialog leaves the
 * upgrade running on the machine. */
export function RuntimeUpgradeDialog({
  machineId,
  machineName,
  onClose,
}: {
  machineId: string;
  machineName: string;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const m = t.monitor.runtimeUpgrade;
  const [attempt, setAttempt] = useState(0);
  const [upgrade, setUpgrade] = useState<RuntimeUpgradeView | null>(null);
  const [failure, setFailure] = useState<Failure | null>(null);
  const upgradeRef = useRef<RuntimeUpgradeView | null>(null);
  upgradeRef.current = upgrade;
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let alive = true;
    setUpgrade(null);
    setFailure(null);
    api
      .startRuntimeUpgrade(machineId, "claude")
      .then((next) => {
        if (alive) setUpgrade(next);
      })
      .catch((e) => {
        if (!alive) return;
        setFailure(
          isHttpStatus(e, 409)
            ? { kind: "offline" }
            : { kind: "error", message: isHttpStatus(e, 422) ? m.startRefused : serverMessageOf(e) || String(e) }
        );
      });
    return () => {
      alive = false;
    };
  }, [machineId, attempt]);

  useEffect(() => {
    const unsubscribe = api.subscribeEvents((topic) => {
      const current = upgradeRef.current;
      if (topic !== "runtime_upgrade" || !current) return;
      api
        .getRuntimeUpgrade(machineId, current.upgradeId)
        .then((next) => {
          if (upgradeRef.current?.upgradeId === next.upgradeId) setUpgrade(next);
        })
        .catch((e) => console.warn("RuntimeUpgradeDialog: refetch failed", e));
    });
    return unsubscribe;
  }, [machineId]);

  const stillStarting = failure === null && (upgrade === null || upgrade.state === "starting");
  useEffect(() => {
    if (!stillStarting) return;
    const timer = window.setTimeout(() => setFailure({ kind: "noResponse" }), RUNTIME_UPGRADE_START_TIMEOUT_MS);
    return () => window.clearTimeout(timer);
  }, [stillStarting, machineId, attempt]);

  const close = useCallback(() => onClose(), [onClose]);
  useEscapeLayer(close, rootRef);

  let body: React.ReactNode;
  let ended = true;
  if (failure?.kind === "offline") {
    body = (
      <Status tone="bad">
        <p className="runtime-login__line runtime-login__line--bad" data-testid="runtime-upgrade-offline">
          {machineName}
          {m.offlineTail}
        </p>
      </Status>
    );
  } else if (failure?.kind === "noResponse") {
    body = (
      <Status tone="bad">
        <div data-testid="runtime-upgrade-no-response">
          <p className="runtime-login__line runtime-login__line--bad">
            {machineName}
            {m.noResponseTail}
          </p>
          <p className="runtime-login__hint">{m.noResponseCauses}</p>
        </div>
      </Status>
    );
  } else if (failure?.kind === "error") {
    body = (
      <Status tone="bad">
        <p className="runtime-login__line runtime-login__line--bad" data-testid="runtime-upgrade-failed">
          {m.failedLead}
          {failure.message}
        </p>
      </Status>
    );
  } else if (upgrade === null || upgrade.state === "starting") {
    ended = false;
    body = (
      <Status tone="busy">
        <p className="runtime-login__line" data-testid="runtime-upgrade-preparing">
          {m.preparingLead}
          {machineName}
          {m.preparingTail}
        </p>
      </Status>
    );
  } else if (upgrade.state === "succeeded") {
    body = (
      <Status tone="good">
        <p className="runtime-login__line runtime-login__line--good" data-testid="runtime-upgrade-succeeded">
          {m.succeededLead}
          {upgrade.fromVersion ?? t.monitor.dash}
          {m.arrow}
          {upgrade.toVersion ?? t.monitor.dash}
        </p>
        <p className="runtime-login__hint">{m.runningMembersHint}</p>
      </Status>
    );
  } else if (upgrade.state === "failed") {
    body = (
      <Status tone="bad">
        <div data-testid="runtime-upgrade-failed">
          <p className="runtime-login__line runtime-login__line--bad">{m.failedHeading}</p>
          {upgrade.reason && (
            <p className="runtime-login__cli" data-testid="runtime-upgrade-reason">
              {m.reasonLead}
              {upgrade.reason}
            </p>
          )}
        </div>
      </Status>
    );
  } else if (upgrade.state === "expired") {
    body = (
      <Status tone="bad">
        <p className="runtime-login__line runtime-login__line--bad" data-testid="runtime-upgrade-failed">
          {m.expired}
        </p>
      </Status>
    );
  } else {
    // `running`, and any state this build does not know: still in flight.
    ended = false;
    body = (
      <Status tone="busy">
        <p className="runtime-login__line" data-testid="runtime-upgrade-running">
          {m.running}
        </p>
        {upgrade.fromVersion && (
          <p className="runtime-login__hint" data-testid="runtime-upgrade-from">
            {m.fromLead}
            {upgrade.fromVersion}
          </p>
        )}
      </Status>
    );
  }

  const succeeded = failure === null && upgrade?.state === "succeeded";
  return (
    <div
      ref={rootRef}
      className="confirm-modal"
      role="dialog"
      aria-modal="true"
      aria-label={m.title}
      data-testid="runtime-upgrade-dialog"
    >
      <div className="confirm-modal__box runtime-login__box">
        <div className="runtime-login__head">
          <span className="runtime-login__mark" aria-hidden="true">
            <TerminalIcon size={17} />
          </span>
          <div className="runtime-login__head-text">
            <div className="runtime-login__title">{m.title}</div>
            <p className="runtime-login__subtitle" data-testid="runtime-upgrade-subtitle">
              {m.subtitleLead}
              {machineName}
              {m.subtitleTail}
            </p>
          </div>
        </div>
        <div className="confirm-modal__body">{body}</div>
        <div className="confirm-modal__actions">
          {ended && !succeeded && (
            <button
              type="button"
              className="confirm-modal__btn confirm-modal__btn--accent"
              data-testid="runtime-upgrade-restart"
              onClick={() => setAttempt((a) => a + 1)}
            >
              {m.restart}
            </button>
          )}
          <button type="button" className="confirm-modal__btn" data-testid="runtime-upgrade-close" onClick={close}>
            {m.close}
          </button>
        </div>
      </div>
    </div>
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
