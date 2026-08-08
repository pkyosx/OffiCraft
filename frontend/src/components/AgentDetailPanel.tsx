import { useEffect, useRef, useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
import { effortText } from "../i18n/compose";
import { formatCost } from "../lib/cost";
import { Markdown } from "./Markdown";
import {
  CheckIcon,
  ChevronDownIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  CopyIcon,
  FileTextIcon,
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
/** The display name of a runtime — the ONE place the mapping lives, so the
 * readout and the "changing to" hint beside it cannot disagree. "" (nothing
 * reported) has no label; callers render the dash. */
export function runtimeLabel(runtime: "claude" | "codex" | ""): string {
  return runtime === "codex" ? "Codex" : runtime === "claude" ? "Claude Code" : "";
}

/** The ONE renderer for a cell's "changed, not applied yet" line.
 *
 * 🔴 It renders NOTHING — not an empty div, not a placeholder, not a spacer —
 * when there is no pending change. That is a requirement, not an optimisation:
 * the owner asked for the marks on condition the panel not get busier
 * (2026-07-31), so a panel with nothing pending must be DOM-identical to the
 * one before this existed. Keeping the emptiness inside one component is what
 * makes that checkable in one place instead of four.
 *
 * Styling is the existing `mp-field__hint` grey the machine cell has always
 * used for exactly this — the ticket says reuse it, do not invent a second
 * visual language for the same idea.
 */
function PendingHint({
  p,
  cell,
  text,
}: {
  p: string;
  cell: "runtime" | "model" | "effort" | "machine";
  text?: string;
}) {
  if (!text) return null;
  return (
    <div className="mp-field__hint" data-testid={`${p}-${cell}-pending`}>
      {text}
    </div>
  );
}

export interface AgentDetailVM {
  /** data-testid prefix ("mp" for the member page, "worker-detail" for the
   * outsource page) — keeps each page's existing stable test surface. */
  testIdPrefix: string;
  /** True while the agent's session is really up — gates the refocus button
   * (the server 409s an offline refocus on both kinds). */
  online: boolean;
  /** The owner-CONFIGURED runtime. Drives the Claude/Codex ACCOUNT label only
   * — the readout below is state, and the two must not be conflated. */
  runtime: "claude" | "codex";
  /** STATE readout — what the agent self-reported it is running right now.
   * "" ⇒ the honest dash; never the configured launch value (owner ruling
   * 2026-07-31:「成員面板以及監控台，一定要顯示回報回來的狀態，不能顯示設定值」). */
  reportedRuntime: "claude" | "codex" | "";
  model: string;
  effort: string;
  /** The four PENDING hints — one per configurable cell, each a ready-made
   * line (「→ 要換成 ○○」) or "" for nothing.
   *
   * They are strings, not values to compare, because the comparison needs the
   * domain object and the locale, and both live in the wrappers. The rule they
   * all obey: a hint appears ONLY when a reported value is KNOWN and differs
   * from the configured one. Unknown reports render nothing at all — no empty
   * container, no placeholder, no extra spacing — because a panel with no
   * pending change must look exactly as it did before this existed. */
  pending?: {
    runtime?: string;
    model?: string;
    effort?: string;
    machine?: string;
  };
  /** Resolved machine display text; "" ⇒ dash. Wrappers apply their own gate
   * (member: awake-only; worker: 尚未分配 fallback text). */
  machineText: string;
  /** True when the 模型 row shows what the agent REPORTED rather than what the
   * owner configured (the member panel; T-927a). The row then carries a tag, or
   * three values sit side by side under one heading with two different meanings
   * and nothing telling them apart — and the settings dialog, which edits the
   * CONFIGURED value, would look like it disagrees with the panel. */
  modelIsReported?: boolean;
  /** Optional action next to the 機器 label (the worker's 改機器 button). */
  machineAction?: ReactNode;
  /** Readable Claude account name; "" ⇒ dash — NEVER a raw credential key
   * (the server already resolves alias/label or nulls it, T-ba6b). */
  accountText: string;
  contextPct: number | null;
  compactionCount?: number | null;
  cost: number | null;
  onRefocus?: () => Promise<void>;
  refocusSince: number | null;
  /** Which operation opened the in-flight wind-down ("" when none), and the
   * epoch it is collected by at the latest (null when none). Together they turn
   * the panel's history line into a live "applying your change" line. */
  refocusOp?: string;
  refocusDeadline?: number | null;
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
  /** Pluggable cards AFTER the initial-prompt card — the LAST slot the panel
   * offers (member: RESUME SUMMARY, T-8b0d). Undefined ⇒ renders nothing, so
   * a caller that never passes it (the worker page today) is unaffected. */
  afterPromptCards?: ReactNode;
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
  afterPromptCards,
}: AgentDetailPanelProps) {
  const { t, msg } = useI18n();
  const dash = t.mp.dash;
  const p = vm.testIdPrefix;

  // ── the four configurable cells: reported state, plus a pending hint ──────
  //
  // 🔴 The in-place model/effort EDITOR that used to live here is gone (T-7f28).
  // It was unreachable: it hung off an optional `onSaveModelEffort` prop that
  // NEITHER caller has ever passed, so the edit button, the editor, its save
  // handler and the "configured value" hint beneath the readouts were all dead
  // — along with the local override state that existed to paper over a save the
  // parent had not refetched yet. The live editor is the member panel's
  // settings dialog. `AgentDetailPanel.pending-change.test.tsx` pins the absence.
  const shownModel = vm.model;
  const shownEffort = vm.effort;
  // Known effort levels render 中文字 + the raw key (the member page's format,
  // now the ONE format); an unknown/custom effort string renders verbatim.
  const effortLevelText =
    shownEffort === "low" ||
    shownEffort === "medium" ||
    shownEffort === "high" ||
    shownEffort === "max"
      ? effortText(t, shownEffort)
      : null;
  const pending = vm.pending ?? {};

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
  // While an OWNER-initiated wind-down is open, say what is happening instead
  // of when it started. 「上次重新聚焦 <time>」 is a true sentence that reads as
  // history — the owner who just changed a setting needs to know the window is
  // the reason it has not taken effect yet, and roughly when it will (T-7f28).
  // Only the two owner-op causes qualify: a context-pressure handover or a bare
  // 重新聚焦 is not applying anything of the owner's, so those keep the old line.
  const windDownNote =
    (vm.refocusOp === "relocate" || vm.refocusOp === "runtime/model") &&
    vm.refocusDeadline != null
      ? msg.agentWindDownForChange(
          new Date(vm.refocusDeadline * 1000).toLocaleTimeString(),
        )
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
  // 🔴 Which key has actually been READ (set on success ONLY) and which read is
  // currently in flight. They were one ref stamped at fetch START, and that is
  // what made the card stick on 「載入中…」 forever:
  //
  //   `vm.prompt.fetch` is an inline arrow in BOTH wrappers (the member's
  //   `async () => (await api.getBootstrap(member.role)).context`, the worker's
  //   `onFetchBootContext` prop, itself an arrow rebuilt by OfficePage), so its
  //   identity changes on EVERY render. With it in the deps, any repaint —
  //   an SSE delta is enough — tore the effect down (`alive = false`, so neither
  //   `.then` nor `.catch` could write state) and the rerun bailed at the
  //   already-stamped key. Collapsing and re-expanding could not recover it
  //   either: the stamp said "loaded" for a read that never landed.
  //
  // So: the fetch is read through a ref (a repaint is NOT a reason to re-read —
  // only a different agent is), the loaded stamp is written when the text
  // arrives, and staleness is decided by comparing the key instead of by an
  // `alive` flag a repaint can flip.
  const loadedKeyRef = useRef<string | null>(null);
  const inFlightKeyRef = useRef<string | null>(null);
  const promptFetch = vm.prompt?.fetch;
  const promptFetchRef = useRef(promptFetch);
  promptFetchRef.current = promptFetch;
  const promptKey = vm.prompt?.cacheKey;

  function runPromptFetch(key: string) {
    const fetchFn = promptFetchRef.current;
    if (!fetchFn) return;
    inFlightKeyRef.current = key;
    setPrompt({ text: "", loading: true, error: false });
    fetchFn()
      .then((text) => {
        // A newer key superseded this read (the owner switched agents) — its own
        // effect owns the card now, so this answer is stale.
        if (inFlightKeyRef.current !== key) return;
        inFlightKeyRef.current = null;
        loadedKeyRef.current = key; // stamped on ARRIVAL, never on departure
        setPrompt({ text, loading: false, error: false });
      })
      .catch(() => {
        if (inFlightKeyRef.current !== key) return;
        inFlightKeyRef.current = null;
        // No stamp: the read failed, so re-expanding (or 重試) must read again.
        setPrompt({ text: "", loading: false, error: true });
      });
  }

  useEffect(() => {
    if (!showPrompt || promptKey == null) return;
    if (loadedKeyRef.current === promptKey) return;
    if (inFlightKeyRef.current === promptKey) return;
    runPromptFetch(promptKey);
    // NO cleanup that cancels the read: unmount/repaint is not a cancellation,
    // and the key check above is what keeps a stale answer off the screen.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [showPrompt, promptKey]);

  const contextText = vm.contextPct != null ? `${Math.round(vm.contextPct)}%` : dash;
  const contextDisplay =
    vm.runtime === "codex" && vm.compactionCount != null
      ? `${contextText} (${t.mp.compactionCount(vm.compactionCount)})`
      : contextText;
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

      {/* info card: LEFT 執行環境 + 模型 + 投入度 (editable launch intents), RIGHT 機器 +
       * runtime account — the member page's mp-info2 layout, now the ONE layout. */}
      <div className="mp-card mp-info2">
        <div className="mp-field" data-testid={`${p}-model-effort-cell`}>
          <div className="mp-field__head">
            <div className="mp-field__label">{t.mp.agentRuntime}</div>
          </div>
          <div className="mp-field__value" data-testid={`${p}-runtime-value`}>
            {runtimeLabel(vm.reportedRuntime) || dash}
          </div>
          <PendingHint p={p} cell="runtime" text={pending.runtime} />
          <div className="mp-field__label mp-field__label--stacked">
            {t.mp.model}
          </div>
          <div className="mp-field__value" data-testid={`${p}-model-value`}>
            {shownModel || dash}
            {/* Deliberately NOT the parenthesised form the 投入度 row uses
                below: that one restates the raw value, this one states the
                value's PROVENANCE. Same styling with the same punctuation
                would put two different kinds of thing in the same shape. */}
            {vm.modelIsReported && shownModel && (
              <span className="mp-field__hint">
                {" · "}
                {t.mp.modelReportedTag}
              </span>
            )}
          </div>
          <PendingHint p={p} cell="model" text={pending.model} />
          <div className="mp-field__label mp-field__label--stacked">
            {t.mp.effort}
          </div>
          <div className="mp-field__value" data-testid={`${p}-effort-value`}>
            {effortLevelText != null ? (
              <>
                {effortLevelText}{" "}
                <span className="mp-field__hint">({shownEffort})</span>
              </>
            ) : (
              shownEffort || dash
            )}
          </div>
          <PendingHint p={p} cell="effort" text={pending.effort} />
        </div>
        <div className="mp-field mp-field--divider">
          <div className="mp-field__head">
            <div className="mp-field__label">{t.mp.machine}</div>
            {vm.machineAction}
          </div>
          <div className="mp-field__value" data-testid={`${p}-machine`}>
            {vm.machineText || dash}
          </div>
          <PendingHint p={p} cell="machine" text={pending.machine} />
          <div className="mp-field__label mp-field__label--stacked">
            {/* The CONFIGURED runtime names the account space, not the
                reported one: the account shown belongs to the runtime this
                agent is set up under, and it must stay labelled even before
                anything has reported (when reportedRuntime is still ""). */}
            {vm.runtime === "codex" ? t.mp.codexAccount : t.mp.claudeAccount}
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
              {contextDisplay}
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
        {windDownNote && (
          <div
            className="mp-runtime__note mp-runtime__note--muted"
            data-testid={`${p}-wind-down-note`}
          >
            {windDownNote}
          </div>
        )}
        {!windDownNote && refocusSinceText && (
          <div
            className="mp-runtime__note mp-runtime__note--muted"
            data-testid={`${p}-refocus-since`}
          >
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
                // A failed read says so AND offers the way out. Leaving it on
                // 「載入中…」 was the old shape: it read as "still working" and
                // there was nothing to press.
                <div data-testid={`${p}-prompt-error`}>
                  <span>{t.mp.promptError}</span>{" "}
                  <button
                    type="button"
                    className="doc-btn"
                    data-testid={`${p}-prompt-retry`}
                    onClick={() => {
                      if (promptKey != null) runPromptFetch(promptKey);
                    }}
                  >
                    {t.mp.promptRetry}
                  </button>
                </div>
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

      {afterPromptCards}
    </div>
  );
}
