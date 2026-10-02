import { useState, useEffect, useRef } from "react";
import { useI18n } from "../i18n";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { api } from "../api";
import { formatDuration } from "../lib/duration";
import { useWebhooks } from "../hooks/useWebhooks";
import type {
  WebhookEndpoint,
  WebhookPlatform,
  WebhookRequestLog,
} from "../api/adapter";
import { ConfirmModal } from "./ConfirmModal";
import {
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  CloseIcon,
  CopyIcon,
  MonitorIcon,
  TrashIcon,
} from "./icons";
import "./member-detail.css";

/** 回呼端點 · WEBHOOK — the external inlets bound to one member, staff or
 * outsource alike: list, enable/disable, signing-secret rotation, add, delete,
 * and the per-endpoint 事件統計 window. */
export function WebhooksCard({ memberId }: { memberId: string }) {
  const { t } = useI18n();
  const {
    webhooks,
    error: webhooksError,
    create: createWebhook,
    update: updateWebhook,
    remove: removeWebhook,
  } = useWebhooks(memberId);
  const [showWebhooks, setShowWebhooks] = useState(false);
  const [addingWebhook, setAddingWebhook] = useState(false);
  const [newEndpointId, setNewEndpointId] = useState("");
  const [newPurpose, setNewPurpose] = useState("");
  const [newPlatform, setNewPlatform] =
    useState<WebhookPlatform>("generic");
  const [newSigningSecret, setNewSigningSecret] = useState("");
  const [createWebhookBusy, setCreateWebhookBusy] = useState(false);
  const [createWebhookError, setCreateWebhookError] = useState(false);
  const [copiedToken, setCopiedToken] = useState<string | null>(null);
  const [toggleBusyId, setToggleBusyId] = useState<string | null>(null);
  // Signing-secret rotation, scoped to one endpoint at a time.
  const [rotateSecretId, setRotateSecretId] = useState<string | null>(null);
  const [rotateSecretValue, setRotateSecretValue] = useState("");
  const [rotateSecretBusy, setRotateSecretBusy] = useState(false);
  const [deleteWebhookTarget, setDeleteWebhookTarget] =
    useState<WebhookEndpoint | null>(null);
  const [deleteWebhookBusy, setDeleteWebhookBusy] = useState(false);
  // Per-row 事件統計 popup — stores the endpointId so the window always reads
  // the LIVE endpoint from `webhooks` (counters keep updating while open).
  const [statsEndpointId, setStatsEndpointId] = useState<string | null>(null);
  const statsWebhook =
    statsEndpointId != null
      ? (webhooks.find((wh) => wh.endpointId === statsEndpointId) ?? null)
      : null;
  // The 最近請求 list (server debug ring buffer, last 5 raw requests) is
  // fetched ONLY while the window is open: null = loading. One row at a time
  // expands to its raw headers + body.
  const [statsRequests, setStatsRequests] = useState<
    WebhookRequestLog[] | null
  >(null);
  const [statsRequestsError, setStatsRequestsError] = useState(false);
  const [expandedRequest, setExpandedRequest] = useState<number | null>(null);
  // The stats window lives inside a still-mounted panel, so it holds a layer
  // only while it is open.
  const statsModalRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(
    () => setStatsEndpointId(null),
    statsModalRef,
    statsEndpointId != null,
  );
  useEffect(() => {
    setStatsRequests(null);
    setStatsRequestsError(false);
    setExpandedRequest(null);
    if (statsEndpointId == null) return;
    let alive = true;
    api
      .listWebhookRequests(memberId, statsEndpointId)
      .then((rows) => {
        if (alive) setStatsRequests(rows);
      })
      .catch(() => {
        if (alive) setStatsRequestsError(true);
      });
    return () => {
      alive = false;
    };
  }, [statsEndpointId, memberId]);

  // The full callback URL the copy button yields (masked visually in the row).
  // Composed from the page origin so it stays portable across the tunnel host.
  function webhookUrl(token: string): string {
    return `${window.location.origin}/in?t=${token}`;
  }
  // The 事件統計 window (M4 可觀測性) helpers: an endpoint that has never seen
  // a request reads a friendly empty face; otherwise the top half renders
  // two stat blocks (last received / dropped + reason badge)
  // and the bottom half the raw 最近請求 ring buffer.
  function webhookNeverReceived(wh: WebhookEndpoint): boolean {
    return (
      wh.lastReceivedTs <= 0 &&
      wh.deliveredCount === 0 &&
      wh.droppedCount === 0
    );
  }
  function webhookDropReasonLabel(reason: string): string {
    return reason === "sig_failed"
      ? t.mp.webhook.dropReasonSigFailed
      : reason === "disabled"
        ? t.mp.webhook.dropReasonDisabled
        : reason === "member_gone"
          ? t.mp.webhook.dropReasonMemberGone
          : reason;
  }
  // Outcome → badge tone + label. Drops carry their coarse reason as
  // "dropped:<reason>" — the badge shows 丟棄 · <reason label>.
  function webhookOutcomeTone(
    outcome: string
  ): "delivered" | "dropped" | "neutral" {
    if (outcome === "delivered") return "delivered";
    if (outcome.startsWith("dropped:") || outcome === "dropped")
      return "dropped";
    return "neutral";
  }
  function webhookOutcomeLabel(outcome: string): string {
    if (outcome === "delivered") return t.mp.webhook.outcomeDelivered;
    if (outcome === "challenge") return t.mp.webhook.outcomeChallenge;
    if (outcome === "ping") return t.mp.webhook.outcomePing;
    if (outcome.startsWith("dropped:")) {
      const reason = outcome.slice("dropped:".length);
      return `${t.mp.webhook.outcomeDropped} · ${webhookDropReasonLabel(reason)}`;
    }
    return outcome;
  }
  // Raw header JSON → aligned "Name: value" lines for the expanded request
  // (falls back to the raw string when it isn't the expected JSON map).
  function webhookHeaderLines(headers: string): string {
    try {
      const parsed: unknown = JSON.parse(headers);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        return Object.entries(parsed as Record<string, unknown>)
          .map(
            ([k, v]) =>
              `${k}: ${Array.isArray(v) ? v.join(", ") : String(v)}`
          )
          .join("\n");
      }
    } catch {
      // truncated / non-JSON headers — show the raw text honestly
    }
    return headers;
  }

  function webhookHostPath(): string {
    try {
      return `${window.location.host}/in?t=`;
    } catch {
      return "/in?t=";
    }
  }

  async function copyWebhook(token: string) {
    try {
      await navigator.clipboard.writeText(webhookUrl(token));
      setCopiedToken(token);
      window.setTimeout(
        () => setCopiedToken((cur) => (cur === token ? null : cur)),
        1600
      );
    } catch {
      // clipboard unavailable — no fake success
    }
  }

  function resetWebhookForm() {
    setAddingWebhook(false);
    setNewEndpointId("");
    setNewPurpose("");
    setNewPlatform("generic");
    setNewSigningSecret("");
    setCreateWebhookError(false);
  }

  // slack/github require a signing secret; generic never shows the field.
  const newPlatformNeedsSecret =
    newPlatform === "slack" || newPlatform === "github";
  const createWebhookDisabled =
    createWebhookBusy ||
    newEndpointId.trim() === "" ||
    (newPlatformNeedsSecret && newSigningSecret.trim() === "");

  async function submitCreateWebhook() {
    const endpointId = newEndpointId.trim();
    if (!endpointId || createWebhookBusy) return;
    if (newPlatformNeedsSecret && newSigningSecret.trim() === "") return;
    setCreateWebhookBusy(true);
    setCreateWebhookError(false);
    try {
      await createWebhook({
        endpointId,
        purpose: newPurpose.trim(),
        platform: newPlatform,
        // Only send the secret for platforms that use it (generic ignores it).
        ...(newPlatformNeedsSecret
          ? { signingSecret: newSigningSecret.trim() }
          : {}),
      });
      resetWebhookForm();
    } catch {
      setCreateWebhookError(true);
    } finally {
      setCreateWebhookBusy(false);
    }
  }

  function startRotateSecret(endpointId: string) {
    setRotateSecretId(endpointId);
    setRotateSecretValue("");
  }

  function cancelRotateSecret() {
    setRotateSecretId(null);
    setRotateSecretValue("");
  }

  async function submitRotateSecret(endpointId: string) {
    const secret = rotateSecretValue.trim();
    if (!secret || rotateSecretBusy) return;
    setRotateSecretBusy(true);
    try {
      await updateWebhook(endpointId, { signingSecret: secret });
      cancelRotateSecret();
    } catch {
      // a refetch keeps truth; the row stays on its prior state
    } finally {
      setRotateSecretBusy(false);
    }
  }

  async function toggleWebhook(e: WebhookEndpoint) {
    if (toggleBusyId) return;
    setToggleBusyId(e.endpointId);
    try {
      await updateWebhook(e.endpointId, {
        status: e.status === "enabled" ? "disabled" : "enabled",
      });
    } catch {
      // surfaced by the list staying on its prior state; a refetch keeps truth
    } finally {
      setToggleBusyId(null);
    }
  }

  async function confirmDeleteWebhook() {
    if (!deleteWebhookTarget) return;
    setDeleteWebhookBusy(true);
    try {
      await removeWebhook(deleteWebhookTarget.endpointId);
      setDeleteWebhookTarget(null);
    } finally {
      setDeleteWebhookBusy(false);
    }
  }

  return (
    <>
      {/* expandable: webhook endpoints (M4 回呼端點) */}
      <div className="mp-card mp-expand mp-webhook">
        <button
          type="button"
          className="mp-expand__head"
          aria-expanded={showWebhooks}
          onClick={() => setShowWebhooks((v) => !v)}
          data-testid="mp-webhook-toggle"
        >
          <MonitorIcon size={15} className="mp-expand__icon" />
          <span className="mp-expand__title">{t.mp.webhook.title}</span>
          {webhooks.length > 0 && (
            <span className="mp-webhook__count">{webhooks.length}</span>
          )}
          {showWebhooks ? (
            <ChevronDownIcon size={16} className="mp-expand__chevron" />
          ) : (
            <ChevronRightIcon size={16} className="mp-expand__chevron" />
          )}
        </button>
        {showWebhooks && (
          <div className="mp-expand__body mp-webhook__body">
            {webhooksError ? (
              <div className="mp-webhook__error">{t.mp.webhook.loadError}</div>
            ) : (
              <>
                {webhooks.length === 0 && !addingWebhook && (
                  <div className="mp-webhook__empty">{t.mp.webhook.empty}</div>
                )}
                {webhooks.map((wh) => {
                  const on = wh.status === "enabled";
                  return (
                    <div className="mp-webhook__row" key={wh.endpointId}>
                      <div className="mp-webhook__rowhead">
                        <span
                          className={`mp-webhook__dot mp-webhook__dot--${on ? "on" : "off"}`}
                        />
                        <code className="mp-webhook__chip">{wh.endpointId}</code>
                        {/* T-069d: the row-head entry reads a constant 事件統計
                            label — owner asked to keep the numbers out of the
                            row; clicking it still opens the full window. */}
                        <button
                          type="button"
                          className="mp-webhook__statssummary"
                          onClick={() => setStatsEndpointId(wh.endpointId)}
                          data-testid={`mp-webhook-stats-${wh.endpointId}`}
                        >
                          {t.mp.webhook.statsTitle}
                        </button>
                        <span className="mp-webhook__spacer" />
                        <span className="mp-webhook__statusword">
                          {on ? t.mp.webhook.enabled : t.mp.webhook.disabled}
                        </span>
                        <button
                          type="button"
                          role="switch"
                          aria-checked={on}
                          aria-label={t.mp.webhook.title + " " + wh.endpointId}
                          disabled={toggleBusyId === wh.endpointId}
                          className={`mp-toggle${on ? " mp-toggle--on" : ""}`}
                          onClick={() => toggleWebhook(wh)}
                          data-testid={`mp-webhook-toggle-${wh.endpointId}`}
                        >
                          <span className="mp-toggle__knob" />
                        </button>
                      </div>
                      {wh.purpose && (
                        <div className="mp-webhook__purpose">{wh.purpose}</div>
                      )}
                      <div className="mp-webhook__urlrow">
                        <code className="mp-webhook__url" title={t.mp.webhook.copy}>
                          {webhookHostPath()}
                          <span className="mp-webhook__mask">
                            {"•".repeat(12)}
                          </span>
                        </code>
                        <button
                          type="button"
                          className="btn mp-webhook__copy"
                          onClick={() => copyWebhook(wh.token)}
                        >
                          {copiedToken === wh.token ? (
                            <CheckIcon size={14} />
                          ) : (
                            <CopyIcon size={14} />
                          )}
                          <span>
                            {copiedToken === wh.token
                              ? t.mp.webhook.copied
                              : t.mp.webhook.copy}
                          </span>
                        </button>
                        <button
                          type="button"
                          className="mp-webhook__delete"
                          aria-label={t.mp.webhook.deleteLabel}
                          onClick={() => setDeleteWebhookTarget(wh)}
                          data-testid={`mp-webhook-delete-${wh.endpointId}`}
                        >
                          <TrashIcon size={15} />
                        </button>
                      </div>
                      {/* signing-secret rotation — only for platforms that use it */}
                      {wh.platform !== "generic" &&
                        (rotateSecretId === wh.endpointId ? (
                          <div className="mp-webhook__rotate">
                            <label className="mp-webhook__field">
                              <span className="mp-webhook__fieldlabel">
                                {t.mp.webhook.signingSecretLabel}
                              </span>
                              <input
                                type="password"
                                className="mp-webhook__input"
                                placeholder={
                                  t.mp.webhook.signingSecretPlaceholder
                                }
                                value={rotateSecretValue}
                                onChange={(e) =>
                                  setRotateSecretValue(e.target.value)
                                }
                                autoFocus
                                data-testid={`mp-webhook-rotate-input-${wh.endpointId}`}
                              />
                            </label>
                            <div className="mp-webhook__formactions">
                              <button
                                type="button"
                                className="btn"
                                onClick={cancelRotateSecret}
                                disabled={rotateSecretBusy}
                              >
                                {t.mp.webhook.cancel}
                              </button>
                              <button
                                type="button"
                                className="btn mp-webhook__submit"
                                onClick={() => submitRotateSecret(wh.endpointId)}
                                disabled={
                                  rotateSecretBusy ||
                                  rotateSecretValue.trim() === ""
                                }
                                data-testid={`mp-webhook-rotate-save-${wh.endpointId}`}
                              >
                                {t.mp.webhook.rotateSecretSave}
                              </button>
                            </div>
                          </div>
                        ) : (
                          <button
                            type="button"
                            className="mp-webhook__rotatelink"
                            onClick={() => startRotateSecret(wh.endpointId)}
                            data-testid={`mp-webhook-rotate-${wh.endpointId}`}
                          >
                            {t.mp.webhook.rotateSecret}
                          </button>
                        ))}
                    </div>
                  );
                })}

                {addingWebhook ? (
                  <div className="mp-webhook__form">
                    <label className="mp-webhook__field">
                      <span className="mp-webhook__fieldlabel">
                        {t.mp.webhook.endpointIdLabel}
                      </span>
                      <input
                        type="text"
                        className="mp-webhook__input"
                        placeholder={t.mp.webhook.endpointIdPlaceholder}
                        value={newEndpointId}
                        onChange={(e) => setNewEndpointId(e.target.value)}
                        autoFocus
                      />
                    </label>
                    <label className="mp-webhook__field">
                      <span className="mp-webhook__fieldlabel">
                        {t.mp.webhook.purposeLabel}
                      </span>
                      <input
                        type="text"
                        className="mp-webhook__input"
                        placeholder={t.mp.webhook.purposePlaceholder}
                        value={newPurpose}
                        onChange={(e) => setNewPurpose(e.target.value)}
                      />
                    </label>
                    <label className="mp-webhook__field">
                      <span className="mp-webhook__fieldlabel">
                        {t.mp.webhook.platformLabel}
                      </span>
                      <select
                        className="mp-webhook__input mp-webhook__select"
                        value={newPlatform}
                        onChange={(e) =>
                          setNewPlatform(e.target.value as WebhookPlatform)
                        }
                        data-testid="mp-webhook-platform-select"
                      >
                        <option value="generic">
                          {t.mp.webhook.platformGeneric}
                        </option>
                        <option value="slack">
                          {t.mp.webhook.platformSlack}
                        </option>
                        <option value="github">
                          {t.mp.webhook.platformGithub}
                        </option>
                      </select>
                    </label>
                    {newPlatformNeedsSecret && (
                      <>
                        <label className="mp-webhook__field">
                          <span className="mp-webhook__fieldlabel">
                            {t.mp.webhook.signingSecretLabel}
                            <span
                              className="mp-webhook__required"
                              aria-hidden="true"
                            >
                              {" *"}
                            </span>
                          </span>
                          <input
                            type="password"
                            className="mp-webhook__input"
                            placeholder={t.mp.webhook.signingSecretPlaceholder}
                            value={newSigningSecret}
                            onChange={(e) =>
                              setNewSigningSecret(e.target.value)
                            }
                            required
                            aria-required="true"
                            data-testid="mp-webhook-secret-input"
                          />
                        </label>
                        <div className="mp-webhook__helper">
                          {newPlatform === "slack"
                            ? t.mp.webhook.helperSlack
                            : t.mp.webhook.helperGithub}
                        </div>
                        {newSigningSecret.trim() === "" && (
                          <div className="mp-webhook__hint">
                            {t.mp.webhook.signingSecretRequired}
                          </div>
                        )}
                      </>
                    )}
                    {createWebhookError && (
                      <div className="mp-webhook__error">
                        {t.mp.webhook.createError}
                      </div>
                    )}
                    <div className="mp-webhook__formactions">
                      <button
                        type="button"
                        className="btn"
                        onClick={resetWebhookForm}
                        disabled={createWebhookBusy}
                      >
                        {t.mp.webhook.cancel}
                      </button>
                      <button
                        type="button"
                        className="btn mp-webhook__submit"
                        onClick={submitCreateWebhook}
                        disabled={createWebhookDisabled}
                        data-testid="mp-webhook-create"
                      >
                        {t.mp.webhook.create}
                      </button>
                    </div>
                  </div>
                ) : (
                  <button
                    type="button"
                    className="mp-webhook__add"
                    onClick={() => setAddingWebhook(true)}
                    data-testid="mp-webhook-add"
                  >
                    + {t.mp.webhook.add}
                  </button>
                )}
              </>
            )}
          </div>
        )}
      </div>

      {deleteWebhookTarget && (
        <ConfirmModal
          body={t.mp.webhook.deleteConfirm}
          cancelLabel={t.mp.webhook.cancel}
          confirmLabel={t.mp.webhook.deleteLabel}
          danger
          busy={deleteWebhookBusy}
          onCancel={() => setDeleteWebhookTarget(null)}
          onConfirm={confirmDeleteWebhook}
          testId="mp-webhook-delete-confirm"
        />
      )}

      {/* 事件統計 window — read-only observability for ONE endpoint, opened
          from the per-row link: two stat blocks up top (never-received face
          when the endpoint has no traffic at all), the raw 最近請求 ring
          buffer below (one row expands to its headers + body). Closes via ✕,
          Esc, or clicking the dimmed backdrop. T-2c1c: no endpoint chip in
          the title (the modal opens from that row — repeating it adds no
          information) and no delivered tile. */}
      {statsWebhook && (
        <div
          ref={statsModalRef}
          className="mp-webhook__statsmodal"
          role="dialog"
          aria-modal="true"
          aria-label={t.mp.webhook.statsTitle}
          data-testid="mp-webhook-stats-modal"
          onClick={() => setStatsEndpointId(null)}
        >
          <div
            className="mp-webhook__statsbox"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="mp-webhook__statshead">
              <span className="mp-webhook__statstitle">
                {t.mp.webhook.statsTitle}
              </span>
              <span className="mp-webhook__spacer" />
              <button
                type="button"
                className="mp-webhook__statsclose"
                aria-label={t.mp.webhook.statsClose}
                onClick={() => setStatsEndpointId(null)}
                data-testid="mp-webhook-stats-close"
              >
                <CloseIcon size={15} />
              </button>
            </div>
            <div
              className="mp-webhook__statsbody"
              data-testid="mp-webhook-stats-body"
            >
              {webhookNeverReceived(statsWebhook) ? (
                <div className="mp-webhook__statsempty">
                  <div className="mp-webhook__statsempty-title">
                    {t.mp.webhook.statsNever}
                  </div>
                  <div className="mp-webhook__statsempty-hint">
                    {t.mp.webhook.statsNeverHint}
                  </div>
                </div>
              ) : (
                <>
                  <div className="mp-webhook__statsgrid">
                    <div className="mp-webhook__stat">
                      <span className="mp-webhook__statlabel">
                        {t.mp.webhook.statsLastReceivedLabel}
                      </span>
                      <span className="mp-webhook__statvalue">
                        {statsWebhook.lastReceivedTs > 0
                          ? t.mp.webhook.statsAgo(
                              formatDuration(
                                Date.now() / 1000 - statsWebhook.lastReceivedTs
                              )
                            )
                          : t.mp.dash}
                      </span>
                    </div>
                    <div className="mp-webhook__stat">
                      <span className="mp-webhook__statlabel">
                        {t.mp.webhook.statsDroppedLabel}
                      </span>
                      <span
                        className={`mp-webhook__statvalue${
                          statsWebhook.droppedCount > 0
                            ? " mp-webhook__statvalue--dropped"
                            : ""
                        }`}
                      >
                        {statsWebhook.droppedCount}
                      </span>
                      {statsWebhook.droppedCount > 0 &&
                        statsWebhook.lastDropReason && (
                          <span className="mp-webhook__statnote">
                            {webhookDropReasonLabel(
                              statsWebhook.lastDropReason
                            )}
                          </span>
                        )}
                    </div>
                  </div>
                  <div
                    className="mp-webhook__requests"
                    data-testid="mp-webhook-requests"
                  >
                    <div className="mp-webhook__requeststitle">
                      {t.mp.webhook.requestsTitle}
                    </div>
                    {statsRequestsError ? (
                      <div className="mp-webhook__requestsnote">
                        {t.mp.webhook.requestsError}
                      </div>
                    ) : statsRequests == null ? (
                      <div className="mp-webhook__requestsnote">
                        {t.mp.webhook.requestsLoading}
                      </div>
                    ) : statsRequests.length === 0 ? (
                      <div className="mp-webhook__requestsnote">
                        {t.mp.webhook.requestsEmpty}
                      </div>
                    ) : (
                      <ul className="mp-webhook__requestlist">
                        {statsRequests.map((req, i) => (
                          <li className="mp-webhook__request" key={i}>
                            <button
                              type="button"
                              className="mp-webhook__requestrow"
                              aria-expanded={expandedRequest === i}
                              onClick={() =>
                                setExpandedRequest(
                                  expandedRequest === i ? null : i
                                )
                              }
                              data-testid={`mp-webhook-request-${i}`}
                            >
                              <span
                                className={`mp-webhook__outcome mp-webhook__outcome--${webhookOutcomeTone(req.outcome)}`}
                              >
                                {webhookOutcomeLabel(req.outcome)}
                              </span>
                              <span className="mp-webhook__requesttime">
                                {t.mp.webhook.statsAgo(
                                  formatDuration(Date.now() / 1000 - req.ts)
                                )}
                              </span>
                              <span className="mp-webhook__spacer" />
                              {req.truncated && (
                                <span className="mp-webhook__requesttrunc">
                                  {t.mp.webhook.requestTruncated}
                                </span>
                              )}
                              {expandedRequest === i ? (
                                <ChevronDownIcon
                                  size={14}
                                  className="mp-webhook__requestchevron"
                                />
                              ) : (
                                <ChevronRightIcon
                                  size={14}
                                  className="mp-webhook__requestchevron"
                                />
                              )}
                            </button>
                            {expandedRequest === i && (
                              <div
                                className="mp-webhook__requestdetail"
                                data-testid={`mp-webhook-request-detail-${i}`}
                              >
                                <div className="mp-webhook__requestsection">
                                  {t.mp.webhook.requestHeaders}
                                </div>
                                <pre className="mp-webhook__requestpre">
                                  {webhookHeaderLines(req.headers)}
                                </pre>
                                <div className="mp-webhook__requestsection">
                                  {t.mp.webhook.requestBody}
                                </div>
                                <pre className="mp-webhook__requestpre">
                                  {req.body || t.mp.webhook.requestBodyEmpty}
                                </pre>
                              </div>
                            )}
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  );
}
