import { useRef, useState } from "react";
import { useI18n } from "../i18n";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { api } from "../api";
import { ApiError } from "../api/errors";
import { formatCost } from "../lib/cost";
import { ConfirmModal } from "./ConfirmModal";
import { formatDuration } from "../lib/duration";
import { useMembers } from "../hooks/useMembers";
import { useMonitoring } from "../hooks/useMonitoring";
import { useMachines } from "../hooks/useMachines";
import { useOutsourceWorkers } from "../hooks/useOutsourceWorkers";
import { useServerSettings } from "../hooks/useServerSettings";
import type {
  MonMachineView,
  MonAccountView,
  MonSessionView,
  Member,
  MachineView,
  UninstallResultView,
  BootstrapResultView,
  CutoverEffect,
} from "../types";
import type { OutsourceWorkerView } from "../api/adapter";
import {
  joinSessionRuntime,
  findSessionFor,
  isReportingTelemetry,
} from "../lib/runtime";
import { useHashRoute } from "../lib/hashRoute";
import { Avatar } from "./Avatar";
import { avatarKindForMember } from "../lib/avatarKind";
import { InlineEdit } from "./InlineEdit";
import { MemberDetailPanel } from "./MemberDetailPanel";
import { PresenceBadge } from "./PresenceBadge";
import { CopyIcon, CheckIcon, CloseIcon } from "./icons";
// The 歸零 pill on the account card is the SAME control as the one on the member
// panel — same look, same danger colour, same size — so it wears the `mp` block's
// class rather than a second copy of those rules under `mon`. Importing the
// sheet here is the repo's convention for borrowing a block (ResumeSummaryCard
// does the same): relying on some other component happening to have imported it
// is what makes a style silently disappear when that component is removed.
import "./member-detail.css";
import "./monitor.css";

