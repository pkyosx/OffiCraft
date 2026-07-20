import { useEffect, useRef, useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
import { formatCost } from "../lib/cost";
import { ModelEffortEditor } from "./ModelEffortEditor";
import { Markdown } from "./Markdown";
import {
  CheckIcon,
  ChevronDownIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  CopyIcon,
  FileTextIcon,
  PencilIcon,
} from "./icons";
import "./member-detail.css";

/** The lazily-fetched initial-prompt expand card's config. `fetch` returns the
 * CURRENT boot/persona text (a preview — never a token); it is re-fetched when
 * `cacheKey` changes (member: the role; worker: the worker id). `note` is an
 * optional honesty caveat rendered above the markdown (the worker's 「目前版本
 * 重組,非派工當下逐字版」). */
export interface AgentDetailPrompt {
  fetch: () => Promise<string>;
  cacheKey: string;
  hint: string;
  note?: string;
}

/** The ONE view model both detail pages project into (T-ba6b convergence —
 * owner constitution: 「外包只是一個系統會幫我產生跟刪除的正職員工」, so both
 * kinds render through the SAME cards). Every display value arrives ALREADY
 * resolved + gated by the wrapper (machine display name, readable account or
 * "" — internal identifiers never reach this component); "" / null render the
 * honest dash, never a fabricated value. */
export interface AgentDetailVM {
  /** data-testid prefix ("mp" for the member page, "worker-detail" for the
   * outsource page) — keeps each page's existing stable test surface. */
  testIdPrefix: string;
  /** True while the agent's session is really up — gates the refocus button
   * (the server 409s an offline refocus on both kinds). */
  online: boolean;
  model: string;
  effort: string;
  /** The note under the model/effort editor (member: next-wake semantics;
   * worker: active-respawn semantics). */
  modelEffortNote: string;
  /** Persist a model/effort edit. Undefined ⇒ the cell is read-only. */
  onSaveModelEffort?: (model: string, effort: string) => Promise<void>;
  /** Resolved machine display text; "" ⇒ dash. Wrappers apply their own gate
   * (member: awake-only; worker: 尚未分配 fallback text). */
  machineText: string;
  /** Optional action next to the 機器 label (the worker's 改機器 button). */
  machineAction?: ReactNode;
  /** Readable Claude account name; "" ⇒ dash — NEVER a raw credential key
   * (the server already resolves alias/label or nulls it, T-ba6b). */
  accountText: string;
  contextPct: number | null;
  cost: number | null;
  onRefocus?: () => Promise<void>;
  refocusSince: number | null;
  refocusSubmittedNote: string;
  refocusSinceLabel: (t: string) => string;
  lastOp: string;
  lastOpVerb: string;
  lastOpOk: boolean | null;
  lastOpLog: string;
  lastOpReason: string;
  lastOpAt: number | null;
  tmuxSession: string;
  terminalHint: string;
  prompt?: AgentDetailPrompt;
}

interface AgentDetailPanelProps {
  vm: AgentDetailVM;
  onBack: () => void;
  /** The kind-specific identity card (member: avatar + rename + presence +
   * action buttons; worker: briefcase + codename + task chip). */
  identity: ReactNode;
  /** Modal-ish overlays (machine pickers, confirms) — rendered right after
   * the identity card, same as both panels always did. */
  overlays?: ReactNode;
  /** Pluggable cards between overlays and the 模型/機器 info card (worker:
   * 委託任務, T-b0e3 — owner wants it above 模型/機器, not buried after 最近操作).
   * Undefined ⇒ renders nothing, so the member page (no caller passes this) is
   * unaffected. */
  afterIdentityCards?: ReactNode;
  /** Pluggable cards between the info card and the runtime card (worker:
   * 狀態 + 委託人). */
  afterInfoCards?: ReactNode;
  /** Pluggable cards between the 最近操作 card and the terminal card. Unused by
   * the worker panel since T-b0e3 (委託任務 moved to afterIdentityCards); kept
   * for any future kind-specific card that belongs after 最近操作. */
  beforeTerminalCards?: ReactNode;
  /** Pluggable expand cards after the terminal card, BEFORE the initial-prompt
   * card (member: 回呼端點 webhook). */
  extraExpandCards?: ReactNode;
}

/**
 * The ONE detail panel both the member page and the outsource-worker page
 * render through (card order = the member panel's, the convergence baseline):
 * back → identity (slot) → 模型/投入度 | 機器/Claude Account → runtime
 * (context% + est.$ + 換手) → 最近操作 → terminal → expand cards (slot +
 * initial prompt). Kind-specific content plugs in through the slots; the
 * shared cards read only the unified view model.
 */
export function AgentDetailPanel({
  vm,
  onBack,
  identity,
  overlays,
  afterIdentityCards,
  afterInfoCards,
  beforeTerminalCards,
  extraExpandCards,
}: AgentDetailPanelProps) {
  const { t } = useI18n();
  const dash = t.mp.dash;
  const p = vm.testIdPrefix;

  // ── model / effort editing (shared quick-pick editor; persistence is the
  // wrapper's — member PATCHes the member, worker POSTs the model op) ────────
  const [meEditing, setMeEditing] = useState(false);
  const [meModel, setMeModel] = useState("");
  const [meEffort, setMeEffort] = useState("medium");
  const [meBusy, setMeBusy] = useState(false);
  const [meError, setMeError] = useState(false);
  const [meOverride, setMeOverride] = useState<{
    model: string;
    effort: string;
  } | null>(null);
  const shownModel = meOverride?.model ?? vm.model;
  const shownEffort = meOverride?.effort ?? vm.effort;
  // Known effort levels render 中文字 + the raw key (the member page's format,
  // now the ONE format); an unknown/custom effort string renders verbatim.
  const effortLevelText =
    shownEffort === "low" || shownEffort === "medium" || shownEffort === "high"
      ? t.mp.effortLevel(shownEffort)
      : null;

  function startMeEdit() {
    setMeModel(shownModel);
    setMeEffort(shownEffort || "medium");
    setMeError(false);
    setMeEditing(true);
  }

  async function saveMeEdit() {
    if (!vm.onSaveModelEffort) return;
    setMeBusy(true);
    setMeError(false);
    try {
      await vm.onSaveModelEffort(meModel.trim(), meEffort);
      setMeOverride({ model: meModel.trim(), effort: meEffort });
      setMeEditing(false);
    } catch {
      setMeError(true);
    } finally {
      setMeBusy(false);
    }
  }

  // ── refocus pulse (in-flight → persistent done / transient error) ──────────
  const [refocusState, setRefocusState] = useState<
    "idle" | "pending" | "done" | "error"
  >("idle");
  const refocusTimer = useRef<number | null>(null);
  useEffect(
    () => () => {
      if (refocusTimer.current) window.clearTimeout(refocusTimer.current);
    },
    []
  );
  async function handleRefocus() {
    if (!vm.onRefocus || refocusState === "pending") return;
    if (refocusTimer.current) window.clearTimeout(refocusTimer.current);
    setRefocusState("pending");
    try {
      await vm.onRefocus();
      // DONE PERSISTS: the POST only writes the intent — the compaction /
      // respawn runs asynchronously with no instant visible change, so a
      // persistent "sent" note is the honest acknowledgement.
      setRefocusState("done");
    } catch {
      setRefocusState("error");
      refocusTimer.current = window.setTimeout(
        () => setRefocusState("idle"),
        1800
      );
    }
  }
  const refocusPending = refocusState === "pending";
  const refocusLabel =
    refocusState === "pending"
      ? t.mp.refocusing
      : refocusState === "done"
        ? t.mp.refocusDone
        : refocusState === "error"
          ? t.mp.refocusError
          : t.mp.refocus;
  const refocusSinceText =
    vm.refocusSince != null
      ? new Date(vm.refocusSince * 1000).toLocaleString()
      : null;

  // ── 最近操作 (last warden receipt) ─────────────────────────────────────────
  const hasLastOp = vm.lastOp !== "" && vm.lastOpAt != null;
  const lastOpAtText =
    vm.lastOpAt != null ? new Date(vm.lastOpAt * 1000).toLocaleString() : null;
  const [showLastOpLog, setShowLastOpLog] = useState(false);
  const lastOpReason = (vm.lastOpReason ?? "").trim();
  const lastOpLog = (vm.lastOpLog ?? "").trim();

  // ── terminal copy ──────────────────────────────────────────────────────────
  const [copied, setCopied] = useState(false);
  async function copyTmux() {
    const cmd = `tmux -L officraft attach -t ${vm.tmuxSession}`;
    try {
      await navigator.clipboard.writeText(cmd);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      // clipboard unavailable — no fake success
    }
  }

  // ── initial prompt (lazy fetch on first expand; re-fetch on cacheKey) ─────
  const [showPrompt, setShowPrompt] = useState(false);
  const [prompt, setPrompt] = useState<{
    text: string;
    loading: boolean;
    error: boolean;
  }>({ text: "", loading: false, error: false });
  const loadedKeyRef = useRef<string | null>(null);
  const promptFetch = vm.prompt?.fetch;
  const promptKey = vm.prompt?.cacheKey;
  useEffect(() => {
    if (!showPrompt || !promptFetch || promptKey == null) return;
    if (loadedKeyRef.current === promptKey) return;
    let alive = true;
    loadedKeyRef.current = promptKey;
    setPrompt({ text: "", loading: true, error: false });
    promptFetch()
      .then((text) => {
        if (alive) setPrompt({ text, loading: false, error: false });
      })
      .catch(() => {
        if (!alive) return;
        loadedKeyRef.current = null; // allow a retry on re-expand
        setPrompt({ text: "", loading: false, error: true });
      });
    return () => {
      alive = false;
    };
  }, [showPrompt, promptFetch, promptKey]);

  const contextText = vm.contextPct != null ? `${vm.contextPct}%` : dash;
  const costText = vm.cost != null ? formatCost(vm.cost) : dash;

  return (
    <div className="mp">
      <button type="button" className="mp__back" onClick={onBack}>
        <ChevronLeftIcon size={18} />
        <span>{t.mp.back}</span>
      </button>

      {identity}
      {overlays}
      {afterIdentityCards}

      {/* info card: LEFT 模型 + 投入度 (editable launch intents), RIGHT 機器 +
       * Claude Account — the member page's mp-info2 layout, now the ONE layout. */}
      <div className="mp-card mp-info2">
        <div className="mp-field" data-testid={`${p}-model-effort-cell`}>
          {!meEditing ? (
            <>
              <div className="mp-field__head">
                <div className="mp-field__label">{t.mp.model}</div>
                {vm.onSaveModelEffort && (
                  <button
                    type="button"
                    className="doc-btn doc-btn--edit"
                    data-testid={`${p}-model-effort-edit`}
                    onClick={startMeEdit}
                  >
                    <PencilIcon size={14} />
                    <span>{t.settings.edit}</span>
                  </button>
                )}
              </div>
              <div className="mp-field__value">{shownModel || dash}</div>
              <div className="mp-field__label mp-field__label--stacked">
                {t.mp.effort}
              </div>
              <div className="mp-field__value">
                {effortLevelText != null ? (
                  <>
                    {effortLevelText}{" "}
                    <span className="mp-field__hint">({shownEffort})</span>
                  </>
                ) : (
                  shownEffort || dash
                )}
              </div>
            </>
          ) : (
            <div
              className="mp-info2__editor"
              data-testid={`${p}-model-effort-editor`}
            >
              <ModelEffortEditor
                model={meModel}
                effort={meEffort}
                onModelChange={setMeModel}
                onEffortChange={setMeEffort}
              />
              <div className="mp-field__hint">{vm.modelEffortNote}</div>
              {meError && (
                <div className="mp-field__hint mp-info2__error">
                  {t.mp.modelEffortError}
                </div>
              )}
              <div className="mp-info2__actions">
                <button
                  type="button"
                  className="doc-btn"
                  disabled={meBusy}
                  onClick={() => setMeEditing(false)}
                >
                  {t.mp.modelEffortCancel}
                </button>
                <button
                  type="button"
                  className="doc-btn doc-btn--accent"
                  disabled={meBusy}
                  onClick={() => void saveMeEdit()}
                  data-testid={`${p}-model-effort-save`}
                >
                  {t.mp.modelEffortSave}
                </button>
              </div>
            </div>
          )}
        </div>
        <div className="mp-field mp-field--divider">
          <div className="mp-field__head">
            <div className="mp-field__label">{t.mp.machine}</div>
            {vm.machineAction}
          </div>
          <div className="mp-field__value" data-testid={`${p}-machine`}>
            {vm.machineText || dash}
          </div>
          <div className="mp-field__label mp-field__label--stacked">
            {t.mp.claudeAccount}
          </div>
          <div className="mp-field__value" data-testid={`${p}-account`}>
            {vm.accountText || dash}
          </div>
        </div>
      </div>

      {afterInfoCards}

      {/* runtime card: context% + est.$ + 換手 */}
      <div className="mp-card mp-runtime">
        <div className="mp-runtime__head">
          <span className="mp-card__title">{t.mp.runtime}</span>
        </div>
        <div className="mp-runtime__cells">
          <div className="mp-cell">
            <div className="mp-cell__head">
              <span className="mp-cell__label">🧠 {t.mp.context}</span>
              <button
                type="button"
                className={`mp-refocus mp-refocus--${refocusState}`}
                data-testid={`${p}-refocus`}
                disabled={!vm.online || refocusPending || !vm.onRefocus}
                title={vm.online ? t.mp.refocus : t.mp.refocusOfflineHint}
                onClick={() => void handleRefocus()}
              >
                {refocusLabel}
              </button>
            </div>
            <div className="mp-cell__value" data-testid={`${p}-context`}>
              {contextText}
            </div>
          </div>
          <div className="mp-cell">
            <div className="mp-cell__head">
              <span className="mp-cell__label">💲 {t.mp.estimatedCost}</span>
            </div>
            <div className="mp-cell__value" data-testid={`${p}-cost`}>
              {costText}
            </div>
          </div>
        </div>
        {refocusState === "done" && (
          <div className="mp-runtime__note" data-testid={`${p}-refocus-note`}>
            {vm.refocusSubmittedNote}
          </div>
        )}
        {refocusSinceText && (
          <div className="mp-runtime__note mp-runtime__note--muted">
            {vm.refocusSinceLabel(refocusSinceText)}
          </div>
        )}
        {!vm.online && (
          <div className="mp-runtime__note mp-runtime__note--muted">
            {t.mp.refocusOfflineHint}
          </div>
        )}
      </div>

      {/* 最近操作 (last warden op receipt) — only once a real op reported. */}
      {hasLastOp && (
        <div className="mp-card mp-lastop">
          <div className="mp-card__title">{t.mp.lastOp}</div>
          <div
            className={`mp-lastop__head mp-lastop__head--${
              vm.lastOpOk ? "ok" : "fail"
            }`}
          >
            <span className="mp-lastop__icon" aria-hidden="true">
              {vm.lastOpOk ? "✓" : "✗"}
            </span>
            <span className="mp-lastop__verb">{vm.lastOpVerb}</span>
            <span className="mp-lastop__result">
              {vm.lastOpOk ? t.mp.lastOpOk : t.mp.lastOpFail}
            </span>
            {lastOpAtText && (
              <span className="mp-lastop__at">· {lastOpAtText}</span>
            )}
          </div>
          {/* On failure surface the structured REASON first — a bare「✕ 啟動
              失敗」tells the owner nothing; absent reason renders status-only
              (honest, never fabricated). */}
          {!vm.lastOpOk && lastOpReason && (
            <div
              className="mp-lastop__reason"
              data-testid={`${p}-lastop-reason`}
            >
              {vm.lastOpReason}
            </div>
          )}
          {!vm.lastOpOk && lastOpLog && lastOpLog !== lastOpReason && (
            <div className="mp-lastop__logwrap">
              <button
                type="button"
                className="mp-lastop__toggle"
                aria-expanded={showLastOpLog}
                onClick={() => setShowLastOpLog((v) => !v)}
              >
                {showLastOpLog ? (
                  <ChevronDownIcon size={14} />
                ) : (
                  <ChevronRightIcon size={14} />
                )}
                <span>{t.mp.lastOpLogLabel}</span>
              </button>
              {showLastOpLog && (
                <pre className="mp-lastop__log">{vm.lastOpLog}</pre>
              )}
            </div>
          )}
        </div>
      )}

      {beforeTerminalCards}

      {/* terminal / tmux */}
      <div className="mp-card mp-terminal">
        <div className="mp-card__title mp-terminal__title">{t.mp.terminal}</div>
        <div className="mp-terminal__row">
          <code className="mp-terminal__cmd">
            <span className="mp-terminal__prompt">$</span> tmux -L officraft
            attach -t {vm.tmuxSession}
          </code>
          <button
            type="button"
            className="btn mp-terminal__copy"
            onClick={copyTmux}
            data-testid={`${p}-copy`}
          >
            {copied ? <CheckIcon size={14} /> : <CopyIcon size={14} />}
            <span>{copied ? t.mp.copied : t.mp.copyCommand}</span>
          </button>
        </div>
        <div className="mp-terminal__hint">{vm.terminalHint}</div>
      </div>

      {extraExpandCards}

      {/* expandable: initial prompt */}
      {vm.prompt && (
        <div className="mp-card mp-expand">
          <button
            type="button"
            className="mp-expand__head"
            aria-expanded={showPrompt}
            onClick={() => setShowPrompt((v) => !v)}
            data-testid={`${p}-prompt-toggle`}
          >
            <FileTextIcon size={15} className="mp-expand__icon" />
            <span className="mp-expand__title">{t.mp.initialPrompt}</span>
            <span className="mp-expand__hint">· {vm.prompt.hint}</span>
            {showPrompt ? (
              <ChevronDownIcon size={16} className="mp-expand__chevron" />
            ) : (
              <ChevronRightIcon size={16} className="mp-expand__chevron" />
            )}
          </button>
          {showPrompt && (
            <div className="mp-expand__body" data-testid={`${p}-prompt-body`}>
              {prompt.loading ? (
                t.mp.promptLoading
              ) : prompt.error ? (
                t.mp.promptError
              ) : (
                <>
                  {vm.prompt.note && (
                    <div
                      className="mp-field__hint"
                      data-testid={`${p}-prompt-note`}
                    >
                      {vm.prompt.note}
                    </div>
                  )}
                  <Markdown source={prompt.text} className="doc-md" />
                </>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