export function MonitorPage() {
  const { t, msg } = useI18n();
  const { settings } = useServerSettings();
  const refreshSeconds = settings?.monitoringRefreshSeconds ?? 5;
  const { monitoring, refetch } = useMonitoring({ refreshSeconds });
  // The machine registry (GET /api/machines) is the source for the machines
  // panel identity + online + teardown target — NOT the monitoring telemetry.
  const { machines, refetch: refetchMachines } = useMachines({ refreshSeconds });
  // Inline-rename failure surface (e.g. server 422 on a blank/whitespace name).
  // Never silently swallow the PATCH rejection — show an honest banner.
  const [renameError, setRenameError] = useState<string | null>(null);
  // 帳號詳情 modal target (T-a9a7): the account whose real identity (key /
  // email / org / cost) is being inspected; null = closed. Same
  // target-state pattern as the uninstall/delete modals below.
  const [detailAccount, setDetailAccount] = useState<MonAccountView | null>(
    null
  );
  // The roster is the join source for a session's effort badge + the member the
  // detail panel needs. subscribeEvents inside each hook reconciles by refetch.
  const { members, refetch: refetchMembers } = useMembers();
  // Outsource workers (O-xx) are ALSO live AI sessions — they burn context and
  // cost the same way members do (owner report 2026-07-19). This hook supplies
  // their IDENTITY (codename / avatar / bound task) and the row controls; their
  // TELEMETRY columns come from the monitoring `sessions` array below, which
  // now carries a row per worker under its own `ow-` id.
  const outsource = useOutsourceWorkers();
  // The open member-detail rides on the URL hash (#monitor/member/<id>) so a
  // refresh restores it; a stale id self-heals (lookup below misses → list view).
  const [route, setRoute] = useHashRoute();
  const detailId = route.detailId ?? null;
  const setDetailId = (id: string | null) =>
    setRoute({ page: "monitor", detailId: id ?? undefined });

  // ── 新增機器 / 上線 (onboard) ── the "+新增機器" add entry sits BELOW the
  // machine table card (the entry is the only frame — never nested
  // inside the card's border); clicking it grows an INLINE editable row
  // (owner-aligned pattern, same as 角色誌's 新增角色定義): one machine-name
  // field — Enter/確認 creates the row, Esc/取消 collapses it back.
  const [onboardAdding, setOnboardAdding] = useState(false);
  const [onboardName, setOnboardName] = useState("");
  const [onboardBusy, setOnboardBusy] = useState(false);
  const [onboardError, setOnboardError] = useState<string | null>(null);
  // IME composition guard (same rule as InlineEdit): an Enter confirming a CJK
  // candidate must not submit the row.
  const onboardComposingRef = useRef(false);

  // ── install (安裝) ── the verb has TWO shapes by row kind, and they are NOT
  // equally dangerous — which is why neither the gating nor the wording is
  // shared between them:
  //   • the server-self row → an in-place bootstrap-on-server (see installSelf).
  //     OFFLINE: run directly, nothing is being replaced. ONLINE: it overwrites
  //     the warden currently in service and every member on that box drops —
  //     irreversible, so it goes through `bootstrapConfirmTarget` first.
  //   • every other (remote) machine → a SINGLE uniform copy-command dialog
  //     (`installTarget`): copy the command, run it on that machine. This sends
  //     NO request and changes NOTHING — it renders a string. No confirm: a
  //     second click that guards nothing only trains the owner to click through
  //     the one above, which does guard something.
  const [installTarget, setInstallTarget] = useState<MachineView | null>(null);
  // The server-self reinstall-over-a-live-warden confirm target; null = closed.
  const [bootstrapConfirmTarget, setBootstrapConfirmTarget] =
    useState<MachineView | null>(null);
  // The in-place bootstrap-on-server result for the server-self row (POST
  // /bootstrap-here): {ok, exitCode, log}; on !ok the `log` (failure reason, e.g.
  // the one-warden guard) is surfaced verbatim — never swallowed. `bootstrapTarget`
  // marks the machine acted on so the result block below the table names it.
  const [bootstrapTarget, setBootstrapTarget] = useState<MachineView | null>(
    null
  );
  const [bootstrapBusy, setBootstrapBusy] = useState(false);
  const [bootstrapError, setBootstrapError] = useState<string | null>(null);
  const [bootstrapResult, setBootstrapResult] =
    useState<BootstrapResultView | null>(null);

  // ── uninstall (解除安裝) state ── the "uninstall" verb → POST
  // /api/machines/{id}/uninstall: drive the uninstall RPC to the warden (runs
  // `ocwarden uninstall` on its box). ONLINE-ONLY (an offline machine has nothing
  // to uninstall). A HOST-mutating remote action → confirm first. The record is
  // KEPT (re-installable) — the row does NOT drop; the machine goes offline once
  // the warden reports the receipt. The result carries {dispatched}.
  const [uninstallTarget, setUninstallTarget] = useState<MachineView | null>(
    null
  );
  // Guard: when members are still ACTUALLY ONLINE on the machine, clicking
  // uninstall opens THIS warning dialog first (advise taking them offline)
  // instead of the plain confirm. Proceed runs the same uninstall; a machine
  // whose agents are all offline keeps the direct-to-confirm behavior.
  const [uninstallWarnTarget, setUninstallWarnTarget] =
    useState<MachineView | null>(null);
  const [uninstallBusy, setUninstallBusy] = useState(false);
  const [uninstallError, setUninstallError] = useState<string | null>(null);
  const [uninstallResult, setUninstallResult] = useState<{
    machine: MachineView;
    result: UninstallResultView;
  } | null>(null);

  // ── delete (刪除) state ── the "delete" verb → DELETE /api/machines/{id}: a PURE
  // roster soft-delete (delete ≠ uninstall ≠ stop). It removes the machine record;
  // it does NOT tear the warden off the box (that is uninstall). A destructive
  // action → confirm first; on success the row drops (refetch). The result carries
  // {removed}.
  const [deleteTarget, setDeleteTarget] = useState<MachineView | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  // Live session telemetry is the SAME source the "AI 會話" rows below read.
  const sessions = monitoring?.sessions ?? [];

  const rosterDetail = detailId
    ? members.find((m) => m.id === detailId)
    : undefined;
  // Join the live session's context/cost onto the member so the detail panel
  // shows the SAME value as the session row (same source, never divergent).
  const detail = rosterDetail
    ? joinSessionRuntime(rosterDetail, sessions)
    : undefined;

  // Clicking a session row opens the SAME member-detail panel the office uses,
  // wired to the SAME api mutations (activate/refocus/rename → refetch).
  if (detail) {
    return (
      <MemberDetailPanel
        member={detail}
        onBack={() => setDetailId(null)}
        onActivate={async (machineId) => {
          // Return the result (T-7fa1) — the Monitor entry renders the SAME
          // MemberDetailPanel, so it must feed it the same verdict.
          const result = await api.activateMember(detail.id, machineId);
          try {
            await refetchMembers();
          } catch {
            /* the verdict outlives a failed refresh (NIT-4) */
          }
          return result;
        }}
        // 改機器 (placement only): re-pin the member's desired machine and let the
        // server reconcile a live member onto it — NEVER wakes the member (never
        // touches desired_state), unlike onActivate. Mirrors OfficePage's
        // MemberDetailPanel entry; the Monitor entry was the only member-detail
        // surface still missing it. Refetch via THIS page's roster hook
        // (refetchMembers) so the new pin surfaces the same way every other
        // action here reconciles — not OfficePage's own refetch().
        onRelocate={async (machineId) => {
          const result = await api.relocateMember(detail.id, machineId);
          try {
            await refetchMembers();
          } catch {
            /* the verdict outlives a failed refresh (NIT-4) */
          }
          return result;
        }}
        // Graceful stop / cancel-wake (retains the row). Refetch and let
        // server-driven presence surface stopping → stopped. Same stop/force-stop
        // capability the office entry offers — the shared MemberDetailPanel must
        // behave identically no matter which entry opened it.
        onDeactivate={async () => {
          await api.deactivateMember(detail.id);
          await refetchMembers();
        }}
        // Force-stop (immediate kill): the LAST rung — the robust STOP goes to
        // the warden now.
        // 加速停止 — the MIDDLE rung of 停止 → 加速停止 → 強制停止. Puts the
        // wind-down that is already open on the server's clock and tells the
        // member; it is NOT a kill, so the member can still finish early.
        onAcceleratedStop={async () => {
          await api.acceleratedStopMember(detail.id);
          await refetchMembers();
        }}
        onForceStop={async () => {
          await api.forceStopMember(detail.id);
          await refetchMembers();
        }}
        onRefocus={async () => {
          await api.refocusMember(detail.id);
          await refetchMembers();
        }}
        // 成本歸零 (T-53, owner-only + irreversible; the panel confirms first).
        // Both refetches: the figure the cockpit renders is folded from the
        // monitoring read, not the roster row, so refetching members alone
        // would leave the old number on screen.
        onResetCost={async () => {
          await api.resetMemberCost(detail.id);
          await Promise.all([refetchMembers(), refetch()]);
        }}
        onRename={async (name) => {
          await api.patchMember(detail.id, { name });
          await refetchMembers();
        }}
        onUpdateAvatar={async (file) => {
          await api.updateMemberAvatar(detail.id, file);
          await refetchMembers();
        }}
        onRemoveAvatar={async () => {
          await api.removeMemberAvatar(detail.id);
          await refetchMembers();
        }}
      />
    );
  }

  const dash = t.monitor.dash;
  // `machines` is the registry (useMachines) — the machines panel's source.
  const accounts = monitoring?.accounts ?? [];
  // Hardware telemetry per host (Monitor §2 fold). We JOIN it onto each registry
  // machine so one row carries identity/online/actions (registry) AND CPU/RAM/
  // POWER (telemetry). The telemetry card's `machine` key IS the host machine-id
  // (the warden's own id, e.g. `m-server-self`) — the SAME value as the registry
  // row's `machineId`. So the join key is `machineId` directly; we do NOT bounce
  // through the warden member's `desiredMachineId` (which is empty for a warden that
  // hosts itself → that stale indirection made every hardware cell show dash).
  // No telemetry for a machine → hardware columns show dash (the row NEVER
  // disappears; regression #1 was the whole hardware block being dropped).
  const monMachines = monitoring?.machines ?? [];
  const hwByHost = new Map(monMachines.map((mm) => [mm.machine, mm]));

  // Rename a machine/account display label: PATCH (by stable id) then refetch
  // monitoring for the fresh label (the PATCH returns a narrow alias, not a row —
  // we never merge it in). On failure (server 422 on blank, etc.) show an honest
  // banner; InlineEdit already blocks empty/unchanged commits client-side.
  const renameMachine = (id: string, next: string) => {
    setRenameError(null);
    api
      .patchMachine(id, { displayName: next })
      .then(() => refetchMachines())
      .catch(() => setRenameError(t.monitor.renameError));
  };
  const renameAccount = (id: string, next: string) => {
    setRenameError(null);
    api
      .patchAccount(id, { displayName: next })
      .then(() => refetch())
      .catch(() => setRenameError(t.monitor.renameError));
  };

  // Collapse the inline onboard row back (Esc / 取消 / after a create).
  const resetOnboardRow = () => {
    setOnboardAdding(false);
    setOnboardName("");
    setOnboardError(null);
  };

  // Add a machine with the name typed in the inline row (blank → keep the row
  // open, nothing created). On success we refetch the registry so the new
  // machine surfaces as a new row (offline until its warden reports in). The
  // onboard result's boot_command is deliberately discarded — it is re-mintable
  // anytime via that row's Install (api.getMachineBootCommand).
  const addMachine = async () => {
    const name = onboardName.trim();
    if (onboardBusy || !name) return;
    setOnboardBusy(true);
    setOnboardError(null);
    try {
      await api.onboardMachine(name);
      await refetchMachines();
      resetOnboardRow();
    } catch {
      setOnboardError(t.monitor.machine.onboardError);
    } finally {
      setOnboardBusy(false);
    }
  };

  // Open the copy-command install dialog for a REMOTE (non-server-self) machine.
  const openInstall = (machine: MachineView) => {
    setInstallTarget(machine);
  };
  const closeInstall = () => {
    setInstallTarget(null);
  };

  // Install the server-self machine IN PLACE → POST bootstrap-here (owner/admin-agent HOST
  // action): the server installs the warden on itself. Run directly on click when
  // the machine is OFFLINE; while it is ONLINE the caller opens a confirm first
  // (bootstrapConfirmTarget) because that overwrites a live warden. A failed install is a REAL result (ok=false + log), NOT a thrown error —
  // only a transport/gate failure lands in catch.
  //
  // T-ba62: the log is KEPT ON SUCCESS TOO. It used to be discarded on the ok
  // branch, on the reasoning that "the row flipping online IS the signal" — but
  // the installer's most important output was a WARNING it emitted while still
  // exiting 0 (claude unresolvable), and that branch threw it away unread. So
  // "installed cleanly" and "installed with a warning that guarantees every
  // spawn will fail" rendered identically: no panel, no log, a green row. The
  // block below now shows both outcomes and labels which one it is; the owner
  // dismisses it.
  const installSelf = async (machine: MachineView) => {
    if (bootstrapBusy) return;
    setBootstrapTarget(machine);
    setBootstrapResult(null);
    setBootstrapError(null);
    setBootstrapBusy(true);
    try {
      const result = await api.bootstrapOnServer(machine.machineId);
      setBootstrapResult(result);
      if (result.ok) {
        await Promise.all([refetchMachines(), refetchMembers()]);
      }
    } catch (e) {
      // Surface the server's error detail (e.g. the 503 "ocwarden binary is
      // not available" reason) — a bare "request failed" hides the actual fix.
      const detail = e instanceof ApiError ? e.serverMessage : "";
      setBootstrapError(
        detail
          ? msg.machineBootstrapErrorDetail(detail)
          : t.monitor.machine.bootstrapError
      );
    } finally {
      setBootstrapBusy(false);
    }
  };

  // Confirm uninstall → POST /api/machines/{id}/uninstall (owner-only remote HOST
  // action): drive the uninstall RPC to the warden. The record is KEPT
  // (re-installable) — the row does NOT drop. A transport/gate failure lands in
  // catch (honest error banner). On success we surface the {dispatched} result and
  // refetch so the machine's online state (→ offline once the warden reports)
  // reconciles by refetch, never by an optimistic guess.
  // The real AI members ACTUALLY ONLINE on a machine right now (owner-decided
  // criterion, same as the server's 409 gate: live presence + observed
  // position — an offline member merely BOUND here via desiredMachineId never
  // counts, so an all-offline machine uninstalls/deletes without a warning).
  // The machine's own warden is excluded — it IS the machine.
  const membersOnMachine = (machineId: string) =>
    members.filter(
      (m) =>
        m.kind === "staff" && m.status === "online" && m.machine === machineId
    );

  // Machine mid-uninstall (①): the warden member still carries the one-shot
  // desired_state="uninstall" intent — the server consumes it (→ "offline")
  // the moment it observes the warden disconnect. Until then the row shows the
  // same in-progress treatment as installing: button re-labelled + disabled;
  // the member-delta refetch flips it back automatically.
  const uninstalling = (machineId: string) =>
    members.find((m) => m.id === machineId)?.desiredState === "uninstall";

  const runUninstall = async (machine: MachineView) => {
    if (uninstallBusy) return;
    setUninstallBusy(true);
    setUninstallError(null);
    try {
      const result = await api.uninstallMachine(machine.machineId);
      setUninstallResult({ machine, result });
      setUninstallTarget(null);
      setUninstallWarnTarget(null);
      await Promise.all([refetchMachines(), refetchMembers()]);
    } catch {
      setUninstallError(t.monitor.machine.uninstallError);
    } finally {
      setUninstallBusy(false);
    }
  };

  const confirmUninstall = () => {
    if (!uninstallTarget) return;
    void runUninstall(uninstallTarget);
  };

  // Confirm delete → DELETE /api/machines/{id} (owner-only): a PURE roster
  // soft-delete (no warden command). On success the row drops (refetch). A
  // transport/gate failure lands in catch (honest error banner; the row stays).
  const confirmDelete = async () => {
    if (!deleteTarget || deleteBusy) return;
    setDeleteBusy(true);
    setDeleteError(null);
    try {
      await api.deleteMachine(deleteTarget.machineId);
      setDeleteTarget(null);
      await Promise.all([refetchMachines(), refetchMembers()]);
    } catch {
      setDeleteError(t.monitor.machine.deleteError);
    } finally {
      setDeleteBusy(false);
    }
  };

  // "AI 會話" lists only real AI assistant sessions. A warden is a machine-layer
  // telemetry daemon (a script, not an AI) — its hardware already shows under
  // 機器資訊, so it must NOT appear here as if it were an AI agent (Seth's
  // warden≠LLM rule; the office roster is already filtered the same way in
  // OfficePage). A session with no roster match is left visible (honest: we
  // can't prove it's non-AI).
  //
  // Outsource workers are excluded HERE by their `ow-` id prefix rather than by
  // a roster lookup: their telemetry rides this same array, and the worker lane
  // below already renders them (with identity + controls a bare session row has
  // no way to draw). The prefix is the contract — a worker that happens to be
  // missing from the roster fold must not fall through into this lane and get
  // listed twice.
  //
  // T-14 項目 6 reviewed this filter and DELIBERATELY LEFT IT. It is not a
  // client-side patch over the duplicate `sessions` rows the merged roster read
  // could have produced — that duplicate is gone at the source (the monitoring
  // handler's own driver guard; server-side test
  // TestGetMonitoring_LiveContractorCountsAsOneAgentNotTwo asserts exactly one
  // row per contractor id). This filter answers a DIFFERENT question that is
  // still live: workers legitimately ride this array so the worker lane can
  // join their telemetry, and without the prefix test they would render in BOTH
  // lanes. Deleting it would be a regression with nothing to do with 項目 6.
  const aiSessions = sessions.filter((s) => {
    if (s.id.startsWith("ow-")) return false;
    const m = members.find((x) => x.id === s.id);
    return m?.kind !== "warden" && m?.kind !== "outsource";
  });

  return (
    <div className="monitor">
      {renameError && <div className="mon-error">{renameError}</div>}
      {/* No 備份健康 card here (T-5e71, owner 2026-08-02): the backup verdict
          moved to 設定 › 系統更新與備份, next to the software update it belongs
          with. */}
      {/* ── §1 帳號資訊 (Accounts) ── */}
      <section className="mon-section">
        <div className="mon-section__title">{t.monitor.accountsTitle}</div>
        {accounts.length === 0 ? (
          <div className="mon-empty">{t.monitor.accountsEmpty}</div>
        ) : (
          <div className="mon-accounts">
            {accounts.map((a) => (
              <AccountCard
                key={`${a.account}@${a.machine}`}
                account={a}
                onRename={(next) => renameAccount(a.account, next)}
                onDetail={() => setDetailAccount(a)}
                // 帳號歸零 (T-53, owner ruling rc-5c5d7c7c6dcd): the account's
                // own figure, cleared without touching any member. Only the
                // monitoring refetch is needed — no roster row changed, which
                // is the whole point of the separation.
                onResetCost={async () => {
                  await api.resetAccountCost(a.account);
                  await refetch();
                }}
              />
            ))}
          </div>
        )}
        {detailAccount && (
          <AccountDetailModal
            account={detailAccount}
            onClose={() => setDetailAccount(null)}
          />
        )}
      </section>

      {/* ── §2 機器資訊 (Machines) ── */}
      <section className="mon-section">
        <div className="mon-section__head">
          <div className="mon-section__title">{t.monitor.machinesTitle}</div>
        </div>

        {onboardError && <div className="mon-error">{onboardError}</div>}

        <div className="mon-table-wrap">
          <table className="mon-table">
            <thead>
              <tr>
                {/* 機器 + 狀態 are ONE column (T-674d): they were split, and the
                 * name cell was narrow enough that the machine-id chip wrapped
                 * to a second line on every row. Merging is not decoration —
                 * the id is the machine's identity and belongs beside its name,
                 * and the online badge is the same row's other identity fact.
                 * The removed 狀態 header is a header only; the badge itself is
                 * unchanged and still an honest passthrough of `online`. */}
                <th className="mon-table__left">{t.monitor.machineCol.machine}</th>
                <th className="mon-table__left">{t.monitor.machineCol.claude}</th>
                <th className="mon-table__left">{t.monitor.machineCol.codex}</th>
                <th>{t.monitor.machineCol.cpu}</th>
                <th>{t.monitor.machineCol.ram}</th>
                <th>{t.monitor.machineCol.power}</th>
                <th className="mon-table__right">
                  {t.monitor.machine.actionsCol}
                </th>
              </tr>
            </thead>
            <tbody>
              {machines.length === 0 ? (
                <tr>
                  <td className="mon-table__left mon-muted" colSpan={7}>
                    {t.monitor.machine.machinesEmpty}
                  </td>
                </tr>
              ) : (
                machines.map((m) => {
                  // Join hardware telemetry by this machine's own id (see
                  // hwByHost above — its key IS the host machine-id). Undefined
                  // when the machine reported no telemetry → hardware cells fall
                  // back to dash, row stays.
                  const hw = hwByHost.get(m.machineId);
                  return (
                  <tr key={m.machineId}>
                    {/* display_name is the editable label; the PATCH target is the
                     * stable machineId, NOT the label. */}
                    <td
                      className="mon-table__left"
                      data-label={t.monitor.machineCol.machine}
                    >
                      <div className="mon-machine-name">
                        <InlineEdit
                          value={m.displayName}
                          onCommit={(next) => renameMachine(m.machineId, next)}
                          ariaLabel={t.monitor.renameMachine}
                          placeholder={t.monitor.renamePlaceholder}
                          displayClassName={`mon-table__strong${
                            m.isSelf ? " mon-self-name" : ""
                          }`}
                        />
                        {/* Stable machine id (the warden member's own id / token
                            sub) — the machine's identity, never editable. Mirrors
                            the member detail panel's id badge. */}
                        <span
                          className="mon-machine-id"
                          data-testid="mon-machine-id"
                          title={m.machineId}
                        >
                          {m.machineId}
                        </span>
                        {/* online badge — honest passthrough of the registry's
                         * online, now living in the merged 機器 cell (T-674d).
                         * Same markup, same source; only its column moved. */}
                        <span
                          className={`mon-online${
                            m.online ? " mon-online--on" : " mon-online--off"
                          }`}
                        >
                          <span
                            className={`status-dot ${
                              m.online
                                ? "status-dot--online"
                                : "status-dot--offline"
                            }`}
                            aria-hidden
                          />
                          {m.online
                            ? t.monitor.machine.online
                            : t.monitor.machine.offline}
                        </span>
                        {/* Nothing is rendered here for a machine whose
                         * cutover is PROVEN in effect — and that silence is
                         * now the point: a blank means "measured, fine", and
                         * the two states that used to share that blank say so
                         * for themselves below. The badge that used to live
                         * here named an internal shape vocabulary nobody
                         * outside this codebase can read, and its green face
                         * asserted a cutover had taken effect when it only
                         * ever observed warden's own parent. */}
                        <CutoverEffectLine effect={m.cutoverEffect} />
                      </div>
                    </td>
                    {/* Per-runtime version columns (T-674d), replacing the old
                     * ✓/✗ Runtimes digest. Both cells read the SAME capability
                     * map the digest read — nothing new is collected, and no
                     * version is ever synthesized.
                     *
                     * Claude additionally falls back to the registry's own
                     * `claude_version` (its long-standing source, T-97ee/T-7c5b)
                     * when the machine has no capability entry — that keeps the
                     * column's meaning exactly as it was for older wardens.
                     * Codex has no such registry field, so its ONLY source is
                     * the capability map; absent means unknown, and the cell
                     * says so rather than inventing a number.
                     *
                     * The ✗ states the digest carried are NOT dropped: they are
                     * spelled out ("not installed" / "signed out"), because
                     * they are the only on-screen explanation for a worker
                     * parked on machine_unavailable. And because these values
                     * come from telemetry that is never cleared on disconnect,
                     * a non-fresh probe is MARKED, never shown plain. */}
                    <td
                      className="mon-table__left"
                      data-label={t.monitor.machineCol.claude}
                      data-testid="mon-claude-version"
                    >
                      <RuntimeVersionCell
                        capability={hw?.runtimeCapabilities?.claude}
                        fallbackVersion={m.claudeVersion}
                        stale={hw?.runtimeCapabilitiesStale}
                        testIdPrefix="mon-claude"
                      />
                    </td>
                    <td
                      className="mon-table__left"
                      data-label={t.monitor.machineCol.codex}
                      data-testid="mon-codex-version"
                    >
                      <RuntimeVersionCell
                        capability={hw?.runtimeCapabilities?.codex}
                        fallbackVersion={null}
                        stale={hw?.runtimeCapabilitiesStale}
                        testIdPrefix="mon-codex"
                      />
                    </td>
                    {/* Hardware telemetry (joined by host). Honest dash when the
                     * host reported no telemetry — never a fabricated number.
                     *
                     * The dash alone is NOT enough (T-b36a): the server also
                     * withholds the numbers of an EXPIRED sample, so "this box
                     * has never reported hardware" and "it reported, then went
                     * dark an hour ago" both land here as three dashes — and
                     * only the second is something an operator can act on. When
                     * the server says the sample is stale, the dash is marked
                     * with its reason (same mon-stale marker the runtime
                     * readiness cell uses; one visual vocabulary for one
                     * freshness rule). `hardwareStale === true` and nothing
                     * else: false is a live sample whose probe simply had no
                     * answer, null is a box that never measured.
                     *
                     * And a THIRD blank (T-aad2), which the two above cannot
                     * describe: the probe DID report, with a value the server
                     * cannot read. That used to be pixel-identical to "never
                     * measured", so a warden whose CPU reading turned into a
                     * string looked exactly like a machine with no such probe.
                     * `hardwareInvalid` names the keys per cell, so one broken
                     * probe marks its own cell and leaves its siblings alone.
                     * Separate mark from stale on purpose: "nobody has looked
                     * lately" and "the reporter is broken" are different jobs
                     * for whoever is reading this screen. */}
                    <td data-label={t.monitor.machineCol.cpu} data-testid="mon-cpu">
                      {pctText(hw?.cpuPct ?? null, dash)}
                      {hw?.hardwareStale === true && <HardwareStaleMark />}
                      {badHardware(hw, "cpu_pct") && <HardwareBadMark />}
                    </td>
                    <td data-label={t.monitor.machineCol.ram} data-testid="mon-ram">
                      {pctText(hw?.ramPct ?? null, dash)}
                      {hw?.hardwareStale === true && <HardwareStaleMark />}
                      {badHardware(hw, "ram_pct") && <HardwareBadMark />}
                    </td>
                    <td data-label={t.monitor.machineCol.power} data-testid="mon-power">
                      {powerText(hw ? hw.acPower : null, hw?.batteryPct ?? null, dash)}
                      {hw?.hardwareStale === true && <HardwareStaleMark />}
                      {(badHardware(hw, "ac_power") || badHardware(hw, "battery_pct")) && (
                        <HardwareBadMark />
                      )}
                    </td>
                    {/* Actions — the machine-lifecycle verbs (T-IUD):
                     *   install   → server-self: in-place bootstrap-on-server —
                     *               run directly while offline, but confirm first
                     *               while ONLINE (it overwrites the live warden);
                     *               other machines: a single copy-command dialog.
                     *   uninstall → POST /uninstall (drive the uninstall RPC to the
                     *               warden). ONLINE-ONLY — an offline machine has
                     *               nothing to uninstall (disabled + reason tooltip).
                     *   delete    → DELETE /machines/{id} (PURE roster soft-delete);
                     *               NOT offered for the server-self row (undeletable).
                     */}
                    <td
                      className="mon-table__right"
                      data-label={t.monitor.machine.actionsCol}
                    >
                      <div className="mon-actions">
                        <button
                          type="button"
                          className="btn btn--accent-ghost"
                          data-testid="mon-install-btn"
                          disabled={m.isSelf && bootstrapBusy}
                          onClick={() => {
                            if (!m.isSelf) {
                              // Remote machine: shows a command to copy. No
                              // request, online or not.
                              openInstall(m);
                              return;
                            }
                            if (m.online) {
                              // Reinstalling over a LIVE warden — confirm first.
                              setBootstrapConfirmTarget(m);
                              return;
                            }
                            void installSelf(m);
                          }}
                        >
                          {m.isSelf && bootstrapBusy
                            ? t.monitor.machine.bootstrapBusy
                            : m.online
                              ? // Online ⇒ this machine HAS a warden talking to the
                                // station, so this click reinstalls over it. Offline
                                // is NOT the negation: the server keeps no "was this
                                // ever installed" field, so an installed-but-powered-off
                                // machine is indistinguishable from one that never was
                                // (T-ce3d). Owner ruled 2026-08-20 to use online as the
                                // proxy anyway — the ACTION is identical either way
                                // (`install --force`), only the word differs.
                                t.monitor.machine.reinstall
                              : t.monitor.machine.install}
                        </button>
                        {/* Mid-uninstall (intent still pending on the warden) the
                         * button wears the SAME in-progress treatment as install:
                         * transitional label + disabled, until the server consumes
                         * the one-shot intent on the warden's disconnect. */}
                        <button
                          type="button"
                          className="btn btn--accent-ghost"
                          data-testid="mon-uninstall-btn"
                          disabled={!m.online || uninstalling(m.machineId)}
                          {...(!m.online
                            ? { title: t.monitor.machine.uninstallOfflineHint }
                            : {})}
                          onClick={() => {
                            setUninstallError(null);
                            if (membersOnMachine(m.machineId).length > 0) {
                              setUninstallWarnTarget(m);
                            } else {
                              setUninstallTarget(m);
                            }
                          }}
                        >
                          {uninstalling(m.machineId)
                            ? t.monitor.machine.uninstallInProgress
                            : t.monitor.machine.uninstall}
                        </button>
                        {/* The server-self row is NOT deletable — the button stays
                         * (disabled) so every row's columns line up. */}
                        <button
                          type="button"
                          className="btn btn--danger-ghost"
                          data-testid="mon-delete-btn"
                          disabled={m.isSelf}
                          onClick={() => {
                            if (m.isSelf) return;
                            setDeleteError(null);
                            setDeleteTarget(m);
                          }}
                        >
                          {t.monitor.machine.deleteMachine}
                        </button>
                      </div>
                    </td>
                  </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>

        {/* "+新增機器 / 上線" — a standalone add entry BELOW the machine
         * table card. Owner feedback (M2 acceptance): the entry must be the
         * ONLY frame around the add affordance — as a table row it rendered
         * boxed inside the card's own border (frame-in-frame). 修仙 batch 1:
         * it wears the SHARED `.add-entry` silhouette (centered "+ label",
         * solid low-key neutral frame, no accent green), identical to 角色誌's
         * 新增角色定義. Clicking grows the INLINE editable row in place
         * (owner-aligned pattern): one machine-name field, Enter/確認 creates,
         * Esc/取消 collapses. */}
        <div className="mon-onboard">
          {!onboardAdding ? (
            <button
              type="button"
              id="mon-onboard-entry"
              className="add-entry"
              onClick={() => setOnboardAdding(true)}
            >
              + {t.monitor.machine.onboardEntry}
            </button>
          ) : (
            <div className="mon-onboard-edit" data-testid="mon-onboard-row">
              <input
                className="mon-onboard-edit__input"
                value={onboardName}
                autoFocus
                placeholder={t.monitor.machine.onboardNamePlaceholder}
                aria-label={t.monitor.machine.onboardNamePlaceholder}
                onChange={(e) => setOnboardName(e.target.value)}
                onCompositionStart={() => {
                  onboardComposingRef.current = true;
                }}
                onCompositionEnd={(e) => {
                  onboardComposingRef.current = false;
                  setOnboardName(e.currentTarget.value);
                }}
                onKeyDown={(e) => {
                  if (
                    e.nativeEvent.isComposing ||
                    e.keyCode === 229 ||
                    onboardComposingRef.current
                  ) {
                    return;
                  }
                  if (e.key === "Enter") void addMachine();
                  if (e.key === "Escape") {
                    // Spent here — see InlineEdit: the shared Esc dispatcher
                    // must not also close the surface around this field.
                    e.preventDefault();
                    resetOnboardRow();
                  }
                }}
                data-testid="mon-onboard-name"
              />
              <button
                type="button"
                className="btn btn--ghost"
                disabled={onboardBusy}
                onClick={resetOnboardRow}
              >
                {t.common.cancel}
              </button>
              <button
                type="button"
                className="btn btn--accent-ghost"
                disabled={onboardBusy}
                onClick={() => void addMachine()}
                data-testid="mon-onboard-confirm"
              >
                {onboardBusy
                  ? t.monitor.machine.onboardBusy
                  : t.monitor.machine.onboardConfirm}
              </button>
            </div>
          )}
        </div>

        {/* in-place install RESULT for the server-self row (POST /bootstrap-here):
         * the `log` (surfaced verbatim — never swallowed) in a dismissible block.
         * T-ba62: shown for SUCCESS as well as failure. A successful exit code
         * does not mean a clean install — the installer's warnings ride the same
         * log, and discarding them on the ok branch is what made a warden that
         * refuses every spawn look exactly like a healthy one. */}
        {bootstrapTarget && (bootstrapResult || bootstrapError) && (
          <div className="mon-cmd" data-testid="mon-bootstrap-result-block">
            <div className="mon-cmd__head">
              <span className="mon-cmd__title">
                {t.monitor.machine.installTitle} · {bootstrapTarget.displayName}
              </span>
              <button
                type="button"
                className="btn btn--ghost mon-cmd__close"
                aria-label={t.monitor.machine.close}
                onClick={() => {
                  setBootstrapTarget(null);
                  setBootstrapResult(null);
                  setBootstrapError(null);
                }}
              >
                {t.monitor.machine.close}
              </button>
            </div>
            {bootstrapError && <div className="mon-error">{bootstrapError}</div>}
            {bootstrapResult && (
              <div
                className={
                  bootstrapResult.ok ? "mon-cmd" : "mon-cmd mon-cmd--err"
                }
              >
                <p className="mon-cmd__hint">
                  {bootstrapResult.ok
                    ? t.monitor.machine.bootstrapSucceeded
                    : msg.machineBootstrapFailed(bootstrapResult.exitCode)}
                </p>
                <pre className="mon-log" data-testid="mon-bootstrap-log">
                  {bootstrapResult.log}
                </pre>
              </div>
            )}
          </div>
        )}

        {/* reinstall confirm — server-self row, machine ONLINE. This is the one
         * install shape that actually mutates a host: it overwrites the warden
         * in service and drops every member on that box, and it cannot be
         * undone. Cancel must leave NOTHING behind — no request, no state.
         * The remote-machine install deliberately does NOT come through here;
         * it renders a command and sends nothing. */}
        {bootstrapConfirmTarget && (
          <div
            className="mon-confirm"
            data-testid="mon-bootstrap-confirm"
            role="dialog"
            aria-modal="true"
          >
            <div className="mon-confirm__box">
              <div className="mon-confirm__title">
                {t.monitor.machine.bootstrapConfirmTitle}
              </div>
              <p className="mon-confirm__body">
                {msg.machineBootstrapConfirmBody(
                  bootstrapConfirmTarget.displayName
                )}
              </p>
              <div className="mon-confirm__actions">
                <button
                  type="button"
                  className="btn btn--ghost"
                  data-testid="mon-bootstrap-cancel-btn"
                  onClick={() => setBootstrapConfirmTarget(null)}
                  disabled={bootstrapBusy}
                >
                  {t.common.cancel}
                </button>
                <button
                  type="button"
                  className="btn btn--danger-ghost"
                  data-testid="mon-bootstrap-confirm-btn"
                  onClick={() => {
                    const target = bootstrapConfirmTarget;
                    setBootstrapConfirmTarget(null);
                    void installSelf(target);
                  }}
                  disabled={bootstrapBusy}
                >
                  {t.monitor.machine.bootstrapConfirm}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* install dialog (remote machines only) — a SINGLE uniform screen: copy
         * the command, run it on that machine to install the warden (the command
         * re-mints a fresh token — SECURITY: rendered into a copy control only,
         * never logged). The server-self row does NOT open this dialog (its Install
         * is an in-place bootstrap-on-server run directly). */}
        {installTarget && (
          <div
            className="mon-confirm"
            data-testid="mon-install-dialog"
            role="dialog"
            aria-modal="true"
          >
            <div className="mon-confirm__box mon-confirm__box--accent mon-install__box">
              <div className="mon-cmd__head">
                <span className="mon-confirm__title">
                  {t.monitor.machine.installTitle} · {installTarget.displayName}
                </span>
                <button
                  type="button"
                  className="btn btn--ghost mon-cmd__close"
                  aria-label={t.monitor.machine.close}
                  onClick={closeInstall}
                >
                  {t.monitor.machine.close}
                </button>
              </div>
              <p className="mon-cmd__hint">
                {t.monitor.machine.installRemoteHint}
              </p>
              <CopyBootCommandButton machineId={installTarget.machineId} />
            </div>
          </div>
        )}

        {/* uninstall guard — members are still bound to this machine. Warn (advise
         * taking them offline first) before the HOST-mutating uninstall; proceed
         * runs the same uninstall, cancel backs out. Machines with no members
         * bound skip straight to the plain confirm below. */}
        {uninstallWarnTarget && (
          <div
            className="mon-confirm"
            data-testid="mon-uninstall-warn"
            role="dialog"
            aria-modal="true"
          >
            <div className="mon-confirm__box">
              <div className="mon-confirm__title">
                {t.monitor.machine.uninstallWarnTitle}
              </div>
              <p className="mon-confirm__body">
                {msg.machineUninstallWarnBody(
                  uninstallWarnTarget.displayName,
                  membersOnMachine(uninstallWarnTarget.machineId).length
                )}
              </p>
              <ul
                className="mon-confirm__members"
                data-testid="mon-uninstall-warn-members"
              >
                {membersOnMachine(uninstallWarnTarget.machineId).map((mem) => (
                  <li key={mem.id}>{mem.name}</li>
                ))}
              </ul>
              {uninstallError && (
                <div className="mon-error">{uninstallError}</div>
              )}
              <div className="mon-confirm__actions">
                <button
                  type="button"
                  className="btn btn--ghost"
                  onClick={() => setUninstallWarnTarget(null)}
                  disabled={uninstallBusy}
                >
                  {t.common.cancel}
                </button>
                <button
                  type="button"
                  className="btn btn--danger-ghost"
                  data-testid="mon-uninstall-warn-proceed-btn"
                  onClick={() => void runUninstall(uninstallWarnTarget)}
                  disabled={uninstallBusy}
                >
                  {uninstallBusy
                    ? t.monitor.machine.uninstallBusy
                    : t.monitor.machine.uninstallWarnProceed}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* uninstall confirm (二次確認) — a HOST-mutating remote action */}
        {uninstallTarget && (
          <div
            className="mon-confirm"
            data-testid="mon-uninstall-confirm"
            role="dialog"
            aria-modal="true"
          >
            <div className="mon-confirm__box">
              <div className="mon-confirm__title">
                {t.monitor.machine.uninstallConfirmTitle}
              </div>
              <p className="mon-confirm__body">
                {msg.machineUninstallConfirmBody(
                  uninstallTarget.displayName
                )}
              </p>
              {uninstallError && (
                <div className="mon-error">{uninstallError}</div>
              )}
              <div className="mon-confirm__actions">
                <button
                  type="button"
                  className="btn btn--ghost"
                  onClick={() => setUninstallTarget(null)}
                  disabled={uninstallBusy}
                >
                  {t.common.cancel}
                </button>
                <button
                  type="button"
                  className="btn btn--danger-ghost"
                  data-testid="mon-uninstall-confirm-btn"
                  onClick={() => void confirmUninstall()}
                  disabled={uninstallBusy}
                >
                  {uninstallBusy
                    ? t.monitor.machine.uninstallBusy
                    : t.monitor.machine.uninstallConfirm}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* uninstall result — the machine is going offline (dispatched) OR was
         * already offline (nothing dispatched). The record is KEPT either way. */}
        {uninstallResult && (
          <div className="mon-cmd" data-testid="mon-uninstall-result">
            <div className="mon-cmd__head">
              <span className="mon-cmd__title">
                {t.monitor.machine.uninstallResultTitle} ·{" "}
                {uninstallResult.machine.displayName}
              </span>
              <button
                type="button"
                className="btn btn--ghost mon-cmd__close"
                aria-label={t.monitor.machine.close}
                onClick={() => setUninstallResult(null)}
              >
                {t.monitor.machine.close}
              </button>
            </div>
            <p className="mon-cmd__hint" data-testid="mon-uninstall-note">
              {uninstallResult.result.dispatched
                ? t.monitor.machine.uninstallDispatched
                : t.monitor.machine.uninstallAlreadyOffline}
            </p>
          </div>
        )}

        {/* delete confirm (二次確認) — a PURE roster soft-delete (no warden command) */}
        {deleteTarget && (
          <div
            className="mon-confirm"
            data-testid="mon-delete-confirm"
            role="dialog"
            aria-modal="true"
          >
            <div className="mon-confirm__box">
              <div className="mon-confirm__title">
                {t.monitor.machine.deleteConfirmTitle}
              </div>
              <p className="mon-confirm__body">
                {msg.machineDeleteConfirmBody(deleteTarget.displayName)}
              </p>
              {deleteError && <div className="mon-error">{deleteError}</div>}
              <div className="mon-confirm__actions">
                <button
                  type="button"
                  className="btn btn--ghost"
                  onClick={() => setDeleteTarget(null)}
                  disabled={deleteBusy}
                >
                  {t.common.cancel}
                </button>
                <button
                  type="button"
                  className="btn btn--danger-ghost"
                  data-testid="mon-delete-confirm-btn"
                  onClick={() => void confirmDelete()}
                  disabled={deleteBusy}
                >
                  {deleteBusy
                    ? t.monitor.machine.deleteBusy
                    : t.monitor.machine.deleteConfirm}
                </button>
              </div>
            </div>
          </div>
        )}
      </section>

      {/* ── §3 AI 會話 (AI Sessions) ── */}
      <section className="mon-section">
        <div className="mon-section__title">{t.monitor.sessionsTitle}</div>
        <div className="mon-table-wrap">
          <table className="mon-table mon-table--sessions">
            <thead>
              <tr>
                <th className="mon-table__left">{t.monitor.sessionCol.member}</th>
                <th className="mon-table__left">{t.monitor.sessionCol.machine}</th>
                <th className="mon-table__left">{t.monitor.sessionCol.account}</th>
                <th className="mon-table__left">{t.monitor.sessionCol.model}</th>
                <th>🧠 {t.monitor.sessionCol.context}</th>
                <th>💲 {t.monitor.sessionCol.estCost}</th>
              </tr>
            </thead>
            <tbody>
              {aiSessions.map((s) => (
                <SessionRow
                  key={s.id}
                  session={s}
                  members={members}
                  dash={dash}
                  onOpen={() => setDetailId(s.id)}
                />
              ))}
              {/* Outsource workers (O-xx) share this one table — they are live AI
               * sessions too. Rendered AFTER the member rows through the same td
               * shape so the two read as one list; the member rows above are
               * untouched. Each cell falls back to an honest dash when the worker
               * never reported that column. */}
              {outsource.workers.map((w) => (
                <OutsourceSessionRow
                  key={w.id}
                  worker={w}
                  // Telemetry columns come from the worker's OWN session row —
                  // joined by id, never from the worker DTO's configured
                  // model/effort (that pair is the launch intent and is always
                  // populated, which is precisely why a missing report was
                  // invisible here for so long).
                  session={findSessionFor(w.id, sessions)}
                  dash={dash}
                  // T-cf32: owner ruling — the whole row is clickable, SAME
                  // affordance as the member SessionRow above (no separate
                  // avatar hit-target; that option was shown and declined).
                  // The destination is the office page's EXISTING worker
                  // detail route (#office/worker/<id> — WorkerDetailPanel,
                  // already wired with every mutation there), reused via the
                  // shared setRoute/HashRoute helper — not a hand-built hash
                  // string, and not a duplicate panel embedded here.
                  onOpen={() => setRoute({ page: "office", workerId: w.id })}
                />
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

/** What this row says about the cutover — which for THREE of the four states is
 * NOTHING, and that is the contract:
 *
 *   "not_effective" proven otherwise → the short amber mark. The only state
 *                   with a face, and the face is deliberately terse.
 *   "effective"     proven in effect → silence.
 *   "unproven"      the machine checked and could not settle it → silence.
 *   null            the machine has never reported → silence.
 *
 * ⚠️ This used to be three full sentences, and they were added to fix a real
 * incident: before them all four states shared one blank, so a machine whose
 * cutover had NOT taken effect looked healthy for three hours. **That incident
 * is still fenced off** — the proven failure still has a face, it is just very
 * short now (owner 2026-08-04, rc-aaa0e7967f8a: the three sentences were too
 * long, told nobody what to do, and named a concept the reader does not have).
 *
 * What DID fall back to silence are the two "no answer" states, and that is the
 * point rather than a regression: "the box checked and could not tell" and "the
 * box has never checked" are both the ABSENCE of a verdict, and a reader who
 * finishes either sentence cannot do anything with it. A line that costs a row
 * of screen and buys no action is worse than no line. The distinction that the
 * incident was about — measured-and-failed vs everything else — is exactly the
 * one that survives.
 *
 * The mark carries NO internal vocabulary and tells nobody to restart anything:
 * this surface makes the state VISIBLE and stops there. It does not pretend to
 * explain what is wrong either — whoever sees it is meant to come and ask. */
function CutoverEffectLine({ effect }: { effect: CutoverEffect }) {
  const { t } = useI18n();
  const m = t.monitor.machine;
  if (effect !== "not_effective") return null;
  return (
    <span
      className="mon-cutover-warn"
      data-testid="mon-cutover-warning"
      role="status"
    >
      {m.cutoverNotInEffect}
    </span>
  );
}

/** The marker that turns a blank hardware cell into a STATEMENT: the number is
 * missing because the sample expired, not because this machine has never been
 * measured. It is the same `mon-stale` chip the runtime-readiness cell uses —
 * one freshness rule on the wire, one way of saying "old" on screen. */
function HardwareStaleMark() {
  const { t } = useI18n();
  return (
    <span
      className="mon-stale"
      data-testid="mon-hardware-stale"
      title={t.monitor.machine.hardwareStaleHint}
    >
      {t.monitor.machine.hardwareStale}
    </span>
  );
}

/** Whether this machine reported `key` with a value the server could not read.
 * Undefined hardware (a host with no telemetry row at all) is not a defect —
 * it is the "never measured" case, which is precisely what this mark must not
 * be confused with. */
function badHardware(hw: MonMachineView | undefined, key: string): boolean {
  return hw?.hardwareInvalid?.includes(key) === true;
}

/** The marker for a cell that is blank because the reported value was
 * UNREADABLE, not because it is old and not because it was never taken. It is
 * deliberately not the `mon-stale` chip: the two blanks have different causes
 * and different fixes (go look at that machine's warden, vs nobody has probed
 * it lately), and collapsing them back into one chip would re-create exactly
 * the ambiguity this field was added to remove. */
function HardwareBadMark() {
  const { t } = useI18n();
  return (
    <span
      className="mon-stale mon-bad"
      data-testid="mon-hardware-bad"
      title={t.monitor.machine.hardwareBadHint}
    >
      {t.monitor.machine.hardwareBad}
    </span>
  );
}

/** One runtime's cell in the machine table (T-674d) — the per-runtime version
 * columns that replaced the single ✓/✗ Runtimes digest.
 *
 * Reads the SAME capability map the digest read; nothing new is collected and
 * no version is ever synthesized. What it must NOT lose is the digest's ✗: an
 * `installed:false` / `loggedIn:false` is the reason placement refuses this
 * machine and a worker sits stamped `machine_unavailable`, and this cell is
 * still the only place that reason appears on screen. So those states are
 * spelled out as words rather than expressed by an absent version — an empty
 * cell would read as "we don't know", which is a different and wrong claim.
 *
 * The four honest outcomes, in order:
 *   never reported     → dash, titled "never probed"
 *   installed:false    → "not installed"
 *   version present    → the version verbatim
 *   installed, no ver. → "installed" (the probe answered, without a number)
 * plus a "signed out" mark whenever `loggedIn === false`, which can accompany
 * a perfectly good version — an installed, up-to-date, logged-out runtime is
 * exactly the case the operator needs to see.
 *
 * ⚠️ THAT MARK IS CURRENTLY REACHABLE FOR CODEX ONLY. Since T-b3d0 the warden
 * OMITS claude's `logged_in` when it cannot find evidence rather than sending
 * `false` (it was calling an unmeasured login a no, and placement then pinned
 * such a host to codex irreversibly — see runtimeprobe.go). Absent arrives here
 * as null, and null is not `=== false`, so a genuinely signed-out claude host
 * now shows "installed + version" with nothing saying it cannot run. That is a
 * KNOWN diagnostic loss, taken deliberately over the irreversible mis-pin; the
 * failure surfaces on the member row at spawn instead (claude_not_logged_in,
 * which names the Codex exit). Do not "fix" this by making the collector send
 * false again — restoring the badge that way restores the mis-pin with it.
 *
 * `fallbackVersion` exists only for Claude: the machine registry has carried
 * its own `claude_version` since T-97ee/T-7c5b, and an older warden reports
 * that without a capability map. Using it keeps the Claude column meaning
 * exactly what it meant before this change. Codex has no such registry field,
 * so it passes null and an absent capability stays an honest unknown.
 *
 * Freshness follows the same rule as the digest (T-b36a): capability telemetry
 * is never cleared on disconnect, so anything sourced from it is MARKED unless
 * the server says it is fresh (`stale === false`). The registry fallback is not
 * telemetry and carries no mark. */
function RuntimeVersionCell({
  capability,
  fallbackVersion,
  stale,
  testIdPrefix,
}: {
  capability?: { installed: boolean | null; loggedIn: boolean | null; version: string | null };
  fallbackVersion: string | null;
  stale: boolean | null | undefined;
  testIdPrefix: string;
}) {
  const { t } = useI18n();
  const dash = t.monitor.dash;
  const m = t.monitor.machine;

  // No capability entry at all. Claude may still have the registry version;
  // anything else is the honest "never probed" dash — never a guess.
  if (!capability) {
    return fallbackVersion != null ? (
      <span>{fallbackVersion}</span>
    ) : (
      <span title={m.runtimeUnknown}>{dash}</span>
    );
  }

  const staleMark = stale !== false && (
    <span
      className="mon-stale"
      data-testid={`${testIdPrefix}-stale`}
      title={m.runtimeStaleHint}
    >
      {m.runtimeStale}
    </span>
  );

  // A reported false is an ANSWER: say it, do not leave the cell blank.
  if (capability.installed === false) {
    return (
      <>
        <span className="mon-muted" title={m.runtimeNotInstalledHint}>
          {m.runtimeNotInstalled}
        </span>
        {staleMark}
      </>
    );
  }

  // installed === null with no version is "the probe told us nothing".
  if (capability.installed == null && capability.version == null) {
    return <span title={m.runtimeUnknown}>{dash}</span>;
  }

  return (
    <>
      {capability.version != null ? (
        <span>{capability.version}</span>
      ) : (
        <span className="mon-muted" title={m.runtimeNoVersionHint}>
          {m.runtimeNoVersion}
        </span>
      )}
      {capability.loggedIn === false && (
        <span
          className="mon-stale mon-bad"
          data-testid={`${testIdPrefix}-logged-out`}
          title={m.runtimeLoggedOutHint}
        >
          {m.runtimeLoggedOut}
        </span>
      )}
      {staleMark}
    </>
  );
}

/** The effort chip of one AI-session row, and the guard that keeps its blank
 * honest.
 *
 * Three outcomes, deliberately three:
 *   reported          → the value, in the usual `mon-badge` pill
 *   nothing reporting → nothing at all (the row's other columns are dashes too;
 *                       a marker here would be noise)
 *   reporting, absent → a `mon-stale` chip holding the dash
 *
 * The third is the whole point. An agent that is online and landing context% /
 * cost / account but never sends `effort` looked EXACTLY like one that has not
 * started reporting — both were an empty badge slot — and that is how a missing
 * field survived unnoticed for months. The chip is the same quiet muted-outline
 * marker the machine table already uses to say "this blank has a reason"
 * (`mon-stale`, T-b36a), NOT the alarming `mon-bad` tint: nothing is broken on
 * the operator's side, a value is simply owed and absent. */
function EffortBadge({
  effort,
  reporting,
  dash,
}: {
  effort: string;
  reporting: boolean;
  dash: string;
}) {
  const { t } = useI18n();
  if (effort) return <span className="mon-badge">{effort}</span>;
  if (!reporting) return null;
  return (
    <span
      className="mon-stale"
      data-testid="mon-effort-missing"
      title={t.mp.effort}
    >
      {dash}
    </span>
  );
}

/** Format a percentage, honest "—" when the source is null (never fabricated). */
function pctText(v: number | null, dash: string): string {
  return v != null ? `${Math.round(v)}%` : dash;
}

function contextText(
  pct: number | null,
  runtime: "claude" | "codex" | "",
  compactions: number | null,
  dash: string,
  compactionLabel: (n: number) => string
): string {
  const text = pctText(pct, dash);
  return runtime === "codex" && compactions != null
    ? `${text} (${compactionLabel(compactions)})`
    : text;
}

/** Power state for a machine row: AC (🔌) vs battery (🔋), with the battery
 * level appended when known. Honest "—" when no power source was reported. */
function powerText(
  acPower: boolean | null,
  batteryPct: number | null,
  dash: string
): string {
  if (acPower == null) return dash;
  const icon = acPower ? "🔌" : "🔋";
  return batteryPct != null ? `${icon} ${batteryPct}%` : icon;
}

/** A per-machine "copy install command" button — scenario 2 of the install
 * dialog (run it on ANOTHER machine). On click it RE-FETCHES the machine's boot
 * command (api.getMachineBootCommand re-mints a fresh token) and copies the
 * returned string to the clipboard. SECURITY: the command is a secret; it is
 * written straight to the clipboard and NEVER held in state, logged, or rendered.
 * A fetch/clipboard failure surfaces an honest error label (no fake "copied"). */
function CopyBootCommandButton({ machineId }: { machineId: string }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);

  async function run() {
    if (busy) return;
    setBusy(true);
    setFailed(false);
    try {
      const command = await api.getMachineBootCommand(machineId);
      await navigator.clipboard.writeText(command);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      // fetch or clipboard unavailable — honest error label, no fake success
      setFailed(true);
      window.setTimeout(() => setFailed(false), 2400);
    } finally {
      setBusy(false);
    }
  }

  const label = failed
    ? t.monitor.machine.copyBootCmdError
    : copied
      ? t.monitor.machine.copied
      : t.monitor.machine.copyBootCmd;

  return (
    <button
      type="button"
      className="btn btn--accent-ghost"
      data-testid="mon-copy-boot-cmd-btn"
      disabled={busy}
      onClick={() => void run()}
    >
      {copied ? <CheckIcon size={14} /> : <CopyIcon size={14} />}
      <span>{label}</span>
    </button>
  );
}

/** One session row. The member cell prefers the roster member (real name /
 * status / lastSeen); the click-through only appears when a roster match exists.
 * The effort badge shows the REAL live effort self-reported from the session's
 * telemetry (NOT the roster's owner-intent member.effort) — dash when unreported. */
function SessionRow({
  session,
  members,
  dash,
  onOpen,
}: {
  session: MonSessionView;
  members: Member[];
  dash: string;
  onOpen: () => void;
}) {
  const { t } = useI18n();
  const roster = members.find((m) => m.id === session.id);

  const name = roster?.name ?? session.name;
  const roleKey = roster?.role ?? session.role;
  // REAL live effort from the session's telemetry (self-reported statusLine);
  // "" → no badge (honest dash, never the roster owner-intent fallback).
  const effort = session.effort;

  // i18n label for a known seed key; a CUSTOM role falls back to the roster
  // member's server-resolved title (roleName), then the raw key.
  const roleLabel =
    (t.office.role as Record<string, string>)[roleKey] ??
    (roster?.roleName || roleKey);

  // Cumulative cost = live + banked, aligned with the detail panel est.$
  // (MemberDetailPanel). On the edge-triggered live→banked pop at ended
  // sessions the live cost moves into bankedCost, so summing keeps an idle
  // row's total instead of blinking to "—". Honest: dash only when BOTH are
  // null (no source at all); live/banked never overlap (no double-count).
  const totalCost =
    session.cost == null && session.bankedCost == null
      ? null
      : (session.cost ?? 0) + (session.bankedCost ?? 0);

  return (
    <tr
      className={roster ? "mon-row--clickable" : undefined}
      onClick={roster ? onOpen : undefined}
      role={roster ? "button" : undefined}
      tabIndex={roster ? 0 : undefined}
      onKeyDown={
        roster
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                onOpen();
              }
            }
          : undefined
      }
    >
      <td className="mon-table__left" data-label={t.monitor.sessionCol.member}>
        <div className="mon-member">
          <Avatar
            size={34}
            kind={roster ? avatarKindForMember(roster) : "member"}
            src={roster?.avatarUrl}
          />
          <div className="mon-member__body">
            <div className="mon-member__name">{name}</div>
            <div className="mon-member__sub">
              {/* Roster match → the SHARED PresenceBadge (single presence
               * truth). No roster match → session-only data has no lifecycle
               * (no real presence to show), so fall back to role ONLY — never a
               * fake last-seen "Never online". */}
              {roster ? (
                <PresenceBadge member={roster} />
              ) : (
                <span>{roleLabel}</span>
              )}
            </div>
          </div>
        </div>
      </td>
      <td className="mon-table__left" data-label={t.monitor.sessionCol.machine}>
        {session.machine || dash}
      </td>
      {/* account tag joined onto the session wire (toMonSession) — honest dash
       * when the member never reported one */}
      <td
        className={`mon-table__left${session.account ? "" : " mon-muted"}`}
        data-label={t.monitor.sessionCol.account}
      >
        {session.account || dash}
      </td>
      <td className="mon-table__left" data-label={t.monitor.sessionCol.model}>
        <span className="mon-model">{session.model || dash}</span>
        <EffortBadge
          effort={effort}
          reporting={isReportingTelemetry(session)}
          dash={dash}
        />
      </td>
      <td data-label={t.monitor.sessionCol.context}>
        {contextText(session.contextPct, session.runtime, session.compactionCount, dash, t.mp.compactionCount)}
      </td>
      <td data-label={t.monitor.sessionCol.estCost}>
        {totalCost != null ? formatCost(totalCost) : dash}
      </td>
    </tr>
  );
}

/** One outsource-worker session row. Shares the SessionRow td shape so the two
 * kinds read as a single list. The member cell shows the anonymous codename
 * (O-xx) over its task context (title → type → 任務編號) so the reader can tell
 * WHAT the worker is doing; every runtime column falls back to an honest dash
 * when the worker never reported it.
 *
 * The row IS clickable (T-cf32; owner ruling — same whole-row affordance as
 * SessionRow above, no separate avatar hit-target). This note used to say the
 * opposite ("non-clickable — a worker has no detail entry, a fake click
 * target would be dishonest"); that premise is now STALE, not current fact —
 * `WorkerDetailPanel` (frontend/src/components/WorkerDetailPanel.tsx) and its
 * route (`#office/worker/<id>`, hashRoute.ts) have existed since T-ba6b/
 * T-f190 and OfficePage's OutsourcePanel already opens them. `onOpen` (passed
 * by the caller) routes there — a REAL, already-existing destination, not an
 * invented one, so the honesty concern the old comment raised still holds; it
 * is just satisfied a different way than the member row (which stays on
 * Monitor's own `#monitor/member/<id>`; there is no monitor-scoped worker
 * route, so this one crosses to the office page that owns the panel instead
 * of duplicating it here). */
function OutsourceSessionRow({
  worker,
  session,
  dash,
  onOpen,
}: {
  worker: OutsourceWorkerView;
  /** The worker's own row in the unified `sessions` array (joined by `ow-` id).
   * `undefined` = it has reported no telemetry at all. */
  session?: MonSessionView;
  dash: string;
  onOpen: () => void;
}) {
  const { t, msg } = useI18n();

  // Task context for the sub-line: the bound task's title first, then its type
  // name, then the task number (the full id since T-5291, not a four-hex short
  // form) — honest dash when none resolved.
  const context =
    worker.taskTitle || worker.taskTypeName || worker.taskNo || dash;

  // Cumulative cost = live + banked (same summing rule as the member SessionRow);
  // honest dash only when BOTH sources are null. Read off the SESSION, not the
  // worker DTO — one telemetry source for both kinds of row.
  const totalCost =
    session?.cost == null && session?.bankedCost == null
      ? null
      : (session?.cost ?? 0) + (session?.bankedCost ?? 0);

  return (
    <tr
      className="mon-row--clickable"
      onClick={onOpen}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      data-testid="mon-outsource-row"
    >
      <td className="mon-table__left" data-label={t.monitor.sessionCol.member}>
        <div className="mon-member">
          <Avatar size={34} kind="outsource" src={worker.avatarUrl} />
          <div className="mon-member__body">
            <div className="mon-member__name">
              {worker.codename ? msg.outsourceLabel(worker.codename) : dash}
            </div>
            <div className="mon-member__sub">
              <span>{context}</span>
            </div>
          </div>
        </div>
      </td>
      {/* 機器 / 帳號 come off the SESSION, exactly like the member row above.
        * The worker DTO's own machine is the spawn DISPATCH TARGET (server
        * projectWorker prefers it over the observed host), i.e. where the
        * worker was SENT — an intent, on a surface the owner ruled must show
        * reported state. Owner accepted the cost explicitly: a just-dispatched
        * worker that has not connected yet shows a dash here rather than the
        * machine it was aimed at. */}
      <td className="mon-table__left" data-label={t.monitor.sessionCol.machine}>
        {session?.machine || dash}
      </td>
      <td
        className={`mon-table__left${session?.account ? "" : " mon-muted"}`}
        data-label={t.monitor.sessionCol.account}
      >
        {session?.account || dash}
      </td>
      <td className="mon-table__left" data-label={t.monitor.sessionCol.model}>
        <span className="mon-model">{session?.model || dash}</span>
        <EffortBadge
          effort={session?.effort ?? ""}
          reporting={isReportingTelemetry(session)}
          dash={dash}
        />
      </td>
      <td data-label={t.monitor.sessionCol.context}>
        {contextText(session?.contextPct ?? null, session?.runtime ?? "", session?.compactionCount ?? null, dash, t.mp.compactionCount)}
      </td>
      <td data-label={t.monitor.sessionCol.estCost}>
        {totalCost != null ? formatCost(totalCost) : dash}
      </td>
    </tr>
  );
}

/** One account usage card (Monitor §1). Shape-complete for the warden slice; in
 * M1 accounts is empty so this never renders. Every metric is honest — "—" when
 * the source is null. */
/** Exported for the CT visual guard (the pill's placement inside the cost line
 * and the confirm's coverage are real-browser questions). */
export function AccountCard({
  account,
  onRename,
  onDetail,
  onResetCost,
}: {
  account: MonAccountView;
  onRename: (next: string) => void;
  onDetail: () => void;
  onResetCost: () => Promise<void>;
}) {
  const { t, msg } = useI18n();
  const dash = t.monitor.dash;
  const overheated = account.sevenDay?.overheated === true;
  const costText = account.cost != null ? formatCost(account.cost) : dash;
  // 帳號歸零 is IRREVERSIBLE — nothing is retained server-side and there is no
  // undo route — so the click never fires it. The confirm is the whole safety
  // mechanism, exactly as on the member panel.
  const [costResetOpen, setCostResetOpen] = useState(false);
  const [costResetPending, setCostResetPending] = useState(false);
  const [costResetError, setCostResetError] = useState<string | null>(null);

  async function handleCostReset() {
    setCostResetPending(true);
    setCostResetError(null);
    try {
      await onResetCost();
      setCostResetOpen(false);
    } catch {
      // Keep the modal open and say so: a reset that silently failed looks
      // exactly like one that worked — the card renders the dash either way.
      setCostResetError(t.monitor.costResetError);
    } finally {
      setCostResetPending(false);
    }
  }

  return (
    <div className="mon-acct">
      <div className="mon-acct__head">
        <div className="mon-acct__id">
          <span
            className={`status-dot ${
              overheated ? "status-dot--offline mon-dot--hot" : "status-dot--online"
            }`}
            aria-hidden
          />
          {/* display_name is editable; the PATCH target is the stable id
           * (account.account, wired via onRename at the call site) */}
          <InlineEdit
            value={account.displayName}
            onCommit={onRename}
            ariaLabel={t.monitor.renameAccount}
            placeholder={t.monitor.renamePlaceholder}
            displayClassName="mon-acct__name"
          />
          {/* 機器名 chip removed (T-cb1f): it read the SAME MonAccountView.machine
           * string the 帳號詳情 modal's 使用機器 row renders, so the modal is a
           * strict superset — single- or multi-machine, the text was identical.
           * Zero information lost; the card head stays for identity + rename. */}
          {/* 詳情 entry (T-a9a7) — a dedicated small button, NOT a whole-card
           * onClick (the head already hosts the InlineEdit rename click). */}
          <button
            type="button"
            className="mon-acct__detailbtn"
            onClick={onDetail}
            data-testid="mon-acct-detail-open"
          >
            {t.monitor.detail.open}
          </button>
        </div>
        <div className="mon-acct__cost">
          {t.monitor.estimate} {costText}
          <button
            type="button"
            className="mp-cost-reset mon-acct__reset"
            data-testid="mon-acct-cost-reset"
            // Nothing accumulated ⇒ nothing to destroy. The condition is the
            // SAME one the figure renders the dash for, so the button and the
            // value can never disagree.
            disabled={costResetPending || account.cost == null}
            title={t.monitor.costResetHint}
            onClick={() => setCostResetOpen(true)}
          >
            {t.monitor.costReset}
          </button>
        </div>
      </div>
      {costResetOpen && (
        <ConfirmModal
          testId="mon-acct-cost-reset-confirm"
          confirmTestId="mon-acct-cost-reset-confirm-btn"
          body={msg.accountCostResetConfirmBody(costText)}
          error={costResetError}
          cancelLabel={t.common.cancel}
          confirmLabel={t.monitor.costResetConfirm}
          busy={costResetPending}
          danger
          onCancel={() => {
            setCostResetOpen(false);
            setCostResetError(null);
          }}
          onConfirm={() => void handleCostReset()}
        />
      )}

      <UsageBar
        label={t.monitor.fiveHour}
        usagePct={account.fiveHour?.usagePct ?? null}
        timePct={account.fiveHour?.timePct ?? null}
        measuredAt={account.fiveHour?.measuredAt ?? null}
        overheated={false}
        dash={dash}
        usageLabel={t.monitor.usage}
        timeLabel={t.monitor.time}
        overheatedLabel={t.monitor.overheated}
        measuredAgo={msg.monitorMeasuredAgo}
      />
      <UsageBar
        label={t.monitor.sevenDay}
        usagePct={account.sevenDay?.usagePct ?? null}
        timePct={account.sevenDay?.timePct ?? null}
        measuredAt={account.sevenDay?.measuredAt ?? null}
        overheated={overheated}
        dash={dash}
        usageLabel={t.monitor.usage}
        timeLabel={t.monitor.time}
        overheatedLabel={t.monitor.overheated}
        measuredAgo={msg.monitorMeasuredAgo}
      />
    </div>
  );
}

/** Split the stable account key "<account identifier>/<orgUuid>" at the LAST "/" — the
 * reporter (contextreport.readClaudeAccount, T-f694) makes the second dimension
 * the org uuid OR absent, so a bare account identifier (no "/") yields orgUuid null.
 * Exported for unit tests. */
export function splitAccountKey(key: string): {
  accountIdentifier: string;
  orgUuid: string | null;
} {
  const i = key.lastIndexOf("/");
  if (i < 0) return { accountIdentifier: key, orgUuid: null };
  return { accountIdentifier: key.slice(0, i), orgUuid: key.slice(i + 1) };
}

/** Parse the reporter label "<base>(<org>)" (contextreport.readClaudeAccountLabel
 * fixed contract: base — email, or displayName fallback — first, org name as
 * the TRAILING parenthesis; no parenthesis → no org). Exported for unit tests. */
export function parseAccountLabel(label: string): {
  base: string;
  org: string | null;
} {
  if (label.endsWith(")")) {
    const i = label.lastIndexOf("(");
    if (i > 0) {
      return { base: label.slice(0, i), org: label.slice(i + 1, -1) };
    }
  }
  return { base: label, org: null };
}

/** 帳號詳情 modal (T-a9a7) — the real identity behind one claude account row:
 * key 全文 / account identifier / org UUID 維度 / email / org / 回報標籤原文 / 機器 / 成本.
 * email+org derive ONLY from the owner-only accountLabel (null → honest "—",
 * never guessed from displayName). Same dim-overlay + card language as
 * MemberDetailPanel's 事件統計 window; closes via ✕, Esc, or the backdrop. */
function AccountDetailModal({
  account,
  onClose,
}: {
  account: MonAccountView;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const dash = t.monitor.dash;
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(onClose, rootRef);

  const { accountIdentifier, orgUuid } = splitAccountKey(account.account);
  const label =
    account.accountLabel != null ? parseAccountLabel(account.accountLabel) : null;

  const rows: { key: string; label: string; value: string; code?: boolean }[] = [
    { key: "key", label: t.monitor.detail.accountKey, value: account.account, code: true },
    { key: "account", label: t.monitor.detail.accountIdentifier, value: accountIdentifier || dash, code: true },
    {
      key: "orgUuid",
      label: t.monitor.detail.orgUuid,
      value: orgUuid ?? dash,
      code: orgUuid != null,
    },
    { key: "email", label: t.monitor.detail.email, value: label?.base || dash },
    { key: "org", label: t.monitor.detail.org, value: label?.org || dash },
    {
      key: "label",
      label: t.monitor.detail.labelRaw,
      value: account.accountLabel ?? dash,
    },
    {
      key: "machines",
      label: t.monitor.detail.machines,
      value: account.machine || dash,
    },
    {
      key: "cost",
      label: t.monitor.detail.estCost,
      value: account.cost != null ? formatCost(account.cost) : dash,
    },
  ];

  return (
    <div
      ref={rootRef}
      className="mon-detailmodal"
      role="dialog"
      aria-modal="true"
      aria-label={t.monitor.detail.title}
      data-testid="mon-acct-detail-modal"
      onClick={onClose}
    >
      <div className="mon-detailbox" onClick={(e) => e.stopPropagation()}>
        <div className="mon-detailhead">
          <span className="mon-detailtitle">{t.monitor.detail.title}</span>
          <code className="mon-detailchip">{account.displayName}</code>
          <span className="mon-detailspacer" />
          <button
            type="button"
            className="mon-detailclose"
            aria-label={t.monitor.detail.close}
            onClick={onClose}
            data-testid="mon-acct-detail-close"
          >
            <CloseIcon size={15} />
          </button>
        </div>
        <div className="mon-detailgrid" data-testid="mon-acct-detail-body">
          {rows.map((row) => (
            <div className="mon-detailrow" key={row.key}>
              <span className="mon-detaillabel">{row.label}</span>
              {row.code && row.value !== dash ? (
                <code className="mon-detailvalue mon-detailvalue--code">
                  {row.value}
                </code>
              ) : (
                <span className="mon-detailvalue">{row.value}</span>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

/** One usage window: a "用量 X% · 時間 Y%" label + a progress bar (fill = usage,
 * marker = time). Red when the window is overheated. */
function UsageBar({
  label,
  usagePct,
  timePct,
  measuredAt,
  overheated,
  dash,
  usageLabel,
  timeLabel,
  overheatedLabel,
  measuredAgo,
}: {
  label: string;
  usagePct: number | null;
  timePct: number | null;
  measuredAt: number | null;
  overheated: boolean;
  dash: string;
  usageLabel: string;
  timeLabel: string;
  overheatedLabel: string;
  measuredAgo: (age: string) => string;
}) {
  const usageText = usagePct != null ? `${usagePct}%` : dash;
  const timeText = timePct != null ? `${timePct}%` : dash;
  // The age rides ALONGSIDE the usage number, always, whenever the BE supplied
  // one — not only past some staleness threshold (T-3b90). A label that only
  // appears once a number has gone stale is itself only correct at the moment
  // you happen to look: the owner who closed this tab on day one and reopens
  // it on day three must be able to read the number's age off the card the
  // same way, and a threshold this side of the wire would be a second home for
  // a rule the BE already owns. No stamp → say nothing rather than imply "now".
  const ageText =
    usagePct != null && measuredAt != null
      ? measuredAgo(formatDuration(Math.max(0, Date.now() / 1000 - measuredAt)))
      : null;
  const fillW = usagePct != null ? Math.min(100, Math.max(0, usagePct)) : 0;
  const markerL = timePct != null ? Math.min(100, Math.max(0, timePct)) : null;

  return (
    <div className="mon-usage">
      <div className="mon-usage__label">
        <span className="mon-usage__window">{label}</span>
        <span className="mon-usage__stats">
          · {usageLabel} {usageText}
          {ageText && (
            <span className="mon-usage__age" data-testid="mon-usage-age">
              {" "}
              ({ageText})
            </span>
          )}{" "}
          · {timeLabel} {timeText}
          {overheated && (
            <span className="mon-usage__hot"> · {overheatedLabel}</span>
          )}
        </span>
      </div>
      <div className="mon-usage__track">
        <div
          className={`mon-usage__fill${overheated ? " mon-usage__fill--hot" : ""}`}
          style={{ width: `${fillW}%` }}
        />
        {markerL != null && (
          <div className="mon-usage__marker" style={{ left: `${markerL}%` }} />
        )}
      </div>
    </div>
  );
}
